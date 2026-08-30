// Package runtime contains the small process boundary used by a Worker to
// speak to the independent Cloud Agent Runtime.
package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

const (
	maxEventQueueItems  = 128
	maxCommandTombstone = 4096
	runtimeStopTimeout  = 5 * time.Second
)

var (
	ErrNilContext   = errors.New("runtime client context is nil")
	ErrClientClosed = errors.New("runtime client is closed")
)

type Config struct {
	Command        []string
	Environment    []string
	Directory      string
	CredentialFile string
}

type Client struct {
	stdin   io.WriteCloser
	process *exec.Cmd
	done    chan struct{}
	events  chan runtimeprotocol.Message

	mu          sync.Mutex
	writes      sync.Mutex
	pending     map[string]pendingRequest
	tombstones  map[string]struct{}
	closed      bool
	terminalErr error
}

type result struct {
	message runtimeprotocol.Message
	err     error
}

type pendingRequest struct {
	command runtimeprotocol.Command
	waiter  chan result
}

func New(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(config.Command) == 0 || strings.TrimSpace(config.Command[0]) == "" {
		return nil, fmt.Errorf("%w: runtime command is required", runtimeprotocol.ErrInvalidCommand)
	}
	command := append([]string(nil), config.Command...)
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Dir = config.Directory
	if config.Environment != nil {
		process.Env = append([]string(nil), config.Environment...)
	}
	var credential *os.File
	if config.CredentialFile != "" {
		var err error
		credential, err = os.Open(config.CredentialFile)
		if err != nil {
			return nil, fmt.Errorf("runtime credential: %w", err)
		}
		defer credential.Close()
		process.ExtraFiles = []*os.File{credential}
		process.Env = credentialEnvironment(process.Env)
	}
	stdin, err := process.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("runtime stdin: %w", err)
	}
	process.Cancel = func() error {
		_ = stdin.Close()
		return nil
	}
	process.WaitDelay = runtimeStopTimeout
	stdout, err := process.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("runtime stdout: %w", err)
	}
	stderr, err := process.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("runtime stderr: %w", err)
	}
	if err := process.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("runtime start: %w", err)
	}

	client := &Client{
		stdin: stdin, process: process, done: make(chan struct{}),
		events:  make(chan runtimeprotocol.Message, maxEventQueueItems),
		pending: make(map[string]pendingRequest), tombstones: make(map[string]struct{}),
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go client.run(stdout)
	return client, nil
}

func credentialEnvironment(environment []string) []string {
	if environment == nil {
		environment = os.Environ()
	}
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "CLOUD_AGENT_PROVIDER_CREDENTIAL_FD=") && !strings.HasPrefix(entry, "SYNARA_PROVIDER_CREDENTIAL_FD=") {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, "CLOUD_AGENT_PROVIDER_CREDENTIAL_FD=3")
}

func (client *Client) Events() <-chan runtimeprotocol.Message {
	if client == nil {
		return nil
	}
	// The channel is the ordered output stream. It includes terminal messages
	// so callers can forward Runtime output without racing Execute waiters.
	return client.events
}

func (client *Client) Execute(ctx context.Context, command runtimeprotocol.Command) (runtimeprotocol.Message, error) {
	if ctx == nil {
		return runtimeprotocol.Message{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return runtimeprotocol.Message{}, err
	}
	if err := runtimeprotocol.ValidateCommand(command); err != nil {
		return runtimeprotocol.Message{}, err
	}
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) > runtimeprotocol.MaxCommandBytes {
		return runtimeprotocol.Message{}, fmt.Errorf("%w: command exceeds %d bytes", runtimeprotocol.ErrInvalidCommand, runtimeprotocol.MaxCommandBytes)
	}
	encoded = append(encoded, '\n')
	waiter := make(chan result, 1)
	client.mu.Lock()
	if client.closed {
		err := client.terminalErr
		client.mu.Unlock()
		if err != nil {
			return runtimeprotocol.Message{}, err
		}
		return runtimeprotocol.Message{}, ErrClientClosed
	}
	if _, exists := client.pending[command.CommandID]; exists {
		client.mu.Unlock()
		return runtimeprotocol.Message{}, fmt.Errorf("%w: duplicate command id", runtimeprotocol.ErrInvalidCommand)
	}
	if _, exists := client.tombstones[command.CommandID]; exists {
		client.mu.Unlock()
		return runtimeprotocol.Message{}, fmt.Errorf("%w: command id was already cancelled", runtimeprotocol.ErrInvalidCommand)
	}
	client.pending[command.CommandID] = pendingRequest{command: command, waiter: waiter}
	client.mu.Unlock()

	client.writes.Lock()
	_, writeErr := client.stdin.Write(encoded)
	client.writes.Unlock()
	if writeErr != nil {
		client.removePending(command.CommandID, writeErr)
		return runtimeprotocol.Message{}, writeErr
	}
	select {
	case response := <-waiter:
		return response.message, response.err
	case <-ctx.Done():
		client.cancelPending(command.CommandID)
		return runtimeprotocol.Message{}, ctx.Err()
	case <-client.done:
		client.mu.Lock()
		err := client.terminalErr
		client.mu.Unlock()
		if err == nil {
			err = ErrClientClosed
		}
		return runtimeprotocol.Message{}, err
	}
}

func (client *Client) Close(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	if client == nil {
		return nil
	}
	client.mu.Lock()
	if !client.closed {
		client.closed = true
		client.terminalErr = ErrClientClosed
	}
	client.mu.Unlock()
	_ = client.stdin.Close()
	timer := time.NewTimer(runtimeStopTimeout)
	defer timer.Stop()
	select {
	case <-client.done:
		return nil
	case <-ctx.Done():
		if client.process.Process != nil {
			_ = client.process.Process.Kill()
		}
		return ctx.Err()
	case <-timer.C:
		if client.process.Process != nil {
			_ = client.process.Process.Kill()
		}
		select {
		case <-client.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (client *Client) run(stdout io.ReadCloser) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), runtimeprotocol.MaxMessageBytes+1)
	var runErr error
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > runtimeprotocol.MaxMessageBytes {
			runErr = fmt.Errorf("%w: message exceeds %d bytes", runtimeprotocol.ErrProtocolViolation, runtimeprotocol.MaxMessageBytes)
			break
		}
		var message runtimeprotocol.Message
		if err := json.Unmarshal(line, &message); err != nil {
			runErr = fmt.Errorf("%w: invalid JSON", runtimeprotocol.ErrProtocolViolation)
			break
		}
		if err := runtimeprotocol.ValidateMessage(message); err != nil {
			runErr = err
			break
		}
		if message.MessageType == "Result" || message.MessageType == "Error" {
			if err := client.deliverTerminal(message); err != nil {
				runErr = err
				break
			}
			continue
		}
		if err := client.deliverEvent(message); err != nil {
			runErr = err
		}
		if runErr != nil {
			break
		}
	}
	if runErr == nil && scanner.Err() != nil {
		runErr = fmt.Errorf("%w: message exceeds %d bytes", runtimeprotocol.ErrProtocolViolation, runtimeprotocol.MaxMessageBytes)
	}
	_ = stdout.Close()
	if client.process.Process != nil && runErr != nil {
		_ = client.process.Process.Kill()
	}
	waitErr := client.process.Wait()
	if runErr == nil {
		if waitErr != nil {
			runErr = fmt.Errorf("runtime exited: %w", waitErr)
		} else {
			runErr = fmt.Errorf("%w: runtime exited before a terminal response", runtimeprotocol.ErrProtocolViolation)
		}
	}
	client.finish(runErr)
}

func (client *Client) finish(err error) {
	client.mu.Lock()
	if client.closed && client.terminalErr == ErrClientClosed {
		// Preserve the caller-visible close error while still releasing waiters.
	} else if err != nil {
		client.terminalErr = err
	}
	client.closed = true
	for commandID, pending := range client.pending {
		pending.waiter <- result{err: client.terminalErr}
		delete(client.pending, commandID)
	}
	close(client.done)
	close(client.events)
	client.mu.Unlock()
}

func (client *Client) deliverTerminal(message runtimeprotocol.Message) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	pending, ok := client.pending[message.CommandID]
	if !ok {
		if _, cancelled := client.tombstones[message.CommandID]; cancelled {
			delete(client.tombstones, message.CommandID)
			return nil
		}
		return fmt.Errorf("%w: unknown command id", runtimeprotocol.ErrProtocolViolation)
	}
	if message.RequestID != pending.command.RequestID || message.ExecutionID != pending.command.ExecutionID || message.Generation != pending.command.Generation {
		delete(client.pending, message.CommandID)
		err := fmt.Errorf("%w: response identity does not match command", runtimeprotocol.ErrProtocolViolation)
		pending.waiter <- result{err: err}
		return err
	}
	select {
	case client.events <- message:
	default:
		delete(client.pending, message.CommandID)
		err := fmt.Errorf("%w: event queue is full", runtimeprotocol.ErrProtocolViolation)
		pending.waiter <- result{err: err}
		return err
	}
	delete(client.pending, message.CommandID)
	pending.waiter <- result{message: message, err: messageError(message)}
	return nil
}

func (client *Client) deliverEvent(message runtimeprotocol.Message) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	pending, ok := client.pending[message.CommandID]
	if !ok {
		if _, cancelled := client.tombstones[message.CommandID]; cancelled {
			return nil
		}
		return fmt.Errorf("%w: unknown event command id", runtimeprotocol.ErrProtocolViolation)
	}
	if message.RequestID != pending.command.RequestID || message.ExecutionID != pending.command.ExecutionID || message.Generation != pending.command.Generation {
		return fmt.Errorf("%w: event identity does not match command", runtimeprotocol.ErrProtocolViolation)
	}
	select {
	case client.events <- message:
		return nil
	default:
		return fmt.Errorf("%w: event queue is full", runtimeprotocol.ErrProtocolViolation)
	}
}

func (client *Client) removePending(commandID string, err error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if pending, ok := client.pending[commandID]; ok {
		delete(client.pending, commandID)
		pending.waiter <- result{err: err}
	}
}

func (client *Client) cancelPending(commandID string) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if _, ok := client.pending[commandID]; !ok {
		return
	}
	delete(client.pending, commandID)
	client.tombstones[commandID] = struct{}{}
	for len(client.tombstones) > maxCommandTombstone {
		for oldest := range client.tombstones {
			delete(client.tombstones, oldest)
			break
		}
	}
}

func messageError(message runtimeprotocol.Message) error {
	if message.MessageType != "Error" || message.Error == nil {
		return nil
	}
	return fmt.Errorf("runtime %s: %s", message.Error.Code, message.Error.Message)
}
