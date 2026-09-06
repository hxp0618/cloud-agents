package accessgateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
)

func TestRoutesCannotBecomeArbitraryProxy(t *testing.T) {
	valid := "/v1/tenants/tenant-a/projects/project-a/sandbox-access-grants/grant-a/pty-sessions/session-a/ws"
	route, ok := parseRoute(valid)
	if !ok || route.action != "websocket" || route.session != "session-a" {
		t.Fatal("fixed PTY WebSocket route rejected")
	}
	for _, path := range []string{
		valid + "/anything",
		"/v1/tenants/tenant-a/projects/project-a/sandbox-access-grants/grant-a/proxy/http://example.com",
		"/v1/admin/tenants/tenant-a/projects/project-a/sandbox-access-grants/grant-a/pty-sessions",
	} {
		if _, ok := parseRoute(path); ok {
			t.Fatalf("unsafe route accepted: %s", path)
		}
	}
	if _, ok := bearer([]string{"Bearer cag1_too-short"}); ok {
		t.Fatal("wrong-length grant token accepted")
	}
	for path, action := range map[string]string{
		"/v1/tenants/tenant-a/projects/project-a/sandbox-access-grants/grant-a/files":         "files",
		"/v1/tenants/tenant-a/projects/project-a/sandbox-access-grants/grant-a/files/content": "file-content",
	} {
		if route, ok := parseRoute(path); !ok || route.action != action {
			t.Fatalf("Files route rejected: %s", path)
		}
	}
	request := httptest.NewRequest(http.MethodGet, valid, nil)
	request.URL.RawQuery = "path=notes.txt&path=secret.txt"
	if _, ok := strictQuery(request, "path"); ok {
		t.Fatal("duplicate query authority accepted")
	}
	if status, code := gatewayError(opensandbox.ErrFileLimit); status != http.StatusRequestEntityTooLarge || code != "SANDBOX_FILE_LIMIT" {
		t.Fatalf("file limit mapping = %d %s", status, code)
	}
	if status, code := gatewayError(errors.New("upstream secret")); status != http.StatusServiceUnavailable || code != "SANDBOX_ACCESS_UNAVAILABLE" {
		t.Fatalf("untrusted error leaked = %d %s", status, code)
	}
}
