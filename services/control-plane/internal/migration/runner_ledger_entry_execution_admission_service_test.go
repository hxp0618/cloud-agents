package migration

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type runnerLedgerEntryExecutionAdmissionFixture struct {
	base   *runnerLedgerEntryAdmissionFixture
	permit *runnerLedgerEntryExecutionPermit
}

func newRunnerLedgerEntryExecutionAdmissionFixture(t *testing.T, disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction) *runnerLedgerEntryExecutionAdmissionFixture {
	t.Helper()
	return &runnerLedgerEntryExecutionAdmissionFixture{
		base: newRunnerLedgerEntryAdmissionFixture(t, disposition, state, action),
	}
}

func (fixture *runnerLedgerEntryExecutionAdmissionFixture) prepare(ctx context.Context) (*runnerLedgerEntryExecutionPermit, error) {
	base := fixture.base.service.kernel.base
	permit, err := fixture.base.service.kernel.runner.prepareRunnerLedgerEntryExecutionAdmission(
		ctx, "test-only", base.bundle, base.plans, fixture.base.service.evidence, base.candidate, fixture.base.fact,
	)
	fixture.permit = permit
	return permit, err
}

func (fixture *runnerLedgerEntryExecutionAdmissionFixture) close(t *testing.T) {
	t.Helper()
	if fixture == nil || fixture.base == nil {
		return
	}
	if fixture.permit != nil {
		if _, live := runnerLedgerEntryExecutionPermitRegistry.Load(fixture.permit); live {
			if err := closeRunnerLedgerEntryExecutionPermit(fixture.permit, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	revokeRunnerLedgerEntryExecutionAdmissionClaims(fixture.base.service.evidence)
	fixture.base.close(t)
}

func TestRunnerLedgerEntryExecutionAdmissionAcceptsExactlyFourGeneratedPairs(t *testing.T) {
	for _, test := range []struct {
		name        string
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
		entryIndex  uint32
	}{
		{"empty-brand-new", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 0},
		{"empty-inherited-first", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt, 0},
		{"partial-inherited-next", runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry, 1},
		{"partial-terminal-next", runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, test.disposition, test.state, test.action)
			defer fixture.close(t)
			permit, err := fixture.prepare(context.Background())
			if err != nil || !validRunnerLedgerEntryExecutionPermit(permit) ||
				permit.action != runnerLedgerEntryExecutionAdmissionPrepare || permit.selection.entryIndex != test.entryIndex ||
				permit.selection.planCount == 0 || permit.selection.planDigest == ([32]byte{}) {
				t.Fatalf("permit=%+v err=%v", permit, err)
			}
			database := fixture.base.database
			evidence := fixture.base.service.evidence
			if fixture.base.connector.attempts != 1 || database.ledgerReadCalls != 4 || database.setRoleCalls != 1 ||
				database.lockCalls != 1 || database.unlockCalls != 0 || database.closeCalls != 0 ||
				database.beginCalls != 0 || database.boundaryCalls != 2 || database.queryCalls != 0 ||
				database.backend.executeCalls != 0 || database.backend.ledgerInsertCalls != 0 || database.backend.commitCalls != 0 ||
				!database.locked || database.closed || evidence.executionBindCalls != 1 || evidence.executionConsumeCalls != 1 ||
				evidence.entryBindCalls != 0 || evidence.entryConsumeCalls != 0 {
				t.Fatalf("execution admission escaped read-only boundary: database=%+v evidence=%+v", database, evidence)
			}
			if err := closeRunnerLedgerEntryExecutionPermit(permit, nil); err != nil {
				t.Fatal(err)
			}
			if database.unlockCalls != 1 || database.closeCalls != 1 || !database.closed || database.locked ||
				database.beginCalls != 0 || database.backend.executeCalls != 0 || database.backend.ledgerInsertCalls != 0 ||
				database.backend.commitCalls != 0 {
				t.Fatalf("close_without_mutation escaped boundary: %+v", database)
			}
			if _, retained := runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder.Load(evidence); !retained {
				t.Fatal("execution-admission terminal use record was not retained until evidence close")
			}
		})
	}
}

func TestRunnerLedgerEntryExecutionAdmissionRejectsRetryAfterCompleteReadOnlyClassification(t *testing.T) {
	fixture := newRunnerLedgerEntryExecutionAdmissionFixture(
		t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt,
	)
	defer fixture.close(t)
	permit, err := fixture.prepare(context.Background())
	var stable *Error
	if permit != nil || !errors.As(err, &stable) || stable.Code != CodeProjectionNotImplemented ||
		stable.Op != "runner-ledger-entry-execution-admission-selection" || stable.Err != nil {
		t.Fatalf("retry permit=%+v err=%#v", permit, stable)
	}
	database := fixture.base.database
	evidence := fixture.base.service.evidence
	if fixture.base.connector.attempts != 1 || database.ledgerReadCalls != 4 || database.beginCalls != 0 ||
		database.backend.executeCalls != 0 || database.backend.ledgerInsertCalls != 0 || database.backend.commitCalls != 0 ||
		database.unlockCalls != 1 || database.closeCalls != 1 || !database.closed ||
		evidence.executionBindCalls != 1 || evidence.executionConsumeCalls != 1 {
		t.Fatalf("retry classification escaped read-only boundary: database=%+v evidence=%+v", database, evidence)
	}
	base := fixture.base.service.kernel.base
	second, secondErr := fixture.base.service.kernel.runner.prepareRunnerLedgerEntryExecutionAdmission(
		context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate, fixture.base.fact,
	)
	if second != nil || !IsCode(secondErr, CodeEvidenceRecoveryRequired) || fixture.base.connector.attempts != 1 {
		t.Fatalf("retry replay permit=%+v err=%v connects=%d", second, secondErr, fixture.base.connector.attempts)
	}
}

func TestRunnerLedgerEntryExecutionPermitIsNonCopyableAndOneShot(t *testing.T) {
	fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
	defer fixture.close(t)
	permit, err := fixture.prepare(context.Background())
	if err != nil || !validRunnerLedgerEntryExecutionPermit(permit) {
		t.Fatalf("permit=%+v err=%v", permit, err)
	}
	copyValue := *permit
	if err := closeRunnerLedgerEntryExecutionPermit(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) ||
		!validRunnerLedgerEntryExecutionPermit(permit) {
		t.Fatalf("copy close err=%v original-valid=%t", err, validRunnerLedgerEntryExecutionPermit(permit))
	}
	if err := closeRunnerLedgerEntryExecutionPermit(&runnerLedgerEntryExecutionPermit{}, nil); !IsCode(err, CodeTransactionBoundary) {
		t.Fatalf("literal close err=%v", err)
	}
	if err := closeRunnerLedgerEntryExecutionPermit(permit, nil); err != nil {
		t.Fatal(err)
	}
	if err := closeRunnerLedgerEntryExecutionPermit(permit, nil); !IsCode(err, CodeTransactionBoundary) {
		t.Fatalf("second close err=%v", err)
	}
	if validRunnerLedgerEntryExecutionPermit(permit) {
		t.Fatal("closed execution permit remained valid")
	}
}

func TestRunnerLedgerEntryExecutionAdmissionClaimRejectsCopyLiteralAndSecondConsumption(t *testing.T) {
	fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
	defer fixture.close(t)
	base := fixture.base.service.kernel.base
	evidence := fixture.base.service.evidence
	request := runnerLedgerEntryExecutionAdmissionClaimRequest{fact: fixture.base.fact, candidate: base.candidate}
	claim, err := evidence.bindRunnerLedgerEntryExecutionAdmissionClaim(context.Background(), request)
	if err != nil || !validRunnerLedgerEntryExecutionAdmissionClaim(claim, evidence, base.candidate.binding) {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	copyValue := *claim
	if boundary, err := evidence.consumeRunnerLedgerEntryExecutionAdmissionClaim(context.Background(), &copyValue, base.candidate); boundary.canonical != ([32]byte{}) || !IsCode(err, CodeEvidenceRecoveryRequired) ||
		!validRunnerLedgerEntryExecutionAdmissionClaim(claim, evidence, base.candidate.binding) {
		t.Fatalf("copy boundary=%+v err=%v original-valid=%t", boundary, err, validRunnerLedgerEntryExecutionAdmissionClaim(claim, evidence, base.candidate.binding))
	}
	if boundary, err := evidence.consumeRunnerLedgerEntryExecutionAdmissionClaim(
		context.Background(), &runnerLedgerEntryExecutionAdmissionClaim{}, base.candidate,
	); boundary.canonical != ([32]byte{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal boundary=%+v err=%v", boundary, err)
	}
	boundary, err := evidence.consumeRunnerLedgerEntryExecutionAdmissionClaim(context.Background(), claim, base.candidate)
	if err != nil || boundary.canonical == ([32]byte{}) ||
		boundary.canonical != runnerLedgerEntryExecutionAdmissionEvidenceBoundaryDigest(boundary) ||
		boundary.claimDigest != claim.canonical {
		t.Fatalf("boundary=%+v err=%v", boundary, err)
	}
	if second, err := evidence.consumeRunnerLedgerEntryExecutionAdmissionClaim(context.Background(), claim, base.candidate); second.canonical != ([32]byte{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("second boundary=%+v err=%v", second, err)
	}
}

func TestRunnerLedgerEntryExecutionAdmissionRejectsPermitAndUseRecordTamper(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*runnerLedgerEntryExecutionPermit)
	}{
		{"evidence-boundary", func(permit *runnerLedgerEntryExecutionPermit) { permit.evidenceBoundary[0] ^= 0xff }},
		{"action", func(permit *runnerLedgerEntryExecutionPermit) { permit.action = "caller_selected" }},
		{"plan", func(permit *runnerLedgerEntryExecutionPermit) { permit.selection.planDigest[0] ^= 0xff }},
		{"binding-session", func(permit *runnerLedgerEntryExecutionPermit) { permit.binding.session = nil }},
		{"binding-evidence", func(permit *runnerLedgerEntryExecutionPermit) { permit.binding.evidenceBinder = nil }},
		{"binding-candidate", func(permit *runnerLedgerEntryExecutionPermit) { permit.binding.candidateBinding = nil }},
		{"registry-use", func(permit *runnerLedgerEntryExecutionPermit) {
			permit.use.mu.Lock()
			permit.use.boundary[0] ^= 0xff
			permit.use.mu.Unlock()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			permit, err := fixture.prepare(context.Background())
			if err != nil || !validRunnerLedgerEntryExecutionPermit(permit) {
				t.Fatalf("permit=%+v err=%v", permit, err)
			}
			test.mutate(permit)
			if validRunnerLedgerEntryExecutionPermit(permit) {
				t.Fatal("tampered permit remained valid")
			}
			if err := closeRunnerLedgerEntryExecutionPermit(permit, nil); !IsCode(err, CodeTransactionBoundary) ||
				fixture.base.database.unlockCalls != 1 || fixture.base.database.closeCalls != 1 || !fixture.base.database.closed {
				t.Fatalf("tampered close err=%v database=%+v", err, fixture.base.database)
			}
		})
	}
}

func TestRunnerLedgerEntryExecutionAdmissionRejectsCrossProfileAndForeignEvidenceBeforeDatabase(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*runnerLedgerConsumerFact)
		foreign bool
	}{
		{"consumer-profile", func(fact *runnerLedgerConsumerFact) { fact.profileID = "runner-ledger-consumer/v2" }, false},
		{"consumer-profile-digest", func(fact *runnerLedgerConsumerFact) {
			fact.profileDigest = testDigest("foreign-consumer-profile").String()
		}, false},
		{"consumer-action", func(fact *runnerLedgerConsumerFact) { fact.action = runnerLedgerConsumerRecoveryNotImplemented }, false},
		{"consumer-subject", func(fact *runnerLedgerConsumerFact) { fact.subjectDigest = testDigest("foreign-consumer-fact") }, false},
		{"foreign-evidence-binder", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			fact := fixture.base.fact.clone()
			if test.mutate != nil {
				test.mutate(&fact)
			}
			base := fixture.base.service.kernel.base
			var evidence EvidenceSession = fixture.base.service.evidence
			if test.foreign {
				evidence = base.evidence
			}
			permit, err := fixture.base.service.kernel.runner.prepareRunnerLedgerEntryExecutionAdmission(
				context.Background(), "test-only", base.bundle, base.plans, evidence, base.candidate, fact,
			)
			if permit != nil || err == nil || fixture.base.connector.attempts != 0 ||
				fixture.base.database.beginCalls != 0 || fixture.base.database.closeCalls != 0 ||
				fixture.base.service.evidence.executionConsumeCalls != 0 {
				t.Fatalf("cross-profile permit=%+v err=%v fixture=%+v", permit, err, fixture)
			}
		})
	}
}

func TestRunnerLedgerEntryExecutionAdmissionRejectsLedgerCatalogAndEvidenceDrift(t *testing.T) {
	for _, test := range []struct {
		name     string
		prepare  func(*runnerLedgerEntryExecutionAdmissionFixture)
		wantCode ErrorCode
	}{
		{
			name: "final-ledger",
			prepare: func(fixture *runnerLedgerEntryExecutionAdmissionFixture) {
				row := ledgerRowFor(fixture.base.service.kernel.base.bundle.Manifest.SchemaBundle.Migrations[0], fixture.base.service.kernel.base.bundle.Manifest.SchemaBundleDigest)
				fixture.base.database.ledgerRowsByRead[3] = []LedgerRow{row}
			},
			wantCode: CodeInvalidLedger,
		},
		{
			name: "final-catalog",
			prepare: func(fixture *runnerLedgerEntryExecutionAdmissionFixture) {
				fixture.base.service.kernel.factory.mutatePrecondition = func(result *ProjectionResult[CatalogStateProjection]) {
					if len(fixture.base.service.kernel.factory.preconditionPhases) >= 3 {
						result.Digest = testDigest("execution-admission-final-catalog-drift")
					}
				}
			},
			wantCode: CodeCatalogDrift,
		},
		{
			name: "final-evidence",
			prepare: func(fixture *runnerLedgerEntryExecutionAdmissionFixture) {
				fixture.base.service.evidence.mutateBeforeExecutionConsume = func(evidence *runnerLedgerPreflightEvidenceFake) {
					evidence.recovery.tailDigest = testDigest("execution-admission-evidence-drift")
				}
			},
			wantCode: CodeEvidenceRecoveryRequired,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			test.prepare(fixture)
			permit, err := fixture.prepare(context.Background())
			if permit != nil || !IsCode(err, test.wantCode) || fixture.base.database.beginCalls != 0 ||
				fixture.base.database.backend.executeCalls != 0 || fixture.base.database.backend.ledgerInsertCalls != 0 ||
				fixture.base.database.backend.commitCalls != 0 || fixture.base.database.unlockCalls != 1 ||
				fixture.base.database.closeCalls != 1 || !fixture.base.database.closed {
				t.Fatalf("drift permit=%+v err=%v database=%+v", permit, err, fixture.base.database)
			}
		})
	}
}

func TestRunnerLedgerEntryExecutionAdmissionFreshSessionFaultsFailClosed(t *testing.T) {
	for _, test := range []struct {
		name        string
		prepare     func(*runnerLedgerEntryExecutionAdmissionFixture)
		wantCode    ErrorCode
		wantConnect int
		wantUnlock  int
		wantClose   int
	}{
		{"connector-unconfigured", func(f *runnerLedgerEntryExecutionAdmissionFixture) { f.base.service.kernel.runner.Connector = nil }, CodeProjectionNotImplemented, 0, 0, 0},
		{"connect", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.connector.err = errors.New("secret-connect")
		}, CodeTransactionBoundary, 1, 0, 0},
		{"connect-returned-session", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.connector.err = errors.New("secret-connect")
			f.base.connector.returnSessionOnError = true
		}, CodeTransactionBoundary, 1, 0, 1},
		{"settings", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.settingsErr = errors.New("secret-settings")
		}, CodeTransactionBoundary, 1, 0, 1},
		{"lock", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.lockErr = errors.New("secret-lock")
		}, CodeTransactionBoundary, 1, 0, 1},
		{"migration-authority", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.service.kernel.factory.projectionErr[AuthorityPhaseMigrationRole] = fail(CodeAuthorityDrift, "fixture", "secret", errors.New("secret-authority"))
		}, CodeAuthorityDrift, 1, 1, 1},
		{"final-migration-authority", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.snapshotMetadataNth[4] = func(metadata *SnapshotMetadata) { metadata.DatabaseName += "_drift" }
		}, CodeAuthorityDrift, 1, 1, 1},
		{"session-identity", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.snapshotMetadataMutate[AuthorityPhaseMigrationRole] = func(metadata *SnapshotMetadata) { metadata.ServerVersionNum++ }
		}, CodeProjectionMetadataMismatch, 1, 1, 1},
		{"final-boundary-read", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.boundaryErr[2] = errors.New("secret-boundary")
		}, CodeTransactionBoundary, 1, 1, 1},
		{"final-lock-lost", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.boundaryMutate[2] = func(boundary *BoundaryState) { boundary.LockHeld = false }
		}, CodeLockLost, 1, 1, 1},
		{"final-role-drift", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.boundaryMutate[2] = func(boundary *BoundaryState) { boundary.CurrentUser = "foreign_role" }
		}, CodeAuthorityDrift, 1, 1, 1},
		{"final-session-not-idle", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.boundaryMutate[2] = func(boundary *BoundaryState) { boundary.TxStatus = 'T' }
		}, CodeTransactionBoundary, 1, 1, 1},
		{"final-ledger-read", func(f *runnerLedgerEntryExecutionAdmissionFixture) {
			f.base.database.ledgerReadErr[3] = errors.New("secret-ledger-read")
		}, CodeTransactionBoundary, 1, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			test.prepare(fixture)
			permit, err := fixture.prepare(context.Background())
			var stable *Error
			if permit != nil || !errors.As(err, &stable) || stable.Code != test.wantCode || stable.Err != nil ||
				strings.Contains(err.Error(), "secret-") || fixture.base.connector.attempts != test.wantConnect ||
				fixture.base.database.unlockCalls != test.wantUnlock || fixture.base.database.closeCalls != test.wantClose ||
				fixture.base.database.beginCalls != 0 || fixture.base.database.backend.executeCalls != 0 ||
				fixture.base.database.backend.ledgerInsertCalls != 0 || fixture.base.database.backend.commitCalls != 0 ||
				fixture.base.service.evidence.executionBindCalls != 1 || fixture.base.service.evidence.executionConsumeCalls != 0 {
				t.Fatalf("fault permit=%+v err=%#v connector=%+v database=%+v evidence=%+v", permit, stable, fixture.base.connector, fixture.base.database, fixture.base.service.evidence)
			}
		})
	}
}

func TestRunnerLedgerEntryExecutionAdmissionContextAndEvidenceRevocation(t *testing.T) {
	t.Run("pre-canceled", func(t *testing.T) {
		fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		permit, err := fixture.prepare(ctx)
		if permit != nil || !IsCode(err, CodeContextCanceled) || fixture.base.connector.attempts != 0 ||
			fixture.base.service.evidence.executionBindCalls != 0 || fixture.base.database.closeCalls != 0 {
			t.Fatalf("pre-canceled permit=%+v err=%v fixture=%+v", permit, err, fixture)
		}
	})

	t.Run("canceled-after-claim-bind", func(t *testing.T) {
		fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.base.service.evidence.afterExecutionBind = cancel
		permit, err := fixture.prepare(ctx)
		if permit != nil || !IsCode(err, CodeContextCanceled) || fixture.base.connector.attempts != 0 ||
			fixture.base.service.evidence.executionBindCalls != 1 || fixture.base.service.evidence.executionConsumeCalls != 0 ||
			fixture.base.database.closeCalls != 0 {
			t.Fatalf("post-bind cancel permit=%+v err=%v fixture=%+v", permit, err, fixture)
		}
	})

	t.Run("canceled-after-evidence-consume", func(t *testing.T) {
		fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.base.service.evidence.afterExecutionConsume = cancel
		permit, err := fixture.prepare(ctx)
		if permit != nil || !IsCode(err, CodeContextCanceled) || fixture.base.connector.attempts != 1 ||
			fixture.base.service.evidence.executionBindCalls != 1 || fixture.base.service.evidence.executionConsumeCalls != 1 ||
			fixture.base.database.unlockCalls != 1 || fixture.base.database.closeCalls != 1 || !fixture.base.database.closed {
			t.Fatalf("post-consume cancel permit=%+v err=%v database=%+v evidence=%+v", permit, err, fixture.base.database, fixture.base.service.evidence)
		}
		base := fixture.base.service.kernel.base
		second, secondErr := fixture.base.service.kernel.runner.prepareRunnerLedgerEntryExecutionAdmission(
			context.Background(), "test-only", base.bundle, base.plans, fixture.base.service.evidence, base.candidate, fixture.base.fact,
		)
		if second != nil || !IsCode(secondErr, CodeEvidenceRecoveryRequired) || fixture.base.connector.attempts != 1 {
			t.Fatalf("post-consume retry permit=%+v err=%v attempts=%d", second, secondErr, fixture.base.connector.attempts)
		}
	})

	t.Run("evidence-close-invalidates-live-permit", func(t *testing.T) {
		fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		permit, err := fixture.prepare(context.Background())
		if err != nil || !validRunnerLedgerEntryExecutionPermit(permit) {
			t.Fatalf("permit=%+v err=%v", permit, err)
		}
		if err := fixture.base.service.evidence.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if validRunnerLedgerEntryExecutionPermit(permit) {
			t.Fatal("evidence close left execution permit valid")
		}
		if err := closeRunnerLedgerEntryExecutionPermit(permit, nil); !IsCode(err, CodeTransactionBoundary) ||
			fixture.base.database.unlockCalls != 1 || fixture.base.database.closeCalls != 1 || !fixture.base.database.closed {
			t.Fatalf("revoked permit close err=%v database=%+v", err, fixture.base.database)
		}
	})
}

func TestRunnerLedgerEntryExecutionAdmissionCleanupFailureDominatesNotImplemented(t *testing.T) {
	for _, test := range []struct {
		name       string
		unlockErr  error
		closeErr   error
		wantOp     string
		wantUnlock int
	}{
		{"unlock", errors.New("secret-unlock"), nil, "runner-advisory-unlock", 1},
		{"close", nil, errors.New("secret-close"), "runner-database-close", 1},
		{"close-over-unlock", errors.New("secret-unlock"), errors.New("secret-close"), "runner-database-close", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryExecutionAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			fixture.base.database.unlockErr, fixture.base.database.closeErr = test.unlockErr, test.closeErr
			permit, err := fixture.prepare(context.Background())
			if err != nil || permit == nil {
				t.Fatalf("prepare permit=%+v err=%v", permit, err)
			}
			var stable *Error
			closeErr := closeRunnerLedgerEntryExecutionPermit(permit, nil)
			if !errors.As(closeErr, &stable) || stable.Code != CodeTransactionBoundary || stable.Op != test.wantOp ||
				stable.Err != nil || strings.Contains(closeErr.Error(), "secret-") ||
				fixture.base.database.unlockCalls != test.wantUnlock || fixture.base.database.closeCalls != 1 ||
				fixture.base.database.beginCalls != 0 || fixture.base.database.backend.executeCalls != 0 ||
				fixture.base.database.backend.ledgerInsertCalls != 0 || fixture.base.database.backend.commitCalls != 0 {
				t.Fatalf("cleanup err=%#v database=%+v", stable, fixture.base.database)
			}
		})
	}
}

func TestRunnerLedgerEntryExecutionAdmissionProductionGraphHasOnlyCloseWithoutMutation(t *testing.T) {
	files := []string{
		"runner_ledger_entry_execution_admission_claim.go",
		"runner_ledger_entry_execution_admission_permit.go",
		"runner_ledger_consumer_service.go",
	}
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true, "Append": true, "AppendDurable": true,
		"Insert": true, "prepareCurrentDatabaseSession": true, "prepareCurrentTransaction": true,
		"prepareCurrentStatement": true, "appendCurrentStatementIntent": true, "runCurrentSingleEntry": true,
		"ReserveAndActivateSuccessor": true, "closeRunnerLedgerEntryAdmissionPermit": true,
	}
	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if path == "database/sql" || path == "net/http" || strings.Contains(path, "pgx") {
				t.Fatalf("%s imports forbidden package %q", name, path)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			called := ""
			switch function := call.Fun.(type) {
			case *ast.Ident:
				called = function.Name
			case *ast.SelectorExpr:
				called = function.Sel.Name
			}
			if forbidden[called] {
				t.Fatalf("%s acquired forbidden call edge %s", name, called)
			}
			return true
		})
	}
	permitType := reflect.TypeOf(runnerLedgerEntryExecutionPermit{})
	for index := 0; index < permitType.NumMethod(); index++ {
		t.Fatalf("execution permit unexpectedly exposes method %s", permitType.Method(index).Name)
	}
}

func TestRunnerLedgerEntryExecutionAdmissionAuthorityHasOnlyReviewedProductionConsumers(t *testing.T) {
	allowed := map[string]map[string]bool{
		"evidence_runner_ledger_entry_success.go": {
			"runnerLedgerEntryExecutionAdmissionClaimBinder": true,
		},
		"evidence_session.go": {
			"runnerLedgerEntryExecutionAdmissionClaimBinder":              true,
			"runnerLedgerEntryExecutionAdmissionClaimRequest":             true,
			"runnerLedgerEntryExecutionAdmissionEvidenceBoundary":         true,
			"runnerLedgerEntryExecutionAdmissionEvidenceFacts":            true,
			"runnerLedgerEntryExecutionAdmissionClaim":                    true,
			"bindRunnerLedgerEntryExecutionAdmissionClaimFromEvidence":    true,
			"consumeRunnerLedgerEntryExecutionAdmissionClaimFromEvidence": true,
			"revokeRunnerLedgerEntryExecutionAdmissionClaims":             true,
		},
		"runner_ledger_consumer_service.go": {
			"prepareRunnerLedgerEntryExecutionAdmission": true,
			"closeRunnerLedgerEntryExecutionPermit":      true,
		},
		"runner_ledger_entry_success_kernel.go": {
			"runnerLedgerEntryExecutionAdmissionUseRecord":   true,
			"runnerLedgerEntryExecutionPermit":               true,
			"runnerLedgerEntryExecutionPermitRegistryRecord": true,
			"runnerLedgerEntryExecutionPermitRegistry":       true,
			"validRunnerLedgerEntryExecutionAdmissionUse":    true,
		},
	}
	ownedFiles := map[string]bool{
		"runner_ledger_entry_execution_admission_claim.go":  true,
		"runner_ledger_entry_execution_admission_permit.go": true,
	}
	symbols := map[string]bool{
		"runnerLedgerEntryExecutionAdmissionClaimBinder":              true,
		"runnerLedgerEntryExecutionAdmissionClaimRequest":             true,
		"runnerLedgerEntryExecutionAdmissionEvidenceFacts":            true,
		"runnerLedgerEntryExecutionAdmissionEvidenceBoundary":         true,
		"runnerLedgerEntryExecutionAdmissionClaim":                    true,
		"runnerLedgerEntryExecutionAdmissionUseRecord":                true,
		"runnerLedgerEntryExecutionPermit":                            true,
		"runnerLedgerEntryExecutionPermitBinding":                     true,
		"runnerLedgerEntryExecutionPermitRegistryRecord":              true,
		"bindRunnerLedgerEntryExecutionAdmissionClaimFromEvidence":    true,
		"consumeRunnerLedgerEntryExecutionAdmissionClaimFromEvidence": true,
		"validRunnerLedgerEntryExecutionAdmissionUse":                 true,
		"revokeRunnerLedgerEntryExecutionAdmissionClaims":             true,
		"prepareRunnerLedgerEntryExecutionAdmission":                  true,
		"bindRunnerLedgerEntryExecutionPermit":                        true,
		"validRunnerLedgerEntryExecutionPermit":                       true,
		"closeRunnerLedgerEntryExecutionPermit":                       true,
		"runnerLedgerEntryExecutionAdmissionClaimRegistry":            true,
		"runnerLedgerEntryExecutionAdmissionUseByEvidenceBinder":      true,
		"runnerLedgerEntryExecutionPermitRegistry":                    true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || ownedFiles[name] {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				called := ""
				switch target := call.Fun.(type) {
				case *ast.Ident:
					called = target.Name
				case *ast.SelectorExpr:
					called = target.Sel.Name
				}
				if symbols[called] && !allowed[name][called] {
					t.Fatalf("execution-admission authority call %s acquired unreviewed production consumer %s", called, name)
					return false
				}
			}
			identifier, ok := node.(*ast.Ident)
			if !ok || !symbols[identifier.Name] || allowed[name][identifier.Name] {
				return true
			}
			t.Fatalf("execution-admission authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			return false
		})
	}
}
