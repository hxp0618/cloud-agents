import type {
  DeploymentTarget,
  NetworkPolicy,
  RuntimeProfile,
  SandboxUsageMetric,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";
import type { MessageKey, Translate } from "../i18n";

export type ConnectionStatus = "disconnected" | "connecting" | "connected" | "error";
export type TargetKind = DeploymentTarget["spec"]["targetKind"];
export type WorkloadTrust = RuntimeProfile["spec"]["workloadTrust"];
export type IsolationRuntime = RuntimeProfile["spec"]["isolationRuntime"];

export const workloadTrusts = [
  "trusted-single-tenant",
  "dedicated-node",
  "shared-untrusted",
] as const satisfies readonly WorkloadTrust[];

export function runtimeProfileTargetAllowed(
  target: DeploymentTarget,
  workloadTrust: WorkloadTrust,
): boolean {
  return (
    target.spec.targetKind === "remote-worker" ||
    (workloadTrust === "trusted-single-tenant" && target.spec.targetKind === "docker")
  );
}

// A runtime profile can be created when some trust level has both a target and a network policy.
export function runtimeProfileCreatable(
  targets: readonly DeploymentTarget[],
  networkPolicies: readonly NetworkPolicy[],
): boolean {
  return workloadTrusts.some(
    (trust) =>
      targets.some((target) => runtimeProfileTargetAllowed(target, trust)) &&
      networkPolicies.some((policy) => executableFoundationNetworkPolicy(policy, trust)),
  );
}

export function executableFoundationNetworkPolicy(
  policy: NetworkPolicy,
  workloadTrust: WorkloadTrust,
): boolean {
  if (
    workloadTrust === "shared-untrusted" &&
    (policy.spec.defaultEgress !== "deny" ||
      policy.spec.allowedEgress.length !== 0 ||
      policy.spec.previewEnabled)
  )
    return false;
  return (
    (policy.spec.defaultEgress === "deny" ||
      (policy.spec.defaultEgress === "restricted" && policy.spec.allowedEgress.length > 0)) &&
    policy.spec.allowlistPolicyRef === undefined &&
    policy.spec.dnsPolicyRef === undefined &&
    policy.spec.proxyPolicyRef === undefined &&
    !policy.spec.ingressEnabled
  );
}

export function phaseTone(phase: string): string {
  if (
    phase === "ready" ||
    phase === "available" ||
    phase === "complete" ||
    phase === "succeeded" ||
    phase === "published"
  )
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
      "deleting",
    ].includes(phase)
  )
    return "running";
  if (
    phase === "unavailable" ||
    phase === "failed" ||
    phase === "blocked" ||
    phase === "disabled" ||
    phase === "cleanup-pending" ||
    phase === "cleanup_failed"
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
  available: "phase.available",
  deleting: "phase.deleting",
  cleanup_failed: "phase.cleanupFailed",
  deleted: "phase.deleted",
  unknown: "phase.unknown",
  stopped: "phase.stopped",
  Pending: "phase.pending",
  Running: "phase.running",
  Pausing: "phase.stopping",
  Paused: "phase.stopped",
  Resuming: "phase.starting",
  Stopping: "phase.stopping",
  Terminated: "phase.stopped",
  Failed: "phase.failed",
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
  "remote-worker.drain": "remoteWorkerEnrollment.drain",
  "remote-worker.resume": "remoteWorkerEnrollment.resume",
});

const operationImpactMessageKeys: Readonly<Record<string, MessageKey>> = Object.freeze({
  "target.register": "operation.impact.register",
  "target.probe": "operation.impact.probe",
  "target.drain": "operation.impact.drain",
  "target.resume": "operation.impact.resume",
  "target.cleanup": "operation.impact.cleanup",
  "target.upgrade": "operation.impact.upgrade",
  "target.rollback": "operation.impact.rollback",
  "sandbox.stop": "operation.impact.sandboxStop",
  "sandbox.rebuild": "operation.impact.sandboxRebuild",
  "remote-worker.drain": "operation.impact.remoteWorkerDrain",
  "remote-worker.resume": "operation.impact.remoteWorkerResume",
});

export const sandboxUsageMetrics = Object.freeze([
  "allocatedMilliseconds",
  "cpuMillisMilliseconds",
  "memoryByteMilliseconds",
  "workspaceUsedBytes",
  "networkReceivedBytes",
  "networkTransmittedBytes",
] as const satisfies readonly SandboxUsageMetric[]);

const resourceMessageKeys: Readonly<Record<string, MessageKey>> = Object.freeze({
  DeploymentTarget: "maintenance.deploymentTarget",
  RemoteWorkerEnrollment: "maintenance.remoteWorker",
  container: "resource.container",
  deployment: "resource.deployment",
  pods: "resource.pods",
  service: "resource.service",
  "workspace-volume": "resource.workspaceVolume",
});

export function phaseLabel(phase: string, t: Translate): string {
  const key = phaseMessageKeys[phase];
  return key === undefined ? phase : t(key);
}

export function auditLabel(action: string, t: Translate): string {
  const key = auditMessageKeys[action];
  return key === undefined ? action : t(key);
}

export function operationImpactLabel(action: string, t: Translate): string {
  const key = operationImpactMessageKeys[action];
  return key === undefined ? action : t(key);
}

export function resourceLabel(kind: string, t: Translate): string {
  const key = resourceMessageKeys[kind];
  return key === undefined ? kind : t(key);
}

export function targetKindLabel(kind: TargetKind, t: Translate): string {
  if (kind === "kubernetes") return t("target.kind.kubernetes");
  if (kind === "ssh") return t("target.kind.ssh");
  if (kind === "remote-worker") return t("target.kind.remoteWorker");
  return t("target.kind.docker");
}

export function providerLabel(provider: string): string {
  if (provider === "claudeAgent") return "Claude Code";
  if (provider === "deepseek-harness") return "deepseek-harness";
  if (provider === "pi") return "Pi";
  return provider === "codex" ? "Codex" : provider;
}

export function shortDigest(value: string): string {
  return `${value.slice(0, 15)}…${value.slice(-8)}`;
}

export function runtimeProfileTargetLabel(profile: RuntimeProfile): string {
  const selector = profile.spec.targetSelector;
  return selector === undefined
    ? profile.spec.targetId
    : `${selector.regionId} / ${selector.resourcePoolId} / ${selector.runtime} / ${selector.architecture}`;
}
