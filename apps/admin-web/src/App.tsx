import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import {
  createHTTPClient,
  type AdminAuditEvent,
  type DeploymentTarget,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetRegisterRequest,
  type EnvironmentLease,
  type EnvironmentProfile,
  type EnvironmentProfileCreateRequest,
  type MaintenanceOperation,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  adminErrorMessage,
  cleanupRequestFromPreview,
  listAdminLeases,
  listAdminProfileAuditEvents,
  listAdminProfiles,
  listAdminTargetAuditEvents,
  listAdminTargetOperations,
  listAdminTargets,
  newIdempotencyKey,
  newRequestId,
  readSavedAdminConnection,
  replaceLease,
  replaceProfile,
  replaceTarget,
  writeSavedAdminConnection,
  type AdminClient,
  type SavedAdminConnection,
} from "./admin";

type ConnectionStatus = "disconnected" | "connecting" | "connected" | "error";
type Page = "overview" | "targets" | "profiles" | "leases";
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
  if (phase === "ready" || phase === "complete" || phase === "succeeded" || phase === "published")
    return "success";
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
  if (phase === "unavailable" || phase === "failed" || phase === "blocked" || phase === "disabled")
    return "danger";
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

async function loadProfileAuthority(
  client: AdminClient,
  connection: SavedAdminConnection,
  preferredProfileVersionId: string,
  signal: AbortSignal,
): Promise<
  Readonly<{ profiles: readonly EnvironmentProfile[]; selectedProfileVersionId: string }>
> {
  let profiles = await listAdminProfiles(client, connection.tenantId, connection.projectId, signal);
  const selectedProfileVersionId = profiles.some(
    ({ metadata }) => metadata.uid === preferredProfileVersionId,
  )
    ? preferredProfileVersionId
    : (profiles[0]?.metadata.uid ?? "");
  const selected = profiles.find(({ metadata }) => metadata.uid === selectedProfileVersionId);
  if (selected !== undefined) {
    const detail = await client.getAdminEnvironmentProfile(
      connection.tenantId,
      connection.projectId,
      selected.spec.profileId,
      selected.spec.version,
      newRequestId(),
      signal,
    );
    profiles = replaceProfile(profiles, detail.value);
  }
  return Object.freeze({ profiles, selectedProfileVersionId });
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

async function loadProfileAudit(
  client: AdminClient,
  connection: SavedAdminConnection,
  profile: EnvironmentProfile,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  return listAdminProfileAuditEvents(
    client,
    connection.tenantId,
    connection.projectId,
    profile.spec.profileId,
    profile.spec.version,
    signal,
  );
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
  const [profiles, setProfiles] = useState<readonly EnvironmentProfile[]>(Object.freeze([]));
  const [selectedProfileVersionId, setSelectedProfileVersionId] = useState("");
  const [profileAudit, setProfileAudit] = useState<readonly AdminAuditEvent[]>(Object.freeze([]));
  const [page, setPage] = useState<Page>("overview");
  const [query, setQuery] = useState("");
  const [targetDetailOpen, setTargetDetailOpen] = useState(false);
  const [cleanupConfirmationOpen, setCleanupConfirmationOpen] = useState(false);
  const [leaseDetailOpen, setLeaseDetailOpen] = useState(false);
  const [registering, setRegistering] = useState(false);
  const [profileDetailOpen, setProfileDetailOpen] = useState(false);
  const [creatingProfile, setCreatingProfile] = useState(false);
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
  const [profileForm, setProfileForm] = useState({
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
  const selectedProfile = profiles.find(
    ({ metadata }) => metadata.uid === selectedProfileVersionId,
  );
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
  const visibleProfiles =
    normalizedQuery === ""
      ? profiles
      : profiles.filter(({ metadata, spec }) =>
          [
            metadata.uid,
            metadata.name,
            spec.profileId,
            String(spec.version),
            spec.status,
            ...spec.providerKinds,
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
    setCleanupConfirmationOpen(false);
    setLeaseDetailOpen(false);
    setProfileDetailOpen(false);
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
    setProfiles(Object.freeze([]));
    setSelectedProfileVersionId("");
    setProfileAudit(Object.freeze([]));
    setTargetDetailOpen(false);
    setCleanupConfirmationOpen(false);
    setLeaseDetailOpen(false);
    setProfileDetailOpen(false);
    setCreatingProfile(false);
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
      const [loadedTargets, loadedLeases, loadedProfiles] = await Promise.all([
        loadTargetAuthority(nextClient, nextConnection, selectedTargetId, signal),
        loadLeaseAuthority(nextClient, nextConnection, selectedLeaseId, signal),
        loadProfileAuthority(nextClient, nextConnection, selectedProfileVersionId, signal),
      ]);
      setClient(nextClient);
      setConnection(nextConnection);
      setTargets(loadedTargets.targets);
      setSelectedTargetId(loadedTargets.selectedTargetId);
      setTargetOperations(Object.freeze([]));
      setTargetAudit(Object.freeze([]));
      setLeases(loadedLeases.leases);
      setSelectedLeaseId(loadedLeases.selectedLeaseId);
      setProfiles(loadedProfiles.profiles);
      setSelectedProfileVersionId(loadedProfiles.selectedProfileVersionId);
      setProfileAudit(Object.freeze([]));
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
      const [loadedTargets, loadedLeases, loadedProfiles] = await Promise.all([
        loadTargetAuthority(client, connection, selectedTargetId, signal),
        loadLeaseAuthority(client, connection, selectedLeaseId, signal),
        loadProfileAuthority(client, connection, selectedProfileVersionId, signal),
      ]);
      setTargets(loadedTargets.targets);
      setSelectedTargetId(loadedTargets.selectedTargetId);
      setLeases(loadedLeases.leases);
      setSelectedLeaseId(loadedLeases.selectedLeaseId);
      setProfiles(loadedProfiles.profiles);
      setSelectedProfileVersionId(loadedProfiles.selectedProfileVersionId);
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
      const profile = loadedProfiles.profiles.find(
        ({ metadata }) => metadata.uid === loadedProfiles.selectedProfileVersionId,
      );
      if (profileDetailOpen && profile !== undefined) {
        setProfileAudit(await loadProfileAudit(client, connection, profile, signal));
      }
    });
  }

  function selectTarget(targetId: string) {
    setTargetDetailOpen(true);
    setCleanupConfirmationOpen(false);
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

  function selectProfile(profileVersionId: string) {
    setProfileDetailOpen(true);
    if (client === null) return;
    const profile = profiles.find(({ metadata }) => metadata.uid === profileVersionId);
    if (profile === undefined) return;
    setSelectedProfileVersionId(profileVersionId);
    setProfileAudit(Object.freeze([]));
    void runOperation(
      `get-profile:${profileVersionId}`,
      "Profile detail refresh",
      async (signal) => {
        const [result, audit] = await Promise.all([
          client.getAdminEnvironmentProfile(
            connection.tenantId,
            connection.projectId,
            profile.spec.profileId,
            profile.spec.version,
            newRequestId(),
            signal,
          ),
          loadProfileAudit(client, connection, profile, signal),
        ]);
        setProfiles((current) => replaceProfile(current, result.value));
        setProfileAudit(audit);
      },
    );
  }

  function createProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null) return;
    const providerKinds: ("codex" | "claudeAgent")[] = [];
    if (profileForm.codex) providerKinds.push("codex");
    if (profileForm.claudeAgent) providerKinds.push("claudeAgent");
    const body: EnvironmentProfileCreateRequest = {
      profileId: profileForm.profileId.trim(),
      profileName: profileForm.profileName.trim(),
      version: Number(profileForm.version),
      description: profileForm.description.trim(),
      providerKinds,
      cpuLimitMillis: Number(profileForm.cpuLimitMillis),
      memoryLimitBytes: Number(profileForm.memoryLimitMiB) * 1_048_576,
      storagePolicyRef: profileForm.storagePolicyRef.trim(),
      networkPolicyRef: profileForm.networkPolicyRef.trim(),
      releaseDigest: profileForm.releaseDigest.trim() as `sha256:${string}`,
      targetRefs: [
        ...new Set(profileForm.targetRefs.split(",").map((value) => value.trim())),
      ].filter(Boolean),
      providerCredentialRef: profileForm.providerCredentialRef.trim(),
    };
    const key = `create-profile:${body.profileId}:${body.version}`;
    void runOperation(key, `Create ${body.profileName} v${body.version}`, async (signal) => {
      const result = await client.createAdminEnvironmentProfile(
        connection.tenantId,
        connection.projectId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      setProfiles((current) => replaceProfile(current, result.value));
      setSelectedProfileVersionId(result.value.metadata.uid);
      setProfileAudit(await loadProfileAudit(client, connection, result.value, signal));
      setProfileDetailOpen(true);
      setProfileForm({
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
      });
      setCreatingProfile(false);
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
        setCleanupConfirmationOpen(true);
      },
    );
  }

  function cleanupTarget() {
    if (
      client === null ||
      selectedTarget === undefined ||
      selectedCleanupPreview === null ||
      !selectedCleanupPreview.spec.canCleanup
    )
      return;
    const target = selectedTarget;
    const preview = selectedCleanupPreview;
    const key = `cleanup:${target.metadata.uid}:${preview.spec.expectedGeneration}:${preview.spec.expectedResourceVersion}:${preview.spec.impactDigest}`;
    setCleanupConfirmationOpen(false);
    void runOperation(key, `Cleanup ${target.metadata.name}`, async (signal) => {
      await client.cleanupAdminDeploymentTarget(
        connection.tenantId,
        connection.projectId,
        target.metadata.uid,
        newRequestId(),
        idempotencyKey(key),
        cleanupRequestFromPreview(preview),
        signal,
      );
      const activity = await loadTargetActivity(client, connection, target.metadata.uid, signal);
      setTargetOperations(activity.operations);
      setTargetAudit(activity.audit);
      setCleanupPreview(null);
    });
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
                placeholder="Required scopes: targets.*, leases.*, profiles.*, audit.list"
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
          <button
            className={page === "profiles" ? "active" : ""}
            onClick={() => navigate("profiles")}
            title="Environment Profiles"
          >
            <span aria-hidden="true">▣</span>{" "}
            <span className="nav-label">Environment Profiles</span>
            <b>{profiles.length}</b>
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
                    : page === "profiles"
                      ? "Environment profiles"
                      : "Environment leases"}
              </h1>
              <p>
                {page === "overview"
                  ? "Current infrastructure authority for the selected tenant and project."
                  : page === "targets"
                    ? "Register and operate Docker, Kubernetes, and SSH execution capacity."
                    : page === "profiles"
                      ? "Create immutable environment versions before publishing them to users."
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
              {page === "profiles" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={() => setCreatingProfile(true)}
                  disabled={busy !== null}
                >
                  Create profile
                </button>
              ) : page !== "leases" ? (
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
          ) : page === "profiles" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label="Search environment profiles"
                  placeholder="Search by ID, name, version, or provider"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">profiles.list · {visibleProfiles.length}</span>
              </div>
              <div className="panel target-list-panel">
                <ProfileTable
                  profiles={visibleProfiles}
                  selectedProfileVersionId={selectedProfileVersionId}
                  onSelect={selectProfile}
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
          onClose={() => {
            setTargetDetailOpen(false);
            setCleanupConfirmationOpen(false);
          }}
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
              onProbe={probeTarget}
              onPreviewCleanup={previewTargetCleanup}
              disabled={busy !== null}
            />
          </aside>
        </AdminSheet>
      ) : null}

      {cleanupConfirmationOpen &&
      selectedTarget !== undefined &&
      selectedCleanupPreview !== null ? (
        <AdminSheet
          label={`Confirm cleanup for ${selectedTarget.metadata.name}`}
          onClose={() => setCleanupConfirmationOpen(false)}
        >
          <CleanupConfirmation
            target={selectedTarget}
            preview={selectedCleanupPreview}
            disabled={busy !== null}
            onClose={() => setCleanupConfirmationOpen(false)}
            onConfirm={cleanupTarget}
          />
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

      {profileDetailOpen && selectedProfile !== undefined ? (
        <AdminSheet
          label={`Environment profile ${selectedProfile.metadata.name}`}
          onClose={() => setProfileDetailOpen(false)}
        >
          <aside className="detail-panel" aria-label="Selected environment profile">
            <button
              className="sheet-close"
              type="button"
              aria-label="Close"
              onClick={() => setProfileDetailOpen(false)}
            >
              ×
            </button>
            <ProfileDetail profile={selectedProfile} audit={profileAudit} />
          </aside>
        </AdminSheet>
      ) : null}

      {creatingProfile ? (
        <AdminSheet label="Create environment profile" onClose={() => setCreatingProfile(false)}>
          <section className="dialog" aria-labelledby="create-profile-title">
            <div className="panel-heading">
              <div>
                <div className="eyebrow">profiles.create</div>
                <h2 id="create-profile-title">Create environment profile</h2>
                <p>Creates one immutable draft version in Control Plane.</p>
              </div>
              <button
                className="icon-button"
                type="button"
                aria-label="Close"
                onClick={() => setCreatingProfile(false)}
              >
                ×
              </button>
            </div>
            <form className="resource-form" onSubmit={createProfile}>
              <div className="form-row">
                <label>
                  <span>Profile ID</span>
                  <input
                    value={profileForm.profileId}
                    onChange={(event) =>
                      setProfileForm({ ...profileForm, profileId: event.target.value })
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
                  <span>Profile name</span>
                  <input
                    value={profileForm.profileName}
                    onChange={(event) =>
                      setProfileForm({ ...profileForm, profileName: event.target.value })
                    }
                    placeholder="development"
                    maxLength={128}
                    required
                    spellCheck={false}
                  />
                </label>
              </div>
              <label>
                <span>Version</span>
                <input
                  type="number"
                  min="1"
                  max="2147483647"
                  step="1"
                  value={profileForm.version}
                  onChange={(event) =>
                    setProfileForm({ ...profileForm, version: event.target.value })
                  }
                  required
                />
                <small>New profile IDs begin at 1; later drafts use the next version.</small>
              </label>
              <label>
                <span>Description</span>
                <input
                  value={profileForm.description}
                  onChange={(event) =>
                    setProfileForm({ ...profileForm, description: event.target.value })
                  }
                  placeholder="Coding workspace for daily development"
                  maxLength={1024}
                  required
                />
              </label>
              <fieldset className="provider-options">
                <legend>Providers</legend>
                <label className="confirmation-check">
                  <input
                    type="checkbox"
                    checked={profileForm.codex}
                    onChange={(event) =>
                      setProfileForm({ ...profileForm, codex: event.target.checked })
                    }
                  />
                  <span>Codex</span>
                </label>
                <label className="confirmation-check">
                  <input
                    type="checkbox"
                    checked={profileForm.claudeAgent}
                    onChange={(event) =>
                      setProfileForm({ ...profileForm, claudeAgent: event.target.checked })
                    }
                  />
                  <span>Claude Code</span>
                </label>
              </fieldset>
              <div className="form-row">
                <label>
                  <span>CPU limit (mCPU)</span>
                  <input
                    type="number"
                    min="100"
                    max="64000"
                    step="100"
                    value={profileForm.cpuLimitMillis}
                    onChange={(event) =>
                      setProfileForm({ ...profileForm, cpuLimitMillis: event.target.value })
                    }
                    required
                  />
                </label>
                <label>
                  <span>Memory limit (MiB)</span>
                  <input
                    type="number"
                    min="128"
                    max="1048576"
                    step="128"
                    value={profileForm.memoryLimitMiB}
                    onChange={(event) =>
                      setProfileForm({ ...profileForm, memoryLimitMiB: event.target.value })
                    }
                    required
                  />
                </label>
              </div>
              <label>
                <span>Storage policy reference</span>
                <input
                  value={profileForm.storagePolicyRef}
                  onChange={(event) =>
                    setProfileForm({ ...profileForm, storagePolicyRef: event.target.value })
                  }
                  placeholder="storage-standard"
                  maxLength={128}
                  required
                  spellCheck={false}
                />
              </label>
              <label>
                <span>Network policy reference</span>
                <input
                  value={profileForm.networkPolicyRef}
                  onChange={(event) =>
                    setProfileForm({ ...profileForm, networkPolicyRef: event.target.value })
                  }
                  placeholder="network-egress"
                  maxLength={128}
                  required
                  spellCheck={false}
                />
              </label>
              <label>
                <span>Release digest</span>
                <input
                  value={profileForm.releaseDigest}
                  onChange={(event) =>
                    setProfileForm({ ...profileForm, releaseDigest: event.target.value })
                  }
                  placeholder={`sha256:${"a".repeat(64)}`}
                  minLength={71}
                  maxLength={71}
                  required
                  spellCheck={false}
                />
              </label>
              <label>
                <span>Target references</span>
                <input
                  value={profileForm.targetRefs}
                  onChange={(event) =>
                    setProfileForm({ ...profileForm, targetRefs: event.target.value })
                  }
                  placeholder="docker-primary, ssh-overflow"
                  required
                  spellCheck={false}
                />
                <small>Comma-separated Target IDs or selectors resolved by Control Plane.</small>
              </label>
              <label>
                <span>Provider credential reference</span>
                <input
                  value={profileForm.providerCredentialRef}
                  onChange={(event) =>
                    setProfileForm({ ...profileForm, providerCredentialRef: event.target.value })
                  }
                  placeholder="provider-default"
                  maxLength={128}
                  required
                  spellCheck={false}
                />
                <small>Only this opaque reference enters the browser; secret bytes do not.</small>
              </label>
              <div className="dialog-actions">
                <button
                  className="button ghost"
                  type="button"
                  onClick={() => setCreatingProfile(false)}
                >
                  Cancel
                </button>
                <button className="button primary" type="submit" disabled={busy !== null}>
                  Create draft
                </button>
              </div>
            </form>
          </section>
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

function ProfileTable({
  profiles,
  selectedProfileVersionId,
  onSelect,
}: Readonly<{
  profiles: readonly EnvironmentProfile[];
  selectedProfileVersionId: string;
  onSelect: (profileVersionId: string) => void;
}>) {
  if (profiles.length === 0)
    return (
      <div className="table-empty">No environment profile versions exist in this project.</div>
    );
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Version</th>
            <th>Status</th>
            <th>Providers</th>
            <th>Capacity</th>
            <th>Updated</th>
            <th aria-label="Actions" />
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
              <td className="mono">v{profile.spec.version}</td>
              <td>
                <span className={`phase ${phaseTone(profile.spec.status)}`}>
                  <i /> {profile.spec.status}
                </span>
              </td>
              <td>{profile.spec.providerKinds.join(" · ")}</td>
              <td>
                {profile.spec.cpuLimitMillis} mCPU ·{" "}
                {Math.round(profile.spec.memoryLimitBytes / 1_048_576)} MiB
              </td>
              <td>{formatTime(profile.metadata.updatedAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={`View ${profile.metadata.name} version ${profile.spec.version}`}
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

function TargetDetail({
  target,
  operations,
  audit,
  onProbe,
  onPreviewCleanup,
  disabled,
}: Readonly<{
  target: DeploymentTarget;
  operations: readonly MaintenanceOperation[];
  audit: readonly AdminAuditEvent[];
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
      </section>
    </>
  );
}

function CleanupConfirmation({
  target,
  preview,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  target: DeploymentTarget;
  preview: DeploymentTargetCleanupPreview;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const [confirmed, setConfirmed] = useState(false);
  const resourceCount = preview.spec.workers.reduce(
    (count, worker) => count + worker.resources.length,
    0,
  );
  return (
    <section className="dialog" aria-labelledby="cleanup-title">
      <div className="panel-heading">
        <div>
          <div className="eyebrow">targets.act · destructive</div>
          <h2 id="cleanup-title">Confirm target cleanup</h2>
          <p>{target.metadata.name}</p>
        </div>
        <button className="icon-button" type="button" aria-label="Close" onClick={onClose}>
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
        <div className={`banner ${preview.spec.canCleanup ? "running" : "danger"}`} role="status">
          {preview.spec.canCleanup
            ? `${preview.spec.workers.length} Workers and ${resourceCount} resources will be deleted.`
            : "Cleanup is blocked because at least one Worker has an active Lease."}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>Target</dt>
            <dd className="mono">{target.metadata.uid}</dd>
          </div>
          <div>
            <dt>Generation</dt>
            <dd className="mono">{preview.spec.expectedGeneration}</dd>
          </div>
          <div>
            <dt>Resource version</dt>
            <dd className="mono">{preview.spec.expectedResourceVersion}</dd>
          </div>
        </dl>
        <div className="cleanup-preview" aria-label="Cleanup impact">
          {preview.spec.workers.length === 0 ? (
            <p>No platform-owned Workers or resources were found.</p>
          ) : (
            preview.spec.workers.map((worker) => (
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
        </div>
        {preview.spec.canCleanup ? (
          <label className="confirmation-check">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(event) => setConfirmed(event.target.checked)}
              disabled={disabled}
              data-sheet-autofocus
            />
            <span>I reviewed the resource names and generation above.</span>
          </label>
        ) : null}
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="button danger"
            type="submit"
            disabled={disabled || !preview.spec.canCleanup || !confirmed}
          >
            Confirm cleanup
          </button>
        </div>
      </form>
    </section>
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

function ProfileDetail({
  profile,
  audit,
}: Readonly<{ profile: EnvironmentProfile; audit: readonly AdminAuditEvent[] }>) {
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          P
        </div>
        <div>
          <div className="eyebrow">Environment profile · v{profile.spec.version}</div>
          <h2>{profile.metadata.name}</h2>
          <span className={`phase ${phaseTone(profile.spec.status)}`}>
            <i /> {profile.spec.status}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>Profile ID</dt>
          <dd className="mono">{profile.spec.profileId}</dd>
        </div>
        <div>
          <dt>Version resource</dt>
          <dd className="mono break">{profile.metadata.uid}</dd>
        </div>
        <div>
          <dt>Description</dt>
          <dd>{profile.spec.description}</dd>
        </div>
        <div>
          <dt>Providers</dt>
          <dd>{profile.spec.providerKinds.join(" · ")}</dd>
        </div>
        <div>
          <dt>CPU / memory</dt>
          <dd>
            {profile.spec.cpuLimitMillis} mCPU /{" "}
            {Math.round(profile.spec.memoryLimitBytes / 1_048_576)} MiB
          </dd>
        </div>
        <div>
          <dt>Storage policy</dt>
          <dd className="mono">{profile.spec.storagePolicyRef}</dd>
        </div>
        <div>
          <dt>Network policy</dt>
          <dd className="mono">{profile.spec.networkPolicyRef}</dd>
        </div>
        <div>
          <dt>Release digest</dt>
          <dd className="mono break">{profile.spec.releaseDigest}</dd>
        </div>
        <div>
          <dt>Target references</dt>
          <dd className="mono break">{profile.spec.targetRefs.join(", ")}</dd>
        </div>
        <div>
          <dt>Provider credential ref</dt>
          <dd className="mono break">{profile.spec.providerCredentialRef}</dd>
        </div>
        <div>
          <dt>Resource version</dt>
          <dd className="mono">{profile.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>Created</dt>
          <dd>{formatTime(profile.metadata.createdAt)}</dd>
        </div>
        <div>
          <dt>Published</dt>
          <dd>{formatTime(profile.spec.publishedAt)}</dd>
        </div>
        <div>
          <dt>Disabled</dt>
          <dd>{formatTime(profile.spec.disabledAt)}</dd>
        </div>
      </dl>
      <section className="activity-block" aria-labelledby="profile-audit-title">
        <div className="activity-heading">
          <h3 id="profile-audit-title">Audit</h3>
          <span className="scope-chip">audit.list · {audit.length}</span>
        </div>
        {audit.length === 0 ? (
          <p className="activity-empty">No audit events were recorded for this profile version.</p>
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
    </>
  );
}
