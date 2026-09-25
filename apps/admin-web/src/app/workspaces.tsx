import { useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import {
  type AdminSandboxSession,
  type RuntimeProfile,
  type WorkspaceSnapshot,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { targetIdentifierPattern } from "../admin";
import { phaseLabel, phaseTone, runtimeProfileTargetLabel } from "./presentation";

export type WorkspaceSnapshotDraft = Readonly<{
  snapshotId: string;
  sourceSandboxId: string;
  retentionSeconds: string;
}>;

export type WorkspaceSnapshotRestoreDraft = Readonly<{
  snapshotId: string;
  workspaceId: string;
  workspaceName: string;
  sandboxId: string;
  runtimeProfileVersionId: string;
  ttlSeconds: string;
}>;

export function WorkspaceSnapshotPanel({
  snapshots,
  sandboxes,
  restoreRuntimeProfiles,
  selectedRestoreSnapshot,
  snapshotForm,
  restoreForm,
  busy,
  onCreate,
  onRestore,
  onSnapshotFormChange,
  onRestoreFormChange,
  onCleanup,
}: Readonly<{
  snapshots: readonly WorkspaceSnapshot[];
  sandboxes: readonly AdminSandboxSession[];
  restoreRuntimeProfiles: readonly RuntimeProfile[];
  selectedRestoreSnapshot: WorkspaceSnapshot | undefined;
  snapshotForm: WorkspaceSnapshotDraft;
  restoreForm: WorkspaceSnapshotRestoreDraft;
  busy: boolean;
  onCreate: (event: FormEvent<HTMLFormElement>) => void;
  onRestore: (event: FormEvent<HTMLFormElement>) => void;
  onSnapshotFormChange: (draft: WorkspaceSnapshotDraft) => void;
  onRestoreFormChange: (draft: WorkspaceSnapshotRestoreDraft) => void;
  onCleanup: (snapshot: WorkspaceSnapshot) => void;
}>) {
  const { t, number } = useI18n();
  return (
    <section className="panel overview-panel">
      <div className="panel-heading">
        <div>
          <h2>{t("workspaceSnapshot.title")}</h2>
          <p>{t("workspaceSnapshot.description")}</p>
        </div>
        <span className="scope-chip">
          snapshots.list · snapshots.create · snapshots.act · snapshots.delete
        </span>
      </div>
      <form className="resource-form" onSubmit={onCreate}>
        <div className="form-row">
          <label>
            <span>{t("workspaceSnapshot.id")}</span>
            <input
              required
              maxLength={128}
              spellCheck={false}
              value={snapshotForm.snapshotId}
              onChange={(event) =>
                onSnapshotFormChange({
                  ...snapshotForm,
                  snapshotId: event.target.value,
                })
              }
              placeholder="snapshot-before-upgrade"
            />
          </label>
          <label>
            <span>{t("workspaceSnapshot.source")}</span>
            <select
              required
              value={snapshotForm.sourceSandboxId}
              onChange={(event) =>
                onSnapshotFormChange({
                  ...snapshotForm,
                  sourceSandboxId: event.target.value,
                })
              }
            >
              <option value="">{t("workspaceSnapshot.selectSource")}</option>
              {sandboxes
                .filter(({ spec }) => spec.writerReleased && spec.observedState === "stopped")
                .map((sandbox) => (
                  <option key={sandbox.metadata.uid} value={sandbox.metadata.uid}>
                    {sandbox.spec.workspaceName} · {sandbox.metadata.uid} · g
                    {number(sandbox.spec.generation)}
                  </option>
                ))}
            </select>
          </label>
          <label>
            <span>{t("workspaceSnapshot.retention")}</span>
            <input
              required
              type="number"
              min={1}
              max={31_536_000}
              value={snapshotForm.retentionSeconds}
              onChange={(event) =>
                onSnapshotFormChange({
                  ...snapshotForm,
                  retentionSeconds: event.target.value,
                })
              }
            />
          </label>
        </div>
        <p className="cluster-boundary">{t("workspaceSnapshot.offlineBoundary")}</p>
        <button
          className="button primary"
          type="submit"
          disabled={busy || snapshotForm.sourceSandboxId === ""}
        >
          {t("workspaceSnapshot.create")}
        </button>
      </form>
      <form className="resource-form" onSubmit={onRestore}>
        <div className="form-row">
          <label>
            <span>{t("workspaceSnapshot.restoreSource")}</span>
            <select
              required
              value={restoreForm.snapshotId}
              onChange={(event) =>
                onRestoreFormChange({
                  ...restoreForm,
                  snapshotId: event.target.value,
                  runtimeProfileVersionId: "",
                })
              }
            >
              <option value="">{t("workspaceSnapshot.selectRestoreSource")}</option>
              {snapshots
                .filter(({ spec }) => spec.status === "available")
                .map((snapshot) => (
                  <option key={snapshot.metadata.uid} value={snapshot.metadata.uid}>
                    {snapshot.metadata.name} · {snapshot.spec.backend} ·{" "}
                    {snapshot.spec.sourceTargetId}
                  </option>
                ))}
            </select>
          </label>
          <label>
            <span>{t("workspaceSnapshot.restoreProfile")}</span>
            <select
              required
              value={restoreForm.runtimeProfileVersionId}
              onChange={(event) =>
                onRestoreFormChange({
                  ...restoreForm,
                  runtimeProfileVersionId: event.target.value,
                })
              }
            >
              <option value="">{t("workspaceSnapshot.selectRestoreProfile")}</option>
              {restoreRuntimeProfiles.map((profile) => (
                <option key={profile.metadata.uid} value={profile.metadata.uid}>
                  {profile.metadata.name} · v{number(profile.spec.version)} ·{" "}
                  {runtimeProfileTargetLabel(profile)}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="form-row">
          {(
            [
              ["workspaceId", "workspaceSnapshot.restoreWorkspaceId", "workspace-restored"],
              ["workspaceName", "workspaceSnapshot.restoreWorkspaceName", "Restored workspace"],
              ["sandboxId", "workspaceSnapshot.restoreSandboxId", "sandbox-restored"],
            ] as const
          ).map(([field, label, placeholder]) => (
            <label key={field}>
              <span>{t(label)}</span>
              <input
                required
                maxLength={128}
                pattern={targetIdentifierPattern}
                spellCheck={false}
                value={restoreForm[field]}
                placeholder={placeholder}
                onChange={(event) =>
                  onRestoreFormChange({
                    ...restoreForm,
                    [field]: event.target.value,
                  })
                }
              />
            </label>
          ))}
          <label>
            <span>{t("workspaceSnapshot.restoreTtl")}</span>
            <input
              required
              type="number"
              min={60}
              max={86_400}
              value={restoreForm.ttlSeconds}
              onChange={(event) =>
                onRestoreFormChange({
                  ...restoreForm,
                  ttlSeconds: event.target.value,
                })
              }
            />
          </label>
        </div>
        <p className="cluster-boundary">{t("workspaceSnapshot.restoreBoundary")}</p>
        <button
          className="button primary"
          type="submit"
          disabled={
            busy ||
            selectedRestoreSnapshot?.spec.status !== "available" ||
            restoreForm.runtimeProfileVersionId === ""
          }
        >
          {t("workspaceSnapshot.restore")}
        </button>
      </form>
      <WorkspaceSnapshotTable snapshots={snapshots} onCleanup={onCleanup} />
    </section>
  );
}

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
