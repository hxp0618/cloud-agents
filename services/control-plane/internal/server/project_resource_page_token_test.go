package server

import "testing"

func TestPageTokenRoundTripBindsExpectedParts(t *testing.T) {
	token, ok := encodePageToken("project/v1",
		pageTokenPart{value: "tenant-alpha", path: "/tenantId"},
		pageTokenPart{value: "organization-alpha", path: "/organizationId"},
		pageTokenPart{value: "project-alpha", path: "/projectId"},
	)
	if !ok {
		t.Fatal("valid page token was not encoded")
	}

	parts, ok := decodePageToken("project/v1", token,
		pageTokenPart{value: "tenant-alpha", path: "/tenantId"},
		pageTokenPart{value: "organization-alpha", path: "/organizationId"},
		pageTokenPart{path: "/projectId"},
	)
	if !ok || len(parts) != 3 || parts[2] != "project-alpha" {
		t.Fatalf("decoded page token = %#v / %t", parts, ok)
	}

	if _, ok := decodePageToken("project/v1", token,
		pageTokenPart{value: "tenant-alpha", path: "/tenantId"},
		pageTokenPart{value: "organization-other", path: "/organizationId"},
		pageTokenPart{path: "/projectId"},
	); ok {
		t.Fatal("cross-scope page token was accepted")
	}
}

func TestPageTokenRejectsInvalidIdentifier(t *testing.T) {
	if _, ok := encodePageToken("project/v1", pageTokenPart{value: "bad/id", path: "/projectId"}); ok {
		t.Fatal("invalid identifier was encoded")
	}
}
