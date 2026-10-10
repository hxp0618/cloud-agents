import { SheetHeading } from "../AdminSheet";
import { useState } from "react";
import {
  type AdminAuditEvent,
  type EnvironmentProfile,
  type RuntimeProfile,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { useI18n } from "../i18n";
import {
  auditLabel,
  phaseLabel,
  phaseTone,
  providerLabel,
  runtimeProfileTargetLabel,
} from "./presentation";

export type ProfileTransition = "publish" | "disable";

export function ProfileTable({
  profiles,
  selectedProfileVersionId,
  onSelect,
}: Readonly<{
  profiles: readonly EnvironmentProfile[];
  selectedProfileVersionId: string;
  onSelect: (profileVersionId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (profiles.length === 0) return <div className="table-empty">{t("table.empty.profiles")}</div>;
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("table.version")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.providers")}</th>
            <th>{t("table.capacity")}</th>
            <th>{t("table.updated")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {profiles.map((profile) => (
            <tr
              key={profile.metadata.uid}
              className={profile.metadata.uid === selectedProfileVersionId ? "selected" : ""}
              onClick={() => onSelect(profile.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(profile.metadata.uid)}>
                  <strong>{profile.metadata.name}</strong>
                  <small>{profile.spec.profileId}</small>
                </button>
              </td>
              <td className="mono">v{number(profile.spec.version)}</td>
              <td>
                <span className={`phase ${phaseTone(profile.spec.status)}`}>
                  <i /> {phaseLabel(profile.spec.status, t)}
                </span>
              </td>
              <td>{profile.spec.providerKinds.join(" · ")}</td>
              <td>
                {number(profile.spec.cpuLimitMillis)} mCPU ·{" "}
                {number(Math.round(profile.spec.memoryLimitBytes / 1_048_576))} MiB
              </td>
              <td>{dateTime(profile.metadata.updatedAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", {
                    name: `${profile.metadata.name} v${number(profile.spec.version)}`,
                  })}
                  onClick={() => onSelect(profile.metadata.uid)}
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

export function RuntimeProfileTable({
  profiles,
  selectedProfileVersionId,
  onSelect,
}: Readonly<{
  profiles: readonly RuntimeProfile[];
  selectedProfileVersionId: string;
  onSelect: (profileVersionId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (profiles.length === 0)
    return <div className="table-empty">{t("table.empty.runtimeProfiles")}</div>;
  return (
    <div
      className="table-scroll"
      tabIndex={0}
      role="region"
      aria-label={t("page.runtimeProfiles.title")}
    >
      <table>
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("table.version")}</th>
            <th>{t("table.status")}</th>
            <th>{t("runtimeProfile.isolation")}</th>
            <th>{t("table.target")}</th>
            <th>{t("table.capacity")}</th>
            <th>{t("table.updated")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {profiles.map((profile) => (
            <tr
              key={profile.metadata.uid}
              className={profile.metadata.uid === selectedProfileVersionId ? "selected" : ""}
              onClick={() => onSelect(profile.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(profile.metadata.uid)}>
                  <strong>{profile.metadata.name}</strong>
                  <small>{profile.spec.profileId}</small>
                </button>
              </td>
              <td className="mono">v{number(profile.spec.version)}</td>
              <td>
                <span className={`phase ${phaseTone(profile.spec.status)}`}>
                  <i /> {phaseLabel(profile.spec.status, t)}
                </span>
              </td>
              <td>
                {t(`runtimeProfile.workloadTrust.${profile.spec.workloadTrust}`)} ·{" "}
                {t(`runtimeProfile.isolationRuntime.${profile.spec.isolationRuntime}`)}
              </td>
              <td className="mono">{runtimeProfileTargetLabel(profile)}</td>
              <td>
                {number(profile.spec.cpuMillis)} mCPU ·{" "}
                {number(Math.round(profile.spec.memoryBytes / 1_048_576))} MiB
              </td>
              <td>{dateTime(profile.metadata.updatedAt ?? profile.metadata.createdAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: profile.metadata.name })}
                  onClick={() => onSelect(profile.metadata.uid)}
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

export function RuntimeProfileDetail({
  profile,
  disabled,
  onTransition,
}: Readonly<{
  profile: RuntimeProfile;
  disabled: boolean;
  onTransition: (action: ProfileTransition) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  return (
    <>
      <div className="detail-heading">
        <div>
          <div className="eyebrow">
            {t("runtimeProfile.eyebrow", { version: number(profile.spec.version) })}
          </div>
          <h2>{profile.metadata.name}</h2>
          <span className={`phase ${phaseTone(profile.spec.status)}`}>
            <i /> {phaseLabel(profile.spec.status, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("runtimeProfile.id")}</dt>
          <dd className="mono">{profile.spec.profileId}</dd>
        </div>
        <div>
          <dt>{t("profile.version")}</dt>
          <dd className="mono">v{number(profile.spec.version)}</dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.description")}</dt>
          <dd>{profile.spec.description}</dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.workloadTrust")}</dt>
          <dd>{t(`runtimeProfile.workloadTrust.${profile.spec.workloadTrust}`)}</dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.isolationRuntime")}</dt>
          <dd>{t(`runtimeProfile.isolationRuntime.${profile.spec.isolationRuntime}`)}</dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.target")}</dt>
          <dd className="mono">{runtimeProfileTargetLabel(profile)}</dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.networkPolicy")}</dt>
          <dd className="mono">{profile.spec.networkPolicyRef ?? t("common.notBound")}</dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.image")}</dt>
          <dd className="mono break">{profile.spec.imageUri}</dd>
        </div>
        <div>
          <dt>{t("runtimeProfile.releaseDigest")}</dt>
          <dd className="mono break">{profile.spec.releaseDigest}</dd>
        </div>
        <div>
          <dt>{t("profile.cpuMemory")}</dt>
          <dd>
            {number(profile.spec.cpuMillis)} mCPU /{" "}
            {number(Math.round(profile.spec.memoryBytes / 1_048_576))} MiB
          </dd>
        </div>
        <div>
          <dt>{t("detail.resourceVersion")}</dt>
          <dd className="mono">{profile.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>{t("profile.created")}</dt>
          <dd>{dateTime(profile.metadata.createdAt)}</dd>
        </div>
        <div>
          <dt>{t("profile.published")}</dt>
          <dd>
            {profile.spec.publishedAt === undefined
              ? t("common.never")
              : dateTime(profile.spec.publishedAt)}
          </dd>
        </div>
        <div>
          <dt>{t("profile.disabled")}</dt>
          <dd>
            {profile.spec.disabledAt === undefined
              ? t("common.never")
              : dateTime(profile.spec.disabledAt)}
          </dd>
        </div>
      </dl>
      {profile.spec.isolationRuntime === "gvisor" ? (
        <p className="boundary-note">{t("runtimeProfile.gvisorLimitation")}</p>
      ) : null}
      {profile.spec.status === "draft" ? (
        <section className="action-block">
          <h3>{t("profile.publishTitle")}</h3>
          <p>{t("profile.publishDescription")}</p>
          <button
            className="button primary"
            type="button"
            disabled={disabled}
            onClick={() => onTransition("publish")}
          >
            {t("profile.publishVersion")}
          </button>
        </section>
      ) : profile.spec.status === "published" ? (
        <section className="action-block">
          <h3>{t("profile.disableTitle")}</h3>
          <p>{t("profile.disableDescription")}</p>
          <button
            className="button danger"
            type="button"
            disabled={disabled}
            onClick={() => onTransition("disable")}
          >
            {t("profile.disableVersion")}
          </button>
        </section>
      ) : null}
      <p className="boundary-note">{t("runtimeProfile.boundary")}</p>
    </>
  );
}

export function ProfileTransitionConfirmation({
  profile,
  action,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  profile: EnvironmentProfile | RuntimeProfile;
  action: ProfileTransition;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t, number } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  const publishing = action === "publish";
  const actionLabel = t(publishing ? "profile.transition.publish" : "profile.transition.disable");
  return (
    <section className="dialog" aria-labelledby="profile-transition-title">
      <SheetHeading id="profile-transition-title" title={t("profile.transition.title", { action: actionLabel })} subject={<>{profile.metadata.name} · v{number(profile.spec.version)}</>} onClose={onClose} />
      <form
        className="resource-form"
        onSubmit={(event) => {
          event.preventDefault();
          onConfirm();
        }}
      >
        <div className={`banner ${publishing ? "running" : "danger"}`} role="status">
          {publishing
            ? t("profile.transition.publishImpact")
            : t("profile.transition.disableImpact")}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("profile.id")}</dt>
            <dd className="mono">{profile.spec.profileId}</dd>
          </div>
          <div>
            <dt>{t("profile.version")}</dt>
            <dd className="mono">{number(profile.spec.version)}</dd>
          </div>
          <div>
            <dt>{t("profile.expectedResourceVersion")}</dt>
            <dd className="mono">{profile.metadata.resourceVersion}</dd>
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
          <span>{t("profile.transition.review")}</span>
        </label>
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button
            className={`button ${publishing ? "primary" : "danger"}`}
            type="submit"
            disabled={disabled || !confirmed}
          >
            {t("profile.transition.confirm", { action: actionLabel })}
          </button>
        </div>
      </form>
    </section>
  );
}

export function ProfileDetail({
  profile,
  audit,
  disabled,
  onTransition,
}: Readonly<{
  profile: EnvironmentProfile;
  audit: readonly AdminAuditEvent[];
  disabled: boolean;
  onTransition: (action: ProfileTransition) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          P
        </div>
        <div>
          <div className="eyebrow">
            {t("profile.eyebrow", { version: number(profile.spec.version) })}
          </div>
          <h2>{profile.metadata.name}</h2>
          <span className={`phase ${phaseTone(profile.spec.status)}`}>
            <i /> {phaseLabel(profile.spec.status, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("profile.id")}</dt>
          <dd className="mono">{profile.spec.profileId}</dd>
        </div>
        <div>
          <dt>{t("profile.versionResource")}</dt>
          <dd className="mono break">{profile.metadata.uid}</dd>
        </div>
        <div>
          <dt>{t("profile.description")}</dt>
          <dd>{profile.spec.description}</dd>
        </div>
        <div>
          <dt>{t("profile.providers")}</dt>
          <dd>{profile.spec.providerKinds.map(providerLabel).join(" · ")}</dd>
        </div>
        <div>
          <dt>{t("profile.cpuMemory")}</dt>
          <dd>
            {number(profile.spec.cpuLimitMillis)} mCPU /{" "}
            {number(Math.round(profile.spec.memoryLimitBytes / 1_048_576))} MiB
          </dd>
        </div>
        <div>
          <dt>{t("profile.storagePolicy")}</dt>
          <dd className="mono">{profile.spec.storagePolicyRef}</dd>
        </div>
        <div>
          <dt>{t("profile.networkPolicy")}</dt>
          <dd className="mono">{profile.spec.networkPolicyRef}</dd>
        </div>
        <div>
          <dt>{t("profile.releaseDigest")}</dt>
          <dd className="mono break">{profile.spec.releaseDigest}</dd>
        </div>
        <div>
          <dt>{t("profile.targetRefs")}</dt>
          <dd className="mono break">{profile.spec.targetRefs.join(", ")}</dd>
        </div>
        <div>
          <dt>{t("profile.providerCredentialRef")}</dt>
          <dd className="mono break">{profile.spec.providerCredentialRef}</dd>
        </div>
        <div>
          <dt>{t("detail.resourceVersion")}</dt>
          <dd className="mono">{profile.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>{t("profile.created")}</dt>
          <dd>{dateTime(profile.metadata.createdAt)}</dd>
        </div>
        <div>
          <dt>{t("profile.published")}</dt>
          <dd>{dateTime(profile.spec.publishedAt)}</dd>
        </div>
        <div>
          <dt>{t("profile.disabled")}</dt>
          <dd>{dateTime(profile.spec.disabledAt)}</dd>
        </div>
      </dl>
      {profile.spec.status !== "disabled" ? (
        <section className="action-block">
          <div>
            <h3>
              {t(profile.spec.status === "draft" ? "profile.publishTitle" : "profile.disableTitle")}
            </h3>
            <p>
              {t(
                profile.spec.status === "draft"
                  ? "profile.publishDescription"
                  : "profile.disableDescription",
              )}
            </p>
          </div>
          <button
            className={`button ${profile.spec.status === "draft" ? "primary" : "danger"}`}
            type="button"
            onClick={() => onTransition(profile.spec.status === "draft" ? "publish" : "disable")}
            disabled={disabled}
          >
            {t(
              profile.spec.status === "draft" ? "profile.publishVersion" : "profile.disableVersion",
            )}
          </button>
        </section>
      ) : null}
      <section className="activity-block" aria-labelledby="profile-audit-title">
        <div className="activity-heading">
          <h3 id="profile-audit-title">{t("detail.audit")}</h3>
        </div>
        {audit.length === 0 ? (
          <p className="activity-empty">{t("profile.noAudit")}</p>
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
    </>
  );
}
