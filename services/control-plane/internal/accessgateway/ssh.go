package accessgateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgrant"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"golang.org/x/crypto/ssh"
)

const (
	sshHandshakeTimeout = 10 * time.Second
	sshWriteTimeout     = 5 * time.Second
)

type sshIdentity struct{ tenant, project, grant, tokenDigest string }

type sshBridge struct {
	server    *Server
	identity  sshIdentity
	authority postgres.SandboxAccessGrantAuthority
	remote    bool
	pty       bool
	since     int64
	columns   uint32
	rows      uint32
	client    *opensandbox.Client
	input     opensandbox.PTYInput
	sessionID string
	websocket *websocket.Conn
	writeMu   sync.Mutex
	pending   []sshCandidateFrame
}

type sshCandidateFrame struct {
	stream   byte
	data     []byte
	event    string
	exitCode int
}

type sshPTYRequest struct {
	Term                      string
	Columns, Rows             uint32
	WidthPixels, HeightPixels uint32
	Modes                     string
}

type sshWindowChange struct{ Columns, Rows, WidthPixels, HeightPixels uint32 }

func (server *Server) ServeSSH(ctx context.Context, listener net.Listener, signer ssh.Signer) error {
	if server == nil || ctx == nil || listener == nil || signer == nil {
		return errors.New("SSH access Gateway configuration is invalid")
	}
	configuration := &ssh.ServerConfig{
		MaxAuthTries: 3,
		PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			tenant, project, grant, ok := accessgrant.ParseSSHUsername(metadata.User())
			token := string(password)
			if !ok {
				return nil, errors.New("SSH authentication failed")
			}
			if _, valid := bearer([]string{"Bearer " + token}); !valid {
				return nil, errors.New("SSH authentication failed")
			}
			authContext, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			digest := accessgrant.Digest(token)
			if _, err := server.store.ResolveGrant(authContext, tenant, project, grant, digest); err != nil {
				return nil, errors.New("SSH authentication failed")
			}
			return &ssh.Permissions{Extensions: map[string]string{
				"tenant": tenant, "project": project, "grant": grant, "tokenDigest": digest,
			}}, nil
		},
	}
	configuration.AddHostKey(signer)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return errors.New("SSH access Gateway stopped")
		}
		go server.serveSSHConnection(ctx, connection, configuration)
	}
}

func (server *Server) serveSSHConnection(ctx context.Context, raw net.Conn, configuration *ssh.ServerConfig) {
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(sshHandshakeTimeout))
	connection, channels, requests, err := ssh.NewServerConn(raw, configuration)
	if err != nil {
		return
	}
	_ = raw.SetDeadline(time.Time{})
	connectionContext, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = connection.Wait()
		cancel()
	}()
	go func() {
		for request := range requests {
			_ = request.Reply(false, nil)
		}
	}()
	identity, ok := sshIdentityFromPermissions(connection.Permissions)
	if !ok {
		_ = connection.Close()
		return
	}
	go server.watchSSHGrant(connectionContext, connection, identity)
	accepted := false
	var sessions sync.WaitGroup
	for channelRequest := range channels {
		if accepted || channelRequest.ChannelType() != "session" {
			_ = channelRequest.Reject(ssh.Prohibited, "only one Sandbox session channel is allowed")
			continue
		}
		channel, channelRequests, err := channelRequest.Accept()
		if err != nil {
			continue
		}
		accepted = true
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			server.serveSSHSession(connectionContext, channel, channelRequests, identity)
		}()
	}
	cancel()
	sessions.Wait()
}

func sshIdentityFromPermissions(permissions *ssh.Permissions) (sshIdentity, bool) {
	if permissions == nil || permissions.Extensions == nil {
		return sshIdentity{}, false
	}
	value := sshIdentity{
		tenant: permissions.Extensions["tenant"], project: permissions.Extensions["project"],
		grant: permissions.Extensions["grant"], tokenDigest: permissions.Extensions["tokenDigest"],
	}
	tenant, project, grant, ok := accessgrant.ParseSSHUsername(value.tenant + ":" + value.project + ":" + value.grant)
	return value, ok && tenant == value.tenant && project == value.project && grant == value.grant && len(value.tokenDigest) == 71
}

func (server *Server) watchSSHGrant(ctx context.Context, connection *ssh.ServerConn, identity sshIdentity) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = connection.Close()
			return
		case <-ticker.C:
			checkContext, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := server.store.ResolveGrant(checkContext, identity.tenant, identity.project, identity.grant, identity.tokenDigest)
			cancel()
			if err != nil {
				_ = connection.Close()
				return
			}
		}
	}
}

func (server *Server) serveSSHSession(ctx context.Context, channel ssh.Channel, requests <-chan *ssh.Request, identity sshIdentity) {
	defer channel.Close()
	var terminal *sshPTYRequest
	for {
		select {
		case <-ctx.Done():
			return
		case request, open := <-requests:
			if !open {
				return
			}
			switch request.Type {
			case "pty-req":
				var value sshPTYRequest
				valid := terminal == nil && ssh.Unmarshal(request.Payload, &value) == nil && validTerminal(value)
				_ = request.Reply(valid, nil)
				if valid {
					terminal = &value
				}
			case "shell":
				if len(request.Payload) != 0 {
					_ = request.Reply(false, nil)
					continue
				}
				bridge, err := server.startSSHBridge(ctx, identity, "", terminal)
				_ = request.Reply(err == nil, nil)
				if err == nil {
					bridge.run(ctx, channel, requests, terminal != nil)
				}
				return
			case "exec":
				var value struct{ Command string }
				valid := ssh.Unmarshal(request.Payload, &value) == nil && validSSHCommand(value.Command)
				if !valid {
					_ = request.Reply(false, nil)
					continue
				}
				bridge, err := server.startSSHBridge(ctx, identity, value.Command, terminal)
				_ = request.Reply(err == nil, nil)
				if err == nil {
					bridge.run(ctx, channel, requests, terminal != nil)
				}
				return
			default:
				_ = request.Reply(false, nil)
			}
		}
	}
}

func validTerminal(value sshPTYRequest) bool {
	return len(value.Term) > 0 && len(value.Term) <= 128 && utf8.ValidString(value.Term) &&
		value.Columns > 0 && value.Columns <= 1000 && value.Rows > 0 && value.Rows <= 1000 && len(value.Modes) <= 4096
}

func validSSHCommand(command string) bool {
	return len(command) > 0 && len(command) <= 8192 && utf8.ValidString(command) && strings.IndexByte(command, 0) < 0
}

func (server *Server) startSSHBridge(ctx context.Context, identity sshIdentity, command string, terminal *sshPTYRequest) (*sshBridge, error) {
	authority, err := server.store.ResolveGrant(ctx, identity.tenant, identity.project, identity.grant, identity.tokenDigest)
	if err != nil {
		return nil, err
	}
	input := ptyInput(authority)
	input.Command = command
	var client *opensandbox.Client
	var created opensandbox.PTYObservation
	remote := authority.Access.TargetKind == "remote-worker"
	if remote {
		var receipt platform.RemoteWorkerSandboxPTYCommandReceipt
		receipt, err = server.executeRemotePTYReceipt(ctx, postgres.RemoteWorkerSandboxPTYRequest{
			Authority: authority, TokenDigest: identity.tokenDigest, Action: "create", SSH: true,
		})
		created = opensandbox.PTYObservation{SessionID: receipt.SessionID}
		if receipt.Running != nil {
			created.Running = *receipt.Running
		}
		if receipt.OutputOffset != nil {
			created.OutputOffset = *receipt.OutputOffset
		}
	} else {
		client, err = server.client(authority)
		if err == nil {
			created, err = client.CreatePTY(ctx, input)
		}
	}
	if err != nil {
		return nil, err
	}
	if _, err := server.store.PersistPTYSession(ctx, identity.tenant, identity.project, identity.grant, created.SessionID, identity.tokenDigest); err != nil {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if remote {
			_, _ = server.executeRemotePTYReceipt(cleanupContext, postgres.RemoteWorkerSandboxPTYRequest{
				Authority: authority, TokenDigest: identity.tokenDigest, Action: "delete",
				SessionID: created.SessionID, SSH: true,
			})
		} else {
			_ = client.DeletePTY(cleanupContext, input, created.SessionID)
		}
		return nil, err
	}
	bridge := &sshBridge{server: server, identity: identity, authority: authority, remote: remote,
		pty: terminal != nil, since: created.OutputOffset, client: client, input: input, sessionID: created.SessionID}
	if terminal != nil {
		bridge.columns, bridge.rows = terminal.Columns, terminal.Rows
	}
	if remote {
		connectContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := bridge.awaitRemoteConnected(connectContext); err != nil {
			bridge.close()
			return nil, err
		}
		if command != "" {
			payload := append([]byte{0}, []byte("exec /bin/sh -c "+shellQuote(command)+"\n")...)
			frames, running, err := bridge.remoteExchange(connectContext, &platform.RemoteWorkerSandboxPTYFrame{
				MessageType: "binary", PayloadBase64URL: base64.RawURLEncoding.EncodeToString(payload),
			})
			if err != nil {
				bridge.close()
				return nil, err
			}
			bridge.pending = append(bridge.pending, frames...)
			if !running && !slices.ContainsFunc(frames, func(frame sshCandidateFrame) bool { return frame.event == "exit" }) {
				bridge.close()
				return nil, opensandbox.ErrUnavailable
			}
		}
		return bridge, nil
	}
	target, headers, err := client.PTYWebSocketTarget(ctx, input, created.SessionID)
	if err != nil {
		bridge.close()
		return nil, err
	}
	switch target.Scheme {
	case "http":
		target.Scheme = "ws"
	case "https":
		target.Scheme = "wss"
	default:
		bridge.close()
		return nil, opensandbox.ErrUnavailable
	}
	query := target.Query()
	query.Set("takeover", "1")
	if terminal == nil {
		query.Set("pty", "0")
	}
	target.RawQuery = query.Encode()
	dialer := websocket.Dialer{HandshakeTimeout: sshHandshakeTimeout}
	connection, response, err := dialer.DialContext(ctx, target.String(), headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		bridge.close()
		return nil, opensandbox.ErrUnavailable
	}
	bridge.websocket = connection
	connection.SetReadLimit((1 << 20) + 9)
	if err := bridge.awaitConnected(); err != nil {
		bridge.close()
		return nil, err
	}
	if terminal != nil {
		if err := bridge.control(map[string]any{"type": "resize", "cols": terminal.Columns, "rows": terminal.Rows}); err != nil {
			bridge.close()
			return nil, err
		}
	}
	return bridge, nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func (bridge *sshBridge) remoteExchange(ctx context.Context, input *platform.RemoteWorkerSandboxPTYFrame) ([]sshCandidateFrame, bool, error) {
	pty := bridge.pty
	receipt, err := bridge.server.executeRemotePTYReceipt(ctx, postgres.RemoteWorkerSandboxPTYRequest{
		Authority: bridge.authority, TokenDigest: bridge.identity.tokenDigest, Action: "exchange",
		SessionID: bridge.sessionID, Since: bridge.since, Takeover: true, Input: input, PTY: &pty, SSH: true,
	})
	if err != nil || receipt.Running == nil || receipt.OutputOffset == nil || receipt.Frames == nil {
		if err != nil {
			return nil, false, err
		}
		return nil, false, opensandbox.ErrUnavailable
	}
	frames := make([]sshCandidateFrame, 0, len(*receipt.Frames))
	for _, remote := range *receipt.Frames {
		payload, decodeErr := base64.RawURLEncoding.Strict().DecodeString(remote.PayloadBase64URL)
		messageType := websocket.BinaryMessage
		if remote.MessageType == "text" {
			messageType = websocket.TextMessage
		}
		frame, frameErr := decodeSSHCandidateFrame(messageType, payload)
		if decodeErr != nil || frameErr != nil {
			return nil, false, opensandbox.ErrUnavailable
		}
		frames = append(frames, frame)
	}
	bridge.since = *receipt.OutputOffset
	return frames, *receipt.Running, nil
}

func (bridge *sshBridge) awaitRemoteConnected(ctx context.Context) error {
	for {
		frames, running, err := bridge.remoteExchange(ctx, nil)
		if err != nil {
			return err
		}
		connected := false
		for _, frame := range frames {
			switch frame.event {
			case "connected":
				connected = true
			case "":
				frame.data = append([]byte(nil), frame.data...)
				bridge.pending = append(bridge.pending, frame)
			default:
				bridge.pending = append(bridge.pending, frame)
			}
		}
		if connected {
			return nil
		}
		if !running {
			return opensandbox.ErrUnavailable
		}
	}
}

func (bridge *sshBridge) awaitConnected() error {
	_ = bridge.websocket.SetReadDeadline(time.Now().Add(sshHandshakeTimeout))
	defer bridge.websocket.SetReadDeadline(time.Time{})
	buffered := 0
	for {
		messageType, payload, err := bridge.websocket.ReadMessage()
		if err != nil {
			return opensandbox.ErrUnavailable
		}
		frame, err := decodeSSHCandidateFrame(messageType, payload)
		if err != nil {
			return err
		}
		if frame.event == "connected" {
			return nil
		}
		if frame.event != "" {
			return opensandbox.ErrUnavailable
		}
		buffered += len(frame.data)
		if buffered > 1<<20 {
			return opensandbox.ErrOutputLimit
		}
		frame.data = append([]byte(nil), frame.data...)
		bridge.pending = append(bridge.pending, frame)
	}
}

func decodeSSHCandidateFrame(messageType int, payload []byte) (sshCandidateFrame, error) {
	if messageType == websocket.BinaryMessage {
		if len(payload) < 1 {
			return sshCandidateFrame{}, opensandbox.ErrUnavailable
		}
		switch payload[0] {
		case 1, 2:
			return sshCandidateFrame{stream: payload[0], data: payload[1:]}, nil
		case 3:
			if len(payload) < 9 {
				return sshCandidateFrame{}, opensandbox.ErrUnavailable
			}
			return sshCandidateFrame{stream: 1, data: payload[9:]}, nil
		default:
			return sshCandidateFrame{}, opensandbox.ErrUnavailable
		}
	}
	if messageType != websocket.TextMessage || len(payload) > 64<<10 {
		return sshCandidateFrame{}, opensandbox.ErrUnavailable
	}
	var event struct {
		Type     string `json:"type"`
		ExitCode *int   `json:"exit_code"`
	}
	if json.Unmarshal(payload, &event) != nil {
		return sshCandidateFrame{}, opensandbox.ErrUnavailable
	}
	switch event.Type {
	case "connected":
		return sshCandidateFrame{event: event.Type}, nil
	case "exit":
		if event.ExitCode == nil || *event.ExitCode < 0 || *event.ExitCode > 255 {
			return sshCandidateFrame{}, opensandbox.ErrUnavailable
		}
		return sshCandidateFrame{event: event.Type, exitCode: *event.ExitCode}, nil
	default:
		return sshCandidateFrame{}, opensandbox.ErrUnavailable
	}
}

func (bridge *sshBridge) run(ctx context.Context, channel ssh.Channel, requests <-chan *ssh.Request, terminal bool) {
	if bridge.remote {
		bridge.runRemote(ctx, channel, requests, terminal)
		return
	}
	defer bridge.close()
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			_ = bridge.websocket.Close()
		case <-closed:
		}
	}()
	go bridge.forwardInput(channel)
	go bridge.forwardRequests(requests, terminal)
	for _, frame := range bridge.pending {
		if bridge.writeOutput(channel, frame) != nil {
			return
		}
	}
	for {
		messageType, payload, err := bridge.websocket.ReadMessage()
		if err != nil {
			return
		}
		frame, err := decodeSSHCandidateFrame(messageType, payload)
		if err != nil {
			return
		}
		if frame.event == "exit" {
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: uint32(frame.exitCode)}))
			return
		}
		if frame.event == "" && bridge.writeOutput(channel, frame) != nil {
			return
		}
	}
}

func sshControlFrame(value any) (*platform.RemoteWorkerSandboxPTYFrame, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &platform.RemoteWorkerSandboxPTYFrame{MessageType: "text",
		PayloadBase64URL: base64.RawURLEncoding.EncodeToString(payload)}, nil
}

func (bridge *sshBridge) runRemote(ctx context.Context, channel ssh.Channel, requests <-chan *ssh.Request, terminal bool) {
	defer bridge.close()
	inputs := make(chan *platform.RemoteWorkerSandboxPTYFrame, 8)
	if terminal {
		frame, err := sshControlFrame(map[string]any{"type": "resize", "cols": bridge.columns, "rows": bridge.rows})
		if err != nil {
			return
		}
		inputs <- frame
	}
	go bridge.forwardRemoteInput(ctx, channel, inputs)
	go bridge.forwardRemoteRequests(ctx, requests, terminal, inputs)
	for _, frame := range bridge.pending {
		if frame.event == "exit" {
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: uint32(frame.exitCode)}))
			return
		}
		if frame.event == "" && bridge.writeOutput(channel, frame) != nil {
			return
		}
	}
	for {
		var input *platform.RemoteWorkerSandboxPTYFrame
		select {
		case <-ctx.Done():
			return
		case input = <-inputs:
		case <-time.After(100 * time.Millisecond):
		}
		frames, running, err := bridge.remoteExchange(ctx, input)
		if err != nil {
			return
		}
		for _, frame := range frames {
			if frame.event == "exit" {
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: uint32(frame.exitCode)}))
				return
			}
			if frame.event == "" && bridge.writeOutput(channel, frame) != nil {
				return
			}
		}
		if !running {
			return
		}
	}
}

func (bridge *sshBridge) forwardRemoteInput(ctx context.Context, channel ssh.Channel, inputs chan<- *platform.RemoteWorkerSandboxPTYFrame) {
	buffer := make([]byte, 32<<10)
	for {
		count, err := channel.Read(buffer)
		if count > 0 {
			payload := make([]byte, count+1)
			copy(payload[1:], buffer[:count])
			frame := &platform.RemoteWorkerSandboxPTYFrame{MessageType: "binary",
				PayloadBase64URL: base64.RawURLEncoding.EncodeToString(payload)}
			select {
			case inputs <- frame:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (bridge *sshBridge) forwardRemoteRequests(ctx context.Context, requests <-chan *ssh.Request, terminal bool, inputs chan<- *platform.RemoteWorkerSandboxPTYFrame) {
	for request := range requests {
		var control any
		switch request.Type {
		case "window-change":
			var value sshWindowChange
			if terminal && ssh.Unmarshal(request.Payload, &value) == nil && value.Columns > 0 && value.Columns <= 1000 && value.Rows > 0 && value.Rows <= 1000 {
				control = map[string]any{"type": "resize", "cols": value.Columns, "rows": value.Rows}
			}
		case "signal":
			var value struct{ Signal string }
			if ssh.Unmarshal(request.Payload, &value) == nil && slices.Contains([]string{"INT", "TERM", "KILL", "HUP"}, value.Signal) {
				control = map[string]any{"type": "signal", "signal": "SIG" + value.Signal}
			}
		}
		frame, err := sshControlFrame(control)
		allowed := control != nil && err == nil
		if allowed {
			select {
			case inputs <- frame:
			case <-ctx.Done():
				allowed = false
			}
		}
		_ = request.Reply(allowed, nil)
	}
}

func (bridge *sshBridge) writeOutput(channel ssh.Channel, frame sshCandidateFrame) error {
	writer := io.Writer(channel)
	if frame.stream == 2 {
		writer = channel.Stderr()
	}
	_, err := writer.Write(frame.data)
	return err
}

func (bridge *sshBridge) forwardInput(channel ssh.Channel) {
	buffer := make([]byte, 32<<10)
	for {
		count, err := channel.Read(buffer)
		if count > 0 {
			frame := make([]byte, count+1)
			copy(frame[1:], buffer[:count])
			bridge.writeMu.Lock()
			_ = bridge.websocket.SetWriteDeadline(time.Now().Add(sshWriteTimeout))
			writeErr := bridge.websocket.WriteMessage(websocket.BinaryMessage, frame)
			bridge.writeMu.Unlock()
			if writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (bridge *sshBridge) forwardRequests(requests <-chan *ssh.Request, terminal bool) {
	for request := range requests {
		allowed := false
		switch request.Type {
		case "window-change":
			var value sshWindowChange
			allowed = terminal && ssh.Unmarshal(request.Payload, &value) == nil && value.Columns > 0 && value.Columns <= 1000 && value.Rows > 0 && value.Rows <= 1000 &&
				bridge.control(map[string]any{"type": "resize", "cols": value.Columns, "rows": value.Rows}) == nil
		case "signal":
			var value struct{ Signal string }
			if ssh.Unmarshal(request.Payload, &value) == nil {
				switch value.Signal {
				case "INT", "TERM", "KILL", "HUP":
					allowed = bridge.control(map[string]any{"type": "signal", "signal": "SIG" + value.Signal}) == nil
				}
			}
		}
		_ = request.Reply(allowed, nil)
	}
}

func (bridge *sshBridge) control(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	bridge.writeMu.Lock()
	defer bridge.writeMu.Unlock()
	_ = bridge.websocket.SetWriteDeadline(time.Now().Add(sshWriteTimeout))
	if err := bridge.websocket.WriteMessage(websocket.TextMessage, payload); err != nil {
		return opensandbox.ErrUnavailable
	}
	return nil
}

func (bridge *sshBridge) close() {
	if bridge.websocket != nil {
		_ = bridge.websocket.Close()
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if bridge.remote {
		_, _ = bridge.server.executeRemotePTYReceipt(cleanupContext, postgres.RemoteWorkerSandboxPTYRequest{
			Authority: bridge.authority, TokenDigest: bridge.identity.tokenDigest, Action: "delete",
			SessionID: bridge.sessionID, SSH: true,
		})
	} else {
		_ = bridge.client.DeletePTY(cleanupContext, bridge.input, bridge.sessionID)
	}
	_ = bridge.server.store.MarkPTYSessionDeleted(cleanupContext, bridge.identity.tenant, bridge.identity.project, bridge.identity.grant, bridge.sessionID, bridge.identity.tokenDigest)
}
