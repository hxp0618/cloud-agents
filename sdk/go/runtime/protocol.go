// Package runtime defines the JSON protocol shared by the Cloud Agent Runtime,
// Worker, and Control Plane clients.
package runtime

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
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
		!validEnvelopeIdentifier(message.RequestID) || !validEnvelopeIdentifier(message.ExecutionID) ||
		!validEnvelopeIdentifier(message.CommandID) || message.Generation == 0 ||
		!validOccurredAt(message.OccurredAt) || message.MessageType == "" {
		return fmt.Errorf("%w: message envelope fields are invalid", ErrProtocolViolation)
	}
	switch message.MessageType {
	case "Error":
		if message.Payload != nil || !validRuntimeError(message.Error) {
			return fmt.Errorf("%w: Error message body is invalid", ErrProtocolViolation)
		}
		return nil
	case "Event", "InteractionRequest", "ArtifactCandidate", "Checkpoint", "Result", "Progress":
		if message.Payload == nil || message.Error != nil {
			return fmt.Errorf("%w: payload message body is invalid", ErrProtocolViolation)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown message type", ErrProtocolViolation)
	}
}

func validEnvelopeIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= 200 && utf8.ValidString(value)
}

func validOccurredAt(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func validRuntimeError(value *Error) bool {
	if value == nil || value.Message == "" || len(value.Message) > 4096 || !utf8.ValidString(value.Message) {
		return false
	}
	switch value.Code {
	case "provider_not_installed", "provider_version_incompatible", "capability_unsupported", "credential_missing", "credential_invalid", "authentication_required", "session_resume_invalid", "session_resume_expired", "provider_rate_limited", "provider_unavailable", "workspace_invalid", "protocol_violation", "cancelled", "interrupted", "internal_error":
		return true
	default:
		return false
	}
}
