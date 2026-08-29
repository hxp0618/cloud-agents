package managedagent

import (
	"strings"
	"testing"
)

func TestEventCursorRoundTripBindsProfileAndScope(t *testing.T) {
	profile := ManagedAgentLifecycleEventProfile()
	want := EventCursor{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, Sequence: 7,
		EventID: "managed-agent-event-7", ProfileID: profile.ID, ProfileDigest: profile.Digest,
	}
	token, err := EncodeEventCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEventCursor(token)
	if err != nil || got != want {
		t.Fatalf("cursor = %#v, err = %v", got, err)
	}
	if _, err := DecodeEventCursor(token + "!"); err == nil {
		t.Fatal("accepted malformed cursor")
	}
	if _, err := DecodeEventCursor(strings.TrimSuffix(token, "=")); err != nil {
		t.Fatalf("raw base64 cursor should remain decodable: %v", err)
	}
}

func TestEventCursorRejectsCrossScopeAndProfile(t *testing.T) {
	profile := ManagedAgentLifecycleEventProfile()
	base := EventCursor{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, Sequence: 1,
		EventID: "managed-agent-event-1", ProfileID: profile.ID, ProfileDigest: profile.Digest,
	}
	if _, err := EncodeEventCursor(EventCursor{Scope: Scope{TenantID: "tenant-alpha"}, Sequence: 1, EventID: base.EventID, ProfileID: profile.ID, ProfileDigest: profile.Digest}); err == nil {
		t.Fatal("accepted incomplete cursor scope")
	}
	if _, err := EncodeEventCursor(EventCursor{Scope: base.Scope, Sequence: 1, EventID: base.EventID, ProfileID: "other", ProfileDigest: profile.Digest}); err == nil {
		t.Fatal("accepted mismatched cursor profile")
	}
}
