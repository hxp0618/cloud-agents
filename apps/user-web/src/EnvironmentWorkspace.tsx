import { useEffect, useRef, useState } from "react";
import {
  ClientError,
  type Client,
  type EnvironmentProfileSummary,
  type ProjectLeaseQuotaSummary,
  type UserEnvironment,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { AgentWorkspace } from "./AgentWorkspace";
import {
  environmentErrorMessage,
  loadEnvironmentProfiles,
  loadProjectLeaseQuota,
  newIdempotencyKey,
  newRequestId,
  readEnvironmentSelection,
  writeEnvironmentSelection,
} from "./environment";

type EnvironmentWorkspaceProps = Readonly<{
  client: Client;
  tenantId: string;
  projectId: string;
  projectName: string;
}>;

function profileKey(profile: EnvironmentProfileSummary): string {
  return `${profile.profileId}:${profile.version}`;
}

function environmentTone(phase: UserEnvironment["observedPhase"]): string {
  if (phase === "ready" || phase === "terminated") return "success";
  if (phase === "failed") return "danger";
  return "running";
}

function providerLabel(provider: EnvironmentProfileSummary["providerKinds"][number]): string {
  return provider === "claudeAgent" ? "Claude Code" : "Codex";
}

export function EnvironmentWorkspace({
  client,
  tenantId,
  projectId,
  projectName,
}: EnvironmentWorkspaceProps) {
  const saved = useRef(readEnvironmentSelection(window.sessionStorage, tenantId, projectId));
  const [profiles, setProfiles] = useState<readonly EnvironmentProfileSummary[]>([]);
  const [leaseQuota, setLeaseQuota] = useState<ProjectLeaseQuotaSummary>();
  const [selectedProfileKey, setSelectedProfileKey] = useState("");
  const [environment, setEnvironment] = useState<UserEnvironment>();
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const operationControllerRef = useRef<AbortController | null>(null);
  const pendingCreateRef = useRef<{ bodyKey: string; idempotencyKey: string } | undefined>(
    undefined,
  );

  const selectedProfile = profiles.find((profile) => profileKey(profile) === selectedProfileKey);

  function persist(profile: EnvironmentProfileSummary | undefined, environmentId: string) {
    writeEnvironmentSelection(window.sessionStorage, {
      tenantId,
      projectId,
      profileId: profile?.profileId ?? "",
      profileVersion: profile?.version ?? 0,
      environmentId,
    });
  }

  useEffect(() => {
    const controller = new AbortController();
    operationControllerRef.current = controller;
    setLoadState("loading");
    setError("");
    void (async () => {
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
      const [loadedProfiles, loadedLeaseQuota] = await Promise.all([
        loadEnvironmentProfiles(client, tenantId, projectId, signal),
        loadProjectLeaseQuota(client, tenantId, projectId, signal),
      ]);
      let loadedEnvironment: UserEnvironment | undefined;
      if (saved.current.environmentId !== "") {
        try {
          loadedEnvironment = (
            await client.getEnvironment(
              tenantId,
              projectId,
              saved.current.environmentId,
              newRequestId(),
              signal,
            )
          ).value;
        } catch (cause) {
          if (!(cause instanceof ClientError) || cause.status !== 404) throw cause;
        }
      }
      if (controller.signal.aborted) return;
      const environmentProfile = loadedProfiles.find(
        ({ profileId, version }) =>
          profileId === loadedEnvironment?.profileId &&
          version === loadedEnvironment.profileVersion,
      );
      if (loadedEnvironment !== undefined && environmentProfile === undefined)
        loadedEnvironment = undefined;
      const selected =
        environmentProfile ??
        loadedProfiles.find(
          ({ profileId, version }) =>
            profileId === saved.current.profileId && version === saved.current.profileVersion,
        ) ??
        loadedProfiles[0];
      setProfiles(loadedProfiles);
      setLeaseQuota(loadedLeaseQuota);
      setSelectedProfileKey(selected === undefined ? "" : profileKey(selected));
      setEnvironment(loadedEnvironment);
      persist(selected, loadedEnvironment?.environmentId ?? "");
      setLoadState("ready");
    })().catch((cause: unknown) => {
      if (controller.signal.aborted) return;
      setLoadState("error");
      setError(environmentErrorMessage(cause));
    });
    return () => controller.abort();
  }, [client, projectId, tenantId]);

  useEffect(() => {
    if (
      environment === undefined ||
      (environment.observedPhase !== "provisioning" && environment.observedPhase !== "terminating")
    )
      return;
    const environmentId = environment.environmentId;
    const controller = new AbortController();
    let polling = false;
    const poll = async () => {
      if (polling || document.visibilityState !== "visible") return;
      polling = true;
      try {
        const result = await client.getEnvironment(
          tenantId,
          projectId,
          environmentId,
          newRequestId(),
          AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
        );
        if (controller.signal.aborted) return;
        setEnvironment(result.value);
        setError("");
      } catch (cause) {
        if (!controller.signal.aborted) setError(environmentErrorMessage(cause));
      } finally {
        polling = false;
      }
    };
    const interval = window.setInterval(poll, 1_500);
    return () => {
      controller.abort();
      window.clearInterval(interval);
    };
  }, [client, environment?.environmentId, environment?.observedPhase, projectId, tenantId]);

  useEffect(
    () => () => {
      operationControllerRef.current?.abort();
    },
    [],
  );

  async function runOperation(label: string, operation: (signal: AbortSignal) => Promise<void>) {
    if (busy !== "") return;
    setBusy(label);
    setError("");
    const controller = new AbortController();
    operationControllerRef.current = controller;
    try {
      await operation(AbortSignal.any([controller.signal, AbortSignal.timeout(150_000)]));
    } catch (cause) {
      setError(environmentErrorMessage(cause));
    } finally {
      if (operationControllerRef.current === controller) operationControllerRef.current = null;
      setBusy("");
    }
  }

  function selectProfile(key: string) {
    const profile = profiles.find((candidate) => profileKey(candidate) === key);
    if (profile === undefined) return;
    setSelectedProfileKey(key);
    setEnvironment(undefined);
    pendingCreateRef.current = undefined;
    persist(profile, "");
  }

  function refreshProfiles() {
    void runOperation("Refreshing Profiles", async (signal) => {
      const [loaded, loadedLeaseQuota] = await Promise.all([
        loadEnvironmentProfiles(client, tenantId, projectId, signal),
        loadProjectLeaseQuota(client, tenantId, projectId, signal),
      ]);
      const selected =
        loaded.find((profile) => profileKey(profile) === selectedProfileKey) ?? loaded[0];
      const selectedEnvironment =
        environment !== undefined &&
        selected !== undefined &&
        environment.profileId === selected.profileId &&
        environment.profileVersion === selected.version
          ? environment
          : undefined;
      setProfiles(loaded);
      setLeaseQuota(loadedLeaseQuota);
      setSelectedProfileKey(selected === undefined ? "" : profileKey(selected));
      setEnvironment(selectedEnvironment);
      persist(selected, selectedEnvironment?.environmentId ?? "");
      setLoadState("ready");
    });
  }

  function refreshEnvironment() {
    if (environment === undefined) return;
    void runOperation("Refreshing environment", async (signal) => {
      const result = await client.getEnvironment(
        tenantId,
        projectId,
        environment.environmentId,
        newRequestId(),
        signal,
      );
      setEnvironment(result.value);
    });
  }

  function createEnvironment() {
    if (selectedProfile === undefined) return;
    const body = {
      profileId: selectedProfile.profileId,
      profileVersion: selectedProfile.version,
    };
    const bodyKey = JSON.stringify(body);
    const pending = pendingCreateRef.current;
    const idempotencyKey =
      pending?.bodyKey === bodyKey ? pending.idempotencyKey : newIdempotencyKey();
    pendingCreateRef.current = { bodyKey, idempotencyKey };
    void runOperation("Preparing environment", async (signal) => {
      const result = await client.createEnvironment(
        tenantId,
        projectId,
        newRequestId(),
        idempotencyKey,
        body,
        signal,
      );
      pendingCreateRef.current = undefined;
      setEnvironment(result.value);
      persist(selectedProfile, result.value.environmentId);
    });
  }

  const environmentActive =
    environment?.observedPhase === "provisioning" || environment?.observedPhase === "ready";

  return (
    <>
      <aside className="left-rail" aria-label="Environment Profiles">
        <section className="panel rail-section environment-section">
          <div className="panel-heading">
            <span>
              <small>Published catalog</small>
              <h2>Environment Profiles</h2>
            </span>
            <button
              className="icon-button"
              type="button"
              onClick={refreshProfiles}
              disabled={busy !== ""}
              aria-label="Refresh Environment Profiles"
              title="Refresh Environment Profiles"
            >
              ↻
            </button>
          </div>

          <div className="resource-scroll">
            {loadState === "loading" ? (
              <div className="loading-state" role="status">
                <strong>Loading Profiles</strong>
                <span>Reading the published catalog from Control Plane…</span>
              </div>
            ) : profiles.length === 0 ? (
              <div className="loading-state">
                <strong>No available Profile</strong>
                <span>Ask an administrator to publish an environment for this project.</span>
              </div>
            ) : (
              <div className="resource-list" aria-label="Available Environment Profiles">
                {profiles.map((profile) => (
                  <button
                    className={`resource-card ${profileKey(profile) === selectedProfileKey ? "selected" : ""}`}
                    type="button"
                    key={profileKey(profile)}
                    onClick={() => selectProfile(profileKey(profile))}
                    disabled={busy !== ""}
                  >
                    <span className="kind-mark kind-profile" aria-hidden="true">
                      EP
                    </span>
                    <span className="resource-copy">
                      <strong>{profile.name}</strong>
                      <small>
                        v{profile.version} · {profile.providerKinds.map(providerLabel).join(" / ")}
                      </small>
                    </span>
                    <span className="phase-badge success">available</span>
                  </button>
                ))}
              </div>
            )}
          </div>

          {selectedProfile ? (
            <div className="profile-details">
              <div>
                <small>Selected Profile</small>
                <strong>
                  {selectedProfile.name} · v{selectedProfile.version}
                </strong>
                <p>{selectedProfile.description}</p>
                <small>{selectedProfile.storageSummary}</small>
              </div>
              {environment ? (
                <div className="environment-state" aria-live="polite">
                  <span>
                    <small>Environment</small>
                    <strong>{environment.observedPhase}</strong>
                  </span>
                  <span className={`phase-badge ${environmentTone(environment.observedPhase)}`}>
                    {environment.observedPhase}
                  </span>
                </div>
              ) : null}
            </div>
          ) : null}

          {leaseQuota ? (
            <div className="quota-summary" aria-label="Project environment limits">
              <div>
                <small>Project capacity</small>
                <strong>
                  {leaseQuota.activeLeases} of {leaseQuota.maxConcurrentLeases} environments active
                </strong>
              </div>
              <p>
                {leaseQuota.usedCpuMillis.toLocaleString()} /{" "}
                {leaseQuota.maxCpuMillis.toLocaleString()} mCPU
                {" · "}
                {(leaseQuota.usedMemoryBytes / 1073741824).toLocaleString(undefined, {
                  maximumFractionDigits: 1,
                })}{" "}
                /{" "}
                {(leaseQuota.maxMemoryBytes / 1073741824).toLocaleString(undefined, {
                  maximumFractionDigits: 1,
                })}{" "}
                GiB
                {" · max "}
                {Math.floor(leaseQuota.maxLeaseTtlSeconds / 60).toLocaleString()} min
              </p>
            </div>
          ) : null}

          <div className="resource-actions">
            <button
              className="button primary compact"
              type="button"
              onClick={createEnvironment}
              disabled={busy !== "" || selectedProfile === undefined || environmentActive}
            >
              {busy === "Preparing environment"
                ? "Preparing…"
                : environment?.observedPhase === "failed" ||
                    environment?.observedPhase === "terminated"
                  ? "Prepare again"
                  : environmentActive
                    ? environment?.observedPhase
                    : "Prepare environment"}
            </button>
            <button
              className="button ghost compact"
              type="button"
              onClick={refreshEnvironment}
              disabled={busy !== "" || environment === undefined}
            >
              Refresh
            </button>
          </div>

          {error ? (
            <details className="diagnostic compact-diagnostic" open>
              <summary>Environment needs attention</summary>
              <p>{error}</p>
            </details>
          ) : null}
          {loadState === "error" && !error ? (
            <div className="loading-state">Environment catalog could not be loaded.</div>
          ) : null}
        </section>
      </aside>

      <AgentWorkspace
        client={client}
        tenantId={tenantId}
        projectId={projectId}
        projectName={projectName}
        profile={selectedProfile}
        environment={environment}
      />
    </>
  );
}
