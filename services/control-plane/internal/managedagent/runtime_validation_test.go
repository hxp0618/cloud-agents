package managedagent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

type countedJSONValue struct{ calls *int }

func (value countedJSONValue) MarshalJSON() ([]byte, error) {
	*value.calls++
	return []byte(`"value"`), nil
}

func TestEncodeRuntimeMessagesReusesCanonicalEncoding(t *testing.T) {
	calls := 0
	messages := []runtimeprotocol.Message{{
		RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7,
		CommandID: "command", OccurredAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		MessageType: "Progress", Payload: map[string]any{"text": countedJSONValue{calls: &calls}},
	}}
	encoded, digest, err := EncodeRuntimeMessages(messages, "execution", 7)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(encoded)
	if calls != 1 || digest != "sha256:"+hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("marshal calls = %d, digest = %q", calls, digest)
	}
}

func TestEncodeRuntimeMessagesPreservesEmptySliceEncoding(t *testing.T) {
	for _, messages := range [][]runtimeprotocol.Message{nil, {}} {
		encoded, _, err := EncodeRuntimeMessages(messages, "execution", 7)
		want, marshalErr := json.Marshal(messages)
		if err != nil || marshalErr != nil || !bytes.Equal(encoded, want) {
			t.Fatalf("encoded = %q, want %q, error = %v/%v", encoded, want, err, marshalErr)
		}
	}
}
