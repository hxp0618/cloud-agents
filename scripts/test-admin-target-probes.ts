import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  ClientError,
  createHTTPClient,
  encodeDeploymentTargetRegisterRequest,
  type DeploymentTargetRegisterRequest,
  type AdminDeniedWriteEvent,
} from "../sdk/typescript/src/platform";

// Probe only: supply real target endpoints and server-mounted credential references, never secret bytes.
const [endpoint, adminTokenFile, userTokenFile, tenant, project, targetsFile, output] =
  process.argv.slice(2);
assert.ok(
  endpoint && adminTokenFile && userTokenFile && tenant && project && targetsFile && output,
  "usage: bun scripts/test-admin-target-probes.ts ENDPOINT ADMIN_TOKEN_FILE USER_TOKEN_FILE TENANT PROJECT TARGETS_JSON NEW_OUTPUT_DIR",
);
const bodies: DeploymentTargetRegisterRequest[] = JSON.parse(readFileSync(targetsFile, "utf8"));
assert.ok(Array.isArray(bodies));
assert.equal(bodies.length, 3);
assert.deepEqual(bodies.map((body) => body.targetKind).sort(), ["docker", "kubernetes", "ssh"]);
assert.equal(new Set(bodies.map((body) => body.targetId)).size, 3);
for (const body of bodies) encodeDeploymentTargetRegisterRequest(body);
const admin = createHTTPClient(endpoint, readFileSync(adminTokenFile, "utf8").trim());
const user = createHTTPClient(endpoint, readFileSync(userTokenFile, "utf8").trim());
const requestId = () => `probe-check-${randomUUID()}`;
const signal = () => AbortSignal.timeout(30_000);
const results: object[] = [];
const denied: string[] = [];
const deniedWrites: AdminDeniedWriteEvent[] = [];
mkdirSync(output, { mode: 0o700 }); // Refuse to overwrite an earlier run.
const save = (complete: boolean) =>
  writeFileSync(
    join(output, "probe-evidence.json"),
    JSON.stringify(
      {
        capturedAt: new Date().toISOString(),
        complete,
        tenant,
        project,
        results,
        denied,
        deniedWrites,
        boundary:
          "Real transport Probe acceptance only; no Worker deployment, lifecycle cleanup or Provider Turn qualification.",
      },
      null,
      2,
    ) + "\n",
  );
save(false);

// Existing resources are out of scope. Resolve every target before the first write.
for (const body of bodies) {
  await assert.rejects(
    admin.getAdminDeploymentTarget(tenant, project, body.targetId, requestId(), signal()),
    (error: unknown) => error instanceof ClientError && error.status === 404,
    `Target ${body.targetId} already exists or cannot be verified absent; refusing mutation`,
  );
}

for (const body of bodies) {
  const id = body.targetId;
  const forbid = async (name: string, action: (correlation: string) => Promise<unknown>) => {
    const correlation = requestId();
    await assert.rejects(
      () => action(correlation),
      (error: unknown) => error instanceof ClientError && error.status === 403,
      name,
    );
    denied.push(`${id}:${name}:403`);
    if (name === "register" || name === "probe") {
      const page = (
        await admin.listAdminDeniedWriteEvents(
          tenant,
          project,
          requestId(),
          200,
          undefined,
          signal(),
        )
      ).value;
      const events = page.events.filter((event) => event.requestId === correlation);
      assert.equal(events.length, 1, "Denied write must have a correlated Audit event");
      const event = events[0];
      assert.equal(
        event.action,
        name === "register" ? "adminRegisterDeploymentTarget" : "adminProbeDeploymentTarget",
      );
      assert.equal(event.result, "denied");
      assert.equal(event.stableErrorCode, "AUTHORIZATION_DENIED");
      assert.match(event.actor, /^sha256:[0-9a-f]{64}$/);
      if (name === "probe") assert.equal(event.resourceId, id);
      assert.ok(!("operationId" in event) && !("resourceGeneration" in event));
      deniedWrites.push(event);
    }
  };
  await forbid("register", (correlation) =>
    user.registerAdminDeploymentTarget(tenant, project, correlation, randomUUID(), body, signal()),
  );
  const registrationKey = randomUUID();
  const registered = (
    await admin.registerAdminDeploymentTarget(
      tenant,
      project,
      requestId(),
      registrationKey,
      body,
      signal(),
    )
  ).value;
  assert.equal(registered.spec.observedPhase, "unprobed");
  assert.deepEqual(
    (
      await admin.registerAdminDeploymentTarget(
        tenant,
        project,
        requestId(),
        registrationKey,
        body,
        signal(),
      )
    ).value,
    registered,
  );
  await forbid("probe", (correlation) =>
    user.probeAdminDeploymentTarget(
      tenant,
      project,
      id,
      correlation,
      randomUUID(),
      { expectedGeneration: registered.spec.generation },
      signal(),
    ),
  );
  const probeKey = randomUUID();
  const probe = () =>
    admin.probeAdminDeploymentTarget(
      tenant,
      project,
      id,
      requestId(),
      probeKey,
      { expectedGeneration: registered.spec.generation },
      signal(),
    );
  const ready = (await probe()).value;
  assert.equal(
    ready.spec.observedPhase,
    "ready",
    `${body.targetKind}: ${ready.spec.stableErrorCode}`,
  );
  assert.deepEqual((await probe()).value, ready, "Probe replay must preserve its result");
  const persisted = (
    await admin.getAdminDeploymentTarget(tenant, project, id, requestId(), signal())
  ).value;
  assert.deepEqual(persisted, ready);
  const operations = (
    await admin.listAdminDeploymentTargetOperations(
      tenant,
      project,
      id,
      requestId(),
      200,
      undefined,
      signal(),
    )
  ).value;
  const audit = (
    await admin.listAdminDeploymentTargetAuditEvents(
      tenant,
      project,
      id,
      requestId(),
      200,
      undefined,
      signal(),
    )
  ).value;
  assert.equal(operations.nextPageToken, undefined);
  assert.equal(audit.nextPageToken, undefined);
  for (const action of ["target.register", "target.probe"] as const) {
    const matched = operations.operations.filter((op) => op.action === action);
    assert.equal(matched.length, 1, "Replay must not duplicate operations");
    assert.equal(matched[0].state, "succeeded");
    assert.ok(
      audit.events.some(
        (event) =>
          event.operationId === matched[0].operationId &&
          event.action === action &&
          event.result === "succeeded",
      ),
    );
  }
  await forbid("get", () =>
    user.getAdminDeploymentTarget(tenant, project, id, requestId(), signal()),
  );
  await forbid("operations", () =>
    user.listAdminDeploymentTargetOperations(
      tenant,
      project,
      id,
      requestId(),
      200,
      undefined,
      signal(),
    ),
  );
  await forbid("audit", () =>
    user.listAdminDeploymentTargetAuditEvents(
      tenant,
      project,
      id,
      requestId(),
      200,
      undefined,
      signal(),
    ),
  );
  results.push({
    targetId: id,
    kind: body.targetKind,
    phase: persisted.spec.observedPhase,
    generation: persisted.spec.generation,
    resourceVersion: persisted.metadata.resourceVersion,
    apiVersion: persisted.spec.apiVersion,
    engineVersion: persisted.spec.engineVersion,
    os: persisted.spec.os,
    architecture: persisted.spec.architecture,
    operations: operations.operations,
    audit: audit.events,
    registrationReplay: true,
    probeReplay: true,
  });
  save(false);
  process.stdout.write(
    `${body.targetKind}: ready, replay verified, Operation/Audit persisted, user scope denied\n`,
  );
}
save(true);
