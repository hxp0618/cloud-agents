package migration

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type runnerLedgerRecoveryAdmissionFixture struct {
	service   *runnerLedgerPreflightServiceFixture
	fact      runnerLedgerConsumerFact
	database  *runnerPreflightSession
	connector *runnerPreflightConnector
	permit    runnerLedgerRecoveryCloseOnlyPermit
}

func newRunnerLedgerRecoveryAdmissionFixture(t *testing.T, disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction) *runnerLedgerRecoveryAdmissionFixture {
	t.Helper()
	service := newRunnerLedgerPreflightServiceFixture(t)
	service.configure(t, disposition, state, action, 16)
	base := service.kernel.base
	claim, err := service.kernel.runner.prepareRunnerLedgerPreflightClaim(
		context.Background(), "test-only", base.bundle, base.plans, service.evidence, base.candidate,
	)
	if err != nil {
		service.close(t)
		t.Fatal(err)
	}
	defer revokeRunnerLedgerPreflightClaim(claim)
	dispatch, err := service.kernel.runner.claimRunnerLedgerPreflightDispatch(context.Background(), service.evidence, base.candidate, claim)
	if err != nil {
		service.close(t)
		t.Fatal(err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, base.bundle.Manifest.ManifestDigest)
	if err != nil || (fact.action != runnerLedgerConsumerEntryNotImplemented && fact.action != runnerLedgerConsumerRecoveryNotImplemented) {
		service.close(t)
		t.Fatalf("recovery consumer fact=%+v err=%v", fact, err)
	}
	if _, ok := generatedRunnerLedgerRecoveryAdmissionAction(disposition, state, action); !ok {
		service.close(t)
		t.Fatalf("pair %s/%s/%s has no generated recovery action", disposition, state, action)
	}
	rows := make([]LedgerRow, 0, fact.dispatch.fact.orderedMigrationPrefixLength)
	for index := uint32(0); index < fact.dispatch.fact.orderedMigrationPrefixLength; index++ {
		rows = append(rows, ledgerRowFor(base.bundle.Manifest.SchemaBundle.Migrations[index], base.bundle.Manifest.SchemaBundleDigest))
	}
	database := newRunnerPreflightSession()
	database.ledgerRowsByRead = [][]LedgerRow{
		cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows), cloneProjectionValue(rows),
	}
	connector := &runnerPreflightConnector{session: database}
	service.kernel.runner.Connector = connector
	return &runnerLedgerRecoveryAdmissionFixture{service: service, fact: fact, database: database, connector: connector}
}

func (fixture *runnerLedgerRecoveryAdmissionFixture) prepare(ctx context.Context) (runnerLedgerRecoveryCloseOnlyPermit, error) {
	base := fixture.service.kernel.base
	permit, err := fixture.service.kernel.runner.prepareRunnerLedgerRecoveryAdmission(
		ctx, "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate, fixture.fact,
	)
	fixture.permit = permit
	return permit, err
}

func (fixture *runnerLedgerRecoveryAdmissionFixture) close(t *testing.T) {
	t.Helper()
	if fixture == nil {
		return
	}
	if fixture.permit != nil {
		if _, live := runnerLedgerRecoveryAdmissionPermitRegistry.Load(fixture.permit); live {
			if err := fixture.permit.closeWithoutMutation(nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	revokeRunnerLedgerRecoveryAdmissionClaims(fixture.service.evidence)
	if fixture.database != nil && !fixture.database.closed {
		if err := closeRunnerDatabasePreflight(fixture.database, fixture.service.kernel.base.key, fixture.database.locked, nil); err != nil {
			t.Fatal(err)
		}
	}
	fixture.service.close(t)
}

func TestRunnerLedgerRecoveryAdmissionAcceptsExactlyTwelveGeneratedPairs(t *testing.T) {
	common := generatedRunnerLedgerRecoveryProfiles[0]
	if common.pairCount != 12 {
		t.Fatalf("common recovery pairs=%d", common.pairCount)
	}
	counts := map[runnerLedgerRecoveryAction]int{}
	for index := uint8(0); index < common.pairCount; index++ {
		pair := common.pairs[index]
		name := string(pair.profileAction) + "/" + string(pair.state) + "/" + string(pair.action)
		t.Run(name, func(t *testing.T) {
			if pair.profileAction == generatedRunnerLedgerRecoveryProfiles[2].action ||
				pair.profileAction == generatedRunnerLedgerRecoveryProfiles[3].action {
				fixture := newRunnerLedgerRecoveryReconciliationFixture(t, pair.state, runnerLedgerReconciliationExactPending, 16)
				defer fixture.close(t)
				base := fixture.success.execution.base.service.kernel.base
				runner := fixture.success.execution.base.service.kernel.runner
				permit, err := runner.prepareRunnerLedgerRecoveryAdmission(
					context.Background(), "test-only", base.bundle, base.plans,
					fixture.success.execution.base.service.evidence, base.candidate, fixture.fact,
				)
				core := runnerLedgerRecoveryPermitCore(permit)
				if err != nil || !validRunnerLedgerRecoveryAdmissionPermit(permit) || core == nil ||
					core.action != pair.profileAction || core.selection.action != pair.profileAction ||
					core.selection.planCount == 0 || core.selection.planDigest == ([32]byte{}) ||
					core.selection.maxAttempts == 0 || core.selection.attemptIndex == 0 {
					t.Fatalf("reconciliation permit=%T core=%+v err=%v", permit, core, err)
				}
				assertRunnerLedgerRecoveryPermitType(t, permit, pair.profileAction)
				counts[pair.profileAction]++
				if err := permit.closeWithoutMutation(nil); err != nil {
					t.Fatal(err)
				}
				if !fixture.database.closed || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 ||
					fixture.database.beginCalls != 0 || fixture.database.backend.executeCalls != 0 ||
					fixture.database.backend.ledgerInsertCalls != 0 || fixture.database.backend.commitCalls != 0 {
					t.Fatalf("reconciliation close_without_mutation escaped boundary: %+v", fixture.database)
				}
				return
			}
			fixture := newRunnerLedgerRecoveryAdmissionFixture(t, pair.disposition, pair.state, pair.action)
			defer fixture.close(t)
			permit, err := fixture.prepare(context.Background())
			core := runnerLedgerRecoveryPermitCore(permit)
			if err != nil || !validRunnerLedgerRecoveryAdmissionPermit(permit) || core == nil ||
				core.action != pair.profileAction || core.selection.action != pair.profileAction ||
				core.selection.profileIndex == 0 || core.selection.profileIndex == 6 ||
				core.selection.planCount == 0 || core.selection.planDigest == ([32]byte{}) ||
				core.selection.maxAttempts == 0 || core.selection.attemptIndex == 0 {
				t.Fatalf("permit=%T core=%+v err=%v", permit, core, err)
			}
			assertRunnerLedgerRecoveryPermitType(t, permit, pair.profileAction)
			counts[pair.profileAction]++
			database := fixture.database
			evidence := fixture.service.evidence
			if fixture.connector.attempts != 1 || database.ledgerReadCalls != 4 || database.setRoleCalls != 1 ||
				database.lockCalls != 1 || database.unlockCalls != 0 || database.closeCalls != 0 ||
				database.beginCalls != 0 || database.boundaryCalls != 2 || database.queryCalls != 0 ||
				database.backend.executeCalls != 0 || database.backend.ledgerInsertCalls != 0 || database.backend.commitCalls != 0 ||
				!database.locked || database.closed || evidence.recoveryBindCalls != 1 || evidence.recoveryConsumeCalls != 1 ||
				evidence.entryBindCalls != 0 || evidence.entryConsumeCalls != 0 ||
				evidence.executionBindCalls != 0 || evidence.executionConsumeCalls != 0 {
				t.Fatalf("recovery admission escaped read-only boundary: database=%+v evidence=%+v", database, evidence)
			}
			if err := permit.closeWithoutMutation(nil); err != nil {
				t.Fatal(err)
			}
			if database.unlockCalls != 1 || database.closeCalls != 1 || !database.closed || database.locked ||
				database.beginCalls != 0 || database.backend.executeCalls != 0 || database.backend.ledgerInsertCalls != 0 ||
				database.backend.commitCalls != 0 {
				t.Fatalf("close_without_mutation escaped boundary: %+v", database)
			}
			if _, retained := runnerLedgerRecoveryAdmissionUseByEvidenceBind.Load(evidence); !retained {
				t.Fatal("recovery admission terminal use record was not retained until evidence close")
			}
		})
	}
	if counts[generatedRunnerLedgerRecoveryProfiles[1].action] != 4 ||
		counts[generatedRunnerLedgerRecoveryProfiles[2].action] != 1 ||
		counts[generatedRunnerLedgerRecoveryProfiles[3].action] != 1 ||
		counts[generatedRunnerLedgerRecoveryProfiles[4].action] != 1 ||
		counts[generatedRunnerLedgerRecoveryProfiles[5].action] != 3 ||
		counts[generatedRunnerLedgerRecoveryProfiles[7].action] != 2 {
		t.Fatalf("action counts=%v", counts)
	}
}

func assertRunnerLedgerRecoveryPermitType(t *testing.T, permit runnerLedgerRecoveryCloseOnlyPermit, action runnerLedgerRecoveryAction) {
	t.Helper()
	switch action {
	case generatedRunnerLedgerRecoveryProfiles[1].action:
		if _, ok := permit.(*runnerLedgerAbortTerminalAdmissionPermit); !ok {
			t.Fatalf("abort action minted %T", permit)
		}
	case generatedRunnerLedgerRecoveryProfiles[2].action:
		if _, ok := permit.(*runnerLedgerCommitObservationAdmissionPermit); !ok {
			t.Fatalf("commit observation action minted %T", permit)
		}
	case generatedRunnerLedgerRecoveryProfiles[3].action:
		if _, ok := permit.(*runnerLedgerAmbiguousResolutionAdmissionPermit); !ok {
			t.Fatalf("ambiguous action minted %T", permit)
		}
	case generatedRunnerLedgerRecoveryProfiles[4].action:
		if _, ok := permit.(*runnerLedgerRetryHandoffAdmissionPermit); !ok {
			t.Fatalf("retry handoff action minted %T", permit)
		}
	case generatedRunnerLedgerRecoveryProfiles[5].action:
		if _, ok := permit.(*runnerLedgerRecoveryExecutionAdmissionPermit); !ok {
			t.Fatalf("recovery execution action minted %T", permit)
		}
	case generatedRunnerLedgerRecoveryProfiles[7].action:
		if _, ok := permit.(*runnerLedgerReturnFailureAdmissionPermit); !ok {
			t.Fatalf("return failure action minted %T", permit)
		}
	default:
		t.Fatalf("unexpected action %s", action)
	}
}

func TestRunnerLedgerRecoveryAdmissionPermitIsNonCopyableProfileSpecificAndOneShot(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAdmissionFixture(
		t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable,
	)
	defer fixture.close(t)
	permit, err := fixture.prepare(context.Background())
	if err != nil || !validRunnerLedgerRecoveryAdmissionPermit(permit) {
		t.Fatalf("permit=%T err=%v", permit, err)
	}
	abort := permit.(*runnerLedgerAbortTerminalAdmissionPermit)
	copyValue := *abort
	if err := copyValue.closeWithoutMutation(nil); !IsCode(err, CodeTransactionBoundary) || !validRunnerLedgerRecoveryAdmissionPermit(permit) {
		t.Fatalf("copy close err=%v original-valid=%t", err, validRunnerLedgerRecoveryAdmissionPermit(permit))
	}
	if err := (&runnerLedgerAbortTerminalAdmissionPermit{}).closeWithoutMutation(nil); !IsCode(err, CodeTransactionBoundary) {
		t.Fatalf("literal close err=%v", err)
	}
	foreign := &runnerLedgerCommitObservationAdmissionPermit{core: abort.core}
	foreign.self = foreign
	if err := foreign.closeWithoutMutation(nil); !IsCode(err, CodeTransactionBoundary) || !validRunnerLedgerRecoveryAdmissionPermit(permit) {
		t.Fatalf("cross-profile close err=%v original-valid=%t", err, validRunnerLedgerRecoveryAdmissionPermit(permit))
	}
	if err := permit.closeWithoutMutation(nil); err != nil {
		t.Fatal(err)
	}
	if err := permit.closeWithoutMutation(nil); !IsCode(err, CodeTransactionBoundary) {
		t.Fatalf("second close err=%v", err)
	}
	if !fixture.database.closed || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 {
		t.Fatalf("exact session was not closed once: %+v", fixture.database)
	}
}

func TestRunnerLedgerRecoveryAdmissionClaimIsNonCopyableAndRegistryBound(t *testing.T) {
	fixture := newRunnerLedgerRecoveryAdmissionFixture(
		t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt,
	)
	defer fixture.close(t)
	base := fixture.service.kernel.base
	request := runnerLedgerRecoveryAdmissionClaimRequest{fact: fixture.fact.clone(), candidate: base.candidate}
	claim, err := fixture.service.evidence.bindRunnerLedgerRecoveryAdmissionClaim(context.Background(), request)
	if err != nil || !validRunnerLedgerRecoveryAdmissionClaim(claim, fixture.service.evidence, base.candidate.binding) {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	defer revokeRunnerLedgerRecoveryAdmissionClaim(claim)
	copyClaim := *claim
	if validRunnerLedgerRecoveryAdmissionClaim(&copyClaim, fixture.service.evidence, base.candidate.binding) ||
		validRunnerLedgerRecoveryAdmissionClaim(&runnerLedgerRecoveryAdmissionClaim{}, fixture.service.evidence, base.candidate.binding) {
		t.Fatal("ordinary recovery claim copy or literal was accepted")
	}
	runnerLedgerRecoveryAdmissionClaimRegistry.Delete(claim)
	boundary, err := fixture.service.evidence.consumeRunnerLedgerRecoveryAdmissionClaim(context.Background(), claim, base.candidate)
	if boundary.canonical != ([32]byte{}) || !IsCode(err, CodeEvidenceRecoveryRequired) || fixture.service.evidence.recoveryConsumeCalls != 1 {
		t.Fatalf("registry-missing boundary=%+v err=%v evidence=%+v", boundary, err, fixture.service.evidence)
	}
}

func TestRunnerLedgerRecoveryAdmissionContextCancellationClosesExactBoundary(t *testing.T) {
	t.Run("after-claim-before-database", func(t *testing.T) {
		fixture := newRunnerLedgerRecoveryAdmissionFixture(
			t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt,
		)
		defer fixture.close(t)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.service.evidence.afterRecoveryBind = cancel
		permit, err := fixture.prepare(ctx)
		if permit != nil || !IsCode(err, CodeContextCanceled) || fixture.connector.attempts != 0 ||
			fixture.service.evidence.recoveryBindCalls != 1 || fixture.service.evidence.recoveryConsumeCalls != 0 {
			t.Fatalf("permit=%T err=%v connector=%+v evidence=%+v", permit, err, fixture.connector, fixture.service.evidence)
		}
	})

	t.Run("after-evidence-consume", func(t *testing.T) {
		fixture := newRunnerLedgerRecoveryAdmissionFixture(
			t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt,
		)
		defer fixture.close(t)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.service.evidence.afterRecoveryConsume = cancel
		permit, err := fixture.prepare(ctx)
		if permit != nil || !IsCode(err, CodeContextCanceled) || !fixture.database.closed || fixture.database.locked ||
			fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.beginCalls != 0 {
			t.Fatalf("permit=%T err=%v database=%+v", permit, err, fixture.database)
		}
	})
}

func TestRunnerLedgerRecoveryAdmissionPermitDriftStillClosesRegisteredSession(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*runnerLedgerAbortTerminalAdmissionPermit)
	}{
		{"owner-self", func(permit *runnerLedgerAbortTerminalAdmissionPermit) { permit.self = nil }},
		{"core-canonical", func(permit *runnerLedgerAbortTerminalAdmissionPermit) { permit.core.canonical = [32]byte{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerRecoveryAdmissionFixture(
				t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable,
			)
			defer fixture.close(t)
			permit, err := fixture.prepare(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			abort := permit.(*runnerLedgerAbortTerminalAdmissionPermit)
			test.mutate(abort)
			if err := abort.closeWithoutMutation(nil); !IsCode(err, CodeTransactionBoundary) {
				t.Fatalf("drift close err=%v", err)
			}
			if !fixture.database.closed || fixture.database.locked || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 {
				t.Fatalf("trusted registered session was not closed: %+v", fixture.database)
			}
			if _, live := runnerLedgerRecoveryAdmissionPermitRegistry.Load(permit); live {
				t.Fatal("drifted permit registry record remained live")
			}
		})
	}
}

func TestRunnerLedgerRecoveryAdmissionClaimRejectsFullRootDriftAndCleanupDominates(t *testing.T) {
	t.Run("full-root-drift", func(t *testing.T) {
		fixture := newRunnerLedgerRecoveryAdmissionFixture(
			t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt,
		)
		defer fixture.close(t)
		fixture.service.evidence.mutateBeforeRecoveryConsume = func(evidence *runnerLedgerPreflightEvidenceFake) {
			evidence.sessionDigest[0] ^= 1
		}
		permit, err := fixture.prepare(context.Background())
		if permit != nil || !IsCode(err, CodeEvidenceRecoveryRequired) || !fixture.database.closed ||
			fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || fixture.database.beginCalls != 0 {
			t.Fatalf("permit=%T err=%v database=%+v", permit, err, fixture.database)
		}
	})

	t.Run("close-uncertainty-dominates", func(t *testing.T) {
		fixture := newRunnerLedgerRecoveryAdmissionFixture(
			t, runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDivergent, RecoveryReturnFailure,
		)
		defer fixture.close(t)
		permit, err := fixture.prepare(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		fixture.database.closeErr = errors.New("secret-close-uncertain")
		err = permit.closeWithoutMutation(fail(CodeProjectionNotImplemented, "runner-ledger-consumer-recovery", "closed", nil))
		var stable *Error
		if !errors.As(err, &stable) || stable.Code != CodeTransactionBoundary || stable.Op != "runner-database-close" ||
			stable.Err != nil || fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || !fixture.database.closed {
			t.Fatalf("error=%#v database=%+v", stable, fixture.database)
		}
	})
}

func TestRunnerLedgerRecoveryAdmissionRejectsUnknownPairBeforeDatabase(t *testing.T) {
	fixture := newRunnerLedgerPreflightServiceFixture(t)
	defer fixture.close(t)
	fixture.configure(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 16)
	dispatch := testRunnerLedgerConsumerDispatch(t, runnerLedgerPreflightEmptyBrandNew)
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, fixture.kernel.base.bundle.Manifest.ManifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := fixture.kernel.runner.prepareRunnerLedgerRecoveryAdmission(
		context.Background(), "test-only", fixture.kernel.base.bundle, fixture.kernel.base.plans,
		fixture.evidence, fixture.kernel.base.candidate, fact,
	)
	if permit != nil || !IsCode(err, CodeProjectionNotImplemented) || fixture.kernel.connector.attempts != 0 ||
		fixture.evidence.recoveryBindCalls != 0 || fixture.evidence.recoveryConsumeCalls != 0 {
		t.Fatalf("permit=%T err=%v connector=%+v evidence=%+v", permit, err, fixture.kernel.connector, fixture.evidence)
	}
}

func TestRunnerLedgerRecoveryAdmissionPermitHasNoWriterOrResultSurface(t *testing.T) {
	interfaceType := reflect.TypeOf((*runnerLedgerRecoveryCloseOnlyPermit)(nil)).Elem()
	if interfaceType.NumMethod() != 3 {
		t.Fatalf("close-only interface methods=%d", interfaceType.NumMethod())
	}
	for _, forbidden := range []string{"BeginMigration", "Append", "Execute", "Commit", "Result", "Handoff", "Consume"} {
		if _, ok := interfaceType.MethodByName(forbidden); ok {
			t.Fatalf("close-only permit exposes %s", forbidden)
		}
	}
}
