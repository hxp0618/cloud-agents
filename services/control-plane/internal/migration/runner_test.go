package migration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync/atomic"
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

func TestPublicRunnerExactAdmissionStopsAtUnconfiguredEvidenceSinkWithZeroSideEffects(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	backend := &fakeBackend{}
	connector := &fakeConnector{backend: backend}
	verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
	source := &memoryArtifactSource{data: raw}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust: verifier, Connector: connector, Observer: observer,
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
	}
	before := liveVerifiedEvidenceRunBindings()
	_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != CodeProjectionNotImplemented || migrationErr.Op != "runner-evidence-sink" {
		t.Fatalf("exact admission did not stop at the unconfigured sink boundary: %v", err)
	}
	assertPublicAdmissionCounts(t, verifier, source, observer)
	if liveVerifiedEvidenceRunBindings() != before {
		t.Fatalf("public runner left a discarded evidence candidate live: got=%d want=%d", liveVerifiedEvidenceRunBindings(), before)
	}
	assertNoRunnerSideEffects(t, connector, backend)
}

func TestPublicRunnerProjectsAllPreexecutionAuthorityPhasesAndDurablyAppendsStatementIntent(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	connector := &runnerPreflightConnector{session: newRunnerPreflightSession()}
	factory := &runnerPreflightProjectorFactory{}
	verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
	sink := &runnerEvidenceSinkFake{}
	source := &memoryArtifactSource{data: raw}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust: verifier, Evidence: sink, Connector: connector, Observer: observer,
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
		projectionFactory: factory,
	}
	before := liveVerifiedEvidenceRunBindings()
	_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != CodeProjectionNotImplemented || migrationErr.Op != "runner-statement-execute" || migrationErr.Err != nil {
		t.Fatalf("authority-preflight runner did not stop after the durable statement-intent boundary: %v", err)
	}
	if sink.calls != 1 || sink.session == nil || sink.session.bindCalls != 1 || sink.session.journal.replayCalls != 1 || sink.session.journal.appendCalls != 1 || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 || sink.session.snapshot.cursor.Valid() || liveVerifiedEvidenceRunBindings() != before {
		t.Fatalf("evidence session lifecycle mismatch: sink=%d session=%+v live=%d/%d", sink.calls, sink.session, liveVerifiedEvidenceRunBindings(), before)
	}
	wantTransitions := []RunnerState{StateVerifyTrust, StateLoadBundle, StateConnect, StateLocked}
	if verifier.calls != 1 || source.reads != 1 || !reflect.DeepEqual(observer.transitions, wantTransitions) {
		t.Fatalf("runner preflight ordering mismatch: verify=%d read=%d transitions=%v", verifier.calls, source.reads, observer.transitions)
	}
	assertRunnerAuthorityPreflightLifecycle(t, connector, factory)
}

func TestPublicRunnerEvidenceOpenClosedResultAndCleanupFaults(t *testing.T) {
	for _, test := range []struct {
		name       string
		configure  func(*runnerEvidenceSinkFake)
		want       ErrorCode
		wantClosed bool
	}{
		{"open-recovery-required", func(s *runnerEvidenceSinkFake) {
			s.openErr = fail(CodeEvidenceRecoveryRequired, "fake", "secret", errors.New("secret"))
		}, CodeEvidenceRecoveryRequired, false},
		{"open-canceled", func(s *runnerEvidenceSinkFake) { s.openErr = context.Canceled }, CodeContextCanceled, false},
		{"open-deadline", func(s *runnerEvidenceSinkFake) { s.openErr = context.DeadlineExceeded }, CodeDeadlineExceeded, false},
		{"error-with-values", func(s *runnerEvidenceSinkFake) { s.openErr = errors.New("secret"); s.valuesWithError = true }, CodeEvidenceJournalFailed, true},
		{"missing-session", func(s *runnerEvidenceSinkFake) { s.missingSession = true }, CodeEvidenceJournalFailed, true},
		{"missing-snapshot", func(s *runnerEvidenceSinkFake) { s.missingSnapshot = true }, CodeEvidenceJournalFailed, true},
		{"candidate-swap", func(s *runnerEvidenceSinkFake) { s.swapCandidate = true }, CodeEvidenceJournalFailed, true},
		{"replay-corrupt", func(s *runnerEvidenceSinkFake) {
			s.replayErr = fail(CodeEvidenceJournalCorrupt, "fake", "secret", errors.New("secret"))
		}, CodeEvidenceJournalCorrupt, true},
		{"replay-corrupt-dominates-context-cause", func(s *runnerEvidenceSinkFake) {
			s.replayErr = fail(CodeEvidenceJournalCorrupt, "fake", "secret", context.Canceled)
		}, CodeEvidenceJournalCorrupt, true},
		{"close-failure-dominates", func(s *runnerEvidenceSinkFake) { s.sessionCloseErr = errors.New("secret-close") }, CodeEvidenceJournalFailed, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, decision := buildExactAdmissionRuntime(t)
			backend := &fakeBackend{}
			connector := &fakeConnector{backend: backend}
			verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
			sink := &runnerEvidenceSinkFake{}
			test.configure(sink)
			source := &memoryArtifactSource{data: raw}
			before := liveVerifiedEvidenceRunBindings()
			var databaseConnector DatabaseConnector = connector
			if test.name == "close-failure-dominates" {
				databaseConnector = nil
			}
			runner := Runner{Trust: verifier, Evidence: sink, Connector: databaseConnector, Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{}}
			_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
			var migrationErr *Error
			if !errors.As(err, &migrationErr) || migrationErr.Code != test.want || migrationErr.Err != nil || liveVerifiedEvidenceRunBindings() != before {
				t.Fatalf("closed result mapping/cleanup mismatch: err=%#v live=%d/%d", migrationErr, liveVerifiedEvidenceRunBindings(), before)
			}
			if test.wantClosed && (sink.session == nil || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1) {
				t.Fatalf("returned session was not closed exactly once: %+v", sink.session)
			}
			assertNoRunnerSideEffects(t, connector, backend)
		})
	}
}

func TestPublicRunnerRequiresSameVerifierRecoveryBeforeArtifactRead(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	backend := &fakeBackend{}
	connector := &fakeConnector{backend: backend}
	source := &memoryArtifactSource{data: raw}
	observer := &recordingStateObserver{}
	runner := Runner{
		Trust: plainRunnerTrustVerifier{decision: decision}, Connector: connector, Observer: observer,
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
	}
	_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	if !IsCode(err, CodeEvidenceRecoveryRequired) || source.reads != 0 || !reflect.DeepEqual(observer.transitions, []RunnerState{StateVerifyTrust}) {
		t.Fatalf("runner did not reject a verifier without current recovery authority before artifact read: err=%v reads=%d transitions=%v", err, source.reads, observer.transitions)
	}
	assertNoRunnerSideEffects(t, connector, backend)
}

func TestPublicRunnerRejectsMalformedSameVerifierRecoveryWithNoLiveCandidate(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	backend := &fakeBackend{}
	connector := &fakeConnector{backend: backend}
	verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: []byte("{}")}
	source := &memoryArtifactSource{data: raw}
	before := liveVerifiedEvidenceRunBindings()
	runner := Runner{
		Trust: verifier, Connector: connector,
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
	}
	_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	if !IsCode(err, CodeEvidenceRecoveryRequired) || verifier.calls != 1 || verifier.evidenceCalls != 1 || source.reads != 1 || liveVerifiedEvidenceRunBindings() != before {
		t.Fatalf("malformed recovery input crossed or leaked the total binder: err=%v verify=%d evidence=%d reads=%d live=%d/%d", err, verifier.calls, verifier.evidenceCalls, source.reads, liveVerifiedEvidenceRunBindings(), before)
	}
	assertNoRunnerSideEffects(t, connector, backend)
}

func TestPublicRunnerRedactsCurrentEvidenceVerifierFailureBeforeArtifactRead(t *testing.T) {
	raw, decision := buildExactAdmissionRuntime(t)
	backend := &fakeBackend{}
	connector := &fakeConnector{backend: backend}
	verifier := &sequenceTrustVerifier{fallback: decision, recoveryErr: errors.New("secret-current-verifier-cause")}
	source := &memoryArtifactSource{data: raw}
	runner := Runner{
		Trust: verifier, Connector: connector,
		Ledger: &fakeLedgerStore{}, Authority: acceptingAuthority{}, Catalog: acceptingCatalog{}, Intermediate: acceptingIntermediate{},
	}
	_, err := runner.Run(context.Background(), RunRequest{Artifact: source, TargetDSN: "test-only"})
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != CodeUntrusted || migrationErr.Err != nil || verifier.calls != 1 || verifier.evidenceCalls != 1 || source.reads != 0 {
		t.Fatalf("current evidence verifier failure was not redacted before artifact read: err=%#v verify=%d evidence=%d reads=%d", migrationErr, verifier.calls, verifier.evidenceCalls, source.reads)
	}
	assertNoRunnerSideEffects(t, connector, backend)
}

func TestVerifyRunnerCurrentEvidenceOwnsInputAndOutputAndSkipsLooseVerify(t *testing.T) {
	input := CandidateEnvelope{Subject: []byte("candidate-subject"), DetachedEnvelope: []byte("candidate-envelope")}
	artifact := []byte("recovery-artifact")
	verifier := &recordingCurrentEvidenceVerifier{artifact: artifact}
	_, ownedArtifact, err := verifyRunnerCurrentEvidence(context.Background(), verifier, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Subject[0] ^= 1
	input.DetachedEnvelope[0] ^= 1
	artifact[0] ^= 1
	if verifier.looseCalls != 0 || verifier.evidenceCalls != 1 || string(verifier.candidate.Subject) != "candidate-subject" || string(verifier.candidate.DetachedEnvelope) != "candidate-envelope" || string(ownedArtifact) != "recovery-artifact" {
		t.Fatalf("combined verifier aliases or call path escaped ownership: loose=%d evidence=%d candidate=%q/%q artifact=%q", verifier.looseCalls, verifier.evidenceCalls, verifier.candidate.Subject, verifier.candidate.DetachedEnvelope, ownedArtifact)
	}
}

func TestVerifyRunnerCurrentEvidenceRejectsOversizeBeforeCopy(t *testing.T) {
	verifier := &recordingCurrentEvidenceVerifier{artifact: make([]byte, maxDecisionRecoveryArtifactBytes+1)}
	if _, _, err := verifyRunnerCurrentEvidence(context.Background(), verifier, CandidateEnvelope{}); !IsCode(err, CodeEvidenceRecoveryRequired) || verifier.evidenceCalls != 1 {
		t.Fatalf("oversize recovery artifact was not rejected at the combined verifier boundary: err=%v calls=%d", err, verifier.evidenceCalls)
	}
	verifier = &recordingCurrentEvidenceVerifier{artifact: []byte("bounded")}
	if _, _, err := verifyRunnerCurrentEvidence(context.Background(), verifier, CandidateEnvelope{Subject: make([]byte, maxCandidateEnvelopeComponentBytes+1)}); !IsCode(err, CodeUntrusted) || verifier.evidenceCalls != 0 {
		t.Fatalf("oversize candidate subject reached the verifier: err=%v calls=%d", err, verifier.evidenceCalls)
	}
	if _, _, err := verifyRunnerCurrentEvidence(context.Background(), verifier, CandidateEnvelope{DetachedEnvelope: make([]byte, maxCandidateEnvelopeComponentBytes+1)}); !IsCode(err, CodeUntrusted) || verifier.evidenceCalls != 0 {
		t.Fatalf("oversize detached envelope reached the verifier: err=%v calls=%d", err, verifier.evidenceCalls)
	}
}

func TestCurrentEvidenceVerifierHasOneProductionCallEdge(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || len(name) >= 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "verifyCurrentEvidence" {
				return true
			}
			calls++
			if name != "runner.go" {
				t.Fatalf("current evidence verifier call edge spread into %s", name)
			}
			return true
		})
	}
	if calls != 1 {
		t.Fatalf("current evidence verifier call edges=%d want=1", calls)
	}
}

func runnerDecisionRecoveryArtifact(t *testing.T, decision VerifiedTrustDecision) []byte {
	t.Helper()
	bindings, err := decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	projection := func(kind, subject string) decisionRecoveryProjectionSubjectInput {
		bytes := []byte(subject)
		return decisionRecoveryProjectionSubjectInput{
			Kind: kind, SubjectDigest: DigestBytes(bytes),
			SubjectBase64URLNoPadding:          base64.RawURLEncoding.EncodeToString(bytes),
			DetachedEnvelopeBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("signature-" + kind)),
		}
	}
	inputs := decisionRecoveryVerificationInputs{
		FormatVersion: decisionRecoveryArtifactFormatVersion, ProfileDigest: bindings.decisionRecoveryArtifactProfileDigest,
		OldRunnerProjectionDecisionDigest: bindings.runnerProjectionDecisionDigest,
		RepositoryIdentity:                decision.repositoryIdentity, ReleaseIdentity: decision.releaseIdentity,
		CandidateSubjectBase64URLNoPadding:          base64.RawURLEncoding.EncodeToString([]byte("runner-candidate")),
		CandidateDetachedEnvelopeBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("runner-candidate-signature")),
		ProjectionSubjectInputs: []decisionRecoveryProjectionSubjectInput{
			projection("release", "runner-release-subject"),
			projection("authority_profile", "runner-authority-profile-subject"),
			projection("authority_binding", "runner-authority-binding-subject"),
		},
	}
	canonical, err := canonicalContractKey(inputs)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(canonical)
}

func liveVerifiedEvidenceRunBindings() int {
	count := 0
	verifiedEvidenceRunBindingRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
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
	return buildExactAdmissionRuntimeWithAuthority(t, nil)
}

func buildExactAdmissionRuntimeWithAuthority(t *testing.T, expected *AuthorityExpectedProjections) ([]byte, VerifiedTrustDecision) {
	t.Helper()
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	if expected != nil {
		fixture.authorityBinding.ExpectedProjections = cloneProjectionValue(*expected)
		var err error
		fixture.authority, err = bindVerifiedAuthorityContract(mustCanonicalDigest(t, fixture.authorityBinding), fixture.authorityBinding.ExpectedProjections, fixture.expiresAt, uint64(fixture.authorityBinding.SecurityEpoch))
		if err != nil {
			t.Fatal(err)
		}
	}
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

type runnerEvidenceSinkFake struct {
	calls            int
	openErr          error
	valuesWithError  bool
	missingSession   bool
	missingSnapshot  bool
	swapCandidate    bool
	replayErr        error
	sessionCloseErr  error
	mutateSnapshot   func(*RecoverySnapshot)
	configureSession func(*runnerEvidenceSessionFake)
	session          *runnerEvidenceSessionFake
}

func (sink *runnerEvidenceSinkFake) Open(_ context.Context, run VerifiedEvidenceRun, runtime VerifiedRuntimeArtifact) (EvidenceSession, *RecoverySnapshot, error) {
	sink.calls++
	if sink.openErr != nil && !sink.valuesWithError {
		return nil, nil, sink.openErr
	}
	candidate, err := ownedCurrentCandidateFromEvidenceRun(run, runtime)
	if err != nil {
		return nil, nil, err
	}
	session := newRunnerEvidenceSessionFake(candidate)
	session.journal.replayErr = sink.replayErr
	session.closeErr = sink.sessionCloseErr
	if sink.configureSession != nil {
		sink.configureSession(session)
	}
	if sink.mutateSnapshot != nil {
		sink.mutateSnapshot(session.snapshot)
	}
	if sink.swapCandidate {
		session.candidate.binding = &verifiedEvidenceRunBinding{}
	}
	sink.session = session
	if sink.openErr != nil {
		return session, cloneRecoverySnapshot(session.snapshot), sink.openErr
	}
	if sink.missingSession {
		snapshot := cloneRecoverySnapshot(session.snapshot)
		_ = session.Close(context.Background())
		return nil, snapshot, nil
	}
	if sink.missingSnapshot {
		return session, nil, nil
	}
	return session, cloneRecoverySnapshot(session.snapshot), nil
}

func (*runnerEvidenceSinkFake) evidenceSinkSealed() {}

type runnerEvidenceSessionFake struct {
	candidate                   OwnedCurrentCandidate
	active                      ActiveGeneration
	journal                     *runnerEvidenceJournalFake
	snapshot                    *RecoverySnapshot
	bindErr                     error
	bindCalls                   int
	bindNoJournal               bool
	bindNoRecord                bool
	mutateBoundIntent           func(*StatementIntent)
	mutateBoundAuthority        func(*JournalCursor, *OwnedEvidenceRecord)
	intermediateBindErr         error
	intermediateBindCalls       int
	intermediateNoJournal       bool
	intermediateNoRecord        bool
	mutateBoundIntermediate     func(*StatementIntermediateEvidence)
	mutateIntermediateAuthority func(*JournalCursor, *OwnedEvidenceRecord)
	closeErr                    error
	closeCalls                  int
	closed                      bool
}

func newRunnerEvidenceSessionFake(candidate OwnedCurrentCandidate) *runnerEvidenceSessionFake {
	generation := generationIdentity{
		owner: candidate.owner, executionLineageDigest: candidate.verifiedRun.executionLineageDigest,
		journalIdentityDigest: testDigest("runner-open-journal"), runnerProjectionDecisionDigest: candidate.verifiedRun.runnerProjectionDecisionDigest,
		schemaBundleDigest: candidate.verifiedRun.schemaBundleDigest,
	}
	tail := testDigest("runner-open-tail")
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := JournalCursor{
		owner: candidate.owner, generation: generation, segmentIndex: 0, nextSequence: 1,
		previousRecordDigest: digestPointer(tail), lineageIndexNextSequence: 3,
		lineageIndexPreviousRecordDigest: testDigest("runner-open-index-tail"), valid: valid,
	}
	snapshot := &RecoverySnapshot{
		owner: candidate.owner, generation: generation, cursor: cursor, tailDigest: tail,
		state: RecoveryBrandNew, nextPermittedAction: RecoveryBeginFirstAttempt,
	}
	journal := &runnerEvidenceJournalFake{cursor: cursor, snapshot: snapshot}
	session := &runnerEvidenceSessionFake{
		candidate: candidate, journal: journal, snapshot: snapshot,
		active: ActiveGeneration{identity: generation, kind: activeGenerationCurrent, journal: journal, ownedDecision: candidate.verifiedRun.currentDecision},
	}
	journal.session = session
	return session
}

func (session *runnerEvidenceSessionFake) CurrentCandidate() OwnedCurrentCandidate {
	if session == nil || session.closed {
		return OwnedCurrentCandidate{}
	}
	candidate, err := cloneSessionCandidate(session.candidate)
	if err != nil {
		return OwnedCurrentCandidate{}
	}
	return candidate
}

func (session *runnerEvidenceSessionFake) ActiveGeneration() ActiveGeneration {
	if session == nil || session.closed {
		return ActiveGeneration{}
	}
	return session.active
}

func (session *runnerEvidenceSessionFake) Journal() EvidenceJournal {
	if session == nil || session.closed {
		return nil
	}
	return session.journal
}

func (session *runnerEvidenceSessionFake) RecoverySnapshot() *RecoverySnapshot {
	if session == nil || session.closed {
		return nil
	}
	return cloneRecoverySnapshot(session.snapshot)
}

func (*runnerEvidenceSessionFake) ReserveAndActivateSuccessor(context.Context, *VerifiedLineageSupersessionAuthority) (ActiveGeneration, *RecoverySnapshot, error) {
	return ActiveGeneration{}, nil, fail(CodeProjectionNotImplemented, "runner-test", "not implemented", nil)
}

func (session *runnerEvidenceSessionFake) bindRunnerStatementIntentRecord(ctx context.Context, request runnerStatementIntentRecordRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	session.bindCalls++
	if err := ctx.Err(); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if session.bindErr != nil {
		return nil, JournalCursor{}, nil, session.bindErr
	}
	if session.closed || request.candidateBinding == nil || request.candidateBinding != session.candidate.binding || !sameGenerationIdentity(request.generation, session.active.identity) || request.maxAttempts == 0 || generationJournalRecoveryDigest(session.snapshot) != request.recoveryDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-test-statement-bind", "statement binder inputs differ", nil)
	}
	bindings, err := session.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	catalogContract, err := runnerStatementIntentVerifiedSubject(bindings, request.plan, request.authorityBefore, request.catalogBefore)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	intent, err := buildBrandNewRunnerStatementIntent(
		request.plan, request.authorityBefore, request.catalogBefore, catalogContract,
		request.generation.schemaBundleDigest, bindings.authorityProfileDigest, bindings.authorityBindingDigest,
	)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if session.mutateBoundIntent != nil {
		session.mutateBoundIntent(&intent)
	}
	cursor := session.journal.cursor.clone()
	ownedPlan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	witness := ownedStatementIntentWitness{ownedAppendContext: ownedAppendContext{generation: request.generation, cursor: cursor.clone()}, plan: ownedPlan}
	owned := &OwnedEvidenceRecord{
		wire: EvidenceRecord{StatementIntent: &intent}, witness: witness,
		generation: request.generation, cursor: cursor.clone(), consumed: &atomic.Bool{},
	}
	if session.mutateBoundAuthority != nil {
		session.mutateBoundAuthority(&cursor, owned)
	}
	session.journal.maxAttempts = request.maxAttempts
	if session.bindNoJournal {
		return nil, cursor, owned, nil
	}
	if session.bindNoRecord {
		return session.journal, cursor, nil, nil
	}
	return session.journal, cursor, owned, nil
}

func (*runnerEvidenceSessionFake) runnerStatementIntentRecordBinderSealed() {}

func (session *runnerEvidenceSessionFake) bindRunnerIntermediateRecord(ctx context.Context, request runnerIntermediateRecordRequest) (EvidenceJournal, JournalCursor, *OwnedEvidenceRecord, error) {
	session.intermediateBindCalls++
	if err := ctx.Err(); err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if session.intermediateBindErr != nil {
		return nil, JournalCursor{}, nil, session.intermediateBindErr
	}
	if session.closed || request.candidateBinding == nil || request.candidateBinding != session.candidate.binding || !sameGenerationIdentity(request.generation, session.active.identity) || request.maxAttempts == 0 || generationJournalRecoveryDigest(session.snapshot) != request.recoveryDigest {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-test-intermediate-bind", "intermediate binder inputs differ", nil)
	}
	bindings, err := session.candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil || runnerFinalIntermediateVerifiedSubjects(bindings, request) != nil {
		return nil, JournalCursor{}, nil, fail(CodeEvidenceRecoveryRequired, "runner-test-intermediate-bind", "intermediate verified subjects differ", nil)
	}
	intermediate, err := buildRunnerFinalIntermediateEvidence(request)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	if session.mutateBoundIntermediate != nil {
		session.mutateBoundIntermediate(&intermediate)
	}
	cursor := session.journal.cursor.clone()
	plan, err := cloneRunnerStatementIntentPlan(request.plan)
	if err != nil {
		return nil, JournalCursor{}, nil, err
	}
	priorIntent := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence - 1,
		RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: cloneStatementIntentPointer(&request.intent)},
		RecordDigest: *cursor.previousRecordDigest,
	}
	witness := ownedIntermediateWitness{
		ownedAppendContext: ownedAppendContext{generation: request.generation, cursor: cursor.clone()},
		plan:               plan, stateDigest: request.state.IntermediateStateDigest, priorIntent: priorIntent,
	}
	owned := &OwnedEvidenceRecord{
		wire: EvidenceRecord{Intermediate: &intermediate}, witness: witness,
		generation: request.generation, cursor: cursor.clone(), consumed: &atomic.Bool{},
	}
	if session.mutateIntermediateAuthority != nil {
		session.mutateIntermediateAuthority(&cursor, owned)
	}
	session.journal.maxAttempts = request.maxAttempts
	if session.intermediateNoJournal {
		return nil, cursor, owned, nil
	}
	if session.intermediateNoRecord {
		return session.journal, cursor, nil, nil
	}
	return session.journal, cursor, owned, nil
}

func (*runnerEvidenceSessionFake) runnerIntermediateRecordBinderSealed() {}

func (session *runnerEvidenceSessionFake) Close(ctx context.Context) error {
	if session == nil || session.closed {
		return errors.New("session already closed")
	}
	session.closeCalls++
	session.closed = true
	journalErr := session.journal.Close(ctx)
	if session.closeErr != nil {
		return session.closeErr
	}
	return journalErr
}

func (*runnerEvidenceSessionFake) evidenceSessionSealed() {}

type runnerEvidenceJournalFake struct {
	session               *runnerEvidenceSessionFake
	cursor                JournalCursor
	snapshot              *RecoverySnapshot
	maxAttempts           uint32
	replayErr             error
	replayCalls           int
	appendErr             error
	appendValuesWithError bool
	appendOutcome         appendOutcome
	mutateAppendResult    func(*AppendResult)
	mutateAppendSnapshot  func(*RecoverySnapshot)
	appendCalls           int
	appendedRecord        EvidenceRecord
	closeCalls            int
	closed                bool
}

func (journal *runnerEvidenceJournalFake) Replay(context.Context) (JournalCursor, *RecoverySnapshot, error) {
	journal.replayCalls++
	if journal.replayErr != nil {
		return JournalCursor{}, nil, journal.replayErr
	}
	if journal.closed {
		return JournalCursor{}, nil, errors.New("journal closed")
	}
	return journal.cursor.clone(), cloneRecoverySnapshot(journal.snapshot), nil
}

func (journal *runnerEvidenceJournalFake) AppendDurable(ctx context.Context, cursor JournalCursor, owned *OwnedEvidenceRecord) (AppendResult, error) {
	journal.appendCalls++
	if err := ctx.Err(); err != nil {
		return AppendResult{}, err
	}
	if journal.closed {
		return AppendResult{}, errors.New("journal closed")
	}
	if journal.appendErr != nil && !journal.appendValuesWithError {
		return AppendResult{}, journal.appendErr
	}
	record, err := owned.consume(cursor.generation, cursor)
	kind := EvidenceRecordKind("")
	if err == nil {
		switch {
		case record.StatementIntent != nil:
			kind = EvidenceRecordStatementIntent
		case record.Intermediate != nil:
			kind = EvidenceRecordIntermediate
		default:
			err = errors.New("unsupported runner evidence record")
		}
	}
	if err != nil || validateEvidenceRecord(record) != nil {
		if err == nil {
			err = errors.New("invalid runner evidence record")
		}
		return AppendResult{}, err
	}
	previousSnapshot := cloneRecoverySnapshot(journal.snapshot)
	journal.appendedRecord = cloneEvidenceRecord(record)
	frame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence,
		PreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest),
		RecordKind:           kind, Record: cloneEvidenceRecord(record),
	}
	frame.RecordDigest, err = frame.ComputeDigest()
	if err != nil || frame.Validate() != nil {
		return AppendResult{}, errors.New("invalid statement frame")
	}
	checkpoint := DigestBytes([]byte("runner-test-checkpoint:" + frame.RecordDigest.String()))
	outcome := journal.appendOutcome
	if outcome == "" {
		outcome = appendOutcomeDurable
	}
	var durable *JournalCursor
	if outcome == appendOutcomeDurable {
		valid := &atomic.Bool{}
		valid.Store(true)
		next := cursor.clone()
		next.valid = valid
		next.nextSequence++
		next.previousRecordDigest = digestPointer(frame.RecordDigest)
		next.lineageIndexNextSequence++
		next.lineageIndexPreviousRecordDigest = checkpoint
		next.latestCheckpointRecordDigest = digestPointer(checkpoint)
		durable = &next
	}
	result, err := finishConsumedAppend(cursor, cursor.generation, outcome, durable, frame.RecordDigest, checkpoint)
	if err != nil {
		return AppendResult{}, err
	}
	if outcome == appendOutcomeDurable {
		next := result.DurableCursor()
		var snapshot *RecoverySnapshot
		switch kind {
		case EvidenceRecordStatementIntent:
			action := recoveryAbortAction(record.StatementIntent.AttemptIndex, journal.maxAttempts)
			snapshot = &RecoverySnapshot{
				owner: cursor.generation.owner, generation: cursor.generation, cursor: next.clone(), tailDigest: frame.RecordDigest,
				state: RecoveryDanglingStatementIntent, migrationID: cloneStringPointer(&record.StatementIntent.MigrationID),
				attemptIndex:                    cloneUint32Pointer(&record.StatementIntent.AttemptIndex),
				lastStatementIntent:             recoveredValue(cursor.generation, *next, frame.RecordDigest, frame.RecordDigest, *record.StatementIntent),
				lastStatementIntentRecordDigest: digestPointer(frame.RecordDigest), nextPermittedAction: action,
			}
		case EvidenceRecordIntermediate:
			if previousSnapshot == nil || previousSnapshot.lastStatementIntent == nil || previousSnapshot.lastStatementIntentRecordDigest == nil {
				return AppendResult{}, errors.New("intermediate predecessor is unavailable")
			}
			intent := previousSnapshot.lastStatementIntent.value
			action := recoveryAbortAction(record.Intermediate.State.AttemptIndex, journal.maxAttempts)
			snapshot = &RecoverySnapshot{
				owner: cursor.generation.owner, generation: cursor.generation, cursor: next.clone(), tailDigest: frame.RecordDigest,
				state: RecoveryDanglingIntermediate, migrationID: cloneStringPointer(&record.Intermediate.State.MigrationID),
				attemptIndex: cloneUint32Pointer(&record.Intermediate.State.AttemptIndex), previousAttemptTerminalDigest: cloneDigestPointer(intent.PreviousAttemptTerminalDigest),
				lastStatementIntent:                  recoveredValue(cursor.generation, *next, frame.RecordDigest, *previousSnapshot.lastStatementIntentRecordDigest, intent),
				lastStatementIntentRecordDigest:      cloneDigestPointer(previousSnapshot.lastStatementIntentRecordDigest),
				lastIntermediateEvidence:             recoveredValue(cursor.generation, *next, frame.RecordDigest, frame.RecordDigest, *record.Intermediate),
				lastIntermediateEvidenceRecordDigest: digestPointer(frame.RecordDigest),
				lastIntermediateStateDigest:          digestPointer(record.Intermediate.State.IntermediateStateDigest), nextPermittedAction: action,
			}
		}
		if journal.mutateAppendSnapshot != nil {
			journal.mutateAppendSnapshot(snapshot)
		}
		journal.cursor = next.clone()
		journal.snapshot = snapshot
		if journal.session != nil {
			journal.session.snapshot = snapshot
		}
	}
	if journal.mutateAppendResult != nil {
		journal.mutateAppendResult(&result)
	}
	return result, journal.appendErr
}

func (journal *runnerEvidenceJournalFake) Close(context.Context) error {
	if journal.closed {
		return errors.New("journal already closed")
	}
	journal.closeCalls++
	journal.closed = true
	if journal.cursor.valid != nil {
		journal.cursor.valid.Store(false)
	}
	return nil
}

func (*runnerEvidenceJournalFake) evidenceJournalSealed() {}

type sequenceTrustVerifier struct {
	recoveryVerifierFake
	decisions        []VerifiedTrustDecision
	fallback         VerifiedTrustDecision
	calls            int
	evidenceCalls    int
	recoveryArtifact []byte
	recoveryErr      error
}

type plainRunnerTrustVerifier struct{ decision VerifiedTrustDecision }

func (verifier plainRunnerTrustVerifier) Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error) {
	return verifier.decision, nil
}

type recordingCurrentEvidenceVerifier struct {
	decision      VerifiedTrustDecision
	artifact      []byte
	candidate     CandidateEnvelope
	looseCalls    int
	evidenceCalls int
}

func (verifier *recordingCurrentEvidenceVerifier) Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error) {
	verifier.looseCalls++
	return verifier.decision, nil
}

func (verifier *recordingCurrentEvidenceVerifier) verifyCurrentEvidence(_ context.Context, candidate CandidateEnvelope) (VerifiedTrustDecision, []byte, error) {
	verifier.evidenceCalls++
	verifier.candidate = candidate
	return verifier.decision, verifier.artifact, nil
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

func (verifier *sequenceTrustVerifier) verifyCurrentEvidence(ctx context.Context, candidate CandidateEnvelope) (VerifiedTrustDecision, []byte, error) {
	verifier.evidenceCalls++
	decision, err := verifier.Verify(ctx, candidate)
	if err != nil {
		return VerifiedTrustDecision{}, nil, err
	}
	if verifier.recoveryErr != nil {
		return decision, nil, verifier.recoveryErr
	}
	return decision, verifier.recoveryArtifact, nil
}
