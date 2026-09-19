import { type FormEvent, type ReactNode } from "react";
import {
  type DeploymentTarget,
  type EnvironmentProfileCreateRequest,
  type NetworkPolicy,
  type RuntimeProfile,
  type RuntimeProfileCreateRequest,
  type StoragePolicy,
  type WorkerRelease,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { AdminSheet } from "../AdminSheet";
import { targetIdentifierPattern } from "../admin";
import { useI18n } from "../i18n";
import {
  executableFoundationNetworkPolicy,
  phaseLabel,
  shortDigest,
  type WorkloadTrust,
} from "./presentation";

export type EnvironmentProfileDraft = {
  profileId: string;
  profileName: string;
  version: string;
  description: string;
  codex: boolean;
  claudeAgent: boolean;
  cpuLimitMillis: string;
  memoryLimitMiB: string;
  storagePolicyRef: string;
  networkPolicyRef: string;
  releaseDigest: string;
  targetRefs: string;
  providerCredentialRef: string;
};

export function environmentProfileForm(): EnvironmentProfileDraft {
  return {
    profileId: "",
    profileName: "",
    version: "1",
    description: "",
    codex: true,
    claudeAgent: true,
    cpuLimitMillis: "2000",
    memoryLimitMiB: "4096",
    storagePolicyRef: "",
    networkPolicyRef: "",
    releaseDigest: "",
    targetRefs: "",
    providerCredentialRef: "",
  };
}

export function environmentProfileCreateRequestFrom(
  draft: EnvironmentProfileDraft,
): EnvironmentProfileCreateRequest {
  const providerKinds: ("codex" | "claudeAgent")[] = [];
  if (draft.codex) providerKinds.push("codex");
  if (draft.claudeAgent) providerKinds.push("claudeAgent");
  return {
    profileId: draft.profileId.trim(),
    profileName: draft.profileName.trim(),
    version: Number(draft.version),
    description: draft.description.trim(),
    providerKinds,
    cpuLimitMillis: Number(draft.cpuLimitMillis),
    memoryLimitBytes: Number(draft.memoryLimitMiB) * 1_048_576,
    storagePolicyRef: draft.storagePolicyRef.trim(),
    networkPolicyRef: draft.networkPolicyRef.trim(),
    releaseDigest: draft.releaseDigest.trim() as `sha256:${string}`,
    targetRefs: [...new Set(draft.targetRefs.split(",").map((value) => value.trim()))].filter(
      Boolean,
    ),
    providerCredentialRef: draft.providerCredentialRef.trim(),
  };
}

export function EnvironmentProfileCreateForm({
  draft,
  storagePolicies,
  networkPolicies,
  releases,
  feedback,
  disabled,
  onDraftChange,
  onClose,
  onSubmit,
}: Readonly<{
  draft: EnvironmentProfileDraft;
  storagePolicies: readonly StoragePolicy[];
  networkPolicies: readonly NetworkPolicy[];
  releases: readonly WorkerRelease[];
  feedback: ReactNode;
  disabled: boolean;
  onDraftChange: (draft: EnvironmentProfileDraft) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}>) {
  const { t } = useI18n();
  return (
    <AdminSheet feedback={feedback} label={t("profile.create.title")} onClose={onClose}>
      <section className="dialog" aria-labelledby="create-profile-title">
        <div className="panel-heading">
          <div>
            <div className="eyebrow">{t("profile.create.eyebrow")}</div>
            <h2 id="create-profile-title">{t("profile.create.title")}</h2>
            <p>{t("profile.create.description")}</p>
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
              <span>{t("profile.id")}</span>
              <input
                value={draft.profileId}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    profileId: event.target.value,
                  })
                }
                placeholder="development"
                maxLength={128}
                required
                autoFocus
                data-sheet-autofocus
                spellCheck={false}
              />
            </label>
            <label>
              <span>{t("profile.name")}</span>
              <input
                value={draft.profileName}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    profileName: event.target.value,
                  })
                }
                placeholder="development"
                maxLength={128}
                required
                spellCheck={false}
              />
            </label>
          </div>
          <label>
            <span>{t("profile.version")}</span>
            <input
              type="number"
              min="1"
              max="2147483647"
              step="1"
              value={draft.version}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  version: event.target.value,
                })
              }
              required
            />
            <small>{t("profile.versionHelp")}</small>
          </label>
          <label>
            <span>{t("profile.description")}</span>
            <input
              value={draft.description}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  description: event.target.value,
                })
              }
              placeholder={t("profile.descriptionPlaceholder")}
              maxLength={1024}
              required
            />
          </label>
          <fieldset className="provider-options">
            <legend>{t("profile.providers")}</legend>
            <label className="confirmation-check">
              <input
                type="checkbox"
                checked={draft.codex}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    codex: event.target.checked,
                  })
                }
              />
              <span>Codex</span>
            </label>
            <label className="confirmation-check">
              <input
                type="checkbox"
                checked={draft.claudeAgent}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    claudeAgent: event.target.checked,
                  })
                }
              />
              <span>Claude Code</span>
            </label>
          </fieldset>
          <div className="form-row">
            <label>
              <span>{t("profile.cpuLimit")}</span>
              <input
                type="number"
                min="100"
                max="64000"
                step="100"
                value={draft.cpuLimitMillis}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    cpuLimitMillis: event.target.value,
                  })
                }
                required
              />
            </label>
            <label>
              <span>{t("profile.memoryLimit")}</span>
              <input
                type="number"
                min="128"
                max="1048576"
                step="128"
                value={draft.memoryLimitMiB}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    memoryLimitMiB: event.target.value,
                  })
                }
                required
              />
            </label>
          </div>
          <label>
            <span>{t("profile.storagePolicyRef")}</span>
            <select
              value={draft.storagePolicyRef}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  storagePolicyRef: event.target.value,
                })
              }
              required
            >
              <option value="" disabled>
                {t("profile.selectStoragePolicy")}
              </option>
              {storagePolicies.map((policy) => (
                <option key={policy.metadata.uid} value={policy.metadata.uid}>
                  {policy.metadata.name} · {policy.spec.userSummary}
                </option>
              ))}
            </select>
            <small>{t("profile.storagePolicyHelp")}</small>
          </label>
          <label>
            <span>{t("profile.networkPolicyRef")}</span>
            <select
              value={draft.networkPolicyRef}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  networkPolicyRef: event.target.value,
                })
              }
              required
            >
              <option value="">{t("profile.selectNetworkPolicy")}</option>
              {networkPolicies.map((policy) => (
                <option key={policy.metadata.uid} value={policy.metadata.uid}>
                  {policy.metadata.name} · {policy.spec.userSummary}
                </option>
              ))}
            </select>
            <small>{t("profile.networkPolicyHelp")}</small>
          </label>
          <label>
            <span>{t("profile.releaseDigest")}</span>
            <select
              value={draft.releaseDigest}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  releaseDigest: event.target.value,
                })
              }
              required
            >
              <option value="" disabled>
                {releases.length === 0
                  ? t("profile.noApprovedReleases")
                  : t("profile.selectRelease")}
              </option>
              {releases.map((release) => (
                <option key={release.metadata.uid} value={release.spec.releaseDigest}>
                  {release.metadata.name} · {shortDigest(release.spec.releaseDigest)}
                </option>
              ))}
            </select>
            <small>{t("profile.releaseDigestHelp")}</small>
          </label>
          <label>
            <span>{t("profile.targetRefs")}</span>
            <input
              value={draft.targetRefs}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  targetRefs: event.target.value,
                })
              }
              placeholder="docker-primary, ssh-overflow"
              required
              spellCheck={false}
            />
            <small>{t("profile.targetRefsHelp")}</small>
          </label>
          <label>
            <span>{t("profile.providerCredentialRef")}</span>
            <input
              value={draft.providerCredentialRef}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  providerCredentialRef: event.target.value,
                })
              }
              placeholder="provider-default"
              maxLength={128}
              required
              spellCheck={false}
            />
            <small>{t("profile.providerCredentialRefHelp")}</small>
          </label>
          <div className="dialog-actions">
            <button className="button ghost" type="button" onClick={() => onClose()}>
              {t("action.cancel")}
            </button>
            <button className="button primary" type="submit" disabled={disabled}>
              {t("profile.createDraft")}
            </button>
          </div>
        </form>
      </section>
    </AdminSheet>
  );
}

export type RuntimeProfileDraft = {
  profileId: string;
  profileName: string;
  version: string;
  description: string;
  workloadTrust: RuntimeProfile["spec"]["workloadTrust"];
  targetId: string;
  networkPolicyRef: string;
  imageUri: string;
  releaseDigest: string;
  cpuMillis: string;
  memoryMiB: string;
};

export function runtimeProfileForm(): RuntimeProfileDraft {
  return {
    profileId: "",
    profileName: "",
    version: "1",
    description: "",
    workloadTrust: "trusted-single-tenant",
    targetId: "",
    networkPolicyRef: "",
    imageUri: "",
    releaseDigest: "",
    cpuMillis: "1000",
    memoryMiB: "1024",
  };
}

export function isolationRuntimeForTrust(
  workloadTrust: RuntimeProfile["spec"]["workloadTrust"],
): RuntimeProfile["spec"]["isolationRuntime"] {
  return workloadTrust === "shared-untrusted" ? "gvisor" : "runc";
}

export function runtimeProfileCreateRequestFrom(
  draft: RuntimeProfileDraft,
): RuntimeProfileCreateRequest {
  return {
    profileId: draft.profileId.trim(),
    profileName: draft.profileName.trim(),
    version: Number(draft.version),
    description: draft.description.trim(),
    workloadTrust: draft.workloadTrust,
    isolationRuntime: isolationRuntimeForTrust(draft.workloadTrust),
    ...(draft.targetId === "pool-remote-worker:arm64"
      ? {
          targetSelector: {
            regionId: "region-local",
            resourcePoolId: "pool-remote-worker",
            runtime: "docker" as const,
            architecture: "arm64" as const,
          },
        }
      : draft.targetId === "pool-remote-worker:amd64"
        ? {
            targetSelector: {
              regionId: "region-local",
              resourcePoolId: "pool-remote-worker",
              runtime: "docker" as const,
              architecture: "amd64" as const,
            },
          }
        : { targetId: draft.targetId }),
    networkPolicyRef: draft.networkPolicyRef,
    imageUri: draft.imageUri.trim(),
    releaseDigest: draft.releaseDigest.trim() as `sha256:${string}`,
    cpuMillis: Number(draft.cpuMillis),
    memoryBytes: Number(draft.memoryMiB) * 1_048_576,
  };
}

export function RuntimeProfileCreateForm({
  draft,
  targets,
  networkPolicies,
  feedback,
  disabled,
  onDraftChange,
  onClose,
  onSubmit,
}: Readonly<{
  draft: RuntimeProfileDraft;
  targets: readonly DeploymentTarget[];
  networkPolicies: readonly NetworkPolicy[];
  feedback: ReactNode;
  disabled: boolean;
  onDraftChange: (draft: RuntimeProfileDraft) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}>) {
  const { t } = useI18n();
  return (
    <AdminSheet label={t("runtimeProfile.createTitle")} feedback={feedback} onClose={onClose}>
      <section className="dialog" aria-labelledby="create-runtime-profile-title">
        <div className="panel-heading">
          <div>
            <div className="eyebrow">no-Agent</div>
            <h2 id="create-runtime-profile-title">{t("runtimeProfile.createTitle")}</h2>
            <p>{t("runtimeProfile.createDescription")}</p>
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
              <span>{t("runtimeProfile.id")}</span>
              <input
                value={draft.profileId}
                pattern={targetIdentifierPattern}
                maxLength={128}
                required
                autoFocus
                data-sheet-autofocus
                spellCheck={false}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    profileId: event.target.value,
                  })
                }
              />
            </label>
            <label>
              <span>{t("runtimeProfile.name")}</span>
              <input
                value={draft.profileName}
                pattern={targetIdentifierPattern}
                maxLength={128}
                required
                spellCheck={false}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    profileName: event.target.value,
                  })
                }
              />
            </label>
          </div>
          <div className="form-row">
            <label>
              <span>{t("runtimeProfile.workloadTrust")}</span>
              <select
                value={draft.workloadTrust}
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    workloadTrust: event.target.value as WorkloadTrust,
                    targetId: "",
                    networkPolicyRef: "",
                  })
                }
              >
                {(["trusted-single-tenant", "dedicated-node", "shared-untrusted"] as const).map(
                  (value) => (
                    <option key={value} value={value}>
                      {t(`runtimeProfile.workloadTrust.${value}`)}
                    </option>
                  ),
                )}
              </select>
            </label>
            <label>
              <span>{t("runtimeProfile.isolationRuntime")}</span>
              <select
                aria-readonly="true"
                disabled
                value={isolationRuntimeForTrust(draft.workloadTrust)}
              >
                <option value={isolationRuntimeForTrust(draft.workloadTrust)}>
                  {t(
                    `runtimeProfile.isolationRuntime.${isolationRuntimeForTrust(draft.workloadTrust)}`,
                  )}
                </option>
              </select>
            </label>
          </div>
          {draft.workloadTrust === "shared-untrusted" ? (
            <p className="boundary-note">{t("runtimeProfile.gvisorLimitation")}</p>
          ) : null}
          <div className="form-row">
            <label>
              <span>{t("profile.version")}</span>
              <input
                type="number"
                min="1"
                max="2147483647"
                value={draft.version}
                required
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    version: event.target.value,
                  })
                }
              />
            </label>
            <label>
              <span>{t("runtimeProfile.target")}</span>
              <select
                value={draft.targetId}
                required
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    targetId: event.target.value,
                  })
                }
              >
                <option value="" disabled>
                  {t("runtimeProfile.selectTarget")}
                </option>
                {targets.some(
                  ({ spec }) =>
                    spec.targetKind === "remote-worker" && spec.architecture === "arm64",
                ) ? (
                  <option value="pool-remote-worker:arm64">
                    {t("runtimeProfile.remoteWorkerPoolArm64")}
                  </option>
                ) : null}
                {targets.some(
                  ({ spec }) =>
                    spec.targetKind === "remote-worker" && spec.architecture === "amd64",
                ) ? (
                  <option value="pool-remote-worker:amd64">
                    {t("runtimeProfile.remoteWorkerPoolAmd64")}
                  </option>
                ) : null}
                {targets
                  .filter(
                    ({ spec }) =>
                      spec.targetKind === "remote-worker" ||
                      (draft.workloadTrust === "trusted-single-tenant" &&
                        spec.targetKind === "docker"),
                  )
                  .map((target) => (
                    <option key={target.metadata.uid} value={target.metadata.uid}>
                      {target.metadata.name} · {phaseLabel(target.spec.observedPhase, t)}
                    </option>
                  ))}
              </select>
            </label>
          </div>
          <label>
            <span>{t("runtimeProfile.networkPolicy")}</span>
            <select
              value={draft.networkPolicyRef}
              required
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  networkPolicyRef: event.target.value,
                })
              }
            >
              <option value="" disabled>
                {t("runtimeProfile.selectNetworkPolicy")}
              </option>
              {networkPolicies
                .filter((policy) => executableFoundationNetworkPolicy(policy, draft.workloadTrust))
                .map((policy) => (
                  <option key={policy.metadata.uid} value={policy.metadata.uid}>
                    {policy.metadata.name} · {policy.spec.userSummary}
                  </option>
                ))}
            </select>
            <small>{t("runtimeProfile.networkPolicyHelp")}</small>
          </label>
          <label>
            <span>{t("runtimeProfile.description")}</span>
            <input
              value={draft.description}
              placeholder={t("runtimeProfile.descriptionPlaceholder")}
              minLength={1}
              maxLength={1024}
              required
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  description: event.target.value,
                })
              }
            />
          </label>
          <label>
            <span>{t("runtimeProfile.image")}</span>
            <input
              className="mono"
              value={draft.imageUri}
              placeholder={`registry.example/runtime@sha256:${"a".repeat(64)}`}
              maxLength={1024}
              required
              spellCheck={false}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  imageUri: event.target.value,
                })
              }
            />
          </label>
          <label>
            <span>{t("runtimeProfile.releaseDigest")}</span>
            <input
              className="mono"
              value={draft.releaseDigest}
              placeholder={`sha256:${"a".repeat(64)}`}
              pattern="sha256:[0-9a-f]{64}"
              minLength={71}
              maxLength={71}
              required
              spellCheck={false}
              onChange={(event) =>
                onDraftChange({
                  ...draft,
                  releaseDigest: event.target.value,
                })
              }
            />
          </label>
          <div className="form-row">
            <label>
              <span>{t("runtimeProfile.cpu")}</span>
              <input
                type="number"
                min="100"
                max="64000"
                value={draft.cpuMillis}
                required
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    cpuMillis: event.target.value,
                  })
                }
              />
            </label>
            <label>
              <span>{t("runtimeProfile.memory")}</span>
              <input
                type="number"
                min="128"
                max="1048576"
                value={draft.memoryMiB}
                required
                onChange={(event) =>
                  onDraftChange({
                    ...draft,
                    memoryMiB: event.target.value,
                  })
                }
              />
            </label>
          </div>
          <p className="boundary-note">{t("runtimeProfile.boundary")}</p>
          <div className="dialog-actions">
            <button className="button ghost" type="button" onClick={() => onClose()}>
              {t("action.cancel")}
            </button>
            <button className="button primary" type="submit" disabled={disabled}>
              {t("runtimeProfile.createDraft")}
            </button>
          </div>
        </form>
      </section>
    </AdminSheet>
  );
}
