package migration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// runLegacyCharacterizationForTest is the only call edge into the retired
// ADR-0009 execution state machine. Production Runner.Run cannot reach it.
func runLegacyCharacterizationForTest(runner *Runner, ctx context.Context, request RunRequest) (RunResult, error) {
	return runner.runLegacyCharacterization(ctx, request)
}

func TestPublicRunnerRejectsCheckedInMutableContractsWithZeroSideEffects(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	backend := &fakeBackend{}
	connector := &fakeConnector{backend: backend}
	verifier := &sequenceTrustVerifier{fallback: testTrustDecision(raw, manifest)}
	source := &memoryArtifactSource{data: raw}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust: verifier, Connector: connector, Observer: observer,
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
	}
	_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	if !IsCode(err, CodeUntrusted) {
		t.Fatalf("public runner accepted a release decision without exact projection bindings: %v", err)
	}
	assertPublicAdmissionCounts(t, verifier, source, observer)
	assertNoRunnerSideEffects(t, connector, backend)
}

func TestPublicRunnerExactAdmissionStopsAtUnwiredJournalWithZeroSideEffects(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	backend := &fakeBackend{}
	connector := &fakeConnector{backend: backend}
	verifier := &sequenceTrustVerifier{fallback: decision}
	source := &memoryArtifactSource{data: raw}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust: verifier, Connector: connector, Observer: observer,
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
	}
	_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != CodeProjectionNotImplemented || migrationErr.Op != "runner-evidence-journal" {
		t.Fatalf("exact admission did not stop at the unwired journal boundary: %v", err)
	}
	assertPublicAdmissionCounts(t, verifier, source, observer)
	assertNoRunnerSideEffects(t, connector, backend)
}

type recordingStateObserver struct{ transitions []RunnerState }

func (observer *recordingStateObserver) Transition(state RunnerState) {
	observer.transitions = append(observer.transitions, state)
}

func assertPublicAdmissionCounts(t *testing.T, verifier *sequenceTrustVerifier, source *memoryArtifactSource, observer *recordingStateObserver) {
	t.Helper()
	wantTransitions := []RunnerState{StateVerifyTrust, StateLoadBundle}
	if verifier.calls != 1 || source.reads != 1 || !reflect.DeepEqual(observer.transitions, wantTransitions) {
		t.Fatalf("public admission ordering/reverify mismatch: verify=%d read=%d transitions=%v want=%v", verifier.calls, source.reads, observer.transitions, wantTransitions)
	}
}

func assertNoRunnerSideEffects(t *testing.T, connector *fakeConnector, backend *fakeBackend) {
	t.Helper()
	if connector.attempts != 0 || connector.connections != 0 || backend.queryCalls != 0 || backend.beginCalls != 0 ||
		backend.executeCalls != 0 || backend.ledgerReadCalls != 0 || backend.ledgerInsertCalls != 0 || backend.commitCalls != 0 {
		t.Fatalf("public runner crossed the pre-connect gate: connect=%d/%d query=%d begin=%d execute=%d ledger=%d/%d commit=%d",
			connector.attempts, connector.connections, backend.queryCalls, backend.beginCalls, backend.executeCalls,
			backend.ledgerReadCalls, backend.ledgerInsertCalls, backend.commitCalls)
	}
}

func buildExactAdmissionRuntime(t *testing.T) ([]byte, VerifiedTrustDecision) {
	t.Helper()
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	direct, catalog := exactPlanBundle(t, fixture.decision.expectedSchemaBundleDigest, fixture.initialScope.BoundPrecondition(), fixture.authorityProfile)
	entry := direct.Manifest.SchemaBundle.Migrations[0]
	entry.Name = "test exact admission"
	entry.Phase = "expand"
	entry.SchemaFrom = "absent"
	entry.SchemaTo = "000001"
	entry.CompatibleControlPlaneMin = "0.1.0-alpha.1"
	entry.CompatibleControlPlaneMax = "0.2.0-0"
	entry.CompatibleWorkerMin = "0.1.0-alpha.1"
	entry.CompatibleWorkerMax = "0.2.0-0"
	entry.TransactionMode = "transactional"
	entry.Reentrancy = "ledger_guarded"
	entry.RollbackBoundary = "point_in_time_restore"

	checkedRaw := mustRead(t, filepath.Join(migrationRoot(t), "manifest.json"))
	checkedManifest, _, err := DecodeManifest(checkedRaw)
	if err != nil {
		t.Fatal(err)
	}
	globalRecord := checkedManifest.SchemaBundle.GlobalTableAuthority
	globalRaw := mustRead(t, modulePathForRuntimeArtifact(t, globalRecord.Path))
	schemaBundle := SchemaBundle{
		Lineage: "cloud-agents-platform", SchemaHead: "000001", AdvisoryLock: checkedManifest.SchemaBundle.AdvisoryLock,
		GlobalTableAuthority: globalRecord, ProjectionScopeAuthority: ProjectionScopeAuthority{
			DefaultACLOwners: []string{MigrationOwnerRole}, ObjectCreatorClosure: []string{MigrationOwnerRole},
		},
		Migrations: []MigrationEntry{entry},
	}
	schemaDigest := schemaBundleDigestForTest(t, schemaBundle)
	schemaDocumentRaw := mustJSON(t, SchemaBundleDocument{FormatVersion: SchemaBundleFormatVersion, SchemaBundle: schemaBundle, SchemaBundleDigest: schemaDigest})
	schemaRecord := ArtifactRecord{Path: RuntimeSchemaBundlePath, Mode: "100644", SizeBytes: uint64(len(schemaDocumentRaw)), SHA256: DigestBytes(schemaDocumentRaw)}

	authorityRecord := direct.Manifest.ExecutionPolicy.AuthorityContract
	authorityRaw := append([]byte(nil), direct.Files[authorityRecord.Path]...)
	catalogRaw := mustJSON(t, catalog)
	catalogRecord := entry.CatalogContract
	if catalogRecord.SizeBytes != uint64(len(catalogRaw)) || catalogRecord.SHA256 != DigestBytes(catalogRaw) {
		t.Fatal("exact catalog helper descriptor drifted")
	}
	sqlRaw := append([]byte(nil), direct.Files[entry.SQLArtifact.Path]...)
	runtimeRecords := []ArtifactRecord{entry.SQLArtifact, authorityRecord, globalRecord, catalogRecord, schemaRecord}
	sort.Slice(runtimeRecords, func(i, j int) bool { return runtimeRecords[i].Path < runtimeRecords[j].Path })
	policy := checkedManifest.ExecutionPolicy
	policy.AuthorityContract = authorityRecord
	manifest := &Manifest{
		FormatVersion: ManifestFormatVersion, SchemaBundle: schemaBundle, SchemaBundleDigest: schemaDigest,
		BootstrapBundle: checkedManifest.BootstrapBundle, BootstrapBundleDigest: checkedManifest.BootstrapBundleDigest,
		ExecutionPolicy: policy, RuntimeArtifacts: runtimeRecords,
	}
	manifestRaw := encodeTestManifest(t, manifest)
	files := map[string][]byte{
		entry.SQLArtifact.Path: sqlRaw, authorityRecord.Path: authorityRaw, globalRecord.Path: globalRaw,
		catalogRecord.Path: catalogRaw, RuntimeSchemaBundlePath: schemaDocumentRaw, RuntimeManifestPath: manifestRaw,
	}
	members := make([]tarMember, 0, len(files))
	for path, data := range files {
		members = append(members, tarMember{Path: path, Data: data})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Path < members[j].Path })
	raw := writeTestUSTAR(t, members)

	fixture.decision.expectedSchemaBundleDigest = schemaDigest
	fixture.decision.expectedBootstrapBundleDigest = manifest.BootstrapBundleDigest
	fixture.decision.expectedManifestDigest = manifest.ManifestDigest
	fixture.decision.expectedOuterArtifactDigest = DigestBytes(raw)
	fixture.initialScope, err = bindVerifiedSchemaBundleScope(
		schemaDigest, fixture.initialScope.Scope(), fixture.initialScope.BoundPrecondition(),
		fixture.initialScope.DefaultACLOwners(), fixture.initialScope.ObjectCreatorClosure(), fixture.decision.expiresAt, fixture.decision.securityEpoch,
	)
	if err != nil {
		t.Fatal(err)
	}
	catalogSubject, err := bindVerifiedExecutableCatalogSubject(catalogRaw, catalogRecord.SHA256, fixture.expiresAt.Add(time.Hour), 1, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := bindVerifiedRunnerProjectionDecision(
		fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority,
		fixture.recoveryPolicy, fixture.initialScope, []verifiedExecutableCatalogSubject{catalogSubject}, fixture.now,
	)
	if err != nil {
		t.Fatal(err)
	}
	return raw, decision
}

func schemaBundleDigestForTest(t *testing.T, bundle SchemaBundle) Digest {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"schema_bundle": bundle})
	if err != nil {
		t.Fatal(err)
	}
	value, err := ParseStrictJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := digestDomainObject(SchemaBundleDomain, "schema_bundle", value.(map[string]JSONValue)["schema_bundle"])
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestRunnerRecoversAmbiguousCommitByExactLedgerReplay(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	backend := &fakeBackend{ambiguousCommits: 1, ambiguousApplies: true}
	connector := &fakeConnector{backend: backend}
	source := &memoryArtifactSource{data: raw}
	runner := Runner{
		Trust:        testTrustVerifier{decision: testTrustDecision(raw, manifest)},
		Connector:    connector,
		Ledger:       &fakeLedgerStore{},
		Authority:    acceptingAuthority{},
		Catalog:      acceptingCatalog{},
		Intermediate: acceptingIntermediate{},
	}
	result, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalHead != "000002" || len(result.AmbiguousRecovered) != 1 || result.AmbiguousRecovered[0] != "000001" || len(backend.rows) != 2 || connector.connections < 2 {
		t.Fatalf("unexpected recovery: result=%+v rows=%d connections=%d", result, len(backend.rows), connector.connections)
	}
}

func TestRunnerRetriesOnlyExactPendingStateAfterAmbiguousCommit(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	backend := &fakeBackend{ambiguousCommits: 1, ambiguousApplies: false}
	runner := Runner{
		Trust: testTrustVerifier{decision: testTrustDecision(raw, manifest)}, Connector: &fakeConnector{backend: backend},
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
	}
	result, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalHead != "000002" || len(result.Applied) != 2 || len(result.AmbiguousRecovered) != 0 || len(backend.rows) != 2 {
		t.Fatalf("pending retry did not converge exactly: result=%+v rows=%d", result, len(backend.rows))
	}
}

func TestRunnerRejectsDivergentLedgerAfterAmbiguousCommit(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	backend := &fakeBackend{ambiguousCommits: 1, ambiguousApplies: true, mutateAmbiguousRow: true}
	runner := Runner{
		Trust: testTrustVerifier{decision: testTrustDecision(raw, manifest)}, Connector: &fakeConnector{backend: backend},
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
	}
	_, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	if !IsCode(err, CodeInvalidLedger) {
		t.Fatalf("divergent ambiguous ledger was not rejected: %v", err)
	}
}

func TestAmbiguousReconnectReverifiesExactTrustBeforeConnect(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	initial := testTrustDecision(raw, manifest)
	changed := initial
	changed.securityEpoch++
	verifier := &sequenceTrustVerifier{decisions: []VerifiedTrustDecision{initial, changed}}
	connector := &fakeConnector{backend: &fakeBackend{ambiguousCommits: 1, ambiguousApplies: true}}
	source := &memoryArtifactSource{data: raw}
	runner := Runner{Trust: verifier, Connector: connector, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
	_, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	if !IsCode(err, CodeUntrusted) || verifier.calls != 2 || connector.attempts != 1 || source.reads != 1 {
		t.Fatalf("reverify ordering/exact comparison failed: err=%v trust=%d connect=%d reads=%d", err, verifier.calls, connector.attempts, source.reads)
	}
}

func TestConnectRetryIsBoundedAndReverifiesWithoutRereadingArtifact(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	decision := testTrustDecision(raw, manifest)
	verifier := &sequenceTrustVerifier{decisions: []VerifiedTrustDecision{decision, decision}}
	connector := &fakeConnector{backend: &fakeBackend{}, connectErrors: []error{io.EOF}}
	source := &memoryArtifactSource{data: raw}
	runner := Runner{Trust: verifier, Connector: connector, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
	result, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalHead != "000002" || connector.attempts != 2 || verifier.calls != 2 || source.reads != 1 {
		t.Fatalf("connect retry boundary mismatch: result=%+v attempts=%d trust=%d reads=%d", result, connector.attempts, verifier.calls, source.reads)
	}
}

func TestConnectRetryExhaustionAndTerminalError(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		errors       []error
		wantAttempts int
		wantTrust    int
	}{
		{name: "bounded-connection-loss", errors: []error{io.EOF, io.EOF, io.EOF}, wantAttempts: 3, wantTrust: 3},
		{name: "terminal", errors: []error{errors.New("configuration rejected")}, wantAttempts: 1, wantTrust: 1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			raw, manifest := buildCheckedInRuntimeTar(t)
			decision := testTrustDecision(raw, manifest)
			verifier := &sequenceTrustVerifier{fallback: decision}
			connector := &fakeConnector{backend: &fakeBackend{}, connectErrors: append([]error(nil), test.errors...)}
			source := &memoryArtifactSource{data: raw}
			runner := Runner{Trust: verifier, Connector: connector, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
			_, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
			if err == nil || connector.attempts != test.wantAttempts || verifier.calls != test.wantTrust || source.reads != 1 {
				t.Fatalf("connect retry was not bounded: err=%v attempts=%d trust=%d reads=%d", err, connector.attempts, verifier.calls, source.reads)
			}
		})
	}
}

func TestTransactionRetryExhaustionIsBounded(t *testing.T) {
	t.Parallel()
	raw, manifest := buildCheckedInRuntimeTar(t)
	decision := testTrustDecision(raw, manifest)
	backend := &fakeBackend{executeErrors: []error{
		&pgconn.PgError{Code: "40001"}, &pgconn.PgError{Code: "40001"}, &pgconn.PgError{Code: "40001"},
	}}
	connector := &fakeConnector{backend: backend}
	runner := Runner{Trust: &sequenceTrustVerifier{fallback: decision}, Connector: connector, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
	_, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
	if err == nil || len(backend.rows) != 0 || connector.attempts != 1 || len(backend.executeErrors) != 0 {
		t.Fatalf("transaction retry was not bounded to three: err=%v rows=%d attempts=%d remaining=%d", err, len(backend.rows), connector.attempts, len(backend.executeErrors))
	}
}

func TestTransactionRetryClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		err           error
		wantSuccess   bool
		wantReconnect bool
	}{
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, wantSuccess: true},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, wantSuccess: true},
		{name: "connection-loss", err: io.EOF, wantSuccess: true, wantReconnect: true},
		{name: "permission", err: &pgconn.PgError{Code: "42501"}},
		{name: "unknown", err: errors.New("unknown failure")},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			raw, manifest := buildCheckedInRuntimeTar(t)
			decision := testTrustDecision(raw, manifest)
			verifier := &sequenceTrustVerifier{fallback: decision}
			backend := &fakeBackend{executeErrors: []error{test.err}}
			connector := &fakeConnector{backend: backend}
			runner := Runner{Trust: verifier, Connector: connector, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
			result, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
			if test.wantSuccess {
				if err != nil || result.FinalHead != "000002" || len(backend.rows) != 2 {
					t.Fatalf("classified retry failed: result=%+v rows=%d err=%v", result, len(backend.rows), err)
				}
			} else if err == nil || len(backend.rows) != 0 || connector.attempts != 1 {
				t.Fatalf("non-retryable error was retried: attempts=%d rows=%d err=%v", connector.attempts, len(backend.rows), err)
			}
			if test.wantReconnect && (connector.attempts < 2 || verifier.calls < 2) {
				t.Fatalf("connection loss did not reverify/reconnect: attempts=%d trust=%d", connector.attempts, verifier.calls)
			}
		})
	}
}

func TestCommitErrorClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		err           error
		wantSuccess   bool
		wantReconnect bool
	}{
		{name: "serialization-abort", err: &pgconn.PgError{Code: "40001"}, wantSuccess: true},
		{name: "deadlock-abort", err: &pgconn.PgError{Code: "40P01"}, wantSuccess: true},
		{name: "permission-terminal", err: &pgconn.PgError{Code: "42501"}},
		{name: "constraint-terminal", err: &pgconn.PgError{Code: "23505"}},
		{name: "unknown-terminal", err: errors.New("commit rejected")},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			raw, manifest := buildCheckedInRuntimeTar(t)
			decision := testTrustDecision(raw, manifest)
			backend := &fakeBackend{commitErrors: []error{test.err}}
			connector := &fakeConnector{backend: backend}
			verifier := &sequenceTrustVerifier{fallback: decision}
			runner := Runner{Trust: verifier, Connector: connector, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
			result, err := runLegacyCharacterizationForTest(&runner, context.Background(), RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: "test-only"})
			if test.wantSuccess {
				if err != nil || result.FinalHead != "000002" || len(backend.rows) != 2 || connector.attempts != 1 || verifier.calls != 1 {
					t.Fatalf("confirmed commit abort was not locally retried: result=%+v rows=%d connect=%d trust=%d err=%v", result, len(backend.rows), connector.attempts, verifier.calls, err)
				}
			} else if err == nil || len(backend.rows) != 0 || connector.attempts != 1 || verifier.calls != 1 {
				t.Fatalf("terminal commit error was retried/reconciled: rows=%d connect=%d trust=%d err=%v", len(backend.rows), connector.attempts, verifier.calls, err)
			}
		})
	}
}

type fakeBackend struct {
	rows               []LedgerRow
	ambiguousCommits   int
	ambiguousApplies   bool
	mutateAmbiguousRow bool
	executeErrors      []error
	commitErrors       []error
	queryCalls         int
	beginCalls         int
	executeCalls       int
	ledgerReadCalls    int
	ledgerInsertCalls  int
	commitCalls        int
}

type backendCarrier interface{ migrationBackend() *fakeBackend }

type fakeConnector struct {
	backend       *fakeBackend
	connections   int
	attempts      int
	connectErrors []error
}

func (connector *fakeConnector) Connect(context.Context, string) (DatabaseSession, error) {
	connector.attempts++
	if len(connector.connectErrors) > 0 {
		err := connector.connectErrors[0]
		connector.connectErrors = connector.connectErrors[1:]
		return nil, err
	}
	if connector.backend == nil {
		connector.backend = &fakeBackend{}
	}
	connector.connections++
	return &fakeSession{backend: connector.backend}, nil
}

type fakeSession struct {
	backend *fakeBackend
	locked  bool
	closed  bool
}

func (session *fakeSession) migrationBackend() *fakeBackend                            { return session.backend }
func (session *fakeSession) Queryer() Queryer                                          { return fakeQueryer{backend: session.backend} }
func (session *fakeSession) ServerMajor(context.Context) (int, error)                  { return 16, nil }
func (session *fakeSession) SetRoleAndSettings(context.Context, ExecutionPolicy) error { return nil }
func (session *fakeSession) AcquireAdvisoryLock(context.Context, int64) error {
	session.locked = true
	return nil
}
func (session *fakeSession) Boundary(context.Context, int64) (BoundaryState, error) {
	return BoundaryState{TxStatus: 'I', CurrentUser: MigrationOwnerRole, LockHeld: session.locked}, nil
}
func (session *fakeSession) BeginMigration(context.Context) (MigrationTransaction, error) {
	session.backend.beginCalls++
	return &fakeTx{backend: session.backend, active: true}, nil
}
func (session *fakeSession) UnlockAndReset(context.Context, int64) error {
	session.locked = false
	return nil
}
func (session *fakeSession) Close(context.Context) error {
	session.closed = true
	session.locked = false
	return nil
}

type fakeQueryer struct{ backend *fakeBackend }

func (queryer fakeQueryer) migrationBackend() *fakeBackend { return queryer.backend }
func (queryer fakeQueryer) Query(context.Context, string, ...any) (Rows, error) {
	queryer.backend.queryCalls++
	return fakeRows{}, nil
}
func (queryer fakeQueryer) QueryRow(context.Context, string, ...any) Row {
	queryer.backend.queryCalls++
	return fakeRow{}
}

type fakeRows struct{}

func (fakeRows) Next() bool        { return false }
func (fakeRows) Scan(...any) error { return errors.New("no row") }
func (fakeRows) Err() error        { return nil }
func (fakeRows) Close()            {}

type fakeRow struct{}

func (fakeRow) Scan(...any) error { return errors.New("no row") }

type fakeTx struct {
	backend *fakeBackend
	pending *LedgerRow
	active  bool
}

func (transaction *fakeTx) migrationBackend() *fakeBackend { return transaction.backend }
func (transaction *fakeTx) Query(context.Context, string, ...any) (Rows, error) {
	return fakeRows{}, nil
}
func (transaction *fakeTx) QueryRow(context.Context, string, ...any) Row { return fakeRow{} }
func (transaction *fakeTx) Exec(context.Context, string, ...any) (CommandTag, error) {
	return fakeTag(1), nil
}
func (transaction *fakeTx) ExecuteStatement(context.Context, []byte) error {
	transaction.backend.executeCalls++
	if len(transaction.backend.executeErrors) == 0 {
		return nil
	}
	err := transaction.backend.executeErrors[0]
	transaction.backend.executeErrors = transaction.backend.executeErrors[1:]
	return err
}
func (transaction *fakeTx) Boundary(context.Context, int64) (BoundaryState, error) {
	return BoundaryState{TxStatus: 'T', CurrentUser: MigrationOwnerRole, LockHeld: true}, nil
}
func (transaction *fakeTx) Commit(context.Context) error {
	transaction.backend.commitCalls++
	if !transaction.active {
		return errors.New("transaction closed")
	}
	transaction.active = false
	if len(transaction.backend.commitErrors) > 0 {
		err := transaction.backend.commitErrors[0]
		transaction.backend.commitErrors = transaction.backend.commitErrors[1:]
		return err
	}
	if transaction.backend.ambiguousCommits > 0 {
		transaction.backend.ambiguousCommits--
		if transaction.backend.ambiguousApplies && transaction.pending != nil {
			row := *transaction.pending
			if transaction.backend.mutateAmbiguousRow {
				row.MigrationName += "-drift"
			}
			transaction.backend.rows = append(transaction.backend.rows, row)
		}
		return io.EOF
	}
	if transaction.pending != nil {
		transaction.backend.rows = append(transaction.backend.rows, *transaction.pending)
	}
	return nil
}
func (transaction *fakeTx) Rollback(context.Context) error {
	transaction.active = false
	transaction.pending = nil
	return nil
}

type fakeTag int64

func (tag fakeTag) RowsAffected() int64 { return int64(tag) }

type fakeLedgerStore struct{}

func (*fakeLedgerStore) Read(_ context.Context, queryer Queryer) ([]LedgerRow, error) {
	carrier, ok := queryer.(backendCarrier)
	if !ok {
		return nil, errors.New("missing fake backend")
	}
	carrier.migrationBackend().ledgerReadCalls++
	return append([]LedgerRow(nil), carrier.migrationBackend().rows...), nil
}
func (*fakeLedgerStore) Insert(_ context.Context, executor CommandExecutor, entry MigrationEntry, digest Digest) error {
	tx, ok := executor.(*fakeTx)
	if !ok {
		return errors.New("not fake transaction")
	}
	tx.backend.ledgerInsertCalls++
	row := ledgerRowFor(entry, digest)
	tx.pending = &row
	return nil
}

type acceptingAuthority struct{}

func (acceptingAuthority) ValidateAuthority(context.Context, Queryer, int, []byte) (AuthorityProjection, error) {
	return AuthorityProjection{}, nil
}

type acceptingCatalog struct{}

func (acceptingCatalog) ValidateCatalog(_ context.Context, _ Queryer, _ int, _ []byte, head string) (CatalogProjection, error) {
	return CatalogProjection{SchemaHead: head}, nil
}
func (acceptingCatalog) ValidatePredecessor(context.Context, Queryer, int, CatalogPrecondition, map[string][]byte) (CatalogProjection, error) {
	return CatalogProjection{SchemaHead: "absent"}, nil
}

type acceptingIntermediate struct{}

func (acceptingIntermediate) ValidateIntermediate(context.Context, Queryer, int, MigrationEntry, SQLStatement, StatementPlan, CatalogProjection) error {
	return nil
}

type sequenceTrustVerifier struct {
	decisions []VerifiedTrustDecision
	fallback  VerifiedTrustDecision
	calls     int
}

func (verifier *sequenceTrustVerifier) Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error) {
	verifier.calls++
	if len(verifier.decisions) == 0 {
		return verifier.fallback, nil
	}
	decision := verifier.decisions[0]
	verifier.decisions = verifier.decisions[1:]
	return decision, nil
}
