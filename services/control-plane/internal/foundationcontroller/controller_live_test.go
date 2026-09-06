package foundationcontroller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type liveControllerEnvironment struct {
	controller      *Controller
	store           *postgres.DurableCoordinationService
	owner           *pgxpool.Pool
	sandbox         *opensandbox.Client
	dockerSocket    string
	dockerEndpoint  string
	sandboxEndpoint string
	sandboxKey      string
}

func TestLiveFoundationControllerRestart(t *testing.T) {
	phase := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_PHASE")
	if phase != "prepare" && phase != "recover" && phase != "stop" && phase != "rebuild" && phase != "ttl" && phase != "rebuild-final" {
		t.Skip("live foundation Controller phase is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	environment := newLiveControllerEnvironment(t, ctx)
	if phase == "prepare" {
		prepareLiveControllerRestart(t, ctx, environment)
		return
	}
	if phase == "recover" {
		recoverLiveControllerRestart(t, ctx, environment)
		return
	}
	if phase == "ttl" {
		ttlLiveController(t, ctx, environment)
		return
	}
	lifecycleLiveController(t, ctx, environment, phase)
}

func newLiveControllerEnvironment(t *testing.T, ctx context.Context) liveControllerEnvironment {
	t.Helper()
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_OWNER_DATABASE_URL")
	sandboxEndpoint := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_OPENSANDBOX_ENDPOINT")
	sandboxKey := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_OPENSANDBOX_KEY")
	dockerSocket := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_DOCKER_SOCKET")
	if runtimeURL == "" || ownerURL == "" || sandboxEndpoint == "" || sandboxKey == "" || dockerSocket == "" {
		t.Fatal("live foundation Controller environment is incomplete")
	}
	if info, err := os.Stat(dockerSocket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("Docker socket is unavailable: %v", err)
	}

	proxyTransport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", dockerSocket)
	}}
	proxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			request.URL.Scheme = "http"
			request.URL.Host = "docker"
			request.Host = "docker"
		},
		Transport: proxyTransport,
	}
	server := httptest.NewUnstartedServer(proxy)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(func() { server.Close(); proxyTransport.CloseIdleConnections() })

	root := t.TempDir()
	reference := filepath.Join(root, "fixture-only")
	if err := os.Mkdir(reference, 0o700); err != nil {
		t.Fatal(err)
	}
	certificate := server.TLS.Certificates[0]
	privateKey, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	for name, value := range map[string][]byte{"ca.pem": certPEM, "cert.pem": certPEM, "key.pem": keyPEM} {
		if err := os.WriteFile(filepath.Join(reference, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	credential, err := json.Marshal(map[string]string{"endpoint": sandboxEndpoint, "apiKey": sandboxKey})
	if err != nil || os.WriteFile(filepath.Join(reference, "opensandbox.json"), credential, 0o600) != nil {
		t.Fatal("write OpenSandbox credential")
	}

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
	t.Cleanup(owner.Close)
	if command, err := owner.Exec(ctx, `UPDATE cloud_agents.deployment_targets
		SET endpoint=$1, updated_at=transaction_timestamp()
		WHERE tenant_id='tenant' AND project_uid='project' AND target_uid='target'`, server.URL); err != nil || command.RowsAffected() != 1 {
		t.Fatalf("bind live Docker endpoint = %d / %v", command.RowsAffected(), err)
	}

	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimePool.Close)
	store, err := postgres.NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	dockerDirectory, err := dockertarget.NewCredentialDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	sandboxDirectory, err := opensandbox.NewCredentialDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := sandboxDirectory.Client("fixture-only")
	if err != nil {
		t.Fatal(err)
	}
	controller, err := New(store, dockerDirectory, sandboxDirectory)
	if err != nil {
		t.Fatal(err)
	}
	return liveControllerEnvironment{controller, store, owner, sandbox, dockerSocket, server.URL, sandboxEndpoint, sandboxKey}
}

func prepareLiveControllerRestart(t *testing.T, ctx context.Context, environment liveControllerEnvironment) {
	t.Helper()
	subject := digest("foundation-controller")
	claimed, err := environment.store.ClaimFoundationSandbox(ctx, postgres.FoundationSandboxClaimInput{
		HolderID: "prepare-controller", HolderIncarnation: "prepare-incarnation", ClaimToken: "prepare-claim",
		LeaseSeconds: 1, SubjectDigest: subject, AuditFactID: "audit-live-prepare-claim",
	})
	if err != nil || claimed.DatabaseOutcome != postgres.DatabaseCommitted || !claimed.Found || claimed.Claim.SandboxID != "sandbox" {
		t.Fatalf("prepare claim = %#v / %v", claimed, err)
	}
	result := environment.controller.execute(ctx, claimed.Claim)
	if result.err != nil || result.runtimeState != "Running" {
		t.Fatalf("prepare physical effect = %#v", result)
	}
	allowedIP := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_ALLOWED_IP")
	blockedIP := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_BLOCKED_IP")
	dockerGateway := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_DOCKER_GATEWAY")
	if net.ParseIP(allowedIP) == nil || net.ParseIP(blockedIP) == nil || net.ParseIP(dockerGateway) == nil {
		t.Fatal("live network isolation fixtures are invalid")
	}
	networkOutput := liveCommand(t, ctx, environment, result.runtimeID, fmt.Sprintf(
		`node -e 'const checks=[["ALLOWED","http://%s:8080"],["CROSS","http://%s:8080"],["HOST","http://%s:18891"],["METADATA","http://169.254.169.254"]];(async()=>{for(const [name,target] of checks){try{const response=await fetch(target,{signal:AbortSignal.timeout(1000)});const body=await response.text();console.log("CAG_NETWORK_"+name+"="+(name==="ALLOWED"&&response.ok&&body.trim()==="allowed"?"allowed":"reachable"))}catch{console.log("CAG_NETWORK_"+name+"=blocked")}}})()'`,
		allowedIP, blockedIP, dockerGateway,
	))
	for _, marker := range []string{"CAG_NETWORK_ALLOWED=allowed", "CAG_NETWORK_CROSS=blocked", "CAG_NETWORK_HOST=blocked", "CAG_NETWORK_METADATA=blocked"} {
		if !strings.Contains(networkOutput, marker) {
			t.Fatalf("network policy did not prove %q: %s", marker, networkOutput)
		}
	}
	proof := "cloud-agents-controller-restart"
	proofDigest := sha256.Sum256([]byte(proof))
	output := liveCommand(t, ctx, environment, result.runtimeID,
		"printf %s "+proof+" > /workspace/controller-proof.txt && sha256sum /workspace/controller-proof.txt")
	if !strings.Contains(output, hex.EncodeToString(proofDigest[:])) {
		t.Fatalf("workspace write response = %s", output)
	}
	receipt, _ := json.Marshal(map[string]string{
		"runtimeId": result.runtimeID, "volumeName": result.volumeName,
		"proofDigest": hex.EncodeToString(proofDigest[:]), "operationId": claimed.Claim.OperationID,
		"specDigest": claimed.Claim.SpecDigest, "expiresAt": claimed.Claim.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"networkPolicy": "restricted allow plus cross-sandbox/host/metadata deny",
	})
	t.Logf("FOUNDATION_LIVE_PREPARE=%s", receipt)
	// Intentionally exit without settlement: the next OS process must reap and adopt.
}

func recoverLiveControllerRestart(t *testing.T, ctx context.Context, environment liveControllerEnvironment) {
	t.Helper()
	expectedRuntime := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_RUNTIME_ID")
	expectedVolume := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_VOLUME_NAME")
	expectedDigest := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_PROOF_DIGEST")
	if expectedRuntime == "" || expectedVolume == "" || expectedDigest == "" {
		t.Fatal("live recovery receipt is missing")
	}
	if worked, err := environment.controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("reap abandoned claim = %v / %v", worked, err)
	}
	if worked, err := environment.controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("adopt and settle = %v / %v", worked, err)
	}

	success := readLiveSandbox(t, ctx, environment.owner, "sandbox")
	if success.runtimeID != expectedRuntime || success.volumeName != expectedVolume ||
		success.observedState != "running" || success.operationState != "succeeded" || success.deliveryAttempts != 2 {
		t.Fatalf("recovered success = %#v", success)
	}
	identity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: success.workspaceID,
		Sandbox: "sandbox", Operation: success.operationID, Generation: success.generation, SpecDigest: success.specDigest}
	observation, err := environment.sandbox.Find(ctx, identity)
	if err != nil || observation.RuntimeID != expectedRuntime {
		t.Fatalf("adopted receipt = %#v / %v", observation, err)
	}
	if output := liveCommand(t, ctx, environment, expectedRuntime, "sha256sum /workspace/controller-proof.txt"); !strings.Contains(output, expectedDigest) {
		t.Fatalf("recovered workspace response = %s", output)
	}

	var failure liveSandboxRow
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep((time.Second << (attempt - 1)) + 100*time.Millisecond)
		}
		if worked, err := environment.controller.RunOne(ctx); err != nil || !worked {
			t.Fatalf("failure compensation attempt %d = %v / %v", attempt+1, worked, err)
		}
		failure = readLiveSandbox(t, ctx, environment.owner, "sandbox-terminal")
		if failure.observedState == "failed" && failure.operationState == "failed" && failure.cleanupPhase == "complete" {
			break
		}
	}
	if failure.observedState != "failed" || failure.operationState != "failed" || failure.cleanupPhase != "complete" ||
		failure.stableError != "opensandbox_runtime_failed" || failure.runtimeID == "" {
		t.Fatalf("compensated failure = %#v", failure)
	}
	failureIdentity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: failure.workspaceID,
		Sandbox: "sandbox-terminal", Operation: failure.operationID, Generation: failure.generation, SpecDigest: failure.specDigest}
	if _, err := environment.sandbox.Find(ctx, failureIdentity); !errors.Is(err, opensandbox.ErrNotFound) {
		t.Fatalf("failed runtime still exists: %v", err)
	}

	removeLiveVolume(t, ctx, environment.dockerSocket, failure.volumeName, failure.workspaceID)
	if count := liveSandboxCount(t, ctx, environment); count != 1 {
		t.Fatalf("OpenSandbox residue = %d", count)
	}
	receipt, _ := json.Marshal(map[string]any{
		"runtimeId": expectedRuntime, "adopted": true, "deliveryAttempts": success.deliveryAttempts,
		"failureRuntimeId": failure.runtimeID, "failureCompensated": true,
		"workspaceDigest": expectedDigest, "cleanup": "failed runtime and volume removed; successful runtime retained for lifecycle",
	})
	t.Logf("FOUNDATION_LIVE_RECOVER=%s", receipt)
}

func lifecycleLiveController(t *testing.T, ctx context.Context, environment liveControllerEnvironment, phase string) {
	t.Helper()
	priorRuntime := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_RUNTIME_ID")
	expectedVolume := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_VOLUME_NAME")
	expectedDigest := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_PROOF_DIGEST")
	if priorRuntime == "" || expectedVolume == "" || expectedDigest == "" {
		t.Fatal("live lifecycle receipt is missing")
	}
	if worked, err := environment.controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("%s reconcile = %v / %v", phase, worked, err)
	}
	current := readLiveSandbox(t, ctx, environment.owner, "sandbox")
	if current.volumeName != expectedVolume || current.operationState != "succeeded" || current.cleanupPhase != "complete" {
		t.Fatalf("%s settlement = %#v", phase, current)
	}
	if phase == "stop" {
		priorOperationID := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_PRIOR_OPERATION_ID")
		priorSpecDigest := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_PRIOR_SPEC_DIGEST")
		if priorOperationID == "" || priorSpecDigest == "" {
			t.Fatal("prior runtime receipt is missing")
		}
		if current.observedState != "stopped" || !current.writerReleased || current.runtimeID != "" || current.generation != 2 {
			t.Fatalf("stop settlement = %#v", current)
		}
		oldIdentity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: current.workspaceID,
			Sandbox: "sandbox", Operation: priorOperationID, Generation: 1, SpecDigest: priorSpecDigest}
		if _, err := environment.sandbox.Find(ctx, oldIdentity); !errors.Is(err, opensandbox.ErrNotFound) {
			t.Fatalf("stopped runtime still exists: %v", err)
		}
		receipt, _ := json.Marshal(map[string]any{"generation": current.generation, "runtimeDeleted": true,
			"workspaceVolume": current.volumeName, "writerReleased": current.writerReleased})
		t.Logf("FOUNDATION_LIVE_STOP=%s", receipt)
		return
	}
	expectedGeneration := int64(3)
	if phase == "rebuild-final" {
		expectedGeneration = 5
	}
	if current.observedState != "running" || current.writerReleased || current.runtimeID == "" ||
		current.runtimeID == priorRuntime || current.generation != expectedGeneration || current.ttlSeconds != 120 ||
		current.expiresAt.IsZero() || current.lifecycleTrigger != "manual" {
		t.Fatalf("rebuild settlement = %#v", current)
	}
	if output := liveCommand(t, ctx, environment, current.runtimeID, "sha256sum /workspace/controller-proof.txt"); !strings.Contains(output, expectedDigest) {
		t.Fatalf("rebuilt workspace response = %s", output)
	}
	if phase == "rebuild" {
		receipt, _ := json.Marshal(map[string]any{"generation": current.generation, "runtimeId": current.runtimeID,
			"operationId": current.operationID, "specDigest": current.specDigest,
			"workspaceDigest": expectedDigest, "expiresAt": current.expiresAt.UTC().Format(time.RFC3339Nano),
			"lifecycleTrigger": current.lifecycleTrigger, "cleanup": "runtime and retained Workspace remain for TTL"})
		t.Logf("FOUNDATION_LIVE_REBUILD=%s", receipt)
		return
	}
	currentIdentity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: current.workspaceID,
		Sandbox: "sandbox", Operation: current.operationID, Generation: current.generation, SpecDigest: current.specDigest}
	staleIdentity := currentIdentity
	staleIdentity.Generation--
	if err := environment.sandbox.Delete(ctx, staleIdentity, current.runtimeID); !errors.Is(err, opensandbox.ErrConflict) {
		t.Fatalf("stale generation was not fenced: %v", err)
	}
	wrongOwner := dockertarget.FoundationWorkspaceVolume{TenantID: "tenant", ProjectID: "project", TargetID: "target", WorkspaceID: "workspace-foreign"}
	createLiveVolume(t, ctx, environment.dockerSocket, wrongOwner.Name(), map[string]string{"cloud-agents.dev/managed": "true", "cloud-agents.dev/resource": "foundation-workspace", "cloud-agents.dev/tenant": "other"})
	if _, err := environment.controller.docker.EnsureFoundationWorkspaceVolume(ctx, environment.dockerEndpoint, "fixture-only", wrongOwner); !errors.Is(err, dockertarget.ErrDeploymentConflict) {
		t.Fatalf("foreign volume owner was not rejected: %v", err)
	}
	status, _ := liveDockerRequest(t, ctx, environment.dockerSocket, http.MethodDelete, "/volumes/"+wrongOwner.Name())
	if status != http.StatusNoContent {
		t.Fatalf("foreign fixture cleanup status=%d", status)
	}
	if err := environment.sandbox.Delete(ctx, currentIdentity, current.runtimeID); err != nil {
		t.Fatalf("delete rebuilt runtime: %v", err)
	}
	removeLiveVolume(t, ctx, environment.dockerSocket, current.volumeName, current.workspaceID)
	if count := liveSandboxCount(t, ctx, environment); count != 0 {
		t.Fatalf("OpenSandbox residue = %d", count)
	}
	receipt, _ := json.Marshal(map[string]any{"generation": current.generation, "runtimeId": current.runtimeID,
		"priorRuntimeId": priorRuntime, "workspaceDigest": expectedDigest, "staleGenerationRejected": true,
		"foreignVolumeOwnerRejected": true, "cleanup": "zero owned sandboxes and volumes"})
	t.Logf("FOUNDATION_LIVE_REBUILD_FINAL=%s", receipt)
}

func ttlLiveController(t *testing.T, ctx context.Context, environment liveControllerEnvironment) {
	t.Helper()
	priorRuntime := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_RUNTIME_ID")
	priorOperationID := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_PRIOR_OPERATION_ID")
	priorSpecDigest := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_PRIOR_SPEC_DIGEST")
	expectedVolume := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_VOLUME_NAME")
	if priorRuntime == "" || priorOperationID == "" || priorSpecDigest == "" || expectedVolume == "" {
		t.Fatal("TTL runtime receipt is missing")
	}
	if worked, err := environment.controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("TTL acceptance = %v / %v", worked, err)
	}
	accepted := readLiveSandbox(t, ctx, environment.owner, "sandbox")
	if accepted.generation != 4 || accepted.observedState != "running" || accepted.lifecycleTrigger != "ttl" {
		t.Fatalf("TTL acceptance = %#v", accepted)
	}
	expectedDigest, err := coordination.FoundationSandboxLifecycleDigest(coordination.FoundationSandboxLifecycleInput{
		Scope:                   coordination.FoundationScope{TenantID: "tenant", ProjectID: "project"},
		SandboxID:               "sandbox",
		Action:                  coordination.FoundationSandboxStop,
		ConfirmedSandboxID:      "sandbox",
		ComputeDisposition:      "delete",
		WorkspaceDisposition:    "retain",
		ExpectedGeneration:      3,
		ExpectedResourceVersion: accepted.resourceVersion,
		Mutation:                coordination.FoundationMutation{RequestID: "ttl-check", IdempotencyKey: "foundation-ttl-check-key"},
	})
	if err != nil || accepted.specDigest != expectedDigest {
		t.Fatalf("TTL request digest = %q / %q / %v", accepted.specDigest, expectedDigest, err)
	}
	if worked, err := environment.controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("TTL stop reconcile = %v / %v", worked, err)
	}
	current := readLiveSandbox(t, ctx, environment.owner, "sandbox")
	if current.generation != 4 || current.observedState != "stopped" || !current.writerReleased ||
		current.runtimeID != "" || current.volumeName != expectedVolume || current.lifecycleTrigger != "ttl" ||
		current.expiresAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("TTL settlement = %#v", current)
	}
	oldIdentity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: current.workspaceID,
		Sandbox: "sandbox", Operation: priorOperationID, Generation: 3, SpecDigest: priorSpecDigest}
	if _, err := environment.sandbox.Find(ctx, oldIdentity); !errors.Is(err, opensandbox.ErrNotFound) {
		t.Fatalf("expired runtime still exists: %v", err)
	}
	var ttlAudits int
	if err := environment.owner.QueryRow(ctx, `SELECT count(*) FROM cloud_agents.coordination_audit_facts
		WHERE tenant_id='tenant' AND operation_id=$1 AND transition='sandbox.ttl.accept'`, current.operationID).Scan(&ttlAudits); err != nil || ttlAudits != 1 {
		t.Fatalf("TTL audit count=%d error=%v", ttlAudits, err)
	}
	receipt, _ := json.Marshal(map[string]any{"generation": current.generation, "operationId": current.operationID,
		"expiredRuntimeId": priorRuntime, "runtimeDeleted": true, "workspaceVolume": current.volumeName,
		"writerReleased": current.writerReleased, "lifecycleTrigger": current.lifecycleTrigger,
		"expiresAt": current.expiresAt.UTC().Format(time.RFC3339Nano), "requestDigestVerified": true, "ttlAuditFacts": ttlAudits})
	t.Logf("FOUNDATION_LIVE_TTL=%s", receipt)
}

type liveSandboxRow struct {
	workspaceID, operationID, specDigest, runtimeID, observedState string
	volumeName, operationState, cleanupPhase, stableError          string
	lifecycleTrigger                                               string
	generation, resourceVersion                                    int64
	ttlSeconds                                                     int32
	expiresAt                                                      time.Time
	deliveryAttempts                                               int
	writerReleased                                                 bool
}

func readLiveSandbox(t *testing.T, ctx context.Context, owner *pgxpool.Pool, sandboxID string) liveSandboxRow {
	t.Helper()
	var row liveSandboxRow
	err := owner.QueryRow(ctx, `SELECT s.workspace_uid,s.operation_id,s.generation,s.resource_version,s.spec_digest,
		COALESCE(s.runtime_uid,''),s.observed_state,v.physical_volume_uid,o.state,o.cleanup_phase,
		COALESCE(o.terminal_error_code,''),e.delivery_attempts,s.writer_released,
		COALESCE(a.lifecycle_trigger,''),COALESCE(s.ttl_seconds,0),s.expires_at
		FROM cloud_agents.sandbox_sessions s
		JOIN cloud_agents.workspace_volumes v USING (tenant_id,project_uid,workspace_uid)
		JOIN cloud_agents.platform_operations o ON o.tenant_id=s.tenant_id AND o.operation_id=s.operation_id AND o.operation_generation=s.operation_generation
		JOIN cloud_agents.outbox_events e ON e.tenant_id=s.tenant_id AND e.operation_id=s.operation_id AND e.operation_generation=s.operation_generation
		LEFT JOIN cloud_agents.foundation_sandbox_activity a ON a.tenant_id=s.tenant_id AND a.operation_uid=s.operation_id AND a.sandbox_generation=s.operation_generation
		WHERE s.tenant_id='tenant' AND s.sandbox_uid=$1`, sandboxID).Scan(
		&row.workspaceID, &row.operationID, &row.generation, &row.resourceVersion, &row.specDigest, &row.runtimeID,
		&row.observedState, &row.volumeName, &row.operationState, &row.cleanupPhase,
		&row.stableError, &row.deliveryAttempts, &row.writerReleased, &row.lifecycleTrigger,
		&row.ttlSeconds, &row.expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func createLiveVolume(t *testing.T, ctx context.Context, socket, name string, labels map[string]string) {
	t.Helper()
	encoded, _ := json.Marshal(map[string]any{"Name": name, "Labels": labels})
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/volumes/create", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create foreign fixture volume status=%d", response.StatusCode)
	}
}

func liveCommand(t *testing.T, ctx context.Context, environment liveControllerEnvironment, runtimeID, command string) string {
	t.Helper()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		environment.sandboxEndpoint+"/v1/sandboxes/"+runtimeID+"/endpoints/44772", http.NoBody)
	request.Header.Set("OPEN-SANDBOX-API-KEY", environment.sandboxKey)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var endpoint struct {
		Endpoint string            `json:"endpoint"`
		Headers  map[string]string `json:"headers"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&endpoint) != nil {
		t.Fatalf("execd endpoint status = %d", response.StatusCode)
	}
	raw := endpoint.Endpoint
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	address, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("invalid execd endpoint %q", raw)
	}
	ip := net.ParseIP(address.Hostname())
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("invalid execd endpoint %q", raw)
	}
	body, _ := json.Marshal(map[string]any{"command": command, "cwd": "/workspace", "timeout": 10000})
	request, _ = http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(raw, "/")+"/command", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for name, value := range endpoint.Headers {
		request.Header.Set(name, value)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(result) > 1<<20 || response.StatusCode != http.StatusOK {
		t.Fatalf("command status = %d err=%v body=%s", response.StatusCode, err, result)
	}
	return string(result)
}

func removeLiveVolume(t *testing.T, ctx context.Context, socket, name, workspace string) {
	t.Helper()
	status, body := liveDockerRequest(t, ctx, socket, http.MethodGet, "/volumes/"+name)
	var volume struct {
		Name   string            `json:"Name"`
		Labels map[string]string `json:"Labels"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &volume) != nil {
		t.Fatalf("inspect owned volume %q status=%d", name, status)
	}
	expected := map[string]string{
		"cloud-agents.dev/managed": "true", "cloud-agents.dev/resource": "foundation-workspace",
		"cloud-agents.dev/tenant": "tenant", "cloud-agents.dev/project": "project",
		"cloud-agents.dev/target": "target", "cloud-agents.dev/workspace": workspace,
	}
	if volume.Name != name || !reflect.DeepEqual(volume.Labels, expected) {
		t.Fatalf("refuse volume cleanup: %#v", volume)
	}
	status, body = liveDockerRequest(t, ctx, socket, http.MethodDelete, "/volumes/"+name)
	if status != http.StatusNoContent {
		t.Fatalf("delete owned volume %q status=%d body=%s", name, status, body)
	}
	status, _ = liveDockerRequest(t, ctx, socket, http.MethodGet, "/volumes/"+name)
	if status != http.StatusNotFound {
		t.Fatalf("volume %q remained: status=%d", name, status)
	}
}

func liveDockerRequest(t *testing.T, ctx context.Context, socket, method, path string) (int, []byte) {
	t.Helper()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	request, _ := http.NewRequestWithContext(ctx, method, "http://docker"+path, http.NoBody)
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		t.Fatal("Docker response is invalid")
	}
	return response.StatusCode, body
}

func liveSandboxCount(t *testing.T, ctx context.Context, environment liveControllerEnvironment) int {
	t.Helper()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, environment.sandboxEndpoint+"/v1/sandboxes?page=1&pageSize=100", http.NoBody)
	request.Header.Set("OPEN-SANDBOX-API-KEY", environment.sandboxKey)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&page) != nil {
		t.Fatalf("sandbox list status = %d", response.StatusCode)
	}
	return len(page.Items)
}
