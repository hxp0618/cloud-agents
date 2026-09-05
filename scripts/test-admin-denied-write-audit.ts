import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { ClientError, createHTTPClient } from "../sdk/typescript/src/platform";

// Acceptance probe for an owned disposable dev project. Intentionally fails while Audit is missing.
// Registers one unprobed Target; the ordinary-user Probe must never reach the actuator.
const [endpoint, userTokenFile, adminTokenFile, tenant, project] = process.argv.slice(2);
assert.ok(
  endpoint && userTokenFile && adminTokenFile && tenant && project,
  "usage: bun scripts/test-admin-denied-write-audit.ts LOCAL_ENDPOINT USER_TOKEN_FILE ADMIN_TOKEN_FILE TENANT EMPTY_PROJECT",
);
const url = new URL(endpoint);
assert.ok(
  url.protocol === "http:" &&
    url.hostname === "127.0.0.1" &&
    url.pathname === "/" &&
    !url.search &&
    !url.hash &&
    !url.username &&
    !url.password,
  "Requires an owned loopback dev stack",
);
const user = createHTTPClient(endpoint, readFileSync(userTokenFile, "utf8").trim());
const admin = createHTTPClient(endpoint, readFileSync(adminTokenFile, "utf8").trim());
const signal = () => AbortSignal.timeout(10_000);
await user.getProject(tenant, project, randomUUID(), signal());
const initial = (
  await admin.listAdminDeploymentTargets(tenant, project, randomUUID(), 1, undefined, signal())
).value;
assert.equal(initial.deploymentTargets.length, 0, "Refusing to register into a nonempty project");
const targetId = `denied-audit-${randomUUID()}`;
const target = (
  await admin.registerAdminDeploymentTarget(
    tenant,
    project,
    randomUUID(),
    randomUUID(),
    {
      targetId,
      targetName: targetId,
      targetKind: "docker",
      endpoint: "https://127.0.0.1:1",
      credentialRef: "denied-audit-unconfigured",
    },
    signal(),
  )
).value;
const audit = async () =>
  (
    await admin.listAdminDeploymentTargetAuditEvents(
      tenant,
      project,
      targetId,
      randomUUID(),
      200,
      undefined,
      signal(),
    )
  ).value;
const before = await audit();
assert.equal(before.events.length, 1, "Registration audit positive control failed");
assert.equal(before.events[0].action, "target.register");
assert.equal(before.events[0].result, "succeeded");
const deniedRequestId = randomUUID();
await assert.rejects(
  user.probeAdminDeploymentTarget(
    tenant,
    project,
    targetId,
    deniedRequestId,
    randomUUID(),
    { expectedGeneration: target.spec.generation },
    signal(),
  ),
  (error: unknown) => error instanceof ClientError && error.status === 403,
);
const afterTarget = (
  await admin.getAdminDeploymentTarget(tenant, project, targetId, randomUUID(), signal())
).value;
assert.deepEqual(afterTarget, target, "Denied Probe changed the Target");
const after = await audit();
const matching = after.events.filter((event) => event.requestId === deniedRequestId);
process.stdout.write(
  JSON.stringify({
    project,
    targetId,
    deniedRequestId,
    status: 403,
    beforeEvents: before.events.length,
    afterEvents: after.events.length,
    matchingEvents: matching.length,
    generation: afterTarget.spec.generation,
    phase: afterTarget.spec.observedPhase,
  }) + "\n",
);
assert.equal(
  matching.length,
  1,
  "M1 acceptance gap: denied Admin write has no queryable Audit event",
);
assert.equal(matching[0].stableErrorCode, "AUTHORIZATION_DENIED");
