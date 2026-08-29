package managedagent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

const eventCursorVersion = "managed-agent-event-cursor/v1"

// ValidateForAPI exposes the same scope validation used by the lifecycle
// kernel to sibling transport/store packages.
func (scope Scope) ValidateForAPI() error { return scope.validate() }

type eventCursorWire struct {
	Version       string `json:"version"`
	Scope         Scope  `json:"scope"`
	Sequence      uint64 `json:"sequence"`
	EventID       string `json:"eventId"`
	ProfileID     string `json:"profileId"`
	ProfileDigest string `json:"profileDigest"`
}

// EncodeEventCursor keeps the cursor opaque on the HTTP surface while its
// scope/profile binding remains available for fail-closed validation.
func EncodeEventCursor(cursor EventCursor) (string, error) {
	profile := ManagedAgentLifecycleEventProfile()
	if !profile.Valid() || cursor.Sequence == 0 || cursor.EventID == "" || cursor.Scope.validate() != nil || cursor.ProfileID != profile.ID || cursor.ProfileDigest != profile.Digest {
		return "", fmt.Errorf("%w: event cursor", ErrInvalidInput)
	}
	value, err := json.Marshal(eventCursorWire{Version: eventCursorVersion, Scope: cursor.Scope, Sequence: cursor.Sequence, EventID: cursor.EventID, ProfileID: cursor.ProfileID, ProfileDigest: cursor.ProfileDigest})
	if err != nil {
		return "", fmt.Errorf("%w: event cursor", ErrInvalidInput)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// DecodeEventCursor rejects malformed, cross-profile, and cross-scope tokens.
func DecodeEventCursor(token string) (EventCursor, error) {
	if token == "" {
		return EventCursor{}, nil
	}
	if len(token) > 2048 {
		return EventCursor{}, fmt.Errorf("%w: event cursor", ErrInvalidInput)
	}
	value, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return EventCursor{}, fmt.Errorf("%w: event cursor", ErrInvalidInput)
	}
	var wire eventCursorWire
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var trailing any
	if err := decoder.Decode(&wire); err != nil || decoder.Decode(&trailing) != io.EOF || wire.Version != eventCursorVersion || wire.Scope.validate() != nil || wire.Sequence == 0 || wire.EventID == "" {
		return EventCursor{}, fmt.Errorf("%w: event cursor", ErrInvalidInput)
	}
	profile := ManagedAgentLifecycleEventProfile()
	if wire.ProfileID != profile.ID || wire.ProfileDigest != profile.Digest {
		return EventCursor{}, fmt.Errorf("%w: event cursor profile", ErrInvalidInput)
	}
	return EventCursor{Scope: wire.Scope, Sequence: wire.Sequence, EventID: wire.EventID, ProfileID: wire.ProfileID, ProfileDigest: wire.ProfileDigest}, nil
}
