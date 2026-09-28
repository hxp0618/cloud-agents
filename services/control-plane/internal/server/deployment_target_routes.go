package server

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

func deploymentTargetPath(path string) (tenantID, projectID, targetID, action string, ok bool) {
	return deploymentTargetPathWithPrefix(path, ProjectRoutePrefix, false)
}

func adminDeploymentTargetPath(path string) (tenantID, projectID, targetID, action string, ok bool) {
	return deploymentTargetPathWithPrefix(path, adminDeploymentTargetRoutePrefix, true)
}

func deploymentTargetPathWithPrefix(path, prefix string, admin bool) (tenantID, projectID, targetID, action string, ok bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if admin && len(parts) == 4 && parts[1] == "projects" && parts[3] == "maintenance-operations" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "maintenance-operations", true
	}
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "deployment-targets" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "collection", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "deployment-targets" && parts[0] != "" && parts[2] != "" {
		if admin && strings.HasSuffix(parts[4], ":scheduling-preview") {
			targetID = strings.TrimSuffix(parts[4], ":scheduling-preview")
			if targetID != "" {
				return parts[0], parts[2], targetID, "scheduling-preview", true
			}
		} else if admin && strings.HasSuffix(parts[4], ":scheduling") {
			targetID = strings.TrimSuffix(parts[4], ":scheduling")
			if targetID != "" {
				return parts[0], parts[2], targetID, "scheduling", true
			}
		} else if admin && strings.HasSuffix(parts[4], ":cleanup-preview") {
			targetID = strings.TrimSuffix(parts[4], ":cleanup-preview")
			if targetID != "" {
				return parts[0], parts[2], targetID, "cleanup-preview", true
			}
		} else if strings.HasSuffix(parts[4], ":probe") {
			targetID = strings.TrimSuffix(parts[4], ":probe")
			if targetID != "" {
				return parts[0], parts[2], targetID, "probe", true
			}
		} else if strings.HasSuffix(parts[4], ":cleanup") {
			targetID = strings.TrimSuffix(parts[4], ":cleanup")
			if targetID != "" {
				return parts[0], parts[2], targetID, "cleanup", true
			}
		} else if parts[4] != "" && !strings.Contains(parts[4], ":") {
			return parts[0], parts[2], parts[4], "get", true
		}
	}
	if admin && len(parts) == 6 && parts[1] == "projects" && parts[3] == "deployment-targets" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		if parts[5] == "operations" || parts[5] == "audit-events" {
			return parts[0], parts[2], parts[4], parts[5], true
		}
	}
	return "", "", "", "", false
}

func deploymentTargetAdminPermission(action, method string) (string, bool) {
	switch {
	case action == "collection" && method == http.MethodGet:
		return "targets.list", true
	case action == "collection" && method == http.MethodPost:
		return "targets.create", true
	case action == "get" && method == http.MethodGet:
		return "targets.get", true
	case action == "cleanup-preview" && method == http.MethodGet:
		return "targets.get", true
	case action == "scheduling-preview" && method == http.MethodGet:
		return "targets.get", true
	case action == "operations" && method == http.MethodGet:
		return "operations.list", true
	case action == "maintenance-operations" && method == http.MethodGet:
		return "operations.list", true
	case action == "audit-events" && method == http.MethodGet:
		return "audit.list", true
	case action == "probe" && method == http.MethodPost:
		return "targets.act", true
	case action == "cleanup" && method == http.MethodPost:
		return "targets.act", true
	case action == "scheduling" && method == http.MethodPost:
		return "targets.act", true
	default:
		return "", false
	}
}

func HandlesDeploymentTargetPath(path string) bool {
	_, _, _, _, ok := deploymentTargetPath(path)
	return ok
}

func HandlesAdminDeploymentTargetPath(path string) bool {
	_, _, _, _, ok := adminDeploymentTargetPath(path)
	return ok
}

func encodeDeploymentTargetPageToken(tenantID, projectID, targetID string) (string, bool) {
	return encodeProjectResourcePageToken("deployment-target/v1", tenantID, projectID, targetID)
}

func decodeDeploymentTargetPageToken(tenantID, projectID, token string) (string, bool) {
	return decodeProjectResourcePageToken("deployment-target/v1", tenantID, projectID, token)
}

func encodeDeploymentTargetActivityPageToken(kind, tenantID, projectID, targetID string, timestamp time.Time, id string) (string, bool) {
	if kind != "operation" && kind != "audit" || timestamp.IsZero() || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(targetID, "/targetId") != nil || commonv1alpha1.ValidateIdentifier(id, "/cursorId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("deployment-target-activity/v1\x00" + kind + "\x00" + tenantID + "\x00" + projectID + "\x00" + targetID + "\x00" + timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + id))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeDeploymentTargetActivityPageToken(kind, tenantID, projectID, targetID, token string) (*time.Time, string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 7 || parts[0] != "deployment-target-activity/v1" || parts[1] != kind || parts[2] != tenantID || parts[3] != projectID || parts[4] != targetID || commonv1alpha1.ValidateIdentifier(parts[6], "/cursorId") != nil {
		return nil, "", false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, parts[5])
	if err != nil {
		return nil, "", false
	}
	return &timestamp, parts[6], true
}

func encodeMaintenanceOperationPageToken(tenantID, projectID, targetID string, timestamp time.Time, operationID string) (string, bool) {
	if timestamp.IsZero() || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(targetID, "/targetId") != nil || commonv1alpha1.ValidateIdentifier(operationID, "/operationId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("maintenance-operation/v1\x00" + tenantID + "\x00" + projectID + "\x00" + targetID + "\x00" + timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + operationID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeMaintenanceOperationPageToken(tenantID, projectID, token string) (*time.Time, string, string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return nil, "", "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 || parts[0] != "maintenance-operation/v1" || parts[1] != tenantID || parts[2] != projectID || commonv1alpha1.ValidateIdentifier(parts[3], "/targetId") != nil || commonv1alpha1.ValidateIdentifier(parts[5], "/operationId") != nil {
		return nil, "", "", false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil {
		return nil, "", "", false
	}
	return &timestamp, parts[3], parts[5], true
}
