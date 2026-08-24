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

type runnerLedgerEntryAdmissionFixture struct {
	service   *runnerLedgerPreflightServiceFixture
	fact      runnerLedgerConsumerFact
	database  *runnerPreflightSession
	connector *runnerPreflightConnector
	permit    *runnerLedgerEntryAdmissionPermit
}

func newRunnerLedgerEntryAdmissionFixture(t *testing.T, disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction) *runnerLedgerEntryAdmissionFixture {
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
	if err != nil || fact.action != runnerLedgerConsumerEntryNotImplemented {
		service.close(t)
		t.Fatalf("entry consumer fact=%+v err=%v", fact, err)
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
	return &runnerLedgerEntryAdmissionFixture{service: service, fact: fact, database: database, connector: connector}
}

func (fixture *runnerLedgerEntryAdmissionFixture) prepare(t *testing.T) (*runnerLedgerEntryAdmissionPermit, error) {
	t.Helper()
	base := fixture.service.kernel.base
	permit, err := fixture.service.kernel.runner.prepareRunnerLedgerEntryAdmission(
		context.Background(), "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate, fixture.fact,
	)
	fixture.permit = permit
	return permit, err
}

func (fixture *runnerLedgerEntryAdmissionFixture) close(t *testing.T) {
	t.Helper()
	if fixture == nil {
		return
	}
	if fixture.permit != nil {
		if _, live := runnerLedgerEntryAdmissionPermitRegistry.Load(fixture.permit); live {
			if err := closeRunnerLedgerEntryAdmissionPermit(fixture.permit, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	revokeRunnerLedgerEntryAdmissionClaims(fixture.service.evidence)
	if fixture.database != nil && !fixture.database.closed {
		if err := closeRunnerDatabasePreflight(fixture.database, fixture.service.kernel.base.key, fixture.database.locked, nil); err != nil {
			t.Fatal(err)
		}
	}
	fixture.service.close(t)
}

func TestRunnerLedgerEntryAdmissionAcceptsExactlyFiveGeneratedPairs(t *testing.T) {
	for _, test := range []struct {
		name        string
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
		entryIndex  uint32
	}{
		{"empty-brand-new", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, 0},
		{"empty-inherited-first", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt, 0},
		{"empty-inherited-retry", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt, 0},
		{"partial-inherited-next", runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry, 1},
		{"partial-terminal-next", runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryAdmissionFixture(t, test.disposition, test.state, test.action)
			defer fixture.close(t)
			permit, err := fixture.prepare(t)
			if err != nil || !validRunnerLedgerEntryAdmissionPermit(permit) || permit.selection.entryIndex != test.entryIndex {
				t.Fatalf("permit=%+v err=%v", permit, err)
			}
			if fixture.connector.attempts != 1 || fixture.database.ledgerReadCalls != 4 || fixture.database.setRoleCalls != 1 ||
				fixture.database.lockCalls != 1 || fixture.database.unlockCalls != 0 || fixture.database.closeCalls != 0 ||
				fixture.database.beginCalls != 0 || fixture.database.backend.executeCalls != 0 ||
				fixture.database.backend.ledgerInsertCalls != 0 || fixture.database.backend.commitCalls != 0 ||
				!fixture.database.locked || fixture.database.closed || fixture.service.evidence.entryBindCalls != 1 ||
				fixture.service.evidence.entryConsumeCalls != 1 {
				t.Fatalf("admission escaped read-only boundary: database=%+v evidence=%+v", fixture.database, fixture.service.evidence)
			}
			if err := closeRunnerLedgerEntryAdmissionPermit(permit, nil); err != nil {
				t.Fatal(err)
			}
			if fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || !fixture.database.closed || fixture.database.locked {
				t.Fatalf("permit close did not release exact session: %+v", fixture.database)
			}
		})
	}
}

func TestRunnerLedgerEntryAdmissionPermitIsNonCopyableAndOneShot(t *testing.T) {
	fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
	defer fixture.close(t)
	permit, err := fixture.prepare(t)
	if err != nil || !validRunnerLedgerEntryAdmissionPermit(permit) {
		t.Fatalf("permit=%+v err=%v", permit, err)
	}
	copyValue := *permit
	if err := closeRunnerLedgerEntryAdmissionPermit(&copyValue, nil); !IsCode(err, CodeTransactionBoundary) || !validRunnerLedgerEntryAdmissionPermit(permit) {
		t.Fatalf("copy close err=%v original-valid=%t", err, validRunnerLedgerEntryAdmissionPermit(permit))
	}
	if err := closeRunnerLedgerEntryAdmissionPermit(&runnerLedgerEntryAdmissionPermit{}, nil); !IsCode(err, CodeTransactionBoundary) {
		t.Fatalf("literal close err=%v", err)
	}
	if err := closeRunnerLedgerEntryAdmissionPermit(permit, nil); err != nil {
		t.Fatal(err)
	}
	if err := closeRunnerLedgerEntryAdmissionPermit(permit, nil); !IsCode(err, CodeTransactionBoundary) {
		t.Fatalf("second close err=%v", err)
	}
	base := fixture.service.kernel.base
	if second, err := fixture.service.kernel.runner.prepareRunnerLedgerEntryAdmission(
		context.Background(), "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate, fixture.fact,
	); second != nil || !IsCode(err, CodeEvidenceRecoveryRequired) || fixture.connector.attempts != 1 {
		t.Fatalf("second transition permit=%+v err=%v attempts=%d", second, err, fixture.connector.attempts)
	}
}

func TestRunnerLedgerEntryAdmissionRejectsPermitAndUseRecordTamper(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*runnerLedgerEntryAdmissionPermit)
	}{
		{
			name: "permit-boundary",
			mutate: func(permit *runnerLedgerEntryAdmissionPermit) {
				permit.evidenceBoundary[0] ^= 0xff
			},
		},
		{
			name: "registry-use-boundary",
			mutate: func(permit *runnerLedgerEntryAdmissionPermit) {
				permit.use.mu.Lock()
				permit.use.boundary[0] ^= 0xff
				permit.use.mu.Unlock()
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			permit, err := fixture.prepare(t)
			if err != nil || !validRunnerLedgerEntryAdmissionPermit(permit) {
				t.Fatalf("permit=%+v err=%v", permit, err)
			}
			test.mutate(permit)
			if validRunnerLedgerEntryAdmissionPermit(permit) {
				t.Fatal("tampered permit remained valid")
			}
			if err := closeRunnerLedgerEntryAdmissionPermit(permit, nil); !IsCode(err, CodeTransactionBoundary) ||
				fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || !fixture.database.closed {
				t.Fatalf("tampered permit close err=%v database=%+v", err, fixture.database)
			}
		})
	}
}

func TestRunnerLedgerEntryAdmissionRejectsCrossProfileFactBeforeAuthority(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*runnerLedgerConsumerFact)
	}{
		{"profile", func(fact *runnerLedgerConsumerFact) { fact.profileID = "runner-ledger-consumer/v2" }},
		{"profile-digest", func(fact *runnerLedgerConsumerFact) {
			fact.profileDigest = testDigest("foreign-consumer-profile").String()
		}},
		{"action", func(fact *runnerLedgerConsumerFact) { fact.action = runnerLedgerConsumerRecoveryNotImplemented }},
		{"subject", func(fact *runnerLedgerConsumerFact) { fact.subjectDigest = testDigest("foreign-consumer-fact") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			fact := fixture.fact.clone()
			test.mutate(&fact)
			base := fixture.service.kernel.base
			permit, err := fixture.service.kernel.runner.prepareRunnerLedgerEntryAdmission(
				context.Background(), "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate, fact,
			)
			if permit != nil || !IsCode(err, CodeProjectionNotImplemented) || fixture.connector.attempts != 0 ||
				fixture.service.evidence.entryBindCalls != 0 || fixture.service.evidence.entryConsumeCalls != 0 {
				t.Fatalf("cross-profile permit=%+v err=%v fixture=%+v", permit, err, fixture)
			}
		})
	}
}

func TestRunnerLedgerEntryAdmissionClaimRejectsCopyLiteralAndSecondConsumption(t *testing.T) {
	fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
	defer fixture.close(t)
	base := fixture.service.kernel.base
	request := runnerLedgerEntryAdmissionClaimRequest{fact: fixture.fact, candidate: base.candidate}
	claim, err := fixture.service.evidence.bindRunnerLedgerEntryAdmissionClaim(context.Background(), request)
	if err != nil || !validRunnerLedgerEntryAdmissionClaim(claim, fixture.service.evidence, base.candidate.binding) {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	copyValue := *claim
	if boundary, err := fixture.service.evidence.consumeRunnerLedgerEntryAdmissionClaim(context.Background(), &copyValue, base.candidate); boundary.canonical != ([32]byte{}) || !IsCode(err, CodeEvidenceRecoveryRequired) || !validRunnerLedgerEntryAdmissionClaim(claim, fixture.service.evidence, base.candidate.binding) {
		t.Fatalf("copy consume boundary=%+v err=%v original-valid=%t", boundary, err, validRunnerLedgerEntryAdmissionClaim(claim, fixture.service.evidence, base.candidate.binding))
	}
	if boundary, err := fixture.service.evidence.consumeRunnerLedgerEntryAdmissionClaim(context.Background(), &runnerLedgerEntryAdmissionClaim{}, base.candidate); boundary.canonical != ([32]byte{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal consume boundary=%+v err=%v", boundary, err)
	}
	boundary, err := fixture.service.evidence.consumeRunnerLedgerEntryAdmissionClaim(context.Background(), claim, base.candidate)
	if err != nil || boundary.canonical == ([32]byte{}) || boundary.canonical != runnerLedgerEntryAdmissionEvidenceBoundaryDigest(boundary) {
		t.Fatalf("consume boundary=%+v err=%v", boundary, err)
	}
	if second, err := fixture.service.evidence.consumeRunnerLedgerEntryAdmissionClaim(context.Background(), claim, base.candidate); second.canonical != ([32]byte{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("second consume boundary=%+v err=%v", second, err)
	}
}

func TestRunnerLedgerEntryAdmissionRejectsLedgerCatalogAndEvidenceDrift(t *testing.T) {
	for _, test := range []struct {
		name     string
		prepare  func(*runnerLedgerEntryAdmissionFixture)
		wantCode ErrorCode
	}{
		{
			name: "final-ledger",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				row := ledgerRowFor(fixture.service.kernel.base.bundle.Manifest.SchemaBundle.Migrations[0], fixture.service.kernel.base.bundle.Manifest.SchemaBundleDigest)
				fixture.database.ledgerRowsByRead[3] = []LedgerRow{row}
			},
			wantCode: CodeInvalidLedger,
		},
		{
			name: "final-catalog",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.service.kernel.factory.mutatePrecondition = func(result *ProjectionResult[CatalogStateProjection]) {
					if len(fixture.service.kernel.factory.preconditionPhases) >= 3 {
						result.Digest = testDigest("entry-admission-final-catalog-drift")
					}
				}
			},
			wantCode: CodeCatalogDrift,
		},
		{
			name: "final-evidence",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.service.evidence.mutateBeforeEntryConsume = func(evidence *runnerLedgerPreflightEvidenceFake) {
					evidence.recovery.tailDigest = testDigest("entry-admission-evidence-drift")
				}
			},
			wantCode: CodeEvidenceRecoveryRequired,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			test.prepare(fixture)
			permit, err := fixture.prepare(t)
			if permit != nil || !IsCode(err, test.wantCode) || fixture.database.beginCalls != 0 ||
				fixture.database.backend.executeCalls != 0 || fixture.database.backend.ledgerInsertCalls != 0 ||
				fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || !fixture.database.closed {
				t.Fatalf("drift permit=%+v err=%v database=%+v", permit, err, fixture.database)
			}
		})
	}
}

func TestRunnerLedgerEntryAdmissionFreshSessionFaultsFailClosed(t *testing.T) {
	for _, test := range []struct {
		name        string
		prepare     func(*runnerLedgerEntryAdmissionFixture)
		wantCode    ErrorCode
		wantConnect int
		wantUnlock  int
		wantClose   int
	}{
		{
			name: "connector-unconfigured",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.service.kernel.runner.Connector = nil
			},
			wantCode: CodeProjectionNotImplemented,
		},
		{
			name: "connect",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.connector.err = errors.New("secret-connect")
			},
			wantCode: CodeTransactionBoundary, wantConnect: 1,
		},
		{
			name: "connect-returned-session",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.connector.err = errors.New("secret-connect")
				fixture.connector.returnSessionOnError = true
			},
			wantCode: CodeTransactionBoundary, wantConnect: 1, wantClose: 1,
		},
		{
			name: "settings",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.database.settingsErr = errors.New("secret-settings")
			},
			wantCode: CodeTransactionBoundary, wantConnect: 1, wantClose: 1,
		},
		{
			name: "lock",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.database.lockErr = errors.New("secret-lock")
			},
			wantCode: CodeTransactionBoundary, wantConnect: 1, wantClose: 1,
		},
		{
			name: "migration-authority",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.service.kernel.factory.projectionErr[AuthorityPhaseMigrationRole] = fail(CodeAuthorityDrift, "fixture", "secret", errors.New("secret-authority"))
			},
			wantCode: CodeAuthorityDrift, wantConnect: 1, wantUnlock: 1, wantClose: 1,
		},
		{
			name: "session-identity",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.database.snapshotMetadataMutate[AuthorityPhaseMigrationRole] = func(metadata *SnapshotMetadata) {
					metadata.ServerVersionNum++
				}
			},
			wantCode: CodeProjectionMetadataMismatch, wantConnect: 1, wantUnlock: 1, wantClose: 1,
		},
		{
			name: "final-ledger-read",
			prepare: func(fixture *runnerLedgerEntryAdmissionFixture) {
				fixture.database.ledgerReadErr[3] = errors.New("secret-ledger-read")
			},
			wantCode: CodeTransactionBoundary, wantConnect: 1, wantUnlock: 1, wantClose: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			test.prepare(fixture)
			permit, err := fixture.prepare(t)
			var stable *Error
			if permit != nil || !errors.As(err, &stable) || stable.Code != test.wantCode || stable.Err != nil ||
				strings.Contains(err.Error(), "secret-") || fixture.connector.attempts != test.wantConnect ||
				fixture.database.unlockCalls != test.wantUnlock || fixture.database.closeCalls != test.wantClose ||
				fixture.database.beginCalls != 0 || fixture.database.backend.executeCalls != 0 ||
				fixture.database.backend.ledgerInsertCalls != 0 || fixture.database.backend.commitCalls != 0 ||
				fixture.service.evidence.entryBindCalls != 1 || fixture.service.evidence.entryConsumeCalls != 0 {
				t.Fatalf("fault permit=%+v err=%#v connector=%+v database=%+v evidence=%+v", permit, stable, fixture.connector, fixture.database, fixture.service.evidence)
			}
		})
	}
}

func TestRunnerLedgerEntryAdmissionContextAndEvidenceRevocation(t *testing.T) {
	t.Run("pre-canceled", func(t *testing.T) {
		fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		base := fixture.service.kernel.base
		permit, err := fixture.service.kernel.runner.prepareRunnerLedgerEntryAdmission(
			ctx, "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate, fixture.fact,
		)
		if permit != nil || !IsCode(err, CodeContextCanceled) || fixture.connector.attempts != 0 ||
			fixture.service.evidence.entryBindCalls != 0 || fixture.database.closeCalls != 0 {
			t.Fatalf("pre-canceled permit=%+v err=%v fixture=%+v", permit, err, fixture)
		}
	})

	t.Run("canceled-after-claim-bind", func(t *testing.T) {
		fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.service.evidence.afterEntryBind = cancel
		base := fixture.service.kernel.base
		permit, err := fixture.service.kernel.runner.prepareRunnerLedgerEntryAdmission(
			ctx, "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate, fixture.fact,
		)
		if permit != nil || !IsCode(err, CodeContextCanceled) || fixture.connector.attempts != 0 ||
			fixture.service.evidence.entryBindCalls != 1 || fixture.service.evidence.entryConsumeCalls != 0 ||
			fixture.database.closeCalls != 0 {
			t.Fatalf("post-bind cancel permit=%+v err=%v fixture=%+v", permit, err, fixture)
		}
	})

	t.Run("canceled-after-evidence-consume", func(t *testing.T) {
		fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.service.evidence.afterEntryConsume = cancel
		base := fixture.service.kernel.base
		permit, err := fixture.service.kernel.runner.prepareRunnerLedgerEntryAdmission(
			ctx, "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate, fixture.fact,
		)
		if permit != nil || !IsCode(err, CodeContextCanceled) || fixture.connector.attempts != 1 ||
			fixture.service.evidence.entryBindCalls != 1 || fixture.service.evidence.entryConsumeCalls != 1 ||
			fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || !fixture.database.closed {
			t.Fatalf("post-consume cancel permit=%+v err=%v database=%+v evidence=%+v", permit, err, fixture.database, fixture.service.evidence)
		}
		if second, secondErr := fixture.service.kernel.runner.prepareRunnerLedgerEntryAdmission(
			context.Background(), "test-only", base.bundle, base.plans, fixture.service.evidence, base.candidate, fixture.fact,
		); second != nil || !IsCode(secondErr, CodeEvidenceRecoveryRequired) || fixture.connector.attempts != 1 {
			t.Fatalf("post-consume retry permit=%+v err=%v attempts=%d", second, secondErr, fixture.connector.attempts)
		}
	})

	t.Run("evidence-close-invalidates-live-permit", func(t *testing.T) {
		fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
		defer fixture.close(t)
		permit, err := fixture.prepare(t)
		if err != nil || !validRunnerLedgerEntryAdmissionPermit(permit) {
			t.Fatalf("permit=%+v err=%v", permit, err)
		}
		if err := fixture.service.evidence.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if validRunnerLedgerEntryAdmissionPermit(permit) {
			t.Fatal("evidence-close left permit valid")
		}
		if err := closeRunnerLedgerEntryAdmissionPermit(permit, nil); !IsCode(err, CodeTransactionBoundary) ||
			fixture.database.unlockCalls != 1 || fixture.database.closeCalls != 1 || !fixture.database.closed {
			t.Fatalf("revoked permit close err=%v database=%+v", err, fixture.database)
		}
	})
}

func TestRunnerLedgerEntryAdmissionCleanupFailureDominatesNotImplemented(t *testing.T) {
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
			fixture := newRunnerLedgerEntryAdmissionFixture(t, runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt)
			defer fixture.close(t)
			fixture.database.unlockErr, fixture.database.closeErr = test.unlockErr, test.closeErr
			permit, err := fixture.prepare(t)
			if err != nil || permit == nil {
				t.Fatalf("prepare permit=%+v err=%v", permit, err)
			}
			var stable *Error
			closeErr := closeRunnerLedgerEntryAdmissionPermit(permit, nil)
			if !errors.As(closeErr, &stable) || stable.Code != CodeTransactionBoundary || stable.Op != test.wantOp || stable.Err != nil ||
				strings.Contains(closeErr.Error(), "secret-") || fixture.database.unlockCalls != test.wantUnlock || fixture.database.closeCalls != 1 {
				t.Fatalf("cleanup err=%#v database=%+v", stable, fixture.database)
			}
		})
	}
}

func TestRunnerLedgerEntryAdmissionProductionGraphHasNoWriterOrExternalEdge(t *testing.T) {
	files := []string{
		"runner_ledger_entry_admission_claim.go", "runner_ledger_entry_admission_permit.go",
		"runner_ledger_catalog_preflight.go", "runner_ledger_consumer_service.go",
	}
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true, "Append": true, "AppendDurable": true,
		"Insert": true, "prepareCurrentDatabaseSession": true, "prepareCurrentTransaction": true,
		"prepareCurrentStatement": true, "appendCurrentStatementIntent": true, "runCurrentSingleEntry": true,
		"ReserveAndActivateSuccessor": true,
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
	evidenceFile, err := parser.ParseFile(token.NewFileSet(), "evidence_session.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	evidenceFunctions := map[string]bool{
		"bindRunnerLedgerEntryAdmissionClaim":    false,
		"consumeRunnerLedgerEntryAdmissionClaim": false,
	}
	for _, declaration := range evidenceFile.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil {
			continue
		}
		if _, reviewed := evidenceFunctions[function.Name.Name]; !reviewed {
			continue
		}
		evidenceFunctions[function.Name.Name] = true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			called := ""
			switch target := call.Fun.(type) {
			case *ast.Ident:
				called = target.Name
			case *ast.SelectorExpr:
				called = target.Sel.Name
			}
			if forbidden[called] {
				t.Fatalf("evidence_session.go %s acquired forbidden call edge %s", function.Name.Name, called)
			}
			return true
		})
	}
	for function, found := range evidenceFunctions {
		if !found {
			t.Fatalf("evidence entry-admission binder %s was not found", function)
		}
	}
	permitType := reflect.TypeOf(runnerLedgerEntryAdmissionPermit{})
	for index := 0; index < permitType.NumMethod(); index++ {
		t.Fatalf("permit unexpectedly exposes method %s", permitType.Method(index).Name)
	}
}

func TestRunnerLedgerEntryAdmissionAuthorityHasOnlyReviewedProductionConsumers(t *testing.T) {
	allowed := map[string]map[string]bool{
		"evidence_session.go": {
			"runnerLedgerEntryAdmissionClaimBinder":              true,
			"runnerLedgerEntryAdmissionClaimRequest":             true,
			"runnerLedgerEntryAdmissionEvidenceBoundary":         true,
			"runnerLedgerEntryAdmissionEvidenceFacts":            true,
			"runnerLedgerEntryAdmissionClaim":                    true,
			"bindRunnerLedgerEntryAdmissionClaimFromEvidence":    true,
			"consumeRunnerLedgerEntryAdmissionClaimFromEvidence": true,
			"revokeRunnerLedgerEntryAdmissionClaims":             true,
		},
		"runner_ledger_consumer_service.go": {
			"prepareRunnerLedgerEntryAdmission":     true,
			"closeRunnerLedgerEntryAdmissionPermit": true,
		},
	}
	ownedFiles := map[string]bool{
		"runner_ledger_entry_admission_claim.go":  true,
		"runner_ledger_entry_admission_permit.go": true,
	}
	symbols := map[string]bool{
		"runnerLedgerEntryAdmissionClaimBinder":              true,
		"runnerLedgerEntryAdmissionClaimRequest":             true,
		"runnerLedgerEntryAdmissionEvidenceFacts":            true,
		"runnerLedgerEntryAdmissionEvidenceBoundary":         true,
		"runnerLedgerEntryAdmissionClaim":                    true,
		"runnerLedgerEntryAdmissionUseRecord":                true,
		"runnerLedgerEntryAdmissionPermit":                   true,
		"runnerLedgerEntryAdmissionPermitBinding":            true,
		"runnerLedgerEntryAdmissionPermitRegistryRecord":     true,
		"bindRunnerLedgerEntryAdmissionClaimFromEvidence":    true,
		"consumeRunnerLedgerEntryAdmissionClaimFromEvidence": true,
		"validRunnerLedgerEntryAdmissionUse":                 true,
		"revokeRunnerLedgerEntryAdmissionClaims":             true,
		"prepareRunnerLedgerEntryAdmission":                  true,
		"bindRunnerLedgerEntryAdmissionPermit":               true,
		"validRunnerLedgerEntryAdmissionPermit":              true,
		"closeRunnerLedgerEntryAdmissionPermit":              true,
		"runnerLedgerEntryAdmissionClaimRegistry":            true,
		"runnerLedgerEntryAdmissionUseByEvidenceBinder":      true,
		"runnerLedgerEntryAdmissionPermitRegistry":           true,
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
					t.Fatalf("entry-admission authority call %s acquired unreviewed production consumer %s", called, name)
					return false
				}
			}
			identifier, ok := node.(*ast.Ident)
			if !ok || !symbols[identifier.Name] || allowed[name][identifier.Name] {
				return true
			}
			t.Fatalf("entry-admission authority %s acquired unreviewed production consumer %s", identifier.Name, name)
			return false
		})
	}
}
