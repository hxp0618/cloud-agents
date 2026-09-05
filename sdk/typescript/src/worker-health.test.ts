import { describe, expect, it } from "vitest";
import { Client, decodeWorkerHealthObservation } from "./platform";

const observation = {
  apiVersion: "platform.cloud-agents.dev/v1alpha1",
  kind: "WorkerHealthObservation",
  tenantId: "tenant-alpha",
  projectId: "project-alpha",
  workerId: "lease-alpha",
  generation: 2,
  resourceVersion: "3",
  state: "serving",
  checkedAt: "2026-09-05T12:00:00Z",
};

describe("Admin Worker health observation", () => {
  it("validates authority, state, generation and closed payload", async () => {
    expect(decodeWorkerHealthObservation(observation).state).toBe("serving");
    for (const change of [
      { state: "online" },
      { generation: 0 },
      { generation: 1.5 },
      { checkedAt: "yesterday" },
      { resourceVersion: "01" },
      { endpoint: "https://secret.test" },
    ]) {
      expect(() => decodeWorkerHealthObservation({ ...observation, ...change })).toThrow();
    }
    const calls: unknown[] = [];
    let responseBody = observation;
    const client = new Client(async (request) => {
      calls.push(request);
      return { status: 200, headers: {}, body: JSON.stringify(responseBody) };
    });
    const result = await client.getAdminWorkerHealth(
      "tenant-alpha",
      "project-alpha",
      "lease-alpha",
      "request-worker-health",
      2,
    );
    expect(result.value).toEqual(observation);
    expect(calls).toEqual([
      {
        method: "GET",
        path: "/v1/admin/tenants/tenant-alpha/projects/project-alpha/workers/lease-alpha/health?expectedGeneration=2",
        headers: { "X-Request-ID": "request-worker-health" },
      },
    ]);
    for (const change of [
      { tenantId: "tenant-other" },
      { projectId: "project-other" },
      { workerId: "lease-other" },
      { generation: 3 },
    ]) {
      responseBody = { ...observation, ...change };
      await expect(
        client.getAdminWorkerHealth(
          "tenant-alpha",
          "project-alpha",
          "lease-alpha",
          "request-worker-health",
          2,
        ),
      ).rejects.toThrow();
    }
    const count = calls.length;
    await expect(
      client.getAdminWorkerHealth(
        "tenant-alpha",
        "project-alpha",
        "lease-alpha",
        "request-worker-health",
        0,
      ),
    ).rejects.toThrow();
    expect(calls).toHaveLength(count);
  });
});
