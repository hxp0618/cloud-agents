import { useI18n } from "../i18n";
import type { FormEvent, ReactNode } from "react";
import { AdminSheet } from "../AdminSheet";
import { shortDigest } from "./presentation";
import {
  type StoragePolicy,
  type WorkerRelease,
  type WorkerReleaseRegisterRequest,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

export function StoragePolicyTable({
  policies,
  selectedPolicyId,
  onSelect,
}: Readonly<{
  policies: readonly StoragePolicy[];
  selectedPolicyId: string;
  onSelect: (policyId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (policies.length === 0)
    return <div className="table-empty">{t("table.empty.storagePolicies")}</div>;
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("storagePolicy.userSummary")}</th>
            <th>{t("table.capacity")}</th>
            <th>{t("storagePolicy.lifecycle")}</th>
            <th>{t("table.version")}</th>
            <th>{t("table.updated")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {policies.map((policy) => (
            <tr
              key={policy.metadata.uid}
              className={policy.metadata.uid === selectedPolicyId ? "selected" : ""}
              onClick={() => onSelect(policy.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(policy.metadata.uid)}>
                  <strong>{policy.metadata.name}</strong>
                  <small>{policy.metadata.uid}</small>
                </button>
              </td>
              <td>{policy.spec.userSummary}</td>
              <td>{number(policy.spec.workspaceCapacityBytes / 1_073_741_824)} GiB</td>
              <td>{t("storagePolicy.lifecycleImmediate")}</td>
              <td className="mono">rv{policy.metadata.resourceVersion}</td>
              <td>{dateTime(policy.metadata.updatedAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: policy.metadata.name })}
                  onClick={() => onSelect(policy.metadata.uid)}
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

export type WorkerReleaseDraft = {
  releaseId: string;
  releaseName: string;
  imageRepository: string;
  releaseDigest: string;
  platformVersion: string;
  runtimeVersion: string;
  codexVersion: string;
  claudeCodeVersion: string;
  amd64: boolean;
  arm64: boolean;
  verificationEvidenceDigest: string;
};

export function workerReleaseForm(): WorkerReleaseDraft {
  return {
    releaseId: "",
    releaseName: "",
    imageRepository: "",
    releaseDigest: "",
    platformVersion: "",
    runtimeVersion: "",
    codexVersion: "",
    claudeCodeVersion: "",
    amd64: true,
    arm64: false,
    verificationEvidenceDigest: "",
  };
}

export function workerReleaseRegisterRequestFrom(
  draft: WorkerReleaseDraft,
): WorkerReleaseRegisterRequest {
  const architectures: ("linux/amd64" | "linux/arm64")[] = [];
  if (draft.amd64) architectures.push("linux/amd64");
  if (draft.arm64) architectures.push("linux/arm64");
  return {
    releaseId: draft.releaseId.trim(),
    releaseName: draft.releaseName.trim(),
    imageRepository: draft.imageRepository.trim(),
    releaseDigest: draft.releaseDigest.trim() as `sha256:${string}`,
    platformVersion: draft.platformVersion.trim(),
    runtimeVersion: draft.runtimeVersion.trim(),
    codexVersion: draft.codexVersion.trim(),
    claudeCodeVersion: draft.claudeCodeVersion.trim(),
    architectures,
    verificationEvidenceDigest: draft.verificationEvidenceDigest.trim() as `sha256:${string}`,
  };
}

export function ReleaseRegistrationForm({
  draft,
  feedback,
  disabled,
  onDraftChange,
  onClose,
  onSubmit,
}: Readonly<{
  draft: WorkerReleaseDraft;
  feedback: ReactNode;
  disabled: boolean;
  onDraftChange: (draft: WorkerReleaseDraft) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}>) {
  const { t } = useI18n();
  return (
    <AdminSheet label={t("release.register.title")} feedback={feedback} onClose={onClose}>
      <section className="dialog" aria-labelledby="register-release-title">
        <div className="panel-heading">
          <div>
            <div className="eyebrow">{t("release.register.eyebrow")}</div>
            <h2 id="register-release-title">{t("release.register.title")}</h2>
            <p>{t("release.register.description")}</p>
          </div>
          <button
            className="icon-button"
            type="button"
            aria-label={t("action.close")}
            onClick={() => onClose()}
          >
            ×
          </button>
        </div>
        <form className="resource-form" onSubmit={onSubmit}>
          <div className="form-row">
            <label>
              <span>{t("release.id")}</span>
              <input
                value={draft.releaseId}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    releaseId: event.target.value,
                  })
                }
                placeholder="worker-v1"
                maxLength={128}
                required
                autoFocus
                data-sheet-autofocus
                spellCheck={false}
              />
            </label>
            <label>
              <span>{t("release.name")}</span>
              <input
                value={draft.releaseName}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    releaseName: event.target.value,
                  })
                }
                placeholder="worker-v1"
                maxLength={128}
                required
                spellCheck={false}
              />
            </label>
          </div>
          <label>
            <span>{t("release.imageRepository")}</span>
            <input
              value={draft.imageRepository}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  imageRepository: event.target.value,
                })
              }
              placeholder="registry.example.test/cloud-agents/worker"
              maxLength={512}
              required
              spellCheck={false}
            />
            <small>{t("release.imageRepositoryHelp")}</small>
          </label>
          <label>
            <span>{t("release.digest")}</span>
            <input
              value={draft.releaseDigest}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  releaseDigest: event.target.value,
                })
              }
              placeholder={`sha256:${"a".repeat(64)}`}
              minLength={71}
              maxLength={71}
              required
              spellCheck={false}
            />
          </label>
          <div className="form-row">
            <label>
              <span>{t("release.platformVersion")}</span>
              <input
                value={draft.platformVersion}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    platformVersion: event.target.value,
                  })
                }
                placeholder="platform-v1"
                maxLength={128}
                required
                spellCheck={false}
              />
            </label>
            <label>
              <span>{t("release.runtimeVersion")}</span>
              <input
                value={draft.runtimeVersion}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    runtimeVersion: event.target.value,
                  })
                }
                placeholder="runtime-v1"
                maxLength={128}
                required
                spellCheck={false}
              />
            </label>
          </div>
          <div className="form-row">
            <label>
              <span>{t("release.codexVersion")}</span>
              <input
                value={draft.codexVersion}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    codexVersion: event.target.value,
                  })
                }
                placeholder="codex-v1"
                maxLength={128}
                required
                spellCheck={false}
              />
            </label>
            <label>
              <span>{t("release.claudeCodeVersion")}</span>
              <input
                value={draft.claudeCodeVersion}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    claudeCodeVersion: event.target.value,
                  })
                }
                placeholder="claude-v1"
                maxLength={128}
                required
                spellCheck={false}
              />
            </label>
          </div>
          <fieldset className="provider-options">
            <legend>{t("release.architectures")}</legend>
            <label className="confirmation-check">
              <input
                type="checkbox"
                checked={draft.amd64}
                required={!draft.arm64}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    amd64: event.target.checked,
                  })
                }
              />
              <span>linux/amd64</span>
            </label>
            <label className="confirmation-check">
              <input
                type="checkbox"
                checked={draft.arm64}
                required={!draft.amd64}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    arm64: event.target.checked,
                  })
                }
              />
              <span>linux/arm64</span>
            </label>
          </fieldset>
          <label>
            <span>{t("release.evidenceDigest")}</span>
            <input
              value={draft.verificationEvidenceDigest}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  verificationEvidenceDigest: event.target.value,
                })
              }
              placeholder={`sha256:${"b".repeat(64)}`}
              minLength={71}
              maxLength={71}
              required
              spellCheck={false}
            />
            <small>{t("release.evidenceDigestHelp")}</small>
          </label>
          <div className="dialog-actions">
            <button className="button ghost" type="button" onClick={() => onClose()}>
              {t("action.cancel")}
            </button>
            <button className="button primary" type="submit" disabled={disabled}>
              {t("action.registerRelease")}
            </button>
          </div>
        </form>
      </section>
    </AdminSheet>
  );
}

export function ReleaseTable({ releases }: Readonly<{ releases: readonly WorkerRelease[] }>) {
  const { t, dateTime } = useI18n();
  if (releases.length === 0) return <div className="table-empty">{t("table.empty.releases")}</div>;
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("table.imageRepository")}</th>
            <th>{t("table.release")}</th>
            <th>{t("table.platform")}</th>
            <th>{t("table.runtime")}</th>
            <th>{t("table.providers")}</th>
            <th>{t("table.architectures")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.approved")}</th>
          </tr>
        </thead>
        <tbody>
          {releases.map((release) => (
            <tr key={release.metadata.uid}>
              <td>
                <strong>{release.metadata.name}</strong>
                <small className="table-subline">{release.metadata.uid}</small>
              </td>
              <td className="mono">{release.spec.imageRepository}</td>
              <td className="mono" title={release.spec.releaseDigest}>
                {shortDigest(release.spec.releaseDigest)}
              </td>
              <td>{release.spec.platformVersion}</td>
              <td>{release.spec.runtimeVersion}</td>
              <td>
                Codex {release.spec.codexVersion}
                <small className="table-subline">Claude {release.spec.claudeCodeVersion}</small>
              </td>
              <td>{release.spec.architectures.join(" · ")}</td>
              <td>
                <span className="phase success">
                  <i /> {t("release.approvedAttested")}
                </span>
              </td>
              <td>{dateTime(release.spec.approvedAt)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
