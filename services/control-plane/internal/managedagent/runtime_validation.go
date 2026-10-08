package managedagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

func runtimeInteractionIdentity(message runtimeprotocol.Message) (string, string, error) {
	requestID, requestOK := message.Payload["requestId"].(string)
	interactionType, typeOK := message.Payload["interactionType"].(string)
	if !requestOK || validateRuntimeInteractionToken(requestID, "interaction request id") != nil || !typeOK || interactionType != "approval" && interactionType != "user-input" {
		return "", "", errors.New("Runtime interaction request payload is invalid")
	}
	return requestID, interactionType, nil
}

func validateRuntimeInteractionToken(value, field string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRuntimeInteractionRequestIDCharacters {
		return fmt.Errorf("%w: %s", ErrInvalidInput, field)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("%w: %s", ErrInvalidInput, field)
		}
	}
	return nil
}

func RuntimeMessageDigest(message runtimeprotocol.Message) (string, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func RuntimeMessagesDigest(messages []runtimeprotocol.Message, executionID string, generation uint64) (string, error) {
	_, digest, err := EncodeRuntimeMessages(messages, executionID, generation)
	return digest, err
}

// EncodeRuntimeMessages validates a checkpoint transcript and returns the bytes used by its digest.
func EncodeRuntimeMessages(messages []runtimeprotocol.Message, executionID string, generation uint64) ([]byte, string, error) {
	encoded, err := encodeRuntimeMessageTranscript(messages, executionID, generation, false)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(encoded)
	return encoded, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func RuntimeInteractionResolutionDigest(input ResolveRuntimeInteractionInput) (string, string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil ||
		validateRuntimeInteractionToken(input.InteractionRequestID, "interaction request id") != nil ||
		validateIdentifier(input.RequestID, maxIdentifierBytes, "request id") != nil ||
		(input.InteractionType != "approval" && input.InteractionType != "user-input") || input.Payload == nil {
		return "", "", ErrInvalidInput
	}
	encoded, err := json.Marshal(input.Payload)
	if err != nil || len(encoded) < 2 || len(encoded) > 65536 {
		return "", "", ErrInvalidInput
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), string(encoded), nil
}

func RuntimeSideEffectReconciliationDigest(input ReconcileRuntimeSideEffectInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil ||
		validateIdentifier(input.RequestID, maxIdentifierBytes, "request id") != nil ||
		validateToken(input.IdempotencyKey, maxMutationBytes, "idempotency key") != nil ||
		validateDigest(input.CheckpointDigest, "checkpoint digest") != nil ||
		(input.Outcome != "confirmed" && input.Outcome != "not-applied") {
		return "", ErrInvalidInput
	}
	encoded, err := json.Marshal(struct {
		CheckpointDigest string `json:"checkpointDigest"`
		IdempotencyKey   string `json:"idempotencyKey"`
		Outcome          string `json:"outcome"`
	}{input.CheckpointDigest, input.IdempotencyKey, input.Outcome})
	if err != nil {
		return "", ErrInvalidInput
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ValidRuntimeExecutionClaim(claim RuntimeExecutionClaim) bool {
	return claim.Acquired && claim.AttemptNumber > 0 && claim.ExpiresAt.IsZero() == false &&
		validateExecutionInput(claim.Scope, claim.SessionID, claim.TurnID, claim.ExecutionID, claim.Generation) == nil &&
		validateIdentifier(claim.HolderID, maxIdentifierBytes, "claim holder") == nil &&
		validateIdentifier(claim.Incarnation, maxIdentifierBytes, "claim incarnation") == nil &&
		validateIdentifier(claim.Token, maxIdentifierBytes, "claim token") == nil
}

func validateRuntimeTerminalMessage(input CompleteRuntimeExecutionInput) error {
	if err := validateRuntimeMessageTranscript(input.Messages, input.ExecutionID, input.Generation, true); err != nil {
		return fmt.Errorf("%w: Runtime terminal message", ErrInvalidInput)
	}
	digest, err := RuntimeMessageDigest(input.Messages[len(input.Messages)-1])
	if err != nil || digest != input.ResultDigest {
		return fmt.Errorf("%w: Runtime terminal digest", ErrInvalidInput)
	}
	return nil
}

func validateRuntimeFailureMessages(input FailRuntimeExecutionInput) error {
	err := validateRuntimeMessageTranscript(input.Messages, input.ExecutionID, input.Generation, false)
	if err == nil || input.ErrorCode != "runtime_result_invalid" {
		return err
	}
	return validateRuntimeMessageTranscript(input.Messages, input.ExecutionID, input.Generation, true)
}

func validateRuntimeMessageTranscript(messages []runtimeprotocol.Message, executionID string, generation uint64, requireResult bool) error {
	_, err := encodeRuntimeMessageTranscript(messages, executionID, generation, requireResult)
	return err
}

func encodeRuntimeMessageTranscript(messages []runtimeprotocol.Message, executionID string, generation uint64, requireResult bool) ([]byte, error) {
	if len(messages) == 0 {
		if requireResult {
			return nil, ErrInvalidInput
		}
		encoded, err := json.Marshal(messages)
		if err != nil {
			return nil, ErrInvalidInput
		}
		return encoded, nil
	}
	if len(messages) > maxRuntimeExecutionMessages {
		return nil, ErrInvalidInput
	}
	for index, message := range messages {
		terminal := message.MessageType == "Result" || message.MessageType == "Error"
		// Recovery appends frames from a new Runtime command; request/command IDs
		// may change across attempts while execution and generation remain fenced.
		if runtimeprotocol.ValidateMessage(message) != nil || message.ExecutionID != executionID || message.Generation != generation || terminal && index != len(messages)-1 || !requireResult && message.MessageType == "Result" {
			return nil, ErrInvalidInput
		}
	}
	encoded, err := json.Marshal(messages)
	if err != nil || len(encoded) > runtimeprotocol.MaxMessageBytes || requireResult && messages[len(messages)-1].MessageType != "Result" {
		return nil, ErrInvalidInput
	}
	return encoded, nil
}

func ValidRuntimeErrorCode(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
