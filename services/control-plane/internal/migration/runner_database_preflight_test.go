package migration

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
	"time"
)

func TestRunnerClosesEvidenceWhenDatabaseConnectorIsUnconfigured(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
	sink := &runnerEvidenceSinkFake{}
	before := liveVerifiedEvidenceRunBindings()
	runner := Runner{Trust: verifier, Evidence: sink}
	_, err := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != CodeProjectionNotImplemented || migrationErr.Op != "runner-database-connector" || migrationErr.Err != nil {
		t.Fatalf("unconfigured database connector boundary=%#v", migrationErr)
	}
	if sink.session == nil || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 || liveVerifiedEvidenceRunBindings() != before {
		t.Fatalf("unconfigured connector leaked evidence authority: session=%+v live=%d/%d", sink.session, liveVerifiedEvidenceRunBindings(), before)
	}
}

func TestRunnerDatabasePreflightFaultsCloseEvidenceAndDatabase(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*runnerPreflightConnector, *runnerPreflightSession, *runnerPreflightProjectorFactory, *runnerEvidenceSinkFake)
		wantCode  ErrorCode
		wantClose int
	}{
		{"connect", func(c *runnerPreflightConnector, _ *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			c.err = errors.New("secret-connect")
		}, CodeTransactionBoundary, 0},
		{"connect-closed-result", func(c *runnerPreflightConnector, _ *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			c.err = errors.New("secret-connect")
			c.returnSessionOnError = true
		}, CodeTransactionBoundary, 1},
		{"connected-snapshot", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.snapshotOpenErr[AuthorityPhaseConnectedSession] = errors.New("secret-connected-snapshot")
		}, CodeTransactionBoundary, 1},
		{"connected-snapshot-close", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.snapshotCloseErr[AuthorityPhaseConnectedSession] = errors.New("secret-connected-snapshot-close")
		}, CodeTransactionBoundary, 1},
		{"projector", func(_ *runnerPreflightConnector, _ *runnerPreflightSession, f *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			f.factoryErr[AuthorityPhaseConnectedSession] = errors.New("secret-projector")
		}, CodeTransactionBoundary, 1},
		{"connected-projection", func(_ *runnerPreflightConnector, _ *runnerPreflightSession, f *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			f.projectionErr[AuthorityPhaseConnectedSession] = fail(CodeAuthorityDrift, "fake", "secret", errors.New("secret-projection"))
		}, CodeAuthorityDrift, 1},
		{"connected-result", func(_ *runnerPreflightConnector, _ *runnerPreflightSession, f *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			f.mutateResult[AuthorityPhaseConnectedSession] = func(result *ProjectionResult[AuthorityProjection]) {
				result.Digest = testDigest("wrong-connected-result")
			}
		}, CodeAuthorityDrift, 1},
		{"unsupported-major", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.snapshotMetadataMutate[AuthorityPhaseConnectedSession] = func(metadata *SnapshotMetadata) {
				metadata.PostgresMajor = 14
				metadata.ServerVersionNum = 140000
			}
		}, CodeUnsupported, 1},
		{"settings", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.settingsErr = errors.New("secret-settings")
		}, CodeTransactionBoundary, 1},
		{"lock", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.lockErr = errors.New("secret-lock")
		}, CodeTransactionBoundary, 1},
		{"migration-snapshot", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.snapshotOpenErr[AuthorityPhaseMigrationRole] = fail(CodeProjectionSnapshotInvalid, "fake", "secret", errors.New("secret-migration-snapshot"))
		}, CodeProjectionSnapshotInvalid, 1},
		{"migration-snapshot-close", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.snapshotCloseErr[AuthorityPhaseMigrationRole] = errors.New("secret-migration-snapshot-close")
		}, CodeTransactionBoundary, 1},
		{"migration-projection", func(_ *runnerPreflightConnector, _ *runnerPreflightSession, f *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			f.projectionErr[AuthorityPhaseMigrationRole] = fail(CodeAuthorityDrift, "fake", "secret", errors.New("secret-migration-projection"))
		}, CodeAuthorityDrift, 1},
		{"same-session-metadata", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.snapshotMetadataMutate[AuthorityPhaseMigrationRole] = func(metadata *SnapshotMetadata) { metadata.ServerVersionNum++ }
		}, CodeProjectionMetadataMismatch, 1},
		{"precondition-session-metadata", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.snapshotMetadataNth[3] = func(metadata *SnapshotMetadata) { metadata.DatabaseName += "_drift" }
		}, CodeProjectionMetadataMismatch, 1},
		{"unlock", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.unlockErr = errors.New("secret-unlock")
		}, CodeTransactionBoundary, 1},
		{"database-close", func(_ *runnerPreflightConnector, s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			s.closeErr = errors.New("secret-close")
		}, CodeTransactionBoundary, 1},
		{"database-close-dominates", func(_ *runnerPreflightConnector, s *runnerPreflightSession, f *runnerPreflightProjectorFactory, _ *runnerEvidenceSinkFake) {
			f.projectionErr[AuthorityPhaseMigrationRole] = fail(CodeAuthorityDrift, "fake", "secret", errors.New("secret-projection"))
			s.closeErr = errors.New("secret-close")
		}, CodeTransactionBoundary, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, decision := buildExactAdmissionRuntime(t)
			session := newRunnerPreflightSession()
			connector := &runnerPreflightConnector{session: session}
			factory := &runnerPreflightProjectorFactory{}
			factory.initialize()
			sink := &runnerEvidenceSinkFake{}
			test.configure(connector, session, factory, sink)
			verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
			runner := Runner{Trust: verifier, Evidence: sink, Connector: connector, projectionFactory: factory}
			_, err := runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
			var migrationErr *Error
			if !errors.As(err, &migrationErr) || migrationErr.Code != test.wantCode || migrationErr.Err != nil || containsErrorText(err, "secret-") {
				t.Fatalf("preflight fault escaped stable mapping: err=%#v", migrationErr)
			}
			wantBegin := 0
			if test.name == "unlock" || test.name == "database-close" {
				wantBegin = 1
			}
			if sink.session == nil || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 || session.closeCalls != test.wantClose || session.beginCalls != wantBegin || session.transaction.rollbackCalls != wantBegin || session.transaction.executeCalls != 0 || session.transaction.execCalls != 0 || session.transaction.commitCalls != 0 || session.queryCalls != 0 {
				t.Fatalf("fault cleanup crossed a forbidden boundary: evidence=%+v session=%+v", sink.session, session)
			}
			if session.locked && session.closeErr == nil {
				t.Fatal("successful database close left the fake session lock held")
			}
		})
	}
}

func TestRunnerLedgerAndInitialPreconditionFaultsRemainReadOnly(t *testing.T) {
	for _, test := range []struct {
		name             string
		configure        func(*runnerPreflightSession, *runnerPreflightProjectorFactory, *RuntimeBundle)
		wantCode         ErrorCode
		wantOp           string
		wantLedgerReads  int
		wantPrecondition int
	}{
		{"ledger-first-read", func(s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *RuntimeBundle) {
			s.ledgerReadErr[1] = errors.New("secret-ledger-first")
		}, CodeTransactionBoundary, "runner-ledger-preflight", 1, 0},
		{"complete-ledger", func(s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, bundle *RuntimeBundle) {
			s.ledgerRowsByRead = [][]LedgerRow{{ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[0], bundle.Manifest.SchemaBundleDigest)}}
		}, CodeProjectionNotImplemented, "runner-complete-ledger-preflight", 1, 0},
		{"precondition-projection", func(_ *runnerPreflightSession, f *runnerPreflightProjectorFactory, _ *RuntimeBundle) {
			f.preconditionErr = fail(CodeCatalogDrift, "fake", "secret", errors.New("secret-precondition"))
		}, CodeCatalogDrift, "runner-initial-precondition", 1, 1},
		{"precondition-result", func(_ *runnerPreflightSession, f *runnerPreflightProjectorFactory, _ *RuntimeBundle) {
			f.mutatePrecondition = func(result *ProjectionResult[CatalogStateProjection]) {
				result.Digest = testDigest("wrong-precondition")
			}
		}, CodeCatalogDrift, "runner-initial-precondition", 1, 1},
		{"precondition-metadata", func(_ *runnerPreflightSession, f *runnerPreflightProjectorFactory, _ *RuntimeBundle) {
			f.mutatePrecondition = func(result *ProjectionResult[CatalogStateProjection]) { result.Metadata.QueryCount++ }
		}, CodeProjectionMetadataMismatch, "runner-initial-precondition", 1, 1},
		{"ledger-second-read", func(s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, _ *RuntimeBundle) {
			s.ledgerReadErr[2] = errors.New("secret-ledger-second")
		}, CodeTransactionBoundary, "runner-ledger-preflight", 2, 1},
		{"ledger-drift", func(s *runnerPreflightSession, _ *runnerPreflightProjectorFactory, bundle *RuntimeBundle) {
			row := ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[0], bundle.Manifest.SchemaBundleDigest)
			s.ledgerRowsByRead = [][]LedgerRow{{}, {row}}
		}, CodeInvalidLedger, "runner-ledger-preflight", 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, decision := buildExactAdmissionRuntime(t)
			bundle, err := LoadRuntimeBundle(raw, decision)
			if err != nil {
				t.Fatal(err)
			}
			session := newRunnerPreflightSession()
			factory := &runnerPreflightProjectorFactory{}
			factory.initialize()
			test.configure(session, factory, bundle)
			sink := &runnerEvidenceSinkFake{}
			verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
			runner := Runner{Trust: verifier, Evidence: sink, Connector: &runnerPreflightConnector{session: session}, projectionFactory: factory, Ledger: &fakeLedgerStore{}}
			_, err = runner.Run(context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
			var migrationErr *Error
			if !errors.As(err, &migrationErr) || migrationErr.Code != test.wantCode || migrationErr.Op != test.wantOp || migrationErr.Err != nil || containsErrorText(err, "secret-") {
				t.Fatalf("ledger/precondition fault mapping=%#v", migrationErr)
			}
			if sink.session == nil || sink.session.closeCalls != 1 || session.ledgerReadCalls != test.wantLedgerReads || len(factory.preconditionPhases) != test.wantPrecondition || session.unlockCalls != 1 || session.closeCalls != 1 || session.beginCalls != 0 || session.queryCalls != 0 || session.backend.ledgerReadCalls != 0 || session.backend.ledgerInsertCalls != 0 || session.backend.executeCalls != 0 || session.backend.commitCalls != 0 {
				t.Fatalf("ledger/precondition fault crossed write boundary: session=%+v factory=%+v backend=%+v", session, factory, session.backend)
			}
		})
	}
}

func TestRunnerAuthorityProjectionResultIsTotallyChecked(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	contract := fixture.authority
	phase := AuthorityPhaseConnectedSession
	snapshot := runnerPreflightSnapshotMetadata(phase)
	valid := runnerPreflightProjectionResult(t, snapshot, contract, phase)
	if err := validateRunnerAuthorityProjectionResult(valid, snapshot, contract, phase); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ProjectionResult[AuthorityProjection]){
		"projection": func(result *ProjectionResult[AuthorityProjection]) {
			result.Projection.CurrentUser = MigrationOwnerRole
		},
		"digest": func(result *ProjectionResult[AuthorityProjection]) { result.Digest = testDigest("wrong-result") },
		"subject": func(result *ProjectionResult[AuthorityProjection]) {
			result.Metadata.VerifiedSubjectDigest = testDigest("wrong-subject")
		},
		"query-count": func(result *ProjectionResult[AuthorityProjection]) {
			result.Metadata.QueryCount--
		},
		"row-count": func(result *ProjectionResult[AuthorityProjection]) { result.Metadata.RowCount = 0 },
		"total-bytes": func(result *ProjectionResult[AuthorityProjection]) {
			result.Metadata.TotalBytes = 0
		},
		"snapshot": func(result *ProjectionResult[AuthorityProjection]) { result.Metadata.Snapshot.ServerVersionNum++ },
	} {
		t.Run(name, func(t *testing.T) {
			fault := cloneProjectionValue(valid)
			mutate(&fault)
			if err := validateRunnerAuthorityProjectionResult(fault, snapshot, contract, phase); err == nil {
				t.Fatal("mutated authority result was accepted")
			}
		})
	}
}

func TestRunnerLedgerPrefixExcludesOnlyObservationalColumns(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatal(err)
	}
	row := ledgerRowFor(bundle.Manifest.SchemaBundle.Migrations[0], bundle.Manifest.SchemaBundleDigest)
	row.AppliedAt = time.Unix(10, 0).UTC()
	row.AppliedBy = "first-observer"
	leftRow, err := commitIntentLedgerRowFromObserved(row)
	if err != nil {
		t.Fatal(err)
	}
	row.AppliedAt = time.Unix(20, 0).UTC()
	row.AppliedBy = "second-observer"
	rightRow, err := commitIntentLedgerRowFromObserved(row)
	if err != nil {
		t.Fatal(err)
	}
	leftDigest, err := LedgerPrefixDigest([]CommitIntentLedgerRow{leftRow})
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := LedgerPrefixDigest([]CommitIntentLedgerRow{rightRow})
	if err != nil {
		t.Fatal(err)
	}
	left := runnerLedgerPrefix{rows: []CommitIntentLedgerRow{leftRow}, digest: leftDigest, head: row.MigrationID}
	right := runnerLedgerPrefix{rows: []CommitIntentLedgerRow{rightRow}, digest: rightDigest, head: row.MigrationID}
	if !sameRunnerLedgerPrefix(left, right) {
		t.Fatal("observational applied_at/applied_by changed the exact identity prefix")
	}
	right.rows[0].MigrationName += "-drift"
	right.digest, err = LedgerPrefixDigest(right.rows)
	if err != nil {
		t.Fatal(err)
	}
	if sameRunnerLedgerPrefix(left, right) {
		t.Fatal("ledger-backed identity drift was ignored")
	}
}

func TestRunnerProjectionFactoryIsNotPubliclyInjectable(t *testing.T) {
	field, ok := reflect.TypeOf(Runner{}).FieldByName("projectionFactory")
	if !ok || field.PkgPath == "" {
		t.Fatal("runner projection factory became a public caller-supplied seam")
	}
	var _ runnerAuthorityProjectorFactory = (*runnerPreflightProjectorFactory)(nil)
}

func TestRunnerDatabasePreflightHasNoMigrationOrLedgerCallEdge(t *testing.T) {
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true,
		"Queryer": true, "ServerMajor": true, "Insert": true, "Exec": true, "Query": true, "QueryRow": true,
		"Ledger": true, "Catalog": true, "Intermediate": true,
		"ProjectCatalog": true, "ProjectTransitionState": true,
	}
	for _, name := range []string{"runner_database_preflight.go", "runner_prepared_session.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && forbidden[selector.Sel.Name] {
				t.Fatalf("%s acquired forbidden %s call edge", name, selector.Sel.Name)
			}
			return true
		})
	}
}

func assertRunnerAuthorityPreflightLifecycle(t *testing.T, connector *runnerPreflightConnector, factory *runnerPreflightProjectorFactory) {
	t.Helper()
	session := connector.session
	wantSnapshots := []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole, AuthorityPhaseMigrationRole}
	wantFactory := []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole, AuthorityPhaseMigrationRole, AuthorityPhaseMigrationTransaction, AuthorityPhaseMigrationTransaction}
	wantAuthority := []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole, AuthorityPhaseMigrationTransaction, AuthorityPhaseMigrationTransaction}
	wantPrecondition := []AuthorityPhase{AuthorityPhaseMigrationRole, AuthorityPhaseMigrationTransaction, AuthorityPhaseMigrationTransaction}
	wantTransactionSteps := []string{"begin", "profile-enter", "metadata", "profile-restore", "boundary", "profile-enter", "metadata", "profile-restore", "boundary", "rollback"}
	transaction := session.transaction
	statementSnapshots := len(factory.snapshotMetadata) == 5 && factory.snapshotMetadata[3].MigrationID != nil && factory.snapshotMetadata[4].MigrationID != nil && *factory.snapshotMetadata[3].MigrationID == "000001" && *factory.snapshotMetadata[4].MigrationID == "000001" && factory.snapshotMetadata[3].StatementIndex == nil && factory.snapshotMetadata[4].StatementIndex != nil && *factory.snapshotMetadata[4].StatementIndex == 0
	if connector.attempts != 1 || session == nil || session.setRoleCalls != 1 || session.lockCalls != 1 || session.unlockCalls != 1 || session.closeCalls != 1 || session.serverMajorCalls != 0 || session.boundaryCalls != 0 || session.beginCalls != 1 || session.queryCalls != 0 || session.ledgerReadCalls != 2 || transaction == nil || transaction.profileEnterCalls != 2 || transaction.profileRestoreCalls != 2 || transaction.profile != "execution" || transaction.metadata[7] != int64(300000) || transaction.metadata[8] != int64(30000) || transaction.metadata[9] != int64(60000) || transaction.metadataReadCalls != 2 || transaction.queryCalls != 0 || transaction.execCalls != 0 || transaction.executeCalls != 0 || transaction.boundaryCalls != 2 || transaction.commitCalls != 0 || transaction.rollbackCalls != 1 || transaction.active || transaction.status != 'I' || !reflect.DeepEqual(transaction.steps, wantTransactionSteps) || session.backend.ledgerReadCalls != 0 || session.backend.ledgerInsertCalls != 0 || session.backend.executeCalls != 0 || session.backend.commitCalls != 0 || !session.closed || session.locked || session.roleConfigured || !reflect.DeepEqual(session.snapshotPhases, wantSnapshots) || !reflect.DeepEqual(session.snapshotClosePhases, wantSnapshots) || !reflect.DeepEqual(factory.factoryPhases, wantFactory) || !reflect.DeepEqual(factory.projectionPhases, wantAuthority) || !reflect.DeepEqual(factory.preconditionPhases, wantPrecondition) || !statementSnapshots || liveRunnerPreparedCurrentSessions() != 0 || liveRunnerPreparedCurrentTransactions() != 0 || liveRunnerPreparedCurrentStatements() != 0 {
		t.Fatalf("runner authority preflight lifecycle mismatch: connector=%+v session=%+v transaction=%+v factory=%+v", connector, session, transaction, factory)
	}
}

type runnerPreflightConnector struct {
	session              *runnerPreflightSession
	err                  error
	returnSessionOnError bool
	attempts             int
}

func (connector *runnerPreflightConnector) Connect(context.Context, string) (DatabaseSession, error) {
	connector.attempts++
	if connector.err != nil && !connector.returnSessionOnError {
		return nil, connector.err
	}
	return connector.session, connector.err
}

type runnerPreflightSession struct {
	backend                *fakeBackend
	roleConfigured         bool
	executionPolicy        ExecutionPolicy
	locked                 bool
	projectionActive       bool
	closed                 bool
	settingsErr            error
	lockErr                error
	unlockErr              error
	closeErr               error
	beginErr               error
	beginReturnsOnError    bool
	beginReturnsNil        bool
	snapshotOpenErr        map[AuthorityPhase]error
	snapshotCloseErr       map[AuthorityPhase]error
	snapshotMetadataMutate map[AuthorityPhase]func(*SnapshotMetadata)
	snapshotMetadataNth    map[int]func(*SnapshotMetadata)
	snapshotPhases         []AuthorityPhase
	snapshotClosePhases    []AuthorityPhase
	ledgerRowsByRead       [][]LedgerRow
	ledgerReadErr          map[int]error
	afterLedgerRead        map[int]func()
	setRoleCalls           int
	lockCalls              int
	unlockCalls            int
	unlockKeys             []int64
	closeCalls             int
	serverMajorCalls       int
	boundaryCalls          int
	beginCalls             int
	queryCalls             int
	ledgerReadCalls        int
	transaction            *runnerPreflightTransaction
}

func newRunnerPreflightSession() *runnerPreflightSession {
	session := &runnerPreflightSession{
		backend: &fakeBackend{}, snapshotOpenErr: map[AuthorityPhase]error{}, snapshotCloseErr: map[AuthorityPhase]error{},
		snapshotMetadataMutate: map[AuthorityPhase]func(*SnapshotMetadata){}, snapshotMetadataNth: map[int]func(*SnapshotMetadata){},
		ledgerReadErr: map[int]error{}, afterLedgerRead: map[int]func(){},
	}
	session.transaction = newRunnerPreflightTransaction(session)
	return session
}

func (session *runnerPreflightSession) Queryer() Queryer {
	session.queryCalls++
	return fakeQueryer{backend: session.backend}
}

func (session *runnerPreflightSession) ServerMajor(context.Context) (int, error) {
	session.serverMajorCalls++
	return 16, nil
}

func (session *runnerPreflightSession) SetRoleAndSettings(_ context.Context, policy ExecutionPolicy) error {
	session.setRoleCalls++
	if session.closed || session.projectionActive || session.roleConfigured || session.locked {
		return errors.New("invalid role lifecycle")
	}
	if session.settingsErr != nil {
		return session.settingsErr
	}
	session.roleConfigured = true
	session.executionPolicy = policy
	return nil
}

func (session *runnerPreflightSession) AcquireAdvisoryLock(context.Context, int64) error {
	session.lockCalls++
	if session.closed || session.projectionActive || !session.roleConfigured || session.locked {
		return errors.New("invalid lock lifecycle")
	}
	if session.lockErr != nil {
		return session.lockErr
	}
	session.locked = true
	return nil
}

func (session *runnerPreflightSession) Boundary(context.Context, int64) (BoundaryState, error) {
	session.boundaryCalls++
	return BoundaryState{TxStatus: 'I', CurrentUser: MigrationOwnerRole, LockHeld: session.locked}, nil
}

func (session *runnerPreflightSession) readRunnerLedgerPrefix(context.Context) ([]LedgerRow, error) {
	session.ledgerReadCalls++
	if session.closed || session.projectionActive || !session.roleConfigured || !session.locked {
		return nil, errors.New("invalid ledger lifecycle")
	}
	if err := session.ledgerReadErr[session.ledgerReadCalls]; err != nil {
		return nil, err
	}
	if after := session.afterLedgerRead[session.ledgerReadCalls]; after != nil {
		after()
	}
	if session.ledgerReadCalls <= len(session.ledgerRowsByRead) {
		return cloneProjectionValue(session.ledgerRowsByRead[session.ledgerReadCalls-1]), nil
	}
	return []LedgerRow{}, nil
}

func (session *runnerPreflightSession) BeginMigration(context.Context) (MigrationTransaction, error) {
	session.beginCalls++
	if session.closed || session.projectionActive || !session.roleConfigured || !session.locked || session.transaction.active {
		return nil, errors.New("invalid migration transaction lifecycle")
	}
	session.transaction.active = true
	session.transaction.status = 'T'
	session.transaction.commitClaimed = false
	session.transaction.commitConnectionClosed = false
	session.transaction.commitClosedAfter = false
	session.transaction.profile = "execution"
	session.transaction.steps = append(session.transaction.steps, "begin")
	session.transaction.setTimeoutMetadata(session.executionPolicy.StatementTimeoutMS, session.executionPolicy.LockTimeoutMS, session.executionPolicy.IdleInTransactionSessionTimeoutMS)
	if session.beginErr != nil && !session.beginReturnsOnError {
		session.transaction.active = false
		session.transaction.status = 'I'
		return nil, session.beginErr
	}
	if session.beginReturnsNil {
		session.transaction.active = false
		session.transaction.status = 'I'
		return nil, session.beginErr
	}
	return session.transaction, session.beginErr
}

func (session *runnerPreflightSession) UnlockAndReset(_ context.Context, key int64) error {
	session.unlockCalls++
	session.unlockKeys = append(session.unlockKeys, key)
	if session.unlockErr != nil {
		return session.unlockErr
	}
	if session.transaction.active {
		return errors.New("cannot unlock while migration transaction remains active")
	}
	session.locked = false
	session.roleConfigured = false
	session.executionPolicy = ExecutionPolicy{}
	return nil
}

func (session *runnerPreflightSession) Close(context.Context) error {
	session.closeCalls++
	session.closed = true
	session.locked = false
	session.transaction.active = false
	session.transaction.status = 'I'
	if session.closeErr != nil {
		return session.closeErr
	}
	return nil
}

type runnerPreflightTransaction struct {
	session                *runnerPreflightSession
	active                 bool
	status                 byte
	metadata               []any
	afterMetadataScan      func()
	metadataReadErr        error
	boundaryErr            error
	boundaryMutate         func(*BoundaryState)
	rollbackErr            error
	rollbackLeavesOpen     bool
	profileEnterErr        error
	profileRestoreErr      error
	profile                string
	profileEnterCalls      int
	profileRestoreCalls    int
	metadataReadCalls      int
	queryCalls             int
	execCalls              int
	executeCalls           int
	executeAllowed         bool
	executeErr             error
	executeMutate          func([]byte)
	executedSQL            [][]byte
	ledgerInsertErr        error
	ledgerReadErr          error
	ledgerReadMutate       func([]LedgerRow) []LedgerRow
	pendingLedger          *LedgerRow
	ledgerInsertCalls      int
	ledgerReadCalls        int
	boundaryCalls          int
	commitCalls            int
	commitClaimed          bool
	commitErr              error
	commitStatusAfter      byte
	commitConnectionClosed bool
	commitClosedAfter      bool
	rollbackCalls          int
	steps                  []string
}

func newRunnerPreflightTransaction(session *runnerPreflightSession) *runnerPreflightTransaction {
	expected := minimalAuthorityProjection(AuthorityPhaseMigrationTransaction)
	return &runnerPreflightTransaction{
		session: session,
		status:  'I', commitStatusAfter: 'I',
		metadata: []any{
			"160000", expected.DatabaseName, expected.SessionUser, expected.CurrentUser,
			"serializable", "off", "off",
			projectionQueryTimeout.Milliseconds(), projectionLockTimeout.Milliseconds(), projectionIdleInTransactionTimeout.Milliseconds(),
		},
	}
}

func (transaction *runnerPreflightTransaction) projectionTxStatus() byte { return transaction.status }

func (transaction *runnerPreflightTransaction) enterRunnerProjectionProfile(context.Context) error {
	transaction.profileEnterCalls++
	transaction.steps = append(transaction.steps, "profile-enter")
	if transaction.profileEnterErr != nil {
		return transaction.profileEnterErr
	}
	if !transaction.active || transaction.status != 'T' || transaction.profile != "execution" {
		return errors.New("invalid projection profile transition")
	}
	transaction.profile = "projection"
	transaction.setTimeoutMetadata(uint64(projectionQueryTimeout.Milliseconds()), uint64(projectionLockTimeout.Milliseconds()), uint64(projectionIdleInTransactionTimeout.Milliseconds()))
	return nil
}

func (transaction *runnerPreflightTransaction) restoreRunnerExecutionProfile(_ context.Context, policy ExecutionPolicy) error {
	transaction.profileRestoreCalls++
	transaction.steps = append(transaction.steps, "profile-restore")
	if transaction.profileRestoreErr != nil {
		return transaction.profileRestoreErr
	}
	if !transaction.active || transaction.status != 'T' || transaction.profile != "projection" || !runnerCanonicalEqual(policy, transaction.session.executionPolicy) {
		return errors.New("invalid execution profile transition")
	}
	transaction.profile = "execution"
	transaction.setTimeoutMetadata(policy.StatementTimeoutMS, policy.LockTimeoutMS, policy.IdleInTransactionSessionTimeoutMS)
	return nil
}

func (*runnerPreflightTransaction) runnerTransactionProjectionProfileSealed() {}

func (transaction *runnerPreflightTransaction) setTimeoutMetadata(statementTimeoutMS, lockTimeoutMS, idleTimeoutMS uint64) {
	transaction.metadata[7] = int64(statementTimeoutMS)
	transaction.metadata[8] = int64(lockTimeoutMS)
	transaction.metadata[9] = int64(idleTimeoutMS)
}

func (transaction *runnerPreflightTransaction) Query(context.Context, string, ...any) (Rows, error) {
	transaction.queryCalls++
	return nil, errors.New("raw transaction query is forbidden in runner preflight")
}

func (transaction *runnerPreflightTransaction) QueryRow(ctx context.Context, _ string, _ ...any) Row {
	transaction.metadataReadCalls++
	transaction.steps = append(transaction.steps, "metadata")
	return runnerPreflightMetadataRow{ctx: ctx, values: transaction.metadata, err: transaction.metadataReadErr, afterScan: transaction.afterMetadataScan}
}

func (transaction *runnerPreflightTransaction) Exec(context.Context, string, ...any) (CommandTag, error) {
	transaction.execCalls++
	return nil, errors.New("transaction exec is forbidden in runner preflight")
}

func (transaction *runnerPreflightTransaction) ExecuteStatement(ctx context.Context, raw []byte) error {
	transaction.executeCalls++
	transaction.session.backend.executeCalls++
	transaction.steps = append(transaction.steps, "execute")
	owned := append([]byte(nil), raw...)
	transaction.executedSQL = append(transaction.executedSQL, owned)
	if !transaction.executeAllowed {
		return errors.New("migration statement execution is forbidden in runner preflight")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if transaction.executeMutate != nil {
		transaction.executeMutate(raw)
	}
	return transaction.executeErr
}

func (transaction *runnerPreflightTransaction) insertAndReadRunnerLedgerRow(ctx context.Context, entry MigrationEntry, digest Digest) ([]LedgerRow, error) {
	transaction.ledgerInsertCalls++
	transaction.session.backend.ledgerInsertCalls++
	transaction.steps = append(transaction.steps, "ledger-insert")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !transaction.active || transaction.status != 'T' || transaction.profile != "execution" {
		return nil, errors.New("invalid ledger transaction lifecycle")
	}
	if transaction.ledgerInsertErr != nil {
		return nil, transaction.ledgerInsertErr
	}
	row := ledgerRowFor(entry, digest)
	transaction.pendingLedger = &row
	transaction.ledgerReadCalls++
	transaction.steps = append(transaction.steps, "ledger-readback")
	if transaction.ledgerReadErr != nil {
		return nil, transaction.ledgerReadErr
	}
	rows := []LedgerRow{cloneProjectionValue(row)}
	if transaction.ledgerReadMutate != nil {
		rows = transaction.ledgerReadMutate(rows)
	}
	return cloneProjectionValue(rows), nil
}

func (*runnerPreflightTransaction) runnerTransactionLedgerSealed() {}

func (transaction *runnerPreflightTransaction) Boundary(context.Context, int64) (BoundaryState, error) {
	transaction.boundaryCalls++
	transaction.steps = append(transaction.steps, "boundary")
	if transaction.boundaryErr != nil {
		return BoundaryState{}, transaction.boundaryErr
	}
	boundary := BoundaryState{TxStatus: transaction.status, CurrentUser: MigrationOwnerRole, LockHeld: transaction.session.locked}
	if transaction.boundaryMutate != nil {
		transaction.boundaryMutate(&boundary)
	}
	return boundary, nil
}

func (transaction *runnerPreflightTransaction) Commit(context.Context) error {
	transaction.commitCalls++
	transaction.session.backend.commitCalls++
	transaction.active = false
	transaction.status = transaction.commitStatusAfter
	transaction.commitConnectionClosed = transaction.commitClosedAfter
	transaction.pendingLedger = nil
	return transaction.commitErr
}

func (transaction *runnerPreflightTransaction) claimRunnerCommitProtocol() bool {
	if transaction.commitClaimed || !transaction.active || transaction.status != 'T' || transaction.profile != "execution" {
		return false
	}
	transaction.commitClaimed = true
	return true
}

func (transaction *runnerPreflightTransaction) runnerCommitProtocolStatus() byte {
	return transaction.status
}

func (transaction *runnerPreflightTransaction) runnerCommitProtocolConnectionClosed() bool {
	return transaction.commitConnectionClosed
}

func (*runnerPreflightTransaction) runnerCommitProtocolSealed() {}

func (transaction *runnerPreflightTransaction) Rollback(context.Context) error {
	transaction.rollbackCalls++
	transaction.steps = append(transaction.steps, "rollback")
	if transaction.rollbackErr != nil {
		if !transaction.rollbackLeavesOpen {
			transaction.active = false
			transaction.status = 'I'
			transaction.pendingLedger = nil
		}
		return transaction.rollbackErr
	}
	if transaction.rollbackLeavesOpen {
		return nil
	}
	transaction.active = false
	transaction.status = 'I'
	transaction.pendingLedger = nil
	return nil
}

type runnerPreflightMetadataRow struct {
	ctx       context.Context
	values    []any
	err       error
	afterScan func()
}

func (row runnerPreflightMetadataRow) Scan(targets ...any) error {
	if row.err != nil {
		return row.err
	}
	if err := row.ctx.Err(); err != nil {
		return err
	}
	if len(targets) != len(row.values) {
		return errors.New("transaction metadata target count differs")
	}
	for index := range targets {
		target := reflect.ValueOf(targets[index])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return errors.New("transaction metadata target is not writable")
		}
		value := reflect.ValueOf(row.values[index])
		if !value.IsValid() || !value.Type().AssignableTo(target.Elem().Type()) {
			return errors.New("transaction metadata type differs")
		}
		target.Elem().Set(value)
	}
	if row.afterScan != nil {
		row.afterScan()
	}
	return nil
}

func (session *runnerPreflightSession) beginRunnerSessionProjectionSnapshot(_ context.Context, phase AuthorityPhase) (RunnerSessionProjectionSnapshot, error) {
	if err := session.snapshotOpenErr[phase]; err != nil {
		return nil, err
	}
	valid := !session.closed && !session.projectionActive
	switch phase {
	case AuthorityPhaseConnectedSession:
		valid = valid && !session.roleConfigured && !session.locked
	case AuthorityPhaseMigrationRole:
		valid = valid && session.roleConfigured && session.locked
	default:
		valid = false
	}
	if !valid {
		return nil, errors.New("invalid snapshot lifecycle")
	}
	session.projectionActive = true
	session.snapshotPhases = append(session.snapshotPhases, phase)
	metadata := runnerPreflightSnapshotMetadata(phase)
	if mutate := session.snapshotMetadataMutate[phase]; mutate != nil {
		mutate(&metadata)
	}
	if mutate := session.snapshotMetadataNth[len(session.snapshotPhases)]; mutate != nil {
		mutate(&metadata)
	}
	return &runnerPreflightSnapshot{session: session, phase: phase, metadata: metadata}, nil
}

type runnerPreflightSnapshot struct {
	session  *runnerPreflightSession
	phase    AuthorityPhase
	metadata SnapshotMetadata
	returned bool
	closeErr error
}

func (*runnerPreflightSnapshot) queryProjection(context.Context, projectionQueryID, ...any) (Rows, error) {
	return nil, errors.New("fake runner snapshot has no raw projection queries")
}

func (*runnerPreflightSnapshot) queryProjectionRow(context.Context, projectionQueryID, ...any) Row {
	return projectionErrorRow{err: errors.New("fake runner snapshot has no raw projection queries")}
}

func (*runnerPreflightSnapshot) projectionStats() projectionQueryStats { return projectionQueryStats{} }

func (snapshot *runnerPreflightSnapshot) Metadata() SnapshotMetadata { return snapshot.metadata }

func (*runnerPreflightSnapshot) projectionSnapshot() {}

func (*runnerPreflightSnapshot) runnerSessionProjectionSnapshot() {}

func (snapshot *runnerPreflightSnapshot) RollbackAndReturnToRunner(context.Context) error {
	if snapshot.returned {
		return snapshot.closeErr
	}
	snapshot.returned = true
	snapshot.session.snapshotClosePhases = append(snapshot.session.snapshotClosePhases, snapshot.phase)
	snapshot.session.projectionActive = false
	snapshot.closeErr = snapshot.session.snapshotCloseErr[snapshot.phase]
	return snapshot.closeErr
}

func runnerPreflightSnapshotMetadata(phase AuthorityPhase) SnapshotMetadata {
	expected := minimalAuthorityProjection(phase)
	return SnapshotMetadata{
		Mode: IdleReadSnapshot, Ownership: OwnedIdleSnapshot, PostgresMajor: 16, ServerVersionNum: 160000,
		DatabaseName: expected.DatabaseName, AuthorityPhase: phase, SessionUser: expected.SessionUser, CurrentUser: expected.CurrentUser,
		IsolationLevel: "repeatable_read", AccessMode: "read_only", TxStatus: "T",
	}
}

type runnerPreflightProjectorFactory struct {
	factoryErr         map[AuthorityPhase]error
	projectionErr      map[AuthorityPhase]error
	preconditionErr    error
	transitionErr      error
	catalogErr         error
	mutateResult       map[AuthorityPhase]func(*ProjectionResult[AuthorityProjection])
	mutatePrecondition func(*ProjectionResult[CatalogStateProjection])
	mutateTransition   func(*ProjectionResult[CatalogStateProjection])
	mutateCatalog      func(*ProjectionResult[CatalogProjection])
	transitionState    *CatalogStateProjection
	factoryPhases      []AuthorityPhase
	projectionPhases   []AuthorityPhase
	preconditionPhases []AuthorityPhase
	transitionPhases   []AuthorityPhase
	transitionScopes   []ProjectionScope
	catalogPhases      []AuthorityPhase
	catalogScopes      []ProjectionScope
	snapshotMetadata   []SnapshotMetadata
}

func (factory *runnerPreflightProjectorFactory) initialize() {
	if factory.factoryErr == nil {
		factory.factoryErr = map[AuthorityPhase]error{}
	}
	if factory.projectionErr == nil {
		factory.projectionErr = map[AuthorityPhase]error{}
	}
	if factory.mutateResult == nil {
		factory.mutateResult = map[AuthorityPhase]func(*ProjectionResult[AuthorityProjection]){}
	}
}

func (factory *runnerPreflightProjectorFactory) newRunnerAuthorityProjector(_ context.Context, snapshot ProjectionSnapshot) (runnerAuthorityProjector, error) {
	factory.initialize()
	phase := snapshot.Metadata().AuthorityPhase
	factory.factoryPhases = append(factory.factoryPhases, phase)
	factory.snapshotMetadata = append(factory.snapshotMetadata, cloneProjectionValue(snapshot.Metadata()))
	if err := factory.factoryErr[phase]; err != nil {
		return nil, err
	}
	return &runnerPreflightProjector{factory: factory, snapshot: snapshot}, nil
}

func (*runnerPreflightProjectorFactory) runnerAuthorityProjectorFactorySealed() {}

type runnerPreflightProjector struct {
	factory  *runnerPreflightProjectorFactory
	snapshot ProjectionSnapshot
}

func (projector *runnerPreflightProjector) ProjectAuthority(_ context.Context, snapshot ProjectionSnapshot, contract VerifiedAuthorityContract, phase AuthorityPhase) (ProjectionResult[AuthorityProjection], error) {
	projector.factory.projectionPhases = append(projector.factory.projectionPhases, phase)
	if snapshot != projector.snapshot || snapshot.Metadata().AuthorityPhase != phase {
		return ProjectionResult[AuthorityProjection]{}, errors.New("projection snapshot swap")
	}
	if err := projector.factory.projectionErr[phase]; err != nil {
		return ProjectionResult[AuthorityProjection]{}, err
	}
	result := runnerPreflightProjectionResult(nil, snapshot.Metadata(), contract, phase)
	if mutate := projector.factory.mutateResult[phase]; mutate != nil {
		mutate(&result)
	}
	return result, nil
}

func (projector *runnerPreflightProjector) ProjectPrecondition(_ context.Context, snapshot ProjectionSnapshot, scope VerifiedSchemaBundleScope, condition CatalogPrecondition) (ProjectionResult[CatalogStateProjection], error) {
	phase := snapshot.Metadata().AuthorityPhase
	projector.factory.preconditionPhases = append(projector.factory.preconditionPhases, phase)
	if snapshot != projector.snapshot || phase != AuthorityPhaseMigrationRole && phase != AuthorityPhaseMigrationTransaction {
		return ProjectionResult[CatalogStateProjection]{}, errors.New("precondition snapshot swap")
	}
	if projector.factory.preconditionErr != nil {
		return ProjectionResult[CatalogStateProjection]{}, projector.factory.preconditionErr
	}
	bound := scope.BoundPrecondition()
	if !runnerCanonicalEqual(bound, condition) || len(bound.AcceptedStates) == 0 {
		return ProjectionResult[CatalogStateProjection]{}, errors.New("precondition binding mismatch")
	}
	state := cloneProjectionValue(bound.AcceptedStates[0])
	for _, accepted := range bound.AcceptedStates {
		if accepted.Absent != nil {
			state = cloneProjectionValue(accepted)
			break
		}
	}
	digest, err := state.ComputeDigest()
	if err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	resultScope := acceptedScope(state)
	result := ProjectionResult[CatalogStateProjection]{
		Projection: state, Digest: digest,
		Metadata: ProjectionMetadata{
			ProjectionKind: ProjectionKindCatalogState, DigestDomain: CatalogStateDigestDomain, AdapterProfile: PostgreSQLCatalogAdapter,
			Snapshot: snapshot.Metadata(), VerifiedSubjectDigest: scope.SubjectDigest(), Scope: cloneScopePointer(&resultScope),
			LimitsProfile: ProjectionLimitsProfile, QueryCount: 1, RowCount: 1, TotalBytes: 1, RedactionProfile: ProjectionRedactionProfile,
		},
	}
	if projector.factory.mutatePrecondition != nil {
		projector.factory.mutatePrecondition(&result)
	}
	return result, nil
}

func (projector *runnerPreflightProjector) ProjectTransitionState(_ context.Context, snapshot ProjectionSnapshot, contract VerifiedCatalogContract, scope ProjectionScope) (ProjectionResult[CatalogStateProjection], error) {
	phase := snapshot.Metadata().AuthorityPhase
	projector.factory.transitionPhases = append(projector.factory.transitionPhases, phase)
	projector.factory.transitionScopes = append(projector.factory.transitionScopes, cloneProjectionValue(scope))
	if snapshot != projector.snapshot || phase != AuthorityPhaseMigrationTransaction || contract.validate() != nil || scope.Validate() != nil {
		return ProjectionResult[CatalogStateProjection]{}, errors.New("transition snapshot or binding mismatch")
	}
	if projector.factory.transitionErr != nil {
		return ProjectionResult[CatalogStateProjection]{}, projector.factory.transitionErr
	}
	if projector.factory.transitionState == nil {
		return ProjectionResult[CatalogStateProjection]{}, errors.New("transition state is unavailable")
	}
	state := cloneProjectionValue(*projector.factory.transitionState)
	digest, err := state.ComputeDigest()
	if err != nil {
		return ProjectionResult[CatalogStateProjection]{}, err
	}
	result := ProjectionResult[CatalogStateProjection]{
		Projection: state, Digest: digest,
		Metadata: ProjectionMetadata{
			ProjectionKind: ProjectionKindCatalogState, DigestDomain: CatalogStateDigestDomain, AdapterProfile: PostgreSQLCatalogAdapter,
			Snapshot: snapshot.Metadata(), VerifiedSubjectDigest: contract.SubjectDigest(), Scope: cloneScopePointer(&scope),
			LimitsProfile: ProjectionLimitsProfile, QueryCount: 1, RowCount: 1, TotalBytes: 1, RedactionProfile: ProjectionRedactionProfile,
		},
	}
	if projector.factory.mutateTransition != nil {
		projector.factory.mutateTransition(&result)
	}
	return result, nil
}

func (projector *runnerPreflightProjector) ProjectCatalog(_ context.Context, snapshot ProjectionSnapshot, contract VerifiedCatalogContract, scope ProjectionScope) (ProjectionResult[CatalogProjection], error) {
	phase := snapshot.Metadata().AuthorityPhase
	projector.factory.catalogPhases = append(projector.factory.catalogPhases, phase)
	projector.factory.catalogScopes = append(projector.factory.catalogScopes, cloneProjectionValue(scope))
	if snapshot != projector.snapshot || phase != AuthorityPhaseMigrationTransaction || contract.validate() != nil || scope.Validate() != nil || !equalProjectionScopes(scope, contract.Scope()) {
		return ProjectionResult[CatalogProjection]{}, errors.New("catalog snapshot or binding mismatch")
	}
	if projector.factory.catalogErr != nil {
		return ProjectionResult[CatalogProjection]{}, projector.factory.catalogErr
	}
	projection := contract.ExpectedProjection()
	digest, err := digestProjectionWrapper(CatalogProjectionDigestDomain, projection)
	if err != nil {
		return ProjectionResult[CatalogProjection]{}, err
	}
	result := ProjectionResult[CatalogProjection]{
		Projection: projection, Digest: digest,
		Metadata: ProjectionMetadata{
			ProjectionKind: ProjectionKindCatalog, DigestDomain: CatalogProjectionDigestDomain, AdapterProfile: PostgreSQLCatalogAdapter,
			Snapshot: snapshot.Metadata(), VerifiedSubjectDigest: contract.SubjectDigest(), Scope: cloneScopePointer(&scope),
			LimitsProfile: ProjectionLimitsProfile, QueryCount: 1, RowCount: 1, TotalBytes: 1, RedactionProfile: ProjectionRedactionProfile,
		},
	}
	if projector.factory.mutateCatalog != nil {
		projector.factory.mutateCatalog(&result)
	}
	return result, nil
}

func runnerPreflightProjectionResult(t *testing.T, snapshot SnapshotMetadata, contract VerifiedAuthorityContract, phase AuthorityPhase) ProjectionResult[AuthorityProjection] {
	expected, err := contract.ExpectedProjection(phase)
	if err != nil {
		if t != nil {
			t.Fatal(err)
		}
		return ProjectionResult[AuthorityProjection]{}
	}
	digest, err := digestProjectionWrapper(AuthorityProjectionDigestDomain, expected)
	if err != nil {
		if t != nil {
			t.Fatal(err)
		}
		return ProjectionResult[AuthorityProjection]{}
	}
	return ProjectionResult[AuthorityProjection]{
		Projection: expected,
		Digest:     digest,
		Metadata: ProjectionMetadata{
			ProjectionKind: ProjectionKindAuthority, DigestDomain: AuthorityProjectionDigestDomain, AdapterProfile: PostgreSQLAuthorityAdapter,
			Snapshot: snapshot, VerifiedSubjectDigest: contract.SubjectDigest(), LimitsProfile: ProjectionLimitsProfile,
			QueryCount: runnerAuthorityProjectionQueryCount, RowCount: 1, TotalBytes: 1, RedactionProfile: ProjectionRedactionProfile,
		},
	}
}

var _ runnerSessionProjectionSnapshotProvider = (*runnerPreflightSession)(nil)
var _ runnerLedgerPrefixReader = (*runnerPreflightSession)(nil)
var _ RunnerSessionProjectionSnapshot = (*runnerPreflightSnapshot)(nil)
