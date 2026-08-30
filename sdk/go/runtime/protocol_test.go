package runtime

import (
	"errors"
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
	message := Message{RequestID: "request-1", Protocol: Protocol{Major: ProtocolMajor, Minor: ProtocolMinor}, ExecutionID: "execution-1", Generation: 1, CommandID: "command-1", OccurredAt: "2026-08-30T00:00:00Z", MessageType: "Result"}
	if err := ValidateMessage(message); err != nil {
		t.Fatal(err)
	}
	message.MessageType = "Unknown"
	if err := ValidateMessage(message); !errors.Is(err, ErrProtocolViolation) {
		t.Fatalf("ValidateMessage error = %v", err)
	}
}
