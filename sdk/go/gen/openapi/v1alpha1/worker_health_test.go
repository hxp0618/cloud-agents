package v1alpha1

import (
	"context"
	"strings"
	"testing"
)

func TestAdminWorkerHealthClientAuthority(t *testing.T) {
	body := `{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"WorkerHealthObservation","tenantId":"tenant-alpha","projectId":"project-alpha","workerId":"lease-alpha","generation":2,"resourceVersion":"3","state":"serving","checkedAt":"2026-09-05T12:00:00Z"}`
	calls := 0
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		calls++
		if request.Method != "GET" || request.Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/workers/lease-alpha/health?expectedGeneration=2" {
			t.Fatalf("request=%#v", request)
		}
		return Response{Status: 200, Body: []byte(body)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.GetAdminWorkerHealth(context.Background(), "tenant-alpha", "project-alpha", "lease-alpha", "request-worker-health", 2)
	if err != nil || value.Value.State != "serving" {
		t.Fatalf("value=%#v err=%v", value, err)
	}
	body = strings.Replace(body, "tenant-alpha", "tenant-other", 1)
	if _, err := client.GetAdminWorkerHealth(context.Background(), "tenant-alpha", "project-alpha", "lease-alpha", "request-worker-health", 2); err == nil {
		t.Fatal("accepted cross-tenant observation")
	}
	for _, generation := range []int64{0, 9007199254740992} {
		if _, err := client.GetAdminWorkerHealth(context.Background(), "tenant-alpha", "project-alpha", "lease-alpha", "request-worker-health", generation); err == nil {
			t.Fatal("accepted invalid generation")
		}
	}
	if _, err := client.GetAdminWorkerHealth(context.Background(), "tenant-alpha", "project-alpha", "", "request-worker-health", 2); err == nil {
		t.Fatal("accepted empty worker ID")
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
