package managedagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
)

func TestFoundationRuntimeUsesGorillaWebSocketFrameTypes(t *testing.T) {
	if foundationRuntimeFrameBinary != websocket.BinaryMessage || foundationRuntimeFrameText != websocket.TextMessage {
		t.Fatal("Foundation Runtime frame types drifted from Gorilla WebSocket")
	}
}

func TestFoundationRuntimeSessionHealthRejectsClosedPTYStream(t *testing.T) {
	done := make(chan struct{})
	close(done)
	session := &foundationRuntimeSession{done: done}
	if err := session.checkHealth(context.Background()); !errors.Is(err, ErrRuntimeEnvironmentUnavailable) {
		t.Fatalf("closed Foundation Runtime health = %v", err)
	}
}

func TestFoundationRuntimeSessionHealthRejectsStoppedPTY(t *testing.T) {
	id := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: "workspace", Sandbox: "sandbox", Operation: "operation", Generation: 1, SpecDigest: "sha256:" + strings.Repeat("a", 64)}
	running := true
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/sandboxes/runtime-1" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": "runtime-1", "metadata": id.Labels(), "status": map[string]string{"state": "Running"}})
			return
		}
		if request.URL.Path == "/v1/sandboxes/runtime-1/endpoints/44772" {
			_ = json.NewEncoder(writer).Encode(map[string]string{"endpoint": server.URL + "/v1/sandboxes/runtime-1/proxy/44772"})
			return
		}
		if request.URL.Path == "/v1/sandboxes/runtime-1/proxy/44772/pty/pty-1" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"session_id": "pty-1", "running": running, "output_offset": 0})
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	client, err := opensandbox.New(server.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	session := &foundationRuntimeSession{client: client, pty: opensandbox.PTYInput{Identity: id, RuntimeID: "runtime-1"}, sessionID: "pty-1", done: make(chan struct{})}
	if err := session.checkHealth(context.Background()); err != nil {
		t.Fatalf("running Foundation PTY health = %v", err)
	}
	running = false
	if err := session.checkHealth(context.Background()); !errors.Is(err, ErrRuntimeEnvironmentUnavailable) {
		t.Fatalf("stopped Foundation PTY health = %v", err)
	}
}

type foundationRemoteRuntimeStoreFake struct {
	inputs [][]byte
	closed bool
}

func (store *foundationRemoteRuntimeStoreFake) OpenFoundationRemoteRuntime(_ context.Context, _ VerifiedPrincipalSource, _ RuntimeSessionSnapshot, _ string) (FoundationRemoteRuntimeHandle, error) {
	return FoundationRemoteRuntimeHandle{Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, GrantID: "grant-alpha", TokenDigest: "sha256:" + strings.Repeat("a", 64), SessionID: "session-alpha"}, nil
}

func (store *foundationRemoteRuntimeStoreFake) ExchangeFoundationRemoteRuntime(_ context.Context, _ FoundationRemoteRuntimeHandle, _ uint64, _ int64, input []byte) (FoundationRemoteRuntimeExchange, error) {
	if input != nil {
		store.inputs = append(store.inputs, append([]byte(nil), input...))
		if bytes.Contains(input, []byte("cloud-agent-runtime")) {
			return FoundationRemoteRuntimeExchange{Frames: []FoundationRemoteRuntimeFrame{{MessageType: "binary", Payload: append([]byte{1}, []byte(foundationRuntimeInputReady)...)}}, OutputOffset: int64(len(foundationRuntimeInputReady)), Running: true}, nil
		}
	}
	return FoundationRemoteRuntimeExchange{Running: true}, nil
}

func (store *foundationRemoteRuntimeStoreFake) CloseFoundationRemoteRuntime(_ context.Context, _ VerifiedPrincipalSource, _ FoundationRemoteRuntimeHandle) error {
	store.closed = true
	return nil
}

func (*foundationRemoteRuntimeStoreFake) ReadFoundationRemoteArtifact(context.Context, VerifiedPrincipalSource, RuntimeSessionSnapshot, string) ([]byte, error) {
	return nil, nil
}

func TestFoundationRuntimeKeepsCredentialOutOfCommandAndRoutesBySandboxGeneration(t *testing.T) {
	t.Setenv(foundationManagedWriteDelayEnv, "12000")
	root := t.TempDir()
	targets, err := opensandbox.NewCredentialDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	credential := []byte(`{"apiKey":"secret-value"}`)
	if err := os.WriteFile(filepath.Join(root, "tenant-alpha.codex.json"), credential, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewFoundationRuntime(targets, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readFoundationProviderCredential(root, "tenant-alpha", "codex")
	if err != nil || string(got) != string(credential) {
		t.Fatalf("credential = %q, error = %v", got, err)
	}
	command := foundationRuntimeCommand(len(credential), "codex", "docker")
	if strings.Contains(command, "secret-value") || !strings.Contains(command, "count="+strconv.Itoa(len(credential))) || !strings.Contains(command, "CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex") || !strings.Contains(command, "CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1") || !strings.Contains(command, foundationManagedWriteDelayEnv+"=12000") {
		t.Fatalf("unsafe credential command = %q", command)
	}
	t.Setenv(foundationDeepseekToolDelayEnv, "5000")
	t.Setenv(foundationDeepseekManagedToolDelayEnv, "5000")
	remoteCommand := foundationRuntimeCommand(len(credential), "deepseek-harness", "remote-worker")
	if !strings.Contains(remoteCommand, foundationDeepseekToolDelayEnv+"=5000") || !strings.Contains(remoteCommand, foundationDeepseekManagedToolDelayEnv+"=5000") {
		t.Fatal("RemoteWorker deepseek delay was not injected")
	}
	if !strings.Contains(foundationRuntimeCommand(len(credential), "deepseek-harness", "kubernetes"), foundationDeepseekToolDelayEnv+"=5000") {
		t.Fatal("Kubernetes deepseek delay was not injected")
	}
	if !strings.Contains(foundationRuntimeCommand(len(credential), "claudeAgent", "kubernetes"), "CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=kubernetes-restricted-v1") {
		t.Fatal("Kubernetes Runtime did not receive its outer sandbox profile")
	}
	executionID := "execution-fence"
	digest := sha256.Sum256([]byte(executionID))
	fenced := foundationRuntimeCommand(len(credential), "codex", "docker", executionID)
	if !strings.Contains(fenced, "/tmp/cloud-agents-runtime/"+hex.EncodeToString(digest[:])+".pid") || !strings.Contains(fenced, "kill -KILL") || !strings.Contains(fenced, "cloud-agent-runtime") {
		t.Fatalf("runtime writer fence is missing: %q", fenced)
	}
	coordinator := &DurableRuntimeExecutionCoordinator{foundationRuntime: runtime}
	worker, err := coordinator.workerForSession(RuntimeSessionSnapshot{SessionSnapshot: SessionSnapshot{WorkspaceID: "workspace-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 9}, FoundationSandboxReady: true})
	if err != nil || worker.foundation != runtime || worker.generation != 9 {
		t.Fatalf("worker = %#v, error = %v", worker, err)
	}
	if err := os.Symlink(filepath.Join(root, "tenant-alpha.codex.json"), filepath.Join(root, "tenant-alpha.claudeAgent.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readFoundationProviderCredential(root, "tenant-alpha", "claudeAgent"); !errors.Is(err, ErrRuntimeEnvironmentUnavailable) {
		t.Fatalf("symlink credential error = %v", err)
	}
}

func TestFoundationRuntimeBootstrapRunsWithoutATerminal(t *testing.T) {
	credential := []byte(`{"apiKey":"secret-value"}`)
	command := strings.Replace(foundationRuntimeCommand(len(credential), "codex", "docker"),
		"exec /usr/local/bin/cloud-agent-runtime", "printf runtime-started", 1)
	process := exec.Command("sh", "-c", command)
	process.Stdin = bytes.NewReader(credential)
	output, err := process.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte(foundationRuntimeInputReady)) || !bytes.Contains(output, []byte("runtime-started")) {
		t.Fatalf("non-PTY bootstrap output = %q, error = %v", output, err)
	}
}

func TestFoundationRuntimeInjectsOnlyDigestPinnedCapabilityManifest(t *testing.T) {
	binding := &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "skill-bundle", ResourceId: "skill-1", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64), GrantId: "grant-1", ExpiresAtUnixSeconds: 1_900_000_000, ReadOnly: true}
	session := RuntimeSessionSnapshot{CapabilityBindings: []*workerruntimev1alpha1.RuntimeCapabilityBinding{binding}}
	manifest := foundationCapabilityManifest{Version: 1, Bindings: []foundationCapabilityManifestRef{{ResourceKind: "skill-bundle", ResourceID: "skill-1", Version: "v1", Digest: binding.Digest, GrantID: "grant-1", ExpiresAtUnixSecond: binding.ExpiresAtUnixSeconds, ReadOnly: true}}}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded)
	session.CapabilityManifestDigest = fmt.Sprintf("sha256:%x", sum[:])
	got, err := foundationRuntimeCapabilityManifest(session)
	if err != nil || string(got) != string(encoded) {
		t.Fatalf("capability manifest = %q, %v", got, err)
	}
	command := foundationRuntimeCommandWithManifest(1, "codex", "docker", got)
	if !strings.Contains(command, "CLOUD_AGENT_CAPABILITY_MANIFEST_B64="+base64.RawURLEncoding.EncodeToString(encoded)) {
		t.Fatal("capability manifest was not injected")
	}
	materialization := []byte(`{"version":1,"mcp":[{"resourceId":"server-1","version":"v1","digest":"sha256:` + strings.Repeat("b", 64) + `","transport":"streamable-http","endpoint":"https://mcp.example.test/mcp","token":"short-lived","allowedHosts":["mcp.example.test"]}]}`)
	command = strings.Replace(foundationRuntimeCommandWithMaterialization(1, len(materialization), "codex", "docker", encoded), "exec /usr/local/bin/cloud-agent-runtime", "cat <&4", 1)
	process := exec.Command("sh", "-c", command)
	process.Stdin = bytes.NewReader(append([]byte("x"), materialization...))
	output, commandErr := process.CombinedOutput()
	if commandErr != nil || !bytes.Contains(output, materialization) || !bytes.Contains(output, []byte(foundationRuntimeInputReady)) || strings.Contains(command, "short-lived") {
		t.Fatalf("capability materialization bootstrap = %q, error = %v", output, commandErr)
	}
	session.CapabilityManifestDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err := foundationRuntimeCapabilityManifest(session); !errors.Is(err, ErrRuntimeEnvironmentUnavailable) {
		t.Fatalf("digest mismatch = %v", err)
	}
}

func TestCloseFoundationRuntimeConnectionUsesNormalClose(t *testing.T) {
	closeCode := make(chan int, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_, _, err = connection.ReadMessage()
		if closeErr, ok := err.(*websocket.CloseError); ok {
			closeCode <- closeErr.Code
		}
	}))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	closeFoundationRuntimeConnection(connection)
	select {
	case code := <-closeCode:
		if code != websocket.CloseNormalClosure {
			t.Fatalf("close code=%d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not observe the close handshake")
	}
}

func TestFoundationRuntimeStartsThroughOutboundRemoteWorkerWithoutPersistingCredential(t *testing.T) {
	root := t.TempDir()
	credential := []byte(`{"apiKey":"remote-secret"}`)
	if err := os.WriteFile(filepath.Join(root, "tenant-alpha.codex.json"), credential, 0o600); err != nil {
		t.Fatal(err)
	}
	remote := &foundationRemoteRuntimeStoreFake{}
	runtime, err := NewFoundationRuntime(nil, root, remote)
	if err != nil {
		t.Fatal(err)
	}
	session, err := runtime.open(context.Background(), func() (*authn.VerifiedPrincipal, error) { return nil, nil }, RuntimeSessionSnapshot{
		SessionSnapshot:      SessionSnapshot{Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, ProviderKind: "codex", WorkspaceID: "workspace-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 3},
		FoundationTargetKind: "remote-worker", FoundationSandboxReady: true,
	}, "execution-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(remote.inputs) != 2 || strings.Contains(string(remote.inputs[0]), "remote-secret") || !strings.Contains(string(remote.inputs[0]), "/usr/local/bin/cloud-agent-runtime\n") || !bytes.Equal(remote.inputs[1], credential) {
		t.Fatalf("runtime bootstrap inputs are invalid")
	}
	_ = session.CloseResponse()
	if !remote.closed {
		t.Fatal("remote Runtime session was not deleted")
	}
}

func TestFoundationCapabilityMaterializationReadsOnlyExactTenantBinding(t *testing.T) {
	root := t.TempDir()
	digest := "sha256:" + strings.Repeat("a", 64)
	binding := &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "mcp-server", ResourceId: "server-1", Version: "v1", Digest: digest, Transport: "streamable-http", ConnectionRef: "connection-1", CredentialRef: "credential-1", GrantId: "grant-1", NetworkPolicyRef: "network-1", ExpiresAtUnixSeconds: 1_900_000_000, Permissions: []string{"mcp.call"}}
	data := []byte(`{"version":1,"mcp":[{"resourceId":"server-1","version":"v1","digest":"` + digest + `","transport":"streamable-http","endpoint":"https://mcp.example.test/mcp","token":"short-lived","allowedHosts":["mcp.example.test"]}]}`)
	if err := os.WriteFile(filepath.Join(root, "tenant-alpha.capabilities.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := foundationCapabilityMaterializationFile(root, "tenant-alpha", []*workerruntimev1alpha1.RuntimeCapabilityBinding{binding}); err != nil || string(got) != string(data) {
		t.Fatalf("materialization = %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(root, "tenant-alpha.capabilities.json"), append(data, []byte(`{}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := foundationCapabilityMaterializationFile(root, "tenant-alpha", []*workerruntimev1alpha1.RuntimeCapabilityBinding{binding}); !errors.Is(err, ErrRuntimeEnvironmentUnavailable) {
		t.Fatalf("trailing materialization error = %v", err)
	}
	if _, err := foundationCapabilityMaterializationFile(root, "tenant-beta", []*workerruntimev1alpha1.RuntimeCapabilityBinding{binding}); !errors.Is(err, ErrRuntimeEnvironmentUnavailable) {
		t.Fatalf("missing tenant materialization error = %v", err)
	}
}

func TestValidFoundationMcpMaterializationRejectsUnsupportedTransport(t *testing.T) {
	base := foundationMcpMaterialization{
		ResourceID:   "server-1",
		Version:      "v1",
		Digest:       "sha256:" + strings.Repeat("a", 64),
		Endpoint:     "https://mcp.example.test/mcp",
		Token:        "short-lived",
		AllowedHosts: []string{"mcp.example.test"},
	}
	for _, transport := range []string{"sse", "stdio"} {
		item := base
		item.Transport = transport
		if validFoundationMcpMaterialization(item) {
			t.Fatalf("unsupported transport %q was accepted", transport)
		}
	}
	base.Transport = "streamable-http"
	if !validFoundationMcpMaterialization(base) {
		t.Fatal("streamable-http transport was rejected")
	}
}
