package identity_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
)

type store struct {
	calls           int
	application     api.IdentityApplication
	clientIP        netip.Addr
	handle          string
	csrf            string
	err             error
	publicKeys      api.IdentityJWKS
	tenants         []api.BrowserTenant
	policy          api.EmailSuffixPolicy
	policyInput     api.EmailSuffixPolicyUpdate
	invitation      api.InvitationCreated
	invitationCalls int
}

func (s *store) JWKS(context.Context) (api.IdentityJWKS, error) { return s.publicKeys, s.err }

func (s *store) session(application api.IdentityApplication) api.BrowserSession {
	return api.BrowserSession{Application: application, User: api.CurrentUser{ID: "user-1", Email: "member@example.com", DisplayName: "Member", DisplayRoles: []string{}}, Tenants: []api.BrowserTenant{}, CSRFToken: s.csrf}
}

func (s *store) PasswordLogin(_ context.Context, application api.IdentityApplication, ip netip.Addr, _ api.PasswordLoginRequest) (api.IdentityLoginResult, error) {
	s.calls++
	s.application, s.clientIP = application, ip
	return api.IdentityLoginResult{Session: s.session(application), SessionHandle: s.handle}, s.err
}
func (s *store) Session(_ context.Context, application api.IdentityApplication, digest [32]byte) (api.BrowserSession, error) {
	s.calls++
	want, _ := browserauth.ProofDigest(s.handle)
	if digest != want {
		return api.BrowserSession{}, browserauth.ErrUnauthorized
	}
	return s.session(application), s.err
}
func (s *store) Tenants(context.Context, api.IdentityApplication, [32]byte, int, string) (api.BrowserTenantPage, error) {
	s.calls++
	return api.BrowserTenantPage{Tenants: s.tenants}, s.err
}
func (s *store) GetEmailSuffixPolicy(context.Context, [32]byte, string) (api.EmailSuffixPolicy, error) {
	s.calls++
	return s.policy, s.err
}
func (s *store) UpdateEmailSuffixPolicy(_ context.Context, _ [32]byte, _ string, input api.EmailSuffixPolicyUpdate) (api.EmailSuffixPolicy, error) {
	s.calls++
	s.policyInput = input
	return s.policy, s.err
}
func (s *store) Logout(context.Context, api.IdentityApplication, [32]byte, string) error {
	s.calls++
	return s.err
}
func (s *store) IssueTenantToken(context.Context, api.IdentityApplication, [32]byte, api.TenantTokenIssueRequest) (api.TenantToken, error) {
	s.calls++
	return api.TenantToken{}, s.err
}
func (s *store) TokenStatus(context.Context, api.TokenStatusRequest) (api.TokenStatus, error) {
	s.calls++
	return api.TokenStatus{Status: "inactive"}, s.err
}

func (s *store) CreateInvitation(context.Context, [32]byte, string, api.InvitationCreateRequest) (api.InvitationCreated, error) {
	s.invitationCalls++
	return s.invitation, s.err
}
func (s *store) ListInvitations(context.Context, [32]byte, string, int, string) (api.InvitationPage, error) {
	s.invitationCalls++
	return api.InvitationPage{Invitations: []api.Invitation{s.invitation.Invitation}}, s.err
}
func (s *store) RevokeInvitation(context.Context, [32]byte, string, string) error {
	s.invitationCalls++
	return s.err
}
func (s *store) AcceptInvitation(_ context.Context, _ api.IdentityApplication, _ *[32]byte, clientIP netip.Addr, _ api.InvitationAcceptRequest) error {
	s.invitationCalls++
	s.clientIP = clientIP
	return s.err
}
func (s *store) ListAccounts(context.Context, [32]byte, int, string) (api.IdentityAccountPage, error) {
	s.calls++
	return api.IdentityAccountPage{Accounts: []api.IdentityAccount{}}, s.err
}
func (s *store) ListTenantAccounts(context.Context, [32]byte, string, int, string) (api.IdentityAccountPage, error) {
	s.calls++
	return api.IdentityAccountPage{Accounts: []api.IdentityAccount{}}, s.err
}
func (s *store) DisableAccount(context.Context, [32]byte, string) error { s.calls++; return s.err }
func (s *store) IssuePasswordReset(context.Context, [32]byte, string) (api.PasswordResetCreated, error) {
	s.calls++
	return api.PasswordResetCreated{}, s.err
}
func (s *store) ChangePassword(context.Context, api.IdentityApplication, [32]byte, api.PasswordChangeRequest) error {
	s.calls++
	return s.err
}
func (s *store) AcceptPasswordReset(context.Context, netip.Addr, api.PasswordResetAcceptRequest) error {
	s.calls++
	return s.err
}
func (s *store) ListAuditEvents(context.Context, [32]byte, string, int, string) (api.IdentityAuditPage, error) {
	s.calls++
	return api.IdentityAuditPage{Events: []api.IdentityAuditEvent{}}, s.err
}

func (s *store) ListControlPlaneAuditEvents(context.Context, [32]byte, string, int, string) (api.ControlPlaneAuditPage, error) {
	s.calls++
	return api.ControlPlaneAuditPage{Events: []api.ControlPlaneAuditEvent{}}, s.err
}

func TestIdentityHTTPServiceBoundary(t *testing.T) {
	newProof := func() string {
		value, _, err := browserauth.NewProof()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	credentials := identity.ServiceCredentials{AdminWeb: newProof(), UserWeb: newProof(), ControlPlane: newProof()}
	backend := &store{handle: newProof(), csrf: newProof(), tenants: []api.BrowserTenant{}, policy: api.EmailSuffixPolicy{TenantID: "tenant-1", ResourceVersion: "2", AllowedDomains: []string{"example.com"}}}
	fixture, err := os.ReadFile("../../../../contracts/identity/v1alpha1/fixtures/golden/identity-jwks.json")
	if err != nil {
		t.Fatal(err)
	}
	backend.publicKeys, err = api.DecodeIdentityJWKSJSON(fixture)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := identity.NewServer(backend, credentials)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, body, ip, handle string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Request-ID", "request-identity-test")
		request.Header.Set("X-Cloud-Agents-Client-IP", ip)
		request.Header.Set("X-Cloud-Agents-Session", handle)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for _, test := range []struct {
		name, credential, path string
		status                 int
	}{
		{"tenant audit", credentials.AdminWeb, "/v1/identity/tenants/tenant-1/control-plane-audit-events?pageSize=20", http.StatusOK},
		{"user purpose denied", credentials.UserWeb, "/v1/identity/tenants/tenant-1/control-plane-audit-events?pageSize=20", http.StatusForbidden},
		{"unbounded page denied", credentials.AdminWeb, "/v1/identity/tenants/tenant-1/control-plane-audit-events?pageSize=201", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := call(http.MethodGet, test.path, test.credential, "", "", backend.handle)
			if response.Code != test.status {
				t.Fatalf("audit status=%d want=%d", response.Code, test.status)
			}
		})
	}
	backend.calls = 0
	loginBody := `{"email":"member@example.com","password":"a sufficiently long password"}`
	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, httptest.NewRequest("GET", "/.well-known/jwks.json", nil))
	if publicResponse.Code != 200 || publicResponse.Header().Get("X-Cloud-Agents-Session") != "" {
		t.Fatal("public JWKS requires service authentication or leaks a session")
	}
	if _, err := api.DecodeIdentityJWKSJSON(publicResponse.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct {
		header string
		values []string
	}{
		{"X-Request-ID", nil}, {"X-Request-ID", []string{"request-one", "request-two"}},
		{"Content-Type", []string{"application/json", "text/plain"}},
	} {
		request := httptest.NewRequest("POST", "/v1/identity/login/password", strings.NewReader(loginBody))
		request.Header.Set("Authorization", "Bearer "+credentials.AdminWeb)
		request.Header.Set("X-Request-ID", "request-identity-test")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Cloud-Agents-Client-IP", "192.0.2.1")
		request.Header.Del(invalid.header)
		for _, value := range invalid.values {
			request.Header.Add(invalid.header, value)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code < 400 || backend.calls != 0 {
			t.Fatal("ambiguous service headers accepted")
		}
	}
	for _, test := range []struct {
		token, body, ip string
		status          int
	}{
		{"unknown", loginBody, "192.0.2.1", 401},
		{credentials.ControlPlane, loginBody, "192.0.2.1", 403},
		{credentials.AdminWeb, `{"email":"member@example.com","password":"a sufficiently long password","application":"user"}`, "192.0.2.1", 400},
		{credentials.AdminWeb, loginBody, "192.0.2.1, 192.0.2.2", 400},
		{credentials.AdminWeb, loginBody, "", 400},
	} {
		response := call("POST", "/v1/identity/login/password", test.token, test.body, test.ip, "")
		if response.Code != test.status || backend.calls != 0 {
			t.Fatalf("rejection status=%d calls=%d", response.Code, backend.calls)
		}
	}
	for _, pair := range []struct {
		token string
		app   api.IdentityApplication
	}{{credentials.AdminWeb, api.IdentityApplicationAdmin}, {credentials.UserWeb, api.IdentityApplicationUser}} {
		response := call("POST", "/v1/identity/login/password", pair.token, loginBody, "192.0.2.1", "")
		if response.Code != 200 || backend.application != pair.app || backend.clientIP.String() != "192.0.2.1" {
			t.Fatalf("login status=%d application=%q", response.Code, backend.application)
		}
		if response.Header().Get("X-Cloud-Agents-Session") != backend.handle || strings.Contains(response.Body.String(), backend.handle) || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unsafe login response")
		}
		if _, err := api.DecodeBrowserSessionJSON(response.Body.Bytes()); err != nil {
			t.Fatal(err)
		}
		if response.Header().Get("X-Request-ID") != "request-identity-test" {
			t.Fatal("request correlation lost")
		}
	}
	before := backend.calls
	statusBody := `{"tokenSha256":"sha256:` + strings.Repeat("a", 64) + `","expectedApplication":"admin","expectedClientId":"cloud-agents-admin-web","expectedTenantId":"tenant-1"}`
	if response := call("POST", "/v1/identity/token-status", credentials.AdminWeb, statusBody, "", ""); response.Code != 403 || backend.calls != before {
		t.Fatal("Web service may introspect tokens")
	}
	if response := call("POST", "/v1/identity/token-status", credentials.ControlPlane, statusBody, "", ""); response.Code != 200 {
		t.Fatalf("CP status=%d", response.Code)
	}
	if response := call("GET", "/v1/identity/session", credentials.AdminWeb, "", "", backend.handle); response.Code != 200 {
		t.Fatalf("session status=%d", response.Code)
	}
	backend.csrf = backend.handle
	if response := call("POST", "/v1/identity/login/password", credentials.AdminWeb, loginBody, "192.0.2.1", ""); response.Code != 503 || strings.Contains(response.Body.String(), backend.handle) || response.Header().Get("X-Cloud-Agents-Session") != "" {
		t.Fatal("session handle may not be exposed as CSRF proof")
	}
	backend.csrf = newProof()
	upstream := httptest.NewTLSServer(handler)
	defer upstream.Close()
	client, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, credentials.UserWeb, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	login, err := client.PasswordLogin(context.Background(), "request-login", "2001:db8::1", api.PasswordLoginRequest{Email: "member@example.com", Password: "a sufficiently long password"})
	if err != nil || login.Session.Application != api.IdentityApplicationUser || login.SessionHandle != backend.handle {
		t.Fatalf("SDK login failed: %v", err)
	}
	if _, err := client.GetBrowserSession(context.Background(), login.SessionHandle, "request-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListBrowserTenants(context.Background(), login.SessionHandle, "request-tenants", 100, ""); err != nil {
		t.Fatal(err)
	}
	backend.tenants = []api.BrowserTenant{{ID: "tenant-1", Name: "One", DisplayRoles: []string{}}, {ID: "tenant-2", Name: "Two", DisplayRoles: []string{}}}
	if _, err := client.ListBrowserTenants(context.Background(), login.SessionHandle, "request-tenants", 1, ""); err == nil {
		t.Fatal("tenant page exceeded requested bound")
	}
	backend.tenants = []api.BrowserTenant{}
	if err := client.LogoutBrowserSession(context.Background(), login.SessionHandle, "request-logout", login.Session.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetEmailSuffixPolicy(context.Background(), login.SessionHandle, "tenant-1", "request-policy"); err == nil {
		t.Fatal("User-purpose service credential reached the Admin email policy route")
	}
	adminClient, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, credentials.AdminWeb, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := adminClient.GetEmailSuffixPolicy(context.Background(), login.SessionHandle, "tenant-1", "request-policy")
	if err != nil || policy.ResourceVersion != "2" {
		t.Fatalf("email policy read failed: %#v, %v", policy, err)
	}
	if _, err := adminClient.UpdateEmailSuffixPolicy(context.Background(), login.SessionHandle, "tenant-1", "request-policy-update", newProof(), api.EmailSuffixPolicyUpdate{ExpectedResourceVersion: "2", AllowedDomains: []string{"example.com"}}); err == nil {
		t.Fatal("email policy update accepted the wrong CSRF proof")
	}
	updated, err := adminClient.UpdateEmailSuffixPolicy(context.Background(), login.SessionHandle, "tenant-1", "request-policy-update", login.Session.CSRFToken, api.EmailSuffixPolicyUpdate{ExpectedResourceVersion: "2", AllowedDomains: []string{"example.com"}})
	if err != nil || updated.ResourceVersion != "2" || backend.policyInput.ExpectedResourceVersion != "2" {
		t.Fatalf("email policy update failed: %#v, %#v, %v", updated, backend.policyInput, err)
	}
	backend.err = errors.New("database password and private token must stay secret")
	response := call("GET", "/v1/identity/session", credentials.AdminWeb, "", "", backend.handle)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private") || response.Header().Get("X-Cloud-Agents-Session") != "" {
		t.Fatal("backend error was exposed")
	}
}

func TestIdentityInvitationHTTPBoundary(t *testing.T) {
	proof := func() string {
		value, _, err := browserauth.NewProof()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	credentials := identity.ServiceCredentials{AdminWeb: proof(), UserWeb: proof(), ControlPlane: proof()}
	backend := &store{handle: proof(), csrf: proof()}
	fixture, err := os.ReadFile("../../../../contracts/identity/v1alpha1/fixtures/golden/invitation-created.json")
	if err != nil {
		t.Fatal(err)
	}
	backend.invitation, err = api.DecodeInvitationCreatedJSON(fixture)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := identity.NewServer(backend, credentials)
	if err != nil {
		t.Fatal(err)
	}
	create := `{"email":"invited@example.com","roleName":"project.developer","scopeLevel":"project","scopeId":"project-one","verification":"admin-attested"}`
	acceptNew := `{"invitationCode":"` + backend.invitation.InvitationCode + `","password":"a sufficiently long password","displayName":"Invited User"}`
	acceptExisting := `{"invitationCode":"` + backend.invitation.InvitationCode + `"}`
	for _, test := range []struct {
		name, method, path, credential, handle, csrf, clientIP, body string
		status                                                       int
		calls                                                        int
	}{
		{"create", "POST", "/v1/identity/tenants/tenant-one/invitations", credentials.AdminWeb, backend.handle, backend.csrf, "", create, 201, 1},
		{"list", "GET", "/v1/identity/tenants/tenant-one/invitations?pageSize=20", credentials.AdminWeb, backend.handle, "", "", "", 200, 1},
		{"revoke", "DELETE", "/v1/identity/tenants/tenant-one/invitations/invite-one", credentials.AdminWeb, backend.handle, backend.csrf, "", "", 204, 1},
		{"user create denied", "POST", "/v1/identity/tenants/tenant-one/invitations", credentials.UserWeb, backend.handle, backend.csrf, "", create, 403, 0},
		{"create csrf denied", "POST", "/v1/identity/tenants/tenant-one/invitations", credentials.AdminWeb, backend.handle, proof(), "", create, 403, 0},
		{"anonymous new acceptance", "POST", "/v1/identity/invitations/accept", credentials.UserWeb, "", "", "192.0.2.44", acceptNew, 204, 1},
		{"existing acceptance", "POST", "/v1/identity/invitations/accept", credentials.UserWeb, backend.handle, backend.csrf, "192.0.2.45", acceptExisting, 204, 1},
		{"acceptance without trusted IP", "POST", "/v1/identity/invitations/accept", credentials.UserWeb, "", "", "", acceptNew, 400, 0},
		{"existing csrf denied", "POST", "/v1/identity/invitations/accept", credentials.UserWeb, backend.handle, "", "192.0.2.45", acceptExisting, 403, 0},
		{"malformed session never anonymous", "POST", "/v1/identity/invitations/accept", credentials.UserWeb, "bad-session", "", "192.0.2.44", acceptNew, 401, 0},
		{"unbounded list denied", "GET", "/v1/identity/tenants/tenant-one/invitations?pageSize=201", credentials.AdminWeb, backend.handle, "", "", "", 400, 0},
		{"method refused", "POST", "/v1/identity/tenants/tenant-one/invitations/invite-one", credentials.AdminWeb, backend.handle, backend.csrf, "", create, 404, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := backend.invitationCalls
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+test.credential)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Request-ID", "invitation-http-test")
			if test.clientIP != "" {
				request.Header.Set("X-Cloud-Agents-Client-IP", test.clientIP)
			}
			if test.handle != "" {
				request.Header.Set("X-Cloud-Agents-Session", test.handle)
			}
			if test.csrf != "" {
				request.Header.Set("X-CSRF-Token", test.csrf)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || backend.invitationCalls-before != test.calls {
				t.Fatalf("status=%d calls=%d, expected %d/%d", response.Code, backend.invitationCalls-before, test.status, test.calls)
			}
			if response.Header().Get("X-Cloud-Agents-Session") != "" {
				t.Fatal("invitation minted a session")
			}
			if test.calls == 1 && test.clientIP != "" && backend.clientIP.String() != test.clientIP {
				t.Fatalf("trusted client IP = %q, want %q", backend.clientIP, test.clientIP)
			}
			if test.method == "GET" && strings.Contains(response.Body.String(), backend.invitation.InvitationCode) {
				t.Fatal("invitation listing exposed proof")
			}
		})
	}
}
