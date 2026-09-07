package accessgateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
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
	tenant, project, grant, session, action, suffix string
	port                                            int32
}

func parseRoute(path string) (route, bool) {
	if !strings.HasPrefix(path, "/v1/tenants/") {
		return route{}, false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/v1/tenants/"), "/")
	if len(parts) < 6 || parts[0] == "" || parts[1] != "projects" || parts[2] == "" ||
		parts[3] != "sandbox-access-grants" || parts[4] == "" {
		return route{}, false
	}
	value := route{tenant: parts[0], project: parts[2], grant: parts[4]}
	if parts[5] == "files" {
		if len(parts) == 6 {
			value.action = "files"
			return value, true
		}
		if len(parts) == 7 && parts[6] == "content" {
			value.action = "file-content"
			return value, true
		}
		return route{}, false
	}
	if parts[5] == "preview-ports" {
		if len(parts) < 7 || parts[6] == "" {
			return route{}, false
		}
		port, err := strconv.ParseInt(parts[6], 10, 32)
		if err != nil || strconv.FormatInt(port, 10) != parts[6] || port < 1024 || port > 65535 || port == 44772 {
			return route{}, false
		}
		value.port = int32(port)
		if len(parts) == 7 {
			value.action = "preview-port"
			return value, true
		}
		if parts[7] != "proxy" {
			return route{}, false
		}
		value.action = "preview-proxy"
		if len(parts) > 8 {
			value.suffix = "/" + strings.Join(parts[8:], "/")
		}
		return value, true
	}
	if parts[5] != "pty-sessions" {
		return route{}, false
	}
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
	if common.ValidateIdentifier(requestID, "/X-Request-ID") != nil {
		requestID = "request-unavailable"
		writer.Header().Set("X-Request-ID", requestID)
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	value, ok := parseRoute(request.URL.Path)
	if !ok {
		writeProblem(writer, http.StatusNotFound, "ROUTE_NOT_FOUND")
		return
	}
	allowed := value.action == "create" && request.Method == http.MethodPost ||
		value.action == "detail" && (request.Method == http.MethodGet || request.Method == http.MethodDelete) ||
		value.action == "websocket" && request.Method == http.MethodGet ||
		value.action == "files" && (request.Method == http.MethodGet || request.Method == http.MethodPut || request.Method == http.MethodDelete) ||
		value.action == "file-content" && request.Method == http.MethodGet ||
		value.action == "preview-port" && (request.Method == http.MethodPut || request.Method == http.MethodDelete) ||
		value.action == "preview-proxy" && (request.Method == http.MethodGet || request.Method == http.MethodPost ||
			request.Method == http.MethodPut || request.Method == http.MethodPatch || request.Method == http.MethodDelete)
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
	case "files":
		switch request.Method {
		case http.MethodGet:
			server.listFiles(writer, request, value, digest)
		case http.MethodPut:
			server.writeFile(writer, request, value, digest)
		case http.MethodDelete:
			server.deleteFile(writer, request, value, digest)
		}
	case "file-content":
		server.readFile(writer, request, value, digest)
	case "preview-port":
		if request.Method == http.MethodPut {
			server.registerPreviewPort(writer, request, value, digest)
		} else {
			server.revokePreviewPort(writer, request, value, digest)
		}
	case "preview-proxy":
		server.proxyPreview(writer, request, value, digest)
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
	var client *opensandbox.Client
	var created opensandbox.PTYObservation
	if authority.Access.TargetKind == "remote-worker" {
		created, err = server.executeRemotePTY(request.Context(), authority, tokenDigest,
			request.Header.Get("X-Request-ID"), "create", "", 0, false, nil)
	} else {
		client, err = server.client(authority)
		if err == nil {
			created, err = client.CreatePTY(request.Context(), ptyInput(authority))
		}
	}
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	session, err := server.store.PersistPTYSession(request.Context(), route.tenant, route.project, route.grant, created.SessionID, tokenDigest)
	if err != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), 5*time.Second)
		defer cancel()
		if client != nil {
			_ = client.DeletePTY(cleanupContext, ptyInput(authority), created.SessionID)
		} else if commandID, commandErr := newPTYCommandID(); commandErr == nil {
			_, _ = server.store.ExecuteRemoteWorkerSandboxPTY(cleanupContext, postgres.RemoteWorkerSandboxPTYRequest{
				Authority: authority, CommandID: commandID, RequestID: request.Header.Get("X-Request-ID"),
				TokenDigest: tokenDigest, Action: "delete", SessionID: created.SessionID,
			})
		}
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
	var observation opensandbox.PTYObservation
	if session.Grant.Access.TargetKind == "remote-worker" {
		observation, err = server.executeRemotePTY(request.Context(), session.Grant, tokenDigest,
			request.Header.Get("X-Request-ID"), "get", route.session, 0, false, nil)
	} else {
		var client *opensandbox.Client
		client, err = server.client(session.Grant)
		if err == nil {
			observation, err = client.GetPTY(request.Context(), ptyInput(session.Grant), route.session)
		}
	}
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
	if session.Grant.Access.TargetKind == "remote-worker" {
		_, err = server.executeRemotePTY(request.Context(), session.Grant, tokenDigest,
			request.Header.Get("X-Request-ID"), "delete", route.session, 0, false, nil)
	} else {
		var client *opensandbox.Client
		client, err = server.client(session.Grant)
		if err == nil {
			err = client.DeletePTY(request.Context(), ptyInput(session.Grant), route.session)
		}
	}
	if err != nil && !errors.Is(err, opensandbox.ErrNotFound) {
		writeGatewayError(writer, err)
		return
	}
	if err := server.store.MarkPTYSessionDeleted(request.Context(), route.tenant, route.project, route.grant, route.session, tokenDigest); err != nil {
		writeGatewayError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func newPTYCommandID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "rwpty-" + hex.EncodeToString(random[:]), nil
}

func (server *Server) executeRemotePTY(ctx context.Context, authority postgres.SandboxAccessGrantAuthority,
	tokenDigest, requestID, action, sessionID string, since int64, takeover bool,
	input *platform.RemoteWorkerSandboxPTYFrame,
) (opensandbox.PTYObservation, error) {
	commandID, err := newPTYCommandID()
	if err != nil {
		return opensandbox.PTYObservation{}, err
	}
	receipt, err := server.store.ExecuteRemoteWorkerSandboxPTY(ctx, postgres.RemoteWorkerSandboxPTYRequest{
		Authority: authority, CommandID: commandID, RequestID: requestID, TokenDigest: tokenDigest,
		Action: action, SessionID: sessionID, Since: since, Takeover: takeover, Input: input,
	})
	if err != nil {
		return opensandbox.PTYObservation{}, err
	}
	if err := remotePTYError(receipt); err != nil {
		return opensandbox.PTYObservation{}, err
	}
	observation := opensandbox.PTYObservation{SessionID: receipt.SessionID}
	if receipt.Running != nil {
		observation.Running = *receipt.Running
	}
	if receipt.OutputOffset != nil {
		observation.OutputOffset = *receipt.OutputOffset
	}
	return observation, nil
}

func remotePTYError(receipt platform.RemoteWorkerSandboxPTYCommandReceipt) error {
	if receipt.Result == "succeeded" {
		return nil
	}
	switch receipt.StableErrorCode {
	case "sandbox_runtime_unavailable":
		return opensandbox.ErrRuntimeFailed
	case "sandbox_pty_not_found":
		return opensandbox.ErrNotFound
	case "sandbox_pty_conflict":
		return opensandbox.ErrConflict
	case "sandbox_pty_output_limit":
		return opensandbox.ErrOutputLimit
	case "sandbox_pty_invalid":
		return opensandbox.ErrInvalid
	default:
		return opensandbox.ErrUnavailable
	}
}

type fileAccess struct {
	authority postgres.SandboxAccessGrantAuthority
	client    *opensandbox.Client
	eventID   string
}

func strictQuery(request *http.Request, allowed ...string) (url.Values, bool) {
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return nil, false
	}
	accepted := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		accepted[name] = struct{}{}
	}
	for name, values := range query {
		if _, ok := accepted[name]; !ok || len(values) != 1 {
			return nil, false
		}
	}
	return query, true
}

func newFileEventID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "file-" + hex.EncodeToString(random[:]), nil
}

func fileErrorCode(err error) string {
	_, code := gatewayError(err)
	return code
}

func (server *Server) startFileAccess(request *http.Request, value route, tokenDigest, action string) (fileAccess, error) {
	authority, err := server.store.ResolveGrant(request.Context(), value.tenant, value.project, value.grant, tokenDigest)
	if err != nil {
		return fileAccess{}, err
	}
	eventID, err := newFileEventID()
	if err != nil {
		return fileAccess{}, err
	}
	if err := server.store.StartFileAccess(request.Context(), value.tenant, value.project, value.grant, eventID, action, tokenDigest, request.Header.Get("X-Request-ID")); err != nil {
		return fileAccess{}, err
	}
	var client *opensandbox.Client
	if authority.Access.TargetKind == "docker" {
		client, err = server.client(authority)
		if err != nil {
			_ = server.finishFileAccess(request.Context(), value, tokenDigest, eventID, 0, err)
			return fileAccess{}, err
		}
	}
	return fileAccess{authority: authority, client: client, eventID: eventID}, nil
}

func remoteFileError(receipt platform.RemoteWorkerSandboxFileCommandReceipt) error {
	if receipt.Result == "succeeded" {
		return nil
	}
	switch receipt.StableErrorCode {
	case "sandbox_runtime_unavailable":
		return opensandbox.ErrRuntimeFailed
	case "sandbox_file_not_found":
		return opensandbox.ErrNotFound
	case "sandbox_file_conflict":
		return opensandbox.ErrConflict
	case "sandbox_file_limit":
		return opensandbox.ErrFileLimit
	case "sandbox_file_invalid":
		return opensandbox.ErrInvalid
	case "sandbox_file_timeout":
		return context.DeadlineExceeded
	default:
		return opensandbox.ErrUnavailable
	}
}

func remoteFileRequest(access fileAccess, tokenDigest, requestID, action, path string) postgres.RemoteWorkerSandboxFileRequest {
	return postgres.RemoteWorkerSandboxFileRequest{Authority: access.authority, EventID: access.eventID,
		RequestID: requestID, TokenDigest: tokenDigest, Action: action, Path: path}
}

func (server *Server) finishFileAccess(ctx context.Context, value route, tokenDigest, eventID string, bytesTransferred int64, operationErr error) error {
	outcome, stableErrorCode := "succeeded", ""
	if operationErr != nil {
		outcome, stableErrorCode = "failed", fileErrorCode(operationErr)
	}
	finishContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	completionErr := server.store.CompleteFileAccess(finishContext, value.tenant, value.project, value.grant, eventID, tokenDigest, outcome, stableErrorCode, bytesTransferred)
	if operationErr != nil {
		return operationErr
	}
	return completionErr
}

func (server *Server) listFiles(writer http.ResponseWriter, request *http.Request, value route, tokenDigest string) {
	query, ok := strictQuery(request, "path")
	if !ok || query.Get("path") == "" {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	validated, err := openapi.ValidateListSandboxFilesServerRequest(value.tenant, value.project, value.grant, request.Header.Get("X-Request-ID"), query.Get("path"))
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	access, err := server.startFileAccess(request, value, tokenDigest, "list")
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	result := make([]platform.SandboxFileEntry, 0)
	var operationErr error
	if access.client != nil {
		entries, err := access.client.ListFiles(request.Context(), ptyInput(access.authority), validated.Path)
		operationErr = err
		for _, entry := range entries {
			result = append(result, platform.SandboxFileEntry{Path: entry.Path, Type: entry.Type, SizeBytes: entry.SizeBytes, ModifiedAt: entry.ModifiedAt, FileVersion: entry.FileVersion})
		}
	} else {
		remote, err := server.store.ExecuteRemoteWorkerSandboxFile(request.Context(), remoteFileRequest(access, tokenDigest, request.Header.Get("X-Request-ID"), "list", validated.Path))
		operationErr = err
		if operationErr == nil {
			operationErr = remoteFileError(remote)
		}
		if operationErr == nil {
			result = remote.List.Entries
		}
	}
	if err := server.finishFileAccess(request.Context(), value, tokenDigest, access.eventID, 0, operationErr); err != nil {
		writeGatewayError(writer, err)
		return
	}
	grant := access.authority
	body, encodeErr := platform.EncodeSandboxFilePageResponseJSON(common.ResponseEnvelope[platform.SandboxFilePage]{Value: platform.SandboxFilePage{
		APIVersion: platform.APIVersion, Kind: "SandboxFilePage",
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: grant.Access.Scope.ProjectID},
		GrantID:    grant.GrantID, SandboxID: grant.Access.SandboxID, Generation: grant.Access.Generation,
		Path: validated.Path, Entries: result,
	}})
	writeGatewayJSON(writer, http.StatusOK, body, encodeErr)
}

func (server *Server) readFile(writer http.ResponseWriter, request *http.Request, value route, tokenDigest string) {
	query, ok := strictQuery(request, "path", "offset", "limit", "fileVersion")
	if !ok || query.Get("path") == "" {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	offset, limit := int64(0), int64(1<<20)
	var err error
	if query.Has("offset") {
		offset, err = strconv.ParseInt(query.Get("offset"), 10, 64)
	}
	if err == nil && query.Has("limit") {
		limit, err = strconv.ParseInt(query.Get("limit"), 10, 32)
	}
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	validated, err := openapi.ValidateReadSandboxFileServerRequest(value.tenant, value.project, value.grant, request.Header.Get("X-Request-ID"), query.Get("path"), offset, int(limit), query.Get("fileVersion"))
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	access, err := server.startFileAccess(request, value, tokenDigest, "read")
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	read := opensandbox.FileRead{Path: validated.Path}
	var operationErr error
	if access.client != nil {
		read, operationErr = access.client.ReadFile(request.Context(), ptyInput(access.authority), validated.Path, validated.Offset, validated.Limit, validated.FileVersion)
	} else {
		remoteRequest := remoteFileRequest(access, tokenDigest, request.Header.Get("X-Request-ID"), "read", validated.Path)
		remoteRequest.Offset, remoteRequest.Limit, remoteRequest.FileVersion = validated.Offset, int64(validated.Limit), validated.FileVersion
		remote, err := server.store.ExecuteRemoteWorkerSandboxFile(request.Context(), remoteRequest)
		operationErr = err
		if operationErr == nil {
			operationErr = remoteFileError(remote)
		}
		if operationErr == nil {
			content, err := base64.RawURLEncoding.DecodeString(remote.Read.ContentBase64URL)
			if err != nil {
				operationErr = opensandbox.ErrUnavailable
			} else {
				read.FileVersion, read.Offset, read.TotalBytes, read.Content = remote.Read.FileVersion, remote.Read.Offset, remote.Read.TotalBytes, content
			}
		}
	}
	if err := server.finishFileAccess(request.Context(), value, tokenDigest, access.eventID, int64(len(read.Content)), operationErr); err != nil {
		writeGatewayError(writer, err)
		return
	}
	nextOffset := read.Offset + int64(len(read.Content))
	grant := access.authority
	body, encodeErr := platform.EncodeSandboxFileReadPageResponseJSON(common.ResponseEnvelope[platform.SandboxFileReadPage]{Value: platform.SandboxFileReadPage{
		APIVersion: platform.APIVersion, Kind: "SandboxFileReadPage",
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: grant.Access.Scope.ProjectID},
		GrantID:    grant.GrantID, SandboxID: grant.Access.SandboxID, Generation: grant.Access.Generation,
		Path: read.Path, FileVersion: read.FileVersion, Offset: read.Offset, NextOffset: nextOffset,
		TotalBytes: read.TotalBytes, EOF: nextOffset == read.TotalBytes,
		ContentBase64URL: base64.RawURLEncoding.EncodeToString(read.Content),
	}})
	writeGatewayJSON(writer, http.StatusOK, body, encodeErr)
}

func (server *Server) writeFile(writer http.ResponseWriter, request *http.Request, value route, tokenDigest string) {
	if query, ok := strictQuery(request); !ok || len(query) != 0 {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])) != "application/json" {
		writeProblem(writer, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 22369622+2049))
	if err != nil || len(body) > 22369622+2048 {
		writeProblem(writer, http.StatusRequestEntityTooLarge, "SANDBOX_FILE_LIMIT")
		return
	}
	validated, err := openapi.ValidateWriteSandboxFileServerRequest(value.tenant, value.project, value.grant, request.Header.Get("X-Request-ID"), body)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	content, err := base64.RawURLEncoding.DecodeString(validated.Body.ContentBase64URL)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	access, err := server.startFileAccess(request, value, tokenDigest, "write")
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	var entry platform.SandboxFileEntry
	var operationErr error
	if access.client != nil {
		written, err := access.client.WriteFile(request.Context(), ptyInput(access.authority), validated.Path, content)
		operationErr = err
		entry = platform.SandboxFileEntry{Path: written.Path, Type: written.Type, SizeBytes: written.SizeBytes, ModifiedAt: written.ModifiedAt, FileVersion: written.FileVersion}
	} else {
		remoteRequest := remoteFileRequest(access, tokenDigest, request.Header.Get("X-Request-ID"), "write", validated.Path)
		remoteRequest.Content = content
		remote, err := server.store.ExecuteRemoteWorkerSandboxFile(request.Context(), remoteRequest)
		operationErr = err
		if operationErr == nil {
			operationErr = remoteFileError(remote)
		}
		if operationErr == nil {
			entry = remote.Write.Entry
		}
	}
	if err := server.finishFileAccess(request.Context(), value, tokenDigest, access.eventID, int64(len(content)), operationErr); err != nil {
		writeGatewayError(writer, err)
		return
	}
	body, encodeErr := platform.EncodeSandboxFileEntryResponseJSON(common.ResponseEnvelope[platform.SandboxFileEntry]{Value: platform.SandboxFileEntry{
		Path: entry.Path, Type: entry.Type, SizeBytes: entry.SizeBytes, ModifiedAt: entry.ModifiedAt, FileVersion: entry.FileVersion,
	}})
	writeGatewayJSON(writer, http.StatusOK, body, encodeErr)
}

func (server *Server) deleteFile(writer http.ResponseWriter, request *http.Request, value route, tokenDigest string) {
	query, ok := strictQuery(request, "path")
	if !ok || query.Get("path") == "" {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	validated, err := openapi.ValidateDeleteSandboxFileServerRequest(value.tenant, value.project, value.grant, request.Header.Get("X-Request-ID"), query.Get("path"))
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	access, err := server.startFileAccess(request, value, tokenDigest, "delete")
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	var operationErr error
	if access.client != nil {
		operationErr = access.client.DeleteFile(request.Context(), ptyInput(access.authority), validated.Path)
	} else {
		remote, err := server.store.ExecuteRemoteWorkerSandboxFile(request.Context(), remoteFileRequest(access, tokenDigest, request.Header.Get("X-Request-ID"), "delete", validated.Path))
		operationErr = err
		if operationErr == nil {
			operationErr = remoteFileError(remote)
		}
	}
	if err := server.finishFileAccess(request.Context(), value, tokenDigest, access.eventID, 0, operationErr); err != nil {
		writeGatewayError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func previewProxyPath(grant postgres.SandboxAccessGrantAuthority, port int32) string {
	return "/v1/tenants/" + grant.Access.Scope.TenantID + "/projects/" + grant.Access.Scope.ProjectID +
		"/sandbox-access-grants/" + grant.GrantID + "/preview-ports/" + strconv.FormatInt(int64(port), 10) + "/proxy"
}

func (server *Server) registerPreviewPort(writer http.ResponseWriter, request *http.Request, value route, tokenDigest string) {
	if query, ok := strictQuery(request); !ok || len(query) != 0 {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if _, err := openapi.ValidateRegisterSandboxPreviewPortServerRequest(value.tenant, value.project, value.grant, request.Header.Get("X-Request-ID"), value.port); err != nil {
		writeGatewayError(writer, err)
		return
	}
	registered, err := server.store.RegisterPreviewPort(request.Context(), value.tenant, value.project, value.grant, value.port, tokenDigest)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	grant := registered.Grant
	body, encodeErr := platform.EncodeSandboxPreviewPortResponseJSON(common.ResponseEnvelope[platform.SandboxPreviewPort]{Value: platform.SandboxPreviewPort{
		APIVersion: platform.APIVersion, Kind: "SandboxPreviewPort",
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: grant.Access.Scope.ProjectID},
		GrantID:    grant.GrantID, SandboxID: grant.Access.SandboxID, Generation: grant.Access.Generation,
		Port: registered.Port, Status: "active", ProxyPath: previewProxyPath(grant, registered.Port),
		RegisteredAt: registered.RegisteredAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}})
	writeGatewayJSON(writer, http.StatusOK, body, encodeErr)
}

func (server *Server) revokePreviewPort(writer http.ResponseWriter, request *http.Request, value route, tokenDigest string) {
	if query, ok := strictQuery(request); !ok || len(query) != 0 {
		writeProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if _, err := openapi.ValidateRevokeSandboxPreviewPortServerRequest(value.tenant, value.project, value.grant, request.Header.Get("X-Request-ID"), value.port); err != nil {
		writeGatewayError(writer, err)
		return
	}
	if err := server.store.RevokePreviewPort(request.Context(), value.tenant, value.project, value.grant, value.port, tokenDigest); err != nil {
		writeGatewayError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) proxyPreview(writer http.ResponseWriter, request *http.Request, value route, tokenDigest string) {
	registered, err := server.store.ResolvePreviewPort(request.Context(), value.tenant, value.project, value.grant, value.port, tokenDigest)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	if registered.Grant.Access.TargetKind == "remote-worker" {
		server.proxyRemotePreview(writer, request, value, tokenDigest, registered.Grant)
		return
	}
	client, err := server.client(registered.Grant)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	target, headers, err := client.PreviewHTTPProxyTarget(request.Context(), ptyInput(registered.Grant), value.port)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(upstream *http.Request) {
		upstream.URL.Scheme, upstream.URL.Host = target.Scheme, target.Host
		upstream.URL.Path, upstream.URL.RawPath = strings.TrimSuffix(target.Path, "/")+value.suffix, ""
		upstream.URL.RawQuery = request.URL.RawQuery
		upstream.Host = target.Host
		for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "OPEN-SANDBOX-API-KEY"} {
			upstream.Header.Del(name)
		}
		for name, values := range headers {
			upstream.Header.Del(name)
			for _, headerValue := range values {
				upstream.Header.Add(name, headerValue)
			}
		}
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		response.Header.Del("Set-Cookie")
		response.Header.Set("Cache-Control", "private, no-store")
		response.Header.Set("Referrer-Policy", "no-referrer")
		return nil
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
				if _, err := server.store.ResolvePreviewPort(authorizedContext, value.tenant, value.project, value.grant, value.port, tokenDigest); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	proxy.ServeHTTP(writer, request.WithContext(authorizedContext))
}

func (server *Server) proxyRemotePreview(writer http.ResponseWriter, request *http.Request, value route, tokenDigest string, authority postgres.SandboxAccessGrantAuthority) {
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writeProblem(writer, http.StatusRequestEntityTooLarge, "SANDBOX_PREVIEW_INPUT_LIMIT")
		return
	}
	headers, err := opensandbox.CanonicalPreviewHeaders(request.Header)
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	commandHeaders := make([]platform.RemoteWorkerSandboxPreviewHeader, len(headers))
	for index, header := range headers {
		commandHeaders[index] = platform.RemoteWorkerSandboxPreviewHeader{Name: header.Name, Value: header.Value}
	}
	requestPath := value.suffix
	if requestPath == "" {
		requestPath = "/"
	}
	receipt, err := server.store.ExecuteRemoteWorkerSandboxPreview(request.Context(), postgres.RemoteWorkerSandboxPreviewRequest{
		Authority: authority, CommandID: "rwpreview-" + rand.Text(), RequestID: request.Header.Get("X-Request-ID"),
		TokenDigest: tokenDigest, Method: request.Method, Path: requestPath, RawQuery: request.URL.RawQuery,
		Port: int64(value.port), Headers: commandHeaders, Body: body,
	})
	if err != nil {
		writeGatewayError(writer, err)
		return
	}
	if err := remotePreviewError(receipt); err != nil {
		writeGatewayError(writer, err)
		return
	}
	for _, header := range *receipt.Headers {
		writer.Header().Add(header.Name, header.Value)
	}
	writer.Header().Del("Set-Cookie")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.WriteHeader(int(*receipt.StatusCode))
	body, _ = base64.RawURLEncoding.DecodeString(*receipt.BodyBase64URL)
	_, _ = writer.Write(body)
}

func remotePreviewError(receipt platform.RemoteWorkerSandboxPreviewCommandReceipt) error {
	if receipt.Result == "succeeded" {
		return nil
	}
	switch receipt.StableErrorCode {
	case "sandbox_runtime_unavailable":
		return opensandbox.ErrRuntimeFailed
	case "sandbox_preview_not_found":
		return opensandbox.ErrNotFound
	case "sandbox_preview_input_limit":
		return opensandbox.ErrFileLimit
	case "sandbox_preview_output_limit":
		return opensandbox.ErrOutputLimit
	case "sandbox_preview_invalid":
		return opensandbox.ErrInvalid
	default:
		return opensandbox.ErrUnavailable
	}
}

func writeGatewayJSON(writer http.ResponseWriter, status int, body []byte, err error) {
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
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
	if session.Grant.Access.TargetKind == "remote-worker" {
		server.remotePTYWebSocket(writer, request, route, tokenDigest, session)
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

type remotePTYInput struct {
	frame *platform.RemoteWorkerSandboxPTYFrame
	err   error
}

func (server *Server) remotePTYWebSocket(writer http.ResponseWriter, request *http.Request, route route,
	tokenDigest string, session postgres.SandboxPTYSessionAuthority,
) {
	connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(64 << 10)
	authorizedContext, cancel := context.WithCancel(request.Context())
	defer cancel()
	inputs := make(chan remotePTYInput, 1)
	go func() {
		defer cancel()
		for {
			messageType, payload, readErr := connection.ReadMessage()
			if readErr != nil {
				select {
				case inputs <- remotePTYInput{err: readErr}:
				default:
				}
				return
			}
			if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage || len(payload) > 64<<10 {
				select {
				case inputs <- remotePTYInput{err: opensandbox.ErrInvalid}:
				default:
				}
				return
			}
			frameType := "binary"
			if messageType == websocket.TextMessage {
				frameType = "text"
			}
			select {
			case inputs <- remotePTYInput{frame: &platform.RemoteWorkerSandboxPTYFrame{
				MessageType: frameType, PayloadBase64URL: base64.RawURLEncoding.EncodeToString(payload)}}:
			case <-authorizedContext.Done():
				return
			}
		}
	}()
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
	since, _ := strconv.ParseInt(request.URL.Query().Get("since"), 10, 64)
	takeover := request.URL.Query().Get("takeover") == "1"
	for {
		var input *platform.RemoteWorkerSandboxPTYFrame
		select {
		case <-authorizedContext.Done():
			return
		case received := <-inputs:
			if received.err != nil {
				return
			}
			input = received.frame
		case <-time.After(100 * time.Millisecond):
		}
		commandID, err := newPTYCommandID()
		if err != nil {
			return
		}
		receipt, err := server.store.ExecuteRemoteWorkerSandboxPTY(authorizedContext, postgres.RemoteWorkerSandboxPTYRequest{
			Authority: session.Grant, CommandID: commandID, RequestID: request.Header.Get("X-Request-ID"),
			TokenDigest: tokenDigest, Action: "exchange", SessionID: route.session,
			Since: since, Takeover: takeover, Input: input,
		})
		if err != nil || remotePTYError(receipt) != nil || receipt.OutputOffset == nil || receipt.Running == nil || receipt.Frames == nil {
			return
		}
		for _, frame := range *receipt.Frames {
			payload, decodeErr := base64.RawURLEncoding.Strict().DecodeString(frame.PayloadBase64URL)
			messageType := websocket.BinaryMessage
			if frame.MessageType == "text" {
				messageType = websocket.TextMessage
			}
			if decodeErr != nil || connection.WriteMessage(messageType, payload) != nil {
				return
			}
		}
		since, takeover = *receipt.OutputOffset, true
		if !*receipt.Running {
			_ = connection.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			return
		}
	}
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
	status, code := gatewayError(err)
	writeProblem(writer, status, code)
}

func gatewayError(err error) (int, string) {
	var contractErr *common.JSONContractError
	switch {
	case errors.Is(err, postgres.ErrSandboxAccessGrantDenied):
		return http.StatusForbidden, "ACCESS_GRANT_DENIED"
	case errors.Is(err, coordination.ErrFoundationSandboxNotFound), errors.Is(err, opensandbox.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, postgres.ErrSandboxPreviewPortNotFound):
		return http.StatusNotFound, "PREVIEW_PORT_NOT_FOUND"
	case errors.Is(err, coordination.ErrFoundationSandboxConflict), errors.Is(err, opensandbox.ErrConflict), errors.Is(err, opensandbox.ErrRuntimeFailed):
		return http.StatusConflict, "RESOURCE_CONFLICT"
	case errors.Is(err, opensandbox.ErrFileLimit):
		return http.StatusRequestEntityTooLarge, "SANDBOX_FILE_LIMIT"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput), errors.Is(err, opensandbox.ErrInvalid), errors.As(err, &contractErr):
		return http.StatusBadRequest, "INVALID_REQUEST"
	default:
		return http.StatusServiceUnavailable, "SANDBOX_ACCESS_UNAVAILABLE"
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
