import { useState } from "react";
import { DetailMore } from "./form-fields";
import { SheetHeading } from "../AdminSheet";
import {
  type DeploymentTarget,
  type EnvironmentLease,
  type EnvironmentLeaseUpgradePreview,
  type WorkerRelease,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { useI18n } from "../i18n";
import { phaseLabel, phaseTone, shortDigest } from "./presentation";

export function LeaseTable({
  leases,
  filtered = false,
  selectedLeaseId,
  onSelect,
}: Readonly<{
  leases: readonly EnvironmentLease[];
  filtered?: boolean;
  selectedLeaseId: string;
  onSelect: (leaseId: string) => void;
}>) {
  const { t, dateTime } = useI18n();
  if (leases.length === 0)
    return (
      <div className="table-empty">
        {t(filtered ? "lease.filter.noMatches" : "table.empty.leases")}
      </div>
    );
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("table.observed")}</th>
            <th>{t("table.cleanup")}</th>
            <th>{t("table.expires")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {leases.map((lease) => (
            <tr
              key={lease.metadata.uid}
              className={lease.metadata.uid === selectedLeaseId ? "selected" : ""}
              onClick={() => onSelect(lease.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(lease.metadata.uid)}>
                  <strong>{lease.metadata.name}</strong>
                  <small>{lease.metadata.uid}</small>
                </button>
              </td>
              <td>
                <span className={`phase ${phaseTone(lease.spec.observedPhase)}`}>
                  <i />
                  {phaseLabel(lease.spec.observedPhase, t)}
                </span>
              </td>
              <td>
                <span className={`phase ${phaseTone(lease.spec.cleanupPhase)}`}>
                  <i />
                  {phaseLabel(lease.spec.cleanupPhase, t)}
                </span>
              </td>
              <td>{dateTime(lease.spec.expiresAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: lease.metadata.name })}
                  onClick={() => onSelect(lease.metadata.uid)}
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

export function LeaseDetail({
  lease,
  target,
  releases,
  upgradeReleaseDigest,
  onUpgradeReleaseDigestChange,
  onPreviewUpgrade,
  onPreviewRollback,
  disabled,
}: Readonly<{
  lease: EnvironmentLease;
  target: DeploymentTarget | undefined;
  releases: readonly WorkerRelease[];
  upgradeReleaseDigest: string;
  onUpgradeReleaseDigestChange: (digest: string) => void;
  onPreviewUpgrade: () => void;
  onPreviewRollback: () => void;
  disabled: boolean;
}>) {
  const { t, number, dateTime } = useI18n();
  const eligibleReleases = releases.filter(
    ({ spec }) => spec.releaseDigest !== lease.spec.releaseDigest,
  );
  const canPreview =
    target?.spec.observedPhase === "ready" &&
    target.spec.schedulingState === "drained" &&
    lease.spec.desiredPhase === "active" &&
    ["ready", "failed"].includes(lease.spec.observedPhase) &&
    lease.spec.cleanupPhase === "none";
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          L
        </div>
        <div>
          <div className="eyebrow">{t("lease.eyebrow")}</div>
          <h2>{lease.metadata.name}</h2>
          <span className={`phase ${phaseTone(lease.spec.observedPhase)}`}>
            <i />
            {phaseLabel(lease.spec.observedPhase, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("lease.target")}</dt>
          <dd className="mono">{lease.spec.targetId ?? t("common.legacyLease")}</dd>
        </div>
        <div>
          <dt>{t("lease.cleanupPhase")}</dt>
          <dd className={lease.spec.cleanupPhase === "blocked" ? "danger-text" : ""}>
            {phaseLabel(lease.spec.cleanupPhase, t)}
          </dd>
        </div>
        <div>
          <dt>{t("lease.cpuMemory")}</dt>
          <dd>
            {lease.spec.cpuLimitMillis === undefined
              ? t("common.notBound")
              : `${number(lease.spec.cpuLimitMillis)} mCPU / ${number(Math.round((lease.spec.memoryLimitBytes ?? 0) / 1_048_576))} MiB`}
          </dd>
        </div>
        <div>
          <dt>{t("lease.providerCredentialRef")}</dt>
          <dd className="mono">{lease.spec.providerCredentialRef ?? t("common.legacyLease")}</dd>
        </div>
        <div>
          <dt>{t("lease.expires")}</dt>
          <dd>{dateTime(lease.spec.expiresAt)}</dd>
        </div>
        <div>
          <dt>{t("lease.updated")}</dt>
          <dd>{dateTime(lease.metadata.updatedAt)}</dd>
        </div>
        {lease.spec.stableErrorCode !== undefined && lease.spec.stableErrorCode !== "" ? (
          <div>
            <dt>{t("detail.stableError")}</dt>
            <dd className="danger-text">{lease.spec.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
      <DetailMore title={t("detail.diagnostics")}>
        <div>
          <dt>{t("lease.id")}</dt>
          <dd className="mono">{lease.metadata.uid}</dd>
        </div>
        <div>
          <dt>{t("lease.environmentId")}</dt>
          <dd className="mono">{lease.spec.environmentId}</dd>
        </div>
        <div>
          <dt>{t("lease.desiredPhase")}</dt>
          <dd>{phaseLabel(lease.spec.desiredPhase, t)}</dd>
        </div>
        <div>
          <dt>{t("table.generation")}</dt>
          <dd className="mono">{number(lease.spec.generation)}</dd>
        </div>
        <div>
          <dt>{t("detail.resourceVersion")}</dt>
          <dd className="mono">{lease.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>{t("lease.releaseDigest")}</dt>
          <dd className="mono break">{lease.spec.releaseDigest}</dd>
        </div>
        <div>
          <dt>{t("lease.workerEndpoint")}</dt>
          <dd className="mono break">{lease.spec.workerEndpoint ?? t("common.notReady")}</dd>
        </div>
      </DetailMore>
      <section className="action-block">
        <div>
          <h3>{t("lease.releaseLifecycle")}</h3>
          <p>
            {canPreview
              ? t("lease.releaseReady")
              : t("lease.releaseRequiresDrain", {
                  observed: phaseLabel(target?.spec.observedPhase ?? "unprobed", t),
                  scheduling: phaseLabel(target?.spec.schedulingState ?? "active", t),
                })}
          </p>
        </div>
        <label>
          <span>{t("lease.upgradeRelease")}</span>
          <select
            value={upgradeReleaseDigest}
            onChange={(event) => onUpgradeReleaseDigestChange(event.target.value)}
            disabled={disabled || !canPreview || eligibleReleases.length === 0}
          >
            {eligibleReleases.length === 0 ? (
              <option value="">{t("lease.noUpgradeRelease")}</option>
            ) : (
              eligibleReleases.map((release) => (
                <option key={release.metadata.uid} value={release.spec.releaseDigest}>
                  {release.metadata.name} · {shortDigest(release.spec.releaseDigest)}
                </option>
              ))
            )}
          </select>
          <small>{t("lease.releaseAuthority")}</small>
        </label>
        <div className="heading-actions">
          <button
            className="button ghost"
            type="button"
            onClick={onPreviewRollback}
            disabled={disabled || !canPreview}
          >
            {t("lease.previewRollback")}
          </button>
          <button
            className="button primary"
            type="button"
            onClick={onPreviewUpgrade}
            disabled={disabled || !canPreview || upgradeReleaseDigest === ""}
          >
            {t("lease.previewUpgrade")}
          </button>
        </div>
      </section>
    </>
  );
}

export function LeaseReleaseConfirmation({
  lease,
  preview,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  lease: EnvironmentLease;
  preview: EnvironmentLeaseUpgradePreview;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t, number } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  const actionLabel = t(
    preview.spec.action === "upgrade" ? "lease.actionUpgrade" : "lease.actionRollback",
  );
  return (
    <section className="dialog" aria-labelledby="lease-release-title">
      <SheetHeading
        id="lease-release-title"
        title={t("lease.releaseConfirmTitle", { action: actionLabel })}
        subject={lease.metadata.name}
        onClose={onClose}
      />
      <form
        className="resource-form"
        onSubmit={(event) => {
          event.preventDefault();
          onConfirm();
        }}
      >
        <div className="banner danger" role="status">
          {t("lease.releaseImpact", {
            action: actionLabel,
            workers: number(preview.spec.affectedWorkers),
            leases: number(preview.spec.affectedLeases),
            targets: number(preview.spec.affectedTargets),
          })}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("lease.id")}</dt>
            <dd className="mono">{lease.metadata.uid}</dd>
          </div>
          <div>
            <dt>{t("lease.target")}</dt>
            <dd className="mono">{preview.spec.targetId}</dd>
          </div>
          <div>
            <dt>{t("lease.currentRelease")}</dt>
            <dd className="mono break">{preview.spec.currentReleaseDigest}</dd>
          </div>
          <div>
            <dt>{t("lease.targetRelease")}</dt>
            <dd className="mono break">{preview.spec.targetReleaseDigest}</dd>
          </div>
          <div>
            <dt>{t("lease.rollbackRelease")}</dt>
            <dd className="mono break">
              {preview.spec.rollbackReleaseDigest} · g{number(preview.spec.rollbackGeneration)}
            </dd>
          </div>
          <div>
            <dt>{t("lease.impactDigest")}</dt>
            <dd className="mono break">{preview.spec.impactDigest}</dd>
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
          <span>{t("lease.releaseReview", { action: actionLabel })}</span>
        </label>
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button className="button danger" type="submit" disabled={disabled || !confirmed}>
            {t("lease.releaseConfirm", { action: actionLabel })}
          </button>
        </div>
      </form>
    </section>
  );
}
