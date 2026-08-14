package migration

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
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
			if sink.session == nil || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 || session.closeCalls != test.wantClose || session.beginCalls != 0 || session.queryCalls != 0 {
				t.Fatalf("fault cleanup crossed a forbidden boundary: evidence=%+v session=%+v", sink.session, session)
			}
			if session.locked && session.closeErr == nil {
				t.Fatal("successful database close left the fake session lock held")
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

func TestRunnerProjectionFactoryIsNotPubliclyInjectable(t *testing.T) {
	field, ok := reflect.TypeOf(Runner{}).FieldByName("projectionFactory")
	if !ok || field.PkgPath == "" {
		t.Fatal("runner projection factory became a public caller-supplied seam")
	}
	var _ runnerAuthorityProjectorFactory = (*runnerPreflightProjectorFactory)(nil)
}

func TestRunnerDatabasePreflightHasNoMigrationOrLedgerCallEdge(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runner_database_preflight.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true,
		"Queryer": true, "ServerMajor": true, "Insert": true, "Exec": true, "Query": true, "QueryRow": true,
		"Ledger": true, "Catalog": true, "Intermediate": true,
		"ProjectCatalog": true, "ProjectPrecondition": true, "ProjectTransitionState": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && forbidden[selector.Sel.Name] {
			t.Fatalf("database authority preflight acquired forbidden %s call edge", selector.Sel.Name)
		}
		return true
	})
}

func assertRunnerAuthorityPreflightLifecycle(t *testing.T, connector *runnerPreflightConnector, factory *runnerPreflightProjectorFactory) {
	t.Helper()
	session := connector.session
	wantPhases := []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole}
	if connector.attempts != 1 || session == nil || session.setRoleCalls != 1 || session.lockCalls != 1 || session.unlockCalls != 1 || session.closeCalls != 1 || session.serverMajorCalls != 0 || session.boundaryCalls != 0 || session.beginCalls != 0 || session.queryCalls != 0 || !session.closed || session.locked || session.roleConfigured || !reflect.DeepEqual(session.snapshotPhases, wantPhases) || !reflect.DeepEqual(session.snapshotClosePhases, wantPhases) || !reflect.DeepEqual(factory.factoryPhases, wantPhases) || !reflect.DeepEqual(factory.projectionPhases, wantPhases) {
		t.Fatalf("runner authority preflight lifecycle mismatch: connector=%+v session=%+v factory=%+v", connector, session, factory)
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
	locked                 bool
	projectionActive       bool
	closed                 bool
	settingsErr            error
	lockErr                error
	unlockErr              error
	closeErr               error
	snapshotOpenErr        map[AuthorityPhase]error
	snapshotCloseErr       map[AuthorityPhase]error
	snapshotMetadataMutate map[AuthorityPhase]func(*SnapshotMetadata)
	snapshotPhases         []AuthorityPhase
	snapshotClosePhases    []AuthorityPhase
	setRoleCalls           int
	lockCalls              int
	unlockCalls            int
	closeCalls             int
	serverMajorCalls       int
	boundaryCalls          int
	beginCalls             int
	queryCalls             int
}

func newRunnerPreflightSession() *runnerPreflightSession {
	return &runnerPreflightSession{
		backend: &fakeBackend{}, snapshotOpenErr: map[AuthorityPhase]error{}, snapshotCloseErr: map[AuthorityPhase]error{},
		snapshotMetadataMutate: map[AuthorityPhase]func(*SnapshotMetadata){},
	}
}

func (session *runnerPreflightSession) Queryer() Queryer {
	session.queryCalls++
	return fakeQueryer{backend: session.backend}
}

func (session *runnerPreflightSession) ServerMajor(context.Context) (int, error) {
	session.serverMajorCalls++
	return 16, nil
}

func (session *runnerPreflightSession) SetRoleAndSettings(context.Context, ExecutionPolicy) error {
	session.setRoleCalls++
	if session.closed || session.projectionActive || session.roleConfigured || session.locked {
		return errors.New("invalid role lifecycle")
	}
	if session.settingsErr != nil {
		return session.settingsErr
	}
	session.roleConfigured = true
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

func (session *runnerPreflightSession) BeginMigration(context.Context) (MigrationTransaction, error) {
	session.beginCalls++
	return nil, errors.New("migration transaction is forbidden in authority preflight")
}

func (session *runnerPreflightSession) UnlockAndReset(context.Context, int64) error {
	session.unlockCalls++
	if session.unlockErr != nil {
		return session.unlockErr
	}
	session.locked = false
	session.roleConfigured = false
	return nil
}

func (session *runnerPreflightSession) Close(context.Context) error {
	session.closeCalls++
	session.closed = true
	session.locked = false
	if session.closeErr != nil {
		return session.closeErr
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
	factoryErr       map[AuthorityPhase]error
	projectionErr    map[AuthorityPhase]error
	mutateResult     map[AuthorityPhase]func(*ProjectionResult[AuthorityProjection])
	factoryPhases    []AuthorityPhase
	projectionPhases []AuthorityPhase
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
var _ RunnerSessionProjectionSnapshot = (*runnerPreflightSnapshot)(nil)
