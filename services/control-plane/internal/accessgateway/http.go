package accessgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgrant"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type Server struct {
	store  *postgres.AccessGatewayStore
	access *opensandbox.CredentialDirectory
}

func New(store *postgres.AccessGatewayStore, access *opensandbox.CredentialDirectory) (*Server, error) {
	if store == nil || access == nil {
		return nil, errors.New("access Gateway configuration is invalid")
	}
	return &Server{store: store, access: access}, nil
}

type route struct {
	tenant, project, grant, session, action string
}

func parseRoute(path string) (route, bool) {
	if !strings.HasPrefix(path, "/v1/tenants/") {
		return route{}, false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/v1/tenants/"), "/")
	if len(parts) < 6 || parts[0] == "" || parts[1] != "projects" || parts[2] == "" ||
		parts[3] != "sandbox-access-grants" || parts[4] == "" || parts[5] != "pty-sessions" {
		return route{}, false
	}
	value := route{tenant: parts[0], project: parts[2], grant: parts[4]}
	switch len(parts) {
	case 6:
		value.action = "create"
	case 7:
		if parts[6] == "" {
			return route{}, false
		}
		value.session, value.action = parts[6], "detail"
	case 8:
		if parts[6] == "" || parts[7] != "ws" {
			return route{}, false
		}
		value.session, value.action = parts[6], "websocket"
	default:
		return route{}, false
	}
	return value, true
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" || strings.ContainsAny(requestID, "\r\n,/") || len(requestID) > 128 {
		requestID = "request-unavailable"
	}
	writer.Header().Set("X-Request-ID", requestID)
	value, ok := parseRoute(request.URL.Path)
	if !ok {
		writeProblem(writer, http.StatusNotFound, "ROUTE_NOT_FOUND")
		return
	}
	allowed := value.action == "create" && request.Method == http.MethodPost ||
		value.action == "detail" && (request.Method == http.MethodGet || request.Method == http.MethodDelete) ||
		value.action == "websocket" && request.Method == http.MethodGet
	if !allowed {
		writeProblem(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	token, ok := bearer(request.Header.Values("Authorization"))
	if !ok {
		writeProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		return
	}
	digest := accessgrant.Digest(token)
	switch value.action {
	case "create":
		server.create(writer, request, value, digest)
	case "detail":
		if request.Method == http.MethodGet {
			server.get(writer, request, value, digest)
		} else {
			server.delete(writer, request, value, digest)
		}
	case "websocket":
		server.webSocket(writer, request, value, digest)
	}
}

func bearer(values []string) (string, bool) {
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", false
	}
	value := strings.TrimPrefix(values[0], "Bearer ")
	return value, strings.HasPrefix(value, "cag1_") && len(value) == 48 && !strings.ContainsAny(value, " \t\r\n,")
}

func ptyInput(authority postgres.SandboxAccessGrantAuthority) opensandbox.PTYInput {
	access := authority.Access
	return opensandbox.PTYInput{Identity: opensandbox.Identity{
		Tenant: access.Scope.TenantID, Project: access.Scope.ProjectID, Workspace: access.WorkspaceID,
		Sandbox: access.SandboxID, Operation: access.RuntimeOperationID,
		Generation: access.RuntimeGeneration, SpecDigest: access.RuntimeSpecDigest,
	}, RuntimeID: access.RuntimeID}
}

func (server *Server) client(authority postgres.SandboxAccessGrantAuthority) (*opensandbox.Client, error) {
	return server.access.Client(authority.Access.CredentialRef)
}

func (server *Server) create(writer http.ResponseWriter, request *http.Request, route route, tokenDigest string) {
	authority, err := server.store.ResolveGrant(request.Context(), route.tenant, route.project, route.grant, tokenDigest)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	client, err := server.client(authority)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	created, err := client.CreatePTY(request.Context(), ptyInput(authority))
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	session, err := server.store.PersistPTYSession(request.Context(), route.tenant, route.project, route.grant, created.SessionID, tokenDigest)
	if err != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), 5*time.Second)
		defer cancel()
		_ = client.DeletePTY(cleanupContext, ptyInput(authority), created.SessionID)
		writeGatewayError(writer, err)
		return
	}
	writeSession(writer, http.StatusCreated, session, created)
}

func (server *Server) get(writer http.ResponseWriter, request *http.Request, route route, tokenDigest string) {
	session, err := server.store.ResolvePTYSession(request.Context(), route.tenant, route.project, route.grant, route.session, tokenDigest)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	client, err := server.client(session.Grant)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	observation, err := client.GetPTY(request.Context(), ptyInput(session.Grant), route.session)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	writeSession(writer, http.StatusOK, session, observation)
}

func (server *Server) delete(writer http.ResponseWriter, request *http.Request, route route, tokenDigest string) {
	session, err := server.store.ResolvePTYSession(request.Context(), route.tenant, route.project, route.grant, route.session, tokenDigest)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	client, err := server.client(session.Grant)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	if err := client.DeletePTY(request.Context(), ptyInput(session.Grant), route.session); err != nil && !errors.Is(err, opensandbox.ErrNotFound) {
		writeGatewayError(writer, err)
		return
	}
	if err := server.store.MarkPTYSessionDeleted(request.Context(), route.tenant, route.project, route.grant, route.session, tokenDigest); err != nil {
		writeGatewayError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) webSocket(writer http.ResponseWriter, request *http.Request, route route, tokenDigest string) {
	query := request.URL.Query()
	for key, values := range query {
		if (key != "since" && key != "takeover") || len(values) != 1 {
			writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
	}
	if raw := query.Get("since"); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err != nil || value < 0 {
			writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
	}
	if raw := query.Get("takeover"); raw != "" && raw != "1" {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	session, err := server.store.ResolvePTYSession(request.Context(), route.tenant, route.project, route.grant, route.session, tokenDigest)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	client, err := server.client(session.Grant)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	target, headers, err := client.PTYWebSocketTarget(request.Context(), ptyInput(session.Grant), route.session)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(upstream *http.Request) {
		upstream.URL.Scheme, upstream.URL.Host, upstream.URL.Path = target.Scheme, target.Host, target.Path
		upstream.URL.RawPath, upstream.URL.RawQuery = "", request.URL.RawQuery
		upstream.Host = target.Host
		upstream.Header.Del("Authorization")
		upstream.Header.Del("Cookie")
		upstream.Header.Del("Forwarded")
		upstream.Header.Del("X-Forwarded-For")
		upstream.Header.Del("X-Forwarded-Host")
		upstream.Header.Del("X-Forwarded-Proto")
		for name, values := range headers {
			upstream.Header.Del(name)
			for _, value := range values {
				upstream.Header.Add(name, value)
			}
		}
	}
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, _ error) {
		writeProblem(response, http.StatusBadGateway, "SANDBOX_ACCESS_UNAVAILABLE")
	}
	authorizedContext, cancel := context.WithCancel(request.Context())
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-authorizedContext.Done():
				return
			case <-ticker.C:
				if _, err := server.store.ResolvePTYSession(authorizedContext, route.tenant, route.project, route.grant, route.session, tokenDigest); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	proxy.ServeHTTP(writer, request.WithContext(authorizedContext))
}

func writeSession(writer http.ResponseWriter, status int, session postgres.SandboxPTYSessionAuthority, observation opensandbox.PTYObservation) {
	state := "created"
	if observation.Running {
		state = "running"
	}
	grant := session.Grant
	value := platform.SandboxPTYSession{APIVersion: platform.APIVersion, Kind: "SandboxPTYSession",
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: grant.Access.Scope.ProjectID},
		GrantID:    grant.GrantID, SandboxID: grant.Access.SandboxID, Generation: grant.Access.Generation,
		SessionID: session.SessionID, State: state, OutputOffset: observation.OutputOffset,
		WebSocketPath: "/v1/tenants/" + grant.Access.Scope.TenantID + "/projects/" + grant.Access.Scope.ProjectID +
			"/sandbox-access-grants/" + grant.GrantID + "/pty-sessions/" + session.SessionID + "/ws",
		CreatedAt: session.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}
	body, err := platform.EncodeSandboxPTYSessionResponseJSON(common.ResponseEnvelope[platform.SandboxPTYSession]{Value: value})
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func writeGatewayError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrSandboxAccessGrantDenied):
		writeProblem(writer, http.StatusForbidden, "ACCESS_GRANT_DENIED")
	case errors.Is(err, coordination.ErrFoundationSandboxNotFound), errors.Is(err, opensandbox.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "NOT_FOUND")
	case errors.Is(err, coordination.ErrFoundationSandboxConflict), errors.Is(err, opensandbox.ErrConflict), errors.Is(err, opensandbox.ErrRuntimeFailed):
		writeProblem(writer, http.StatusConflict, "RESOURCE_CONFLICT")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput), errors.Is(err, opensandbox.ErrInvalid):
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
	default:
		writeProblem(writer, http.StatusServiceUnavailable, "SANDBOX_ACCESS_UNAVAILABLE")
	}
}

func writeProblem(writer http.ResponseWriter, status int, code string) {
	requestID := writer.Header().Get("X-Request-ID")
	problem := common.Problem{Type: "https://problems.cloud-agents.dev/" + strings.ToLower(strings.ReplaceAll(code, "_", "-")),
		Title: strings.ReplaceAll(strings.ToLower(code), "_", " "), Status: status,
		Error: common.StableError{Code: code, Retryable: status >= 500}, RequestID: requestID}
	body, _ := json.Marshal(problem)
	if status == http.StatusUnauthorized {
		writer.Header().Set("WWW-Authenticate", "Bearer")
	}
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}
