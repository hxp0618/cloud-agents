package postgres

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
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

func TestDurableCoordinationJWTUserSurfaceRequiresVerifiedPrincipal(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "durable_coordination.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]bool{
		"IdempotencyClaimInput": false, "IdempotencySuccessInput": false, "IdempotencyFailureInput": false,
	}
	methods := map[string]bool{
		"ClaimIdempotency": false, "CompleteIdempotencySuccess": false, "CompleteIdempotencyFailure": false,
	}
	settlements := map[string]string{
		"ClaimIdempotency":           "settleIdempotencyClaim",
		"CompleteIdempotencySuccess": "settleIdempotencySuccess",
		"CompleteIdempotencyFailure": "settleIdempotencyFailure",
	}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.GenDecl:
			for _, specification := range declaration.Specs {
				typeSpecification, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if _, tracked := inputs[typeSpecification.Name.Name]; !tracked {
					continue
				}
				structure, ok := typeSpecification.Type.(*ast.StructType)
				if !ok {
					t.Fatalf("%s is not a struct", typeSpecification.Name.Name)
				}
				for _, field := range structure.Fields.List {
					for _, name := range field.Names {
						if name.Name == "Actor" {
							t.Fatalf("%s exposes raw Actor", typeSpecification.Name.Name)
						}
					}
				}
				inputs[typeSpecification.Name.Name] = true
			}
		case *ast.FuncDecl:
			if _, tracked := methods[declaration.Name.Name]; !tracked || declaration.Recv == nil {
				continue
			}
			if len(declaration.Type.Params.List) != 4 {
				t.Fatalf("%s params = %#v", declaration.Name.Name, declaration.Type.Params.List)
			}
			pointer, ok := declaration.Type.Params.List[2].Type.(*ast.StarExpr)
			if !ok {
				t.Fatalf("%s principal parameter is not a pointer", declaration.Name.Name)
			}
			selector, selectorOK := pointer.X.(*ast.SelectorExpr)
			if !selectorOK {
				t.Fatalf("%s principal parameter is not package-qualified", declaration.Name.Name)
			}
			packageName, packageOK := selector.X.(*ast.Ident)
			if !packageOK || packageName.Name != "authn" || selector.Sel.Name != "VerifiedPrincipal" {
				t.Fatalf("%s does not require *authn.VerifiedPrincipal", declaration.Name.Name)
			}
			var callback *ast.FuncLit
			ast.Inspect(declaration.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "WithVerifiedOperation" || len(call.Args) != 2 {
					return true
				}
				packageName, ok := selector.X.(*ast.Ident)
				if ok && packageName.Name == "authz" {
					callback, _ = call.Args[1].(*ast.FuncLit)
				}
				return true
			})
			if callback == nil {
				t.Fatalf("%s does not outer-wrap with authz.WithVerifiedOperation", declaration.Name.Name)
			}
			calls := make(map[string]int)
			ast.Inspect(callback.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch function := call.Fun.(type) {
				case *ast.Ident:
					calls[function.Name]++
				case *ast.SelectorExpr:
					calls[function.Sel.Name]++
				}
				return true
			})
			for _, required := range []string{"Bind", "withTenantMutation", "executeVerifiedRBACOperation", settlements[declaration.Name.Name]} {
				if calls[required] != 1 {
					t.Fatalf("%s callback call count %s = %d", declaration.Name.Name, required, calls[required])
				}
			}
			if calls["authorizeMutation"] != 0 || calls["bindAuthorizedProfile"] != 0 {
				t.Fatalf("%s retains a raw actor authorization bypass", declaration.Name.Name)
			}
			methods[declaration.Name.Name] = true
		}
	}
	for name, found := range inputs {
		if !found {
			t.Fatalf("input type %s not found", name)
		}
	}
	for name, found := range methods {
		if !found {
			t.Fatalf("method %s not found", name)
		}
	}
}

func TestDurableCoordinationClaimSettlementStaysClosed(t *testing.T) {
	expires := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	result, err := settleIdempotencyClaim(IdempotencyClaimResult{Disposition: "created"}, stringPtr("pending"), &expires, nil)
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.Disposition != "created" ||
		result.ReplayState != "pending" || !result.ExpiresAt.Equal(expires) {
		t.Fatalf("created result/error = %#v / %v", result, err)
	}
	conflict, err := settleIdempotencyClaim(IdempotencyClaimResult{Disposition: "conflict"}, nil, nil, nil)
	if err != nil || conflict != (IdempotencyClaimResult{DatabaseOutcome: DatabaseRejected, Disposition: "conflict"}) {
		t.Fatalf("conflict result/error = %#v / %v", conflict, err)
	}
	rejected, err := settleIdempotencyClaim(IdempotencyClaimResult{Disposition: "created"}, stringPtr("pending"), &expires,
		&pgconn.PgError{Code: "40001", Message: "serialization"})
	if err != nil || rejected != (IdempotencyClaimResult{DatabaseOutcome: DatabaseRejected}) {
		t.Fatalf("rejected result/error = %#v / %v", rejected, err)
	}
}

func TestCoordinationCommitConflictPreservesRejection(t *testing.T) {
	for _, code := range []string{"40001", "23505"} {
		raw := &pgconn.PgError{Code: code, Message: "private database diagnostic"}
		for _, err := range []error{raw, mapMutationDatabaseError("commit tenant mutation transaction", raw)} {
			for _, mapped := range []error{mapCoordinationDatabaseError("operation", err), mapDeploymentTargetError(err)} {
				if !errors.Is(mapped, ErrCoordinationRejected) {
					t.Fatalf("%s rejection mapped to %v", code, mapped)
				}
			}
		}
	}
	// A lost commit acknowledgement remains unknown, even if another wrapped
	// error is a conflict. Never invite blind replay of an uncertain commit.
	unknown := errors.Join(ErrMutationCommitUnknown, ErrMutationConflict)
	if err := mapCoordinationDatabaseError("operation", unknown); !errors.Is(err, ErrCoordinationCommitUnknown) || errors.Is(err, ErrCoordinationRejected) {
		t.Fatalf("unknown outcome = %v", err)
	}
	unrelated := errors.New("unrelated failure")
	if err := mapCoordinationDatabaseError("operation", unrelated); !errors.Is(err, unrelated) || errors.Is(err, ErrCoordinationRejected) {
		t.Fatalf("unrelated outcome = %v", err)
	}
}

func TestDurableCoordinationCompletionHasClosedCommitOutcomes(t *testing.T) {
	success := IdempotencySuccessInput{
		Profile: coordination.ManagedAgentCreateProject(), Request: coordinationProjectRequest(),
		IdempotencyKey: "idempotency-key-0001",
		ResourceID:     "project-alpha", ResourceVersion: 7, EventID: "event-project-alpha",
		PayloadDigest: coordinationPayload, AuditFactID: "audit-idempotency-success",
	}
	tests := []struct {
		name        string
		settleErr   error
		wantOutcome DatabaseOutcome
	}{
		{name: "confirmed", wantOutcome: DatabaseCommitted},
		{name: "rejected", settleErr: &pgconn.PgError{Code: "40001", Message: "serialization"}, wantOutcome: DatabaseRejected},
		{name: "unknown", settleErr: ErrMutationCommitUnknown, wantOutcome: DatabaseUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := settleIdempotencySuccess(IdempotencySuccessResult{
				ReplayState: "succeeded", ResourceKind: "project", ResourceID: "project-alpha",
				ResourceVersion: 7, OutboxEventID: "event-project-alpha", OutboxState: "pending",
			}, test.settleErr, success)
			if err != nil || result.DatabaseOutcome != test.wantOutcome {
				t.Fatalf("completion result/error = %#v / %v", result, err)
			}
			if test.wantOutcome != DatabaseCommitted && result != (IdempotencySuccessResult{DatabaseOutcome: test.wantOutcome}) {
				t.Fatalf("non-committed result leaked = %#v", result)
			}
		})
	}
}

func TestDurableCoordinationFailureAndResultDriftFailClosed(t *testing.T) {
	input := IdempotencyFailureInput{
		Profile: coordination.ManagedAgentCreateProject(), Request: coordinationProjectRequest(),
		IdempotencyKey:  "idempotency-key-0001",
		StableErrorCode: "stable.failure", AuditFactID: "audit-idempotency-failure",
	}
	result, err := settleIdempotencyFailure(IdempotencyFailureResult{ReplayState: "failed", StableErrorCode: "stable.failure"}, nil, input)
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.ReplayState != "failed" {
		t.Fatalf("failure result/error = %#v / %v", result, err)
	}
	_, err = settleIdempotencyFailure(IdempotencyFailureResult{ReplayState: "failed", StableErrorCode: "different.failure"}, nil, input)
	if !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("drift error = %v", err)
	}
}

func TestDurableCoordinationTypedJWTKernelsUseOnlyGeneratedFunctions(t *testing.T) {
	ctx := context.Background()
	request := coordinationProjectRequest()
	intent, err := coordination.BindManagedAgentCreateProject(
		coordination.ManagedAgentCreateProject(), coordinationTenant, request,
	)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	claimInput := IdempotencyClaimInput{
		Profile: coordination.ManagedAgentCreateProject(), Request: request,
		IdempotencyKey: "idempotency-key-0001", AuditFactID: "audit-idempotency-claim",
	}
	claimTransaction := &fakeTransaction{rows: []rowScanner{rowValues(
		"created", stringPtr("pending"), (*string)(nil), (*int64)(nil), (*string)(nil), (*string)(nil),
		(*int64)(nil), (*string)(nil), &expires,
	)}}
	claim, replayState, claimExpiresAt, err := claimIdempotencyTransaction(
		ctx, &tenantReadHandle{transaction: claimTransaction}, coordinationTenant,
		coordinationSubject, intent.RequestDigest(), claimInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	claim, err = settleIdempotencyClaim(claim, replayState, claimExpiresAt, nil)
	if err != nil || claim.DatabaseOutcome != DatabaseCommitted || len(claimTransaction.queries) != 1 ||
		claimTransaction.queries[0].sql != claimManagedAgentCreateProjectIdempotencySQL {
		t.Fatalf("claim kernel/result = %#v / %v / %#v", claim, err, claimTransaction.queries)
	}
	claimArguments := claimTransaction.queries[0].arguments
	if len(claimArguments) != 5 || claimArguments[0] != coordinationTenant || claimArguments[1] != coordinationSubject ||
		claimArguments[2] != claimInput.IdempotencyKey || claimArguments[3] != intent.RequestDigest() || claimArguments[4] != claimInput.AuditFactID {
		t.Fatalf("claim arguments = %#v", claimArguments)
	}

	successInput := IdempotencySuccessInput{
		Profile: coordination.ManagedAgentCreateProject(), Request: request, IdempotencyKey: claimInput.IdempotencyKey,
		ResourceID: "project-alpha", ResourceVersion: 7, EventID: "event-project-alpha",
		PayloadDigest: coordinationPayload, AuditFactID: "audit-idempotency-success",
	}
	successTransaction := &fakeTransaction{rows: []rowScanner{rowValues(
		"succeeded", "project", successInput.ResourceID, successInput.ResourceVersion, successInput.EventID, "pending",
	)}}
	success, err := completeIdempotencySuccessTransaction(
		ctx, &tenantReadHandle{transaction: successTransaction}, coordinationTenant,
		coordinationSubject, intent.RequestDigest(), successInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	success, err = settleIdempotencySuccess(success, nil, successInput)
	if err != nil || success.DatabaseOutcome != DatabaseCommitted || len(successTransaction.queries) != 1 ||
		successTransaction.queries[0].sql != completeManagedAgentCreateProjectSuccessSQL {
		t.Fatalf("success kernel/result = %#v / %v / %#v", success, err, successTransaction.queries)
	}

	failureInput := IdempotencyFailureInput{
		Profile: coordination.ManagedAgentCreateProject(), Request: request, IdempotencyKey: claimInput.IdempotencyKey,
		StableErrorCode: "stable.failure", AuditFactID: "audit-idempotency-failure",
	}
	failureTransaction := &fakeTransaction{rows: []rowScanner{rowValues("failed", failureInput.StableErrorCode)}}
	failure, err := completeIdempotencyFailureTransaction(
		ctx, &tenantReadHandle{transaction: failureTransaction}, coordinationTenant,
		coordinationSubject, intent.RequestDigest(), failureInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	failure, err = settleIdempotencyFailure(failure, nil, failureInput)
	if err != nil || failure.DatabaseOutcome != DatabaseCommitted || len(failureTransaction.queries) != 1 ||
		failureTransaction.queries[0].sql != completeManagedAgentCreateProjectFailureSQL {
		t.Fatalf("failure kernel/result = %#v / %v / %#v", failure, err, failureTransaction.queries)
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
	transaction := coordinationTransaction(rowValues((*string)(nil)))
	service, connection := coordinationService(t, transaction)
	_, _, err := service.bindProfile(context.Background(), coordinationTenant, coordination.Profile{}, coordinationProjectRequest(), "audit-idempotency-claim")
	if !errors.Is(err, ErrCoordinationInvalidInput) || connection.beginOptions != nil {
		t.Fatalf("zero profile error/database use = %v / %#v", err, connection.beginOptions)
	}
}

func TestDurableCoordinationPublicJWTUserPathRejectsMissingPrincipalBeforeDatabase(t *testing.T) {
	transaction := coordinationTransaction(rowValues((*string)(nil)))
	service, connection := coordinationService(t, transaction)
	result, err := service.ClaimIdempotency(context.Background(), coordinationTenant, nil, IdempotencyClaimInput{
		Profile: coordination.ManagedAgentCreateProject(), Request: coordinationProjectRequest(),
		IdempotencyKey: "idempotency-key-0001", AuditFactID: "audit-idempotency-claim",
	})
	if err == nil || result != (IdempotencyClaimResult{}) || connection.beginOptions != nil {
		t.Fatalf("nil principal result/error/database use = %#v / %v / %#v", result, err, connection.beginOptions)
	}
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
