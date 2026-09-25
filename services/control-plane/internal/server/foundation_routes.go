package server

import (
	"net/http"
	"strconv"
	"strings"

	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
)

func foundationPath(path string) (admin bool, tenantID, projectID, resourceID, secondaryID string, version int64, action string, ok bool) {
	prefix := "/v1/tenants/"
	if strings.HasPrefix(path, "/v1/admin/tenants/") {
		admin, prefix = true, "/v1/admin/tenants/"
	} else if !strings.HasPrefix(path, prefix) {
		return false, "", "", "", "", 0, "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 4 && parts[0] != "" && parts[1] == "projects" && parts[2] != "" {
		switch {
		case parts[3] == "runtime-profiles" && admin:
			return true, parts[0], parts[2], "", "", 0, "admin-collection", true
		case parts[3] == "runtime-profiles":
			return false, parts[0], parts[2], "", "", 0, "public-list", true
		case parts[3] == "sandbox-sessions" && !admin:
			return false, parts[0], parts[2], "", "", 0, "create-sandbox", true
		case parts[3] == "sandbox-sessions" && admin:
			return true, parts[0], parts[2], "", "", 0, "admin-sandbox-collection", true
		case parts[3] == "workspace-snapshots" && admin:
			return true, parts[0], parts[2], "", "", 0, "admin-snapshot-collection", true
		}
	}
	if len(parts) == 6 && parts[1] == "projects" && parts[3] == "sandbox-sessions" && parts[4] != "" && parts[5] == "access-grants" {
		if admin {
			return true, parts[0], parts[2], parts[4], "", 0, "admin-access-grant-collection", true
		}
		return false, parts[0], parts[2], parts[4], "", 0, "issue-access-grant", true
	}
	if admin && len(parts) == 7 && parts[1] == "projects" && parts[3] == "sandbox-sessions" &&
		parts[4] != "" && parts[5] == "access-grants" {
		grantID, found := strings.CutSuffix(parts[6], ":revoke")
		return true, parts[0], parts[2], parts[4], grantID, 0, "revoke-access-grant", found && grantID != ""
	}
	if admin && len(parts) == 5 && parts[1] == "projects" && parts[3] == "sandbox-sessions" && parts[4] != "" {
		sandboxID, sandboxAction := parts[4], "get-admin-sandbox"
		if value, found := strings.CutSuffix(sandboxID, ":stop"); found {
			sandboxID, sandboxAction = value, internalcoordination.FoundationSandboxStop
		} else if value, found := strings.CutSuffix(sandboxID, ":rebuild"); found {
			sandboxID, sandboxAction = value, internalcoordination.FoundationSandboxRebuild
		} else if value, found := strings.CutSuffix(sandboxID, ":correct-usage"); found {
			sandboxID, sandboxAction = value, "correct-sandbox-usage"
		}
		return true, parts[0], parts[2], sandboxID, "", 0, sandboxAction, sandboxID != ""
	}
	if admin && len(parts) == 5 && parts[1] == "projects" && parts[3] == "workspace-snapshots" && parts[4] != "" {
		snapshotID, snapshotAction := parts[4], "get-workspace-snapshot"
		if value, found := strings.CutSuffix(snapshotID, ":restore"); found {
			snapshotID, snapshotAction = value, "restore-workspace-snapshot"
		} else if value, found := strings.CutSuffix(snapshotID, ":cleanup"); found {
			snapshotID, snapshotAction = value, "cleanup-workspace-snapshot"
		}
		return true, parts[0], parts[2], snapshotID, "", 0, snapshotAction, snapshotID != ""
	}
	if !admin && len(parts) == 5 && parts[1] == "projects" && parts[3] == "sandbox-sessions" {
		if sandboxID, found := strings.CutSuffix(parts[4], ":exec"); found && sandboxID != "" {
			return false, parts[0], parts[2], sandboxID, "", 0, "exec-sandbox", true
		}
	}
	if !admin || len(parts) != 7 || parts[1] != "projects" || parts[3] != "runtime-profiles" || parts[4] == "" || parts[5] != "versions" {
		return false, "", "", "", "", 0, "", false
	}
	versionPart, detailAction := parts[6], "get-profile"
	if value, found := strings.CutSuffix(versionPart, ":publish"); found {
		versionPart, detailAction = value, internalcoordination.RuntimeProfilePublish
	} else if value, found := strings.CutSuffix(versionPart, ":disable"); found {
		versionPart, detailAction = value, internalcoordination.RuntimeProfileDisable
	}
	parsed, err := strconv.ParseInt(versionPart, 10, 64)
	if err != nil || parsed < 1 || parsed > 2147483647 {
		return false, "", "", "", "", 0, "", false
	}
	return true, parts[0], parts[2], parts[4], "", parsed, detailAction, true
}

func foundationPermission(admin bool, action, method string) (projectPermission, permission string, ok bool) {
	switch {
	case admin && action == "admin-collection" && method == http.MethodGet:
		return "projects.get", "profiles.list", true
	case admin && action == "admin-collection" && method == http.MethodPost:
		return "projects.act", "profiles.create", true
	case admin && action == "get-profile" && method == http.MethodGet:
		return "projects.get", "profiles.get", true
	case admin && (action == internalcoordination.RuntimeProfilePublish || action == internalcoordination.RuntimeProfileDisable) && method == http.MethodPost:
		return "projects.act", "profiles.act", true
	case !admin && action == "public-list" && method == http.MethodGet:
		return "projects.get", "environment-profiles.list", true
	case !admin && action == "create-sandbox" && method == http.MethodPost:
		return "projects.act", "environments.create", true
	case !admin && action == "exec-sandbox" && method == http.MethodPost:
		return "projects.act", "sandboxes.update", true
	case !admin && action == "issue-access-grant" && method == http.MethodPost:
		return "projects.act", "sandboxes.update", true
	case admin && action == "admin-sandbox-collection" && method == http.MethodGet:
		return "projects.get", "sandboxes.list", true
	case admin && action == "get-admin-sandbox" && method == http.MethodGet:
		return "projects.get", "sandboxes.get", true
	case admin && action == "admin-snapshot-collection" && method == http.MethodGet:
		return "projects.get", "snapshots.list", true
	case admin && action == "admin-snapshot-collection" && method == http.MethodPost:
		return "projects.act", "snapshots.create", true
	case admin && action == "get-workspace-snapshot" && method == http.MethodGet:
		return "projects.get", "snapshots.get", true
	case admin && action == "restore-workspace-snapshot" && method == http.MethodPost:
		return "projects.act", "snapshots.act", true
	case admin && action == "cleanup-workspace-snapshot" && method == http.MethodPost:
		return "projects.act", "snapshots.delete", true
	case admin && action == "admin-access-grant-collection" && method == http.MethodGet:
		return "projects.get", "sandboxes.get", true
	case admin && action == "revoke-access-grant" && method == http.MethodPost:
		return "projects.act", "sandboxes.act", true
	case admin && (action == internalcoordination.FoundationSandboxStop || action == internalcoordination.FoundationSandboxRebuild) && method == http.MethodPost:
		return "projects.act", "sandboxes.act", true
	case admin && action == "correct-sandbox-usage" && method == http.MethodPost:
		return "projects.act", "sandboxes.act", true
	default:
		return "", "", false
	}
}

func encodeFoundationPageToken(kind, tenantID, projectID, value string) (string, bool) {
	if value == "" {
		return "", true
	}
	return encodeProjectResourcePageToken(kind, tenantID, projectID, value)
}

func decodeFoundationPageToken(kind, tenantID, projectID, token string) (string, bool) {
	if token == "" {
		return "", true
	}
	return decodeProjectResourcePageToken(kind, tenantID, projectID, token)
}

func HandlesFoundationPath(path string) bool {
	_, _, _, _, _, _, _, ok := foundationPath(path)
	return ok
}
