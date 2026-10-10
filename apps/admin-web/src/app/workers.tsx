import { useEffect, useRef, useState } from "react";
import { DetailMore } from "./form-fields";
import {
  type Worker,
  type WorkerHealthObservation,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import {
  adminFailure,
  type AdminClient,
  type SavedAdminConnection,
  type ClusterHostSummary,
} from "../admin";
import { useI18n } from "../i18n";
import { WorkerHealthBadge } from "../WorkerHealthBadge";
import { phaseLabel, phaseTone, shortDigest, targetKindLabel } from "./presentation";

export function ClusterHostTable({
  summaries,
  selectedTargetId,
  onSelect,
}: Readonly<{
  summaries: readonly ClusterHostSummary[];
  selectedTargetId: string;
  onSelect: (targetId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (summaries.length === 0)
    return <div className="table-empty">{t("table.empty.clusterHosts")}</div>;
  return (
    <div className="table-scroll">
      <table className="cluster-host-table">
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("table.kind")}</th>
            <th>{t("table.runtime")}</th>
            <th>{t("table.workers")}</th>
            <th>{t("table.lastHealth")}</th>
            <th>{t("table.status")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {summaries.map(({ target, workerCount, readyWorkerCount, latestHealthAt }) => (
            <tr
              key={target.metadata.uid}
              className={target.metadata.uid === selectedTargetId ? "selected" : ""}
              onClick={() => onSelect(target.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(target.metadata.uid)}>
                  <strong>{target.metadata.name}</strong>
                  {target.metadata.uid === target.metadata.name ? null : (
                    <small>{target.metadata.uid}</small>
                  )}
                </button>
              </td>
              <td>
                <span className="kind-badge">{targetKindLabel(target.spec.targetKind, t)}</span>
              </td>
              <td>
                {target.spec.engineVersion === "" ? (
                  "—"
                ) : (
                  <>
                    <strong>{target.spec.engineVersion}</strong>
                    <small className="table-subline">
                      {t("cluster.apiVersion", { version: target.spec.apiVersion })}
                    </small>
                  </>
                )}
              </td>
              <td>
                {workerCount === 0
                  ? t("cluster.noWorkers")
                  : t("cluster.workerSummary", {
                      ready: number(readyWorkerCount),
                      total: number(workerCount),
                    })}
              </td>
              <td>{dateTime(latestHealthAt)}</td>
              <td>
                <span className={`phase ${phaseTone(target.spec.observedPhase)}`}>
                  <i /> {phaseLabel(target.spec.observedPhase, t)}
                </span>
                {target.spec.schedulingState === "active" ? null : (
                  <small className="table-subline">
                    {t("detail.schedulingState")}: {phaseLabel(target.spec.schedulingState, t)}
                  </small>
                )}
              </td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: target.metadata.name })}
                  onClick={() => onSelect(target.metadata.uid)}
                >
                  ···
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function WorkerTable({
  workers,
  filtered,
  selectedWorkerId,
  onSelect,
}: Readonly<{
  workers: readonly Worker[];
  filtered: boolean;
  selectedWorkerId: string;
  onSelect: (workerId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (workers.length === 0)
    return (
      <div className="table-empty">
        {t(filtered ? "table.empty.filteredWorkers" : "table.empty.workers")}
      </div>
    );
  return (
    <div className="table-scroll">
      <table className="worker-table">
        <thead>
          <tr>
            <th>{t("table.workerId")}</th>
            <th>{t("table.target")}</th>
            <th>{t("table.lease")}</th>
            <th>{t("table.release")}</th>
            <th>{t("worker.periodicHealth")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.resourceLimits")}</th>
            <th>{t("table.started")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {workers.map((worker) => (
            <tr
              key={worker.metadata.uid}
              className={worker.metadata.uid === selectedWorkerId ? "selected" : ""}
              onClick={() => onSelect(worker.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(worker.metadata.uid)}>
                  <strong>{worker.metadata.name}</strong>
                  <small>{worker.metadata.uid}</small>
                </button>
              </td>
              <td>
                <strong>{worker.spec.targetId}</strong>
                <small className="table-subline">
                  {targetKindLabel(worker.spec.targetKind, t)}
                </small>
              </td>
              <td className="mono">{worker.spec.leaseId}</td>
              <td className="mono" title={worker.spec.releaseDigest}>
                {shortDigest(worker.spec.releaseDigest)}
              </td>
              <td>
                <WorkerHealthBadge worker={worker} />
                {worker.spec.health !== undefined ? (
                  <small className="table-subline">{dateTime(worker.spec.health.checkedAt)}</small>
                ) : null}
              </td>
              <td>
                <span className={`phase ${phaseTone(worker.spec.state)}`}>
                  <i />
                  {phaseLabel(worker.spec.state, t)}
                </span>
              </td>
              <td>
                {number(worker.spec.cpuLimitMillis)} mCPU ·{" "}
                {number(Math.round(worker.spec.memoryLimitBytes / 1_048_576))} MiB
              </td>
              <td>{dateTime(worker.metadata.createdAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: worker.metadata.name })}
                  onClick={() => onSelect(worker.metadata.uid)}
                >
                  ···
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function WorkerHealthCheck({
  worker,
  client,
  connection,
}: Readonly<{
  worker: Worker;
  client: AdminClient;
  connection: SavedAdminConnection;
}>) {
  const { t, dateTime } = useI18n();
  const [observation, setObservation] = useState<WorkerHealthObservation | null>(null);
  const [failure, setFailure] = useState<ReturnType<typeof adminFailure> | null>(null);
  const [pending, setPending] = useState(false);
  const active = useRef<AbortController | null>(null);
  useEffect(() => () => active.current?.abort(), [client, connection]);
  async function check() {
    active.current?.abort();
    const controller = new AbortController();
    active.current = controller;
    setPending(true);
    setObservation(null);
    setFailure(null);
    try {
      const result = await client.getAdminWorkerHealth(
        connection.tenantId,
        connection.projectId,
        worker.spec.leaseId,
        crypto.randomUUID(),
        worker.spec.generation,
        controller.signal,
      );
      if (!controller.signal.aborted) setObservation(result.value);
    } catch (cause) {
      if (!controller.signal.aborted) setFailure(adminFailure(cause));
    } finally {
      if (!controller.signal.aborted) setPending(false);
    }
  }
  return (
    <section
      className="action-block worker-health-check"
      aria-label={t("worker.liveHealth")}
      aria-busy={pending}
    >
      <button
        type="button"
        className="button outline"
        disabled={pending || worker.spec.state !== "ready"}
        onClick={() => void check()}
      >
        {t(pending ? "worker.checkingHealth" : "worker.checkHealth")}
      </button>
      <p className="boundary-note">{t("worker.liveHealthBoundary")}</p>
      <div role="status" aria-live="polite">
        {observation !== null ? (
          <p>
            {t(
              observation.state === "serving" ? "worker.healthServing" : "worker.healthUnavailable",
            )}{" "}
            · {dateTime(observation.checkedAt)}
          </p>
        ) : null}
      </div>
      {failure !== null ? (
        <p className="danger-text" role="alert">
          {t(failure.key)}
          {failure.code !== null ? (
            <>
              {" "}
              · <code>{failure.code}</code>
            </>
          ) : null}
        </p>
      ) : null}
    </section>
  );
}

export function WorkerDetail({ worker }: Readonly<{ worker: Worker }>) {
  const { t, number, dateTime } = useI18n();
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          W
        </div>
        <div>
          <div className="eyebrow">{t("worker.eyebrow")}</div>
          <h2>{worker.metadata.name}</h2>
          <span className={`phase ${phaseTone(worker.spec.state)}`}>
            <i />
            {phaseLabel(worker.spec.state, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("worker.periodicHealth")}</dt>
          <dd>
            <WorkerHealthBadge worker={worker} />
          </dd>
        </div>
        {worker.spec.health !== undefined ? (
          <>
            <div>
              <dt>{t("worker.healthLastSuccessAt")}</dt>
              <dd>
                {worker.spec.health.lastSuccessAt === undefined
                  ? t("common.notObserved")
                  : dateTime(worker.spec.health.lastSuccessAt)}
              </dd>
            </div>
          </>
        ) : null}
        <div>
          <dt>{t("worker.lease")}</dt>
          <dd className="mono">{worker.spec.leaseId}</dd>
        </div>
        <div>
          <dt>{t("worker.target")}</dt>
          <dd className="mono">
            {worker.spec.targetId} · {targetKindLabel(worker.spec.targetKind, t)} · g
            {number(worker.spec.targetGeneration)}
          </dd>
        </div>
        <div>
          <dt>{t("worker.resourceLimits")}</dt>
          <dd>
            {number(worker.spec.cpuLimitMillis)} mCPU /{" "}
            {number(Math.round(worker.spec.memoryLimitBytes / 1_048_576))} MiB
          </dd>
        </div>
        <div>
          <dt>{t("worker.cleanupPhase")}</dt>
          <dd>{phaseLabel(worker.spec.cleanupPhase, t)}</dd>
        </div>
        <div>
          <dt>{t("worker.lastHealthAt")}</dt>
          <dd>
            {worker.spec.lastHealthAt === undefined
              ? t("common.notObserved")
              : dateTime(worker.spec.lastHealthAt)}
          </dd>
        </div>
        <div>
          <dt>{t("worker.readyAt")}</dt>
          <dd>
            {worker.spec.readyAt === undefined
              ? t("common.notReady")
              : dateTime(worker.spec.readyAt)}
          </dd>
        </div>
        <div>
          <dt>{t("worker.startedAt")}</dt>
          <dd>{dateTime(worker.metadata.createdAt)}</dd>
        </div>
        <div>
          <dt>{t("worker.updatedAt")}</dt>
          <dd>{dateTime(worker.metadata.updatedAt)}</dd>
        </div>
        {worker.spec.stableErrorCode !== "" ? (
          <div>
            <dt>{t("detail.stableError")}</dt>
            <dd className="danger-text">{worker.spec.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
      <DetailMore title={t("detail.diagnostics")}>
        <div>
          <dt>{t("worker.id")}</dt>
          <dd className="mono">{worker.metadata.uid}</dd>
        </div>
        <div>
          <dt>{t("table.generation")}</dt>
          <dd className="mono">{number(worker.spec.generation)}</dd>
        </div>
        <div>
          <dt>{t("detail.resourceVersion")}</dt>
          <dd className="mono">{worker.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>{t("worker.releaseDigest")}</dt>
          <dd className="mono break">{worker.spec.releaseDigest}</dd>
        </div>
        <div>
          <dt>{t("worker.identity")}</dt>
          <dd className="mono break">{worker.spec.workerSpiffeId ?? t("common.notReady")}</dd>
        </div>
        <div>
          <dt>{t("worker.serverName")}</dt>
          <dd className="mono">{worker.spec.workerServerName ?? t("common.notReady")}</dd>
        </div>
        {worker.spec.health !== undefined ? (
          <>
            <div>
              <dt>{t("worker.healthCheckedAt")}</dt>
              <dd>{dateTime(worker.spec.health.checkedAt)}</dd>
            </div>
            <div>
              <dt>{t("worker.healthExpiresAt")}</dt>
              <dd>{dateTime(worker.spec.health.expiresAt)}</dd>
            </div>
          </>
        ) : null}
      </DetailMore>
    </>
  );
}
