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
	sandboxEndpoint string
	sandboxKey      string
}

func TestLiveFoundationControllerRestart(t *testing.T) {
	phase := os.Getenv("CLOUD_AGENTS_FOUNDATION_LIVE_PHASE")
	if phase != "prepare" && phase != "recover" {
		t.Skip("live foundation Controller phase is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	environment := newLiveControllerEnvironment(t, ctx)
	if phase == "prepare" {
		prepareLiveControllerRestart(t, ctx, environment)
		return
	}
	recoverLiveControllerRestart(t, ctx, environment)
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
	return liveControllerEnvironment{controller, store, owner, sandbox, dockerSocket, sandboxEndpoint, sandboxKey}
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
	proof := "cloud-agents-controller-restart"
	proofDigest := sha256.Sum256([]byte(proof))
	output := liveCommand(t, ctx, environment, result.runtimeID,
		"printf %s "+proof+" > /workspace/controller-proof.txt && sha256sum /workspace/controller-proof.txt")
	if !strings.Contains(output, hex.EncodeToString(proofDigest[:])) {
		t.Fatalf("workspace write response = %s", output)
	}
	receipt, _ := json.Marshal(map[string]string{
		"runtimeId": result.runtimeID, "volumeName": result.volumeName,
		"proofDigest": hex.EncodeToString(proofDigest[:]),
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

	if worked, err := environment.controller.RunOne(ctx); err != nil || !worked {
		t.Fatalf("failure compensation = %v / %v", worked, err)
	}
	failure := readLiveSandbox(t, ctx, environment.owner, "sandbox-terminal")
	if failure.observedState != "failed" || failure.operationState != "failed" || failure.cleanupPhase != "complete" ||
		failure.stableError != "opensandbox_runtime_failed" || failure.runtimeID == "" {
		t.Fatalf("compensated failure = %#v", failure)
	}
	failureIdentity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: failure.workspaceID,
		Sandbox: "sandbox-terminal", Operation: failure.operationID, Generation: failure.generation, SpecDigest: failure.specDigest}
	if _, err := environment.sandbox.Find(ctx, failureIdentity); !errors.Is(err, opensandbox.ErrNotFound) {
		t.Fatalf("failed runtime still exists: %v", err)
	}

	if err := environment.sandbox.Delete(ctx, identity, expectedRuntime); err != nil {
		t.Fatalf("delete successful runtime: %v", err)
	}
	for _, volume := range []struct {
		name, workspace string
	}{{success.volumeName, success.workspaceID}, {failure.volumeName, failure.workspaceID}} {
		removeLiveVolume(t, ctx, environment.dockerSocket, volume.name, volume.workspace)
	}
	if count := liveSandboxCount(t, ctx, environment); count != 0 {
		t.Fatalf("OpenSandbox residue = %d", count)
	}
	receipt, _ := json.Marshal(map[string]any{
		"runtimeId": expectedRuntime, "adopted": true, "deliveryAttempts": success.deliveryAttempts,
		"failureRuntimeId": failure.runtimeID, "failureCompensated": true,
		"workspaceDigest": expectedDigest, "cleanup": "zero owned sandboxes and volumes",
	})
	t.Logf("FOUNDATION_LIVE_RECOVER=%s", receipt)
}

type liveSandboxRow struct {
	workspaceID, operationID, specDigest, runtimeID, observedState string
	volumeName, operationState, cleanupPhase, stableError          string
	generation                                                     int64
	deliveryAttempts                                               int
}

func readLiveSandbox(t *testing.T, ctx context.Context, owner *pgxpool.Pool, sandboxID string) liveSandboxRow {
	t.Helper()
	var row liveSandboxRow
	err := owner.QueryRow(ctx, `SELECT s.workspace_uid,s.operation_id,s.generation,s.spec_digest,
		COALESCE(s.runtime_uid,''),s.observed_state,v.physical_volume_uid,o.state,o.cleanup_phase,
		COALESCE(o.terminal_error_code,''),e.delivery_attempts
		FROM cloud_agents.sandbox_sessions s
		JOIN cloud_agents.workspace_volumes v USING (tenant_id,project_uid,workspace_uid)
		JOIN cloud_agents.platform_operations o ON o.tenant_id=s.tenant_id AND o.operation_id=s.operation_id AND o.operation_generation=s.operation_generation
		JOIN cloud_agents.outbox_events e ON e.tenant_id=s.tenant_id AND e.operation_id=s.operation_id AND e.operation_generation=s.operation_generation
		WHERE s.tenant_id='tenant' AND s.sandbox_uid=$1`, sandboxID).Scan(
		&row.workspaceID, &row.operationID, &row.generation, &row.specDigest, &row.runtimeID,
		&row.observedState, &row.volumeName, &row.operationState, &row.cleanupPhase,
		&row.stableError, &row.deliveryAttempts)
	if err != nil {
		t.Fatal(err)
	}
	return row
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
