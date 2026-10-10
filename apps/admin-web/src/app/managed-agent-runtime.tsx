import { useState } from "react";
import {
  type AdminSandboxSession,
  type ManagedAgentEvent,
  type ManagedAgentExecution,
  type ManagedAgentSideEffectReconciliationRequest,
  type RuntimeProfile,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import {
  CLOUD_AGENT_PROVIDER_CAPABILITY_CATALOG,
  type CloudAgentProviderCapabilityCatalogEntry,
} from "@cloud-agents/cloud-agent-protocol";
import { useI18n } from "../i18n";
import { phaseLabel, phaseTone, providerLabel, shortDigest } from "./presentation";
import { AdminAgentEvents } from "../AdminAgentEvents";
import type { AdminClient, AdminManagedAgentRuntime, SavedAdminConnection } from "../admin";

export function ManagedAgentRuntimeSection({
  sandbox,
  runtime,
  client,
  connection,
  runtimeProfiles,
  disabled,
  onReconcileSideEffect,
  onNextSessions,
  onNextExecutions,
  onSelectSession,
}: Readonly<{
  sandbox: AdminSandboxSession;
  runtime: AdminManagedAgentRuntime;
  client: AdminClient | null;
  connection: SavedAdminConnection;
  runtimeProfiles: readonly RuntimeProfile[];
  disabled: boolean;
  onReconcileSideEffect: (
    execution: ManagedAgentExecution,
    outcome: ManagedAgentSideEffectReconciliationRequest["outcome"],
  ) => void;
  onNextSessions: () => void;
  onNextExecutions: () => void;
  onSelectSession: (sessionId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  const [confirmedExecutionId, setConfirmedExecutionId] = useState("");
  const { sessions, executions } = runtime;
  const profile = runtimeProfiles.find(
    ({ spec }) =>
      spec.profileId === sandbox.spec.runtimeProfileId &&
      spec.version === sandbox.spec.runtimeProfileVersion,
  );
  const providerEntry = (
    providerKind: string,
  ): CloudAgentProviderCapabilityCatalogEntry | undefined =>
    CLOUD_AGENT_PROVIDER_CAPABILITY_CATALOG.providers.find(
      ({ provider }) => provider === providerKind,
    );
  return (
    <section className="activity-block" aria-labelledby="managed-agent-runtime-title">
      <div className="activity-heading">
        <h3 id="managed-agent-runtime-title">{t("agentRuntime.title")}</h3>
        <span className="mono">{number(sessions.length)}</span>
      </div>
      <p>{t("agentRuntime.description")}</p>
      <p>{t("agentRuntime.windowDescription")}</p>
      <div className="button-row">
        <label>
          <span>{t("agentRuntime.sessionWindow")}</span>
          <select
            value={runtime.selectedSessionId}
            onChange={(event) => onSelectSession(event.target.value)}
            disabled={disabled}
          >
            {sessions.map(({ metadata }) => (
              <option key={metadata.uid} value={metadata.uid}>
                {metadata.uid}
              </option>
            ))}
          </select>
        </label>
        <button
          className="button outline"
          type="button"
          disabled={disabled || runtime.nextSessionPageToken === undefined}
          onClick={onNextSessions}
        >
          {t("agentRuntime.nextSessionWindow")}
        </button>
        <button
          className="button outline"
          type="button"
          disabled={disabled || runtime.nextExecutionPageToken === undefined}
          onClick={onNextExecutions}
        >
          {t("agentRuntime.nextExecutionWindow")}
        </button>
      </div>
      {sessions.length === 0 ? (
        <p className="activity-empty">{t("agentRuntime.empty")}</p>
      ) : (
        <ul className="activity-list">
          {sessions.map((session) => {
            const provider = providerEntry(session.spec.providerKind);
            const sessionExecutions =
              session.metadata.uid === runtime.selectedSessionId ? executions : [];
            const nativeCapabilities = Object.entries(provider?.capabilities ?? {})
              .filter(([, support]) => support === "native")
              .map(([capability]) => capability)
              .join(", ");
            const emulatedCapabilities = Object.entries(provider?.capabilities ?? {})
              .filter(([, support]) => support === "emulated")
              .map(([capability]) => capability)
              .join(", ");
            return (
              <li key={session.metadata.uid}>
                <div>
                  <strong>{providerLabel(session.spec.providerKind)}</strong>
                  <span className={`phase ${phaseTone(session.spec.state)}`}>
                    <i /> {phaseLabel(session.spec.state, t)}
                  </span>
                </div>
                <small className="mono break">
                  {session.metadata.uid} · {provider?.adapterVersion ?? t("common.notAvailable")}
                </small>
                <small>
                  {t("agentRuntime.release", {
                    release: profile?.spec.releaseDigest
                      ? shortDigest(profile.spec.releaseDigest)
                      : t("common.notAvailable"),
                    version:
                      provider?.runtimePolicy.compatibleRange.minimumInclusive ??
                      t("common.notAvailable"),
                  })}
                </small>
                <small>
                  {t("agentRuntime.nativeCapabilities", { capabilities: nativeCapabilities })}
                </small>
                {emulatedCapabilities ? (
                  <small>
                    {t("agentRuntime.emulatedCapabilities", { capabilities: emulatedCapabilities })}
                  </small>
                ) : null}
                <small className="mono break">
                  {t("agentRuntime.placement", {
                    target: sandbox.spec.targetId,
                    runtime: sandbox.spec.runtimeId ?? t("common.notBound"),
                  })}
                </small>
                {session.metadata.uid !==
                runtime.selectedSessionId ? null : sessionExecutions.length === 0 ? (
                  <small>{t("agentRuntime.executionEmpty")}</small>
                ) : (
                  <ul className="activity-list">
                    {sessionExecutions.map((execution) => {
                      const checkpoint = execution.spec.checkpoint;
                      const canReconcile =
                        execution.spec.recoveryState === "awaiting_reconciliation" &&
                        execution.spec.recoveryReason === "side_effect_outcome_unknown" &&
                        checkpoint?.pendingSideEffect === true;
                      return (
                        <li key={execution.metadata.uid}>
                          <div>
                            <strong className="mono break">{execution.metadata.uid}</strong>
                            <span className={`phase ${phaseTone(execution.spec.state)}`}>
                              <i /> {phaseLabel(execution.spec.state, t)}
                            </span>
                          </div>
                          <small>
                            {t("agentRuntime.attempt", {
                              attempt: number(execution.spec.attemptNumber),
                              generation: number(execution.spec.generation),
                              recovery: execution.spec.recoveryState,
                            })}
                          </small>
                          {execution.spec.recoveryMode === undefined ? null : (
                            <small className="mono break">
                              {t(`agentRuntime.recoveryMode.${execution.spec.recoveryMode}`)} ·{" "}
                              {t("agentRuntime.recoveryPlacement", {
                                source:
                                  execution.spec.recoverySourceTargetId ?? t("common.notAvailable"),
                                target: execution.spec.recoveryTargetId ?? t("common.notAvailable"),
                              })}
                            </small>
                          )}
                          <small>
                            {execution.spec.claimExpiresAt === undefined
                              ? t("agentRuntime.heartbeatMissing")
                              : t("agentRuntime.heartbeat", {
                                  at: dateTime(execution.spec.claimExpiresAt),
                                })}
                          </small>
                          {execution.spec.recoveryReason === undefined ? null : (
                            <small className="danger-text">
                              {t("agentRuntime.recoveryReason", {
                                reason: execution.spec.recoveryReason,
                              })}
                            </small>
                          )}
                          {checkpoint === undefined ? (
                            <small>{t("agentRuntime.checkpointMissing")}</small>
                          ) : (
                            <small className="mono break">
                              {t("agentRuntime.checkpoint", {
                                sequence: number(checkpoint.sequence),
                                protocol: checkpoint.protocol,
                                digest: shortDigest(checkpoint.digest),
                                at: dateTime(checkpoint.createdAt),
                              })}
                            </small>
                          )}
                          {canReconcile ? (
                            <div className="resource-form">
                              <p className="danger-text">{t("agentRuntime.reconcileImpact")}</p>
                              <label className="confirmation-check">
                                <input
                                  type="checkbox"
                                  checked={confirmedExecutionId === execution.metadata.uid}
                                  onChange={(event) =>
                                    setConfirmedExecutionId(
                                      event.target.checked ? execution.metadata.uid : "",
                                    )
                                  }
                                  disabled={disabled}
                                />
                                <span>
                                  {t("agentRuntime.reconcileReview", {
                                    name: execution.metadata.uid,
                                    generation: number(execution.spec.generation),
                                  })}
                                </span>
                              </label>
                              <div className="button-row">
                                <button
                                  className="button danger"
                                  type="button"
                                  disabled={
                                    disabled || confirmedExecutionId !== execution.metadata.uid
                                  }
                                  onClick={() => {
                                    setConfirmedExecutionId("");
                                    onReconcileSideEffect(execution, "confirmed");
                                  }}
                                >
                                  {t("agentRuntime.reconcileConfirmed")}
                                </button>
                                <button
                                  className="button danger"
                                  type="button"
                                  disabled={
                                    disabled || confirmedExecutionId !== execution.metadata.uid
                                  }
                                  onClick={() => {
                                    setConfirmedExecutionId("");
                                    onReconcileSideEffect(execution, "not-applied");
                                  }}
                                >
                                  {t("agentRuntime.reconcileNotApplied")}
                                </button>
                              </div>
                            </div>
                          ) : null}
                        </li>
                      );
                    })}
                  </ul>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <div className="activity-heading">
        <h3>{t("agentRuntime.auditTitle")}</h3>
      </div>
      <AdminAgentEvents
        key={sandbox.metadata.uid}
        client={client}
        connection={connection}
        sessions={sessions}
      >
        {(events) =>
          events.length === 0 ? (
            <p className="activity-empty">{t("agentRuntime.auditEmpty")}</p>
          ) : (
            <ul className="activity-list">
              {events.map((event) => (
                <li key={event.metadata.uid}>
                  <div>
                    <strong>{event.spec.operation}</strong>
                    <span className="mono">#{event.metadata.sequence}</span>
                  </div>
                  <small>
                    {dateTime(event.metadata.occurredAt)} ·{" "}
                    {event.spec.executionId ?? sessionLabel(event)}
                  </small>
                  <small className="mono break">{shortDigest(event.spec.mutationDigest)}</small>
                  {event.spec.errorCode === undefined ? null : (
                    <small className="danger-text">{event.spec.errorCode}</small>
                  )}
                </li>
              ))}
            </ul>
          )
        }
      </AdminAgentEvents>
    </section>
  );
}

function sessionLabel(event: ManagedAgentEvent): string {
  return event.spec.turnId ?? event.metadata.sessionId;
}
