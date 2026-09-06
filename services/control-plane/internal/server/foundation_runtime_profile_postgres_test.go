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
	"fmt"
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
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
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
		(SELECT count(*) FROM cloud_agents.network_policies) +
		(SELECT count(*) FROM cloud_agents.workspaces) +
		(SELECT count(*) FROM cloud_agents.sandbox_sessions)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("requires an empty disposable fixture: count=%d err=%v", existing, err)
	}

	verifier, adminToken, userToken := foundationVerifierAndTokens(t)
	store, err := postgres.NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	foundationHandler, err := NewFoundationHTTPServer(verifier, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	networkPolicyHandler, err := NewNetworkPolicyHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if HandlesNetworkPolicyPath(request.URL.Path) {
			networkPolicyHandler.ServeHTTP(writer, request)
			return
		}
		foundationHandler.ServeHTTP(writer, request)
	})
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
	allowedEgress := []string{"api.openai.com"}
	if target := os.Getenv("CLOUD_AGENTS_FOUNDATION_ALLOWED_EGRESS"); target != "" {
		allowedEgress = []string{target}
	}
	policyRequest := platform.NetworkPolicySetRequest{
		ExpectedResourceVersion: "0", PolicyName: "network-restricted", UserSummary: "Approved outbound access",
		DefaultEgress: "restricted", AllowedEgress: allowedEgress, PreviewEnabled: true,
	}
	policy, err := admin.SetAdminNetworkPolicy(ctx, "tenant", "project", "network-restricted", "request-network-create", "network-create-key", policyRequest)
	if err != nil || policy.Value.Spec.DefaultEgress != "restricted" || policy.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create network policy: value=%+v err=%v", policy.Value, err)
	}
	policyRequest.ExpectedResourceVersion = "1"
	policyRequest.UserSummary = "Approved outbound access, verified"
	policy, err = admin.SetAdminNetworkPolicy(ctx, "tenant", "project", "network-restricted", "request-network-update", "network-update-key", policyRequest)
	if err != nil || policy.Value.Spec.UserSummary != policyRequest.UserSummary || policy.Value.Metadata.ResourceVersion != "2" {
		t.Fatalf("update network policy: value=%+v err=%v", policy.Value, err)
	}
	denyRequest := platform.NetworkPolicySetRequest{
		ExpectedResourceVersion: "0", PolicyName: "network-deny", UserSummary: "No outbound access",
		DefaultEgress: "deny", AllowedEgress: []string{}, PreviewEnabled: true,
	}
	if denied, err := admin.SetAdminNetworkPolicy(ctx, "tenant", "project", "network-deny", "request-network-deny", "network-deny-key", denyRequest); err != nil || denied.Value.Spec.DefaultEgress != "deny" {
		t.Fatalf("create deny network policy: value=%+v err=%v", denied.Value, err)
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
		NetworkPolicyRef: "network-restricted",
		ImageURI:         successImage, ReleaseDigest: release,
		CPUMillis: 500, MemoryBytes: 536870912,
	}
	if _, err := platform.EncodeRuntimeProfileCreateRequestJSON(create); err != nil {
		t.Fatalf("generated profile request validation: %v", err)
	}
	created, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-create", "profile-create-key", create)
	if err != nil || created.Value.Spec.Status != "draft" || created.Value.Metadata.ResourceVersion != "1" {
		t.Fatalf("create profile: value=%+v err=%v", created.Value, err)
	}
	policyRequest.ExpectedResourceVersion = "2"
	policyRequest.UserSummary = "Changed after profile reference"
	if _, err := admin.SetAdminNetworkPolicy(ctx, "tenant", "project", "network-restricted", "request-network-referenced", "network-referenced-key", policyRequest); clientStatus(err) != http.StatusConflict {
		t.Fatalf("referenced network policy status=%d err=%v", clientStatus(err), err)
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
		failureCreate.NetworkPolicyRef = "network-deny"
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
		RuntimeProfileID: "profile", RuntimeProfileVersion: 1, TTLSeconds: 120,
	}
	sandbox, err := user.CreateSandbox(ctx, "tenant", "project", "request-sandbox", "sandbox-create-key", sandboxRequest)
	if err != nil || sandbox.Value.ObservedState != "pending" || sandbox.Value.OperationID == "" || sandbox.Value.ExpiresAt == "" {
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
	for _, item := range adminSandboxes.Value.SandboxSessions {
		expectedPolicy := "network-restricted"
		if failureImage != "" && item.Metadata.UID == "sandbox-terminal" {
			expectedPolicy = "network-deny"
		}
		if item.Spec.TTLSeconds != 120 || item.Spec.ExpiresAt == "" || item.Spec.NetworkPolicyRef != expectedPolicy || item.Spec.NetworkPolicyEnforcement != "pending" {
			t.Fatalf("Admin sandbox TTL projection=%+v", item.Spec)
		}
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

func TestFoundationSandboxLifecyclePostgres(t *testing.T) {
	action := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIFECYCLE_ACTION")
	if action != "stop" && action != "rebuild" {
		t.Skip("foundation Sandbox lifecycle action is not configured")
	}
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	if runtimeURL == "" || ownerURL == "" {
		t.Skip("isolated foundation Sandbox lifecycle PostgreSQL environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimePool.Close()
	ownerConfig, err := pgxpool.ParseConfig(ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	ownerConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET ROLE cloud_agents_migration_owner")
		return err
	}
	owner, err := pgxpool.NewWithConfig(ctx, ownerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	verifier, adminToken, userToken := foundationVerifierAndTokens(t)
	store, err := postgres.NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewFoundationHTTPServer(verifier, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	admin, _ := api.NewHTTPClientWithClient(httpServer.URL, adminToken, httpServer.Client())
	user, _ := api.NewHTTPClientWithClient(httpServer.URL, userToken, httpServer.Client())
	current, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-lifecycle-get")
	if err != nil {
		t.Fatal(err)
	}
	compute := "delete"
	if action == "rebuild" {
		compute = "create"
	}
	body := platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: current.Value.Spec.Generation, ExpectedResourceVersion: current.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "sandbox", ComputeDisposition: compute, WorkspaceDisposition: "retain",
	}
	call := func(client *api.Client, requestID, key string, request platform.SandboxSessionLifecycleRequest) (api.SandboxSessionLifecycleOperationResult, error) {
		if action == "stop" {
			return client.StopAdminSandboxSession(ctx, "tenant", "project", "sandbox", requestID, key, request)
		}
		return client.RebuildAdminSandboxSession(ctx, "tenant", "project", "sandbox", requestID, key, request)
	}
	if _, err := call(user, "request-lifecycle-user-denied", "sandbox-lifecycle-user-key", body); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user lifecycle status=%d err=%v", clientStatus(err), err)
	}
	key := fmt.Sprintf("sandbox-lifecycle-%s-g%d-key", action, current.Value.Spec.Generation+1)
	operation, err := call(admin, "request-lifecycle-"+action, key, body)
	if err != nil || operation.Value.Action != "sandbox."+action || operation.Value.State != "pending" ||
		operation.Value.SandboxGeneration != current.Value.Spec.Generation+1 || operation.Value.WorkspaceDisposition != "retain" {
		t.Fatalf("lifecycle operation=%+v err=%v", operation.Value, err)
	}
	replay, err := call(admin, "request-lifecycle-replay", key, body)
	if err != nil || replay.Value.OperationID != operation.Value.OperationID {
		t.Fatalf("lifecycle replay=%+v err=%v", replay.Value, err)
	}
	if _, err := call(admin, "request-lifecycle-stale", key+"-stale", body); clientStatus(err) != http.StatusConflict {
		t.Fatalf("stale lifecycle fence status=%d err=%v", clientStatus(err), err)
	}
	var activities, audits int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.foundation_sandbox_activity WHERE sandbox_uid='sandbox' AND action=$1 AND operation_uid=$2),
		(SELECT count(*) FROM cloud_agents.coordination_audit_facts WHERE operation_id=$2 AND transition=$3)`,
		"sandbox."+action, operation.Value.OperationID, "sandbox."+action+".accept").Scan(&activities, &audits); err != nil || activities != 1 || audits != 1 {
		t.Fatalf("lifecycle durable activity=%d audit=%d err=%v", activities, audits, err)
	}
	encoded, _ := json.Marshal(map[string]any{
		"action": action, "operationId": operation.Value.OperationID,
		"generation": operation.Value.SandboxGeneration, "priorResourceVersion": body.ExpectedResourceVersion,
		"ordinaryUserStatus": 403, "staleFenceStatus": 409, "workspaceDisposition": "retain",
	})
	t.Logf("FOUNDATION_LIFECYCLE_API=%s", encoded)
}

func TestFoundationSandboxExecPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	credentialPath := os.Getenv("CLOUD_AGENTS_FOUNDATION_ACCESS_CREDENTIAL_DIRECTORY")
	proofDigest := os.Getenv("CLOUD_AGENTS_FOUNDATION_EXPECTED_PROOF_DIGEST")
	if runtimeURL == "" || ownerURL == "" || credentialPath == "" || proofDigest == "" {
		t.Skip("foundation Sandbox Exec environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	var generation int64
	if err := owner.QueryRow(ctx, `SELECT generation FROM cloud_agents.sandbox_sessions
		WHERE tenant_id='tenant' AND project_uid='project' AND sandbox_uid='sandbox'
		  AND desired_state='running' AND observed_state='running' AND runtime_state='Running'`).Scan(&generation); err != nil {
		t.Fatalf("running Sandbox unavailable: %v", err)
	}
	verifier, adminToken, userToken := foundationVerifierAndTokens(t)
	store, err := postgres.NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := opensandbox.NewCredentialDirectory(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewFoundationHTTPServer(verifier, store, credentials, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	admin, _ := api.NewHTTPClientWithClient(httpServer.URL, adminToken, httpServer.Client())
	user, _ := api.NewHTTPClientWithClient(httpServer.URL, userToken, httpServer.Client())

	stale := platform.SandboxExecRequest{ExpectedGeneration: generation - 1, Command: "true", TimeoutSeconds: 10}
	if _, err := user.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-exec-stale", stale); clientStatus(err) != http.StatusConflict {
		t.Fatalf("stale generation status=%d err=%v", clientStatus(err), err)
	}
	request := platform.SandboxExecRequest{ExpectedGeneration: generation, Command: "true", TimeoutSeconds: 10}
	if _, err := admin.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-exec-admin", request); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("Admin Exec status=%d err=%v", clientStatus(err), err)
	}
	request.Command = `head -c 1048577 /dev/zero | tr '\0' x`
	if _, err := user.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-exec-output-limit", request); clientStatus(err) != http.StatusRequestEntityTooLarge {
		t.Fatalf("output limit status=%d err=%v", clientStatus(err), err)
	}
	request.Command = "sha256sum /workspace/controller-proof.txt; printf exec-stderr >&2; exit 7"
	result, err := user.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-exec-success", request)
	if err != nil || result.Value.Generation != generation || result.Value.ExitCode != 7 ||
		!strings.Contains(result.Value.Stdout, proofDigest) || result.Value.Stderr != "exec-stderr" ||
		result.Value.ExecutionTimeMillis < 0 || result.Value.ExecutionTimeMillis > 65000 {
		t.Fatalf("Exec result=%+v err=%v", result.Value, err)
	}
	encoded, err := platform.EncodeSandboxExecResultResponseJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"credentialRef", "providerCredentialRef", "endpoint", "runtimeId", "OPEN-SANDBOX"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Exec response disclosed %q", forbidden)
		}
	}
	evidence, _ := json.Marshal(map[string]any{
		"generation": generation, "exitCode": result.Value.ExitCode, "proofDigestVerified": true,
		"executionTimeMillis": result.Value.ExecutionTimeMillis,
		"adminStatus":         403, "staleGenerationStatus": 409, "outputLimitStatus": 413,
		"responseInfrastructureRedacted": true,
	})
	t.Logf("FOUNDATION_EXEC_API=%s", evidence)
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
	verifier, tokens := foundationVerifierAndScopedTokens(t,
		"projects.act projects.get profiles.act profiles.create profiles.get profiles.list sandboxes.act sandboxes.get sandboxes.list network-policies.update",
		"environment-profiles.list environments.create projects.act projects.get sandboxes.update",
	)
	return verifier, tokens[0], tokens[1]
}

func foundationVerifierAndScopedTokens(t *testing.T, scopes ...string) (*authn.ConfiguredVerifier, []string) {
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
	tokens := make([]string, 0, len(scopes))
	for index, scope := range scopes {
		tokens = append(tokens, issue(fmt.Sprintf("foundation-token-%d", index), scope))
	}
	return verifier, tokens
}
