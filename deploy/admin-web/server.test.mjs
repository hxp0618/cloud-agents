import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer as createHTTPSServer, request as httpsRequest } from "node:https";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";

import {
  decodeBrowserSession,
  decodeBrowserTenantPage,
  decodeEmailSuffixPolicy,
} from "../../sdk/typescript/dist/platform.mjs";
import { createWebServer } from "../web/server.mjs";

const directory = mkdtempSync(join(tmpdir(), "cloud-agents-admin-web-"));
const certificatePath = join(directory, "tls.crt");
const privateKeyPath = join(directory, "tls.key");
execFileSync(
  "openssl",
  [
    "req",
    "-x509",
    "-newkey",
    "rsa:2048",
    "-nodes",
    "-days",
    "1",
    "-subj",
    "/CN=cloud-agents-web-test",
    "-addext",
    "subjectAltName=DNS:admin.example.test,DNS:user.example.test,IP:127.0.0.1",
    "-keyout",
    privateKeyPath,
    "-out",
    certificatePath,
  ],
  { stdio: "ignore" },
);
const certificate = readFileSync(certificatePath);
const privateKey = readFileSync(privateKeyPath);
mkdirSync(join(directory, "assets"));
writeFileSync(join(directory, "index.html"), "<main>Admin Web</main>");
writeFileSync(join(directory, "assets", "app.js"), "export {};\n");

const adminCredential = "A".repeat(43);
const userCredential = "B".repeat(43);
const csrfToken = "C".repeat(43);
const tenantId = "tenant-one";
const projectId = "project-one";
const sessions = new Map();
let emailPolicy = {
  tenantId,
  resourceVersion: "1",
  allowedDomains: ["example.test"],
};
const identityRequests = [];
const controlPlaneRequests = [];

const sessionFor = (application) => ({
  application,
  user: {
    id: `account-${application}`,
    email: `${application}@example.test`,
    displayName: `${application} user`,
    displayRoles: application === "admin" ? ["platform.admin"] : [],
  },
  tenants: [
    {
      id: tenantId,
      name: "Tenant One",
      displayRoles: application === "admin" ? ["tenant.admin"] : [],
    },
  ],
  csrfToken,
});

const invitationCode = "I".repeat(43);
const invitation = {
  id: "invite-one",
  tenantId,
  email: "invited@example.test",
  roleName: "project.developer",
  scopeLevel: "project",
  scopeId: "project-one",
  verification: "admin-attested",
  state: "pending",
  createdAt: "2026-10-08T00:00:00Z",
  expiresAt: "2026-10-09T00:00:00Z",
};
const identityAccount = {
  id: "account-admin",
  subject: { kind: "user", issuer: "https://identity.example.test", subject: "user-account-admin" },
  email: "admin@example.test",
  displayName: "Admin",
  state: "active",
  platformAdmin: true,
  emailVerifiedAt: "2026-10-08T00:00:00Z",
  createdAt: "2026-10-08T00:00:00Z",
};
const oauthState = "S".repeat(43);
const reauthProof = "Q".repeat(43);
const cliState = "T".repeat(43);
const cliCode = "E".repeat(43);
const cliGrant = "G".repeat(43);
const loginMethod = {
  id: "login-method-one",
  providerId: "test-oidc",
  issuer: "https://idp.example.test",
  subject: "provider-user-one",
  createdAt: "2026-10-09T00:00:00Z",
};
const providerClient = {
  providerId: "test-oidc",
  application: "admin",
  displayName: "Example identity",
  providerKind: "oidc",
  issuer: "https://idp.example.test",
  clientId: "admin-web",
  redirectUri: "https://admin.example.test/auth/provider/callback",
  secretRef: "oidc-secret",
  scopes: ["email", "openid"],
  trustProviderEmail: false,
  allowedOrganizationIds: [],
  enabled: true,
  resourceVersion: "1",
};

function readRequestBody(request) {
  return new Promise((resolve) => {
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
  });
}

function sendJSON(response, status, value, headers = {}) {
  response.writeHead(status, { "content-type": "application/json", ...headers });
  response.end(JSON.stringify(value));
}

const identityServer = createHTTPSServer(
  { key: privateKey, cert: certificate, minVersion: "TLSv1.2" },
  async (request, response) => {
    const body = await readRequestBody(request);
    const application =
      request.headers.authorization === `Bearer ${adminCredential}`
        ? "admin"
        : request.headers.authorization === `Bearer ${userCredential}`
          ? "user"
          : undefined;
    identityRequests.push({
      application,
      body,
      headers: request.headers,
      method: request.method,
      url: request.url,
    });
    if (application === undefined) return sendJSON(response, 401, {});
    if (request.method === "GET" && request.url === "/v1/identity/login/providers")
      return sendJSON(response, 200, {
        providers: [{ id: "test-oidc", kind: "oidc", displayName: "Example identity" }],
      });
    if (request.method === "POST" && request.url === "/v1/identity/login/provider/start")
      return sendJSON(
        response,
        200,
        {
          authorizationUrl: `https://idp.example.test/authorize?state=${oauthState}`,
          expiresAt: "2030-01-01T00:00:00Z",
        },
        {
          "x-cloud-agents-oauth-state": oauthState,
        },
      );
    if (request.method === "POST" && request.url === "/v1/identity/login/provider/callback") {
      const input = JSON.parse(body);
      if (input.code === "reauth-code") {
        const rotated = `${application}-session-handle-provider-reauth`;
        sessions.get(`${application}-session-handle-password-reauth`).active = false;
        sessions.set(rotated, { active: true, application });
        return sendJSON(
          response,
          200,
          { action: "reauth", expiresAt: "2030-01-01T00:00:00Z" },
          { "x-cloud-agents-reauthentication": reauthProof, "x-cloud-agents-session": rotated },
        );
      }
      if (input.code === "link-code")
        return sendJSON(response, 200, { action: "link", loginMethod });
      const handle = `${application}-session-handle-0001`;
      sessions.set(handle, { active: true, application });
      return sendJSON(
        response,
        200,
        { action: "login", session: sessionFor(application) },
        { "x-cloud-agents-session": handle },
      );
    }
    if (request.method === "POST" && request.url === "/v1/identity/password-resets/accept")
      return response.writeHead(204).end();
    if (request.method === "POST" && request.url === "/v1/identity/login/password") {
      if (JSON.parse(body).password === "mistyped-password") return sendJSON(response, 401, {});
      const handle = `${application}-session-handle-0001`;
      sessions.set(handle, { active: true, application });
      return sendJSON(response, 200, sessionFor(application), {
        "x-cloud-agents-session": handle,
      });
    }
    if (
      request.method === "POST" &&
      request.url === "/v1/identity/invitations/accept" &&
      request.headers["x-cloud-agents-session"] === undefined
    )
      return response.writeHead(204).end();
    if (request.method === "POST" && request.url === "/v1/identity/cli/authorizations")
      return sendJSON(response, 200, {
        authorizationId: "cli-one",
        expiresAt: "2030-01-01T00:00:00Z",
      });
    if (request.method === "POST" && request.url === "/v1/identity/cli/grants/exchange")
      return sendJSON(response, 200, {
        credential: cliGrant,
        application,
        expiresAt: "2030-01-01T00:00:00Z",
      });
    if (request.method === "GET" && request.url?.startsWith("/v1/identity/cli/tenants?"))
      return sendJSON(response, 200, { tenants: sessionFor(application).tenants });
    if (request.method === "DELETE" && request.url === "/v1/identity/cli/grant")
      return response.writeHead(204).end();
    if (
      request.method === "POST" &&
      ["/v1/identity/cli/tenant-token", "/v1/identity/automation/tenant-token"].includes(
        request.url,
      )
    )
      return sendJSON(response, 200, {
        accessToken: `cli-header.${"p".repeat(32)}.${"s".repeat(32)}`,
        tokenType: "Bearer",
        expiresAt: "2030-01-01T00:00:00Z",
      });
    const handle = request.headers["x-cloud-agents-session"];
    const session = typeof handle === "string" ? sessions.get(handle) : undefined;
    if (session === undefined || !session.active) return sendJSON(response, 401, {});
    if (
      request.method === "POST" &&
      request.url === "/v1/identity/cli/authorizations/cli-one/approve"
    )
      return sendJSON(response, 200, {
        callbackPort: 41000,
        state: cliState,
        authorizationCode: cliCode,
        expiresAt: "2030-01-01T00:00:00Z",
      });
    if (request.method === "POST" && request.url === "/v1/identity/me/reauthenticate/password") {
      const rotated = `${application}-session-handle-password-reauth`;
      session.active = false;
      sessions.set(rotated, { active: true, application });
      return sendJSON(
        response,
        200,
        { expiresAt: "2030-01-01T00:00:00Z" },
        { "x-cloud-agents-reauthentication": reauthProof, "x-cloud-agents-session": rotated },
      );
    }
    if (request.method === "GET" && request.url === "/v1/identity/me/login-methods")
      return sendJSON(response, 200, { passwordEnabled: true, loginMethods: [loginMethod] });
    if (
      (request.method === "DELETE" &&
        request.url === `/v1/identity/me/login-methods/${loginMethod.id}`) ||
      (request.method === "POST" && request.url === "/v1/identity/me/password")
    )
      return response.writeHead(204).end();
    if (request.method === "GET" && request.url === "/v1/identity/providers")
      return sendJSON(response, 200, { providers: [providerClient] });
    if (
      request.method === "PUT" &&
      request.url === "/v1/identity/providers/test-oidc/applications/admin"
    )
      return sendJSON(response, 200, providerClient);
    if (
      request.method === "GET" &&
      (request.url?.startsWith("/v1/identity/accounts?") ||
        request.url?.startsWith(`/v1/identity/tenants/${tenantId}/accounts?`))
    )
      return sendJSON(response, 200, { accounts: [identityAccount] });
    if (
      request.method === "GET" &&
      (request.url?.includes("/audit-events?") ||
        request.url?.includes("/control-plane-audit-events?"))
    )
      return sendJSON(response, 200, { events: [] });
    if (
      request.method === "POST" &&
      request.url === "/v1/identity/accounts/account-admin/password-reset"
    )
      return sendJSON(response, 201, {
        userId: "account-admin",
        resetCode: "R".repeat(43),
        expiresAt: "2026-10-09T00:30:00Z",
      });
    if (
      (request.method === "POST" &&
        request.url === "/v1/identity/accounts/account-admin/disable") ||
      (request.method === "PUT" && request.url === "/v1/identity/me/password")
    )
      return response.writeHead(204).end();
    if (request.method === "GET" && request.url === "/v1/identity/session") {
      const projection = sessionFor(session.application);
      if (session.platformAdmin === false) projection.user.displayRoles = [];
      return sendJSON(response, 200, projection);
    }
    if (request.method === "GET" && request.url === "/v1/identity/me")
      return sendJSON(response, 200, sessionFor(session.application).user);
    if (request.method === "GET" && request.url?.startsWith("/v1/identity/me/tenants?"))
      return sendJSON(response, 200, { tenants: sessionFor(session.application).tenants });
    if (request.method === "DELETE" && request.url === "/v1/identity/session") {
      if (request.headers["x-csrf-token"] !== csrfToken) return sendJSON(response, 403, {});
      session.active = false;
      response.writeHead(204).end();
      return;
    }
    if (request.url === `/v1/identity/tenants/${tenantId}/email-policy`) {
      if (session.application !== "admin") return sendJSON(response, 403, {});
      if (request.method === "GET") return sendJSON(response, 200, emailPolicy);
      if (request.method === "PUT") {
        if (request.headers["x-csrf-token"] !== csrfToken) return sendJSON(response, 403, {});
        const input = JSON.parse(body);
        emailPolicy = {
          tenantId,
          resourceVersion: String(Number(emailPolicy.resourceVersion) + 1),
          allowedDomains: input.allowedDomains,
        };
        return sendJSON(response, 200, emailPolicy);
      }
    }
    if (request.method === "POST" && request.url === "/v1/identity/invitations/accept")
      return response.writeHead(204).end();
    if (request.url?.startsWith(`/v1/identity/tenants/${tenantId}/invitations`)) {
      if (request.method === "GET") return sendJSON(response, 200, { invitations: [invitation] });
      if (request.method === "DELETE") return response.writeHead(204).end();
      return sendJSON(response, 201, { invitation, invitationCode });
    }
    if (request.method === "POST" && request.url === "/v1/identity/tenant-token") {
      const input = JSON.parse(body);
      if (input.tenantId !== tenantId) return sendJSON(response, 403, {});
      const suffix = input.projectId === undefined ? "tenant" : `project-${input.projectId}`;
      return sendJSON(response, 200, {
        accessToken: `${session.application}-${suffix}-header.${"p".repeat(32)}.${"s".repeat(32)}`,
        tokenType: "Bearer",
        expiresAt: "2030-01-01T00:00:00Z",
      });
    }
    sendJSON(response, 404, {});
  },
);

const controlPlaneServer = createHTTPSServer(
  { key: privateKey, cert: certificate, minVersion: "TLSv1.2" },
  async (request, response) => {
    const body = await readRequestBody(request);
    controlPlaneRequests.push({
      body,
      headers: request.headers,
      method: request.method,
      url: request.url,
    });
    response.writeHead(201, {
      "content-type": "application/json",
      "set-cookie": "control-plane-secret=must-not-reach-browser",
      "x-resource-version": "7",
    });
    response.end('{"ok":true}\n');
  },
);

await Promise.all([
  new Promise((resolve) => identityServer.listen(0, "127.0.0.1", resolve)),
  new Promise((resolve) => controlPlaneServer.listen(0, "127.0.0.1", resolve)),
]);

const common = {
  root: directory,
  tls: { cert: certificate, key: privateKey },
  identityURL: `https://127.0.0.1:${identityServer.address().port}`,
  identityRootCA: certificate,
  controlPlaneURL: `https://127.0.0.1:${controlPlaneServer.address().port}`,
  controlPlaneRootCA: certificate,
};
const adminProxy = createWebServer({
  ...common,
  scope: "admin",
  origin: "https://admin.example.test",
  peerOrigin: "https://user.example.test",
  identityServiceCredential: adminCredential,
});
const userProxy = createWebServer({
  ...common,
  scope: "user",
  origin: "https://user.example.test",
  peerOrigin: "https://admin.example.test",
  identityServiceCredential: userCredential,
});
await Promise.all([
  new Promise((resolve) => adminProxy.listen(0, "127.0.0.1", resolve)),
  new Promise((resolve) => userProxy.listen(0, "127.0.0.1", resolve)),
]);

function browserRequest(server, hostname, path, { method = "GET", headers = {}, body } = {}) {
  return new Promise((resolve, reject) => {
    const request = httpsRequest(
      {
        ca: certificate,
        headers: { host: hostname, ...headers },
        hostname: "127.0.0.1",
        method,
        minVersion: "TLSv1.2",
        path,
        port: server.address().port,
        rejectUnauthorized: true,
        servername: hostname,
      },
      (response) => {
        const chunks = [];
        response.on("data", (chunk) => chunks.push(chunk));
        response.on("end", () =>
          resolve({
            body: Buffer.concat(chunks).toString("utf8"),
            headers: response.headers,
            status: response.statusCode,
          }),
        );
      },
    );
    request.on("error", reject);
    if (body !== undefined) request.write(body);
    request.end();
  });
}

let adminCookie;
let userCookie;

before(() => {
  assert.throws(() =>
    createWebServer({
      ...common,
      scope: "admin",
      origin: "https://same.example.test",
      peerOrigin: "https://same.example.test:444",
      identityServiceCredential: adminCredential,
    }),
  );
  assert.throws(() =>
    createWebServer({
      ...common,
      scope: "admin",
      origin: "https://admin.example.test",
      peerOrigin: "https://user.example.test",
      identityURL: "http://127.0.0.1:8081",
      identityServiceCredential: adminCredential,
    }),
  );
});

after(async () => {
  for (const server of [adminProxy, userProxy, identityServer, controlPlaneServer])
    server.closeAllConnections();
  await Promise.all(
    [adminProxy, userProxy, identityServer, controlPlaneServer].map(
      (server) => new Promise((resolve) => server.close(resolve)),
    ),
  );
  rmSync(directory, { recursive: true, force: true });
});

test("serves the SPA only on its fixed HTTPS hostname", async () => {
  const response = await browserRequest(adminProxy, "admin.example.test", "/targets");
  assert.equal(response.status, 200);
  assert.equal(response.body, "<main>Admin Web</main>");
  assert.match(response.headers["content-security-policy"], /connect-src 'self'/u);
  assert.equal((await browserRequest(adminProxy, "user.example.test", "/targets")).status, 421);
});

test("logs in through Identity without exposing its session handle", async () => {
  const denied = await browserRequest(
    adminProxy,
    "admin.example.test",
    "/v1/identity/login/password",
    {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: '{"email":"admin@example.test","password":"password"}',
    },
  );
  assert.equal(denied.status, 403);
  const malformed = await browserRequest(
    adminProxy,
    "admin.example.test",
    "/v1/identity/login/password",
    {
      method: "POST",
      headers: { "content-type": "application/json", origin: "https://admin.example.test" },
      body: "{",
    },
  );
  assert.equal(malformed.status, 400);

  const response = await browserRequest(
    adminProxy,
    "admin.example.test",
    "/v1/identity/login/password",
    {
      method: "POST",
      headers: {
        authorization: "Bearer browser-must-not-pass",
        "content-type": "application/json",
        origin: "https://admin.example.test",
        "x-cloud-agents-session": "browser-must-not-pass",
        "x-forwarded-for": "203.0.113.9",
      },
      body: '{"email":"admin@example.test","password":"password"}',
    },
  );
  assert.equal(response.status, 200);
  const session = decodeBrowserSession(JSON.parse(response.body));
  assert.equal(session.application, "admin");
  assert.equal(response.headers["x-cloud-agents-session"], undefined);
  assert.equal(response.body.includes("session-handle"), false);
  [adminCookie] = response.headers["set-cookie"];
  assert.match(
    adminCookie,
    /^__Host-cloud-agents-admin-session=[A-Za-z0-9_-]+; Secure; HttpOnly; Path=\/; SameSite=Lax$/u,
  );
  const login = identityRequests.at(-1);
  assert.equal(login.headers.authorization, `Bearer ${adminCredential}`);
  assert.notEqual(login.headers["x-cloud-agents-client-ip"], "203.0.113.9");
  assert.equal(login.headers["x-forwarded-for"], undefined);
});

test("lists tenants through the generated server client contract", async () => {
  const response = await browserRequest(
    adminProxy,
    "admin.example.test",
    "/v1/identity/me/tenants?pageSize=1",
    { headers: { cookie: adminCookie } },
  );
  assert.equal(response.status, 200);
  assert.equal(decodeBrowserTenantPage(JSON.parse(response.body)).tenants[0].id, tenantId);
  const beforeIdentity = identityRequests.length;
  const privateRoute = await browserRequest(
    adminProxy,
    "admin.example.test",
    "/v1/identity/tenant-token",
    {
      method: "POST",
      headers: {
        cookie: adminCookie,
        origin: "https://admin.example.test",
        "x-csrf-token": csrfToken,
      },
      body: JSON.stringify({ tenantId }),
    },
  );
  assert.equal(privateRoute.status, 404);
  assert.equal(identityRequests.length, beforeIdentity);
});

test("proxies the bounded Admin email policy facade without exposing session authority", async () => {
  const loaded = await browserRequest(
    adminProxy,
    "admin.example.test",
    `/v1/identity/tenants/${tenantId}/email-policy`,
    { headers: { cookie: adminCookie } },
  );
  assert.deepEqual(decodeEmailSuffixPolicy(JSON.parse(loaded.body)).allowedDomains, [
    "example.test",
  ]);

  const allowedDomains = Array.from(
    { length: 64 },
    (_, index) => `${String(index).padStart(2, "0")}.${"a".repeat(50)}.example.test`,
  );
  const body = JSON.stringify({ expectedResourceVersion: "1", allowedDomains });
  assert.ok(body.length > 4096);
  const rejected = await browserRequest(
    adminProxy,
    "admin.example.test",
    `/v1/identity/tenants/${tenantId}/email-policy`,
    {
      method: "PUT",
      headers: {
        cookie: adminCookie,
        "content-type": "application/json",
        "x-csrf-token": csrfToken,
      },
      body,
    },
  );
  assert.equal(rejected.status, 403);
  const updated = await browserRequest(
    adminProxy,
    "admin.example.test",
    `/v1/identity/tenants/${tenantId}/email-policy`,
    {
      method: "PUT",
      headers: {
        cookie: adminCookie,
        "content-type": "application/json",
        origin: "https://admin.example.test",
        "x-csrf-token": csrfToken,
      },
      body,
    },
  );
  assert.deepEqual(
    decodeEmailSuffixPolicy(JSON.parse(updated.body)).allowedDomains,
    allowedDomains,
  );
  assert.equal(updated.headers["x-cloud-agents-session"], undefined);
});

test("proxies invitation administration and capability acceptance with separate session rules", async () => {
  const userLogin = await browserRequest(
    userProxy,
    "user.example.test",
    "/v1/identity/login/password",
    {
      method: "POST",
      headers: { origin: "https://user.example.test", "content-type": "application/json" },
      body: JSON.stringify({
        email: "user@example.test",
        password: "a sufficiently long password",
      }),
    },
  );
  userCookie = userLogin.headers["set-cookie"][0].split(";")[0];
  const collection = `/v1/identity/tenants/${tenantId}/invitations`;
  const destination = await browserRequest(adminProxy, "admin.example.test", "/user-console");
  assert.equal(destination.status, 302);
  assert.equal(destination.headers.location, "https://user.example.test/");
  const adminHeaders = {
    cookie: adminCookie,
    origin: "https://admin.example.test",
    "x-csrf-token": csrfToken,
    "content-type": "application/json",
  };
  const create = {
    email: invitation.email,
    roleName: invitation.roleName,
    scopeLevel: invitation.scopeLevel,
    scopeId: invitation.scopeId,
    verification: invitation.verification,
  };
  assert.equal(
    (
      await browserRequest(userProxy, "user.example.test", collection, {
        headers: { cookie: userCookie },
      })
    ).status,
    404,
  );
  assert.equal(
    (
      await browserRequest(adminProxy, "admin.example.test", collection, {
        method: "POST",
        headers: { ...adminHeaders, origin: "https://other.example.test" },
        body: JSON.stringify(create),
      })
    ).status,
    403,
  );
  const created = await browserRequest(adminProxy, "admin.example.test", collection, {
    method: "POST",
    headers: adminHeaders,
    body: JSON.stringify(create),
  });
  assert.equal(created.status, 201);
  assert.equal(JSON.parse(created.body).invitationCode, invitationCode);
  assert.equal(created.headers["x-cloud-agents-session"], undefined);
  const listed = await browserRequest(
    adminProxy,
    "admin.example.test",
    `${collection}?pageSize=20`,
    { headers: { cookie: adminCookie } },
  );
  assert.equal(listed.status, 200);
  assert.equal(listed.body.includes(invitationCode), false);
  assert.equal(
    (
      await browserRequest(adminProxy, "admin.example.test", `${collection}/invite-one`, {
        method: "DELETE",
        headers: adminHeaders,
      })
    ).status,
    204,
  );
  const acceptPath = "/v1/identity/invitations/accept";
  const anonymousHeaders = {
    origin: "https://user.example.test",
    "content-type": "application/json",
  };
  const accepted = await browserRequest(userProxy, "user.example.test", acceptPath, {
    method: "POST",
    headers: anonymousHeaders,
    body: JSON.stringify({
      invitationCode,
      password: "a sufficiently long password",
      displayName: "Invited",
    }),
  });
  assert.equal(accepted.status, 204);
  assert.equal(accepted.headers["set-cookie"], undefined);
  assert.equal(identityRequests.at(-1).headers["x-cloud-agents-session"], undefined);
  assert.equal(typeof identityRequests.at(-1).headers["x-cloud-agents-client-ip"], "string");
  assert.notEqual(identityRequests.at(-1).headers["x-cloud-agents-client-ip"], "203.0.113.99");
  const rejected = await browserRequest(userProxy, "user.example.test", acceptPath, {
    method: "POST",
    headers: { ...anonymousHeaders, cookie: userCookie },
    body: JSON.stringify({ invitationCode }),
  });
  assert.equal(rejected.status, 403);
  const existing = await browserRequest(userProxy, "user.example.test", acceptPath, {
    method: "POST",
    headers: {
      ...anonymousHeaders,
      cookie: userCookie,
      "x-csrf-token": csrfToken,
      "x-cloud-agents-client-ip": "203.0.113.99",
      "x-forwarded-for": "203.0.113.99",
    },
    body: JSON.stringify({ invitationCode }),
  });
  assert.equal(existing.status, 204);
  assert.equal(identityRequests.at(-1).headers["x-cloud-agents-session"], userCookie.split("=")[1]);
  assert.equal(typeof identityRequests.at(-1).headers["x-cloud-agents-client-ip"], "string");
  assert.notEqual(identityRequests.at(-1).headers["x-cloud-agents-client-ip"], "203.0.113.99");
});

test("issues tenant or project tokens from the request path and strips browser authority", async () => {
  const missingCSRF = await browserRequest(
    adminProxy,
    "admin.example.test",
    `/v1/admin/tenants/${tenantId}/projects/${projectId}`,
    { headers: { cookie: adminCookie } },
  );
  assert.equal(missingCSRF.status, 403);
  const prior = controlPlaneRequests.length;

  const tenantResponse = await browserRequest(
    adminProxy,
    "admin.example.test",
    `/v1/admin/tenants/${tenantId}/projects/${projectId}`,
    {
      headers: {
        authorization: "Bearer browser-must-not-pass",
        cookie: adminCookie,
        "x-cloud-agents-session": "browser-must-not-pass",
        "x-csrf-token": csrfToken,
      },
    },
  );
  assert.equal(tenantResponse.status, 201);
  assert.deepEqual(JSON.parse(tenantResponse.body), { ok: true });
  assert.equal(tenantResponse.headers["set-cookie"], undefined);
  assert.equal(controlPlaneRequests.length, prior + 1);
  const tenantIssue = identityRequests.findLast(
    (request) => request.url === "/v1/identity/tenant-token",
  );
  assert.deepEqual(JSON.parse(tenantIssue.body), { tenantId });
  assert.equal(
    controlPlaneRequests.at(-1).headers.authorization,
    `Bearer admin-tenant-header.${"p".repeat(32)}.${"s".repeat(32)}`,
  );
  assert.equal(controlPlaneRequests.at(-1).headers.cookie, undefined);
  assert.equal(controlPlaneRequests.at(-1).headers["x-cloud-agents-session"], undefined);

  const projectResponse = await browserRequest(
    adminProxy,
    "admin.example.test",
    `/v1/admin/tenants/${tenantId}/projects/${projectId}/deployment-targets`,
    {
      method: "POST",
      headers: {
        cookie: adminCookie,
        "content-type": "application/json",
        origin: "https://admin.example.test",
        "x-csrf-token": csrfToken,
      },
      body: '{"value":1}',
    },
  );
  assert.equal(projectResponse.status, 201);
  const projectIssue = identityRequests.findLast(
    (request) => request.url === "/v1/identity/tenant-token",
  );
  assert.deepEqual(JSON.parse(projectIssue.body), { tenantId, projectId });
  assert.equal(
    controlPlaneRequests.at(-1).headers.authorization,
    `Bearer admin-project-${projectId}-header.${"p".repeat(32)}.${"s".repeat(32)}`,
  );
  assert.equal(projectResponse.body.includes("access-token"), false);
});

test("keeps Admin and User Control Plane route trees disjoint", async () => {
  const beforeIdentity = identityRequests.length;
  const beforeControlPlane = controlPlaneRequests.length;
  assert.equal(
    (
      await browserRequest(
        adminProxy,
        "admin.example.test",
        `/v1/tenants/${tenantId}/projects/${projectId}`,
      )
    ).status,
    404,
  );
  assert.equal(
    (
      await browserRequest(
        userProxy,
        "user.example.test",
        `/v1/admin/tenants/${tenantId}/projects/${projectId}`,
      )
    ).status,
    404,
  );
  assert.equal(
    (await browserRequest(userProxy, "user.example.test", `/v1/tenants/${tenantId}/memberships`))
      .status,
    404,
  );
  assert.equal(identityRequests.length, beforeIdentity);
  assert.equal(controlPlaneRequests.length, beforeControlPlane);
});

test("preserves bounded ManagedAgent and Sandbox file request sizes", async () => {
  const mistyped = await browserRequest(
    userProxy,
    "user.example.test",
    "/v1/identity/login/password",
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        origin: "https://user.example.test",
      },
      body: '{"email":"user@example.test","password":"mistyped-password"}',
    },
  );
  assert.equal(mistyped.status, 401);
  assert.equal(mistyped.headers["set-cookie"], undefined);
  const login = await browserRequest(
    userProxy,
    "user.example.test",
    "/v1/identity/login/password",
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        origin: "https://user.example.test",
      },
      body: '{"email":"user@example.test","password":"password"}',
    },
  );
  assert.equal(login.status, 200);
  [userCookie] = login.headers["set-cookie"];
  const headers = {
    cookie: userCookie,
    "content-type": "application/json",
    origin: "https://user.example.test",
    "x-csrf-token": csrfToken,
  };

  const myProjects = await browserRequest(
    userProxy,
    "user.example.test",
    `/v1/tenants/${tenantId}/my-projects?pageSize=200`,
    { headers },
  );
  assert.equal(myProjects.status, 201);
  assert.deepEqual(
    JSON.parse(
      identityRequests.findLast((request) => request.url === "/v1/identity/tenant-token").body,
    ),
    { tenantId },
  );

  const fileBody = JSON.stringify({
    path: "large.bin",
    contentBase64Url: "A".repeat((2 << 20) + 1),
  });
  const fileResponse = await browserRequest(
    userProxy,
    "user.example.test",
    `/v1/tenants/${tenantId}/projects/${projectId}/sandbox-access-grants/grant-one/files`,
    { method: "PUT", headers, body: fileBody },
  );
  assert.equal(fileResponse.status, 201);
  assert.equal(controlPlaneRequests.at(-1).body.length, fileBody.length);

  const managedAgentBody = `{"turnId":"turn-one","inputText":"${"\\u003c".repeat(1 << 20)}"}`;
  const managedAgentResponse = await browserRequest(
    userProxy,
    "user.example.test",
    `/v1/tenants/${tenantId}/projects/${projectId}/sessions/session-one/executions`,
    { method: "POST", headers, body: managedAgentBody },
  );
  assert.equal(managedAgentResponse.status, 201);
  assert.equal(controlPlaneRequests.at(-1).body.length, managedAgentBody.length);

  const tooLarge = await browserRequest(
    userProxy,
    "user.example.test",
    `/v1/tenants/${tenantId}/projects/${projectId}/sandbox-access-grants/grant-one/files`,
    {
      method: "PUT",
      headers,
      body: "A".repeat(Math.ceil(((16 << 20) * 4) / 3) + 2049),
    },
  );
  assert.equal(tooLarge.status, 413);
});

test("rejects tampered tenants, encoded paths, and cross-app sessions", async () => {
  const beforeControlPlane = controlPlaneRequests.length;
  const tampered = await browserRequest(
    adminProxy,
    "admin.example.test",
    `/v1/admin/tenants/tenant-other/projects/${projectId}`,
    { headers: { cookie: adminCookie, "x-csrf-token": csrfToken } },
  );
  assert.equal(tampered.status, 403);
  assert.equal(controlPlaneRequests.length, beforeControlPlane);
  assert.equal(
    (
      await browserRequest(
        adminProxy,
        "admin.example.test",
        `/v1/tenants/${tenantId}%2fother/projects/${projectId}`,
      )
    ).status,
    400,
  );
  const handle = adminCookie.match(/=([^;]+)/u)[1];
  const crossApp = await browserRequest(userProxy, "user.example.test", "/v1/identity/session", {
    headers: { cookie: `__Host-cloud-agents-user-session=${handle}` },
  });
  assert.equal(crossApp.status, 401);
  assert.match(crossApp.headers["set-cookie"][0], /Max-Age=0/u);
});

test("keeps account security behind current sessions, CSRF, and server-derived client IP", async () => {
  const headers = {
    cookie: adminCookie,
    origin: "https://admin.example.test",
    "x-csrf-token": csrfToken,
    "content-type": "application/json",
  };
  for (const path of [
    "/v1/identity/accounts?pageSize=20",
    `/v1/identity/tenants/${tenantId}/accounts?pageSize=20`,
    "/v1/identity/audit-events?pageSize=20",
    `/v1/identity/tenants/${tenantId}/audit-events?pageSize=20`,
    `/v1/identity/tenants/${tenantId}/control-plane-audit-events?pageSize=20`,
  ]) {
    assert.equal(
      (await browserRequest(adminProxy, "admin.example.test", path, { headers })).status,
      200,
    );
    const count = identityRequests.length;
    assert.equal(
      (
        await browserRequest(userProxy, "user.example.test", path, {
          headers: { cookie: userCookie },
        })
      ).status,
      404,
    );
    assert.equal(identityRequests.length, count);
  }
  for (const action of ["disable", "password-reset"]) {
    const path = `/v1/identity/accounts/account-admin/${action}`;
    assert.equal(
      (
        await browserRequest(adminProxy, "admin.example.test", path, {
          method: "POST",
          headers: { ...headers, origin: "https://other.example.test" },
          body: "{}",
        })
      ).status,
      403,
    );
    assert.equal(
      (
        await browserRequest(adminProxy, "admin.example.test", path, {
          method: "POST",
          headers,
          body: '{"password":"ignored-must-be-rejected"}',
        })
      ).status,
      400,
    );
    const result = await browserRequest(adminProxy, "admin.example.test", path, {
      method: "POST",
      headers,
      body: "{}",
    });
    assert.equal(result.status, action === "disable" ? 204 : 201);
    assert.equal(result.headers["x-cloud-agents-session"], undefined);
  }
  const changed = await browserRequest(
    adminProxy,
    "admin.example.test",
    "/v1/identity/me/password",
    {
      method: "PUT",
      headers,
      body: JSON.stringify({
        currentPassword: "a current long password",
        newPassword: "a replacement long password",
      }),
    },
  );
  assert.equal(changed.status, 204);
  assert.match(changed.headers["set-cookie"][0], /Max-Age=0/u);
  const resetBody = JSON.stringify({
    resetCode: "R".repeat(43),
    newPassword: "a replacement long password",
  });
  assert.equal(
    (
      await browserRequest(userProxy, "user.example.test", "/v1/identity/password-resets/accept", {
        method: "POST",
        body: resetBody,
      })
    ).status,
    403,
  );
  const reset = await browserRequest(
    userProxy,
    "user.example.test",
    "/v1/identity/password-resets/accept",
    {
      method: "POST",
      headers: {
        origin: "https://user.example.test",
        "content-type": "application/json",
        "x-forwarded-for": "203.0.113.9",
        "x-cloud-agents-client-ip": "203.0.113.10",
        "x-cloud-agents-session": "must-not-pass",
        cookie: userCookie,
      },
      body: resetBody,
    },
  );
  assert.equal(reset.status, 204);
  assert.equal(identityRequests.at(-1).headers["x-cloud-agents-session"], undefined);
  assert.notEqual(identityRequests.at(-1).headers["x-cloud-agents-client-ip"], "203.0.113.9");
  assert.notEqual(identityRequests.at(-1).headers["x-cloud-agents-client-ip"], "203.0.113.10");
  assert.match(reset.headers["set-cookie"][0], /Max-Age=0/u);
});

test("rejects unbound OAuth callbacks before exchanging any credential", async () => {
  const before = identityRequests.length;
  const state = "S".repeat(43);
  for (const [index, query] of [
    `state=${state}&code=provider-code`,
    `state=${state}&state=${state}&code=provider-code`,
    `state=${state}&code=provider-code&authCode=another-code`,
    `state=${state}&code=provider-code&redirect=https://attacker.example`,
    "error=access_denied",
  ].entries()) {
    const result = await browserRequest(
      adminProxy,
      "admin.example.test",
      `/auth/provider/callback?${query}`,
      {
        headers: {
          cookie: `__Host-cloud-agents-${index === 0 ? "user" : "admin"}-oauth-flow=${state}`,
        },
      },
    );
    assert.equal(result.status, 303);
    assert.equal(result.headers.location, "/#identity=failed");
    assert.match(
      result.headers["set-cookie"].join(";"),
      /__Host-cloud-agents-admin-oauth-flow=;.*HttpOnly.*Max-Age=0/u,
    );
    assert.equal(result.body, "");
  }
  assert.equal(identityRequests.length, before);
});

test("keeps provider state and reauthentication proofs behind the session proxy", async () => {
  const request = (path, options = {}) =>
    browserRequest(adminProxy, "admin.example.test", path, options);
  const initialLogin = await request("/v1/identity/login/password", {
    method: "POST",
    headers: { origin: "https://admin.example.test", "content-type": "application/json" },
    body: JSON.stringify({ email: "admin@example.test", password: "fixture password value" }),
  });
  assert.equal(initialLogin.status, 200);
  adminCookie = initialLogin.headers["set-cookie"][0].split(";")[0];
  const headers = {
    origin: "https://admin.example.test",
    "content-type": "application/json",
    cookie: adminCookie,
    "x-csrf-token": csrfToken,
  };
  const providers = await request("/v1/identity/login/providers");
  assert.equal(providers.status, 200);
  assert.equal(JSON.parse(providers.body).providers[0].id, "test-oidc");
  const configured = await request("/v1/identity/providers", { headers });
  assert.equal(configured.status, 200);
  assert.equal(JSON.parse(configured.body).providers[0].secretRef, "oidc-secret");
  const {
    providerId: _id,
    application: _application,
    resourceVersion: _version,
    ...configuration
  } = providerClient;
  const configuredBody = { ...configuration, expectedResourceVersion: "0" };
  assert.equal(
    (
      await request("/v1/identity/providers/test-oidc/applications/admin", {
        method: "PUT",
        headers,
        body: JSON.stringify(configuredBody),
      })
    ).status,
    200,
  );
  assert.equal(
    (
      await request("/v1/identity/providers/test-oidc/applications/admin", {
        method: "PUT",
        headers,
        body: JSON.stringify({ ...configuredBody, clientSecret: "must-not-be-accepted" }),
      })
    ).status,
    400,
  );
  const tenantHandle = "tenant-admin-session-handle";
  sessions.set(tenantHandle, { active: true, application: "admin", platformAdmin: false });
  assert.equal(
    (
      await request("/v1/identity/providers", {
        headers: { ...headers, cookie: `__Host-cloud-agents-admin-session=${tenantHandle}` },
      })
    ).status,
    403,
  );
  const started = await request("/v1/identity/login/provider/start", {
    method: "POST",
    headers: { ...headers, "x-cloud-agents-session": "browser-forged-session" },
    body: JSON.stringify({ providerId: "test-oidc", purpose: "login" }),
  });
  assert.equal(started.status, 200);
  assert.deepEqual(Object.keys(JSON.parse(started.body)).sort(), ["authorizationUrl", "expiresAt"]);
  assert.equal(started.headers["x-cloud-agents-oauth-state"], undefined);
  assert.equal(identityRequests.at(-1).headers["x-cloud-agents-session"], undefined);
  const flowCookie = started.headers["set-cookie"][0].split(";")[0];
  assert.match(
    started.headers["set-cookie"][0],
    /Secure; HttpOnly; Path=\/; SameSite=Lax; Max-Age=600$/u,
  );
  const callback = await request(
    `/auth/provider/callback?state=${oauthState}&authCode=login-code`,
    { headers: { cookie: flowCookie } },
  );
  assert.equal(callback.status, 303);
  assert.equal(callback.headers.location, "/#identity=signed-in");
  assert.equal(callback.headers["x-cloud-agents-session"], undefined);
  assert.equal(JSON.parse(identityRequests.at(-1).body).code, "login-code");
  assert.equal(callback.body, "");
  const reauth = await request("/v1/identity/me/reauthenticate/password", {
    method: "POST",
    headers,
    body: JSON.stringify({ password: "current-password-value" }),
  });
  assert.equal(reauth.status, 200);
  assert.deepEqual(Object.keys(JSON.parse(reauth.body)), ["expiresAt"]);
  assert.equal(reauth.body.includes(reauthProof), false);
  assert.equal(reauth.headers["x-cloud-agents-reauthentication"], undefined);
  const proofCookie = reauth.headers["set-cookie"][2].split(";")[0];
  const previousCookie = adminCookie;
  adminCookie = reauth.headers["set-cookie"][0].split(";")[0];
  headers.cookie = adminCookie;
  assert.notEqual(adminCookie, previousCookie);
  assert.equal(
    (await request("/v1/identity/session", { headers: { cookie: previousCookie } })).status,
    401,
  );
  assert.equal(
    (await request("/v1/identity/session", { headers: { cookie: adminCookie } })).status,
    200,
  );
  assert.match(
    reauth.headers["set-cookie"][2],
    /Secure; HttpOnly; Path=\/; SameSite=Lax; Max-Age=300$/u,
  );
  const beforeSpoofed = identityRequests.length;
  const spoofed = await request("/v1/identity/login/provider/start", {
    method: "POST",
    headers: { ...headers, "x-cloud-agents-reauthentication": reauthProof },
    body: JSON.stringify({ providerId: "test-oidc", purpose: "link" }),
  });
  assert.equal(spoofed.status, 403);
  assert.equal(identityRequests.length, beforeSpoofed + 1);
  const linked = await request("/v1/identity/login/provider/start", {
    method: "POST",
    headers: { ...headers, cookie: `${adminCookie}; ${proofCookie}` },
    body: JSON.stringify({ providerId: "test-oidc", purpose: "link" }),
  });
  assert.equal(linked.status, 200);
  assert.equal(identityRequests.at(-1).headers["x-cloud-agents-reauthentication"], reauthProof);
  assert.match(linked.headers["set-cookie"][1], /Max-Age=0$/u);
  const completed = await request(
    `/auth/provider/callback?state=${oauthState}&code=link-code&iss=https%3A%2F%2Fidp.example.test&session_state=keycloak-session`,
    { headers: { cookie: flowCookie } },
  );
  assert.equal(completed.headers.location, "/#identity=linked");
  assert.equal(JSON.parse(identityRequests.at(-1).body).issuer, "https://idp.example.test");
  assert.equal(JSON.parse(identityRequests.at(-1).body).sessionState, "keycloak-session");
  const reauthenticated = await request(
    `/auth/provider/callback?state=${oauthState}&code=reauth-code`,
    { headers: { cookie: flowCookie } },
  );
  assert.equal(reauthenticated.headers.location, "/#identity=reauthenticated");
  assert.match(
    reauthenticated.headers["set-cookie"][2],
    /__Host-cloud-agents-admin-reauth=.*HttpOnly/u,
  );
  assert.notEqual(reauthenticated.headers["set-cookie"][1].split(";")[0], adminCookie);
  assert.equal(
    (await request("/v1/identity/session", { headers: { cookie: adminCookie } })).status,
    401,
  );
  adminCookie = reauthenticated.headers["set-cookie"][1].split(";")[0];
  headers.cookie = adminCookie;
  const unlinked = await request(`/v1/identity/me/login-methods/${loginMethod.id}`, {
    method: "DELETE",
    headers: { ...headers, cookie: `${adminCookie}; ${proofCookie}` },
  });
  assert.equal(unlinked.status, 204);
  assert.match(unlinked.headers["set-cookie"][0], /Max-Age=0$/u);
  const enabled = await request("/v1/identity/me/password", {
    method: "POST",
    headers: { ...headers, cookie: `${adminCookie}; ${proofCookie}` },
    body: JSON.stringify({ newPassword: "new password with sufficient length" }),
  });
  assert.equal(enabled.status, 204);
  assert.equal(enabled.headers["set-cookie"].length, 4);
  assert.ok(enabled.headers["set-cookie"].every((cookie) => cookie.endsWith("Max-Age=0")));
  assert.equal(
    (await browserRequest(userProxy, "user.example.test", "/v1/identity/providers")).status,
    404,
  );
});

test("keeps CLI approval bound to a private flow cookie and current session", async () => {
  const request = (path, options) =>
    browserRequest(adminProxy, "admin.example.test", path, options);
  const response = await request("/v1/auth/cli/request");
  assert.equal(response.status, 200);
  assert.deepEqual(JSON.parse(response.body), { pending: false, application: "admin" });
  const start = {
    application: "admin",
    callbackPort: 41000,
    state: cliState,
    codeChallenge: "K".repeat(43),
  };
  const machineHeaders = { "content-type": "application/json" };
  const started = await request("/v1/auth/cli/start", {
    method: "POST",
    headers: machineHeaders,
    body: JSON.stringify(start),
  });
  assert.equal(started.status, 200);
  const verification = new URL(JSON.parse(started.body).verificationUrl);
  assert.equal(verification.origin, "https://admin.example.test");
  assert.equal(verification.pathname, "/auth/cli/authorize");
  assert.equal(verification.searchParams.get("state"), cliState);
  assert.equal(identityRequests.at(-1).headers["x-cloud-agents-client-ip"], "127.0.0.1");
  for (const headers of [
    { cookie: adminCookie },
    { origin: "https://admin.example.test" },
    { "sec-fetch-site": "same-origin" },
  ]) {
    assert.equal(
      (
        await request("/v1/auth/cli/start", {
          method: "POST",
          headers: { ...machineHeaders, ...headers },
          body: JSON.stringify(start),
        })
      ).status,
      403,
    );
  }
  assert.equal(
    (
      await request("/v1/auth/cli/start", {
        method: "POST",
        headers: machineHeaders,
        body: JSON.stringify({ ...start, application: "user" }),
      })
    ).status,
    403,
  );
  assert.equal(
    (await request(verification.pathname + verification.search + "&state=" + cliState)).status,
    400,
  );
  const entered = await request(verification.pathname + verification.search);
  assert.equal(entered.status, 303);
  assert.equal(entered.headers.location, "/#cli");
  assert.equal(entered.headers["referrer-policy"], "same-origin");
  assert.match(
    entered.headers["set-cookie"][0],
    /Secure; HttpOnly; Path=\/; SameSite=Lax; Max-Age=300$/u,
  );
  const flowCookie = entered.headers["set-cookie"][0].split(";")[0];
  const pending = await request("/v1/auth/cli/request", { headers: { cookie: flowCookie } });
  assert.deepEqual(JSON.parse(pending.body), { pending: true, application: "admin" });
  assert.equal(pending.body.includes(cliState), false);
  const cliPage = await request("/", { headers: { cookie: flowCookie } });
  assert.match(
    cliPage.headers["content-security-policy"],
    /form-action 'self' http:\/\/127\.0\.0\.1:\*/u,
  );
  assert.equal(cliPage.headers["referrer-policy"], "same-origin");
  const ordinaryPage = await request("/");
  assert.doesNotMatch(ordinaryPage.headers["content-security-policy"], /127\.0\.0\.1/u);
  assert.equal(ordinaryPage.headers["referrer-policy"], "no-referrer");
  // A provider login preserves the pending approval without returning its code to JavaScript.
  const callback = await request(`/auth/provider/callback?state=${oauthState}&code=login-code`, {
    headers: { cookie: `${flowCookie}; __Host-cloud-agents-admin-oauth-flow=${oauthState}` },
  });
  assert.equal(callback.headers.location, "/#cli");
  adminCookie = callback.headers["set-cookie"][1].split(";")[0];
  const formHeaders = {
    cookie: `${adminCookie}; ${flowCookie}`,
    origin: "https://admin.example.test",
    "content-type": "application/x-www-form-urlencoded",
  };
  const form = `csrfToken=${csrfToken}`;
  for (const [headers, body, status] of [
    [{ ...formHeaders, origin: "https://foreign.example.test" }, form, 403],
    [formHeaders, `csrfToken=${"X".repeat(43)}`, 403],
    [formHeaders, `${form}&csrfToken=${csrfToken}`, 400],
    [{ ...formHeaders, cookie: flowCookie }, form, 401],
    [{ ...formHeaders, cookie: adminCookie }, form, 410],
  ]) {
    assert.equal(
      (await request("/v1/auth/cli/approve", { method: "POST", headers, body })).status,
      status,
    );
  }
  const approved = await request("/v1/auth/cli/approve", {
    method: "POST",
    headers: formHeaders,
    body: form,
  });
  assert.equal(approved.status, 303);
  assert.match(
    approved.headers["content-security-policy"],
    /form-action 'self' http:\/\/127\.0\.0\.1:\*/u,
  );
  assert.equal(approved.headers["referrer-policy"], "same-origin");
  assert.equal(approved.body, "");
  assert.equal(
    approved.headers.location,
    `http://127.0.0.1:41000/callback?code=${cliCode}&state=${cliState}`,
  );
  assert.match(approved.headers["set-cookie"][0], /Max-Age=0$/u);
  assert.equal(identityRequests.at(-1).headers["x-csrf-token"], csrfToken);
  assert.deepEqual(JSON.parse(identityRequests.at(-1).body), { state: cliState });
  const cancelled = await request("/v1/auth/cli/cancel", {
    method: "POST",
    headers: formHeaders,
    body: form,
  });
  assert.equal(cancelled.status, 303);
  assert.equal(cancelled.headers.location, "/#cli=cancelled");
  const exchanged = await request("/v1/auth/cli/exchange", {
    method: "POST",
    headers: machineHeaders,
    body: JSON.stringify({
      authorizationId: "cli-one",
      authorizationCode: cliCode,
      codeVerifier: "V".repeat(43),
    }),
  });
  assert.equal(exchanged.status, 200);
  assert.equal(JSON.parse(exchanged.body).credential, cliGrant);
  const credentialHeaders = { ...machineHeaders, authorization: `Bearer ${cliGrant}` };
  const listed = await request("/v1/auth/cli/tenants?pageSize=10", { headers: credentialHeaders });
  assert.equal(listed.status, 200);
  assert.equal(identityRequests.at(-1).headers["x-cloud-agents-cli-grant"], cliGrant);
  assert.equal(identityRequests.at(-1).headers.authorization, `Bearer ${adminCredential}`);
  assert.equal(
    (await request("/v1/auth/cli/tenants?pageSize=201", { headers: credentialHeaders })).status,
    400,
  );
  for (const [path, privateHeader] of [
    ["cli", "x-cloud-agents-cli-grant"],
    ["automation", "x-cloud-agents-automation-credential"],
  ]) {
    const issued = await request(`/v1/auth/${path}/tenant-token`, {
      method: "POST",
      headers: credentialHeaders,
      body: JSON.stringify({ tenantId, projectId }),
    });
    assert.equal(issued.status, 200);
    assert.equal(identityRequests.at(-1).headers[privateHeader], cliGrant);
    assert.equal(issued.headers[privateHeader], undefined);
    assert.equal(
      (
        await request(`/v1/auth/${path}/tenant-token`, {
          method: "POST",
          headers: { ...credentialHeaders, cookie: adminCookie },
          body: JSON.stringify({ tenantId }),
        })
      ).status,
      403,
    );
  }
  assert.equal(
    (await request("/v1/auth/cli/grant", { method: "DELETE", headers: credentialHeaders })).status,
    204,
  );
});

test("logs out with Origin and CSRF checks and revokes the durable session", async () => {
  const rejected = await browserRequest(adminProxy, "admin.example.test", "/v1/identity/session", {
    method: "DELETE",
    headers: {
      cookie: adminCookie,
      origin: "https://admin.example.test",
      "x-csrf-token": "D".repeat(43),
    },
  });
  assert.equal(rejected.status, 403);
  const response = await browserRequest(adminProxy, "admin.example.test", "/v1/identity/session", {
    method: "DELETE",
    headers: {
      cookie: adminCookie,
      origin: "https://admin.example.test",
      "x-csrf-token": csrfToken,
    },
  });
  assert.equal(response.status, 204);
  assert.match(response.headers["set-cookie"][0], /Max-Age=0/u);
  assert.equal(
    (
      await browserRequest(adminProxy, "admin.example.test", "/v1/identity/session", {
        headers: { cookie: adminCookie },
      })
    ).status,
    401,
  );
});
