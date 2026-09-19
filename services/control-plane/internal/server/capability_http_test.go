package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type capabilityVerifierFake struct{ fail string }

func (fake *capabilityVerifierFake) Verify(_ string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	if request.RequiredPermission == fake.fail {
		return nil, errors.New("denied")
	}
	return &authn.VerifiedPrincipal{}, nil
}

type capabilityStoreFake struct {
	mcp                platform.McpServer
	skill              platform.SkillBundle
	mcpList, skillList int
}

func (fake *capabilityStoreFake) CreateMcpServer(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _ string, _ platform.McpServerCreateRequest) (platform.McpServer, error) {
	return fake.mcp, nil
}
func (fake *capabilityStoreFake) RevokeMcpServer(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _, _ string, _ platform.McpServerRevokeRequest) (platform.McpServer, error) {
	fake.mcp.Spec.Status = "revoked"
	fake.mcp.Spec.RevokedAt = "2026-09-14T00:00:00Z"
	return fake.mcp, nil
}
func (fake *capabilityStoreFake) GetMcpServer(context.Context, string, *authn.VerifiedPrincipal, string, string) (platform.McpServer, error) {
	return fake.mcp, nil
}
func (fake *capabilityStoreFake) ListMcpServers(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.McpServerPage, error) {
	fake.mcpList++
	return postgres.McpServerPage{Servers: []platform.McpServer{fake.mcp}, NextServerID: fake.mcp.Metadata.UID}, nil
}
func (fake *capabilityStoreFake) CreateSkillBundle(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _ string, _ platform.SkillBundleCreateRequest) (platform.SkillBundle, error) {
	return fake.skill, nil
}
func (fake *capabilityStoreFake) RevokeSkillBundle(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _, _ string, _ platform.SkillBundleRevokeRequest) (platform.SkillBundle, error) {
	fake.skill.Spec.Status = "revoked"
	fake.skill.Spec.RevokedAt = "2026-09-14T00:00:00Z"
	return fake.skill, nil
}
func (fake *capabilityStoreFake) GetSkillBundle(context.Context, string, *authn.VerifiedPrincipal, string, string) (platform.SkillBundle, error) {
	return fake.skill, nil
}
func (fake *capabilityStoreFake) ListSkillBundles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.SkillBundlePage, error) {
	fake.skillList++
	return postgres.SkillBundlePage{Bundles: []platform.SkillBundle{fake.skill}, NextBundleID: fake.skill.Metadata.UID}, nil
}

func TestCapabilityHTTPServerOpaqueLifecycleAndScope(t *testing.T) {
	now := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	meta := func(id string) common.ResourceMetadata {
		return common.ResourceMetadata{UID: id, Name: id, TenantRef: common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: "tenant-alpha"}, ResourceVersion: "1", CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	}
	store := &capabilityStoreFake{
		mcp:   platform.McpServer{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "McpServer", Metadata: meta("mcp-alpha")}, Spec: platform.McpServerSpec{ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: "project-alpha"}, Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64), Transport: "sse", ConnectionRef: "connection-alpha", CredentialRef: "credential-alpha", NetworkPolicyRef: "network-alpha", Permissions: []string{"tools.read"}, Status: "active"}},
		skill: platform.SkillBundle{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "SkillBundle", Metadata: meta("skill-alpha")}, Spec: platform.SkillBundleSpec{ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: "project-alpha"}, Version: "v1", Digest: "sha256:" + strings.Repeat("b", 64), SourceRef: "source-alpha", SignatureRef: "signature-alpha", SigningKeyID: "key-alpha", CompatibleProviders: []string{"codex", "pi"}, MountReadOnly: true, Status: "active"}},
	}
	verifier := &capabilityVerifierFake{}
	handler, err := NewCapabilityHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer token")
		r.Header.Set("X-Request-ID", "request-capability")
		if method == http.MethodPost {
			r.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if got := request(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/mcp-servers", `{"serverId":"mcp-alpha","version":"v1","digest":"sha256:`+strings.Repeat("a", 64)+`","transport":"sse","connectionRef":"connection-alpha","credentialRef":"credential-alpha","networkPolicyRef":"network-alpha","permissions":["tools.read"]}`); got.Code != http.StatusCreated || !strings.Contains(got.Body.String(), `"kind":"McpServer"`) || strings.Contains(got.Body.String(), "token") {
		t.Fatalf("create status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/mcp-servers/mcp-alpha", ""); got.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/mcp-servers?pageSize=1", ""); got.Code != http.StatusOK || store.mcpList != 1 || !strings.Contains(got.Body.String(), "nextPageToken") {
		t.Fatalf("list status=%d calls=%d body=%s", got.Code, store.mcpList, got.Body.String())
	}
	if got := request(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/mcp-servers/mcp-alpha:revoke", `{"expectedResourceVersion":"1","reasonCode":"security"}`); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"status":"revoked"`) {
		t.Fatalf("revoke status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodPost, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/skill-bundles", `{"bundleId":"skill-alpha","version":"v1","digest":"sha256:`+strings.Repeat("b", 64)+`","sourceRef":"source-alpha","signatureRef":"signature-alpha","signingKeyId":"key-alpha","compatibleProviders":["codex","pi"]}`); got.Code != http.StatusCreated || !strings.Contains(got.Body.String(), `"kind":"SkillBundle"`) {
		t.Fatalf("skill create status=%d body=%s", got.Code, got.Body.String())
	}
	verifier.fail = "mcp-servers.list"
	if got := request(http.MethodGet, "/v1/admin/tenants/tenant-alpha/projects/project-alpha/mcp-servers", ""); got.Code != http.StatusForbidden {
		t.Fatalf("scope status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestCapabilityAdminPathRejectsUnknownShape(t *testing.T) {
	if HandlesCapabilityAdminPath("/v1/admin/tenants/t/projects/p/mcp-servers/mcp:delete") {
		t.Fatal("unknown action accepted")
	}
}
