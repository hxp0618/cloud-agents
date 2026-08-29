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
	"os/exec"
	"strings"
	"sync"
)

const (
	ProtocolMajor       = 2
	ProtocolMinor       = 3
	MaxCommandBytes     = 2 * 1024 * 1024
	MaxMessageBytes     = 1 * 1024 * 1024
	maxEventQueueItems  = 128
	maxCommandTombstone = 4096
)

var (
	ErrNilContext        = errors.New("runtime client context is nil")
	ErrInvalidCommand    = errors.New("runtime client command is invalid")
	ErrClientClosed      = errors.New("runtime client is closed")
	ErrProtocolViolation = errors.New("runtime protocol violation")
)

type Command struct {
	RequestID   string         `json:"requestId"`
	Protocol    Protocol       `json:"protocolVersion"`
	ExecutionID string         `json:"executionId"`
	Generation  uint64         `json:"generation"`
	CommandType string         `json:"commandType"`
	CommandID   string         `json:"commandId"`
	OccurredAt  string         `json:"occurredAt"`
	Payload     map[string]any `json:"payload"`
}

type Protocol struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
}

type Error struct {
	Code                  string `json:"code"`
	Message               string `json:"message"`
	Retryable             bool   `json:"retryable"`
	RequiresNewExecution  bool   `json:"requiresNewExecution"`
	RequiresUserAction    bool   `json:"requiresUserAction"`
	CanReconstructHistory bool   `json:"canReconstructFromHistory"`
	CanMoveWorker         bool   `json:"canMoveWorker"`
}

type Message struct {
	RequestID   string         `json:"requestId"`
	Protocol    Protocol       `json:"protocolVersion"`
	ExecutionID string         `json:"executionId"`
	Generation  uint64         `json:"generation"`
	CommandID   string         `json:"commandId"`
	OccurredAt  string         `json:"occurredAt"`
	MessageType string         `json:"messageType"`
	Payload     map[string]any `json:"payload,omitempty"`
	Error       *Error         `json:"error,omitempty"`
}

type Config struct {
	Command     []string
	Environment []string
	Directory   string
}

type Client struct {
	stdin   io.WriteCloser
	process *exec.Cmd
	done    chan struct{}
	events  chan Message

	mu          sync.Mutex
	writes      sync.Mutex
	pending     map[string]pendingRequest
	tombstones  map[string]struct{}
	closed      bool
	terminalErr error
}

type result struct {
	message Message
	err     error
}

type pendingRequest struct {
	command Command
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
		return nil, fmt.Errorf("%w: runtime command is required", ErrInvalidCommand)
	}
	command := append([]string(nil), config.Command...)
	process := exec.Command(command[0], command[1:]...)
	process.Dir = config.Directory
	if config.Environment != nil {
		process.Env = append([]string(nil), config.Environment...)
	}
	stdin, err := process.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("runtime stdin: %w", err)
	}
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
		events:  make(chan Message, maxEventQueueItems),
		pending: make(map[string]pendingRequest), tombstones: make(map[string]struct{}),
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go client.run(stdout)
	return client, nil
}

func (client *Client) Events() <-chan Message {
	if client == nil {
		return nil
	}
	// The channel is the ordered output stream. It includes terminal messages
	// so callers can forward Runtime output without racing Execute waiters.
	return client.events
}

func (client *Client) Execute(ctx context.Context, command Command) (Message, error) {
	if ctx == nil {
		return Message{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return Message{}, err
	}
	if err := validateCommand(command); err != nil {
		return Message{}, err
	}
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) > MaxCommandBytes {
		return Message{}, fmt.Errorf("%w: command exceeds %d bytes", ErrInvalidCommand, MaxCommandBytes)
	}
	encoded = append(encoded, '\n')
	waiter := make(chan result, 1)
	client.mu.Lock()
	if client.closed {
		err := client.terminalErr
		client.mu.Unlock()
		if err != nil {
			return Message{}, err
		}
		return Message{}, ErrClientClosed
	}
	if _, exists := client.pending[command.CommandID]; exists {
		client.mu.Unlock()
		return Message{}, fmt.Errorf("%w: duplicate command id", ErrInvalidCommand)
	}
	if _, exists := client.tombstones[command.CommandID]; exists {
		client.mu.Unlock()
		return Message{}, fmt.Errorf("%w: command id was already cancelled", ErrInvalidCommand)
	}
	client.pending[command.CommandID] = pendingRequest{command: command, waiter: waiter}
	client.mu.Unlock()

	client.writes.Lock()
	_, writeErr := client.stdin.Write(encoded)
	client.writes.Unlock()
	if writeErr != nil {
		client.removePending(command.CommandID, writeErr)
		return Message{}, writeErr
	}
	select {
	case response := <-waiter:
		return response.message, response.err
	case <-ctx.Done():
		client.cancelPending(command.CommandID)
		return Message{}, ctx.Err()
	case <-client.done:
		client.mu.Lock()
		err := client.terminalErr
		client.mu.Unlock()
		if err == nil {
			err = ErrClientClosed
		}
		return Message{}, err
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

func (client *Client) run(stdout io.ReadCloser) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), MaxMessageBytes+1)
	var runErr error
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > MaxMessageBytes {
			runErr = fmt.Errorf("%w: message exceeds %d bytes", ErrProtocolViolation, MaxMessageBytes)
			break
		}
		var message Message
		if err := json.Unmarshal(line, &message); err != nil {
			runErr = fmt.Errorf("%w: invalid JSON", ErrProtocolViolation)
			break
		}
		if err := validateMessage(message); err != nil {
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
		runErr = fmt.Errorf("%w: message exceeds %d bytes", ErrProtocolViolation, MaxMessageBytes)
	}
	_ = stdout.Close()
	if client.process.Process != nil && runErr != nil {
		_ = client.process.Process.Kill()
	}
	waitErr := client.process.Wait()
	if runErr == nil && waitErr != nil {
		runErr = fmt.Errorf("runtime exited: %w", waitErr)
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

func (client *Client) deliverTerminal(message Message) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	pending, ok := client.pending[message.CommandID]
	if !ok {
		if _, cancelled := client.tombstones[message.CommandID]; cancelled {
			delete(client.tombstones, message.CommandID)
			return nil
		}
		return fmt.Errorf("%w: unknown command id", ErrProtocolViolation)
	}
	if message.RequestID != pending.command.RequestID || message.ExecutionID != pending.command.ExecutionID || message.Generation != pending.command.Generation {
		delete(client.pending, message.CommandID)
		err := fmt.Errorf("%w: response identity does not match command", ErrProtocolViolation)
		pending.waiter <- result{err: err}
		return err
	}
	select {
	case client.events <- message:
	default:
		delete(client.pending, message.CommandID)
		err := fmt.Errorf("%w: event queue is full", ErrProtocolViolation)
		pending.waiter <- result{err: err}
		return err
	}
	delete(client.pending, message.CommandID)
	pending.waiter <- result{message: message, err: messageError(message)}
	return nil
}

func (client *Client) deliverEvent(message Message) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	pending, ok := client.pending[message.CommandID]
	if !ok {
		if _, cancelled := client.tombstones[message.CommandID]; cancelled {
			return nil
		}
		return fmt.Errorf("%w: unknown event command id", ErrProtocolViolation)
	}
	if message.RequestID != pending.command.RequestID || message.ExecutionID != pending.command.ExecutionID || message.Generation != pending.command.Generation {
		return fmt.Errorf("%w: event identity does not match command", ErrProtocolViolation)
	}
	select {
	case client.events <- message:
		return nil
	default:
		return fmt.Errorf("%w: event queue is full", ErrProtocolViolation)
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

func validateCommand(command Command) error {
	if command.Protocol.Major != ProtocolMajor || command.Protocol.Minor > ProtocolMinor ||
		command.RequestID == "" || command.ExecutionID == "" || command.CommandType == "" ||
		command.CommandID == "" || command.OccurredAt == "" || command.Generation == 0 || command.Payload == nil {
		return fmt.Errorf("%w: envelope fields are invalid", ErrInvalidCommand)
	}
	switch command.CommandType {
	case "Describe", "StartSession", "ResumeSession", "SendTurn", "SteerTurn", "InterruptTurn", "SuspendTurn", "ResolveApproval", "ResolveUserInput", "CompactSession", "RollbackSession", "ForkSession", "StartReview", "GenerateText", "StopSession":
		return nil
	default:
		return fmt.Errorf("%w: unknown command type", ErrInvalidCommand)
	}
}

// ValidateCommand applies the same closed Runtime command vocabulary used by
// the process client. It is also used by the Supervisor transport wrapper.
func ValidateCommand(command Command) error { return validateCommand(command) }

func validateMessage(message Message) error {
	if message.Protocol.Major != ProtocolMajor || message.Protocol.Minor > ProtocolMinor ||
		message.RequestID == "" || message.ExecutionID == "" || message.CommandID == "" ||
		message.OccurredAt == "" || message.MessageType == "" {
		return fmt.Errorf("%w: message envelope fields are invalid", ErrProtocolViolation)
	}
	if message.MessageType == "Error" && message.Error == nil {
		return fmt.Errorf("%w: Error message has no error body", ErrProtocolViolation)
	}
	if message.MessageType != "Error" && message.MessageType != "Event" && message.MessageType != "InteractionRequest" && message.MessageType != "ArtifactCandidate" && message.MessageType != "Checkpoint" && message.MessageType != "Result" && message.MessageType != "Progress" {
		return fmt.Errorf("%w: unknown message type", ErrProtocolViolation)
	}
	return nil
}

// ValidateMessage applies the Runtime message envelope checks at a transport
// boundary before a message is exposed to a caller.
func ValidateMessage(message Message) error { return validateMessage(message) }

func messageError(message Message) error {
	if message.MessageType != "Error" || message.Error == nil {
		return nil
	}
	return fmt.Errorf("runtime %s: %s", message.Error.Code, message.Error.Message)
}
