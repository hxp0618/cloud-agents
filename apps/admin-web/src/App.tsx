import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import {
  createHTTPClient,
  type AdminAuditEvent,
  type DeploymentTarget,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetRegisterRequest,
  type EnvironmentLease,
  type MaintenanceOperation,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  adminErrorMessage,
  listAdminLeases,
  listAdminTargetAuditEvents,
  listAdminTargetOperations,
  listAdminTargets,
  newIdempotencyKey,
  newRequestId,
  readSavedAdminConnection,
  replaceLease,
  replaceTarget,
  writeSavedAdminConnection,
  type AdminClient,
  type SavedAdminConnection,
} from "./admin";

type ConnectionStatus = "disconnected" | "connecting" | "connected" | "error";
type Page = "overview" | "targets" | "leases";
type TargetKind = DeploymentTargetRegisterRequest["targetKind"];
type BusyOperation = Readonly<{ key: string; label: string }>;
type Theme = "light" | "dark";

const targetEndpointPlaceholder: Readonly<Record<TargetKind, string>> = Object.freeze({
  docker: "https://docker.example.test:2376",
  kubernetes: "https://kubernetes.example.test:6443",
  ssh: "ssh://worker.example.test:22",
});

function initialConnection(): SavedAdminConnection {
  const saved = readSavedAdminConnection(window.sessionStorage);
  return {
    endpoint: saved.endpoint || window.location.origin,
    tenantId: saved.tenantId,
    projectId: saved.projectId,
  };
}

function initialTheme(): Theme {
  const saved = window.localStorage.getItem("cloud-agents-admin-theme");
  if (saved === "light" || saved === "dark") return saved;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function statusLabel(status: ConnectionStatus): string {
  if (status === "connected") return "Admin API connected";
  if (status === "connecting") return "Authorizing";
  if (status === "error") return "Connection failed";
  return "Disconnected";
}

function phaseTone(phase: string): string {
  if (phase === "ready" || phase === "complete" || phase === "succeeded") return "success";
  if (
    [
      "probing",
      "provisioning",
      "terminating",
      "pending",
      "revoking",
      "reaping",
      "requested",
      "running",
      "queued",
    ].includes(phase)
  )
    return "running";
  if (phase === "unavailable" || phase === "failed" || phase === "blocked") return "danger";
  return "neutral";
}

function formatTime(value: string | undefined): string {
  if (value === undefined || value === "") return "Never";
  const parsed = new Date(value);
  return Number.isNaN(parsed.valueOf()) ? value : parsed.toLocaleString();
}

function AdminSheet({
  label,
  onClose,
  children,
}: Readonly<{ label: string; onClose: () => void; children: ReactNode }>) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = ref.current;
    if (dialog === null) return;
    if (!dialog.open) {
      dialog.showModal();
      dialog.querySelector<HTMLElement>("[data-sheet-autofocus]")?.focus();
    }
    return () => {
      if (dialog.open) dialog.close();
    };
  }, []);

  return (
    <dialog
      ref={ref}
      className="admin-sheet"
      aria-label={label}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      {children}
    </dialog>
  );
}

async function loadTargetAuthority(
  client: AdminClient,
  connection: SavedAdminConnection,
  preferredTargetId: string,
  signal: AbortSignal,
): Promise<Readonly<{ targets: readonly DeploymentTarget[]; selectedTargetId: string }>> {
  let targets = await listAdminTargets(client, connection.tenantId, connection.projectId, signal);
  const selectedTargetId = targets.some(({ metadata }) => metadata.uid === preferredTargetId)
    ? preferredTargetId
    : (targets[0]?.metadata.uid ?? "");
  if (selectedTargetId !== "") {
    const detail = await client.getAdminDeploymentTarget(
      connection.tenantId,
      connection.projectId,
      selectedTargetId,
      newRequestId(),
      signal,
    );
    targets = replaceTarget(targets, detail.value);
  }
  return Object.freeze({ targets, selectedTargetId });
}

async function loadLeaseAuthority(
  client: AdminClient,
  connection: SavedAdminConnection,
  preferredLeaseId: string,
  signal: AbortSignal,
): Promise<Readonly<{ leases: readonly EnvironmentLease[]; selectedLeaseId: string }>> {
  let leases = await listAdminLeases(client, connection.tenantId, connection.projectId, signal);
  const selectedLeaseId = leases.some(({ metadata }) => metadata.uid === preferredLeaseId)
    ? preferredLeaseId
    : (leases[0]?.metadata.uid ?? "");
  if (selectedLeaseId !== "") {
    const detail = await client.getAdminEnvironmentLease(
      connection.tenantId,
      connection.projectId,
      selectedLeaseId,
      newRequestId(),
      signal,
    );
    leases = replaceLease(leases, detail.value);
  }
  return Object.freeze({ leases, selectedLeaseId });
}

async function loadTargetActivity(
  client: AdminClient,
  connection: SavedAdminConnection,
  targetId: string,
  signal: AbortSignal,
): Promise<
  Readonly<{
    operations: readonly MaintenanceOperation[];
    audit: readonly AdminAuditEvent[];
  }>
> {
  const [operations, audit] = await Promise.all([
    listAdminTargetOperations(client, connection.tenantId, connection.projectId, targetId, signal),
    listAdminTargetAuditEvents(client, connection.tenantId, connection.projectId, targetId, signal),
  ]);
  return Object.freeze({ operations, audit });
}

export function App() {
  const [theme, setTheme] = useState<Theme>(initialTheme);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const [connection, setConnection] = useState(initialConnection);
  const [token, setToken] = useState("");
  const [status, setStatus] = useState<ConnectionStatus>("disconnected");
  const [client, setClient] = useState<AdminClient | null>(null);
  const [targets, setTargets] = useState<readonly DeploymentTarget[]>(Object.freeze([]));
  const [selectedTargetId, setSelectedTargetId] = useState("");
  const [targetOperations, setTargetOperations] = useState<readonly MaintenanceOperation[]>(
    Object.freeze([]),
  );
  const [targetAudit, setTargetAudit] = useState<readonly AdminAuditEvent[]>(Object.freeze([]));
  const [cleanupPreview, setCleanupPreview] = useState<DeploymentTargetCleanupPreview | null>(null);
  const [leases, setLeases] = useState<readonly EnvironmentLease[]>(Object.freeze([]));
  const [selectedLeaseId, setSelectedLeaseId] = useState("");
  const [page, setPage] = useState<Page>("overview");
  const [query, setQuery] = useState("");
  const [targetDetailOpen, setTargetDetailOpen] = useState(false);
  const [leaseDetailOpen, setLeaseDetailOpen] = useState(false);
  const [registering, setRegistering] = useState(false);
  const [busy, setBusy] = useState<BusyOperation | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [targetForm, setTargetForm] = useState({
    targetId: "",
    targetName: "",
    targetKind: "docker" as TargetKind,
    endpoint: "",
    credentialRef: "",
  });
  const requestRef = useRef<AbortController | null>(null);
  const busyRef = useRef(false);
  const pendingKeysRef = useRef(new Map<string, string>());
  const profileMenuRef = useRef<HTMLDetailsElement>(null);

  const connected = status === "connected" && client !== null;
  const selectedTarget = targets.find(({ metadata }) => metadata.uid === selectedTargetId);
  const selectedCleanupPreview =
    selectedTarget !== undefined &&
    cleanupPreview?.metadata.uid === selectedTarget?.metadata.uid &&
    cleanupPreview.metadata.resourceVersion === selectedTarget.metadata.resourceVersion
      ? cleanupPreview
      : null;
  const selectedLease = leases.find(({ metadata }) => metadata.uid === selectedLeaseId);
  const readyCount = targets.filter(({ spec }) => spec.observedPhase === "ready").length;
  const unavailableCount = targets.filter(
    ({ spec }) => spec.observedPhase === "unavailable",
  ).length;
  const attentionCount = targets.length - readyCount;
  const readyLeaseCount = leases.filter(({ spec }) => spec.observedPhase === "ready").length;
  const leaseAttentionCount = leases.filter(
    ({ spec }) => spec.observedPhase === "failed" || spec.cleanupPhase === "blocked",
  ).length;
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const visibleTargets =
    normalizedQuery === ""
      ? targets
      : targets.filter(({ metadata, spec }) =>
          [metadata.uid, metadata.name, spec.targetKind, spec.observedPhase].some((value) =>
            value.toLocaleLowerCase().includes(normalizedQuery),
          ),
        );
  const visibleLeases =
    normalizedQuery === ""
      ? leases
      : leases.filter(({ metadata, spec }) =>
          [
            metadata.uid,
            metadata.name,
            spec.environmentId,
            spec.observedPhase,
            spec.cleanupPhase,
          ].some((value) => value.toLocaleLowerCase().includes(normalizedQuery)),
        );

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
    window.localStorage.setItem("cloud-agents-admin-theme", theme);
  }, [theme]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && profileMenuRef.current?.open) {
        profileMenuRef.current.open = false;
        profileMenuRef.current.querySelector<HTMLElement>("summary")?.focus();
        return;
      }
      if (event.key.toLocaleLowerCase() !== "b" || (!event.metaKey && !event.ctrlKey)) return;
      event.preventDefault();
      setSidebarOpen((open) => !open);
    };
    const closeProfileMenu = (event: PointerEvent) => {
      const menu = profileMenuRef.current;
      if (menu?.open && event.target instanceof Node && !menu.contains(event.target)) {
        menu.open = false;
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    window.addEventListener("pointerdown", closeProfileMenu);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      window.removeEventListener("pointerdown", closeProfileMenu);
    };
  }, []);

  useEffect(
    () => () => {
      requestRef.current?.abort();
    },
    [],
  );

  useEffect(() => {
    if (
      !connected ||
      client === null ||
      (!targets.some(({ spec }) => spec.observedPhase === "probing") &&
        !leases.some(
          ({ spec }) =>
            spec.observedPhase === "provisioning" ||
            spec.observedPhase === "terminating" ||
            ["pending", "revoking", "reaping"].includes(spec.cleanupPhase),
        ))
    )
      return;
    const controller = new AbortController();
    const interval = window.setInterval(() => {
      if (document.visibilityState !== "visible" || busyRef.current) return;
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
      void Promise.all([
        loadTargetAuthority(client, connection, selectedTargetId, signal),
        loadLeaseAuthority(client, connection, selectedLeaseId, signal),
      ])
        .then(([loadedTargets, loadedLeases]) => {
          setTargets(loadedTargets.targets);
          setSelectedTargetId(loadedTargets.selectedTargetId);
          setLeases(loadedLeases.leases);
          setSelectedLeaseId(loadedLeases.selectedLeaseId);
        })
        .catch((cause: unknown) => {
          if (!controller.signal.aborted) setError(adminErrorMessage(cause));
        });
    }, 5_000);
    return () => {
      controller.abort();
      window.clearInterval(interval);
    };
  }, [client, connected, connection, leases, selectedLeaseId, selectedTargetId, targets]);

  function updateConnection(field: keyof SavedAdminConnection, value: string) {
    setConnection((current) => ({ ...current, [field]: value }));
  }

  function navigate(nextPage: Page) {
    setPage(nextPage);
    setQuery("");
    setMobileNavOpen(false);
    setTargetDetailOpen(false);
    setLeaseDetailOpen(false);
  }

  function disconnect() {
    requestRef.current?.abort();
    requestRef.current = null;
    setClient(null);
    setToken("");
    setTargets(Object.freeze([]));
    setSelectedTargetId("");
    setTargetOperations(Object.freeze([]));
    setTargetAudit(Object.freeze([]));
    setCleanupPreview(null);
    setLeases(Object.freeze([]));
    setSelectedLeaseId("");
    setTargetDetailOpen(false);
    setLeaseDetailOpen(false);
    setMobileNavOpen(false);
    setBusy(null);
    setError("");
    setNotice("");
    setStatus("disconnected");
  }

  async function connect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (status === "connecting") return;
    const nextConnection = {
      endpoint: connection.endpoint.trim().replace(/\/+$/u, ""),
      tenantId: connection.tenantId.trim(),
      projectId: connection.projectId.trim(),
    };
    const bearer = token.trim();
    if (Object.values(nextConnection).some((value) => value === "") || bearer === "") {
      setStatus("error");
      setError("Endpoint, tenant, project, and admin bearer token are required.");
      return;
    }
    const controller = new AbortController();
    requestRef.current = controller;
    setStatus("connecting");
    setError("");
    try {
      const nextClient = createHTTPClient(nextConnection.endpoint, bearer);
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
      const [loadedTargets, loadedLeases] = await Promise.all([
        loadTargetAuthority(nextClient, nextConnection, selectedTargetId, signal),
        loadLeaseAuthority(nextClient, nextConnection, selectedLeaseId, signal),
      ]);
      setClient(nextClient);
      setConnection(nextConnection);
      setTargets(loadedTargets.targets);
      setSelectedTargetId(loadedTargets.selectedTargetId);
      setTargetOperations(Object.freeze([]));
      setTargetAudit(Object.freeze([]));
      setLeases(loadedLeases.leases);
      setSelectedLeaseId(loadedLeases.selectedLeaseId);
      writeSavedAdminConnection(window.sessionStorage, nextConnection);
      setToken("");
      setStatus("connected");
    } catch (cause) {
      setClient(null);
      setStatus(controller.signal.aborted ? "disconnected" : "error");
      setError(controller.signal.aborted ? "" : adminErrorMessage(cause));
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
    }
  }

  function idempotencyKey(key: string): string {
    const existing = pendingKeysRef.current.get(key);
    if (existing !== undefined) return existing;
    const created = newIdempotencyKey();
    pendingKeysRef.current.set(key, created);
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
    setNotice("");
    const controller = new AbortController();
    requestRef.current = controller;
    try {
      await operation(AbortSignal.any([controller.signal, AbortSignal.timeout(150_000)]));
      pendingKeysRef.current.delete(key);
      setNotice(`${label} completed.`);
    } catch (cause) {
      setError(adminErrorMessage(cause));
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
      busyRef.current = false;
      setBusy(null);
    }
  }

  function refresh() {
    if (client === null) return;
    void runOperation("refresh", "Authority refresh", async (signal) => {
      const [loadedTargets, loadedLeases] = await Promise.all([
        loadTargetAuthority(client, connection, selectedTargetId, signal),
        loadLeaseAuthority(client, connection, selectedLeaseId, signal),
      ]);
      setTargets(loadedTargets.targets);
      setSelectedTargetId(loadedTargets.selectedTargetId);
      setLeases(loadedLeases.leases);
      setSelectedLeaseId(loadedLeases.selectedLeaseId);
      if (targetDetailOpen && loadedTargets.selectedTargetId !== "") {
        const activity = await loadTargetActivity(
          client,
          connection,
          loadedTargets.selectedTargetId,
          signal,
        );
        setTargetOperations(activity.operations);
        setTargetAudit(activity.audit);
      }
    });
  }

  function selectTarget(targetId: string) {
    setTargetDetailOpen(true);
    if (client === null) return;
    setCleanupPreview(null);
    setTargetOperations(Object.freeze([]));
    setTargetAudit(Object.freeze([]));
    setSelectedTargetId(targetId);
    void runOperation(`get:${targetId}`, "Target detail refresh", async (signal) => {
      const [result, activity] = await Promise.all([
        client.getAdminDeploymentTarget(
          connection.tenantId,
          connection.projectId,
          targetId,
          newRequestId(),
          signal,
        ),
        loadTargetActivity(client, connection, targetId, signal),
      ]);
      setTargets((current) => replaceTarget(current, result.value));
      setTargetOperations(activity.operations);
      setTargetAudit(activity.audit);
    });
  }

  function selectLease(leaseId: string) {
    setLeaseDetailOpen(true);
    if (client === null || leaseId === selectedLeaseId) {
      setSelectedLeaseId(leaseId);
      return;
    }
    setSelectedLeaseId(leaseId);
    void runOperation(`get-lease:${leaseId}`, "Lease detail refresh", async (signal) => {
      const result = await client.getAdminEnvironmentLease(
        connection.tenantId,
        connection.projectId,
        leaseId,
        newRequestId(),
        signal,
      );
      setLeases((current) => replaceLease(current, result.value));
    });
  }

  function registerTarget(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null) return;
    const body: DeploymentTargetRegisterRequest = {
      targetId: targetForm.targetId.trim(),
      targetName: targetForm.targetName.trim(),
      targetKind: targetForm.targetKind,
      endpoint: targetForm.endpoint.trim(),
      credentialRef: targetForm.credentialRef.trim(),
    };
    const key = `register:${body.targetId}`;
    void runOperation(key, `Register ${body.targetName}`, async (signal) => {
      const result = await client.registerAdminDeploymentTarget(
        connection.tenantId,
        connection.projectId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      setTargets((current) => replaceTarget(current, result.value));
      setSelectedTargetId(result.value.metadata.uid);
      setCleanupPreview(null);
      setTargetForm({
        targetId: "",
        targetName: "",
        targetKind: "docker",
        endpoint: "",
        credentialRef: "",
      });
      setRegistering(false);
    });
  }

  function probeTarget() {
    if (client === null || selectedTarget === undefined) return;
    const target = selectedTarget;
    const key = `probe:${target.metadata.uid}:${target.spec.generation}`;
    void runOperation(key, `Probe ${target.metadata.name}`, async (signal) => {
      const result = await client.probeAdminDeploymentTarget(
        connection.tenantId,
        connection.projectId,
        target.metadata.uid,
        newRequestId(),
        idempotencyKey(key),
        { expectedGeneration: target.spec.generation },
        signal,
      );
      setTargets((current) => replaceTarget(current, result.value));
      const activity = await loadTargetActivity(
        client,
        connection,
        result.value.metadata.uid,
        signal,
      );
      setTargetOperations(activity.operations);
      setTargetAudit(activity.audit);
      setCleanupPreview(null);
    });
  }

  function previewTargetCleanup() {
    if (client === null || selectedTarget === undefined) return;
    const target = selectedTarget;
    void runOperation(
      `cleanup-preview:${target.metadata.uid}:${target.metadata.resourceVersion}`,
      `Preview cleanup for ${target.metadata.name}`,
      async (signal) => {
        const result = await client.previewAdminDeploymentTargetCleanup(
          connection.tenantId,
          connection.projectId,
          target.metadata.uid,
          newRequestId(),
          signal,
        );
        setCleanupPreview(result.value);
      },
    );
  }

  if (!connected) {
    return (
      <main className="connect-view">
        <button
          className="button outline connect-theme"
          type="button"
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
        >
          {theme === "dark" ? "Light mode" : "Dark mode"}
        </button>
        <section className="connect-card" aria-labelledby="connect-title">
          <div className="brand-lockup">
            <span className="brand-mark" aria-hidden="true">
              CA
            </span>
            <span>
              <strong>Cloud Agents</strong>
              <small>Admin Console</small>
            </span>
          </div>
          <div className="eyebrow">Control Plane · Admin API</div>
          <h1 id="connect-title">Operate Cloud Agents infrastructure.</h1>
          <p className="lede">
            Connect with an admin-scoped token. The bearer stays in memory and every resource read
            or action runs through Control Plane.
          </p>
          <form className="connect-form" onSubmit={connect}>
            <label>
              <span>Control Plane endpoint</span>
              <input
                type="url"
                value={connection.endpoint}
                onChange={(event) => updateConnection("endpoint", event.target.value)}
                placeholder="https://agents.example.com"
                autoComplete="url"
                required
                disabled={status === "connecting"}
              />
              <small>
                Use this origin when <code>/v1/admin</code> is reverse proxied.
              </small>
            </label>
            <div className="form-row">
              <label>
                <span>Tenant ID</span>
                <input
                  value={connection.tenantId}
                  onChange={(event) => updateConnection("tenantId", event.target.value)}
                  placeholder="tenant-local"
                  autoComplete="off"
                  spellCheck={false}
                  required
                />
              </label>
              <label>
                <span>Project ID</span>
                <input
                  value={connection.projectId}
                  onChange={(event) => updateConnection("projectId", event.target.value)}
                  placeholder="project-local"
                  autoComplete="off"
                  spellCheck={false}
                  required
                />
              </label>
            </div>
            <label>
              <span>Admin bearer token</span>
              <input
                type="password"
                value={token}
                onChange={(event) => setToken(event.target.value)}
                placeholder="Required scopes: targets.* and leases.list/get"
                autoComplete="off"
                spellCheck={false}
                required
                disabled={status === "connecting"}
              />
              <small>Never written to local or session storage.</small>
            </label>
            {error !== "" ? (
              <p className="banner danger" role="alert">
                {error}
              </p>
            ) : null}
            <button
              className="button primary wide"
              type="submit"
              disabled={status === "connecting"}
            >
              {status === "connecting" ? "Authorizing…" : "Connect to Admin API"}
            </button>
          </form>
          <div className={`connection-state state-${status}`} role="status" aria-live="polite">
            <span className="status-dot" aria-hidden="true" />
            {statusLabel(status)}
          </div>
        </section>
      </main>
    );
  }

  return (
    <div className={`app-shell${sidebarOpen ? "" : " sidebar-collapsed"}`}>
      <button
        className={`mobile-nav-backdrop${mobileNavOpen ? " open" : ""}`}
        type="button"
        aria-label="Close navigation"
        onClick={() => setMobileNavOpen(false)}
      />
      <aside className={`sidebar${mobileNavOpen ? " mobile-open" : ""}`}>
        <div className="brand-lockup sidebar-brand">
          <span className="brand-mark" aria-hidden="true">
            CA
          </span>
          <span>
            <strong>Cloud Agents</strong>
            <small>Admin Console</small>
          </span>
          <button
            className="sidebar-trigger"
            type="button"
            aria-label={sidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
            aria-expanded={sidebarOpen}
            title="Toggle sidebar (⌘/Ctrl+B)"
            onClick={() => setSidebarOpen((open) => !open)}
          >
            ◧
          </button>
        </div>
        <nav aria-label="Admin resources">
          <button
            className={page === "overview" ? "active" : ""}
            onClick={() => navigate("overview")}
            title="Overview"
          >
            <span aria-hidden="true">⌁</span> <span className="nav-label">Overview</span>
          </button>
          <button
            className={page === "targets" ? "active" : ""}
            onClick={() => navigate("targets")}
            title="Deployment Targets"
          >
            <span aria-hidden="true">◎</span> <span className="nav-label">Deployment Targets</span>
            <b>{targets.length}</b>
          </button>
          <button
            className={page === "leases" ? "active" : ""}
            onClick={() => navigate("leases")}
            title="Environment Leases"
          >
            <span aria-hidden="true">◇</span> <span className="nav-label">Environment Leases</span>
            <b>{leases.length}</b>
          </button>
        </nav>
        <div className="sidebar-boundary">
          <small>Admin boundary</small>
          <p>No conversations, prompts, workspace files, artifacts, or secret bytes.</p>
        </div>
      </aside>

      <section className="app-main">
        <header className="topbar">
          <button
            className="mobile-nav-trigger"
            type="button"
            aria-label="Open navigation"
            aria-expanded={mobileNavOpen}
            onClick={() => setMobileNavOpen(true)}
          >
            ◧
          </button>
          <div className="breadcrumbs">
            <strong>{connection.projectId}</strong>
            <small>{connection.tenantId}</small>
          </div>
          <div className="topbar-context">
            <span title={connection.endpoint}>{connection.endpoint}</span>
            <span className="live">
              <i /> Admin API
            </span>
          </div>
          <details ref={profileMenuRef} className="profile-menu">
            <summary className="button outline compact">Admin</summary>
            <div className="dropdown-menu">
              <div className="dropdown-context">
                <strong>{connection.projectId}</strong>
                <small>{connection.tenantId}</small>
              </div>
              <button
                type="button"
                onClick={(event) => {
                  setTheme(theme === "dark" ? "light" : "dark");
                  event.currentTarget.closest("details")?.removeAttribute("open");
                }}
              >
                {theme === "dark" ? "Light mode" : "Dark mode"}
              </button>
              <button type="button" onClick={disconnect}>
                Disconnect
              </button>
            </div>
          </details>
        </header>

        <main className="content">
          <div className="page-heading">
            <div>
              <h1>
                {page === "overview"
                  ? "Operations overview"
                  : page === "targets"
                    ? "Deployment targets"
                    : "Environment leases"}
              </h1>
              <p>
                {page === "overview"
                  ? "Current infrastructure authority for the selected tenant and project."
                  : page === "targets"
                    ? "Register and operate Docker, Kubernetes, and SSH execution capacity."
                    : "Inspect server-authoritative environment lifecycle and cleanup state."}
              </p>
            </div>
            <div className="heading-actions">
              <button
                className="button outline"
                type="button"
                onClick={refresh}
                disabled={busy !== null}
              >
                Refresh
              </button>
              {page !== "leases" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={() => {
                    navigate("targets");
                    setRegistering(true);
                  }}
                  disabled={busy !== null}
                >
                  Register target
                </button>
              ) : null}
            </div>
          </div>

          {busy !== null ? (
            <div className="banner running" role="status" aria-live="polite">
              <span className="spinner" aria-hidden="true" /> {busy.label}…
              <button type="button" onClick={() => requestRef.current?.abort()}>
                Cancel wait
              </button>
            </div>
          ) : null}
          {error !== "" ? (
            <div className="banner danger" role="alert">
              {error}
            </div>
          ) : null}
          {notice !== "" ? (
            <div className="banner success" role="status">
              {notice}
            </div>
          ) : null}

          {page === "overview" ? (
            <>
              <section className="metric-grid" aria-label="Infrastructure overview">
                <article className="metric-card">
                  <small>Total targets</small>
                  <strong>{targets.length}</strong>
                  <span>Across Docker, Kubernetes, and SSH</span>
                </article>
                <article className="metric-card warning-accent">
                  <small>Target attention</small>
                  <strong>{attentionCount}</strong>
                  <span>
                    {readyCount} ready · {unavailableCount} unavailable
                  </span>
                </article>
                <article className="metric-card success-accent">
                  <small>Environment leases</small>
                  <strong>{leases.length}</strong>
                  <span>{readyLeaseCount} currently ready</span>
                </article>
                <article className="metric-card warning-accent">
                  <small>Lease attention</small>
                  <strong>{leaseAttentionCount}</strong>
                  <span>Failed lifecycle or blocked cleanup</span>
                </article>
              </section>
              <section className="panel overview-panel">
                <div className="panel-heading">
                  <div>
                    <h2>Target health</h2>
                    <p>Live resources returned by the Admin API.</p>
                  </div>
                  <button className="text-button" type="button" onClick={() => navigate("targets")}>
                    View all targets →
                  </button>
                </div>
                <TargetTable
                  targets={targets.slice(0, 6)}
                  selectedTargetId={selectedTargetId}
                  onSelect={(targetId) => {
                    navigate("targets");
                    selectTarget(targetId);
                  }}
                />
              </section>
              <section className="panel overview-panel">
                <div className="panel-heading">
                  <div>
                    <h2>Lease lifecycle</h2>
                    <p>Desired, observed, and cleanup phases from Control Plane.</p>
                  </div>
                  <button className="text-button" type="button" onClick={() => navigate("leases")}>
                    View all leases →
                  </button>
                </div>
                <LeaseTable
                  leases={leases.slice(0, 6)}
                  selectedLeaseId={selectedLeaseId}
                  onSelect={(leaseId) => {
                    navigate("leases");
                    selectLease(leaseId);
                  }}
                />
              </section>
            </>
          ) : page === "targets" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label="Search deployment targets"
                  placeholder="Search by ID or name"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">targets.list · {visibleTargets.length}</span>
              </div>
              <div className="panel target-list-panel">
                <TargetTable
                  targets={visibleTargets}
                  selectedTargetId={selectedTargetId}
                  onSelect={selectTarget}
                />
              </div>
            </section>
          ) : (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label="Search environment leases"
                  placeholder="Search by ID or environment"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">leases.list · {visibleLeases.length}</span>
              </div>
              <div className="panel target-list-panel">
                <LeaseTable
                  leases={visibleLeases}
                  selectedLeaseId={selectedLeaseId}
                  onSelect={selectLease}
                />
              </div>
            </section>
          )}
        </main>
      </section>

      {targetDetailOpen && selectedTarget !== undefined ? (
        <AdminSheet
          label={`Deployment target ${selectedTarget.metadata.name}`}
          onClose={() => setTargetDetailOpen(false)}
        >
          <aside className="detail-panel" aria-label="Selected deployment target">
            <button
              className="sheet-close"
              type="button"
              aria-label="Close"
              onClick={() => setTargetDetailOpen(false)}
            >
              ×
            </button>
            <TargetDetail
              target={selectedTarget}
              operations={targetOperations}
              audit={targetAudit}
              cleanupPreview={selectedCleanupPreview}
              onProbe={probeTarget}
              onPreviewCleanup={previewTargetCleanup}
              disabled={busy !== null}
            />
          </aside>
        </AdminSheet>
      ) : null}

      {leaseDetailOpen && selectedLease !== undefined ? (
        <AdminSheet
          label={`Environment lease ${selectedLease.metadata.name}`}
          onClose={() => setLeaseDetailOpen(false)}
        >
          <aside className="detail-panel" aria-label="Selected environment lease">
            <button
              className="sheet-close"
              type="button"
              aria-label="Close"
              onClick={() => setLeaseDetailOpen(false)}
            >
              ×
            </button>
            <LeaseDetail lease={selectedLease} />
          </aside>
        </AdminSheet>
      ) : null}

      {registering ? (
        <AdminSheet label="Register deployment target" onClose={() => setRegistering(false)}>
          <section className="dialog" aria-labelledby="register-title">
            <div className="panel-heading">
              <div>
                <div className="eyebrow">targets.create</div>
                <h2 id="register-title">Register deployment target</h2>
                <p>References resolve server-side; credential bytes never enter the browser.</p>
              </div>
              <button
                className="icon-button"
                type="button"
                aria-label="Close"
                onClick={() => setRegistering(false)}
              >
                ×
              </button>
            </div>
            <form className="resource-form" onSubmit={registerTarget}>
              <div className="form-row">
                <label>
                  <span>Target ID</span>
                  <input
                    value={targetForm.targetId}
                    onChange={(event) =>
                      setTargetForm({ ...targetForm, targetId: event.target.value })
                    }
                    placeholder="docker-primary"
                    maxLength={128}
                    required
                    autoFocus
                    data-sheet-autofocus
                    spellCheck={false}
                  />
                </label>
                <label>
                  <span>Display name</span>
                  <input
                    value={targetForm.targetName}
                    onChange={(event) =>
                      setTargetForm({ ...targetForm, targetName: event.target.value })
                    }
                    placeholder="docker-primary"
                    maxLength={128}
                    required
                  />
                </label>
              </div>
              <label>
                <span>Target kind</span>
                <select
                  value={targetForm.targetKind}
                  onChange={(event) =>
                    setTargetForm({ ...targetForm, targetKind: event.target.value as TargetKind })
                  }
                >
                  <option value="docker">Docker API</option>
                  <option value="kubernetes">Kubernetes API</option>
                  <option value="ssh">SSH host</option>
                </select>
              </label>
              <label>
                <span>Endpoint</span>
                <input
                  type="url"
                  value={targetForm.endpoint}
                  onChange={(event) =>
                    setTargetForm({ ...targetForm, endpoint: event.target.value })
                  }
                  placeholder={targetEndpointPlaceholder[targetForm.targetKind]}
                  maxLength={2048}
                  required
                  spellCheck={false}
                />
                <small>Control Plane connects to this endpoint; the browser never does.</small>
              </label>
              <label>
                <span>Credential reference</span>
                <input
                  value={targetForm.credentialRef}
                  onChange={(event) =>
                    setTargetForm({ ...targetForm, credentialRef: event.target.value })
                  }
                  placeholder={`${targetForm.targetKind}-primary`}
                  maxLength={128}
                  required
                  spellCheck={false}
                />
                <small>Opaque reference to a deployment-owned credential bundle.</small>
              </label>
              <div className="dialog-actions">
                <button
                  className="button ghost"
                  type="button"
                  onClick={() => setRegistering(false)}
                >
                  Cancel
                </button>
                <button className="button primary" type="submit" disabled={busy !== null}>
                  Register target
                </button>
              </div>
            </form>
          </section>
        </AdminSheet>
      ) : null}
    </div>
  );
}

function TargetTable({
  targets,
  selectedTargetId,
  onSelect,
}: Readonly<{
  targets: readonly DeploymentTarget[];
  selectedTargetId: string;
  onSelect: (targetId: string) => void;
}>) {
  if (targets.length === 0)
    return <div className="table-empty">No deployment targets are registered in this project.</div>;
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Kind</th>
            <th>Status</th>
            <th>Generation</th>
            <th>Last probe</th>
            <th aria-label="Actions" />
          </tr>
        </thead>
        <tbody>
          {targets.map((target) => (
            <tr
              key={target.metadata.uid}
              className={target.metadata.uid === selectedTargetId ? "selected" : ""}
              onClick={() => onSelect(target.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(target.metadata.uid)}>
                  <strong>{target.metadata.name}</strong>
                  <small>{target.metadata.uid}</small>
                </button>
              </td>
              <td>
                <span className="kind-badge">{target.spec.targetKind}</span>
              </td>
              <td>
                <span className={`phase ${phaseTone(target.spec.observedPhase)}`}>
                  <i />
                  {target.spec.observedPhase}
                </span>
              </td>
              <td className="mono">g{target.spec.generation}</td>
              <td>{formatTime(target.spec.lastProbeAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={`View ${target.metadata.name}`}
                  onClick={() => onSelect(target.metadata.uid)}
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

function LeaseTable({
  leases,
  selectedLeaseId,
  onSelect,
}: Readonly<{
  leases: readonly EnvironmentLease[];
  selectedLeaseId: string;
  onSelect: (leaseId: string) => void;
}>) {
  if (leases.length === 0)
    return <div className="table-empty">No environment leases exist in this project.</div>;
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Observed</th>
            <th>Cleanup</th>
            <th>Generation</th>
            <th>Expires</th>
            <th aria-label="Actions" />
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
                  {lease.spec.observedPhase}
                </span>
              </td>
              <td>
                <span className={`phase ${phaseTone(lease.spec.cleanupPhase)}`}>
                  <i />
                  {lease.spec.cleanupPhase}
                </span>
              </td>
              <td className="mono">g{lease.spec.generation}</td>
              <td>{formatTime(lease.spec.expiresAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={`View ${lease.metadata.name}`}
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

function TargetDetail({
  target,
  operations,
  audit,
  cleanupPreview,
  onProbe,
  onPreviewCleanup,
  disabled,
}: Readonly<{
  target: DeploymentTarget;
  operations: readonly MaintenanceOperation[];
  audit: readonly AdminAuditEvent[];
  cleanupPreview: DeploymentTargetCleanupPreview | null;
  onProbe: () => void;
  onPreviewCleanup: () => void;
  disabled: boolean;
}>) {
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          {target.spec.targetKind.slice(0, 1).toUpperCase()}
        </div>
        <div>
          <div className="eyebrow">{target.spec.targetKind} target</div>
          <h2>{target.metadata.name}</h2>
          <span className={`phase ${phaseTone(target.spec.observedPhase)}`}>
            <i />
            {target.spec.observedPhase}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>Target ID</dt>
          <dd className="mono">{target.metadata.uid}</dd>
        </div>
        <div>
          <dt>Endpoint</dt>
          <dd className="mono break">{target.spec.endpoint}</dd>
        </div>
        <div>
          <dt>Credential ref</dt>
          <dd className="mono">{target.spec.credentialRef}</dd>
        </div>
        <div>
          <dt>Generation</dt>
          <dd className="mono">{target.spec.generation}</dd>
        </div>
        <div>
          <dt>Resource version</dt>
          <dd className="mono">{target.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>Runtime API</dt>
          <dd>{target.spec.apiVersion || "Not observed"}</dd>
        </div>
        <div>
          <dt>Engine</dt>
          <dd>{target.spec.engineVersion || "Not observed"}</dd>
        </div>
        <div>
          <dt>Platform</dt>
          <dd>
            {[target.spec.os, target.spec.architecture].filter(Boolean).join(" / ") ||
              "Not observed"}
          </dd>
        </div>
        <div>
          <dt>Last probe</dt>
          <dd>{formatTime(target.spec.lastProbeAt)}</dd>
        </div>
        {target.spec.stableErrorCode !== "" ? (
          <div>
            <dt>Stable error</dt>
            <dd className="danger-text">{target.spec.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
      <section className="action-block">
        <div>
          <h3>Probe target</h3>
          <p>
            Checks connectivity from Control Plane using expected generation{" "}
            {target.spec.generation}.
          </p>
        </div>
        <button
          className="button primary"
          type="button"
          onClick={onProbe}
          disabled={disabled || target.spec.observedPhase === "probing"}
        >
          Run probe
        </button>
      </section>
      <section className="activity-block" aria-labelledby="target-operations-title">
        <div className="activity-heading">
          <h3 id="target-operations-title">Operations</h3>
          <span className="scope-chip">operations.list · {operations.length}</span>
        </div>
        {operations.length === 0 ? (
          <p className="activity-empty">No durable operations were recorded for this target.</p>
        ) : (
          <ol className="activity-list">
            {operations.map((operation) => (
              <li key={operation.operationId}>
                <div>
                  <strong>{operation.action}</strong>
                  <span className={`phase ${phaseTone(operation.state)}`}>
                    <i /> {operation.state}
                  </span>
                </div>
                <p>{operation.impactSummary}</p>
                <small className="mono">
                  {operation.operationId} · g{operation.resourceGeneration} ·{" "}
                  {operation.currentStep}
                </small>
                <small>{formatTime(operation.updatedAt)}</small>
              </li>
            ))}
          </ol>
        )}
      </section>
      <section className="activity-block" aria-labelledby="target-audit-title">
        <div className="activity-heading">
          <h3 id="target-audit-title">Audit</h3>
          <span className="scope-chip">audit.list · {audit.length}</span>
        </div>
        {audit.length === 0 ? (
          <p className="activity-empty">No audit events were recorded for this target.</p>
        ) : (
          <ol className="activity-list compact">
            {audit.map((event) => (
              <li key={event.eventId}>
                <div>
                  <strong>{event.action}</strong>
                  <span className={`phase ${phaseTone(event.result)}`}>
                    <i /> {event.result}
                  </span>
                </div>
                <small className="mono break">actor {event.actor}</small>
                <small className="mono">
                  {event.requestId} · {formatTime(event.occurredAt)}
                </small>
              </li>
            ))}
          </ol>
        )}
      </section>
      <section className="action-block cleanup-preview-block">
        <div>
          <h3>Cleanup impact</h3>
          <p>Reads platform-owned resources from this target without deleting them.</p>
        </div>
        <button
          className="button ghost"
          type="button"
          onClick={onPreviewCleanup}
          disabled={disabled}
        >
          Preview cleanup
        </button>
        {cleanupPreview === null ? null : (
          <div className="cleanup-preview" aria-live="polite">
            <div className="cleanup-preview-summary">
              <span className={`phase ${cleanupPreview.spec.canCleanup ? "success" : "danger"}`}>
                <i />
                {cleanupPreview.spec.canCleanup
                  ? "No active Lease blockers"
                  : "Blocked by active Lease"}
              </span>
              <span className="mono">
                g{cleanupPreview.spec.expectedGeneration} · rv
                {cleanupPreview.spec.expectedResourceVersion}
              </span>
            </div>
            {cleanupPreview.spec.workers.length === 0 ? (
              <p>No platform-owned Workers were found.</p>
            ) : (
              cleanupPreview.spec.workers.map((worker) => (
                <article
                  className="cleanup-worker"
                  key={`${worker.workerName}:${worker.leaseGeneration}`}
                >
                  <div>
                    <strong className="mono">{worker.workerName}</strong>
                    <span
                      className={`phase ${worker.disposition === "blocked" ? "danger" : "success"}`}
                    >
                      <i /> {worker.disposition}
                    </span>
                  </div>
                  <small className="mono">
                    {worker.leaseId} · g{worker.leaseGeneration}
                  </small>
                  <ul>
                    {worker.resources.map((resource) => (
                      <li key={`${resource.resourceKind}:${resource.resourceName}`}>
                        <span>{resource.resourceKind}</span>
                        <code>{resource.resourceName}</code>
                      </li>
                    ))}
                  </ul>
                </article>
              ))
            )}
            <p className="preview-boundary">
              Cleanup preview is read-only; execution is not enabled in this slice.
            </p>
          </div>
        )}
      </section>
    </>
  );
}

function LeaseDetail({ lease }: Readonly<{ lease: EnvironmentLease }>) {
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          L
        </div>
        <div>
          <div className="eyebrow">Environment lease</div>
          <h2>{lease.metadata.name}</h2>
          <span className={`phase ${phaseTone(lease.spec.observedPhase)}`}>
            <i />
            {lease.spec.observedPhase}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>Lease ID</dt>
          <dd className="mono">{lease.metadata.uid}</dd>
        </div>
        <div>
          <dt>Environment ID</dt>
          <dd className="mono">{lease.spec.environmentId}</dd>
        </div>
        <div>
          <dt>Target</dt>
          <dd className="mono">{lease.spec.targetId ?? "Legacy lease"}</dd>
        </div>
        <div>
          <dt>Desired phase</dt>
          <dd>{lease.spec.desiredPhase}</dd>
        </div>
        <div>
          <dt>Cleanup phase</dt>
          <dd className={lease.spec.cleanupPhase === "blocked" ? "danger-text" : ""}>
            {lease.spec.cleanupPhase}
          </dd>
        </div>
        <div>
          <dt>Generation</dt>
          <dd className="mono">{lease.spec.generation}</dd>
        </div>
        <div>
          <dt>Resource version</dt>
          <dd className="mono">{lease.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>Release digest</dt>
          <dd className="mono break">{lease.spec.releaseDigest}</dd>
        </div>
        <div>
          <dt>CPU / memory</dt>
          <dd>
            {lease.spec.cpuLimitMillis === undefined
              ? "Not bound"
              : `${lease.spec.cpuLimitMillis} mCPU / ${Math.round((lease.spec.memoryLimitBytes ?? 0) / 1_048_576)} MiB`}
          </dd>
        </div>
        <div>
          <dt>Provider credential ref</dt>
          <dd className="mono">{lease.spec.providerCredentialRef ?? "Legacy lease"}</dd>
        </div>
        <div>
          <dt>Worker endpoint</dt>
          <dd className="mono break">{lease.spec.workerEndpoint ?? "Not ready"}</dd>
        </div>
        <div>
          <dt>Expires</dt>
          <dd>{formatTime(lease.spec.expiresAt)}</dd>
        </div>
        <div>
          <dt>Updated</dt>
          <dd>{formatTime(lease.metadata.updatedAt)}</dd>
        </div>
        {lease.spec.stableErrorCode !== undefined && lease.spec.stableErrorCode !== "" ? (
          <div>
            <dt>Stable error</dt>
            <dd className="danger-text">{lease.spec.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
    </>
  );
}
