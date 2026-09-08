package server

import (
	"bytes"
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
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
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
		WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc",
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
	sandboxID := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIFECYCLE_SANDBOX_ID")
	if sandboxID == "" {
		sandboxID = "sandbox"
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
	current, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", sandboxID, "request-lifecycle-get")
	if err != nil {
		t.Fatal(err)
	}
	if current.Value.Spec.Usage == nil || current.Value.Spec.Usage.AllocatedMilliseconds == "" ||
		current.Value.Spec.Usage.CPUMillisMilliseconds == "" || current.Value.Spec.Usage.MemoryByteMilliseconds == "" ||
		current.Value.Spec.Usage.CheckpointedAt == "" {
		t.Fatalf("Admin sandbox usage projection = %+v", current.Value.Spec.Usage)
	}
	compute := "delete"
	if action == "rebuild" {
		compute = "create"
	}
	body := platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: current.Value.Spec.Generation, ExpectedResourceVersion: current.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: sandboxID, ComputeDisposition: compute, WorkspaceDisposition: "retain",
	}
	call := func(client *api.Client, requestID, key string, request platform.SandboxSessionLifecycleRequest) (api.SandboxSessionLifecycleOperationResult, error) {
		if action == "stop" {
			return client.StopAdminSandboxSession(ctx, "tenant", "project", sandboxID, requestID, key, request)
		}
		return client.RebuildAdminSandboxSession(ctx, "tenant", "project", sandboxID, requestID, key, request)
	}
	if _, err := call(user, "request-lifecycle-user-denied", "sandbox-lifecycle-user-key", body); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user lifecycle status=%d err=%v", clientStatus(err), err)
	}
	key := fmt.Sprintf("%s-lifecycle-%s-g%d-key", sandboxID, action, current.Value.Spec.Generation+1)
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
		(SELECT count(*) FROM cloud_agents.foundation_sandbox_activity WHERE sandbox_uid=$4 AND action=$1 AND operation_uid=$2),
		(SELECT count(*) FROM cloud_agents.coordination_audit_facts WHERE operation_id=$2 AND transition=$3)`,
		"sandbox."+action, operation.Value.OperationID, "sandbox."+action+".accept", sandboxID).Scan(&activities, &audits); err != nil || activities != 1 || audits != 1 {
		t.Fatalf("lifecycle durable activity=%d audit=%d err=%v", activities, audits, err)
	}
	encoded, _ := json.Marshal(map[string]any{
		"action": action, "operationId": operation.Value.OperationID,
		"generation": operation.Value.SandboxGeneration, "priorResourceVersion": body.ExpectedResourceVersion,
		"ordinaryUserStatus": 403, "staleFenceStatus": 409, "workspaceDisposition": "retain",
	})
	marker := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIFECYCLE_MARKER")
	if marker == "" {
		marker = "FOUNDATION_LIFECYCLE_API"
	}
	t.Logf("%s=%s", marker, encoded)
}

func TestFoundationWorkspaceSnapshotPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	if runtimeURL == "" || ownerURL == "" {
		t.Skip("isolated foundation Workspace snapshot PostgreSQL environment not configured")
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
	sandbox, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-snapshot-source")
	if err != nil || sandbox.Value.Spec.ObservedState != "stopped" || !sandbox.Value.Spec.WriterReleased {
		t.Fatalf("snapshot source=%+v err=%v", sandbox.Value.Spec, err)
	}
	request := platform.WorkspaceSnapshotCreateRequest{SnapshotID: "snapshot", SourceSandboxID: "sandbox", ExpectedSandboxGeneration: sandbox.Value.Spec.Generation, RetentionSeconds: 3600}
	if _, err := user.CreateAdminWorkspaceSnapshot(ctx, "tenant", "project", "request-snapshot-user", "snapshot-user-denied-key", request); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user snapshot status=%d err=%v", clientStatus(err), err)
	}
	created, err := admin.CreateAdminWorkspaceSnapshot(ctx, "tenant", "project", "request-snapshot-create", "workspace-snapshot-create-key", request)
	if err != nil || created.Value.Spec.Status != "pending" || created.Value.Spec.SourceWorkspaceID != sandbox.Value.Spec.WorkspaceID || created.Value.Spec.ConsistencyMode != "offline" {
		t.Fatalf("created snapshot=%+v err=%v", created.Value, err)
	}
	replay, err := admin.CreateAdminWorkspaceSnapshot(ctx, "tenant", "project", "request-snapshot-replay", "workspace-snapshot-create-key", request)
	if err != nil || replay.Value.Spec.OperationID != created.Value.Spec.OperationID {
		t.Fatalf("snapshot replay=%+v err=%v", replay.Value, err)
	}
	var snapshots, outbox, audits, writerFences int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.workspace_snapshots WHERE tenant_id='tenant' AND snapshot_uid='snapshot' AND status='pending'),
		(SELECT count(*) FROM cloud_agents.outbox_events WHERE tenant_id='tenant' AND aggregate_kind='workspaceSnapshot' AND aggregate_id='snapshot' AND state='pending'),
		(SELECT count(*) FROM cloud_agents.coordination_audit_facts WHERE tenant_id='tenant' AND operation_id=$1 AND transition='workspace.snapshot.accept'),
		(SELECT count(*) FROM cloud_agents.sandbox_sessions WHERE tenant_id='tenant' AND project_uid='project'
		  AND sandbox_uid='sandbox' AND desired_state='stopped' AND observed_state='stopped' AND NOT writer_released)`, created.Value.Spec.OperationID).Scan(&snapshots, &outbox, &audits, &writerFences); err != nil || snapshots != 1 || outbox != 1 || audits != 1 || writerFences != 1 {
		t.Fatalf("snapshot authority=%d/%d/%d fence=%d err=%v", snapshots, outbox, audits, writerFences, err)
	}
	rebuild := platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: sandbox.Value.Spec.Generation, ExpectedResourceVersion: sandbox.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "sandbox", ComputeDisposition: "create", WorkspaceDisposition: "retain",
	}
	if _, err := admin.RebuildAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-snapshot-fenced-rebuild", "snapshot-fenced-rebuild-key", rebuild); clientStatus(err) != http.StatusConflict {
		t.Fatalf("snapshot-fenced rebuild status=%d err=%v", clientStatus(err), err)
	}
	rawRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/admin/tenants/tenant/projects/project/workspace-snapshots/snapshot", nil)
	rawRequest.Header.Set("Authorization", "Bearer "+adminToken)
	rawRequest.Header.Set("X-Request-ID", "request-snapshot-redaction")
	rawResponse, err := httpServer.Client().Do(rawRequest)
	if err != nil {
		t.Fatal(err)
	}
	rawBody, _ := io.ReadAll(rawResponse.Body)
	rawResponse.Body.Close()
	if rawResponse.StatusCode != http.StatusOK {
		t.Fatalf("snapshot GET status=%d body=%s", rawResponse.StatusCode, rawBody)
	}
	for _, forbidden := range []string{"physicalSnapshot", "contentDigest", "sourcePhysical", "imageUri", "endpoint", "credentialRef", "providerCredentialRef", "prompt", "artifact", "fileContent"} {
		if strings.Contains(string(rawBody), forbidden) {
			t.Fatalf("snapshot response disclosed %q: %s", forbidden, rawBody)
		}
	}
	encoded, _ := json.Marshal(map[string]any{"snapshotId": "snapshot", "operationId": created.Value.Spec.OperationID,
		"sourceWorkspaceId": created.Value.Spec.SourceWorkspaceID, "ordinaryUserStatus": 403,
		"status": created.Value.Spec.Status, "adminContentRedacted": true, "idempotentReplay": true,
		"writerFenceReserved": true, "fencedRebuildStatus": 409})
	t.Logf("FOUNDATION_SNAPSHOT_API=%s", encoded)
}

func TestFoundationWorkspaceRestorePostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	if runtimeURL == "" || ownerURL == "" {
		t.Skip("isolated foundation Workspace restore PostgreSQL environment not configured")
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
	available, err := admin.GetAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-restore-source")
	if err != nil || available.Value.Spec.Status != "available" {
		t.Fatalf("available snapshot=%+v err=%v", available.Value, err)
	}
	sourceProfile, err := admin.GetAdminRuntimeProfile(ctx, "tenant", "project", "profile", 1, "request-restore-profile-source")
	if err != nil || sourceProfile.Value.Spec.TargetID == "" {
		t.Fatalf("restore source profile=%+v err=%v", sourceProfile.Value, err)
	}
	restoreProfileRequest := platform.RuntimeProfileCreateRequest{ProfileID: "profile-restore", ProfileName: "profile-restore", Version: 1,
		Description: "Snapshot restore target", TargetID: sourceProfile.Value.Spec.TargetID,
		WorkloadTrust: sourceProfile.Value.Spec.WorkloadTrust, IsolationRuntime: sourceProfile.Value.Spec.IsolationRuntime,
		NetworkPolicyRef: sourceProfile.Value.Spec.NetworkPolicyRef, ImageURI: sourceProfile.Value.Spec.ImageURI,
		ReleaseDigest: sourceProfile.Value.Spec.ReleaseDigest, CPUMillis: sourceProfile.Value.Spec.CPUMillis, MemoryBytes: sourceProfile.Value.Spec.MemoryBytes}
	createdProfile, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-restore-profile-create", "restore-profile-create-key", restoreProfileRequest)
	if err != nil {
		t.Fatalf("create restore profile=%+v err=%v", createdProfile.Value, err)
	}
	publishedProfile, err := admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "profile-restore", 1,
		"request-restore-profile-publish", "restore-profile-publish-key",
		platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: createdProfile.Value.Metadata.ResourceVersion})
	if err != nil || publishedProfile.Value.Spec.Status != "published" {
		t.Fatalf("publish restore profile=%+v err=%v", publishedProfile.Value, err)
	}
	version, err := strconv.ParseInt(available.Value.Metadata.ResourceVersion, 10, 64)
	if err != nil || version < 2 {
		t.Fatalf("snapshot version=%q err=%v", available.Value.Metadata.ResourceVersion, err)
	}
	restore := platform.WorkspaceSnapshotRestoreRequest{ExpectedSnapshotResourceVersion: available.Value.Metadata.ResourceVersion,
		WorkspaceID: "workspace-restored", WorkspaceName: "workspace-restored", SandboxID: "sandbox-restored",
		RuntimeProfileID: "profile-restore", RuntimeProfileVersion: 1, TTLSeconds: 3600}
	if _, err := user.RestoreAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-restore-user", "snapshot-restore-user-key", restore); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user restore status=%d err=%v", clientStatus(err), err)
	}
	stale := restore
	stale.ExpectedSnapshotResourceVersion = strconv.FormatInt(version-1, 10)
	if _, err := admin.RestoreAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-restore-stale", "snapshot-restore-stale-key", stale); clientStatus(err) != http.StatusConflict {
		t.Fatalf("stale restore status=%d err=%v", clientStatus(err), err)
	}
	restored, err := admin.RestoreAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-restore", "workspace-snapshot-restore-key", restore)
	if err != nil || restored.Value.WorkspaceID != restore.WorkspaceID || restored.Value.SandboxID != restore.SandboxID || restored.Value.ObservedState != "pending" {
		t.Fatalf("restored sandbox=%+v err=%v", restored.Value, err)
	}
	replay, err := admin.RestoreAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-restore-replay", "workspace-snapshot-restore-key", restore)
	if err != nil || replay.Value.OperationID != restored.Value.OperationID {
		t.Fatalf("restore replay=%+v err=%v", replay.Value, err)
	}
	_, err = admin.CleanupAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-restore-cleanup", "snapshot-restore-cleanup-key",
		platform.WorkspaceSnapshotCleanupRequest{ExpectedSnapshotResourceVersion: available.Value.Metadata.ResourceVersion,
			ConfirmedSnapshotID: "snapshot", ConfirmedSourceWorkspaceID: available.Value.Spec.SourceWorkspaceID, SnapshotDisposition: "delete"})
	if clientStatus(err) != http.StatusConflict {
		t.Fatalf("active restore cleanup status=%d err=%v", clientStatus(err), err)
	}
	var restores, audits, writerFences int
	if err := owner.QueryRow(ctx, `SELECT
(SELECT count(*) FROM cloud_agents.workspace_snapshot_restores WHERE tenant_id='tenant' AND project_uid='project'
  AND snapshot_uid='snapshot' AND workspace_uid='workspace-restored' AND sandbox_uid='sandbox-restored'),
(SELECT count(*) FROM cloud_agents.coordination_audit_facts WHERE tenant_id='tenant' AND operation_id=$1
  AND transition='workspace.snapshot.restore.accept'),
(SELECT count(*) FROM cloud_agents.sandbox_sessions WHERE tenant_id='tenant' AND project_uid='project'
  AND workspace_uid='workspace-restored' AND sandbox_uid='sandbox-restored' AND NOT writer_released)`, restored.Value.OperationID).Scan(&restores, &audits, &writerFences); err != nil || restores != 1 || audits != 1 || writerFences != 1 {
		t.Fatalf("restore authority=%d audit=%d fence=%d err=%v", restores, audits, writerFences, err)
	}
	raw, _ := json.Marshal(restored.Value)
	for _, forbidden := range []string{"contentDigest", "physicalSnapshot", "endpoint", "credentialRef", "providerCredentialRef", "prompt", "artifact", "fileContent"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("restore response disclosed %q: %s", forbidden, raw)
		}
	}
	encoded, _ := json.Marshal(map[string]any{"snapshotId": "snapshot", "operationId": restored.Value.OperationID,
		"workspaceId": restored.Value.WorkspaceID, "sandboxId": restored.Value.SandboxID,
		"ordinaryUserStatus": 403, "staleVersionStatus": 409, "idempotentReplay": true,
		"writerFenceReserved": true, "activeRestoreCleanupStatus": 409, "adminContentRedacted": true})
	t.Logf("FOUNDATION_RESTORE_API=%s", encoded)
}

func TestFoundationWorkspaceSnapshotCleanupPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	if runtimeURL == "" || ownerURL == "" {
		t.Skip("isolated foundation Workspace snapshot cleanup PostgreSQL environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	available, err := admin.GetAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-cleanup-source")
	if err != nil || available.Value.Spec.Status != "available" || available.Value.Spec.RetentionSeconds == nil ||
		*available.Value.Spec.RetentionSeconds != 3600 || available.Value.Spec.ExpiresAt == "" {
		t.Fatalf("cleanup source=%+v err=%v", available.Value, err)
	}
	page, err := admin.ListAdminWorkspaceSnapshots(ctx, "tenant", "project", "request-cleanup-list", 200, "")
	if err != nil || len(page.Value.WorkspaceSnapshots) != 1 || page.Value.WorkspaceSnapshots[0].Metadata.UID != "snapshot" {
		t.Fatalf("cleanup snapshot list=%+v err=%v", page.Value, err)
	}
	if os.Getenv("CLOUD_AGENTS_FOUNDATION_BROWSER_SCRIPT") != "" {
		verifySnapshotAdminBrowser(t, runtimePool)
	}
	var databaseVersion int64
	var databaseWorkspace, databaseStatus, physicalSnapshotID, contentDigest string
	var sizeBytes int64
	if err := owner.QueryRow(ctx, `SELECT resource_version, source_workspace_uid, status,
		physical_snapshot_uid, content_digest, size_bytes FROM cloud_agents.workspace_snapshots
		WHERE tenant_id='tenant' AND project_uid='project' AND snapshot_uid='snapshot'`).Scan(
		&databaseVersion, &databaseWorkspace, &databaseStatus, &physicalSnapshotID, &contentDigest, &sizeBytes,
	); err != nil || strconv.FormatInt(databaseVersion, 10) != available.Value.Metadata.ResourceVersion ||
		databaseWorkspace != available.Value.Spec.SourceWorkspaceID || databaseStatus != "available" ||
		physicalSnapshotID == "" || !strings.HasPrefix(contentDigest, "sha256:") || sizeBytes < 1 {
		t.Fatalf("cleanup database authority rv=%d workspace=%q status=%q physical=%q digest=%q size=%d err=%v api=%+v",
			databaseVersion, databaseWorkspace, databaseStatus, physicalSnapshotID, contentDigest, sizeBytes, err, available.Value)
	}
	body := platform.WorkspaceSnapshotCleanupRequest{ExpectedSnapshotResourceVersion: available.Value.Metadata.ResourceVersion,
		ConfirmedSnapshotID: "snapshot", ConfirmedSourceWorkspaceID: available.Value.Spec.SourceWorkspaceID,
		SnapshotDisposition: "delete"}
	if _, err := user.CleanupAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-cleanup-user", "snapshot-cleanup-user-key", body); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user cleanup status=%d err=%v", clientStatus(err), err)
	}
	version, _ := strconv.ParseInt(body.ExpectedSnapshotResourceVersion, 10, 64)
	stale := body
	stale.ExpectedSnapshotResourceVersion = strconv.FormatInt(version-1, 10)
	if _, err := admin.CleanupAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-cleanup-stale", "snapshot-cleanup-stale-key", stale); clientStatus(err) != http.StatusConflict {
		t.Fatalf("stale cleanup status=%d err=%v", clientStatus(err), err)
	}
	failed, err := admin.CleanupAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-cleanup-failure", "snapshot-cleanup-failure-key", body)
	if err != nil {
		t.Fatalf("failure-path acceptance: %v", err)
	}
	subject := "sha256:" + strings.Repeat("d", 64)
	claim, err := store.ClaimWorkspaceSnapshotCleanup(ctx, "cleanup-test", "cleanup-test-inc", "cleanup-test-token", 30, subject, "audit-cleanup-test-claim")
	if err != nil || !claim.Found || claim.Claim.OperationID != failed.Value.Spec.CleanupOperationID {
		t.Fatalf("cleanup failure claim=%+v err=%v", claim, err)
	}
	renewed, err := store.RenewWorkspaceSnapshotCleanup(ctx, claim.Claim, 60)
	if err != nil || !renewed.After(claim.Claim.ClaimExpiresAt) {
		t.Fatalf("cleanup renewal=%s err=%v", renewed, err)
	}
	if _, err := store.RenewWorkspaceSnapshotCleanup(ctx, claim.Claim, 60); err == nil {
		t.Fatal("stale cleanup renewal accepted")
	}
	claim.Claim.ClaimExpiresAt = renewed
	settled, err := store.SettleWorkspaceSnapshotCleanup(ctx, postgres.WorkspaceSnapshotCleanupSettlement{
		Claim: claim.Claim, Transition: "failed", StableErrorCode: "SNAPSHOT_OWNER_CONFLICT",
		SubjectDigest: subject, AuditFactID: "audit-cleanup-test-failed",
	})
	if err != nil || settled.OperationState != "failed" {
		t.Fatalf("cleanup terminal failure=%+v err=%v", settled, err)
	}
	failedState, err := admin.GetAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-cleanup-failed-state")
	if err != nil || failedState.Value.Spec.Status != "cleanup_failed" || failedState.Value.Spec.StableErrorCode != "SNAPSHOT_OWNER_CONFLICT" {
		t.Fatalf("cleanup failed state=%+v err=%v", failedState.Value, err)
	}
	body.ExpectedSnapshotResourceVersion = failedState.Value.Metadata.ResourceVersion
	accepted, err := admin.CleanupAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-cleanup", "workspace-snapshot-cleanup-key", body)
	if err != nil || accepted.Value.Spec.Status != "deleting" || accepted.Value.Spec.CleanupOperationID == "" ||
		accepted.Value.Spec.CleanupTrigger != "manual" {
		t.Fatalf("cleanup accepted=%+v err=%v", accepted.Value, err)
	}
	replay, err := admin.CleanupAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", "request-cleanup-replay", "workspace-snapshot-cleanup-key", body)
	if err != nil || replay.Value.Spec.CleanupOperationID != accepted.Value.Spec.CleanupOperationID {
		t.Fatalf("cleanup replay=%+v err=%v", replay.Value, err)
	}
	var pending, operations, audits int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.outbox_events WHERE tenant_id='tenant' AND operation_id=$1 AND state='pending'),
		(SELECT count(*) FROM cloud_agents.platform_operations WHERE tenant_id='tenant' AND operation_id=$1 AND state='pending'),
		(SELECT count(*) FROM cloud_agents.coordination_audit_facts WHERE tenant_id='tenant' AND operation_id=$1
		  AND transition='workspace.snapshot.cleanup.accept.manual')`, accepted.Value.Spec.CleanupOperationID).
		Scan(&pending, &operations, &audits); err != nil || pending != 1 || operations != 1 || audits != 1 {
		t.Fatalf("cleanup authority=%d/%d/%d err=%v", pending, operations, audits, err)
	}
	raw, _ := json.Marshal(accepted.Value)
	for _, forbidden := range []string{"physicalSnapshot", "contentDigest", "endpoint", "credentialRef", "providerCredentialRef", "prompt", "artifact", "fileContent"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("cleanup response disclosed %q: %s", forbidden, raw)
		}
	}
	encoded, _ := json.Marshal(map[string]any{"snapshotId": "snapshot", "operationId": accepted.Value.Spec.CleanupOperationID,
		"ordinaryUserStatus": 403, "staleVersionStatus": 409, "idempotentReplay": true,
		"failedOperationId": failed.Value.Spec.CleanupOperationID, "failedCleanupRetried": true, "renewalFenced": true,
		"status": accepted.Value.Spec.Status, "trigger": accepted.Value.Spec.CleanupTrigger, "adminContentRedacted": true})
	t.Logf("FOUNDATION_SNAPSHOT_CLEANUP_API=%s", encoded)
}

func TestFoundationWorkspaceSnapshotExpiryPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	if runtimeURL == "" {
		t.Skip("isolated foundation Workspace snapshot expiry PostgreSQL environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimePool.Close()
	verifier, adminToken, _ := foundationVerifierAndTokens(t)
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
	sandbox, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-expiry-source")
	if err != nil || sandbox.Value.Spec.ObservedState != "stopped" || !sandbox.Value.Spec.WriterReleased {
		t.Fatalf("expiry source=%+v err=%v", sandbox.Value, err)
	}
	request := platform.WorkspaceSnapshotCreateRequest{SnapshotID: "snapshot-expiring", SourceSandboxID: "sandbox",
		ExpectedSandboxGeneration: sandbox.Value.Spec.Generation, RetentionSeconds: 1}
	created, err := admin.CreateAdminWorkspaceSnapshot(ctx, "tenant", "project", "request-expiry-create", "workspace-snapshot-expiry-key", request)
	if err != nil || created.Value.Spec.Status != "pending" || created.Value.Spec.RetentionSeconds == nil ||
		*created.Value.Spec.RetentionSeconds != 1 || created.Value.Spec.ExpiresAt == "" {
		t.Fatalf("expiring snapshot=%+v err=%v", created.Value, err)
	}
	encoded, _ := json.Marshal(map[string]any{"snapshotId": created.Value.Metadata.UID,
		"operationId": created.Value.Spec.OperationID, "retentionSeconds": *created.Value.Spec.RetentionSeconds,
		"expiresAt": created.Value.Spec.ExpiresAt, "status": created.Value.Spec.Status})
	t.Logf("FOUNDATION_SNAPSHOT_EXPIRY_API=%s", encoded)
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
		"projects.act projects.get profiles.act profiles.create profiles.get profiles.list sandboxes.act sandboxes.get sandboxes.list snapshots.act snapshots.create snapshots.delete snapshots.get snapshots.list network-policies.update",
		"environment-profiles.list environments.create projects.act projects.get sandboxes.update",
	)
	return verifier, tokens[0], tokens[1]
}

// Optional real-browser check uses the same persisted resources and production Admin handlers.
func verifySnapshotAdminBrowser(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	verifier, tokens := foundationVerifierAndScopedTokens(t,
		"projects.act projects.get targets.list targets.get leases.list leases.get workers.list releases.list profiles.list profiles.get sandboxes.list snapshots.list snapshots.get snapshots.delete storage-policies.list network-policies.list quotas.get quotas.update audit.list operations.list",
	)
	store, err := postgres.NewDurableCoordinationService(pool)
	if err != nil {
		t.Fatal(err)
	}
	foundation, err := NewFoundationHTTPServer(verifier, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := NewAdminDeploymentTargetHTTPServer(verifier, store, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := NewAdminEnvironmentLeaseHTTPServer(verifier, store, nil, nil, nil, dockertarget.WorkerTrust{})
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := NewAdminEnvironmentProfileHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	releases, err := NewAdminWorkerReleaseHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	quota, err := NewProjectLeaseQuotaHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := NewStoragePolicyHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	network, err := NewNetworkPolicyHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	routes := []struct {
		handles func(string) bool
		handler http.Handler
	}{
		{HandlesFoundationPath, foundation}, {HandlesAdminEnvironmentLeasePath, leases},
		{HandlesAdminEnvironmentProfilePath, profiles}, {HandlesAdminWorkerReleasePath, releases},
		{HandlesProjectLeaseQuotaPath, quota}, {HandlesStoragePolicyPath, storage}, {HandlesNetworkPolicyPath, network},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, route := range routes {
			if route.handles(r.URL.Path) {
				route.handler.ServeHTTP(w, r)
				return
			}
		}
		targets.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := api.NewHTTPClientWithClient(server.URL, tokens[0], server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.GetAdminProjectLeaseQuota(ctx, "tenant", "project", "request-browser-quota-get"); clientStatus(err) == http.StatusNotFound {
		_, err = admin.SetAdminProjectLeaseQuota(ctx, "tenant", "project", "request-browser-quota-set", "browser-quota-set-key",
			platform.ProjectLeaseQuotaSetRequest{ExpectedResourceVersion: "0", MaxConcurrentLeases: 8,
				MaxCPUMillis: 16000, MaxMemoryBytes: 34359738368, MaxLeaseTTLSeconds: 3600})
	}
	if err != nil {
		t.Fatalf("prepare Admin browser quota: %v", err)
	}
	viteContext, stopVite := context.WithCancel(ctx)
	var viteLog bytes.Buffer
	vite := exec.CommandContext(viteContext, "./node_modules/.bin/vite")
	vite.Dir = "apps/admin-web"
	vite.Env = append(os.Environ(), "CLOUD_AGENTS_CONTROL_PLANE_URL="+server.URL)
	vite.Stdout = &viteLog
	vite.Stderr = &viteLog
	if err := vite.Start(); err != nil {
		t.Fatalf("start Admin Web: %v", err)
	}
	defer func() {
		stopVite()
		_ = vite.Wait()
	}()
	appURL := "http://127.0.0.1:4174"
	ready := false
	client := &http.Client{Timeout: time.Second}
	for attempt := 0; attempt < 200; attempt++ {
		response, err := client.Get(appURL)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("Admin Web did not start:\n%s", viteLog.String())
	}
	command := exec.CommandContext(ctx, os.Getenv("CLOUD_AGENTS_FOUNDATION_BROWSER_PYTHON"), os.Getenv("CLOUD_AGENTS_FOUNDATION_BROWSER_SCRIPT"))
	command.Env = append(os.Environ(), "SNAPSHOT_ADMIN_APP_URL="+appURL, "SNAPSHOT_ADMIN_TOKEN="+tokens[0])
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Admin browser check: %v\n%s", err, output)
	} else {
		t.Log(strings.TrimSpace(string(output)))
	}
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
