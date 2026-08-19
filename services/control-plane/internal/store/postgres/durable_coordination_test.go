package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	coordinationTenant  = "tenant-alpha"
	coordinationSubject = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	coordinationPayload = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestDurableCoordinationClaimUsesOnlyGeneratedProfileFunction(t *testing.T) {
	expires := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	transaction := authorizedCoordinationTransaction(t, rowValues(
		"created", stringPtr("pending"), (*string)(nil), (*int64)(nil), (*string)(nil), (*string)(nil),
		(*int64)(nil), (*string)(nil), timePtr(expires),
	))
	service, connection := coordinationService(t, transaction)
	result, err := service.ClaimIdempotency(context.Background(), coordinationTenant, IdempotencyClaimInput{
		Profile: coordination.ManagedAgentCreateProject(), Actor: coordinationActor(), Request: coordinationProjectRequest(),
		IdempotencyKey: "idempotency-key-0001",
		AuditFactID:    "audit-idempotency-claim",
	})
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.Disposition != "created" ||
		result.ReplayState != "pending" || !result.ExpiresAt.Equal(expires) {
		t.Fatalf("claim result/error = %#v / %v", result, err)
	}
	if len(transaction.queries) != 6 || transaction.queries[5].sql != claimManagedAgentCreateProjectIdempotencySQL {
		t.Fatalf("claim query trace = %#v", transaction.queries)
	}
	arguments := transaction.queries[5].arguments
	wantSubject, err := coordinationActor().Digest()
	if err != nil {
		t.Fatal(err)
	}
	intent, err := coordination.BindManagedAgentCreateProject(
		coordination.ManagedAgentCreateProject(), coordinationTenant, coordinationProjectRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(arguments) != 5 || arguments[0] != coordinationTenant || arguments[1] != wantSubject ||
		arguments[2] != "idempotency-key-0001" || arguments[3] != intent.RequestDigest() ||
		arguments[4] != "audit-idempotency-claim" {
		t.Fatalf("claim arguments = %#v", arguments)
	}
	assertCoordinationCommitted(t, transaction, connection)
}

func TestDurableCoordinationClaimReturnsClosedReplayAndConflict(t *testing.T) {
	expires := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	resourceKind := "project"
	resourceID := "project-alpha"
	resourceVersion := int64(7)
	tests := []struct {
		name        string
		disposition string
		wantOutcome DatabaseOutcome
	}{
		{name: "terminal replay", disposition: "replay", wantOutcome: DatabaseCommitted},
		{name: "digest conflict", disposition: "conflict", wantOutcome: DatabaseRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := []any{test.disposition, stringPtr("succeeded"), (*string)(nil), (*int64)(nil), &resourceKind, &resourceID,
				&resourceVersion, (*string)(nil), &expires}
			if test.disposition == "conflict" {
				values = []any{test.disposition, (*string)(nil), (*string)(nil), (*int64)(nil), (*string)(nil),
					(*string)(nil), (*int64)(nil), (*string)(nil), (*time.Time)(nil)}
			}
			transaction := authorizedCoordinationTransaction(t, rowValues(values...))
			service, _ := coordinationService(t, transaction)
			result, err := service.ClaimIdempotency(context.Background(), coordinationTenant, IdempotencyClaimInput{
				Profile: coordination.ManagedAgentCreateProject(), Actor: coordinationActor(), Request: coordinationProjectRequest(),
				IdempotencyKey: "idempotency-key-0001",
				AuditFactID:    "audit-idempotency-replay",
			})
			if err != nil || result.DatabaseOutcome != test.wantOutcome || result.Disposition != test.disposition {
				t.Fatalf("replay result/error = %#v / %v", result, err)
			}
			if test.disposition == "replay" && (result.ResourceID == nil || *result.ResourceID != resourceID) {
				t.Fatalf("terminal replay result = %#v", result)
			}
			if test.disposition == "conflict" && result != (IdempotencyClaimResult{DatabaseOutcome: DatabaseRejected, Disposition: "conflict"}) {
				t.Fatalf("conflict leaked envelope = %#v", result)
			}
		})
	}
}

func TestDurableCoordinationClaimConvertsSerializationFailureToClosedRejection(t *testing.T) {
	transaction := authorizedCoordinationTransaction(t, rowError(&pgconn.PgError{Code: "40001", Message: "serialization"}))
	service, _ := coordinationService(t, transaction)
	result, err := service.ClaimIdempotency(context.Background(), coordinationTenant, IdempotencyClaimInput{
		Profile: coordination.ManagedAgentCreateProject(), Actor: coordinationActor(), Request: coordinationProjectRequest(),
		IdempotencyKey: "idempotency-key-0001",
		AuditFactID:    "audit-idempotency-claim",
	})
	if err != nil || result != (IdempotencyClaimResult{DatabaseOutcome: DatabaseRejected}) ||
		transaction.rollbackCalls != 1 || transaction.commitCalls != 0 {
		t.Fatalf("serialization result/error/settlement = %#v / %v / %d/%d", result, err, transaction.commitCalls, transaction.rollbackCalls)
	}
}

func TestDurableCoordinationCompletionHasClosedCommitOutcomes(t *testing.T) {
	success := IdempotencySuccessInput{
		Profile: coordination.ManagedAgentCreateProject(), Actor: coordinationActor(), Request: coordinationProjectRequest(),
		IdempotencyKey: "idempotency-key-0001",
		ResourceID:     "project-alpha", ResourceVersion: 7, EventID: "event-project-alpha",
		PayloadDigest: coordinationPayload, AuditFactID: "audit-idempotency-success",
	}
	tests := []struct {
		name        string
		commitErr   error
		wantOutcome DatabaseOutcome
	}{
		{name: "confirmed", wantOutcome: DatabaseCommitted},
		{name: "rejected", commitErr: &pgconn.PgError{Code: "40001", Message: "serialization"}, wantOutcome: DatabaseRejected},
		{name: "unknown", commitErr: errors.New("commit response lost"), wantOutcome: DatabaseUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := authorizedCoordinationTransaction(t, rowValues(
				"succeeded", "project", "project-alpha", int64(7), "event-project-alpha", "pending",
			))
			transaction.commitErr = test.commitErr
			service, connection := coordinationService(t, transaction)
			result, err := service.CompleteIdempotencySuccess(context.Background(), coordinationTenant, success)
			if err != nil || result.DatabaseOutcome != test.wantOutcome {
				t.Fatalf("completion result/error = %#v / %v", result, err)
			}
			if test.wantOutcome != DatabaseCommitted && result != (IdempotencySuccessResult{DatabaseOutcome: test.wantOutcome}) {
				t.Fatalf("non-committed result leaked = %#v", result)
			}
			if test.commitErr != nil && connection.hijackCalls != 1 {
				t.Fatalf("unknown/rejected connection hijacks = %d", connection.hijackCalls)
			}
		})
	}
}

func TestDurableCoordinationFailureAndResultDriftFailClosed(t *testing.T) {
	transaction := authorizedCoordinationTransaction(t, rowValues("failed", "stable.failure"))
	service, _ := coordinationService(t, transaction)
	result, err := service.CompleteIdempotencyFailure(context.Background(), coordinationTenant, IdempotencyFailureInput{
		Profile: coordination.ManagedAgentCreateProject(), Actor: coordinationActor(), Request: coordinationProjectRequest(),
		IdempotencyKey:  "idempotency-key-0001",
		StableErrorCode: "stable.failure", AuditFactID: "audit-idempotency-failure",
	})
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.ReplayState != "failed" {
		t.Fatalf("failure result/error = %#v / %v", result, err)
	}

	driftTransaction := authorizedCoordinationTransaction(t, rowValues("failed", "different.failure"))
	driftService, _ := coordinationService(t, driftTransaction)
	_, err = driftService.CompleteIdempotencyFailure(context.Background(), coordinationTenant, IdempotencyFailureInput{
		Profile: coordination.ManagedAgentCreateProject(), Actor: coordinationActor(), Request: coordinationProjectRequest(),
		IdempotencyKey:  "idempotency-key-0001",
		StableErrorCode: "stable.failure", AuditFactID: "audit-idempotency-failure",
	})
	if !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("drift error = %v", err)
	}
}

func TestDurableCoordinationLeaderUsesGlobalSerializableBoundary(t *testing.T) {
	expires := time.Date(2026, 8, 19, 0, 1, 0, 0, time.UTC)
	token := int64(7)
	transaction := &fakeTransaction{rows: []rowScanner{rowValues("acquired", &token, &expires)}}
	service, connection := coordinationService(t, transaction)
	result, err := service.AcquireLeader(context.Background(), LeaderLeaseInput{
		LeaderName: "outbox-dispatcher", HolderID: "holder-alpha", HolderIncarnation: "incarnation-alpha", LeaseSeconds: 60,
	})
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.FencingToken != 7 || !result.LeaseExpiresAt.Equal(expires) {
		t.Fatalf("leader result/error = %#v / %v", result, err)
	}
	if len(transaction.queries) != 1 || transaction.queries[0].sql != acquireCoordinationLeaderSQL {
		t.Fatalf("global transaction queries = %#v", transaction.queries)
	}
	if len(connection.beginOptions) != 1 || connection.beginOptions[0] != (pgx.TxOptions{
		IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite, DeferrableMode: pgx.NotDeferrable,
	}) {
		t.Fatalf("leader tx options = %#v", connection.beginOptions)
	}
}

func TestDurableCoordinationLeaderReturnsClosedBusyRejectedAndUnknownOutcomes(t *testing.T) {
	expires := time.Date(2026, 8, 19, 0, 1, 0, 0, time.UTC)
	token := int64(7)
	tests := []struct {
		name        string
		operation   rowScanner
		commitErr   error
		renew       bool
		wantOutcome DatabaseOutcome
	}{
		{name: "acquire busy", operation: rowValues("busy", &token, &expires), wantOutcome: DatabaseRejected},
		{name: "renew rejected", operation: rowValues("rejected", (*int64)(nil), (*time.Time)(nil)), renew: true, wantOutcome: DatabaseRejected},
		{name: "commit unknown", operation: rowValues("acquired", &token, &expires), commitErr: errors.New("commit response lost"), wantOutcome: DatabaseUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: []rowScanner{test.operation}, commitErr: test.commitErr}
			service, _ := coordinationService(t, transaction)
			input := LeaderLeaseInput{
				LeaderName: "outbox-dispatcher", HolderID: "holder-alpha", HolderIncarnation: "incarnation-alpha",
				FencingToken: token, LeaseSeconds: 60,
			}
			var result LeaderLeaseResult
			var err error
			if test.renew {
				result, err = service.RenewLeader(context.Background(), input)
			} else {
				result, err = service.AcquireLeader(context.Background(), input)
			}
			if err != nil || result.DatabaseOutcome != test.wantOutcome {
				t.Fatalf("leader result/error = %#v / %v", result, err)
			}
			if test.wantOutcome == DatabaseUnknown && result != (LeaderLeaseResult{DatabaseOutcome: DatabaseUnknown}) {
				t.Fatalf("unknown result leaked = %#v", result)
			}
		})
	}
}

func TestDurableCoordinationOutboxBindsFullClaimTuple(t *testing.T) {
	resourceVersion := int64(7)
	expires := time.Date(2026, 8, 19, 0, 1, 0, 0, time.UTC)
	claimTransaction := coordinationTransaction(rowValues(
		"event-project-alpha", "managedAgentCreateProject/v1alpha1",
		coordination.ManagedAgentCreateProject().ProfileDigest(),
		"resource_change", "project", "project-alpha", int64(7), &resourceVersion, int64(0),
		(*string)(nil), (*int64)(nil), coordinationPayload, int32(1), expires,
	))
	service, _ := coordinationService(t, claimTransaction)
	claimResult, err := service.ClaimOutbox(context.Background(), coordinationTenant, OutboxClaimInput{
		HolderID: "holder-alpha", HolderIncarnation: "incarnation-alpha", ClaimToken: "claim-token-alpha",
		LeaseSeconds: 60, SubjectDigest: coordinationSubject, AuditFactID: "audit-outbox-claim",
	})
	if err != nil || claimResult.DatabaseOutcome != DatabaseCommitted || !claimResult.Found ||
		!validOutboxClaim(claimResult.Claim) {
		t.Fatalf("outbox claim/error = %#v / %v", claimResult, err)
	}

	settlementTransaction := coordinationTransaction(rowValues(
		"event-project-alpha", "retry_wait", int32(1), timePtr(expires.Add(time.Second)),
	))
	settlementService, _ := coordinationService(t, settlementTransaction)
	settled, err := settlementService.RetryOutbox(context.Background(), coordinationTenant, OutboxSettlementInput{
		Claim: claimResult.Claim, SubjectDigest: coordinationSubject, AuditFactID: "audit-outbox-retry",
	})
	if err != nil || settled.DatabaseOutcome != DatabaseCommitted || settled.State != "retry_wait" {
		t.Fatalf("outbox retry/error = %#v / %v", settled, err)
	}
	if settlementTransaction.queries[2].sql != retryOutboxEventSQL {
		t.Fatalf("retry statement = %q", settlementTransaction.queries[2].sql)
	}
	arguments := settlementTransaction.queries[2].arguments
	if len(arguments) != 8 || arguments[1] != claimResult.Claim.EventID ||
		arguments[2] != claimResult.Claim.HolderID || arguments[3] != claimResult.Claim.HolderIncarnation ||
		arguments[4] != claimResult.Claim.ClaimToken || arguments[5] != claimResult.Claim.ClaimExpiresAt ||
		arguments[6] != coordinationSubject || arguments[7] != "audit-outbox-retry" {
		t.Fatalf("settlement full tuple = %#v", arguments)
	}
}

func TestDurableCoordinationRejectsZeroProfileBeforeDatabase(t *testing.T) {
	transaction := authorizedCoordinationTransaction(t, rowValues(
		"created", stringPtr("pending"), (*string)(nil), (*int64)(nil), (*string)(nil), (*string)(nil),
		(*int64)(nil), (*string)(nil), timePtr(time.Now()),
	))
	service, connection := coordinationService(t, transaction)
	_, err := service.ClaimIdempotency(context.Background(), coordinationTenant, IdempotencyClaimInput{
		Actor: coordinationActor(), Request: coordinationProjectRequest(), IdempotencyKey: "idempotency-key-0001",
		AuditFactID: "audit-idempotency-claim",
	})
	if !errors.Is(err, ErrCoordinationInvalidInput) || connection.beginOptions != nil {
		t.Fatalf("zero profile error/database use = %v / %#v", err, connection.beginOptions)
	}
}

func TestDurableCoordinationAuthorizesGeneratedProfileInSameTransaction(t *testing.T) {
	t.Run("denied actor", func(t *testing.T) {
		actor := coordinationActor()
		digest, err := actor.Digest()
		if err != nil {
			t.Fatal(err)
		}
		rows := mutationAuthorizationRows(t, coordinationTenant, actor, digest, authz.ScopeOrganization, "organization-alpha")
		rows[len(rows)-1] = rowValues([]byte("[]"))
		transaction := &fakeTransaction{rows: append(rows, rowValues(
			"created", stringPtr("pending"), (*string)(nil), (*int64)(nil), (*string)(nil), (*string)(nil),
			(*int64)(nil), (*string)(nil), timePtr(time.Now()),
		))}
		service, _ := coordinationService(t, transaction)
		_, err = service.ClaimIdempotency(context.Background(), coordinationTenant, IdempotencyClaimInput{
			Profile: coordination.ManagedAgentCreateProject(), Actor: actor, Request: coordinationProjectRequest(),
			IdempotencyKey: "idempotency-key-0001",
			AuditFactID:    "audit-idempotency-denied",
		})
		if !errors.Is(err, ErrMutationDenied) || len(transaction.queries) != 5 ||
			transaction.rollbackCalls != 1 || transaction.commitCalls != 0 {
			t.Fatalf("denied error/query/settlement = %v / %d / %d/%d", err, len(transaction.queries), transaction.commitCalls, transaction.rollbackCalls)
		}
	})

	t.Run("profile-excluded unicode organization scope", func(t *testing.T) {
		transaction := authorizedCoordinationTransaction(t, rowValues(
			"created", stringPtr("pending"), (*string)(nil), (*int64)(nil), (*string)(nil), (*string)(nil),
			(*int64)(nil), (*string)(nil), timePtr(time.Now()),
		))
		service, connection := coordinationService(t, transaction)
		_, err := service.ClaimIdempotency(context.Background(), coordinationTenant, IdempotencyClaimInput{
			Profile: coordination.ManagedAgentCreateProject(), Actor: coordinationActor(),
			Request: func() coordination.ManagedAgentCreateProjectRequest {
				request := coordinationProjectRequest()
				request.OrganizationRef.ID = "organization-café"
				return request
			}(),
			IdempotencyKey: "idempotency-key-0001",
			AuditFactID:    "audit-idempotency-wrong-scope",
		})
		if !errors.Is(err, ErrCoordinationInvalidInput) || connection.beginOptions != nil {
			t.Fatalf("wrong scope error/database use = %v / %#v", err, connection.beginOptions)
		}
	})
}

type fixedOutboxPort struct {
	result outboxDeliveryResult
	calls  int
}

func (port *fixedOutboxPort) deliver(_ context.Context, _ OutboxClaim) outboxDeliveryResult {
	port.calls++
	return port.result
}

func TestOutboxDispatcherUsesOnlyInjectedPortAndSettlesAfterDelivery(t *testing.T) {
	resourceVersion := int64(7)
	expires := time.Date(2026, 8, 19, 0, 1, 0, 0, time.UTC)
	claimTransaction := coordinationTransaction(rowValues(
		"event-project-alpha", "managedAgentCreateProject/v1alpha1",
		coordination.ManagedAgentCreateProject().ProfileDigest(),
		"resource_change", "project", "project-alpha", int64(7), &resourceVersion, int64(0),
		(*string)(nil), (*int64)(nil), coordinationPayload, int32(1), expires,
	))
	settlementTransaction := coordinationTransaction(rowValues(
		"event-project-alpha", "delivered", int32(1), (*time.Time)(nil),
	))
	connection := newFakeConnection(claimTransaction, settlementTransaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil)), rowValues((*string)(nil))}
	service, err := newDurableCoordinationService(newTenantTransactionRunner(&fakePool{connection: connection}, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	port := &fixedOutboxPort{result: outboxDeliveryResult{disposition: outboxDelivered}}
	dispatcher, err := newOutboxDispatcher(service, port)
	if err != nil {
		t.Fatal(err)
	}
	result, err := dispatcher.dispatchOne(context.Background(), coordinationTenant, OutboxClaimInput{
		HolderID: "holder-alpha", HolderIncarnation: "incarnation-alpha", ClaimToken: "claim-token-alpha",
		LeaseSeconds: 60, SubjectDigest: coordinationSubject, AuditFactID: "audit-outbox-claim",
	}, "audit-outbox-delivered")
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.State != "delivered" || port.calls != 1 {
		t.Fatalf("dispatch result/error/calls = %#v / %v / %d", result, err, port.calls)
	}
}

func coordinationTransaction(operation rowScanner) *fakeTransaction {
	return &fakeTransaction{rows: []rowScanner{
		rowValues(coordinationTenant), rowValues(coordinationTenant), operation,
	}}
}

func authorizedCoordinationTransaction(t *testing.T, operation rowScanner) *fakeTransaction {
	t.Helper()
	actor := coordinationActor()
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return &fakeTransaction{rows: append(
		mutationAuthorizationRows(t, coordinationTenant, actor, digest, authz.ScopeOrganization, "organization-alpha"),
		operation,
	)}
}

func coordinationActor() authz.SubjectRef {
	return authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-admin"}
}

func coordinationScope() authz.ScopeRef {
	return authz.ScopeRef{Level: authz.ScopeOrganization, ID: "organization-alpha"}
}

func coordinationProjectRequest() coordination.ManagedAgentCreateProjectRequest {
	return coordination.ManagedAgentCreateProjectRequest{
		Name: "project-alpha",
		OrganizationRef: coordination.OrganizationRef{
			Namespace: "cloud-agents",
			Kind:      "organization",
			ID:        coordinationScope().ID,
		},
		DisplayName: "Project Alpha",
	}
}

func coordinationService(t *testing.T, transaction *fakeTransaction) (*DurableCoordinationService, *fakeConnection) {
	t.Helper()
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	service, err := newDurableCoordinationService(newTenantTransactionRunner(&fakePool{connection: connection}, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return service, connection
}

func assertCoordinationCommitted(t *testing.T, transaction *fakeTransaction, connection *fakeConnection) {
	t.Helper()
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 {
		t.Fatalf("coordination commit/rollback = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func timePtr(value time.Time) *time.Time { return &value }
