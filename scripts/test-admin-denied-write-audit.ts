import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { ClientError, createHTTPClient } from "../sdk/typescript/src/platform";

// Acceptance probe for an owned disposable dev project, using real HTTP and PostgreSQL authority.
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
assert.equal(
  after.events.length,
  before.events.length,
  "Rejected Probe fabricated an executed operation",
);
const deniedPage = () =>
  admin
    .listAdminDeniedWriteEvents(tenant, project, randomUUID(), 200, undefined, signal())
    .then(({ value }) => value);
const denied = await deniedPage();
const matching = denied.events.filter((event) => event.requestId === deniedRequestId);
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
assert.equal(matching.length, 1, "Denied Admin write must have one queryable Audit event");
assert.equal(matching[0].stableErrorCode, "AUTHORIZATION_DENIED");
assert.equal(matching[0].action, "adminProbeDeploymentTarget");
assert.equal(matching[0].resourceId, targetId);
assert.equal(matching[0].result, "denied");
assert.match(matching[0].actor, /^sha256:[0-9a-f]{64}$/);
assert.ok(!("operationId" in matching[0]) && !("resourceGeneration" in matching[0]));
await assert.rejects(
  user.listAdminDeniedWriteEvents(tenant, project, randomUUID(), 1, undefined, signal()),
  (error: unknown) => error instanceof ClientError && error.status === 403,
);

const contract = JSON.parse(
  readFileSync(new URL("../contracts/managed-host/v1alpha1/openapi.json", import.meta.url), "utf8"),
);
const expected = new Map<string, string>();
for (let repeat = 0; repeat < 2; repeat++) {
  for (const [path, item] of Object.entries(
    contract.paths as Record<string, Record<string, { operationId?: string }>>,
  )) {
    if (!path.startsWith("/v1/admin/")) continue;
    for (const [method, operation] of Object.entries(item)) {
      if (method !== "post" && method !== "put") continue;
      assert.ok(operation.operationId);
      const requestId = randomUUID();
      expected.set(requestId, operation.operationId);
      const parameters: Record<string, string> = {
        tenantId: tenant,
        projectId: project,
        profileVersion: "2",
      };
      const route = path.replace(
        /\{([^}]+)\}/g,
        (_, key: string) => parameters[key] ?? "audit-nonexistent",
      );
      const response = await fetch(new URL(route, endpoint), {
        method: method.toUpperCase(),
        redirect: "error",
        signal: signal(),
        headers: {
          Authorization: `Bearer ${readFileSync(userTokenFile, "utf8").trim()}`,
          "X-Request-ID": requestId,
          "Idempotency-Key": randomUUID(),
          "Content-Type": "application/json",
        },
        // Invalid JSON proves denial routing never needs request-body decoding or business execution.
        body: '{"credentialRef":"do-not-store-audit-secret-canary"',
      });
      assert.equal(response.status, 403, operation.operationId);
      assert.equal((await response.json()).error.code, "AUTHORIZATION_DENIED");
    }
  }
}
assert.equal(expected.size, 26, "Review coverage when Admin write contracts change");
const collected = [];
let next: string | undefined;
let pages = 0;
do {
  const page = (
    await admin.listAdminDeniedWriteEvents(tenant, project, randomUUID(), 5, next, signal())
  ).value;
  collected.push(...page.events);
  next = page.nextPageToken;
  assert.ok(++pages < 20, "Pagination loop");
} while (next);
assert.equal(collected.length, denied.events.length + expected.size);
assert.equal(new Set(collected.map((event) => event.eventId)).size, collected.length);
for (const [requestId, operation] of expected) {
  const events = collected.filter((event) => event.requestId === requestId);
  assert.equal(events.length, 1);
  assert.equal(events[0].action, operation);
  assert.equal(events[0].actor, matching[0].actor);
  assert.equal(events[0].tenantId, tenant);
  assert.equal(events[0].projectId, project);
}
assert.ok(!JSON.stringify(collected).includes("do-not-store-audit-secret-canary"));
const replayToken = (
  await admin.listAdminDeniedWriteEvents(tenant, project, randomUUID(), 1, undefined, signal())
).value.nextPageToken;
assert.ok(replayToken);
await assert.rejects(
  admin.listAdminDeniedWriteEvents(
    tenant,
    "audit-other-project",
    randomUUID(),
    1,
    replayToken,
    signal(),
  ),
  (error: unknown) => error instanceof ClientError && error.status === 400,
);
assert.deepEqual(
  (await admin.getAdminDeploymentTarget(tenant, project, targetId, randomUUID(), signal())).value,
  target,
);
const malformedCorrelation = await fetch(
  new URL(
    `/v1/admin/tenants/${tenant}/projects/${project}/deployment-targets/${targetId}:probe`,
    endpoint,
  ),
  {
    method: "POST",
    redirect: "error",
    signal: signal(),
    headers: {
      Authorization: `Bearer ${readFileSync(userTokenFile, "utf8").trim()}`,
      "X-Request-ID": "invalid/correlation",
      "Idempotency-Key": randomUUID(),
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ expectedGeneration: 1 }),
  },
);
assert.equal(malformedCorrelation.status, 403);
const correlationEvents = (await deniedPage()).events.filter(
  (event) => event.requestId === "request-unknown",
);
assert.equal(
  correlationEvents.length,
  1,
  "Malformed caller correlation must not bypass denial Audit",
);
assert.equal(correlationEvents[0].resourceId, targetId);
process.stdout.write(
  JSON.stringify({
    result: "passed",
    writeRoutes: expected.size / 2,
    persistedEvents: collected.length,
    pages,
    deniedQueryStatus: 403,
    scopedCursorReplayStatus: 400,
    targetUnchanged: true,
    secretCanaryAbsent: true,
    malformedCorrelationAudited: true,
  }) + "\n",
);
