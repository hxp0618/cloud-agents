package server

import (
	"context"
	"errors"
	"io"
	"net/http"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type capabilityCatalogStore interface {
	CreateMcpServer(context.Context, string, *authn.VerifiedPrincipal, string, string, platform.McpServerCreateRequest) (platform.McpServer, error)
	RevokeMcpServer(context.Context, string, *authn.VerifiedPrincipal, string, string, string, platform.McpServerRevokeRequest) (platform.McpServer, error)
	GetMcpServer(context.Context, string, *authn.VerifiedPrincipal, string, string) (platform.McpServer, error)
	ListMcpServers(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.McpServerPage, error)
	CreateSkillBundle(context.Context, string, *authn.VerifiedPrincipal, string, string, platform.SkillBundleCreateRequest) (platform.SkillBundle, error)
	RevokeSkillBundle(context.Context, string, *authn.VerifiedPrincipal, string, string, string, platform.SkillBundleRevokeRequest) (platform.SkillBundle, error)
	GetSkillBundle(context.Context, string, *authn.VerifiedPrincipal, string, string) (platform.SkillBundle, error)
	ListSkillBundles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.SkillBundlePage, error)
}

type CapabilityHTTPServer struct {
	verifier AccessTokenVerifier
	store    capabilityCatalogStore
}

func NewCapabilityHTTPServer(verifier AccessTokenVerifier, store capabilityCatalogStore) (*CapabilityHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("capability HTTP server configuration is invalid")
	}
	return &CapabilityHTTPServer{verifier: verifier, store: store}, nil
}

func (server *CapabilityHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, resourceID, action, ok := capabilityAdminPath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method == http.MethodGet {
		if listAction := map[string]string{"adminCreateMcpServer": "adminListMcpServers", "adminCreateSkillBundle": "adminListSkillBundles"}[action]; listAction != "" {
			action = listAction
		}
	}
	permission, projectPermission, ok := capabilityPermission(action, request.Method)
	if !ok {
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok || common.ValidateIdentifier(requestID, "/requestId") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := verifyHTTPRequestAccessToken(request.Context(), server.verifier, bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: projectPermission})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := verifyHTTPRequestAccessToken(request.Context(), server.verifier, bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission}); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	switch action {
	case "adminListMcpServers":
		server.listMcpServers(writer, request, tenantID, projectID, requestID, principal)
	case "adminCreateMcpServer":
		server.createMcpServer(writer, request, tenantID, projectID, requestID, principal)
	case "adminGetMcpServer":
		server.getMcpServer(writer, request, tenantID, projectID, resourceID, requestID, principal)
	case "adminRevokeMcpServer":
		server.revokeMcpServer(writer, request, tenantID, projectID, resourceID, requestID, principal)
	case "adminListSkillBundles":
		server.listSkillBundles(writer, request, tenantID, projectID, requestID, principal)
	case "adminCreateSkillBundle":
		server.createSkillBundle(writer, request, tenantID, projectID, requestID, principal)
	case "adminGetSkillBundle":
		server.getSkillBundle(writer, request, tenantID, projectID, resourceID, requestID, principal)
	case "adminRevokeSkillBundle":
		server.revokeSkillBundle(writer, request, tenantID, projectID, resourceID, requestID, principal)
	}
}

func capabilityPermission(action, method string) (string, string, bool) {
	switch action {
	case "adminListMcpServers":
		if method == http.MethodGet {
			return "mcp-servers.list", "projects.get", true
		}
	case "adminCreateMcpServer":
		if method == http.MethodPost {
			return "mcp-servers.create", "projects.act", true
		}
	case "adminGetMcpServer":
		if method == http.MethodGet {
			return "mcp-servers.get", "projects.get", true
		}
	case "adminRevokeMcpServer":
		if method == http.MethodPost {
			return "mcp-servers.delete", "projects.act", true
		}
	case "adminListSkillBundles":
		if method == http.MethodGet {
			return "skill-bundles.list", "projects.get", true
		}
	case "adminCreateSkillBundle":
		if method == http.MethodPost {
			return "skill-bundles.create", "projects.act", true
		}
	case "adminGetSkillBundle":
		if method == http.MethodGet {
			return "skill-bundles.get", "projects.get", true
		}
	case "adminRevokeSkillBundle":
		if method == http.MethodPost {
			return "skill-bundles.delete", "projects.act", true
		}
	}
	return "", "", false
}

func (server *CapabilityHTTPServer) createMcpServer(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || common.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	input, err := platform.DecodeMcpServerCreateRequestJSON(body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	resource, err := server.store.CreateMcpServer(request.Context(), tenantID, principal, projectID, idempotencyKey, input)
	if err != nil {
		writeCapabilityError(writer, err)
		return
	}
	writeCapabilityResource(writer, http.StatusCreated, requestID, resource, true)
}

func (server *CapabilityHTTPServer) revokeMcpServer(writer http.ResponseWriter, request *http.Request, tenantID, projectID, serverID, requestID string, principal *authn.VerifiedPrincipal) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || common.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	input, err := platform.DecodeMcpServerRevokeRequestJSON(body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	resource, err := server.store.RevokeMcpServer(request.Context(), tenantID, principal, projectID, serverID, idempotencyKey, input)
	if err != nil {
		writeCapabilityError(writer, err)
		return
	}
	writeCapabilityResource(writer, http.StatusOK, requestID, resource, true)
}

func (server *CapabilityHTTPServer) getMcpServer(writer http.ResponseWriter, request *http.Request, tenantID, projectID, serverID, requestID string, principal *authn.VerifiedPrincipal) {
	if common.ValidateIdentifier(serverID, "/mcpServerId") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	resource, err := server.store.GetMcpServer(request.Context(), tenantID, principal, projectID, serverID)
	if err != nil {
		writeCapabilityError(writer, err)
		return
	}
	writeCapabilityResource(writer, http.StatusOK, requestID, resource, true)
}

func (server *CapabilityHTTPServer) listMcpServers(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok || pageSize < 1 || pageSize > 200 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	after, ok := decodeCapabilityPageToken("mcp", tenantID, projectID, pageToken)
	if !ok && pageToken != "" {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := server.store.ListMcpServers(request.Context(), tenantID, principal, projectID, after, pageSize)
	if err != nil {
		writeCapabilityError(writer, err)
		return
	}
	next := ""
	if page.NextServerID != "" {
		next, ok = encodeCapabilityPageToken("mcp", tenantID, projectID, page.NextServerID)
		if !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platform.EncodeMcpServerPageResponseJSON(common.ResponseEnvelope[platform.McpServerPage]{Value: platform.McpServerPage{APIVersion: platform.APIVersion, Kind: "McpServerPage", McpServers: page.Servers, NextPageToken: next}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *CapabilityHTTPServer) createSkillBundle(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || common.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	input, err := platform.DecodeSkillBundleCreateRequestJSON(body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	resource, err := server.store.CreateSkillBundle(request.Context(), tenantID, principal, projectID, idempotencyKey, input)
	if err != nil {
		writeCapabilityError(writer, err)
		return
	}
	writeCapabilityResource(writer, http.StatusCreated, requestID, resource, false)
}

func (server *CapabilityHTTPServer) revokeSkillBundle(writer http.ResponseWriter, request *http.Request, tenantID, projectID, bundleID, requestID string, principal *authn.VerifiedPrincipal) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok || common.ValidateIdempotencyKey(idempotencyKey, "/Idempotency-Key") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	input, err := platform.DecodeSkillBundleRevokeRequestJSON(body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	resource, err := server.store.RevokeSkillBundle(request.Context(), tenantID, principal, projectID, bundleID, idempotencyKey, input)
	if err != nil {
		writeCapabilityError(writer, err)
		return
	}
	writeCapabilityResource(writer, http.StatusOK, requestID, resource, false)
}

func (server *CapabilityHTTPServer) getSkillBundle(writer http.ResponseWriter, request *http.Request, tenantID, projectID, bundleID, requestID string, principal *authn.VerifiedPrincipal) {
	if common.ValidateIdentifier(bundleID, "/skillBundleId") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	resource, err := server.store.GetSkillBundle(request.Context(), tenantID, principal, projectID, bundleID)
	if err != nil {
		writeCapabilityError(writer, err)
		return
	}
	writeCapabilityResource(writer, http.StatusOK, requestID, resource, false)
}

func (server *CapabilityHTTPServer) listSkillBundles(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok || pageSize < 1 || pageSize > 200 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	after, ok := decodeCapabilityPageToken("skill", tenantID, projectID, pageToken)
	if !ok && pageToken != "" {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := server.store.ListSkillBundles(request.Context(), tenantID, principal, projectID, after, pageSize)
	if err != nil {
		writeCapabilityError(writer, err)
		return
	}
	next := ""
	if page.NextBundleID != "" {
		next, ok = encodeCapabilityPageToken("skill", tenantID, projectID, page.NextBundleID)
		if !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platform.EncodeSkillBundlePageResponseJSON(common.ResponseEnvelope[platform.SkillBundlePage]{Value: platform.SkillBundlePage{APIVersion: platform.APIVersion, Kind: "SkillBundlePage", SkillBundles: page.Bundles, NextPageToken: next}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func writeCapabilityResource(writer http.ResponseWriter, status int, requestID string, resource interface{}, mcp bool) {
	var body []byte
	var err error
	if mcp {
		body, err = platform.EncodeMcpServerResponseJSON(common.ResponseEnvelope[platform.McpServer]{Value: resource.(platform.McpServer)})
	} else {
		body, err = platform.EncodeSkillBundleResponseJSON(common.ResponseEnvelope[platform.SkillBundle]{Value: resource.(platform.SkillBundle)})
	}
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if mcp {
		writer.Header().Set("X-Resource-Version", resource.(platform.McpServer).Metadata.ResourceVersion)
	} else {
		writer.Header().Set("X-Resource-Version", resource.(platform.SkillBundle).Metadata.ResourceVersion)
	}
	writeJSONResponse(writer, status, requestID, body)
}

func writeCapabilityError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrCapabilityNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrCapabilityIdempotencyConflict), errors.Is(err, postgres.ErrCapabilityResourceVersionConflict):
		writePublicProblem(writer, http.StatusConflict, "conflict")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}

func encodeCapabilityPageToken(kind, tenantID, projectID, resourceID string) (string, bool) {
	if kind != "mcp" && kind != "skill" {
		return "", false
	}
	return encodePageToken("capability/"+kind+"/v1",
		pageTokenPart{value: tenantID, path: "/tenantId"},
		pageTokenPart{value: projectID, path: "/projectId"},
		pageTokenPart{value: resourceID, path: "/resourceId"},
	)
}

func decodeCapabilityPageToken(kind, tenantID, projectID, token string) (string, bool) {
	if token == "" {
		return "", true
	}
	if kind != "mcp" && kind != "skill" {
		return "", false
	}
	parts, ok := decodePageToken("capability/"+kind+"/v1", token,
		pageTokenPart{value: tenantID, path: "/tenantId"},
		pageTokenPart{value: projectID, path: "/projectId"},
		pageTokenPart{path: "/resourceId"},
	)
	if !ok {
		return "", false
	}
	return parts[2], true
}
