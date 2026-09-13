package managedagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

type FoundationRemoteRuntimeHandle struct {
	Scope           Scope
	GrantID         string
	TokenDigest     string
	SessionID       string
	SandboxID       string
	Generation      int64
	ResourceVersion int64
}

type FoundationRemoteRuntimeFrame struct {
	MessageType string
	Payload     []byte
}

type FoundationRemoteRuntimeExchange struct {
	Frames       []FoundationRemoteRuntimeFrame
	OutputOffset int64
	Running      bool
}

type FoundationRemoteRuntimeStore interface {
	OpenFoundationRemoteRuntime(context.Context, VerifiedPrincipalSource, RuntimeSessionSnapshot, string) (FoundationRemoteRuntimeHandle, error)
	ExchangeFoundationRemoteRuntime(context.Context, FoundationRemoteRuntimeHandle, uint64, int64, []byte) (FoundationRemoteRuntimeExchange, error)
	CloseFoundationRemoteRuntime(context.Context, VerifiedPrincipalSource, FoundationRemoteRuntimeHandle) error
	ReadFoundationRemoteArtifact(context.Context, VerifiedPrincipalSource, RuntimeSessionSnapshot, string) ([]byte, error)
}

type remoteFoundationRuntimeSession struct {
	store           FoundationRemoteRuntimeStore
	principalSource VerifiedPrincipalSource
	handle          FoundationRemoteRuntimeHandle
	executionID     string
	generation      uint64
	incoming        chan runtimeprotocol.Message
	done            chan struct{}
	ctx             context.Context
	cancel          context.CancelFunc
	writes          sync.Mutex
	closeOnce       sync.Once
	finishOnce      sync.Once
	errMu           sync.Mutex
	terminalErr     error
	errRead         bool
	sequence        uint64
	since           int64
	stdout          []byte
}

func newRemoteFoundationRuntimeSession(store FoundationRemoteRuntimeStore, principalSource VerifiedPrincipalSource, handle FoundationRemoteRuntimeHandle, executionID string, generation uint64) *remoteFoundationRuntimeSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &remoteFoundationRuntimeSession{store: store, principalSource: principalSource, handle: handle,
		executionID: executionID, generation: generation, incoming: make(chan runtimeprotocol.Message, 128),
		done: make(chan struct{}), ctx: ctx, cancel: cancel}
}

func (session *remoteFoundationRuntimeSession) start() { go session.run() }

func (session *remoteFoundationRuntimeSession) bootstrap(ctx context.Context, command string) error {
	if session == nil || ctx == nil || command == "" {
		return ErrDurableRuntimeExecutionUnavailable
	}
	bootstrapContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var output []byte
	input := []byte(command + "\n")
	for {
		session.sequence++
		result, err := session.store.ExchangeFoundationRemoteRuntime(bootstrapContext, session.handle, session.sequence, session.since, input)
		if err != nil {
			return ErrDurableRuntimeExecutionUnavailable
		}
		input = nil
		session.since = result.OutputOffset
		for _, frame := range result.Frames {
			messageType := foundationRuntimeFrameBinary
			if frame.MessageType == "text" {
				messageType = foundationRuntimeFrameText
			} else if frame.MessageType != "binary" {
				return runtimeprotocol.ErrProtocolViolation
			}
			ready, frameErr := consumeFoundationRuntimeInputReadyFrame(messageType, frame.Payload, &output)
			if frameErr != nil {
				return frameErr
			}
			if ready {
				return nil
			}
		}
		if !result.Running {
			return ErrDurableRuntimeExecutionUnavailable
		}
	}
}

func (session *remoteFoundationRuntimeSession) Send(ctx context.Context, command runtimeprotocol.Command) error {
	if session == nil || ctx == nil || command.ExecutionID != session.executionID || command.Generation != session.generation || runtimeprotocol.ValidateCommand(command) != nil {
		return ErrDurableRuntimeExecutionUnavailable
	}
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) > runtimeprotocol.MaxCommandBytes {
		return ErrDurableRuntimeExecutionUnavailable
	}
	return session.write(ctx, append(encoded, '\n'))
}

func (session *remoteFoundationRuntimeSession) write(ctx context.Context, payload []byte) error {
	if session == nil || ctx == nil {
		return ErrDurableRuntimeExecutionUnavailable
	}
	session.writes.Lock()
	defer session.writes.Unlock()
	select {
	case <-session.done:
		return ErrDurableRuntimeExecutionUnavailable
	default:
	}
	return session.exchange(ctx, payload)
}

func (session *remoteFoundationRuntimeSession) exchange(ctx context.Context, payload []byte) error {
	session.sequence++
	result, err := session.store.ExchangeFoundationRemoteRuntime(ctx, session.handle, session.sequence, session.since, payload)
	if err != nil {
		return ErrDurableRuntimeExecutionUnavailable
	}
	session.since = result.OutputOffset
	for _, frame := range result.Frames {
		messageType := foundationRuntimeFrameBinary
		if frame.MessageType == "text" {
			messageType = foundationRuntimeFrameText
		} else if frame.MessageType != "binary" {
			return runtimeprotocol.ErrProtocolViolation
		}
		if err := consumeFoundationRuntimeFrame(messageType, frame.Payload, &session.stdout, session.executionID, session.generation, session.incoming); err != nil {
			if errors.Is(err, io.EOF) {
				session.finish(err)
				return nil
			}
			return err
		}
	}
	if !result.Running {
		session.finish(io.EOF)
	}
	return nil
}

func (session *remoteFoundationRuntimeSession) run() {
	for {
		select {
		case <-session.done:
			return
		case <-time.After(100 * time.Millisecond):
		}
		session.writes.Lock()
		err := session.exchange(session.ctx, nil)
		session.writes.Unlock()
		if err != nil {
			session.finish(err)
			return
		}
	}
}

func (session *remoteFoundationRuntimeSession) Receive() (runtimeprotocol.Message, error) {
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

func (session *remoteFoundationRuntimeSession) CloseRequest() error { return session.CloseResponse() }

func (session *remoteFoundationRuntimeSession) CloseResponse() error {
	if session == nil {
		return nil
	}
	session.closeOnce.Do(func() {
		session.cancel()
		session.writes.Lock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = session.store.CloseFoundationRemoteRuntime(ctx, session.principalSource, session.handle)
		session.writes.Unlock()
		session.finish(io.EOF)
	})
	return nil
}

func (session *remoteFoundationRuntimeSession) finish(err error) {
	session.finishOnce.Do(func() {
		session.cancel()
		err = finishFoundationRuntimeOutput(session.stdout, err)
		session.errMu.Lock()
		session.terminalErr = err
		session.errMu.Unlock()
		close(session.incoming)
		close(session.done)
	})
}

func consumeFoundationRuntimeFrame(messageType int, payload []byte, stdout *[]byte, executionID string, generation uint64, incoming chan<- runtimeprotocol.Message) error {
	content, err := foundationRuntimeFrameContent(messageType, payload)
	if err != nil || content == nil {
		return err
	}
	*stdout = append(*stdout, content...)
	for {
		newline := bytes.IndexByte(*stdout, '\n')
		if newline < 0 {
			if len(*stdout) > runtimeprotocol.MaxMessageBytes {
				return runtimeprotocol.ErrProtocolViolation
			}
			return nil
		}
		line := (*stdout)[:newline]
		*stdout = (*stdout)[newline+1:]
		if len(line) == 0 {
			continue
		}
		var message runtimeprotocol.Message
		if len(line) > runtimeprotocol.MaxMessageBytes || json.Unmarshal(line, &message) != nil || runtimeprotocol.ValidateMessage(message) != nil || message.ExecutionID != executionID || message.Generation != generation {
			return runtimeprotocol.ErrProtocolViolation
		}
		select {
		case incoming <- message:
		default:
			return runtimeprotocol.ErrProtocolViolation
		}
	}
}

func consumeFoundationRuntimeInputReadyFrame(messageType int, payload []byte, output *[]byte) (bool, error) {
	content, err := foundationRuntimeFrameContent(messageType, payload)
	if err != nil || content == nil {
		return false, err
	}
	*output = append(*output, content...)
	if len(*output) > 64<<10 {
		return false, runtimeprotocol.ErrProtocolViolation
	}
	return bytes.Contains(*output, []byte(foundationRuntimeInputReady)), nil
}

func foundationRuntimeFrameContent(messageType int, payload []byte) ([]byte, error) {
	switch messageType {
	case foundationRuntimeFrameBinary:
		if len(payload) < 1 {
			return nil, runtimeprotocol.ErrProtocolViolation
		}
		switch payload[0] {
		case 1:
			return payload[1:], nil
		case 2:
			return nil, nil
		case 3:
			if len(payload) < 9 {
				return nil, runtimeprotocol.ErrProtocolViolation
			}
			return payload[9:], nil
		default:
			return nil, runtimeprotocol.ErrProtocolViolation
		}
	case foundationRuntimeFrameText:
		var control struct {
			Type string `json:"type"`
			Code string `json:"code"`
		}
		if json.Unmarshal(payload, &control) != nil {
			return nil, runtimeprotocol.ErrProtocolViolation
		}
		switch control.Type {
		case "connected":
			return nil, nil
		case "exit":
			return nil, io.EOF
		case "error":
			return nil, fmt.Errorf("%w: %s", ErrDurableRuntimeExecutionUnavailable, control.Code)
		default:
			return nil, runtimeprotocol.ErrProtocolViolation
		}
	default:
		return nil, runtimeprotocol.ErrProtocolViolation
	}
}

func finishFoundationRuntimeOutput(stdout []byte, err error) error {
	if len(bytes.TrimSpace(stdout)) != 0 && errors.Is(err, io.EOF) {
		return runtimeprotocol.ErrProtocolViolation
	}
	return err
}

var _ runtimeSession = (*remoteFoundationRuntimeSession)(nil)
