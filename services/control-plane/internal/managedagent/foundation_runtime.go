package managedagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
)

const (
	maxFoundationProviderCredentialBytes = (64 << 10) - 1
	foundationRuntimeInputReady          = "\x1ecloud-agent-runtime-input-ready\x1f"
	foundationManagedWriteDelayEnv       = "CLOUD_AGENT_CODEX_MANAGED_WRITE_RECEIPT_DELAY_MS"
	maxFoundationManagedWriteDelayMS     = 30_000
)

const (
	foundationRuntimeFrameBinary = websocket.BinaryMessage
	foundationRuntimeFrameText   = websocket.TextMessage
)

type FoundationRuntime struct {
	sandboxes   *opensandbox.CredentialDirectory
	credentials string
	remote      FoundationRemoteRuntimeStore
}

func NewFoundationRuntime(sandboxes *opensandbox.CredentialDirectory, providerCredentialDirectory string, remote ...FoundationRemoteRuntimeStore) (*FoundationRuntime, error) {
	if sandboxes == nil && (len(remote) == 0 || remote[0] == nil) {
		return nil, ErrDurableRuntimeExecutionUnavailable
	}
	info, err := os.Lstat(providerCredentialDirectory)
	if err != nil || !filepath.IsAbs(providerCredentialDirectory) || filepath.Clean(providerCredentialDirectory) != providerCredentialDirectory || providerCredentialDirectory == string(filepath.Separator) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrDurableRuntimeExecutionUnavailable
	}
	result := &FoundationRuntime{sandboxes: sandboxes, credentials: providerCredentialDirectory}
	if len(remote) > 0 {
		result.remote = remote[0]
	}
	return result, nil
}

func (runtime *FoundationRuntime) open(ctx context.Context, principalSource VerifiedPrincipalSource, session RuntimeSessionSnapshot, executionID string) (runtimeSession, error) {
	if runtime == nil || ctx == nil || !session.FoundationSandboxReady || executionID == "" ||
		session.FoundationTargetKind != "docker" && session.FoundationTargetKind != "kubernetes" && session.FoundationTargetKind != "remote-worker" {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	credential, err := readFoundationProviderCredential(runtime.credentials, session.Scope.TenantID, session.ProviderKind)
	if err != nil {
		return nil, err
	}
	command := foundationRuntimeCommand(len(credential), session.ProviderKind, session.FoundationTargetKind, executionID)
	if session.FoundationTargetKind == "remote-worker" {
		if runtime.remote == nil || principalSource == nil {
			return nil, ErrRuntimeEnvironmentUnavailable
		}
		handle, openErr := runtime.remote.OpenFoundationRemoteRuntime(ctx, principalSource, session, executionID)
		if openErr != nil {
			return nil, ErrRuntimeEnvironmentUnavailable
		}
		result := newRemoteFoundationRuntimeSession(runtime.remote, principalSource, handle, executionID, session.SandboxGeneration)
		if result.bootstrap(ctx, command) != nil {
			_ = result.CloseResponse()
			return nil, ErrRuntimeEnvironmentUnavailable
		}
		if result.write(ctx, credential) != nil {
			_ = result.CloseResponse()
			return nil, ErrRuntimeEnvironmentUnavailable
		}
		result.start()
		return result, nil
	}
	if runtime.sandboxes == nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	client, err := runtime.sandboxes.Client(session.FoundationTargetCredential)
	if err != nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	input := foundationPTYInput(session, "")
	created, err := client.CreatePTY(ctx, input)
	if err != nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	target, headers, err := client.PTYWebSocketTarget(ctx, input, created.SessionID)
	if err != nil {
		_ = client.DeletePTY(context.Background(), input, created.SessionID)
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	switch target.Scheme {
	case "http":
		target.Scheme = "ws"
	case "https":
		target.Scheme = "wss"
	default:
		_ = client.DeletePTY(context.Background(), input, created.SessionID)
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	query := target.Query()
	query.Set("since", "0")
	query.Set("takeover", "1")
	query.Set("pty", "0")
	target.RawQuery = query.Encode()
	connection, response, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).DialContext(ctx, target.String(), headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		_ = client.DeletePTY(context.Background(), input, created.SessionID)
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	if err := opensandbox.AwaitPTYConnected(ctx, connection); err != nil {
		_ = connection.Close()
		_ = client.DeletePTY(context.Background(), input, created.SessionID)
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	connection.SetReadLimit(runtimeprotocol.MaxMessageBytes + 64<<10)
	frame := append([]byte{0}, []byte(command+"\n")...)
	if err := connection.WriteMessage(websocket.BinaryMessage, frame); err != nil || awaitFoundationRuntimeInputReady(ctx, connection) != nil {
		_ = connection.Close()
		_ = client.DeletePTY(context.Background(), input, created.SessionID)
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	result := &foundationRuntimeSession{
		client: client, pty: input, sessionID: created.SessionID, connection: connection,
		executionID: executionID, generation: session.SandboxGeneration,
		incoming: make(chan runtimeprotocol.Message, 128), done: make(chan struct{}),
	}
	go result.run()
	if err := result.write(credential); err != nil {
		_ = result.CloseResponse()
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	return result, nil
}

func (runtime *FoundationRuntime) readArtifact(ctx context.Context, principalSource VerifiedPrincipalSource, session RuntimeSessionSnapshot, rootDirectory, relativePath string, expectedSize *uint64, expectedSHA256 string) ([]byte, error) {
	if runtime == nil || ctx == nil || !session.FoundationSandboxReady || session.FoundationTargetKind != "docker" && session.FoundationTargetKind != "kubernetes" && session.FoundationTargetKind != "remote-worker" {
		return nil, ErrRuntimeArtifactUnavailable
	}
	relativeRoot, err := filepath.Rel("/workspace", filepath.Clean(rootDirectory))
	if err != nil || relativeRoot == ".." || strings.HasPrefix(relativeRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeRoot) {
		return nil, ErrRuntimeArtifactUnavailable
	}
	artifactPath := filepath.ToSlash(filepath.Join(relativeRoot, relativePath))
	if session.FoundationTargetKind == "remote-worker" {
		if runtime.remote == nil || principalSource == nil {
			return nil, ErrRuntimeArtifactUnavailable
		}
		data, readErr := runtime.remote.ReadFoundationRemoteArtifact(ctx, principalSource, session, artifactPath)
		if readErr != nil {
			return nil, ErrRuntimeArtifactUnavailable
		}
		digest := sha256.Sum256(data)
		if expectedSize != nil && uint64(len(data)) != *expectedSize || expectedSHA256 != "" && hex.EncodeToString(digest[:]) != expectedSHA256 {
			return nil, ErrRuntimeArtifactUnavailable
		}
		return data, nil
	}
	client, err := runtime.sandboxes.Client(session.FoundationTargetCredential)
	if err != nil {
		return nil, ErrRuntimeArtifactUnavailable
	}
	input := foundationPTYInput(session, "")
	var artifact bytes.Buffer
	var version string
	var total int64 = -1
	for offset := int64(0); total < 0 || offset < total; {
		page, readErr := client.ReadFile(ctx, input, artifactPath, offset, 1<<20, version)
		if readErr != nil || page.TotalBytes < 0 || page.TotalBytes > runtimeprotocol.MaxArtifactBytes || total >= 0 && page.TotalBytes != total {
			return nil, ErrRuntimeArtifactUnavailable
		}
		if version == "" {
			version, total = page.FileVersion, page.TotalBytes
		}
		if len(page.Content) == 0 && offset < total {
			return nil, ErrRuntimeArtifactUnavailable
		}
		_, _ = artifact.Write(page.Content)
		offset += int64(len(page.Content))
	}
	data := artifact.Bytes()
	digest := sha256.Sum256(data)
	if expectedSize != nil && uint64(len(data)) != *expectedSize || expectedSHA256 != "" && hex.EncodeToString(digest[:]) != expectedSHA256 {
		return nil, ErrRuntimeArtifactUnavailable
	}
	return data, nil
}

func foundationPTYInput(session RuntimeSessionSnapshot, command string) opensandbox.PTYInput {
	return opensandbox.PTYInput{Identity: opensandbox.Identity{
		Tenant: session.Scope.TenantID, Project: session.Scope.ProjectID,
		Workspace: session.WorkspaceID, Sandbox: session.SandboxID,
		Operation: session.FoundationRuntimeOperation, Generation: int64(session.SandboxGeneration),
		SpecDigest: session.FoundationRuntimeSpecDigest,
	}, RuntimeID: session.FoundationRuntimeID, Command: command}
}

func foundationRuntimeCommand(credentialBytes int, providerKind, targetKind string, executionIDs ...string) string {
	outerSandbox := "single-tenant-trusted-v1"
	if targetKind == "kubernetes" {
		outerSandbox = "kubernetes-restricted-v1"
	}
	runtimeEnvironment := "CLOUD_AGENT_PROVIDER_CREDENTIAL_FD=3 CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=" + providerKind + " CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=" + outerSandbox
	if delay := foundationManagedWriteDelay(); delay != "" {
		runtimeEnvironment += " " + foundationManagedWriteDelayEnv + "=" + delay
	}
	fence := ""
	if len(executionIDs) > 0 && executionIDs[0] != "" {
		digest := sha256.Sum256([]byte(executionIDs[0]))
		pidFile := "/tmp/cloud-agents-runtime/" + hex.EncodeToString(digest[:]) + ".pid"
		fence = "mkdir -p /tmp/cloud-agents-runtime || exit 68; pid_file=" + pidFile + "; if test -s \"$pid_file\"; then old_pid=$(cat \"$pid_file\" 2>/dev/null || true); case \"$old_pid\" in ''|*[!0-9]*) old_pid=;; esac; if test -n \"$old_pid\" && test \"$old_pid\" != \"$$\" && test -r \"/proc/$old_pid/cmdline\"; then old_cmd=$(tr '\\000' ' ' <\"/proc/$old_pid/cmdline\" 2>/dev/null || true); case \"$old_cmd\" in *cloud-agent-runtime*) kill -KILL \"$old_pid\" 2>/dev/null || true;; esac; fi; fi; printf '%s\\n' \"$$\" >\"$pid_file\" || exit 68; "
	}
	return "if test -t 0; then stty -echo 2>/dev/null || exit 69; fi; printf '\\036cloud-agent-runtime-input-ready\\037\\n' || exit 69; umask 077; " + fence + "credential=$(mktemp /tmp/cloud-agent-credential.XXXXXX) || exit 70; trap 'if test -f \"$pid_file\" && test \"$(cat \"$pid_file\" 2>/dev/null)\" = \"$$\"; then rm -f \"$pid_file\"; fi; rm -f \"$credential\"' EXIT HUP INT TERM; dd if=/dev/stdin of=\"$credential\" bs=1 count=" + strconv.Itoa(credentialBytes) + " status=none || exit 71; exec 3<\"$credential\"; rm -f \"$credential\"; trap - EXIT; export " + runtimeEnvironment + "; exec /usr/local/bin/cloud-agent-runtime"
}

func foundationManagedWriteDelay() string {
	value := strings.TrimSpace(os.Getenv(foundationManagedWriteDelayEnv))
	milliseconds, err := strconv.Atoi(value)
	if err != nil || milliseconds <= 0 || milliseconds > maxFoundationManagedWriteDelayMS {
		return ""
	}
	return strconv.Itoa(milliseconds)
}

func awaitFoundationRuntimeInputReady(ctx context.Context, connection *websocket.Conn) error {
	deadline := time.Now().Add(5 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetReadDeadline(deadline); err != nil {
		return ErrDurableRuntimeExecutionUnavailable
	}
	defer connection.SetReadDeadline(time.Time{}) //nolint:errcheck
	var output []byte
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return ErrDurableRuntimeExecutionUnavailable
		}
		ready, err := consumeFoundationRuntimeInputReadyFrame(messageType, payload, &output)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
	}
}

func readFoundationProviderCredential(directory, tenantID, providerKind string) ([]byte, error) {
	if validateIdentifier(tenantID, maxIdentifierBytes, "tenant id") != nil || validateIdentifier(providerKind, maxProviderBytes, "provider kind") != nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	defer root.Close()
	name := tenantID + "." + providerKind + ".json"
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maxFoundationProviderCredentialBytes {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	credential, readErr := io.ReadAll(io.LimitReader(file, maxFoundationProviderCredentialBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(credential) < 1 || len(credential) > maxFoundationProviderCredentialBytes {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	return credential, nil
}

type foundationRuntimeSession struct {
	client      *opensandbox.Client
	pty         opensandbox.PTYInput
	sessionID   string
	connection  *websocket.Conn
	executionID string
	generation  uint64
	incoming    chan runtimeprotocol.Message
	done        chan struct{}
	writes      sync.Mutex
	closeOnce   sync.Once
	errMu       sync.Mutex
	terminalErr error
	errRead     bool
}

func (session *foundationRuntimeSession) Send(ctx context.Context, command runtimeprotocol.Command) error {
	if session == nil || ctx == nil || command.ExecutionID != session.executionID || command.Generation != session.generation || runtimeprotocol.ValidateCommand(command) != nil {
		return ErrDurableRuntimeExecutionUnavailable
	}
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) > runtimeprotocol.MaxCommandBytes {
		return ErrDurableRuntimeExecutionUnavailable
	}
	return session.write(append(encoded, '\n'))
}

func (session *foundationRuntimeSession) write(payload []byte) error {
	session.writes.Lock()
	defer session.writes.Unlock()
	select {
	case <-session.done:
		return ErrDurableRuntimeExecutionUnavailable
	default:
	}
	frame := make([]byte, len(payload)+1)
	copy(frame[1:], payload)
	if err := session.connection.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return ErrDurableRuntimeExecutionUnavailable
	}
	return nil
}

func (session *foundationRuntimeSession) Receive() (runtimeprotocol.Message, error) {
	message, ok := <-session.incoming
	if ok {
		return message, nil
	}
	session.errMu.Lock()
	defer session.errMu.Unlock()
	if session.terminalErr != nil && !session.errRead {
		session.errRead = true
		return runtimeprotocol.Message{}, session.terminalErr
	}
	return runtimeprotocol.Message{}, io.EOF
}

func (session *foundationRuntimeSession) CloseRequest() error { return session.CloseResponse() }

func (session *foundationRuntimeSession) CloseResponse() error {
	if session == nil {
		return nil
	}
	session.closeOnce.Do(func() {
		closeFoundationRuntimeConnection(session.connection)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = session.client.DeletePTY(ctx, session.pty, session.sessionID)
	})
	return nil
}

func (session *foundationRuntimeSession) run() {
	defer close(session.done)
	var stdout []byte
	var runErr error
	for runErr == nil {
		messageType, payload, err := session.connection.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				runErr = io.EOF
			} else {
				runErr = ErrDurableRuntimeExecutionUnavailable
			}
			break
		}
		runErr = consumeFoundationRuntimeFrame(messageType, payload, &stdout, session.executionID, session.generation, session.incoming)
	}
	runErr = finishFoundationRuntimeOutput(stdout, runErr)
	session.errMu.Lock()
	session.terminalErr = runErr
	session.errMu.Unlock()
	close(session.incoming)
}

var _ runtimeSession = (*foundationRuntimeSession)(nil)

func closeFoundationRuntimeConnection(connection *websocket.Conn) {
	if connection == nil {
		return
	}
	deadline := time.Now().Add(time.Second)
	if connection.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), deadline) == nil {
		_ = connection.SetReadDeadline(deadline)
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				break
			}
		}
	}
	_ = connection.Close()
}
