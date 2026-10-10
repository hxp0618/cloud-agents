package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

func TestTokenAuthorizationUsesOneTenantTransaction(t *testing.T) {
	request, digest, subject, subjectDigest := tokenAuthorizationFixture(t, api.IdentityApplicationUser, "project-alpha")
	transaction := tokenAuthorizationTransaction(t, request.TenantID, request.ProjectID, subject, subjectDigest,
		databaseCatalogFixture(t), databaseCandidateFixture(t, subject, subjectDigest, nil))
	service, connection := tokenAuthorizationTestService(t, transaction)

	authorization, err := service.AuthorizeTenantToken(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.UserID != "alpha" || authorization.Issuer != subject.Issuer ||
		authorization.TenantID != request.TenantID || authorization.ProjectID != request.ProjectID ||
		authorization.Application != request.Application {
		t.Fatalf("authorization = %#v", authorization)
	}
	if got := authorization.Scopes; len(got) != 6 || !containsString(got, "environment-profiles.list") || !containsString(got, "environment-quotas.get") || !containsString(got, "environments.get") || !containsString(got, "projects.get") {
		t.Fatalf("scopes = %v", got)
	}
	if len(transaction.queries) != 6 || transaction.queries[2].sql != readIdentitySessionSubjectSQL ||
		transaction.queries[3].sql != resolveAuthorizationScopeSQL || transaction.queries[4].sql != readBuiltinRoleCatalogSQL ||
		transaction.queries[5].sql != readAuthorizationCandidatesSQL {
		t.Fatalf("transaction queries = %#v", transaction.queries)
	}
	if got, ok := transaction.queries[2].arguments[0].([]byte); !ok || len(got) != sha256.Size || string(got) != string(digest[:]) ||
		transaction.queries[2].arguments[1] != "user" {
		t.Fatalf("session query arguments = %#v", transaction.queries[2].arguments)
	}
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 {
		t.Fatalf("commit/rollback = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
	}
	assertReadOnlyOptions(t, connection.beginOptions)
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTokenAuthorizationRejectsOtherProjectAndInactiveSession(t *testing.T) {
	request, _, subject, subjectDigest := tokenAuthorizationFixture(t, api.IdentityApplicationUser, "project-alpha")
	for _, test := range []struct {
		name    string
		rows    []rowScanner
		wantErr error
	}{
		{
			name:    "other project membership",
			wantErr: ErrTokenAuthorizationDenied,
			rows: tokenAuthorizationRows(t, request.TenantID, request.ProjectID, subject, subjectDigest,
				databaseCatalogFixture(t), databaseProjectCandidateFixture(t, subject, subjectDigest, "project-beta", nil)),
		},
		{
			name:    "inactive session",
			wantErr: ErrTokenAuthorizationDenied,
			rows:    []rowScanner{rowValues(request.TenantID), rowValues(request.TenantID), rowError(pgx.ErrNoRows)},
		},
		{
			name:    "subject digest drift",
			wantErr: ErrTokenAuthorizationAuthority,
			rows: []rowScanner{rowValues(request.TenantID), rowValues(request.TenantID),
				rowValues("alpha", subject.Kind, subject.Issuer, subject.Subject, "sha256:"+string(make([]byte, 64)))},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: test.rows}
			service, connection := tokenAuthorizationTestService(t, transaction)
			if _, err := service.AuthorizeTenantToken(context.Background(), request); !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if transaction.commitCalls != 0 || transaction.rollbackCalls != 1 {
				t.Fatalf("commit/rollback = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
			}
			assertConnectionDisposition(t, connection, 1, 0)
		})
	}
}

func TestAdminTokenAuthorizationUsesV2GlobalBinding(t *testing.T) {
	request, _, subject, subjectDigest := tokenAuthorizationFixture(t, api.IdentityApplicationAdmin, "project-alpha")
	rows := tokenAuthorizationRows(t, request.TenantID, request.ProjectID, subject, subjectDigest,
		databaseCatalogVersionFixture(t, 2), mustJSON(t, []any{}))
	rows = append(rows, rowValues("alpha", subject.Kind, subject.Issuer, subject.Subject, subjectDigest))
	transaction := &fakeTransaction{rows: rows}
	service, _ := tokenAuthorizationTestService(t, transaction)

	authorization, err := service.AuthorizeTenantToken(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"audit.list", "mcp-servers.create", "operations.list", "projects.act", "projects.get", "snapshots.create"} {
		if !containsString(authorization.Scopes, scope) {
			t.Fatalf("admin scopes %v missing %q", authorization.Scopes, scope)
		}
	}
	if len(authorization.Scopes) != 46 {
		t.Fatalf("admin project-token scope count = %d, want 46: %v", len(authorization.Scopes), authorization.Scopes)
	}
	for _, scope := range []string{"memberships.create", "remote-worker-bootstrap.act", "sessions.get", "tenants.update"} {
		if containsString(authorization.Scopes, scope) {
			t.Fatalf("admin scopes %v contain %q", authorization.Scopes, scope)
		}
	}
	if len(transaction.queries) != 7 || transaction.queries[6].arguments[0] != subject.Kind ||
		transaction.queries[6].arguments[1] != subject.Issuer || transaction.queries[6].arguments[2] != subject.Subject {
		t.Fatalf("global binding query = %#v", transaction.queries)
	}
}

func TestTokenAuthorizationRejectsInvalidInputBeforeDatabase(t *testing.T) {
	request, _, _, _ := tokenAuthorizationFixture(t, api.IdentityApplicationUser, "project-alpha")
	connection := newFakeConnection()
	pool := &fakePool{connection: connection}
	userRunner := newTenantTransactionRunner(pool, time.Second)
	adminRunner := newTenantTransactionRunner(pool, time.Second)
	adminRunner.application = "admin"
	service, err := newTokenAuthorizationService(userRunner, adminRunner)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*api.TenantTokenAuthorizationRequest){
		func(value *api.TenantTokenAuthorizationRequest) { value.Application = "other" },
		func(value *api.TenantTokenAuthorizationRequest) { value.SessionSHA256 = "sha256:AA" },
		func(value *api.TenantTokenAuthorizationRequest) { value.TenantID = "../tenant" },
		func(value *api.TenantTokenAuthorizationRequest) { value.ProjectID = "project/alpha" },
	} {
		value := request
		mutate(&value)
		if _, err := service.AuthorizeTenantToken(context.Background(), value); !errors.Is(err, ErrTokenAuthorizationInvalidInput) {
			t.Fatalf("request %#v error = %v", value, err)
		}
	}
	if pool.acquireCalls != 0 {
		t.Fatalf("database acquire calls = %d", pool.acquireCalls)
	}
}

func tokenAuthorizationFixture(t *testing.T, application api.IdentityApplication, projectID string) (api.TenantTokenAuthorizationRequest, [sha256.Size]byte, authz.SubjectRef, string) {
	t.Helper()
	var digest [sha256.Size]byte
	for index := range digest {
		digest[index] = 0x11
	}
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	subjectDigest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return api.TenantTokenAuthorizationRequest{
		Application: application, SessionSHA256: "sha256:" + hex.EncodeToString(digest[:]),
		TenantID: "tenant-alpha", ProjectID: projectID,
	}, digest, subject, subjectDigest
}

func tokenAuthorizationTransaction(
	t *testing.T,
	tenantID, projectID string,
	subject authz.SubjectRef,
	subjectDigest string,
	catalog, candidates []byte,
) *fakeTransaction {
	t.Helper()
	return &fakeTransaction{rows: tokenAuthorizationRows(t, tenantID, projectID, subject, subjectDigest, catalog, candidates)}
}

func tokenAuthorizationRows(
	t *testing.T,
	tenantID, projectID string,
	subject authz.SubjectRef,
	subjectDigest string,
	catalog, candidates []byte,
) []rowScanner {
	t.Helper()
	organizationID := "organization-alpha"
	return []rowScanner{
		rowValues(tenantID),
		rowValues(tenantID),
		rowValues("alpha", subject.Kind, subject.Issuer, subject.Subject, subjectDigest),
		rowValues("project", tenantID, &organizationID, &projectID),
		rowValues(catalog),
		rowValues(candidates),
	}
}

func tokenAuthorizationTestService(t *testing.T, transaction *fakeTransaction) (*TokenAuthorizationService, *fakeConnection) {
	t.Helper()
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	pool := &fakePool{connection: connection}
	userRunner := newTenantTransactionRunner(pool, time.Second)
	adminRunner := newTenantTransactionRunner(pool, time.Second)
	adminRunner.application = "admin"
	service, err := newTokenAuthorizationService(userRunner, adminRunner)
	if err != nil {
		t.Fatal(err)
	}
	return service, connection
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
