import { type MaintenanceOperation } from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { useI18n, type MessageKey } from "../i18n";
import {
  auditLabel,
  operationImpactLabel,
  phaseLabel,
  phaseTone,
  resourceLabel,
} from "./presentation";

export function MaintenanceOperationTable({
  operations,
  emptyMessage = "table.empty.maintenance",
  selectedOperationId,
  onSelect,
}: Readonly<{
  operations: readonly MaintenanceOperation[];
  emptyMessage?: MessageKey;
  selectedOperationId: string;
  onSelect: (operationId: string) => void;
}>) {
  const { t, dateTime } = useI18n();
  if (operations.length === 0) return <div className="table-empty">{t(emptyMessage)}</div>;
  return (
    <div
      className="table-scroll"
      tabIndex={0}
      role="region"
      aria-label={t("page.maintenance.title")}
    >
      <table>
        <thead>
          <tr>
            <th>{t("table.operation")}</th>
            <th>{t("table.resource")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.currentStep")}</th>
            <th>{t("maintenance.stableErrorCode")}</th>
            <th>{t("table.updated")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {operations.map((operation) => (
            <tr
              key={operation.operationId}
              className={operation.operationId === selectedOperationId ? "selected" : ""}
              onClick={() => onSelect(operation.operationId)}
            >
              <td>
                <button type="button" onClick={() => onSelect(operation.operationId)}>
                  <strong>{auditLabel(operation.action, t)}</strong>
                  <small>{operation.operationId}</small>
                </button>
              </td>
              <td>
                <strong>{operation.resourceId}</strong>
                <small className="table-subline">{resourceLabel(operation.resourceKind, t)}</small>
              </td>
              <td>
                <span className={`phase ${phaseTone(operation.state)}`}>
                  <i /> {phaseLabel(operation.state, t)}
                </span>
              </td>
              <td className="mono">{operation.currentStep}</td>
              <td className="mono">{operation.stableErrorCode ?? "—"}</td>
              <td>{dateTime(operation.updatedAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: operation.operationId })}
                  onClick={() => onSelect(operation.operationId)}
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

export function MaintenanceOperationDetail({
  operation,
}: Readonly<{ operation: MaintenanceOperation }>) {
  const { t, number, dateTime } = useI18n();
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          ↻
        </div>
        <div>
          <div className="eyebrow">{auditLabel(operation.action, t)}</div>
          <h2>{operation.resourceId}</h2>
          <span className={`phase ${phaseTone(operation.state)}`}>
            <i /> {phaseLabel(operation.state, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("maintenance.operationId")}</dt>
          <dd className="mono break">{operation.operationId}</dd>
        </div>
        <div>
          <dt>{t("maintenance.resource")}</dt>
          <dd className="mono">
            {operation.resourceKind} · {operation.resourceId}
          </dd>
        </div>
        <div>
          <dt>{t("table.generation")}</dt>
          <dd className="mono">g{number(operation.resourceGeneration)}</dd>
        </div>
        <div>
          <dt>{t("maintenance.currentStep")}</dt>
          <dd className="mono">{operation.currentStep}</dd>
        </div>
        <div>
          <dt>{t("maintenance.requestId")}</dt>
          <dd className="mono break">{operation.requestId}</dd>
        </div>
        <div>
          <dt>{t("maintenance.idempotencyKey")}</dt>
          <dd className="mono break">{operation.idempotencyKey}</dd>
        </div>
        <div>
          <dt>{t("maintenance.requestedBy")}</dt>
          <dd className="mono break">{operation.requestedBy}</dd>
        </div>
        <div>
          <dt>{t("maintenance.requestedAt")}</dt>
          <dd>{dateTime(operation.requestedAt)}</dd>
        </div>
        <div>
          <dt>{t("table.updated")}</dt>
          <dd>{dateTime(operation.updatedAt)}</dd>
        </div>
        <div>
          <dt>{t("maintenance.retryable")}</dt>
          <dd>{t(operation.retryable ? "common.yes" : "common.no")}</dd>
        </div>
        {operation.stableErrorCode ? (
          <div>
            <dt>{t("detail.stableError")}</dt>
            <dd className="danger-text">{operation.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
      <section className="activity-block" aria-labelledby="maintenance-impact-title">
        <div className="activity-heading">
          <h3 id="maintenance-impact-title">{t("maintenance.impact")}</h3>
        </div>
        <p>{operationImpactLabel(operation.action, t)}</p>
        <details className="operation-diagnostic">
          <summary>{t("common.diagnostics")}</summary>
          <p>{operation.impactSummary}</p>
        </details>
      </section>
    </>
  );
}
