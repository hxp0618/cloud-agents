package managedagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
)

func TestFoundationRuntimeUsesGorillaWebSocketFrameTypes(t *testing.T) {
	if foundationRuntimeFrameBinary != websocket.BinaryMessage || foundationRuntimeFrameText != websocket.TextMessage {
		t.Fatal("Foundation Runtime frame types drifted from Gorilla WebSocket")
	}
}

type foundationRemoteRuntimeStoreFake struct {
	command string
	inputs  [][]byte
	closed  bool
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

func TestCloseFoundationRuntimeConnectionUsesNormalClose(t *testing.T) {
	closeCode := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
