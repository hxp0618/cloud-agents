import { useState } from "react";
import { useI18n } from "../i18n";
import { phaseLabel, phaseTone } from "./presentation";
import { type WorkspaceSnapshot } from "@cloud-agents/cloud-agent-platform-sdk/platform";

export function WorkspaceSnapshotTable({
  snapshots,
  onCleanup,
}: Readonly<{
  snapshots: readonly WorkspaceSnapshot[];
  onCleanup: (snapshot: WorkspaceSnapshot) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (snapshots.length === 0)
    return <div className="table-empty">{t("workspaceSnapshot.empty")}</div>;
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>{t("workspaceSnapshot.id")}</th>
            <th>{t("workspaceSnapshot.sourceWorkspace")}</th>
            <th>{t("workspaceSnapshot.sourceTarget")}</th>
            <th>{t("workspaceSnapshot.backend")}</th>
            <th>{t("table.status")}</th>
            <th>{t("workspaceSnapshot.size")}</th>
            <th>{t("workspaceSnapshot.retention")}</th>
            <th>{t("workspaceSnapshot.expires")}</th>
            <th>{t("workspaceSnapshot.operation")}</th>
            <th>{t("table.updated")}</th>
            <th>{t("table.actions")}</th>
          </tr>
        </thead>
        <tbody>
          {snapshots.map((snapshot) => (
            <tr key={snapshot.metadata.uid}>
              <td>
                <strong>{snapshot.metadata.name}</strong>
                <small className="mono">rv{snapshot.metadata.resourceVersion}</small>
              </td>
              <td>
                <span className="mono">{snapshot.spec.sourceWorkspaceId}</span>
                <small>rv{snapshot.spec.sourceWorkspaceResourceVersion}</small>
              </td>
              <td className="mono">{snapshot.spec.sourceTargetId}</td>
              <td className="mono">{snapshot.spec.backend}</td>
              <td>
                <span className={`phase ${phaseTone(snapshot.spec.status)}`}>
                  <i /> {phaseLabel(snapshot.spec.status, t)}
                </span>
              </td>
              <td>
                {snapshot.spec.sizeBytes === undefined
                  ? "—"
                  : `${number(snapshot.spec.sizeBytes)} B`}
              </td>
              <td>
                {snapshot.spec.retentionSeconds === undefined
                  ? t("common.never")
                  : t("workspaceSnapshot.retentionValue", {
                      seconds: number(snapshot.spec.retentionSeconds),
                    })}
              </td>
              <td>
                {snapshot.spec.expiresAt === undefined
                  ? t("common.never")
                  : dateTime(snapshot.spec.expiresAt)}
              </td>
              <td className="mono">
                {snapshot.spec.cleanupOperationId ?? snapshot.spec.operationId}
              </td>
              <td>{dateTime(snapshot.metadata.updatedAt)}</td>
              <td>
                {snapshot.spec.status === "available" ||
                snapshot.spec.status === "cleanup_failed" ? (
                  <button
                    className="button danger compact"
                    type="button"
                    onClick={() => onCleanup(snapshot)}
                  >
                    {t("workspaceSnapshot.cleanupAction")}
                  </button>
                ) : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function WorkspaceSnapshotCleanupConfirmation({
  snapshot,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  snapshot: WorkspaceSnapshot;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t, number, dateTime } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  return (
    <section className="dialog" aria-labelledby="snapshot-cleanup-title">
      <div className="panel-heading">
        <div>
          <div className="eyebrow">snapshots.delete · {t("common.destructive")}</div>
          <h2 id="snapshot-cleanup-title">{t("workspaceSnapshot.cleanupTitle")}</h2>
          <p>{t("workspaceSnapshot.cleanupDescription")}</p>
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
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("workspaceSnapshot.id")}</dt>
            <dd className="mono">{snapshot.metadata.uid}</dd>
          </div>
          <div>
            <dt>{t("workspaceSnapshot.sourceWorkspace")}</dt>
            <dd className="mono">{snapshot.spec.sourceWorkspaceId}</dd>
          </div>
          <div>
            <dt>{t("detail.resourceVersion")}</dt>
            <dd className="mono">{snapshot.metadata.resourceVersion}</dd>
          </div>
          <div>
            <dt>{t("workspaceSnapshot.size")}</dt>
            <dd>
              {snapshot.spec.sizeBytes === undefined ? "—" : `${number(snapshot.spec.sizeBytes)} B`}
            </dd>
          </div>
          <div>
            <dt>{t("workspaceSnapshot.expires")}</dt>
            <dd>
              {snapshot.spec.expiresAt === undefined
                ? t("common.never")
                : dateTime(snapshot.spec.expiresAt)}
            </dd>
          </div>
        </dl>
        <p className="cluster-boundary">{t("workspaceSnapshot.cleanupBoundary")}</p>
        <label className="confirmation-check">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
            disabled={disabled}
            data-sheet-autofocus
          />
          <span>{t("workspaceSnapshot.cleanupReview")}</span>
        </label>
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button className="button danger" type="submit" disabled={disabled || !confirmed}>
            {t("workspaceSnapshot.cleanupConfirm")}
          </button>
        </div>
      </form>
    </section>
  );
}
