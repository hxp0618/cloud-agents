package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type serviceAccountVerifierFake struct {
	requests []authn.VerificationRequest
	tokens   []string
	errAt    int
}

func (fake *serviceAccountVerifierFake) Verify(token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	fake.tokens = append(fake.tokens, token)
	fake.requests = append(fake.requests, request)
	if fake.errAt > 0 && len(fake.requests) == fake.errAt {
		return nil, errors.New("verification denied")
	}
	return &authn.VerifiedPrincipal{}, nil
}

type serviceAccountManagerFake struct {
	created           api.ServiceAccountCreated
	page              api.ServiceAccountPage
	rotated           api.ServiceAccountRotated
	disabledVersion   int64
	scope             authz.ScopeRef
	err               error
	createCalls       int
	listCalls         int
	rotateCalls       int
	disableCalls      int
	managementCalls   int
	after             string
	limit             int
	createRequest     api.ServiceAccountCreateRequest
	expectedVersion   int64
	lastCorrelation   string
	denialAction      string
	denialApplication api.IdentityApplication
	denialScope       authz.ScopeRef
	denialCalls       int
	createPrincipals  [2]*authn.VerifiedPrincipal
}

func (fake *serviceAccountManagerFake) Create(_ context.Context, _ string, membership, binding *authn.VerifiedPrincipal, request api.ServiceAccountCreateRequest, correlationID string) (api.ServiceAccountCreated, error) {
	fake.createCalls++
	fake.createPrincipals = [2]*authn.VerifiedPrincipal{membership, binding}
	fake.createRequest = request
	fake.lastCorrelation = correlationID
	return fake.created, fake.err
}

func (fake *serviceAccountManagerFake) ManagementScope(context.Context, string, string) (authz.ScopeRef, error) {
	fake.managementCalls++
	return fake.scope, fake.err
}

func (fake *serviceAccountManagerFake) Rotate(_ context.Context, _, _ string, _ *authn.VerifiedPrincipal, expected int64, correlationID string) (api.ServiceAccountRotated, error) {
	fake.rotateCalls++
	fake.expectedVersion = expected
	fake.lastCorrelation = correlationID
	return fake.rotated, fake.err
}

func (fake *serviceAccountManagerFake) Disable(_ context.Context, _, _ string, _ *authn.VerifiedPrincipal, expected int64, correlationID string) (int64, error) {
	fake.disableCalls++
	fake.expectedVersion = expected
	fake.lastCorrelation = correlationID
	return fake.disabledVersion, fake.err
}

func (fake *serviceAccountManagerFake) List(_ context.Context, _ string, _ *authn.VerifiedPrincipal, after string, limit int) (api.ServiceAccountPage, error) {
	fake.listCalls++
	fake.after, fake.limit = after, limit
	return fake.page, fake.err
}

func (fake *serviceAccountManagerFake) RecordPermissionDenial(_ context.Context, _, _ string, action string, application api.IdentityApplication, scope authz.ScopeRef, _ *authn.VerifiedPrincipal, correlationID string) error {
	fake.denialCalls++
	fake.denialAction = action
	fake.denialApplication = application
	fake.denialScope = scope
	fake.lastCorrelation = correlationID
	return fake.err
}

func TestServiceAccountHTTPServerCreateRequiresBothExactPermissions(t *testing.T) {
	manager := &serviceAccountManagerFake{created: serviceAccountCreatedFixture(t)}
	verifier := &serviceAccountVerifierFake{}
	server, err := NewServiceAccountHTTPServer(verifier, manager)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"serviceAccountId":"automation-one","displayName":"Automation One","application":"admin","roleName":"tenant.admin","scopeLevel":"project","scopeId":"project-one"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant-one/service-accounts", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "request-one")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("X-Resource-Version") != "1" || manager.createCalls != 1 {
		t.Fatalf("status=%d manager=%#v body=%s", response.Code, manager, response.Body.String())
	}
	wantBase := authn.VerificationRequest{TenantID: "tenant-one", ResourceLevel: "project", ResourceID: "project-one"}
	if len(verifier.requests) != 2 || verifier.tokens[0] != "admin-token" || verifier.tokens[1] != "admin-token" {
		t.Fatalf("verifications=%#v tokens=%#v", verifier.requests, verifier.tokens)
	}
	if verifier.requests[0] != (authn.VerificationRequest{TenantID: wantBase.TenantID, ResourceLevel: wantBase.ResourceLevel, ResourceID: wantBase.ResourceID, RequiredPermission: "memberships.create"}) ||
		verifier.requests[1] != (authn.VerificationRequest{TenantID: wantBase.TenantID, ResourceLevel: wantBase.ResourceLevel, ResourceID: wantBase.ResourceID, RequiredPermission: "role-bindings.bind"}) ||
		manager.createPrincipals[0] == manager.createPrincipals[1] {
		t.Fatalf("verification requests=%#v principals=%#v", verifier.requests, manager.createPrincipals)
	}
	decoded, err := api.DecodeServiceAccountCreatedJSON(response.Body.Bytes())
	if err != nil || decoded.Credential == "" || decoded.ServiceAccount.ID != "automation-one" {
		t.Fatalf("response=%#v err=%v", decoded, err)
	}
}

func TestServiceAccountHTTPServerAuditsAuthenticatedCreatePermissionDenial(t *testing.T) {
	manager := &serviceAccountManagerFake{}
	verifier := &serviceAccountVerifierFake{errAt: 2}
	server, _ := NewServiceAccountHTTPServer(verifier, manager)
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant-one/service-accounts", strings.NewReader(
		`{"serviceAccountId":"automation-one","displayName":"Automation One","application":"admin","roleName":"tenant.admin","scopeLevel":"tenant","scopeId":"tenant-one"}`,
	))
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "request-one")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.createCalls != 0 || manager.denialCalls != 1 || len(verifier.requests) != 3 ||
		verifier.requests[2].RequiredPermission != "tenants.get" || manager.denialAction != "create" ||
		manager.denialApplication != api.IdentityApplicationAdmin || manager.denialScope != (authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-one"}) {
		t.Fatalf("status=%d manager=%#v verifications=%#v", response.Code, manager, verifier.requests)
	}
}

func TestServiceAccountHTTPServerAuditsRealViewerScopeDenial(t *testing.T) {
	manager := &serviceAccountManagerFake{}
	verifier, token := serviceAccountViewerVerifierAndToken(t)
	server, _ := NewServiceAccountHTTPServer(verifier, manager)
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant/service-accounts", strings.NewReader(
		`{"serviceAccountId":"automation-one","displayName":"Automation One","application":"admin","roleName":"project.viewer","scopeLevel":"project","scopeId":"project-one"}`,
	))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "request-viewer-denial")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.createCalls != 0 || manager.denialCalls != 1 ||
		manager.denialScope != (authz.ScopeRef{Level: authz.ScopeProject, ID: "project-one"}) || manager.lastCorrelation != "request-viewer-denial" {
		t.Fatalf("status=%d manager=%#v body=%s", response.Code, manager, response.Body.String())
	}
}

func TestServiceAccountHTTPServerReturnsUnavailableWhenDenialAuditFails(t *testing.T) {
	manager := &serviceAccountManagerFake{err: postgres.ErrServiceAccountAuditUnavailable}
	verifier, token := serviceAccountViewerVerifierAndToken(t)
	server, _ := NewServiceAccountHTTPServer(verifier, manager)
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant/service-accounts", strings.NewReader(
		`{"serviceAccountId":"automation-one","displayName":"Automation One","application":"admin","roleName":"project.viewer","scopeLevel":"project","scopeId":"project-one"}`,
	))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "request-audit-failure")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || manager.createCalls != 0 || manager.denialCalls != 1 ||
		!strings.Contains(response.Body.String(), `"code":"SERVICE_ACCOUNT_AUDIT_UNAVAILABLE"`) ||
		strings.Contains(response.Body.String(), "postgres service account audit") {
		t.Fatalf("status=%d manager=%#v body=%s", response.Code, manager, response.Body.String())
	}
}

func serviceAccountViewerVerifierAndToken(t *testing.T) (*authn.ConfiguredVerifier, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	const issuer = "https://service-account-viewer.test"
	const audience = "https://service-account-viewer.test/control-plane"
	jwk, _ := json.Marshal(map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": "service-account-viewer-key",
		"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), "use": "sig",
	})
	verifier, err := authn.NewConfiguredVerifier(authn.ConfiguredVerifierConfig{
		Issuer: issuer, Audience: audience, Generation: 1, SecurityEpoch: 1,
		NotBefore: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		Keys:  []authn.ConfiguredVerifierKey{{JWK: jwk, Enabled: true, NotBefore: now.Add(-time.Minute).Unix(), NotAfter: now.Add(10 * time.Minute).Unix()}},
		Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Invalidate)
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "service-account-viewer-key", "typ": "at+jwt"})
	claims, _ := json.Marshal(map[string]any{
		"iss": issuer, "sub": "service-viewer", "aud": audience, "exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(),
		"jti": "service-account-viewer-token", "client_id": "cloud-agents-automation", "scope": "projects.get",
		"https://schemas.cloud-agents.dev/claims/subject-kind":   "serviceAccount",
		"https://schemas.cloud-agents.dev/claims/tenant-id":      "tenant",
		"https://schemas.cloud-agents.dev/claims/security-epoch": int64(1),
		"https://schemas.cloud-agents.dev/claims/token-profile":  "cloud-agents-access-token/v1",
	})
	protected := base64.RawURLEncoding.EncodeToString(header)
	payload := base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(protected + "." + payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return verifier, protected + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestServiceAccountHTTPServerListBindsCursorAndTenantPermission(t *testing.T) {
	manager := &serviceAccountManagerFake{page: api.ServiceAccountPage{
		ServiceAccounts: []api.ServiceAccount{serviceAccountFixture()}, NextPageToken: "automation-one",
	}}
	verifier := &serviceAccountVerifierFake{}
	server, _ := NewServiceAccountHTTPServer(verifier, manager)
	request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-one/service-accounts?pageSize=1", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("X-Request-ID", "request-one")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || manager.listCalls != 1 || manager.limit != 1 || manager.after != "" || len(verifier.requests) != 1 ||
		verifier.requests[0] != (authn.VerificationRequest{TenantID: "tenant-one", ResourceLevel: "tenant", ResourceID: "tenant-one", RequiredPermission: "memberships.list"}) {
		t.Fatalf("status=%d manager=%#v requests=%#v body=%s", response.Code, manager, verifier.requests, response.Body.String())
	}
	page, err := api.DecodeServiceAccountPageJSON(response.Body.Bytes())
	if err != nil || page.NextPageToken == "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	after, ok := decodeServiceAccountPageToken("tenant-one", page.NextPageToken)
	if !ok || after != "automation-one" {
		t.Fatalf("after=%q ok=%v", after, ok)
	}

	other := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-other/service-accounts?pageToken="+page.NextPageToken, nil)
	other.Header.Set("Authorization", "Bearer admin-token")
	other.Header.Set("X-Request-ID", "request-two")
	otherResponse := httptest.NewRecorder()
	server.ServeHTTP(otherResponse, other)
	if otherResponse.Code != http.StatusBadRequest || manager.listCalls != 1 {
		t.Fatalf("cross-tenant cursor status=%d calls=%d", otherResponse.Code, manager.listCalls)
	}
}

func TestServiceAccountHTTPServerUsesStoredScopeForRotateAndDisable(t *testing.T) {
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	manager := &serviceAccountManagerFake{
		scope: authz.ScopeRef{Level: authz.ScopeOrganization, ID: "organization-one"},
		rotated: api.ServiceAccountRotated{
			ResourceVersion: "2", CredentialVersion: "2", Credential: credential,
			CredentialExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
		},
		disabledVersion: 3,
	}
	verifier := &serviceAccountVerifierFake{}
	server, _ := NewServiceAccountHTTPServer(verifier, manager)
	for _, test := range []struct {
		action, permission, body string
		wantStatus               int
	}{
		{"rotate-credential", "memberships.update", `{"expectedResourceVersion":"1"}`, http.StatusOK},
		{"disable", "memberships.delete", `{"expectedResourceVersion":"2"}`, http.StatusNoContent},
	} {
		request := httptest.NewRequest(http.MethodPost, "/v1/admin/tenants/tenant-one/service-accounts/automation-one:"+test.action, bytes.NewBufferString(test.body))
		request.Header.Set("Authorization", "Bearer admin-token")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Request-ID", "request-"+test.action)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != test.wantStatus {
			t.Fatalf("%s status=%d body=%s", test.action, response.Code, response.Body.String())
		}
		seen := verifier.requests[len(verifier.requests)-1]
		if seen != (authn.VerificationRequest{TenantID: "tenant-one", ResourceLevel: "organization", ResourceID: "organization-one", RequiredPermission: test.permission}) {
			t.Fatalf("%s verification=%#v", test.action, seen)
		}
	}
	if manager.managementCalls != 2 || manager.rotateCalls != 1 || manager.disableCalls != 1 {
		t.Fatalf("manager=%#v", manager)
	}
}

func TestServiceAccountHTTPServerRejectsUnknownAndAmbiguousRoutes(t *testing.T) {
	server, _ := NewServiceAccountHTTPServer(&serviceAccountVerifierFake{}, &serviceAccountManagerFake{})
	for _, test := range []struct {
		name, target, raw string
		want              int
	}{
		{"unknown action", "/v1/admin/tenants/tenant-one/service-accounts/automation-one:enable", "", http.StatusNotFound},
		{"trailing slash", "/v1/admin/tenants/tenant-one/service-accounts/", "", http.StatusNotFound},
		{"unknown query", "/v1/admin/tenants/tenant-one/service-accounts?scope=tenant", "", http.StatusBadRequest},
		{"ambiguous raw path", "/v1/admin/tenants/tenant-one/service-accounts", "/v1/admin/tenants/tenant-one%2fservice-accounts", http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			request.URL.RawPath = test.raw
			request.Header.Set("Authorization", "Bearer admin-token")
			request.Header.Set("X-Request-ID", "request-one")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func serviceAccountCreatedFixture(t *testing.T) api.ServiceAccountCreated {
	t.Helper()
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	return api.ServiceAccountCreated{
		ServiceAccount:      serviceAccountFixture(),
		Credential:          credential,
		CredentialExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
	}
}

func serviceAccountFixture() api.ServiceAccount {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return api.ServiceAccount{
		ID: "automation-one", TenantID: "tenant-one", DisplayName: "Automation One",
		Application: api.IdentityApplicationAdmin, ScopeLevel: "project", ScopeID: "project-one",
		RoleName: "tenant.admin", State: "active", ResourceVersion: "1",
		Subject:   common.SubjectRef{Kind: "serviceAccount", Issuer: "https://identity.example.test", Subject: "service-automation-one"},
		CreatedAt: now, UpdatedAt: now,
	}
}
