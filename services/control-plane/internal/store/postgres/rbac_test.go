package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

func TestGlobalRoleReadIsFixedByStoreApplication(t *testing.T) {
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.test", Subject: "user-1"}
	digest, _ := subject.Digest()
	for _, app := range []string{"", "user", "admin"} {
		transaction := &fakeTransaction{rows: []rowScanner{rowValues("1", subject.Kind, subject.Issuer, subject.Subject, digest)}}
		handle := &tenantReadHandle{active: true, transaction: transaction, tenantID: "tenant-1", application: app}
		binding, err := handle.readGlobalRoleBinding(context.Background(), subject)
		if err != nil {
			t.Fatal(err)
		}
		if app != "admin" {
			if binding != nil || len(transaction.queries) != 0 {
				t.Fatal("user store queried global administration authority")
			}
			continue
		}
		if binding == nil || binding.Subject != subject || binding.SubjectHash != digest || binding.RoleName != "platform.admin" || binding.RoleVersion != 2 {
			t.Fatal("admin global binding was not projected exactly")
		}
		transaction.rows = []rowScanner{rowError(pgx.ErrNoRows)}
		if binding, err := handle.readGlobalRoleBinding(context.Background(), subject); err != nil || binding != nil {
			t.Fatal("revoked global binding was retained")
		}
	}
}

func TestRBACFactReadersUseExactBoundedQueries(t *testing.T) {
	tenantID := "tenant-alpha"
	subject := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	organizationID := "organization-alpha"
	projectID := "project-alpha"
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues("project", tenantID, &organizationID, &projectID),
		rowValues(databaseCatalogFixture(t)),
		rowValues(databaseCandidateFixture(t, subject, digest, nil)),
	}}
	handle := &tenantReadHandle{active: true, transaction: transaction, tenantID: tenantID, clock: time.Now, application: "user"}

	scope, resolved, err := handle.resolveAuthorizationScope(context.Background(), authz.ScopeRef{Level: authz.ScopeProject, ID: projectID})
	if err != nil || !resolved || scope.ProjectID != projectID || scope.OrganizationID != organizationID {
		t.Fatalf("resolved scope/error = %#v/%v", scope, err)
	}
	catalog, err := handle.readBuiltinRoleCatalog(context.Background())
	if err != nil || len(catalog.Roles) != 7 {
		t.Fatalf("catalog roles/error = %d/%v", len(catalog.Roles), err)
	}
	candidates, err := handle.readAuthorizationCandidates(context.Background(), subject)
	if err != nil || len(candidates) != 1 || candidates[0].Membership.Subject != subject {
		t.Fatalf("candidates/error = %#v/%v", candidates, err)
	}
	if len(transaction.queries) != 3 || transaction.queries[0].sql != resolveAuthorizationScopeSQL ||
		transaction.queries[1].sql != readBuiltinRoleCatalogSQL || transaction.queries[2].sql != readAuthorizationCandidatesSQL {
		t.Fatalf("query identities = %#v", transaction.queries)
	}
	if got := transaction.queries[2].arguments; len(got) != 3 || got[0] != subject.Kind || got[1] != subject.Issuer || got[2] != subject.Subject {
		t.Fatalf("candidate arguments = %#v", got)
	}
	if strings.Contains(readAuthorizationCandidatesSQL, "binding.subject_digest = membership.subject_digest") ||
		strings.Contains(readAuthorizationCandidatesSQL, "membership.resource_version < binding.resource_version") ||
		!strings.Contains(readAuthorizationCandidatesSQL, "membership_admission.change_kind = 'created'") ||
		!strings.Contains(readAuthorizationCandidatesSQL, "membership_admission.resource_version < binding.resource_version") {
		t.Fatal("candidate query no longer exposes digest drift or creation ordering")
	}
}

func TestRBACResourceReadsStayTenantAndScopeBound(t *testing.T) {
	for name, statement := range map[string]string{
		"membership":   getMembershipSQL,
		"role binding": getRoleBindingSQL,
	} {
		if !strings.Contains(statement, "tenant_id = cloud_agents.require_tenant_id()") ||
			!strings.Contains(statement, "scope_level = $2") ||
			!strings.Contains(statement, "END = $3") {
			t.Fatalf("%s query is not tenant and stored-scope bound: %s", name, statement)
		}
	}
	for name, scope := range map[string]authz.ScopeRef{
		"platform":        {Level: authz.ScopePlatform},
		"tenant mismatch": {Level: authz.ScopeTenant, ID: "other-tenant"},
	} {
		if err := validateStoredRBACRead("tenant-alpha", "resource-alpha", scope); err == nil {
			t.Fatalf("%s scope was accepted", name)
		}
	}
}

func TestRBACFactReadersRejectMalformedAndOversizedJSON(t *testing.T) {
	handle := &tenantReadHandle{active: true, tenantID: "tenant-alpha", clock: time.Now}
	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{name: "unknown catalog member", raw: []byte(`[{"unknown":true}]`)},
		{name: "trailing candidate value", raw: []byte(`[] []`)},
		{name: "oversized", raw: make([]byte, maxCandidateJSONBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			handle.transaction = &fakeTransaction{rows: []rowScanner{rowValues(test.raw)}}
			var err error
			if strings.Contains(test.name, "catalog") {
				_, err = handle.readBuiltinRoleCatalog(context.Background())
			} else {
				_, err = handle.readAuthorizationCandidates(context.Background(), authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"})
			}
			if err == nil {
				t.Fatal("malformed database JSON was accepted")
			}
		})
	}
}

func TestRBACProductionSurfaceHasNoStandaloneAuthorizeOrRawActor(t *testing.T) {
	files := []string{"tenant_transaction.go", "rbac.go", "rbac_mutation.go"}
	fileSet := token.NewFileSet()
	for _, name := range files {
		parsed, err := parser.ParseFile(fileSet, name, nil, parser.AllErrors)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			function, ok := node.(*ast.FuncDecl)
			if !ok {
				return true
			}
			if function.Name.Name == "Authorize" || function.Name.Name == "authorizeMutation" {
				t.Errorf("forbidden standalone authorization function %s remains", function.Name.Name)
			}
			if function.Name.Name == "CreateMembership" || function.Name.Name == "ResumeMembership" || function.Name.Name == "SuspendMembership" ||
				function.Name.Name == "RevokeMembership" || function.Name.Name == "BindRole" || function.Name.Name == "RevokeRoleBinding" {
				if function.Type.Params == nil || len(function.Type.Params.List) != 4 {
					t.Errorf("%s parameter shape drift", function.Name.Name)
					return true
				}
				pointer, ok := function.Type.Params.List[2].Type.(*ast.StarExpr)
				if !ok {
					t.Errorf("%s third parameter is not a pointer", function.Name.Name)
					return true
				}
				selector, selectorOK := pointer.X.(*ast.SelectorExpr)
				if !selectorOK {
					t.Errorf("%s third parameter is not qualified", function.Name.Name)
					return true
				}
				packageName, packageOK := selector.X.(*ast.Ident)
				if !packageOK || packageName.Name != "authn" || selector.Sel.Name != "VerifiedPrincipal" {
					t.Errorf("%s third parameter is not *authn.VerifiedPrincipal", function.Name.Name)
				}
			}
			return true
		})
	}
	var _ *authn.VerifiedPrincipal
}

func databaseCatalogFixture(t *testing.T) []byte {
	return databaseCatalogVersionFixture(t, 1)
}

func databaseCatalogVersionFixture(t *testing.T, version int64) []byte {
	t.Helper()
	var source struct {
		CatalogRevision string `json:"catalogRevision"`
		PublishedAt     string `json:"publishedAt"`
		Roles           []struct {
			Name        string   `json:"name"`
			Version     int64    `json:"version"`
			ScopeLevel  string   `json:"scopeLevel"`
			State       string   `json:"state"`
			Permissions []string `json:"permissions"`
		} `json:"roles"`
	}
	readRepositoryJSON(t, fmt.Sprintf("contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v%d.json", version), &source)
	rows := make([]map[string]any, len(source.Roles))
	for index, role := range source.Roles {
		catalogRevision := int64(1)
		publishedAt := "2026-08-17T00:00:00Z"
		if source.CatalogRevision == "2" && role.Name == "platform.admin" {
			catalogRevision = 2
			publishedAt = source.PublishedAt
		}
		rows[index] = map[string]any{
			"name": role.Name, "version": role.Version, "catalog_revision": catalogRevision,
			"scope_level": role.ScopeLevel, "state": role.State,
			"published_at": publishedAt, "permissions": role.Permissions,
		}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func databaseCandidateFixture(t *testing.T, subject authz.SubjectRef, digest string, expiresAt *time.Time) []byte {
	return databaseProjectCandidateFixture(t, subject, digest, "project-alpha", expiresAt)
}

func databaseProjectCandidateFixture(t *testing.T, subject authz.SubjectRef, digest, projectID string, expiresAt *time.Time) []byte {
	t.Helper()
	expiry := any(nil)
	if expiresAt != nil {
		expiry = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	scope := map[string]any{"level": "project", "tenant_id": "tenant-alpha", "organization_id": "organization-alpha", "project_id": projectID}
	rows := []map[string]any{{
		"membership": map[string]any{"uid": "membership-alpha", "subject_kind": subject.Kind, "subject_issuer": subject.Issuer, "subject_value": subject.Subject, "subject_digest": digest, "scope": scope, "state": "active", "expires_at": expiry},
		"binding":    map[string]any{"uid": "role-binding-alpha", "subject_kind": subject.Kind, "subject_issuer": subject.Issuer, "subject_value": subject.Subject, "subject_digest": digest, "role_name": "project.viewer", "role_version": int64(1), "scope": scope, "state": "active", "expires_at": expiry},
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

func stringPtr(value string) *string { return &value }
