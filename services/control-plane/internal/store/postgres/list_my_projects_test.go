package postgres

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
)

func TestExecuteMyProjectsSelectionFiltersWithRBACEvaluator(t *testing.T) {
	now := time.Now().UTC()
	principal, subject := testMyProjectsPrincipal(t, now, "tenant-alpha", "user-alpha")
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	projects := []Project{
		myProjectFixture(now, "project-alpha", "organization-alpha"),
		myProjectFixture(now, "project-beta", "organization-beta"),
	}
	raw, _ := json.Marshal(projects)
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues("tenant-alpha"), rowValues("tenant-alpha"),
		rowValues("tenant", "tenant-alpha", nil, nil),
		rowValues(databaseCatalogFixture(t)),
		rowValues(databaseProjectCandidateFixture(t, subject, digest, "project-alpha", nil)),
		rowValues(raw),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.clock = func() time.Time { return now }
	service, err := newDurableCoordinationService(runner)
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ListMyProjects(context.Background(), "tenant-alpha", principal, MyProjectsCursor{}, 50)
	if err != nil || len(page.Projects) != 1 || page.Projects[0].UID != "project-alpha" || page.NextProjectUID != "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if len(transaction.queries) != 6 || transaction.queries[2].sql != resolveAuthorizationScopeSQL ||
		transaction.queries[3].sql != readBuiltinRoleCatalogSQL || transaction.queries[4].sql != readAuthorizationCandidatesSQL ||
		transaction.queries[5].sql != listMyProjectsBatchSQL {
		t.Fatalf("queries=%#v", transaction.queries)
	}
}

func TestExecuteMyProjectsSelectionScansPastUnauthorizedRawBatch(t *testing.T) {
	now := time.Now().UTC()
	principal, subject := testMyProjectsPrincipal(t, now, "tenant-alpha", "user-alpha")
	digest, _ := subject.Digest()
	first := make([]Project, myProjectsScanBatchSize)
	for index := range first {
		first[index] = myProjectFixture(now, fmt.Sprintf("project-%03d", index), "organization-other")
	}
	second := []Project{myProjectFixture(now, "project-zzz", "organization-alpha")}
	firstRaw, _ := json.Marshal(first)
	secondRaw, _ := json.Marshal(second)
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues("tenant-alpha"), rowValues("tenant-alpha"),
		rowValues("tenant", "tenant-alpha", nil, nil),
		rowValues(databaseCatalogFixture(t)),
		rowValues(databaseProjectCandidateFixture(t, subject, digest, "project-zzz", nil)),
		rowValues(firstRaw), rowValues(secondRaw),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.clock = func() time.Time { return now }
	service, _ := newDurableCoordinationService(runner)
	page, err := service.ListMyProjects(context.Background(), "tenant-alpha", principal, MyProjectsCursor{}, 1)
	if err != nil || len(page.Projects) != 1 || page.Projects[0].UID != "project-zzz" || page.NextProjectUID != "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if len(transaction.queries) != 7 || transaction.queries[5].arguments[0] != "" || transaction.queries[6].arguments[0] != "project-200" {
		t.Fatalf("batch queries=%#v", transaction.queries)
	}
}

func TestMyProjectsRejectsCursorFromAnotherSubject(t *testing.T) {
	now := time.Now().UTC()
	principal, _ := testMyProjectsPrincipal(t, now, "tenant-alpha", "user-alpha")
	runner := newTenantTransactionRunner(&fakePool{}, time.Second)
	service, err := newDurableCoordinationService(runner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ListMyProjects(context.Background(), "tenant-alpha", principal, MyProjectsCursor{
		AfterProjectUID: "project-alpha",
		SubjectKey:      strings.Repeat("b", 64),
	}, 50)
	if err != ErrCoordinationInvalidInput {
		t.Fatalf("cursor error=%v", err)
	}
}

func TestExecuteMyProjectsSelectionRejectsRevokedFacts(t *testing.T) {
	now := time.Now().UTC()
	principal, subject := testMyProjectsPrincipal(t, now, "tenant-alpha", "user-alpha")
	digest, _ := subject.Digest()
	candidate := strings.ReplaceAll(string(databaseProjectCandidateFixture(t, subject, digest, "project-alpha", nil)), `"state":"active"`, `"state":"revoked"`)
	projectsRaw, _ := json.Marshal([]Project{myProjectFixture(now, "project-alpha", "organization-alpha")})
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues("tenant-alpha"), rowValues("tenant-alpha"),
		rowValues("tenant", "tenant-alpha", nil, nil),
		rowValues(databaseCatalogFixture(t)), rowValues([]byte(candidate)), rowValues(projectsRaw),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.clock = func() time.Time { return now }
	service, _ := newDurableCoordinationService(runner)
	page, err := service.ListMyProjects(context.Background(), "tenant-alpha", principal, MyProjectsCursor{}, 50)
	if err != nil || len(page.Projects) != 0 || page.NextProjectUID != "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}

func TestMyProjectsSQLAndCursorStayTenantSubjectAndActiveBound(t *testing.T) {
	for name, statement := range map[string]string{"cursor": myProjectsCursorIdentitySQL, "list": listMyProjectsBatchSQL} {
		for _, fragment := range []string{"cloud_agents.require_tenant_id()", "tenant.state = 'active'", "organization.state = 'active'", "project.state = 'active'"} {
			if !strings.Contains(statement, fragment) {
				t.Fatalf("%s SQL lost %q: %s", name, fragment, statement)
			}
		}
	}
	if !validSubjectCursorKey(strings.Repeat("a", 64)) || validSubjectCursorKey(strings.Repeat("A", 64)) || validSubjectCursorKey("short") {
		t.Fatal("subject cursor key validation drifted")
	}
	now := time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)
	raw, _ := json.Marshal([]Project{myProjectFixture(now, "project-alpha", "organization-alpha")})
	if _, err := decodeMyProjectsBatch(raw, "tenant-other"); err != ErrCoordinationResultDrift {
		t.Fatalf("cross-tenant batch error=%v", err)
	}
}

func myProjectFixture(now time.Time, projectID, organizationID string) Project {
	return Project{
		UID: projectID, Name: projectID, TenantID: "tenant-alpha", OrganizationID: organizationID,
		DisplayName: projectID, State: "active", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
}

func testMyProjectsPrincipal(t *testing.T, now time.Time, tenantID, subjectValue string) (*authn.VerifiedPrincipal, authz.SubjectRef) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const issuer = "https://my-projects.test"
	const audience = "https://my-projects.test/control-plane"
	jwk, _ := json.Marshal(map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": "my-projects-key",
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
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "my-projects-key", "typ": "at+jwt"})
	claims, _ := json.Marshal(map[string]any{
		"iss": issuer, "sub": subjectValue, "aud": audience, "exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(),
		"jti": "my-projects-token", "client_id": "my-projects-test", "scope": "projects.list",
		"https://schemas.cloud-agents.dev/claims/subject-kind":   "user",
		"https://schemas.cloud-agents.dev/claims/tenant-id":      tenantID,
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
	token := protected + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	principal, err := verifier.Verify(token, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: "tenant", ResourceID: tenantID, RequiredPermission: "projects.list",
	})
	if err != nil {
		t.Fatal(err)
	}
	return principal, authz.SubjectRef{Kind: "user", Issuer: issuer, Subject: subjectValue}
}
