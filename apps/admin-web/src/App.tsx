import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  createHTTPClient,
  type AdminAuditEvent,
  type DeploymentTarget,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetSchedulingPreview,
  type DeploymentTargetRegisterRequest,
  type EnvironmentLease,
  type EnvironmentLeaseUpgradePreview,
  type EnvironmentProfile,
  type EnvironmentProfileCreateRequest,
  type MaintenanceOperation,
  type NetworkPolicy,
  type ProjectLeaseQuota,
  type ProjectLeaseQuotaSetRequest,
  type StoragePolicy,
  type StoragePolicySetRequest,
  type Worker,
  type WorkerRelease,
  type WorkerReleaseRegisterRequest,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  adminFailure,
  filterAdminTargets,
  filterAdminMaintenanceOperations,
  filterAdminLeases,
  leaseNeedsAttention,
  cleanupRequestFromPreview,
  leaseReleaseRequestFromPreview,
  listAdminLeases,
  listAdminMaintenanceOperations,
  listAdminProjectLeaseQuotaAuditEvents,
  listAdminStoragePolicies,
  listAdminNetworkPolicies,
  listAdminStoragePolicyAuditEvents,
  listAdminProfileAuditEvents,
  listAdminProfiles,
  listAdminReleases,
  listAdminTargetAuditEvents,
  listAdminTargetOperations,
  listAdminTargets,
  listAdminWorkers,
  loadAdminProjectLeaseQuota,
  newIdempotencyKey,
  newRequestId,
  readSavedAdminConnection,
  replaceLease,
  replaceProfile,
  replaceRelease,
  replaceStoragePolicy,
  replaceTarget,
  schedulingRequestFromPreview,
  summarizeClusterHosts,
  writeSavedAdminConnection,
  type AdminClient,
  type ClusterHostSummary,
  type SavedAdminConnection,
} from "./admin";
import { NetworkPolicyPanel } from "./NetworkPolicyPanel";
import { TargetFilters } from "./TargetFilters";
import { AdminSidebar } from "./AdminSidebar";
import { NavigationCommands, NavigationIcon, ResourceNavigation, type Page } from "./navigation";
import {
  normalizeLocale,
  useI18n,
  type MessageKey,
  type MessageValues,
  type Translate,
} from "./i18n";

type ConnectionStatus = "disconnected" | "connecting" | "connected" | "error";
type TargetKind = DeploymentTargetRegisterRequest["targetKind"];
type ProfileTransition = "publish" | "disable";
type LeaseReleaseTransition = "upgrade" | "rollback";
type LocalizedMessage = Readonly<{ key: MessageKey; values?: MessageValues }>;
type BusyOperation = Readonly<{ message: LocalizedMessage }>;
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

function quotaFormFrom(quota?: ProjectLeaseQuota) {
  return {
    maxConcurrentLeases: String(quota?.spec.maxConcurrentLeases ?? 8),
    maxCpuMillis: String(quota?.spec.maxCpuMillis ?? 16_000),
    maxMemoryMiB: String((quota?.spec.maxMemoryBytes ?? 34_359_738_368) / 1_048_576),
    maxLeaseTtlSeconds: String(quota?.spec.maxLeaseTtlSeconds ?? 3_600),
  };
}

function storagePolicyFormFrom(policy?: StoragePolicy) {
  return {
    policyId: policy?.metadata.uid ?? "",
    policyName: policy?.metadata.name ?? "",
    userSummary: policy?.spec.userSummary ?? "",
    workspaceCapacityGiB: String(
      (policy?.spec.workspaceCapacityBytes ?? 21_474_836_480) / 1_073_741_824,
    ),
    snapshotBackendRef: policy?.spec.snapshotBackendRef ?? "",
    artifactBackendRef: policy?.spec.artifactBackendRef ?? "",
  };
}

function statusLabel(status: ConnectionStatus, t: Translate): string {
  if (status === "connected") return t("connection.connected");
  if (status === "connecting") return t("connection.authorizing");
  if (status === "error") return t("connection.failed");
  return t("connection.disconnected");
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
      "starting",
      "stopping",
      "drained",
    ].includes(phase)
  )
    return "running";
  if (
    phase === "unavailable" ||
    phase === "failed" ||
    phase === "blocked" ||
    phase === "disabled" ||
    phase === "cleanup-pending"
  )
    return "danger";
  return "neutral";
}

const phaseMessageKeys: Readonly<Record<string, MessageKey>> = Object.freeze({
  unprobed: "phase.unprobed",
  probing: "phase.probing",
  ready: "phase.ready",
  unavailable: "phase.unavailable",
  active: "phase.active",
  drained: "phase.drained",
  provisioning: "phase.provisioning",
  terminating: "phase.terminating",
  terminated: "phase.terminated",
  failed: "phase.failed",
  none: "phase.none",
  pending: "phase.pending",
  revoking: "phase.revoking",
  reaping: "phase.reaping",
  complete: "phase.complete",
  blocked: "phase.blocked",
  draft: "phase.draft",
  published: "phase.published",
  disabled: "phase.disabled",
  queued: "phase.queued",
  running: "phase.running",
  succeeded: "phase.succeeded",
  cancelled: "phase.cancelled",
  requested: "phase.requested",
  cleanup: "phase.cleanup",
  starting: "phase.starting",
  stopping: "phase.stopping",
  "cleanup-pending": "phase.cleanupPending",
});

const auditMessageKeys: Readonly<Record<string, MessageKey>> = Object.freeze({
  "target.register": "audit.targetRegister",
  "target.probe": "audit.targetProbe",
  "target.drain": "audit.targetDrain",
  "target.resume": "audit.targetResume",
  "target.cleanup": "audit.targetCleanup",
  "target.upgrade": "audit.targetUpgrade",
  "target.rollback": "audit.targetRollback",
  "profile.create": "audit.profileCreate",
  "profile.publish": "audit.profilePublish",
  "profile.disable": "audit.profileDisable",
  "quota.set": "audit.quotaSet",
  "storage-policy.set": "audit.storagePolicySet",
  "network-policy.set": "audit.networkPolicySet",
});

const operationImpactMessageKeys: Readonly<Record<string, MessageKey>> = Object.freeze({
  "target.register": "operation.impact.register",
  "target.probe": "operation.impact.probe",
  "target.drain": "operation.impact.drain",
  "target.resume": "operation.impact.resume",
  "target.cleanup": "operation.impact.cleanup",
  "target.upgrade": "operation.impact.upgrade",
  "target.rollback": "operation.impact.rollback",
});

const resourceMessageKeys: Readonly<Record<string, MessageKey>> = Object.freeze({
  container: "resource.container",
  deployment: "resource.deployment",
  pods: "resource.pods",
  service: "resource.service",
  "workspace-volume": "resource.workspaceVolume",
});

function phaseLabel(phase: string, t: Translate): string {
  const key = phaseMessageKeys[phase];
  return key === undefined ? phase : t(key);
}

function auditLabel(action: string, t: Translate): string {
  const key = auditMessageKeys[action];
  return key === undefined ? action : t(key);
}

function operationImpactLabel(action: string, t: Translate): string {
  const key = operationImpactMessageKeys[action];
  return key === undefined ? action : t(key);
}

function resourceLabel(kind: string, t: Translate): string {
  const key = resourceMessageKeys[kind];
  return key === undefined ? kind : t(key);
}

function targetKindLabel(kind: TargetKind, t: Translate): string {
  if (kind === "kubernetes") return t("target.kind.kubernetes");
  if (kind === "ssh") return t("target.kind.ssh");
  return t("target.kind.docker");
}

function providerLabel(provider: string): string {
  return provider === "claudeAgent" ? "Claude Code" : "Codex";
}

function shortDigest(value: string): string {
  return `${value.slice(0, 15)}…${value.slice(-8)}`;
}

function AdminSheet({
  label,
  onClose,
  children,
  confirmation = false,
  returnFocus,
  feedback,
}: Readonly<{
  label: string;
  onClose: () => void;
  children: ReactNode;
  confirmation?: boolean;
  returnFocus?: HTMLElement | null;
  feedback: ReactNode;
}>) {
  const ref = useRef<HTMLDialogElement>(null);
  const triggerRef = useRef(returnFocus);

  useLayoutEffect(() => {
    const dialog = ref.current;
    if (dialog === null) return;
    if (!dialog.open) {
      dialog.showModal();
      dialog.querySelector<HTMLElement>("[data-sheet-autofocus]")?.focus();
    }
    return () => {
      // Close before DOM removal so native focus restoration can return to the trigger.
      if (dialog.open) dialog.close();
      // Async previews disable their trigger before opening, so the native dialog cannot capture it.
      if (triggerRef.current?.isConnected) triggerRef.current.focus();
    };
  }, []);

  return (
    <dialog
      ref={ref}
      className={`admin-sheet${confirmation ? " confirmation-dialog" : ""}`}
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
      {feedback}
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
  Readonly<{
    profiles: readonly EnvironmentProfile[];
    selectedProfileVersionId: string;
  }>
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
  const { locale, setLocale, t, number, dateTime } = useI18n();
  const [theme, setTheme] = useState<Theme>(initialTheme);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const [commandsOpen, setCommandsOpen] = useState(false);
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
  const [schedulingPreview, setSchedulingPreview] =
    useState<DeploymentTargetSchedulingPreview | null>(null);
  const [leases, setLeases] = useState<readonly EnvironmentLease[]>(Object.freeze([]));
  const [selectedLeaseId, setSelectedLeaseId] = useState("");
  const [leaseReleasePreview, setLeaseReleasePreview] =
    useState<EnvironmentLeaseUpgradePreview | null>(null);
  const [leaseReleaseConfirmationOpen, setLeaseReleaseConfirmationOpen] = useState(false);
  const [selectedUpgradeReleaseDigest, setSelectedUpgradeReleaseDigest] = useState("");
  const [workers, setWorkers] = useState<readonly Worker[]>(Object.freeze([]));
  const [selectedWorkerId, setSelectedWorkerId] = useState("");
  const [releases, setReleases] = useState<readonly WorkerRelease[]>(Object.freeze([]));
  const [profiles, setProfiles] = useState<readonly EnvironmentProfile[]>(Object.freeze([]));
  const [selectedProfileVersionId, setSelectedProfileVersionId] = useState("");
  const [profileAudit, setProfileAudit] = useState<readonly AdminAuditEvent[]>(Object.freeze([]));
  const [storagePolicies, setStoragePolicies] = useState<readonly StoragePolicy[]>(
    Object.freeze([]),
  );
  const [networkPolicies, setNetworkPolicies] = useState<readonly NetworkPolicy[]>([]);
  const [networkEditorEpoch, setNetworkEditorEpoch] = useState(0);
  const [selectedStoragePolicyId, setSelectedStoragePolicyId] = useState("");
  const [storagePolicyAudit, setStoragePolicyAudit] = useState<readonly AdminAuditEvent[]>(
    Object.freeze([]),
  );
  const [leaseQuota, setLeaseQuota] = useState<ProjectLeaseQuota>();
  const [leaseQuotaAudit, setLeaseQuotaAudit] = useState<readonly AdminAuditEvent[]>(
    Object.freeze([]),
  );
  const [maintenanceOperations, setMaintenanceOperations] = useState<
    readonly MaintenanceOperation[]
  >(Object.freeze([]));
  const [selectedMaintenanceOperationId, setSelectedMaintenanceOperationId] = useState("");
  const [page, setPage] = useState<Page>("overview");
  const [query, setQuery] = useState("");
  const [targetKindFilter, setTargetKindFilter] = useState<readonly TargetKind[]>([]);
  const [leaseAttentionOnly, setLeaseAttentionOnly] = useState(false);
  const [leasePhaseFilter, setLeasePhaseFilter] = useState<
    EnvironmentLease["spec"]["observedPhase"] | ""
  >("");
  const [leaseCleanupBlockedOnly, setLeaseCleanupBlockedOnly] = useState(false);
  const [failedOperationsOnly, setFailedOperationsOnly] = useState(false);
  const [targetPhaseFilter, setTargetPhaseFilter] = useState<
    readonly DeploymentTarget["spec"]["observedPhase"][]
  >([]);
  const [targetDetailOpen, setTargetDetailOpen] = useState(false);
  const [cleanupConfirmationOpen, setCleanupConfirmationOpen] = useState(false);
  const [schedulingConfirmationOpen, setSchedulingConfirmationOpen] = useState(false);
  const [leaseDetailOpen, setLeaseDetailOpen] = useState(false);
  const [workerDetailOpen, setWorkerDetailOpen] = useState(false);
  const [registering, setRegistering] = useState(false);
  const [registeringRelease, setRegisteringRelease] = useState(false);
  const [profileDetailOpen, setProfileDetailOpen] = useState(false);
  const [maintenanceDetailOpen, setMaintenanceDetailOpen] = useState(false);
  const [profileTransition, setProfileTransition] = useState<ProfileTransition | null>(null);
  const [creatingProfile, setCreatingProfile] = useState(false);
  const [busy, setBusy] = useState<BusyOperation | null>(null);
  const [error, setError] = useState<ReturnType<typeof adminFailure> | null>(null);
  const [notice, setNotice] = useState<LocalizedMessage | null>(null);
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
  const [releaseForm, setReleaseForm] = useState({
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
  });
  const [quotaForm, setQuotaForm] = useState(quotaFormFrom);
  const [storagePolicyForm, setStoragePolicyForm] = useState(storagePolicyFormFrom);
  const requestRef = useRef<AbortController | null>(null);
  const busyRef = useRef(false);
  const operationTriggerRef = useRef<HTMLElement | null>(null);
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
  const selectedSchedulingPreview =
    selectedTarget !== undefined &&
    schedulingPreview?.metadata.uid === selectedTarget.metadata.uid &&
    schedulingPreview.metadata.resourceVersion === selectedTarget.metadata.resourceVersion
      ? schedulingPreview
      : null;
  const selectedLease = leases.find(({ metadata }) => metadata.uid === selectedLeaseId);
  const selectedLeaseTarget = targets.find(
    ({ metadata }) => metadata.uid === selectedLease?.spec.targetId,
  );
  const upgradeReleaseDigest =
    releases.find(
      ({ spec }) =>
        spec.releaseDigest === selectedUpgradeReleaseDigest &&
        spec.releaseDigest !== selectedLease?.spec.releaseDigest,
    )?.spec.releaseDigest ??
    releases.find(({ spec }) => spec.releaseDigest !== selectedLease?.spec.releaseDigest)?.spec
      .releaseDigest ??
    "";
  const selectedLeaseReleasePreview =
    selectedLease !== undefined &&
    leaseReleasePreview?.metadata.uid === selectedLease.metadata.uid &&
    leaseReleasePreview.metadata.resourceVersion === selectedLease.metadata.resourceVersion
      ? leaseReleasePreview
      : null;
  const selectedWorker = workers.find(({ metadata }) => metadata.uid === selectedWorkerId);
  const selectedProfile = profiles.find(
    ({ metadata }) => metadata.uid === selectedProfileVersionId,
  );
  const selectedStoragePolicy = storagePolicies.find(
    ({ metadata }) => metadata.uid === selectedStoragePolicyId,
  );
  const selectedStoragePolicyReferenced = profiles.some(
    ({ spec }) => spec.storagePolicyRef === selectedStoragePolicyId,
  );
  const selectedMaintenanceOperation = maintenanceOperations.find(
    ({ operationId }) => operationId === selectedMaintenanceOperationId,
  );
  const readyCount = targets.filter(({ spec }) => spec.observedPhase === "ready").length;
  const probingCount = targets.filter(({ spec }) => spec.observedPhase === "probing").length;
  const unprobedCount = targets.filter(({ spec }) => spec.observedPhase === "unprobed").length;
  const unavailableCount = targets.filter(
    ({ spec }) => spec.observedPhase === "unavailable",
  ).length;
  const attentionCount = targets.length - readyCount;
  const readyLeaseCount = leases.filter(({ spec }) => spec.observedPhase === "ready").length;
  const leaseAttentionCount = leases.filter(leaseNeedsAttention).length;
  const readyWorkerCount = workers.filter(({ spec }) => spec.state === "ready").length;
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const visibleTargets = filterAdminTargets(targets, query, targetKindFilter, targetPhaseFilter);
  const targetsFiltered =
    query.trim() !== "" || targetKindFilter.length > 0 || targetPhaseFilter.length > 0;
  const visibleLeases = filterAdminLeases(
    leases,
    query,
    leaseAttentionOnly,
    leasePhaseFilter,
    leaseCleanupBlockedOnly,
  );
  const visibleWorkers =
    normalizedQuery === ""
      ? workers
      : workers.filter(({ metadata, spec }) =>
          [
            metadata.uid,
            metadata.name,
            spec.leaseId,
            spec.targetId,
            spec.targetKind,
            spec.state,
            spec.releaseDigest,
          ].some((value) => value.toLocaleLowerCase().includes(normalizedQuery)),
        );
  const visibleReleases =
    normalizedQuery === ""
      ? releases
      : releases.filter(({ metadata, spec }) =>
          [
            metadata.uid,
            metadata.name,
            spec.imageRepository,
            spec.releaseDigest,
            spec.platformVersion,
            spec.runtimeVersion,
            spec.codexVersion,
            spec.claudeCodeVersion,
            ...spec.architectures,
          ].some((value) => value.toLocaleLowerCase().includes(normalizedQuery)),
        );
  const clusterHosts = summarizeClusterHosts(targets, workers);
  const visibleClusterHosts =
    normalizedQuery === ""
      ? clusterHosts
      : clusterHosts.filter(
          ({ target }) =>
            [
              target.metadata.uid,
              target.metadata.name,
              target.spec.targetKind,
              target.spec.observedPhase,
              target.spec.schedulingState,
              target.spec.apiVersion,
              target.spec.engineVersion,
              target.spec.os,
              target.spec.architecture,
            ].some((value) => value.toLocaleLowerCase().includes(normalizedQuery)) ||
            visibleWorkers.some(({ spec }) => spec.targetId === target.metadata.uid),
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
  const visibleStoragePolicies =
    normalizedQuery === ""
      ? storagePolicies
      : storagePolicies.filter(({ metadata, spec }) =>
          [metadata.uid, metadata.name, spec.userSummary, spec.workspaceType].some((value) =>
            value.toLocaleLowerCase().includes(normalizedQuery),
          ),
        );
  const visibleMaintenanceOperations = filterAdminMaintenanceOperations(
    maintenanceOperations,
    query,
    failedOperationsOnly,
  );
  const failedMaintenanceOperations = filterAdminMaintenanceOperations(
    maintenanceOperations,
    "",
    true,
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
      if (
        (!event.metaKey && !event.ctrlKey) ||
        event.altKey ||
        !document.querySelector(".app-shell")
      )
        return;
      if (event.key.toLowerCase() === "k") {
        if (
          document.querySelector("dialog[open]:not(.mobile-nav-dialog):not(.navigation-commands)")
        )
          return;
        event.preventDefault();
        setCommandsOpen((open) => !open);
        return;
      }
      if (event.key.toLowerCase() !== "b") return;
      if (document.querySelector("dialog[open]:not(.mobile-nav-dialog)")) return;
      event.preventDefault();
      if (window.matchMedia("(max-width: 767px)").matches) {
        setMobileNavOpen((open) => !open);
      } else {
        setSidebarOpen((open) => !open);
      }
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
        listAdminWorkers(client, connection.tenantId, connection.projectId, signal),
      ])
        .then(([loadedTargets, loadedLeases, loadedWorkers]) => {
          setTargets(loadedTargets.targets);
          setSelectedTargetId(loadedTargets.selectedTargetId);
          setLeases(loadedLeases.leases);
          setSelectedLeaseId(loadedLeases.selectedLeaseId);
          setWorkers(loadedWorkers);
          setSelectedWorkerId((current) =>
            loadedWorkers.some(({ metadata }) => metadata.uid === current)
              ? current
              : (loadedWorkers[0]?.metadata.uid ?? ""),
          );
        })
        .catch((cause: unknown) => {
          if (!controller.signal.aborted) setError(adminFailure(cause));
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
    setError(null);
    setCommandsOpen(false);
    setPage(nextPage);
    setQuery("");
    setTargetKindFilter([]);
    setTargetPhaseFilter([]);
    setLeaseAttentionOnly(false);
    setLeasePhaseFilter("");
    setLeaseCleanupBlockedOnly(false);
    setFailedOperationsOnly(false);
    setMobileNavOpen(false);
    setTargetDetailOpen(false);
    setCleanupConfirmationOpen(false);
    setSchedulingConfirmationOpen(false);
    setLeaseDetailOpen(false);
    setLeaseReleaseConfirmationOpen(false);
    setWorkerDetailOpen(false);
    setProfileDetailOpen(false);
    setMaintenanceDetailOpen(false);
    setProfileTransition(null);
    setRegisteringRelease(false);
  }

  function disconnect() {
    setCommandsOpen(false);
    requestRef.current?.abort();
    requestRef.current = null;
    setClient(null);
    setToken("");
    setTargets(Object.freeze([]));
    setSelectedTargetId("");
    setTargetOperations(Object.freeze([]));
    setTargetAudit(Object.freeze([]));
    setCleanupPreview(null);
    setSchedulingPreview(null);
    setLeases(Object.freeze([]));
    setSelectedLeaseId("");
    setLeaseReleasePreview(null);
    setLeaseReleaseConfirmationOpen(false);
    setSelectedUpgradeReleaseDigest("");
    setWorkers(Object.freeze([]));
    setSelectedWorkerId("");
    setReleases(Object.freeze([]));
    setProfiles(Object.freeze([]));
    setSelectedProfileVersionId("");
    setProfileAudit(Object.freeze([]));
    setStoragePolicies(Object.freeze([]));
    setNetworkPolicies([]);
    setNetworkEditorEpoch((current) => current + 1);
    setSelectedStoragePolicyId("");
    setStoragePolicyAudit(Object.freeze([]));
    setStoragePolicyForm(storagePolicyFormFrom());
    setLeaseQuota(undefined);
    setLeaseQuotaAudit(Object.freeze([]));
    setQuotaForm(quotaFormFrom());
    setMaintenanceOperations(Object.freeze([]));
    setSelectedMaintenanceOperationId("");
    setTargetDetailOpen(false);
    setCleanupConfirmationOpen(false);
    setSchedulingConfirmationOpen(false);
    setLeaseDetailOpen(false);
    setWorkerDetailOpen(false);
    setRegisteringRelease(false);
    setProfileDetailOpen(false);
    setMaintenanceDetailOpen(false);
    setProfileTransition(null);
    setCreatingProfile(false);
    setMobileNavOpen(false);
    setBusy(null);
    setError(null);
    setNotice(null);
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
      setError({ key: "connection.required", code: null });
      return;
    }
    const controller = new AbortController();
    requestRef.current = controller;
    setStatus("connecting");
    setError(null);
    try {
      const nextClient = createHTTPClient(nextConnection.endpoint, bearer);
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
      const [
        loadedTargets,
        loadedLeases,
        loadedWorkers,
        loadedReleases,
        loadedProfiles,
        loadedStoragePolicies,
        loadedNetworkPolicies,
        loadedQuota,
        loadedQuotaAudit,
        loadedMaintenanceOperations,
      ] = await Promise.all([
        loadTargetAuthority(nextClient, nextConnection, selectedTargetId, signal),
        loadLeaseAuthority(nextClient, nextConnection, selectedLeaseId, signal),
        listAdminWorkers(nextClient, nextConnection.tenantId, nextConnection.projectId, signal),
        listAdminReleases(nextClient, nextConnection.tenantId, nextConnection.projectId, signal),
        loadProfileAuthority(nextClient, nextConnection, selectedProfileVersionId, signal),
        listAdminStoragePolicies(
          nextClient,
          nextConnection.tenantId,
          nextConnection.projectId,
          signal,
        ),
        listAdminNetworkPolicies(
          nextClient,
          nextConnection.tenantId,
          nextConnection.projectId,
          signal,
        ),
        loadAdminProjectLeaseQuota(
          nextClient,
          nextConnection.tenantId,
          nextConnection.projectId,
          signal,
        ),
        listAdminProjectLeaseQuotaAuditEvents(
          nextClient,
          nextConnection.tenantId,
          nextConnection.projectId,
          signal,
        ),
        listAdminMaintenanceOperations(
          nextClient,
          nextConnection.tenantId,
          nextConnection.projectId,
          signal,
        ),
      ]);
      setClient(nextClient);
      setConnection(nextConnection);
      setTargets(loadedTargets.targets);
      setSelectedTargetId(loadedTargets.selectedTargetId);
      setTargetOperations(Object.freeze([]));
      setTargetAudit(Object.freeze([]));
      setLeases(loadedLeases.leases);
      setSelectedLeaseId(loadedLeases.selectedLeaseId);
      setWorkers(loadedWorkers);
      setSelectedWorkerId(loadedWorkers[0]?.metadata.uid ?? "");
      setReleases(loadedReleases);
      setProfiles(loadedProfiles.profiles);
      setSelectedProfileVersionId(loadedProfiles.selectedProfileVersionId);
      setProfileAudit(Object.freeze([]));
      setStoragePolicies(loadedStoragePolicies);
      setNetworkPolicies(loadedNetworkPolicies);
      setSelectedStoragePolicyId(loadedStoragePolicies[0]?.metadata.uid ?? "");
      setStoragePolicyAudit(Object.freeze([]));
      setStoragePolicyForm(storagePolicyFormFrom(loadedStoragePolicies[0]));
      setLeaseQuota(loadedQuota);
      setLeaseQuotaAudit(loadedQuotaAudit);
      setQuotaForm(quotaFormFrom(loadedQuota));
      setMaintenanceOperations(loadedMaintenanceOperations);
      setSelectedMaintenanceOperationId(loadedMaintenanceOperations[0]?.operationId ?? "");
      writeSavedAdminConnection(window.sessionStorage, nextConnection);
      setToken("");
      setStatus("connected");
    } catch (cause) {
      setClient(null);
      setStatus(controller.signal.aborted ? "disconnected" : "error");
      setError(controller.signal.aborted ? null : adminFailure(cause));
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
    operationKey: string,
    message: LocalizedMessage,
    operation: (signal: AbortSignal) => Promise<void>,
  ) {
    if (busyRef.current) return;
    operationTriggerRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    busyRef.current = true;
    setBusy({ message });
    setError(null);
    setNotice(null);
    const controller = new AbortController();
    requestRef.current = controller;
    try {
      await operation(AbortSignal.any([controller.signal, AbortSignal.timeout(150_000)]));
      pendingKeysRef.current.delete(operationKey);
      setNotice(message);
    } catch (cause) {
      setError(adminFailure(cause));
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
      busyRef.current = false;
      setBusy(null);
    }
  }

  async function reloadMaintenanceOperations(signal: AbortSignal) {
    if (client === null) return;
    const loaded = await listAdminMaintenanceOperations(
      client,
      connection.tenantId,
      connection.projectId,
      signal,
    );
    setMaintenanceOperations(loaded);
    setSelectedMaintenanceOperationId((current) =>
      loaded.some(({ operationId }) => operationId === current)
        ? current
        : (loaded[0]?.operationId ?? ""),
    );
  }

  function refresh() {
    if (client === null) return;
    void runOperation("refresh", { key: "operation.refresh" }, async (signal) => {
      const [
        loadedTargets,
        loadedLeases,
        loadedWorkers,
        loadedReleases,
        loadedProfiles,
        loadedStoragePolicies,
        loadedNetworkPolicies,
        loadedQuota,
        loadedQuotaAudit,
        loadedMaintenanceOperations,
      ] = await Promise.all([
        loadTargetAuthority(client, connection, selectedTargetId, signal),
        loadLeaseAuthority(client, connection, selectedLeaseId, signal),
        listAdminWorkers(client, connection.tenantId, connection.projectId, signal),
        listAdminReleases(client, connection.tenantId, connection.projectId, signal),
        loadProfileAuthority(client, connection, selectedProfileVersionId, signal),
        listAdminStoragePolicies(client, connection.tenantId, connection.projectId, signal),
        listAdminNetworkPolicies(client, connection.tenantId, connection.projectId, signal),
        loadAdminProjectLeaseQuota(client, connection.tenantId, connection.projectId, signal),
        listAdminProjectLeaseQuotaAuditEvents(
          client,
          connection.tenantId,
          connection.projectId,
          signal,
        ),
        listAdminMaintenanceOperations(client, connection.tenantId, connection.projectId, signal),
      ]);
      setTargets(loadedTargets.targets);
      setSelectedTargetId(loadedTargets.selectedTargetId);
      setLeases(loadedLeases.leases);
      setSelectedLeaseId(loadedLeases.selectedLeaseId);
      setWorkers(loadedWorkers);
      setSelectedWorkerId((current) =>
        loadedWorkers.some(({ metadata }) => metadata.uid === current)
          ? current
          : (loadedWorkers[0]?.metadata.uid ?? ""),
      );
      setReleases(loadedReleases);
      setProfiles(loadedProfiles.profiles);
      setSelectedProfileVersionId(loadedProfiles.selectedProfileVersionId);
      setStoragePolicies(loadedStoragePolicies);
      setNetworkPolicies(loadedNetworkPolicies);
      setSelectedStoragePolicyId((current) =>
        loadedStoragePolicies.some(({ metadata }) => metadata.uid === current)
          ? current
          : (loadedStoragePolicies[0]?.metadata.uid ?? ""),
      );
      setLeaseQuota(loadedQuota);
      setLeaseQuotaAudit(loadedQuotaAudit);
      setQuotaForm(quotaFormFrom(loadedQuota));
      setMaintenanceOperations(loadedMaintenanceOperations);
      setSelectedMaintenanceOperationId((current) =>
        loadedMaintenanceOperations.some(({ operationId }) => operationId === current)
          ? current
          : (loadedMaintenanceOperations[0]?.operationId ?? ""),
      );
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
      const storagePolicy =
        loadedStoragePolicies.find(({ metadata }) => metadata.uid === selectedStoragePolicyId) ??
        loadedStoragePolicies[0];
      setStoragePolicyForm(storagePolicyFormFrom(storagePolicy));
      setStoragePolicyAudit(
        storagePolicy === undefined
          ? Object.freeze([])
          : await listAdminStoragePolicyAuditEvents(
              client,
              connection.tenantId,
              connection.projectId,
              storagePolicy.metadata.uid,
              signal,
            ),
      );
    });
  }

  function selectTarget(targetId: string) {
    setTargetDetailOpen(true);
    setCleanupConfirmationOpen(false);
    setSchedulingConfirmationOpen(false);
    if (client === null) return;
    setCleanupPreview(null);
    setSchedulingPreview(null);
    setTargetOperations(Object.freeze([]));
    setTargetAudit(Object.freeze([]));
    setSelectedTargetId(targetId);
    void runOperation(`get:${targetId}`, { key: "operation.targetDetail" }, async (signal) => {
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
    setLeaseReleasePreview(null);
    setLeaseReleaseConfirmationOpen(false);
    if (client === null || leaseId === selectedLeaseId) {
      setSelectedLeaseId(leaseId);
      return;
    }
    setSelectedLeaseId(leaseId);
    void runOperation(`get-lease:${leaseId}`, { key: "operation.leaseDetail" }, async (signal) => {
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

  function selectWorker(workerId: string) {
    setSelectedWorkerId(workerId);
    setWorkerDetailOpen(true);
  }

  function previewLeaseRelease(action: LeaseReleaseTransition) {
    if (
      client === null ||
      selectedLease === undefined ||
      (action === "upgrade" && upgradeReleaseDigest === "")
    )
      return;
    const lease = selectedLease;
    void runOperation(
      `lease-release-preview:${action}:${lease.metadata.uid}:${lease.metadata.resourceVersion}:${upgradeReleaseDigest}`,
      {
        key: action === "upgrade" ? "operation.previewUpgrade" : "operation.previewRollback",
        values: { name: lease.metadata.name },
      },
      async (signal) => {
        const result =
          action === "upgrade"
            ? await client.previewAdminEnvironmentLeaseUpgrade(
                connection.tenantId,
                connection.projectId,
                lease.metadata.uid,
                upgradeReleaseDigest,
                newRequestId(),
                signal,
              )
            : await client.previewAdminEnvironmentLeaseRollback(
                connection.tenantId,
                connection.projectId,
                lease.metadata.uid,
                newRequestId(),
                signal,
              );
        setLeaseReleasePreview(result.value);
        setLeaseReleaseConfirmationOpen(true);
      },
    );
  }

  function transitionLeaseRelease() {
    if (client === null || selectedLease === undefined || selectedLeaseReleasePreview === null)
      return;
    const lease = selectedLease;
    const preview = selectedLeaseReleasePreview;
    const action = preview.spec.action;
    const key = `lease-release:${action}:${lease.metadata.uid}:${preview.spec.expectedGeneration}:${preview.spec.expectedResourceVersion}:${preview.spec.impactDigest}`;
    setLeaseReleaseConfirmationOpen(false);
    void runOperation(
      key,
      {
        key: action === "upgrade" ? "operation.upgradeLease" : "operation.rollbackLease",
        values: { name: lease.metadata.name },
      },
      async (signal) => {
        const args = [
          connection.tenantId,
          connection.projectId,
          lease.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          leaseReleaseRequestFromPreview(preview),
          signal,
        ] as const;
        const result =
          action === "upgrade"
            ? await client.upgradeAdminEnvironmentLease(...args)
            : await client.rollbackAdminEnvironmentLease(...args);
        const [leaseResult, loadedWorkers, loadedOperations] = await Promise.all([
          client.getAdminEnvironmentLease(
            connection.tenantId,
            connection.projectId,
            lease.metadata.uid,
            newRequestId(),
            signal,
          ),
          listAdminWorkers(client, connection.tenantId, connection.projectId, signal),
          listAdminMaintenanceOperations(client, connection.tenantId, connection.projectId, signal),
        ]);
        setLeases((current) => replaceLease(current, leaseResult.value));
        setWorkers(loadedWorkers);
        setSelectedWorkerId((current) =>
          loadedWorkers.some(({ metadata }) => metadata.uid === current)
            ? current
            : (loadedWorkers[0]?.metadata.uid ?? ""),
        );
        setMaintenanceOperations(loadedOperations);
        setSelectedMaintenanceOperationId(result.value.operationId);
        setLeaseReleasePreview(null);
        setSelectedUpgradeReleaseDigest("");
        if (result.value.state !== "succeeded") {
          pendingKeysRef.current.delete(key);
          throw new Error(
            result.value.stableErrorCode || "environment lease release transition failed",
          );
        }
      },
    );
  }

  function selectProfile(profileVersionId: string) {
    setProfileDetailOpen(true);
    setProfileTransition(null);
    if (client === null) return;
    const profile = profiles.find(({ metadata }) => metadata.uid === profileVersionId);
    if (profile === undefined) return;
    setSelectedProfileVersionId(profileVersionId);
    setProfileAudit(Object.freeze([]));
    void runOperation(
      `get-profile:${profileVersionId}`,
      { key: "operation.profileDetail" },
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

  function selectMaintenanceOperation(operationId: string) {
    setSelectedMaintenanceOperationId(operationId);
    setMaintenanceDetailOpen(true);
  }

  function updateLeaseQuota(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null) return;
    const body: ProjectLeaseQuotaSetRequest = {
      expectedResourceVersion: leaseQuota?.metadata.resourceVersion ?? "0",
      maxConcurrentLeases: Number(quotaForm.maxConcurrentLeases),
      maxCpuMillis: Number(quotaForm.maxCpuMillis),
      maxMemoryBytes: Number(quotaForm.maxMemoryMiB) * 1_048_576,
      maxLeaseTtlSeconds: Number(quotaForm.maxLeaseTtlSeconds),
    };
    const key = `set-lease-quota:${Object.values(body).join(":")}`;
    void runOperation(key, { key: "operation.setQuota" }, async (signal) => {
      const result = await client.setAdminProjectLeaseQuota(
        connection.tenantId,
        connection.projectId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      setLeaseQuota(result.value);
      setQuotaForm(quotaFormFrom(result.value));
      setLeaseQuotaAudit(
        await listAdminProjectLeaseQuotaAuditEvents(
          client,
          connection.tenantId,
          connection.projectId,
          signal,
        ),
      );
    });
  }

  function selectStoragePolicy(policyId: string) {
    if (client === null) return;
    setSelectedStoragePolicyId(policyId);
    setStoragePolicyAudit(Object.freeze([]));
    void runOperation(
      `get-storage-policy:${policyId}`,
      { key: "operation.storagePolicyDetail" },
      async (signal) => {
        const [result, audit] = await Promise.all([
          client.getAdminStoragePolicy(
            connection.tenantId,
            connection.projectId,
            policyId,
            newRequestId(),
            signal,
          ),
          listAdminStoragePolicyAuditEvents(
            client,
            connection.tenantId,
            connection.projectId,
            policyId,
            signal,
          ),
        ]);
        setStoragePolicies((current) => replaceStoragePolicy(current, result.value));
        setStoragePolicyForm(storagePolicyFormFrom(result.value));
        setStoragePolicyAudit(audit);
      },
    );
  }

  function newStoragePolicy() {
    setSelectedStoragePolicyId("");
    setStoragePolicyAudit(Object.freeze([]));
    setStoragePolicyForm(storagePolicyFormFrom());
  }

  function saveStoragePolicy(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null || selectedStoragePolicyReferenced) return;
    const policyId = storagePolicyForm.policyId.trim();
    const existing = storagePolicies.find(({ metadata }) => metadata.uid === policyId);
    const body: StoragePolicySetRequest = {
      expectedResourceVersion: existing?.metadata.resourceVersion ?? "0",
      policyName: storagePolicyForm.policyName.trim(),
      userSummary: storagePolicyForm.userSummary.trim(),
      workspaceType: "managed-volume",
      workspaceCapacityBytes: Number(storagePolicyForm.workspaceCapacityGiB) * 1_073_741_824,
      retentionSeconds: 0,
      cleanupOnLeaseTermination: true,
      ...(storagePolicyForm.snapshotBackendRef.trim() === ""
        ? {}
        : { snapshotBackendRef: storagePolicyForm.snapshotBackendRef.trim() }),
      ...(storagePolicyForm.artifactBackendRef.trim() === ""
        ? {}
        : { artifactBackendRef: storagePolicyForm.artifactBackendRef.trim() }),
      allowWorkspaceReuse: true,
    };
    const key = `set-storage-policy:${policyId}:${body.expectedResourceVersion}`;
    void runOperation(key, { key: "operation.setStoragePolicy" }, async (signal) => {
      const result = await client.setAdminStoragePolicy(
        connection.tenantId,
        connection.projectId,
        policyId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      setStoragePolicies((current) => replaceStoragePolicy(current, result.value));
      setSelectedStoragePolicyId(result.value.metadata.uid);
      setStoragePolicyForm(storagePolicyFormFrom(result.value));
      setStoragePolicyAudit(
        await listAdminStoragePolicyAuditEvents(
          client,
          connection.tenantId,
          connection.projectId,
          result.value.metadata.uid,
          signal,
        ),
      );
    });
  }

  function registerRelease(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null) return;
    const architectures: ("linux/amd64" | "linux/arm64")[] = [];
    if (releaseForm.amd64) architectures.push("linux/amd64");
    if (releaseForm.arm64) architectures.push("linux/arm64");
    const body: WorkerReleaseRegisterRequest = {
      releaseId: releaseForm.releaseId.trim(),
      releaseName: releaseForm.releaseName.trim(),
      imageRepository: releaseForm.imageRepository.trim(),
      releaseDigest: releaseForm.releaseDigest.trim() as `sha256:${string}`,
      platformVersion: releaseForm.platformVersion.trim(),
      runtimeVersion: releaseForm.runtimeVersion.trim(),
      codexVersion: releaseForm.codexVersion.trim(),
      claudeCodeVersion: releaseForm.claudeCodeVersion.trim(),
      architectures,
      verificationEvidenceDigest:
        releaseForm.verificationEvidenceDigest.trim() as `sha256:${string}`,
    };
    const key = `register-release:${body.releaseId}`;
    void runOperation(
      key,
      { key: "operation.registerRelease", values: { name: body.releaseName } },
      async (signal) => {
        const result = await client.registerAdminWorkerRelease(
          connection.tenantId,
          connection.projectId,
          newRequestId(),
          idempotencyKey(key),
          body,
          signal,
        );
        setReleases((current) => replaceRelease(current, result.value));
        setReleaseForm({
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
        });
        setRegisteringRelease(false);
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
    void runOperation(
      key,
      {
        key: "operation.createProfile",
        values: { name: body.profileName, version: body.version },
      },
      async (signal) => {
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
      },
    );
  }

  function transitionProfile() {
    if (client === null || selectedProfile === undefined || profileTransition === null) return;
    const profile = selectedProfile;
    const action = profileTransition;
    const key = `${action}-profile:${profile.metadata.uid}:${profile.metadata.resourceVersion}`;
    setProfileTransition(null);
    void runOperation(
      key,
      {
        key: action === "publish" ? "operation.publishProfile" : "operation.disableProfile",
        values: { name: profile.metadata.name, version: profile.spec.version },
      },
      async (signal) => {
        const args = [
          connection.tenantId,
          connection.projectId,
          profile.spec.profileId,
          profile.spec.version,
          newRequestId(),
          idempotencyKey(key),
          { expectedResourceVersion: profile.metadata.resourceVersion },
          signal,
        ] as const;
        const result =
          action === "publish"
            ? await client.publishAdminEnvironmentProfile(...args)
            : await client.disableAdminEnvironmentProfile(...args);
        setProfiles((current) => replaceProfile(current, result.value));
        setProfileAudit(await loadProfileAudit(client, connection, result.value, signal));
      },
    );
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
    void runOperation(
      key,
      { key: "operation.registerTarget", values: { name: body.targetName } },
      async (signal) => {
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
        setSchedulingPreview(null);
        setTargetForm({
          targetId: "",
          targetName: "",
          targetKind: "docker",
          endpoint: "",
          credentialRef: "",
        });
        setRegistering(false);
        await reloadMaintenanceOperations(signal);
      },
    );
  }

  function probeTarget() {
    if (client === null || selectedTarget === undefined) return;
    const target = selectedTarget;
    const key = `probe:${target.metadata.uid}:${target.spec.generation}`;
    void runOperation(
      key,
      { key: "operation.probeTarget", values: { name: target.metadata.name } },
      async (signal) => {
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
        setSchedulingPreview(null);
        await reloadMaintenanceOperations(signal);
      },
    );
  }

  function previewTargetCleanup() {
    if (client === null || selectedTarget === undefined) return;
    const target = selectedTarget;
    void runOperation(
      `cleanup-preview:${target.metadata.uid}:${target.metadata.resourceVersion}`,
      {
        key: "operation.previewCleanup",
        values: { name: target.metadata.name },
      },
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

  function previewTargetScheduling() {
    if (client === null || selectedTarget === undefined) return;
    const target = selectedTarget;
    void runOperation(
      `scheduling-preview:${target.metadata.uid}:${target.metadata.resourceVersion}`,
      {
        key: "operation.previewScheduling",
        values: { name: target.metadata.name },
      },
      async (signal) => {
        const result = await client.previewAdminDeploymentTargetScheduling(
          connection.tenantId,
          connection.projectId,
          target.metadata.uid,
          newRequestId(),
          signal,
        );
        setSchedulingPreview(result.value);
        setSchedulingConfirmationOpen(true);
      },
    );
  }

  function transitionTargetScheduling() {
    if (client === null || selectedTarget === undefined || selectedSchedulingPreview === null)
      return;
    const target = selectedTarget;
    const preview = selectedSchedulingPreview;
    const action = preview.spec.desiredState === "drained" ? "drain" : "resume";
    const key = `scheduling:${target.metadata.uid}:${preview.spec.expectedGeneration}:${preview.spec.expectedResourceVersion}:${preview.spec.desiredState}:${preview.spec.impactDigest}`;
    setSchedulingConfirmationOpen(false);
    void runOperation(
      key,
      {
        key: action === "drain" ? "operation.drainTarget" : "operation.resumeTarget",
        values: { name: target.metadata.name },
      },
      async (signal) => {
        await client.transitionAdminDeploymentTargetScheduling(
          connection.tenantId,
          connection.projectId,
          target.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          schedulingRequestFromPreview(preview),
          signal,
        );
        const [result, activity] = await Promise.all([
          client.getAdminDeploymentTarget(
            connection.tenantId,
            connection.projectId,
            target.metadata.uid,
            newRequestId(),
            signal,
          ),
          loadTargetActivity(client, connection, target.metadata.uid, signal),
        ]);
        setTargets((current) => replaceTarget(current, result.value));
        setTargetOperations(activity.operations);
        setTargetAudit(activity.audit);
        setCleanupPreview(null);
        setSchedulingPreview(null);
        await reloadMaintenanceOperations(signal);
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
    void runOperation(
      key,
      {
        key: "operation.cleanupTarget",
        values: { name: target.metadata.name },
      },
      async (signal) => {
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
        setSchedulingPreview(null);
        await reloadMaintenanceOperations(signal);
      },
    );
  }

  if (!connected) {
    return (
      <main className="connect-view">
        <div className="connect-preferences">
          <select
            value={locale}
            aria-label={t("account.language")}
            onChange={(event) => setLocale(normalizeLocale(event.target.value))}
          >
            <option value="zh-CN">{t("locale.zhCN")}</option>
            <option value="en-US">{t("locale.enUS")}</option>
          </select>
          <button
            className="button outline"
            type="button"
            onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          >
            {t(theme === "dark" ? "action.lightMode" : "action.darkMode")}
          </button>
        </div>
        <section className="connect-card" aria-labelledby="connect-title">
          <div className="brand-lockup">
            <span className="brand-mark" aria-hidden="true">
              CA
            </span>
            <span>
              <strong>Cloud Agents</strong>
              <small>{t("brand.adminConsole")}</small>
            </span>
          </div>
          <div className="eyebrow">{t("connection.context")}</div>
          <h1 id="connect-title">{t("connection.title")}</h1>
          <p className="lede">{t("connection.description")}</p>
          <form className="connect-form" onSubmit={connect}>
            <label>
              <span>{t("connection.endpoint")}</span>
              <input
                type="url"
                value={connection.endpoint}
                onChange={(event) => updateConnection("endpoint", event.target.value)}
                placeholder="https://agents.example.com"
                autoComplete="url"
                required
                disabled={status === "connecting"}
              />
              <small>{t("connection.endpointHelp")}</small>
            </label>
            <div className="form-row">
              <label>
                <span>{t("connection.tenantId")}</span>
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
                <span>{t("connection.projectId")}</span>
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
              <span>{t("connection.token")}</span>
              <input
                type="password"
                value={token}
                onChange={(event) => setToken(event.target.value)}
                placeholder={t("connection.tokenPlaceholder")}
                autoComplete="off"
                spellCheck={false}
                required
                disabled={status === "connecting"}
              />
              <small>{t("connection.tokenHelp")}</small>
            </label>
            {error !== null ? (
              <p className="banner danger" role="alert">
                {t(error.key)}
              </p>
            ) : null}
            <button
              className="button primary wide"
              type="submit"
              disabled={status === "connecting"}
            >
              {t(status === "connecting" ? "connection.authorizingProgress" : "connection.connect")}
            </button>
          </form>
          <div className={`connection-state state-${status}`} role="status" aria-live="polite">
            <span className="status-dot" aria-hidden="true" />
            {statusLabel(status, t)}
          </div>
        </section>
      </main>
    );
  }

  const feedback =
    busy !== null || error !== null ? (
      <div className="operation-feedback">
        {busy !== null ? (
          <div className="banner running" role="status" aria-live="polite">
            <span className="spinner" aria-hidden="true" />
            <span>{t(busy.message.key, busy.message.values)}…</span>
            <button type="button" onClick={() => requestRef.current?.abort()}>
              {t("action.cancelWait")}
            </button>
          </div>
        ) : null}
        {error !== null ? (
          <div className="banner danger" role="alert">
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              aria-hidden="true"
            >
              <circle cx="12" cy="12" r="9" />
              <path d="M12 7v6M12 16h.01" />
            </svg>
            <div>
              {error.code !== null ? <code>{error.code}</code> : null}
              <p>{t(error.key)}</p>
            </div>
          </div>
        ) : null}
      </div>
    ) : null;

  return (
    <div className={`app-shell${sidebarOpen ? "" : " sidebar-collapsed"}`}>
      {commandsOpen ? (
        <NavigationCommands
          page={page}
          onNavigate={navigate}
          onClose={() => setCommandsOpen(false)}
        />
      ) : null}
      <AdminSidebar open={mobileNavOpen} onOpenChange={setMobileNavOpen} label={t("nav.resources")}>
        <div className="brand-lockup sidebar-brand">
          <span className="brand-mark" aria-hidden="true">
            CA
          </span>
          <span>
            <strong>Cloud Agents</strong>
            <small>{t("brand.adminConsole")}</small>
          </span>
          <button
            className="sidebar-trigger"
            type="button"
            aria-label={t(sidebarOpen ? "action.collapseSidebar" : "action.expandSidebar")}
            aria-expanded={sidebarOpen}
            title={t("action.toggleSidebar")}
            onClick={() => setSidebarOpen((open) => !open)}
          >
            <NavigationIcon name="sidebar" />
          </button>
        </div>
        <ResourceNavigation
          page={page}
          onNavigate={navigate}
          onSearch={() => setCommandsOpen(true)}
          counts={{
            targets: targets.length,
            leases: leases.length,
            workers: workers.length,
            releases: releases.length,
            profiles: profiles.length,
            storage: storagePolicies.length,
            network: networkPolicies.length,
            quotas: leaseQuota === undefined ? 0 : 1,
            maintenance: maintenanceOperations.length,
          }}
        />
        <div className="sidebar-boundary">
          <small>{t("boundary.title")}</small>
          <p>{t("boundary.description")}</p>
        </div>
      </AdminSidebar>

      <section className="app-main">
        <header className="topbar">
          <button
            className="mobile-nav-trigger"
            type="button"
            aria-label={t("action.openNavigation")}
            aria-expanded={mobileNavOpen}
            onClick={() => setMobileNavOpen(true)}
          >
            <NavigationIcon name="sidebar" />
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
            <summary className="button outline compact">{t("account.admin")}</summary>
            <div className="dropdown-menu">
              <div className="dropdown-context">
                <strong>{connection.projectId}</strong>
                <small>{connection.tenantId}</small>
              </div>
              <label className="locale-picker">
                <span>{t("account.language")}</span>
                <select
                  value={locale}
                  aria-label={t("account.language")}
                  onChange={(event) => setLocale(normalizeLocale(event.target.value))}
                >
                  <option value="zh-CN">{t("locale.zhCN")}</option>
                  <option value="en-US">{t("locale.enUS")}</option>
                </select>
              </label>
              <button
                type="button"
                onClick={(event) => {
                  setTheme(theme === "dark" ? "light" : "dark");
                  event.currentTarget.closest("details")?.removeAttribute("open");
                }}
              >
                {t(theme === "dark" ? "action.lightMode" : "action.darkMode")}
              </button>
              <button type="button" onClick={disconnect}>
                {t("action.disconnect")}
              </button>
            </div>
          </details>
        </header>

        <main className="content">
          <div className="page-heading">
            <div>
              <h1>
                {page === "overview"
                  ? t("page.overview.title")
                  : page === "targets"
                    ? t("page.targets.title")
                    : page === "workers"
                      ? t("page.workers.title")
                      : page === "releases"
                        ? t("page.releases.title")
                        : page === "profiles"
                          ? t("page.profiles.title")
                          : page === "storage"
                            ? t("page.storagePolicies.title")
                            : page === "network"
                              ? t("page.networkPolicies.title")
                              : page === "quotas"
                                ? t("page.quotas.title")
                                : page === "leases"
                                  ? t("page.leases.title")
                                  : t("page.maintenance.title")}
              </h1>
              <p>
                {page === "overview"
                  ? t("page.overview.description")
                  : page === "targets"
                    ? t("page.targets.description")
                    : page === "workers"
                      ? t("page.workers.description")
                      : page === "releases"
                        ? t("page.releases.description")
                        : page === "profiles"
                          ? t("page.profiles.description")
                          : page === "storage"
                            ? t("page.storagePolicies.description")
                            : page === "network"
                              ? t("page.networkPolicies.description")
                              : page === "quotas"
                                ? t("page.quotas.description")
                                : page === "leases"
                                  ? t("page.leases.description")
                                  : t("page.maintenance.description")}
              </p>
            </div>
            <div className="heading-actions">
              <button
                className="button outline"
                type="button"
                onClick={refresh}
                disabled={busy !== null}
              >
                {t("action.refresh")}
              </button>
              {page === "releases" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={() => setRegisteringRelease(true)}
                  disabled={busy !== null}
                >
                  {t("action.registerRelease")}
                </button>
              ) : page === "profiles" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={() => {
                    setProfileForm((current) => ({
                      ...current,
                      storagePolicyRef:
                        current.storagePolicyRef || storagePolicies[0]?.metadata.uid || "",
                      networkPolicyRef:
                        current.networkPolicyRef || networkPolicies[0]?.metadata.uid || "",
                    }));
                    setCreatingProfile(true);
                  }}
                  disabled={
                    busy !== null ||
                    releases.length === 0 ||
                    storagePolicies.length === 0 ||
                    networkPolicies.length === 0
                  }
                >
                  {t("action.createProfile")}
                </button>
              ) : page === "network" ? (
                <button
                  className="button primary"
                  type="button"
                  disabled={busy !== null}
                  onClick={() => setNetworkEditorEpoch((current) => current + 1)}
                >
                  {t("action.newNetworkPolicy")}
                </button>
              ) : page === "storage" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={newStoragePolicy}
                  disabled={busy !== null}
                >
                  {t("action.newStoragePolicy")}
                </button>
              ) : page === "overview" || page === "targets" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={() => {
                    navigate("targets");
                    setRegistering(true);
                  }}
                  disabled={busy !== null}
                >
                  {t("action.registerTarget")}
                </button>
              ) : null}
            </div>
          </div>

          {feedback}
          {notice !== null ? (
            <div className="banner success" role="status">
              {t("notice.completed", {
                operation: t(notice.key, notice.values),
              })}
            </div>
          ) : null}

          {page === "overview" ? (
            <>
              <section className="metric-grid" aria-label={t("overview.label")}>
                <button
                  type="button"
                  className="metric-card"
                  data-metric="targets"
                  onClick={() => navigate("targets")}
                >
                  <small>{t("overview.totalTargets")}</small>
                  <strong>{number(targets.length)}</strong>
                  <span>{t("overview.targetKinds")}</span>
                </button>
                <button
                  type="button"
                  className="metric-card warning-accent"
                  data-metric="target-attention"
                  onClick={() => {
                    navigate("targets");
                    setTargetPhaseFilter(["unprobed", "probing", "unavailable"]);
                  }}
                >
                  <small>{t("overview.targetAttention")}</small>
                  <strong>{number(attentionCount)}</strong>
                  <span>
                    {t("overview.targetSummary", {
                      ready: number(readyCount),
                      probing: number(probingCount),
                      unprobed: number(unprobedCount),
                      unavailable: number(unavailableCount),
                    })}
                  </span>
                </button>
                <button
                  type="button"
                  className="metric-card success-accent"
                  data-metric="leases"
                  onClick={() => navigate("leases")}
                >
                  <small>{t("overview.environmentLeases")}</small>
                  <strong>{number(leases.length)}</strong>
                  <span>
                    {t("overview.readyLeases", {
                      count: number(readyLeaseCount),
                    })}
                  </span>
                </button>
                <button
                  type="button"
                  className="metric-card success-accent"
                  data-metric="workers"
                  onClick={() => navigate("workers")}
                >
                  <small>{t("overview.workers")}</small>
                  <strong>{number(workers.length)}</strong>
                  <span>
                    {t("overview.readyWorkers", {
                      count: number(readyWorkerCount),
                    })}
                  </span>
                </button>
                <button
                  type="button"
                  className="metric-card warning-accent"
                  data-metric="lease-attention"
                  onClick={() => {
                    navigate("leases");
                    setLeaseAttentionOnly(true);
                  }}
                >
                  <small>{t("overview.leaseAttention")}</small>
                  <strong>{number(leaseAttentionCount)}</strong>
                  <span>{t("overview.leaseAttentionDescription")}</span>
                </button>
              </section>
              <section
                className="panel overview-panel recent-failed-operations"
                aria-labelledby="recent-failed-operations-title"
              >
                <div className="panel-heading">
                  <div>
                    <h2 id="recent-failed-operations-title">{t("overview.failedOperations")}</h2>
                    <p>{t("overview.failedOperationsDescription")}</p>
                  </div>
                  <button
                    className="text-button"
                    type="button"
                    onClick={() => {
                      navigate("maintenance");
                      setFailedOperationsOnly(true);
                    }}
                  >
                    {t("overview.viewFailedOperations", {
                      count: number(failedMaintenanceOperations.length),
                    })}
                  </button>
                </div>
                <MaintenanceOperationTable
                  operations={failedMaintenanceOperations.slice(0, 6)}
                  emptyMessage="overview.noFailedOperations"
                  selectedOperationId={selectedMaintenanceOperationId}
                  onSelect={selectMaintenanceOperation}
                />
              </section>
              <section className="panel overview-panel">
                <div className="panel-heading">
                  <div>
                    <h2>{t("overview.targetHealth")}</h2>
                    <p>{t("overview.liveResources")}</p>
                  </div>
                  <button className="text-button" type="button" onClick={() => navigate("targets")}>
                    {t("overview.viewTargets")}
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
                    <h2>{t("overview.leaseLifecycle")}</h2>
                    <p>{t("overview.leaseLifecycleDescription")}</p>
                  </div>
                  <button className="text-button" type="button" onClick={() => navigate("leases")}>
                    {t("overview.viewLeases")}
                  </button>
                </div>
                <div
                  className="lease-state-summary"
                  role="group"
                  aria-label={t("overview.leaseStates")}
                >
                  {(["provisioning", "ready", "terminating", "terminated", "failed"] as const).map(
                    (phase) => (
                      <button
                        key={phase}
                        className="button outline"
                        type="button"
                        data-lease-state={phase}
                        onClick={() => {
                          navigate("leases");
                          setLeasePhaseFilter(phase);
                        }}
                      >
                        {phaseLabel(phase, t)} ·{" "}
                        {number(filterAdminLeases(leases, "", false, phase).length)}
                      </button>
                    ),
                  )}
                  <button
                    className="button outline"
                    type="button"
                    data-lease-state="cleanup-blocked"
                    onClick={() => {
                      navigate("leases");
                      setLeaseCleanupBlockedOnly(true);
                    }}
                  >
                    {t("overview.cleanupBlocked")} ·{" "}
                    {number(filterAdminLeases(leases, "", false, "", true).length)}
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
              <div className="list-toolbar target-toolbar">
                <input
                  type="search"
                  aria-label={t("search.targets.label")}
                  placeholder={t("search.targets.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <TargetFilters
                  kinds={targetKindFilter}
                  phases={targetPhaseFilter}
                  onKinds={setTargetKindFilter}
                  onPhases={setTargetPhaseFilter}
                />
                <button
                  className="button outline target-filters-clear"
                  type="button"
                  disabled={!targetsFiltered}
                  onClick={() => {
                    setQuery("");
                    setTargetKindFilter([]);
                    setTargetPhaseFilter([]);
                  }}
                >
                  {t("target.filter.clear")}
                </button>
                <span className="scope-chip" role="status">
                  targets.list · {number(visibleTargets.length)}
                </span>
              </div>
              <div className="panel target-list-panel">
                <TargetTable
                  targets={visibleTargets}
                  filtered={targetsFiltered}
                  selectedTargetId={selectedTargetId}
                  onSelect={selectTarget}
                />
              </div>
            </section>
          ) : page === "workers" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label={t("search.workers.label")}
                  placeholder={t("search.workers.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">
                  targets.list · {number(visibleClusterHosts.length)}
                </span>
                <span className="scope-chip">workers.list · {number(visibleWorkers.length)}</span>
              </div>
              <div className="panel target-list-panel">
                <div className="panel-heading">
                  <div>
                    <h2>{t("cluster.overviewTitle")}</h2>
                    <p>{t("cluster.overviewDescription")}</p>
                  </div>
                </div>
                <ClusterHostTable
                  summaries={visibleClusterHosts}
                  selectedTargetId={selectedTargetId}
                  onSelect={selectTarget}
                />
              </div>
              <div className="panel target-list-panel">
                <div className="panel-heading">
                  <div>
                    <h2>{t("cluster.workersTitle")}</h2>
                    <p>{t("cluster.workersDescription")}</p>
                  </div>
                </div>
                <WorkerTable
                  workers={visibleWorkers}
                  selectedWorkerId={selectedWorkerId}
                  onSelect={selectWorker}
                />
              </div>
              <p className="cluster-boundary">{t("cluster.authorityBoundary")}</p>
            </section>
          ) : page === "releases" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label={t("search.releases.label")}
                  placeholder={t("search.releases.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">releases.list · {number(visibleReleases.length)}</span>
              </div>
              <div className="panel target-list-panel">
                <ReleaseTable releases={visibleReleases} />
              </div>
            </section>
          ) : page === "profiles" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label={t("search.profiles.label")}
                  placeholder={t("search.profiles.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">profiles.list · {number(visibleProfiles.length)}</span>
              </div>
              <div className="panel target-list-panel">
                <ProfileTable
                  profiles={visibleProfiles}
                  selectedProfileVersionId={selectedProfileVersionId}
                  onSelect={selectProfile}
                />
              </div>
            </section>
          ) : page === "network" && client !== null ? (
            <NetworkPolicyPanel
              key={networkEditorEpoch}
              client={client}
              connection={connection}
              policies={networkPolicies}
              profiles={profiles}
              query={query}
              busy={busy !== null}
              onQuery={setQuery}
              onChange={setNetworkPolicies}
              run={runOperation}
              idempotencyKey={idempotencyKey}
            />
          ) : page === "storage" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label={t("search.storagePolicies.label")}
                  placeholder={t("search.storagePolicies.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">
                  storage-policies.list · {number(visibleStoragePolicies.length)}
                </span>
              </div>
              <div className="panel target-list-panel">
                <StoragePolicyTable
                  policies={visibleStoragePolicies}
                  selectedPolicyId={selectedStoragePolicyId}
                  onSelect={selectStoragePolicy}
                />
              </div>

              <section className="panel overview-panel">
                <div className="panel-heading">
                  <div>
                    <h2>{t("storagePolicy.formTitle")}</h2>
                    <p>{t("storagePolicy.formDescription")}</p>
                  </div>
                  <span className="scope-chip">storage-policies.get · storage-policies.update</span>
                </div>
                <form className="resource-form" onSubmit={saveStoragePolicy}>
                  <div className="form-row">
                    <label>
                      <span>{t("storagePolicy.id")}</span>
                      <input
                        required
                        maxLength={128}
                        spellCheck={false}
                        value={storagePolicyForm.policyId}
                        disabled={selectedStoragePolicy !== undefined}
                        onChange={(event) =>
                          setStoragePolicyForm((current) => ({
                            ...current,
                            policyId: event.target.value,
                          }))
                        }
                        placeholder="storage-standard"
                      />
                    </label>
                    <label>
                      <span>{t("storagePolicy.name")}</span>
                      <input
                        required
                        maxLength={128}
                        spellCheck={false}
                        value={storagePolicyForm.policyName}
                        disabled={selectedStoragePolicyReferenced}
                        onChange={(event) =>
                          setStoragePolicyForm((current) => ({
                            ...current,
                            policyName: event.target.value,
                          }))
                        }
                        placeholder="storage-standard"
                      />
                    </label>
                  </div>
                  <label>
                    <span>{t("storagePolicy.userSummary")}</span>
                    <input
                      required
                      maxLength={256}
                      value={storagePolicyForm.userSummary}
                      disabled={selectedStoragePolicyReferenced}
                      onChange={(event) =>
                        setStoragePolicyForm((current) => ({
                          ...current,
                          userSummary: event.target.value,
                        }))
                      }
                      placeholder={t("storagePolicy.userSummaryPlaceholder")}
                    />
                    <small>{t("storagePolicy.userSummaryHelp")}</small>
                  </label>
                  <label>
                    <span>{t("storagePolicy.capacityGiB")}</span>
                    <input
                      required
                      type="number"
                      min="0.125"
                      max="1024"
                      step="0.125"
                      value={storagePolicyForm.workspaceCapacityGiB}
                      disabled={selectedStoragePolicyReferenced}
                      onChange={(event) =>
                        setStoragePolicyForm((current) => ({
                          ...current,
                          workspaceCapacityGiB: event.target.value,
                        }))
                      }
                    />
                  </label>
                  <div className="form-row">
                    <label>
                      <span>{t("storagePolicy.snapshotBackendRef")}</span>
                      <input
                        maxLength={128}
                        spellCheck={false}
                        value={storagePolicyForm.snapshotBackendRef}
                        disabled={selectedStoragePolicyReferenced}
                        onChange={(event) =>
                          setStoragePolicyForm((current) => ({
                            ...current,
                            snapshotBackendRef: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label>
                      <span>{t("storagePolicy.artifactBackendRef")}</span>
                      <input
                        maxLength={128}
                        spellCheck={false}
                        value={storagePolicyForm.artifactBackendRef}
                        disabled={selectedStoragePolicyReferenced}
                        onChange={(event) =>
                          setStoragePolicyForm((current) => ({
                            ...current,
                            artifactBackendRef: event.target.value,
                          }))
                        }
                      />
                    </label>
                  </div>
                  <p className="cluster-boundary">
                    {t(
                      selectedStoragePolicyReferenced
                        ? "storagePolicy.referencedBoundary"
                        : "storagePolicy.lifecycleBoundary",
                    )}
                  </p>
                  <button
                    className="button primary"
                    type="submit"
                    disabled={busy !== null || selectedStoragePolicyReferenced}
                  >
                    {t("storagePolicy.save")}
                  </button>
                </form>
              </section>

              <section className="activity-block" aria-labelledby="storage-policy-audit-title">
                <div className="activity-heading">
                  <h2 id="storage-policy-audit-title">{t("storagePolicy.audit")}</h2>
                  <span className="scope-chip">
                    audit.list · {number(storagePolicyAudit.length)}
                  </span>
                </div>
                {storagePolicyAudit.length === 0 ? (
                  <p className="activity-empty">{t("storagePolicy.noAudit")}</p>
                ) : (
                  <ol className="activity-list compact">
                    {storagePolicyAudit.map((event) => (
                      <li key={event.eventId}>
                        <div>
                          <strong>{auditLabel(event.action, t)}</strong>
                          <span className={`phase ${phaseTone(event.result)}`}>
                            <i /> {phaseLabel(event.result, t)}
                          </span>
                        </div>
                        <small className="mono break">
                          {t("common.actor", { actor: event.actor })}
                        </small>
                        <small className="mono">
                          {event.requestId} · {dateTime(event.occurredAt)}
                        </small>
                      </li>
                    ))}
                  </ol>
                )}
              </section>
            </section>
          ) : page === "quotas" ? (
            <section className="resource-list">
              <section className="metric-grid" aria-label={t("quota.summary")}>
                <article className="metric-card">
                  <small>{t("quota.concurrent")}</small>
                  <strong>
                    {leaseQuota === undefined
                      ? "—"
                      : `${number(leaseQuota.status.activeLeases)} / ${number(leaseQuota.spec.maxConcurrentLeases)}`}
                  </strong>
                  <span>{t("quota.activeLeases")}</span>
                </article>
                <article className="metric-card">
                  <small>{t("quota.cpu")}</small>
                  <strong>
                    {leaseQuota === undefined
                      ? "—"
                      : `${number(leaseQuota.status.usedCpuMillis)} / ${number(leaseQuota.spec.maxCpuMillis)}`}
                  </strong>
                  <span>mCPU</span>
                </article>
                <article className="metric-card">
                  <small>{t("quota.memory")}</small>
                  <strong>
                    {leaseQuota === undefined
                      ? "—"
                      : `${number(Math.round(leaseQuota.status.usedMemoryBytes / 1_048_576))} / ${number(Math.round(leaseQuota.spec.maxMemoryBytes / 1_048_576))}`}
                  </strong>
                  <span>MiB</span>
                </article>
                <article className="metric-card">
                  <small>{t("quota.maxTtl")}</small>
                  <strong>
                    {leaseQuota === undefined
                      ? "—"
                      : number(Math.floor(leaseQuota.spec.maxLeaseTtlSeconds / 60))}
                  </strong>
                  <span>{t("quota.minutes")}</span>
                </article>
                <article className="metric-card">
                  <small>{t("detail.resourceVersion")}</small>
                  <strong>{leaseQuota?.metadata.resourceVersion ?? "—"}</strong>
                  <span>
                    {leaseQuota === undefined ? t("quota.notConfigured") : t("quota.configured")}
                  </span>
                </article>
              </section>

              <section className="panel overview-panel">
                <div className="panel-heading">
                  <div>
                    <h2>{t("quota.formTitle")}</h2>
                    <p>{t("quota.formDescription")}</p>
                  </div>
                  <span className="scope-chip">quotas.get · quotas.update</span>
                </div>
                <form className="resource-form" onSubmit={updateLeaseQuota}>
                  <div className="form-row">
                    <label>
                      <span>{t("quota.concurrent")}</span>
                      <input
                        required
                        type="number"
                        min="1"
                        max="8000"
                        value={quotaForm.maxConcurrentLeases}
                        onChange={(event) =>
                          setQuotaForm((current) => ({
                            ...current,
                            maxConcurrentLeases: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label>
                      <span>{t("quota.cpuMillis")}</span>
                      <input
                        required
                        type="number"
                        min="100"
                        max="512000000"
                        step="100"
                        value={quotaForm.maxCpuMillis}
                        onChange={(event) =>
                          setQuotaForm((current) => ({
                            ...current,
                            maxCpuMillis: event.target.value,
                          }))
                        }
                      />
                    </label>
                  </div>
                  <div className="form-row">
                    <label>
                      <span>{t("quota.memoryMiB")}</span>
                      <input
                        required
                        type="number"
                        min="128"
                        max="8388608000"
                        value={quotaForm.maxMemoryMiB}
                        onChange={(event) =>
                          setQuotaForm((current) => ({
                            ...current,
                            maxMemoryMiB: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label>
                      <span>{t("quota.ttlSeconds")}</span>
                      <input
                        required
                        type="number"
                        min="60"
                        max="86400"
                        value={quotaForm.maxLeaseTtlSeconds}
                        onChange={(event) =>
                          setQuotaForm((current) => ({
                            ...current,
                            maxLeaseTtlSeconds: event.target.value,
                          }))
                        }
                      />
                    </label>
                  </div>
                  <p className="cluster-boundary">{t("quota.boundary")}</p>
                  <button className="button primary" type="submit" disabled={busy !== null}>
                    {t("quota.save")}
                  </button>
                </form>
              </section>

              <section className="activity-block" aria-labelledby="quota-audit-title">
                <div className="activity-heading">
                  <h2 id="quota-audit-title">{t("quota.audit")}</h2>
                  <span className="scope-chip">audit.list · {number(leaseQuotaAudit.length)}</span>
                </div>
                {leaseQuotaAudit.length === 0 ? (
                  <p className="activity-empty">{t("quota.noAudit")}</p>
                ) : (
                  <ol className="activity-list compact">
                    {leaseQuotaAudit.map((event) => (
                      <li key={event.eventId}>
                        <div>
                          <strong>{auditLabel(event.action, t)}</strong>
                          <span className={`phase ${phaseTone(event.result)}`}>
                            <i /> {phaseLabel(event.result, t)}
                          </span>
                        </div>
                        <small className="mono break">
                          {t("common.actor", { actor: event.actor })}
                        </small>
                        <small className="mono">
                          {event.requestId} · {dateTime(event.occurredAt)}
                        </small>
                      </li>
                    ))}
                  </ol>
                )}
              </section>
            </section>
          ) : page === "leases" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label={t("search.leases.label")}
                  placeholder={t("search.leases.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <button
                  className="button outline state-filter lease-attention-filter"
                  type="button"
                  aria-pressed={leaseAttentionOnly}
                  onClick={() => setLeaseAttentionOnly((current) => !current)}
                  title={t("overview.leaseAttentionDescription")}
                >
                  {t("overview.leaseAttention")}
                </button>
                <span className="scope-chip" role="status">
                  leases.list · {number(visibleLeases.length)}
                </span>
                {leasePhaseFilter !== "" || leaseCleanupBlockedOnly ? (
                  <div
                    className="lease-active-state"
                    role="group"
                    aria-label={t("overview.leaseStates")}
                  >
                    <span className="scope-chip">
                      {leaseCleanupBlockedOnly
                        ? t("overview.cleanupBlocked")
                        : `${t("table.observed")}: ${phaseLabel(leasePhaseFilter, t)}`}
                    </span>
                    <button
                      className="button outline"
                      type="button"
                      onClick={() => {
                        setLeasePhaseFilter("");
                        setLeaseCleanupBlockedOnly(false);
                      }}
                    >
                      {t("lease.clearState")}
                    </button>
                  </div>
                ) : null}
              </div>
              <div className="panel target-list-panel">
                <LeaseTable
                  leases={visibleLeases}
                  filtered={
                    leaseAttentionOnly ||
                    leasePhaseFilter !== "" ||
                    leaseCleanupBlockedOnly ||
                    query.trim() !== ""
                  }
                  selectedLeaseId={selectedLeaseId}
                  onSelect={selectLease}
                />
              </div>
            </section>
          ) : (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label={t("search.maintenance.label")}
                  placeholder={t("search.maintenance.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <button
                  className="button outline state-filter maintenance-failed-filter"
                  type="button"
                  aria-pressed={failedOperationsOnly}
                  onClick={() => setFailedOperationsOnly((current) => !current)}
                >
                  {t("maintenance.failedOnly")}
                </button>
                <span className="scope-chip" role="status">
                  operations.list · {number(visibleMaintenanceOperations.length)}
                </span>
              </div>
              <div className="panel target-list-panel">
                <MaintenanceOperationTable
                  operations={visibleMaintenanceOperations}
                  emptyMessage={
                    failedOperationsOnly || query.trim() !== ""
                      ? "maintenance.noMatches"
                      : "table.empty.maintenance"
                  }
                  selectedOperationId={selectedMaintenanceOperationId}
                  onSelect={selectMaintenanceOperation}
                />
              </div>
            </section>
          )}
        </main>
      </section>

      {targetDetailOpen && selectedTarget !== undefined ? (
        <AdminSheet
          label={t("sheet.target", { name: selectedTarget.metadata.name })}
          feedback={feedback}
          onClose={() => {
            setTargetDetailOpen(false);
            setCleanupConfirmationOpen(false);
            setSchedulingConfirmationOpen(false);
          }}
        >
          <aside className="detail-panel" aria-label={t("sheet.selectedTarget")}>
            <button
              className="sheet-close"
              type="button"
              aria-label={t("action.close")}
              onClick={() => {
                setTargetDetailOpen(false);
                setCleanupConfirmationOpen(false);
                setSchedulingConfirmationOpen(false);
              }}
            >
              ×
            </button>
            <TargetDetail
              target={selectedTarget}
              operations={targetOperations}
              audit={targetAudit}
              onProbe={probeTarget}
              onPreviewScheduling={previewTargetScheduling}
              onPreviewCleanup={previewTargetCleanup}
              disabled={busy !== null}
            />
          </aside>
        </AdminSheet>
      ) : null}

      {schedulingConfirmationOpen &&
      selectedTarget !== undefined &&
      selectedSchedulingPreview !== null ? (
        <AdminSheet
          label={t("sheet.scheduling", { name: selectedTarget.metadata.name })}
          feedback={feedback}
          confirmation
          returnFocus={operationTriggerRef.current}
          onClose={() => setSchedulingConfirmationOpen(false)}
        >
          <SchedulingConfirmation
            target={selectedTarget}
            preview={selectedSchedulingPreview}
            disabled={busy !== null}
            onClose={() => setSchedulingConfirmationOpen(false)}
            onConfirm={transitionTargetScheduling}
          />
        </AdminSheet>
      ) : null}

      {cleanupConfirmationOpen &&
      selectedTarget !== undefined &&
      selectedCleanupPreview !== null ? (
        <AdminSheet
          label={t("sheet.cleanup", { name: selectedTarget.metadata.name })}
          feedback={feedback}
          confirmation
          returnFocus={operationTriggerRef.current}
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
          label={t("sheet.lease", { name: selectedLease.metadata.name })}
          feedback={feedback}
          onClose={() => {
            setLeaseDetailOpen(false);
            setLeaseReleaseConfirmationOpen(false);
          }}
        >
          <aside className="detail-panel" aria-label={t("sheet.selectedLease")}>
            <button
              className="sheet-close"
              type="button"
              aria-label={t("action.close")}
              onClick={() => {
                setLeaseDetailOpen(false);
                setLeaseReleaseConfirmationOpen(false);
              }}
            >
              ×
            </button>
            <LeaseDetail
              lease={selectedLease}
              target={selectedLeaseTarget}
              releases={releases}
              upgradeReleaseDigest={upgradeReleaseDigest}
              onUpgradeReleaseDigestChange={setSelectedUpgradeReleaseDigest}
              onPreviewUpgrade={() => previewLeaseRelease("upgrade")}
              onPreviewRollback={() => previewLeaseRelease("rollback")}
              disabled={busy !== null}
            />
          </aside>
        </AdminSheet>
      ) : null}

      {leaseReleaseConfirmationOpen &&
      selectedLease !== undefined &&
      selectedLeaseReleasePreview !== null ? (
        <AdminSheet
          label={t("sheet.leaseRelease", { name: selectedLease.metadata.name })}
          feedback={feedback}
          confirmation
          returnFocus={operationTriggerRef.current}
          onClose={() => setLeaseReleaseConfirmationOpen(false)}
        >
          <LeaseReleaseConfirmation
            lease={selectedLease}
            preview={selectedLeaseReleasePreview}
            disabled={busy !== null}
            onClose={() => setLeaseReleaseConfirmationOpen(false)}
            onConfirm={transitionLeaseRelease}
          />
        </AdminSheet>
      ) : null}

      {workerDetailOpen && selectedWorker !== undefined ? (
        <AdminSheet
          label={t("sheet.worker", { name: selectedWorker.metadata.name })}
          feedback={feedback}
          onClose={() => setWorkerDetailOpen(false)}
        >
          <aside className="detail-panel" aria-label={t("sheet.selectedWorker")}>
            <button
              className="sheet-close"
              type="button"
              aria-label={t("action.close")}
              onClick={() => setWorkerDetailOpen(false)}
            >
              ×
            </button>
            <WorkerDetail worker={selectedWorker} />
          </aside>
        </AdminSheet>
      ) : null}

      {profileDetailOpen && selectedProfile !== undefined ? (
        <AdminSheet
          label={t("sheet.profile", { name: selectedProfile.metadata.name })}
          feedback={feedback}
          onClose={() => {
            setProfileDetailOpen(false);
            setProfileTransition(null);
          }}
        >
          <aside className="detail-panel" aria-label={t("sheet.selectedProfile")}>
            <button
              className="sheet-close"
              type="button"
              aria-label={t("action.close")}
              onClick={() => {
                setProfileDetailOpen(false);
                setProfileTransition(null);
              }}
            >
              ×
            </button>
            <ProfileDetail
              profile={selectedProfile}
              audit={profileAudit}
              disabled={busy !== null}
              onTransition={setProfileTransition}
            />
          </aside>
        </AdminSheet>
      ) : null}

      {maintenanceDetailOpen && selectedMaintenanceOperation !== undefined ? (
        <AdminSheet
          feedback={feedback}
          label={t("sheet.maintenance", {
            id: selectedMaintenanceOperation.operationId,
          })}
          onClose={() => setMaintenanceDetailOpen(false)}
        >
          <aside className="detail-panel" aria-label={t("sheet.selectedMaintenance")}>
            <button
              className="sheet-close"
              type="button"
              aria-label={t("action.close")}
              onClick={() => setMaintenanceDetailOpen(false)}
            >
              ×
            </button>
            <MaintenanceOperationDetail operation={selectedMaintenanceOperation} />
          </aside>
        </AdminSheet>
      ) : null}

      {profileTransition !== null && selectedProfile !== undefined ? (
        <AdminSheet
          confirmation
          feedback={feedback}
          label={t("sheet.profileTransition", {
            action: t(
              profileTransition === "publish"
                ? "profile.transition.publish"
                : "profile.transition.disable",
            ),
            name: selectedProfile.metadata.name,
          })}
          onClose={() => setProfileTransition(null)}
        >
          <ProfileTransitionConfirmation
            profile={selectedProfile}
            action={profileTransition}
            disabled={busy !== null}
            onClose={() => setProfileTransition(null)}
            onConfirm={transitionProfile}
          />
        </AdminSheet>
      ) : null}

      {registeringRelease ? (
        <AdminSheet
          label={t("release.register.title")}
          feedback={feedback}
          onClose={() => setRegisteringRelease(false)}
        >
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
                onClick={() => setRegisteringRelease(false)}
              >
                ×
              </button>
            </div>
            <form className="resource-form" onSubmit={registerRelease}>
              <div className="form-row">
                <label>
                  <span>{t("release.id")}</span>
                  <input
                    value={releaseForm.releaseId}
                    onChange={(event) =>
                      setReleaseForm({
                        ...releaseForm,
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
                    value={releaseForm.releaseName}
                    onChange={(event) =>
                      setReleaseForm({
                        ...releaseForm,
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
                  value={releaseForm.imageRepository}
                  onChange={(event) =>
                    setReleaseForm({
                      ...releaseForm,
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
                  value={releaseForm.releaseDigest}
                  onChange={(event) =>
                    setReleaseForm({
                      ...releaseForm,
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
                    value={releaseForm.platformVersion}
                    onChange={(event) =>
                      setReleaseForm({
                        ...releaseForm,
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
                    value={releaseForm.runtimeVersion}
                    onChange={(event) =>
                      setReleaseForm({
                        ...releaseForm,
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
                    value={releaseForm.codexVersion}
                    onChange={(event) =>
                      setReleaseForm({
                        ...releaseForm,
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
                    value={releaseForm.claudeCodeVersion}
                    onChange={(event) =>
                      setReleaseForm({
                        ...releaseForm,
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
                    checked={releaseForm.amd64}
                    required={!releaseForm.arm64}
                    onChange={(event) =>
                      setReleaseForm({
                        ...releaseForm,
                        amd64: event.target.checked,
                      })
                    }
                  />
                  <span>linux/amd64</span>
                </label>
                <label className="confirmation-check">
                  <input
                    type="checkbox"
                    checked={releaseForm.arm64}
                    required={!releaseForm.amd64}
                    onChange={(event) =>
                      setReleaseForm({
                        ...releaseForm,
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
                  value={releaseForm.verificationEvidenceDigest}
                  onChange={(event) =>
                    setReleaseForm({
                      ...releaseForm,
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
                <button
                  className="button ghost"
                  type="button"
                  onClick={() => setRegisteringRelease(false)}
                >
                  {t("action.cancel")}
                </button>
                <button className="button primary" type="submit" disabled={busy !== null}>
                  {t("action.registerRelease")}
                </button>
              </div>
            </form>
          </section>
        </AdminSheet>
      ) : null}

      {creatingProfile ? (
        <AdminSheet
          feedback={feedback}
          label={t("profile.create.title")}
          onClose={() => setCreatingProfile(false)}
        >
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
                onClick={() => setCreatingProfile(false)}
              >
                ×
              </button>
            </div>
            <form className="resource-form" onSubmit={createProfile}>
              <div className="form-row">
                <label>
                  <span>{t("profile.id")}</span>
                  <input
                    value={profileForm.profileId}
                    onChange={(event) =>
                      setProfileForm({
                        ...profileForm,
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
                    value={profileForm.profileName}
                    onChange={(event) =>
                      setProfileForm({
                        ...profileForm,
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
                  value={profileForm.version}
                  onChange={(event) =>
                    setProfileForm({
                      ...profileForm,
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
                  value={profileForm.description}
                  onChange={(event) =>
                    setProfileForm({
                      ...profileForm,
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
                    checked={profileForm.codex}
                    onChange={(event) =>
                      setProfileForm({
                        ...profileForm,
                        codex: event.target.checked,
                      })
                    }
                  />
                  <span>Codex</span>
                </label>
                <label className="confirmation-check">
                  <input
                    type="checkbox"
                    checked={profileForm.claudeAgent}
                    onChange={(event) =>
                      setProfileForm({
                        ...profileForm,
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
                    value={profileForm.cpuLimitMillis}
                    onChange={(event) =>
                      setProfileForm({
                        ...profileForm,
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
                    value={profileForm.memoryLimitMiB}
                    onChange={(event) =>
                      setProfileForm({
                        ...profileForm,
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
                  value={profileForm.storagePolicyRef}
                  onChange={(event) =>
                    setProfileForm({
                      ...profileForm,
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
                  value={profileForm.networkPolicyRef}
                  onChange={(event) =>
                    setProfileForm({
                      ...profileForm,
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
                  value={profileForm.releaseDigest}
                  onChange={(event) =>
                    setProfileForm({
                      ...profileForm,
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
                  value={profileForm.targetRefs}
                  onChange={(event) =>
                    setProfileForm({
                      ...profileForm,
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
                  value={profileForm.providerCredentialRef}
                  onChange={(event) =>
                    setProfileForm({
                      ...profileForm,
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
                <button
                  className="button ghost"
                  type="button"
                  onClick={() => setCreatingProfile(false)}
                >
                  {t("action.cancel")}
                </button>
                <button className="button primary" type="submit" disabled={busy !== null}>
                  {t("profile.createDraft")}
                </button>
              </div>
            </form>
          </section>
        </AdminSheet>
      ) : null}

      {registering ? (
        <AdminSheet
          feedback={feedback}
          label={t("target.register.title")}
          onClose={() => setRegistering(false)}
        >
          <section className="dialog" aria-labelledby="register-title">
            <div className="panel-heading">
              <div>
                <div className="eyebrow">{t("target.register.eyebrow")}</div>
                <h2 id="register-title">{t("target.register.title")}</h2>
                <p>{t("target.register.description")}</p>
              </div>
              <button
                className="icon-button"
                type="button"
                aria-label={t("action.close")}
                onClick={() => setRegistering(false)}
              >
                ×
              </button>
            </div>
            <form className="resource-form" onSubmit={registerTarget}>
              <div className="form-row">
                <label>
                  <span>{t("target.id")}</span>
                  <input
                    value={targetForm.targetId}
                    onChange={(event) =>
                      setTargetForm({
                        ...targetForm,
                        targetId: event.target.value,
                      })
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
                  <span>{t("target.displayName")}</span>
                  <input
                    value={targetForm.targetName}
                    onChange={(event) =>
                      setTargetForm({
                        ...targetForm,
                        targetName: event.target.value,
                      })
                    }
                    placeholder="docker-primary"
                    maxLength={128}
                    required
                  />
                </label>
              </div>
              <label>
                <span>{t("target.kind")}</span>
                <select
                  value={targetForm.targetKind}
                  onChange={(event) =>
                    setTargetForm({
                      ...targetForm,
                      targetKind: event.target.value as TargetKind,
                    })
                  }
                >
                  <option value="docker">{t("target.kind.docker")}</option>
                  <option value="kubernetes">{t("target.kind.kubernetes")}</option>
                  <option value="ssh">{t("target.kind.ssh")}</option>
                </select>
              </label>
              <label>
                <span>{t("target.endpoint")}</span>
                <input
                  type="url"
                  value={targetForm.endpoint}
                  onChange={(event) =>
                    setTargetForm({
                      ...targetForm,
                      endpoint: event.target.value,
                    })
                  }
                  placeholder={targetEndpointPlaceholder[targetForm.targetKind]}
                  maxLength={2048}
                  required
                  spellCheck={false}
                />
                <small>{t("target.endpointHelp")}</small>
              </label>
              <label>
                <span>{t("target.credentialRef")}</span>
                <input
                  value={targetForm.credentialRef}
                  onChange={(event) =>
                    setTargetForm({
                      ...targetForm,
                      credentialRef: event.target.value,
                    })
                  }
                  placeholder={`${targetForm.targetKind}-primary`}
                  maxLength={128}
                  required
                  spellCheck={false}
                />
                <small>{t("target.credentialRefHelp")}</small>
              </label>
              <div className="dialog-actions">
                <button
                  className="button ghost"
                  type="button"
                  onClick={() => setRegistering(false)}
                >
                  {t("action.cancel")}
                </button>
                <button className="button primary" type="submit" disabled={busy !== null}>
                  {t("action.registerTarget")}
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
  filtered = false,
  selectedTargetId,
  onSelect,
}: Readonly<{
  targets: readonly DeploymentTarget[];
  filtered?: boolean;
  selectedTargetId: string;
  onSelect: (targetId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (targets.length === 0)
    return (
      <div className="table-empty">
        {t(filtered ? "target.filter.noMatches" : "table.empty.targets")}
      </div>
    );
  return (
    <div className="table-scroll" tabIndex={0} role="region" aria-label={t("page.targets.title")}>
      <table className="target-table">
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("table.kind")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.engineApi")}</th>
            <th>{t("table.osArchitecture")}</th>
            <th>{t("table.generation")}</th>
            <th>{t("table.lastProbe")}</th>
            <th aria-label={t("table.actions")} />
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
                <span className="kind-badge" data-kind={target.spec.targetKind}>
                  {targetKindLabel(target.spec.targetKind, t)}
                </span>
              </td>
              <td>
                <span className={`phase ${phaseTone(target.spec.observedPhase)}`}>
                  <i /> {phaseLabel(target.spec.observedPhase, t)}
                </span>
                <small className="table-subline">
                  {t("detail.schedulingState")}: {phaseLabel(target.spec.schedulingState, t)}
                </small>
              </td>
              <td className="target-probe-facts">
                <span>{target.spec.engineVersion || t("common.notAvailable")}</span>
                <small className="table-subline">
                  {t("cluster.apiVersion", {
                    version: target.spec.apiVersion || t("common.notAvailable"),
                  })}
                </small>
              </td>
              <td className="target-probe-facts">
                {target.spec.os && target.spec.architecture
                  ? `${target.spec.os} / ${target.spec.architecture}`
                  : t("common.notAvailable")}
              </td>
              <td className="mono">g{number(target.spec.generation)}</td>
              <td>{dateTime(target.spec.lastProbeAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: target.metadata.name })}
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
  filtered = false,
  selectedLeaseId,
  onSelect,
}: Readonly<{
  leases: readonly EnvironmentLease[];
  filtered?: boolean;
  selectedLeaseId: string;
  onSelect: (leaseId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
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
            <th>{t("table.generation")}</th>
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
              <td className="mono">g{number(lease.spec.generation)}</td>
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

function ClusterHostTable({
  summaries,
  selectedTargetId,
  onSelect,
}: Readonly<{
  summaries: readonly ClusterHostSummary[];
  selectedTargetId: string;
  onSelect: (targetId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (summaries.length === 0)
    return <div className="table-empty">{t("table.empty.clusterHosts")}</div>;
  return (
    <div className="table-scroll">
      <table className="cluster-host-table">
        <thead>
          <tr>
            <th>{t("table.name")}</th>
            <th>{t("table.kind")}</th>
            <th>{t("table.runtime")}</th>
            <th>{t("table.platform")}</th>
            <th>{t("table.workers")}</th>
            <th>{t("table.lastHealth")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.lastProbe")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {summaries.map(({ target, workerCount, readyWorkerCount, latestHealthAt }) => (
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
                <span className="kind-badge">{targetKindLabel(target.spec.targetKind, t)}</span>
              </td>
              <td>
                <strong>{target.spec.engineVersion || t("common.notObserved")}</strong>
                <small className="table-subline">
                  {target.spec.apiVersion
                    ? t("cluster.apiVersion", {
                        version: target.spec.apiVersion,
                      })
                    : t("common.notObserved")}
                </small>
              </td>
              <td>
                {[target.spec.os, target.spec.architecture].filter(Boolean).join(" / ") ||
                  t("common.notObserved")}
              </td>
              <td>
                {workerCount === 0
                  ? t("cluster.noWorkers")
                  : t("cluster.workerSummary", {
                      ready: number(readyWorkerCount),
                      total: number(workerCount),
                    })}
              </td>
              <td>{dateTime(latestHealthAt)}</td>
              <td>
                <span className={`phase ${phaseTone(target.spec.observedPhase)}`}>
                  <i /> {phaseLabel(target.spec.observedPhase, t)}
                </span>
                <small className="table-subline">
                  {t("detail.schedulingState")}: {phaseLabel(target.spec.schedulingState, t)}
                </small>
              </td>
              <td>{dateTime(target.spec.lastProbeAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: target.metadata.name })}
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

function WorkerTable({
  workers,
  selectedWorkerId,
  onSelect,
}: Readonly<{
  workers: readonly Worker[];
  selectedWorkerId: string;
  onSelect: (workerId: string) => void;
}>) {
  const { t, number, dateTime } = useI18n();
  if (workers.length === 0) return <div className="table-empty">{t("table.empty.workers")}</div>;
  return (
    <div className="table-scroll">
      <table className="worker-table">
        <thead>
          <tr>
            <th>{t("table.workerId")}</th>
            <th>{t("table.target")}</th>
            <th>{t("table.lease")}</th>
            <th>{t("table.release")}</th>
            <th>{t("table.generation")}</th>
            <th>{t("table.lastHealth")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.resourceLimits")}</th>
            <th>{t("table.started")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {workers.map((worker) => (
            <tr
              key={worker.metadata.uid}
              className={worker.metadata.uid === selectedWorkerId ? "selected" : ""}
              onClick={() => onSelect(worker.metadata.uid)}
            >
              <td>
                <button type="button" onClick={() => onSelect(worker.metadata.uid)}>
                  <strong>{worker.metadata.name}</strong>
                  <small>{worker.metadata.uid}</small>
                </button>
              </td>
              <td>
                <strong>{worker.spec.targetId}</strong>
                <small className="table-subline">
                  {targetKindLabel(worker.spec.targetKind, t)}
                </small>
              </td>
              <td className="mono">{worker.spec.leaseId}</td>
              <td className="mono" title={worker.spec.releaseDigest}>
                {shortDigest(worker.spec.releaseDigest)}
              </td>
              <td className="mono">g{number(worker.spec.generation)}</td>
              <td>
                {worker.spec.lastHealthAt === undefined
                  ? t("common.notObserved")
                  : dateTime(worker.spec.lastHealthAt)}
              </td>
              <td>
                <span className={`phase ${phaseTone(worker.spec.state)}`}>
                  <i />
                  {phaseLabel(worker.spec.state, t)}
                </span>
              </td>
              <td>
                {number(worker.spec.cpuLimitMillis)} mCPU ·{" "}
                {number(Math.round(worker.spec.memoryLimitBytes / 1_048_576))} MiB
              </td>
              <td>{dateTime(worker.metadata.createdAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: worker.metadata.name })}
                  onClick={() => onSelect(worker.metadata.uid)}
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

function ReleaseTable({ releases }: Readonly<{ releases: readonly WorkerRelease[] }>) {
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

function StoragePolicyTable({
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

function ProfileTable({
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

function MaintenanceOperationTable({
  operations,
  emptyMessage = "table.empty.maintenance",
  selectedOperationId,
  onSelect,
}: Readonly<{
  operations: readonly MaintenanceOperation[];
  emptyMessage?: MessageKey;
  selectedOperationId: string;
  onSelect: (operationId: string) => void;
}>) {
  const { t, dateTime } = useI18n();
  if (operations.length === 0) return <div className="table-empty">{t(emptyMessage)}</div>;
  return (
    <div
      className="table-scroll"
      tabIndex={0}
      role="region"
      aria-label={t("page.maintenance.title")}
    >
      <table>
        <thead>
          <tr>
            <th>{t("table.operation")}</th>
            <th>{t("table.resource")}</th>
            <th>{t("table.status")}</th>
            <th>{t("table.currentStep")}</th>
            <th>{t("maintenance.stableErrorCode")}</th>
            <th>{t("table.updated")}</th>
            <th aria-label={t("table.actions")} />
          </tr>
        </thead>
        <tbody>
          {operations.map((operation) => (
            <tr
              key={operation.operationId}
              className={operation.operationId === selectedOperationId ? "selected" : ""}
              onClick={() => onSelect(operation.operationId)}
            >
              <td>
                <button type="button" onClick={() => onSelect(operation.operationId)}>
                  <strong>{auditLabel(operation.action, t)}</strong>
                  <small>{operation.operationId}</small>
                </button>
              </td>
              <td>
                <strong>{operation.resourceId}</strong>
                <small className="table-subline">{t("maintenance.deploymentTarget")}</small>
              </td>
              <td>
                <span className={`phase ${phaseTone(operation.state)}`}>
                  <i /> {phaseLabel(operation.state, t)}
                </span>
              </td>
              <td className="mono">{operation.currentStep}</td>
              <td className="mono">{operation.stableErrorCode ?? "—"}</td>
              <td>{dateTime(operation.updatedAt)}</td>
              <td className="row-action-cell">
                <button
                  className="row-action"
                  type="button"
                  aria-label={t("table.view", { name: operation.operationId })}
                  onClick={() => onSelect(operation.operationId)}
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

function MaintenanceOperationDetail({ operation }: Readonly<{ operation: MaintenanceOperation }>) {
  const { t, number, dateTime } = useI18n();
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          ↻
        </div>
        <div>
          <div className="eyebrow">{auditLabel(operation.action, t)}</div>
          <h2>{operation.resourceId}</h2>
          <span className={`phase ${phaseTone(operation.state)}`}>
            <i /> {phaseLabel(operation.state, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("maintenance.operationId")}</dt>
          <dd className="mono break">{operation.operationId}</dd>
        </div>
        <div>
          <dt>{t("maintenance.resource")}</dt>
          <dd className="mono">
            {operation.resourceKind} · {operation.resourceId}
          </dd>
        </div>
        <div>
          <dt>{t("table.generation")}</dt>
          <dd className="mono">g{number(operation.resourceGeneration)}</dd>
        </div>
        <div>
          <dt>{t("maintenance.currentStep")}</dt>
          <dd className="mono">{operation.currentStep}</dd>
        </div>
        <div>
          <dt>{t("maintenance.requestId")}</dt>
          <dd className="mono break">{operation.requestId}</dd>
        </div>
        <div>
          <dt>{t("maintenance.idempotencyKey")}</dt>
          <dd className="mono break">{operation.idempotencyKey}</dd>
        </div>
        <div>
          <dt>{t("maintenance.requestedBy")}</dt>
          <dd className="mono break">{operation.requestedBy}</dd>
        </div>
        <div>
          <dt>{t("maintenance.requestedAt")}</dt>
          <dd>{dateTime(operation.requestedAt)}</dd>
        </div>
        <div>
          <dt>{t("table.updated")}</dt>
          <dd>{dateTime(operation.updatedAt)}</dd>
        </div>
        <div>
          <dt>{t("maintenance.retryable")}</dt>
          <dd>{t(operation.retryable ? "common.yes" : "common.no")}</dd>
        </div>
        {operation.stableErrorCode ? (
          <div>
            <dt>{t("detail.stableError")}</dt>
            <dd className="danger-text">{operation.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
      <section className="activity-block" aria-labelledby="maintenance-impact-title">
        <div className="activity-heading">
          <h3 id="maintenance-impact-title">{t("maintenance.impact")}</h3>
        </div>
        <p>{operationImpactLabel(operation.action, t)}</p>
        <details className="operation-diagnostic">
          <summary>{t("common.diagnostics")}</summary>
          <p>{operation.impactSummary}</p>
        </details>
      </section>
    </>
  );
}

function TargetDetail({
  target,
  operations,
  audit,
  onProbe,
  onPreviewScheduling,
  onPreviewCleanup,
  disabled,
}: Readonly<{
  target: DeploymentTarget;
  operations: readonly MaintenanceOperation[];
  audit: readonly AdminAuditEvent[];
  onProbe: () => void;
  onPreviewScheduling: () => void;
  onPreviewCleanup: () => void;
  disabled: boolean;
}>) {
  const { t, number, dateTime } = useI18n();
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          {target.spec.targetKind.slice(0, 1).toUpperCase()}
        </div>
        <div>
          <div className="eyebrow">
            {t("detail.targetEyebrow", {
              kind: targetKindLabel(target.spec.targetKind, t),
            })}
          </div>
          <h2>{target.metadata.name}</h2>
          <span className={`phase ${phaseTone(target.spec.observedPhase)}`}>
            <i />
            {phaseLabel(target.spec.observedPhase, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("target.id")}</dt>
          <dd className="mono">{target.metadata.uid}</dd>
        </div>
        <div>
          <dt>{t("target.endpoint")}</dt>
          <dd className="mono break">{target.spec.endpoint}</dd>
        </div>
        <div>
          <dt>{t("target.credentialRef")}</dt>
          <dd className="mono">{target.spec.credentialRef}</dd>
        </div>
        <div>
          <dt>{t("table.generation")}</dt>
          <dd className="mono">{number(target.spec.generation)}</dd>
        </div>
        <div>
          <dt>{t("detail.schedulingState")}</dt>
          <dd>{phaseLabel(target.spec.schedulingState, t)}</dd>
        </div>
        <div>
          <dt>{t("detail.resourceVersion")}</dt>
          <dd className="mono">{target.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>{t("detail.runtimeApi")}</dt>
          <dd>{target.spec.apiVersion || t("common.notObserved")}</dd>
        </div>
        <div>
          <dt>{t("detail.engine")}</dt>
          <dd>{target.spec.engineVersion || t("common.notObserved")}</dd>
        </div>
        <div>
          <dt>{t("detail.platform")}</dt>
          <dd>
            {[target.spec.os, target.spec.architecture].filter(Boolean).join(" / ") ||
              t("common.notObserved")}
          </dd>
        </div>
        <div>
          <dt>{t("table.lastProbe")}</dt>
          <dd>{dateTime(target.spec.lastProbeAt)}</dd>
        </div>
        {target.spec.stableErrorCode !== "" ? (
          <div>
            <dt>{t("detail.stableError")}</dt>
            <dd className="danger-text">{target.spec.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
      <section className="action-block">
        <div>
          <h3>{t("detail.schedulingTitle")}</h3>
          <p>{t("detail.schedulingDescription")}</p>
        </div>
        <button
          className="button ghost"
          type="button"
          onClick={onPreviewScheduling}
          disabled={disabled}
        >
          {t(
            target.spec.schedulingState === "active"
              ? "detail.previewDrain"
              : "detail.previewResume",
          )}
        </button>
      </section>
      <section className="action-block">
        <div>
          <h3>{t("detail.probeTitle")}</h3>
          <p>
            {t("detail.probeDescription", {
              generation: number(target.spec.generation),
            })}
          </p>
        </div>
        <button
          className="button primary"
          type="button"
          onClick={onProbe}
          disabled={disabled || target.spec.observedPhase === "probing"}
        >
          {t("detail.runProbe")}
        </button>
      </section>
      <section className="activity-block" aria-labelledby="target-operations-title">
        <div className="activity-heading">
          <h3 id="target-operations-title">{t("detail.operations")}</h3>
          <span className="scope-chip">operations.list · {number(operations.length)}</span>
        </div>
        {operations.length === 0 ? (
          <p className="activity-empty">{t("detail.noOperations")}</p>
        ) : (
          <ol className="activity-list">
            {operations.map((operation) => (
              <li key={operation.operationId}>
                <div>
                  <strong>{auditLabel(operation.action, t)}</strong>
                  <span className={`phase ${phaseTone(operation.state)}`}>
                    <i /> {phaseLabel(operation.state, t)}
                  </span>
                </div>
                <p>{operationImpactLabel(operation.action, t)}</p>
                <details className="operation-diagnostic">
                  <summary>{t("common.diagnostics")}</summary>
                  <p>{operation.impactSummary}</p>
                  {operation.stableErrorCode ? <code>{operation.stableErrorCode}</code> : null}
                </details>
                <small className="mono">
                  {operation.operationId} · g{number(operation.resourceGeneration)} ·{" "}
                  {operation.currentStep}
                </small>
                <small>{dateTime(operation.updatedAt)}</small>
              </li>
            ))}
          </ol>
        )}
      </section>
      <section className="activity-block" aria-labelledby="target-audit-title">
        <div className="activity-heading">
          <h3 id="target-audit-title">{t("detail.audit")}</h3>
          <span className="scope-chip">audit.list · {number(audit.length)}</span>
        </div>
        {audit.length === 0 ? (
          <p className="activity-empty">{t("detail.noTargetAudit")}</p>
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
      <section className="action-block cleanup-preview-block">
        <div>
          <h3>{t("detail.cleanupImpact")}</h3>
          <p>{t("detail.cleanupImpactDescription")}</p>
        </div>
        <button
          className="button ghost"
          type="button"
          onClick={onPreviewCleanup}
          disabled={disabled}
        >
          {t("detail.previewCleanup")}
        </button>
      </section>
    </>
  );
}

function SchedulingConfirmation({
  target,
  preview,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  target: DeploymentTarget;
  preview: DeploymentTargetSchedulingPreview;
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t, number } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  const draining = preview.spec.desiredState === "drained";
  return (
    <section className="dialog" aria-labelledby="scheduling-title">
      <div className="panel-heading">
        <div>
          <div className="eyebrow">targets.act · {t("common.destructive")}</div>
          <h2 id="scheduling-title">{t("scheduling.confirmTitle")}</h2>
          <p>{target.metadata.name}</p>
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
        <div className={`banner ${draining ? "danger" : "running"}`} role="status">
          {t(draining ? "scheduling.drainSummary" : "scheduling.resumeSummary", {
            leases: number(preview.spec.activeLeases.length),
          })}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("lease.target")}</dt>
            <dd className="mono">{target.metadata.uid}</dd>
          </div>
          <div>
            <dt>{t("table.generation")}</dt>
            <dd className="mono">{number(preview.spec.expectedGeneration)}</dd>
          </div>
          <div>
            <dt>{t("detail.resourceVersion")}</dt>
            <dd className="mono">{preview.spec.expectedResourceVersion}</dd>
          </div>
        </dl>
        <div className="cleanup-preview" aria-label={t("detail.schedulingTitle")}>
          {preview.spec.activeLeases.length === 0 ? (
            <p>{t("scheduling.none")}</p>
          ) : (
            preview.spec.activeLeases.map((lease) => (
              <article className="cleanup-worker" key={lease.leaseId}>
                <div>
                  <strong className="mono">{lease.leaseName}</strong>
                  <span className={`phase ${phaseTone(lease.observedPhase)}`}>
                    <i /> {phaseLabel(lease.observedPhase, t)}
                  </span>
                </div>
                <small className="mono">
                  {lease.leaseId} · g{number(lease.generation)}
                </small>
              </article>
            ))
          )}
        </div>
        <label className="confirmation-check">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
            disabled={disabled}
            data-sheet-autofocus
          />
          <span>{t("scheduling.review")}</span>
        </label>
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button
            className={`button ${draining ? "danger" : "primary"}`}
            type="submit"
            disabled={disabled || !confirmed}
          >
            {t(draining ? "scheduling.confirmDrain" : "scheduling.confirmResume")}
          </button>
        </div>
      </form>
    </section>
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
  const { t, number } = useI18n();
  const [confirmed, setConfirmed] = useState(false);
  const resourceCount = preview.spec.workers.reduce(
    (count, worker) => count + worker.resources.length,
    0,
  );
  return (
    <section className="dialog" aria-labelledby="cleanup-title">
      <div className="panel-heading">
        <div>
          <div className="eyebrow">targets.act · {t("common.destructive")}</div>
          <h2 id="cleanup-title">{t("cleanup.confirmTitle")}</h2>
          <p>{target.metadata.name}</p>
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
        <div className={`banner ${preview.spec.canCleanup ? "running" : "danger"}`} role="status">
          {preview.spec.canCleanup
            ? t("cleanup.summary", {
                workers: number(preview.spec.workers.length),
                resources: number(resourceCount),
              })
            : t("cleanup.blocked")}
        </div>
        <dl className="detail-list cleanup-fence">
          <div>
            <dt>{t("lease.target")}</dt>
            <dd className="mono">{target.metadata.uid}</dd>
          </div>
          <div>
            <dt>{t("table.generation")}</dt>
            <dd className="mono">{number(preview.spec.expectedGeneration)}</dd>
          </div>
          <div>
            <dt>{t("detail.resourceVersion")}</dt>
            <dd className="mono">{preview.spec.expectedResourceVersion}</dd>
          </div>
        </dl>
        <div className="cleanup-preview" aria-label={t("detail.cleanupImpact")}>
          {preview.spec.workers.length === 0 ? (
            <p>{t("cleanup.none")}</p>
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
                    <i /> {phaseLabel(worker.disposition, t)}
                  </span>
                </div>
                <small className="mono">
                  {worker.leaseId} · g{number(worker.leaseGeneration)}
                </small>
                <ul>
                  {worker.resources.map((resource) => (
                    <li key={`${resource.resourceKind}:${resource.resourceName}`}>
                      <span>{resourceLabel(resource.resourceKind, t)}</span>
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
            <span>{t("cleanup.review")}</span>
          </label>
        ) : null}
        <div className="dialog-actions">
          <button className="button ghost" type="button" onClick={onClose}>
            {t("action.cancel")}
          </button>
          <button
            className="button danger"
            type="submit"
            disabled={disabled || !preview.spec.canCleanup || !confirmed}
          >
            {t("cleanup.confirm")}
          </button>
        </div>
      </form>
    </section>
  );
}

function WorkerDetail({ worker }: Readonly<{ worker: Worker }>) {
  const { t, number, dateTime } = useI18n();
  return (
    <>
      <div className="detail-heading">
        <div className="target-glyph" aria-hidden="true">
          W
        </div>
        <div>
          <div className="eyebrow">{t("worker.eyebrow")}</div>
          <h2>{worker.metadata.name}</h2>
          <span className={`phase ${phaseTone(worker.spec.state)}`}>
            <i />
            {phaseLabel(worker.spec.state, t)}
          </span>
        </div>
      </div>
      <dl className="detail-list">
        <div>
          <dt>{t("worker.id")}</dt>
          <dd className="mono">{worker.metadata.uid}</dd>
        </div>
        <div>
          <dt>{t("worker.lease")}</dt>
          <dd className="mono">{worker.spec.leaseId}</dd>
        </div>
        <div>
          <dt>{t("worker.target")}</dt>
          <dd className="mono">
            {worker.spec.targetId} · {targetKindLabel(worker.spec.targetKind, t)} · g
            {number(worker.spec.targetGeneration)}
          </dd>
        </div>
        <div>
          <dt>{t("table.generation")}</dt>
          <dd className="mono">{number(worker.spec.generation)}</dd>
        </div>
        <div>
          <dt>{t("detail.resourceVersion")}</dt>
          <dd className="mono">{worker.metadata.resourceVersion}</dd>
        </div>
        <div>
          <dt>{t("worker.releaseDigest")}</dt>
          <dd className="mono break">{worker.spec.releaseDigest}</dd>
        </div>
        <div>
          <dt>{t("worker.resourceLimits")}</dt>
          <dd>
            {number(worker.spec.cpuLimitMillis)} mCPU /{" "}
            {number(Math.round(worker.spec.memoryLimitBytes / 1_048_576))} MiB
          </dd>
        </div>
        <div>
          <dt>{t("worker.cleanupPhase")}</dt>
          <dd>{phaseLabel(worker.spec.cleanupPhase, t)}</dd>
        </div>
        <div>
          <dt>{t("worker.lastHealthAt")}</dt>
          <dd>
            {worker.spec.lastHealthAt === undefined
              ? t("common.notObserved")
              : dateTime(worker.spec.lastHealthAt)}
          </dd>
        </div>
        <div>
          <dt>{t("worker.readyAt")}</dt>
          <dd>
            {worker.spec.readyAt === undefined
              ? t("common.notReady")
              : dateTime(worker.spec.readyAt)}
          </dd>
        </div>
        <div>
          <dt>{t("worker.startedAt")}</dt>
          <dd>{dateTime(worker.metadata.createdAt)}</dd>
        </div>
        <div>
          <dt>{t("worker.identity")}</dt>
          <dd className="mono break">{worker.spec.workerSpiffeId ?? t("common.notReady")}</dd>
        </div>
        <div>
          <dt>{t("worker.serverName")}</dt>
          <dd className="mono">{worker.spec.workerServerName ?? t("common.notReady")}</dd>
        </div>
        <div>
          <dt>{t("worker.updatedAt")}</dt>
          <dd>{dateTime(worker.metadata.updatedAt)}</dd>
        </div>
        {worker.spec.stableErrorCode !== "" ? (
          <div>
            <dt>{t("detail.stableError")}</dt>
            <dd className="danger-text">{worker.spec.stableErrorCode}</dd>
          </div>
        ) : null}
      </dl>
      <p className="boundary-note">{t("worker.healthBoundary")}</p>
    </>
  );
}

function LeaseDetail({
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
          <dt>{t("lease.id")}</dt>
          <dd className="mono">{lease.metadata.uid}</dd>
        </div>
        <div>
          <dt>{t("lease.environmentId")}</dt>
          <dd className="mono">{lease.spec.environmentId}</dd>
        </div>
        <div>
          <dt>{t("lease.target")}</dt>
          <dd className="mono">{lease.spec.targetId ?? t("common.legacyLease")}</dd>
        </div>
        <div>
          <dt>{t("lease.desiredPhase")}</dt>
          <dd>{phaseLabel(lease.spec.desiredPhase, t)}</dd>
        </div>
        <div>
          <dt>{t("lease.cleanupPhase")}</dt>
          <dd className={lease.spec.cleanupPhase === "blocked" ? "danger-text" : ""}>
            {phaseLabel(lease.spec.cleanupPhase, t)}
          </dd>
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
          <dt>{t("lease.workerEndpoint")}</dt>
          <dd className="mono break">{lease.spec.workerEndpoint ?? t("common.notReady")}</dd>
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
      <p className="boundary-note">{t("lease.releaseBoundary")}</p>
    </>
  );
}

function LeaseReleaseConfirmation({
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
      <div className="panel-heading">
        <div>
          <div className="eyebrow">leases.act · {t("common.destructive")}</div>
          <h2 id="lease-release-title">
            {t("lease.releaseConfirmTitle", { action: actionLabel })}
          </h2>
          <p>{lease.metadata.name}</p>
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
            <dt>{t("table.generation")}</dt>
            <dd className="mono">{number(preview.spec.expectedGeneration)}</dd>
          </div>
          <div>
            <dt>{t("detail.resourceVersion")}</dt>
            <dd className="mono">{preview.spec.expectedResourceVersion}</dd>
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

function ProfileTransitionConfirmation({
  profile,
  action,
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  profile: EnvironmentProfile;
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
      <div className="panel-heading">
        <div>
          <div className="eyebrow">{t("profile.transition.eyebrow")}</div>
          <h2 id="profile-transition-title">
            {t("profile.transition.title", { action: actionLabel })}
          </h2>
          <p>
            {profile.metadata.name} · v{number(profile.spec.version)}
          </p>
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

function ProfileDetail({
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
          <span className="scope-chip">audit.list · {number(audit.length)}</span>
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
