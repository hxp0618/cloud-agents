// Package runtime defines the JSON protocol shared by the Cloud Agent Runtime,
// Worker, and Control Plane clients.
package runtime

import (
	"errors"
	"fmt"
)

const (
	ProtocolMajor   = 2
	ProtocolMinor   = 3
	MaxCommandBytes = 2 * 1024 * 1024
	MaxMessageBytes = 1 * 1024 * 1024
)

var (
	ErrInvalidCommand    = errors.New("runtime command is invalid")
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

func ValidateCommand(command Command) error {
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

func ValidateMessage(message Message) error {
	if message.Protocol.Major != ProtocolMajor || message.Protocol.Minor > ProtocolMinor ||
		message.RequestID == "" || message.ExecutionID == "" || message.CommandID == "" ||
		message.OccurredAt == "" || message.MessageType == "" {
		return fmt.Errorf("%w: message envelope fields are invalid", ErrProtocolViolation)
	}
	if message.MessageType == "Error" && message.Error == nil {
		return fmt.Errorf("%w: Error message has no error body", ErrProtocolViolation)
	}
	switch message.MessageType {
	case "Error", "Event", "InteractionRequest", "ArtifactCandidate", "Checkpoint", "Result", "Progress":
		return nil
	default:
		return fmt.Errorf("%w: unknown message type", ErrProtocolViolation)
	}
}
