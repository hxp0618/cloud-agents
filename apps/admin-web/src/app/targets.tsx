import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { DetailMore } from "./form-fields";
import {
  type AdminAuditEvent,
  type DeploymentTarget,
  type DeploymentTargetRegisterRequest,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetSchedulingPreview,
  type MaintenanceOperation,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { AdminSheet, SheetHeading } from "../AdminSheet";
import { identifierFromName } from "../admin";
import { useI18n } from "../i18n";
import { NameField, Suggestions } from "./form-fields";
import {
  maxKubeconfigBytes,
  parseKubeconfig,
  selectKubeconfigContext,
  type Kubeconfig,
  type KubeconfigContext,
  type KubeconfigProblem,
} from "./kubeconfig";
import {
  auditLabel,
  operationImpactLabel,
  phaseLabel,
  phaseTone,
  resourceLabel,
  targetKindLabel,
} from "./presentation";
import { pageAdminTargets, targetPageSizes } from "../admin";

export type TargetRegistrationDraft = {
  targetName: string;
  targetKind: DeploymentTargetRegisterRequest["targetKind"];
  endpoint: string;
  credentialRef: string;
  kubernetesConnection: "endpoint" | "kubeconfig";
  kubeconfig: string;
  kubeconfigContext: string;
};

export type KubeconfigSelection =
  | Readonly<{ kubeconfig: Kubeconfig; context: KubeconfigContext }>
  | KubeconfigProblem;

export const targetEndpointPlaceholder: Readonly<
  Record<TargetRegistrationDraft["targetKind"], string>
> = Object.freeze({
  docker: "https://docker.example.test:2376",
  kubernetes: "https://kubernetes.example.test:6443",
  ssh: "ssh://worker.example.test:22",
});

export function targetRegistrationForm(): TargetRegistrationDraft {
  return {
    targetName: "",
    targetKind: "docker",
    endpoint: "",
    credentialRef: "",
    kubernetesConnection: "endpoint",
    kubeconfig: "",
    kubeconfigContext: "",
  };
}

function usesKubeconfig(draft: KubeconfigDraft): boolean {
  return draft.targetKind === "kubernetes" && draft.kubernetesConnection === "kubeconfig";
}

type KubeconfigDraft = Pick<
  TargetRegistrationDraft,
  "targetKind" | "kubernetesConnection" | "kubeconfig" | "kubeconfigContext"
>;

/** Returns null until a kubeconfig is entered for a kubeconfig-based Kubernetes target. */
export function kubeconfigSelectionFrom(draft: KubeconfigDraft): KubeconfigSelection | null {
  if (!usesKubeconfig(draft) || draft.kubeconfig.trim() === "") return null;
  const kubeconfig = parseKubeconfig(draft.kubeconfig);
  if (typeof kubeconfig === "string") return kubeconfig;
  return { kubeconfig, context: selectKubeconfigContext(kubeconfig, draft.kubeconfigContext) };
}

/** Returns null when the selected kubeconfig context cannot be registered. */
export function deploymentTargetRegisterRequestFrom(
  draft: TargetRegistrationDraft,
): DeploymentTargetRegisterRequest | null {
  const targetName = draft.targetName.trim();
  const request = {
    targetId: identifierFromName(targetName, draft.targetKind),
    targetName,
    targetKind: draft.targetKind,
    endpoint: draft.endpoint.trim(),
    credentialRef: draft.credentialRef.trim(),
  };
  if (!usesKubeconfig(draft)) return request;
  const selection = kubeconfigSelectionFrom(draft);
  if (selection === null || typeof selection === "string") return null;
  const { connection } = selection.context;
  if (connection === null) return null;
  return {
    ...request,
    endpoint: connection.endpoint,
    kubernetesCredential: connection.credential,
  };
}

/** Drops kubeconfig secrets from a draft while keeping the other fields. */
export function withoutKubeconfig(draft: TargetRegistrationDraft): TargetRegistrationDraft {
  return { ...draft, kubeconfig: "", kubeconfigContext: "" };
}

export function TargetRegistrationForm({
  draft,
  feedback,
  disabled,
  nameTaken,
  credentialRefSuggestions,
  onDraftChange,
  onClose,
  onSubmit,
}: Readonly<{
  draft: TargetRegistrationDraft;
  feedback: ReactNode;
  disabled: boolean;
  nameTaken: boolean;
  credentialRefSuggestions: readonly string[];
  onDraftChange: (draft: TargetRegistrationDraft) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}>) {
  const { t } = useI18n();
  const { targetKind, kubernetesConnection, kubeconfig, kubeconfigContext } = draft;
  const kubeconfigMode = usesKubeconfig(draft);
  const selection = useMemo(
    () =>
      kubeconfigSelectionFrom({ targetKind, kubernetesConnection, kubeconfig, kubeconfigContext }),
    [targetKind, kubernetesConnection, kubeconfig, kubeconfigContext],
  );
  const selected = selection === null || typeof selection === "string" ? null : selection;
  const problem = typeof selection === "string" ? selection : (selected?.context.problem ?? null);
  const connection = selected?.context.connection ?? null;
  async function loadKubeconfigFile(input: HTMLInputElement) {
    const file = input.files?.[0];
    input.value = "";
    if (file === undefined) return;
    // Reading one byte past the limit is enough for the parser to report tooLarge.
    const text = await file.slice(0, maxKubeconfigBytes + 1).text();
    onDraftChange({ ...draft, kubeconfig: text, kubeconfigContext: "" });
  }
  return (
    <AdminSheet feedback={feedback} label={t("target.register.title")} onClose={onClose}>
      <section className="dialog" aria-labelledby="register-title">
        <SheetHeading id="register-title" title={t("target.register.title")} onClose={onClose} />
        <form className="resource-form" onSubmit={onSubmit}>
          <NameField
            label={t("target.displayName")}
            value={draft.targetName}
            takenMessage={nameTaken ? t("form.nameTaken") : ""}
            placeholder="docker-primary"
            autoFocus
            onChange={(targetName) => onDraftChange({ ...draft, targetName })}
          />
          <label>
            <span>{t("target.kind")}</span>
            <select
              value={draft.targetKind}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  targetKind: event.target.value as TargetRegistrationDraft["targetKind"],
                })
              }
            >
              <option value="docker">{t("target.kind.docker")}</option>
              <option value="kubernetes">{t("target.kind.kubernetes")}</option>
              <option value="ssh">{t("target.kind.ssh")}</option>
            </select>
          </label>
          {draft.targetKind === "kubernetes" ? (
            <label>
              <span>{t("target.connection")}</span>
              <select
                value={draft.kubernetesConnection}
                onChange={(event) =>
                  onDraftChange({
                    ...withoutKubeconfig(draft),
                    kubernetesConnection: event.target
                      .value as TargetRegistrationDraft["kubernetesConnection"],
                  })
                }
              >
                <option value="endpoint">{t("target.connection.endpoint")}</option>
                <option value="kubeconfig">{t("target.connection.kubeconfig")}</option>
              </select>
            </label>
          ) : null}
          {kubeconfigMode ? (
            <>
              <label>
                <span>{t("target.kubeconfig.file")}</span>
                <input
                  type="file"
                  accept=".yaml,.yml,.conf,.config,text/yaml,application/yaml,text/plain"
                  onChange={(event) => void loadKubeconfigFile(event.currentTarget)}
                />
              </label>
              <label>
                <span>{t("target.kubeconfig.content")}</span>
                <textarea
                  value={draft.kubeconfig}
                  rows={8}
                  required
                  spellCheck={false}
                  autoComplete="off"
                  aria-invalid={problem === null ? undefined : true}
                  onChange={(event) => onDraftChange({ ...draft, kubeconfig: event.target.value })}
                />
                {problem === null ? (
                  <small>{t("target.kubeconfig.help")}</small>
                ) : (
                  <small className="danger-text" role="alert">
                    {t(`target.kubeconfig.problem.${problem}`)}
                  </small>
                )}
              </label>
              {selected === null ? null : (
                <>
                  <label>
                    <span>{t("target.kubeconfig.context")}</span>
                    <select
                      value={selected.context.name}
                      onChange={(event) =>
                        onDraftChange({ ...draft, kubeconfigContext: event.target.value })
                      }
                    >
                      {selected.kubeconfig.contexts.map(({ name }) => (
                        <option key={name} value={name}>
                          {name === selected.kubeconfig.currentContext
                            ? `${name} (${t("target.kubeconfig.current")})`
                            : name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    <span>{t("target.kubeconfig.server")}</span>
                    <input value={selected.context.server} readOnly spellCheck={false} />
                    {connection === null ? null : (
                      <small>{t(`target.kubeconfig.auth.${connection.authentication}`)}</small>
                    )}
                  </label>
                </>
              )}
            </>
          ) : (
            <label>
              <span>{t("target.endpoint")}</span>
              <input
                type="url"
                value={draft.endpoint}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    endpoint: event.target.value,
                  })
                }
                placeholder={targetEndpointPlaceholder[draft.targetKind]}
                maxLength={2048}
                required
                spellCheck={false}
              />
            </label>
          )}
          <label>
            <span>{t("target.credentialRef")}</span>
            <input
              value={draft.credentialRef}
              list="target-credential-refs"
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  credentialRef: event.target.value,
                })
              }
              placeholder={`${draft.targetKind}-primary`}
              maxLength={128}
              required
              spellCheck={false}
            />
            <Suggestions id="target-credential-refs" values={credentialRefSuggestions} />
            <small>
              {t(
                kubeconfigMode ? "target.kubeconfig.credentialRefHelp" : "target.credentialRefHelp",
              )}
            </small>
          </label>
          <div className="dialog-actions">
            <button className="button ghost" type="button" onClick={() => onClose()}>
              {t("action.cancel")}
            </button>
            <button
              className="button primary"
              type="submit"
              disabled={disabled || (kubeconfigMode && connection === null)}
            >
              {t("action.registerTarget")}
            </button>
          </div>
        </form>
      </section>
    </AdminSheet>
  );
}

export function PaginatedTargets({
  filterKey,
  targets,
  filtered,
  selectedTargetId,
  onSelect,
  empty,
}: Readonly<{
  filterKey: string;
  targets: readonly DeploymentTarget[];
  filtered: boolean;
  selectedTargetId: string;
  onSelect: (id: string) => void;
  empty: ReactNode;
}>) {
  const { t, number } = useI18n();
  const [size, setSize] = useState(25);
  const [position, setPosition] = useState({ filterKey, index: 0 });
  const page = pageAdminTargets(
    targets,
    position.filterKey === filterKey ? position.index : 0,
    size,
  );
  // Keep the stored position in range when filters or a refreshed resource snapshot shrink it.
  if (position.filterKey !== filterKey || position.index !== page.index)
    setPosition({ filterKey, index: page.index });
  const changePage = (index: number) => setPosition({ filterKey, index });
  if (targets.length === 0) return empty;
  return (
    <>
      <div className="panel target-list-panel">
        <TargetTable
          targets={page.items}
          filtered={filtered}
          selectedTargetId={selectedTargetId}
          onSelect={onSelect}
        />
      </div>
      <nav
        className="resource-pagination"
        aria-label={t("pagination.label")}
        data-total={targets.length}
      >
        <div className="pagination-summary">
          <select
            aria-label={t("pagination.size")}
            value={size}
            onChange={(event) => {
              setSize(Number(event.target.value));
              changePage(0);
            }}
          >
            {targetPageSizes.map((value) => (
              <option key={value} value={value}>
                {t("pagination.perPage", { count: number(value) })}
              </option>
            ))}
          </select>
          <span>{t("pagination.total", { count: number(targets.length) })}</span>
        </div>
        <div className="pagination-navigation">
          <span role="status">
            {t("pagination.page", {
              page: number(page.index + 1),
              pages: number(page.count),
            })}
          </span>
          <div className="pagination-buttons">
            {(
              [
                ["first", 0, page.index === 0, "M11 6l-6 6 6 6M19 6l-6 6 6 6"],
                ["previous", page.index - 1, page.index === 0, "M15 6l-6 6 6 6"],
                ["next", page.index + 1, page.index === page.count - 1, "M9 6l6 6-6 6"],
                [
                  "last",
                  page.count - 1,
                  page.index === page.count - 1,
                  "M5 6l6 6-6 6M13 6l6 6-6 6",
                ],
              ] as const
            ).map(([action, index, disabled, path]) => (
              <button
                key={action}
                type="button"
                className={`button outline pagination-${action}`}
                aria-label={t(`pagination.${action}`)}
                title={t(`pagination.${action}`)}
                disabled={disabled}
                onClick={() => changePage(index)}
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <path d={path} />
                </svg>
              </button>
            ))}
          </div>
        </div>
      </nav>
    </>
  );
}

export function TargetTable({
  targets,
  filtered = false,
  selectedTargetId,
  onSelect,
}: Readonly<{
  targets: readonly DeploymentTarget[];
  filtered?: boolean;
  selectedTargetId: string;
  onSelect: (targetId: string) => void;
}>) {
  const { t, dateTime } = useI18n();
  if (targets.length === 0)
    return (
      <div className="table-empty">
        {t(filtered ? "target.filter.noMatches" : "table.empty.targets")}
      </div>
    );
  return (
    <div className="table-scroll" tabIndex={0} role="region" aria-label={t("page.targets.title")}>
      <table className="target-table">
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("table.kind")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.engineApi")}</th>
            <th>{t("table.lastProbe")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {targets.map((target) => (
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
                <span className="kind-badge" data-kind={target.spec.targetKind}>
                  {targetKindLabel(target.spec.targetKind, t)}
                </span>
              </td>
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
              <td className="target-probe-facts">
                {target.spec.engineVersion === "" ? (
                  "—"
                ) : (
                  <>
                    <span>{target.spec.engineVersion}</span>
                    <small className="table-subline">
                      {t("cluster.apiVersion", { version: target.spec.apiVersion })}
                    </small>
                  </>
                )}
              </td>
              <td>{dateTime(target.spec.lastProbeAt)}</td>
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

export function TargetDetail({
  target,
  operations,
  audit,
  onProbe,
  onPreviewScheduling,
  onPreviewCleanup,
  disabled,
}: Readonly<{
  target: DeploymentTarget;
  operations: readonly MaintenanceOperation[];
  audit: readonly AdminAuditEvent[];
  onProbe: () => void;
  onPreviewScheduling: () => void;
  onPreviewCleanup: () => void;
  disabled: boolean;
}>) {
  const { t, number, dateTime } = useI18n();
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          {target.spec.targetKind.slice(0, 1).toUpperCase()}
        </div>
        <div>
          <div className="eyebrow">
            {t("detail.targetEyebrow", {
              kind: targetKindLabel(target.spec.targetKind, t),
            })}
          </div>
          <h2>{target.metadata.name}</h2>
          <span className={`phase ${phaseTone(target.spec.observedPhase)}`}>
            <i />
            {phaseLabel(target.spec.observedPhase, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("target.endpoint")}</dt>
          <dd className="mono break">{target.spec.endpoint}</dd>
        </div>
        <div>
          <dt>{t("target.credentialRef")}</dt>
          <dd className="mono">{target.spec.credentialRef}</dd>
        </div>
        <div>
          <dt>{t("detail.schedulingState")}</dt>
          <dd>{phaseLabel(target.spec.schedulingState, t)}</dd>
        </div>
        <div>
          <dt>{t("detail.runtimeApi")}</dt>
          <dd>{target.spec.apiVersion || t("common.notObserved")}</dd>
        </div>
        <div>
          <dt>{t("detail.engine")}</dt>
          <dd>{target.spec.engineVersion || t("common.notObserved")}</dd>
        </div>
        <div>
          <dt>{t("detail.platform")}</dt>
          <dd>
            {[target.spec.os, target.spec.architecture].filter(Boolean).join(" / ") ||
              t("common.notObserved")}
          </dd>
        </div>
        <div>
          <dt>{t("table.lastProbe")}</dt>
          <dd>{dateTime(target.spec.lastProbeAt)}</dd>
        </div>
        {target.spec.stableErrorCode !== "" ? (
          <div>
            <dt>{t("detail.stableError")}</dt>
            <dd className="danger-text">{target.spec.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
      <DetailMore title={t("detail.diagnostics")}>
        <div>
          <dt>{t("target.id")}</dt>
          <dd className="mono">{target.metadata.uid}</dd>
        </div>
        <div>
          <dt>{t("table.generation")}</dt>
          <dd className="mono">{number(target.spec.generation)}</dd>
        </div>
        <div>
          <dt>{t("detail.resourceVersion")}</dt>
          <dd className="mono">{target.metadata.resourceVersion}</dd>
        </div>
      </DetailMore>
      {target.spec.targetKind === "remote-worker" ? (
        <p className="cluster-boundary">{t("detail.remoteWorkerManaged")}</p>
      ) : null}
      <section className="action-block">
        <div>
          <h3>{t("detail.schedulingTitle")}</h3>
          <p>{t("detail.schedulingDescription")}</p>
        </div>
        <button
          className="button ghost"
          type="button"
          onClick={onPreviewScheduling}
          disabled={disabled || target.spec.targetKind === "remote-worker"}
        >
          {t(
            target.spec.schedulingState === "active"
              ? "detail.previewDrain"
              : "detail.previewResume",
          )}
        </button>
      </section>
      <section className="action-block">
        <div>
          <h3>{t("detail.probeTitle")}</h3>
          <p>{t("detail.probeDescription")}</p>
        </div>
        <button
          className="button primary"
          type="button"
          onClick={onProbe}
          disabled={
            disabled ||
            target.spec.targetKind === "remote-worker" ||
            target.spec.observedPhase === "probing"
          }
        >
          {t("detail.runProbe")}
        </button>
      </section>
      <section className="activity-block" aria-labelledby="target-operations-title">
        <div className="activity-heading">
          <h3 id="target-operations-title">{t("detail.operations")}</h3>
        </div>
        {operations.length === 0 ? (
          <p className="activity-empty">{t("detail.noOperations")}</p>
        ) : (
          <ol className="activity-list">
            {operations.map((operation) => (
              <li key={operation.operationId}>
                <div>
                  <strong>{auditLabel(operation.action, t)}</strong>
                  <span className={`phase ${phaseTone(operation.state)}`}>
                    <i /> {phaseLabel(operation.state, t)}
                  </span>
                </div>
                <p>{operationImpactLabel(operation.action, t)}</p>
                <details className="operation-diagnostic">
                  <summary>{t("common.diagnostics")}</summary>
                  <p>{operation.impactSummary}</p>
                  {operation.stableErrorCode ? <code>{operation.stableErrorCode}</code> : null}
                </details>
                <small className="mono">
                  {operation.operationId} · g{number(operation.resourceGeneration)} ·{" "}
                  {operation.currentStep}
                </small>
                <small>{dateTime(operation.updatedAt)}</small>
              </li>
            ))}
          </ol>
        )}
      </section>
      <section className="activity-block" aria-labelledby="target-audit-title">
        <div className="activity-heading">
          <h3 id="target-audit-title">{t("detail.audit")}</h3>
        </div>
        {audit.length === 0 ? (
          <p className="activity-empty">{t("detail.noTargetAudit")}</p>
        ) : (
          <ol className="activity-list compact">
            {audit.map((event) => (
              <li key={event.eventId}>
                <div>
                  <strong>{auditLabel(event.action, t)}</strong>
                  <span className={`phase ${phaseTone(event.result)}`}>
                    <i /> {phaseLabel(event.result, t)}
                  </span>
                </div>
                <small className="mono break">{t("common.actor", { actor: event.actor })}</small>
                <small className="mono">
                  {event.requestId} · {dateTime(event.occurredAt)}
                </small>
              </li>
            ))}
          </ol>
        )}
      </section>
      <section className="action-block cleanup-preview-block">
        <div>
          <h3>{t("detail.cleanupImpact")}</h3>
          <p>{t("detail.cleanupImpactDescription")}</p>
        </div>
        <button
          className="button ghost"
          type="button"
          onClick={onPreviewCleanup}
          disabled={disabled || target.spec.targetKind === "remote-worker"}
        >
          {t("detail.previewCleanup")}
        </button>
      </section>
    </>
  );
}

export function SchedulingConfirmation({
  target,
  preview,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  target: DeploymentTarget;
  preview: DeploymentTargetSchedulingPreview;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t, number } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  const draining = preview.spec.desiredState === "drained";
  return (
    <section className="dialog" aria-labelledby="scheduling-title">
      <SheetHeading
        id="scheduling-title"
        title={t("scheduling.confirmTitle")}
        subject={target.metadata.name}
        onClose={onClose}
      />
      <form
        className="resource-form"
        onSubmit={(event) => {
          event.preventDefault();
          onConfirm();
        }}
      >
        <div className={`banner ${draining ? "danger" : "running"}`} role="status">
          {t(draining ? "scheduling.drainSummary" : "scheduling.resumeSummary", {
            leases: number(preview.spec.activeLeases.length),
          })}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("lease.target")}</dt>
            <dd className="mono">{target.metadata.uid}</dd>
          </div>
        </dl>
        <div className="cleanup-preview" aria-label={t("detail.schedulingTitle")}>
          {preview.spec.activeLeases.length === 0 ? (
            <p>{t("scheduling.none")}</p>
          ) : (
            preview.spec.activeLeases.map((lease) => (
              <article className="cleanup-worker" key={lease.leaseId}>
                <div>
                  <strong className="mono">{lease.leaseName}</strong>
                  <span className={`phase ${phaseTone(lease.observedPhase)}`}>
                    <i /> {phaseLabel(lease.observedPhase, t)}
                  </span>
                </div>
                <small className="mono">
                  {lease.leaseId} · g{number(lease.generation)}
                </small>
              </article>
            ))
          )}
        </div>
        <label className="confirmation-check">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
            disabled={disabled}
            data-sheet-autofocus
          />
          <span>{t("scheduling.review")}</span>
        </label>
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button
            className={`button ${draining ? "danger" : "primary"}`}
            type="submit"
            disabled={disabled || !confirmed}
          >
            {t(draining ? "scheduling.confirmDrain" : "scheduling.confirmResume")}
          </button>
        </div>
      </form>
    </section>
  );
}

export function CleanupConfirmation({
  target,
  preview,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  target: DeploymentTarget;
  preview: DeploymentTargetCleanupPreview;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t, number } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  const resourceCount = preview.spec.workers.reduce(
    (count, worker) => count + worker.resources.length,
    0,
  );
  return (
    <section className="dialog" aria-labelledby="cleanup-title">
      <SheetHeading
        id="cleanup-title"
        title={t("cleanup.confirmTitle")}
        subject={target.metadata.name}
        onClose={onClose}
      />
      <form
        className="resource-form"
        onSubmit={(event) => {
          event.preventDefault();
          onConfirm();
        }}
      >
        <div className={`banner ${preview.spec.canCleanup ? "running" : "danger"}`} role="status">
          {preview.spec.canCleanup
            ? t("cleanup.summary", {
                workers: number(preview.spec.workers.length),
                resources: number(resourceCount),
              })
            : t("cleanup.blocked")}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("lease.target")}</dt>
            <dd className="mono">{target.metadata.uid}</dd>
          </div>
        </dl>
        <div className="cleanup-preview" aria-label={t("detail.cleanupImpact")}>
          {preview.spec.workers.length === 0 ? (
            <p>{t("cleanup.none")}</p>
          ) : (
            preview.spec.workers.map((worker) => (
              <article
                className="cleanup-worker"
                key={`${worker.workerName}:${worker.leaseGeneration}`}
              >
                <div>
                  <strong className="mono">{worker.workerName}</strong>
                  <span
                    className={`phase ${worker.disposition === "blocked" ? "danger" : "success"}`}
                  >
                    <i /> {phaseLabel(worker.disposition, t)}
                  </span>
                </div>
                <small className="mono">
                  {worker.leaseId} · g{number(worker.leaseGeneration)}
                </small>
                <ul>
                  {worker.resources.map((resource) => (
                    <li key={`${resource.resourceKind}:${resource.resourceName}`}>
                      <span>{resourceLabel(resource.resourceKind, t)}</span>
                      <code>{resource.resourceName}</code>
                    </li>
                  ))}
                </ul>
              </article>
            ))
          )}
        </div>
        {preview.spec.canCleanup ? (
          <label className="confirmation-check">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(event) => setConfirmed(event.target.checked)}
              disabled={disabled}
              data-sheet-autofocus
            />
            <span>{t("cleanup.review")}</span>
          </label>
        ) : null}
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button
            className="button danger"
            type="submit"
            disabled={disabled || !preview.spec.canCleanup || !confirmed}
          >
            {t("cleanup.confirm")}
          </button>
        </div>
      </form>
    </section>
  );
}
