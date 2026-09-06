package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
)

func TestRemoteWorkerHeartbeatUsesOnlyMTLSTransportAuthority(t *testing.T) {
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Headers: map[string]string{"Cache-Control": "no-store"}, Body: []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RemoteWorkerHeartbeat","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"enrollmentId":"enrollment-alpha","workerId":"worker-alpha","incarnationId":"incarnation-alpha","generation":2,"observedGeneration":1,"desiredState":"drained","observedState":"active","healthState":"online","acceptedAt":"2026-09-06T12:00:00Z","expiresAt":"2026-09-06T12:00:30Z","nextHeartbeatAfterSeconds":5,"reconcileRequired":true,"command":{"commandId":"command-alpha","generation":2,"desiredState":"drained","deadline":"2026-09-06T12:00:30Z"}}`)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := platform.RemoteWorkerHeartbeatRequest{
		IncarnationID: "incarnation-alpha", ObservedGeneration: 1, ObservedState: "active",
		WorkerVersion: "v0.1.0", OS: "linux", Architecture: "arm64", KernelVersion: "6.12.1",
		Capabilities: []string{"docker", "exec", "files"},
		Capacity:     platform.RemoteWorkerCapacity{CPUMillis: 4000, MemoryBytes: 8 << 30, DiskBytes: 40 << 30},
	}
	result, err := client.HeartbeatRemoteWorker(context.Background(), "tenant-alpha", "project-alpha", "enrollment-alpha", "request-heartbeat", request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.Command == nil || result.Value.Command.CommandID != "command-alpha" || result.Value.Command.DesiredState != "drained" {
		t.Fatalf("command=%#v", result.Value.Command)
	}
	var body map[string]any
	if json.Unmarshal(seen.Body, &body) != nil || body["observedState"] != "active" || seen.Headers["X-Request-ID"] != "request-heartbeat" || seen.Headers["Authorization"] != "" {
		t.Fatalf("request=%#v body=%#v", seen, body)
	}
}
