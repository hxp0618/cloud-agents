import { randomUUID, timingSafeEqual } from "node:crypto";
import { createReadStream, lstatSync, readFileSync, statSync } from "node:fs";
import { Agent as HTTPSAgent, createServer, request as httpsRequest } from "node:https";
import { isIP } from "node:net";
import { extname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import {
  ClientError,
  IdentityServiceClient,
  JSONContractError,
} from "../../sdk/typescript/dist/platform.mjs";

const maxAuthBodyBytes = 4 * 1024;
const maxEmailPolicyBodyBytes = 32 * 1024;
const maxProxyBodyBytes = 2 * 1024 * 1024;
const maxManagedAgentBodyBytes = 6 * 1024 * 1024 + 4 * 1024;
const maxSandboxFileWriteBodyBytes = Math.ceil((16 * 1024 * 1024 * 4) / 3) + 2048;
const maxUpstreamBodyBytes = 16 * 1024 * 1024;
const upstreamTimeoutMilliseconds = 30_000;
const proxyRequestHeaders = ["accept", "content-type", "idempotency-key", "if-match"];
const proxyResponseHeaders = [
  "content-disposition",
  "content-length",
  "content-type",
  "etag",
  "location",
  "pragma",
  "retry-after",
  "x-request-id",
  "x-resource-version",
];
const contentTypes = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".ico": "image/x-icon",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".woff2": "font/woff2",
};
const managementCollections = new Set([
  "organizations",
  "projects",
  "memberships",
  "roles",
  "role-bindings",
]);

function secure(response, cliFlow = false) {
  response.setHeader(
    "Content-Security-Policy",
    `default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'self'${cliFlow ? " http://127.0.0.1:*" : ""}; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'`,
  );
  response.setHeader("Permissions-Policy", "camera=(), geolocation=(), microphone=()");
  response.setHeader("Referrer-Policy", cliFlow ? "same-origin" : "no-referrer");
  response.setHeader("X-Content-Type-Options", "nosniff");
  response.setHeader("X-Frame-Options", "DENY");
}

function strictHTTPSURL(value, name) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${name} must be an HTTPS origin`);
  }
  if (
    value.trim() !== value ||
    url.protocol !== "https:" ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash
  )
    throw new Error(`${name} must be an HTTPS origin`);
  return url;
}

function validateApplication(scope, originValue, peerOriginValue) {
  if (scope !== "admin" && scope !== "user")
    throw new Error("CLOUD_AGENTS_WEB_SCOPE must be admin or user");
  const origin = strictHTTPSURL(originValue, "CLOUD_AGENTS_WEB_ORIGIN");
  const peerOrigin = strictHTTPSURL(peerOriginValue, "CLOUD_AGENTS_WEB_PEER_ORIGIN");
  if (origin.hostname === peerOrigin.hostname)
    throw new Error("Admin and User Web must use different HTTPS hostnames");
  return origin;
}

function parseRawURL(requestURL) {
  if (
    typeof requestURL !== "string" ||
    requestURL.length < 1 ||
    requestURL.length > 4096 ||
    !requestURL.startsWith("/") ||
    requestURL.startsWith("//") ||
    /[\\#\u0000-\u001f\u007f]/u.test(requestURL)
  )
    return undefined;
  const question = requestURL.indexOf("?");
  const rawPath = question === -1 ? requestURL : requestURL.slice(0, question);
  if (
    rawPath.includes("%") ||
    rawPath.includes("//") ||
    rawPath.endsWith("/.") ||
    rawPath.endsWith("/..")
  )
    return undefined;
  const url = new URL(requestURL, "https://web.invalid");
  return url.pathname === rawPath ? url : undefined;
}

function identifier(value) {
  return typeof value === "string" && /^[A-Za-z0-9._~-]{1,128}$/u.test(value);
}

function managementRemainder(remainder, method) {
  if (remainder[0] === "service-accounts")
    return (
      (remainder.length === 1 && (method === "GET" || method === "POST")) ||
      (remainder.length === 2 &&
        method === "POST" &&
        /^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}:(?:rotate-credential|disable)$/u.test(remainder[1]))
    );
  if (remainder.length === 0) return true;
  if (!managementCollections.has(remainder[0])) return false;
  if (remainder.length === 1) return true;
  if (remainder.length !== 2) return false;
  const actionParts = remainder[1].split(":");
  if (actionParts.length > 2) return false;
  const [resourceID, action] = actionParts;
  return (
    identifier(resourceID) && (action === undefined || /^(?:resume|suspend|revoke)$/u.test(action))
  );
}

function routeBodyLimit(method, remainder) {
  if (
    method === "PUT" &&
    remainder.length === 5 &&
    remainder[0] === "projects" &&
    identifier(remainder[1]) &&
    remainder[2] === "sandbox-access-grants" &&
    identifier(remainder[3]) &&
    remainder[4] === "files"
  )
    return maxSandboxFileWriteBodyBytes;
  if (
    method === "POST" &&
    remainder.length === 5 &&
    remainder[0] === "projects" &&
    identifier(remainder[1]) &&
    remainder[2] === "sessions" &&
    identifier(remainder[3]) &&
    (remainder[4] === "turns" || remainder[4] === "executions")
  )
    return maxManagedAgentBodyBytes;
  return maxProxyBodyBytes;
}

function controlPlaneRoute(scope, method, pathname) {
  const parts = pathname.split("/").slice(1);
  let offset;
  if (parts[0] === "v1" && parts[1] === "admin" && parts[2] === "tenants") {
    if (scope !== "admin") return undefined;
    offset = 3;
  } else if (parts[0] === "v1" && parts[1] === "tenants") {
    if (scope !== "user") return undefined;
    offset = 2;
  } else return undefined;
  const tenantId = parts[offset];
  if (!identifier(tenantId)) return undefined;
  const remainder = parts.slice(offset + 1);
  if (scope === "admin" && managementRemainder(remainder, method))
    return { tenantId, maxBodyBytes: routeBodyLimit(method, remainder) };
  if (
    scope === "user" &&
    method === "GET" &&
    remainder.length === 1 &&
    remainder[0] === "my-projects"
  )
    return { tenantId, maxBodyBytes: maxProxyBodyBytes };
  if (remainder[0] !== "projects" || !identifier(remainder[1])) return undefined;
  if (remainder.length === 2) return { tenantId, maxBodyBytes: routeBodyLimit(method, remainder) };
  return {
    tenantId,
    projectId: remainder[1],
    maxBodyBytes: routeBodyLimit(method, remainder),
  };
}

function cookieName(scope, purpose = "session") {
  return `__Host-cloud-agents-${scope}-${purpose}`;
}

function sessionCookie(scope, value, purpose = "session", maximumAge) {
  return `${cookieName(scope, purpose)}=${value}; Secure; HttpOnly; Path=/; SameSite=Lax${maximumAge === undefined ? "" : `; Max-Age=${maximumAge}`}`;
}

function expiredSessionCookie(scope, purpose = "session") {
  return sessionCookie(scope, "", purpose, 0);
}

function clearedIdentityCookies(scope) {
  return ["session", "oauth-flow", "reauth", "cli-flow"].map((purpose) =>
    expiredSessionCookie(scope, purpose),
  );
}

function readSessionCookie(request, scope, purpose = "session") {
  const raw = request.headers.cookie;
  if (typeof raw !== "string" || raw.length > 4096) return undefined;
  const prefix = `${cookieName(scope, purpose)}=`;
  const matches = raw
    .split(";")
    .map((part) => part.trim())
    .filter((part) => part.startsWith(prefix));
  if (matches.length !== 1) return undefined;
  const value = matches[0].slice(prefix.length);
  const valid =
    purpose === "cli-flow"
      ? /^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}:[A-Za-z0-9_-]{43}$/u
      : purpose === "session"
        ? /^[A-Za-z0-9_-]{16,2048}$/u
        : /^[A-Za-z0-9_-]{43}$/u;
  return valid.test(value) ? value : undefined;
}

function sameSecret(left, right) {
  if (typeof left !== "string" || typeof right !== "string") return false;
  const leftBytes = Buffer.from(left);
  const rightBytes = Buffer.from(right);
  return leftBytes.length === rightBytes.length && timingSafeEqual(leftBytes, rightBytes);
}

function requestID() {
  return `web-${randomUUID()}`;
}

function checkOrigin(request, origin) {
  return request.headers.origin === origin.origin;
}

function clientAddress(request, trustedProxy) {
  const remote = request.socket.remoteAddress;
  if (typeof remote !== "string" || isIP(remote) === 0)
    throw new Error("client address unavailable");
  if (trustedProxy !== undefined && remote === trustedProxy) {
    const forwarded = request.headers["x-forwarded-for"];
    if (typeof forwarded !== "string" || forwarded.includes(",") || isIP(forwarded) === 0)
      throw new Error("trusted proxy client address invalid");
    return forwarded;
  }
  return remote;
}

async function readBody(request, maximum) {
  const declared = request.headers["content-length"];
  if (
    declared !== undefined &&
    (!/^(?:0|[1-9][0-9]*)$/u.test(declared) || Number(declared) > maximum)
  )
    throw Object.assign(new Error("request body too large"), { status: 413 });
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > maximum) throw Object.assign(new Error("request body too large"), { status: 413 });
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, size);
}

function upstreamAgent(rootCA) {
  if (!Buffer.isBuffer(rootCA) || rootCA.length < 1)
    throw new Error("upstream root CA is required");
  return new HTTPSAgent({
    ca: rootCA,
    keepAlive: true,
    maxSockets: 64,
    minVersion: "TLSv1.2",
    rejectUnauthorized: true,
  });
}

function requestUpstream(baseURL, agent, request, maximum, signal) {
  return new Promise((resolvePromise, rejectPromise) => {
    const target = new URL(request.path, baseURL);
    if (target.origin !== baseURL.origin) {
      rejectPromise(new Error("upstream request escaped its origin"));
      return;
    }
    const outgoing = httpsRequest(
      target,
      { agent, method: request.method, headers: request.headers, signal },
      (incoming) => {
        const chunks = [];
        let size = 0;
        incoming.on("data", (chunk) => {
          size += chunk.length;
          if (size > maximum) incoming.destroy(new Error("upstream response too large"));
          else chunks.push(chunk);
        });
        incoming.on("error", rejectPromise);
        incoming.on("end", () =>
          resolvePromise({
            status: incoming.statusCode ?? 502,
            headers: Object.fromEntries(
              Object.entries(incoming.headers).flatMap(([name, value]) =>
                typeof value === "string" ? [[name, value]] : [],
              ),
            ),
            body: Buffer.concat(chunks, size),
          }),
        );
      },
    );
    outgoing.setTimeout(upstreamTimeoutMilliseconds, () =>
      outgoing.destroy(new Error("upstream timeout")),
    );
    outgoing.on("error", rejectPromise);
    if (request.body !== undefined && request.body.length > 0) outgoing.write(request.body);
    outgoing.end();
  });
}

function identityTransport(baseURL, agent, serviceCredential) {
  if (!/^[A-Za-z0-9_-]{43}$/u.test(serviceCredential))
    throw new Error("identity service credential is invalid");
  return async (request, signal) => {
    if (!request.path.startsWith("/v1/identity/") || /[\r\n]/u.test(request.path))
      throw new Error("identity request is invalid");
    const headers = { ...request.headers, authorization: `Bearer ${serviceCredential}` };
    const body = request.body === undefined ? undefined : Buffer.from(request.body);
    if (body !== undefined) {
      headers["content-type"] = "application/json";
      headers["content-length"] = String(body.length);
    }
    const response = await requestUpstream(
      baseURL,
      agent,
      { method: request.method, path: request.path, headers, body },
      2 * 1024 * 1024,
      signal,
    );
    return {
      status: response.status,
      headers: response.headers,
      body: response.body.toString("utf8"),
    };
  };
}

function problem(response, status, title, scope, clearCookie = false) {
  if (clearCookie) response.setHeader("Set-Cookie", clearedIdentityCookies(scope));
  response.writeHead(status, {
    "cache-control": "no-store",
    "content-type": "application/problem+json",
  });
  response.end(JSON.stringify({ title, status }) + "\n");
}

// Identity answers 401 both for a dead session and for a mistyped password, so
// only callers whose request carries nothing but the session handle may clear cookies.
function identityFailure(response, error, scope, sessionOnly = false) {
  const status =
    error instanceof ClientError && error.status >= 400 && error.status < 500 ? error.status : 502;
  problem(
    response,
    status,
    status === 502 ? "Identity service unavailable" : "Identity request rejected",
    scope,
    sessionOnly && status === 401,
  );
}

function json(response, status, value, extraHeaders = {}) {
  const body = Buffer.from(JSON.stringify(value) + "\n");
  response.writeHead(status, {
    "cache-control": "no-store",
    "content-length": String(body.length),
    "content-type": "application/json",
    ...extraHeaders,
  });
  response.end(body);
}

function requestAbort(request, response) {
  const controller = new AbortController();
  request.once("aborted", () => controller.abort());
  response.once("close", () => {
    if (!response.writableEnded) controller.abort();
  });
  return controller.signal;
}

async function authenticatedSession(identity, request, response, scope, signal) {
  const handle = readSessionCookie(request, scope);
  if (handle === undefined) {
    problem(response, 401, "Authentication required", scope);
    return undefined;
  }
  try {
    const session = await identity.getBrowserSession(handle, requestID(), signal);
    if (session.application !== scope) {
      problem(response, 401, "Authentication required", scope, true);
      return undefined;
    }
    return { handle, session };
  } catch (error) {
    identityFailure(response, error, scope, true);
    return undefined;
  }
}

async function handleProviderCallback(request, response, url, context) {
  const { identity, scope } = context;
  const cookies = [expiredSessionCookie(scope, "oauth-flow")];
  let outcome = "failed";
  try {
    const allowed = new Set(["state", "code", "authCode", "iss", "session_state"]);
    const keys = [...url.searchParams.keys()];
    const state = url.searchParams.get("state");
    const code = url.searchParams.get("code") ?? url.searchParams.get("authCode");
    if (
      request.method !== "GET" ||
      keys.some((key) => !allowed.has(key)) ||
      new Set(keys).size !== keys.length ||
      url.searchParams.has("code") === url.searchParams.has("authCode") ||
      !sameSecret(state, readSessionCookie(request, scope, "oauth-flow")) ||
      typeof code !== "string" ||
      code.length < 1 ||
      code.length > 2048 ||
      /[\u0000-\u001f\u007f]/u.test(code)
    )
      throw new Error("unbound provider callback");
    const result = await identity.completeProviderAuthorization(
      requestID(),
      {
        state,
        code,
        ...(url.searchParams.has("iss") ? { issuer: url.searchParams.get("iss") } : {}),
        ...(url.searchParams.has("session_state")
          ? { sessionState: url.searchParams.get("session_state") }
          : {}),
      },
      requestAbort(request, response),
    );
    if (result.action === "login" || result.action === "invitation") {
      if (result.session.application !== scope) throw new Error("identity application mismatch");
      cookies.push(
        sessionCookie(scope, result.sessionHandle),
        expiredSessionCookie(scope, "reauth"),
      );
      outcome = "signed-in";
    } else if (result.action === "reauth") {
      cookies.push(
        sessionCookie(scope, result.sessionHandle),
        sessionCookie(scope, result.reauthProof, "reauth", 300),
      );
      outcome = "reauthenticated";
    } else if (result.action === "link") {
      outcome = "linked";
    }
  } catch {
    // Callback errors never echo authorization codes or provider response bodies.
  }
  response
    .writeHead(303, {
      "cache-control": "no-store",
      "set-cookie": cookies,
      location:
        outcome === "signed-in" && readSessionCookie(request, scope, "cli-flow") !== undefined
          ? "/#cli"
          : `/#identity=${outcome}`,
    })
    .end();
}

async function handleCLI(request, response, url, context) {
  const { identity, origin, scope, trustedProxy } = context;
  const path = url.pathname;
  const flow = readSessionCookie(request, scope, "cli-flow");
  if (path === "/auth/cli/authorize") {
    const keys = [...url.searchParams.keys()];
    const id = url.searchParams.get("id");
    const state = url.searchParams.get("state");
    if (
      request.method !== "GET" ||
      keys.length !== 2 ||
      url.searchParams.getAll("id").length !== 1 ||
      url.searchParams.getAll("state").length !== 1 ||
      !/^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$/u.test(id ?? "") ||
      !/^[A-Za-z0-9_-]{43}$/u.test(state ?? "")
    )
      return problem(response, 400, "Invalid CLI authorization", scope);
    secure(response, true);
    return response
      .writeHead(303, {
        location: "/#cli",
        "set-cookie": sessionCookie(scope, `${id}:${state}`, "cli-flow", 300),
      })
      .end();
  }
  if (path === "/v1/auth/cli/request" && request.method === "GET" && url.search === "")
    return json(response, 200, { pending: flow !== undefined, application: scope });
  const signal = requestAbort(request, response);
  try {
    if (path === "/v1/auth/cli/approve" || path === "/v1/auth/cli/cancel") {
      if (request.method !== "POST" || url.search !== "")
        return problem(response, 404, "Not found", scope);
      if (!checkOrigin(request, origin)) return problem(response, 403, "Origin rejected", scope);
      if (request.headers["content-type"] !== "application/x-www-form-urlencoded")
        return problem(response, 415, "Form required", scope);
      const form = new URLSearchParams(
        (await readBody(request, maxAuthBodyBytes)).toString("utf8"),
      );
      if ([...form.keys()].length !== 1 || form.getAll("csrfToken").length !== 1)
        return problem(response, 400, "Invalid approval form", scope);
      const authenticated = await authenticatedSession(identity, request, response, scope, signal);
      if (authenticated === undefined) return;
      const csrf = form.get("csrfToken");
      if (!sameSecret(csrf, authenticated.session.csrfToken))
        return problem(response, 403, "CSRF rejected", scope);
      if (flow === undefined) return problem(response, 410, "CLI authorization expired", scope);
      response.setHeader("Set-Cookie", expiredSessionCookie(scope, "cli-flow"));
      if (path === "/v1/auth/cli/cancel")
        return response.writeHead(303, { location: "/#cli=cancelled" }).end();
      const [id, state] = flow.split(":");
      const approved = await identity.approveCLIAuthorization(
        authenticated.handle,
        id,
        requestID(),
        csrf,
        { state },
        signal,
      );
      if (
        !sameSecret(approved.state, state) ||
        !Number.isInteger(approved.callbackPort) ||
        approved.callbackPort < 1024 ||
        approved.callbackPort > 65535 ||
        !/^[A-Za-z0-9_-]{43}$/u.test(approved.authorizationCode)
      )
        throw new Error("Invalid CLI approval result");
      const callback = new URL(`http://127.0.0.1:${approved.callbackPort}/callback`);
      callback.searchParams.set("code", approved.authorizationCode);
      callback.searchParams.set("state", approved.state);
      secure(response, true);
      return response.writeHead(303, { location: callback.href }).end();
    }
    const machineRoute =
      (request.method === "POST" &&
        [
          "/v1/auth/cli/start",
          "/v1/auth/cli/exchange",
          "/v1/auth/cli/tenant-token",
          "/v1/auth/automation/tenant-token",
        ].includes(path)) ||
      (request.method === "GET" && path === "/v1/auth/cli/tenants") ||
      (request.method === "DELETE" && path === "/v1/auth/cli/grant");
    if (!machineRoute) return problem(response, 404, "Not found", scope);
    // Machine credentials never share the browser's cookie-authenticated route surface.
    if (
      request.headers.cookie !== undefined ||
      request.headers.origin !== undefined ||
      request.headers["sec-fetch-site"] !== undefined
    )
      return problem(response, 403, "CLI client required", scope);
    if (path !== "/v1/auth/cli/tenants" && url.search !== "")
      return problem(response, 400, "Invalid CLI request", scope);
    let body;
    if (request.method === "POST") {
      if (
        !/^application\/json(?:;\s*charset=utf-8)?$/iu.test(request.headers["content-type"] ?? "")
      )
        return problem(response, 415, "JSON required", scope);
      body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
    }
    if (path === "/v1/auth/cli/start" || path === "/v1/auth/cli/exchange") {
      if (request.headers.authorization !== undefined)
        return problem(response, 400, "Unexpected credential", scope);
      if (path === "/v1/auth/cli/exchange")
        return json(
          response,
          200,
          await identity.exchangeCLIGrant(
            requestID(),
            clientAddress(request, trustedProxy),
            body,
            signal,
          ),
        );
      if (body?.application !== scope) return problem(response, 403, "Application mismatch", scope);
      const started = await identity.startCLIAuthorization(
        requestID(),
        clientAddress(request, trustedProxy),
        body,
        signal,
      );
      const verification = new URL("/auth/cli/authorize", origin);
      verification.searchParams.set("id", started.authorizationId);
      verification.searchParams.set("state", body.state);
      return json(response, 200, { ...started, verificationUrl: verification.href });
    }
    const authorizationCount = request.rawHeaders.filter(
      (_, index) => index % 2 === 0 && request.rawHeaders[index].toLowerCase() === "authorization",
    ).length;
    const credential = /^Bearer ([A-Za-z0-9_-]{43})$/u.exec(
      request.headers.authorization ?? "",
    )?.[1];
    if (authorizationCount !== 1 || credential === undefined)
      return problem(response, 401, "CLI credential required", scope);
    if (path === "/v1/auth/cli/tenants") {
      const page = identityPagination(url);
      if (page === undefined) return problem(response, 400, "Invalid tenant page", scope);
      return json(
        response,
        200,
        await identity.listCLITenants(
          requestID(),
          credential,
          page.pageSize,
          page.pageToken,
          signal,
        ),
      );
    }
    if (path === "/v1/auth/cli/grant") {
      if ((await readBody(request, maxAuthBodyBytes)).length !== 0)
        return problem(response, 400, "Invalid logout request", scope);
      await identity.revokeCLIGrant(requestID(), credential, signal);
      return response.writeHead(204).end();
    }
    return json(
      response,
      200,
      path === "/v1/auth/automation/tenant-token"
        ? await identity.issueAutomationTenantToken(requestID(), credential, body, signal)
        : await identity.issueCLITenantToken(requestID(), credential, body, signal),
    );
  } catch (error) {
    if (error?.status === 413) return problem(response, 413, "Request body too large", scope);
    if (error instanceof SyntaxError || error instanceof JSONContractError)
      return problem(response, 400, "CLI request rejected", scope);
    return identityFailure(response, error, scope);
  }
}

async function handleIdentity(request, response, url, context) {
  const { identity, origin, scope, trustedProxy } = context;
  const signal = requestAbort(request, response);
  if (url.pathname === "/v1/identity/login/password" && request.method === "POST") {
    if (!checkOrigin(request, origin)) return problem(response, 403, "Origin rejected", scope);
    try {
      const body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
      const result = await identity.passwordLogin(
        requestID(),
        clientAddress(request, trustedProxy),
        body,
        signal,
      );
      if (result.session.application !== scope) throw new Error("identity application mismatch");
      json(response, 200, result.session, {
        "set-cookie": [
          sessionCookie(scope, result.sessionHandle),
          expiredSessionCookie(scope, "oauth-flow"),
          expiredSessionCookie(scope, "reauth"),
        ],
      });
    } catch (error) {
      if (error?.status === 413) problem(response, 413, "Request body too large", scope);
      else if (error instanceof SyntaxError || error instanceof JSONContractError)
        problem(response, 400, "Login request rejected", scope);
      else identityFailure(response, error, scope);
    }
    return;
  }
  const providerConfiguration =
    /^\/v1\/identity\/providers\/([A-Za-z0-9._~-]{1,128})\/applications\/(admin|user)$/u.exec(
      url.pathname,
    );
  const loginMethod = /^\/v1\/identity\/me\/login-methods\/([A-Za-z0-9._~-]{1,128})$/u.exec(
    url.pathname,
  );
  if (
    [
      "/v1/identity/login/providers",
      "/v1/identity/login/provider/start",
      "/v1/identity/me/login-methods",
      "/v1/identity/me/reauthenticate/password",
      "/v1/identity/providers",
    ].includes(url.pathname) ||
    providerConfiguration !== null ||
    loginMethod !== null ||
    (url.pathname === "/v1/identity/me/password" && request.method === "POST")
  )
    return handleProviders(
      request,
      response,
      url,
      context,
      signal,
      providerConfiguration,
      loginMethod,
    );
  const invitationMatch =
    /^\/v1\/identity\/tenants\/([A-Za-z0-9._~-]{1,128})\/invitations(?:\/([A-Za-z0-9._~-]{1,128}))?$/u.exec(
      url.pathname,
    );
  if (invitationMatch !== null || url.pathname === "/v1/identity/invitations/accept")
    return handleInvitation(request, response, url, context, signal, invitationMatch);
  const accountCollection =
    /^\/v1\/identity\/(?:tenants\/([A-Za-z0-9._~-]{1,128})\/)?(accounts|audit-events|control-plane-audit-events)$/u.exec(
      url.pathname,
    );
  const accountAction =
    /^\/v1\/identity\/accounts\/([A-Za-z0-9._~-]{1,128})\/(disable|password-reset)$/u.exec(
      url.pathname,
    );
  if (
    accountCollection !== null ||
    accountAction !== null ||
    url.pathname === "/v1/identity/me/password" ||
    url.pathname === "/v1/identity/password-resets/accept"
  )
    return handleAccountSecurity(
      request,
      response,
      url,
      context,
      signal,
      accountCollection,
      accountAction,
    );
  const emailPolicyMatch =
    /^\/v1\/identity\/tenants\/([A-Za-z0-9._~-]{1,128})\/email-policy$/u.exec(url.pathname);
  const supported =
    (url.pathname === "/v1/identity/session" &&
      (request.method === "GET" || request.method === "DELETE")) ||
    (url.pathname === "/v1/identity/me" && request.method === "GET") ||
    (url.pathname === "/v1/identity/me/tenants" && request.method === "GET") ||
    (scope === "admin" &&
      emailPolicyMatch !== null &&
      (request.method === "GET" || request.method === "PUT"));
  if (!supported) return problem(response, 404, "Not found", scope);
  const authenticated = await authenticatedSession(identity, request, response, scope, signal);
  if (authenticated === undefined) return;
  if (url.pathname === "/v1/identity/session" && request.method === "GET")
    return json(response, 200, authenticated.session);
  if (url.pathname === "/v1/identity/me" && request.method === "GET") {
    try {
      return json(
        response,
        200,
        await identity.getCurrentUser(authenticated.handle, requestID(), signal),
      );
    } catch (error) {
      return identityFailure(response, error, scope, true);
    }
  }
  if (url.pathname === "/v1/identity/me/tenants" && request.method === "GET") {
    const page = identityPagination(url);
    if (page === undefined) return problem(response, 400, "Invalid tenant page", scope);
    const { pageSize, pageToken } = page;
    try {
      return json(
        response,
        200,
        await identity.listBrowserTenants(
          authenticated.handle,
          requestID(),
          pageSize,
          pageToken,
          signal,
        ),
      );
    } catch (error) {
      return identityFailure(response, error, scope, true);
    }
  }
  if (emailPolicyMatch !== null && scope === "admin") {
    if (url.search !== "") return problem(response, 400, "Invalid email policy request", scope);
    const tenantId = emailPolicyMatch[1];
    try {
      if (request.method === "GET")
        return json(
          response,
          200,
          await identity.getEmailSuffixPolicy(authenticated.handle, tenantId, requestID(), signal),
        );
      const csrf = request.headers["x-csrf-token"];
      if (!checkOrigin(request, origin) || !sameSecret(csrf, authenticated.session.csrfToken))
        return problem(response, 403, "CSRF rejected", scope);
      const body = JSON.parse((await readBody(request, maxEmailPolicyBodyBytes)).toString("utf8"));
      return json(
        response,
        200,
        await identity.updateEmailSuffixPolicy(
          authenticated.handle,
          tenantId,
          requestID(),
          csrf,
          body,
          signal,
        ),
      );
    } catch (error) {
      if (error?.status === 413) return problem(response, 413, "Request body too large", scope);
      if (error instanceof SyntaxError || error instanceof JSONContractError)
        return problem(response, 400, "Email policy request rejected", scope);
      return identityFailure(response, error, scope, true);
    }
  }
  if (url.pathname === "/v1/identity/session" && request.method === "DELETE") {
    const csrf = request.headers["x-csrf-token"];
    if (!checkOrigin(request, origin) || !sameSecret(csrf, authenticated.session.csrfToken))
      return problem(response, 403, "CSRF rejected", scope);
    try {
      await identity.logoutBrowserSession(authenticated.handle, requestID(), csrf, signal);
      response
        .writeHead(204, {
          "cache-control": "no-store",
          "set-cookie": clearedIdentityCookies(scope),
        })
        .end();
    } catch (error) {
      identityFailure(response, error, scope, true);
    }
    return;
  }
  problem(response, 404, "Not found", scope);
}

async function handleProviders(
  request,
  response,
  url,
  context,
  signal,
  configuration,
  loginMethod,
) {
  const { identity, origin, scope, trustedProxy } = context;
  if (url.search !== "") return problem(response, 400, "Invalid provider request", scope);
  const path = url.pathname;
  const isConfiguration = configuration !== null || path === "/v1/identity/providers";
  if (isConfiguration && scope !== "admin") return problem(response, 404, "Not found", scope);
  if (request.method !== "GET" && !checkOrigin(request, origin))
    return problem(response, 403, "Origin rejected", scope);
  try {
    if (path === "/v1/identity/login/providers" && request.method === "GET")
      return json(response, 200, await identity.listLoginProviders(requestID(), signal));
    if (path === "/v1/identity/login/provider/start" && request.method === "POST") {
      const body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
      let authenticated;
      let csrf;
      if (body?.purpose === "reauth" || body?.purpose === "link") {
        authenticated = await authenticatedSession(identity, request, response, scope, signal);
        if (authenticated === undefined) return;
        csrf = request.headers["x-csrf-token"];
        if (!sameSecret(csrf, authenticated.session.csrfToken))
          return problem(response, 403, "CSRF rejected", scope);
      }
      const reauth =
        body?.purpose === "link" ? readSessionCookie(request, scope, "reauth") : undefined;
      response.setHeader("Set-Cookie", [
        expiredSessionCookie(scope, "oauth-flow"),
        expiredSessionCookie(scope, "reauth"),
      ]);
      if (body?.purpose === "link" && reauth === undefined)
        return problem(response, 403, "Reauthentication required", scope);
      const result = await identity.startProviderAuthorization(
        authenticated?.handle,
        requestID(),
        body?.purpose === "invitation" ? clientAddress(request, trustedProxy) : undefined,
        csrf,
        reauth,
        body,
        signal,
      );
      return json(response, 200, result.authorization, {
        "set-cookie": [
          sessionCookie(scope, result.state, "oauth-flow", 600),
          expiredSessionCookie(scope, "reauth"),
        ],
      });
    }
    const supported =
      (request.method === "GET" &&
        (path === "/v1/identity/me/login-methods" || path === "/v1/identity/providers")) ||
      (request.method === "POST" &&
        (path === "/v1/identity/me/reauthenticate/password" ||
          path === "/v1/identity/me/password")) ||
      (request.method === "DELETE" && loginMethod !== null) ||
      (request.method === "PUT" && configuration !== null);
    if (!supported) return problem(response, 404, "Not found", scope);
    const authenticated = await authenticatedSession(identity, request, response, scope, signal);
    if (authenticated === undefined) return;
    if (isConfiguration && !authenticated.session.user.displayRoles.includes("platform.admin"))
      return problem(response, 403, "Platform administrator required", scope);
    if (request.method === "GET")
      return json(
        response,
        200,
        path === "/v1/identity/providers"
          ? await identity.listProviderClients(authenticated.handle, requestID(), signal)
          : await identity.listLoginMethods(authenticated.handle, requestID(), signal),
      );
    const csrf = request.headers["x-csrf-token"];
    if (!sameSecret(csrf, authenticated.session.csrfToken))
      return problem(response, 403, "CSRF rejected", scope);
    if (configuration !== null) {
      const body = JSON.parse((await readBody(request, maxEmailPolicyBodyBytes)).toString("utf8"));
      return json(
        response,
        200,
        await identity.updateProviderClient(
          authenticated.handle,
          configuration[1],
          configuration[2],
          requestID(),
          csrf,
          body,
          signal,
        ),
      );
    }
    if (path === "/v1/identity/me/reauthenticate/password") {
      response.setHeader("Set-Cookie", expiredSessionCookie(scope, "reauth"));
      const body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
      const result = await identity.passwordReauthenticate(
        authenticated.handle,
        requestID(),
        csrf,
        body,
        signal,
      );
      return json(
        response,
        200,
        { expiresAt: result.expiresAt },
        {
          "set-cookie": [
            sessionCookie(scope, result.sessionHandle),
            expiredSessionCookie(scope, "oauth-flow"),
            sessionCookie(scope, result.reauthProof, "reauth", 300),
          ],
        },
      );
    }
    const reauth = readSessionCookie(request, scope, "reauth");
    response.setHeader("Set-Cookie", expiredSessionCookie(scope, "reauth"));
    if (reauth === undefined) return problem(response, 403, "Reauthentication required", scope);
    if (loginMethod !== null) {
      const body = await readBody(request, maxAuthBodyBytes);
      if (body.length !== 0) return problem(response, 400, "Invalid unlink request", scope);
      await identity.unlinkLoginMethod(
        authenticated.handle,
        loginMethod[1],
        requestID(),
        csrf,
        reauth,
        signal,
      );
    } else {
      const body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
      await identity.enablePassword(authenticated.handle, requestID(), csrf, reauth, body, signal);
      response.setHeader("Set-Cookie", clearedIdentityCookies(scope));
    }
    response.writeHead(204, { "cache-control": "no-store" }).end();
  } catch (error) {
    if (error?.status === 413) return problem(response, 413, "Request body too large", scope);
    if (error instanceof SyntaxError || error instanceof JSONContractError)
      return problem(response, 400, "Provider request rejected", scope);
    return identityFailure(response, error, scope);
  }
}

async function handleAccountSecurity(request, response, url, context, signal, collection, action) {
  const { identity, origin, scope, trustedProxy } = context;
  const reset = url.pathname === "/v1/identity/password-resets/accept";
  const passwordChange = url.pathname === "/v1/identity/me/password";
  if (
    (collection !== null && (scope !== "admin" || request.method !== "GET")) ||
    (action !== null && (scope !== "admin" || request.method !== "POST")) ||
    (reset && request.method !== "POST") ||
    (passwordChange && request.method !== "PUT")
  )
    return problem(response, 404, "Not found", scope);
  if (collection?.[2] === "control-plane-audit-events" && collection[1] === undefined)
    return problem(response, 404, "Not found", scope);
  if (collection === null && url.search !== "")
    return problem(response, 400, "Invalid account request", scope);
  if (request.method !== "GET" && !checkOrigin(request, origin))
    return problem(response, 403, "Origin rejected", scope);
  try {
    if (reset) {
      const body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
      await identity.acceptPasswordReset(
        requestID(),
        clientAddress(request, trustedProxy),
        body,
        signal,
      );
      return response
        .writeHead(204, {
          "cache-control": "no-store",
          "set-cookie": clearedIdentityCookies(scope),
        })
        .end();
    }
    const authenticated = await authenticatedSession(identity, request, response, scope, signal);
    if (authenticated === undefined) return;
    if (collection !== null) {
      const page = identityPagination(url);
      if (page === undefined) return problem(response, 400, "Invalid account page", scope);
      const { pageSize, pageToken } = page;
      const tenantId = collection[1];
      const result =
        collection[2] === "control-plane-audit-events"
          ? await identity.listControlPlaneAuditEvents(
              authenticated.handle,
              tenantId,
              requestID(),
              pageSize,
              pageToken,
              signal,
            )
          : collection[2] === "audit-events"
            ? await identity.listIdentityAuditEvents(
                authenticated.handle,
                tenantId,
                requestID(),
                pageSize,
                pageToken,
                signal,
              )
            : tenantId === undefined
              ? await identity.listIdentityAccounts(
                  authenticated.handle,
                  requestID(),
                  pageSize,
                  pageToken,
                  signal,
                )
              : await identity.listTenantIdentityAccounts(
                  authenticated.handle,
                  tenantId,
                  requestID(),
                  pageSize,
                  pageToken,
                  signal,
                );
      return json(response, 200, result);
    }
    const csrf = request.headers["x-csrf-token"];
    if (!sameSecret(csrf, authenticated.session.csrfToken))
      return problem(response, 403, "CSRF rejected", scope);
    const body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
    if (passwordChange) {
      await identity.changePassword(authenticated.handle, requestID(), csrf, body, signal);
      return response
        .writeHead(204, {
          "cache-control": "no-store",
          "set-cookie": clearedIdentityCookies(scope),
        })
        .end();
    }
    if (
      body === null ||
      typeof body !== "object" ||
      Array.isArray(body) ||
      Object.keys(body).length !== 0
    )
      return problem(response, 400, "Invalid account request", scope);
    if (action[2] === "password-reset")
      return json(
        response,
        201,
        await identity.issuePasswordReset(
          authenticated.handle,
          action[1],
          requestID(),
          csrf,
          signal,
        ),
      );
    await identity.disableIdentityAccount(
      authenticated.handle,
      action[1],
      requestID(),
      csrf,
      signal,
    );
    return response.writeHead(204, { "cache-control": "no-store" }).end();
  } catch (error) {
    if (error?.status === 413) return problem(response, 413, "Request body too large", scope);
    if (error instanceof SyntaxError || error instanceof JSONContractError)
      return problem(response, 400, "Account request rejected", scope);
    return identityFailure(response, error, scope);
  }
}

function identityPagination(url) {
  if (
    [...url.searchParams.keys()].some((key) => key !== "pageSize" && key !== "pageToken") ||
    url.searchParams.getAll("pageSize").length > 1 ||
    url.searchParams.getAll("pageToken").length > 1
  )
    return undefined;
  const pageSizeText = url.searchParams.get("pageSize") ?? "200",
    pageSize = Number(pageSizeText);
  if (
    !/^(?:[1-9]|[1-9][0-9]|1[0-9]{2}|200)$/u.test(pageSizeText) ||
    !Number.isSafeInteger(pageSize)
  )
    return undefined;
  return { pageSize, pageToken: url.searchParams.get("pageToken") ?? undefined };
}

async function handleInvitation(request, response, url, context, signal, match) {
  const { identity, origin, scope, trustedProxy } = context;
  const accept = match === null;
  if (
    accept
      ? request.method !== "POST"
      : scope !== "admin" ||
        (match[2] === undefined
          ? !["GET", "POST"].includes(request.method)
          : request.method !== "DELETE")
  )
    return problem(response, 404, "Not found", scope);
  if (request.method !== "GET" && (!checkOrigin(request, origin) || url.search !== ""))
    return problem(response, 403, "Origin or request rejected", scope);
  const hasCookie =
    typeof request.headers.cookie === "string" &&
    request.headers.cookie
      .split(";")
      .some((entry) => entry.trim().startsWith(`${cookieName(scope)}=`));
  let authenticated;
  if (!accept || hasCookie) {
    authenticated = await authenticatedSession(identity, request, response, scope, signal);
    if (authenticated === undefined) return;
  }
  const csrf = request.headers["x-csrf-token"];
  if (
    request.method !== "GET" &&
    authenticated !== undefined &&
    !sameSecret(csrf, authenticated.session.csrfToken)
  )
    return problem(response, 403, "CSRF rejected", scope);
  if (accept && authenticated === undefined && csrf !== undefined)
    return problem(response, 403, "CSRF rejected", scope);
  try {
    if (accept) {
      const body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
      await identity.acceptInvitation(
        authenticated?.handle,
        requestID(),
        clientAddress(request, trustedProxy),
        csrf,
        body,
        signal,
      );
    } else if (request.method === "GET") {
      const page = identityPagination(url);
      if (page === undefined) return problem(response, 400, "Invalid invitation page", scope);
      return json(
        response,
        200,
        await identity.listInvitations(
          authenticated.handle,
          match[1],
          requestID(),
          page.pageSize,
          page.pageToken,
          signal,
        ),
      );
    } else if (request.method === "POST") {
      const body = JSON.parse((await readBody(request, maxAuthBodyBytes)).toString("utf8"));
      return json(
        response,
        201,
        await identity.createInvitation(
          authenticated.handle,
          match[1],
          requestID(),
          csrf,
          body,
          signal,
        ),
      );
    } else
      await identity.revokeInvitation(
        authenticated.handle,
        match[1],
        match[2],
        requestID(),
        csrf,
        signal,
      );
    response.writeHead(204, { "cache-control": "no-store" }).end();
  } catch (error) {
    if (error?.status === 413) return problem(response, 413, "Request body too large", scope);
    if (error instanceof SyntaxError || error instanceof JSONContractError)
      return problem(response, 400, "Invitation request rejected", scope);
    return identityFailure(response, error, scope);
  }
}

async function handleControlPlane(request, response, url, route, context) {
  const { controlPlane, controlPlaneAgent, identity, origin, scope } = context;
  const signal = requestAbort(request, response);
  const authenticated = await authenticatedSession(identity, request, response, scope, signal);
  if (authenticated === undefined) return;
  const csrf = request.headers["x-csrf-token"];
  if (!sameSecret(csrf, authenticated.session.csrfToken))
    return problem(response, 403, "CSRF rejected", scope);
  if (!["GET", "POST", "PUT", "DELETE"].includes(request.method ?? ""))
    return problem(response, 405, "Method not allowed", scope);
  if (request.method !== "GET" && !checkOrigin(request, origin))
    return problem(response, 403, "Origin rejected", scope);
  let body;
  try {
    body = await readBody(request, route.maxBodyBytes);
  } catch (error) {
    if (error?.status === 413) {
      response.setHeader("Connection", "close");
      response.once("finish", () => request.destroy());
    }
    return problem(response, error?.status === 413 ? 413 : 400, "Request rejected", scope);
  }
  let issued;
  try {
    issued = await identity.issueTenantToken(
      authenticated.handle,
      requestID(),
      route.projectId === undefined
        ? { tenantId: route.tenantId }
        : { tenantId: route.tenantId, projectId: route.projectId },
      signal,
    );
  } catch (error) {
    return identityFailure(response, error, scope, true);
  }
  const headers = Object.fromEntries(
    proxyRequestHeaders.flatMap((name) => {
      const value = request.headers[name];
      return typeof value === "string" ? [[name, value]] : [];
    }),
  );
  headers.authorization = `Bearer ${issued.accessToken}`;
  headers["x-request-id"] = requestID();
  if (body.length > 0) headers["content-length"] = String(body.length);
  try {
    const upstream = await requestUpstream(
      controlPlane,
      controlPlaneAgent,
      { method: request.method, path: url.pathname + url.search, headers, body },
      maxUpstreamBodyBytes,
      signal,
    );
    const responseHeaders = Object.fromEntries(
      proxyResponseHeaders.flatMap((name) => {
        const value = upstream.headers[name];
        return value === undefined ? [] : [[name, value]];
      }),
    );
    response.writeHead(upstream.status, responseHeaders);
    response.end(upstream.body);
  } catch {
    problem(response, 502, `${scope === "admin" ? "Admin" : "User"} API unavailable`, scope);
  }
}

function serveAsset(request, response, url, assetRoot) {
  if (request.method !== "GET" && request.method !== "HEAD") return response.writeHead(404).end();
  if (url.pathname === "/healthz") {
    response.writeHead(200, { "cache-control": "no-store", "content-type": "text/plain" });
    return request.method === "HEAD" ? response.end() : response.end("ok\n");
  }
  let decoded;
  try {
    decoded = decodeURIComponent(url.pathname);
  } catch {
    return response.writeHead(400).end();
  }
  const relative = decoded === "/" ? "index.html" : decoded.slice(1);
  let path = resolve(assetRoot, relative);
  if (!path.startsWith(assetRoot + sep)) return response.writeHead(404).end();
  try {
    if (!statSync(path).isFile()) throw new Error("not a file");
  } catch {
    if (extname(relative) !== "") return response.writeHead(404).end();
    path = resolve(assetRoot, "index.html");
  }
  const extension = extname(path);
  response.writeHead(200, {
    "cache-control": extension === ".html" ? "no-store" : "public, max-age=31536000, immutable",
    "content-type": contentTypes[extension] ?? "application/octet-stream",
  });
  if (request.method === "HEAD") response.end();
  else
    createReadStream(path)
      .on("error", () => response.destroy())
      .pipe(response);
}

export function createWebServer({
  root,
  scope,
  origin: originValue,
  peerOrigin,
  tls,
  identityURL,
  identityRootCA,
  identityServiceCredential,
  controlPlaneURL,
  controlPlaneRootCA,
  trustedProxy,
}) {
  const origin = validateApplication(scope, originValue, peerOrigin);
  const identityEndpoint = strictHTTPSURL(identityURL, "CLOUD_AGENTS_WEB_IDENTITY_URL");
  const controlPlane = strictHTTPSURL(controlPlaneURL, "CLOUD_AGENTS_WEB_CONTROL_PLANE_URL");
  if (trustedProxy !== undefined && isIP(trustedProxy) === 0)
    throw new Error("CLOUD_AGENTS_WEB_TRUSTED_PROXY_IP must be one canonical IP address");
  if (typeof tls?.key !== "string" && !Buffer.isBuffer(tls?.key))
    throw new Error("Web TLS key is required");
  if (typeof tls?.cert !== "string" && !Buffer.isBuffer(tls?.cert))
    throw new Error("Web TLS certificate is required");
  const identityAgent = upstreamAgent(identityRootCA);
  const controlPlaneAgent = upstreamAgent(controlPlaneRootCA);
  const identity = new IdentityServiceClient(
    identityTransport(identityEndpoint, identityAgent, identityServiceCredential),
  );
  const context = {
    controlPlane,
    controlPlaneAgent,
    identity,
    origin,
    scope,
    trustedProxy,
  };
  const assetRoot = resolve(root);
  const server = createServer(
    { key: tls.key, cert: tls.cert, minVersion: "TLSv1.2" },
    (request, response) => {
      secure(
        response,
        request.method === "GET" && readSessionCookie(request, scope, "cli-flow") !== undefined,
      );
      response.setHeader("Cache-Control", "no-store");
      if (request.headers.host !== origin.host)
        return problem(response, 421, "Origin host rejected", scope);
      const url = parseRawURL(request.url);
      if (url === undefined) return problem(response, 400, "Request target rejected", scope);
      if (url.pathname === "/auth/cli/authorize" || url.pathname.startsWith("/v1/auth/")) {
        void handleCLI(request, response, url, context).catch(() => {
          if (!response.headersSent) problem(response, 500, "Request failed", scope);
          else response.destroy();
        });
        return;
      }
      if (url.pathname === "/auth/provider/callback") {
        void handleProviderCallback(request, response, url, context).catch(() => {
          if (!response.headersSent) problem(response, 500, "Request failed", scope);
          else response.destroy();
        });
        return;
      }
      if (
        request.method === "GET" &&
        url.search === "" &&
        url.pathname === (scope === "admin" ? "/user-console" : "/admin-console")
      ) {
        response
          .writeHead(302, {
            location: `${new URL(peerOrigin).origin}/`,
            "cache-control": "no-store",
          })
          .end();
        return;
      }
      if (url.pathname.startsWith("/v1/identity/")) {
        void handleIdentity(request, response, url, context).catch(() => {
          if (!response.headersSent) problem(response, 500, "Request failed", scope);
          else response.destroy();
        });
        return;
      }
      const route = controlPlaneRoute(scope, request.method, url.pathname);
      if (route !== undefined) {
        void handleControlPlane(request, response, url, route, context).catch(() => {
          if (!response.headersSent) problem(response, 500, "Request failed", scope);
          else response.destroy();
        });
        return;
      }
      if (url.pathname.startsWith("/v1/")) return problem(response, 404, "Not found", scope);
      serveAsset(request, response, url, assetRoot);
    },
  );
  server.headersTimeout = 10_000;
  server.requestTimeout = 30_000;
  server.keepAliveTimeout = 5_000;
  server.maxHeadersCount = 100;
  const close = server.close.bind(server);
  server.close = (callback) => {
    identityAgent.destroy();
    controlPlaneAgent.destroy();
    return close(callback);
  };
  return server;
}

function readConfiguredFile(path, maximum, privateFile) {
  const info = lstatSync(path);
  if (
    !info.isFile() ||
    info.isSymbolicLink() ||
    info.size < 1 ||
    info.size > maximum ||
    (privateFile && (info.mode & 0o077) !== 0)
  )
    throw new Error("configured file is invalid");
  return readFileSync(path);
}

if (process.argv[1] !== undefined && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  const scope = process.env.CLOUD_AGENTS_WEB_SCOPE;
  const port = Number(process.env.CLOUD_AGENTS_WEB_PORT);
  if (!Number.isSafeInteger(port) || port < 1 || port > 65535)
    throw new Error("CLOUD_AGENTS_WEB_PORT must be an integer from 1 to 65535");
  const identityServiceCredential = readConfiguredFile(
    process.env.CLOUD_AGENTS_WEB_IDENTITY_CREDENTIAL_FILE,
    128,
    true,
  ).toString("utf8");
  const server = createWebServer({
    root: process.env.CLOUD_AGENTS_WEB_ROOT ?? fileURLToPath(new URL("./dist", import.meta.url)),
    scope,
    origin: process.env.CLOUD_AGENTS_WEB_ORIGIN,
    peerOrigin: process.env.CLOUD_AGENTS_WEB_PEER_ORIGIN,
    tls: {
      cert: readConfiguredFile(process.env.CLOUD_AGENTS_WEB_TLS_CERT_FILE, 1024 * 1024, false),
      key: readConfiguredFile(process.env.CLOUD_AGENTS_WEB_TLS_KEY_FILE, 64 * 1024, true),
    },
    identityURL: process.env.CLOUD_AGENTS_WEB_IDENTITY_URL,
    identityRootCA: readConfiguredFile(
      process.env.CLOUD_AGENTS_WEB_IDENTITY_CA_FILE,
      1024 * 1024,
      false,
    ),
    identityServiceCredential,
    controlPlaneURL: process.env.CLOUD_AGENTS_WEB_CONTROL_PLANE_URL,
    controlPlaneRootCA: readConfiguredFile(
      process.env.CLOUD_AGENTS_WEB_CONTROL_PLANE_CA_FILE,
      1024 * 1024,
      false,
    ),
    trustedProxy: process.env.CLOUD_AGENTS_WEB_TRUSTED_PROXY_IP || undefined,
  });
  server.listen(port, "0.0.0.0", () =>
    process.stdout.write(`Cloud Agents ${scope} Web listening on :${port}\n`),
  );
  process.on("SIGTERM", () => {
    server.close();
    setTimeout(() => server.closeAllConnections(), 10_000).unref();
  });
}
