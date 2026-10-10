package openapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	. "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
)

func TestIdentityClientsSeparateBrowserAndServerAuthority(t *testing.T) {
	sessionBody := readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/browser-session.json")
	browser, err := NewBrowserIdentityClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		if request.Method != "GET" || request.Path != "/v1/identity/session" || request.Headers[HeaderRequestID] != "request-alpha" {
			t.Fatalf("browser request = %#v", request)
		}
		if _, present := request.Headers["Authorization"]; present {
			t.Fatal("browser client accepted an Authorization header")
		}
		return Response{Status: 200, Body: sessionBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	session, err := browser.GetBrowserSession(context.Background(), "request-alpha")
	if err != nil || session.Application != IdentityApplicationAdmin || len(session.Tenants) != 1 {
		t.Fatalf("session=%#v err=%v", session, err)
	}

	var seen Request
	service, err := NewIdentityServiceClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		if request.Path == "/v1/identity/login/password" {
			return Response{Status: 200, Headers: map[string]string{"x-cloud-agents-session": "server-session-handle"}, Body: sessionBody}, nil
		}
		if strings.HasPrefix(request.Path, "/v1/identity/me/tenants?") {
			return Response{Status: 200, Body: readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/browser-tenant-page.json")}, nil
		}
		if strings.HasSuffix(request.Path, "/email-policy") {
			return Response{Status: 200, Body: readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/email-suffix-policy.json")}, nil
		}
		return Response{Status: 200, Body: readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/tenant-token.json")}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	login, err := service.PasswordLogin(context.Background(), "request-login", "192.0.2.10", PasswordLoginRequest{Email: "admin@example.com", Password: "secret-password"})
	if err != nil || login.SessionHandle != "server-session-handle" || login.Session.Application != IdentityApplicationAdmin {
		t.Fatalf("login=%#v err=%v", login, err)
	}
	if seen.Headers[HeaderIdentityClientIP] != "192.0.2.10" {
		t.Fatalf("login client IP header = %q", seen.Headers[HeaderIdentityClientIP])
	}
	for _, clientIP := range []string{"192.0.2.10:443", "192.0.2.010", "2001:0db8::1", "fe80::1%eth0", "192.0.2.1, 198.51.100.2"} {
		if _, err := service.PasswordLogin(context.Background(), "request-login", clientIP, PasswordLoginRequest{Email: "admin@example.com", Password: "secret-password"}); err == nil {
			t.Fatalf("noncanonical client IP accepted: %q", clientIP)
		}
	}
	issued, err := service.IssueTenantToken(context.Background(), "session-handle-value", "request-alpha", TenantTokenIssueRequest{TenantID: "tenant-alpha", ProjectID: "project-alpha"})
	if err != nil || issued.TokenType != "Bearer" {
		t.Fatalf("issued=%#v err=%v", issued, err)
	}
	if seen.Path != "/v1/identity/tenant-token" || seen.Headers["X-Cloud-Agents-Session"] != "session-handle-value" || bytes.Contains(seen.Body, []byte("scope")) {
		t.Fatalf("tenant-token request = %#v", seen)
	}
	page, err := service.ListBrowserTenants(context.Background(), "session-handle-value", "request-alpha", 200, "tenant-page-token-2")
	if err != nil || page.NextPageToken != "tenant-page-token-2" || seen.Path != "/v1/identity/me/tenants?pageSize=200&pageToken=tenant-page-token-2" {
		t.Fatalf("tenant page=%#v request=%#v err=%v", page, seen, err)
	}
	if _, err := service.ListBrowserTenants(context.Background(), "session-handle-value", "request-alpha", 201, ""); err == nil {
		t.Fatal("invalid tenant page size accepted")
	}
	policy, err := service.GetEmailSuffixPolicy(context.Background(), "session-handle-value", "tenant-alpha", "request-alpha")
	if err != nil || policy.ResourceVersion != "2" || seen.Path != "/v1/identity/tenants/tenant-alpha/email-policy" {
		t.Fatalf("email policy=%#v request=%#v err=%v", policy, seen, err)
	}
	policy, err = service.UpdateEmailSuffixPolicy(context.Background(), "session-handle-value", "tenant-alpha", "request-alpha", strings.Repeat("a", 43), EmailSuffixPolicyUpdate{ExpectedResourceVersion: "1", AllowedDomains: []string{"example.com", "xn--bcher-kva.example"}})
	if err != nil || policy.ResourceVersion != "2" || seen.Method != "PUT" || seen.Headers["X-CSRF-Token"] != strings.Repeat("a", 43) || bytes.Contains(seen.Body, []byte("tenantId")) {
		t.Fatalf("email policy update=%#v request=%#v err=%v", policy, seen, err)
	}
	if _, err := service.UpdateEmailSuffixPolicy(context.Background(), "session-handle-value", "tenant-alpha", "request-alpha", strings.Repeat("a", 43), EmailSuffixPolicyUpdate{ExpectedResourceVersion: "2", AllowedDomains: []string{"xn--bcher-kva.example", "example.com"}}); err == nil {
		t.Fatal("unsorted email policy domains accepted")
	}
	if _, err := service.CheckTokenStatus(context.Background(), "request-alpha", TokenStatusRequest{TokenSHA256: "header.payload.signature", ExpectedApplication: IdentityApplicationAdmin, ExpectedTenantID: "tenant-alpha"}); err == nil {
		t.Fatal("raw token accepted as token digest")
	}

	authorization, err := NewIdentityAuthorizationClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Body: readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/tenant-token-authorization.json")}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := authorization.AuthorizeTenantToken(context.Background(), "request-authorize", TenantTokenAuthorizationRequest{
		Application: IdentityApplicationAdmin, SessionSHA256: "sha256:" + strings.Repeat("1", 64),
		TenantID: "tenant-alpha", ProjectID: "project-alpha",
	})
	if err != nil || authorized.UserID != "account-alpha" || len(authorized.Scopes) != 2 {
		t.Fatalf("authorized=%#v err=%v", authorized, err)
	}
	if seen.Path != "/v1/identity/authorize-tenant-token" || bytes.Contains(seen.Body, []byte("userId")) || bytes.Contains(seen.Body, []byte("scopes")) {
		t.Fatalf("authorization request = %#v", seen)
	}
	type browserAuthorizer interface {
		AuthorizeTenantToken(context.Context, string, TenantTokenAuthorizationRequest) (TenantTokenAuthorization, error)
	}
	if _, exposed := any(browser).(browserAuthorizer); exposed {
		t.Fatal("browser client exposes server-only tenant-token authorization")
	}
	mismatched, err := NewIdentityAuthorizationClient(TransportFunc(func(_ context.Context, _ Request) (Response, error) {
		body := bytes.Replace(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/tenant-token-authorization.json"), []byte("tenant-alpha"), []byte("tenant-other"), 1)
		return Response{Status: 200, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mismatched.AuthorizeTenantToken(context.Background(), "request-authorize", TenantTokenAuthorizationRequest{
		Application: IdentityApplicationAdmin, SessionSHA256: "sha256:" + strings.Repeat("1", 64),
		TenantID: "tenant-alpha", ProjectID: "project-alpha",
	}); err == nil {
		t.Fatal("authorization client accepted a response for another tenant")
	}
}

func TestIdentityServerCodecsUseContractValidation(t *testing.T) {
	password, err := DecodePasswordLoginRequestJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/password-login-request.json"))
	if err != nil || password.Email != "admin@example.com" || password.Password == "" {
		t.Fatalf("password=%#v err=%v", password, err)
	}
	issue, err := DecodeTenantTokenIssueRequestJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/tenant-token-issue-request.json"))
	if err != nil || issue.TenantID != "tenant-alpha" {
		t.Fatalf("issue=%#v err=%v", issue, err)
	}
	statusRequest, err := DecodeTokenStatusRequestJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/token-status-request.json"))
	if err != nil || statusRequest.ExpectedApplication != IdentityApplicationAdmin {
		t.Fatalf("status request=%#v err=%v", statusRequest, err)
	}
	if _, err := DecodePasswordLoginRequestJSON([]byte(`{"email":"admin@example.com","password":"secret-password","application":"admin"}`)); err == nil {
		t.Fatal("password request accepted caller-selected application")
	}
	if input, err := ValidateListBrowserTenantsServerRequest(200, "tenant-page-token-2"); err != nil || input.PageSize != 200 || input.PageToken != "tenant-page-token-2" {
		t.Fatalf("tenant pagination=%#v err=%v", input, err)
	}
	policy, err := DecodeEmailSuffixPolicyJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/email-suffix-policy.json"))
	if err != nil || policy.TenantID != "tenant-alpha" || len(policy.AllowedDomains) != 2 {
		t.Fatalf("email policy=%#v err=%v", policy, err)
	}
	if _, err := DecodeEmailSuffixPolicyJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/negative/email-suffix-policy-wildcard.json")); err == nil {
		t.Fatal("wildcard email policy domain accepted")
	}

	user, err := DecodeCurrentUserJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/current-user.json"))
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := EncodeCurrentUserJSON(user); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeCurrentUserJSON(encoded); err != nil {
		t.Fatal(err)
	}
	page, err := DecodeBrowserTenantPageJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/browser-tenant-page.json"))
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := EncodeBrowserTenantPageJSON(page); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeBrowserTenantPageJSON(encoded); err != nil {
		t.Fatal(err)
	}
	session, err := DecodeBrowserSessionJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/browser-session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := EncodeBrowserSessionJSON(session); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeBrowserSessionJSON(encoded); err != nil {
		t.Fatal(err)
	}
	session.Tenants = nil
	if _, err := EncodeBrowserSessionJSON(session); err == nil {
		t.Fatal("nil required tenant array encoded as JSON null")
	}
	token, err := DecodeTenantTokenJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/tenant-token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := EncodeTenantTokenJSON(token); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeTenantTokenJSON(encoded); err != nil {
		t.Fatal(err)
	}
	status, err := DecodeTokenStatusJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/token-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := EncodeTokenStatusJSON(status); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeTokenStatusJSON(encoded); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeTokenStatusJSON(TokenStatus{Status: "caller-selected"}); err == nil {
		t.Fatal("invalid token status encoded")
	}
	authorizationRequest, err := DecodeTenantTokenAuthorizationRequestJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/tenant-token-authorization-request.json"))
	if err != nil || authorizationRequest.Application != IdentityApplicationAdmin || authorizationRequest.SessionSHA256 != "sha256:"+strings.Repeat("1", 64) {
		t.Fatalf("authorization request=%#v err=%v", authorizationRequest, err)
	}
	if _, err := DecodeTenantTokenAuthorizationRequestJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/negative/tenant-token-authorization-authority-injection.json")); err == nil {
		t.Fatal("authorization request accepted caller-selected principal or scopes")
	}
	authorization, err := DecodeTenantTokenAuthorizationJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/tenant-token-authorization.json"))
	if err != nil || authorization.UserID != "account-alpha" || len(authorization.Scopes) != 2 {
		t.Fatalf("authorization=%#v err=%v", authorization, err)
	}
	if encoded, err := EncodeTenantTokenAuthorizationJSON(authorization); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeTenantTokenAuthorizationJSON(encoded); err != nil {
		t.Fatal(err)
	}
	authorization.Scopes = []string{"agents.list", "agents.get"}
	if _, err := EncodeTenantTokenAuthorizationJSON(authorization); err == nil {
		t.Fatal("unsorted authorization scopes encoded")
	}
	jwks, err := DecodeIdentityJWKSJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/identity-jwks.json"))
	if err != nil || len(jwks.Keys) != 1 || jwks.CloudAgentsAuthority.Revision != "1" {
		t.Fatalf("jwks=%#v err=%v", jwks, err)
	}
	if encoded, err := EncodeIdentityJWKSJSON(jwks); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeIdentityJWKSJSON(encoded); err != nil {
		t.Fatal(err)
	}
	jwks.Keys = []IdentityJWK{}
	jwks.CloudAgentsAuthority.Lineage[0].Enabled = false
	if _, err := EncodeIdentityJWKSJSON(jwks); err != nil {
		t.Fatalf("revoke-all JWKS rejected: %v", err)
	}
	if _, err := DecodeIdentityJWKSJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/negative/identity-jwks-private-key.json")); err == nil {
		t.Fatal("private key material accepted in JWKS")
	}
}

func TestGeneratedOpenAPIClientUsesFixtureTransportOnly(t *testing.T) {
	requestBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-create-request.json")
	projectBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project.json")
	projectGetResponse := responseFixture(t, "project", 200)
	projectGetResponse.Body = []byte(strings.Replace(string(projectGetResponse.Body), `"name": "project-alpha"`, `"name": "project-roundtrip"`, 1))
	if string(projectGetResponse.Body) == string(projectBody) {
		t.Fatal("project GET fixture must distinguish metadata.uid from metadata.name")
	}
	mutationBody := []byte(`{"resourceUid":"membership-new","resourceVersion":"8","state":"active"}`)
	resumeBody := []byte(`{"resourceUid":"membership-new","resourceVersion":"9","state":"active"}`)
	membershipBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/membership.json")
	membershipPageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"MembershipPage","memberships":[` + string(membershipBody) + `],"nextPageToken":"membership-page-token-2"}`)
	roleBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/role.json")
	rolePageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RolePage","roles":[` + string(roleBody) + `],"nextPageToken":"role-page-token-2"}`)
	roleBindingBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/role-binding.json")
	roleBindingPageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RoleBindingPage","roleBindings":[` + string(roleBindingBody) + `],"nextPageToken":"role-binding-page-token-2"}`)
	responses := map[string]Response{
		"GET /v1/tenants/tenant-alpha":                                                                                      responseFixture(t, "platform-tenant", 200),
		"GET /v1/tenants/tenant-alpha/organizations/organization-alpha":                                                     responseFixture(t, "organization", 200),
		"GET /v1/tenants/tenant-alpha/organizations?pageSize=1&pageToken=organization-page-token-1":                         {Status: 200, Body: readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/organization-page.json")},
		"GET /v1/tenants/tenant-alpha/projects/project-alpha":                                                               projectGetResponse,
		"GET /v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageSize=1&pageToken=project-page-token-1": {Status: 200, Body: readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-page.json")},
		"GET /v1/tenants/tenant-alpha/memberships/membership-alpha":                                                         responseFixture(t, "membership", 200),
		"GET /v1/tenants/tenant-alpha/memberships?pageSize=1&pageToken=membership-page-token-1":                             {Status: 200, Body: membershipPageBody},
		"GET /v1/tenants/tenant-alpha/roles/role-project-viewer-v1":                                                         responseFixture(t, "role", 200),
		"GET /v1/tenants/tenant-alpha/roles?pageSize=1&pageToken=role-page-token-1":                                         {Status: 200, Body: rolePageBody},
		"GET /v1/tenants/tenant-alpha/role-bindings/role-binding-alpha":                                                     responseFixture(t, "role-binding", 200),
		"GET /v1/tenants/tenant-alpha/role-bindings?pageSize=1&pageToken=role-binding-page-token-1":                         {Status: 200, Body: roleBindingPageBody},
		"GET /v1/managed-host/tenants/tenant-alpha/projects/project-alpha":                                                  projectGetResponse,
		"GET /v1/managed-host/tenants/tenant-alpha/role-bindings/role-binding-alpha":                                        responseFixture(t, "role-binding", 200),
		"POST /v1/tenants/tenant-alpha/organizations":                                                                       {Status: 201, Headers: map[string]string{HeaderResourceVersion: "2"}, Body: responseFixture(t, "organization", 200).Body},
		"POST /v1/tenants/tenant-alpha/projects":                                                                            {Status: 201, Headers: map[string]string{HeaderResourceVersion: "3"}, Body: projectBody},
		"POST /v1/tenants/tenant-alpha/memberships":                                                                         {Status: 201, Headers: map[string]string{HeaderResourceVersion: "8"}, Body: mutationBody},
		"POST /v1/tenants/tenant-alpha/memberships/membership-new:resume":                                                   {Status: 200, Headers: map[string]string{HeaderResourceVersion: "9"}, Body: resumeBody},
	}
	var seen []Request
	client, err := NewClient(TransportFunc(func(ctx context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		return responses[request.Method+" "+request.Path], nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.GetPlatformTenant(ctx, "tenant-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetOrganization(ctx, "tenant-alpha", "organization-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListOrganizations(ctx, "tenant-alpha", "req-alpha", 1, "organization-page-token-1"); err != nil || len(page.Value.Organizations) != 1 {
		t.Fatalf("organization page = %#v / %v", page, err)
	}
	if _, err := client.GetProject(ctx, "tenant-alpha", "project-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListProjects(ctx, "tenant-alpha", "organization-alpha", "req-alpha", 1, "project-page-token-1"); err != nil || len(page.Value.Projects) != 1 {
		t.Fatalf("project page = %#v / %v", page, err)
	}
	if _, err := client.GetMembership(ctx, "tenant-alpha", "membership-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListMemberships(ctx, "tenant-alpha", "req-alpha", 1, "membership-page-token-1"); err != nil || len(page.Value.Memberships) != 1 || page.Value.NextPageToken != "membership-page-token-2" {
		t.Fatalf("membership page = %#v / %v", page, err)
	}
	if _, err := client.GetRole(ctx, "tenant-alpha", "role-project-viewer-v1", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListRoles(ctx, "tenant-alpha", "req-alpha", 1, "role-page-token-1"); err != nil || len(page.Value.Roles) != 1 || page.Value.NextPageToken != "role-page-token-2" {
		t.Fatalf("role page = %#v / %v", page, err)
	}
	if _, err := client.GetRoleBinding(ctx, "tenant-alpha", "role-binding-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListRoleBindings(ctx, "tenant-alpha", "req-alpha", 1, "role-binding-page-token-1"); err != nil || len(page.Value.RoleBindings) != 1 || page.Value.NextPageToken != "role-binding-page-token-2" {
		t.Fatalf("role binding page = %#v / %v", page, err)
	}
	if _, err := client.GetProjectContext(ctx, "tenant-alpha", "project-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetManagedHostRoleBinding(ctx, "tenant-alpha", "role-binding-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateOrganization(ctx, "tenant-alpha", "req-alpha", platform.OrganizationCreateRequest{ExpectedTenantRevision: 1, OrganizationID: "organization-alpha", Name: "organization-alpha", DisplayName: "Organization Alpha", AuditFactUID: "audit-organization", ReasonCode: "operator-request"}); err != nil {
		t.Fatal(err)
	}
	body, err := platform.DecodeProjectCreateRequestJSON(requestBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateProject(ctx, "tenant-alpha", "req-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateMembership(ctx, "tenant-alpha", "req-alpha", platform.MembershipCreateRequest{ExpectedTenantRevision: 7, MembershipID: "membership-new", MembershipName: "membership-new", Subject: common.SubjectRef{Kind: "serviceAccount", Issuer: "https://issuer.example", Subject: "service-alpha"}, Scope: common.AuthorizationScope{Level: "tenant", Ref: rawTenantRef("tenant-alpha")}, AuditFactUID: "audit-create", ReasonCode: "operator-request"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResumeMembership(ctx, "tenant-alpha", "membership-new", "req-alpha", platform.MembershipTransitionRequest{ExpectedTenantRevision: 8, ExpectedResourceVersion: 8, AuditFactUID: "audit-resume", ReasonCode: "operator-request"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 17 {
		t.Fatalf("transport calls = %d, want 17", len(seen))
	}
	for _, request := range seen {
		if request.Headers[HeaderRequestID] != "req-alpha" {
			t.Fatalf("request headers = %#v", request.Headers)
		}
	}
	sentBody, sentErr := platform.DecodeProjectCreateRequestJSON(seen[14].Body)
	if seen[14].Headers[HeaderIdempotencyKey] == "" || sentErr != nil || sentBody != body {
		t.Fatalf("create request = %#v", seen[14])
	}
	if string(seen[16].Body) != `{"expectedTenantRevision":8,"expectedResourceVersion":8,"auditFactUid":"audit-resume","reasonCode":"operator-request"}` {
		t.Fatalf("resume request = %#v", seen[16])
	}
}

func rawTenantRef(id string) *json.RawMessage {
	raw := json.RawMessage(`{"namespace":"cloud-agents","kind":"tenant","id":"` + id + `"}`)
	return &raw
}

func TestGeneratedOpenAPIClientUsesAdminManagementRoutes(t *testing.T) {
	responses := map[string]Response{
		"GET /v1/admin/tenants/tenant-alpha": responseFixture(t, "platform-tenant", 200),
		"GET /v1/admin/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageSize=1&pageToken=project-page-token-1": {
			Status: 200, Body: readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-page.json"),
		},
		"POST /v1/admin/tenants/tenant-alpha/memberships": {
			Status: 201, Headers: map[string]string{HeaderResourceVersion: "8"},
			Body: []byte(`{"resourceUid":"membership-new","resourceVersion":"8","state":"active"}`),
		},
	}
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		return responses[request.Method+" "+request.Path], nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.GetAdminPlatformTenant(ctx, "tenant-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAdminProjects(ctx, "tenant-alpha", "organization-alpha", "req-alpha", 1, "project-page-token-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateAdminMembership(ctx, "tenant-alpha", "req-alpha", platform.MembershipCreateRequest{
		ExpectedTenantRevision: 7, MembershipID: "membership-new", MembershipName: "membership-new",
		Subject: common.SubjectRef{Kind: "serviceAccount", Issuer: "https://issuer.example", Subject: "service-alpha"},
		Scope:   common.AuthorizationScope{Level: "tenant", Ref: rawTenantRef("tenant-alpha")}, AuditFactUID: "audit-create", ReasonCode: "operator-request",
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || seen[0].Path != "/v1/admin/tenants/tenant-alpha" || seen[2].Method != "POST" {
		t.Fatalf("admin requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientUsesMyProjectsRoute(t *testing.T) {
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Body: readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-page.json")}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ListMyProjects(context.Background(), "tenant-alpha", "request-alpha", 1, "project-page-token-1")
	if err != nil || len(page.Value.Projects) != 1 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if seen.Method != "GET" || seen.Path != "/v1/tenants/tenant-alpha/my-projects?pageSize=1&pageToken=project-page-token-1" || seen.Headers[HeaderRequestID] != "request-alpha" {
		t.Fatalf("request=%#v", seen)
	}
}

func TestGeneratedOpenAPIClientDecodesExecutionReconcileEvent(t *testing.T) {
	body := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"EventPage","events":[{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Event","metadata":{"uid":"event-reconcile","projectId":"project-alpha","sessionId":"session-alpha","sequence":"1","occurredAt":"2026-09-23T08:00:00Z"},"spec":{"operation":"execution.reconcile","resource":"Execution","generation":2,"mutationDigest":"sha256:` + strings.Repeat("a", 64) + `","executionId":"execution-alpha","changes":[{"resource":"Execution","from":"recovery:awaiting_reconciliation","to":"recovery:none","version":2}]} }],"nextCursor":"","hasMore":false}`)
	page, err := DecodeManagedAgentEventPageResponseJSON(body)
	if err != nil || page.Value.Events[0].Spec.Operation != "execution.reconcile" {
		t.Fatalf("event page = %#v / %v", page, err)
	}
	for name, invalid := range map[string][]byte{
		"wrong resource":      bytes.Replace(body, []byte(`"resource":"Execution"`), []byte(`"resource":"Session"`), 1),
		"missing executionId": bytes.Replace(body, []byte(`,"executionId":"execution-alpha"`), nil, 1),
	} {
		if _, err := DecodeManagedAgentEventPageResponseJSON(invalid); err == nil {
			t.Errorf("%s event decoded", name)
		}
	}
	responseBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"EventPage","events":[],"nextCursor":"","hasMore":false}`)
	client, err := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		return Response{Status: 200, Body: responseBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	list := func() error {
		_, err := client.ListManagedAgentEvents(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "request-events", "", 0)
		return err
	}
	if err := list(); err != nil {
		t.Fatalf("empty event page: %v", err)
	}
	responseBody = body
	if err := list(); err != nil {
		t.Fatalf("matching event page: %v", err)
	}
	for name, mismatch := range map[string][]byte{
		"project": bytes.Replace(body, []byte(`"projectId":"project-alpha"`), []byte(`"projectId":"project-other"`), 1),
		"session": bytes.Replace(body, []byte(`"sessionId":"session-alpha"`), []byte(`"sessionId":"session-other"`), 1),
	} {
		responseBody = mismatch
		if err := list(); err == nil || !strings.Contains(err.Error(), "PATH_BODY_AUTHORITY_MISMATCH") {
			t.Errorf("%s mismatch error = %v", name, err)
		}
	}
}

func TestGeneratedOpenAPIClientUsesAdminDeploymentTargetRoute(t *testing.T) {
	page := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"DeploymentTargetPage","deploymentTargets":[]}`)
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Body: page}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAdminDeploymentTargets(context.Background(), "tenant-alpha", "project-alpha", "req-alpha", 1, "target-page-token-1"); err != nil {
		t.Fatal(err)
	}
	if seen.Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=1&pageToken=target-page-token-1" {
		t.Fatalf("path = %q", seen.Path)
	}
}

func TestGeneratedOpenAPIClientUsesAdminDeploymentTargetActivityRoutes(t *testing.T) {
	operationPage := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"MaintenanceOperationPage","operations":[{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"MaintenanceOperation","operationId":"operation-alpha","idempotencyKey":"operation-key-123~","action":"target.probe","resourceKind":"DeploymentTarget","resourceId":"docker-alpha","resourceGeneration":2,"requestedBy":"sha256:` + strings.Repeat("a", 64) + `","requestId":"request-alpha","requestedAt":"2026-09-03T08:00:00Z","updatedAt":"2026-09-03T08:01:00Z","state":"succeeded","currentStep":"complete","impactSummary":"Probe deployment target connectivity and capabilities","retryable":false}],"nextPageToken":"operation-page-token-2"}`)
	auditPage := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"AdminAuditEventPage","events":[{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"AdminAuditEvent","eventId":"event-alpha","actor":"sha256:` + strings.Repeat("a", 64) + `","action":"target.probe","resourceKind":"DeploymentTarget","resourceId":"docker-alpha","resourceGeneration":2,"result":"succeeded","occurredAt":"2026-09-03T08:01:00Z","requestId":"request-alpha","operationId":"operation-alpha"}],"nextPageToken":"audit-page-token-2"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		if strings.Contains(request.Path, "/audit-events") {
			return Response{Status: 200, Body: auditPage}, nil
		}
		return Response{Status: 200, Body: operationPage}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if page, err := client.ListAdminDeploymentTargetOperations(ctx, "tenant-alpha", "project-alpha", "docker-alpha", "request-alpha", 1, "operation-page-token-1"); err != nil || len(page.Value.Operations) != 1 {
		t.Fatalf("operation page = %#v / %v", page, err)
	}
	if page, err := client.ListAdminMaintenanceOperations(ctx, "tenant-alpha", "project-alpha", "request-alpha", 1, "maintenance-page-token-1"); err != nil || len(page.Value.Operations) != 1 {
		t.Fatalf("maintenance page = %#v / %v", page, err)
	}
	if page, err := client.ListAdminDeploymentTargetAuditEvents(ctx, "tenant-alpha", "project-alpha", "docker-alpha", "request-alpha", 1, "audit-page-token-1"); err != nil || len(page.Value.Events) != 1 {
		t.Fatalf("audit page = %#v / %v", page, err)
	}
	if len(seen) != 3 || seen[0].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha/operations?pageSize=1&pageToken=operation-page-token-1" || seen[1].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/maintenance-operations?pageSize=1&pageToken=maintenance-page-token-1" || seen[2].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha/audit-events?pageSize=1&pageToken=audit-page-token-1" {
		t.Fatalf("requests = %#v", seen)
	}
	regressed := []byte(strings.Replace(string(operationPage), `"updatedAt":"2026-09-03T08:01:00Z"`, `"updatedAt":"2026-09-03T07:59:59Z"`, 1))
	if _, err := platform.DecodeMaintenanceOperationPageJSON(regressed); err == nil {
		t.Fatal("operation page accepted updatedAt before requestedAt")
	}
}

func TestGeneratedOpenAPIClientUsesAdminCleanupPreviewRoute(t *testing.T) {
	body := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"DeploymentTargetCleanupPreview","metadata":{"uid":"docker-alpha","name":"docker-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"7","createdAt":"2026-09-03T08:00:00Z","updatedAt":"2026-09-03T08:01:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"targetKind":"docker","expectedGeneration":2,"expectedResourceVersion":"7","impactDigest":"sha256:` + strings.Repeat("a", 64) + `","canCleanup":true,"workers":[]}}`)
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Headers: map[string]string{HeaderResourceVersion: "7"}, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := client.PreviewAdminDeploymentTargetCleanup(context.Background(), "tenant-alpha", "project-alpha", "docker-alpha", "request-alpha")
	if err != nil || !preview.Value.Spec.CanCleanup {
		t.Fatalf("preview=%#v error=%v", preview, err)
	}
	if seen.Method != "GET" || seen.Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup-preview" {
		t.Fatalf("request=%#v", seen)
	}
}

func TestGeneratedOpenAPIClientUsesAdminCleanupRoute(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	body := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"MaintenanceOperation","operationId":"operation-alpha","idempotencyKey":"cleanup-key-1234~","action":"target.cleanup","resourceKind":"DeploymentTarget","resourceId":"docker-alpha","resourceGeneration":2,"requestedBy":"sha256:` + strings.Repeat("b", 64) + `","requestId":"request-alpha","requestedAt":"2026-09-03T08:00:00Z","updatedAt":"2026-09-03T08:01:00Z","state":"succeeded","currentStep":"complete","impactSummary":"Cleaned 0 orphan workers and 0 resources","retryable":false}`)
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := platform.DeploymentTargetCleanupRequest{ExpectedGeneration: 2, ExpectedResourceVersion: "7", ImpactDigest: digest}
	operation, err := client.CleanupAdminDeploymentTarget(context.Background(), "tenant-alpha", "project-alpha", "docker-alpha", "request-alpha", "cleanup-key-1234~", request)
	if err != nil || operation.Value.Action != "target.cleanup" {
		t.Fatalf("operation=%#v error=%v", operation, err)
	}
	if seen.Method != "POST" || seen.Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup" || seen.Headers[HeaderIdempotencyKey] != "cleanup-key-1234~" {
		t.Fatalf("request=%#v", seen)
	}
}

func TestGeneratedOpenAPIClientUsesAdminEnvironmentLeaseRoutes(t *testing.T) {
	page := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"EnvironmentLeasePage","environmentLeases":[]}`)
	lease := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"CloudEnvironmentLease","metadata":{"uid":"lease-alpha","name":"lease-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-09-03T08:00:00Z","updatedAt":"2026-09-03T08:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"generation":1,"desiredPhase":"active","observedPhase":"provisioning","cleanupPhase":"none","environmentId":"environment-alpha","releaseDigest":"sha256:` + strings.Repeat("a", 64) + `","expiresAt":"2026-09-03T09:00:00Z"}}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		if strings.HasSuffix(request.Path, "/lease-alpha") {
			return Response{Status: 200, Headers: map[string]string{HeaderResourceVersion: "1"}, Body: lease}, nil
		}
		return Response{Status: 200, Body: page}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.ListAdminEnvironmentLeases(ctx, "tenant-alpha", "project-alpha", "req-alpha", 1, "lease-page-token-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetAdminEnvironmentLease(ctx, "tenant-alpha", "project-alpha", "lease-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=1&pageToken=lease-page-token-1" || seen[1].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha" {
		t.Fatalf("requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientListsAdminWorkers(t *testing.T) {
	body := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"WorkerPage","workers":[]}`)
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAdminWorkers(context.Background(), "tenant-alpha", "project-alpha", "request-alpha", 1, "worker-page-token-1"); err != nil {
		t.Fatal(err)
	}
	if seen.Method != "GET" || seen.Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/workers?pageSize=1&pageToken=worker-page-token-1" {
		t.Fatalf("request=%#v", seen)
	}
}

func TestGeneratedOpenAPIClientListsPublishedEnvironmentProfiles(t *testing.T) {
	body := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"EnvironmentProfileSummaryPage","environmentProfiles":[{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"EnvironmentProfileSummary","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"profileId":"development","name":"development","version":1,"description":"Daily coding workspace","status":"published","availability":"available","providerKinds":["codex","claudeAgent"],"cpuLimitMillis":2000,"memoryLimitBytes":4294967296,"storageSummary":"20 GiB managed workspace","networkSummary":"Public internet access"}]}`)
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ListEnvironmentProfiles(context.Background(), "tenant-alpha", "project-alpha", "request-alpha", 1, "profile-page-token-1")
	if err != nil || len(page.Value.EnvironmentProfiles) != 1 || page.Value.EnvironmentProfiles[0].Availability != "available" {
		t.Fatalf("page=%#v error=%v", page, err)
	}
	if seen.Method != "GET" || seen.Path != "/v1/tenants/tenant-alpha/projects/project-alpha/environment-profiles?pageSize=1&pageToken=profile-page-token-1" {
		t.Fatalf("request=%#v", seen)
	}
}

func TestGeneratedOpenAPIClientSplitsProjectLeaseQuotaAuthority(t *testing.T) {
	quotaBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"ProjectLeaseQuota","metadata":{"uid":"quota-project-alpha","name":"project-lease-quota","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-09-05T01:00:00Z","updatedAt":"2026-09-05T01:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"maxConcurrentLeases":2,"maxCpuMillis":4000,"maxMemoryBytes":8589934592,"maxLeaseTtlSeconds":3600},"status":{"activeLeases":1,"usedCpuMillis":2000,"usedMemoryBytes":4294967296}}`)
	summaryBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"ProjectLeaseQuotaSummary","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"maxConcurrentLeases":2,"activeLeases":1,"maxCpuMillis":4000,"usedCpuMillis":2000,"maxMemoryBytes":8589934592,"usedMemoryBytes":4294967296,"maxLeaseTtlSeconds":3600}`)
	auditBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"AdminAuditEventPage","events":[]}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		if strings.Contains(request.Path, "/audit-events") {
			return Response{Status: 200, Body: auditBody}, nil
		}
		if strings.Contains(request.Path, "/admin/") {
			return Response{Status: 200, Headers: map[string]string{HeaderResourceVersion: "1"}, Body: quotaBody}, nil
		}
		return Response{Status: 200, Body: summaryBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.GetAdminProjectLeaseQuota(ctx, "tenant-alpha", "project-alpha", "request-quota-get"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetAdminProjectLeaseQuota(ctx, "tenant-alpha", "project-alpha", "request-quota-set", "quota-set-key-0001", platform.ProjectLeaseQuotaSetRequest{ExpectedResourceVersion: "0", MaxConcurrentLeases: 2, MaxCPUMillis: 4000, MaxMemoryBytes: 8589934592, MaxLeaseTTLSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAdminProjectLeaseQuotaAuditEvents(ctx, "tenant-alpha", "project-alpha", "request-quota-audit", 50, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetProjectLeaseQuota(ctx, "tenant-alpha", "project-alpha", "request-user-quota"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[0].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/lease-quota" || seen[2].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/lease-quota/audit-events?pageSize=50" || seen[3].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/lease-quota" {
		t.Fatalf("requests=%#v", seen)
	}
	if string(seen[1].Body) != `{"expectedResourceVersion":"0","maxConcurrentLeases":2,"maxCpuMillis":4000,"maxMemoryBytes":8589934592,"maxLeaseTtlSeconds":3600}` {
		t.Fatalf("set body=%s", seen[1].Body)
	}
}

func TestGeneratedOpenAPIClientManagesStoragePolicies(t *testing.T) {
	policyBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"StoragePolicy","metadata":{"uid":"storage-standard","name":"storage-standard","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-09-05T03:00:00Z","updatedAt":"2026-09-05T03:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"userSummary":"20 GiB managed workspace","workspaceType":"managed-volume","workspaceCapacityBytes":21474836480,"retentionSeconds":0,"cleanupOnLeaseTermination":true,"allowWorkspaceReuse":true}}`)
	pageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"StoragePolicyPage","storagePolicies":[` + string(policyBody) + `]}`)
	auditBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"AdminAuditEventPage","events":[]}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		if strings.Contains(request.Path, "/audit-events") {
			return Response{Status: 200, Body: auditBody}, nil
		}
		if strings.HasSuffix(request.Path, "/storage-policies?pageSize=1") {
			return Response{Status: 200, Body: pageBody}, nil
		}
		return Response{Status: 200, Headers: map[string]string{HeaderResourceVersion: "1"}, Body: policyBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.ListAdminStoragePolicies(ctx, "tenant-alpha", "project-alpha", "request-storage-list", 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetAdminStoragePolicy(ctx, "tenant-alpha", "project-alpha", "storage-standard", "request-storage-get"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetAdminStoragePolicy(ctx, "tenant-alpha", "project-alpha", "storage-standard", "request-storage-set", "storage-set-key-0001", platform.StoragePolicySetRequest{
		ExpectedResourceVersion: "0", PolicyName: "storage-standard", UserSummary: "20 GiB managed workspace",
		WorkspaceType: "managed-volume", WorkspaceCapacityBytes: 21474836480,
		RetentionSeconds: 0, CleanupOnLeaseTermination: true, AllowWorkspaceReuse: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAdminStoragePolicyAuditEvents(ctx, "tenant-alpha", "project-alpha", "storage-standard", "request-storage-audit", 1, ""); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[2].Method != "PUT" || seen[3].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies/storage-standard/audit-events?pageSize=1" {
		t.Fatalf("requests=%#v", seen)
	}
}

func TestGeneratedOpenAPIClientManagesNetworkPolicies(t *testing.T) {
	policyBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"NetworkPolicy","metadata":{"uid":"network-public","name":"network-public","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-09-05T04:00:00Z","updatedAt":"2026-09-05T04:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"userSummary":"Public internet access","defaultEgress":"public","allowedEgress":[],"ingressEnabled":false,"previewEnabled":false}}`)
	pageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"NetworkPolicyPage","networkPolicies":[` + string(policyBody) + `]}`)
	auditBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"AdminAuditEventPage","events":[]}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		if strings.Contains(request.Path, "/audit-events") {
			return Response{Status: 200, Body: auditBody}, nil
		}
		if strings.HasSuffix(request.Path, "/network-policies?pageSize=1") {
			return Response{Status: 200, Body: pageBody}, nil
		}
		return Response{Status: 200, Headers: map[string]string{HeaderResourceVersion: "1"}, Body: policyBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.ListAdminNetworkPolicies(ctx, "tenant-alpha", "project-alpha", "request-network-list", 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetAdminNetworkPolicy(ctx, "tenant-alpha", "project-alpha", "network-public", "request-network-get"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetAdminNetworkPolicy(ctx, "tenant-alpha", "project-alpha", "network-public", "request-network-set", "network-set-key-0001", platform.NetworkPolicySetRequest{
		ExpectedResourceVersion: "0", PolicyName: "network-public", UserSummary: "Public internet access",
		DefaultEgress: "public", AllowedEgress: []string{}, IngressEnabled: false, PreviewEnabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAdminNetworkPolicyAuditEvents(ctx, "tenant-alpha", "project-alpha", "network-public", "request-network-audit", 1, ""); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[2].Method != "PUT" || seen[3].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/network-policies/network-public/audit-events?pageSize=1" {
		t.Fatalf("requests=%#v", seen)
	}
}

func TestGeneratedOpenAPIClientCreatesGetsAndTerminatesUserEnvironment(t *testing.T) {
	body := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"UserEnvironment","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"environmentId":"environment-alpha","profileId":"development","profileVersion":1,"observedPhase":"provisioning","expiresAt":"2026-09-04T12:00:00Z"}`)
	terminatedBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"UserEnvironment","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"environmentId":"environment-alpha","profileId":"development","profileVersion":1,"observedPhase":"terminated","expiresAt":"2026-09-04T12:00:00Z"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		status, responseBody := 200, body
		if request.Method == "POST" && !strings.HasSuffix(request.Path, ":terminate") {
			status = 201
		}
		if strings.HasSuffix(request.Path, ":terminate") {
			responseBody = terminatedBody
		}
		return Response{Status: status, Body: responseBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateEnvironment(context.Background(), "tenant-alpha", "project-alpha", "request-create", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R9", platform.UserEnvironmentCreateRequest{ProfileID: "development", ProfileVersion: 1})
	if err != nil || created.Value.EnvironmentID != "environment-alpha" {
		t.Fatalf("create = %#v / %v", created, err)
	}
	got, err := client.GetEnvironment(context.Background(), "tenant-alpha", "project-alpha", "environment-alpha", "request-get")
	if err != nil || got.Value.ProfileID != "development" {
		t.Fatalf("get = %#v / %v", got, err)
	}
	terminated, err := client.TerminateEnvironment(context.Background(), "tenant-alpha", "project-alpha", "environment-alpha", "request-terminate", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R9", platform.UserEnvironmentTerminateRequest{ExpectedGeneration: 1})
	if err != nil || terminated.Value.ObservedPhase != "terminated" {
		t.Fatalf("terminate = %#v / %v", terminated, err)
	}
	if len(seen) != 3 || seen[0].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/environments" || string(seen[0].Body) != `{"profileId":"development","profileVersion":1}` || seen[1].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/environments/environment-alpha" || seen[2].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/environments/environment-alpha:terminate" || string(seen[2].Body) != `{"expectedGeneration":1}` {
		t.Fatalf("requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientFoundationRuntimeProfileAndSandbox(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	profile := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RuntimeProfile","metadata":{"uid":"rp-0123456789abcdef0123456789abcdef","name":"foundation","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-09-05T03:00:00Z","updatedAt":"2026-09-05T03:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"profileId":"foundation","version":1,"description":"Retained no-agent workspace","status":"draft","workloadTrust":"trusted-single-tenant","isolationRuntime":"runc","targetId":"docker-primary","networkPolicyRef":"network-deny","imageUri":"registry.example.test/runtime@` + digest + `","releaseDigest":"` + digest + `","cpuMillis":500,"memoryBytes":536870912}}`)
	adminPage := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RuntimeProfilePage","runtimeProfiles":[` + string(profile) + `]}`)
	publicPage := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RuntimeProfileSummaryPage","runtimeProfiles":[{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RuntimeProfileSummary","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"profileId":"foundation","name":"foundation","version":1,"description":"Retained no-agent workspace","status":"published","availability":"available","cpuMillis":500,"memoryBytes":536870912,"workspaceRetention":"retained"}]}`)
	sandbox := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"SandboxSession","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"operationId":"operation-sandbox","workspaceId":"workspace","sandboxId":"sandbox","runtimeProfileId":"foundation","runtimeProfileVersion":1,"generation":1,"desiredState":"running","observedState":"pending","expiresAt":"2026-09-05T03:01:00Z"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		switch {
		case request.Method == "POST" && strings.Contains(request.Path, "/admin/"):
			return Response{Status: 201, Headers: map[string]string{HeaderResourceVersion: "1"}, Body: profile}, nil
		case request.Method == "GET" && strings.Contains(request.Path, "/admin/"):
			return Response{Status: 200, Body: adminPage}, nil
		case request.Method == "GET":
			return Response{Status: 200, Body: publicPage}, nil
		default:
			return Response{Status: 202, Body: sandbox}, nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	body := platform.RuntimeProfileCreateRequest{
		ProfileID: "foundation", ProfileName: "foundation", Version: 1,
		Description: "Retained no-agent workspace", TargetID: "docker-primary",
		WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc", NetworkPolicyRef: "network-deny",
		ImageURI: "registry.example.test/runtime@" + digest, ReleaseDigest: digest,
		CPUMillis: 500, MemoryBytes: 536870912,
	}
	if _, err := client.CreateAdminRuntimeProfile(context.Background(), "tenant-alpha", "project-alpha", "request-runtime-create", "runtime-create-key", body); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListAdminRuntimeProfiles(context.Background(), "tenant-alpha", "project-alpha", "request-runtime-admin-list", 0, ""); err != nil || len(page.Value.RuntimeProfiles) != 1 {
		t.Fatalf("Admin RuntimeProfiles=%#v error=%v", page.Value, err)
	}
	if page, err := client.ListRuntimeProfiles(context.Background(), "tenant-alpha", "project-alpha", "request-runtime-public-list", 0, ""); err != nil || len(page.Value.RuntimeProfiles) != 1 || page.Value.RuntimeProfiles[0].WorkspaceRetention != "retained" {
		t.Fatalf("public RuntimeProfiles=%#v error=%v", page.Value, err)
	}
	request := platform.SandboxSessionCreateRequest{WorkspaceID: "workspace", WorkspaceName: "workspace", SandboxID: "sandbox", RuntimeProfileID: "foundation", RuntimeProfileVersion: 1, TTLSeconds: 60}
	if result, err := client.CreateSandbox(context.Background(), "tenant-alpha", "project-alpha", "request-sandbox-create", "sandbox-create-key", request); err != nil || result.Value.OperationID != "operation-sandbox" {
		t.Fatalf("Sandbox=%#v error=%v", result.Value, err)
	}
	if len(seen) != 4 || seen[0].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/runtime-profiles" || seen[1].Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/runtime-profiles?pageSize=50" || seen[2].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/runtime-profiles?pageSize=50" || seen[3].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sandbox-sessions" {
		t.Fatalf("foundation requests=%#v", seen)
	}
}

func TestGeneratedOpenAPIClientManagedAgentSessionLifecycle(t *testing.T) {
	sessionBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Session","metadata":{"uid":"session-alpha","projectId":"project-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"providerKind":"codex","workspaceId":"workspace-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":7,"environmentProfileId":"profile-alpha","environmentProfileVersion":3,"state":"active"}}`)
	sessionPageBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"SessionPage","sessions":[{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Session","metadata":{"uid":"session-alpha","projectId":"project-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"providerKind":"codex","workspaceId":"workspace-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":7,"environmentProfileId":"profile-alpha","environmentProfileVersion":3,"state":"active"}}],"nextPageToken":"session-page-token-1"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		body := sessionBody
		if request.Method == "GET" && strings.Contains(request.Path, "/sessions?") {
			body = sessionPageBody
		}
		return Response{Status: map[string]int{"POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions": 201, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha": 200, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions?pageSize=1&pageToken=session-page-token-1&sandboxId=sandbox-alpha": 200, "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha:close": 200}[request.Method+" "+request.Path], Headers: map[string]string{HeaderResourceVersion: "2"}, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := client.CreateManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", ManagedAgentSessionCreateRequest{SessionID: "session-alpha", ProviderKind: "codex", EnvironmentLeaseID: "lease-alpha"})
	if err != nil || created.Value.Spec.ProviderKind != "codex" {
		t.Fatalf("create = %#v / %v", created, err)
	}
	if _, err := client.CreateManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R4", ManagedAgentSessionCreateRequest{SessionID: "session-alpha", ProviderKind: "codex", WorkspaceID: "workspace-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 7, EnvironmentProfileID: "profile-alpha", EnvironmentProfileVersion: 3}); err != nil {
		t.Fatal(err)
	}
	if session, err := client.GetManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha"); err != nil || session.Value.Spec.WorkspaceID != "workspace-alpha" || session.Value.Spec.SandboxGeneration != 7 {
		t.Fatalf("session = %#v / %v", session, err)
	}
	if page, err := client.ListManagedAgentSessions(ctx, "tenant-alpha", "project-alpha", "request-alpha", "sandbox-alpha", 1, "session-page-token-1"); err != nil || len(page.Value.Sessions) != 1 || page.Value.NextPageToken == "" {
		t.Fatalf("session page = %#v / %v", page, err)
	}
	if _, err := client.CloseManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R3"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 5 || string(seen[0].Body) != `{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"lease-alpha"}` || string(seen[1].Body) != `{"sessionId":"session-alpha","providerKind":"codex","workspaceId":"workspace-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":7,"environmentProfileId":"profile-alpha","environmentProfileVersion":3}` || seen[3].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions?pageSize=1&pageToken=session-page-token-1&sandboxId=sandbox-alpha" || seen[4].Body != nil {
		t.Fatalf("session requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientManagedAgentTurnLifecycle(t *testing.T) {
	turnBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Turn","metadata":{"uid":"turn-alpha","projectId":"project-alpha","sessionId":"session-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"inputDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"queued"}}`)
	turnPageBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"TurnPage","turns":[{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Turn","metadata":{"uid":"turn-alpha","projectId":"project-alpha","sessionId":"session-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"inputDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"queued"}}],"nextPageToken":"turn-page-token-1"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		body := turnBody
		if request.Method == "GET" && strings.Contains(request.Path, "/turns?") {
			body = turnPageBody
		}
		return Response{Status: map[string]int{"POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns": 201, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha": 200, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns?pageSize=1&pageToken=turn-page-token-1": 200}[request.Method+" "+request.Path], Headers: map[string]string{HeaderResourceVersion: "2"}, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.CreateManagedAgentTurn(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R4", ManagedAgentTurnCreateRequest{TurnID: "turn-alpha", InputText: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetManagedAgentTurn(ctx, "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "request-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListManagedAgentTurns(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", 1, "turn-page-token-1"); err != nil || len(page.Value.Turns) != 1 || page.Value.NextPageToken == "" {
		t.Fatalf("turn page = %#v / %v", page, err)
	}
	if len(seen) != 3 || seen[2].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns?pageSize=1&pageToken=turn-page-token-1" {
		t.Fatalf("turn requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientManagedAgentExecutionLifecycle(t *testing.T) {
	executionBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Execution","metadata":{"uid":"execution-alpha","projectId":"project-alpha","sessionId":"session-alpha","turnId":"turn-alpha","resourceVersion":"3","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"generation":1,"state":"succeeded","attemptNumber":2,"recoveryState":"recovered","recoveryMode":"cross-node-takeover","recoverySourceTargetId":"docker-source","recoveryTargetId":"docker-target","resultDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"messages":[{"requestId":"request-alpha","protocolVersion":{"major":2,"minor":3},"executionId":"execution-alpha","generation":1,"commandId":"command-alpha","occurredAt":"2026-08-29T08:01:00Z","messageType":"Result","payload":{"text":"done"}}]}`)
	executionPageBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"ExecutionPage","executions":[{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Execution","metadata":{"uid":"execution-alpha","projectId":"project-alpha","sessionId":"session-alpha","turnId":"turn-alpha","resourceVersion":"3","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"generation":1,"state":"succeeded","attemptNumber":2,"recoveryState":"recovered","recoveryMode":"cross-node-takeover","recoverySourceTargetId":"docker-source","recoveryTargetId":"docker-target","resultDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}],"nextPageToken":"execution-page-token-2"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		if strings.HasSuffix(request.Path, "/messages/0/artifact") {
			return Response{Status: 200, Headers: map[string]string{"Content-Type": "text/plain", "Content-Disposition": `attachment; filename="result.txt"`, "Etag": `"sha256:artifact"`}, Body: []byte("artifact bytes")}, nil
		}
		body := executionBody
		if request.Method == "GET" && strings.Contains(request.Path, "/executions?") {
			body = executionPageBody
		}
		status := 200
		if strings.HasSuffix(request.Path, ":reconcile") || strings.HasSuffix(request.Path, ":resolveApproval") || strings.HasSuffix(request.Path, ":resolveUserInput") {
			status = 204
		}
		return Response{Status: status, Headers: map[string]string{HeaderResourceVersion: "3"}, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	invalidPlacement := bytes.Replace(executionBody, []byte(`"docker-target"`), []byte(`"docker-source"`), 1)
	if _, err := DecodeManagedAgentExecutionResponseJSON(invalidPlacement); err == nil {
		t.Fatal("cross-node recovery accepted identical source and target")
	}
	result, err := client.ExecuteManagedAgent(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", ManagedAgentExecutionCreateRequest{TurnID: "turn-alpha", ExecutionID: "execution-alpha", Model: "codex", RuntimeMode: "approval-required", InteractionMode: "plan", InputText: "hello"})
	if err != nil || len(result.Value.Messages) != 1 || result.Value.Messages[0].MessageType != "Result" {
		t.Fatalf("execute = %#v / %v", result, err)
	}
	if result.Value.Spec.RecoveryMode != "cross-node-takeover" {
		t.Fatalf("recovery mode = %q", result.Value.Spec.RecoveryMode)
	}
	if _, err := client.ExecuteManagedAgent(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", ManagedAgentExecutionCreateRequest{TurnID: "turn-alpha", ExecutionID: "execution-alpha", RuntimeMode: "always-allow", InputText: "hello"}); err == nil {
		t.Fatal("execution request accepted an invalid runtime mode")
	}
	if _, err := client.ExecuteManagedAgent(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", ManagedAgentExecutionCreateRequest{TurnID: "turn-alpha", ExecutionID: "execution-alpha", InteractionMode: "chat", InputText: "hello"}); err == nil {
		t.Fatal("execution request accepted an invalid interaction mode")
	}
	if page, err := client.ListManagedAgentExecutions(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "request-list", 1, "execution-page-token-1"); err != nil || len(page.Value.Executions) != 1 || page.Value.NextPageToken == "" || page.Value.Executions[0].Messages != nil {
		t.Fatalf("execution page = %#v / %v", page, err)
	}
	if _, err := client.GetManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-alpha"); err != nil {
		t.Fatal(err)
	}
	artifact, err := client.DownloadManagedAgentArtifact(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-artifact", 0)
	if err != nil || string(artifact.Data) != "artifact bytes" || artifact.FileName != "result.txt" || artifact.ContentType != "text/plain" {
		t.Fatalf("artifact = %#v / %v", artifact, err)
	}
	if _, err := client.DownloadManagedAgentArtifact(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-artifact", 128); err == nil {
		t.Fatal("out-of-range message index was accepted")
	}
	if _, err := client.CancelManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-cancel", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX", ManagedAgentExecutionCancelRequest{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.InterruptManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-interrupt", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EY", ManagedAgentExecutionInterruptRequest{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if err := client.ReconcileManagedAgentSideEffect(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-reconcile", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EZ", ManagedAgentSideEffectReconciliationRequest{Generation: 1, CheckpointDigest: "sha256:" + strings.Repeat("b", 64), Outcome: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveManagedAgentApproval(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-approval", ManagedAgentApprovalResolutionRequest{Generation: 1, RequestID: "codex:generation-1:approval:1", Decision: "accept"}); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveManagedAgentUserInput(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-user-input", ManagedAgentUserInputResolutionRequest{Generation: 1, RequestID: "claude:generation-1:user-input:2", Answers: map[string][]string{"question-1": {"one", "two"}}}); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveManagedAgentUserInput(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-user-input-invalid", ManagedAgentUserInputResolutionRequest{Generation: 1, RequestID: "claude:generation-1:user-input:2", Answers: map[string][]string{"question-1": {"bad\x00answer"}}}); err == nil {
		t.Fatal("user-input resolution accepted a NUL answer")
	}
	if len(seen) != 9 || seen[0].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions" || seen[1].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions?pageSize=1&pageToken=execution-page-token-1" || seen[2].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha" || seen[3].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha/messages/0/artifact" || seen[4].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:cancel" || seen[5].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:interrupt" || seen[6].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:reconcile" || seen[7].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveApproval" || seen[8].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveUserInput" || string(seen[0].Body) != `{"turnId":"turn-alpha","executionId":"execution-alpha","model":"codex","runtimeMode":"approval-required","interactionMode":"plan","inputText":"hello"}` || string(seen[4].Body) != `{"generation":1}` || string(seen[5].Body) != `{"generation":1}` || string(seen[6].Body) != `{"generation":1,"checkpointDigest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","outcome":"confirmed"}` || string(seen[7].Body) != `{"generation":1,"requestId":"codex:generation-1:approval:1","decision":"accept"}` || string(seen[8].Body) != `{"generation":1,"requestId":"claude:generation-1:user-input:2","answers":{"question-1":["one","two"]}}` {
		t.Fatalf("execution requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientRejectsMissingArtifactDisposition(t *testing.T) {
	client, err := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		return Response{Status: 200, Headers: map[string]string{"Content-Type": "text/plain"}, Body: []byte("artifact")}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.DownloadManagedAgentArtifact(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-artifact", 0); err == nil || !strings.Contains(err.Error(), "Content-Disposition is invalid") {
		t.Fatalf("missing Content-Disposition error=%v", err)
	}
}

func TestGeneratedOpenAPIClientErrorAndCancellationBoundaries(t *testing.T) {
	problem := readOpenAPIFixture(t, "common/v1alpha1/fixtures/golden/problem.json")
	transportCalls := 0
	client, err := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		transportCalls++
		return Response{Status: 404, Body: problem}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetProject(context.Background(), "tenant-alpha", "project-alpha", "req-alpha")
	var clientErr *ClientError
	if !errors.As(err, &clientErr) || clientErr.Problem == nil || clientErr.Problem.Status != 404 {
		t.Fatalf("problem error = %#v", err)
	}
	statusMismatchClient, _ := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		return Response{Status: 500, Body: problem}, nil
	}))
	_, err = statusMismatchClient.GetProject(context.Background(), "tenant-alpha", "project-alpha", "req-alpha")
	if !errors.As(err, &clientErr) || clientErr.Cause == nil || !strings.Contains(clientErr.Cause.Error(), "PROBLEM_STATUS_MISMATCH") {
		t.Fatalf("status mismatch = %#v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.GetProject(cancelled, "tenant-alpha", "project-alpha", "req-alpha")
	if !errors.Is(err, context.Canceled) || transportCalls != 1 {
		t.Fatalf("cancel = %v, transport calls = %d", err, transportCalls)
	}
	expired, expiredCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expiredCancel()
	_, err = client.GetProject(expired, "tenant-alpha", "project-alpha", "req-alpha")
	if !errors.Is(err, context.DeadlineExceeded) || transportCalls != 1 {
		t.Fatalf("deadline = %v, transport calls = %d", err, transportCalls)
	}
	if _, err := client.GetProject(context.Background(), "tenant-alpha", "wrong/id", "req-alpha"); err == nil {
		t.Fatal("invalid path identifier accepted")
	}
	if transportCalls != 1 {
		t.Fatalf("invalid path identifier reached transport: calls = %d", transportCalls)
	}
	authorityBody := []byte(strings.Replace(string(readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project.json")), `"uid": "project-alpha"`, `"uid": "project-other"`, 1))
	authorityClient, _ := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		return Response{Status: 200, Headers: map[string]string{HeaderResourceVersion: "3"}, Body: authorityBody}, nil
	}))
	if _, err := authorityClient.GetProject(context.Background(), "tenant-alpha", "project-alpha", "req-alpha"); err == nil || !strings.Contains(err.Error(), "PATH_BODY_AUTHORITY_MISMATCH") {
		t.Fatalf("resource UID authority mismatch = %#v", err)
	}
}

func TestGeneratedOpenAPIServerValidationSeam(t *testing.T) {
	organization, err := ValidateCreateOrganizationServerRequest("tenant-alpha", "req-alpha", []byte(`{"expectedTenantRevision":4,"organizationId":"organization-beta","name":"organization-beta","displayName":"Organization Beta","auditFactUid":"audit-organization-beta","reasonCode":"operator-request"}`))
	if err != nil || organization.Body.OrganizationID != "organization-beta" {
		t.Fatalf("organization server input = %#v / %v", organization, err)
	}
	page, err := ValidateListOrganizationsServerRequest("tenant-alpha", "req-alpha", 50, "organization-page-token-1")
	if err != nil || page.PageSize != 50 || page.PageToken == "" {
		t.Fatalf("organization list input = %#v / %v", page, err)
	}
	roles, err := ValidateListRolesServerRequest("tenant-alpha", "req-alpha", 50, "role-page-token-1")
	if err != nil || roles.PageSize != 50 || roles.PageToken == "" {
		t.Fatalf("role list input = %#v / %v", roles, err)
	}
	memberships, err := ValidateListMembershipsServerRequest("tenant-alpha", "req-alpha", 50, "membership-page-token-1")
	if err != nil || memberships.PageSize != 50 || memberships.PageToken == "" {
		t.Fatalf("membership list input = %#v / %v", memberships, err)
	}
	roleBindings, err := ValidateListRoleBindingsServerRequest("tenant-alpha", "req-alpha", 50, "role-binding-page-token-1")
	if err != nil || roleBindings.PageSize != 50 || roleBindings.PageToken == "" {
		t.Fatalf("role binding list input = %#v / %v", roleBindings, err)
	}
	projects, err := ValidateListProjectsServerRequest("tenant-alpha", "organization-alpha", "req-alpha", 50, "project-page-token-1")
	if err != nil || projects.OrganizationID != "organization-alpha" || projects.PageSize != 50 || projects.PageToken == "" {
		t.Fatalf("project list input = %#v / %v", projects, err)
	}
	myProjects, err := ValidateListMyProjectsServerRequest("tenant-alpha", "req-alpha", 50, "project-page-token-1")
	if err != nil || myProjects.TenantID != "tenant-alpha" || myProjects.PageSize != 50 || myProjects.PageToken == "" {
		t.Fatalf("my projects input = %#v / %v", myProjects, err)
	}
	sessions, err := ValidateListManagedAgentSessionsServerRequest("tenant-alpha", "project-alpha", "req-alpha", "sandbox-alpha", 50, "session-page-token-1")
	if err != nil || sessions.ProjectID != "project-alpha" || sessions.SandboxID != "sandbox-alpha" || sessions.PageSize != 50 || sessions.PageToken == "" {
		t.Fatalf("session list input = %#v / %v", sessions, err)
	}
	turns, err := ValidateListManagedAgentTurnsServerRequest("tenant-alpha", "project-alpha", "session-alpha", "req-alpha", 50, "turn-page-token-1")
	if err != nil || turns.SessionID != "session-alpha" || turns.PageSize != 50 || turns.PageToken == "" {
		t.Fatalf("turn list input = %#v / %v", turns, err)
	}
	executions, err := ValidateListManagedAgentExecutionsServerRequest("tenant-alpha", "project-alpha", "session-alpha", "req-alpha", 50, "execution-page-token-1")
	if err != nil || executions.SessionID != "session-alpha" || executions.PageSize != 50 || executions.PageToken == "" {
		t.Fatalf("execution list input = %#v / %v", executions, err)
	}
	body := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-create-request.json")
	input, err := ValidateCreateProjectServerRequest("tenant-alpha", "req-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body)
	if err != nil || input.Body.Name != "project-alpha" {
		t.Fatalf("server input = %#v / %v", input, err)
	}
	if _, err := ValidateCreateProjectServerRequest("tenant-alpha", "req-alpha", "short", body); err == nil {
		t.Fatal("short idempotency key accepted")
	}
	unicode := []byte(`{"name":"project-alpha","organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-café"},"displayName":"Project Alpha"}`)
	if _, err := ValidateCreateProjectServerRequest("tenant-alpha", "req-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", unicode); err == nil {
		t.Fatal("Unicode organization identifier accepted by server seam")
	}
	if _, err := ValidateGetServerRequest("tenant-alpha", "project-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGetServerRequest("tenant-alpha", "project/alpha", "req-alpha"); err == nil {
		t.Fatal("path separator accepted")
	}
}

func TestRemoteWorkerHeartbeatUsesOnlyMTLSTransportAuthority(t *testing.T) {
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Headers: map[string]string{"Cache-Control": "no-store"}, Body: []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RemoteWorkerHeartbeat","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"enrollmentId":"enrollment-alpha","workerId":"worker-alpha","incarnationId":"incarnation-alpha","generation":2,"observedGeneration":1,"desiredState":"drained","observedState":"active","healthState":"online","acceptedAt":"2026-09-06T12:00:00Z","expiresAt":"2026-09-06T12:00:30Z","nextHeartbeatAfterSeconds":5,"reconcileRequired":true,"command":{"commandId":"command-alpha","generation":2,"desiredState":"drained","deadline":"2026-09-06T12:00:30Z"}}`)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := platform.RemoteWorkerHeartbeatRequest{
		IncarnationID: "incarnation-alpha", ObservedGeneration: 1, ObservedState: "active",
		WorkerVersion: "v0.1.0", OS: "linux", Architecture: "arm64", KernelVersion: "6.12.1",
		Capabilities: []string{"docker", "exec", "files"},
		Capacity:     platform.RemoteWorkerCapacity{CPUMillis: 4000, MemoryBytes: 8 << 30, DiskBytes: 40 << 30},
	}
	result, err := client.HeartbeatRemoteWorker(context.Background(), "tenant-alpha", "project-alpha", "enrollment-alpha", "request-heartbeat", request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.Command == nil || result.Value.Command.CommandID != "command-alpha" || result.Value.Command.DesiredState != "drained" {
		t.Fatalf("command=%#v", result.Value.Command)
	}
	var body map[string]any
	if json.Unmarshal(seen.Body, &body) != nil || body["observedState"] != "active" || seen.Headers[HeaderRequestID] != "request-heartbeat" || seen.Headers["Authorization"] != "" {
		t.Fatalf("request=%#v body=%#v", seen, body)
	}
}

func TestAdminWorkerHealthClientAuthority(t *testing.T) {
	body := `{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"WorkerHealthObservation","tenantId":"tenant-alpha","projectId":"project-alpha","workerId":"lease-alpha","generation":2,"resourceVersion":"3","state":"serving","checkedAt":"2026-09-05T12:00:00Z"}`
	calls := 0
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		calls++
		if request.Method != "GET" || request.Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/workers/lease-alpha/health?expectedGeneration=2" {
			t.Fatalf("request=%#v", request)
		}
		return Response{Status: 200, Body: []byte(body)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.GetAdminWorkerHealth(context.Background(), "tenant-alpha", "project-alpha", "lease-alpha", "request-worker-health", 2)
	if err != nil || value.Value.State != "serving" {
		t.Fatalf("value=%#v err=%v", value, err)
	}
	body = strings.Replace(body, "tenant-alpha", "tenant-other", 1)
	if _, err := client.GetAdminWorkerHealth(context.Background(), "tenant-alpha", "project-alpha", "lease-alpha", "request-worker-health", 2); err == nil {
		t.Fatal("accepted cross-tenant observation")
	}
	for _, generation := range []int64{0, 9007199254740992} {
		if _, err := client.GetAdminWorkerHealth(context.Background(), "tenant-alpha", "project-alpha", "lease-alpha", "request-worker-health", generation); err == nil {
			t.Fatal("accepted invalid generation")
		}
	}
	if _, err := client.GetAdminWorkerHealth(context.Background(), "tenant-alpha", "project-alpha", "", "request-worker-health", 2); err == nil {
		t.Fatal("accepted empty worker ID")
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func responseFixture(t *testing.T, name string, status int) Response {
	t.Helper()
	return Response{
		Status:  status,
		Headers: map[string]string{HeaderResourceVersion: fixtureResourceVersion(t, name)},
		Body:    readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/"+name+".json"),
	}
}

func fixtureResourceVersion(t *testing.T, name string) string {
	t.Helper()
	var value struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/"+name+".json"), &value); err != nil {
		t.Fatal(err)
	}
	return value.Metadata.ResourceVersion
}

func readOpenAPIFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "contracts", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestInvitationClientsKeepProofAndSessionBoundaries(t *testing.T) {
	created := readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/invitation-created.json")
	page := readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/invitation-page.json")
	var seen Request
	client, err := NewIdentityServiceClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		switch request.Method {
		case "GET":
			return Response{Status: 200, Body: page}, nil
		case "DELETE":
			return Response{Status: 204}, nil
		default:
			if request.Path == "/v1/identity/invitations/accept" {
				return Response{Status: 204}, nil
			}
			return Response{Status: 201, Body: created}, nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeInvitationCreateRequestJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/golden/invitation-create-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	proof := strings.Repeat("A", 43)
	result, err := client.CreateInvitation(context.Background(), proof, "tenant-one", "invite-create", proof, input)
	if err != nil || result.Invitation.ID != "invite-one" || result.InvitationCode != proof || seen.Headers["X-Cloud-Agents-Session"] != proof || seen.Headers["X-CSRF-Token"] != proof {
		t.Fatal("create invitation boundary failed", err)
	}
	listed, err := client.ListInvitations(context.Background(), proof, "tenant-one", "invite-list", 20, "")
	if err != nil || len(listed.Invitations) != 1 || !strings.Contains(seen.Path, "pageSize=20") {
		t.Fatal("list invitation boundary failed", err)
	}
	if err := client.RevokeInvitation(context.Background(), proof, "tenant-one", "invite-one", "invite-revoke", proof); err != nil || seen.Method != "DELETE" {
		t.Fatal("revoke invitation boundary failed", err)
	}
	if err := client.AcceptInvitation(context.Background(), "", "invite-accept", "192.0.2.40", "", InvitationAcceptRequest{InvitationCode: proof, Password: "a sufficiently long password", DisplayName: "Invited User"}); err != nil {
		t.Fatal(err)
	}
	if _, present := seen.Headers["X-Cloud-Agents-Session"]; present {
		t.Fatal("anonymous acceptance sent a session handle")
	}
	if seen.Headers["X-Cloud-Agents-Client-IP"] != "192.0.2.40" {
		t.Fatal("anonymous acceptance omitted its trusted client IP")
	}
	if err := client.AcceptInvitation(context.Background(), proof, "invite-existing", "192.0.2.41", proof, InvitationAcceptRequest{InvitationCode: proof}); err != nil || seen.Headers["X-Cloud-Agents-Session"] != proof || seen.Headers["X-CSRF-Token"] != proof || seen.Headers["X-Cloud-Agents-Client-IP"] != "192.0.2.41" {
		t.Fatal("existing-account acceptance omitted its session/CSRF", err)
	}
	if _, err := DecodeInvitationJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/negative/invitation-proof-exposure.json")); err == nil {
		t.Fatal("invitation read exposed proof")
	}
	if _, err := DecodeInvitationCreateRequestJSON(readOpenAPIFixture(t, "identity/v1alpha1/fixtures/negative/invitation-subject-injection.json")); err == nil {
		t.Fatal("invitation allowed a supplied account subject")
	}
	for _, body := range []string{`{"invitationCode":"` + proof + `","password":null}`, `{"invitationCode":"` + proof + `","displayName":""}`} {
		if _, err := DecodeInvitationAcceptRequestJSON([]byte(body)); err == nil {
			t.Fatal("malformed acceptance fields were allowed")
		}
	}
}

func TestReauthenticationClientsRequireRotatedSessionAndPrivateProof(t *testing.T) {
	proof := strings.Repeat("A", 43)
	rotated := strings.Repeat("B", 43)
	omitSession := false
	client, err := NewIdentityServiceClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		headers := map[string]string{"X-Cloud-Agents-Reauthentication": proof}
		if !omitSession {
			headers["X-Cloud-Agents-Session"] = rotated
		}
		fixture := "identity/v1alpha1/fixtures/golden/reauthentication.json"
		if strings.HasSuffix(request.Path, "/callback") {
			fixture = "identity/v1alpha1/fixtures/golden/provider-callback.json"
		}
		return Response{Status: 200, Headers: headers, Body: readOpenAPIFixture(t, fixture)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	password, err := client.PasswordReauthenticate(context.Background(), proof, "password-reauth", proof, PasswordReauthRequest{Password: "a sufficiently long password"})
	if err != nil || password.ReauthProof != proof || password.SessionHandle != rotated {
		t.Fatalf("password reauth = %#v, %v", password, err)
	}
	callback, err := client.CompleteProviderAuthorization(context.Background(), "provider-reauth", ProviderCallbackRequest{State: proof, Code: "authorization-code"})
	if err != nil || callback.Callback.Action != "reauth" || callback.ReauthProof != proof || callback.SessionHandle != rotated {
		t.Fatalf("provider reauth = %#v, %v", callback, err)
	}
	omitSession = true
	if _, err := client.PasswordReauthenticate(context.Background(), proof, "password-reauth", proof, PasswordReauthRequest{Password: "a sufficiently long password"}); err == nil {
		t.Fatal("password reauthentication accepted a missing rotated session")
	}
	if _, err := client.CompleteProviderAuthorization(context.Background(), "provider-reauth", ProviderCallbackRequest{State: proof, Code: "authorization-code"}); err == nil {
		t.Fatal("provider reauthentication accepted a missing rotated session")
	}
}
