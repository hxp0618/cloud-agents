package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRBACMutationTypedKernelsUseClosedFunctionSet(t *testing.T) {
	tenantID := "tenant-alpha"
	target := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-target"}
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
	tests := []struct {
		name      string
		wantSQL   string
		wantUID   string
		wantState string
		invoke    func(context.Context, *tenantReadHandle) (MutationResult, error)
	}{
		{
			name: "create membership", wantSQL: createMembershipSQL, wantUID: "membership-new", wantState: authz.MembershipActive,
			invoke: func(ctx context.Context, handle *tenantReadHandle) (MutationResult, error) {
				return createMembershipInTransaction(ctx, handle, tenantID, CreateMembershipInput{
					ExpectedTenantRevision: 7, MembershipUID: "membership-new", MembershipName: "membership-new",
					Subject: target, Scope: scope, AuditFactUID: "audit-create", ReasonCode: "operator-request",
				})
			},
		},
		{
			name: "suspend membership", wantSQL: suspendMembershipSQL, wantUID: "membership-target", wantState: authz.MembershipSuspended,
			invoke: func(ctx context.Context, handle *tenantReadHandle) (MutationResult, error) {
				return transitionMembershipInTransaction(ctx, handle, tenantID, MembershipTransitionInput{
					ExpectedTenantRevision: 7, MembershipUID: "membership-target", ExpectedResourceVersion: 6,
					AuditFactUID: "audit-suspend", ReasonCode: "operator-request",
				}, suspendMembershipSQL, authz.MembershipSuspended)
			},
		},
		{
			name: "resume membership", wantSQL: resumeMembershipSQL, wantUID: "membership-target", wantState: authz.MembershipActive,
			invoke: func(ctx context.Context, handle *tenantReadHandle) (MutationResult, error) {
				return transitionMembershipInTransaction(ctx, handle, tenantID, MembershipTransitionInput{
					ExpectedTenantRevision: 7, MembershipUID: "membership-target", ExpectedResourceVersion: 6,
					AuditFactUID: "audit-resume", ReasonCode: "operator-request",
				}, resumeMembershipSQL, authz.MembershipActive)
			},
		},
		{
			name: "revoke membership", wantSQL: revokeMembershipSQL, wantUID: "membership-target", wantState: authz.MembershipRevoked,
			invoke: func(ctx context.Context, handle *tenantReadHandle) (MutationResult, error) {
				return transitionMembershipInTransaction(ctx, handle, tenantID, MembershipTransitionInput{
					ExpectedTenantRevision: 7, MembershipUID: "membership-target", ExpectedResourceVersion: 6,
					AuditFactUID: "audit-revoke", ReasonCode: "operator-request",
				}, revokeMembershipSQL, authz.MembershipRevoked)
			},
		},
		{
			name: "bind role", wantSQL: bindRoleSQL, wantUID: "binding-new", wantState: authz.BindingActive,
			invoke: func(ctx context.Context, handle *tenantReadHandle) (MutationResult, error) {
				return bindRoleInTransaction(ctx, handle, tenantID, BindRoleInput{
					ExpectedTenantRevision: 7, RoleBindingUID: "binding-new", RoleBindingName: "binding-new",
					Subject: target, RoleName: "tenant.admin", RoleVersion: 1, Scope: scope,
					AuditFactUID: "audit-bind", ReasonCode: "operator-request",
				})
			},
		},
		{
			name: "revoke role binding", wantSQL: revokeRoleBindingSQL, wantUID: "binding-target", wantState: authz.BindingRevoked,
			invoke: func(ctx context.Context, handle *tenantReadHandle) (MutationResult, error) {
				return revokeRoleBindingInTransaction(ctx, handle, tenantID, RevokeRoleBindingInput{
					ExpectedTenantRevision: 7, RoleBindingUID: "binding-target", ExpectedResourceVersion: 6,
					AuditFactUID: "audit-binding-revoke", ReasonCode: "operator-request",
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: []rowScanner{rowValues(test.wantUID, int64(8), test.wantState)}}
			handle := &tenantReadHandle{active: true, transaction: transaction, tenantID: tenantID, clock: time.Now}
			result, err := test.invoke(context.Background(), handle)
			if err != nil || result != (MutationResult{TenantID: tenantID, ResourceUID: test.wantUID, ResourceVersion: 8, State: test.wantState}) {
				t.Fatalf("result/error = %#v/%v", result, err)
			}
			if len(transaction.queries) != 1 || transaction.queries[0].sql != test.wantSQL {
				t.Fatalf("typed SQL trace = %#v", transaction.queries)
			}
		})
	}
}

func TestRBACMutationTypedKernelSettlement(t *testing.T) {
	tenantID := "tenant-alpha"
	input := CreateMembershipInput{
		ExpectedTenantRevision: 7, MembershipUID: "membership-new", MembershipName: "membership-new",
		Subject: authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "target"},
		Scope:   authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, AuditFactUID: "audit-create", ReasonCode: "operator-request",
	}
	callbackErr := errors.New("protected operation failed")
	for _, test := range []struct {
		name         string
		resultRow    rowScanner
		commitErr    error
		wantErr      error
		wantCommit   int
		wantRollback int
		wantHijack   int
	}{
		{name: "confirmed commit", resultRow: rowValues("membership-new", int64(8), "active"), wantCommit: 1},
		{name: "callback rollback", resultRow: rowError(callbackErr), wantErr: callbackErr, wantRollback: 1},
		{name: "serialization commit", resultRow: rowValues("membership-new", int64(8), "active"), commitErr: &pgconn.PgError{Code: "40001"}, wantErr: ErrMutationConflict, wantCommit: 1, wantHijack: 1},
		{name: "unknown commit", resultRow: rowValues("membership-new", int64(8), "active"), commitErr: errors.New("ack lost"), wantErr: ErrMutationCommitUnknown, wantCommit: 1, wantHijack: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: []rowScanner{rowValues(tenantID), rowValues(tenantID), test.resultRow}, commitErr: test.commitErr}
			connection := newFakeConnection(transaction)
			connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
			runner := newTenantTransactionRunner(&fakePool{connection: connection}, time.Second)
			var result MutationResult
			err := runner.withTenantMutation(context.Background(), tenantID, func(handle *tenantReadHandle) error {
				var operationErr error
				result, operationErr = createMembershipInTransaction(context.Background(), handle, tenantID, input)
				return operationErr
			})
			result, err = settledMutationResult(result, err)
			if !errors.Is(err, test.wantErr) || transaction.commitCalls != test.wantCommit || transaction.rollbackCalls != test.wantRollback || connection.hijackCalls != test.wantHijack {
				t.Fatalf("result/error/commit/rollback/hijack = %#v/%v/%d/%d/%d", result, err, transaction.commitCalls, transaction.rollbackCalls, connection.hijackCalls)
			}
			if test.wantErr != nil && result != (MutationResult{}) {
				t.Fatalf("failed settlement leaked result %#v", result)
			}
			if len(connection.beginOptions) != 1 || connection.beginOptions[0] != (pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite, DeferrableMode: pgx.NotDeferrable}) {
				t.Fatalf("transaction options = %#v", connection.beginOptions)
			}
		})
	}
}

func TestRBACMutationValidationAndDatabaseErrorMapping(t *testing.T) {
	zero := time.Time{}
	if !validMutationIdentifier("tenant-alpha") || validMutationIdentifier("bad/tenant") || validMutationExpiry(&zero, time.Now()) {
		t.Fatal("mutation lexical validation drift")
	}
	if !errors.Is(mapMutationDatabaseError("create", &pgconn.PgError{Code: "23505"}), ErrMutationConflict) ||
		!errors.Is(mapMutationDatabaseError("create", &pgconn.PgError{Code: "42501"}), ErrMutationAuthority) ||
		!errors.Is(mapVerifiedMutationError(authz.ErrOperationDenied), ErrMutationDenied) {
		t.Fatal("mutation typed error mapping drift")
	}
}
