import { describe, expect, it } from "vitest";
import {
  ClientError,
  encodeDeploymentTargetRegisterRequest,
  type ManagedAgentExecution,
  type ManagedAgentSession,
  type McpServer,
  type SkillBundle,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  adminFailure,
  adminMutationKey,
  capabilityBindingRelations,
  availableSandboxLifecycleAction,
  pageAdminTargets,
  pendingIdempotencyKey,
  targetIdentifierPattern,
  filterAdminTargets,
  filterAdminMaintenanceOperations,
  filterAdminLeases,
  filterAdminWorkers,
  leaseNeedsAttention,
  cleanupRequestFromPreview,
  leaseReleaseRequestFromPreview,
  listAdminProjectLeaseQuotaAuditEvents,
  listAdminLeases,
  listAdminProfiles,
  listAdminRuntimeProfiles,
  listAdminSandboxAccessGrants,
  listAdminSandboxes,
  listAdminStoragePolicies,
  listAdminWorkspaceSnapshots,
  listAdminStoragePolicyAuditEvents,
  listAdminReleases,
  listAdminMaintenanceOperations,
  listAdminManagedAgentBindings,
  loadAdminManagedAgentEventPage,
  loadAdminManagedAgentRuntime,
  listAdminRemoteWorkerOperations,
  listAdminTargetAuditEvents,
  listAdminTargetOperations,
  listAdminTargets,
  listAdminWorkers,
  loadAdminProjectLeaseQuota,
  matchesAdminSearch,
  readSavedAdminConnection,
  remoteWorkerFoundationSupport,
  schedulingRequestFromPreview,
  summarizeClusterHosts,
  writeSavedAdminConnection,
  type AdminClient,
} from "../src/admin";

describe("Admin Web boundary", () => {
  it("reuses failed mutation keys only for the same complete request body", () => {
    const pending = new Map<string, string>();
    const body = { targetId: "target", targetName: "a:b", endpoint: "c" };
    const operation = adminMutationKey("register:target", body);
    const key = pendingIdempotencyKey(pending, operation);
    expect(pendingIdempotencyKey(pending, adminMutationKey("register:target", { ...body }))).toBe(
      key,
    );
    for (const changed of [
      { ...body, endpoint: "d" },
      { ...body, targetName: "a", endpoint: "b:c" },
    ]) {
      const changedOperation = adminMutationKey("register:target", changed);
      expect(changedOperation).not.toBe(operation);
      expect(pendingIdempotencyKey(pending, changedOperation)).not.toBe(key);
    }
  });

  it("normalizes shared search terms once and preserves empty-query matching", () => {
    expect(matchesAdminSearch("  ALPHA ", ["beta", "Alpha target"])).toBe(true);
    expect(matchesAdminSearch("missing", ["beta", "Alpha target"])).toBe(false);
    expect(matchesAdminSearch("", [])).toBe(false);
    expect(matchesAdminSearch("", ["anything"])).toBe(true);
  });

  it("derives opaque capability binding relations without provider content", () => {
    const mcp = [
      {
        metadata: { uid: "server-alpha" },
        spec: { version: "v1", digest: "sha256:" + "a".repeat(64) },
      },
    ] as McpServer[];
    const skills = [
      {
        metadata: { uid: "bundle-alpha" },
        spec: { version: "v1", digest: "sha256:" + "b".repeat(64) },
      },
    ] as SkillBundle[];
    const sessions = [
      {
        metadata: { uid: "session-alpha" },
        spec: {
          mcpServerRefs: [
            { serverId: "server-alpha", version: "v1", digest: "sha256:" + "a".repeat(64) },
          ],
          skillBundleRefs: [
            { bundleId: "bundle-alpha", version: "v1", digest: "sha256:" + "b".repeat(64) },
          ],
        },
      },
    ] as unknown as ManagedAgentSession[];
    const executions = [
      {
        metadata: { uid: "execution-alpha" },
        spec: {
          mcpServerRefs: [
            { serverId: "server-alpha", version: "v1", digest: "sha256:" + "a".repeat(64) },
          ],
        },
      },
    ] as unknown as ManagedAgentExecution[];
    expect(capabilityBindingRelations(mcp, skills, sessions, executions)).toEqual([
      {
        kind: "mcp",
        resourceId: "server-alpha",
        version: "v1",
        digest: "sha256:" + "a".repeat(64),
        sessionIds: ["session-alpha"],
        executionIds: ["execution-alpha"],
      },
      {
        kind: "skill",
        resourceId: "bundle-alpha",
        version: "v1",
        digest: "sha256:" + "b".repeat(64),
        sessionIds: ["session-alpha"],
        executionIds: [],
      },
    ]);
  });

  it("shows the same RemoteWorker Foundation baseline enforced by Control Plane", () => {
    const node = {
      architecture: "arm64",
      capabilities: ["docker", "network-dns-nft", "workspace-volume"],
      capacity: { cpuMillis: 4000, memoryBytes: 8 * 1024 ** 3, diskBytes: 20 * 1024 ** 3 },
    } satisfies Parameters<typeof remoteWorkerFoundationSupport>[0];
    expect(remoteWorkerFoundationSupport(node)).toEqual({
      runtime: true,
      architecture: true,
      storage: true,
      network: true,
    });
    expect(
      remoteWorkerFoundationSupport({
        ...node,
        architecture: "riscv64",
        capabilities: ["docker"],
        capacity: { ...node.capacity, diskBytes: 20 * 1024 ** 3 - 1 },
      }),
    ).toEqual({ runtime: true, architecture: false, storage: false, network: false });
  });

  it("offers Sandbox lifecycle actions only from fully settled physical states", () => {
    const sandbox = (spec: Record<string, unknown>) =>
      ({
        spec: {
          operationState: "succeeded",
          cleanupPhase: "complete",
          generation: 4,
          observedGeneration: 4,
          workspaceObservedState: "available",
          physicalVolumeId: "volume-1",
          desiredState: "running",
          observedState: "running",
          writerReleased: false,
          runtimeId: "runtime-1",
          ...spec,
        },
      }) as Parameters<typeof availableSandboxLifecycleAction>[0];
    expect(availableSandboxLifecycleAction(sandbox({}))).toBe("stop");
    expect(
      availableSandboxLifecycleAction(
        sandbox({
          desiredState: "stopped",
          observedState: "stopped",
          writerReleased: true,
          runtimeId: undefined,
        }),
      ),
    ).toBe("rebuild");
    expect(availableSandboxLifecycleAction(sandbox({ operationState: "pending" }))).toBeNull();
    expect(availableSandboxLifecycleAction(sandbox({ observedGeneration: 3 }))).toBeNull();
    expect(availableSandboxLifecycleAction(sandbox({ physicalVolumeId: undefined }))).toBeNull();
  });
  it("uses native identifier constraints matching the generated Target request contract", () => {
    const pattern = new RegExp(`^(?:${targetIdentifierPattern})$`, "v");
    for (const value of [
      "a",
      "9",
      "A.b_c~d-e9",
      "a".repeat(128),
      "",
      "a".repeat(129),
      "has space",
      " leading",
      "trailing ",
      "-name",
      "name-",
      "name\n",
      "中文",
      "name/part",
    ]) {
      for (const field of ["targetId", "targetName"] as const) {
        let accepted = true;
        try {
          encodeDeploymentTargetRegisterRequest({
            targetId: "target-valid",
            targetName: "target-valid",
            targetKind: "docker",
            endpoint: "https://127.0.0.1:1",
            credentialRef: "unused",
            [field]: value,
          });
        } catch {
          accepted = false;
        }
        expect(pattern.test(value)).toBe(accepted);
      }
    }
  });
  it("filters exact failed state and sorts by actual update instant without mutating the snapshot", () => {
    const operations = [
      { operationId: "b", state: "failed", updatedAt: "2026-09-05T10:00:00+08:00" },
      { operationId: "running", state: "running", updatedAt: "2026-09-05T04:00:00Z" },
      { operationId: "a", state: "failed", updatedAt: "2026-09-05T02:00:00Z" },
      { operationId: "new", state: "failed", updatedAt: "2026-09-05T03:00:00Z" },
      {
        operationId: "succeeded-failed-name",
        state: "succeeded",
        updatedAt: "2026-09-05T05:00:00Z",
      },
    ].map((operation) =>
      Object.freeze({
        ...operation,
        action: "target.probe",
        resourceKind: "DeploymentTarget",
        resourceId: "target-1",
        currentStep: "probe",
        requestId: "request-1",
        stableErrorCode: operation.state === "failed" ? "docker-probe-unconfigured" : undefined,
        impactSummary: "do-not-search-this",
        idempotencyKey: "do-not-search-this",
      }),
    ) as unknown as Parameters<typeof filterAdminMaintenanceOperations>[0];
    const before = [...operations];
    Object.freeze(operations);
    expect(
      filterAdminMaintenanceOperations(operations, "", true).map((x) => x.operationId),
    ).toEqual(["new", "a", "b"]);
    expect(filterAdminMaintenanceOperations(operations, " DOCKER-PROBE ", true)).toHaveLength(3);
    expect(filterAdminMaintenanceOperations(operations, "succeeded", true)).toEqual([]);
    expect(filterAdminMaintenanceOperations(operations, "request-1", false)).toHaveLength(5);
    expect(filterAdminMaintenanceOperations(operations, "do-not-search-this", false)).toEqual([]);
    expect(filterAdminMaintenanceOperations(operations, "", false)[0]?.operationId).toBe(
      "succeeded-failed-name",
    );
    expect(operations).toEqual(before);
  });
  it("uses the same failed-or-blocked lease predicate for overview counts and filtered search", () => {
    const leases = [
      ["ready", "pending"],
      ["failed", "pending"],
      ["terminating", "blocked"],
      ["failed", "blocked"],
      ["terminated", "complete"],
    ].map(([observedPhase, cleanupPhase], index) => ({
      metadata: { uid: `lease-${index}`, name: `Lease ${index}` },
      spec: {
        observedPhase,
        cleanupPhase,
        environmentId: `env-${index}`,
        providerCredentialRef: "private-credential",
      },
    })) as unknown as Parameters<typeof filterAdminLeases>[0];
    const attention = leases.filter(leaseNeedsAttention);
    expect(attention).toEqual([leases[1], leases[2], leases[3]]);
    expect(filterAdminLeases(leases, "", true)).toEqual(attention);
    expect(filterAdminLeases(leases, " FAILED ", true)).toEqual([leases[1], leases[3]]);
    expect(filterAdminLeases(leases, "env-2", true)).toEqual([leases[2]]);
    expect(filterAdminLeases(leases, "lease-0", true)).toEqual([]);
    expect(filterAdminLeases(leases, "lease-0", false)).toEqual([leases[0]]);
    expect(filterAdminLeases(leases, "", false)).toEqual(leases);
    expect(filterAdminLeases(leases, "private-credential", false)).toEqual([]);
    expect(filterAdminLeases(leases, "", false, "failed")).toEqual([leases[1], leases[3]]);
    expect(filterAdminLeases(leases, "", false, "", true)).toEqual([leases[2], leases[3]]);
    expect(filterAdminLeases(leases, "", false, "failed", true)).toEqual([leases[3]]);
    expect(filterAdminLeases(leases, "env-2", true, "terminating", true)).toEqual([leases[2]]);
    expect(filterAdminLeases(leases, "env-0", false, "failed")).toEqual([]);
    expect(filterAdminLeases(leases, "", true, "ready")).toEqual([]);
  });
  it("combines target search, kind and phase without searching endpoint or credentials", () => {
    const targets = (["docker", "kubernetes", "ssh"] as const).map((targetKind, index) => ({
      metadata: { uid: `target-${index}`, name: `Host ${targetKind}` },
      spec: {
        targetKind,
        observedPhase: index === 0 ? "ready" : "unprobed",
        schedulingState: "active",
        engineVersion: index === 0 ? "29.4.0" : "",
        apiVersion: index === 0 ? "1.54" : "",
        os: index === 0 ? "linux" : "",
        architecture: index === 0 ? "arm64" : "",
        endpoint: "https://private-endpoint.test",
        credentialRef: "private-credential",
      },
    })) as unknown as Parameters<typeof filterAdminTargets>[0];
    expect(filterAdminTargets(targets, "", [], [])).toEqual(targets);
    expect(filterAdminTargets(targets, " HOST ", ["docker"], ["ready"])).toEqual([targets[0]]);
    expect(filterAdminTargets(targets, "target-1", [], ["unprobed"])).toEqual([targets[1]]);
    expect(filterAdminTargets(targets, "", ["docker"], ["unprobed"])).toEqual([]);
    expect(filterAdminTargets(targets, "", ["docker", "ssh"], [])).toEqual([
      targets[0],
      targets[2],
    ]);
    expect(filterAdminTargets(targets, "", ["docker", "ssh"], ["ready", "unprobed"])).toEqual([
      targets[0],
      targets[2],
    ]);
    expect(filterAdminTargets(targets, "", ["docker", "ssh"], ["unprobed"])).toEqual([targets[2]]);
    for (const fact of ["29.4.0", "1.54", "LINUX", "arm64"])
      expect(filterAdminTargets(targets, fact, [], [])).toEqual([targets[0]]);
    for (const secret of ["private-endpoint", "private-credential"])
      expect(filterAdminTargets(targets, secret, [], [])).toEqual([]);
    expect(targets).toHaveLength(3);
  });
  it("shows only validated stable error codes and localized messages, never raw diagnostics", () => {
    const problem = {
      type: "https://problems.cloud-agents.dev/environment-cleanup-unavailable",
      title: "untrusted diagnostics must not render",
      status: 503,
      error: { code: "ENVIRONMENT_CLEANUP_UNAVAILABLE", retryable: true },
      requestId: "test-safe-feedback",
    };
    expect(adminFailure(new ClientError("cleanup", 503, problem))).toEqual({
      key: "error.actuatorUnavailable",
      code: "ENVIRONMENT_CLEANUP_UNAVAILABLE",
    });
    for (const invalid of [
      { ...problem, credential: "secret-bytes" },
      { ...problem, error: { ...problem.error, code: "secret-bytes" } },
      { ...problem, status: 500 },
      null,
    ])
      expect(adminFailure(new ClientError("cleanup", 503, invalid))).toEqual({
        key: "error.actuatorUnavailable",
        code: null,
      });
    expect(adminFailure(new Error("secret-bytes"))).toEqual({ key: "error.generic", code: null });
  });
  it("turns known quota response codes into actionable messages", () => {
    const codes = [
      ["PROJECT_LEASE_COUNT_QUOTA_EXCEEDED", "error.quotaCount"],
      ["PROJECT_LEASE_CPU_QUOTA_EXCEEDED", "error.quotaCpu"],
      ["PROJECT_LEASE_MEMORY_QUOTA_EXCEEDED", "error.quotaMemory"],
      ["PROJECT_LEASE_TTL_QUOTA_EXCEEDED", "error.quotaTtl"],
    ] as const;
    for (const [code, key] of codes) {
      const problem = {
        type: "https://problems.cloud-agents.dev/lease-quota",
        title: "Conflict",
        status: 409,
        error: { code, retryable: false },
        requestId: "quota-test",
      };
      expect(adminFailure(new ClientError("lease", 409, problem))).toEqual({ key, code });
    }
  });
  it("maps HTTP problem statuses to actionable localized categories", () => {
    const cases = [
      [400, "error.invalidRequest"],
      [405, "error.invalidRequest"],
      [415, "error.invalidRequest"],
      [422, "error.invalidRequest"],
      [408, "error.timeout"],
      [429, "error.rateLimited"],
      [502, "error.actuatorUnavailable"],
      [503, "error.actuatorUnavailable"],
      [504, "error.actuatorUnavailable"],
    ] as const;
    for (const [status, key] of cases)
      expect(adminFailure(new ClientError("request", status))).toEqual({ key, code: null });
  });

  it("does not let a quota code hide an authentication failure", () => {
    const problem = {
      type: "https://problems.cloud-agents.dev/lease-quota",
      title: "Unauthorized",
      status: 401,
      error: { code: "PROJECT_LEASE_COUNT_QUOTA_EXCEEDED", retryable: false },
      requestId: "auth-test",
    };
    expect(adminFailure(new ClientError("lease", 401, problem))).toEqual({
      key: "error.tokenExpired",
      code: "PROJECT_LEASE_COUNT_QUOTA_EXCEEDED",
    });
  });
  it("submits the exact cleanup fences returned by the preview", () => {
    expect(
      cleanupRequestFromPreview({
        spec: {
          expectedGeneration: 7,
          expectedResourceVersion: "42",
          impactDigest: `sha256:${"a".repeat(64)}`,
        },
      } as unknown as Parameters<typeof cleanupRequestFromPreview>[0]),
    ).toEqual({
      expectedGeneration: 7,
      expectedResourceVersion: "42",
      impactDigest: `sha256:${"a".repeat(64)}`,
    });
  });

  it("submits the exact scheduling transition returned by the preview", () => {
    expect(
      schedulingRequestFromPreview({
        spec: {
          expectedGeneration: 7,
          expectedResourceVersion: "42",
          desiredState: "drained",
          impactDigest: `sha256:${"b".repeat(64)}`,
        },
      } as unknown as Parameters<typeof schedulingRequestFromPreview>[0]),
    ).toEqual({
      expectedGeneration: 7,
      expectedResourceVersion: "42",
      desiredState: "drained",
      impactDigest: `sha256:${"b".repeat(64)}`,
    });
  });

  it("submits the exact release transition fences returned by the preview", () => {
    expect(
      leaseReleaseRequestFromPreview({
        spec: {
          targetReleaseDigest: `sha256:${"c".repeat(64)}`,
          expectedGeneration: 8,
          expectedResourceVersion: "43",
          impactDigest: `sha256:${"d".repeat(64)}`,
        },
      } as unknown as Parameters<typeof leaseReleaseRequestFromPreview>[0]),
    ).toEqual({
      releaseDigest: `sha256:${"c".repeat(64)}`,
      expectedGeneration: 8,
      expectedResourceVersion: "43",
      impactDigest: `sha256:${"d".repeat(64)}`,
    });
  });

  it("uses only Admin API target pagination", async () => {
    const paths: Array<string | undefined> = [];
    const client = {
      listAdminDeploymentTargets: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        paths.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "DeploymentTargetPage" as const,
            deploymentTargets: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminTargets(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(paths).toEqual([undefined, "next-page"]);
  });

  it("uses only Admin API lease pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminEnvironmentLeases: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "EnvironmentLeasePage" as const,
            environmentLeases: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminLeases(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("uses only Admin API Worker pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminWorkers: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "WorkerPage" as const,
            workers: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminWorkers(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("uses only Admin API release pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminWorkerReleases: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "WorkerReleasePage" as const,
            workerReleases: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminReleases(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("filters persisted Worker health separately from lifecycle failures without browser-clock guesses", () => {
    const workers = ["online", "expired", "unavailable", undefined, undefined].map((health, i) => ({
      metadata: { uid: `worker-${i}`, name: `worker-${i}` },
      spec: {
        leaseId: `worker-${i}`,
        targetId: "docker-alpha",
        targetKind: "docker",
        releaseDigest: "sha256:test",
        state: i === 4 ? "failed" : "ready",
        ...(health ? { health: { state: health, checkedAt: "2000-01-01T00:00:00Z" } } : {}),
      },
    })) as unknown as Parameters<typeof filterAdminWorkers>[0];
    expect(filterAdminWorkers(workers, "", "online").map((w) => w.metadata.uid)).toEqual([
      "worker-0",
    ]);
    expect(filterAdminWorkers(workers, "", "expired")).toHaveLength(1);
    expect(filterAdminWorkers(workers, "", "unavailable")).toHaveLength(1);
    expect(filterAdminWorkers(workers, "", "not-observed")).toHaveLength(2);
    expect(filterAdminWorkers(workers, "", "failed").map((w) => w.metadata.uid)).toEqual([
      "worker-4",
    ]);
    expect(filterAdminWorkers(workers, " Docker-Alpha ", "online")).toHaveLength(1);
    expect(filterAdminWorkers(workers, "worker-1", "online")).toHaveLength(0);
  });

  it("groups current Worker authority by its Deployment Target", () => {
    const targets = [
      { metadata: { uid: "docker-alpha" } },
      { metadata: { uid: "kubernetes-alpha" } },
    ] as unknown as Parameters<typeof summarizeClusterHosts>[0];
    const workers = [
      {
        spec: {
          targetId: "docker-alpha",
          state: "ready",
          lastHealthAt: "2026-09-04T01:00:00Z",
        },
      },
      {
        spec: {
          targetId: "docker-alpha",
          state: "failed",
          lastHealthAt: "2026-09-04T02:00:00Z",
        },
      },
      { spec: { targetId: "removed-target", state: "ready" } },
    ] as unknown as Parameters<typeof summarizeClusterHosts>[1];

    expect(
      summarizeClusterHosts(targets, workers).map(
        ({ target, workerCount, readyWorkerCount, latestHealthAt }) => ({
          targetId: target.metadata.uid,
          workerCount,
          readyWorkerCount,
          latestHealthAt,
        }),
      ),
    ).toEqual([
      {
        targetId: "docker-alpha",
        workerCount: 2,
        readyWorkerCount: 1,
        latestHealthAt: "2026-09-04T02:00:00Z",
      },
      {
        targetId: "kubernetes-alpha",
        workerCount: 0,
        readyWorkerCount: 0,
        latestHealthAt: undefined,
      },
    ]);
  });

  it("uses only Admin API profile pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminEnvironmentProfiles: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "EnvironmentProfilePage" as const,
            environmentProfiles: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminProfiles(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("uses only Admin API RuntimeProfile and Sandbox pagination", async () => {
    const calls: string[] = [];
    const client = {
      listAdminRuntimeProfiles: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        calls.push(`profiles:${pageToken ?? "first"}`);
        return {
          value: {
            runtimeProfiles: [],
            ...(pageToken === undefined ? { nextPageToken: "next-profile-page" } : {}),
          },
        };
      },
      listAdminSandboxSessions: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        calls.push(`sandboxes:${pageToken ?? "first"}`);
        return {
          value: {
            sandboxSessions: [],
            ...(pageToken === undefined ? { nextPageToken: "next-sandbox-page" } : {}),
          },
        };
      },
      listAdminSandboxAccessGrants: async (
        _tenantId: string,
        _projectId: string,
        _sandboxId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        calls.push(`grants:${pageToken ?? "first"}`);
        return {
          value: {
            accessGrants: [],
            ...(pageToken === undefined ? { nextPageToken: "next-grant-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;
    const signal = new AbortController().signal;
    await listAdminRuntimeProfiles(client, "tenant-alpha", "project-alpha", signal);
    await listAdminSandboxes(client, "tenant-alpha", "project-alpha", signal);
    await listAdminSandboxAccessGrants(
      client,
      "tenant-alpha",
      "project-alpha",
      "sandbox-alpha",
      signal,
    );
    expect(calls).toEqual([
      "profiles:first",
      "profiles:next-profile-page",
      "sandboxes:first",
      "sandboxes:next-sandbox-page",
      "grants:first",
      "grants:next-grant-page",
    ]);
  });

  it("loads only opaque Managed Agent recovery metadata bound to a Sandbox", async () => {
    const calls: string[] = [];
    const client = {
      listAdminManagedAgentSessions: async (
        _tenant: string,
        _project: string,
        _request: string,
        sandboxId?: string,
      ) => {
        calls.push(`sessions:${sandboxId}`);
        return {
          value: {
            sessions: [
              { metadata: { uid: "session-bound" }, spec: { sandboxId: "sandbox-alpha" } },
            ],
          },
        };
      },
      listAdminManagedAgentExecutions: async (
        _tenantId: string,
        _projectId: string,
        sessionId: string,
      ) => {
        calls.push(`executions:${sessionId}`);
        return { value: { executions: [{ metadata: { uid: "execution-alpha" } }] } };
      },
      listAdminManagedAgentEvents: async (
        _tenantId: string,
        _projectId: string,
        sessionId: string,
      ) => {
        calls.push(`events:${sessionId}`);
        return {
          value: {
            events: [{ metadata: { uid: "event-1" } }, { metadata: { uid: "event-2" } }],
            nextCursor: "cursor-2",
            hasMore: false,
          },
        };
      },
    } as unknown as AdminClient;

    const result = await loadAdminManagedAgentRuntime(
      client,
      "tenant-alpha",
      "project-alpha",
      "sandbox-alpha",
      new AbortController().signal,
    );

    expect(result.sessions.map(({ metadata }) => metadata.uid)).toEqual(["session-bound"]);
    expect(result.executions.map(({ metadata }) => metadata.uid)).toEqual(["execution-alpha"]);
    expect(calls).toEqual(["sessions:sandbox-alpha", "executions:session-bound"]);
  });

  it("keeps 33-session metadata usable and reads more than 4096 events only on demand", async () => {
    let eventCalls = 0;
    const client = {
      listAdminManagedAgentSessions: async () => ({
        value: {
          sessions: Array.from({ length: 33 }, (_, index) => ({
            metadata: { uid: `session-${index}` },
            spec: { sandboxId: "sandbox" },
          })),
        },
      }),
      listAdminManagedAgentExecutions: async () => ({ value: { executions: [] } }),
      listAdminManagedAgentEvents: async (
        _tenant: string,
        _project: string,
        session: string,
        _request: string,
        cursor?: string,
        limit?: number,
      ) => {
        eventCalls++;
        expect(limit).toBe(64);
        const offset = Number(cursor ?? 0);
        return {
          value: {
            events: Array.from({ length: 64 }, (_, index) => ({
              metadata: { uid: `${session}-${offset + index}` },
            })),
            hasMore: offset < 64,
            nextCursor: String(offset + 64),
          },
        };
      },
    } as unknown as AdminClient;
    const signal = new AbortController().signal;
    const runtime = await loadAdminManagedAgentRuntime(client, "tenant", "project", "", signal);
    expect(runtime.sessions).toHaveLength(33);
    expect(eventCalls).toBe(0);
    for (const session of runtime.sessions) {
      const first = await loadAdminManagedAgentEventPage(
        client,
        "tenant",
        "project",
        session.metadata.uid,
        signal,
      );
      expect(first.events).toHaveLength(64);
      const last = await loadAdminManagedAgentEventPage(
        client,
        "tenant",
        "project",
        session.metadata.uid,
        signal,
        first,
      );
      expect(last.events).toHaveLength(64);
      expect(last.hasMore).toBe(false);
    }
    expect(eventCalls).toBe(66);
  });

  it.each(["", "cursor-1", "cursor-old"])(
    "rejects missing, stalled or repeated event cursor %s",
    async (nextCursor) => {
      const client = {
        listAdminManagedAgentEvents: async () => ({
          value: { events: [], hasMore: true, nextCursor },
        }),
      } as unknown as AdminClient;
      await expect(
        loadAdminManagedAgentEventPage(
          client,
          "tenant",
          "project",
          "session",
          new AbortController().signal,
          {
            events: [],
            hasMore: true,
            nextCursor: "cursor-1",
            seenCursors: ["cursor-old", "cursor-1"],
          },
        ),
      ).rejects.toThrow("error.agentEventCursor");
    },
  );

  it("loads project-wide capability bindings without fetching event streams", async () => {
    const calls: string[] = [];
    const client = {
      listAdminManagedAgentSessions: async () => ({
        value: {
          sessions: [
            { metadata: { uid: "session-alpha" }, spec: { sandboxId: "sandbox-alpha" } },
            { metadata: { uid: "session-beta" }, spec: { sandboxId: "sandbox-beta" } },
          ],
        },
      }),
      listAdminManagedAgentExecutions: async (
        _tenantId: string,
        _projectId: string,
        sessionId: string,
      ) => {
        calls.push(`executions:${sessionId}`);
        return { value: { executions: [] } };
      },
      listAdminManagedAgentEvents: async () => {
        calls.push("events");
        return { value: { events: [], hasMore: false } };
      },
    } as unknown as AdminClient;

    const result = await listAdminManagedAgentBindings(
      client,
      "tenant-alpha",
      "project-alpha",
      new AbortController().signal,
    );

    expect(result.sessions.map(({ metadata }) => metadata.uid)).toEqual([
      "session-alpha",
      "session-beta",
    ]);
    expect(calls).toEqual(["executions:session-alpha"]);
  });

  it("replaces bounded Session and selected-Session Execution windows", async () => {
    const calls: string[] = [];
    const client = {
      listAdminManagedAgentSessions: async (
        _tenant: string,
        _project: string,
        _request: string,
        _sandbox: string | undefined,
        limit: number,
        token?: string,
      ) => {
        calls.push(`sessions:${token ?? "first"}:${limit}`);
        return {
          value: {
            sessions: [{ metadata: { uid: token ? "session-beta" : "session-alpha" } }],
            nextPageToken: token ? undefined : "session-next",
          },
        };
      },
      listAdminManagedAgentExecutions: async (
        _tenant: string,
        _project: string,
        sessionId: string,
        _request: string,
        limit: number,
        token?: string,
      ) => {
        calls.push(`executions:${sessionId}:${token ?? "first"}:${limit}`);
        return {
          value: {
            executions: [{ metadata: { uid: token ? "execution-2" : "execution-1" } }],
            nextPageToken: token ? undefined : "execution-next",
          },
        };
      },
    } as unknown as AdminClient;
    const signal = new AbortController().signal;
    const first = await loadAdminManagedAgentRuntime(client, "tenant", "project", "", signal);
    const executions = await loadAdminManagedAgentRuntime(
      client,
      "tenant",
      "project",
      "",
      signal,
      first,
      false,
      first.selectedSessionId,
      true,
    );
    const sessions = await loadAdminManagedAgentRuntime(
      client,
      "tenant",
      "project",
      "",
      signal,
      first,
      true,
    );
    const laterExecutions = await loadAdminManagedAgentRuntime(
      client,
      "tenant",
      "project",
      "",
      signal,
      sessions,
      false,
      sessions.selectedSessionId,
      true,
    );
    const refreshed = await loadAdminManagedAgentRuntime(
      client,
      "tenant",
      "project",
      "",
      signal,
      laterExecutions,
    );
    expect(executions.executions[0]?.metadata.uid).toBe("execution-2");
    expect(sessions.sessions[0]?.metadata.uid).toBe("session-beta");
    expect(sessions.selectedSessionId).toBe("session-beta");
    expect(refreshed.sessionPageToken).toBe("session-next");
    expect(refreshed.executionPageToken).toBe("execution-next");
    expect(refreshed.selectedSessionId).toBe("session-beta");
    expect(calls).toEqual([
      "sessions:first:64",
      "executions:session-alpha:first:64",
      "sessions:first:64",
      "executions:session-alpha:execution-next:64",
      "sessions:session-next:64",
      "executions:session-beta:first:64",
      "sessions:session-next:64",
      "executions:session-beta:execution-next:64",
      "sessions:session-next:64",
      "executions:session-beta:execution-next:64",
    ]);
  });

  it("pages and newest-first sorts workspace snapshot metadata from Admin API", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminWorkspaceSnapshots: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            workspaceSnapshots: [
              {
                metadata: {
                  uid: pageToken === undefined ? "older" : "newer",
                  createdAt:
                    pageToken === undefined ? "2026-09-08T01:00:00Z" : "2026-09-08T02:00:00Z",
                },
              },
            ],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    const snapshots = await listAdminWorkspaceSnapshots(
      client,
      "tenant-alpha",
      "project-alpha",
      new AbortController().signal,
    );
    expect(tokens).toEqual([undefined, "next-page"]);
    expect(snapshots.map(({ metadata }) => metadata.uid)).toEqual(["newer", "older"]);
  });

  it("uses only Admin API storage policy and audit pagination", async () => {
    const calls: string[] = [];
    const client = {
      listAdminStoragePolicies: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        calls.push(`policies:${pageToken ?? "first"}`);
        return {
          value: {
            storagePolicies: [],
            ...(pageToken === undefined ? { nextPageToken: "next-policy-page" } : {}),
          },
        };
      },
      listAdminStoragePolicyAuditEvents: async (
        _tenantId: string,
        _projectId: string,
        policyId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        calls.push(`audit:${policyId}:${pageToken ?? "first"}`);
        return {
          value: {
            events: [],
            ...(pageToken === undefined ? { nextPageToken: "next-audit-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;
    const signal = new AbortController().signal;
    await listAdminStoragePolicies(client, "tenant-alpha", "project-alpha", signal);
    await listAdminStoragePolicyAuditEvents(
      client,
      "tenant-alpha",
      "project-alpha",
      "storage-standard",
      signal,
    );
    expect(calls).toEqual([
      "policies:first",
      "policies:next-policy-page",
      "audit:storage-standard:first",
      "audit:storage-standard:next-audit-page",
    ]);
  });

  it("loads project Lease quota from Admin API and treats no policy as optional", async () => {
    const quota = { kind: "ProjectLeaseQuota" };
    const getAdminProjectLeaseQuota = async () => ({ value: quota });
    await expect(
      loadAdminProjectLeaseQuota(
        { getAdminProjectLeaseQuota } as unknown as AdminClient,
        "tenant-alpha",
        "project-alpha",
        new AbortController().signal,
      ),
    ).resolves.toBe(quota);

    await expect(
      loadAdminProjectLeaseQuota(
        {
          getAdminProjectLeaseQuota: async () => {
            throw new ClientError("quota", 404);
          },
        } as unknown as AdminClient,
        "tenant-alpha",
        "project-alpha",
        new AbortController().signal,
      ),
    ).resolves.toBeUndefined();
  });

  it("uses only Admin API quota audit pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminProjectLeaseQuotaAuditEvents: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            events: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminProjectLeaseQuotaAuditEvents(
      client,
      "tenant-alpha",
      "project-alpha",
      new AbortController().signal,
    );
    expect(tokens).toEqual([undefined, "next-page"]);

    await expect(
      listAdminProjectLeaseQuotaAuditEvents(
        {
          listAdminProjectLeaseQuotaAuditEvents: async () => {
            throw new ClientError("quota", 404);
          },
        } as unknown as AdminClient,
        "tenant-alpha",
        "project-alpha",
        new AbortController().signal,
      ),
    ).resolves.toEqual([]);
  });

  it("reads target activity only through scoped Admin API pagination", async () => {
    const calls: string[] = [];
    const client = {
      listAdminDeploymentTargetOperations: async (
        _tenantId: string,
        _projectId: string,
        targetId: string,
        _requestId: string,
        _pageSize?: number,
        _pageToken?: string,
      ) => {
        calls.push(`operations:${targetId}`);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "MaintenanceOperationPage" as const,
            operations: [],
          },
        };
      },
      listAdminDeploymentTargetAuditEvents: async (
        _tenantId: string,
        _projectId: string,
        targetId: string,
      ) => {
        calls.push(`audit:${targetId}`);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "AdminAuditEventPage" as const,
            events: [],
          },
        };
      },
    } as unknown as AdminClient;
    const signal = new AbortController().signal;
    await Promise.all([
      listAdminTargetOperations(client, "tenant-alpha", "project-alpha", "docker-alpha", signal),
      listAdminTargetAuditEvents(client, "tenant-alpha", "project-alpha", "docker-alpha", signal),
    ]);
    expect(calls).toEqual(["operations:docker-alpha", "audit:docker-alpha"]);
  });

  it("reads project maintenance operations through the generated Admin API", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminMaintenanceOperations: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "MaintenanceOperationPage" as const,
            operations: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminMaintenanceOperations(
      client,
      "tenant-alpha",
      "project-alpha",
      new AbortController().signal,
    );
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("reads RemoteWorker operations through scoped Admin API pagination", async () => {
    const calls: string[] = [];
    const client = {
      listAdminRemoteWorkerOperations: async (
        _tenantId: string,
        _projectId: string,
        enrollmentId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        calls.push(`${enrollmentId}:${pageToken ?? "first"}`);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "MaintenanceOperationPage" as const,
            operations: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminRemoteWorkerOperations(
      client,
      "tenant-alpha",
      "project-alpha",
      "enrollment-alpha",
      new AbortController().signal,
    );
    expect(calls).toEqual(["enrollment-alpha:first", "enrollment-alpha:next-page"]);
  });

  it("pages the complete filtered Target snapshot without dropping or duplicating rows", () => {
    const targets = Array.from({ length: 61 }, (_, index) => ({
      metadata: { uid: `target-${index}` },
    })) as unknown as Parameters<typeof pageAdminTargets>[0];
    const pages = [0, 1, 2].map((index) => pageAdminTargets(targets, index, 25));
    expect(pages.flatMap((page) => page.items)).toEqual(targets);
    expect(pages.map((page) => page.items.length)).toEqual([25, 25, 11]);
    expect(pageAdminTargets(targets, 99, 25).index).toBe(2);
    expect(pageAdminTargets(targets, -1, 25).index).toBe(0);
    expect(pageAdminTargets(targets, NaN, 0).size).toBe(25);
    expect(pageAdminTargets(targets.slice(0, 3), 2, 25).items).toEqual(targets.slice(0, 3));
    expect(pageAdminTargets([], 5, 10)).toEqual({ index: 0, count: 1, size: 10, items: [] });
    for (const size of [10, 25, 50, 100, 200])
      expect(pageAdminTargets(targets, 0, size).items.length).toBe(Math.min(size, targets.length));
    expect(targets).toHaveLength(61);
  });

  it("persists context but never bearer or target credentials", () => {
    let value = "";
    const storage = {
      getItem: () => value || null,
      setItem: (_key: string, next: string) => {
        value = next;
      },
    };
    writeSavedAdminConnection(storage, {
      endpoint: "https://control-plane.example.test",
      tenantId: "tenant-alpha",
      projectId: "project-alpha",
      token: "secret-token",
      credentialRef: "docker-secret",
    } as unknown as Parameters<typeof writeSavedAdminConnection>[1]);
    expect(value).not.toContain("secret-token");
    expect(value).not.toContain("docker-secret");
    expect(value).not.toContain("control-plane.example.test");
    expect(readSavedAdminConnection(storage)).toEqual({
      tenantId: "tenant-alpha",
      projectId: "project-alpha",
    });
  });
});
