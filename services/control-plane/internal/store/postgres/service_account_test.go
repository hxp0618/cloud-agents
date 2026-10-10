package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5"
)

func TestServiceAccountCreateCommitsMembershipBindingCredentialTogether(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	const tenantID = "tenant-alpha"
	const rawCredential = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	digest, err := browserauth.ProofDigest(rawCredential)
	if err != nil {
		t.Fatal(err)
	}
	identifiers := []string{
		"00000000000000000000000000000001",
		"00000000000000000000000000000002",
		"00000000000000000000000000000003",
		"00000000000000000000000000000004",
		"00000000000000000000000000000005",
	}
	membershipPrincipal, bindingPrincipal, _, actor := testServiceAccountPrincipals(t, now, tenantID, "user-operator")
	actorDigest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	createdAt := now.Add(time.Second)
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID),
		rowValues("tenant", tenantID, (*string)(nil), (*string)(nil)),
		rowValues(databaseCatalogVersionFixture(t, 2)),
		rowValues(serviceAccountTenantAdminCandidate(t, actor, actorDigest)),
		rowError(pgx.ErrNoRows),
		rowValues(int64(7)),
		rowValues(identifiers[0], int64(8), authz.MembershipActive),
		rowValues(identifiers[1], int64(9), authz.BindingActive),
		rowValues("service-ci", int64(1), "serviceAccount", actor.Issuer, "service-service-ci", createdAt, createdAt),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.application = "admin"
	runner.clock = func() time.Time { return now }
	store, err := newServiceAccountStore(runner)
	if err != nil {
		t.Fatal(err)
	}
	store.newProof = func() (string, [32]byte, error) { return rawCredential, digest, nil }
	identifierIndex := 0
	store.newIdentifier = func() (string, error) {
		identifier := identifiers[identifierIndex]
		identifierIndex++
		return identifier, nil
	}

	result, err := store.Create(context.Background(), tenantID, membershipPrincipal, bindingPrincipal, api.ServiceAccountCreateRequest{
		ServiceAccountID: "service-ci", DisplayName: "CI alpha", Application: api.IdentityApplicationAdmin,
		RoleName: "tenant.admin", ScopeLevel: "tenant", ScopeID: tenantID,
	}, "request-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if result.Credential != rawCredential || result.ServiceAccount.ID != "service-ci" ||
		result.ServiceAccount.Subject.Subject != "service-service-ci" || result.ServiceAccount.ResourceVersion != "1" ||
		result.CredentialExpiresAt != now.Add(serviceAccountCredentialLifetime).Format(time.RFC3339Nano) {
		t.Fatalf("created service account = %#v", result)
	}
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 || len(transaction.queries) != 10 {
		t.Fatalf("settlement/query count = %d/%d/%d", transaction.commitCalls, transaction.rollbackCalls, len(transaction.queries))
	}
	for index, statement := range []string{
		bindTenantSQL, readTenantSQL, resolveAuthorizationScopeSQL, readBuiltinRoleCatalogSQL,
		readAuthorizationCandidatesSQL, `SELECT user_id, subject_kind, subject_issuer, subject_value, subject_digest
		FROM cloud_agents_identity.read_platform_admin($1, $2, $3)`, readServiceAccountTenantRevisionSQL,
		createMembershipSQL, bindRoleSQL, createServiceAccountRecordSQL,
	} {
		if transaction.queries[index].sql != statement {
			t.Fatalf("query %d = %q", index, transaction.queries[index].sql)
		}
	}
	lastArguments := transaction.queries[9].arguments
	if len(lastArguments) != 17 || lastArguments[10].([]byte)[0] != digest[0] || lastArguments[15] != identifiers[4] || lastArguments[16] != "request-alpha" {
		t.Fatalf("record arguments = %#v", lastArguments)
	}
}

func TestServiceAccountCreateNeverLeaksCredentialOnUnknownCommit(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	const tenantID = "tenant-alpha"
	const rawCredential = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	digest, _ := browserauth.ProofDigest(rawCredential)
	membershipPrincipal, bindingPrincipal, _, actor := testServiceAccountPrincipals(t, now, tenantID, "user-operator")
	actorDigest, _ := actor.Digest()
	transaction := &fakeTransaction{commitErr: errors.New("commit acknowledgement lost"), rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID),
		rowValues("tenant", tenantID, (*string)(nil), (*string)(nil)),
		rowValues(databaseCatalogVersionFixture(t, 2)),
		rowValues(serviceAccountTenantAdminCandidate(t, actor, actorDigest)), rowError(pgx.ErrNoRows),
		rowValues(int64(7)), rowValues("00000000000000000000000000000001", int64(8), "active"),
		rowValues("00000000000000000000000000000002", int64(9), "active"),
		rowValues("service-ci", int64(1), "serviceAccount", actor.Issuer, "service-service-ci", now, now),
	}}
	connection := newFakeConnection(transaction)
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.application = "admin"
	runner.clock = func() time.Time { return now }
	store, _ := newServiceAccountStore(runner)
	store.newProof = func() (string, [32]byte, error) { return rawCredential, digest, nil }
	identifiers := []string{
		"00000000000000000000000000000001",
		"00000000000000000000000000000002",
		"00000000000000000000000000000003",
		"00000000000000000000000000000004",
		"00000000000000000000000000000005",
	}
	next := 0
	store.newIdentifier = func() (string, error) {
		identifier := identifiers[next]
		next++
		return identifier, nil
	}
	result, err := store.Create(context.Background(), tenantID, membershipPrincipal, bindingPrincipal, api.ServiceAccountCreateRequest{
		ServiceAccountID: "service-ci", DisplayName: "CI alpha", Application: api.IdentityApplicationAdmin,
		RoleName: "tenant.admin", ScopeLevel: "tenant", ScopeID: tenantID,
	}, "request-alpha")
	if !errors.Is(err, ErrMutationCommitUnknown) || result != (api.ServiceAccountCreated{}) || transaction.commitCalls != 1 || connection.hijackCalls != 1 {
		t.Fatalf("result/error/commit/hijack = %#v/%v/%d/%d", result, err, transaction.commitCalls, connection.hijackCalls)
	}
}

func TestServiceAccountCreateDenialByServiceAccountCommitsSeparateAudit(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	const tenantID = "tenant-alpha"
	const rawCredential = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	digest, _ := browserauth.ProofDigest(rawCredential)
	membershipPrincipal, bindingPrincipal, _, actor := testServiceAccountPrincipalsForKind(t, now, tenantID, "serviceAccount", "service-viewer")
	actorDigest, _ := actor.Digest()
	denied := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID),
		rowValues("tenant", tenantID, (*string)(nil), (*string)(nil)),
		rowValues(databaseCatalogVersionFixture(t, 2)),
		rowValues(databaseCandidateFixture(t, actor, actorDigest, nil)),
		rowError(pgx.ErrNoRows),
	}}
	auditEventID := "00000000000000000000000000000006"
	audit := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID), rowValues(auditEventID),
	}}
	connection := newFakeConnection(denied, audit)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil)), rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.application = "admin"
	runner.clock = func() time.Time { return now }
	store, err := newServiceAccountStore(runner)
	if err != nil {
		t.Fatal(err)
	}
	store.newProof = func() (string, [32]byte, error) { return rawCredential, digest, nil }
	identifiers := []string{
		"00000000000000000000000000000001",
		"00000000000000000000000000000002",
		"00000000000000000000000000000003",
		"00000000000000000000000000000004",
		"00000000000000000000000000000005",
		auditEventID,
	}
	identifierIndex := 0
	store.newIdentifier = func() (string, error) {
		identifier := identifiers[identifierIndex]
		identifierIndex++
		return identifier, nil
	}

	created, err := store.Create(context.Background(), tenantID, membershipPrincipal, bindingPrincipal, api.ServiceAccountCreateRequest{
		ServiceAccountID: "service-ci", DisplayName: "CI alpha", Application: api.IdentityApplicationAdmin,
		RoleName: "tenant.admin", ScopeLevel: "tenant", ScopeID: tenantID,
	}, "request-alpha")
	if !errors.Is(err, ErrMutationDenied) || created != (api.ServiceAccountCreated{}) {
		t.Fatalf("created/error = %#v/%v; denial=%#v audit=%#v outside=%#v begin=%#v", created, err, denied.queries, audit.queries, connection.outsideQueries, connection.beginOptions)
	}
	if denied.rollbackCalls != 1 || denied.commitCalls != 0 || audit.commitCalls != 1 || audit.rollbackCalls != 0 {
		t.Fatalf("denial/audit settlement = %d/%d %d/%d", denied.commitCalls, denied.rollbackCalls, audit.commitCalls, audit.rollbackCalls)
	}
	if len(audit.queries) != 3 || audit.queries[2].sql != recordServiceAccountDenialSQL {
		t.Fatalf("audit queries = %#v", audit.queries)
	}
	arguments := audit.queries[2].arguments
	if len(arguments) != 10 || arguments[0] != tenantID || arguments[1] != "service-ci" ||
		arguments[2] != "create" || arguments[3] != "authorization_denied" ||
		arguments[4] != "admin" || arguments[5] != actor.Kind || arguments[6] != actor.Issuer ||
		arguments[7] != actor.Subject || arguments[8] != auditEventID || arguments[9] != "request-alpha" {
		t.Fatalf("audit arguments = %#v", arguments)
	}
}

func TestServiceAccountPermissionDenialAuditFailureIsUnavailable(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	const tenantID = "tenant-alpha"
	_, _, identityPrincipal, _ := testServiceAccountPrincipalsForKind(t, now, tenantID, "serviceAccount", "service-viewer")
	audit := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID), rowError(errors.New("private audit storage failure")),
	}}
	connection := newFakeConnection(audit)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.application = "admin"
	runner.clock = func() time.Time { return now }
	store, err := newServiceAccountStore(runner)
	if err != nil {
		t.Fatal(err)
	}
	store.newIdentifier = func() (string, error) { return "00000000000000000000000000000008", nil }
	err = store.RecordPermissionDenial(
		context.Background(), tenantID, "service-ci", "create", api.IdentityApplicationAdmin,
		authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, identityPrincipal, "request-audit-failure",
	)
	if !errors.Is(err, ErrServiceAccountAuditUnavailable) || audit.commitCalls != 0 || audit.rollbackCalls != 1 {
		t.Fatalf("error/commit/rollback = %v/%d/%d", err, audit.commitCalls, audit.rollbackCalls)
	}
}

func TestServiceAccountManagementActorIsHumanOnly(t *testing.T) {
	issuer := "https://identity.example.test"
	for _, test := range []struct {
		actor authz.SubjectRef
		want  bool
	}{
		{authz.SubjectRef{Kind: "user", Issuer: issuer, Subject: "user-admin"}, true},
		{authz.SubjectRef{Kind: "serviceAccount", Issuer: issuer, Subject: "service-admin"}, false},
		{authz.SubjectRef{Kind: "workload", Issuer: issuer, Subject: "worker-admin"}, false},
	} {
		if got := validServiceAccountManagementActor(test.actor); got != test.want {
			t.Fatalf("actor=%#v allowed=%v want=%v", test.actor, got, test.want)
		}
	}
}

func TestServiceAccountPermissionDenialConsumesExactIdentityProof(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	const tenantID = "tenant-alpha"
	_, _, identityPrincipal, actor := testServiceAccountPrincipals(t, now, tenantID, "user-operator")
	auditEventID := "00000000000000000000000000000007"
	audit := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID), rowValues(auditEventID),
	}}
	connection := newFakeConnection(audit)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.application = "admin"
	runner.clock = func() time.Time { return now }
	store, err := newServiceAccountStore(runner)
	if err != nil {
		t.Fatal(err)
	}
	store.newIdentifier = func() (string, error) { return auditEventID, nil }
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
	if err := store.RecordPermissionDenial(context.Background(), tenantID, "service-ci", "create", api.IdentityApplicationAdmin, scope, identityPrincipal, "request-viewer-denial"); err != nil {
		t.Fatal(err)
	}
	if audit.commitCalls != 1 || audit.rollbackCalls != 0 || len(audit.queries) != 3 || audit.queries[2].sql != recordServiceAccountDenialSQL {
		t.Fatalf("audit settlement/queries = %d/%d/%#v", audit.commitCalls, audit.rollbackCalls, audit.queries)
	}
	arguments := audit.queries[2].arguments
	if len(arguments) != 10 || arguments[0] != tenantID || arguments[1] != "service-ci" ||
		arguments[2] != "create" || arguments[3] != "authorization_denied" || arguments[4] != "admin" ||
		arguments[5] != actor.Kind || arguments[6] != actor.Issuer || arguments[7] != actor.Subject ||
		arguments[8] != auditEventID || arguments[9] != "request-viewer-denial" {
		t.Fatalf("audit arguments = %#v", arguments)
	}
}
