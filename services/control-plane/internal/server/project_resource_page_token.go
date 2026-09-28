package server

import (
	"encoding/base64"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

// Project-scoped tokens bind the cursor to tenant and project, so cross-scope replay fails closed.
func encodeProjectResourcePageToken(kind, tenantID, projectID, resourceID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(resourceID, "/resourceId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(kind + "\x00" + tenantID + "\x00" + projectID + "\x00" + resourceID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeProjectResourcePageToken(kind, tenantID, projectID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != kind || parts[1] != tenantID || parts[2] != projectID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/projectId") != nil || commonv1alpha1.ValidateIdentifier(parts[3], "/resourceId") != nil {
		return "", false
	}
	return parts[3], true
}
