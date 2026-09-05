import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import {
  ClientError,
  createHTTPClient,
  type DeploymentTargetRegisterRequest,
} from "../sdk/typescript/src/platform";

// Registration only, in a fresh disposable project; never Probe these endpoints.
const [endpoint, adminTokenFile, tenant, project] = process.argv.slice(2);
assert.ok(
  endpoint && adminTokenFile && tenant && project,
  "usage: bun scripts/test-admin-target-registration.ts ENDPOINT ADMIN_TOKEN_FILE TENANT EMPTY_DISPOSABLE_PROJECT",
);
const client = createHTTPClient(endpoint, readFileSync(adminTokenFile, "utf8").trim());
const signal = () => AbortSignal.timeout(30_000);
const existing = (
  await client.listAdminDeploymentTargets(tenant, project, randomUUID(), 1, undefined, signal())
).value;
assert.equal(
  existing.deploymentTargets.length,
  0,
  "Refusing to write into a project with existing Targets",
);
assert.equal(existing.nextPageToken, undefined);
const results: {
  targetId: string;
  status: number;
  key: string;
  body: DeploymentTargetRegisterRequest;
}[] = [];
for (let batch = 0; batch < 4; batch++) {
  await Promise.all(
    Array.from({ length: 10 }, async (_, offset) => {
      const index = batch * 10 + offset;
      const targetId = `registration-${String(index).padStart(3, "0")}`;
      const targetKind = (["docker", "kubernetes", "ssh"] as const)[index % 3];
      const body: DeploymentTargetRegisterRequest = {
        targetId,
        targetName: targetId,
        targetKind,
        endpoint: `${targetKind === "ssh" ? "ssh" : "https"}://127.0.0.1:${1000 + index}`,
        credentialRef: "registration-unconfigured",
      };
      const key = randomUUID();
      try {
        await client.registerAdminDeploymentTarget(
          tenant,
          project,
          randomUUID(),
          key,
          body,
          signal(),
        );
        results.push({ targetId, status: 201, key, body });
      } catch (error) {
        if (!(error instanceof ClientError)) throw error;
        if (error.status === 409)
          assert.equal(error.message, "adminRegisterDeploymentTarget: TARGET_CONFLICT");
        results.push({ targetId, status: error.status, key, body });
      }
    }),
  );
}
const counts = Object.fromEntries(
  [...new Set(results.map((r) => r.status))].map((status) => [
    status,
    results.filter((r) => r.status === status).length,
  ]),
);
process.stdout.write(JSON.stringify({ phase: "concurrent", project, counts }) + "\n");
assert.ok(
  results.every((result) => result.status === 201 || result.status === 409),
  "Unexpected HTTP error during concurrent registration",
);
assert.ok(
  results.some((result) => result.status === 409),
  "No conflict observed; this run does not qualify conflict handling",
);
for (const result of results) {
  const register = () =>
    client.registerAdminDeploymentTarget(
      tenant,
      project,
      randomUUID(),
      result.key,
      result.body,
      signal(),
    );
  const persisted = (await register()).value;
  assert.deepEqual((await register()).value, persisted, "Original-key replay changed the resource");
  assert.equal(persisted.spec.observedPhase, "unprobed");
  assert.equal(persisted.spec.generation, 1);
  const operations = (
    await client.listAdminDeploymentTargetOperations(
      tenant,
      project,
      result.targetId,
      randomUUID(),
      200,
      undefined,
      signal(),
    )
  ).value;
  const audit = (
    await client.listAdminDeploymentTargetAuditEvents(
      tenant,
      project,
      result.targetId,
      randomUUID(),
      200,
      undefined,
      signal(),
    )
  ).value;
  assert.equal(operations.operations.length, 1);
  assert.equal(operations.nextPageToken, undefined);
  assert.equal(operations.operations[0].state, "succeeded");
  assert.equal(audit.events.length, 1);
  assert.equal(audit.nextPageToken, undefined);
  assert.equal(audit.events[0].operationId, operations.operations[0].operationId);
  assert.equal(audit.events[0].result, "succeeded");
}
const final = (
  await client.listAdminDeploymentTargets(tenant, project, randomUUID(), 200, undefined, signal())
).value;
assert.equal(final.deploymentTargets.length, 40);
assert.equal(final.nextPageToken, undefined);
process.stdout.write(
  JSON.stringify({
    phase: "complete",
    project,
    counts,
    targets: 40,
    sameKeyReplay: 40,
    singleOperationAndAudit: 40,
    boundary:
      "Real Admin API/PostgreSQL registration and replay only; no transport Probe, Worker deployment or Provider Turn.",
  }) + "\n",
);
