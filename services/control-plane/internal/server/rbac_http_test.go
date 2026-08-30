package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type rbacHTTPReaderFake struct {
	scope           authz.ScopeRef
	resolveErr      error
	membership      postgres.Membership
	membershipPage  postgres.MembershipPage
	roleBinding     postgres.RoleBinding
	roleBindingPage postgres.RoleBindingPage
	err             error
	resolves        int
	memberships     int
	lists           int
	after           string
	limit           int
	bindings        int
	bindingLists    int
	bindingAfter    string
	bindingLimit    int
}

type rbacHTTPMutatorFake struct {
	created      postgres.CreateMembershipInput
	bound        postgres.BindRoleInput
	transitioned postgres.MembershipTransitionInput
	result       postgres.MutationResult
	err          error
}

func (fake *rbacHTTPMutatorFake) CreateMembership(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input postgres.CreateMembershipInput) (postgres.MutationResult, error) {
	fake.created = input
	return fake.result, fake.err
}
func (fake *rbacHTTPMutatorFake) ResumeMembership(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input postgres.MembershipTransitionInput) (postgres.MutationResult, error) {
	fake.transitioned = input
	return fake.result, fake.err
}
func (fake *rbacHTTPMutatorFake) SuspendMembership(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input postgres.MembershipTransitionInput) (postgres.MutationResult, error) {
	fake.transitioned = input
	return fake.result, fake.err
}
func (fake *rbacHTTPMutatorFake) RevokeMembership(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input postgres.MembershipTransitionInput) (postgres.MutationResult, error) {
	fake.transitioned = input
	return fake.result, fake.err
}
func (fake *rbacHTTPMutatorFake) BindRole(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input postgres.BindRoleInput) (postgres.MutationResult, error) {
	fake.bound = input
	return fake.result, fake.err
}
func (fake *rbacHTTPMutatorFake) RevokeRoleBinding(context.Context, string, *authn.VerifiedPrincipal, postgres.RevokeRoleBindingInput) (postgres.MutationResult, error) {
	return postgres.MutationResult{}, fake.err
}

func (fake *rbacHTTPReaderFake) ResolveMembershipScope(context.Context, string, string) (authz.ScopeRef, error) {
	fake.resolves++
	return fake.scope, fake.resolveErr
}

func (fake *rbacHTTPReaderFake) ResolveRoleBindingScope(context.Context, string, string) (authz.ScopeRef, error) {
	fake.resolves++
	return fake.scope, fake.resolveErr
}

func (fake *rbacHTTPReaderFake) GetMembership(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string, _ authz.ScopeRef) (postgres.Membership, error) {
	fake.memberships++
	return fake.membership, fake.err
}

func (fake *rbacHTTPReaderFake) ListMemberships(_ context.Context, _ string, _ *authn.VerifiedPrincipal, after string, limit int) (postgres.MembershipPage, error) {
	fake.lists++
	fake.after = after
	fake.limit = limit
	return fake.membershipPage, fake.err
}

func (fake *rbacHTTPReaderFake) GetRoleBinding(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string, _ authz.ScopeRef) (postgres.RoleBinding, error) {
	fake.bindings++
	return fake.roleBinding, fake.err
}

func (fake *rbacHTTPReaderFake) ListRoleBindings(_ context.Context, _ string, _ *authn.VerifiedPrincipal, after string, limit int) (postgres.RoleBindingPage, error) {
	fake.bindingLists++
	fake.bindingAfter = after
	fake.bindingLimit = limit
	return fake.roleBindingPage, fake.err
}

func TestRBACHTTPServerUsesStoredScopeAndGeneratedResources(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		path       string
		permission string
		check      func(t *testing.T, body []byte)
		reader     *rbacHTTPReaderFake
	}{
		{
			name: "membership", path: "/v1/tenants/tenant-alpha/memberships/membership-alpha", permission: "memberships.get",
			reader: &rbacHTTPReaderFake{scope: authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"}, membership: postgres.Membership{
				UID: "membership-alpha", Name: "membership-alpha", TenantID: "tenant-alpha", Subject: authz.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"}, Scope: authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"}, State: "active", ResourceVersion: 4, CreatedAt: now, UpdatedAt: now,
			}},
			check: func(t *testing.T, body []byte) {
				value, err := platformv1alpha1.DecodeMembershipResponseJSON(body)
				if err != nil || value.Value.Spec.Scope.Level != "project" || value.Value.Spec.Scope.Ref == nil || value.Value.Spec.Subject.Subject != "user-alpha" {
					t.Fatalf("membership response=%#v err=%v", value.Value, err)
				}
			},
		},
		{
			name: "role binding", path: "/v1/tenants/tenant-alpha/role-bindings/binding-alpha", permission: "role-bindings.get",
			reader: &rbacHTTPReaderFake{scope: authz.ScopeRef{Level: authz.ScopeOrganization, ID: "organization-alpha"}, roleBinding: postgres.RoleBinding{
				UID: "binding-alpha", Name: "binding-alpha", TenantID: "tenant-alpha", Subject: authz.SubjectRef{Kind: "serviceAccount", Issuer: "https://issuer.example", Subject: "agent-alpha"}, RoleName: "organization.admin", RoleVersion: 1, Scope: authz.ScopeRef{Level: authz.ScopeOrganization, ID: "organization-alpha"}, State: "active", ResourceVersion: 5, CreatedAt: now, UpdatedAt: now,
			}},
			check: func(t *testing.T, body []byte) {
				value, err := platformv1alpha1.DecodeRoleBindingResponseJSON(body)
				if err != nil || value.Value.Spec.RoleName != "organization.admin" || value.Value.Spec.Scope.Level != "organization" {
					t.Fatalf("role binding response=%#v err=%v", value.Value, err)
				}
			},
		},
		{
			name: "managed host role binding", path: "/v1/managed-host/tenants/tenant-alpha/role-bindings/binding-alpha", permission: "role-bindings.get",
			reader: &rbacHTTPReaderFake{scope: authz.ScopeRef{Level: authz.ScopeOrganization, ID: "organization-alpha"}, roleBinding: postgres.RoleBinding{
				UID: "binding-alpha", Name: "binding-alpha", TenantID: "tenant-alpha", Subject: authz.SubjectRef{Kind: "serviceAccount", Issuer: "https://issuer.example", Subject: "agent-alpha"}, RoleName: "organization.admin", RoleVersion: 1, Scope: authz.ScopeRef{Level: authz.ScopeOrganization, ID: "organization-alpha"}, State: "active", ResourceVersion: 5, CreatedAt: now, UpdatedAt: now,
			}},
			check: func(t *testing.T, body []byte) {
				value, err := platformv1alpha1.DecodeRoleBindingResponseJSON(body)
				if err != nil || value.Value.Spec.RoleName != "organization.admin" || value.Value.Spec.Scope.Level != "organization" {
					t.Fatalf("role binding response=%#v err=%v", value.Value, err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &projectHTTPVerifierFake{}
			server, err := NewRBACHTTPServer(verifier, test.reader, &rbacHTTPMutatorFake{})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Authorization", "Bearer access-token")
			request.Header.Set("X-Request-ID", "request-alpha")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Header().Get("X-Resource-Version") == "" || test.reader.resolves != 1 {
				t.Fatalf("status=%d headers=%v resolves=%d body=%s", response.Code, response.Header(), test.reader.resolves, response.Body.String())
			}
			if verifier.seen.ResourceLevel != string(test.reader.scope.Level) || verifier.seen.ResourceID != test.reader.scope.ID || verifier.seen.RequiredPermission != test.permission {
				t.Fatalf("verification request=%#v", verifier.seen)
			}
			if test.permission == "memberships.get" && test.reader.memberships != 1 || test.permission == "role-bindings.get" && test.reader.bindings != 1 {
				t.Fatalf("reader calls membership=%d binding=%d", test.reader.memberships, test.reader.bindings)
			}
			test.check(t, response.Body.Bytes())
		})
	}
}

func TestRBACHTTPServerListsMembershipsWithTenantBoundCursor(t *testing.T) {
	now := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &rbacHTTPReaderFake{membershipPage: postgres.MembershipPage{
		Memberships: []postgres.Membership{{
			UID: "membership-alpha", Name: "membership-alpha", TenantID: "tenant-alpha",
			Subject: authz.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"},
			Scope:   authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-alpha"}, State: "active", ResourceVersion: 4, CreatedAt: now, UpdatedAt: now,
		}},
		NextMembershipID: "membership-alpha",
	}}
	server, err := NewRBACHTTPServer(verifier, reader, &rbacHTTPMutatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/memberships?pageSize=1", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-list")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.lists != 1 || reader.after != "" || reader.limit != 1 || reader.resolves != 0 {
		t.Fatalf("status=%d lists=%d after=%q limit=%d resolves=%d body=%s", response.Code, reader.lists, reader.after, reader.limit, reader.resolves, response.Body.String())
	}
	page, err := platformv1alpha1.DecodeMembershipPageResponseJSON(response.Body.Bytes())
	if err != nil || len(page.Value.Memberships) != 1 || page.Value.NextPageToken == "" {
		t.Fatalf("membership page = %#v / %v", page, err)
	}
	if after, ok := decodeMembershipPageToken("tenant-alpha", page.Value.NextPageToken); !ok || after != "membership-alpha" {
		t.Fatalf("next page token = %q / %q / %t", page.Value.NextPageToken, after, ok)
	}
	if verifier.seen != (authn.VerificationRequest{TenantID: "tenant-alpha", ResourceLevel: "tenant", ResourceID: "tenant-alpha", RequiredPermission: "memberships.list"}) {
		t.Fatalf("verification request = %#v", verifier.seen)
	}

	otherTenantToken, ok := encodeMembershipPageToken("tenant-other", "membership-alpha")
	if !ok {
		t.Fatal("valid cross-tenant fixture token was not encoded")
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/memberships?pageToken="+otherTenantToken, nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-invalid")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || reader.lists != 1 || verifier.calls != 1 {
		t.Fatalf("cross-tenant token status=%d verifier calls=%d list calls=%d body=%s", response.Code, verifier.calls, reader.lists, response.Body.String())
	}
}

func TestRBACHTTPServerListsRoleBindingsWithTenantBoundCursor(t *testing.T) {
	now := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &rbacHTTPReaderFake{roleBindingPage: postgres.RoleBindingPage{
		RoleBindings: []postgres.RoleBinding{{
			UID: "binding-alpha", Name: "binding-alpha", TenantID: "tenant-alpha",
			Subject:  authz.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"},
			RoleName: "tenant.admin", RoleVersion: 1, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-alpha"}, State: "active", ResourceVersion: 4, CreatedAt: now, UpdatedAt: now,
		}},
		NextRoleBindingID: "binding-alpha",
	}}
	server, err := NewRBACHTTPServer(verifier, reader, &rbacHTTPMutatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/role-bindings?pageSize=1", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-list")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.bindingLists != 1 || reader.bindingAfter != "" || reader.bindingLimit != 1 || reader.resolves != 0 {
		t.Fatalf("status=%d lists=%d after=%q limit=%d resolves=%d body=%s", response.Code, reader.bindingLists, reader.bindingAfter, reader.bindingLimit, reader.resolves, response.Body.String())
	}
	page, err := platformv1alpha1.DecodeRoleBindingPageResponseJSON(response.Body.Bytes())
	if err != nil || len(page.Value.RoleBindings) != 1 || page.Value.NextPageToken == "" {
		t.Fatalf("role binding page = %#v / %v", page, err)
	}
	if after, ok := decodeRoleBindingPageToken("tenant-alpha", page.Value.NextPageToken); !ok || after != "binding-alpha" {
		t.Fatalf("next page token = %q / %q / %t", page.Value.NextPageToken, after, ok)
	}
	if verifier.seen != (authn.VerificationRequest{TenantID: "tenant-alpha", ResourceLevel: "tenant", ResourceID: "tenant-alpha", RequiredPermission: "role-bindings.list"}) {
		t.Fatalf("verification request = %#v", verifier.seen)
	}

	otherTenantToken, ok := encodeRoleBindingPageToken("tenant-other", "binding-alpha")
	if !ok {
		t.Fatal("valid cross-tenant fixture token was not encoded")
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/role-bindings?pageToken="+otherTenantToken, nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-invalid")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || reader.bindingLists != 1 || verifier.calls != 1 {
		t.Fatalf("cross-tenant token status=%d verifier calls=%d list calls=%d body=%s", response.Code, verifier.calls, reader.bindingLists, response.Body.String())
	}
}

func TestRBACHTTPServerCreatesMembershipWithScopedMutationContract(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	mutator := &rbacHTTPMutatorFake{result: postgres.MutationResult{TenantID: "tenant-alpha", ResourceUID: "membership-new", ResourceVersion: 8, State: "active"}}
	server, err := NewRBACHTTPServer(verifier, &rbacHTTPReaderFake{}, mutator)
	if err != nil {
		t.Fatal(err)
	}
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-alpha"}
	body, err := platformv1alpha1.EncodeMembershipCreateRequestJSON(platformv1alpha1.MembershipCreateRequest{
		ExpectedTenantRevision: 7, MembershipID: "membership-new", MembershipName: "membership-new",
		Subject: commonv1alpha1.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"}, Scope: scopeResource(scope, "tenant-alpha"), AuditFactUID: "audit-create", ReasonCode: "operator-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/memberships", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Header().Get("X-Resource-Version") != "8" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	value, err := platformv1alpha1.DecodeRBACMutationResultResponseJSON(response.Body.Bytes())
	if err != nil || value.Value.ResourceUID != "membership-new" || value.Value.ResourceVersion != "8" {
		t.Fatalf("result=%#v err=%v", value.Value, err)
	}
	if verifier.seen.RequiredPermission != "memberships.create" || verifier.seen.ResourceLevel != "tenant" || verifier.seen.ResourceID != "tenant-alpha" {
		t.Fatalf("verification=%#v", verifier.seen)
	}
	if mutator.created.ExpectedTenantRevision != 7 || mutator.created.Scope != scope || mutator.created.Subject.Subject != "user-alpha" {
		t.Fatalf("mutation input=%#v", mutator.created)
	}
}

func TestRBACHTTPServerMapsMutationConflict(t *testing.T) {
	server, err := NewRBACHTTPServer(&projectHTTPVerifierFake{}, &rbacHTTPReaderFake{}, &rbacHTTPMutatorFake{err: postgres.ErrMutationConflict})
	if err != nil {
		t.Fatal(err)
	}
	body, err := platformv1alpha1.EncodeMembershipCreateRequestJSON(platformv1alpha1.MembershipCreateRequest{ExpectedTenantRevision: 7, MembershipID: "membership-new", MembershipName: "membership-new", Subject: commonv1alpha1.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"}, Scope: scopeResource(authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-alpha"}, "tenant-alpha"), AuditFactUID: "audit-create", ReasonCode: "operator-request"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/memberships", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRBACHTTPServerBindsRoleWithScopedMutationContract(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	mutator := &rbacHTTPMutatorFake{result: postgres.MutationResult{ResourceUID: "binding-new", ResourceVersion: 11, State: "active"}}
	server, err := NewRBACHTTPServer(verifier, &rbacHTTPReaderFake{}, mutator)
	if err != nil {
		t.Fatal(err)
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"}
	body, err := platformv1alpha1.EncodeRoleBindingCreateRequestJSON(platformv1alpha1.RoleBindingCreateRequest{
		ExpectedTenantRevision: 10, RoleBindingID: "binding-new", RoleBindingName: "binding-new",
		Subject: commonv1alpha1.SubjectRef{Kind: "serviceAccount", Issuer: "https://issuer.example", Subject: "agent-alpha"}, RoleName: "project.operator", RoleVersion: 1,
		Scope: scopeResource(scope, "tenant-alpha"), AuditFactUID: "audit-bind", ReasonCode: "operator-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/role-bindings", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Header().Get("X-Resource-Version") != "11" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if verifier.seen.RequiredPermission != "role-bindings.bind" || verifier.seen.ResourceLevel != "project" || verifier.seen.ResourceID != "project-alpha" {
		t.Fatalf("verification=%#v", verifier.seen)
	}
	if mutator.bound.RoleBindingUID != "binding-new" || mutator.bound.RoleName != "project.operator" || mutator.bound.Scope != scope {
		t.Fatalf("mutation input=%#v", mutator.bound)
	}
}

func TestRBACHTTPServerTransitionsMembershipUsingStoredScope(t *testing.T) {
	for _, test := range []struct{ action, permission, state string }{
		{action: "resume", permission: "memberships.update", state: "active"},
		{action: "revoke", permission: "memberships.delete", state: "revoked"},
	} {
		t.Run(test.action, func(t *testing.T) {
			verifier := &projectHTTPVerifierFake{}
			reader := &rbacHTTPReaderFake{scope: authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"}}
			mutator := &rbacHTTPMutatorFake{result: postgres.MutationResult{TenantID: "tenant-alpha", ResourceUID: "membership-alpha", ResourceVersion: 9, State: test.state}}
			server, err := NewRBACHTTPServer(verifier, reader, mutator)
			if err != nil {
				t.Fatal(err)
			}
			body, err := platformv1alpha1.EncodeMembershipTransitionRequestJSON(platformv1alpha1.MembershipTransitionRequest{ExpectedTenantRevision: 8, ExpectedResourceVersion: 7, AuditFactUID: "audit-" + test.action, ReasonCode: "operator-request"})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/memberships/membership-alpha:"+test.action, strings.NewReader(string(body)))
			request.Header.Set("Authorization", "Bearer access-token")
			request.Header.Set("X-Request-ID", "request-alpha")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Header().Get("X-Resource-Version") != "9" || reader.resolves != 1 {
				t.Fatalf("status=%d headers=%v resolves=%d body=%s", response.Code, response.Header(), reader.resolves, response.Body.String())
			}
			if verifier.seen.RequiredPermission != test.permission || verifier.seen.ResourceLevel != "project" || verifier.seen.ResourceID != "project-alpha" {
				t.Fatalf("verification=%#v", verifier.seen)
			}
			if mutator.transitioned.MembershipUID != "membership-alpha" || mutator.transitioned.ExpectedResourceVersion != 7 {
				t.Fatalf("transition=%#v", mutator.transitioned)
			}
		})
	}
}

func TestRBACHTTPServerDoesNotExposeMissingResourceBeforeAuthentication(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	reader := &rbacHTTPReaderFake{resolveErr: postgres.ErrMembershipNotFound}
	server, err := NewRBACHTTPServer(verifier, reader, &rbacHTTPMutatorFake{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/memberships/missing", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || verifier.calls != 0 || reader.resolves != 1 {
		t.Fatalf("status=%d verifier calls=%d resolves=%d body=%s", response.Code, verifier.calls, reader.resolves, response.Body.String())
	}

	if _, err := NewRBACHTTPServer(nil, reader, &rbacHTTPMutatorFake{}); !errors.Is(err, ErrInvalidRBACHTTPServer) {
		t.Fatalf("nil verifier error=%v", err)
	}
	if _, err := NewRBACHTTPServer(verifier, reader, nil); !errors.Is(err, ErrInvalidRBACHTTPServer) {
		t.Fatalf("nil mutator error=%v", err)
	}
}
