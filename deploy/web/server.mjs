import { createReadStream, statSync } from "node:fs";
import { createServer } from "node:http";
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { extname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const requestHeaders = [
  "accept",
  "authorization",
  "content-length",
  "content-type",
  "idempotency-key",
  "if-match",
  "x-request-id",
];
const responseHeaders = [
  "cache-control",
  "content-disposition",
  "content-length",
  "content-type",
  "etag",
  "location",
  "pragma",
  "retry-after",
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

function secure(response) {
  response.setHeader(
    "Content-Security-Policy",
    "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'",
  );
  response.setHeader("Permissions-Policy", "camera=(), geolocation=(), microphone=()");
  response.setHeader("Referrer-Policy", "no-referrer");
  response.setHeader("X-Content-Type-Options", "nosniff");
  response.setHeader("X-Frame-Options", "DENY");
}

function validateUpstream(value) {
  const upstream = new URL(value);
  const loopback =
    upstream.hostname === "[::1]" || /^127(?:\.\d{1,3}){3}$/u.test(upstream.hostname);
  if (
    (upstream.protocol !== "https:" && !(upstream.protocol === "http:" && loopback)) ||
    upstream.username ||
    upstream.password ||
    upstream.pathname !== "/" ||
    upstream.search ||
    upstream.hash
  ) {
    throw new Error("CLOUD_AGENTS_WEB_UPSTREAM must be HTTPS or loopback HTTP without a path");
  }
  return upstream;
}

function apiRoute(scope, pathname) {
  if (scope === "admin") return /^\/v1\/admin(?:\/|$)/u.test(pathname);
  if (scope === "user") {
    return /^\/v1(?:\/|$)/u.test(pathname) && !/^\/v1\/admin(?:\/|$)/u.test(pathname);
  }
  throw new Error("CLOUD_AGENTS_WEB_SCOPE must be admin or user");
}

export function createWebServer({ root, upstream: upstreamValue, scope }) {
  apiRoute(scope, "/");
  const assetRoot = resolve(root);
  const upstream = validateUpstream(upstreamValue);
  return createServer((request, response) => {
    secure(response);
    const url = new URL(request.url ?? "/", "http://web.invalid");
    if (apiRoute(scope, url.pathname)) {
      const headers = Object.fromEntries(
        requestHeaders.flatMap((name) => {
          const value = request.headers[name];
          return value === undefined ? [] : [[name, value]];
        }),
      );
      const proxy = (upstream.protocol === "https:" ? httpsRequest : httpRequest)(
        new URL(url.pathname + url.search, upstream),
        { method: request.method, headers },
        (upstreamResponse) => {
          response.writeHead(
            upstreamResponse.statusCode ?? 502,
            Object.fromEntries(
              responseHeaders.flatMap((name) => {
                const value = upstreamResponse.headers[name];
                return value === undefined ? [] : [[name, value]];
              }),
            ),
          );
          upstreamResponse.pipe(response);
        },
      );
      proxy.on("error", () => {
        if (!response.headersSent) {
          response.writeHead(502, { "content-type": "application/problem+json" });
          response.end(
            `{"title":"${scope === "admin" ? "Admin" : "User"} API unavailable","status":502}\n`,
          );
        } else response.destroy();
      });
      request.pipe(proxy);
      return;
    }
    if (
      /^\/v1(?:\/|$)/u.test(url.pathname) ||
      (request.method !== "GET" && request.method !== "HEAD")
    ) {
      response.writeHead(404).end();
      return;
    }
    if (url.pathname === "/healthz") {
      response.writeHead(200, {
        "cache-control": "no-store",
        "content-type": "text/plain",
      });
      if (request.method === "HEAD") response.end();
      else response.end("ok\n");
      return;
    }
    let decoded;
    try {
      decoded = decodeURIComponent(url.pathname);
    } catch {
      response.writeHead(400).end();
      return;
    }
    const relative = decoded === "/" ? "index.html" : decoded.slice(1);
    let path = resolve(assetRoot, relative);
    if (!path.startsWith(assetRoot + sep)) {
      response.writeHead(404).end();
      return;
    }
    try {
      if (!statSync(path).isFile()) throw new Error("not a file");
    } catch {
      if (extname(relative) !== "") {
        response.writeHead(404).end();
        return;
      }
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
  });
}

if (process.argv[1] !== undefined && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  const scope = process.env.CLOUD_AGENTS_WEB_SCOPE ?? "admin";
  apiRoute(scope, "/");
  const port = Number(
    process.env.CLOUD_AGENTS_WEB_PORT ??
      process.env.CLOUD_AGENTS_ADMIN_WEB_PORT ??
      (scope === "admin" ? "4174" : "4173"),
  );
  if (!Number.isSafeInteger(port) || port < 1 || port > 65535) {
    throw new Error("CLOUD_AGENTS_WEB_PORT must be an integer from 1 to 65535");
  }
  const server = createWebServer({
    root:
      process.env.CLOUD_AGENTS_WEB_ROOT ??
      process.env.CLOUD_AGENTS_ADMIN_WEB_ROOT ??
      fileURLToPath(new URL("./dist", import.meta.url)),
    upstream:
      process.env.CLOUD_AGENTS_WEB_UPSTREAM ??
      process.env.CLOUD_AGENTS_ADMIN_WEB_UPSTREAM ??
      "https://control-plane:8080",
    scope,
  });
  server.listen(port, "0.0.0.0", () =>
    process.stdout.write(
      `Cloud Agents ${scope === "admin" ? "Admin" : "User"} Web listening on :${port}\n`,
    ),
  );
  process.on("SIGTERM", () => server.close());
}
