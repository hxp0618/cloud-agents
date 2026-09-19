import type {
  DeploymentTarget,
  EnvironmentLease,
  EnvironmentProfile,
  NetworkPolicy,
  RemoteWorkerEnrollment,
  RuntimeProfile,
  StoragePolicy,
  WorkerRelease,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

type AdminResource = Readonly<{
  metadata: Readonly<{
    uid: string;
    name: string;
  }>;
}>;

type AdminProfileResource = AdminResource &
  Readonly<{
    spec: Readonly<{
      version: number;
    }>;
  }>;

export function upsertAdminResource<T extends AdminResource>(
  resources: readonly T[],
  resource: T,
  compare: (left: T, right: T) => number = (left, right) =>
    left.metadata.name.localeCompare(right.metadata.name),
): readonly T[] {
  return Object.freeze(
    [
      ...resources.filter(({ metadata }) => metadata.uid !== resource.metadata.uid),
      resource,
    ].toSorted(compare),
  );
}

const compareAdminProfiles = <T extends AdminProfileResource>(left: T, right: T): number =>
  left.metadata.name.localeCompare(right.metadata.name) || right.spec.version - left.spec.version;

export function replaceRemoteWorkerEnrollment(
  enrollments: readonly RemoteWorkerEnrollment[],
  enrollment: RemoteWorkerEnrollment,
): readonly RemoteWorkerEnrollment[] {
  return upsertAdminResource(enrollments, enrollment);
}

export function replaceRelease(
  releases: readonly WorkerRelease[],
  release: WorkerRelease,
): readonly WorkerRelease[] {
  return upsertAdminResource(releases, release);
}

export function replaceRuntimeProfile(
  profiles: readonly RuntimeProfile[],
  profile: RuntimeProfile,
): readonly RuntimeProfile[] {
  return upsertAdminResource(profiles, profile, compareAdminProfiles);
}

export function replaceStoragePolicy(
  policies: readonly StoragePolicy[],
  policy: StoragePolicy,
): readonly StoragePolicy[] {
  return upsertAdminResource(policies, policy);
}

export function replaceNetworkPolicy(
  policies: readonly NetworkPolicy[],
  policy: NetworkPolicy,
): readonly NetworkPolicy[] {
  return upsertAdminResource(policies, policy);
}

export function replaceTarget(
  targets: readonly DeploymentTarget[],
  target: DeploymentTarget,
): readonly DeploymentTarget[] {
  return upsertAdminResource(targets, target);
}

export function replaceLease(
  leases: readonly EnvironmentLease[],
  lease: EnvironmentLease,
): readonly EnvironmentLease[] {
  return upsertAdminResource(leases, lease);
}

export function replaceProfile(
  profiles: readonly EnvironmentProfile[],
  profile: EnvironmentProfile,
): readonly EnvironmentProfile[] {
  return upsertAdminResource(profiles, profile, compareAdminProfiles);
}

export function selectAdminResourceId(
  resources: readonly Readonly<{ metadata: Readonly<{ uid: string }> }>[],
  preferredId: string | undefined,
): string {
  if (preferredId !== undefined && resources.some(({ metadata }) => metadata.uid === preferredId))
    return preferredId;
  return resources[0]?.metadata.uid ?? "";
}
