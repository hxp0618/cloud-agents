import { useEffect, useState, type FormEvent } from "react";
import type {
  AdminAuditEvent,
  MaintenanceOperation,
  RemoteWorkerEnrollment,
  RemoteWorkerNodeSchedulingPreview,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import {
  adminFailure,
  listAdminRemoteWorkerEnrollmentAuditEvents,
  listAdminRemoteWorkerEnrollments,
  listAdminRemoteWorkerOperations,
  newIdempotencyKey,
  newRequestId,
  remoteWorkerFoundationSupport,
  replaceRemoteWorkerEnrollment,
  targetIdentifierPattern,
  type AdminClient,
  type SavedAdminConnection,
} from "./admin";
import { useI18n, type MessageKey } from "./i18n";

const stateKeys: Readonly<Record<RemoteWorkerEnrollment["spec"]["state"], MessageKey>> = {
  pending: "remoteWorkerEnrollment.state.pending",
  "secret-issued": "remoteWorkerEnrollment.state.secretIssued",
  enrolled: "remoteWorkerEnrollment.state.enrolled",
  revoked: "remoteWorkerEnrollment.state.revoked",
  expired: "remoteWorkerEnrollment.state.expired",
};

const nodeHealthKeys: Readonly<
  Record<NonNullable<RemoteWorkerEnrollment["spec"]["node"]>["healthState"], MessageKey>
> = {
  online: "remoteWorkerEnrollment.node.health.online",
  degraded: "remoteWorkerEnrollment.node.health.degraded",
  offline: "remoteWorkerEnrollment.node.health.offline",
};

const nodeStateKeys: Readonly<
  Record<NonNullable<RemoteWorkerEnrollment["spec"]["node"]>["observedState"], MessageKey>
> = {
  active: "remoteWorkerEnrollment.node.state.active",
  drained: "remoteWorkerEnrollment.node.state.drained",
};

const operationStateKeys: Readonly<Record<MaintenanceOperation["state"], MessageKey>> = {
  queued: "phase.queued",
  running: "phase.running",
  succeeded: "phase.succeeded",
  failed: "phase.failed",
  cancelled: "phase.cancelled",
};

const operationActionKeys: Readonly<
  Record<"remote-worker.drain" | "remote-worker.resume", MessageKey>
> = {
  "remote-worker.drain": "remoteWorkerEnrollment.drain",
  "remote-worker.resume": "remoteWorkerEnrollment.resume",
};

function enrollmentStateKey(enrollment: RemoteWorkerEnrollment): MessageKey {
  return enrollment.spec.certificateState === "revoked"
    ? "remoteWorkerEnrollment.certificateState.revoked"
    : stateKeys[enrollment.spec.state];
}

function auditActionKey(action: AdminAuditEvent["action"]): MessageKey {
  if (action === "remote-worker.drain" || action === "remote-worker.resume")
    return operationActionKeys[action];
  if (action === "remote-worker-enrollment.create") return "remoteWorkerEnrollment.auditCreate";
  if (action === "remote-worker-enrollment.claim-secret")
    return "remoteWorkerEnrollment.auditClaim";
  if (action === "remote-worker-enrollment.issue-certificate")
    return "remoteWorkerEnrollment.auditCertificate";
  return "remoteWorkerEnrollment.auditRevoke";
}

function nextSchedulingState(
  node: NonNullable<RemoteWorkerEnrollment["spec"]["node"]>,
): "active" | "drained" {
  if (node.desiredState !== node.observedState) return node.desiredState;
  return node.desiredState === "active" ? "drained" : "active";
}

export function RemoteWorkerEnrollmentPanel({
  client,
  connection,
}: Readonly<{ client: AdminClient; connection: SavedAdminConnection }>) {
  const { t, number, dateTime } = useI18n();
  const [enrollments, setEnrollments] = useState<readonly RemoteWorkerEnrollment[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [audit, setAudit] = useState<readonly AdminAuditEvent[]>([]);
  const [operations, setOperations] = useState<readonly MaintenanceOperation[]>([]);
  const [schedulingPreview, setSchedulingPreview] =
    useState<RemoteWorkerNodeSchedulingPreview | null>(null);
  const [schedulingConfirmation, setSchedulingConfirmation] = useState("");
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ReturnType<typeof adminFailure> | null>(null);
  const [notice, setNotice] = useState<MessageKey | null>(null);
  const [confirmation, setConfirmation] = useState("");
  const [form, setForm] = useState({
    enrollmentId: "",
    workerId: "",
    workerName: "",
    ttlSeconds: "900",
  });
  const selected = enrollments.find(({ metadata }) => metadata.uid === selectedId);
  const foundationSupport = selected?.spec.node
    ? remoteWorkerFoundationSupport(selected.spec.node)
    : null;

  async function load(signal: AbortSignal, preferredId = selectedId) {
    const values = await listAdminRemoteWorkerEnrollments(
      client,
      connection.tenantId,
      connection.projectId,
      signal,
    );
    const id = values.some(({ metadata }) => metadata.uid === preferredId)
      ? preferredId
      : (values[0]?.metadata.uid ?? "");
    if (id === "") {
      setEnrollments(values);
      setSelectedId("");
      setAudit([]);
      setOperations([]);
      setSchedulingPreview(null);
      return;
    }
    const [detail, events, remoteWorkerOperations] = await Promise.all([
      client.getAdminRemoteWorkerEnrollment(
        connection.tenantId,
        connection.projectId,
        id,
        newRequestId(),
        signal,
      ),
      listAdminRemoteWorkerEnrollmentAuditEvents(
        client,
        connection.tenantId,
        connection.projectId,
        id,
        signal,
      ),
      listAdminRemoteWorkerOperations(
        client,
        connection.tenantId,
        connection.projectId,
        id,
        signal,
      ),
    ]);
    setEnrollments(replaceRemoteWorkerEnrollment(values, detail.value));
    setSelectedId(id);
    setAudit(events);
    setOperations(remoteWorkerOperations);
  }

  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    void load(AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]))
      .catch((cause) => {
        if (!controller.signal.aborted) setError(adminFailure(cause));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [client, connection.tenantId, connection.projectId]);

  async function run(operation: (signal: AbortSignal) => Promise<void>, success: MessageKey) {
    if (busy) return;
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await operation(AbortSignal.timeout(30_000));
      setNotice(success);
    } catch (cause) {
      setError(adminFailure(cause));
    } finally {
      setBusy(false);
    }
  }

  function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const body = { ...form, ttlSeconds: Number(form.ttlSeconds) };
    void run(async (signal) => {
      const result = await client.createAdminRemoteWorkerEnrollment(
        connection.tenantId,
        connection.projectId,
        newRequestId(),
        newIdempotencyKey(),
        body,
        signal,
      );
      setCreating(false);
      setForm({ enrollmentId: "", workerId: "", workerName: "", ttlSeconds: "900" });
      await load(signal, result.value.metadata.uid);
    }, "remoteWorkerEnrollment.notice.created");
  }

  function select(enrollmentId: string) {
    setConfirmation("");
    setSchedulingPreview(null);
    setSchedulingConfirmation("");
    void run((signal) => load(signal, enrollmentId), "remoteWorkerEnrollment.notice.loaded");
  }

  function previewScheduling() {
    if (selected?.spec.node === undefined) return;
    const enrollmentId = selected.metadata.uid;
    void run(async (signal) => {
      const result = await client.previewAdminRemoteWorkerScheduling(
        connection.tenantId,
        connection.projectId,
        enrollmentId,
        newRequestId(),
        signal,
      );
      setSchedulingConfirmation("");
      setSchedulingPreview(result.value);
    }, "remoteWorkerEnrollment.notice.schedulingPreview");
  }

  function transitionScheduling() {
    if (schedulingPreview === null || schedulingConfirmation !== schedulingPreview.metadata.uid)
      return;
    const preview = schedulingPreview;
    void run(async (signal) => {
      await client.transitionAdminRemoteWorkerScheduling(
        connection.tenantId,
        connection.projectId,
        preview.metadata.uid,
        newRequestId(),
        newIdempotencyKey(),
        {
          expectedGeneration: preview.spec.expectedGeneration,
          expectedResourceVersion: preview.spec.expectedResourceVersion,
          confirmedEnrollmentId: preview.metadata.uid,
          desiredState: preview.spec.desiredState,
          impactDigest: preview.spec.impactDigest,
        },
        signal,
      );
      setSchedulingPreview(null);
      setSchedulingConfirmation("");
      await load(signal, preview.metadata.uid);
    }, "remoteWorkerEnrollment.notice.schedulingQueued");
  }

  function revoke() {
    if (selected === undefined || confirmation !== selected.metadata.uid) return;
    const enrollment = selected;
    const certificate =
      enrollment.spec.state === "enrolled" && enrollment.spec.certificateState === "active";
    void run(
      async (signal) => {
        const result = await client.revokeAdminRemoteWorkerEnrollment(
          connection.tenantId,
          connection.projectId,
          enrollment.metadata.uid,
          newRequestId(),
          newIdempotencyKey(),
          {
            expectedResourceVersion: enrollment.metadata.resourceVersion,
            confirmedEnrollmentId: enrollment.metadata.uid,
          },
          signal,
        );
        setConfirmation("");
        await load(signal, result.value.metadata.uid);
      },
      certificate
        ? "remoteWorkerEnrollment.notice.certificateRevoked"
        : "remoteWorkerEnrollment.notice.revoked",
    );
  }

  return (
    <section className="resource-list">
      <div className="list-toolbar">
        <span className="scope-chip" role="status">
          remote-worker-enrollments.list · {number(enrollments.length)}
        </span>
        <button
          className="button primary"
          type="button"
          disabled={busy}
          onClick={() => setCreating((value) => !value)}
        >
          {t(creating ? "action.cancel" : "remoteWorkerEnrollment.create")}
        </button>
      </div>
      {error ? (
        <div className="banner danger" role="alert">
          {error.code ? <code>{error.code}</code> : null}
          <p>{t(error.key)}</p>
        </div>
      ) : null}
      {notice ? (
        <div className="banner success" role="status">
          {t(notice)}
        </div>
      ) : null}
      {creating ? (
        <form className="panel resource-form" onSubmit={create}>
          <div className="panel-heading">
            <div>
              <h2>{t("remoteWorkerEnrollment.createTitle")}</h2>
              <p>{t("remoteWorkerEnrollment.createDescription")}</p>
            </div>
          </div>
          <div className="form-row">
            <label>
              <span>{t("remoteWorkerEnrollment.id")}</span>
              <input
                required
                pattern={targetIdentifierPattern}
                maxLength={128}
                value={form.enrollmentId}
                onChange={(event) => setForm({ ...form, enrollmentId: event.target.value })}
              />
            </label>
            <label>
              <span>{t("remoteWorkerEnrollment.workerId")}</span>
              <input
                required
                pattern={targetIdentifierPattern}
                maxLength={128}
                value={form.workerId}
                onChange={(event) => setForm({ ...form, workerId: event.target.value })}
              />
            </label>
          </div>
          <div className="form-row">
            <label>
              <span>{t("remoteWorkerEnrollment.workerName")}</span>
              <input
                required
                pattern={targetIdentifierPattern}
                maxLength={128}
                value={form.workerName}
                onChange={(event) => setForm({ ...form, workerName: event.target.value })}
              />
            </label>
            <label>
              <span>{t("remoteWorkerEnrollment.ttl")}</span>
              <input
                required
                type="number"
                min="300"
                max="3600"
                value={form.ttlSeconds}
                onChange={(event) => setForm({ ...form, ttlSeconds: event.target.value })}
              />
            </label>
          </div>
          <p className="cluster-boundary">{t("remoteWorkerEnrollment.secretBoundary")}</p>
          <button className="button primary" type="submit" disabled={busy}>
            {t("remoteWorkerEnrollment.create")}
          </button>
        </form>
      ) : null}
      <div className="panel target-list-panel">
        {enrollments.length === 0 ? (
          <p className="table-empty">{t("remoteWorkerEnrollment.empty")}</p>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>{t("remoteWorkerEnrollment.workerName")}</th>
                  <th>{t("remoteWorkerEnrollment.workerId")}</th>
                  <th>{t("remoteWorkerEnrollment.state")}</th>
                  <th>{t("remoteWorkerEnrollment.node.health")}</th>
                  <th>{t("remoteWorkerEnrollment.expires")}</th>
                  <th>{t("table.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {enrollments.map((enrollment) => (
                  <tr
                    key={enrollment.metadata.uid}
                    className={selectedId === enrollment.metadata.uid ? "selected" : ""}
                  >
                    <td>
                      <strong>{enrollment.metadata.name}</strong>
                      <small className="mono">{enrollment.metadata.uid}</small>
                    </td>
                    <td className="mono">{enrollment.spec.workerId}</td>
                    <td>
                      <span
                        className={`phase ${enrollment.spec.state === "revoked" || enrollment.spec.state === "expired" || enrollment.spec.certificateState === "revoked" ? "danger" : enrollment.spec.state === "enrolled" ? "success" : "running"}`}
                      >
                        <i />
                        {t(enrollmentStateKey(enrollment))}
                      </span>
                    </td>
                    <td>
                      {enrollment.spec.node ? (
                        <span
                          className={`phase ${enrollment.spec.node.healthState === "online" ? "success" : enrollment.spec.node.healthState === "degraded" ? "running" : "danger"}`}
                        >
                          <i />
                          {t(nodeHealthKeys[enrollment.spec.node.healthState])}
                        </span>
                      ) : (
                        t("remoteWorkerEnrollment.node.notConnected")
                      )}
                    </td>
                    <td>{dateTime(enrollment.spec.expiresAt)}</td>
                    <td>
                      <button
                        className="button outline"
                        type="button"
                        disabled={busy}
                        onClick={() => select(enrollment.metadata.uid)}
                      >
                        {t("remoteWorkerEnrollment.inspect")}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      {selected ? (
        <section className="panel overview-panel">
          <div className="panel-heading">
            <div>
              <div className="eyebrow">RemoteWorker</div>
              <h2>{selected.metadata.name}</h2>
              <p className="mono">{selected.metadata.uid}</p>
            </div>
            <span className="scope-chip">resourceVersion {selected.metadata.resourceVersion}</span>
          </div>
          <dl className="detail-grid">
            <div>
              <dt>{t("remoteWorkerEnrollment.workerId")}</dt>
              <dd className="mono">{selected.spec.workerId}</dd>
            </div>
            <div>
              <dt>{t("remoteWorkerEnrollment.targetId")}</dt>
              <dd className="mono break">{selected.spec.targetId}</dd>
            </div>
            <div>
              <dt>{t("remoteWorkerEnrollment.state")}</dt>
              <dd>{t(enrollmentStateKey(selected))}</dd>
            </div>
            <div>
              <dt>{t("remoteWorkerEnrollment.created")}</dt>
              <dd>{dateTime(selected.metadata.createdAt)}</dd>
            </div>
            <div>
              <dt>{t("remoteWorkerEnrollment.expires")}</dt>
              <dd>{dateTime(selected.spec.expiresAt)}</dd>
            </div>
            {selected.spec.incarnationId ? (
              <div>
                <dt>{t("remoteWorkerEnrollment.incarnation")}</dt>
                <dd className="mono">{selected.spec.incarnationId}</dd>
              </div>
            ) : null}
            {selected.spec.spiffeId ? (
              <div>
                <dt>{t("remoteWorkerEnrollment.spiffeId")}</dt>
                <dd className="mono">{selected.spec.spiffeId}</dd>
              </div>
            ) : null}
            {selected.spec.certificateSha256 ? (
              <div>
                <dt>{t("remoteWorkerEnrollment.certificateSha256")}</dt>
                <dd className="mono">{selected.spec.certificateSha256}</dd>
              </div>
            ) : null}
            {selected.spec.certificateExpiresAt ? (
              <div>
                <dt>{t("remoteWorkerEnrollment.certificateExpires")}</dt>
                <dd>{dateTime(selected.spec.certificateExpiresAt)}</dd>
              </div>
            ) : null}
            {selected.spec.certificateState ? (
              <div>
                <dt>{t("remoteWorkerEnrollment.certificateState")}</dt>
                <dd>
                  {t(
                    selected.spec.certificateState === "active"
                      ? "remoteWorkerEnrollment.certificateState.active"
                      : "remoteWorkerEnrollment.certificateState.revoked",
                  )}
                </dd>
              </div>
            ) : null}
            {selected.spec.certificateRevokedAt ? (
              <div>
                <dt>{t("remoteWorkerEnrollment.certificateRevoked")}</dt>
                <dd>{dateTime(selected.spec.certificateRevokedAt)}</dd>
              </div>
            ) : null}
          </dl>
          {selected.spec.node ? (
            <section className="activity-block">
              <div className="activity-heading">
                <h3>{t("remoteWorkerEnrollment.node.title")}</h3>
                <span
                  className={`phase ${selected.spec.node.healthState === "online" ? "success" : selected.spec.node.healthState === "degraded" ? "running" : "danger"}`}
                >
                  <i />
                  {t(nodeHealthKeys[selected.spec.node.healthState])}
                </span>
              </div>
              <dl className="detail-grid">
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.state")}</dt>
                  <dd>{t(nodeStateKeys[selected.spec.node.observedState])}</dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.generation")}</dt>
                  <dd className="mono">
                    {number(selected.spec.node.observedGeneration)} /{" "}
                    {number(selected.spec.node.generation)}
                  </dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.resourceVersion")}</dt>
                  <dd className="mono">{selected.spec.node.resourceVersion}</dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.workerVersion")}</dt>
                  <dd className="mono">{selected.spec.node.workerVersion}</dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.platform")}</dt>
                  <dd className="mono">
                    {selected.spec.node.os} · {selected.spec.node.architecture} ·{" "}
                    {selected.spec.node.kernelVersion}
                  </dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.capabilities")}</dt>
                  <dd className="mono">{selected.spec.node.capabilities.join(", ")}</dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.capacity")}</dt>
                  <dd>
                    {number(selected.spec.node.capacity.cpuMillis)} mCPU ·{" "}
                    {number(selected.spec.node.capacity.memoryBytes)} B ·{" "}
                    {number(selected.spec.node.capacity.diskBytes)} B
                  </dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.firstConnected")}</dt>
                  <dd>{dateTime(selected.spec.node.firstConnectedAt)}</dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.lastHeartbeat")}</dt>
                  <dd>{dateTime(selected.spec.node.lastHeartbeatAt)}</dd>
                </div>
                <div>
                  <dt>{t("remoteWorkerEnrollment.node.heartbeatExpires")}</dt>
                  <dd>{dateTime(selected.spec.node.heartbeatExpiresAt)}</dd>
                </div>
              </dl>
              {foundationSupport ? (
                <>
                  <h3>{t("remoteWorkerEnrollment.node.foundation.title")}</h3>
                  <dl className="detail-grid">
                    {(
                      [
                        [
                          "remoteWorkerEnrollment.node.foundation.runtime",
                          foundationSupport.runtime,
                          "remoteWorkerEnrollment.node.foundation.runtimeRequirement",
                        ],
                        [
                          "remoteWorkerEnrollment.node.foundation.architecture",
                          foundationSupport.architecture,
                          "remoteWorkerEnrollment.node.foundation.architectureRequirement",
                        ],
                        [
                          "remoteWorkerEnrollment.node.foundation.storage",
                          foundationSupport.storage,
                          "remoteWorkerEnrollment.node.foundation.storageRequirement",
                        ],
                        [
                          "remoteWorkerEnrollment.node.foundation.network",
                          foundationSupport.network,
                          "remoteWorkerEnrollment.node.foundation.networkRequirement",
                        ],
                      ] as const
                    ).map(([label, supported, requirement]) => (
                      <div key={label}>
                        <dt>{t(label)}</dt>
                        <dd>
                          <span className={`phase ${supported ? "success" : "danger"}`}>
                            <i />
                            {t(
                              supported
                                ? "remoteWorkerEnrollment.node.foundation.supported"
                                : "remoteWorkerEnrollment.node.foundation.unsupported",
                            )}
                          </span>{" "}
                          <small>{t(requirement)}</small>
                        </dd>
                      </div>
                    ))}
                  </dl>
                </>
              ) : null}
              <p className="cluster-boundary">{t("remoteWorkerEnrollment.node.boundary")}</p>
              <button
                className="button outline"
                type="button"
                disabled={
                  busy || selected.spec.node.desiredState !== selected.spec.node.observedState
                }
                onClick={previewScheduling}
              >
                {t(
                  nextSchedulingState(selected.spec.node) === "drained"
                    ? "remoteWorkerEnrollment.drain"
                    : "remoteWorkerEnrollment.resume",
                )}
              </button>
              {schedulingPreview ? (
                <div className="danger-zone">
                  <div>
                    <strong>
                      {t(
                        operationActionKeys[
                          schedulingPreview.spec.desiredState === "drained"
                            ? "remote-worker.drain"
                            : "remote-worker.resume"
                        ],
                      )}
                    </strong>
                    <p>{schedulingPreview.spec.impactSummary}</p>
                    <small className="mono">
                      generation {number(schedulingPreview.spec.expectedGeneration)} ·
                      resourceVersion {schedulingPreview.spec.expectedResourceVersion} ·{" "}
                      {t("remoteWorkerEnrollment.commandDeadline", {
                        seconds: number(schedulingPreview.spec.commandDeadlineSeconds),
                      })}
                    </small>
                  </div>
                  <label>
                    <span>
                      {t("remoteWorkerEnrollment.schedulingConfirm", {
                        id: schedulingPreview.metadata.uid,
                      })}
                    </span>
                    <input
                      value={schedulingConfirmation}
                      onChange={(event) => setSchedulingConfirmation(event.target.value)}
                      spellCheck={false}
                    />
                  </label>
                  <button
                    className={`button ${schedulingPreview.spec.desiredState === "drained" ? "danger" : "primary"}`}
                    type="button"
                    disabled={busy || schedulingConfirmation !== schedulingPreview.metadata.uid}
                    onClick={transitionScheduling}
                  >
                    {t("remoteWorkerEnrollment.schedulingSubmit")}
                  </button>
                </div>
              ) : null}
            </section>
          ) : null}
          <section className="activity-block">
            <div className="activity-heading">
              <h3>{t("remoteWorkerEnrollment.operations")}</h3>
              <span className="scope-chip">operations.list · {number(operations.length)}</span>
            </div>
            {operations.length === 0 ? (
              <p className="activity-empty">{t("remoteWorkerEnrollment.operationsEmpty")}</p>
            ) : (
              <ol className="activity-list compact">
                {operations.map((operation) => (
                  <li key={operation.operationId}>
                    <div>
                      <strong>
                        {t(
                          operationActionKeys[
                            operation.action as "remote-worker.drain" | "remote-worker.resume"
                          ],
                        )}
                      </strong>
                      <span
                        className={`phase ${operation.state === "succeeded" ? "success" : operation.state === "failed" ? "danger" : "running"}`}
                      >
                        <i />
                        {t(operationStateKeys[operation.state])}
                      </span>
                    </div>
                    <small className="mono">
                      {operation.operationId} · generation {number(operation.resourceGeneration)} ·{" "}
                      {operation.currentStep}
                    </small>
                    <small>{dateTime(operation.updatedAt)}</small>
                  </li>
                ))}
              </ol>
            )}
          </section>
          <p className="cluster-boundary">{t("remoteWorkerEnrollment.adminBoundary")}</p>
          {selected.spec.state === "pending" ||
          selected.spec.state === "secret-issued" ||
          selected.spec.state === "expired" ||
          (selected.spec.state === "enrolled" && selected.spec.certificateState === "active") ? (
            <div className="danger-zone">
              <label>
                <span>{t("remoteWorkerEnrollment.confirm", { id: selected.metadata.uid })}</span>
                <input
                  value={confirmation}
                  onChange={(event) => setConfirmation(event.target.value)}
                  spellCheck={false}
                />
              </label>
              <button
                className="button danger"
                type="button"
                disabled={busy || confirmation !== selected.metadata.uid}
                onClick={revoke}
              >
                {t(
                  selected.spec.state === "enrolled"
                    ? "remoteWorkerEnrollment.revokeCertificate"
                    : "remoteWorkerEnrollment.revoke",
                )}
              </button>
            </div>
          ) : null}
          <section className="activity-block">
            <div className="activity-heading">
              <h3>{t("remoteWorkerEnrollment.audit")}</h3>
              <span className="scope-chip">audit.list · {number(audit.length)}</span>
            </div>
            {audit.length === 0 ? (
              <p className="activity-empty">{t("remoteWorkerEnrollment.auditEmpty")}</p>
            ) : (
              <ol className="activity-list compact">
                {audit.map((event) => (
                  <li key={event.eventId}>
                    <div>
                      <strong>{t(auditActionKey(event.action))}</strong>
                    </div>
                    <small className="mono">{event.actor}</small>
                    <small>{dateTime(event.occurredAt)}</small>
                  </li>
                ))}
              </ol>
            )}
          </section>
        </section>
      ) : null}
    </section>
  );
}
