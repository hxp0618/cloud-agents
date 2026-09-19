import { ConnectionView } from "./app/connection";
import {
  PaginatedTargets,
  TargetTable,
  TargetDetail,
  TargetRegistrationForm,
  deploymentTargetRegisterRequestFrom,
  targetRegistrationForm,
  SchedulingConfirmation,
  CleanupConfirmation,
} from "./app/targets";
import { ClusterHostTable, WorkerTable, WorkerHealthCheck, WorkerDetail } from "./app/workers";
import { LeaseTable, LeaseDetail, LeaseReleaseConfirmation } from "./app/leases";
import {
  ProfileTable,
  RuntimeProfileTable,
  RuntimeProfileDetail,
  ProfileTransitionConfirmation,
  ProfileDetail,
  type ProfileTransition,
} from "./app/profiles";
import {
  EnvironmentProfileCreateForm,
  environmentProfileCreateRequestFrom,
  environmentProfileForm,
  RuntimeProfileCreateForm,
  runtimeProfileCreateRequestFrom,
  runtimeProfileForm,
} from "./app/profile-forms";
import {
  SandboxTable,
  SandboxDetail,
  SandboxLifecycleConfirmation,
  SandboxGrantRevokeConfirmation,
} from "./app/sandboxes";
import { MaintenanceOperationTable, MaintenanceOperationDetail } from "./app/operations";
import { WorkspaceSnapshotTable, WorkspaceSnapshotCleanupConfirmation } from "./app/workspaces";
import {
  ReleaseRegistrationForm,
  StoragePolicyTable,
  ReleaseTable,
  workerReleaseForm,
  workerReleaseRegisterRequestFrom,
} from "./app/policies";
import { useEffect, useLayoutEffect, useRef, useState, type FormEvent } from "react";
import {
  createHTTPClient,
  type AdminSandboxAccessGrant,
  type AdminSandboxSession,
  type AdminAuditEvent,
  type DeploymentTarget,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetSchedulingPreview,
  type EnvironmentLease,
  type EnvironmentLeaseUpgradePreview,
  type EnvironmentProfile,
  type MaintenanceOperation,
  type ManagedAgentEvent,
  type ManagedAgentExecution,
  type ManagedAgentSession,
  type McpServer,
  type ManagedAgentSideEffectReconciliationRequest,
  type NetworkPolicy,
  type ProjectLeaseQuota,
  type ProjectLeaseQuotaSetRequest,
  type RuntimeProfile,
  type SkillBundle,
  type SandboxAccessGrantRevokeRequest,
  type SandboxSessionLifecycleRequest,
  type SandboxUsageCorrectionRequest,
  type StoragePolicy,
  type StoragePolicySetRequest,
  type Worker,
  type WorkerRelease,
  type WorkspaceSnapshot,
  type WorkspaceSnapshotCleanupRequest,
  type WorkspaceSnapshotCreateRequest,
  type WorkspaceSnapshotRestoreRequest,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { ResourceRefresh } from "./ResourceRefresh";
import { SuccessToast } from "./SuccessToast";
import { AdminSheet } from "./AdminSheet";

import {
  adminFailure,
  targetIdentifierPattern,
  filterAdminTargets,
  filterAdminMaintenanceOperations,
  filterAdminLeases,
  filterAdminWorkers,
  leaseNeedsAttention,
  cleanupRequestFromPreview,
  leaseReleaseRequestFromPreview,
  listAdminMaintenanceOperations,
  loadAdminManagedAgentRuntime,
  listAdminProjectLeaseQuotaAuditEvents,
  listAdminStoragePolicyAuditEvents,
  capabilityBindingRelations,
  listAdminSandboxAccessGrants,
  listAdminSandboxes,
  listAdminWorkspaceSnapshots,
  listAdminWorkers,
  newIdempotencyKey,
  newRequestId,
  readSavedAdminConnection,
  replaceLease,
  replaceProfile,
  replaceRuntimeProfile,
  replaceRelease,
  replaceStoragePolicy,
  replaceTarget,
  schedulingRequestFromPreview,
  summarizeClusterHosts,
  selectAdminResourceId,
  writeSavedAdminConnection,
  type AdminClient,
  type SavedAdminConnection,
  type SandboxLifecycleAction,
  type WorkerStatusFilter,
} from "./admin";
import { NetworkPolicyPanel } from "./NetworkPolicyPanel";
import { CapabilityPanel } from "./CapabilityPanel";
import { DeniedWritePanel } from "./DeniedWritePanel";
import { RemoteWorkerEnrollmentPanel } from "./RemoteWorkerEnrollmentPanel";
import { TargetFilters } from "./TargetFilters";
import { AdminSidebar } from "./AdminSidebar";
import {
  NavigationCommands,
  NavigationIcon,
  navigationPages,
  ResourceNavigation,
  type Page,
} from "./navigation";
import { normalizeLocale, useI18n, type MessageKey, type MessageValues } from "./i18n";
import {
  loadAdminWorkspaceData,
  type AdminWorkspaceData,
  loadProfileAudit,
  loadTargetActivity,
  loadTargetAuthority,
  loadLeaseAuthority,
} from "./app/loaders";
import {
  executableFoundationNetworkPolicy,
  phaseTone,
  phaseLabel,
  auditLabel,
  runtimeProfileTargetLabel,
  type ConnectionStatus,
  type TargetKind,
} from "./app/presentation";

type LeaseReleaseTransition = "upgrade" | "rollback";
type LocalizedMessage = Readonly<{ key: MessageKey; values?: MessageValues }>;
type OperationNotice = LocalizedMessage & Readonly<{ accepted?: boolean }>;
type BusyOperation = Readonly<{ message: LocalizedMessage }>;
type Theme = "light" | "dark";

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
  const [runtimeProfiles, setRuntimeProfiles] = useState<readonly RuntimeProfile[]>(
    Object.freeze([]),
  );
  const [selectedRuntimeProfileVersionId, setSelectedRuntimeProfileVersionId] = useState("");
  const [sandboxes, setSandboxes] = useState<readonly AdminSandboxSession[]>(Object.freeze([]));
  const [managedAgentSessions, setManagedAgentSessions] = useState<readonly ManagedAgentSession[]>(
    Object.freeze([]),
  );
  const [managedAgentExecutions, setManagedAgentExecutions] = useState<
    readonly ManagedAgentExecution[]
  >(Object.freeze([]));
  const [managedAgentEvents, setManagedAgentEvents] = useState<readonly ManagedAgentEvent[]>(
    Object.freeze([]),
  );
  const [workspaceSnapshots, setWorkspaceSnapshots] = useState<readonly WorkspaceSnapshot[]>(
    Object.freeze([]),
  );
  const [snapshotCleanup, setSnapshotCleanup] = useState<WorkspaceSnapshot | null>(null);
  const [selectedSandboxId, setSelectedSandboxId] = useState("");
  const [sandboxAccessGrants, setSandboxAccessGrants] = useState<
    readonly AdminSandboxAccessGrant[]
  >(Object.freeze([]));
  const [sandboxGrantRevoke, setSandboxGrantRevoke] = useState<AdminSandboxAccessGrant | null>(
    null,
  );
  const [storagePolicies, setStoragePolicies] = useState<readonly StoragePolicy[]>(
    Object.freeze([]),
  );
  const [networkPolicies, setNetworkPolicies] = useState<readonly NetworkPolicy[]>([]);
  const [mcpServers, setMcpServers] = useState<readonly McpServer[]>(Object.freeze([]));
  const [skillBundles, setSkillBundles] = useState<readonly SkillBundle[]>(Object.freeze([]));
  const [capabilitySessions, setCapabilitySessions] = useState<readonly ManagedAgentSession[]>(
    Object.freeze([]),
  );
  const [capabilityExecutions, setCapabilityExecutions] = useState<
    readonly ManagedAgentExecution[]
  >(Object.freeze([]));
  const [capabilityEvents, setCapabilityEvents] = useState<readonly ManagedAgentEvent[]>(
    Object.freeze([]),
  );
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
  const [workerStatusFilter, setWorkerStatusFilter] = useState<WorkerStatusFilter>("");
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
  const [runtimeProfileDetailOpen, setRuntimeProfileDetailOpen] = useState(false);
  const [sandboxDetailOpen, setSandboxDetailOpen] = useState(false);
  const [sandboxLifecycleTransition, setSandboxLifecycleTransition] =
    useState<SandboxLifecycleAction | null>(null);
  const [maintenanceDetailOpen, setMaintenanceDetailOpen] = useState(false);
  const [profileTransition, setProfileTransition] = useState<ProfileTransition | null>(null);
  const [runtimeProfileTransition, setRuntimeProfileTransition] =
    useState<ProfileTransition | null>(null);
  const [creatingProfile, setCreatingProfile] = useState(false);
  const [creatingRuntimeProfile, setCreatingRuntimeProfile] = useState(false);
  const [busy, setBusy] = useState<BusyOperation | null>(null);
  const [error, setError] = useState<ReturnType<typeof adminFailure> | null>(null);
  const [notice, setNotice] = useState<OperationNotice | null>(null);
  const [targetForm, setTargetForm] = useState(targetRegistrationForm);
  const [profileForm, setProfileForm] = useState(environmentProfileForm);
  const [runtimeProfileDraft, setRuntimeProfileDraft] = useState(runtimeProfileForm);
  const [releaseForm, setReleaseForm] = useState(workerReleaseForm);
  const [quotaForm, setQuotaForm] = useState(quotaFormFrom);
  const [storagePolicyForm, setStoragePolicyForm] = useState(storagePolicyFormFrom);
  const [snapshotForm, setSnapshotForm] = useState({
    snapshotId: "",
    sourceSandboxId: "",
    retentionSeconds: "604800",
  });
  const [restoreForm, setRestoreForm] = useState({
    snapshotId: "",
    workspaceId: "",
    workspaceName: "",
    sandboxId: "",
    runtimeProfileVersionId: "",
    ttlSeconds: "3600",
  });
  const requestRef = useRef<AbortController | null>(null);
  const busyRef = useRef(false);
  const operationTriggerRef = useRef<HTMLElement | null>(null);
  const cancelledFeedbackRef = useRef<HTMLElement | null>(null);
  const pendingKeysRef = useRef(new Map<string, string>());
  const profileMenuRef = useRef<HTMLDetailsElement>(null);

  const connected = status === "connected" && client !== null;
  useLayoutEffect(() => {
    if (busy !== null) return;
    const feedback = cancelledFeedbackRef.current;
    cancelledFeedbackRef.current = null;
    // Focus the cancellation result in the initiating surface, not an inert background Sheet.
    if (
      feedback?.isConnected &&
      (document.activeElement === document.body || feedback.contains(document.activeElement))
    ) {
      feedback.querySelector<HTMLElement>('[role="alert"]')?.focus();
    }
  }, [busy]);
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
  const selectedRuntimeProfile = runtimeProfiles.find(
    ({ metadata }) => metadata.uid === selectedRuntimeProfileVersionId,
  );
  const selectedSandbox = sandboxes.find(({ metadata }) => metadata.uid === selectedSandboxId);
  const selectedStoragePolicy = storagePolicies.find(
    ({ metadata }) => metadata.uid === selectedStoragePolicyId,
  );
  const selectedRestoreSnapshot = workspaceSnapshots.find(
    ({ metadata }) => metadata.uid === restoreForm.snapshotId,
  );
  const restoreSourceTargetId = selectedRestoreSnapshot?.spec.sourceTargetId;
  const restoreRuntimeProfiles = runtimeProfiles.filter(
    ({ spec }) =>
      spec.status === "published" &&
      "targetId" in spec &&
      (selectedRestoreSnapshot?.spec.backend === "portable-tar-v1" ||
        spec.targetId === restoreSourceTargetId),
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
  const onlineWorkerCount = filterAdminWorkers(workers, "", "online").length;
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
  const visibleWorkers = filterAdminWorkers(workers, query, workerStatusFilter);
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
  const visibleRuntimeProfiles =
    normalizedQuery === ""
      ? runtimeProfiles
      : runtimeProfiles.filter((profile) => {
          const { metadata, spec } = profile;
          return [
            metadata.uid,
            metadata.name,
            spec.profileId,
            String(spec.version),
            spec.status,
            runtimeProfileTargetLabel(profile),
            spec.imageUri,
          ].some((value) => value.toLocaleLowerCase().includes(normalizedQuery));
        });
  const visibleSandboxes =
    normalizedQuery === ""
      ? sandboxes
      : sandboxes.filter(({ metadata, spec }) =>
          [
            metadata.uid,
            spec.workspaceId,
            spec.workspaceName,
            spec.volumeId,
            spec.physicalVolumeId ?? "",
            spec.runtimeProfileId,
            spec.targetId,
            spec.operationId,
            spec.observedState,
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
  const pageEntry = navigationPages.find(({ id }) => id === page)!;

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

  useEffect(() => {
    if (page !== "capabilities" || client === null) return;
    const controller = new AbortController();
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    void loadAdminManagedAgentRuntime(
      client,
      connection.tenantId,
      connection.projectId,
      "",
      signal,
      true,
    )
      .then((runtime) => {
        setCapabilitySessions(runtime.sessions);
        setCapabilityExecutions(runtime.executions);
        setCapabilityEvents(runtime.events);
      })
      .catch((cause) => {
        if (!controller.signal.aborted) setError(adminFailure(cause));
      });
    return () => controller.abort();
  }, [client, connection.projectId, connection.tenantId, page]);

  useEffect(
    () => () => {
      requestRef.current?.abort();
    },
    [],
  );

  useEffect(() => {
    const sandboxLifecyclePending = sandboxes.some(({ spec }) =>
      ["pending", "unknown"].includes(spec.observedState),
    );
    const snapshotPending = workspaceSnapshots.some(({ spec }) =>
      ["pending", "unknown", "deleting"].includes(spec.status),
    );
    const lifecyclePending =
      targets.some(({ spec }) => spec.observedPhase === "probing") ||
      leases.some(
        ({ spec }) =>
          spec.observedPhase === "provisioning" ||
          spec.observedPhase === "terminating" ||
          ["pending", "revoking", "reaping"].includes(spec.cleanupPhase),
      ) ||
      sandboxLifecyclePending ||
      snapshotPending;
    const observeHealth =
      (page === "workers" || page === "overview") &&
      workers.some(({ spec }) => spec.state === "ready");
    if (!connected || client === null || (!lifecyclePending && !observeHealth)) return;
    const controller = new AbortController();
    let polling = false;
    const interval = window.setInterval(
      () => {
        if (document.visibilityState !== "visible" || busyRef.current || polling) return;
        polling = true;
        const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
        void Promise.all([
          lifecyclePending
            ? loadTargetAuthority(client, connection, selectedTargetId, signal)
            : Promise.resolve({ targets, selectedTargetId }),
          lifecyclePending
            ? loadLeaseAuthority(client, connection, selectedLeaseId, signal)
            : Promise.resolve({ leases, selectedLeaseId }),
          listAdminWorkers(client, connection.tenantId, connection.projectId, signal),
          sandboxLifecyclePending
            ? listAdminSandboxes(client, connection.tenantId, connection.projectId, signal)
            : Promise.resolve(sandboxes),
          snapshotPending
            ? listAdminWorkspaceSnapshots(client, connection.tenantId, connection.projectId, signal)
            : Promise.resolve(workspaceSnapshots),
        ])
          .then(
            ([
              loadedTargets,
              loadedLeases,
              loadedWorkers,
              loadedSandboxes,
              loadedWorkspaceSnapshots,
            ]) => {
              if (controller.signal.aborted) return;
              setTargets(loadedTargets.targets);
              setSelectedTargetId(loadedTargets.selectedTargetId);
              setLeases(loadedLeases.leases);
              setSelectedLeaseId(loadedLeases.selectedLeaseId);
              setWorkers(loadedWorkers);
              setSandboxes(loadedSandboxes);
              setWorkspaceSnapshots(loadedWorkspaceSnapshots);
              setSelectedWorkerId((current) => selectAdminResourceId(loadedWorkers, current));
            },
          )
          .catch((cause: unknown) => {
            if (!controller.signal.aborted) setError(adminFailure(cause));
          })
          .finally(() => {
            polling = false;
          });
      },
      lifecyclePending ? 5_000 : 15_000,
    );
    return () => {
      controller.abort();
      window.clearInterval(interval);
    };
  }, [
    client,
    connected,
    connection,
    leases,
    selectedLeaseId,
    selectedTargetId,
    targets,
    workers,
    sandboxes,
    workspaceSnapshots,
    page,
  ]);

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
    setWorkerStatusFilter("");
    setMobileNavOpen(false);
    setTargetDetailOpen(false);
    setCleanupConfirmationOpen(false);
    setSchedulingConfirmationOpen(false);
    setLeaseDetailOpen(false);
    setLeaseReleaseConfirmationOpen(false);
    setWorkerDetailOpen(false);
    setProfileDetailOpen(false);
    setRuntimeProfileDetailOpen(false);
    setSandboxDetailOpen(false);
    setSandboxLifecycleTransition(null);
    setSandboxGrantRevoke(null);
    setSnapshotCleanup(null);
    setMaintenanceDetailOpen(false);
    setProfileTransition(null);
    setRuntimeProfileTransition(null);
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
    setRuntimeProfiles(Object.freeze([]));
    setSelectedRuntimeProfileVersionId("");
    setSandboxes(Object.freeze([]));
    setWorkspaceSnapshots(Object.freeze([]));
    setSnapshotCleanup(null);
    setSelectedSandboxId("");
    setSandboxAccessGrants(Object.freeze([]));
    setSandboxGrantRevoke(null);
    setManagedAgentSessions(Object.freeze([]));
    setManagedAgentExecutions(Object.freeze([]));
    setManagedAgentEvents(Object.freeze([]));
    setStoragePolicies(Object.freeze([]));
    setNetworkPolicies([]);
    setMcpServers(Object.freeze([]));
    setSkillBundles(Object.freeze([]));
    setCapabilitySessions(Object.freeze([]));
    setCapabilityExecutions(Object.freeze([]));
    setNetworkEditorEpoch((current) => current + 1);
    setSelectedStoragePolicyId("");
    setStoragePolicyAudit(Object.freeze([]));
    setStoragePolicyForm(storagePolicyFormFrom());
    setSnapshotForm({
      snapshotId: "",
      sourceSandboxId: "",
      retentionSeconds: "604800",
    });
    setRestoreForm({
      snapshotId: "",
      workspaceId: "",
      workspaceName: "",
      sandboxId: "",
      runtimeProfileVersionId: "",
      ttlSeconds: "3600",
    });
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
    setRuntimeProfileDetailOpen(false);
    setSandboxDetailOpen(false);
    setSandboxLifecycleTransition(null);
    setMaintenanceDetailOpen(false);
    setProfileTransition(null);
    setRuntimeProfileTransition(null);
    setCreatingProfile(false);
    setCreatingRuntimeProfile(false);
    setRuntimeProfileDraft(runtimeProfileForm());
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
      const loaded = await loadAdminWorkspaceData(
        nextClient,
        nextConnection,
        selectedTargetId,
        selectedLeaseId,
        selectedProfileVersionId,
        signal,
      );
      setClient(nextClient);
      setConnection(nextConnection);
      applyWorkspaceData(loaded, "first");
      setTargetOperations(Object.freeze([]));
      setTargetAudit(Object.freeze([]));
      setProfileAudit(Object.freeze([]));
      setStoragePolicyAudit(Object.freeze([]));
      setStoragePolicyForm(storagePolicyFormFrom(loaded.storagePolicies[0]));
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
    accepted = false,
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
      setNotice({ ...message, accepted });
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

  function clearTargetFilters() {
    setQuery("");
    setTargetKindFilter([]);
    setTargetPhaseFilter([]);
  }

  function applyWorkspaceData(loaded: AdminWorkspaceData, selection: "first" | "preserve") {
    setTargets(loaded.targets.targets);
    setSelectedTargetId(loaded.targets.selectedTargetId);
    setLeases(loaded.leases.leases);
    setSelectedLeaseId(loaded.leases.selectedLeaseId);
    setWorkers(loaded.workers);
    setSelectedWorkerId((current) =>
      selectAdminResourceId(loaded.workers, selection === "preserve" ? current : ""),
    );
    setReleases(loaded.releases);
    setProfiles(loaded.profiles.profiles);
    setSelectedProfileVersionId(loaded.profiles.selectedProfileVersionId);
    setRuntimeProfiles(loaded.runtimeProfiles);
    setSelectedRuntimeProfileVersionId((current) =>
      selectAdminResourceId(loaded.runtimeProfiles, selection === "preserve" ? current : ""),
    );
    setSandboxes(loaded.sandboxes);
    setSelectedSandboxId((current) =>
      selectAdminResourceId(loaded.sandboxes, selection === "preserve" ? current : ""),
    );
    setWorkspaceSnapshots(loaded.workspaceSnapshots);
    setStoragePolicies(loaded.storagePolicies);
    setNetworkPolicies(loaded.networkPolicies);
    setMcpServers(loaded.mcpServers);
    setSkillBundles(loaded.skillBundles);
    setSelectedStoragePolicyId((current) =>
      selectAdminResourceId(loaded.storagePolicies, selection === "preserve" ? current : ""),
    );
    setLeaseQuota(loaded.quota);
    setLeaseQuotaAudit(loaded.quotaAudit);
    setQuotaForm(quotaFormFrom(loaded.quota));
    setMaintenanceOperations(loaded.maintenanceOperations);
    setSelectedMaintenanceOperationId((current) =>
      selection === "preserve" &&
      loaded.maintenanceOperations.some(({ operationId }) => operationId === current)
        ? current
        : (loaded.maintenanceOperations[0]?.operationId ?? ""),
    );
  }

  function refresh() {
    if (client === null) return;
    void runOperation("refresh", { key: "operation.refresh" }, async (signal) => {
      const loaded = await loadAdminWorkspaceData(
        client,
        connection,
        selectedTargetId,
        selectedLeaseId,
        selectedProfileVersionId,
        signal,
      );
      applyWorkspaceData(loaded, "preserve");
      if (targetDetailOpen && loaded.targets.selectedTargetId !== "") {
        const activity = await loadTargetActivity(
          client,
          connection,
          loaded.targets.selectedTargetId,
          signal,
        );
        setTargetOperations(activity.operations);
        setTargetAudit(activity.audit);
      }
      const profile = loaded.profiles.profiles.find(
        ({ metadata }) => metadata.uid === loaded.profiles.selectedProfileVersionId,
      );
      if (profileDetailOpen && profile !== undefined) {
        setProfileAudit(await loadProfileAudit(client, connection, profile, signal));
      }
      const storagePolicy =
        loaded.storagePolicies.find(({ metadata }) => metadata.uid === selectedStoragePolicyId) ??
        loaded.storagePolicies[0];
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
        setSelectedWorkerId((current) => selectAdminResourceId(loadedWorkers, current));
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

  function selectRuntimeProfile(profileVersionId: string) {
    setRuntimeProfileDetailOpen(true);
    setRuntimeProfileTransition(null);
    const profile = runtimeProfiles.find(({ metadata }) => metadata.uid === profileVersionId);
    if (client === null || profile === undefined) return;
    setSelectedRuntimeProfileVersionId(profileVersionId);
    void runOperation(
      `get-runtime-profile:${profileVersionId}`,
      { key: "operation.runtimeProfileDetail" },
      async (signal) => {
        const result = await client.getAdminRuntimeProfile(
          connection.tenantId,
          connection.projectId,
          profile.spec.profileId,
          profile.spec.version,
          newRequestId(),
          signal,
        );
        setRuntimeProfiles((current) => replaceRuntimeProfile(current, result.value));
      },
    );
  }

  function selectSandbox(sandboxId: string) {
    setSandboxDetailOpen(true);
    setSandboxLifecycleTransition(null);
    setSandboxGrantRevoke(null);
    setSandboxAccessGrants(Object.freeze([]));
    setManagedAgentSessions(Object.freeze([]));
    setManagedAgentExecutions(Object.freeze([]));
    setManagedAgentEvents(Object.freeze([]));
    setSelectedSandboxId(sandboxId);
    if (client === null) return;
    void runOperation(
      `get-sandbox:${sandboxId}`,
      { key: "operation.sandboxDetail" },
      async (signal) => {
        const [result, grants, agentRuntime] = await Promise.all([
          client.getAdminSandboxSession(
            connection.tenantId,
            connection.projectId,
            sandboxId,
            newRequestId(),
            signal,
          ),
          listAdminSandboxAccessGrants(
            client,
            connection.tenantId,
            connection.projectId,
            sandboxId,
            signal,
          ),
          loadAdminManagedAgentRuntime(
            client,
            connection.tenantId,
            connection.projectId,
            sandboxId,
            signal,
          ),
        ]);
        setSandboxes((current) =>
          Object.freeze(
            current.map((sandbox) => (sandbox.metadata.uid === sandboxId ? result.value : sandbox)),
          ),
        );
        setSandboxAccessGrants(grants);
        setManagedAgentSessions(agentRuntime.sessions);
        setManagedAgentExecutions(agentRuntime.executions);
        setManagedAgentEvents(agentRuntime.events);
      },
    );
  }

  function reconcileManagedAgentSideEffect(
    execution: ManagedAgentExecution,
    outcome: ManagedAgentSideEffectReconciliationRequest["outcome"],
  ) {
    if (client === null || execution.spec.checkpoint === undefined) return;
    const checkpoint = execution.spec.checkpoint;
    const key = `managed-agent:reconcile:${execution.metadata.uid}:${execution.metadata.resourceVersion}:${checkpoint.digest}:${outcome}`;
    void runOperation(
      key,
      { key: "operation.reconcileAgentExecution", values: { name: execution.metadata.uid } },
      async (signal) => {
        await client.reconcileAdminManagedAgentSideEffect(
          connection.tenantId,
          connection.projectId,
          execution.metadata.sessionId,
          execution.metadata.turnId,
          execution.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          { generation: execution.spec.generation, checkpointDigest: checkpoint.digest, outcome },
          signal,
        );
        if (selectedSandbox === undefined) return;
        const runtime = await loadAdminManagedAgentRuntime(
          client,
          connection.tenantId,
          connection.projectId,
          selectedSandbox.metadata.uid,
          signal,
        );
        setManagedAgentSessions(runtime.sessions);
        setManagedAgentExecutions(runtime.executions);
        setManagedAgentEvents(runtime.events);
      },
      true,
    );
  }

  function revokeSandboxAccessGrant() {
    if (client === null || sandboxGrantRevoke === null) return;
    const grant = sandboxGrantRevoke;
    const body: SandboxAccessGrantRevokeRequest = {
      expectedGeneration: grant.spec.generation,
      expectedResourceVersion: grant.metadata.resourceVersion,
      confirmedGrantId: grant.metadata.uid,
    };
    const operationKey = `sandbox-grant:revoke:${grant.metadata.uid}:${grant.metadata.resourceVersion}`;
    setSandboxGrantRevoke(null);
    void runOperation(
      operationKey,
      {
        key: "operation.revokeSandboxGrant",
        values: { name: grant.metadata.uid },
      },
      async (signal) => {
        const result = await client.revokeAdminSandboxAccessGrant(
          connection.tenantId,
          connection.projectId,
          grant.spec.sandboxId,
          grant.metadata.uid,
          newRequestId(),
          idempotencyKey(operationKey),
          body,
          signal,
        );
        setSandboxAccessGrants((current) =>
          Object.freeze(
            current.map((item) =>
              item.metadata.uid === result.value.metadata.uid ? result.value : item,
            ),
          ),
        );
      },
    );
  }

  function transitionSandbox() {
    if (client === null || selectedSandbox === undefined || sandboxLifecycleTransition === null)
      return;
    const sandbox = selectedSandbox;
    const action = sandboxLifecycleTransition;
    const body: SandboxSessionLifecycleRequest = {
      expectedGeneration: sandbox.spec.generation,
      expectedResourceVersion: sandbox.metadata.resourceVersion,
      confirmedSandboxId: sandbox.metadata.uid,
      computeDisposition: action === "stop" ? "delete" : "create",
      workspaceDisposition: "retain",
    };
    const key = `sandbox:${action}:${sandbox.metadata.uid}:${sandbox.metadata.resourceVersion}`;
    setSandboxLifecycleTransition(null);
    void runOperation(
      key,
      {
        key: action === "stop" ? "operation.stopSandbox" : "operation.rebuildSandbox",
        values: { name: sandbox.metadata.name },
      },
      async (signal) => {
        const args = [
          connection.tenantId,
          connection.projectId,
          sandbox.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          body,
          signal,
        ] as const;
        const operation =
          action === "stop"
            ? await client.stopAdminSandboxSession(...args)
            : await client.rebuildAdminSandboxSession(...args);
        const [updated, loadedOperations] = await Promise.all([
          client.getAdminSandboxSession(
            connection.tenantId,
            connection.projectId,
            sandbox.metadata.uid,
            newRequestId(),
            signal,
          ),
          listAdminMaintenanceOperations(client, connection.tenantId, connection.projectId, signal),
        ]);
        setSandboxes((current) =>
          Object.freeze(
            current.map((item) =>
              item.metadata.uid === sandbox.metadata.uid ? updated.value : item,
            ),
          ),
        );
        setMaintenanceOperations(loadedOperations);
        setSelectedMaintenanceOperationId(operation.value.operationId);
      },
      true,
    );
  }

  function correctSandboxUsage(
    correction: Pick<SandboxUsageCorrectionRequest, "metric" | "adjustment" | "reasonCode">,
  ) {
    if (client === null || selectedSandbox === undefined) return;
    const sandbox = selectedSandbox;
    const body: SandboxUsageCorrectionRequest = {
      ...correction,
      expectedGeneration: sandbox.spec.generation,
      expectedResourceVersion: sandbox.metadata.resourceVersion,
      confirmedSandboxId: sandbox.metadata.uid,
    };
    const key = `sandbox:correct-usage:${sandbox.metadata.uid}:${sandbox.metadata.resourceVersion}:${correction.metric}:${correction.adjustment}:${correction.reasonCode}`;
    void runOperation(
      key,
      { key: "operation.correctSandboxUsage", values: { name: sandbox.metadata.name } },
      async (signal) => {
        const updated = await client.correctAdminSandboxUsage(
          connection.tenantId,
          connection.projectId,
          sandbox.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          body,
          signal,
        );
        setSandboxes((current) =>
          Object.freeze(
            current.map((item) =>
              item.metadata.uid === sandbox.metadata.uid ? updated.value : item,
            ),
          ),
        );
      },
      true,
    );
  }

  function createRuntimeProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null) return;
    const body = runtimeProfileCreateRequestFrom(runtimeProfileDraft);
    const key = `create-runtime-profile:${body.profileId}:${body.version}`;
    void runOperation(key, { key: "operation.createRuntimeProfile" }, async (signal) => {
      const result = await client.createAdminRuntimeProfile(
        connection.tenantId,
        connection.projectId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      setRuntimeProfiles((current) => replaceRuntimeProfile(current, result.value));
      setSelectedRuntimeProfileVersionId(result.value.metadata.uid);
      setRuntimeProfileDraft(runtimeProfileForm());
      setCreatingRuntimeProfile(false);
    });
  }

  function transitionRuntimeProfile() {
    if (
      client === null ||
      selectedRuntimeProfile === undefined ||
      runtimeProfileTransition === null
    )
      return;
    const profile = selectedRuntimeProfile;
    const action = runtimeProfileTransition;
    const key = `runtime-profile:${action}:${profile.metadata.uid}:${profile.metadata.resourceVersion}`;
    setRuntimeProfileTransition(null);
    void runOperation(
      key,
      {
        key:
          action === "publish"
            ? "operation.publishRuntimeProfile"
            : "operation.disableRuntimeProfile",
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
            ? await client.publishAdminRuntimeProfile(...args)
            : await client.disableAdminRuntimeProfile(...args);
        setRuntimeProfiles((current) => replaceRuntimeProfile(current, result.value));
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

  function createWorkspaceSnapshot(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null) return;
    const source = sandboxes.find(({ metadata }) => metadata.uid === snapshotForm.sourceSandboxId);
    if (
      source === undefined ||
      !source.spec.writerReleased ||
      source.spec.observedState !== "stopped"
    )
      return;
    const body: WorkspaceSnapshotCreateRequest = {
      snapshotId: snapshotForm.snapshotId.trim(),
      sourceSandboxId: source.metadata.uid,
      expectedSandboxGeneration: source.spec.generation,
      retentionSeconds: Number(snapshotForm.retentionSeconds),
    };
    const key = `create-workspace-snapshot:${Object.values(body).join(":")}`;
    void runOperation(key, { key: "operation.createWorkspaceSnapshot" }, async (signal) => {
      const result = await client.createAdminWorkspaceSnapshot(
        connection.tenantId,
        connection.projectId,
        newRequestId(),
        idempotencyKey(key),
        body,
        signal,
      );
      setWorkspaceSnapshots((current) =>
        Object.freeze([
          result.value,
          ...current.filter(({ metadata }) => metadata.uid !== result.value.metadata.uid),
        ]),
      );
      setSnapshotForm({
        snapshotId: "",
        sourceSandboxId: "",
        retentionSeconds: "604800",
      });
    });
  }

  function cleanupWorkspaceSnapshot() {
    if (client === null || snapshotCleanup === null) return;
    const body: WorkspaceSnapshotCleanupRequest = {
      expectedSnapshotResourceVersion: snapshotCleanup.metadata.resourceVersion,
      confirmedSnapshotId: snapshotCleanup.metadata.uid,
      confirmedSourceWorkspaceId: snapshotCleanup.spec.sourceWorkspaceId,
      snapshotDisposition: "delete",
    };
    const key = `cleanup-workspace-snapshot:${Object.values(body).join(":")}`;
    void runOperation(
      key,
      { key: "operation.cleanupWorkspaceSnapshot" },
      async (signal) => {
        const result = await client.cleanupAdminWorkspaceSnapshot(
          connection.tenantId,
          connection.projectId,
          snapshotCleanup.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          body,
          signal,
        );
        setWorkspaceSnapshots((current) =>
          Object.freeze(
            current.map((snapshot) =>
              snapshot.metadata.uid === result.value.metadata.uid ? result.value : snapshot,
            ),
          ),
        );
        setSnapshotCleanup(null);
      },
      true,
    );
  }

  function restoreWorkspaceSnapshot(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null || selectedRestoreSnapshot?.spec.status !== "available") return;
    const profile = restoreRuntimeProfiles.find(
      ({ metadata }) => metadata.uid === restoreForm.runtimeProfileVersionId,
    );
    if (profile === undefined) return;
    const body: WorkspaceSnapshotRestoreRequest = {
      expectedSnapshotResourceVersion: selectedRestoreSnapshot.metadata.resourceVersion,
      workspaceId: restoreForm.workspaceId.trim(),
      workspaceName: restoreForm.workspaceName.trim(),
      sandboxId: restoreForm.sandboxId.trim(),
      runtimeProfileId: profile.spec.profileId,
      runtimeProfileVersion: profile.spec.version,
      ttlSeconds: Number(restoreForm.ttlSeconds),
    };
    const key = `restore-workspace-snapshot:${selectedRestoreSnapshot.metadata.uid}:${Object.values(body).join(":")}`;
    void runOperation(
      key,
      { key: "operation.restoreWorkspaceSnapshot" },
      async (signal) => {
        const accepted = await client.restoreAdminWorkspaceSnapshot(
          connection.tenantId,
          connection.projectId,
          selectedRestoreSnapshot.metadata.uid,
          newRequestId(),
          idempotencyKey(key),
          body,
          signal,
        );
        const restored = await client.getAdminSandboxSession(
          connection.tenantId,
          connection.projectId,
          accepted.value.sandboxId,
          newRequestId(),
          signal,
        );
        setSandboxes((current) =>
          Object.freeze([
            restored.value,
            ...current.filter(({ metadata }) => metadata.uid !== restored.value.metadata.uid),
          ]),
        );
        setSelectedSandboxId(restored.value.metadata.uid);
        setRestoreForm({
          snapshotId: "",
          workspaceId: "",
          workspaceName: "",
          sandboxId: "",
          runtimeProfileVersionId: "",
          ttlSeconds: "3600",
        });
      },
      true,
    );
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
    const body = workerReleaseRegisterRequestFrom(releaseForm);
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
        setReleaseForm(workerReleaseForm());
        setRegisteringRelease(false);
      },
    );
  }

  function createProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null) return;
    const body = environmentProfileCreateRequestFrom(profileForm);
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
        setProfileForm(environmentProfileForm());
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
    const body = deploymentTargetRegisterRequestFrom(targetForm);
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
        setTargetForm(targetRegistrationForm());
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
      <ConnectionView
        connection={connection}
        token={token}
        status={status}
        error={error}
        theme={theme}
        onConnectionChange={updateConnection}
        onTokenChange={setToken}
        onThemeToggle={() => setTheme(theme === "dark" ? "light" : "dark")}
        onConnect={connect}
      />
    );
  }

  const feedback =
    busy !== null || error !== null ? (
      <div className="operation-feedback">
        {busy !== null ? (
          <div className="banner running" role="status" aria-live="polite">
            <span className="spinner" aria-hidden="true" />
            <span>{t(busy.message.key, busy.message.values)}…</span>
            <button
              type="button"
              onClick={(event) => {
                cancelledFeedbackRef.current = event.currentTarget.closest(".operation-feedback");
                requestRef.current?.abort();
              }}
            >
              {t("action.cancelWait")}
            </button>
          </div>
        ) : null}
        {error !== null ? (
          <div className="banner danger" role="alert" tabIndex={-1}>
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
            runtimeProfiles: runtimeProfiles.length,
            sandboxes: sandboxes.length,
            storage: storagePolicies.length,
            network: networkPolicies.length,
            capabilities: mcpServers.length + skillBundles.length,
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
              <h1>{t(pageEntry.title)}</h1>
              <p>{t(pageEntry.description)}</p>
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
              ) : page === "runtimeProfiles" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={() => {
                    setRuntimeProfileDraft((current) => ({
                      ...current,
                      targetId:
                        current.targetId ||
                        targets.find(
                          ({ spec }) =>
                            (spec.targetKind === "remote-worker" ||
                              (current.workloadTrust === "trusted-single-tenant" &&
                                spec.targetKind === "docker")) &&
                            spec.observedPhase === "ready",
                        )?.metadata.uid ||
                        targets.find(
                          ({ spec }) =>
                            spec.targetKind === "remote-worker" ||
                            (current.workloadTrust === "trusted-single-tenant" &&
                              spec.targetKind === "docker"),
                        )?.metadata.uid ||
                        "",
                      networkPolicyRef:
                        current.networkPolicyRef ||
                        networkPolicies.find((policy) =>
                          executableFoundationNetworkPolicy(policy, current.workloadTrust),
                        )?.metadata.uid ||
                        "",
                    }));
                    setCreatingRuntimeProfile(true);
                  }}
                  disabled={
                    busy !== null ||
                    !targets.some(
                      ({ spec }) =>
                        spec.targetKind === "docker" || spec.targetKind === "remote-worker",
                    ) ||
                    !networkPolicies.some((policy) =>
                      executableFoundationNetworkPolicy(policy, "trusted-single-tenant"),
                    )
                  }
                >
                  {t("action.createRuntimeProfile")}
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
            <SuccessToast
              message={t(notice.accepted ? "notice.accepted" : "notice.completed", {
                operation: t(notice.key, notice.values),
              })}
              closeLabel={t("action.close")}
              onDismiss={() => setNotice(null)}
              returnFocus={operationTriggerRef.current}
            />
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
                    {t("overview.onlineWorkers", {
                      count: number(onlineWorkerCount),
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
              <section className="panel overview-panel" aria-label={t("worker.periodicHealth")}>
                <div className="panel-heading">
                  <div>
                    <h2>{t("worker.periodicHealth")}</h2>
                    <p>{t("worker.periodicHealthBoundary")}</p>
                  </div>
                </div>
                <div className="worker-state-summary">
                  {(["online", "expired", "unavailable", "not-observed", "failed"] as const).map(
                    (state) => (
                      <button
                        type="button"
                        className="button outline"
                        data-worker-state={state}
                        key={state}
                        onClick={() => {
                          navigate("workers");
                          setWorkerStatusFilter(state);
                        }}
                      >
                        {t(`worker.health.${state}`)} ·{" "}
                        {number(filterAdminWorkers(workers, "", state).length)}
                      </button>
                    ),
                  )}
                </div>
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
          ) : page === "sandboxes" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label={t("search.sandboxes.label")}
                  placeholder={t("search.sandboxes.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">
                  sandboxes.list · {number(visibleSandboxes.length)}
                </span>
              </div>
              <div className="panel target-list-panel">
                <SandboxTable
                  sandboxes={visibleSandboxes}
                  selectedSandboxId={selectedSandboxId}
                  onSelect={selectSandbox}
                />
              </div>
              <p className="cluster-boundary">{t("sandbox.boundary")}</p>
            </section>
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
                  onClick={clearTargetFilters}
                >
                  {t("target.filter.clear")}
                </button>
                <span className="scope-chip" role="status">
                  targets.list · {number(visibleTargets.length)}
                </span>
              </div>
              <ResourceRefresh
                loading={busy?.message.key === "operation.refresh"}
                label={t("page.targets.title")}
                columns={(
                  [
                    "table.name",
                    "table.kind",
                    "table.status",
                    "table.engineApi",
                    "table.osArchitecture",
                    "table.generation",
                    "table.lastProbe",
                    "table.actions",
                  ] as const
                ).map((key) => t(key))}
              >
                <PaginatedTargets
                  filterKey={JSON.stringify([query, targetKindFilter, targetPhaseFilter])}
                  targets={visibleTargets}
                  filtered={targetsFiltered}
                  selectedTargetId={selectedTargetId}
                  onSelect={selectTarget}
                  empty={
                    <section className="empty-state" aria-labelledby="target-empty-title">
                      <div className="empty-state-header">
                        <span className="empty-state-icon" aria-hidden="true">
                          <NavigationIcon name="targets" />
                        </span>
                        <h2 id="target-empty-title">
                          {t(targetsFiltered ? "target.empty.filteredTitle" : "target.empty.title")}
                        </h2>
                        <p>
                          {t(
                            targetsFiltered
                              ? "target.filter.noMatches"
                              : "target.empty.description",
                          )}
                        </p>
                      </div>
                      <button
                        className="button primary"
                        type="button"
                        disabled={busy !== null}
                        onClick={targetsFiltered ? clearTargetFilters : () => setRegistering(true)}
                      >
                        {t(targetsFiltered ? "target.filter.clear" : "action.registerTarget")}
                      </button>
                    </section>
                  }
                />
              </ResourceRefresh>
            </section>
          ) : page === "remoteWorkers" && client !== null ? (
            <RemoteWorkerEnrollmentPanel client={client} connection={connection} />
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
                <select
                  aria-label={t("worker.healthFilter")}
                  value={workerStatusFilter}
                  onChange={(event) =>
                    setWorkerStatusFilter(event.target.value as WorkerStatusFilter)
                  }
                >
                  <option value="">{t("worker.health.all")}</option>
                  {(["online", "expired", "unavailable", "not-observed", "failed"] as const).map(
                    (state) => (
                      <option key={state} value={state}>
                        {t(`worker.health.${state}`)}
                      </option>
                    ),
                  )}
                </select>
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
                  filtered={workerStatusFilter !== "" || query.trim() !== ""}
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
          ) : page === "runtimeProfiles" ? (
            <section className="resource-list">
              <div className="list-toolbar">
                <input
                  type="search"
                  aria-label={t("search.runtimeProfiles.label")}
                  placeholder={t("search.runtimeProfiles.placeholder")}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <span className="scope-chip">
                  profiles.list · {number(visibleRuntimeProfiles.length)}
                </span>
              </div>
              <div className="panel target-list-panel">
                <RuntimeProfileTable
                  profiles={visibleRuntimeProfiles}
                  selectedProfileVersionId={selectedRuntimeProfileVersionId}
                  onSelect={selectRuntimeProfile}
                />
              </div>
              <p className="cluster-boundary">{t("runtimeProfile.boundary")}</p>
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
              runtimeProfiles={runtimeProfiles}
              query={query}
              busy={busy !== null}
              onQuery={setQuery}
              onChange={setNetworkPolicies}
              run={runOperation}
              idempotencyKey={idempotencyKey}
            />
          ) : page === "capabilities" ? (
            <CapabilityPanel
              mcpServers={mcpServers}
              skillBundles={skillBundles}
              bindings={capabilityBindingRelations(
                mcpServers,
                skillBundles,
                capabilitySessions,
                capabilityExecutions,
              )}
              events={capabilityEvents}
              query={query}
              onQuery={setQuery}
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
                    <h2>{t("workspaceSnapshot.title")}</h2>
                    <p>{t("workspaceSnapshot.description")}</p>
                  </div>
                  <span className="scope-chip">
                    snapshots.list · snapshots.create · snapshots.act · snapshots.delete
                  </span>
                </div>
                <form className="resource-form" onSubmit={createWorkspaceSnapshot}>
                  <div className="form-row">
                    <label>
                      <span>{t("workspaceSnapshot.id")}</span>
                      <input
                        required
                        maxLength={128}
                        spellCheck={false}
                        value={snapshotForm.snapshotId}
                        onChange={(event) =>
                          setSnapshotForm((current) => ({
                            ...current,
                            snapshotId: event.target.value,
                          }))
                        }
                        placeholder="snapshot-before-upgrade"
                      />
                    </label>
                    <label>
                      <span>{t("workspaceSnapshot.source")}</span>
                      <select
                        required
                        value={snapshotForm.sourceSandboxId}
                        onChange={(event) =>
                          setSnapshotForm((current) => ({
                            ...current,
                            sourceSandboxId: event.target.value,
                          }))
                        }
                      >
                        <option value="">{t("workspaceSnapshot.selectSource")}</option>
                        {sandboxes
                          .filter(
                            ({ spec }) => spec.writerReleased && spec.observedState === "stopped",
                          )
                          .map((sandbox) => (
                            <option key={sandbox.metadata.uid} value={sandbox.metadata.uid}>
                              {sandbox.spec.workspaceName} · {sandbox.metadata.uid} · g
                              {number(sandbox.spec.generation)}
                            </option>
                          ))}
                      </select>
                    </label>
                    <label>
                      <span>{t("workspaceSnapshot.retention")}</span>
                      <input
                        required
                        type="number"
                        min={1}
                        max={31_536_000}
                        value={snapshotForm.retentionSeconds}
                        onChange={(event) =>
                          setSnapshotForm((current) => ({
                            ...current,
                            retentionSeconds: event.target.value,
                          }))
                        }
                      />
                    </label>
                  </div>
                  <p className="cluster-boundary">{t("workspaceSnapshot.offlineBoundary")}</p>
                  <button
                    className="button primary"
                    type="submit"
                    disabled={busy !== null || snapshotForm.sourceSandboxId === ""}
                  >
                    {t("workspaceSnapshot.create")}
                  </button>
                </form>
                <form className="resource-form" onSubmit={restoreWorkspaceSnapshot}>
                  <div className="form-row">
                    <label>
                      <span>{t("workspaceSnapshot.restoreSource")}</span>
                      <select
                        required
                        value={restoreForm.snapshotId}
                        onChange={(event) =>
                          setRestoreForm((current) => ({
                            ...current,
                            snapshotId: event.target.value,
                            runtimeProfileVersionId: "",
                          }))
                        }
                      >
                        <option value="">{t("workspaceSnapshot.selectRestoreSource")}</option>
                        {workspaceSnapshots
                          .filter(({ spec }) => spec.status === "available")
                          .map((snapshot) => (
                            <option key={snapshot.metadata.uid} value={snapshot.metadata.uid}>
                              {snapshot.metadata.name} · {snapshot.spec.backend} ·{" "}
                              {snapshot.spec.sourceTargetId}
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
                          setRestoreForm((current) => ({
                            ...current,
                            runtimeProfileVersionId: event.target.value,
                          }))
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
                    {(
                      [
                        [
                          "workspaceId",
                          "workspaceSnapshot.restoreWorkspaceId",
                          "workspace-restored",
                        ],
                        [
                          "workspaceName",
                          "workspaceSnapshot.restoreWorkspaceName",
                          "Restored workspace",
                        ],
                        ["sandboxId", "workspaceSnapshot.restoreSandboxId", "sandbox-restored"],
                      ] as const
                    ).map(([field, label, placeholder]) => (
                      <label key={field}>
                        <span>{t(label)}</span>
                        <input
                          required
                          maxLength={128}
                          pattern={targetIdentifierPattern}
                          spellCheck={false}
                          value={restoreForm[field]}
                          placeholder={placeholder}
                          onChange={(event) =>
                            setRestoreForm((current) => ({
                              ...current,
                              [field]: event.target.value,
                            }))
                          }
                        />
                      </label>
                    ))}
                    <label>
                      <span>{t("workspaceSnapshot.restoreTtl")}</span>
                      <input
                        required
                        type="number"
                        min={60}
                        max={86_400}
                        value={restoreForm.ttlSeconds}
                        onChange={(event) =>
                          setRestoreForm((current) => ({
                            ...current,
                            ttlSeconds: event.target.value,
                          }))
                        }
                      />
                    </label>
                  </div>
                  <p className="cluster-boundary">{t("workspaceSnapshot.restoreBoundary")}</p>
                  <button
                    className="button primary"
                    type="submit"
                    disabled={
                      busy !== null ||
                      selectedRestoreSnapshot?.spec.status !== "available" ||
                      restoreForm.runtimeProfileVersionId === ""
                    }
                  >
                    {t("workspaceSnapshot.restore")}
                  </button>
                </form>
                <WorkspaceSnapshotTable
                  snapshots={workspaceSnapshots}
                  onCleanup={(snapshot) => {
                    operationTriggerRef.current =
                      document.activeElement instanceof HTMLElement ? document.activeElement : null;
                    setSnapshotCleanup(snapshot);
                  }}
                />
              </section>

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
              <ResourceRefresh
                loading={busy?.message.key === "operation.refresh"}
                label={t("page.leases.title")}
                columns={(
                  [
                    "table.name",
                    "table.observed",
                    "table.cleanup",
                    "table.generation",
                    "table.expires",
                    "table.actions",
                  ] as const
                ).map((key) => t(key))}
              >
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
              </ResourceRefresh>
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
              <DeniedWritePanel
                key={`${connection.tenantId}/${connection.projectId}`}
                client={client}
                connection={connection}
              />
            </section>
          )}
        </main>
      </section>

      {runtimeProfileDetailOpen && selectedRuntimeProfile !== undefined ? (
        <AdminSheet
          label={t("sheet.runtimeProfile", {
            name: selectedRuntimeProfile.metadata.name,
          })}
          feedback={feedback}
          onClose={() => {
            setRuntimeProfileDetailOpen(false);
            setRuntimeProfileTransition(null);
          }}
        >
          <aside className="detail-panel" aria-label={t("sheet.selectedRuntimeProfile")}>
            <button
              className="sheet-close"
              type="button"
              aria-label={t("action.close")}
              onClick={() => {
                setRuntimeProfileDetailOpen(false);
                setRuntimeProfileTransition(null);
              }}
            >
              ×
            </button>
            <RuntimeProfileDetail
              profile={selectedRuntimeProfile}
              disabled={busy !== null}
              onTransition={setRuntimeProfileTransition}
            />
          </aside>
        </AdminSheet>
      ) : null}

      {sandboxDetailOpen && selectedSandbox !== undefined ? (
        <AdminSheet
          label={t("sheet.sandbox", { name: selectedSandbox.metadata.name })}
          feedback={feedback}
          onClose={() => {
            setSandboxDetailOpen(false);
            setSandboxLifecycleTransition(null);
            setSandboxGrantRevoke(null);
          }}
        >
          <aside className="detail-panel" aria-label={t("sheet.selectedSandbox")}>
            <button
              className="sheet-close"
              type="button"
              aria-label={t("action.close")}
              onClick={() => {
                setSandboxDetailOpen(false);
                setSandboxLifecycleTransition(null);
                setSandboxGrantRevoke(null);
              }}
            >
              ×
            </button>
            <SandboxDetail
              sandbox={selectedSandbox}
              grants={sandboxAccessGrants}
              sessions={managedAgentSessions}
              executions={managedAgentExecutions}
              events={managedAgentEvents}
              runtimeProfiles={runtimeProfiles}
              disabled={busy !== null}
              onTransition={setSandboxLifecycleTransition}
              onRevokeGrant={setSandboxGrantRevoke}
              onCorrectUsage={correctSandboxUsage}
              onReconcileSideEffect={reconcileManagedAgentSideEffect}
            />
          </aside>
        </AdminSheet>
      ) : null}

      {sandboxLifecycleTransition !== null && selectedSandbox !== undefined ? (
        <AdminSheet
          confirmation
          feedback={feedback}
          returnFocus={operationTriggerRef.current}
          label={t("sheet.sandboxTransition", {
            action: t(
              sandboxLifecycleTransition === "stop"
                ? "sandbox.lifecycle.stop"
                : "sandbox.lifecycle.rebuild",
            ),
            name: selectedSandbox.metadata.name,
          })}
          onClose={() => setSandboxLifecycleTransition(null)}
        >
          <SandboxLifecycleConfirmation
            sandbox={selectedSandbox}
            action={sandboxLifecycleTransition}
            disabled={busy !== null}
            onClose={() => setSandboxLifecycleTransition(null)}
            onConfirm={transitionSandbox}
          />
        </AdminSheet>
      ) : null}

      {sandboxGrantRevoke !== null ? (
        <AdminSheet
          confirmation
          feedback={feedback}
          returnFocus={operationTriggerRef.current}
          label={t("sheet.sandboxGrantRevoke", {
            name: sandboxGrantRevoke.metadata.uid,
          })}
          onClose={() => setSandboxGrantRevoke(null)}
        >
          <SandboxGrantRevokeConfirmation
            grant={sandboxGrantRevoke}
            disabled={busy !== null}
            onClose={() => setSandboxGrantRevoke(null)}
            onConfirm={revokeSandboxAccessGrant}
          />
        </AdminSheet>
      ) : null}

      {snapshotCleanup !== null ? (
        <AdminSheet
          confirmation
          feedback={feedback}
          returnFocus={operationTriggerRef.current}
          label={t("workspaceSnapshot.cleanupTitle")}
          onClose={() => setSnapshotCleanup(null)}
        >
          <WorkspaceSnapshotCleanupConfirmation
            snapshot={snapshotCleanup}
            disabled={busy !== null}
            onClose={() => setSnapshotCleanup(null)}
            onConfirm={cleanupWorkspaceSnapshot}
          />
        </AdminSheet>
      ) : null}

      {runtimeProfileTransition !== null && selectedRuntimeProfile !== undefined ? (
        <AdminSheet
          confirmation
          feedback={feedback}
          label={t("sheet.profileTransition", {
            action: t(
              runtimeProfileTransition === "publish"
                ? "profile.transition.publish"
                : "profile.transition.disable",
            ),
            name: selectedRuntimeProfile.metadata.name,
          })}
          onClose={() => setRuntimeProfileTransition(null)}
        >
          <ProfileTransitionConfirmation
            profile={selectedRuntimeProfile}
            action={runtimeProfileTransition}
            disabled={busy !== null}
            onClose={() => setRuntimeProfileTransition(null)}
            onConfirm={transitionRuntimeProfile}
          />
        </AdminSheet>
      ) : null}

      {creatingRuntimeProfile ? (
        <RuntimeProfileCreateForm
          draft={runtimeProfileDraft}
          targets={targets}
          networkPolicies={networkPolicies}
          feedback={feedback}
          disabled={busy !== null}
          onDraftChange={setRuntimeProfileDraft}
          onClose={() => setCreatingRuntimeProfile(false)}
          onSubmit={createRuntimeProfile}
        />
      ) : null}

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
            {client !== null ? (
              <WorkerHealthCheck
                key={`${connection.tenantId}:${connection.projectId}:${selectedWorker.metadata.uid}:${selectedWorker.spec.generation}:${selectedWorker.metadata.resourceVersion}`}
                worker={selectedWorker}
                client={client}
                connection={connection}
              />
            ) : null}
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
        <ReleaseRegistrationForm
          draft={releaseForm}
          feedback={feedback}
          disabled={busy !== null}
          onDraftChange={setReleaseForm}
          onClose={() => setRegisteringRelease(false)}
          onSubmit={registerRelease}
        />
      ) : null}

      {creatingProfile ? (
        <EnvironmentProfileCreateForm
          draft={profileForm}
          storagePolicies={storagePolicies}
          networkPolicies={networkPolicies}
          releases={releases}
          feedback={feedback}
          disabled={busy !== null}
          onDraftChange={setProfileForm}
          onClose={() => setCreatingProfile(false)}
          onSubmit={createProfile}
        />
      ) : null}

      {registering ? (
        <TargetRegistrationForm
          draft={targetForm}
          feedback={feedback}
          disabled={busy !== null}
          onDraftChange={setTargetForm}
          onClose={() => setRegistering(false)}
          onSubmit={registerTarget}
        />
      ) : null}
    </div>
  );
}
