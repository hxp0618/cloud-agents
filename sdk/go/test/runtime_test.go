package sdk_test

import (
	"errors"
	"strings"
	"testing"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

func TestRuntimeProtocolValidation(t *testing.T) {
	command := runtimeprotocol.Command{RequestID: "request-1", Protocol: runtimeprotocol.Protocol{Major: runtimeprotocol.ProtocolMajor, Minor: runtimeprotocol.ProtocolMinor}, ExecutionID: "execution-1", Generation: 1, CommandType: "SendTurn", CommandID: "command-1", OccurredAt: "2026-08-30T00:00:00Z", Payload: map[string]any{}}
	if err := runtimeprotocol.ValidateCommand(command); err != nil {
		t.Fatal(err)
	}
	command.CommandType = "Unknown"
	if err := runtimeprotocol.ValidateCommand(command); !errors.Is(err, runtimeprotocol.ErrInvalidCommand) {
		t.Fatalf("ValidateCommand error = %v", err)
	}
	message := runtimeprotocol.Message{RequestID: "request-1", Protocol: runtimeprotocol.Protocol{Major: runtimeprotocol.ProtocolMajor, Minor: runtimeprotocol.ProtocolMinor}, ExecutionID: "execution-1", Generation: 1, CommandID: "command-1", OccurredAt: "2026-08-30T00:00:00Z", MessageType: "Result", Payload: map[string]any{}}
	if err := runtimeprotocol.ValidateMessage(message); err != nil {
		t.Fatal(err)
	}
	errorMessage := runtimeprotocol.Message{RequestID: "request-1", Protocol: runtimeprotocol.Protocol{Major: runtimeprotocol.ProtocolMajor, Minor: runtimeprotocol.ProtocolMinor}, ExecutionID: "execution-1", Generation: 1, CommandID: "command-1", OccurredAt: "2026-08-30T00:00:00Z", MessageType: "Error", Error: &runtimeprotocol.Error{Code: "internal_error", Message: "failed"}}
	if err := runtimeprotocol.ValidateMessage(errorMessage); err != nil {
		t.Fatal(err)
	}
	invalid := []runtimeprotocol.Message{
		{RequestID: strings.Repeat("x", 201), Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: message.OccurredAt, MessageType: message.MessageType, Payload: message.Payload},
		{RequestID: message.RequestID, Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: "not-a-time", MessageType: message.MessageType, Payload: message.Payload},
		{RequestID: message.RequestID, Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: message.OccurredAt, MessageType: "Result"},
		{RequestID: message.RequestID, Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: message.OccurredAt, MessageType: "Result", Payload: message.Payload, Error: errorMessage.Error},
		{RequestID: errorMessage.RequestID, Protocol: errorMessage.Protocol, ExecutionID: errorMessage.ExecutionID, Generation: errorMessage.Generation, CommandID: errorMessage.CommandID, OccurredAt: errorMessage.OccurredAt, MessageType: "Error", Error: &runtimeprotocol.Error{Code: "provider_failed", Message: "failed"}},
		{RequestID: errorMessage.RequestID, Protocol: errorMessage.Protocol, ExecutionID: errorMessage.ExecutionID, Generation: errorMessage.Generation, CommandID: errorMessage.CommandID, OccurredAt: errorMessage.OccurredAt, MessageType: "Error", Error: &runtimeprotocol.Error{Code: "internal_error"}},
		{RequestID: message.RequestID, Protocol: message.Protocol, ExecutionID: message.ExecutionID, Generation: message.Generation, CommandID: message.CommandID, OccurredAt: message.OccurredAt, MessageType: "Unknown", Payload: message.Payload},
	}
	for index, candidate := range invalid {
		if err := runtimeprotocol.ValidateMessage(candidate); !errors.Is(err, runtimeprotocol.ErrProtocolViolation) {
			t.Fatalf("ValidateMessage invalid[%d] error = %v", index, err)
		}
	}
}
