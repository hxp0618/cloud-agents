import { CLIAuthorization } from "../../user-web/src/CLIAuthorization";
import {
  LoginView,
  listAllAdminProjects,
  listAllBrowserTenants,
  loginMessage,
  sessionMessage,
} from "./app/connection";
import {
  PaginatedTargets,
  TargetTable,
  TargetDetail,
  TargetRegistrationForm,
  deploymentTargetRegisterRequestFrom,
  targetRegistrationForm,
  withoutKubeconfig,
  SchedulingConfirmation,
  CleanupConfirmation,
} from "./app/targets";
import { kubernetesCredentialDigest } from "./app/kubeconfig";
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
import { WorkspaceSnapshotCleanupConfirmation, WorkspaceSnapshotPanel } from "./app/workspaces";
import { AdvancedFields, NameField, Suggestions } from "./app/form-fields";
import {
  ReleaseRegistrationForm,
  StoragePolicyTable,
  ReleaseTable,
  workerReleaseForm,
  workerReleaseRegisterRequestFrom,
  quotaFormFrom,
  storagePolicyFormFrom,
} from "./app/policies";
import { useEffect, useLayoutEffect, useRef, useState, type FormEvent } from "react";
import { InvitationManagement } from "./InvitationManagement";
import { ServiceAccountManagement } from "./ServiceAccountManagement";
import { AccountManagement } from "./AccountManagement";
import { ProviderManagement } from "./ProviderManagement";
import { EmailPolicyForm } from "./EmailPolicyForm";
import {
  ClientError,
  createBrowserHTTPClient,
  createSessionHTTPClient,
  type AdminSandboxAccessGrant,
  type AdminSandboxSession,
  type AdminAuditEvent,
  type DeploymentTarget,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetSchedulingPreview,
  type DeploymentTargetRegisterRequest,
  type EnvironmentLease,
  type EnvironmentLeaseUpgradePreview,
  type EnvironmentProfile,
  type MaintenanceOperation,
  type ManagedAgentExecution,
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
  type BrowserSession,
  type BrowserSessionClient,
  type BrowserTenant,
  type Client,
  type Project,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import { ResourceRefresh } from "./ResourceRefresh";
import { SuccessToast } from "./SuccessToast";
import { AdminSheet } from "./AdminSheet";

import {
  adminFailure as adminFailureFrom,
  adminMutationKey,
  filterAdminLeases,
  filterAdminWorkers,
  cleanupRequestFromPreview,
  keepIfUnchanged,
  leaseReleaseRequestFromPreview,
  listAdminMaintenanceOperations,
  loadAdminManagedAgentRuntime,
  listAdminProjectLeaseQuotaAuditEvents,
  listAdminStoragePolicyAuditEvents,
  capabilityBindingRelations,
  identifierFromName,
  identifierWithSuffix,
  listAdminSandboxAccessGrants,
  listAdminWorkers,
  newIdentifierSuffix,
  newRequestId,
  nextProfileVersion,
  pendingIdempotencyKey,
  replaceLease,
  replaceProfile,
  replaceRuntimeProfile,
  replaceRelease,
  replaceStoragePolicy,
  replaceTarget,
  readSavedAdminConnection,
  schedulingRequestFromPreview,
  selectAdminResourceId,
  workerRefreshKey,
  writeSavedAdminConnection,
  type AdminManagedAgentRuntime,
  type SavedAdminConnection,
  type SandboxLifecycleAction,
  type WorkerStatusFilter,
} from "./admin";

function emptyManagedAgentRuntime(): AdminManagedAgentRuntime {
  return Object.freeze({
    sessions: Object.freeze([]),
    executions: Object.freeze([]),
    seenSessionPageTokens: Object.freeze([]),
    selectedSessionId: "",
    seenExecutionPageTokens: Object.freeze([]),
  });
}
import { NetworkPolicyPanel } from "./NetworkPolicyPanel";
import { CapabilityPanel } from "./CapabilityPanel";
import { DeniedWritePanel } from "./DeniedWritePanel";
import { RemoteWorkerEnrollmentPanel } from "./RemoteWorkerEnrollmentPanel";
import { TargetFilters } from "./TargetFilters";
import { AdminSidebar } from "./AdminSidebar";
import { NavigationCommands, NavigationIcon, ResourceNavigation, type Page } from "./navigation";
import { LocaleSelect, useI18n, type MessageKey, type MessageValues } from "./i18n";
import {
  loadAdminWorkspaceData,
  loadAdminPollData,
  type AdminWorkspaceData,
  type AdminPageData,
  loadProfileAudit,
  loadTargetActivity,
} from "./app/loaders";
import {
  executableFoundationNetworkPolicy,
  runtimeProfileCreatable,
  runtimeProfileTargetAllowed,
  phaseTone,
  phaseLabel,
  auditLabel,
  type TargetKind,
} from "./app/presentation";
import { deriveAdminView } from "./app/derived";

type LeaseReleaseTransition = "upgrade" | "rollback";
type LocalizedMessage = Readonly<{ key: MessageKey; values?: MessageValues }>;
type OperationNotice = LocalizedMessage & Readonly<{ accepted?: boolean }>;
type BusyOperation = Readonly<{ message: LocalizedMessage }>;
type Theme = "light" | "dark";

function initialTheme(): Theme {
  const saved = window.localStorage.getItem("cloud-agents-admin-theme");
  if (saved === "light" || saved === "dark") return saved;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

type DashboardProps = Readonly<{
  client: Client;
  connection: SavedAdminConnection;
  projects: readonly Project[];
  session: BrowserSession;
  sessionClient: BrowserSessionClient;
  tenant: BrowserTenant;
  tenants: readonly BrowserTenant[];
  project: Project;
  onScopeChange: (tenantId: string, projectId: string) => void;
  onSessionExpired: () => void;
}>;

function Dashboard({
  client,
  connection,
  projects,
  session,
  sessionClient,
  tenant,
  tenants,
  project,
  onScopeChange,
  onSessionExpired,
}: DashboardProps) {
  const { t, number, dateTime } = useI18n();
  const [theme, setTheme] = useState<Theme>(initialTheme);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const [commandsOpen, setCommandsOpen] = useState(false);
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
  const [managedAgentRuntime, setManagedAgentRuntime] = useState(emptyManagedAgentRuntime);
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
  const [capabilityRuntime, setCapabilityRuntime] = useState(emptyManagedAgentRuntime);
  const [networkEditorEpoch, setNetworkEditorEpoch] = useState(0);
  const [creatingRemoteWorker, setCreatingRemoteWorker] = useState(false);
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
  const [pageLoading, setPageLoading] = useState(false);
  const [pageErrors, setPageErrors] = useState<AdminPageData["errors"]>({});
  const [loadedResources, setLoadedResources] = useState<Set<keyof AdminWorkspaceData>>(
    () => new Set(),
  );
  const loadedPageRef = useRef<Page | null>(null);
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
  const [snapshotForm, setSnapshotForm] = useState(() => ({
    sourceSandboxId: "",
    retentionSeconds: "604800",
    token: newIdentifierSuffix(),
  }));
  const [restoreForm, setRestoreForm] = useState(() => ({
    snapshotId: "",
    workspaceName: "",
    runtimeProfileVersionId: "",
    ttlSeconds: "3600",
    token: newIdentifierSuffix(),
  }));
  const requestRef = useRef<AbortController | null>(null);
  const busyRef = useRef(false);
  const pageLoadingRef = useRef(false);
  const operationTriggerRef = useRef<HTMLElement | null>(null);
  const cancelledFeedbackRef = useRef<HTMLElement | null>(null);
  const pendingKeysRef = useRef(new Map<string, string>());
  const profileMenuRef = useRef<HTMLDetailsElement>(null);

  const connected = true;
  const interactionDisabled = busy !== null || pageLoading;
  const tenantCanManage =
    session.user.displayRoles.includes("platform.admin") ||
    tenant.displayRoles.includes("tenant.admin");
  function adminFailure(cause: unknown) {
    if (cause instanceof ClientError && cause.status === 401) onSessionExpired();
    return adminFailureFrom(cause);
  }
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
  const {
    selectedTarget,
    selectedCleanupPreview,
    selectedSchedulingPreview,
    selectedLease,
    selectedLeaseTarget,
    upgradeReleaseDigest,
    selectedLeaseReleasePreview,
    selectedWorker,
    selectedProfile,
    selectedRuntimeProfile,
    selectedSandbox,
    selectedStoragePolicy,
    selectedRestoreSnapshot,
    restoreRuntimeProfiles,
    selectedStoragePolicyReferenced,
    selectedMaintenanceOperation,
    readyCount,
    probingCount,
    unprobedCount,
    unavailableCount,
    attentionCount,
    readyLeaseCount,
    leaseAttentionCount,
    onlineWorkerCount,
    visibleTargets,
    targetsFiltered,
    visibleLeases,
    visibleWorkers,
    visibleReleases,
    visibleClusterHosts,
    visibleProfiles,
    visibleRuntimeProfiles,
    visibleSandboxes,
    visibleStoragePolicies,
    visibleMaintenanceOperations,
    failedMaintenanceOperations,
    pageEntry,
  } = deriveAdminView({
    resources: {
      targets,
      leases,
      workers,
      releases,
      profiles,
      runtimeProfiles,
      sandboxes,
      workspaceSnapshots,
      storagePolicies,
      maintenanceOperations,
    },
    selection: {
      page,
      targetId: selectedTargetId,
      leaseId: selectedLeaseId,
      upgradeReleaseDigest: selectedUpgradeReleaseDigest,
      workerId: selectedWorkerId,
      profileVersionId: selectedProfileVersionId,
      runtimeProfileVersionId: selectedRuntimeProfileVersionId,
      sandboxId: selectedSandboxId,
      storagePolicyId: selectedStoragePolicyId,
      restoreSnapshotId: restoreForm.snapshotId,
      maintenanceOperationId: selectedMaintenanceOperationId,
    },
    previews: {
      cleanup: cleanupPreview,
      scheduling: schedulingPreview,
      leaseRelease: leaseReleasePreview,
    },
    filters: {
      query,
      targetKinds: targetKindFilter,
      targetPhases: targetPhaseFilter,
      leaseAttentionOnly,
      leasePhase: leasePhaseFilter,
      leaseCleanupBlockedOnly,
      workerStatus: workerStatusFilter,
      failedOperationsOnly,
    },
  });

  const storagePolicyNameTaken =
    selectedStoragePolicy === undefined &&
    storagePolicyForm.policyName.trim() !== "" &&
    storagePolicies.some(
      ({ metadata }) =>
        metadata.uid === identifierFromName(storagePolicyForm.policyName.trim(), "storage-policy"),
    );

  useEffect(() => {
    if (!connected || client === null) return;
    if (loadedPageRef.current === page) {
      pageLoadingRef.current = false;
      setPageLoading(false);
      return;
    }
    const controller = new AbortController();
    pageLoadingRef.current = true;
    setPageLoading(true);
    setPageErrors({});
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    void loadAdminWorkspaceData(
      client,
      connection,
      selectedTargetId,
      selectedLeaseId,
      selectedProfileVersionId,
      signal,
      page,
    )
      .then(async (loaded) => {
        if (controller.signal.aborted) return;
        applyWorkspaceData(loaded.data, "preserve");
        setPageErrors(loaded.errors);
        const runtime =
          page === "capabilities"
            ? await loadAdminManagedAgentRuntime(
                client,
                connection.tenantId,
                connection.projectId,
                "",
                signal,
              )
            : undefined;
        if (controller.signal.aborted) return;
        if (runtime !== undefined) setCapabilityRuntime(runtime);
        loadedPageRef.current = page;
      })
      .catch((cause) => {
        if (!controller.signal.aborted) setError(adminFailure(cause));
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          pageLoadingRef.current = false;
          setPageLoading(false);
        }
      });
    return () => {
      controller.abort();
      pageLoadingRef.current = false;
    };
  }, [client, connected, connection, page]);

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
    const sandboxLifecyclePending = sandboxes.some(({ spec }) =>
      ["pending", "unknown"].includes(spec.observedState),
    );
    const snapshotPending = workspaceSnapshots.some(({ spec }) =>
      ["pending", "unknown", "deleting"].includes(spec.status),
    );
    const targetLifecyclePending = targets.some(({ spec }) => spec.observedPhase === "probing");
    const leaseLifecyclePending = leases.some(
      ({ spec }) =>
        spec.observedPhase === "provisioning" ||
        spec.observedPhase === "terminating" ||
        ["pending", "revoking", "reaping"].includes(spec.cleanupPhase),
    );
    const lifecyclePending =
      targetLifecyclePending || leaseLifecyclePending || sandboxLifecyclePending || snapshotPending;
    const observeHealth =
      (page === "workers" || page === "overview") &&
      workers.some(({ spec }) => spec.state === "ready");
    if (!connected || client === null || (!lifecyclePending && !observeHealth)) return;
    const controller = new AbortController();
    let polling = false;
    const interval = window.setInterval(
      () => {
        if (
          document.visibilityState !== "visible" ||
          busyRef.current ||
          pageLoadingRef.current ||
          polling
        )
          return;
        polling = true;
        const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
        void loadAdminPollData(
          client,
          connection,
          selectedTargetId,
          selectedLeaseId,
          {
            targets: targetLifecyclePending,
            leases: leaseLifecyclePending,
            workers: observeHealth,
            sandboxes: sandboxLifecyclePending,
            workspaceSnapshots: snapshotPending,
          },
          signal,
        )
          .then(({ data, errors }) => {
            if (controller.signal.aborted) return;
            if (data.targets !== undefined) {
              setTargets((current) => keepIfUnchanged(current, data.targets!.targets));
              setSelectedTargetId(data.targets.selectedTargetId);
            }
            if (data.leases !== undefined) {
              setLeases((current) => keepIfUnchanged(current, data.leases!.leases));
              setSelectedLeaseId(data.leases.selectedLeaseId);
            }
            if (data.workers !== undefined) {
              setWorkers((current) => keepIfUnchanged(current, data.workers!, workerRefreshKey));
              setSelectedWorkerId((current) => selectAdminResourceId(data.workers!, current));
            }
            if (data.sandboxes !== undefined)
              setSandboxes((current) => keepIfUnchanged(current, data.sandboxes!));
            if (data.workspaceSnapshots !== undefined)
              setWorkspaceSnapshots((current) =>
                keepIfUnchanged(current, data.workspaceSnapshots!),
              );
            const failure = Object.values(errors)[0];
            if (failure !== undefined) setError(adminFailure(failure));
          })
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

  function navigate(nextPage: Page) {
    if (busyRef.current) {
      setCommandsOpen(false);
      setMobileNavOpen(false);
      return;
    }
    setError(null);
    setCommandsOpen(false);
    setCreatingRemoteWorker(false);
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

  function idempotencyKey(key: string): string {
    return pendingIdempotencyKey(pendingKeysRef.current, key);
  }

  async function runOperation(
    operationKey: string,
    message: LocalizedMessage,
    operation: (signal: AbortSignal) => Promise<void>,
    outcome: "completed" | "accepted" | "silent" = "completed",
  ) {
    if (busyRef.current || pageLoading || pageLoadingRef.current) return;
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
      // Read-only detail loads succeed silently; only state changes deserve a toast.
      if (outcome !== "silent") setNotice({ ...message, accepted: outcome === "accepted" });
    } catch (cause) {
      setError(adminFailure(cause));
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
      busyRef.current = false;
      setBusy(null);
    }
  }

  function logout() {
    void runOperation("auth:logout", { key: "operation.logout" }, async (signal) => {
      await sessionClient.logoutBrowserSession(signal);
      onSessionExpired();
    });
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

  function applyWorkspaceData(
    loaded: Partial<AdminWorkspaceData>,
    selection: "first" | "preserve",
  ) {
    setLoadedResources(
      (current) => new Set([...current, ...(Object.keys(loaded) as (keyof AdminWorkspaceData)[])]),
    );
    if (loaded.targets !== undefined) {
      setTargets(loaded.targets.targets);
      setSelectedTargetId(loaded.targets.selectedTargetId);
    }
    if (loaded.leases !== undefined) {
      setLeases(loaded.leases.leases);
      setSelectedLeaseId(loaded.leases.selectedLeaseId);
    }
    if (loaded.workers !== undefined) {
      setWorkers(loaded.workers);
      setSelectedWorkerId((current) =>
        selectAdminResourceId(loaded.workers!, selection === "preserve" ? current : ""),
      );
    }
    if (loaded.releases !== undefined) setReleases(loaded.releases);
    if (loaded.profiles !== undefined) {
      setProfiles(loaded.profiles.profiles);
      setSelectedProfileVersionId(loaded.profiles.selectedProfileVersionId);
    }
    if (loaded.runtimeProfiles !== undefined) {
      setRuntimeProfiles(loaded.runtimeProfiles);
      setSelectedRuntimeProfileVersionId((current) =>
        selectAdminResourceId(loaded.runtimeProfiles!, selection === "preserve" ? current : ""),
      );
    }
    if (loaded.sandboxes !== undefined) {
      setSandboxes(loaded.sandboxes);
      setSelectedSandboxId((current) =>
        selectAdminResourceId(loaded.sandboxes!, selection === "preserve" ? current : ""),
      );
    }
    if (loaded.workspaceSnapshots !== undefined) setWorkspaceSnapshots(loaded.workspaceSnapshots);
    if (loaded.storagePolicies !== undefined) {
      setStoragePolicies(loaded.storagePolicies);
      setSelectedStoragePolicyId((current) =>
        selectAdminResourceId(loaded.storagePolicies!, selection === "preserve" ? current : ""),
      );
    }
    if (loaded.networkPolicies !== undefined) setNetworkPolicies(loaded.networkPolicies);
    if (loaded.mcpServers !== undefined) setMcpServers(loaded.mcpServers);
    if (loaded.skillBundles !== undefined) setSkillBundles(loaded.skillBundles);
    if (Object.hasOwn(loaded, "quota")) {
      setLeaseQuota(loaded.quota);
      setQuotaForm(quotaFormFrom(loaded.quota));
    }
    if (loaded.quotaAudit !== undefined) setLeaseQuotaAudit(loaded.quotaAudit);
    if (loaded.maintenanceOperations !== undefined) {
      setMaintenanceOperations(loaded.maintenanceOperations);
      setSelectedMaintenanceOperationId((current) =>
        selection === "preserve" &&
        loaded.maintenanceOperations!.some(({ operationId }) => operationId === current)
          ? current
          : (loaded.maintenanceOperations![0]?.operationId ?? ""),
      );
    }
  }

  function refresh() {
    if (client === null) return;
    void runOperation("refresh", { key: "operation.refresh" }, async (signal) => {
      const [workspaceResult, runtimeResult] = await Promise.allSettled([
        loadAdminWorkspaceData(
          client,
          connection,
          selectedTargetId,
          selectedLeaseId,
          selectedProfileVersionId,
          signal,
          page,
        ),
        page === "capabilities"
          ? loadAdminManagedAgentRuntime(
              client,
              connection.tenantId,
              connection.projectId,
              "",
              signal,
              capabilityRuntime,
            )
          : Promise.resolve(undefined),
      ]);
      if (signal.aborted) return;
      if (runtimeResult.status === "fulfilled" && runtimeResult.value !== undefined)
        setCapabilityRuntime(runtimeResult.value);
      if (workspaceResult.status === "rejected") throw workspaceResult.reason;
      const loaded = workspaceResult.value;
      applyWorkspaceData(loaded.data, "preserve");
      setPageErrors(loaded.errors);
      const failure =
        Object.values(loaded.errors)[0] ??
        (runtimeResult.status === "rejected" ? runtimeResult.reason : undefined);
      if (failure !== undefined) throw failure;
      const data = loaded.data;
      if (targetDetailOpen && data.targets !== undefined && data.targets.selectedTargetId !== "") {
        const activity = await loadTargetActivity(
          client,
          connection,
          data.targets.selectedTargetId,
          signal,
        );
        setTargetOperations(activity.operations);
        setTargetAudit(activity.audit);
      }
      const profile = data.profiles?.profiles.find(
        ({ metadata }) => metadata.uid === data.profiles?.selectedProfileVersionId,
      );
      if (profileDetailOpen && profile !== undefined) {
        setProfileAudit(await loadProfileAudit(client, connection, profile, signal));
      }
      const storagePolicy =
        data.storagePolicies?.find(({ metadata }) => metadata.uid === selectedStoragePolicyId) ??
        data.storagePolicies?.[0];
      if (data.storagePolicies === undefined) return;
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
    void runOperation(
      `get:${targetId}`,
      { key: "operation.targetDetail" },
      async (signal) => {
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
      },
      "silent",
    );
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
    void runOperation(
      `get-lease:${leaseId}`,
      { key: "operation.leaseDetail" },
      async (signal) => {
        const result = await client.getAdminEnvironmentLease(
          connection.tenantId,
          connection.projectId,
          leaseId,
          newRequestId(),
          signal,
        );
        setLeases((current) => replaceLease(current, result.value));
      },
      "silent",
    );
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
      "silent",
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
      "silent",
    );
  }

  function selectSandbox(sandboxId: string) {
    setSandboxDetailOpen(true);
    setSandboxLifecycleTransition(null);
    setSandboxGrantRevoke(null);
    setSandboxAccessGrants(Object.freeze([]));
    setManagedAgentRuntime(emptyManagedAgentRuntime());
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
        setManagedAgentRuntime(agentRuntime);
      },
      "silent",
    );
  }

  function pageManagedAgentRuntime(
    scope: "capability" | "sandbox",
    action: "sessions" | "executions" | "select",
    sessionId = "",
  ) {
    if (client === null) return;
    const current = scope === "capability" ? capabilityRuntime : managedAgentRuntime;
    const sandboxId = scope === "sandbox" ? selectedSandboxId : "";
    void runOperation(
      `managed-agent:${scope}:${action}:${sessionId || "next"}`,
      { key: "operation.refresh" },
      async (signal) => {
        const runtime = await loadAdminManagedAgentRuntime(
          client,
          connection.tenantId,
          connection.projectId,
          sandboxId,
          signal,
          current,
          action === "sessions",
          action === "select" ? sessionId : current.selectedSessionId,
          action === "executions",
        );
        if (scope === "capability") setCapabilityRuntime(runtime);
        else setManagedAgentRuntime(runtime);
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
          managedAgentRuntime,
        );
        setManagedAgentRuntime(runtime);
      },
      "accepted",
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
      "accepted",
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
    const key = adminMutationKey("sandbox:correct-usage", body);
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
      "accepted",
    );
  }

  function createRuntimeProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null) return;
    const profileId = identifierFromName(runtimeProfileDraft.profileName.trim(), "runtime-profile");
    const body = runtimeProfileCreateRequestFrom(
      runtimeProfileDraft,
      nextProfileVersion(runtimeProfiles, profileId),
    );
    const key = adminMutationKey("create-runtime-profile", body);
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
    const key = adminMutationKey("set-lease-quota", body);
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
      snapshotId: identifierWithSuffix(
        identifierFromName(source.spec.workspaceName, "snapshot"),
        snapshotForm.token,
      ),
      sourceSandboxId: source.metadata.uid,
      expectedSandboxGeneration: source.spec.generation,
      retentionSeconds: Number(snapshotForm.retentionSeconds),
    };
    const key = adminMutationKey("create-workspace-snapshot", body);
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
        sourceSandboxId: "",
        retentionSeconds: "604800",
        token: newIdentifierSuffix(),
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
    const key = adminMutationKey("cleanup-workspace-snapshot", body);
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
      "accepted",
    );
  }

  function restoreWorkspaceSnapshot(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null || selectedRestoreSnapshot?.spec.status !== "available") return;
    const profile = restoreRuntimeProfiles.find(
      ({ metadata }) => metadata.uid === restoreForm.runtimeProfileVersionId,
    );
    if (profile === undefined) return;
    const workspaceName = restoreForm.workspaceName.trim();
    const base = identifierFromName(workspaceName, "workspace");
    const body: WorkspaceSnapshotRestoreRequest = {
      expectedSnapshotResourceVersion: selectedRestoreSnapshot.metadata.resourceVersion,
      workspaceId: identifierWithSuffix(base, `${restoreForm.token}-w`),
      workspaceName,
      sandboxId: identifierWithSuffix(base, `${restoreForm.token}-s`),
      runtimeProfileId: profile.spec.profileId,
      runtimeProfileVersion: profile.spec.version,
      ttlSeconds: Number(restoreForm.ttlSeconds),
    };
    const key = adminMutationKey(
      `restore-workspace-snapshot:${selectedRestoreSnapshot.metadata.uid}`,
      body,
    );
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
          workspaceName: "",
          runtimeProfileVersionId: "",
          ttlSeconds: "3600",
          token: newIdentifierSuffix(),
        });
      },
      "accepted",
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
      "silent",
    );
  }

  function newStoragePolicy() {
    setSelectedStoragePolicyId("");
    setStoragePolicyAudit(Object.freeze([]));
    setStoragePolicyForm(storagePolicyFormFrom());
  }

  function saveStoragePolicy(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (client === null || selectedStoragePolicyReferenced || storagePolicyNameTaken) return;
    const policyId =
      selectedStoragePolicy?.metadata.uid ??
      identifierFromName(storagePolicyForm.policyName.trim(), "storage-policy");
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
    const key = adminMutationKey(`set-storage-policy:${policyId}`, body);
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
    const key = adminMutationKey("register-release", body);
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
    const profileId = identifierFromName(profileForm.profileName.trim(), "profile");
    const body = environmentProfileCreateRequestFrom(
      profileForm,
      nextProfileVersion(profiles, profileId),
    );
    const key = adminMutationKey("create-profile", body);
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
    if (body === null) return;
    void submitTargetRegistration(body);
  }

  async function submitTargetRegistration(body: DeploymentTargetRegisterRequest) {
    if (client === null) return;
    const { kubernetesCredential, ...visible } = body;
    // Pending mutation keys outlive the form, so they hold only a digest of the credential.
    const key = adminMutationKey("register-target", {
      ...visible,
      kubernetesCredential:
        kubernetesCredential === undefined
          ? undefined
          : await kubernetesCredentialDigest(kubernetesCredential),
    });
    await runOperation(
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

  const accountName = session.user.displayName || session.user.email;
  // Explain a disabled create action instead of leaving the operator guessing.
  const createPrerequisite: MessageKey | null =
    page === "profiles" &&
    (releases.length === 0 || storagePolicies.length === 0 || networkPolicies.length === 0)
      ? "profile.createPrerequisite"
      : page === "runtimeProfiles" && !runtimeProfileCreatable(targets, networkPolicies)
        ? "runtimeProfile.createPrerequisite"
        : null;

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
            <strong>{t("brand.cloudAgents")}</strong>
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
        {projects.length > 1 ? (
          <label className="sidebar-project">
            <span>{t("scope.project")}</span>
            <select
              data-scope="project"
              aria-label={t("scope.project")}
              value={project.metadata.uid}
              disabled={interactionDisabled}
              onChange={(event) => onScopeChange(tenant.id, event.target.value)}
            >
              {projects.map((item) => (
                <option key={item.metadata.uid} value={item.metadata.uid}>
                  {item.spec.displayName}
                </option>
              ))}
            </select>
          </label>
        ) : (
          <div
            className="sidebar-project"
            data-scope="project"
            data-scope-id={project.metadata.uid}
          >
            <span>{t("scope.project")}</span>
            <strong>{project.spec.displayName}</strong>
          </div>
        )}
        <ResourceNavigation
          page={page}
          disabled={busy !== null}
          onNavigate={navigate}
          onSearch={() => setCommandsOpen(true)}
          counts={{
            ...(loadedResources.has("targets") ? { targets: targets.length } : {}),
            ...(loadedResources.has("leases") ? { leases: leases.length } : {}),
            ...(loadedResources.has("workers") ? { workers: workers.length } : {}),
            ...(loadedResources.has("releases") ? { releases: releases.length } : {}),
            ...(loadedResources.has("profiles") ? { profiles: profiles.length } : {}),
            ...(loadedResources.has("runtimeProfiles")
              ? { runtimeProfiles: runtimeProfiles.length }
              : {}),
            ...(loadedResources.has("sandboxes") ? { sandboxes: sandboxes.length } : {}),
            ...(loadedResources.has("storagePolicies") ? { storage: storagePolicies.length } : {}),
            ...(loadedResources.has("networkPolicies") ? { network: networkPolicies.length } : {}),
            ...(loadedResources.has("mcpServers") && loadedResources.has("skillBundles")
              ? { capabilities: mcpServers.length + skillBundles.length }
              : {}),
            ...(loadedResources.has("quota") ? { quotas: leaseQuota === undefined ? 0 : 1 } : {}),
            ...(loadedResources.has("maintenanceOperations")
              ? { maintenance: maintenanceOperations.length }
              : {}),
          }}
        />
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
          <details ref={profileMenuRef} className="profile-menu">
            <summary className="profile-trigger">
              <span className="avatar" aria-hidden="true">
                {accountName.slice(0, 1).toUpperCase()}
              </span>
              <span className="profile-trigger-text">
                <strong>{accountName}</strong>
                <small>{tenant.name}</small>
              </span>
            </summary>
            <div className="dropdown-menu">
              <div className="dropdown-context">
                <strong>{accountName}</strong>
                <small>{session.user.email}</small>
              </div>
              {tenants.length > 1 ? (
                <label className="menu-field">
                  <span>{t("scope.tenant")}</span>
                  <select
                    data-scope="tenant"
                    aria-label={t("scope.tenant")}
                    value={tenant.id}
                    disabled={interactionDisabled}
                    onChange={(event) => {
                      profileMenuRef.current?.removeAttribute("open");
                      onScopeChange(event.target.value, "");
                    }}
                  >
                    {tenants.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                      </option>
                    ))}
                  </select>
                </label>
              ) : (
                <div className="menu-field" data-scope="tenant" data-scope-id={tenant.id}>
                  <span>{t("scope.tenant")}</span>
                  <strong>{tenant.name}</strong>
                </div>
              )}
              <label className="menu-field">
                <span>{t("account.language")}</span>
                <LocaleSelect />
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
              {tenantCanManage ? (
                <EmailPolicyForm client={sessionClient} tenantId={tenant.id} />
              ) : null}
              <AccountManagement
                session={session}
                sessionClient={sessionClient}
                tenant={tenant}
                onSessionEnded={onSessionExpired}
              />
              <ProviderManagement session={session} sessionClient={sessionClient} />
              {tenantCanManage ? (
                <ServiceAccountManagement client={client} tenant={tenant} projects={projects} />
              ) : null}
              {tenantCanManage ? (
                <InvitationManagement
                  client={client}
                  sessionClient={sessionClient}
                  tenant={tenant}
                  projects={projects}
                />
              ) : null}
              <button type="button" disabled={interactionDisabled} onClick={logout}>
                {t("auth.logout")}
              </button>
            </div>
          </details>
        </header>

        <main className="content">
          <div className="page-heading">
            <div>
              <h1>{t(pageEntry.title)}</h1>
              <p>{t(pageEntry.description)}</p>
              {createPrerequisite === null ? null : (
                <p className="page-prerequisite">{t(createPrerequisite)}</p>
              )}
            </div>
            <div className="heading-actions">
              <button
                className="button outline"
                type="button"
                onClick={refresh}
                disabled={interactionDisabled}
              >
                {t("action.refresh")}
              </button>
              {page === "releases" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={() => setRegisteringRelease(true)}
                  disabled={interactionDisabled}
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
                      releaseDigest: current.releaseDigest || releases[0]?.spec.releaseDigest || "",
                    }));
                    setCreatingProfile(true);
                  }}
                  disabled={interactionDisabled || createPrerequisite !== null}
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
                          (target) =>
                            runtimeProfileTargetAllowed(target, current.workloadTrust) &&
                            target.spec.observedPhase === "ready",
                        )?.metadata.uid ||
                        targets.find((target) =>
                          runtimeProfileTargetAllowed(target, current.workloadTrust),
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
                  disabled={interactionDisabled || createPrerequisite !== null}
                >
                  {t("action.createRuntimeProfile")}
                </button>
              ) : page === "remoteWorkers" ? (
                <button
                  className="button primary"
                  type="button"
                  disabled={interactionDisabled || creatingRemoteWorker}
                  onClick={() => setCreatingRemoteWorker(true)}
                >
                  {t("remoteWorkerEnrollment.create")}
                </button>
              ) : page === "network" ? (
                <button
                  className="button primary"
                  type="button"
                  disabled={interactionDisabled}
                  onClick={() => setNetworkEditorEpoch((current) => current + 1)}
                >
                  {t("action.newNetworkPolicy")}
                </button>
              ) : page === "storage" ? (
                <button
                  className="button primary"
                  type="button"
                  onClick={newStoragePolicy}
                  disabled={interactionDisabled}
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
                  disabled={interactionDisabled}
                >
                  {t("action.registerTarget")}
                </button>
              ) : null}
            </div>
          </div>

          {feedback}
          {pageLoading ? <p role="status">{t("page.loading")}</p> : null}
          {Object.entries(pageErrors).map(([resource, cause]) => {
            const failure = adminFailure(cause);
            return (
              <p className="danger-text" role="alert" key={resource}>
                {t("page.resourceFailed", {
                  resource: t(`resource.${resource as keyof AdminWorkspaceData}`),
                })}{" "}
                {t(failure.key)}
                {failure.code ? ` · ${failure.code}` : ""}
              </p>
            );
          })}
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
              </div>
              <div className="panel target-list-panel">
                <SandboxTable
                  sandboxes={visibleSandboxes}
                  selectedSandboxId={selectedSandboxId}
                  onSelect={selectSandbox}
                />
              </div>
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
            <RemoteWorkerEnrollmentPanel
              client={client}
              connection={connection}
              creating={creatingRemoteWorker}
              onCreatingChange={setCreatingRemoteWorker}
            />
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
              </div>
              <div className="panel target-list-panel">
                <div className="panel-heading">
                  <div>
                    <h2>{t("cluster.overviewTitle")}</h2>
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
                  </div>
                </div>
                <WorkerTable
                  workers={visibleWorkers}
                  filtered={workerStatusFilter !== "" || query.trim() !== ""}
                  selectedWorkerId={selectedWorkerId}
                  onSelect={selectWorker}
                />
              </div>
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
              </div>
              <div className="panel target-list-panel">
                <RuntimeProfileTable
                  profiles={visibleRuntimeProfiles}
                  selectedProfileVersionId={selectedRuntimeProfileVersionId}
                  onSelect={selectRuntimeProfile}
                />
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
                capabilityRuntime.sessions,
                capabilityRuntime.executions,
              )}
              runtime={capabilityRuntime}
              client={client}
              connection={connection}
              query={query}
              onQuery={setQuery}
              onNextSessions={() => pageManagedAgentRuntime("capability", "sessions")}
              onNextExecutions={() => pageManagedAgentRuntime("capability", "executions")}
              onSelectSession={(sessionId) =>
                pageManagedAgentRuntime("capability", "select", sessionId)
              }
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
                    <h2>
                      {selectedStoragePolicy === undefined
                        ? t("storagePolicy.createTitle")
                        : t("storagePolicy.editTitle", {
                            name: selectedStoragePolicy.metadata.name,
                          })}
                    </h2>
                  </div>
                </div>
                <form className="resource-form" onSubmit={saveStoragePolicy}>
                  <NameField
                    label={t("storagePolicy.name")}
                    value={storagePolicyForm.policyName}
                    takenMessage={storagePolicyNameTaken ? t("form.nameTaken") : ""}
                    placeholder="storage-standard"
                    disabled={selectedStoragePolicyReferenced}
                    onChange={(policyName) =>
                      setStoragePolicyForm((current) => ({ ...current, policyName }))
                    }
                  />
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
                  <AdvancedFields>
                    <div className="form-row">
                      <label>
                        <span>{t("storagePolicy.snapshotBackendRef")}</span>
                        <input
                          maxLength={128}
                          spellCheck={false}
                          list="storage-backend-refs"
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
                          list="storage-backend-refs"
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
                    <Suggestions
                      id="storage-backend-refs"
                      values={storagePolicies.flatMap(({ spec }) => [
                        spec.snapshotBackendRef,
                        spec.artifactBackendRef,
                      ])}
                    />
                  </AdvancedFields>
                  {selectedStoragePolicyReferenced ? (
                    <p className="cluster-boundary">{t("storagePolicy.referencedBoundary")}</p>
                  ) : null}
                  <button
                    className="button primary"
                    type="submit"
                    disabled={
                      busy !== null || selectedStoragePolicyReferenced || storagePolicyNameTaken
                    }
                  >
                    {t("storagePolicy.save")}
                  </button>
                </form>
              </section>

              {selectedStoragePolicy === undefined ? null : (
                <section className="activity-block" aria-labelledby="storage-policy-audit-title">
                  <div className="activity-heading">
                    <h2 id="storage-policy-audit-title">{t("storagePolicy.audit")}</h2>
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
              )}

              <WorkspaceSnapshotPanel
                snapshots={workspaceSnapshots}
                sandboxes={sandboxes}
                restoreRuntimeProfiles={restoreRuntimeProfiles}
                selectedRestoreSnapshot={selectedRestoreSnapshot}
                snapshotForm={snapshotForm}
                restoreForm={restoreForm}
                busy={busy !== null}
                onCreate={createWorkspaceSnapshot}
                onRestore={restoreWorkspaceSnapshot}
                onSnapshotFormChange={setSnapshotForm}
                onRestoreFormChange={setRestoreForm}
                onCleanup={(snapshot) => {
                  operationTriggerRef.current =
                    document.activeElement instanceof HTMLElement ? document.activeElement : null;
                  setSnapshotCleanup(snapshot);
                }}
              />
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
              </section>

              <section className="panel overview-panel">
                <div className="panel-heading">
                  <div>
                    <h2>{t("quota.formTitle")}</h2>
                  </div>
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
              runtime={managedAgentRuntime}
              client={client}
              connection={connection}
              runtimeProfiles={runtimeProfiles}
              disabled={busy !== null}
              onTransition={setSandboxLifecycleTransition}
              onRevokeGrant={setSandboxGrantRevoke}
              onCorrectUsage={correctSandboxUsage}
              onReconcileSideEffect={reconcileManagedAgentSideEffect}
              onNextSessions={() => pageManagedAgentRuntime("sandbox", "sessions")}
              onNextExecutions={() => pageManagedAgentRuntime("sandbox", "executions")}
              onSelectSession={(sessionId) =>
                pageManagedAgentRuntime("sandbox", "select", sessionId)
              }
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
          nameTaken={
            releaseForm.releaseName.trim() !== "" &&
            releases.some(
              ({ metadata }) =>
                metadata.uid ===
                identifierFromName(releaseForm.releaseName.trim(), "worker-release"),
            )
          }
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
          targets={targets}
          credentialRefSuggestions={profiles.map(({ spec }) => spec.providerCredentialRef)}
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
          nameTaken={
            targetForm.targetName.trim() !== "" &&
            targets.some(
              ({ metadata }) =>
                metadata.uid ===
                identifierFromName(targetForm.targetName.trim(), targetForm.targetKind),
            )
          }
          credentialRefSuggestions={[
            ...targets.map(({ spec }) => spec.credentialRef),
            ...profiles.map(({ spec }) => spec.providerCredentialRef),
          ]}
          onDraftChange={setTargetForm}
          onClose={() => {
            setTargetForm(withoutKubeconfig);
            setRegistering(false);
          }}
          onSubmit={registerTarget}
        />
      ) : null}
    </div>
  );
}

type AdminSessionState = "restoring" | "login" | "ready";

export function App() {
  const { t } = useI18n();
  const [cliAuthorization] = useState(() => window.location.hash === "#cli");
  const [anonymousSessionClient] = useState(() => createSessionHTTPClient(window.location.origin));
  const [sessionClient, setSessionClient] = useState<BrowserSessionClient | null>(null);
  const [client, setClient] = useState<Client | null>(null);
  const [session, setSession] = useState<BrowserSession | null>(null);
  const [tenants, setTenants] = useState<readonly BrowserTenant[]>([]);
  const [projects, setProjects] = useState<readonly Project[]>([]);
  const [connection, setConnection] = useState(() =>
    readSavedAdminConnection(window.sessionStorage),
  );
  const [state, setState] = useState<AdminSessionState>("restoring");
  const [error, setError] = useState("");
  const requestRef = useRef<AbortController | null>(null);

  function clearSession(message = "") {
    requestRef.current?.abort();
    requestRef.current = null;
    setSession(null);
    setSessionClient(null);
    setClient(null);
    setTenants([]);
    setProjects([]);
    setError(message);
    setState("login");
  }

  async function activate(nextSession: BrowserSession, signal: AbortSignal) {
    if (nextSession.application !== "admin") throw new Error("session_wrong_console");
    const authenticatedSessionClient = createSessionHTTPClient(
      window.location.origin,
      nextSession.csrfToken,
    );
    const nextClient = createBrowserHTTPClient(window.location.origin, nextSession.csrfToken);
    const nextTenants = await listAllBrowserTenants(
      authenticatedSessionClient,
      nextSession,
      signal,
    );
    const tenantId = nextTenants.some(({ id }) => id === connection.tenantId)
      ? connection.tenantId
      : (nextTenants[0]?.id ?? "");
    const nextProjects =
      tenantId === "" ? [] : await listAllAdminProjects(nextClient, tenantId, signal);
    const projectId = nextProjects.some(({ metadata }) => metadata.uid === connection.projectId)
      ? connection.projectId
      : (nextProjects[0]?.metadata.uid ?? "");
    const nextConnection = { tenantId, projectId };
    setSession(nextSession);
    setSessionClient(authenticatedSessionClient);
    setClient(nextClient);
    setTenants(nextTenants);
    setProjects(nextProjects);
    setConnection(nextConnection);
    writeSavedAdminConnection(window.sessionStorage, nextConnection);
    setError("");
    setState("ready");
  }

  useEffect(() => {
    const controller = new AbortController();
    requestRef.current = controller;
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    void anonymousSessionClient
      .getBrowserSession(signal)
      .then((restored) => activate(restored, signal))
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        if (cause instanceof ClientError && cause.status === 401) clearSession();
        else clearSession(t(sessionMessage(cause)));
      });
    return () => controller.abort();
  }, [anonymousSessionClient]);

  async function login(email: string, password: string) {
    if (state === "restoring") return;
    const controller = new AbortController();
    requestRef.current?.abort();
    requestRef.current = controller;
    setState("restoring");
    setError("");
    const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]);
    try {
      await activate(
        await anonymousSessionClient.passwordLogin({ email, password }, signal),
        signal,
      );
    } catch (cause) {
      if (!controller.signal.aborted) clearSession(t(loginMessage(cause)));
    }
  }

  async function changeScope(tenantId: string, projectId: string) {
    if (client === null) return;
    if (tenantId === connection.tenantId && projectId !== "") {
      const next = { tenantId, projectId };
      setConnection(next);
      writeSavedAdminConnection(window.sessionStorage, next);
      return;
    }
    const controller = new AbortController();
    requestRef.current?.abort();
    requestRef.current = controller;
    setState("restoring");
    try {
      const nextProjects = await listAllAdminProjects(
        client,
        tenantId,
        AbortSignal.any([controller.signal, AbortSignal.timeout(15_000)]),
      );
      const next = { tenantId, projectId: nextProjects[0]?.metadata.uid ?? "" };
      setProjects(nextProjects);
      setConnection(next);
      writeSavedAdminConnection(window.sessionStorage, next);
      setState("ready");
    } catch (cause) {
      if (!controller.signal.aborted) {
        setError(t(sessionMessage(cause)));
        setState("ready");
      }
    }
  }

  if (state === "login")
    return (
      <LoginView client={anonymousSessionClient} busy={false} error={error} onSubmit={login} />
    );
  if (state === "restoring")
    return (
      <main className="connect-view" aria-live="polite">
        <section className="connect-card">
          <div className="eyebrow">{t("auth.secureSession")}</div>
          <h1>{t("auth.loadingAdmin")}</h1>
          <p className="lede">{t("auth.loadingAdminDescription")}</p>
        </section>
      </main>
    );
  if (session === null || sessionClient === null || client === null) return null;
  if (cliAuthorization)
    return (
      <CLIAuthorization
        client={sessionClient}
        session={session}
        labels={{
          title: t("cli.title"),
          description: t("cli.description"),
          loading: t("cli.loading"),
          expired: t("cli.expired"),
          failed: t("cli.failed"),
          approve: t("cli.approve"),
          cancel: t("cli.cancel"),
          back: t("cli.back"),
        }}
      />
    );
  const tenant = tenants.find(({ id }) => id === connection.tenantId);
  const project = projects.find(({ metadata }) => metadata.uid === connection.projectId);
  if (tenant === undefined || project === undefined)
    return (
      <main className="connect-view">
        <section className="connect-card" aria-labelledby="admin-empty-title">
          <div className="eyebrow">{t("auth.adminScope")}</div>
          <h1 id="admin-empty-title">
            {tenant === undefined ? t("auth.noTenants") : t("auth.noProjects")}
          </h1>
          <p className="lede">
            {tenant === undefined ? t("auth.noTenantsHelp") : t("auth.noProjectsHelp")}
          </p>
          {tenants.length > 0 ? (
            <label>
              <span>{t("scope.tenant")}</span>
              <select
                value={connection.tenantId}
                onChange={(event) => void changeScope(event.target.value, "")}
              >
                {tenants.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          {error ? (
            <div className="error-banner" role="alert">
              {error}
            </div>
          ) : null}
          <AccountManagement
            session={session}
            sessionClient={sessionClient}
            {...(tenant === undefined ? {} : { tenant })}
            onSessionEnded={() => clearSession(t("auth.sessionEnded"))}
          />
          <ProviderManagement session={session} sessionClient={sessionClient} />
          {tenant === undefined ? null : (
            <>
              <EmailPolicyForm client={sessionClient} tenantId={tenant.id} />
              <ServiceAccountManagement client={client} tenant={tenant} projects={projects} />
              <InvitationManagement
                client={client}
                sessionClient={sessionClient}
                tenant={tenant}
                projects={projects}
              />
            </>
          )}
          <button
            className="button outline"
            type="button"
            onClick={() => {
              void sessionClient.logoutBrowserSession().finally(() => clearSession());
            }}
          >
            {t("auth.logout")}
          </button>
        </section>
      </main>
    );
  return (
    <Dashboard
      key={`${tenant.id}/${project.metadata.uid}`}
      client={client}
      connection={connection}
      projects={projects}
      session={session}
      sessionClient={sessionClient}
      tenant={tenant}
      tenants={tenants}
      project={project}
      onScopeChange={(tenantId, projectId) => void changeScope(tenantId, projectId)}
      onSessionExpired={() => clearSession(t("auth.sessionEnded"))}
    />
  );
}
