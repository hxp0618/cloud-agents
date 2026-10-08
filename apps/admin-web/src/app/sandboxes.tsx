import { useState } from "react";
import {
  type AdminSandboxAccessGrant,
  type AdminSandboxSession,
  type ManagedAgentExecution,
  type ManagedAgentSideEffectReconciliationRequest,
  type RuntimeProfile,
  type SandboxUsageCorrectionRequest,
  type SandboxUsageMetric,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import {
  type SandboxLifecycleAction,
  availableSandboxLifecycleAction,
  targetIdentifierPattern,
} from "../admin";
import { useI18n, type MessageKey } from "../i18n";
import { phaseLabel, phaseTone, sandboxUsageMetrics } from "./presentation";
import { ManagedAgentRuntimeSection } from "./managed-agent-runtime";
import type { AdminClient, AdminManagedAgentRuntime, SavedAdminConnection } from "../admin";

export function SandboxTable({
  sandboxes,
  selectedSandboxId,
  onSelect,
}: Readonly<{
  sandboxes: readonly AdminSandboxSession[];
  selectedSandboxId: string;
  onSelect: (sandboxId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (sandboxes.length === 0)
    return <div className="table-empty">{t("table.empty.sandboxes")}</div>;
  return (
    <div className="table-scroll" tabIndex={0} role="region" aria-label={t("page.sandboxes.title")}>
      <table className="sandbox-table">
        <thead>
          <tr>
            <th>{t("sandbox.id")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.workspace")}</th>
            <th>{t("table.profile")}</th>
            <th>{t("table.target")}</th>
            <th>{t("table.generation")}</th>
            <th>{t("table.updated")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {sandboxes.map((sandbox) => (
            <tr
              key={sandbox.metadata.uid}
              className={sandbox.metadata.uid === selectedSandboxId ? "selected" : ""}
              onClick={() => onSelect(sandbox.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(sandbox.metadata.uid)}>
                  <strong>{sandbox.metadata.name}</strong>
                  <small>{sandbox.spec.operationId}</small>
                </button>
              </td>
              <td>
                <span className={`phase ${phaseTone(sandbox.spec.observedState)}`}>
                  <i /> {phaseLabel(sandbox.spec.observedState, t)}
                </span>
              </td>
              <td>
                <strong>{sandbox.spec.workspaceName}</strong>
                <small className="table-subline mono">{sandbox.spec.volumeId}</small>
              </td>
              <td className="mono">
                {sandbox.spec.runtimeProfileId} · v{number(sandbox.spec.runtimeProfileVersion)}
              </td>
              <td className="mono">{sandbox.spec.targetId}</td>
              <td className="mono">
                {number(sandbox.spec.observedGeneration)} / {number(sandbox.spec.generation)}
              </td>
              <td>{dateTime(sandbox.metadata.updatedAt ?? sandbox.metadata.createdAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: sandbox.metadata.name })}
                  onClick={() => onSelect(sandbox.metadata.uid)}
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

export function SandboxDetail({
  sandbox,
  grants,
  runtime,
  client,
  connection,
  runtimeProfiles,
  disabled,
  onTransition,
  onRevokeGrant,
  onCorrectUsage,
  onReconcileSideEffect,
  onNextSessions,
  onNextExecutions,
  onSelectSession,
}: Readonly<{
  sandbox: AdminSandboxSession;
  grants: readonly AdminSandboxAccessGrant[];
  runtime: AdminManagedAgentRuntime;
  client: AdminClient | null;
  connection: SavedAdminConnection;
  runtimeProfiles: readonly RuntimeProfile[];
  disabled: boolean;
  onTransition: (action: SandboxLifecycleAction) => void;
  onRevokeGrant: (grant: AdminSandboxAccessGrant) => void;
  onCorrectUsage: (
    correction: Pick<SandboxUsageCorrectionRequest, "metric" | "adjustment" | "reasonCode">,
  ) => void;
  onReconcileSideEffect: (
    execution: ManagedAgentExecution,
    outcome: ManagedAgentSideEffectReconciliationRequest["outcome"],
  ) => void;
  onNextSessions: () => void;
  onNextExecutions: () => void;
  onSelectSession: (sessionId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  const action = availableSandboxLifecycleAction(sandbox);
  const [correctionMetric, setCorrectionMetric] =
    useState<SandboxUsageMetric>("allocatedMilliseconds");
  const [correctionAdjustment, setCorrectionAdjustment] = useState("");
  const [correctionReason, setCorrectionReason] = useState("");
  const [correctionConfirmed, setCorrectionConfirmed] = useState(false);
  const correctionLimitReached = (sandbox.spec.usageCorrections?.length ?? 0) >= 100;
  return (
    <>
      <div className="detail-heading">
        <div>
          <div className="eyebrow">Sandbox · g{number(sandbox.spec.generation)}</div>
          <h2>{sandbox.metadata.name}</h2>
          <span className={`phase ${phaseTone(sandbox.spec.observedState)}`}>
            <i /> {phaseLabel(sandbox.spec.observedState, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("sandbox.workspace")}</dt>
          <dd className="mono">
            {sandbox.spec.workspaceName} · {sandbox.spec.workspaceId}
          </dd>
        </div>
        <div>
          <dt>{t("sandbox.volume")}</dt>
          <dd className="mono">{sandbox.spec.volumeId}</dd>
        </div>
        <div>
          <dt>{t("sandbox.physicalVolume")}</dt>
          <dd className="mono break">{sandbox.spec.physicalVolumeId ?? t("common.notBound")}</dd>
        </div>
        <div>
          <dt>{t("sandbox.workspaceState")}</dt>
          <dd>{phaseLabel(sandbox.spec.workspaceObservedState, t)}</dd>
        </div>
        <div>
          <dt>{t("sandbox.profile")}</dt>
          <dd className="mono">
            {sandbox.spec.runtimeProfileId} · v{number(sandbox.spec.runtimeProfileVersion)}
          </dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.workloadTrust")}</dt>
          <dd>{t(`runtimeProfile.workloadTrust.${sandbox.spec.workloadTrust}`)}</dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.isolationRuntime")}</dt>
          <dd>{t(`runtimeProfile.isolationRuntime.${sandbox.spec.isolationRuntime}`)}</dd>
        </div>
        <div>
          <dt>{t("sandbox.target")}</dt>
          <dd className="mono">{sandbox.spec.targetId}</dd>
        </div>
        <div>
          <dt>{t("sandbox.networkPolicy")}</dt>
          <dd className="mono">{sandbox.spec.networkPolicyRef ?? t("common.notBound")}</dd>
        </div>
        <div>
          <dt>{t("sandbox.networkPolicyEnforcement")}</dt>
          <dd>
            {t(
              `sandbox.networkPolicyEnforcement.${sandbox.spec.networkPolicyEnforcement}` as MessageKey,
            )}
          </dd>
        </div>
        <div>
          <dt>{t("sandbox.ttl")}</dt>
          <dd>
            {sandbox.spec.ttlSeconds === undefined
              ? t("common.notAvailable")
              : t("sandbox.ttlValue", {
                  seconds: number(sandbox.spec.ttlSeconds),
                })}
          </dd>
        </div>
        <div>
          <dt>{t("sandbox.expiresAt")}</dt>
          <dd>
            {sandbox.spec.expiresAt === undefined
              ? t("common.notAvailable")
              : dateTime(sandbox.spec.expiresAt)}
          </dd>
        </div>
        <div>
          <dt>{t("sandbox.lifecycleTrigger")}</dt>
          <dd>
            {sandbox.spec.lifecycleTrigger === undefined
              ? t("common.notAvailable")
              : t(
                  sandbox.spec.lifecycleTrigger === "ttl"
                    ? "sandbox.lifecycleTrigger.ttl"
                    : "sandbox.lifecycleTrigger.manual",
                )}
          </dd>
        </div>
        <div>
          <dt>{t("sandbox.operation")}</dt>
          <dd className="mono break">{sandbox.spec.operationId}</dd>
        </div>
        <div>
          <dt>{t("sandbox.operationState")}</dt>
          <dd>
            {phaseLabel(sandbox.spec.operationState, t)} ·{" "}
            {phaseLabel(sandbox.spec.cleanupPhase, t)}
          </dd>
        </div>
        <div>
          <dt>{t("sandbox.desiredState")}</dt>
          <dd>{phaseLabel(sandbox.spec.desiredState, t)}</dd>
        </div>
        <div>
          <dt>{t("table.generation")}</dt>
          <dd className="mono">{number(sandbox.spec.generation)}</dd>
        </div>
        <div>
          <dt>{t("sandbox.observedGeneration")}</dt>
          <dd className="mono">{number(sandbox.spec.observedGeneration)}</dd>
        </div>
        <div>
          <dt>{t("sandbox.writerReleased")}</dt>
          <dd>{t(sandbox.spec.writerReleased ? "common.yes" : "common.no")}</dd>
        </div>
        <div>
          <dt>{t("sandbox.runtimeId")}</dt>
          <dd className="mono break">{sandbox.spec.runtimeId ?? t("common.notBound")}</dd>
        </div>
        <div>
          <dt>{t("sandbox.runtimeState")}</dt>
          <dd>{sandbox.spec.runtimeState ?? t("common.notObserved")}</dd>
        </div>
        <div>
          <dt>{t("sandbox.observedAt")}</dt>
          <dd>
            {sandbox.spec.observedAt === undefined
              ? t("common.notObserved")
              : dateTime(sandbox.spec.observedAt)}
          </dd>
        </div>
        {sandbox.spec.usage === undefined ? null : (
          <>
            <div>
              <dt>{t("sandbox.usageGeneration")}</dt>
              <dd className="mono">{number(sandbox.spec.usage.latestRuntimeGeneration)}</dd>
            </div>
            <div>
              <dt>{t("sandbox.usageAllocated")}</dt>
              <dd className="mono">
                {t("sandbox.usageMilliseconds", {
                  value: sandbox.spec.usage.allocatedMilliseconds,
                })}
              </dd>
            </div>
            <div>
              <dt>{t("sandbox.usageCPU")}</dt>
              <dd className="mono">{sandbox.spec.usage.cpuMillisMilliseconds} mCPU·ms</dd>
            </div>
            <div>
              <dt>{t("sandbox.usageMemory")}</dt>
              <dd className="mono">{sandbox.spec.usage.memoryByteMilliseconds} byte·ms</dd>
            </div>
            <div>
              <dt>{t("sandbox.usageCheckpointedAt")}</dt>
              <dd>{dateTime(sandbox.spec.usage.checkpointedAt)}</dd>
            </div>
            <div>
              <dt>{t("sandbox.usageFinalizedAt")}</dt>
              <dd>
                {sandbox.spec.usage.finalizedAt === undefined
                  ? t("sandbox.usageActive")
                  : dateTime(sandbox.spec.usage.finalizedAt)}
              </dd>
            </div>
          </>
        )}
        {sandbox.spec.workspaceVolumeUsage === undefined ? null : (
          <>
            <div>
              <dt>{t("sandbox.workspaceUsageSource")}</dt>
              <dd className="mono">{sandbox.spec.workspaceVolumeUsage.source}</dd>
            </div>
            <div>
              <dt>{t("sandbox.workspaceUsageGeneration")}</dt>
              <dd className="mono">
                {number(sandbox.spec.workspaceVolumeUsage.measurementGeneration)}
              </dd>
            </div>
            <div>
              <dt>{t("sandbox.workspaceUsageState")}</dt>
              <dd>{phaseLabel(sandbox.spec.workspaceVolumeUsage.state, t)}</dd>
            </div>
            <div>
              <dt>{t("sandbox.workspaceUsageBytes")}</dt>
              <dd className="mono">
                {sandbox.spec.workspaceVolumeUsage.usedBytes ?? t("common.notAvailable")}
              </dd>
            </div>
            <div>
              <dt>{t("sandbox.workspaceUsageCheckpointedAt")}</dt>
              <dd>
                {sandbox.spec.workspaceVolumeUsage.checkpointedAt === undefined
                  ? t("common.notObserved")
                  : dateTime(sandbox.spec.workspaceVolumeUsage.checkpointedAt)}
              </dd>
            </div>
            <div>
              <dt>{t("sandbox.workspaceUsageObservedAt")}</dt>
              <dd>
                {sandbox.spec.workspaceVolumeUsage.observedAt === undefined
                  ? t("common.notObserved")
                  : dateTime(sandbox.spec.workspaceVolumeUsage.observedAt)}
              </dd>
            </div>
            {sandbox.spec.workspaceVolumeUsage.stableErrorCode === undefined ? null : (
              <div>
                <dt>{t("detail.stableError")}</dt>
                <dd className="mono danger-text">
                  {sandbox.spec.workspaceVolumeUsage.stableErrorCode}
                </dd>
              </div>
            )}
          </>
        )}
        {sandbox.spec.networkUsage === undefined ? null : (
          <>
            <div>
              <dt>{t("sandbox.networkUsageSource")}</dt>
              <dd className="mono">{sandbox.spec.networkUsage.source}</dd>
            </div>
            <div>
              <dt>{t("sandbox.networkUsageRuntimeGeneration")}</dt>
              <dd className="mono">{number(sandbox.spec.networkUsage.latestRuntimeGeneration)}</dd>
            </div>
            <div>
              <dt>{t("sandbox.networkUsageMeasurementGeneration")}</dt>
              <dd className="mono">{number(sandbox.spec.networkUsage.measurementGeneration)}</dd>
            </div>
            <div>
              <dt>{t("sandbox.networkUsageState")}</dt>
              <dd>{phaseLabel(sandbox.spec.networkUsage.state, t)}</dd>
            </div>
            <div>
              <dt>{t("sandbox.networkUsageReceivedBytes")}</dt>
              <dd className="mono">
                {sandbox.spec.networkUsage.receivedBytes ?? t("common.notAvailable")}
              </dd>
            </div>
            <div>
              <dt>{t("sandbox.networkUsageTransmittedBytes")}</dt>
              <dd className="mono">
                {sandbox.spec.networkUsage.transmittedBytes ?? t("common.notAvailable")}
              </dd>
            </div>
            <div>
              <dt>{t("sandbox.networkUsageCheckpointedAt")}</dt>
              <dd>
                {sandbox.spec.networkUsage.checkpointedAt === undefined
                  ? t("common.notObserved")
                  : dateTime(sandbox.spec.networkUsage.checkpointedAt)}
              </dd>
            </div>
            <div>
              <dt>{t("sandbox.networkUsageObservedAt")}</dt>
              <dd>{dateTime(sandbox.spec.networkUsage.observedAt)}</dd>
            </div>
            {sandbox.spec.networkUsage.stableErrorCode === undefined ? null : (
              <div>
                <dt>{t("detail.stableError")}</dt>
                <dd className="mono danger-text">{sandbox.spec.networkUsage.stableErrorCode}</dd>
              </div>
            )}
          </>
        )}
        <div>
          <dt>{t("detail.resourceVersion")}</dt>
          <dd className="mono">{sandbox.metadata.resourceVersion}</dd>
        </div>
        {sandbox.spec.stableErrorCode === undefined ? null : (
          <div>
            <dt>{t("detail.stableError")}</dt>
            <dd className="mono danger-text">{sandbox.spec.stableErrorCode}</dd>
          </div>
        )}
      </dl>
      <ManagedAgentRuntimeSection
        sandbox={sandbox}
        runtime={runtime}
        client={client}
        connection={connection}
        runtimeProfiles={runtimeProfiles}
        disabled={disabled}
        onReconcileSideEffect={onReconcileSideEffect}
        onNextSessions={onNextSessions}
        onNextExecutions={onNextExecutions}
        onSelectSession={onSelectSession}
      />
      <section className="action-block" aria-labelledby="sandbox-usage-corrections-title">
        <div className="activity-heading">
          <h3 id="sandbox-usage-corrections-title">{t("sandbox.usageCorrections.title")}</h3>
          <span className="mono">{number(sandbox.spec.usageCorrections?.length ?? 0)} / 100</span>
        </div>
        <p>{t("sandbox.usageCorrections.description")}</p>
        {sandbox.spec.usageCorrections?.length ? (
          <ul className="activity-list">
            {sandbox.spec.usageCorrections.map((correction) => (
              <li key={correction.correctionId}>
                <div>
                  <strong>{t(`sandbox.usageMetric.${correction.metric}`)}</strong>
                  <span className="mono">{correction.adjustment}</span>
                </div>
                <small>
                  {correction.reasonCode} · g{number(correction.sandboxGeneration)} · rv{" "}
                  {correction.priorResourceVersion}
                </small>
                <small>
                  {dateTime(correction.createdAt)} · {correction.requestId}
                </small>
              </li>
            ))}
          </ul>
        ) : (
          <p className="activity-empty">{t("sandbox.usageCorrections.empty")}</p>
        )}
        <form
          className="resource-form"
          onSubmit={(event) => {
            event.preventDefault();
            onCorrectUsage({
              metric: correctionMetric,
              adjustment: correctionAdjustment,
              reasonCode: correctionReason,
            });
            setCorrectionAdjustment("");
            setCorrectionReason("");
            setCorrectionConfirmed(false);
          }}
        >
          <label>
            <span>{t("sandbox.usageCorrections.metric")}</span>
            <select
              value={correctionMetric}
              onChange={(event) => setCorrectionMetric(event.target.value as SandboxUsageMetric)}
              disabled={disabled || correctionLimitReached}
            >
              {sandboxUsageMetrics.map((metric) => (
                <option key={metric} value={metric}>
                  {t(`sandbox.usageMetric.${metric}`)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>{t("sandbox.usageCorrections.adjustment")}</span>
            <input
              type="text"
              inputMode="numeric"
              required
              pattern="-?[1-9][0-9]{0,39}"
              value={correctionAdjustment}
              onChange={(event) => setCorrectionAdjustment(event.target.value)}
              disabled={disabled || correctionLimitReached}
            />
          </label>
          <label>
            <span>{t("sandbox.usageCorrections.reason")}</span>
            <input
              type="text"
              required
              pattern={targetIdentifierPattern}
              value={correctionReason}
              onChange={(event) => setCorrectionReason(event.target.value)}
              disabled={disabled || correctionLimitReached}
            />
          </label>
          <dl className="detail-list cleanup-fence">
            <div>
              <dt>{t("sandbox.lifecycle.expectedGeneration")}</dt>
              <dd className="mono">{number(sandbox.spec.generation)}</dd>
            </div>
            <div>
              <dt>{t("sandbox.lifecycle.expectedResourceVersion")}</dt>
              <dd className="mono">{sandbox.metadata.resourceVersion}</dd>
            </div>
          </dl>
          <label className="confirmation-check">
            <input
              type="checkbox"
              checked={correctionConfirmed}
              onChange={(event) => setCorrectionConfirmed(event.target.checked)}
              disabled={disabled || correctionLimitReached}
            />
            <span>{t("sandbox.usageCorrections.review", { name: sandbox.metadata.uid })}</span>
          </label>
          <button
            className="button danger"
            type="submit"
            disabled={disabled || correctionLimitReached || !correctionConfirmed}
          >
            {t(
              correctionLimitReached
                ? "sandbox.usageCorrections.limit"
                : "sandbox.usageCorrections.submit",
            )}
          </button>
        </form>
      </section>
      {sandbox.spec.isolationRuntime === "gvisor" ? (
        <p className="boundary-note">{t("runtimeProfile.gvisorLimitation")}</p>
      ) : null}
      <section className="activity-block" aria-labelledby="sandbox-grants-title">
        <div className="activity-heading">
          <h3 id="sandbox-grants-title">{t("sandbox.grants.title")}</h3>
          <span className="mono">{number(grants.length)}</span>
        </div>
        <p>{t("sandbox.grants.description")}</p>
        {grants.length === 0 ? (
          <p className="activity-empty">{t("sandbox.grants.empty")}</p>
        ) : (
          <ul className="activity-list">
            {grants.map((grant) => (
              <li key={grant.metadata.uid}>
                <div>
                  <strong className="mono break">{grant.metadata.uid}</strong>
                  <span className={`phase ${phaseTone(grant.spec.status)}`}>
                    <i /> {t(`sandbox.grants.status.${grant.spec.status}`)}
                  </span>
                </div>
                <small>
                  {t("sandbox.grants.generationExpiry", {
                    generation: number(grant.spec.generation),
                    expiresAt: dateTime(grant.spec.expiresAt),
                  })}
                </small>
                <small>
                  {t("sandbox.grants.sessions", {
                    count: number(grant.spec.ptySessionCount),
                  })}
                </small>
                <small>
                  {t("sandbox.grants.files", {
                    count: number(grant.spec.fileAccessCount),
                    failures: number(grant.spec.fileFailureCount),
                  })}
                </small>
                <small>
                  {grant.spec.previewPorts.length === 0
                    ? t("sandbox.grants.previewPortsEmpty")
                    : t("sandbox.grants.previewPorts", {
                        ports: grant.spec.previewPorts.map(number).join(", "),
                      })}
                </small>
                {grant.spec.lastFileAction === undefined ||
                grant.spec.lastFileStatus === undefined ||
                grant.spec.lastFileAccessAt === undefined ? null : (
                  <small>
                    {t("sandbox.grants.lastFile", {
                      action: t(`sandbox.grants.fileAction.${grant.spec.lastFileAction}`),
                      status: t(`sandbox.grants.fileStatus.${grant.spec.lastFileStatus}`),
                      at: dateTime(grant.spec.lastFileAccessAt),
                    })}
                  </small>
                )}
                {grant.spec.lastFileErrorCode === undefined ? null : (
                  <small className="mono danger-text">{grant.spec.lastFileErrorCode}</small>
                )}
                {grant.spec.status === "active" ? (
                  <button
                    className="button danger"
                    type="button"
                    disabled={disabled}
                    onClick={() => onRevokeGrant(grant)}
                  >
                    {t("sandbox.grants.revoke")}
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </section>
      {action === null ? null : (
        <section className="action-block">
          <h3>
            {t(
              action === "stop" ? "sandbox.lifecycle.stopTitle" : "sandbox.lifecycle.rebuildTitle",
            )}
          </h3>
          <p>
            {t(
              action === "stop"
                ? "sandbox.lifecycle.stopImpact"
                : "sandbox.lifecycle.rebuildImpact",
            )}
          </p>
          <button
            className={`button ${action === "stop" ? "danger" : "primary"}`}
            type="button"
            disabled={disabled}
            onClick={() => onTransition(action)}
          >
            {t(action === "stop" ? "sandbox.lifecycle.stop" : "sandbox.lifecycle.rebuild")}
          </button>
        </section>
      )}
      <p className="boundary-note">{t("sandbox.boundary")}</p>
    </>
  );
}

export function SandboxLifecycleConfirmation({
  sandbox,
  action,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  sandbox: AdminSandboxSession;
  action: SandboxLifecycleAction;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t, number } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  const stopping = action === "stop";
  const actionLabel = t(stopping ? "sandbox.lifecycle.stop" : "sandbox.lifecycle.rebuild");
  return (
    <section className="dialog" aria-labelledby="sandbox-lifecycle-title">
      <div className="panel-heading">
        <div>
          <div className="eyebrow">{t("sandbox.lifecycle.eyebrow")}</div>
          <h2 id="sandbox-lifecycle-title">
            {t("sandbox.lifecycle.title", { action: actionLabel })}
          </h2>
          <p>{sandbox.metadata.name}</p>
        </div>
        <button
          className="icon-button"
          type="button"
          aria-label={t("action.close")}
          onClick={onClose}
        >
          ×
        </button>
      </div>
      <form
        className="resource-form"
        onSubmit={(event) => {
          event.preventDefault();
          onConfirm();
        }}
      >
        <div className={`banner ${stopping ? "danger" : "running"}`} role="status">
          {t(stopping ? "sandbox.lifecycle.stopImpact" : "sandbox.lifecycle.rebuildImpact")}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("sandbox.id")}</dt>
            <dd className="mono">{sandbox.metadata.uid}</dd>
          </div>
          <div>
            <dt>{t("sandbox.lifecycle.expectedGeneration")}</dt>
            <dd className="mono">{number(sandbox.spec.generation)}</dd>
          </div>
          <div>
            <dt>{t("sandbox.lifecycle.expectedResourceVersion")}</dt>
            <dd className="mono">{sandbox.metadata.resourceVersion}</dd>
          </div>
          <div>
            <dt>{t("sandbox.lifecycle.compute")}</dt>
            <dd>
              {t(stopping ? "sandbox.lifecycle.computeDelete" : "sandbox.lifecycle.computeCreate")}
            </dd>
          </div>
          <div>
            <dt>{t("sandbox.lifecycle.workspace")}</dt>
            <dd>{t("sandbox.lifecycle.workspaceRetain")}</dd>
          </div>
          <div>
            <dt>{t("sandbox.physicalVolume")}</dt>
            <dd className="mono break">{sandbox.spec.physicalVolumeId}</dd>
          </div>
        </dl>
        <label className="confirmation-check">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
            disabled={disabled}
            data-sheet-autofocus
          />
          <span>{t("sandbox.lifecycle.review")}</span>
        </label>
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button
            className={`button ${stopping ? "danger" : "primary"}`}
            type="submit"
            disabled={disabled || !confirmed}
          >
            {t("sandbox.lifecycle.confirm", { action: actionLabel })}
          </button>
        </div>
      </form>
    </section>
  );
}

export function SandboxGrantRevokeConfirmation({
  grant,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  grant: AdminSandboxAccessGrant;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t, number, dateTime } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  return (
    <section className="dialog" aria-labelledby="sandbox-grant-revoke-title">
      <div className="panel-heading">
        <div>
          <div className="eyebrow">{t("sandbox.grants.eyebrow")}</div>
          <h2 id="sandbox-grant-revoke-title">{t("sandbox.grants.revokeTitle")}</h2>
          <p className="mono break">{grant.metadata.uid}</p>
        </div>
        <button
          className="icon-button"
          type="button"
          aria-label={t("action.close")}
          onClick={onClose}
        >
          ×
        </button>
      </div>
      <form
        className="resource-form"
        onSubmit={(event) => {
          event.preventDefault();
          onConfirm();
        }}
      >
        <div className="banner danger" role="status">
          {t("sandbox.grants.revokeImpact")}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("sandbox.grants.id")}</dt>
            <dd className="mono break">{grant.metadata.uid}</dd>
          </div>
          <div>
            <dt>{t("sandbox.id")}</dt>
            <dd className="mono break">{grant.spec.sandboxId}</dd>
          </div>
          <div>
            <dt>{t("sandbox.lifecycle.expectedGeneration")}</dt>
            <dd className="mono">{number(grant.spec.generation)}</dd>
          </div>
          <div>
            <dt>{t("sandbox.lifecycle.expectedResourceVersion")}</dt>
            <dd className="mono">{grant.metadata.resourceVersion}</dd>
          </div>
          <div>
            <dt>{t("sandbox.grants.expiresAt")}</dt>
            <dd>{dateTime(grant.spec.expiresAt)}</dd>
          </div>
          <div>
            <dt>{t("sandbox.grants.ptySessions")}</dt>
            <dd>{number(grant.spec.ptySessionCount)}</dd>
          </div>
          <div>
            <dt>{t("sandbox.grants.fileOperations")}</dt>
            <dd>{number(grant.spec.fileAccessCount)}</dd>
          </div>
          <div>
            <dt>{t("sandbox.grants.fileFailures")}</dt>
            <dd>{number(grant.spec.fileFailureCount)}</dd>
          </div>
          <div>
            <dt>{t("sandbox.grants.previewPortsLabel")}</dt>
            <dd className="mono">
              {grant.spec.previewPorts.length === 0
                ? t("sandbox.grants.previewPortsEmptyValue")
                : grant.spec.previewPorts.map(number).join(", ")}
            </dd>
          </div>
        </dl>
        <label className="confirmation-check">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
            disabled={disabled}
            data-sheet-autofocus
          />
          <span>{t("sandbox.grants.revokeReview")}</span>
        </label>
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button className="button danger" type="submit" disabled={disabled || !confirmed}>
            {t("sandbox.grants.confirmRevoke")}
          </button>
        </div>
      </form>
    </section>
  );
}
