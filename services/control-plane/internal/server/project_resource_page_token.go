package server

import (
	"encoding/base64"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

// Project-scoped tokens bind the cursor to tenant and project, so cross-scope replay fails closed.
func encodeProjectResourcePageToken(kind, tenantID, projectID, resourceID string) (string, bool) {
	return encodePageToken(kind,
		pageTokenPart{value: tenantID, path: "/tenantId"},
		pageTokenPart{value: projectID, path: "/projectId"},
		pageTokenPart{value: resourceID, path: "/resourceId"},
	)
}

func decodeProjectResourcePageToken(kind, tenantID, projectID, token string) (string, bool) {
	parts, ok := decodePageToken(kind, token,
		pageTokenPart{value: tenantID, path: "/tenantId"},
		pageTokenPart{value: projectID, path: "/projectId"},
		pageTokenPart{path: "/resourceId"},
	)
	if !ok {
		return "", false
	}
	return parts[2], true
}

type pageTokenPart struct {
	value string
	path  string
}

func encodePageToken(kind string, parts ...pageTokenPart) (string, bool) {
	values := make([]string, len(parts)+1)
	values[0] = kind
	for index, part := range parts {
		if commonv1alpha1.ValidateIdentifier(part.value, part.path) != nil {
			return "", false
		}
		values[index+1] = part.value
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(values, "\x00")))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodePageToken(kind, token string, expected ...pageTokenPart) ([]string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return nil, false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != len(expected)+1 || parts[0] != kind {
		return nil, false
	}
	for index, part := range expected {
		value := parts[index+1]
		if (part.value != "" && value != part.value) || commonv1alpha1.ValidateIdentifier(value, part.path) != nil {
			return nil, false
		}
	}
	return parts[1:], true
}
