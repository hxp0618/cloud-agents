import { SheetHeading } from "../AdminSheet";
import { useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import {
  type AdminSandboxSession,
  type RuntimeProfile,
  type WorkspaceSnapshot,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { DurationSelect, NameField } from "./form-fields";
import { phaseLabel, phaseTone, runtimeProfileTargetLabel } from "./presentation";

const snapshotRetentionOptions = Object.freeze([86_400, 604_800, 2_592_000, 7_776_000, 31_536_000]);
const restoreTtlOptions = Object.freeze([600, 1800, 3600, 7200, 21_600, 43_200, 86_400]);

export type WorkspaceSnapshotDraft = Readonly<{
  sourceSandboxId: string;
  retentionSeconds: string;
  token: string;
}>;

export type WorkspaceSnapshotRestoreDraft = Readonly<{
  snapshotId: string;
  workspaceName: string;
  runtimeProfileVersionId: string;
  ttlSeconds: string;
  token: string;
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
  const stoppedSandboxes = sandboxes.filter(
    ({ spec }) => spec.writerReleased && spec.observedState === "stopped",
  );
  const availableSnapshots = snapshots.filter(({ spec }) => spec.status === "available");
  return (
    <section className="panel overview-panel">
      <div className="panel-heading">
        <div>
          <h2>{t("workspaceSnapshot.title")}</h2>
        </div>
      </div>
      {stoppedSandboxes.length === 0 ? (
        <p className="boundary-note">{t("workspaceSnapshot.noStoppedSandbox")}</p>
      ) : (
        <form className="resource-form" onSubmit={onCreate}>
          <div className="form-row">
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
                {stoppedSandboxes.map((sandbox) => (
                  <option key={sandbox.metadata.uid} value={sandbox.metadata.uid}>
                    {sandbox.spec.workspaceName} · {sandbox.metadata.name}
                  </option>
                ))}
              </select>
            </label>
            <DurationSelect
              label={t("workspaceSnapshot.retention")}
              value={snapshotForm.retentionSeconds}
              options={snapshotRetentionOptions}
              onChange={(retentionSeconds) =>
                onSnapshotFormChange({ ...snapshotForm, retentionSeconds })
              }
            />
          </div>
          <button
            className="button primary"
            type="submit"
            disabled={busy || snapshotForm.sourceSandboxId === ""}
          >
            {t("workspaceSnapshot.create")}
          </button>
        </form>
      )}
      {availableSnapshots.length === 0 ? null : (
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
                {availableSnapshots.map((snapshot) => (
                  <option key={snapshot.metadata.uid} value={snapshot.metadata.uid}>
                    {snapshot.metadata.name}
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
            <NameField
              label={t("workspaceSnapshot.restoreWorkspaceName")}
              value={restoreForm.workspaceName}
              placeholder="workspace-restored"
              onChange={(workspaceName) => onRestoreFormChange({ ...restoreForm, workspaceName })}
            />
            <DurationSelect
              label={t("workspaceSnapshot.restoreTtl")}
              value={restoreForm.ttlSeconds}
              options={restoreTtlOptions}
              onChange={(ttlSeconds) => onRestoreFormChange({ ...restoreForm, ttlSeconds })}
            />
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
      )}
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
  const { t, number, bytes, dateTime } = useI18n();
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
                {snapshot.spec.sizeBytes === undefined ? "—" : bytes(snapshot.spec.sizeBytes)}
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
  const { t, bytes, dateTime } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  return (
    <section className="dialog" aria-labelledby="snapshot-cleanup-title">
      <SheetHeading
        id="snapshot-cleanup-title"
        title={t("workspaceSnapshot.cleanupTitle")}
        onClose={onClose}
      />
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
            <dd>{snapshot.spec.sizeBytes === undefined ? "—" : bytes(snapshot.spec.sizeBytes)}</dd>
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
