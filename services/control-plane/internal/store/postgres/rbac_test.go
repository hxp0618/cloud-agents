package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

func TestTenantAuthorizationUsesOneBoundReadTransactionAndExactQueries(t *testing.T) {
	tenantID := "tenant-alpha"
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	organizationID := "organization-alpha"
	projectID := "project-alpha"
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID),
		rowValues(tenantID),
		rowValues("project", tenantID, &organizationID, &projectID),
		rowValues(databaseCatalogFixture(t)),
		rowValues(databaseCandidateFixture(t, subject, digest, nil)),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.clock = func() time.Time { return now }

	request := authz.Request{
		Subject: subject, Permission: "projects.get",
		Resource: authz.ScopeRef{Level: authz.ScopeProject, ID: projectID},
	}
	var saved TenantReadCapability
	err = runner.WithTenantRead(context.Background(), tenantID, func(
		ctx context.Context,
		capability TenantReadCapability,
	) error {
		saved = capability
		decision, authorizeErr := capability.Authorize(ctx, request)
		if authorizeErr != nil {
			return authorizeErr
		}
		if !decision.Allowed || decision.Evidence == nil || decision.Evidence.MembershipUID != "membership-alpha" || decision.Evidence.RoleBindingUID != "role-binding-alpha" {
			t.Fatalf("authorization decision = %#v", decision)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTenantRead() error = %v", err)
	}
	if _, err := saved.Authorize(context.Background(), request); !errors.Is(err, ErrTenantCapabilityClosed) {
		t.Fatalf("saved authorization capability error = %v", err)
	}
	if len(transaction.queries) != 5 {
		t.Fatalf("transaction query count = %d, want 5", len(transaction.queries))
	}
	if transaction.queries[2].sql != resolveAuthorizationScopeSQL || transaction.queries[3].sql != readBuiltinRoleCatalogSQL || transaction.queries[4].sql != readAuthorizationCandidatesSQL {
		t.Fatalf("authorization query identity drift: %#v", transaction.queries[2:])
	}
	if got := transaction.queries[2].arguments; len(got) != 2 || got[0] != "project" || got[1] != projectID {
		t.Fatalf("scope query arguments = %#v", got)
	}
	if got := transaction.queries[3].arguments; len(got) != 0 {
		t.Fatalf("catalog query accepted arguments: %#v", got)
	}
	if got := transaction.queries[4].arguments; len(got) != 3 || got[0] != subject.Kind || got[1] != subject.Issuer || got[2] != subject.Subject {
		t.Fatalf("candidate query arguments = %#v", got)
	}
	if strings.Contains(readAuthorizationCandidatesSQL, "binding.subject_digest = membership.subject_digest") || strings.Contains(readAuthorizationCandidatesSQL, "membership.subject_digest = $4") {
		t.Fatal("candidate query hides stored subject digest drift")
	}
	if !strings.Contains(readAuthorizationCandidatesSQL, "membership.resource_version < binding.resource_version") {
		t.Fatal("candidate query admits membership authority created after the role binding")
	}
	for _, query := range transaction.queries[2:] {
		if !strings.Contains(query.sql, "cloud_agents.") || strings.Contains(query.sql, tenantID) {
			t.Fatalf("query is not fully qualified or embedded tenant input: %s", query.sql)
		}
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTenantAuthorizationStoredSubjectDigestDriftFailsClosed(t *testing.T) {
	tenantID := "tenant-alpha"
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	var candidates []map[string]any
	if err := json.Unmarshal(databaseCandidateFixture(t, subject, digest, nil), &candidates); err != nil {
		t.Fatal(err)
	}
	candidates[0]["binding"].(map[string]any)["subject_digest"] = "sha256:" + strings.Repeat("0", 64)
	raw, err := json.Marshal(candidates)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := "organization-alpha"
	projectID := "project-alpha"
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID),
		rowValues("project", tenantID, &organizationID, &projectID),
		rowValues(databaseCatalogFixture(t)), rowValues(raw),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.clock = func() time.Time { return now }
	err = runner.WithTenantRead(context.Background(), tenantID, func(ctx context.Context, capability TenantReadCapability) error {
		_, authorizeErr := capability.Authorize(ctx, authz.Request{
			Subject: subject, Permission: "projects.get",
			Resource: authz.ScopeRef{Level: authz.ScopeProject, ID: projectID},
		})
		return authorizeErr
	})
	if !errors.Is(err, authz.ErrSnapshotMalformed) {
		t.Fatalf("stored digest drift error = %v, want ErrSnapshotMalformed", err)
	}
	if transaction.rollbackCalls != 1 || transaction.commitCalls != 0 {
		t.Fatalf("commit/rollback calls = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestTenantAuthorizationUnknownAndPlatformScopeAvoidUnneededReads(t *testing.T) {
	tenantID := "tenant-alpha"
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	tests := []struct {
		name        string
		resource    authz.ScopeRef
		extraRows   []rowScanner
		wantReason  authz.DenyReason
		wantQueries int
	}{
		{
			name: "unknown project", resource: authz.ScopeRef{Level: authz.ScopeProject, ID: "project-missing"},
			extraRows: []rowScanner{rowError(pgx.ErrNoRows)}, wantReason: authz.DenyUnknownScope, wantQueries: 3,
		},
		{
			name: "platform runtime", resource: authz.ScopeRef{Level: authz.ScopePlatform},
			wantReason: authz.DenyPlatformRuntime, wantQueries: 2,
		},
		{
			name: "invalid tenant scope", resource: authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-other"},
			wantReason: authz.DenyInvalidRequest, wantQueries: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := []rowScanner{rowValues(tenantID), rowValues(tenantID)}
			rows = append(rows, test.extraRows...)
			transaction := &fakeTransaction{rows: rows}
			connection := newFakeConnection(transaction)
			connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
			runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
			runner.clock = func() time.Time { return now }
			err := runner.WithTenantRead(context.Background(), tenantID, func(
				ctx context.Context,
				capability TenantReadCapability,
			) error {
				decision, authorizeErr := capability.Authorize(ctx, authz.Request{
					Subject: subject, Permission: "projects.get", Resource: test.resource,
				})
				if authorizeErr != nil {
					return authorizeErr
				}
				if decision.Allowed || decision.Reason != test.wantReason {
					t.Fatalf("decision = %#v, want %s", decision, test.wantReason)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("WithTenantRead() error = %v", err)
			}
			if len(transaction.queries) != test.wantQueries {
				t.Fatalf("query count = %d, want %d", len(transaction.queries), test.wantQueries)
			}
			assertConnectionDisposition(t, connection, 1, 0)
		})
	}
}

func TestTenantAuthorizationOperationalAndDecodeFailuresRollback(t *testing.T) {
	tenantID := "tenant-alpha"
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	organizationID := "organization-alpha"
	projectID := "project-alpha"
	request := authz.Request{Subject: subject, Permission: "projects.get", Resource: authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}}
	readErr := errors.New("scope read failed")
	tests := []struct {
		name string
		rows []rowScanner
	}{
		{name: "scope query", rows: []rowScanner{rowError(readErr)}},
		{name: "catalog unknown field", rows: []rowScanner{
			rowValues("project", tenantID, &organizationID, &projectID),
			rowValues([]byte(`[{"unknown":true}]`)),
		}},
		{name: "candidate trailing JSON", rows: []rowScanner{
			rowValues("project", tenantID, &organizationID, &projectID),
			rowValues(databaseCatalogFixture(t)),
			rowValues([]byte(`[] []`)),
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := []rowScanner{rowValues(tenantID), rowValues(tenantID)}
			rows = append(rows, test.rows...)
			transaction := &fakeTransaction{rows: rows}
			connection := newFakeConnection(transaction)
			connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
			runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
			runner.clock = func() time.Time { return now }
			err := runner.WithTenantRead(context.Background(), tenantID, func(ctx context.Context, capability TenantReadCapability) error {
				_, authorizeErr := capability.Authorize(ctx, request)
				return authorizeErr
			})
			if err == nil {
				t.Fatal("authorization fault returned nil")
			}
			if transaction.rollbackCalls != 1 || transaction.commitCalls != 0 {
				t.Fatalf("commit/rollback calls = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
			}
			assertConnectionDisposition(t, connection, 1, 0)
		})
	}
}

func TestTenantAuthorizationCandidateLimitFailsClosed(t *testing.T) {
	tenantID := "tenant-alpha"
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	organizationID := "organization-alpha"
	projectID := "project-alpha"
	var template []map[string]any
	if err := json.Unmarshal(databaseCandidateFixture(t, subject, digest, nil), &template); err != nil {
		t.Fatal(err)
	}
	rows := make([]map[string]any, 257)
	for index := range rows {
		row := deepCopyJSON(t, template[0])
		row["membership"].(map[string]any)["uid"] = "membership-" + zeroPadded(index)
		rows[index] = row
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID),
		rowValues("project", tenantID, &organizationID, &projectID),
		rowValues(databaseCatalogFixture(t)), rowValues(raw),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.clock = func() time.Time { return now }
	err = runner.WithTenantRead(context.Background(), tenantID, func(ctx context.Context, capability TenantReadCapability) error {
		_, authorizeErr := capability.Authorize(ctx, authz.Request{Subject: subject, Permission: "projects.get", Resource: authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}})
		return authorizeErr
	})
	if err == nil || !strings.Contains(err.Error(), "candidate limit") {
		t.Fatalf("candidate limit error = %v", err)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func databaseCatalogFixture(t *testing.T) []byte {
	t.Helper()
	var source struct {
		Roles []struct {
			Name        string   `json:"name"`
			Version     int64    `json:"version"`
			ScopeLevel  string   `json:"scopeLevel"`
			State       string   `json:"state"`
			Permissions []string `json:"permissions"`
		} `json:"roles"`
	}
	readRepositoryJSON(t, "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json", &source)
	rows := make([]map[string]any, len(source.Roles))
	for index, role := range source.Roles {
		rows[index] = map[string]any{
			"name": role.Name, "version": role.Version, "catalog_revision": int64(1),
			"scope_level": role.ScopeLevel, "state": role.State,
			"published_at": "2026-08-17T00:00:00Z", "permissions": role.Permissions,
		}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func databaseCandidateFixture(
	t *testing.T,
	subject authz.SubjectRef,
	digest string,
	expiresAt *time.Time,
) []byte {
	t.Helper()
	expiry := any(nil)
	if expiresAt != nil {
		expiry = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	scope := map[string]any{
		"level": "project", "tenant_id": "tenant-alpha",
		"organization_id": "organization-alpha", "project_id": "project-alpha",
	}
	rows := []map[string]any{{
		"membership": map[string]any{
			"uid": "membership-alpha", "subject_kind": subject.Kind,
			"subject_issuer": subject.Issuer, "subject_value": subject.Subject,
			"subject_digest": digest, "scope": scope, "state": "active", "expires_at": expiry,
		},
		"binding": map[string]any{
			"uid": "role-binding-alpha", "subject_kind": subject.Kind,
			"subject_issuer": subject.Issuer, "subject_value": subject.Subject,
			"subject_digest": digest, "role_name": "project.viewer", "role_version": int64(1),
			"scope": scope, "state": "active", "expires_at": expiry,
		},
	}}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func readRepositoryJSON(t *testing.T, relative string, target any) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", filepath.FromSlash(relative))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}

func deepCopyJSON(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var copy map[string]any
	if err := json.Unmarshal(raw, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func zeroPadded(value int) string {
	result := "000" + strconv.Itoa(value)
	return result[len(result)-3:]
}
