package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

func TestPrincipalTokenAuthorizationUsesServiceAccountCredentialAndCurrentRBAC(t *testing.T) {
	request, digest, subject, subjectDigest := principalTokenAuthorizationFixture(t)
	organizationID := "organization-alpha"
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(request.TenantID), rowValues(request.TenantID),
		rowValues("ci-alpha", subject.Kind, subject.Issuer, subject.Subject, subjectDigest),
		rowValues("project", request.TenantID, &organizationID, &request.ProjectID),
		rowValues(databaseCatalogFixture(t)),
		rowValues(databaseCandidateFixture(t, subject, subjectDigest, nil)),
	}}
	service, connection := tokenAuthorizationTestService(t, transaction)
	authorization, err := service.AuthorizePrincipalToken(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.PrincipalID != "ci-alpha" || authorization.Subject != (apiSubject(subject)) ||
		authorization.Issuer != subject.Issuer || authorization.TenantID != request.TenantID ||
		authorization.ProjectID != request.ProjectID || authorization.Application != request.Application ||
		!containsString(authorization.Scopes, "projects.get") {
		t.Fatalf("authorization = %#v", authorization)
	}
	if len(transaction.queries) != 6 || transaction.queries[2].sql != readServiceAccountSubjectSQL ||
		transaction.queries[3].sql != resolveAuthorizationScopeSQL || transaction.queries[4].sql != readBuiltinRoleCatalogSQL ||
		transaction.queries[5].sql != readAuthorizationCandidatesSQL {
		t.Fatalf("queries = %#v", transaction.queries)
	}
	if got := transaction.queries[2].arguments; len(got) != 3 || string(got[0].([]byte)) != string(digest[:]) || got[1] != "user" || got[2] != request.TenantID {
		t.Fatalf("credential query arguments = %#v", got)
	}
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 {
		t.Fatalf("commit/rollback = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
	}
	assertReadOnlyOptions(t, connection.beginOptions)
}

func TestPrincipalTokenAuthorizationAddsRemoteWorkerBootstrapOnlyForProjectAdminAutomation(t *testing.T) {
	request, _, subject, subjectDigest := principalTokenAuthorizationFixture(t)
	request.Application = api.IdentityApplicationAdmin
	organizationID := "organization-alpha"
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(request.TenantID), rowValues(request.TenantID),
		rowValues("ci-alpha", subject.Kind, subject.Issuer, subject.Subject, subjectDigest),
		rowValues("project", request.TenantID, &organizationID, &request.ProjectID),
		rowValues(databaseCatalogFixture(t)),
		rowValues(serviceAccountTenantAdminCandidate(t, subject, subjectDigest)),
		rowError(pgx.ErrNoRows),
	}}
	service, _ := tokenAuthorizationTestService(t, transaction)
	authorization, err := service.AuthorizePrincipalToken(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(authorization.Scopes, "projects.act") || !containsString(authorization.Scopes, "remote-worker-bootstrap.act") {
		t.Fatalf("automation scopes = %v", authorization.Scopes)
	}
	for index := 1; index < len(authorization.Scopes); index++ {
		if authorization.Scopes[index-1] >= authorization.Scopes[index] {
			t.Fatalf("automation scopes are not sorted: %v", authorization.Scopes)
		}
	}
}

func TestPrincipalTokenAuthorizationUsesProjectServiceAccountRoleWithoutTenantAdmin(t *testing.T) {
	request, _, subject, subjectDigest := principalTokenAuthorizationFixture(t)
	request.Application = api.IdentityApplicationAdmin
	organizationID := "organization-alpha"
	for _, test := range []struct {
		name          string
		role          string
		wantAct       bool
		wantBootstrap bool
	}{
		{name: "viewer", role: "project.viewer"},
		{name: "operator", role: "project.operator", wantAct: true, wantBootstrap: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: []rowScanner{
				rowValues(request.TenantID), rowValues(request.TenantID),
				rowValues("ci-alpha", subject.Kind, subject.Issuer, subject.Subject, subjectDigest),
				rowValues("project", request.TenantID, &organizationID, &request.ProjectID),
				rowValues(databaseCatalogFixture(t)),
				rowValues(serviceAccountProjectCandidate(t, subject, subjectDigest, request.ProjectID, test.role)),
				rowError(pgx.ErrNoRows),
			}}
			service, _ := tokenAuthorizationTestService(t, transaction)
			authorization, err := service.AuthorizePrincipalToken(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if !containsString(authorization.Scopes, "projects.get") || containsString(authorization.Scopes, "tenants.get") ||
				containsString(authorization.Scopes, "projects.act") != test.wantAct ||
				containsString(authorization.Scopes, "remote-worker-bootstrap.act") != test.wantBootstrap {
				t.Fatalf("scopes = %v", authorization.Scopes)
			}
		})
	}
}

func TestPrincipalTokenAuthorizationRejectsProjectServiceAccountForOtherProject(t *testing.T) {
	request, _, subject, subjectDigest := principalTokenAuthorizationFixture(t)
	request.Application = api.IdentityApplicationAdmin
	organizationID := "organization-alpha"
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(request.TenantID), rowValues(request.TenantID),
		rowValues("ci-alpha", subject.Kind, subject.Issuer, subject.Subject, subjectDigest),
		rowValues("project", request.TenantID, &organizationID, &request.ProjectID),
		rowValues(databaseCatalogFixture(t)),
		rowValues(serviceAccountProjectCandidate(t, subject, subjectDigest, "project-other", "project.operator")),
		rowError(pgx.ErrNoRows),
	}}
	service, _ := tokenAuthorizationTestService(t, transaction)
	if _, err := service.AuthorizePrincipalToken(context.Background(), request); !errors.Is(err, ErrTokenAuthorizationDenied) {
		t.Fatalf("error = %v, want ErrTokenAuthorizationDenied", err)
	}
}

func TestPrincipalTokenAuthorizationKeepsTenantAdminGateForHumanCLI(t *testing.T) {
	request, digest, _, _ := principalTokenAuthorizationFixture(t)
	request.Application = api.IdentityApplicationAdmin
	request.ClientID = "cloud-agents-cli"
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	subjectDigest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	organizationID := "organization-alpha"
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(request.TenantID), rowValues(request.TenantID),
		rowValues("alpha", subject.Kind, subject.Issuer, subject.Subject, subjectDigest),
		rowValues("project", request.TenantID, &organizationID, &request.ProjectID),
		rowValues(databaseCatalogFixture(t)),
		rowValues(serviceAccountProjectCandidate(t, subject, subjectDigest, request.ProjectID, "project.viewer")),
		rowError(pgx.ErrNoRows),
	}}
	service, _ := tokenAuthorizationTestService(t, transaction)
	if _, err := service.AuthorizePrincipalToken(context.Background(), request); !errors.Is(err, ErrTokenAuthorizationDenied) {
		t.Fatalf("error = %v, want ErrTokenAuthorizationDenied", err)
	}
	if got := transaction.queries[2]; got.sql != readCLIGrantSubjectSQL || len(got.arguments) != 2 || string(got.arguments[0].([]byte)) != string(digest[:]) {
		t.Fatalf("CLI grant subject query = %#v", got)
	}
}

func TestRemoteWorkerBootstrapScopeProjectionRejectsOtherPurposes(t *testing.T) {
	base := api.PrincipalTokenAuthorizationRequest{
		Application: api.IdentityApplicationAdmin, ClientID: "cloud-agents-automation",
		TenantID: "tenant-alpha", ProjectID: "project-alpha",
	}
	for _, test := range []struct {
		name    string
		request api.PrincipalTokenAuthorizationRequest
		scopes  []string
	}{
		{name: "human cli grant", request: func() api.PrincipalTokenAuthorizationRequest {
			value := base
			value.ClientID = "cloud-agents-cli"
			return value
		}(), scopes: []string{"projects.act", "projects.get"}},
		{name: "user automation", request: func() api.PrincipalTokenAuthorizationRequest {
			value := base
			value.Application = api.IdentityApplicationUser
			return value
		}(), scopes: []string{"projects.act", "projects.get"}},
		{name: "tenant automation", request: func() api.PrincipalTokenAuthorizationRequest { value := base; value.ProjectID = ""; return value }(), scopes: []string{"projects.act", "projects.get"}},
		{name: "project viewer", request: base, scopes: []string{"projects.get"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := projectAutomationScopes(test.request, append([]string(nil), test.scopes...))
			if containsString(actual, "remote-worker-bootstrap.act") {
				t.Fatalf("scopes = %v", actual)
			}
		})
	}
}

func TestPrincipalTokenAuthorizationRejectsInactiveOrWrongCredentialKind(t *testing.T) {
	request, _, subject, subjectDigest := principalTokenAuthorizationFixture(t)
	for _, test := range []struct {
		name    string
		row     rowScanner
		wantErr error
	}{
		{name: "inactive credential", row: rowError(pgx.ErrNoRows), wantErr: ErrTokenAuthorizationDenied},
		{name: "human subject for automation", row: rowValues("alpha", "user", subject.Issuer, "user-alpha", subjectDigest), wantErr: ErrTokenAuthorizationAuthority},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: []rowScanner{rowValues(request.TenantID), rowValues(request.TenantID), test.row}}
			service, _ := tokenAuthorizationTestService(t, transaction)
			if _, err := service.AuthorizePrincipalToken(context.Background(), request); !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if transaction.commitCalls != 0 || transaction.rollbackCalls != 1 {
				t.Fatalf("commit/rollback = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
			}
		})
	}
}

func principalTokenAuthorizationFixture(t *testing.T) (api.PrincipalTokenAuthorizationRequest, [sha256.Size]byte, authz.SubjectRef, string) {
	t.Helper()
	var digest [sha256.Size]byte
	for index := range digest {
		digest[index] = 0x22
	}
	subject := authz.SubjectRef{Kind: "serviceAccount", Issuer: "https://identity.example.test/", Subject: "service-ci-alpha"}
	subjectDigest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return api.PrincipalTokenAuthorizationRequest{
		Application: api.IdentityApplicationUser, ClientID: "cloud-agents-automation",
		CredentialSHA256: "sha256:" + hex.EncodeToString(digest[:]), TenantID: "tenant-alpha", ProjectID: "project-alpha",
	}, digest, subject, subjectDigest
}

func apiSubject(subject authz.SubjectRef) common.SubjectRef {
	return common.SubjectRef{Kind: subject.Kind, Issuer: subject.Issuer, Subject: subject.Subject}
}

func serviceAccountProjectCandidate(t *testing.T, subject authz.SubjectRef, digest, projectID, role string) []byte {
	t.Helper()
	scope := map[string]any{"level": "project", "tenant_id": "tenant-alpha", "organization_id": "organization-alpha", "project_id": projectID}
	return mustJSON(t, []any{map[string]any{
		"membership": map[string]any{"uid": "membership-service", "subject_kind": subject.Kind, "subject_issuer": subject.Issuer, "subject_value": subject.Subject, "subject_digest": digest, "scope": scope, "state": "active", "expires_at": nil},
		"binding":    map[string]any{"uid": "binding-service", "subject_kind": subject.Kind, "subject_issuer": subject.Issuer, "subject_value": subject.Subject, "subject_digest": digest, "role_name": role, "role_version": int64(1), "scope": scope, "state": "active", "expires_at": nil},
	}})
}
