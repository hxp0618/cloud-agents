package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
)

func TestHeartbeatLoopReconnectsWithBoundedBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	var waits []time.Duration
	err := runHeartbeatLoop(ctx, false, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("connection unavailable")
		}
		return nil
	}, func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		if len(waits) == 3 {
			return context.Canceled
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || attempts != 3 || !reflect.DeepEqual(waits, []time.Duration{time.Second, 2 * time.Second, 5 * time.Second}) {
		t.Fatalf("attempts=%d waits=%v err=%v", attempts, waits, err)
	}
}

func TestParseConfigBuildsCanonicalHeartbeat(t *testing.T) {
	value, err := parseConfig([]string{
		"--control-plane-url=https://control.example.test", "--tenant=tenant-alpha", "--project=project-alpha",
		"--enrollment=enrollment-alpha", "--incarnation=incarnation-alpha", "--certificate=/tmp/node.pem",
		"--private-key=/tmp/node-key.pem", "--server-ca=/tmp/ca.pem", "--state-file=/tmp/node-state.json", "--kernel-version=6.12.1",
		"--docker-endpoint=https://docker.example.test", "--credential-directory=/tmp", "--credential-ref=fixture-only",
		"--capabilities=docker,exec,files", "--capacity-cpu-millis=4000",
		"--capacity-memory-bytes=8589934592", "--capacity-disk-bytes=42949672960", "--once",
	})
	if err != nil || !value.once || value.heartbeatRequest(initialNodeState(value.incarnationID)).ObservedState != "active" {
		t.Fatalf("config=%#v err=%v", value, err)
	}
	if _, err := parseConfig([]string{"--control-plane-url=https://control.example.test"}); err == nil {
		t.Fatal("accepted incomplete configuration")
	}
	withoutDocker := []string{
		"--control-plane-url=https://control.example.test", "--tenant=tenant-alpha", "--project=project-alpha",
		"--enrollment=enrollment-alpha", "--incarnation=incarnation-alpha", "--certificate=/tmp/node.pem",
		"--private-key=/tmp/node-key.pem", "--server-ca=/tmp/ca.pem", "--state-file=/tmp/node-state.json",
		"--kernel-version=6.12.1", "--capabilities=exec,files", "--capacity-cpu-millis=4000",
		"--capacity-memory-bytes=8589934592", "--capacity-disk-bytes=42949672960",
	}
	if _, err := parseConfig(withoutDocker); err != nil {
		t.Fatalf("non-Docker heartbeat config rejected: %v", err)
	}
	rotating := append([]string{}, withoutDocker...)
	rotating[6] = "--private-key=/tmp/node.pem"
	rotating = append(rotating, "--certificate-resource-version-file=/tmp/identity.resource-version", "--rotate-certificate-once")
	rotationConfig, err := parseConfig(rotating)
	if err != nil || !rotationConfig.rotateCertificateOnce || rotationConfig.certificateRotationBefore != defaultCertificateRotationBefore {
		t.Fatalf("rotating heartbeat config rejected: %#v err=%v", rotationConfig, err)
	}
}

func TestRemoteWorkerCommandStateSurvivesRestartAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node", "state.json")
	state, err := loadNodeState(path, "incarnation-alpha")
	if err != nil || state.ObservedGeneration != 1 || state.ObservedState != "active" {
		t.Fatalf("initial state=%#v err=%v", state, err)
	}
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	command := &platform.RemoteWorkerCommand{CommandID: "command-alpha", Generation: 2, DesiredState: "drained", Deadline: now.Add(30 * time.Second).Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: "incarnation-alpha", Command: command}, now); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadNodeState(path, "incarnation-alpha")
	if err != nil || restarted.ObservedGeneration != 2 || restarted.ObservedState != "drained" || restarted.CommandReceipt == nil || restarted.CommandReceipt.Result != "succeeded" {
		t.Fatalf("restarted state=%#v err=%v", restarted, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode=%v", info.Mode().Perm())
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: "incarnation-alpha", Command: command}, now); err != nil || restarted.ObservedGeneration != 2 || restarted.CommandReceipt == nil || restarted.CommandReceipt.Result != "succeeded" {
		t.Fatalf("duplicate state=%#v err=%v", restarted, err)
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: "incarnation-alpha"}, now); err != nil || restarted.CommandReceipt != nil {
		t.Fatalf("acknowledged state=%#v err=%v", restarted, err)
	}
}

func TestRemoteWorkerDefersCertificateRotationUntilCommandsSettle(t *testing.T) {
	state := initialNodeState("incarnation-alpha")
	if state.hasUnsettledCommand() {
		t.Fatal("idle node reported an unsettled command")
	}
	state.SandboxPTYCommand = &platform.RemoteWorkerSandboxPTYCommand{CommandID: "rwpty-alpha"}
	if !state.hasUnsettledCommand() {
		t.Fatal("in-flight PTY command did not defer certificate rotation")
	}
	state.SandboxPTYCommand = nil
	state.CommandReceipt = &platform.RemoteWorkerCommandReceipt{CommandID: "command-alpha", Generation: 2, Result: "succeeded"}
	if !state.hasUnsettledCommand() {
		t.Fatal("pending command receipt did not defer certificate rotation")
	}
}

func TestRemoteWorkerRejectsExpiredCommandWithoutAdvancingGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := initialNodeState("incarnation-alpha")
	now := time.Date(2026, 9, 6, 10, 0, 31, 0, time.UTC)
	command := &platform.RemoteWorkerCommand{CommandID: "command-alpha", Generation: 2, DesiredState: "drained", Deadline: now.Add(-time.Second).Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: "incarnation-alpha", Command: command}, now); err != nil {
		t.Fatal(err)
	}
	if state.ObservedGeneration != 1 || state.ObservedState != "active" || state.CommandReceipt == nil || state.CommandReceipt.Result != "failed" || state.CommandReceipt.StableErrorCode != "remote-worker-command-expired" {
		t.Fatalf("expired state=%#v", state)
	}
}

func TestRemoteWorkerReconnectDoesNotReplayStartedCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	digest := "sha256:" + strings.Repeat("a", 64)
	deadline := time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	lifecycle := &platform.RemoteWorkerSandboxCommand{CommandID: "rwsc-alpha", Attempt: 1, Action: "sandbox.create",
		OperationID: "operation-alpha", WorkspaceID: "workspace-alpha", WorkspaceName: "workspace-alpha",
		TargetID: "target-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 1,
		WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc",
		ImageURI: "registry.example.test/runtime@" + digest, CPUMillis: 500, MemoryBytes: 536870912,
		SpecDigest: digest, NetworkPolicyID: "network-alpha", NetworkAllowedEgress: []string{"example.test"}, Deadline: deadline}
	state := initialNodeState("incarnation-alpha")
	state.SandboxCommand, state.ExecutingCommandID = lifecycle, lifecycle.CommandID
	if err := saveNodeState(path, state); err != nil {
		t.Fatal(err)
	}
	value := config{stateFile: path}
	if got := value.heartbeatRequest(state).SandboxCommandID; got != lifecycle.CommandID {
		t.Fatalf("renewal command = %q", got)
	}
	renewals := 0
	if err := executePendingSandbox(context.Background(), value, &state, func(context.Context) error {
		renewals++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if state.SandboxCommand != nil || state.SandboxCommandReceipt != nil || state.ExecutingCommandID != "" || renewals != 0 || value.heartbeatRequest(state).SandboxCommandID != "" {
		t.Fatalf("replayed lifecycle command: state=%#v renewals=%d", state, renewals)
	}

	execCommand := &platform.RemoteWorkerSandboxExecCommand{CommandID: "rwexec-alpha", WorkspaceID: "workspace-alpha",
		TargetID: "target-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 1, RuntimeID: "runtime-alpha",
		RuntimeOperationID: "operation-alpha", RuntimeSpecDigest: digest, Command: "touch /workspace/replayed",
		TimeoutSeconds: 10, Deadline: deadline}
	state = initialNodeState("incarnation-alpha")
	state.SandboxExecCommand, state.ExecutingCommandID = execCommand, execCommand.CommandID
	if err := saveNodeState(path, state); err != nil {
		t.Fatal(err)
	}
	if err := executePendingSandboxExec(context.Background(), value, &state); err != nil {
		t.Fatal(err)
	}
	if state.SandboxExecCommandReceipt == nil || state.SandboxExecCommandReceipt.Result != "failed" ||
		state.SandboxExecCommandReceipt.StableErrorCode != "sandbox_access_unavailable" {
		t.Fatalf("restarted Exec state=%#v", state)
	}
	restarted, err := loadNodeState(path, state.IncarnationID)
	if err != nil || restarted.SandboxExecCommandReceipt == nil || restarted.ExecutingCommandID != execCommand.CommandID {
		t.Fatalf("persisted reconnect state=%#v err=%v", restarted, err)
	}
}

func TestRemoteWorkerSandboxExecStateSurvivesRestartAndAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := initialNodeState("incarnation-alpha")
	command := &platform.RemoteWorkerSandboxExecCommand{CommandID: "rwexec-alpha", WorkspaceID: "workspace-alpha",
		TargetID: "target-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 3, RuntimeID: "runtime-alpha",
		RuntimeOperationID: "operation-alpha", RuntimeSpecDigest: "sha256:" + strings.Repeat("a", 64),
		Command: "printf bounded", TimeoutSeconds: 10, Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID, SandboxExecCommand: command}, time.Now()); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadNodeState(path, state.IncarnationID)
	if err != nil || restarted.SandboxExecCommand == nil || restarted.SandboxExecCommand.CommandID != command.CommandID {
		t.Fatalf("restarted state=%#v err=%v", restarted, err)
	}
	restarted.SandboxExecCommandReceipt = &platform.RemoteWorkerSandboxExecCommandReceipt{CommandID: command.CommandID,
		SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration, Result: "succeeded", ExitCode: 7,
		Stdout: "proof\n", Stderr: "failed", ExecutionTimeMillis: 1}
	if err := saveNodeState(path, restarted); err != nil {
		t.Fatal(err)
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID}, time.Now()); err != nil ||
		restarted.SandboxExecCommand != nil || restarted.SandboxExecCommandReceipt != nil {
		t.Fatalf("acknowledged state=%#v err=%v", restarted, err)
	}
}

func TestRemoteWorkerSandboxFileStateSurvivesRestartAndAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := initialNodeState("incarnation-alpha")
	command := &platform.RemoteWorkerSandboxFileCommand{CommandID: "rwfile-alpha", EventID: "file-alpha",
		GrantID: "grant-alpha", WorkspaceID: "workspace-alpha", TargetID: "target-alpha",
		SandboxID: "sandbox-alpha", SandboxGeneration: 3, RuntimeID: "runtime-alpha",
		RuntimeOperationID: "operation-alpha", RuntimeSpecDigest: "sha256:" + strings.Repeat("a", 64),
		Action: "list", Path: ".", Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID, SandboxFileCommand: command}, time.Now()); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadNodeState(path, state.IncarnationID)
	if err != nil || restarted.SandboxFileCommand == nil || restarted.SandboxFileCommand.CommandID != command.CommandID {
		t.Fatalf("restarted state=%#v err=%v", restarted, err)
	}
	restarted.SandboxFileCommandReceipt = &platform.RemoteWorkerSandboxFileCommandReceipt{CommandID: command.CommandID,
		EventID: command.EventID, GrantID: command.GrantID, SandboxID: command.SandboxID,
		SandboxGeneration: command.SandboxGeneration, Action: command.Action, Result: "succeeded",
		List: &platform.RemoteWorkerSandboxFileListResult{Entries: []platform.SandboxFileEntry{}}}
	if err := saveNodeState(path, restarted); err != nil {
		t.Fatal(err)
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID}, time.Now()); err != nil ||
		restarted.SandboxFileCommand != nil || restarted.SandboxFileCommandReceipt != nil {
		t.Fatalf("acknowledged state=%#v err=%v", restarted, err)
	}
}

func TestRemoteWorkerSandboxPTYStateSurvivesRestartAndAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := initialNodeState("incarnation-alpha")
	since, takeover := int64(0), false
	command := &platform.RemoteWorkerSandboxPTYCommand{CommandID: "rwpty-alpha", GrantID: "grant-alpha",
		WorkspaceID: "workspace-alpha", TargetID: "target-alpha", SandboxID: "sandbox-alpha",
		SandboxGeneration: 3, RuntimeID: "runtime-alpha", RuntimeOperationID: "operation-alpha",
		RuntimeSpecDigest: "sha256:" + strings.Repeat("a", 64), Action: "exchange", SessionID: "session-alpha",
		Since: &since, Takeover: &takeover, Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID, SandboxPTYCommand: command}, time.Now()); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadNodeState(path, state.IncarnationID)
	if err != nil || restarted.SandboxPTYCommand == nil || restarted.SandboxPTYCommand.CommandID != command.CommandID {
		t.Fatalf("restarted state=%#v err=%v", restarted, err)
	}
	frames := []platform.RemoteWorkerSandboxPTYFrame{}
	restarted.SandboxPTYCommandReceipt = &platform.RemoteWorkerSandboxPTYCommandReceipt{CommandID: command.CommandID,
		GrantID: command.GrantID, SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration,
		Action: command.Action, Result: "succeeded", SessionID: command.SessionID, Running: boolPointer(true),
		OutputOffset: int64Pointer(0), Frames: &frames}
	if err := saveNodeState(path, restarted); err != nil {
		t.Fatal(err)
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID}, time.Now()); err != nil ||
		restarted.SandboxPTYCommand != nil || restarted.SandboxPTYCommandReceipt != nil {
		t.Fatalf("acknowledged state=%#v err=%v", restarted, err)
	}
}

func TestRemoteWorkerSandboxPreviewStateSurvivesRestartAndAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := initialNodeState("incarnation-alpha")
	command := &platform.RemoteWorkerSandboxPreviewCommand{CommandID: "rwpreview-alpha", GrantID: "grant-alpha",
		WorkspaceID: "workspace-alpha", TargetID: "target-alpha", SandboxID: "sandbox-alpha",
		SandboxGeneration: 3, RuntimeID: "runtime-alpha", RuntimeOperationID: "operation-alpha",
		RuntimeSpecDigest: "sha256:" + strings.Repeat("a", 64), Port: 3000, Method: "GET", Path: "/",
		Headers: []platform.RemoteWorkerSandboxPreviewHeader{}, Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID, SandboxPreviewCommand: command}, time.Now()); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadNodeState(path, state.IncarnationID)
	if err != nil || restarted.SandboxPreviewCommand == nil || restarted.SandboxPreviewCommand.CommandID != command.CommandID {
		t.Fatalf("restarted state=%#v err=%v", restarted, err)
	}
	status, body := int64(200), ""
	headers := []platform.RemoteWorkerSandboxPreviewHeader{}
	restarted.SandboxPreviewCommandReceipt = &platform.RemoteWorkerSandboxPreviewCommandReceipt{CommandID: command.CommandID,
		GrantID: command.GrantID, SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration,
		Port: command.Port, Result: "succeeded", StatusCode: &status, Headers: &headers, BodyBase64URL: &body}
	if err := saveNodeState(path, restarted); err != nil {
		t.Fatal(err)
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID}, time.Now()); err != nil ||
		restarted.SandboxPreviewCommand != nil || restarted.SandboxPreviewCommandReceipt != nil {
		t.Fatalf("acknowledged state=%#v err=%v", restarted, err)
	}
}

func TestBoundSandboxPTYBinaryFramePreservesReplayCursor(t *testing.T) {
	payload := append([]byte{3, 0, 0, 0, 0, 0, 0, 4, 0}, []byte("0123456789")...)
	bounded, offset, truncated, err := boundSandboxPTYBinaryFrame(payload, 1024, 12)
	if err != nil || !truncated || len(bounded) != 12 || offset != 1027 {
		t.Fatalf("bounded=%v offset=%d truncated=%v err=%v", bounded, offset, truncated, err)
	}
	if _, _, _, err := boundSandboxPTYBinaryFrame(payload, 0, 12); !errors.Is(err, opensandbox.ErrUnavailable) {
		t.Fatalf("accepted replay cursor gap: %v", err)
	}
	if _, _, _, err := boundSandboxPTYBinaryFrame(payload, 2048, 12); !errors.Is(err, opensandbox.ErrUnavailable) {
		t.Fatalf("accepted replay cursor regression: %v", err)
	}
}

func TestReadSandboxPTYMessageUsesNormalIdleClose(t *testing.T) {
	closeCode := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			closeCode <- 0
			return
		}
		defer connection.Close()
		connection.SetCloseHandler(func(code int, _ string) error {
			closeCode <- code
			return nil
		})
		_, _, err = connection.ReadMessage()
		if err == nil {
			closeCode <- 0
		}
	}))
	defer server.Close()

	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	messageType, payload, pollComplete, err := readSandboxPTYMessage(context.Background(), connection, 10*time.Millisecond)
	if err != nil || messageType != 0 || payload != nil || !pollComplete {
		t.Fatalf("messageType=%d payload=%q pollComplete=%v err=%v", messageType, payload, pollComplete, err)
	}
	select {
	case code := <-closeCode:
		if code != websocket.CloseNormalClosure {
			t.Fatalf("close code=%d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not observe the close handshake")
	}
}

func TestReadSandboxPTYMessageUsesNormalContextClose(t *testing.T) {
	closeCode := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			closeCode <- 0
			return
		}
		defer connection.Close()
		connection.SetCloseHandler(func(code int, _ string) error {
			closeCode <- code
			return nil
		})
		_, _, _ = connection.ReadMessage()
	}))
	defer server.Close()

	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	messageType, payload, pollComplete, err := readSandboxPTYMessage(ctx, connection, time.Second)
	if !errors.Is(err, context.Canceled) || messageType != 0 || payload != nil || pollComplete {
		t.Fatalf("messageType=%d payload=%q pollComplete=%v err=%v", messageType, payload, pollComplete, err)
	}
	select {
	case code := <-closeCode:
		if code != websocket.CloseNormalClosure {
			t.Fatalf("close code=%d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not observe the close handshake")
	}
}

func TestExchangeSandboxPTYRecoversAbruptProxyCloseFromAuthoritativeState(t *testing.T) {
	identity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: "workspace",
		Sandbox: "sandbox", Operation: "operation", Generation: 1, SpecDigest: "sha256:" + strings.Repeat("a", 64)}
	var connections atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/sandboxes/runtime-alpha":
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": "runtime-alpha", "metadata": identity.Labels(), "status": map[string]string{"state": "Running"}})
		case "/v1/sandboxes/runtime-alpha/endpoints/44772":
			_ = json.NewEncoder(writer).Encode(map[string]any{"endpoint": server.URL + "/v1/sandboxes/runtime-alpha/proxy/44772", "headers": map[string]string{}})
		case "/v1/sandboxes/runtime-alpha/proxy/44772/pty/session-alpha":
			_ = json.NewEncoder(writer).Encode(map[string]any{"session_id": "session-alpha", "running": true, "output_offset": 0})
		case "/v1/sandboxes/runtime-alpha/proxy/44772/pty/session-alpha/ws":
			connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
			if err != nil {
				return
			}
			connections.Add(1)
			_ = connection.UnderlyingConn().Close()
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := opensandbox.New(server.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	since, takeover, pty := int64(0), true, false
	frames, offset, running, err := exchangeSandboxPTY(context.Background(), client,
		opensandbox.PTYInput{Identity: identity, RuntimeID: "runtime-alpha"},
		platform.RemoteWorkerSandboxPTYCommand{SessionID: "session-alpha", Since: &since, Takeover: &takeover, PTY: &pty})
	if err != nil || frames == nil || len(frames) != 0 || offset != 0 || !running || connections.Load() != 3 {
		t.Fatalf("frames=%v offset=%d running=%v connections=%d err=%v", frames, offset, running, connections.Load(), err)
	}
}

func TestExchangeSandboxPTYConsumesReplayBeforeConnected(t *testing.T) {
	identity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: "workspace",
		Sandbox: "sandbox", Operation: "operation", Generation: 1, SpecDigest: "sha256:" + strings.Repeat("a", 64)}
	replay := append([]byte{3, 0, 0, 0, 0, 0, 0, 0, 4}, []byte("done\n")...)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/sandboxes/runtime-alpha":
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": "runtime-alpha", "metadata": identity.Labels(), "status": map[string]string{"state": "Running"}})
		case "/v1/sandboxes/runtime-alpha/endpoints/44772":
			_ = json.NewEncoder(writer).Encode(map[string]any{"endpoint": server.URL + "/v1/sandboxes/runtime-alpha/proxy/44772", "headers": map[string]string{}})
		case "/v1/sandboxes/runtime-alpha/proxy/44772/pty/session-alpha/ws":
			connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			_ = connection.WriteMessage(websocket.BinaryMessage, replay)
			_ = connection.WriteJSON(map[string]string{"type": "connected", "session_id": "session-alpha", "mode": "pipe"})
			_, _, _ = connection.ReadMessage()
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := opensandbox.New(server.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	since, takeover, pty := int64(4), true, false
	frames, offset, running, err := exchangeSandboxPTY(context.Background(), client,
		opensandbox.PTYInput{Identity: identity, RuntimeID: "runtime-alpha"},
		platform.RemoteWorkerSandboxPTYCommand{SessionID: "session-alpha", Since: &since, Takeover: &takeover, PTY: &pty})
	if err != nil || len(frames) != 1 || frames[0].MessageType != "binary" || offset != 9 || !running {
		t.Fatalf("frames=%v offset=%d running=%v err=%v", frames, offset, running, err)
	}
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(frames[0].PayloadBase64URL)
	if decodeErr != nil || !bytes.Equal(decoded, replay) {
		t.Fatalf("decoded=%v decodeErr=%v", decoded, decodeErr)
	}
}

func TestExchangeSandboxPTYRetriesBeforeWritingInput(t *testing.T) {
	identity := opensandbox.Identity{Tenant: "tenant", Project: "project", Workspace: "workspace",
		Sandbox: "sandbox", Operation: "operation", Generation: 1, SpecDigest: "sha256:" + strings.Repeat("a", 64)}
	var connections atomic.Int32
	received := make(chan []byte, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/sandboxes/runtime-alpha":
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": "runtime-alpha", "metadata": identity.Labels(), "status": map[string]string{"state": "Running"}})
		case "/v1/sandboxes/runtime-alpha/endpoints/44772":
			_ = json.NewEncoder(writer).Encode(map[string]any{"endpoint": server.URL + "/v1/sandboxes/runtime-alpha/proxy/44772", "headers": map[string]string{}})
		case "/v1/sandboxes/runtime-alpha/proxy/44772/pty/session-alpha/ws":
			connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
			if err != nil {
				return
			}
			if connections.Add(1) == 1 {
				_ = connection.UnderlyingConn().Close()
				return
			}
			defer connection.Close()
			_ = connection.WriteJSON(map[string]string{"type": "connected", "session_id": "session-alpha", "mode": "pipe"})
			_, payload, readErr := connection.ReadMessage()
			if readErr == nil {
				received <- payload
			}
			_, _, _ = connection.ReadMessage()
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := opensandbox.New(server.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	since, takeover, pty := int64(0), true, false
	input := []byte{0, 'h', 'i', '\n'}
	frames, offset, running, err := exchangeSandboxPTY(context.Background(), client,
		opensandbox.PTYInput{Identity: identity, RuntimeID: "runtime-alpha"},
		platform.RemoteWorkerSandboxPTYCommand{SessionID: "session-alpha", Since: &since, Takeover: &takeover, PTY: &pty,
			Input: &platform.RemoteWorkerSandboxPTYFrame{MessageType: "binary", PayloadBase64URL: base64.RawURLEncoding.EncodeToString(input)}})
	if err != nil || len(frames) != 0 || offset != 0 || !running || connections.Load() != 2 {
		t.Fatalf("frames=%v offset=%d running=%v connections=%d err=%v", frames, offset, running, connections.Load(), err)
	}
	select {
	case payload := <-received:
		if !bytes.Equal(payload, input) {
			t.Fatalf("input=%q", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("input was not delivered after safe attach retry")
	}
}
