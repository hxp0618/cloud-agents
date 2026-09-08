import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";

import { createAdminWebServer } from "./server.mjs";

const directory = mkdtempSync(join(tmpdir(), "cloud-agents-admin-web-"));
mkdirSync(join(directory, "assets"));
writeFileSync(join(directory, "index.html"), "<main>Admin Web</main>");
writeFileSync(join(directory, "assets", "app.js"), "export {};\n");
let received;
const upstream = createServer((request, response) => {
  let body = "";
  request.setEncoding("utf8");
  request.on("data", (chunk) => (body += chunk));
  request.on("end", () => {
    received = {
      body,
      headers: request.headers,
      method: request.method,
      url: request.url,
    };
    response.writeHead(201, {
      "content-type": "application/json",
      "set-cookie": "secret=must-not-reach-browser",
      "x-resource-version": "7",
    });
    response.end('{"ok":true}\n');
  });
});
await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
const proxy = createAdminWebServer({
  root: directory,
  upstream: `http://127.0.0.1:${upstream.address().port}`,
});
await new Promise((resolve) => proxy.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${proxy.address().port}`;

before(() =>
  assert.throws(() =>
    createAdminWebServer({
      root: directory,
      upstream: "http://control-plane:8080",
    }),
  ),
);
after(async () => {
  await Promise.all([
    new Promise((resolve) => proxy.close(resolve)),
    new Promise((resolve) => upstream.close(resolve)),
  ]);
  rmSync(directory, { recursive: true, force: true });
});

test("serves the SPA with browser security headers", async () => {
  const response = await fetch(`${origin}/targets`);
  assert.equal(response.status, 200);
  assert.equal(await response.text(), "<main>Admin Web</main>");
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.match(response.headers.get("content-security-policy"), /connect-src 'self'/u);
  assert.equal((await fetch(`${origin}/assets/app.js`)).headers.get("x-frame-options"), "DENY");
});

test("proxies only Admin API headers and strips response cookies", async () => {
  const response = await fetch(`${origin}/v1/admin/tenants/t1?x=1`, {
    method: "POST",
    headers: {
      authorization: "Bearer admin",
      cookie: "browser-secret=1",
      "content-type": "application/json",
      "idempotency-key": "write-1",
      "x-forwarded-for": "203.0.113.1",
      "x-request-id": "request-1",
    },
    body: '{"value":1}',
  });
  assert.equal(response.status, 201);
  assert.deepEqual(await response.json(), { ok: true });
  assert.equal(response.headers.get("set-cookie"), null);
  assert.equal(response.headers.get("x-resource-version"), "7");
  assert.equal(received.url, "/v1/admin/tenants/t1?x=1");
  assert.equal(received.headers.authorization, "Bearer admin");
  assert.equal(received.headers.cookie, undefined);
  assert.equal(received.headers["x-forwarded-for"], undefined);
  assert.equal(received.body, '{"value":1}');
  const denied = await fetch(`${origin}/v1/tenants/t1`);
  assert.equal(denied.status, 404);
});
