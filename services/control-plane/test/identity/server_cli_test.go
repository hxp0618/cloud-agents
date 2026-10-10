package identity_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
)

func TestIdentityServerCLIRoutesBindApplicationProofsAndClientIP(t *testing.T) {
	adminCredential, _, _ := browserauth.NewProof()
	userCredential, _, _ := browserauth.NewProof()
	controlPlaneCredential, _, _ := browserauth.NewProof()
	sessionHandle, sessionDigest, _ := browserauth.NewProof()
	csrfProof, _, _ := browserauth.NewProof()
	grant, grantDigest, _ := browserauth.NewProof()
	state, _, _ := browserauth.NewProof()
	code, _, _ := browserauth.NewProof()
	verifier, _, _ := browserauth.NewProof()
	store := &cliServerStore{
		session:       api.BrowserSession{Application: api.IdentityApplicationAdmin, CSRFToken: csrfProof},
		authorization: api.CLIAuthorization{AuthorizationID: "cli-auth-test", ExpiresAt: "2030-01-01T00:00:00Z"},
		approved:      api.CLIAuthorizationApproved{CallbackPort: 49190, State: state, AuthorizationCode: code, ExpiresAt: "2030-01-01T00:00:00Z"},
		grant:         api.CLIGrant{Credential: grant, Application: api.IdentityApplicationAdmin, ExpiresAt: "2030-01-01T00:00:00Z"},
		page:          api.BrowserTenantPage{Tenants: []api.BrowserTenant{{ID: "tenant-a", Name: "Tenant A", DisplayRoles: []string{"tenant.admin"}}}},
		token:         api.TenantToken{AccessToken: strings.Repeat("a", 22) + "." + strings.Repeat("b", 22) + "." + strings.Repeat("c", 22), TokenType: "Bearer", ExpiresAt: "2030-01-01T00:00:00Z"},
	}
	server, err := identity.NewServer(store, identity.ServiceCredentials{AdminWeb: adminCredential, UserWeb: userCredential, ControlPlane: controlPlaneCredential})
	if err != nil {
		t.Fatal(err)
	}

	startBody, _ := api.EncodeCLIAuthorizationStartRequestJSON(api.CLIAuthorizationStartRequest{Application: api.IdentityApplicationAdmin, CallbackPort: 49190, State: state, CodeChallenge: verifier})
	response := cliServerRequest(t, server, http.MethodPost, "/v1/identity/cli/authorizations", adminCredential, startBody, map[string]string{api.HeaderIdentityClientIP: "192.0.2.81"})
	if response.Code != http.StatusOK || store.startedApplication != api.IdentityApplicationAdmin || store.clientIP != netip.MustParseAddr("192.0.2.81") {
		t.Fatalf("start status=%d app=%q ip=%v", response.Code, store.startedApplication, store.clientIP)
	}
	wrongApplicationBody, _ := json.Marshal(api.CLIAuthorizationStartRequest{Application: api.IdentityApplicationUser, CallbackPort: 49190, State: state, CodeChallenge: verifier})
	response = cliServerRequest(t, server, http.MethodPost, "/v1/identity/cli/authorizations", adminCredential, wrongApplicationBody, map[string]string{api.HeaderIdentityClientIP: "192.0.2.81"})
	if response.Code != http.StatusBadRequest || store.startCalls != 1 {
		t.Fatalf("cross-application start status=%d calls=%d", response.Code, store.startCalls)
	}

	approveBody, _ := api.EncodeCLIAuthorizationApproveRequestJSON(api.CLIAuthorizationApproveRequest{State: state})
	response = cliServerRequest(t, server, http.MethodPost, "/v1/identity/cli/authorizations/cli-auth-test/approve", adminCredential, approveBody, map[string]string{"X-Cloud-Agents-Session": sessionHandle, "X-CSRF-Token": csrfProof})
	if response.Code != http.StatusOK || store.approvedSession != sessionDigest || store.approvedID != "cli-auth-test" {
		t.Fatalf("approve status=%d session=%x id=%q", response.Code, store.approvedSession, store.approvedID)
	}
	response = cliServerRequest(t, server, http.MethodPost, "/v1/identity/cli/authorizations/cli-auth-test/approve", adminCredential, approveBody, map[string]string{"X-Cloud-Agents-Session": sessionHandle, "X-CSRF-Token": state})
	if response.Code != http.StatusForbidden || store.approveCalls != 1 {
		t.Fatalf("invalid CSRF status=%d calls=%d", response.Code, store.approveCalls)
	}

	exchangeBody, _ := api.EncodeCLIGrantExchangeRequestJSON(api.CLIGrantExchangeRequest{AuthorizationID: "cli-auth-test", AuthorizationCode: code, CodeVerifier: verifier})
	response = cliServerRequest(t, server, http.MethodPost, "/v1/identity/cli/grants/exchange", adminCredential, exchangeBody, map[string]string{api.HeaderIdentityClientIP: "192.0.2.82"})
	if response.Code != http.StatusOK || store.exchangeApplication != api.IdentityApplicationAdmin {
		t.Fatalf("exchange status=%d app=%q", response.Code, store.exchangeApplication)
	}

	response = cliServerRequest(t, server, http.MethodGet, "/v1/identity/cli/tenants?pageSize=20", adminCredential, nil, map[string]string{"X-Cloud-Agents-CLI-Grant": grant})
	if response.Code != http.StatusOK || store.grantDigest != grantDigest || store.pageSize != 20 {
		t.Fatalf("tenants status=%d grant=%x pageSize=%d", response.Code, store.grantDigest, store.pageSize)
	}
	tokenBody, _ := json.Marshal(api.TenantTokenIssueRequest{TenantID: "tenant-a"})
	response = cliServerRequest(t, server, http.MethodPost, "/v1/identity/cli/tenant-token", adminCredential, tokenBody, map[string]string{"X-Cloud-Agents-CLI-Grant": grant})
	if response.Code != http.StatusOK || store.tokenRequest.TenantID != "tenant-a" {
		t.Fatalf("token status=%d request=%#v", response.Code, store.tokenRequest)
	}
	response = cliServerRequest(t, server, http.MethodDelete, "/v1/identity/cli/grant", adminCredential, nil, map[string]string{"X-Cloud-Agents-CLI-Grant": grant})
	if response.Code != http.StatusNoContent || store.revokeCalls != 1 {
		t.Fatalf("revoke status=%d calls=%d", response.Code, store.revokeCalls)
	}
}

type cliServerStore struct {
	identity.Store
	session             api.BrowserSession
	authorization       api.CLIAuthorization
	approved            api.CLIAuthorizationApproved
	grant               api.CLIGrant
	page                api.BrowserTenantPage
	token               api.TenantToken
	startedApplication  api.IdentityApplication
	exchangeApplication api.IdentityApplication
	clientIP            netip.Addr
	approvedSession     [sha256.Size]byte
	approvedID          string
	grantDigest         [sha256.Size]byte
	pageSize            int
	tokenRequest        api.TenantTokenIssueRequest
	startCalls          int
	approveCalls        int
	revokeCalls         int
}

func (store *cliServerStore) Session(_ context.Context, _ api.IdentityApplication, _ [sha256.Size]byte) (api.BrowserSession, error) {
	return store.session, nil
}
func (store *cliServerStore) StartCLIAuthorization(_ context.Context, application api.IdentityApplication, clientIP netip.Addr, _ api.CLIAuthorizationStartRequest) (api.CLIAuthorization, error) {
	store.startCalls++
	store.startedApplication, store.clientIP = application, clientIP
	return store.authorization, nil
}
func (store *cliServerStore) ApproveCLIAuthorization(_ context.Context, _ api.IdentityApplication, session [sha256.Size]byte, authorizationID string, _ api.CLIAuthorizationApproveRequest) (api.CLIAuthorizationApproved, error) {
	store.approveCalls++
	store.approvedSession, store.approvedID = session, authorizationID
	return store.approved, nil
}
func (store *cliServerStore) ExchangeCLIGrant(_ context.Context, application api.IdentityApplication, clientIP netip.Addr, _ api.CLIGrantExchangeRequest) (api.CLIGrant, error) {
	store.exchangeApplication, store.clientIP = application, clientIP
	return store.grant, nil
}
func (store *cliServerStore) ListCLITenants(_ context.Context, _ api.IdentityApplication, grant [sha256.Size]byte, pageSize int, _ string) (api.BrowserTenantPage, error) {
	store.grantDigest, store.pageSize = grant, pageSize
	return store.page, nil
}
func (store *cliServerStore) IssueCLITenantToken(_ context.Context, _ api.IdentityApplication, grant [sha256.Size]byte, request api.TenantTokenIssueRequest) (api.TenantToken, error) {
	store.grantDigest, store.tokenRequest = grant, request
	return store.token, nil
}
func (store *cliServerStore) RevokeCLIGrant(_ context.Context, _ api.IdentityApplication, grant [sha256.Size]byte) error {
	store.revokeCalls++
	store.grantDigest = grant
	return nil
}

func cliServerRequest(t *testing.T, handler http.Handler, method, path, serviceCredential string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "https://identity.test"+path, strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer "+serviceCredential)
	request.Header.Set("X-Request-ID", "cli-request-test")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
