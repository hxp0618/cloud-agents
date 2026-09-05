import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { isDeepStrictEqual } from "node:util";
import { createHTTPClient } from "../sdk/typescript/src/platform";

// Run only against an owned local dev stack. No valid mutation body is ever sent.
const [endpoint, userTokenFile, adminTokenFile, tenant, project] = process.argv.slice(2);
assert.ok(
  endpoint && userTokenFile && adminTokenFile && tenant && project,
  "usage: bun scripts/test-admin-user-boundary.ts LOCAL_ENDPOINT USER_TOKEN_FILE ADMIN_TOKEN_FILE TENANT PROJECT",
);
const origin = new URL(endpoint);
assert.ok(
  origin.protocol === "http:" &&
    origin.hostname === "127.0.0.1" &&
    origin.pathname === "/" &&
    !origin.search &&
    !origin.hash &&
    !origin.username &&
    !origin.password,
  "Only an owned loopback dev endpoint is supported",
);
const token = () => readFileSync(userTokenFile, "utf8").trim();
const signal = () => AbortSignal.timeout(10_000);
const user = createHTTPClient(endpoint, token());
const admin = createHTTPClient(endpoint, readFileSync(adminTokenFile, "utf8").trim());
// Positive controls distinguish an ordinary authenticated user from an expired token or dead API.
await user.getProject(tenant, project, crypto.randomUUID(), signal());
await admin.listAdminDeploymentTargets(
  tenant,
  project,
  crypto.randomUUID(),
  1,
  undefined,
  signal(),
);

const contract = JSON.parse(
  readFileSync(new URL("../contracts/managed-host/v1alpha1/openapi.json", import.meta.url), "utf8"),
);
const parameters: Record<string, string> = {
  tenantId: tenant,
  projectId: project,
  profileVersion: "1",
};
let operations = 0;
let requests = 0;
for (const [path, item] of Object.entries(
  contract.paths as Record<string, Record<string, { operationId?: string }>>,
)) {
  if (!path.startsWith("/v1/admin/")) continue;
  for (const [method, operation] of Object.entries(item)) {
    if (!operation.operationId) continue;
    assert.ok(
      ["get", "post", "put"].includes(method),
      "Review safety before testing a new Admin method",
    );
    const url = new URL(
      path.replace(/\{([^}]+)\}/g, (_, key: string) =>
        encodeURIComponent(parameters[key] ?? "boundary-nonexistent"),
      ),
      origin,
    );
    url.searchParams.set("releaseDigest", `sha256:${"a".repeat(64)}`);
    for (const authenticated of [true, false]) {
      const requestId = crypto.randomUUID();
      const headers: Record<string, string> = { "X-Request-ID": requestId };
      if (authenticated) headers.Authorization = `Bearer ${token()}`;
      if (method !== "get") {
        headers["Content-Type"] = "application/json";
        headers["Idempotency-Key"] = crypto.randomUUID();
      }
      const response = await fetch(url, {
        method: method.toUpperCase(),
        headers,
        redirect: "error",
        signal: signal(),
        body: method === "get" ? undefined : "{",
      });
      const expected = authenticated ? 403 : 401;
      const label = `${operation.operationId} ${authenticated ? "user" : "anonymous"}`;
      // Do not print an unexpected body: a broken endpoint could contain secret bytes.
      assert.equal(response.status, expected, `${label}: unexpected status`);
      assert.equal(response.headers.get("cache-control"), "no-store", label);
      assert.equal(response.headers.get("x-request-id"), requestId, label);
      const problem = await response.json();
      assert.ok(
        isDeepStrictEqual(problem, {
          type: `https://problems.cloud-agents.dev/${authenticated ? "authorization-denied" : "authentication-failed"}`,
          title: authenticated ? "Authorization denied" : "Authentication failed",
          status: expected,
          requestId,
          error: {
            code: authenticated ? "AUTHORIZATION_DENIED" : "AUTHENTICATION_FAILED",
            retryable: false,
          },
        }),
        `${label}: unexpected Problem envelope`,
      );
      requests++;
    }
    operations++;
  }
}
assert.ok(operations > 0, "No Admin operations tested");
// In a fresh project, unknown infrastructure fields must be rejected before Profile lookup.
const profiles = (
  await admin.listAdminEnvironmentProfiles(
    tenant,
    project,
    crypto.randomUUID(),
    1,
    undefined,
    signal(),
  )
).value;
assert.equal(profiles.environmentProfiles.length, 0, "Use a disposable project without Profiles");
const forbiddenFields = [
  "endpoint",
  "credentialRef",
  "providerCredentialRef",
  "targetId",
  "releaseDigest",
  "cpuLimitMillis",
  "memoryLimitBytes",
  "kubeconfig",
  "sshHost",
];
for (const field of forbiddenFields) {
  const response = await fetch(
    new URL(
      `/v1/tenants/${encodeURIComponent(tenant)}/projects/${encodeURIComponent(project)}/environments`,
      origin,
    ),
    {
      method: "POST",
      redirect: "error",
      signal: signal(),
      headers: {
        Authorization: `Bearer ${token()}`,
        "X-Request-ID": crypto.randomUUID(),
        "Idempotency-Key": crypto.randomUUID(),
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        profileId: "boundary-nonexistent",
        profileVersion: 1,
        [field]: "boundary-forbidden",
      }),
    },
  );
  assert.equal(response.status, 400, `${field}: infrastructure input was not rejected`);
  const problem = await response.json();
  assert.ok(problem.error?.code === "INVALID_REQUEST", `${field}: unexpected rejection code`);
}
process.stdout.write(
  JSON.stringify({
    operations,
    requests,
    authenticatedUser: 403,
    anonymous: 401,
    rejectedUserInfrastructureFields: forbiddenFields.length,
  }) + "\n",
);
