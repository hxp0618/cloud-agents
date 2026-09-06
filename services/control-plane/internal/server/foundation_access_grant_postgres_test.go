package server

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgateway"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgrant"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFoundationSandboxAccessGrantPTYPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL")
	credentialDirectory := os.Getenv("CLOUD_AGENTS_FOUNDATION_ACCESS_CREDENTIAL_DIRECTORY")
	if runtimeURL == "" || ownerURL == "" || credentialDirectory == "" {
		t.Skip("isolated running Sandbox environment is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal("runtime pool unavailable")
	}
	defer runtimePool.Close()
	ownerConfig, err := pgxpool.ParseConfig(ownerURL)
	if err != nil {
		t.Fatal("owner pool configuration invalid")
	}
	ownerConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET ROLE cloud_agents_migration_owner")
		return err
	}
	owner, err := pgxpool.NewWithConfig(ctx, ownerConfig)
	if err != nil {
		t.Fatal("owner pool unavailable")
	}
	defer owner.Close()

	verifier, adminToken, userToken := foundationVerifierAndTokens(t)
	coordinationStore, err := postgres.NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := accessgrant.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	foundation, err := NewFoundationHTTPServer(verifier, coordinationStore, nil, codec)
	if err != nil {
		t.Fatal(err)
	}
	foundationServer := httptest.NewServer(foundation)
	defer foundationServer.Close()
	admin, _ := api.NewHTTPClientWithClient(foundationServer.URL, adminToken, foundationServer.Client())
	user, _ := api.NewHTTPClientWithClient(foundationServer.URL, userToken, foundationServer.Client())
	current, err := admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", "request-grant-sandbox")
	if err != nil || current.Value.Spec.ObservedState != "running" {
		t.Fatalf("running Sandbox unavailable: %v", err)
	}
	request := platform.SandboxAccessGrantCreateRequest{ExpectedGeneration: current.Value.Spec.Generation, TTLSeconds: 60}
	if _, err := admin.CreateSandboxAccessGrant(ctx, "tenant", "project", "sandbox", "request-grant-admin-denied", "grant-admin-denied-key", request); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("Admin token Product Grant status=%d err=%v", clientStatus(err), err)
	}
	grant, err := user.CreateSandboxAccessGrant(ctx, "tenant", "project", "sandbox", "request-grant-create", "grant-primary-key-0001", request)
	if err != nil || grant.Value.AccessToken == "" || grant.Value.Generation != current.Value.Spec.Generation {
		t.Fatalf("issue Grant failed: %v", err)
	}
	replay, err := user.CreateSandboxAccessGrant(ctx, "tenant", "project", "sandbox", "request-grant-replay", "grant-primary-key-0001", request)
	if err != nil || replay.Value.GrantID != grant.Value.GrantID || replay.Value.AccessToken != grant.Value.AccessToken {
		t.Fatalf("Grant idempotency failed: %v", err)
	}
	stale := request
	stale.ExpectedGeneration--
	if _, err := user.CreateSandboxAccessGrant(ctx, "tenant", "project", "sandbox", "request-grant-stale", "grant-stale-key-0001", stale); clientStatus(err) != http.StatusConflict {
		t.Fatalf("stale generation status=%d err=%v", clientStatus(err), err)
	}

	gatewayStore, err := postgres.NewAccessGatewayStore(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := opensandbox.NewCredentialDirectory(credentialDirectory)
	if err != nil {
		t.Fatal(err)
	}
	newGateway := func() *httptest.Server {
		handler, err := accessgateway.New(gatewayStore, credentials)
		if err != nil {
			t.Fatal(err)
		}
		return httptest.NewServer(handler)
	}
	gateway := newGateway()
	wrongToken := "cag1_" + strings.Repeat("x", 43)
	if _, err := gatewayStore.ResolveGrant(ctx, "tenant", "project", grant.Value.GrantID, accessgrant.Digest(wrongToken)); !errors.Is(err, postgres.ErrSandboxAccessGrantDenied) {
		t.Fatalf("wrong Grant direct error=%T %v", err, err)
	}
	wrongClient, _ := api.NewHTTPClientWithClient(gateway.URL, wrongToken, gateway.Client())
	if _, err := wrongClient.CreatePTYSession(ctx, "tenant", "project", grant.Value.GrantID, "request-pty-wrong-token"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("wrong Grant token status=%d err=%v", clientStatus(err), err)
	}
	grantClient, _ := api.NewHTTPClientWithClient(gateway.URL, grant.Value.AccessToken, gateway.Client())
	if _, err := grantClient.CreatePTYSession(ctx, "other-tenant", "project", grant.Value.GrantID, "request-pty-cross-tenant"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("cross-tenant Grant status=%d err=%v", clientStatus(err), err)
	}
	session, err := grantClient.CreatePTYSession(ctx, "tenant", "project", grant.Value.GrantID, "request-pty-create")
	if err != nil || session.Value.SessionID == "" {
		t.Fatalf("create PTY failed: %v", err)
	}
	connection := dialPTY(t, gateway.URL, session.Value.WebSocketPath, grant.Value.AccessToken, 0, false)
	if err := connection.WriteMessage(websocket.BinaryMessage, append([]byte{0}, []byte("printf 'CAG_PTY_DIR=%s\\n' \"$PWD\"\n")...)); err != nil {
		t.Fatal(err)
	}
	live := readPTYUntil(t, connection, "CAG_PTY_DIR=/workspace")
	_ = connection.Close()
	observed, err := grantClient.GetPTYSession(ctx, "tenant", "project", grant.Value.GrantID, session.Value.SessionID, "request-pty-observe")
	if err != nil || observed.Value.OutputOffset <= 0 {
		t.Fatalf("PTY observation failed: %v", err)
	}

	gateway.Close()
	gateway = newGateway()
	defer gateway.Close()
	grantClient, _ = api.NewHTTPClientWithClient(gateway.URL, grant.Value.AccessToken, gateway.Client())
	persisted, err := grantClient.GetPTYSession(ctx, "tenant", "project", grant.Value.GrantID, session.Value.SessionID, "request-pty-after-restart")
	if err != nil || persisted.Value.OutputOffset != observed.Value.OutputOffset {
		t.Fatalf("Gateway restart lost PTY mapping: %v", err)
	}
	connection = dialPTY(t, gateway.URL, persisted.Value.WebSocketPath, grant.Value.AccessToken, 0, true)
	replayed := readPTYUntil(t, connection, "CAG_PTY_DIR=/workspace")
	if replayed.replayOffset < 0 {
		t.Fatal("cursor reconnect did not use replay framing")
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, append([]byte{0}, []byte("head -c 1100000 /dev/zero | tr '\\000' x; printf '\\nCAG_PTY_BUFFER_END\\n'\n")...)); err != nil {
		t.Fatal(err)
	}
	readPTYUntil(t, connection, "CAG_PTY_BUFFER_END")
	buffered, err := grantClient.GetPTYSession(ctx, "tenant", "project", grant.Value.GrantID, session.Value.SessionID, "request-pty-buffered")
	if err != nil || buffered.Value.OutputOffset < 1_100_000 {
		t.Fatalf("PTY output offset=%d err=%v", buffered.Value.OutputOffset, err)
	}
	_ = connection.Close()
	connection = dialPTY(t, gateway.URL, buffered.Value.WebSocketPath, grant.Value.AccessToken, 0, true)
	bounded := readPTYUntil(t, connection, "CAG_PTY_BUFFER_END")
	if bounded.replayOffset <= 0 || bounded.replayBytes > 1<<20 {
		t.Fatalf("bounded replay offset=%d bytes=%d", bounded.replayOffset, bounded.replayBytes)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, append([]byte{0}, []byte("ln -sfn /etc /workspace/cag-files-link; printf 'CAG_FILES_LINK_READY\\n'\n")...)); err != nil {
		t.Fatal(err)
	}
	readPTYUntil(t, connection, "CAG_FILES_LINK_READY")

	fileContent := []byte("files-survive-gateway-restart")
	written, err := grantClient.WriteSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-file-write", platform.SandboxFileWriteRequest{
		Path: "cag-files.txt", ContentBase64URL: base64.RawURLEncoding.EncodeToString(fileContent),
	})
	if err != nil || written.Value.Path != "cag-files.txt" || written.Value.SizeBytes != int64(len(fileContent)) {
		t.Fatalf("write file failed: %+v %v", written.Value, err)
	}
	files, err := grantClient.ListSandboxFiles(ctx, "tenant", "project", grant.Value.GrantID, "request-file-list", ".")
	foundFile, foundLink := false, false
	for _, entry := range files.Value.Entries {
		foundFile = foundFile || entry.Path == "cag-files.txt" && entry.Type == "file"
		foundLink = foundLink || entry.Path == "cag-files-link" && entry.Type == "symlink"
	}
	if err != nil || !foundFile || !foundLink {
		t.Fatalf("list files failed: %+v %v", files.Value.Entries, err)
	}
	firstFilePage, err := grantClient.ReadSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-file-read-first", "cag-files.txt", 0, 6, "")
	firstFileContent, decodeErr := base64.RawURLEncoding.DecodeString(firstFilePage.Value.ContentBase64URL)
	if err != nil || decodeErr != nil || string(firstFileContent) != string(fileContent[:6]) || firstFilePage.Value.EOF {
		t.Fatalf("first file page failed: %+v %v %v", firstFilePage.Value, err, decodeErr)
	}

	gateway.Close()
	gateway = newGateway()
	defer gateway.Close()
	grantClient, _ = api.NewHTTPClientWithClient(gateway.URL, grant.Value.AccessToken, gateway.Client())
	wrongClient, _ = api.NewHTTPClientWithClient(gateway.URL, wrongToken, gateway.Client())
	secondFilePage, err := grantClient.ReadSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-file-read-second", "cag-files.txt", firstFilePage.Value.NextOffset, 1<<20, firstFilePage.Value.FileVersion)
	secondFileContent, decodeErr := base64.RawURLEncoding.DecodeString(secondFilePage.Value.ContentBase64URL)
	if err != nil || decodeErr != nil || string(append(firstFileContent, secondFileContent...)) != string(fileContent) || !secondFilePage.Value.EOF {
		t.Fatalf("file restart page failed: %+v %v %v", secondFilePage.Value, err, decodeErr)
	}
	if _, err := wrongClient.ListSandboxFiles(ctx, "tenant", "project", grant.Value.GrantID, "request-file-wrong-token", "."); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("wrong file Grant token status=%d err=%v", clientStatus(err), err)
	}
	if _, err := grantClient.ListSandboxFiles(ctx, "other-tenant", "project", grant.Value.GrantID, "request-file-cross-tenant", "."); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("cross-tenant file Grant status=%d err=%v", clientStatus(err), err)
	}
	if _, err := grantClient.ReadSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-file-symlink", "cag-files-link/passwd", 0, 1, ""); clientStatus(err) != http.StatusConflict {
		t.Fatalf("symlink traversal status=%d err=%v", clientStatus(err), err)
	}
	rawStatus := func(method, target, requestID, contentType string, body io.Reader) int {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, method, gateway.URL+target, body)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+grant.Value.AccessToken)
		request.Header.Set("X-Request-ID", requestID)
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response, err := gateway.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return response.StatusCode
	}
	filesPath := "/v1/tenants/tenant/projects/project/sandbox-access-grants/" + grant.Value.GrantID + "/files"
	if status := rawStatus(http.MethodGet, filesPath+"?path="+url.QueryEscape("../secret"), "request-file-traversal", "", http.NoBody); status != http.StatusBadRequest {
		t.Fatalf("traversal status=%d", status)
	}
	oversized := `{"path":"cag-too-large.txt","contentBase64Url":"` + strings.Repeat("A", 22371680) + `"}`
	if status := rawStatus(http.MethodPut, filesPath, "request-file-oversized", "application/json", strings.NewReader(oversized)); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized file status=%d", status)
	}
	connection = dialPTY(t, gateway.URL, buffered.Value.WebSocketPath, grant.Value.AccessToken, buffered.Value.OutputOffset, true)
	if err := connection.WriteMessage(websocket.BinaryMessage, append([]byte{0}, []byte("rm -f /workspace/cag-files-link; printf 'CAG_FILES_LINK_REMOVED\\n'\n")...)); err != nil {
		t.Fatal(err)
	}
	readPTYUntil(t, connection, "CAG_FILES_LINK_REMOVED")
	if err := grantClient.DeleteSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-file-delete", "cag-files.txt"); err != nil {
		t.Fatal("delete file failed", err)
	}
	if _, err := grantClient.ReadSandboxFile(ctx, "tenant", "project", grant.Value.GrantID, "request-file-after-delete", "cag-files.txt", 0, 1, ""); clientStatus(err) != http.StatusNotFound {
		t.Fatalf("deleted file status=%d err=%v", clientStatus(err), err)
	}
	previewCommand := `node -e 'const http=require("http");http.createServer((req,res)=>{res.setHeader("content-type","application/json");if(req.url.startsWith("/slow")){res.write("open");const timer=setInterval(()=>res.write("."),50);req.on("close",()=>clearInterval(timer));return;}res.end(JSON.stringify({method:req.method,url:req.url,authorization:req.headers.authorization||"",proxyAuthorization:req.headers["proxy-authorization"]||"",cookie:req.headers.cookie||"",apiKey:req.headers["open-sandbox-api-key"]||"",forwarded:req.headers.forwarded||req.headers["x-forwarded-for"]||""}));}).listen(3000,"0.0.0.0");' >/workspace/cag-preview.log 2>&1 & printf 'CAG_PREVIEW_READY\n'`
	if err := connection.WriteMessage(websocket.BinaryMessage, append([]byte{0}, []byte(previewCommand+"\n")...)); err != nil {
		t.Fatal(err)
	}
	readPTYUntil(t, connection, "CAG_PREVIEW_READY")
	preview, err := grantClient.RegisterSandboxPreviewPort(ctx, "tenant", "project", grant.Value.GrantID, "request-preview-register", 3000)
	if err != nil || preview.Value.Port != 3000 || !strings.HasSuffix(preview.Value.ProxyPath, "/preview-ports/3000/proxy") {
		t.Fatalf("register Preview failed: %+v %v", preview.Value, err)
	}
	replayedPreview, err := grantClient.RegisterSandboxPreviewPort(ctx, "tenant", "project", grant.Value.GrantID, "request-preview-register-replay", 3000)
	if err != nil || replayedPreview.Value.RegisteredAt != preview.Value.RegisteredAt {
		t.Fatalf("Preview registration replay failed: %+v %v", replayedPreview.Value, err)
	}
	previewRequest := func(method, target, token string) (int, []byte) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, method, gateway.URL+target, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Proxy-Authorization", "Basic must-not-reach-sandbox")
		request.Header.Set("Cookie", "private-cookie=must-not-reach-sandbox")
		request.Header.Set("Forwarded", "for=must-not-reach-sandbox")
		request.Header.Set("X-Forwarded-For", "must-not-reach-sandbox")
		request.Header.Set("X-Request-ID", "request-preview-proxy")
		response, err := gateway.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, body
	}
	var previewStatus int
	var previewBody []byte
	for attempt := 0; attempt < 20; attempt++ {
		previewStatus, previewBody = previewRequest(http.MethodPost, preview.Value.ProxyPath+"/hello?value=alpha", grant.Value.AccessToken)
		if previewStatus == http.StatusOK {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var previewPayload map[string]string
	if err := json.Unmarshal(previewBody, &previewPayload); err != nil || previewStatus != http.StatusOK ||
		previewPayload["method"] != http.MethodPost || previewPayload["url"] != "/hello?value=alpha" ||
		previewPayload["authorization"] != "" || previewPayload["proxyAuthorization"] != "" ||
		previewPayload["cookie"] != "" || previewPayload["apiKey"] != "" || strings.Contains(previewPayload["forwarded"], "must-not-reach") {
		t.Fatalf("Preview proxy status=%d payload=%v body=%q err=%v", previewStatus, previewPayload, previewBody, err)
	}
	if status, _ := previewRequest(http.MethodGet, preview.Value.ProxyPath, wrongToken); status != http.StatusForbidden {
		t.Fatalf("wrong Preview token status=%d", status)
	}
	crossTenantPath := strings.Replace(preview.Value.ProxyPath, "/tenants/tenant/", "/tenants/other-tenant/", 1)
	if status, _ := previewRequest(http.MethodGet, crossTenantPath, grant.Value.AccessToken); status != http.StatusForbidden {
		t.Fatalf("cross-tenant Preview status=%d", status)
	}
	unregisteredPath := strings.Replace(preview.Value.ProxyPath, "/preview-ports/3000/", "/preview-ports/3001/", 1)
	if status, _ := previewRequest(http.MethodGet, unregisteredPath, grant.Value.AccessToken); status != http.StatusNotFound {
		t.Fatalf("unregistered Preview status=%d", status)
	}
	internalPath := strings.Replace(preview.Value.ProxyPath, "/preview-ports/3000/", "/preview-ports/44772/", 1)
	if status, _ := previewRequest(http.MethodGet, internalPath, grant.Value.AccessToken); status != http.StatusNotFound {
		t.Fatalf("internal Preview port status=%d", status)
	}

	gateway.Close()
	gateway = newGateway()
	defer gateway.Close()
	grantClient, _ = api.NewHTTPClientWithClient(gateway.URL, grant.Value.AccessToken, gateway.Client())
	if status, _ := previewRequest(http.MethodGet, preview.Value.ProxyPath+"/after-restart", grant.Value.AccessToken); status != http.StatusOK {
		t.Fatalf("Gateway restart Preview status=%d", status)
	}
	slowRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway.URL+preview.Value.ProxyPath+"/slow", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	slowRequest.Header.Set("Authorization", "Bearer "+grant.Value.AccessToken)
	slowRequest.Header.Set("X-Request-ID", "request-preview-slow")
	slowResponse, err := gateway.Client().Do(slowRequest)
	if err != nil {
		t.Fatal(err)
	}
	prefix := make([]byte, 4)
	if _, err := io.ReadFull(slowResponse.Body, prefix); err != nil || string(prefix) != "open" {
		t.Fatalf("slow Preview prefix=%q err=%v", prefix, err)
	}
	closedPreview := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(io.Discard, slowResponse.Body)
		closedPreview <- struct{}{}
	}()
	if err := grantClient.RevokeSandboxPreviewPort(ctx, "tenant", "project", grant.Value.GrantID, "request-preview-revoke", 3000); err != nil {
		t.Fatal("revoke Preview failed", err)
	}
	select {
	case <-closedPreview:
	case <-time.After(4 * time.Second):
		t.Fatal("active Preview response remained open after revoke")
	}
	_ = slowResponse.Body.Close()
	if status, _ := previewRequest(http.MethodGet, preview.Value.ProxyPath, grant.Value.AccessToken); status != http.StatusNotFound {
		t.Fatalf("revoked Preview port status=%d", status)
	}
	preview, err = grantClient.RegisterSandboxPreviewPort(ctx, "tenant", "project", grant.Value.GrantID, "request-preview-register-again", 3000)
	if err != nil {
		t.Fatal("re-register Preview failed", err)
	}

	if _, err := user.ListAdminSandboxAccessGrants(ctx, "tenant", "project", "sandbox", "request-grant-user-admin", 50, ""); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("ordinary user Admin Grant status=%d err=%v", clientStatus(err), err)
	}
	page, err := admin.ListAdminSandboxAccessGrants(ctx, "tenant", "project", "sandbox", "request-grant-admin-list", 50, "")
	if err != nil || len(page.Value.AccessGrants) != 1 || page.Value.AccessGrants[0].Spec.PTYSessionCount != 1 ||
		page.Value.AccessGrants[0].Spec.FileAccessCount != 7 || page.Value.AccessGrants[0].Spec.FileFailureCount != 2 ||
		len(page.Value.AccessGrants[0].Spec.PreviewPorts) != 1 || page.Value.AccessGrants[0].Spec.PreviewPorts[0] != 3000 ||
		page.Value.AccessGrants[0].Spec.LastFileAction != "read" || page.Value.AccessGrants[0].Spec.LastFileStatus != "failed" ||
		page.Value.AccessGrants[0].Spec.LastFileErrorCode != "NOT_FOUND" {
		t.Fatalf("Admin Grant metadata failed: %v", err)
	}
	assertAdminGrantRedaction(t, ctx, foundationServer.URL, adminToken, grant.Value.AccessToken)
	resourceVersion := page.Value.AccessGrants[0].Metadata.ResourceVersion
	revokeRequest := platform.SandboxAccessGrantRevokeRequest{
		ExpectedGeneration: current.Value.Spec.Generation, ExpectedResourceVersion: resourceVersion,
		ConfirmedGrantID: grant.Value.GrantID,
	}
	revoked, err := admin.RevokeAdminSandboxAccessGrant(ctx, "tenant", "project", "sandbox", grant.Value.GrantID, "request-grant-revoke", "grant-revoke-key-0001", revokeRequest)
	if err != nil || revoked.Value.Spec.Status != "revoked" {
		t.Fatalf("revoke Grant failed: %v", err)
	}
	if _, err := admin.RevokeAdminSandboxAccessGrant(ctx, "tenant", "project", "sandbox", grant.Value.GrantID, "request-grant-revoke-replay", "grant-revoke-key-0001", revokeRequest); err != nil {
		t.Fatalf("revoke replay failed: %v", err)
	}
	waitPTYClosed(t, connection)
	if _, err := grantClient.GetPTYSession(ctx, "tenant", "project", grant.Value.GrantID, session.Value.SessionID, "request-pty-revoked"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("revoked Grant status=%d err=%v", clientStatus(err), err)
	}
	if status, _ := previewRequest(http.MethodGet, preview.Value.ProxyPath, grant.Value.AccessToken); status != http.StatusForbidden {
		t.Fatalf("revoked Grant Preview status=%d", status)
	}

	expiring, err := user.CreateSandboxAccessGrant(ctx, "tenant", "project", "sandbox", "request-grant-expiring", "grant-expiring-key-0001", request)
	if err != nil {
		t.Fatalf("issue expiring Grant failed: %v", err)
	}
	expiringClient, _ := api.NewHTTPClientWithClient(gateway.URL, expiring.Value.AccessToken, gateway.Client())
	expiringPreview, err := expiringClient.RegisterSandboxPreviewPort(ctx, "tenant", "project", expiring.Value.GrantID, "request-preview-expiring", 3001)
	if err != nil {
		t.Fatalf("register expiring Preview failed: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE cloud_agents.sandbox_access_grants SET expires_at=created_at+interval '1 millisecond' WHERE tenant_id='tenant' AND project_uid='project' AND grant_uid=$1`, expiring.Value.GrantID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	expiredClient, _ := api.NewHTTPClientWithClient(gateway.URL, expiring.Value.AccessToken, gateway.Client())
	if _, err := expiredClient.CreatePTYSession(ctx, "tenant", "project", expiring.Value.GrantID, "request-pty-expired"); clientStatus(err) != http.StatusForbidden {
		t.Fatalf("expired Grant status=%d err=%v", clientStatus(err), err)
	}
	if status, _ := previewRequest(http.MethodGet, expiringPreview.Value.ProxyPath, expiring.Value.AccessToken); status != http.StatusForbidden {
		t.Fatalf("expired Grant Preview status=%d", status)
	}
	page, err = admin.ListAdminSandboxAccessGrants(ctx, "tenant", "project", "sandbox", "request-grant-admin-final", 50, "")
	statuses := map[string]string{}
	for _, item := range page.Value.AccessGrants {
		statuses[item.Metadata.UID] = item.Spec.Status
	}
	if err != nil || len(page.Value.AccessGrants) != 2 || statuses[expiring.Value.GrantID] != "expired" || statuses[grant.Value.GrantID] != "revoked" {
		t.Fatalf("Admin final Grant metadata failed: %v", err)
	}
	var issued, revokedCount, fileCount, fileFailureCount int
	if err := owner.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action='issued'), count(*) FILTER (WHERE action='revoked'), count(*) FILTER (WHERE action LIKE 'file_%'), count(*) FILTER (WHERE action LIKE 'file_%' AND outcome='failed') FROM cloud_agents.sandbox_access_grant_activity WHERE tenant_id='tenant' AND project_uid='project'`).Scan(&issued, &revokedCount, &fileCount, &fileFailureCount); err != nil || issued != 2 || revokedCount != 1 || fileCount != 7 || fileFailureCount != 2 {
		t.Fatalf("Grant activity issued=%d revoked=%d files=%d failures=%d err=%v", issued, revokedCount, fileCount, fileFailureCount, err)
	}
	receipt, _ := json.Marshal(map[string]any{
		"grantId": grant.Value.GrantID, "sessionId": session.Value.SessionID,
		"generation": current.Value.Spec.Generation, "liveOutput": live.outputBytes,
		"restartOffset": persisted.Value.OutputOffset, "replayOffset": replayed.replayOffset,
		"boundedOutputOffset": buffered.Value.OutputOffset, "boundedReplayOffset": bounded.replayOffset,
		"boundedReplayBytes": bounded.replayBytes, "adminStatus": 200, "userAdminStatus": 403,
		"wrongTokenStatus": 403, "crossTenantStatus": 403, "revokedStatus": 403,
		"expiredStatus": 403, "activeConnectionRevoked": true, "adminContentRedacted": true,
		"activityIssued": issued, "activityRevoked": revokedCount,
		"fileWriteBytes": len(fileContent), "filePages": 2, "fileGatewayRestart": true,
		"fileWrongTokenStatus": 403, "fileCrossTenantStatus": 403, "fileTraversalStatus": 400,
		"fileSymlinkStatus": 409, "fileOversizedStatus": 413, "fileDeletedStatus": 404,
		"fileAccessCount": fileCount, "fileFailureCount": fileFailureCount,
		"previewPort": 3000, "previewPrivate": true, "previewGatewayRestart": true,
		"previewWrongTokenStatus": 403, "previewCrossTenantStatus": 403,
		"previewUnregisteredStatus": 404, "previewInternalPortStatus": 404,
		"previewHeadersRedacted": true, "previewActiveResponseRevoked": true,
		"previewRevokedPortStatus": 404, "previewRevokedGrantStatus": 403,
		"previewExpiredGrantStatus": 403,
	})
	t.Logf("FOUNDATION_PTY_API=%s", receipt)
}

type ptyRead struct {
	replayOffset int64
	replayBytes  int
	outputBytes  int
}

func dialPTY(t *testing.T, baseURL, path, token string, since int64, takeover bool) *websocket.Conn {
	t.Helper()
	target, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	target.Scheme = "ws"
	target.Path = path
	query := url.Values{"since": {strconv.FormatInt(since, 10)}}
	if takeover {
		query.Set("takeover", "1")
	}
	target.RawQuery = query.Encode()
	connection, response, err := websocket.DefaultDialer.Dial(target.String(), http.Header{
		"Authorization": {"Bearer " + token}, "X-Request-ID": {"request-pty-websocket"},
	})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatalf("PTY WebSocket dial failed: %v", err)
	}
	return connection
}

func readPTYUntil(t *testing.T, connection *websocket.Conn, marker string) ptyRead {
	t.Helper()
	_ = connection.SetReadDeadline(time.Now().Add(15 * time.Second))
	result := ptyRead{replayOffset: -1}
	tail := ""
	for !strings.Contains(tail, marker) {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("PTY output before %q: %v", marker, err)
		}
		if messageType != websocket.BinaryMessage || len(payload) == 0 {
			continue
		}
		content := payload[1:]
		switch payload[0] {
		case 1, 2:
		case 3:
			if len(payload) < 9 {
				t.Fatal("short PTY replay frame")
			}
			if result.replayOffset < 0 {
				result.replayOffset = int64(binary.BigEndian.Uint64(payload[1:9]))
			}
			content = payload[9:]
			result.replayBytes += len(content)
		default:
			t.Fatalf("unexpected PTY frame type %d", payload[0])
		}
		result.outputBytes += len(content)
		tail += string(content)
		if len(tail) > len(marker)+256 {
			tail = tail[len(tail)-len(marker)-256:]
		}
	}
	return result
}

func waitPTYClosed(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	_ = connection.SetReadDeadline(time.Now().Add(4 * time.Second))
	for {
		if _, _, err := connection.ReadMessage(); err != nil {
			return
		}
	}
}

func assertAdminGrantRedaction(t *testing.T, ctx context.Context, baseURL, adminToken, grantToken string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/admin/tenants/tenant/projects/project/sandbox-sessions/sandbox/access-grants?pageSize=50", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.Header.Set("X-Request-ID", "request-grant-admin-raw")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("Admin Grant response status=%d err=%v", response.StatusCode, err)
	}
	for _, forbidden := range []string{grantToken, "accessToken", "CAG_PTY", "credentialRef", "providerCredentialRef", "endpoint", "proxyPath", "cag-files.txt", "cag-files-link", "cag-preview", "contentBase64Url"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("Admin Grant response disclosed %q", forbidden)
		}
	}
}
