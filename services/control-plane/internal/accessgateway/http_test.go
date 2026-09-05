package accessgateway

import "testing"

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
}
