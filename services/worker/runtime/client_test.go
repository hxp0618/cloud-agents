package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRuntimeHelperProcess(t *testing.T) {
	if os.Getenv("CLOUD_AGENTS_RUNTIME_HELPER") != "1" {
		if os.Getenv("CLOUD_AGENTS_RUNTIME_EXIT_HELPER") == "1" {
			scanner := bufio.NewScanner(os.Stdin)
			_ = scanner.Scan()
			os.Exit(0)
		}
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command Command
		if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
			_, _ = fmt.Fprintln(os.Stdout, `{"messageType":"Error"}`)
			continue
		}
		if command.CommandID == "oversize-1" {
			_, _ = fmt.Fprintln(os.Stdout, strings.Repeat("x", MaxMessageBytes+1))
			continue
		}
		if command.CommandID == "cancelled-1" {
			time.Sleep(100 * time.Millisecond)
		}
		requestID := command.RequestID
		if command.CommandID == "mismatch-1" {
			requestID = "wrong-request"
		}
		if command.CommandType == "SendTurn" {
			_, _ = fmt.Fprintf(os.Stdout, `{"requestId":%q,"protocolVersion":{"major":2,"minor":3},"executionId":%q,"generation":%d,"commandId":%q,"occurredAt":"2026-08-29T00:00:00Z","messageType":"Event","payload":{"text":"partial"}}`+"\n", requestID, command.ExecutionID, command.Generation, command.CommandID)
		}
		_, _ = fmt.Fprintf(os.Stdout, `{"requestId":%q,"protocolVersion":{"major":2,"minor":3},"executionId":%q,"generation":%d,"commandId":%q,"occurredAt":"2026-08-29T00:00:00Z","messageType":"Result","payload":{"ok":true}}`+"\n", requestID, command.ExecutionID, command.Generation, command.CommandID)
	}
	os.Exit(0)
}

func TestClientFailsPendingCommandWhenRuntimeExitsCleanly(t *testing.T) {
	environment := append(os.Environ(), "CLOUD_AGENTS_RUNTIME_EXIT_HELPER=1")
	client, err := New(context.Background(), Config{
		Command: []string{os.Args[0], "-test.run=TestRuntimeHelperProcess", "--"}, Environment: environment,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient(t, client)
	message, err := client.Execute(context.Background(), testCommand("SendTurn", "exit-before-terminal-1"))
	if !errors.Is(err, ErrProtocolViolation) || message.MessageType != "" {
		t.Fatalf("clean Runtime exit = %#v, %v", message, err)
	}
}

func TestClientExecutesCommandsAndPreservesEvents(t *testing.T) {
	client := newTestClient(t)
	defer closeClient(t, client)
	start := testCommand("StartSession", "start-1")
	if message, err := client.Execute(context.Background(), start); err != nil || message.MessageType != "Result" {
		t.Fatalf("StartSession = %#v, %v", message, err)
	}
	turn := testCommand("SendTurn", "turn-1")
	message, err := client.Execute(context.Background(), turn)
	if err != nil || message.Payload["ok"] != true {
		t.Fatalf("SendTurn = %#v, %v", message, err)
	}
	for _, expected := range []struct {
		commandID   string
		messageType string
	}{{start.CommandID, "Result"}, {turn.CommandID, "Event"}, {turn.CommandID, "Result"}} {
		select {
		case actual := <-client.Events():
			if actual.CommandID != expected.commandID || actual.MessageType != expected.messageType {
				t.Fatalf("Runtime output = %#v, expected %s/%s", actual, expected.commandID, expected.messageType)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for Runtime output %s/%s", expected.commandID, expected.messageType)
		}
	}
}

func TestClientDropsLateTerminalAfterCancellation(t *testing.T) {
	client := newTestClient(t)
	defer closeClient(t, client)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Execute(ctx, testCommand("SendTurn", "cancelled-1"))
		result <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-result; err == nil {
		t.Fatal("cancelled Execute unexpectedly succeeded")
	}
	if _, err := client.Execute(context.Background(), testCommand("StartSession", "start-2")); err != nil {
		t.Fatalf("client after cancellation = %v", err)
	}
}

func TestClientRejectsOversizedRuntimeMessage(t *testing.T) {
	client := newTestClient(t)
	defer closeClient(t, client)
	_, err := client.Execute(context.Background(), testCommand("Describe", "oversize-1"))
	if !errors.Is(err, ErrProtocolViolation) {
		t.Fatalf("oversized message error = %v", err)
	}
}

func TestClientRejectsTerminalWithMismatchedExecutionIdentity(t *testing.T) {
	client := newTestClient(t)
	defer closeClient(t, client)
	_, err := client.Execute(context.Background(), testCommand("Describe", "mismatch-1"))
	if !errors.Is(err, ErrProtocolViolation) {
		t.Fatalf("mismatched response error = %v", err)
	}
}

func TestClientRejectsUnknownCommandTypeBeforeWriting(t *testing.T) {
	client := newTestClient(t)
	defer closeClient(t, client)
	_, err := client.Execute(context.Background(), testCommand("Unknown", "unknown-1"))
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("unknown command error = %v", err)
	}
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	environment := append(os.Environ(), "CLOUD_AGENTS_RUNTIME_HELPER=1")
	client, err := New(context.Background(), Config{
		Command: []string{os.Args[0], "-test.run=TestRuntimeHelperProcess", "--"}, Environment: environment,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func closeClient(t *testing.T, client *Client) {
	t.Helper()
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func testCommand(commandType, commandID string) Command {
	return Command{RequestID: "request-" + commandID, Protocol: Protocol{Major: 2, Minor: 3}, ExecutionID: "execution-1", Generation: 1, CommandType: commandType, CommandID: commandID, OccurredAt: "2026-08-29T00:00:00Z", Payload: map[string]any{}}
}
