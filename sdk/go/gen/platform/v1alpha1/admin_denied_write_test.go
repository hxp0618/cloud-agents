package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdminDeniedWriteMetadataContract(t *testing.T) {
	event := map[string]any{"apiVersion": APIVersion, "kind": "AdminDeniedWriteEvent", "eventId": "denied-alpha",
		"tenantId": "tenant-alpha", "projectId": "project-alpha", "actor": "sha256:" + strings.Repeat("a", 64),
		"action": "adminProbeDeploymentTarget", "resourceId": "target-alpha", "result": "denied", "stableErrorCode": "AUTHORIZATION_DENIED",
		"requestId": "request-alpha", "occurredAt": "2026-09-05T12:00:00Z"}
	raw, _ := json.Marshal(event)
	if _, err := DecodeAdminDeniedWriteEventJSON(raw); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"actor": "raw-subject", "action": "arbitrary", "result": "succeeded", "resourceId": "", "profileVersion": nil, "operationId": "invented", "occurredAt": "today"} {
		changed := make(map[string]any)
		for k, v := range event {
			changed[k] = v
		}
		changed[key] = value
		raw, _ := json.Marshal(changed)
		if _, err := DecodeAdminDeniedWriteEventJSON(raw); err == nil {
			t.Fatalf("invalid %s accepted", key)
		}
	}
	page := map[string]any{"apiVersion": APIVersion, "kind": "AdminDeniedWriteEventPage", "events": []any{event}}
	raw, _ = json.Marshal(page)
	if _, err := DecodeAdminDeniedWriteEventPageJSON(raw); err != nil {
		t.Fatal(err)
	}
	event["credentialRef"] = "must-not-accept"
	raw, _ = json.Marshal(page)
	if _, err := DecodeAdminDeniedWriteEventPageJSON(raw); err == nil {
		t.Fatal("nested unknown fields discarded before validation")
	}
}
