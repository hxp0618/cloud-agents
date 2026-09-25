import type {
  DeploymentTarget,
  EnvironmentLease,
  MaintenanceOperation,
  Worker,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

export type ClusterHostSummary = Readonly<{
  target: DeploymentTarget;
  workerCount: number;
  readyWorkerCount: number;
  latestHealthAt: string | undefined;
}>;

export function matchesAdminSearch(query: string, values: readonly string[]): boolean {
  const search = query.trim().toLocaleLowerCase();
  return values.some((value) => value.toLocaleLowerCase().includes(search));
}

export function filterAdminTargets(
  targets: readonly DeploymentTarget[],
  query: string,
  kinds: readonly DeploymentTarget["spec"]["targetKind"][],
  phases: readonly DeploymentTarget["spec"]["observedPhase"][],
): readonly DeploymentTarget[] {
  return targets.filter(
    ({ metadata, spec }) =>
      (kinds.length === 0 || kinds.includes(spec.targetKind)) &&
      (phases.length === 0 || phases.includes(spec.observedPhase)) &&
      matchesAdminSearch(query, [
        metadata.uid,
        metadata.name,
        spec.targetKind,
        spec.observedPhase,
        spec.schedulingState,
        spec.engineVersion,
        spec.apiVersion,
        spec.os,
        spec.architecture,
      ]),
  );
}

export function leaseNeedsAttention({ spec }: EnvironmentLease): boolean {
  return spec.observedPhase === "failed" || spec.cleanupPhase === "blocked";
}

export type WorkerStatusFilter =
  | ""
  | "online"
  | "expired"
  | "unavailable"
  | "not-observed"
  | "failed";

export function filterAdminWorkers(
  workers: readonly Worker[],
  query: string,
  status: WorkerStatusFilter = "",
): readonly Worker[] {
  return workers.filter(
    ({ metadata, spec }) =>
      (status === "" ||
        (status === "failed"
          ? spec.state === "failed"
          : status === "not-observed"
            ? spec.health === undefined
            : spec.health?.state === status)) &&
      matchesAdminSearch(query, [
        metadata.uid,
        metadata.name,
        spec.leaseId,
        spec.targetId,
        spec.targetKind,
        spec.state,
        spec.releaseDigest,
        spec.health?.state ?? "not-observed",
      ]),
  );
}

export function filterAdminMaintenanceOperations(
  operations: readonly MaintenanceOperation[],
  query: string,
  failedOnly: boolean,
): readonly MaintenanceOperation[] {
  return operations
    .filter(
      (operation) =>
        (!failedOnly || operation.state === "failed") &&
        matchesAdminSearch(query, [
          operation.operationId,
          operation.action,
          operation.resourceKind,
          operation.resourceId,
          operation.state,
          operation.currentStep,
          operation.requestId,
          operation.stableErrorCode ?? "",
        ]),
    )
    .sort(
      (left, right) =>
        Date.parse(right.updatedAt) - Date.parse(left.updatedAt) ||
        (left.operationId < right.operationId ? -1 : left.operationId > right.operationId ? 1 : 0),
    );
}

export function filterAdminLeases(
  leases: readonly EnvironmentLease[],
  query: string,
  attentionOnly: boolean,
  observedPhase: EnvironmentLease["spec"]["observedPhase"] | "" = "",
  cleanupBlockedOnly = false,
): readonly EnvironmentLease[] {
  return leases.filter(
    (lease) =>
      (!attentionOnly || leaseNeedsAttention(lease)) &&
      (observedPhase === "" || lease.spec.observedPhase === observedPhase) &&
      (!cleanupBlockedOnly || lease.spec.cleanupPhase === "blocked") &&
      matchesAdminSearch(query, [
        lease.metadata.uid,
        lease.metadata.name,
        lease.spec.environmentId,
        lease.spec.observedPhase,
        lease.spec.cleanupPhase,
      ]),
  );
}

export const targetPageSizes = [10, 25, 50, 100, 200] as const;

// HTML pattern uses the v flag; escape the hyphen while matching SDK identifiers.
export const targetIdentifierPattern = String.raw`[A-Za-z0-9](?:[A-Za-z0-9._~\-]{0,126}[A-Za-z0-9])?`;

export function pageAdminTargets(
  targets: readonly DeploymentTarget[],
  requestedPage: number,
  pageSize: number,
) {
  const size = targetPageSizes.find((value) => value === pageSize) ?? 25;
  const count = Math.max(1, Math.ceil(targets.length / size));
  const index = Math.min(
    count - 1,
    Math.max(0, Number.isFinite(requestedPage) ? Math.floor(requestedPage) : 0),
  );
  return {
    index,
    count,
    size,
    items: targets.slice(index * size, (index + 1) * size),
  };
}

export function summarizeClusterHosts(
  targets: readonly DeploymentTarget[],
  workers: readonly Worker[],
): readonly ClusterHostSummary[] {
  const summaries = new Map(
    targets.map((target) => [
      target.metadata.uid,
      {
        target,
        workerCount: 0,
        readyWorkerCount: 0,
        latestHealthAt: undefined as string | undefined,
      },
    ]),
  );
  for (const worker of workers) {
    const summary = summaries.get(worker.spec.targetId);
    if (summary === undefined) continue;
    summary.workerCount += 1;
    if (worker.spec.state === "ready") summary.readyWorkerCount += 1;
    if (
      worker.spec.lastHealthAt !== undefined &&
      (summary.latestHealthAt === undefined ||
        Date.parse(worker.spec.lastHealthAt) > Date.parse(summary.latestHealthAt))
    )
      summary.latestHealthAt = worker.spec.lastHealthAt;
  }
  return Object.freeze([...summaries.values()].map((summary) => Object.freeze(summary)));
}
