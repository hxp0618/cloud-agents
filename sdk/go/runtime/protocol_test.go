package runtime

import (
	"errors"
	"strings"
	"testing"
)

func TestRuntimeProtocolValidation(t *testing.T) {
	command := Command{RequestID: "request-1", Protocol: Protocol{Major: ProtocolMajor, Minor: ProtocolMinor}, ExecutionID: "execution-1", Generation: 1, CommandType: "SendTurn", CommandID: "command-1", OccurredAt: "2026-08-30T00:00:00Z", Payload: map[string]any{}}
	if err := ValidateCommand(command); err != nil {
		t.Fatal(err)
	}
	command.CommandType = "Unknown"
	if err := ValidateCommand(command); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("ValidateCommand error = %v", err)
	}
	message := Message{RequestID: "request-1", Protocol: Protocol{Major: ProtocolMajor, Minor: ProtocolMinor}, ExecutionID: "execution-1", Generation: 1, CommandID: "command-1", OccurredAt: "2026-08-30T00:00:00Z", MessageType: "Result", Payload: map[string]any{}}
	if err := ValidateMessage(message); err != nil {
		t.Fatal(err)
	}
	errorMessage := Message{RequestID: "request-1", Protocol: Protocol{Major: ProtocolMajor, Minor: ProtocolMinor}, ExecutionID: "execution-1", Generation: 1, CommandID: "command-1", OccurredAt: "2026-08-30T00:00:00Z", MessageType: "Error", Error: &Error{Code: "internal_error", Message: "failed"}}
	if err := ValidateMessage(errorMessage); err != nil {
		t.Fatal(err)
	}
	invalid := []Message{
		{RequestID: strings.Repeat("x", 201), Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: message.OccurredAt, MessageType: message.MessageType, Payload: message.Payload},
		{RequestID: message.RequestID, Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: "not-a-time", MessageType: message.MessageType, Payload: message.Payload},
		{RequestID: message.RequestID, Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: message.OccurredAt, MessageType: "Result"},
		{RequestID: message.RequestID, Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: message.OccurredAt, MessageType: "Result", Payload: message.Payload, Error: errorMessage.Error},
		{RequestID: errorMessage.RequestID, Protocol: errorMessage.Protocol, ExecutionID: errorMessage.ExecutionID, Generation: errorMessage.Generation, CommandID: errorMessage.CommandID, OccurredAt: errorMessage.OccurredAt, MessageType: "Error", Error: &Error{Code: "provider_failed", Message: "failed"}},
		{RequestID: errorMessage.RequestID, Protocol: errorMessage.Protocol, ExecutionID: errorMessage.ExecutionID, Generation: errorMessage.Generation, CommandID: errorMessage.CommandID, OccurredAt: errorMessage.OccurredAt, MessageType: "Error", Error: &Error{Code: "internal_error"}},
		{RequestID: message.RequestID, Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: message.OccurredAt, MessageType: "Unknown", Payload: message.Payload},
	}
	for index, candidate := range invalid {
		if err := ValidateMessage(candidate); !errors.Is(err, ErrProtocolViolation) {
			t.Fatalf("ValidateMessage invalid[%d] error = %v", index, err)
		}
	}
}
