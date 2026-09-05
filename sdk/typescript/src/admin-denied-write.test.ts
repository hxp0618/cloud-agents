import { expect, it } from "vitest";
import { Client, decodeAdminDeniedWriteEvent, decodeAdminDeniedWriteEventPage } from "./platform";

const event = {
  apiVersion: "platform.cloud-agents.dev/v1alpha1",
  kind: "AdminDeniedWriteEvent",
  tenantId: "tenant-alpha",
  projectId: "project-alpha",
  eventId: "denied-alpha",
  actor: `sha256:${"a".repeat(64)}`,
  action: "adminProbeDeploymentTarget",
  resourceId: "target-alpha",
  result: "denied",
  stableErrorCode: "AUTHORIZATION_DENIED",
  requestId: "request-alpha",
  occurredAt: "2026-09-05T12:00:00Z",
};

it("validates closed denied metadata and binds SDK pages to the requested project", async () => {
  expect(decodeAdminDeniedWriteEvent(event)).toEqual(event);
  for (const change of [
    { actor: "raw-subject" },
    { action: "arbitrary-write" },
    { result: "succeeded" },
    { resourceId: "" },
    { profileVersion: 0 },
    { profileVersion: null },
    { endpoint: "secret" },
    { operationId: "invented" },
    { occurredAt: "today" },
  ]) {
    expect(() => decodeAdminDeniedWriteEvent({ ...event, ...change })).toThrow();
  }
  const page = {
    apiVersion: event.apiVersion,
    kind: "AdminDeniedWriteEventPage",
    events: [event],
    nextPageToken: "cursor-0000000000001",
  };
  expect(decodeAdminDeniedWriteEventPage(page).events).toHaveLength(1);
  expect(() =>
    decodeAdminDeniedWriteEventPage({ ...page, events: Array(201).fill(event) }),
  ).toThrow();
  const calls: unknown[] = [];
  let body = page;
  const client = new Client(async (request) => {
    calls.push(request);
    return { status: 200, headers: {}, body: JSON.stringify(body) };
  });
  expect(
    (
      await client.listAdminDeniedWriteEvents(
        "tenant-alpha",
        "project-alpha",
        "request-alpha",
        5,
        "cursor-0000000000001",
      )
    ).value,
  ).toEqual(page);
  expect(calls).toEqual([
    {
      method: "GET",
      path: "/v1/admin/tenants/tenant-alpha/projects/project-alpha/denied-write-events?pageSize=5&pageToken=cursor-0000000000001",
      headers: { "X-Request-ID": "request-alpha" },
    },
  ]);
  body = { ...page, events: [{ ...event, projectId: "project-other" }] };
  await expect(
    client.listAdminDeniedWriteEvents("tenant-alpha", "project-alpha", "request-alpha"),
  ).rejects.toThrow();
  const count = calls.length;
  await expect(
    client.listAdminDeniedWriteEvents("tenant-alpha", "project-alpha", "request-alpha", 201),
  ).rejects.toThrow();
  expect(calls).toHaveLength(count);
});
