package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real generated Admin/User clients, HTTP authorization, PostgreSQL RLS/functions,
// immutable RuntimeProfile lifecycle and durable Operation/outbox acceptance.
// The ready Docker Target is a SQL fixture; no container or Controller is started.
func TestFoundationRuntimeProfilePostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	if runtimeURL == "" || ownerURL == "" {
		t.Skip("isolated foundation RuntimeProfile PostgreSQL environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal("runtime pool unavailable")
	}
	defer runtimePool.Close()
	ownerConfig, err := pgxpool.ParseConfig(ownerURL)
	if err != nil {
		t.Fatal("owner pool configuration invalid")
	}
	ownerConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET ROLE cloud_agents_migration_owner")
		return err
	}
	owner, err := pgxpool.NewWithConfig(ctx, ownerConfig)
	if err != nil {
		t.Fatal("owner pool unavailable")
	}
	defer owner.Close()
	var existing int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.runtime_profiles) +
		(SELECT count(*) FROM cloud_agents.workspaces) +
		(SELECT count(*) FROM cloud_agents.sandbox_sessions)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("requires an empty disposable fixture: count=%d err=%v", existing, err)
	}

	verifier, adminToken, userToken := foundationVerifierAndTokens(t)
	store, err := postgres.NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewFoundationHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	admin, err := api.NewHTTPClientWithClient(httpServer.URL, adminToken, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	user, err := api.NewHTTPClientWithClient(httpServer.URL, userToken, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	successImage := os.Getenv("CLOUD_AGENTS_FOUNDATION_SUCCESS_IMAGE_URI")
	if successImage == "" {
		successImage = "registry.invalid/runtime@sha256:" + strings.Repeat("a", 64)
	}
	successImageParts := strings.Split(successImage, "@")
	if len(successImageParts) != 2 {
		t.Fatal("success image must be digest-pinned")
	}
	release := successImageParts[1]
	create := platform.RuntimeProfileCreateRequest{
		ProfileID: "profile", ProfileName: "profile", Version: 1,
		Description: "No-agent retained workspace", TargetID: "target",
		ImageURI: successImage, ReleaseDigest: release,
		CPUMillis: 500, MemoryBytes: 536870912,
	}
	if _, err := platform.EncodeRuntimeProfileCreateRequestJSON(create); err != nil {
		t.Fatalf("generated profile request validation: %v", err)
	}
	created, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-create", "profile-create-key", create)
	if err != nil || created.Value.Spec.Status != "draft" || created.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create profile: value=%+v err=%v", created.Value, err)
	}
	if _, err := user.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-user-denied", "profile-user-denied-key", create); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user Admin status=%d err=%v", clientStatus(err), err)
	}
	published, err := admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "profile", 1, "request-publish", "profile-publish-key", platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "1"})
	if err != nil || published.Value.Spec.Status != "published" || published.Value.Metadata.ResourceVersion != "2" {
		t.Fatalf("publish profile: value=%+v err=%v", published.Value, err)
	}
	failureImage := os.Getenv("CLOUD_AGENTS_FOUNDATION_FAILURE_IMAGE_URI")
	if failureImage != "" {
		parts := strings.Split(failureImage, "@")
		if len(parts) != 2 {
			t.Fatal("failure image must be digest-pinned")
		}
		failureCreate := create
		failureCreate.ProfileID = "profile-failure"
		failureCreate.ProfileName = "profile-failure"
		failureCreate.Description = "Controller failure compensation"
		failureCreate.ImageURI = failureImage
		failureCreate.ReleaseDigest = parts[1]
		failureCreated, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-failure-create", "profile-failure-create-key", failureCreate)
		if err != nil || failureCreated.Value.Spec.Status != "draft" {
			t.Fatalf("create failure profile: value=%+v err=%v", failureCreated.Value, err)
		}
		failurePublished, err := admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "profile-failure", 1, "request-failure-publish", "profile-failure-publish-key", platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "1"})
		if err != nil || failurePublished.Value.Spec.Status != "published" {
			t.Fatalf("publish failure profile: value=%+v err=%v", failurePublished.Value, err)
		}
	}
	page, err := user.ListRuntimeProfiles(ctx, "tenant", "project", "request-public-list", 50, "")
	expectedProfiles := 1
	if failureImage != "" {
		expectedProfiles = 2
	}
	hasPrimaryProfile := false
	for _, profile := range page.Value.RuntimeProfiles {
		hasPrimaryProfile = hasPrimaryProfile || profile.ProfileID == "profile"
	}
	if err != nil || len(page.Value.RuntimeProfiles) != expectedProfiles || !hasPrimaryProfile {
		t.Fatalf("public profiles: value=%+v err=%v", page.Value, err)
	}
	assertFoundationPublicRedaction(t, ctx, httpServer.URL, userToken)

	sandboxRequest := platform.SandboxSessionCreateRequest{
		WorkspaceID: "workspace", WorkspaceName: "workspace", SandboxID: "sandbox",
		RuntimeProfileID: "profile", RuntimeProfileVersion: 1,
	}
	sandbox, err := user.CreateSandbox(ctx, "tenant", "project", "request-sandbox", "sandbox-create-key", sandboxRequest)
	if err != nil || sandbox.Value.ObservedState != "pending" || sandbox.Value.OperationID == "" {
		t.Fatalf("create sandbox: value=%+v err=%v", sandbox.Value, err)
	}
	terminalRequest := sandboxRequest
	terminalRequest.WorkspaceID = "workspace-terminal"
	terminalRequest.WorkspaceName = "workspace-terminal"
	terminalRequest.SandboxID = "sandbox-terminal"
	if failureImage != "" {
		terminalRequest.RuntimeProfileID = "profile-failure"
	}
	terminal, err := user.CreateSandbox(ctx, "tenant", "project", "request-sandbox-terminal", "sandbox-terminal-key", terminalRequest)
	if err != nil || terminal.Value.ObservedState != "pending" || terminal.Value.OperationID == "" {
		t.Fatalf("create terminal sandbox fixture: value=%+v err=%v", terminal.Value, err)
	}
	if _, err := user.ListAdminSandboxSessions(ctx, "tenant", "project", "request-user-sandbox-denied", 50, ""); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user Admin sandbox status=%d err=%v", clientStatus(err), err)
	}
	adminSandboxes, err := admin.ListAdminSandboxSessions(ctx, "tenant", "project", "request-admin-sandboxes", 50, "")
	if err != nil || len(adminSandboxes.Value.SandboxSessions) != 2 {
		t.Fatalf("Admin sandbox list: value=%+v err=%v", adminSandboxes.Value, err)
	}
	adminSandbox, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-admin-sandbox")
	if err != nil || adminSandbox.Value.Spec.WorkspaceID != "workspace" || adminSandbox.Value.Spec.RuntimeProfileID != "profile" ||
		adminSandbox.Value.Spec.ObservedState != "pending" || adminSandbox.Value.Metadata.ResourceVersion != "0" {
		t.Fatalf("Admin sandbox detail: value=%+v err=%v", adminSandbox.Value, err)
	}
	assertAdminSandboxRedaction(t, ctx, httpServer.URL, adminToken)
	disabled, err := admin.DisableAdminRuntimeProfile(ctx, "tenant", "project", "profile", 1, "request-disable", "profile-disable-key", platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "2"})
	if err != nil || disabled.Value.Spec.Status != "disabled" || disabled.Value.Metadata.ResourceVersion != "3" {
		t.Fatalf("disable profile: value=%+v err=%v", disabled.Value, err)
	}
	if failureImage != "" {
		failureDisabled, err := admin.DisableAdminRuntimeProfile(ctx, "tenant", "project", "profile-failure", 1, "request-failure-disable", "profile-failure-disable-key", platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: "2"})
		if err != nil || failureDisabled.Value.Spec.Status != "disabled" {
			t.Fatalf("disable failure profile: value=%+v err=%v", failureDisabled.Value, err)
		}
	}
	replay, err := user.CreateSandbox(ctx, "tenant", "project", "request-sandbox-replay", "sandbox-create-key", sandboxRequest)
	if err != nil || replay.Value.OperationID != sandbox.Value.OperationID {
		t.Fatalf("sandbox replay after disable: value=%+v err=%v", replay.Value, err)
	}
	sandboxRequest.SandboxID = "sandbox-disabled"
	sandboxRequest.WorkspaceID = "workspace-disabled"
	if _, err := user.CreateSandbox(ctx, "tenant", "project", "request-sandbox-disabled", "sandbox-disabled-key", sandboxRequest); clientStatus(err) != http.StatusConflict {
		t.Fatalf("disabled profile admission status=%d err=%v", clientStatus(err), err)
	}
	page, err = user.ListRuntimeProfiles(ctx, "tenant", "project", "request-public-empty", 50, "")
	if err != nil || len(page.Value.RuntimeProfiles) != 0 {
		t.Fatalf("disabled profile remained public: value=%+v err=%v", page.Value, err)
	}

	var profiles, activities, operations, outbox, workspaces, sandboxes int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.runtime_profiles),
		(SELECT count(*) FROM cloud_agents.runtime_profile_activity),
		(SELECT count(*) FROM cloud_agents.platform_operations WHERE state='pending'),
		(SELECT count(*) FROM cloud_agents.outbox_events WHERE state='pending'),
		(SELECT count(*) FROM cloud_agents.workspaces),
		(SELECT count(*) FROM cloud_agents.sandbox_sessions)`).Scan(
		&profiles, &activities, &operations, &outbox, &workspaces, &sandboxes,
	); err != nil {
		t.Fatal(err)
	}
	expectedActivities := expectedProfiles * 3
	if profiles != expectedProfiles || activities != expectedActivities || operations != 2 || outbox != 2 || workspaces != 2 || sandboxes != 2 {
		t.Fatalf("durable closure profiles=%d activity=%d operations=%d outbox=%d workspaces=%d sandboxes=%d", profiles, activities, operations, outbox, workspaces, sandboxes)
	}
	if _, err := runtimePool.Exec(ctx, `SELECT cloud_agents.accept_foundation_intent_v1(
		'project','direct','direct','direct','target','direct','registry.invalid/runtime@`+release+`',500,536870912,
		'direct-operation','direct-event','sha256:`+strings.Repeat("b", 64)+`','direct-runtime-key','sha256:`+strings.Repeat("c", 64)+`')`); pgErrorCode(err) != "42501" {
		t.Fatalf("runtime bypass was not denied: code=%s err=%v", pgErrorCode(err), err)
	}
	t.Log("real Admin/User generated clients, HTTP403, RuntimeProfile lifecycle, Sandbox operational projection/redaction, RLS-backed durable acceptance, disable/replay and direct-authority denial passed; no Controller or Docker runtime claimed")
}

func assertFoundationPublicRedaction(t *testing.T, ctx context.Context, baseURL, token string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/tenants/tenant/projects/project/runtime-profiles?pageSize=50", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "request-public-raw")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("public response status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	for _, forbidden := range []string{"targetId", "imageUri", "releaseDigest", "endpoint", "credentialRef", "providerCredentialRef", "fixture-only", "127.0.0.1"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("public response disclosed %q: %s", forbidden, body)
		}
	}
}

func assertAdminSandboxRedaction(t *testing.T, ctx context.Context, baseURL, token string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/admin/tenants/tenant/projects/project/sandbox-sessions/sandbox", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "request-admin-sandbox-raw")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("Admin sandbox response status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	for _, forbidden := range []string{"imageUri", "releaseDigest", "endpoint", "credentialRef", "providerCredentialRef", "prompt", "artifact", "fileContent", "registry.invalid"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("Admin sandbox response disclosed %q: %s", forbidden, body)
		}
	}
}

func clientStatus(err error) int {
	var failure *api.ClientError
	if errors.As(err, &failure) {
		return failure.Status
	}
	return 0
}

func pgErrorCode(err error) string {
	var failure *pgconn.PgError
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

func foundationVerifierAndTokens(t *testing.T) (*authn.ConfiguredVerifier, string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	jwk, err := json.Marshal(map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": "foundation-key",
		"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), "use": "sig",
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := authn.NewConfiguredVerifier(authn.ConfiguredVerifierConfig{
		Issuer: "https://foundation.test", Audience: "https://foundation.test/control-plane",
		Generation: 1, SecurityEpoch: 1, NotBefore: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		Keys:  []authn.ConfiguredVerifierKey{{JWK: jwk, Enabled: true, NotBefore: now.Add(-time.Minute).Unix(), NotAfter: now.Add(10 * time.Minute).Unix()}},
		Clock: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Invalidate)
	issue := func(id, scope string) string {
		t.Helper()
		header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "foundation-key", "typ": "at+jwt"})
		claims, _ := json.Marshal(map[string]any{
			"iss": "https://foundation.test", "sub": "foundation-admin", "aud": "https://foundation.test/control-plane",
			"exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(), "jti": id, "client_id": "foundation-test", "scope": scope,
			"https://schemas.cloud-agents.dev/claims/subject-kind":   "user",
			"https://schemas.cloud-agents.dev/claims/tenant-id":      "tenant",
			"https://schemas.cloud-agents.dev/claims/security-epoch": int64(1),
			"https://schemas.cloud-agents.dev/claims/token-profile":  "cloud-agents-access-token/v1",
		})
		protected := base64.RawURLEncoding.EncodeToString(header)
		payload := base64.RawURLEncoding.EncodeToString(claims)
		digest := sha256.Sum256([]byte(protected + "." + payload))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		return protected + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	}
	admin := issue("foundation-admin-token", "projects.act projects.get profiles.act profiles.create profiles.get profiles.list sandboxes.get sandboxes.list")
	user := issue("foundation-user-token", "environment-profiles.list environments.create projects.act projects.get")
	return verifier, admin, user
}
