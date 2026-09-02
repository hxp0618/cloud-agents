import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ClientError,
  type DeploymentTargetRegisterRequest,
  type EnvironmentLease,
  type EnvironmentLeaseCreateRequest,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  infrastructureErrorMessage,
  loadInfrastructure,
  newIdempotencyKey,
  newRequestId,
  readInfrastructureSelection,
  writeInfrastructureSelection,
  type InfrastructureClient,
  type InfrastructureResources,
} from "./infrastructure";

type InfrastructureWorkspaceProps = Readonly<{
  client: InfrastructureClient;
  tenantId: string;
  projectId: string;
  projectName: string;
}>;

type TargetKind = DeploymentTargetRegisterRequest["targetKind"];
type FormMode = "target" | "lease" | "upgrade" | null;
type BusyOperation = Readonly<{ key: string; label: string }>;

const emptyResources: InfrastructureResources = Object.freeze({
  targets: Object.freeze([]),
  leases: Object.freeze([]),
});

const targetPlaceholders: Readonly<Record<TargetKind, string>> = Object.freeze({
  docker: "https://docker-target.example:2376",
  kubernetes: "https://kubernetes.example:6443",
  ssh: "ssh://orb-host.example:22",
});

function replaceResource<T extends { metadata: { uid: string; name: string } }>(
  resources: readonly T[],
  value: T,
): readonly T[] {
  return [
    ...resources.filter(({ metadata }) => metadata.uid !== value.metadata.uid),
    value,
  ].toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name));
}

function phaseTone(phase: string): string {
  if (phase === "ready" || phase === "complete" || phase === "terminated") return "success";
  if (phase === "unavailable" || phase === "failed" || phase === "blocked") return "danger";
  if (phase === "probing" || phase === "provisioning" || phase === "terminating") return "running";
  return "neutral";
}

function targetCredentialHint(kind: TargetKind): string {
  switch (kind) {
    case "kubernetes":
      return "Names a Control Plane credential bundle for kubeconfig or ServiceAccount access.";
    case "ssh":
      return "Names a Control Plane SSH bundle containing host-key authority and private-key files.";
    default:
      return "Names a Control Plane mTLS bundle for the target Docker API.";
  }
}

function providerCredentialHint(kind: TargetKind | undefined): string {
  return kind === "kubernetes"
    ? "Target-side Kubernetes Secret mounted read-only into the Worker."
    : "Target-side Docker volume mounted read-only into the Worker.";
}

export function InfrastructureWorkspace({
  client,
  tenantId,
  projectId,
  projectName,
}: InfrastructureWorkspaceProps) {
  const savedSelection = useRef(
    readInfrastructureSelection(window.sessionStorage, tenantId, projectId),
  );
  const [resources, setResources] = useState<InfrastructureResources>(emptyResources);
  const [selectedTargetId, setSelectedTargetId] = useState(savedSelection.current.targetId);
  const [selectedLeaseId, setSelectedLeaseId] = useState(savedSelection.current.leaseId);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");
  const [formMode, setFormMode] = useState<FormMode>(null);
  const [busy, setBusy] = useState<BusyOperation | null>(null);
  const [error, setError] = useState("");
  const [pollingStopped, setPollingStopped] = useState(false);
  const [targetForm, setTargetForm] = useState({
    targetId: "",
    targetName: "",
    targetKind: "docker" as TargetKind,
    endpoint: "",
    credentialRef: "",
  });
  const [leaseForm, setLeaseForm] = useState({
    leaseId: "",
    leaseName: "",
    releaseDigest: "",
    providerCredentialRef: "",
    cpuLimitMillis: "1000",
    memoryLimitBytes: String(512 * 1024 * 1024),
    ttlSeconds: "3600",
  });
  const [upgradeDigest, setUpgradeDigest] = useState("");
  const operationControllerRef = useRef<AbortController | null>(null);
  const busyRef = useRef(false);
  const pendingKeysRef = useRef(new Map<string, string>());

  const selectedTarget = resources.targets.find(
    ({ metadata }) => metadata.uid === selectedTargetId,
  );
  const selectedLease = resources.leases.find(({ metadata }) => metadata.uid === selectedLeaseId);
  const pollingNeeded =
    resources.targets.some(({ spec }) => spec.observedPhase === "probing") ||
    resources.leases.some(({ spec }) =>
      ["provisioning", "terminating"].includes(spec.observedPhase),
    );

  useEffect(() => {
    const controller = new AbortController();
    setLoadState("loading");
    setError("");
    void loadInfrastructure(
      client,
      tenantId,
      projectId,
      AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
    )
      .then((loaded) => {
        const savedLease = loaded.leases.find(
          ({ metadata }) => metadata.uid === savedSelection.current.leaseId,
        );
        const targetId = savedLease?.spec.targetId
          ? savedLease.spec.targetId
          : loaded.targets.some(({ metadata }) => metadata.uid === savedSelection.current.targetId)
            ? savedSelection.current.targetId
            : (loaded.targets[0]?.metadata.uid ?? "");
        const leaseId = savedLease?.metadata.uid ?? loaded.leases[0]?.metadata.uid ?? "";
        setResources(loaded);
        setSelectedTargetId(targetId);
        setSelectedLeaseId(leaseId);
        writeInfrastructureSelection(window.sessionStorage, {
          tenantId,
          projectId,
          targetId,
          leaseId,
        });
        setLoadState("ready");
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setLoadState("error");
        setError(infrastructureErrorMessage(cause));
      });
    return () => controller.abort();
  }, [client, projectId, tenantId]);

  useEffect(
    () => () => {
      operationControllerRef.current?.abort();
    },
    [],
  );

  useEffect(() => {
    if (!pollingNeeded || pollingStopped) return;
    const controller = new AbortController();
    let polling = false;
    const interval = window.setInterval(() => {
      if (document.visibilityState !== "visible" || polling || busyRef.current) return;
      polling = true;
      void loadInfrastructure(
        client,
        tenantId,
        projectId,
        AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
      )
        .then((loaded) => setResources(loaded))
        .catch((cause: unknown) => {
          if (controller.signal.aborted) return;
          setError(infrastructureErrorMessage(cause));
          if (cause instanceof ClientError && (cause.status === 401 || cause.status === 403))
            setPollingStopped(true);
        })
        .finally(() => {
          polling = false;
        });
    }, 5_000);
    return () => {
      controller.abort();
      window.clearInterval(interval);
    };
  }, [client, pollingNeeded, pollingStopped, projectId, tenantId]);

  function saveSelection(targetId: string, leaseId: string) {
    setSelectedTargetId(targetId);
    setSelectedLeaseId(leaseId);
    writeInfrastructureSelection(window.sessionStorage, {
      tenantId,
      projectId,
      targetId,
      leaseId,
    });
  }

  function selectTarget(targetId: string) {
    const relatedLease = resources.leases.find(({ spec }) => spec.targetId === targetId);
    saveSelection(targetId, relatedLease?.metadata.uid ?? "");
  }

  function selectLease(lease: EnvironmentLease) {
    saveSelection(lease.spec.targetId ?? selectedTargetId, lease.metadata.uid);
  }

  function idempotencyKey(operationKey: string): string {
    const existing = pendingKeysRef.current.get(operationKey);
    if (existing !== undefined) return existing;
    const created = newIdempotencyKey();
    pendingKeysRef.current.set(operationKey, created);
    return created;
  }

  async function runOperation(
    key: string,
    label: string,
    operation: (signal: AbortSignal) => Promise<void>,
  ) {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy({ key, label });
    setError("");
    const controller = new AbortController();
    operationControllerRef.current = controller;
    try {
      await operation(AbortSignal.any([controller.signal, AbortSignal.timeout(150_000)]));
      pendingKeysRef.current.delete(key);
      setPollingStopped(false);
    } catch (cause) {
      setError(infrastructureErrorMessage(cause));
    } finally {
      if (operationControllerRef.current === controller) operationControllerRef.current = null;
      busyRef.current = false;
      setBusy(null);
    }
  }

  function refreshAll() {
    void runOperation("refresh", "Refreshing target and lease authority", async (signal) => {
      setResources(await loadInfrastructure(client, tenantId, projectId, signal));
      setLoadState("ready");
    });
  }

  function refreshTarget() {
    if (selectedTarget === undefined) return;
    void runOperation(
      `get-target:${selectedTarget.metadata.uid}`,
      "Refreshing target state",
      async (signal) => {
        const result = await client.getDeploymentTarget(
          tenantId,
          projectId,
          selectedTarget.metadata.uid,
          newRequestId(),
          signal,
        );
        setResources((current) => ({
          ...current,
          targets: replaceResource(current.targets, result.value),
        }));
      },
    );
  }

  function refreshLease() {
    if (selectedLease === undefined) return;
    void runOperation(
      `get-lease:${selectedLease.metadata.uid}`,
      "Refreshing lease state",
      async (signal) => {
        const result = await client.getManagedHostEnvironmentLease(
          tenantId,
          projectId,
          selectedLease.metadata.uid,
          newRequestId(),
          signal,
        );
        setResources((current) => ({
          ...current,
          leases: replaceResource(current.leases, result.value),
        }));
      },
    );
  }

  function registerTarget(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const body: DeploymentTargetRegisterRequest = {
      targetId: targetForm.targetId.trim(),
      targetName: targetForm.targetName.trim(),
      targetKind: targetForm.targetKind,
      endpoint: targetForm.endpoint.trim(),
      credentialRef: targetForm.credentialRef.trim(),
    };
    const key = `register-target:${JSON.stringify(body)}`;
    void runOperation(key, `Registering ${body.targetKind} target`, async (signal) => {
      const result = await client.registerDeploymentTarget(
        tenantId,
        projectId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      setResources((current) => ({
        ...current,
        targets: replaceResource(current.targets, result.value),
      }));
      saveSelection(result.value.metadata.uid, "");
      setFormMode(null);
    });
  }

  function probeTarget() {
    if (selectedTarget === undefined) return;
    const key = `probe-target:${selectedTarget.metadata.uid}:${selectedTarget.spec.generation}`;
    void runOperation(
      key,
      `Probing ${selectedTarget.spec.targetKind} connectivity`,
      async (signal) => {
        const result = await client.probeDeploymentTarget(
          tenantId,
          projectId,
          selectedTarget.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          { expectedGeneration: selectedTarget.spec.generation },
          signal,
        );
        setResources((current) => ({
          ...current,
          targets: replaceResource(current.targets, result.value),
        }));
      },
    );
  }

  function cleanupTarget() {
    if (
      selectedTarget === undefined ||
      !window.confirm(
        `Clean orphaned Workers for target ${selectedTarget.metadata.name}? Active fenced Lease generations are preserved.`,
      )
    )
      return;
    const key = `cleanup-target:${selectedTarget.metadata.uid}:${selectedTarget.spec.generation}`;
    void runOperation(key, "Reconciling orphaned target workloads", async (signal) => {
      const result = await client.cleanupDeploymentTarget(
        tenantId,
        projectId,
        selectedTarget.metadata.uid,
        newRequestId(),
        idempotencyKey(key),
        { expectedGeneration: selectedTarget.spec.generation },
        signal,
      );
      setResources((current) => ({
        ...current,
        targets: replaceResource(current.targets, result.value),
      }));
    });
  }

  function createLease(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (selectedTarget === undefined) return;
    const body: EnvironmentLeaseCreateRequest = {
      leaseId: leaseForm.leaseId.trim(),
      leaseName: leaseForm.leaseName.trim(),
      releaseDigest: leaseForm.releaseDigest.trim() as `sha256:${string}`,
      targetId: selectedTarget.metadata.uid,
      expectedTargetGeneration: selectedTarget.spec.generation,
      providerCredentialRef: leaseForm.providerCredentialRef.trim(),
      cpuLimitMillis: Number(leaseForm.cpuLimitMillis),
      memoryLimitBytes: Number(leaseForm.memoryLimitBytes),
      ttlSeconds: Number(leaseForm.ttlSeconds),
    };
    const key = `create-lease:${JSON.stringify(body)}`;
    void runOperation(key, "Deploying Worker and waiting for Ready", async (signal) => {
      const result = await client.createManagedHostEnvironmentLease(
        tenantId,
        projectId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      setResources((current) => ({
        ...current,
        leases: replaceResource(current.leases, result.value),
      }));
      saveSelection(selectedTarget.metadata.uid, result.value.metadata.uid);
      setFormMode(null);
    });
  }

  function upgradeLease(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (selectedLease === undefined) return;
    const releaseDigest = upgradeDigest.trim() as `sha256:${string}`;
    const key = `upgrade-lease:${selectedLease.metadata.uid}:${selectedLease.spec.generation}:${releaseDigest}`;
    void runOperation(key, "Starting successor Worker and waiting for Ready", async (signal) => {
      const result = await client.upgradeManagedHostEnvironmentLease(
        tenantId,
        projectId,
        selectedLease.metadata.uid,
        newRequestId(),
        idempotencyKey(key),
        { releaseDigest, expectedGeneration: selectedLease.spec.generation },
        signal,
      );
      setResources((current) => ({
        ...current,
        leases: replaceResource(current.leases, result.value),
      }));
      setUpgradeDigest("");
      setFormMode(null);
    });
  }

  function terminateLease() {
    if (
      selectedLease === undefined ||
      !window.confirm(
        `Terminate Lease ${selectedLease.metadata.name} and clean its Worker workload?`,
      )
    )
      return;
    const key = `terminate-lease:${selectedLease.metadata.uid}:${selectedLease.spec.generation}`;
    void runOperation(key, "Terminating Worker and verifying cleanup", async (signal) => {
      const result = await client.terminateManagedHostEnvironmentLease(
        tenantId,
        projectId,
        selectedLease.metadata.uid,
        newRequestId(),
        idempotencyKey(key),
        { expectedGeneration: selectedLease.spec.generation },
        signal,
      );
      setResources((current) => ({
        ...current,
        leases: replaceResource(current.leases, result.value),
      }));
    });
  }

  return (
    <>
      <aside className="left-rail" aria-label="Targets and leases">
        <section className="panel rail-section target-section">
          <div className="panel-heading">
            <span>
              <small>Infrastructure</small>
              <h2>Deployment Targets</h2>
            </span>
            <span className="heading-actions">
              <button
                className="icon-button"
                type="button"
                onClick={refreshAll}
                disabled={busy !== null}
                aria-label="Refresh targets and leases"
                title="Refresh server state"
              >
                ↻
              </button>
              <button
                className="icon-button"
                type="button"
                onClick={() => setFormMode(formMode === "target" ? null : "target")}
                disabled={busy !== null}
                aria-label="Register target"
                title="Register target"
              >
                +
              </button>
            </span>
          </div>

          {formMode === "target" ? (
            <form
              className="resource-form"
              aria-label="Register deployment target"
              onSubmit={registerTarget}
            >
              <div className="form-title">
                <strong>Register target</strong>
                <button
                  type="button"
                  onClick={() => setFormMode(null)}
                  aria-label="Close target form"
                >
                  ×
                </button>
              </div>
              <label>
                <span>Kind</span>
                <select
                  value={targetForm.targetKind}
                  onChange={(event) =>
                    setTargetForm((current) => ({
                      ...current,
                      targetKind: event.target.value as TargetKind,
                    }))
                  }
                >
                  <option value="docker">Docker</option>
                  <option value="kubernetes">Kubernetes</option>
                  <option value="ssh">SSH</option>
                </select>
              </label>
              <div className="form-grid two-column">
                <label>
                  <span>Target ID</span>
                  <input
                    value={targetForm.targetId}
                    onChange={(event) =>
                      setTargetForm((current) => ({ ...current, targetId: event.target.value }))
                    }
                    placeholder={`orbstack-${targetForm.targetKind}`}
                    required
                    spellCheck={false}
                  />
                </label>
                <label>
                  <span>Name</span>
                  <input
                    value={targetForm.targetName}
                    onChange={(event) =>
                      setTargetForm((current) => ({ ...current, targetName: event.target.value }))
                    }
                    placeholder={`orbstack-${targetForm.targetKind}`}
                    required
                  />
                </label>
              </div>
              <label>
                <span>Endpoint</span>
                <input
                  type="url"
                  value={targetForm.endpoint}
                  onChange={(event) =>
                    setTargetForm((current) => ({ ...current, endpoint: event.target.value }))
                  }
                  placeholder={targetPlaceholders[targetForm.targetKind]}
                  required
                  spellCheck={false}
                />
              </label>
              <label>
                <span>Credential reference</span>
                <input
                  value={targetForm.credentialRef}
                  onChange={(event) =>
                    setTargetForm((current) => ({
                      ...current,
                      credentialRef: event.target.value,
                    }))
                  }
                  placeholder={`orbstack-${targetForm.targetKind}`}
                  required
                  spellCheck={false}
                />
                <small>{targetCredentialHint(targetForm.targetKind)}</small>
              </label>
              <button className="button primary compact" type="submit" disabled={busy !== null}>
                Register
              </button>
            </form>
          ) : null}

          <div className="resource-scroll">
            {loadState === "loading" ? (
              <div className="loading-state" role="status">
                <strong>Loading project authority</strong>
                <span>Fetching target and lease pages…</span>
              </div>
            ) : resources.targets.length === 0 ? (
              <div className="empty-state compact-empty">
                <span className="empty-glyph">01</span>
                <strong>No deployment targets</strong>
                <p>Register Docker, Kubernetes, or SSH, then probe it before creating a Lease.</p>
              </div>
            ) : (
              <div className="resource-list" aria-label="Deployment targets">
                {resources.targets.map((target) => (
                  <button
                    className={`resource-card ${target.metadata.uid === selectedTargetId ? "selected" : ""}`}
                    type="button"
                    key={target.metadata.uid}
                    onClick={() => selectTarget(target.metadata.uid)}
                  >
                    <span className={`kind-mark kind-${target.spec.targetKind}`} aria-hidden="true">
                      {target.spec.targetKind.slice(0, 2).toUpperCase()}
                    </span>
                    <span className="resource-copy">
                      <strong>{target.metadata.name}</strong>
                      <small>
                        {target.spec.targetKind} · gen {target.spec.generation}
                      </small>
                    </span>
                    <span className={`phase-badge ${phaseTone(target.spec.observedPhase)}`}>
                      {target.spec.observedPhase}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>

          {selectedTarget ? (
            <div className="resource-actions" aria-label="Selected target actions">
              <button
                className="button secondary compact"
                type="button"
                onClick={probeTarget}
                disabled={busy !== null}
              >
                {selectedTarget.spec.observedPhase === "unavailable" ? "Retry probe" : "Probe"}
              </button>
              <button
                className="button ghost compact"
                type="button"
                onClick={refreshTarget}
                disabled={busy !== null}
              >
                Get
              </button>
              <button
                className="button ghost compact danger-action"
                type="button"
                onClick={cleanupTarget}
                disabled={busy !== null || selectedTarget.spec.observedPhase !== "ready"}
              >
                Cleanup
              </button>
            </div>
          ) : null}
        </section>

        <section className="panel rail-section lease-section">
          <div className="panel-heading">
            <span>
              <small>Runtime</small>
              <h2>Environment Leases</h2>
            </span>
            <button
              className="icon-button"
              type="button"
              onClick={() => setFormMode(formMode === "lease" ? null : "lease")}
              disabled={busy !== null || selectedTarget?.spec.observedPhase !== "ready"}
              aria-label="Create environment lease"
              title="Create environment lease"
            >
              +
            </button>
          </div>

          {formMode === "lease" && selectedTarget ? (
            <form
              className="resource-form lease-form"
              aria-label="Create environment lease"
              onSubmit={createLease}
            >
              <div className="form-title">
                <strong>Create on {selectedTarget.metadata.name}</strong>
                <button
                  type="button"
                  onClick={() => setFormMode(null)}
                  aria-label="Close lease form"
                >
                  ×
                </button>
              </div>
              <div className="form-grid two-column">
                <label>
                  <span>Lease ID</span>
                  <input
                    value={leaseForm.leaseId}
                    onChange={(event) =>
                      setLeaseForm((current) => ({ ...current, leaseId: event.target.value }))
                    }
                    placeholder="agent-workspace"
                    required
                    spellCheck={false}
                  />
                </label>
                <label>
                  <span>Name</span>
                  <input
                    value={leaseForm.leaseName}
                    onChange={(event) =>
                      setLeaseForm((current) => ({ ...current, leaseName: event.target.value }))
                    }
                    placeholder="agent-workspace"
                    required
                  />
                </label>
              </div>
              <label>
                <span>Worker image digest</span>
                <input
                  value={leaseForm.releaseDigest}
                  onChange={(event) =>
                    setLeaseForm((current) => ({ ...current, releaseDigest: event.target.value }))
                  }
                  placeholder="sha256:…"
                  minLength={71}
                  maxLength={71}
                  required
                  spellCheck={false}
                />
              </label>
              <label>
                <span>Provider credential reference</span>
                <input
                  value={leaseForm.providerCredentialRef}
                  onChange={(event) =>
                    setLeaseForm((current) => ({
                      ...current,
                      providerCredentialRef: event.target.value,
                    }))
                  }
                  placeholder="cloud-agents-provider-credentials"
                  required
                  spellCheck={false}
                />
                <small>{providerCredentialHint(selectedTarget.spec.targetKind)}</small>
              </label>
              <div className="form-grid three-column">
                <label>
                  <span>CPU m</span>
                  <input
                    type="number"
                    min="100"
                    max="64000"
                    value={leaseForm.cpuLimitMillis}
                    onChange={(event) =>
                      setLeaseForm((current) => ({
                        ...current,
                        cpuLimitMillis: event.target.value,
                      }))
                    }
                    required
                  />
                </label>
                <label>
                  <span>Memory bytes</span>
                  <input
                    type="number"
                    min={128 * 1024 * 1024}
                    value={leaseForm.memoryLimitBytes}
                    onChange={(event) =>
                      setLeaseForm((current) => ({
                        ...current,
                        memoryLimitBytes: event.target.value,
                      }))
                    }
                    required
                  />
                </label>
                <label>
                  <span>TTL seconds</span>
                  <input
                    type="number"
                    min="60"
                    value={leaseForm.ttlSeconds}
                    onChange={(event) =>
                      setLeaseForm((current) => ({ ...current, ttlSeconds: event.target.value }))
                    }
                    required
                  />
                </label>
              </div>
              <button className="button primary compact" type="submit" disabled={busy !== null}>
                Deploy and wait for Ready
              </button>
            </form>
          ) : null}

          {formMode === "upgrade" && selectedLease ? (
            <form
              className="resource-form lease-form"
              aria-label="Upgrade environment lease"
              onSubmit={upgradeLease}
            >
              <div className="form-title">
                <strong>Upgrade generation {selectedLease.spec.generation}</strong>
                <button
                  type="button"
                  onClick={() => setFormMode(null)}
                  aria-label="Close upgrade form"
                >
                  ×
                </button>
              </div>
              <label>
                <span>Successor image digest</span>
                <input
                  value={upgradeDigest}
                  onChange={(event) => setUpgradeDigest(event.target.value)}
                  placeholder="sha256:…"
                  minLength={71}
                  maxLength={71}
                  required
                  spellCheck={false}
                />
                <small>
                  The successor reuses this Lease workspace. The old Worker is removed only after
                  the new generation is Ready.
                </small>
              </label>
              <button className="button primary compact" type="submit" disabled={busy !== null}>
                Start zero-downtime upgrade
              </button>
            </form>
          ) : null}

          <div className="lease-list-wrap">
            {resources.leases.length === 0 ? (
              <div className="empty-row">
                <span className="status-dot neutral" aria-hidden="true" />
                No Environment Lease
              </div>
            ) : (
              <div className="resource-list lease-list" aria-label="Environment leases">
                {resources.leases.map((lease) => (
                  <button
                    className={`resource-card ${lease.metadata.uid === selectedLeaseId ? "selected" : ""}`}
                    type="button"
                    key={lease.metadata.uid}
                    onClick={() => selectLease(lease)}
                  >
                    <span className="kind-mark" aria-hidden="true">
                      L{lease.spec.generation}
                    </span>
                    <span className="resource-copy">
                      <strong>{lease.metadata.name}</strong>
                      <small>{lease.spec.targetId ?? "legacy managed host"}</small>
                    </span>
                    <span className={`phase-badge ${phaseTone(lease.spec.observedPhase)}`}>
                      {lease.spec.observedPhase}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>

          {selectedLease ? (
            <div className="resource-actions lease-actions" aria-label="Selected lease actions">
              <button
                className="button secondary compact"
                type="button"
                onClick={() => setFormMode(formMode === "upgrade" ? null : "upgrade")}
                disabled={busy !== null || selectedLease.spec.desiredPhase !== "active"}
              >
                Upgrade
              </button>
              <button
                className="button ghost compact"
                type="button"
                onClick={refreshLease}
                disabled={busy !== null}
              >
                Get
              </button>
              <button
                className="button ghost compact danger-action"
                type="button"
                onClick={terminateLease}
                disabled={busy !== null || selectedLease.spec.desiredPhase !== "active"}
              >
                Terminate
              </button>
            </div>
          ) : null}
        </section>
      </aside>

      <main className="conversation panel">
        <div className="conversation-toolbar">
          <div>
            <small>Agent workspace</small>
            <h1>{projectName || projectId}</h1>
          </div>
          <div className="toolbar-controls">
            <label>
              <span>Provider</span>
              <select aria-label="Agent provider" disabled>
                <option>Codex</option>
                <option>Claude Code</option>
              </select>
            </label>
            <button className="button secondary compact" type="button" disabled>
              New session
            </button>
          </div>
        </div>
        <div className="conversation-empty">
          <div className="agent-orbit" aria-hidden="true">
            <span>CA</span>
          </div>
          <strong>
            {selectedLease?.spec.observedPhase === "ready"
              ? "Worker ready for an Agent session"
              : "Ready the infrastructure before a real turn"}
          </strong>
          <p>
            {selectedLease?.spec.observedPhase === "ready"
              ? `Lease ${selectedLease.metadata.name} is fenced at generation ${selectedLease.spec.generation}. Session and Turn controls arrive in M3.`
              : "Select and probe a target, then deploy an isolated Environment Lease."}
          </p>
          <div className="phase-line" aria-label="Agent workflow">
            <span className="complete">Control Plane</span>
            <span className={selectedTarget?.spec.observedPhase === "ready" ? "complete" : ""}>
              Target
            </span>
            <span className={selectedLease?.spec.observedPhase === "ready" ? "complete" : ""}>
              Lease
            </span>
            <span>Session</span>
          </div>
        </div>
        <form className="prompt-bar" aria-label="Agent prompt">
          <label className="sr-only" htmlFor="prompt">
            Prompt
          </label>
          <textarea
            id="prompt"
            placeholder="Session support is added after the Target and Lease slice"
            rows={2}
            disabled
          />
          <button className="button primary" type="submit" disabled>
            Send
          </button>
        </form>
      </main>

      <aside className="activity panel" aria-label="Activity and interactions">
        <div className="panel-heading activity-heading">
          <span>
            <small>Live state</small>
            <h2>Activity</h2>
          </span>
          <span className={`live-badge ${busy ? "running" : ""}`}>
            {busy ? "Working" : selectedLease?.spec.observedPhase === "ready" ? "Ready" : "Idle"}
          </span>
        </div>
        <dl className="status-table">
          <div>
            <dt>Target</dt>
            <dd>{selectedTarget?.spec.observedPhase ?? "Not selected"}</dd>
          </div>
          <div>
            <dt>Lease</dt>
            <dd>{selectedLease?.spec.observedPhase ?? "Not created"}</dd>
          </div>
          <div>
            <dt>Worker</dt>
            <dd>{selectedLease?.spec.workerEndpoint ? "Online" : "Offline"}</dd>
          </div>
          <div>
            <dt>Execution</dt>
            <dd>Idle</dd>
          </div>
        </dl>

        {busy ? (
          <div className="operation-stage" role="status" aria-live="polite">
            <span className="status-dot" aria-hidden="true" />
            <span>
              <strong>{busy.label}</strong>
              <small>Request is fenced and duplicate submission is disabled.</small>
            </span>
            <button type="button" onClick={() => operationControllerRef.current?.abort()}>
              Cancel wait
            </button>
          </div>
        ) : null}

        {error ? (
          <details className="diagnostic" open>
            <summary>Infrastructure operation failed</summary>
            <p>{error}</p>
            <button className="button ghost compact" type="button" onClick={refreshAll}>
              Refresh server state
            </button>
          </details>
        ) : null}

        {selectedTarget?.spec.stableErrorCode ? (
          <details className="diagnostic">
            <summary>Target diagnostic</summary>
            <code>{selectedTarget.spec.stableErrorCode}</code>
            <p>
              Generation {selectedTarget.spec.generation} remains authoritative and can be probed
              again.
            </p>
          </details>
        ) : null}

        {selectedLease?.spec.stableErrorCode ? (
          <details className="diagnostic">
            <summary>Lease diagnostic</summary>
            <code>{selectedLease.spec.stableErrorCode}</code>
            <p>Refresh before retrying so the current Lease generation remains fenced.</p>
          </details>
        ) : null}

        <div className="timeline-empty">
          <span className="timeline-rule" aria-hidden="true" />
          <strong>Infrastructure timeline</strong>
          <p>
            {selectedLease
              ? `Lease generation ${selectedLease.spec.generation} · cleanup ${selectedLease.spec.cleanupPhase}`
              : selectedTarget
                ? `Target generation ${selectedTarget.spec.generation} · ${selectedTarget.spec.targetKind}`
                : "Select a target to inspect its generation and probe state."}
          </p>
        </div>
      </aside>
    </>
  );
}
