package server

import (
	"net/http"
	"strings"
)

func managedHostEnvironmentLeasePath(path string) (tenantID, projectID, leaseID, action string, ok bool) {
	return environmentLeasePathWithPrefix(path, ManagedHostEnvironmentLeaseRoutePrefix)
}

func adminEnvironmentLeasePath(path string) (tenantID, projectID, leaseID, action string, ok bool) {
	if strings.HasPrefix(path, AdminEnvironmentLeaseRoutePrefix) {
		parts := strings.Split(strings.TrimPrefix(path, AdminEnvironmentLeaseRoutePrefix), "/")
		if len(parts) == 6 && parts[1] == "projects" && parts[3] == "workers" && parts[5] == "health" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
			return parts[0], parts[2], parts[4], "worker-health", true
		}
		if len(parts) == 4 && parts[1] == "projects" && parts[3] == "workers" && parts[0] != "" && parts[2] != "" {
			return parts[0], parts[2], "", "worker-collection", true
		}
		if len(parts) == 5 && parts[1] == "projects" && parts[3] == "environment-leases" && parts[0] != "" && parts[2] != "" {
			for _, suffix := range []string{":upgrade-preview", ":rollback-preview", ":rollback"} {
				if strings.HasSuffix(parts[4], suffix) {
					leaseID = strings.TrimSuffix(parts[4], suffix)
					if leaseID != "" {
						return parts[0], parts[2], leaseID, strings.TrimPrefix(suffix, ":"), true
					}
				}
			}
		}
	}
	return environmentLeasePathWithPrefix(path, AdminEnvironmentLeaseRoutePrefix)
}

func environmentLeasePathWithPrefix(path, prefix string) (tenantID, projectID, leaseID, action string, ok bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "environment-leases" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "collection", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "environment-leases" && parts[0] != "" && parts[2] != "" && parts[4] != "" && !strings.Contains(parts[4], ":") {
		return parts[0], parts[2], parts[4], "get", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "environment-leases" && strings.HasSuffix(parts[4], ":terminate") {
		leaseID = strings.TrimSuffix(parts[4], ":terminate")
		if leaseID != "" {
			return parts[0], parts[2], leaseID, "terminate", true
		}
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "environment-leases" && strings.HasSuffix(parts[4], ":upgrade") {
		leaseID = strings.TrimSuffix(parts[4], ":upgrade")
		if leaseID != "" {
			return parts[0], parts[2], leaseID, "upgrade", true
		}
	}
	return "", "", "", "", false
}

func encodeManagedHostEnvironmentLeasePageToken(tenantID, projectID, leaseID string) (string, bool) {
	return encodeProjectResourcePageToken("environment-lease/v1", tenantID, projectID, leaseID)
}

func decodeManagedHostEnvironmentLeasePageToken(tenantID, projectID, token string) (string, bool) {
	return decodeProjectResourcePageToken("environment-lease/v1", tenantID, projectID, token)
}

func encodeAdminWorkerPageToken(tenantID, projectID, workerID string) (string, bool) {
	return encodeProjectResourcePageToken("worker/v1", tenantID, projectID, workerID)
}

func decodeAdminWorkerPageToken(tenantID, projectID, token string) (string, bool) {
	return decodeProjectResourcePageToken("worker/v1", tenantID, projectID, token)
}

func HandlesManagedHostEnvironmentLeasePath(path string) bool {
	_, _, _, _, ok := managedHostEnvironmentLeasePath(path)
	return ok
}

func HandlesAdminEnvironmentLeasePath(path string) bool {
	_, _, _, _, ok := adminEnvironmentLeasePath(path)
	return ok
}

func environmentLeasePermission(action, method string, admin bool) (string, bool) {
	switch {
	case admin && (action == "worker-collection" || action == "worker-health") && method == http.MethodGet:
		return "workers.list", true
	case action == "collection" && method == http.MethodGet:
		return "leases.list", true
	case action == "get" && method == http.MethodGet:
		return "leases.get", true
	case admin && (action == "upgrade-preview" || action == "rollback-preview") && method == http.MethodGet:
		return "leases.get", true
	case admin && (action == "upgrade" || action == "rollback") && method == http.MethodPost:
		return "leases.act", true
	case !admin && (action == "collection" || action == "terminate" || action == "upgrade") && method == http.MethodPost:
		return "leases.act", true
	default:
		return "", false
	}
}
