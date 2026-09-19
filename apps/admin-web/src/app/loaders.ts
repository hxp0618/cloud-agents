import type {
  AdminAuditEvent,
  DeploymentTarget,
  EnvironmentLease,
  EnvironmentProfile,
  MaintenanceOperation,
  McpServer,
  NetworkPolicy,
  ProjectLeaseQuota,
  RuntimeProfile,
  SkillBundle,
  StoragePolicy,
  Worker,
  WorkerRelease,
  WorkspaceSnapshot,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import {
  listAdminLeases,
  listAdminMaintenanceOperations,
  listAdminMcpServers,
  listAdminNetworkPolicies,
  listAdminProfileAuditEvents,
  listAdminProjectLeaseQuotaAuditEvents,
  listAdminProfiles,
  listAdminReleases,
  listAdminRuntimeProfiles,
  listAdminSandboxes,
  listAdminSkillBundles,
  listAdminStoragePolicies,
  listAdminTargets,
  listAdminWorkers,
  listAdminWorkspaceSnapshots,
  listAdminTargetAuditEvents,
  listAdminTargetOperations,
  loadAdminProjectLeaseQuota,
  replaceLease,
  replaceProfile,
  replaceTarget,
  selectAdminResourceId,
  type AdminClient,
  type SavedAdminConnection,
} from "../admin";
import { newRequestId } from "../admin";

export type AdminWorkspaceData = Readonly<{
  targets: Readonly<{ targets: readonly DeploymentTarget[]; selectedTargetId: string }>;
  leases: Readonly<{ leases: readonly EnvironmentLease[]; selectedLeaseId: string }>;
  workers: readonly Worker[];
  releases: readonly WorkerRelease[];
  profiles: Readonly<{
    profiles: readonly EnvironmentProfile[];
    selectedProfileVersionId: string;
  }>;
  runtimeProfiles: readonly RuntimeProfile[];
  sandboxes: Awaited<ReturnType<typeof listAdminSandboxes>>;
  workspaceSnapshots: readonly WorkspaceSnapshot[];
  storagePolicies: readonly StoragePolicy[];
  networkPolicies: readonly NetworkPolicy[];
  mcpServers: readonly McpServer[];
  skillBundles: readonly SkillBundle[];
  quota: ProjectLeaseQuota | undefined;
  quotaAudit: readonly AdminAuditEvent[];
  maintenanceOperations: readonly MaintenanceOperation[];
}>;

export async function loadTargetAuthority(
  client: AdminClient,
  connection: SavedAdminConnection,
  preferredTargetId: string,
  signal: AbortSignal,
): Promise<Readonly<{ targets: readonly DeploymentTarget[]; selectedTargetId: string }>> {
  let targets = await listAdminTargets(client, connection.tenantId, connection.projectId, signal);
  const selectedTargetId = selectAdminResourceId(targets, preferredTargetId);
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

export async function loadLeaseAuthority(
  client: AdminClient,
  connection: SavedAdminConnection,
  preferredLeaseId: string,
  signal: AbortSignal,
): Promise<Readonly<{ leases: readonly EnvironmentLease[]; selectedLeaseId: string }>> {
  let leases = await listAdminLeases(client, connection.tenantId, connection.projectId, signal);
  const selectedLeaseId = selectAdminResourceId(leases, preferredLeaseId);
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

export async function loadProfileAuthority(
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
  const selectedProfileVersionId = selectAdminResourceId(profiles, preferredProfileVersionId);
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

export async function loadTargetActivity(
  client: AdminClient,
  connection: SavedAdminConnection,
  targetId: string,
  signal: AbortSignal,
): Promise<
  Readonly<{ operations: readonly MaintenanceOperation[]; audit: readonly AdminAuditEvent[] }>
> {
  const [operations, audit] = await Promise.all([
    listAdminTargetOperations(client, connection.tenantId, connection.projectId, targetId, signal),
    listAdminTargetAuditEvents(client, connection.tenantId, connection.projectId, targetId, signal),
  ]);
  return Object.freeze({ operations, audit });
}

export function loadProfileAudit(
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

export async function loadAdminWorkspaceData(
  client: AdminClient,
  connection: SavedAdminConnection,
  selectedTargetId: string,
  selectedLeaseId: string,
  selectedProfileVersionId: string,
  signal: AbortSignal,
): Promise<AdminWorkspaceData> {
  const [
    targets,
    leases,
    workers,
    releases,
    profiles,
    runtimeProfiles,
    sandboxes,
    workspaceSnapshots,
    storagePolicies,
    networkPolicies,
    mcpServers,
    skillBundles,
    quota,
    quotaAudit,
    maintenanceOperations,
  ] = await Promise.all([
    loadTargetAuthority(client, connection, selectedTargetId, signal),
    loadLeaseAuthority(client, connection, selectedLeaseId, signal),
    listAdminWorkers(client, connection.tenantId, connection.projectId, signal),
    listAdminReleases(client, connection.tenantId, connection.projectId, signal),
    loadProfileAuthority(client, connection, selectedProfileVersionId, signal),
    listAdminRuntimeProfiles(client, connection.tenantId, connection.projectId, signal),
    listAdminSandboxes(client, connection.tenantId, connection.projectId, signal),
    listAdminWorkspaceSnapshots(client, connection.tenantId, connection.projectId, signal),
    listAdminStoragePolicies(client, connection.tenantId, connection.projectId, signal),
    listAdminNetworkPolicies(client, connection.tenantId, connection.projectId, signal),
    listAdminMcpServers(client, connection.tenantId, connection.projectId, signal),
    listAdminSkillBundles(client, connection.tenantId, connection.projectId, signal),
    loadAdminProjectLeaseQuota(client, connection.tenantId, connection.projectId, signal),
    listAdminProjectLeaseQuotaAuditEvents(
      client,
      connection.tenantId,
      connection.projectId,
      signal,
    ),
    listAdminMaintenanceOperations(client, connection.tenantId, connection.projectId, signal),
  ]);
  return Object.freeze({
    targets,
    leases,
    workers,
    releases,
    profiles,
    runtimeProfiles,
    sandboxes,
    workspaceSnapshots,
    storagePolicies,
    networkPolicies,
    mcpServers,
    skillBundles,
    quota,
    quotaAudit,
    maintenanceOperations,
  });
}
