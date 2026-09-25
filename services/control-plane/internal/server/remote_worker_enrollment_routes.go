package server

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
)

func remoteWorkerWorkspaceSnapshotPath(path string) (tenantID, projectID, enrollmentID, snapshotID, action string, ok bool) {
	const prefix = "/v1/remote-workers/tenants/"
	if !strings.HasPrefix(path, prefix) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 6 || parts[1] != "projects" || parts[3] != "remote-worker-enrollments" || parts[4] == "" || parts[5] == "" {
		return
	}
	if !strings.HasSuffix(parts[4], ":workspaceSnapshot") {
		return
	}
	return parts[0], parts[2], strings.TrimSuffix(parts[4], ":workspaceSnapshot"), parts[5], "workspace-snapshot", true
}

func remoteWorkerEnrollmentPath(path string) (tenantID, projectID, enrollmentID, action string, ok bool) {
	prefix := adminEnvironmentProfileRoutePrefix
	isBootstrap := false
	isRemoteWorker := false
	if strings.HasPrefix(path, "/v1/remote-worker-bootstrap/tenants/") {
		prefix = "/v1/remote-worker-bootstrap/tenants/"
		isBootstrap = true
	} else if strings.HasPrefix(path, "/v1/remote-workers/tenants/") {
		prefix = "/v1/remote-workers/tenants/"
		isRemoteWorker = true
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "remote-worker-enrollments" && parts[0] != "" && parts[2] != "" {
		if isBootstrap || isRemoteWorker {
			return "", "", "", "", false
		}
		return parts[0], parts[2], "", "collection", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "remote-worker-enrollments" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		if strings.HasSuffix(parts[4], ":scheduling-preview") && !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":scheduling-preview"), "scheduling-preview", true
		}
		if strings.HasSuffix(parts[4], ":scheduling") && !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":scheduling"), "scheduling", true
		}
		if strings.HasSuffix(parts[4], ":revoke") && !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":revoke"), "revoke", true
		}
		if strings.HasSuffix(parts[4], ":claimSecret") && isBootstrap {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":claimSecret"), "claim-secret", true
		}
		if strings.HasSuffix(parts[4], ":issueCertificate") && isBootstrap {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":issueCertificate"), "issue-certificate", true
		}
		if strings.HasSuffix(parts[4], ":rotateCertificate") && isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":rotateCertificate"), "rotate-certificate", true
		}
		if strings.HasSuffix(parts[4], ":heartbeat") && isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":heartbeat"), "heartbeat", true
		}
		if !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], parts[4], "get", true
		}
	}
	if len(parts) == 6 && !isBootstrap && !isRemoteWorker && parts[1] == "projects" && parts[3] == "remote-worker-enrollments" && parts[4] != "" {
		if parts[5] == "audit-events" || parts[5] == "operations" {
			return parts[0], parts[2], parts[4], parts[5], true
		}
	}
	return "", "", "", "", false
}

func remoteWorkerEnrollmentAuthorization(value string) (string, bool) {
	const prefix = "RemoteWorkerEnrollment "
	if !strings.HasPrefix(value, prefix) || strings.ContainsAny(value, "\r\n") {
		return "", false
	}
	secret := strings.TrimPrefix(value, prefix)
	_, err := internalremoteworker.SecretDigest(secret)
	return secret, err == nil
}

func remoteWorkerEnrollmentPermission(action, method string) (string, string, bool) {
	switch {
	case action == "collection" && method == http.MethodGet:
		return "projects.get", "remote-worker-enrollments.list", true
	case action == "collection" && method == http.MethodPost:
		return "projects.act", "remote-worker-enrollments.create", true
	case action == "get" && method == http.MethodGet:
		return "projects.get", "remote-worker-enrollments.get", true
	case action == "revoke" && method == http.MethodPost:
		return "projects.act", "remote-worker-enrollments.act", true
	case action == "scheduling-preview" && method == http.MethodGet:
		return "projects.get", "remote-worker-enrollments.get", true
	case action == "scheduling" && method == http.MethodPost:
		return "projects.act", "remote-worker-enrollments.act", true
	case action == "claim-secret" && method == http.MethodPost:
		return "projects.act", "remote-worker-bootstrap.act", true
	case action == "audit-events" && method == http.MethodGet:
		return "projects.get", "audit.list", true
	case action == "operations" && method == http.MethodGet:
		return "projects.get", "operations.list", true
	default:
		return "", "", false
	}
}

func HandlesRemoteWorkerEnrollmentPath(path string) bool {
	_, _, _, _, ok := remoteWorkerEnrollmentPath(path)
	return ok
}

func encodeRemoteWorkerOperationPageToken(tenantID, projectID, enrollmentID string, requestedAt time.Time, operationID string) (string, bool) {
	if requestedAt.IsZero() || commonv1alpha1.ValidateIdentifier(operationID, "/operationId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("remote-worker-operation/v1\x00" + tenantID + "\x00" + projectID + "\x00" + enrollmentID + "\x00" + requestedAt.UTC().Format(time.RFC3339Nano) + "\x00" + operationID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeRemoteWorkerOperationPageToken(tenantID, projectID, enrollmentID, token string) (*time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 || parts[0] != "remote-worker-operation/v1" || parts[1] != tenantID || parts[2] != projectID || parts[3] != enrollmentID || commonv1alpha1.ValidateIdentifier(parts[5], "/operationId") != nil {
		return nil, "", false
	}
	requestedAt, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil {
		return nil, "", false
	}
	return &requestedAt, parts[5], true
}

func encodeRemoteWorkerEnrollmentAuditPageToken(tenantID, projectID, enrollmentID string, occurredAt time.Time, eventID string) (string, bool) {
	if occurredAt.IsZero() || commonv1alpha1.ValidateIdentifier(eventID, "/eventId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("remote-worker-enrollment-audit/v1\x00" + tenantID + "\x00" + projectID + "\x00" + enrollmentID + "\x00" + occurredAt.UTC().Format(time.RFC3339Nano) + "\x00" + eventID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeRemoteWorkerEnrollmentAuditPageToken(tenantID, projectID, enrollmentID, token string) (*time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 || parts[0] != "remote-worker-enrollment-audit/v1" || parts[1] != tenantID || parts[2] != projectID || parts[3] != enrollmentID || commonv1alpha1.ValidateIdentifier(parts[5], "/eventId") != nil {
		return nil, "", false
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil {
		return nil, "", false
	}
	return &occurredAt, parts[5], true
}
