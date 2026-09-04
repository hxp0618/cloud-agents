package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	internalworkerrelease "github.com/hxp0618/cloud-agents/services/control-plane/internal/workerrelease"
)

type workerReleaseStoreFake struct {
	snapshot   internalworkerrelease.Snapshot
	input      internalworkerrelease.RegisterInput
	registered int
	listed     int
}

func (fake *workerReleaseStoreFake) RegisterWorkerRelease(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalworkerrelease.RegisterInput) (internalworkerrelease.Snapshot, error) {
	fake.registered++
	fake.input = input
	return fake.snapshot, nil
}

func (fake *workerReleaseStoreFake) ListWorkerReleases(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.WorkerReleasePage, error) {
	fake.listed++
	return postgres.WorkerReleasePage{WorkerReleases: []internalworkerrelease.Snapshot{fake.snapshot}}, nil
}

func TestAdminWorkerReleaseHTTPRegisterListAndRejectUserScope(t *testing.T) {
	now := time.Date(2026, time.September, 4, 7, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	store := &workerReleaseStoreFake{snapshot: internalworkerrelease.Snapshot{
		Scope:     internalworkerrelease.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		ReleaseID: "release-alpha", ReleaseName: "release-alpha", ImageRepository: "registry.example.test/cloud-agents/worker",
		ReleaseDigest: digest, PlatformVersion: "platform-v1", RuntimeVersion: "runtime-v1",
		CodexVersion: "codex-v1", ClaudeCodeVersion: "claude-v1", Architectures: []string{"linux/arm64"},
		Status: "approved", VerificationState: "attested", VerificationEvidenceDigest: digest,
		ResourceVersion: 1, CreatedAt: now, UpdatedAt: now, ApprovedAt: now,
	}}
	verifier := &environmentProfileVerifierFake{}
	handler, err := NewAdminWorkerReleaseHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"releaseId":"release-alpha","releaseName":"release-alpha","imageRepository":"registry.example.test/cloud-agents/worker","releaseDigest":"` + digest + `","platformVersion":"platform-v1","runtimeVersion":"runtime-v1","codexVersion":"codex-v1","claudeCodeVersion":"claude-v1","architectures":["linux/arm64"],"verificationEvidenceDigest":"` + digest + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/worker-releases", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-release-create")
	request.Header.Set("Idempotency-Key", "release-create-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	release, decodeErr := platformv1alpha1.DecodeWorkerReleaseResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusCreated || decodeErr != nil || release.Value.Spec.ReleaseDigest != digest || store.registered != 1 || store.input.Mutation.IdempotencyKey != "release-create-key" {
		t.Fatalf("status=%d decodeErr=%v body=%s store=%#v", response.Code, decodeErr, response.Body.String(), store)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/worker-releases?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-release-list")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	page, decodeErr := platformv1alpha1.DecodeWorkerReleasePageResponseJSON(response.Body.Bytes())
	if response.Code != http.StatusOK || decodeErr != nil || len(page.Value.WorkerReleases) != 1 || store.listed != 1 {
		t.Fatalf("status=%d decodeErr=%v body=%s", response.Code, decodeErr, response.Body.String())
	}

	verifier.failPermission = "releases.list"
	request = httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/worker-releases?pageSize=50", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Request-ID", "request-user-release-list")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || store.listed != 1 || !HandlesAdminWorkerReleasePath(request.URL.Path) {
		t.Fatalf("status=%d listed=%d", response.Code, store.listed)
	}
}
