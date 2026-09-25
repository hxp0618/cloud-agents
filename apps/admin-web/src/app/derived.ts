import type {
  DeploymentTarget,
  DeploymentTargetCleanupPreview,
  DeploymentTargetSchedulingPreview,
  EnvironmentLease,
  EnvironmentLeaseUpgradePreview,
  EnvironmentProfile,
  MaintenanceOperation,
  RuntimeProfile,
  AdminSandboxSession,
  StoragePolicy,
  Worker,
  WorkerRelease,
  WorkspaceSnapshot,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  filterAdminLeases,
  filterAdminMaintenanceOperations,
  filterAdminTargets,
  filterAdminWorkers,
  leaseNeedsAttention,
  matchesAdminSearch,
  summarizeClusterHosts,
  type WorkerStatusFilter,
} from "../admin";
import { navigationPages, type Page } from "../navigation";
import { runtimeProfileTargetLabel } from "./presentation";

type TargetPhase = DeploymentTarget["spec"]["observedPhase"];
type LeasePhase = EnvironmentLease["spec"]["observedPhase"];

type AdminDerivedInput = Readonly<{
  resources: Readonly<{
    targets: readonly DeploymentTarget[];
    leases: readonly EnvironmentLease[];
    workers: readonly Worker[];
    releases: readonly WorkerRelease[];
    profiles: readonly EnvironmentProfile[];
    runtimeProfiles: readonly RuntimeProfile[];
    sandboxes: readonly AdminSandboxSession[];
    workspaceSnapshots: readonly WorkspaceSnapshot[];
    storagePolicies: readonly StoragePolicy[];
    maintenanceOperations: readonly MaintenanceOperation[];
  }>;
  selection: Readonly<{
    page: Page;
    targetId: string;
    leaseId: string;
    upgradeReleaseDigest: string;
    workerId: string;
    profileVersionId: string;
    runtimeProfileVersionId: string;
    sandboxId: string;
    storagePolicyId: string;
    restoreSnapshotId: string;
    maintenanceOperationId: string;
  }>;
  previews: Readonly<{
    cleanup: DeploymentTargetCleanupPreview | null;
    scheduling: DeploymentTargetSchedulingPreview | null;
    leaseRelease: EnvironmentLeaseUpgradePreview | null;
  }>;
  filters: Readonly<{
    query: string;
    targetKinds: readonly DeploymentTarget["spec"]["targetKind"][];
    targetPhases: readonly TargetPhase[];
    leaseAttentionOnly: boolean;
    leasePhase: LeasePhase | "";
    leaseCleanupBlockedOnly: boolean;
    workerStatus: WorkerStatusFilter;
    failedOperationsOnly: boolean;
  }>;
}>;

export function deriveAdminView(input: AdminDerivedInput) {
  const { resources, selection, previews, filters } = input;
  const selectedTarget = resources.targets.find(
    ({ metadata }) => metadata.uid === selection.targetId,
  );
  const selectedCleanupPreview =
    selectedTarget !== undefined &&
    previews.cleanup?.metadata.uid === selectedTarget.metadata.uid &&
    previews.cleanup.metadata.resourceVersion === selectedTarget.metadata.resourceVersion
      ? previews.cleanup
      : null;
  const selectedSchedulingPreview =
    selectedTarget !== undefined &&
    previews.scheduling?.metadata.uid === selectedTarget.metadata.uid &&
    previews.scheduling.metadata.resourceVersion === selectedTarget.metadata.resourceVersion
      ? previews.scheduling
      : null;
  const selectedLease = resources.leases.find(({ metadata }) => metadata.uid === selection.leaseId);
  const selectedLeaseTarget = resources.targets.find(
    ({ metadata }) => metadata.uid === selectedLease?.spec.targetId,
  );
  const upgradeReleaseDigest =
    resources.releases.find(
      ({ spec }) =>
        spec.releaseDigest === selection.upgradeReleaseDigest &&
        spec.releaseDigest !== selectedLease?.spec.releaseDigest,
    )?.spec.releaseDigest ??
    resources.releases.find(({ spec }) => spec.releaseDigest !== selectedLease?.spec.releaseDigest)
      ?.spec.releaseDigest ??
    "";
  const selectedLeaseReleasePreview =
    selectedLease !== undefined &&
    previews.leaseRelease?.metadata.uid === selectedLease.metadata.uid &&
    previews.leaseRelease.metadata.resourceVersion === selectedLease.metadata.resourceVersion
      ? previews.leaseRelease
      : null;
  const selectedWorker = resources.workers.find(
    ({ metadata }) => metadata.uid === selection.workerId,
  );
  const selectedProfile = resources.profiles.find(
    ({ metadata }) => metadata.uid === selection.profileVersionId,
  );
  const selectedRuntimeProfile = resources.runtimeProfiles.find(
    ({ metadata }) => metadata.uid === selection.runtimeProfileVersionId,
  );
  const selectedSandbox = resources.sandboxes.find(
    ({ metadata }) => metadata.uid === selection.sandboxId,
  );
  const selectedStoragePolicy = resources.storagePolicies.find(
    ({ metadata }) => metadata.uid === selection.storagePolicyId,
  );
  const selectedRestoreSnapshot = resources.workspaceSnapshots.find(
    ({ metadata }) => metadata.uid === selection.restoreSnapshotId,
  );
  const restoreSourceTargetId = selectedRestoreSnapshot?.spec.sourceTargetId;
  const restoreRuntimeProfiles = resources.runtimeProfiles.filter(
    ({ spec }) =>
      spec.status === "published" &&
      "targetId" in spec &&
      (selectedRestoreSnapshot?.spec.backend === "portable-tar-v1" ||
        spec.targetId === restoreSourceTargetId),
  );
  const selectedStoragePolicyReferenced = resources.profiles.some(
    ({ spec }) => spec.storagePolicyRef === selection.storagePolicyId,
  );
  const selectedMaintenanceOperation = resources.maintenanceOperations.find(
    ({ operationId }) => operationId === selection.maintenanceOperationId,
  );
  const readyCount = resources.targets.filter(({ spec }) => spec.observedPhase === "ready").length;
  const probingCount = resources.targets.filter(
    ({ spec }) => spec.observedPhase === "probing",
  ).length;
  const unprobedCount = resources.targets.filter(
    ({ spec }) => spec.observedPhase === "unprobed",
  ).length;
  const unavailableCount = resources.targets.filter(
    ({ spec }) => spec.observedPhase === "unavailable",
  ).length;
  const attentionCount = resources.targets.length - readyCount;
  const readyLeaseCount = resources.leases.filter(
    ({ spec }) => spec.observedPhase === "ready",
  ).length;
  const leaseAttentionCount = resources.leases.filter(leaseNeedsAttention).length;
  const onlineWorkerCount = filterAdminWorkers(resources.workers, "", "online").length;
  const normalizedQuery = filters.query.trim().toLocaleLowerCase();
  const visibleTargets = filterAdminTargets(
    resources.targets,
    filters.query,
    filters.targetKinds,
    filters.targetPhases,
  );
  const targetsFiltered =
    filters.query.trim() !== "" ||
    filters.targetKinds.length > 0 ||
    filters.targetPhases.length > 0;
  const visibleLeases = filterAdminLeases(
    resources.leases,
    filters.query,
    filters.leaseAttentionOnly,
    filters.leasePhase,
    filters.leaseCleanupBlockedOnly,
  );
  const visibleWorkers = filterAdminWorkers(resources.workers, filters.query, filters.workerStatus);
  const visibleReleases =
    normalizedQuery === ""
      ? resources.releases
      : resources.releases.filter(({ metadata, spec }) =>
          matchesAdminSearch(normalizedQuery, [
            metadata.uid,
            metadata.name,
            spec.imageRepository,
            spec.releaseDigest,
            spec.platformVersion,
            spec.runtimeVersion,
            spec.codexVersion,
            spec.claudeCodeVersion,
            ...spec.architectures,
          ]),
        );
  const clusterHosts = summarizeClusterHosts(resources.targets, resources.workers);
  const visibleClusterHosts =
    normalizedQuery === ""
      ? clusterHosts
      : clusterHosts.filter(
          ({ target }) =>
            matchesAdminSearch(normalizedQuery, [
              target.metadata.uid,
              target.metadata.name,
              target.spec.targetKind,
              target.spec.observedPhase,
              target.spec.schedulingState,
              target.spec.apiVersion,
              target.spec.engineVersion,
              target.spec.os,
              target.spec.architecture,
            ]) || visibleWorkers.some(({ spec }) => spec.targetId === target.metadata.uid),
        );
  const visibleProfiles =
    normalizedQuery === ""
      ? resources.profiles
      : resources.profiles.filter(({ metadata, spec }) =>
          matchesAdminSearch(normalizedQuery, [
            metadata.uid,
            metadata.name,
            spec.profileId,
            String(spec.version),
            spec.status,
            ...spec.providerKinds,
          ]),
        );
  const visibleRuntimeProfiles =
    normalizedQuery === ""
      ? resources.runtimeProfiles
      : resources.runtimeProfiles.filter((profile) => {
          const { metadata, spec } = profile;
          return matchesAdminSearch(normalizedQuery, [
            metadata.uid,
            metadata.name,
            spec.profileId,
            String(spec.version),
            spec.status,
            runtimeProfileTargetLabel(profile),
            spec.imageUri,
          ]);
        });
  const visibleSandboxes =
    normalizedQuery === ""
      ? resources.sandboxes
      : resources.sandboxes.filter(({ metadata, spec }) =>
          matchesAdminSearch(normalizedQuery, [
            metadata.uid,
            spec.workspaceId,
            spec.workspaceName,
            spec.volumeId,
            spec.physicalVolumeId ?? "",
            spec.runtimeProfileId,
            spec.targetId,
            spec.operationId,
            spec.observedState,
          ]),
        );
  const visibleStoragePolicies =
    normalizedQuery === ""
      ? resources.storagePolicies
      : resources.storagePolicies.filter(({ metadata, spec }) =>
          matchesAdminSearch(normalizedQuery, [
            metadata.uid,
            metadata.name,
            spec.userSummary,
            spec.workspaceType,
          ]),
        );
  const visibleMaintenanceOperations = filterAdminMaintenanceOperations(
    resources.maintenanceOperations,
    filters.query,
    filters.failedOperationsOnly,
  );
  const failedMaintenanceOperations = filterAdminMaintenanceOperations(
    resources.maintenanceOperations,
    "",
    true,
  );
  const pageEntry = navigationPages.find(({ id }) => id === selection.page)!;

  return {
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
  } as const;
}
