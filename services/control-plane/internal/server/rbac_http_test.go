package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type rbacHTTPReaderFake struct {
	scope       authz.ScopeRef
	resolveErr  error
	membership  postgres.Membership
	roleBinding postgres.RoleBinding
	err         error
	resolves    int
	memberships int
	bindings    int
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

func (fake *rbacHTTPReaderFake) GetRoleBinding(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string, _ authz.ScopeRef) (postgres.RoleBinding, error) {
	fake.bindings++
	return fake.roleBinding, fake.err
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &projectHTTPVerifierFake{}
			server, err := NewRBACHTTPServer(verifier, test.reader)
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
			if test.name == "membership" && test.reader.memberships != 1 || test.name == "role binding" && test.reader.bindings != 1 {
				t.Fatalf("reader calls membership=%d binding=%d", test.reader.memberships, test.reader.bindings)
			}
			test.check(t, response.Body.Bytes())
		})
	}
}

func TestRBACHTTPServerDoesNotExposeMissingResourceBeforeAuthentication(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	reader := &rbacHTTPReaderFake{resolveErr: postgres.ErrMembershipNotFound}
	server, err := NewRBACHTTPServer(verifier, reader)
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

	if _, err := NewRBACHTTPServer(nil, reader); !errors.Is(err, ErrInvalidRBACHTTPServer) {
		t.Fatalf("nil verifier error=%v", err)
	}
}
