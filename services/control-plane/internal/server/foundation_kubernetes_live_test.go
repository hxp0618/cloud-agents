package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/foundationcontroller"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real generated Admin/User clients, PostgreSQL authority, Kubernetes API,
// OpenSandbox BatchSandbox workload, retained PVC and lifecycle reconciliation.
func TestFoundationKubernetesLifecycle(t *testing.T) {
	phase := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_PHASE")
	if phase == "" {
		phase = "lifecycle"
	}
	if phase != "lifecycle" && phase != "fault-prepare" && phase != "fault-recover" {
		t.Fatalf("unknown Kubernetes lifecycle phase %q", phase)
	}
	started := time.Now()
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_OWNER_DATABASE_URL")
	credentialPath := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_CREDENTIAL_DIRECTORY")
	targetEndpoint := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_TARGET_ENDPOINT")
	credentialRef := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_CREDENTIAL_REF")
	imageURI := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_IMAGE_URI")
	if runtimeURL == "" || ownerURL == "" || credentialPath == "" || targetEndpoint == "" || credentialRef == "" || imageURI == "" {
		t.Skip("isolated foundation Kubernetes environment not configured")
	}
	_, releaseDigest, ok := strings.Cut(imageURI, "@")
	if !ok || !strings.HasPrefix(releaseDigest, "sha256:") {
		t.Fatal("Kubernetes image must be digest-pinned")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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
	var existing int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.deployment_targets) +
		(SELECT count(*) FROM cloud_agents.runtime_profiles) +
		(SELECT count(*) FROM cloud_agents.network_policies) +
		(SELECT count(*) FROM cloud_agents.workspaces) +
		(SELECT count(*) FROM cloud_agents.sandbox_sessions)`).Scan(&existing); err != nil || phase == "fault-recover" && existing != 5 || phase != "fault-recover" && existing != 0 {
		t.Fatalf("unexpected disposable fixture for %s: count=%d err=%v", phase, existing, err)
	}

	verifier, tokens := foundationVerifierAndScopedTokens(t,
		"projects.act projects.get targets.act targets.create targets.get targets.list profiles.act profiles.create profiles.get profiles.list sandboxes.act sandboxes.get sandboxes.list network-policies.update",
		"environment-profiles.list environments.create projects.act projects.get sandboxes.update",
	)
	store, err := postgres.NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	kubernetes, err := kubernetestarget.NewCredentialDirectory(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	sandboxCredentials, err := opensandbox.NewCredentialDirectory(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	targetHandler, err := NewAdminDeploymentTargetHTTPServer(verifier, store, nil, kubernetes, nil)
	if err != nil {
		t.Fatal(err)
	}
	networkHandler, err := NewNetworkPolicyHTTPServer(verifier, store)
	if err != nil {
		t.Fatal(err)
	}
	foundationHandler, err := NewFoundationHTTPServer(verifier, store, sandboxCredentials, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case HandlesAdminDeploymentTargetPath(request.URL.Path):
			targetHandler.ServeHTTP(writer, request)
		case HandlesNetworkPolicyPath(request.URL.Path):
			networkHandler.ServeHTTP(writer, request)
		default:
			foundationHandler.ServeHTTP(writer, request)
		}
	}))
	defer httpServer.Close()
	admin, _ := api.NewHTTPClientWithClient(httpServer.URL, tokens[0], httpServer.Client())
	user, _ := api.NewHTTPClientWithClient(httpServer.URL, tokens[1], httpServer.Client())
	controller, err := foundationcontroller.New(store, nil, kubernetes, sandboxCredentials)
	if err != nil {
		t.Fatal(err)
	}
	if phase == "fault-recover" {
		recoverFoundationKubernetesFault(t, ctx, started, owner, controller, admin, user)
		return
	}

	registered, err := admin.RegisterAdminDeploymentTarget(ctx, "tenant", "project", "request-target-create", "kubernetes-target-create-key", platform.DeploymentTargetRegisterRequest{
		TargetID: "target", TargetName: "target", TargetKind: "kubernetes", Endpoint: targetEndpoint, CredentialRef: credentialRef,
	})
	if err != nil || registered.Value.Spec.ObservedPhase != "unprobed" || registered.Value.Spec.Generation != 1 {
		t.Fatalf("register target: value=%+v err=%v", registered.Value, err)
	}
	if _, err := user.GetAdminDeploymentTarget(ctx, "tenant", "project", "target", "request-target-user-denied"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user Admin Target status=%d err=%v", clientStatus(err), err)
	}
	probed, err := admin.ProbeAdminDeploymentTarget(ctx, "tenant", "project", "target", "request-target-probe", "kubernetes-target-probe-key", platform.DeploymentTargetProbeRequest{ExpectedGeneration: 1})
	if err != nil || probed.Value.Spec.ObservedPhase != "ready" || probed.Value.Spec.APIVersion == "" ||
		probed.Value.Spec.EngineVersion == "" || probed.Value.Spec.OS == "" || probed.Value.Spec.Architecture == "" {
		t.Fatalf("probe target: value=%+v err=%v", probed.Value, err)
	}
	targets, err := admin.ListAdminDeploymentTargets(ctx, "tenant", "project", "request-target-list", 50, "")
	if err != nil || len(targets.Value.DeploymentTargets) != 1 || targets.Value.DeploymentTargets[0].Metadata.UID != "target" {
		t.Fatalf("list target: value=%+v err=%v", targets.Value, err)
	}

	policy, err := admin.SetAdminNetworkPolicy(ctx, "tenant", "project", "network-deny", "request-network-create", "kubernetes-network-create-key", platform.NetworkPolicySetRequest{
		ExpectedResourceVersion: "0", PolicyName: "network-deny", UserSummary: "No outbound access",
		DefaultEgress: "deny", AllowedEgress: []string{}, PreviewEnabled: true,
	})
	if err != nil || policy.Value.Spec.DefaultEgress != "deny" {
		t.Fatalf("create network policy: value=%+v err=%v", policy.Value, err)
	}
	createdProfile, err := admin.CreateAdminRuntimeProfile(ctx, "tenant", "project", "request-profile-create", "kubernetes-profile-create-key", platform.RuntimeProfileCreateRequest{
		ProfileID: "profile", ProfileName: "profile", Version: 1, Description: "Kubernetes retained workspace",
		WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc",
		TargetID: "target", NetworkPolicyRef: "network-deny", ImageURI: imageURI, ReleaseDigest: releaseDigest,
		CPUMillis: 500, MemoryBytes: 536870912,
	})
	if err != nil || createdProfile.Value.Spec.Status != "draft" {
		t.Fatalf("create profile: value=%+v err=%v", createdProfile.Value, err)
	}
	published, err := admin.PublishAdminRuntimeProfile(ctx, "tenant", "project", "profile", 1, "request-profile-publish", "kubernetes-profile-publish-key", platform.RuntimeProfileTransitionRequest{ExpectedResourceVersion: createdProfile.Value.Metadata.ResourceVersion})
	if err != nil || published.Value.Spec.Status != "published" {
		t.Fatalf("publish profile: value=%+v err=%v", published.Value, err)
	}
	profiles, err := user.ListRuntimeProfiles(ctx, "tenant", "project", "request-profile-list", 50, "")
	if err != nil || len(profiles.Value.RuntimeProfiles) != 1 || profiles.Value.RuntimeProfiles[0].ProfileID != "profile" {
		t.Fatalf("public profile list: value=%+v err=%v", profiles.Value, err)
	}
	assertFoundationPublicRedaction(t, ctx, httpServer.URL, tokens[1])

	createdSandbox, err := user.CreateSandbox(ctx, "tenant", "project", "request-sandbox-create", "kubernetes-sandbox-create-key", platform.SandboxSessionCreateRequest{
		WorkspaceID: "workspace", WorkspaceName: "workspace", SandboxID: "sandbox",
		RuntimeProfileID: "profile", RuntimeProfileVersion: 1, TTLSeconds: 600,
	})
	if err != nil || createdSandbox.Value.ObservedState != "pending" {
		t.Fatalf("create sandbox: value=%+v err=%v", createdSandbox.Value, err)
	}
	if phase == "fault-prepare" {
		prepareFoundationKubernetesFault(t, ctx, owner, store, kubernetes, sandboxCredentials)
		return
	}
	if worked, err := controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("reconcile create: worked=%v err=%v", worked, err)
	}
	running, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-sandbox-running")
	if err != nil || running.Value.Spec.ObservedState != "running" || running.Value.Spec.RuntimeState != "Running" ||
		running.Value.Spec.PhysicalVolumeID == "" || running.Value.Spec.RuntimeID == "" {
		t.Fatalf("running sandbox: value=%+v err=%v", running.Value, err)
	}
	assertAdminSandboxRedaction(t, ctx, httpServer.URL, tokens[0])
	proof := "cloud-agents-kubernetes-foundation"
	proofSum := sha256.Sum256([]byte(proof))
	proofDigest := hex.EncodeToString(proofSum[:])
	execRequest := platform.SandboxExecRequest{ExpectedGeneration: running.Value.Spec.Generation, Command: "printf '" + proof + "' > /workspace/kubernetes-proof.txt && sha256sum /workspace/kubernetes-proof.txt", TimeoutSeconds: 30}
	if _, err := admin.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-exec-admin-denied", execRequest); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("Admin Exec status=%d err=%v", clientStatus(err), err)
	}
	executed, err := user.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-exec-create-proof", execRequest)
	if err != nil || executed.Value.ExitCode != 0 || !strings.Contains(executed.Value.Stdout, proofDigest) {
		t.Fatalf("Exec create proof: value=%+v err=%v", executed.Value, err)
	}
	firstRuntimeID := running.Value.Spec.RuntimeID

	firstStop, err := admin.StopAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-sandbox-stop-first", "kubernetes-sandbox-stop-first-key", platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: running.Value.Spec.Generation, ExpectedResourceVersion: running.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "sandbox", ComputeDisposition: "delete", WorkspaceDisposition: "retain",
	})
	if err != nil || firstStop.Value.State != "pending" || firstStop.Value.SandboxGeneration != running.Value.Spec.Generation+1 {
		t.Fatalf("accept first stop: value=%+v err=%v", firstStop.Value, err)
	}
	if worked, err := controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("reconcile first stop: worked=%v err=%v", worked, err)
	}
	stoppedBeforeRebuild, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-sandbox-stopped-first")
	if err != nil || stoppedBeforeRebuild.Value.Spec.ObservedState != "stopped" || stoppedBeforeRebuild.Value.Spec.CleanupPhase != "complete" || !stoppedBeforeRebuild.Value.Spec.WriterReleased {
		t.Fatalf("first stopped sandbox: value=%+v err=%v", stoppedBeforeRebuild.Value, err)
	}

	rebuild, err := admin.RebuildAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-sandbox-rebuild", "kubernetes-sandbox-rebuild-key", platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: stoppedBeforeRebuild.Value.Spec.Generation, ExpectedResourceVersion: stoppedBeforeRebuild.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "sandbox", ComputeDisposition: "create", WorkspaceDisposition: "retain",
	})
	if err != nil || rebuild.Value.State != "pending" || rebuild.Value.SandboxGeneration != stoppedBeforeRebuild.Value.Spec.Generation+1 {
		t.Fatalf("accept rebuild: value=%+v err=%v", rebuild.Value, err)
	}
	if worked, err := controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("reconcile rebuild: worked=%v err=%v", worked, err)
	}
	rebuilt, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-sandbox-rebuilt")
	if err != nil || rebuilt.Value.Spec.ObservedState != "running" || rebuilt.Value.Spec.RuntimeID == firstRuntimeID ||
		rebuilt.Value.Spec.PhysicalVolumeID != running.Value.Spec.PhysicalVolumeID {
		t.Fatalf("rebuilt sandbox: value=%+v err=%v", rebuilt.Value, err)
	}
	if _, err := user.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-exec-stale", platform.SandboxExecRequest{ExpectedGeneration: running.Value.Spec.Generation, Command: "true", TimeoutSeconds: 10}); clientStatus(err) != http.StatusConflict {
		t.Fatalf("stale Exec status=%d err=%v", clientStatus(err), err)
	}
	execRequest.ExpectedGeneration = rebuilt.Value.Spec.Generation
	execRequest.Command = "sha256sum /workspace/kubernetes-proof.txt"
	rebuiltExec, err := user.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-exec-rebuilt-proof", execRequest)
	if err != nil || rebuiltExec.Value.ExitCode != 0 || !strings.Contains(rebuiltExec.Value.Stdout, proofDigest) {
		t.Fatalf("Exec rebuilt proof: value=%+v err=%v", rebuiltExec.Value, err)
	}

	finalStop, err := admin.StopAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-sandbox-stop-final", "kubernetes-sandbox-stop-final-key", platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: rebuilt.Value.Spec.Generation, ExpectedResourceVersion: rebuilt.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "sandbox", ComputeDisposition: "delete", WorkspaceDisposition: "retain",
	})
	if err != nil || finalStop.Value.State != "pending" || finalStop.Value.SandboxGeneration != rebuilt.Value.Spec.Generation+1 {
		t.Fatalf("accept final stop: value=%+v err=%v", finalStop.Value, err)
	}
	if worked, err := controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("reconcile stop: worked=%v err=%v", worked, err)
	}
	stopped, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-sandbox-stopped")
	if err != nil || stopped.Value.Spec.ObservedState != "stopped" || stopped.Value.Spec.CleanupPhase != "complete" || !stopped.Value.Spec.WriterReleased {
		t.Fatalf("stopped sandbox: value=%+v err=%v", stopped.Value, err)
	}
	volume := kubernetestarget.FoundationWorkspaceVolume{TenantID: "tenant", ProjectID: "project", TargetID: "target", WorkspaceID: "workspace"}
	volumeName, err := kubernetes.VerifyFoundationWorkspaceVolume(ctx, targetEndpoint, credentialRef, volume)
	if err != nil || volumeName != running.Value.Spec.PhysicalVolumeID {
		t.Fatalf("retained PVC: name=%q err=%v", volumeName, err)
	}

	var activities, operations, outbox, audits int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.foundation_sandbox_activity WHERE sandbox_uid='sandbox'),
		(SELECT count(*) FROM cloud_agents.platform_operations WHERE state='succeeded' AND operation_id IN (
			SELECT operation_uid FROM cloud_agents.foundation_sandbox_activity WHERE sandbox_uid='sandbox')),
		(SELECT count(*) FROM cloud_agents.outbox_events WHERE aggregate_id='sandbox' AND state='delivered'),
		(SELECT count(*) FROM cloud_agents.coordination_audit_facts WHERE operation_id IN (
			SELECT operation_uid FROM cloud_agents.foundation_sandbox_activity WHERE sandbox_uid='sandbox'))`).Scan(&activities, &operations, &outbox, &audits); err != nil {
		t.Fatal(err)
	}
	if activities != 3 || operations != 3 || outbox != 4 || audits < 9 {
		t.Fatalf("durable lifecycle closure activities=%d operations=%d outbox=%d audits=%d", activities, operations, outbox, audits)
	}
	evidence, _ := json.Marshal(map[string]any{
		"targetApiVersion": probed.Value.Spec.APIVersion, "targetEngineVersion": probed.Value.Spec.EngineVersion,
		"targetOS": probed.Value.Spec.OS, "targetArchitecture": probed.Value.Spec.Architecture,
		"ordinaryUserAdminStatus": 403, "staleGenerationStatus": 409, "adminExecStatus": 403,
		"firstRuntimeId": firstRuntimeID, "rebuiltRuntimeId": rebuilt.Value.Spec.RuntimeID,
		"workspaceDigest": proofDigest, "workspaceVolume": volumeName, "workspaceRetained": true,
		"finalObservedState": stopped.Value.Spec.ObservedState, "finalCleanupPhase": stopped.Value.Spec.CleanupPhase,
		"activities": activities, "operations": operations, "outbox": outbox, "audits": audits,
	})
	t.Logf("FOUNDATION_KUBERNETES=%s", evidence)
}

func prepareFoundationKubernetesFault(t *testing.T, ctx context.Context, owner *pgxpool.Pool, store *postgres.DurableCoordinationService, kubernetes *kubernetestarget.CredentialDirectory, sandboxCredentials *opensandbox.CredentialDirectory) {
	t.Helper()
	subject := sha256.Sum256([]byte("foundation-kubernetes-fault"))
	claimed, err := store.ClaimFoundationSandbox(ctx, postgres.FoundationSandboxClaimInput{
		TargetKind: "kubernetes", HolderID: "fault-prepare", HolderIncarnation: "fault-prepare-incarnation",
		ClaimToken: "fault-prepare-claim", LeaseSeconds: 1, SubjectDigest: "sha256:" + hex.EncodeToString(subject[:]), AuditFactID: "audit-kubernetes-fault-prepare",
	})
	if err != nil || claimed.DatabaseOutcome != postgres.DatabaseCommitted || !claimed.Found || claimed.Claim.SandboxID != "sandbox" {
		t.Fatalf("claim Kubernetes Sandbox: value=%+v err=%v", claimed, err)
	}
	result := foundationcontroller.ExecuteEffect(ctx, nil, kubernetes, sandboxCredentials, claimed.Claim)
	if result.Err != nil || result.RuntimeState != "Running" || result.RuntimeID == "" || result.VolumeName == "" {
		t.Fatalf("physical Kubernetes effect: value=%+v", result)
	}
	client, err := sandboxCredentials.Client(claimed.Claim.CredentialRef)
	if err != nil {
		t.Fatal(err)
	}
	identity := opensandbox.Identity{Tenant: claimed.Claim.TenantID, Project: claimed.Claim.ProjectID,
		Workspace: claimed.Claim.WorkspaceID, Sandbox: claimed.Claim.SandboxID, Operation: claimed.Claim.OperationID,
		Generation: claimed.Claim.SandboxGeneration, SpecDigest: claimed.Claim.SpecDigest}
	proof := "cloud-agents-kubernetes-fault"
	proofSum := sha256.Sum256([]byte(proof))
	executed, err := client.Exec(ctx, opensandbox.ExecInput{Identity: identity, RuntimeID: result.RuntimeID,
		Command: "printf '" + proof + "' > /workspace/kubernetes-fault-proof.txt && sha256sum /workspace/kubernetes-fault-proof.txt", Timeout: 30 * time.Second})
	if err != nil || executed.ExitCode != 0 || !strings.Contains(executed.Stdout, hex.EncodeToString(proofSum[:])) {
		t.Fatalf("write fault proof: value=%+v err=%v", executed, err)
	}
	var operationRows, deliveryAttempts int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.platform_operations WHERE tenant_id='tenant' AND operation_id=$1),
		(SELECT delivery_attempts FROM cloud_agents.outbox_events WHERE tenant_id='tenant' AND operation_id=$1)`, claimed.Claim.OperationID).Scan(&operationRows, &deliveryAttempts); err != nil || operationRows != 1 || deliveryAttempts != 1 {
		t.Fatalf("unsettled durable operation: rows=%d attempts=%d err=%v", operationRows, deliveryAttempts, err)
	}
	receipt, _ := json.Marshal(map[string]any{
		"runtimeId": result.RuntimeID, "volumeName": result.VolumeName, "operationId": claimed.Claim.OperationID,
		"specDigest": claimed.Claim.SpecDigest, "proofDigest": hex.EncodeToString(proofSum[:]),
		"claimExpiresAt": claimed.Claim.ClaimExpiresAt.UTC().Format(time.RFC3339Nano),
		"operationRows":  operationRows, "deliveryAttempts": deliveryAttempts,
	})
	t.Logf("FOUNDATION_KUBERNETES_FAULT_PREPARE=%s", receipt)
	// The test process exits without settlement; a fresh process must reap and adopt.
}

func recoverFoundationKubernetesFault(t *testing.T, ctx context.Context, started time.Time, owner *pgxpool.Pool, controller *foundationcontroller.Controller, admin, user *api.Client) {
	t.Helper()
	expectedRuntime := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_EXPECTED_RUNTIME_ID")
	expectedVolume := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_EXPECTED_VOLUME_NAME")
	expectedOperation := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_EXPECTED_OPERATION_ID")
	expectedDigest := os.Getenv("CLOUD_AGENTS_FOUNDATION_KUBERNETES_EXPECTED_PROOF_DIGEST")
	if expectedRuntime == "" || expectedVolume == "" || expectedOperation == "" || expectedDigest == "" {
		t.Fatal("Kubernetes fault recovery receipt is missing")
	}
	if worked, err := controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("reap expired Kubernetes claim: worked=%v err=%v", worked, err)
	}
	if worked, err := controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("adopt Kubernetes runtime: worked=%v err=%v", worked, err)
	}
	running, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-kubernetes-fault-running")
	if err != nil || running.Value.Spec.ObservedState != "running" || running.Value.Spec.RuntimeID != expectedRuntime ||
		running.Value.Spec.PhysicalVolumeID != expectedVolume {
		t.Fatalf("recovered Kubernetes Sandbox: value=%+v err=%v", running.Value, err)
	}
	executed, err := user.ExecSandbox(ctx, "tenant", "project", "sandbox", "request-kubernetes-fault-proof", platform.SandboxExecRequest{
		ExpectedGeneration: running.Value.Spec.Generation, Command: "sha256sum /workspace/kubernetes-fault-proof.txt", TimeoutSeconds: 30,
	})
	if err != nil || executed.Value.ExitCode != 0 || !strings.Contains(executed.Value.Stdout, expectedDigest) {
		t.Fatalf("read recovered fault proof: value=%+v err=%v", executed.Value, err)
	}

	const cycles = 64
	successLatency := make([]time.Duration, 0, cycles*3)
	deniedLatency := make([]time.Duration, 0, cycles)
	var stateDigest string
	var recoveryToFirstSuccess time.Duration
	for cycle := 0; cycle < cycles; cycle++ {
		prefix := "request-kubernetes-soak-" + fmt.Sprintf("%02d", cycle)
		targets, callErr := timedFoundationRequest(&successLatency, func() (api.DeploymentTargetPageResult, error) {
			return admin.ListAdminDeploymentTargets(ctx, "tenant", "project", prefix+"-targets", 50, "")
		})
		if callErr != nil {
			t.Fatalf("list Kubernetes targets cycle %d: %v", cycle, callErr)
		}
		if cycle == 0 {
			recoveryToFirstSuccess = time.Since(started)
		}
		profiles, callErr := timedFoundationRequest(&successLatency, func() (api.RuntimeProfilePageResult, error) {
			return admin.ListAdminRuntimeProfiles(ctx, "tenant", "project", prefix+"-profiles", 50, "")
		})
		if callErr != nil {
			t.Fatalf("list Kubernetes profiles cycle %d: %v", cycle, callErr)
		}
		sandbox, callErr := timedFoundationRequest(&successLatency, func() (api.AdminSandboxSessionResult, error) {
			return admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", prefix+"-sandbox")
		})
		if callErr != nil {
			t.Fatalf("get Kubernetes Sandbox cycle %d: %v", cycle, callErr)
		}
		_, denied := timedFoundationRequest(&deniedLatency, func() (api.DeploymentTargetPageResult, error) {
			return user.ListAdminDeploymentTargets(ctx, "tenant", "project", prefix+"-user-denied", 50, "")
		})
		if clientStatus(denied) != http.StatusForbidden {
			t.Fatalf("ordinary user Kubernetes Admin cycle %d status=%d err=%v", cycle, clientStatus(denied), denied)
		}
		encoded, marshalErr := json.Marshal([]any{targets.Value, profiles.Value, sandbox.Value})
		if marshalErr != nil || len(targets.Value.DeploymentTargets) != 1 || len(profiles.Value.RuntimeProfiles) != 1 || sandbox.Value.Spec.RuntimeID != expectedRuntime {
			t.Fatalf("Kubernetes soak state cycle %d: targets=%d profiles=%d runtime=%q err=%v", cycle, len(targets.Value.DeploymentTargets), len(profiles.Value.RuntimeProfiles), sandbox.Value.Spec.RuntimeID, marshalErr)
		}
		for _, forbidden := range []string{"apiKey", "privateKey", "BEGIN PRIVATE KEY", "prompt", "artifact", "fileContent", "kubernetes-fault-proof"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("Kubernetes Admin soak disclosed %q", forbidden)
			}
		}
		digest := sha256.Sum256(encoded)
		current := "sha256:" + hex.EncodeToString(digest[:])
		if stateDigest == "" {
			stateDigest = current
		} else if current != stateDigest {
			t.Fatalf("Kubernetes Admin state changed during soak: %s != %s", current, stateDigest)
		}
	}

	stoppedOperation, err := admin.StopAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-kubernetes-fault-stop", "kubernetes-fault-stop-key", platform.SandboxSessionLifecycleRequest{
		ExpectedGeneration: running.Value.Spec.Generation, ExpectedResourceVersion: running.Value.Metadata.ResourceVersion,
		ConfirmedSandboxID: "sandbox", ComputeDisposition: "delete", WorkspaceDisposition: "retain",
	})
	if err != nil || stoppedOperation.Value.State != "pending" {
		t.Fatalf("accept Kubernetes fault stop: value=%+v err=%v", stoppedOperation.Value, err)
	}
	if worked, err := controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("settle Kubernetes fault stop: worked=%v err=%v", worked, err)
	}
	stopped, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-kubernetes-fault-stopped")
	if err != nil || stopped.Value.Spec.ObservedState != "stopped" || stopped.Value.Spec.CleanupPhase != "complete" || !stopped.Value.Spec.WriterReleased {
		t.Fatalf("stopped Kubernetes fault Sandbox: value=%+v err=%v", stopped.Value, err)
	}
	var operationRows, deliveryAttempts int
	if err := owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.platform_operations WHERE tenant_id='tenant' AND operation_id=$1),
		(SELECT delivery_attempts FROM cloud_agents.outbox_events WHERE tenant_id='tenant' AND operation_id=$1)`, expectedOperation).Scan(&operationRows, &deliveryAttempts); err != nil || operationRows != 1 || deliveryAttempts != 2 {
		t.Fatalf("recovered durable operation: rows=%d attempts=%d err=%v", operationRows, deliveryAttempts, err)
	}
	receipt, _ := json.Marshal(map[string]any{
		"runtimeId": expectedRuntime, "volumeName": expectedVolume, "operationId": expectedOperation,
		"workspaceDigest": expectedDigest, "operationRows": operationRows, "deliveryAttempts": deliveryAttempts,
		"recoveryToFirstSuccessMilliseconds": durationMilliseconds(recoveryToFirstSuccess),
		"successLatencyMilliseconds":         foundationLatencySummary(successLatency),
		"deniedLatencyMilliseconds":          foundationLatencySummary(deniedLatency),
		"successfulRequests":                 len(successLatency), "deniedRequests": len(deniedLatency), "cycles": cycles,
		"stateDigest": stateDigest, "finalObservedState": stopped.Value.Spec.ObservedState, "finalCleanupPhase": stopped.Value.Spec.CleanupPhase,
	})
	t.Logf("FOUNDATION_KUBERNETES_FAULT_RECOVER=%s", receipt)
}
