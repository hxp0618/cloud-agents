package server

import "strings"

func capabilityAdminPath(path string) (tenantID, projectID, resourceID, action string, ok bool) {
	const prefix = AdminManagedAgentRuntimeRoutePrefix
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if !strings.HasPrefix(path, prefix) || len(parts) < 4 || parts[1] != "projects" || parts[3] == "" {
		return "", "", "", "", false
	}
	tenantID, projectID, collection := parts[0], parts[2], parts[3]
	if collection != "mcp-servers" && collection != "skill-bundles" {
		return "", "", "", "", false
	}
	if len(parts) == 4 {
		return tenantID, projectID, "", map[string]string{"mcp-servers": "adminCreateMcpServer", "skill-bundles": "adminCreateSkillBundle"}[collection], true
	}
	if len(parts) == 5 && parts[4] != "" {
		if strings.HasSuffix(parts[4], ":revoke") {
			resourceID = strings.TrimSuffix(parts[4], ":revoke")
			return tenantID, projectID, resourceID, map[string]string{"mcp-servers": "adminRevokeMcpServer", "skill-bundles": "adminRevokeSkillBundle"}[collection], resourceID != ""
		}
		if strings.Contains(parts[4], ":") {
			return "", "", "", "", false
		}
		return tenantID, projectID, parts[4], map[string]string{"mcp-servers": "adminGetMcpServer", "skill-bundles": "adminGetSkillBundle"}[collection], true
	}
	return "", "", "", "", false
}

func HandlesCapabilityAdminPath(path string) bool {
	_, _, _, _, ok := capabilityAdminPath(path)
	return ok
}
