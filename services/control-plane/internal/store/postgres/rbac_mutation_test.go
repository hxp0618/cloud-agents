package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRBACMutationServiceCreateUsesOneSerializableAuthorizedWrite(t *testing.T) {
	tenantID := "tenant-alpha"
	actor := mutationActor()
	actorDigest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	transaction := &fakeTransaction{rows: append(
		mutationAuthorizationRows(t, tenantID, actor, actorDigest, authz.ScopeTenant, tenantID),
		rowValues("membership-new", int64(8), "active"),
	)}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.clock = func() time.Time { return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) }
	service, err := newRBACMutationService(runner)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.CreateMembership(context.Background(), tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 7,
		MembershipUID:          "membership-new",
		MembershipName:         "membership-new",
		Subject:                actor,
		Scope:                  authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID},
		AuditFactUID:           "audit-membership-new",
		ReasonCode:             "operator-request",
	})
	if err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}
	if result != (MutationResult{TenantID: tenantID, ResourceUID: "membership-new", ResourceVersion: 8, State: "active"}) {
		t.Fatalf("CreateMembership() result = %#v", result)
	}
	if len(transaction.queries) != 6 || transaction.queries[5].sql != createMembershipSQL {
		t.Fatalf("mutation query trace = %#v", transaction.queries)
	}
	if got := transaction.queries[5].arguments; len(got) != 12 || got[0] != tenantID || got[1] != int64(7) || got[2] != "membership-new" || got[7] != "tenant" || got[8] != tenantID {
		t.Fatalf("create function arguments = %#v", got)
	}
	if len(connection.beginOptions) != 1 || connection.beginOptions[0] != (pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite, DeferrableMode: pgx.NotDeferrable}) {
		t.Fatalf("mutation transaction options = %#v", connection.beginOptions)
	}
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 {
		t.Fatalf("commit/rollback = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestRBACMutationServiceAllFiveMethodsUseClosedFunctionSet(t *testing.T) {
	tenantID := "tenant-alpha"
	actor := mutationActor()
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		wantSQL   string
		invoke    func(*RBACMutationService) (MutationResult, error)
		wantState string
		wantUID   string
	}{
		{
			name: "suspend membership", wantSQL: suspendMembershipSQL, wantState: "suspended", wantUID: "membership-target",
			invoke: func(service *RBACMutationService) (MutationResult, error) {
				return service.SuspendMembership(context.Background(), tenantID, actor, MembershipTransitionInput{
					ExpectedTenantRevision: 7, MembershipUID: "membership-target", ExpectedResourceVersion: 6,
					AuditFactUID: "audit-suspend", ReasonCode: "operator-request",
				})
			},
		},
		{
			name: "revoke membership", wantSQL: revokeMembershipSQL, wantState: "revoked", wantUID: "membership-target",
			invoke: func(service *RBACMutationService) (MutationResult, error) {
				return service.RevokeMembership(context.Background(), tenantID, actor, MembershipTransitionInput{
					ExpectedTenantRevision: 7, MembershipUID: "membership-target", ExpectedResourceVersion: 6,
					AuditFactUID: "audit-revoke", ReasonCode: "operator-request",
				})
			},
		},
		{
			name: "bind role", wantSQL: bindRoleSQL, wantState: "active", wantUID: "role-binding-new",
			invoke: func(service *RBACMutationService) (MutationResult, error) {
				return service.BindRole(context.Background(), tenantID, actor, BindRoleInput{
					ExpectedTenantRevision: 7, RoleBindingUID: "role-binding-new", RoleBindingName: "role-binding-new",
					Subject: actor, RoleName: "tenant.admin", RoleVersion: 1,
					Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, AuditFactUID: "audit-bind", ReasonCode: "operator-request",
				})
			},
		},
		{
			name: "revoke role binding", wantSQL: revokeRoleBindingSQL, wantState: "revoked", wantUID: "role-binding-target",
			invoke: func(service *RBACMutationService) (MutationResult, error) {
				return service.RevokeRoleBinding(context.Background(), tenantID, actor, RevokeRoleBindingInput{
					ExpectedTenantRevision: 7, RoleBindingUID: "role-binding-target", ExpectedResourceVersion: 6,
					AuditFactUID: "audit-binding-revoke", ReasonCode: "operator-request",
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uid := test.wantUID
			transactionRows := mutationAuthorizationRows(t, tenantID, actor, digest, authz.ScopeTenant, tenantID)
			if strings.Contains(test.name, "membership") && !strings.Contains(test.name, "bind") {
				transactionRows = append(
					transactionRows[:2],
					append([]rowScanner{rowValues("tenant", stringPtr(tenantID))}, transactionRows[2:]...)...,
				)
			} else if strings.Contains(test.name, "revoke role") {
				transactionRows = append(
					transactionRows[:2],
					append([]rowScanner{rowValues("tenant", stringPtr(tenantID))}, transactionRows[2:]...)...,
				)
			}
			transactionRows = append(transactionRows, rowValues(uid, int64(8), test.wantState))
			transaction := &fakeTransaction{rows: transactionRows}
			connection := newFakeConnection(transaction)
			connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
			runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
			runner.clock = func() time.Time { return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) }
			service, err := newRBACMutationService(runner)
			if err != nil {
				t.Fatal(err)
			}
			result, err := test.invoke(service)
			if err != nil {
				t.Fatalf("mutation error = %v", err)
			}
			if result.ResourceUID != uid || result.ResourceVersion != 8 || result.State != test.wantState {
				t.Fatalf("result = %#v", result)
			}
			if transaction.queries[len(transaction.queries)-1].sql != test.wantSQL {
				t.Fatalf("last query = %q, want %q", transaction.queries[len(transaction.queries)-1].sql, test.wantSQL)
			}
			assertConnectionDisposition(t, connection, 1, 0)
		})
	}
}

func TestRBACMutationServiceDenialAndInputAreFailClosed(t *testing.T) {
	tenantID := "tenant-alpha"
	actor := mutationActor()
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID),
		rowValues("tenant", tenantID, (*string)(nil), (*string)(nil)),
		rowValues(databaseCatalogFixture(t)), rowValues([]byte("[]")),
	}}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	runner.clock = func() time.Time { return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) }
	service, err := newRBACMutationService(runner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateMembership(context.Background(), tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 7, MembershipUID: "membership-new", MembershipName: "membership-new",
		Subject: actor, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, AuditFactUID: "audit-new", ReasonCode: "operator-request",
	})
	if !errors.Is(err, ErrMutationDenied) {
		t.Fatalf("denied mutation error = %v, want ErrMutationDenied", err)
	}
	if transaction.commitCalls != 0 || transaction.rollbackCalls != 1 || len(transaction.queries) != 5 {
		t.Fatalf("denied transaction commit/rollback/queries = %d/%d/%d", transaction.commitCalls, transaction.rollbackCalls, len(transaction.queries))
	}

	pool := &fakePool{}
	invalidRunner := newTenantTransactionRunner(pool, time.Second)
	invalidService, err := newRBACMutationService(invalidRunner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = invalidService.CreateMembership(context.Background(), tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 7, MembershipUID: "bad/uid", MembershipName: "membership-new",
		Subject: actor, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, AuditFactUID: "audit-new", ReasonCode: "operator-request",
	})
	if !errors.Is(err, ErrMutationInvalidInput) || pool.acquireCalls != 0 {
		t.Fatalf("invalid mutation error/acquires = %v/%d", err, pool.acquireCalls)
	}
}

func TestRBACMutationServiceRejectsTargetAndResultDriftBeforeCommit(t *testing.T) {
	tenantID := "tenant-alpha"
	actor := mutationActor()
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}

	missingTransaction := &fakeTransaction{rows: []rowScanner{
		rowValues(tenantID), rowValues(tenantID), rowError(pgx.ErrNoRows),
	}}
	missingConnection := newFakeConnection(missingTransaction)
	missingConnection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	missingService, err := newRBACMutationService(newTenantTransactionRunner(&fakePool{connection: missingConnection}, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, err = missingService.SuspendMembership(context.Background(), tenantID, actor, MembershipTransitionInput{
		ExpectedTenantRevision: 7, MembershipUID: "missing-membership", ExpectedResourceVersion: 6,
		AuditFactUID: "audit-missing", ReasonCode: "operator-request",
	})
	if !errors.Is(err, ErrMutationTargetNotFound) || len(missingTransaction.queries) != 3 || missingTransaction.commitCalls != 0 || missingTransaction.rollbackCalls != 1 {
		t.Fatalf("missing target error/queries/commit/rollback = %v/%d/%d/%d", err, len(missingTransaction.queries), missingTransaction.commitCalls, missingTransaction.rollbackCalls)
	}
	assertConnectionDisposition(t, missingConnection, 1, 0)

	for _, test := range []struct {
		name string
		row  rowScanner
	}{
		{name: "resource uid", row: rowValues("other-membership", int64(8), "active")},
		{name: "tenant revision", row: rowValues("membership-new", int64(9), "active")},
		{name: "resource state", row: rowValues("membership-new", int64(8), "suspended")},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: append(
				mutationAuthorizationRows(t, tenantID, actor, digest, authz.ScopeTenant, tenantID),
				test.row,
			)}
			connection := newFakeConnection(transaction)
			connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
			service, serviceErr := newRBACMutationService(newTenantTransactionRunner(&fakePool{connection: connection}, time.Second))
			if serviceErr != nil {
				t.Fatal(serviceErr)
			}
			_, mutationErr := service.CreateMembership(context.Background(), tenantID, actor, CreateMembershipInput{
				ExpectedTenantRevision: 7, MembershipUID: "membership-new", MembershipName: "membership-new",
				Subject: actor, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID},
				AuditFactUID: "audit-new", ReasonCode: "operator-request",
			})
			if !errors.Is(mutationErr, ErrMutationResultDrift) || transaction.commitCalls != 0 || transaction.rollbackCalls != 1 {
				t.Fatalf("result drift error/commit/rollback = %v/%d/%d", mutationErr, transaction.commitCalls, transaction.rollbackCalls)
			}
			assertConnectionDisposition(t, connection, 1, 0)
		})
	}
}

func TestRBACMutationServiceMapsConflictAndUnknownCommit(t *testing.T) {
	tenantID := "tenant-alpha"
	actor := mutationActor()
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	pgErr := &pgconn.PgError{Code: "40001", Message: "compare and swap"}
	transaction := &fakeTransaction{rows: append(
		mutationAuthorizationRows(t, tenantID, actor, digest, authz.ScopeTenant, tenantID)[:5],
		rowError(pgErr),
	)}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
	service, err := newRBACMutationService(runner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateMembership(context.Background(), tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 7, MembershipUID: "membership-new", MembershipName: "membership-new",
		Subject: actor, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, AuditFactUID: "audit-new", ReasonCode: "operator-request",
	})
	if !errors.Is(err, ErrMutationConflict) || transaction.rollbackCalls != 1 {
		t.Fatalf("conflict error/rollback = %v/%d", err, transaction.rollbackCalls)
	}

	unknownTransaction := &fakeTransaction{rows: append(
		mutationAuthorizationRows(t, tenantID, actor, digest, authz.ScopeTenant, tenantID),
		rowValues("membership-new", int64(8), "active"),
	), commitErr: errors.New("ack lost")}
	unknownConnection := newFakeConnection(unknownTransaction)
	unknownConnection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	unknownRunner := newTenantTransactionRunner(&fakePool{connection: unknownConnection}, time.Second)
	unknownService, err := newRBACMutationService(unknownRunner)
	if err != nil {
		t.Fatal(err)
	}
	unknownResult, err := unknownService.CreateMembership(context.Background(), tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 7, MembershipUID: "membership-new", MembershipName: "membership-new",
		Subject: actor, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, AuditFactUID: "audit-new", ReasonCode: "operator-request",
	})
	if !errors.Is(err, ErrMutationCommitUnknown) || unknownResult != (MutationResult{}) || unknownConnection.hijackCalls != 1 {
		t.Fatalf("unknown commit result/error/hijack = %#v/%v/%d", unknownResult, err, unknownConnection.hijackCalls)
	}
}

func TestRBACMutationServiceReturnsNoResultWhenCommitFails(t *testing.T) {
	tenantID := "tenant-alpha"
	actor := mutationActor()
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	transaction := &fakeTransaction{
		rows: append(
			mutationAuthorizationRows(t, tenantID, actor, digest, authz.ScopeTenant, tenantID),
			rowValues("membership-new", int64(8), "active"),
		),
		commitErr: &pgconn.PgError{Code: "40001", Message: "serialization failure at commit"},
	}
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	service, err := newRBACMutationService(newTenantTransactionRunner(&fakePool{connection: connection}, time.Second))
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.CreateMembership(context.Background(), tenantID, actor, CreateMembershipInput{
		ExpectedTenantRevision: 7, MembershipUID: "membership-new", MembershipName: "membership-new",
		Subject: actor, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, AuditFactUID: "audit-new", ReasonCode: "operator-request",
	})
	if !errors.Is(err, ErrMutationConflict) || result != (MutationResult{}) || connection.hijackCalls != 1 {
		t.Fatalf("failed commit result/error/hijack = %#v/%v/%d", result, err, connection.hijackCalls)
	}
}

func TestRBACMutationServiceKeepsConfirmedCommitWhenConnectionIsDiscarded(t *testing.T) {
	tenantID := "tenant-alpha"
	actor := mutationActor()
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		configure func(*fakeConnection, *fakeTransaction)
	}{
		{
			name: "non-idle post-commit status",
			configure: func(_ *fakeConnection, transaction *fakeTransaction) {
				transaction.commitStatus = 'T'
			},
		},
		{
			name: "cleared tenant check failed",
			configure: func(connection *fakeConnection, _ *fakeTransaction) {
				connection.outsideRows = []rowScanner{rowError(errors.New("clear check failed"))}
			},
		},
		{
			name: "tenant setting remained",
			configure: func(connection *fakeConnection, _ *fakeTransaction) {
				leaked := tenantID
				connection.outsideRows = []rowScanner{rowValues(&leaked)}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: append(
				mutationAuthorizationRows(t, tenantID, actor, digest, authz.ScopeTenant, tenantID),
				rowValues("membership-new", int64(8), "active"),
			)}
			connection := newFakeConnection(transaction)
			connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
			test.configure(connection, transaction)
			runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
			service, serviceErr := newRBACMutationService(runner)
			if serviceErr != nil {
				t.Fatal(serviceErr)
			}

			result, mutationErr := service.CreateMembership(context.Background(), tenantID, actor, CreateMembershipInput{
				ExpectedTenantRevision: 7, MembershipUID: "membership-new", MembershipName: "membership-new",
				Subject: actor, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID},
				AuditFactUID: "audit-new", ReasonCode: "operator-request",
			})
			if mutationErr != nil || result != (MutationResult{TenantID: tenantID, ResourceUID: "membership-new", ResourceVersion: 8, State: "active"}) {
				t.Fatalf("confirmed mutation result/error = %#v/%v", result, mutationErr)
			}
			assertConnectionDisposition(t, connection, 0, 1)
		})
	}
}

func mutationActor() authz.SubjectRef {
	return authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
}

func stringPtr(value string) *string { return &value }

func mutationAuthorizationRows(
	t *testing.T,
	tenantID string,
	actor authz.SubjectRef,
	digest string,
	scopeLevel authz.ScopeLevel,
	scopeID string,
) []rowScanner {
	t.Helper()
	organizationID := (*string)(nil)
	projectID := (*string)(nil)
	if scopeLevel == authz.ScopeOrganization {
		organizationID = stringPtr(scopeID)
	}
	if scopeLevel == authz.ScopeProject {
		projectID = stringPtr(scopeID)
	}
	return []rowScanner{
		rowValues(tenantID),
		rowValues(tenantID),
		rowValues(string(scopeLevel), tenantID, organizationID, projectID),
		rowValues(databaseCatalogFixture(t)),
		rowValues(databaseTenantAdminCandidateFixture(t, tenantID, actor, digest)),
	}
}

func databaseTenantAdminCandidateFixture(
	t *testing.T,
	tenantID string,
	subject authz.SubjectRef,
	digest string,
) []byte {
	t.Helper()
	return []byte(fmt.Sprintf(`[{"membership":{"uid":"membership-actor","subject_kind":"%s","subject_issuer":"%s","subject_value":"%s","subject_digest":"%s","scope":{"level":"tenant","tenant_id":"%s","organization_id":null,"project_id":null},"state":"active","expires_at":null},"binding":{"uid":"role-binding-actor","subject_kind":"%s","subject_issuer":"%s","subject_value":"%s","subject_digest":"%s","role_name":"tenant.admin","role_version":1,"scope":{"level":"tenant","tenant_id":"%s","organization_id":null,"project_id":null},"state":"active","expires_at":null}}]`, subject.Kind, subject.Issuer, subject.Subject, digest, tenantID, subject.Kind, subject.Issuer, subject.Subject, digest, tenantID))
}
