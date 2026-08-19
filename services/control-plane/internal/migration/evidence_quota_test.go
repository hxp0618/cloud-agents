package migration

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func quotaFactsForTest(counts []uint64, attempts uint64) verifiedQuotaBundleFacts {
	runtimeInputs := sha256.Sum256([]byte("owned-runtime-inputs"))
	outer := []byte("owned-runtime-artifact")
	facts := verifiedQuotaBundleFacts{schemaBundleDigest: DigestBytes([]byte("quota-schema")), lineageQuotaProfile: EvidenceLimitsProfile, maxAttempts: attempts, statementCounts: append([]uint64(nil), counts...), runtimeInputs: runtimeInputs, outerArtifactDigest: DigestBytes(outer), outerArtifactSize: uint64(len(outer))}
	facts.canonical = quotaBundleFactsDigest(facts.lineageQuotaProfile, facts.schemaBundleDigest, facts.maxAttempts, facts.statementCounts, facts.runtimeInputs, facts.outerArtifactDigest, facts.outerArtifactSize)
	return facts
}

func rootFactsForTest(t *testing.T, objects []rootQuotaObjectFact) rootQuotaUsageFacts {
	t.Helper()
	facts := rootQuotaUsageFacts{finalObjects: canonicalRootObjects(objects)}
	for _, object := range objects {
		facts.finalObjectBytes += object.size
	}
	if facts.exceedsLimits() || !facts.valid() {
		t.Fatal("invalid test-only root quota facts")
	}
	return facts
}

func TestProductionRootQuotaStateBinderFailsClosed(t *testing.T) {
	fs := newFakeEvidenceFS()
	root := activeTestRootLease()
	lock := testLineageHandle(root, testEvidenceLockFile(fs, 202, 202, evidenceLineageLockKind))
	if root.publicationLease() != nil {
		t.Fatal("test root facade minted publication or quota authority")
	}
	facts := rootFactsForTest(t, nil)
	fakeState := &verifiedRootQuotaState{facts: facts, canonical: rootQuotaFactsDigest(facts), lock: lock}
	if validRootQuotaLock(lock) {
		t.Fatal("fake active root facade was accepted as quota authority")
	}
	// Even a same-package facade satisfying the handle interface is rejected by
	// exact production-wrapper type before any borrowed publication lease could
	// be considered. This fake returns nil because tests cannot mint that lease.
	borrowed := testLineageHandle(&borrowedEvidenceRootLease{active: true}, testEvidenceLockFile(fs, 203, 203, evidenceLineageLockKind))
	if validRootQuotaLock(borrowed) {
		t.Fatal("borrowed root facade was accepted as quota authority")
	}
	zeroProduction := &evidenceFSRootLease{}
	zeroLock := testLineageHandle(zeroProduction, testEvidenceLockFile(fs, 204, 204, evidenceLineageLockKind))
	if validRootQuotaLock(zeroLock) {
		t.Fatal("unsealed production wrapper was accepted as quota authority")
	}
	if _, err := fakeState.snapshot(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("fake quota state snapshot was accepted: %v", err)
	}
	bundle := quotaAdmissionBundleForTest(t)
	candidate := quotaCandidateForBundle(t, bundle, []byte("recovery"))
	if _, err := admitRootQuota(fakeState, bundle, candidate); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("fake quota state admission was accepted: %v", err)
	}
	if state, err := bindVerifiedRootQuotaState(lock, verifiedRootQuotaScan{}); state != nil || !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("production root quota state binder did not fail closed: state=%+v err=%v", state, err)
	}
}

type borrowedEvidenceRootLease struct{ active bool }

func (l *borrowedEvidenceRootLease) Active() bool                          { return l != nil && l.active }
func (l *borrowedEvidenceRootLease) Close() error                          { l.active = false; return nil }
func (*borrowedEvidenceRootLease) publicationLease() *evidencefs.RootLease { return nil }

func quotaAdmissionBundleForTest(t *testing.T) *RuntimeBundle {
	t.Helper()
	raw, manifest := buildCheckedInRuntimeTar(t)
	bundle, err := LoadRuntimeBundle(raw, testTrustDecision(raw, manifest))
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func quotaCandidateForBundle(t *testing.T, bundle *RuntimeBundle, recoveryBytes []byte) OwnedCurrentCandidate {
	t.Helper()
	runtimeBytes, runtimeManifest := buildCheckedInRuntimeTar(t)
	if DigestBytes(runtimeBytes) != bundle.ownedInputs.outerArtifactDigest || uint64(len(runtimeBytes)) != bundle.ownedInputs.outerArtifactSize || runtimeManifest.SchemaBundleDigest != bundle.quotaFacts.schemaBundleDigest {
		t.Fatal("test bundle outer artifact identity drift")
	}
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	fixture.decision.expectedSchemaBundleDigest = bundle.quotaFacts.schemaBundleDigest
	fixture.decision.expectedOuterArtifactDigest = DigestBytes(runtimeBytes)
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	condition := CatalogPrecondition{AcceptedStates: []CatalogStateProjection{{Absent: &SchemaAbsentProjection{State: "schema_absent", Scope: scope, Schema: "cloud_agents"}}, {Present: &SchemaPresentProjection{State: "schema_present", Scope: scope, Body: minimalCatalogBody()}}}}
	var err error
	fixture.initialScope, err = bindVerifiedSchemaBundleScope(fixture.decision.expectedSchemaBundleDigest, scope, condition, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}, fixture.expiresAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, fixture.catalogs, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	projection := func(kind, subject string) decisionRecoveryProjectionSubjectInput {
		bytes := []byte(subject)
		return decisionRecoveryProjectionSubjectInput{Kind: kind, SubjectDigest: DigestBytes(bytes), SubjectBase64URLNoPadding: base64.RawURLEncoding.EncodeToString(bytes), DetachedEnvelopeBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("signature-" + kind))}
	}
	inputs := decisionRecoveryVerificationInputs{
		FormatVersion: decisionRecoveryArtifactFormatVersion, ProfileDigest: decisionRecoveryArtifactProfileDigest,
		OldRunnerProjectionDecisionDigest: bindings.runnerProjectionDecisionDigest, RepositoryIdentity: bound.repositoryIdentity, ReleaseIdentity: bound.releaseIdentity,
		CandidateSubjectBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("candidate")), CandidateDetachedEnvelopeBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("candidate-signature")),
		ProjectionSubjectInputs: []decisionRecoveryProjectionSubjectInput{projection("release", "release"), projection("authority_profile", "profile"), projection("authority_binding", "binding")},
	}
	canonical, err := canonicalContractKey(inputs)
	if err != nil {
		t.Fatal(err)
	}
	verifier := &recoveryVerifierFake{}
	current, recovery, err := bindVerifierOwnedDecision(verifier, bound, bindings.runnerProjectionDecisionDigest, []byte(canonical))
	if err != nil {
		t.Fatal(err)
	}
	_, _, candidate, err := bindVerifiedEvidenceRun(bound, bindings, current, runtimeBytes, recovery)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(recoveryBytes)) > maxDecisionRecoveryArtifactBytes {
		candidate.decisionRecoveryArtifact.bytes = append(candidate.decisionRecoveryArtifact.bytes, make([]byte, maxDecisionRecoveryArtifactBytes)...)
		candidate.decisionRecoveryArtifact.sizeBytes = uint64(len(candidate.decisionRecoveryArtifact.bytes))
		candidate.decisionRecoveryArtifact.digest = DigestBytes(candidate.decisionRecoveryArtifact.bytes)
	}
	return candidate
}

func TestEvidenceQuotaReservationExactFormula(t *testing.T) {
	facts := quotaFactsForTest([]uint64{1}, 1)
	brandNew := rootFactsForTest(t, nil)
	got, err := calculateEvidenceQuotaReservationForFacts(facts, brandNew)
	if err != nil {
		t.Fatal(err)
	}
	callerBytes := evidenceRecordFrameLimits[EvidenceRecordStatementIntent] + evidenceRecordFrameLimits[EvidenceRecordIntermediate] + evidenceRecordFrameLimits[EvidenceRecordCommitIntent] + evidenceRecordFrameLimits[EvidenceRecordAttemptTerminal] + evidenceRecordFrameLimits[EvidenceRecordAmbiguousResolution]
	wantRecords := uint64(6)
	wantJournal := evidenceRecordFrameLimits[EvidenceRecordHeader] + callerBytes
	wantCheckpoints := wantRecords - 1
	wantIndexRecords := uint64(1 + 2 + wantCheckpoints + 1)
	wantIndex := lineageRecordFrameLimits[LineageRecordHeader] + lineageRecordFrameLimits[LineageRecordGenerationReserved] + lineageRecordFrameLimits[LineageRecordGenerationActivated] + lineageRecordFrameLimits[LineageRecordGenerationCheckpoint]*wantCheckpoints + lineageRecordFrameLimits[LineageRecordGenerationSuperseded]
	if got.ReservedRecords != wantRecords || got.ReservedJournalBytes != wantJournal || got.ReservedSegments != 1 || got.ReservedCheckpointRecords != wantCheckpoints || got.ReservedIndexRecords != wantIndexRecords || got.ReservedIndexBytes != wantIndex || got.ReservedBytes != wantJournal+wantIndex {
		t.Fatalf("reservation=%+v", got)
	}
	existingState := rootFactsForTest(t, nil)
	existingState.targetIndexPresent = true
	existingState.targetIndexRecords = 1
	existingState.targetIndexBytes = lineageRecordFrameLimits[LineageRecordHeader]
	existing, err := calculateEvidenceQuotaReservationForFacts(facts, existingState)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReservedIndexRecords != existing.ReservedIndexRecords+1 || got.ReservedIndexBytes != existing.ReservedIndexBytes+lineageRecordFrameLimits[LineageRecordHeader] {
		t.Fatalf("brand-new delta got=%+v existing=%+v", got, existing)
	}
}

func TestEvidenceQuotaProfilesPreserveV1AndBoundV2CheckpointArithmetic(t *testing.T) {
	t.Parallel()
	legacy := quotaFactsForTest([]uint64{20, 71, 46, 20, 1}, 3)
	legacyReservation, err := calculateEvidenceQuotaReservationForFacts(legacy, rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if legacyReservation.ReservedRecords != 1003 || legacyReservation.ReservedJournalBytes != 158597120 || legacyReservation.ReservedSegments != 10 || legacyReservation.ReservedCheckpointRecords != 1002 || legacyReservation.ReservedIndexRecords != 1006 || legacyReservation.ReservedIndexBytes != 16711680 || legacyReservation.ReservedBytes != 175308800 {
		t.Fatalf("historical v1 arithmetic drifted: %+v", legacyReservation)
	}

	v2 := legacy
	v2.lineageQuotaProfile = LineageQuotaProfileV2
	v2.canonical = quotaBundleFactsDigest(v2.lineageQuotaProfile, v2.schemaBundleDigest, v2.maxAttempts, v2.statementCounts, v2.runtimeInputs, v2.outerArtifactDigest, v2.outerArtifactSize)
	v2Reservation, err := calculateEvidenceQuotaReservationForFacts(v2, rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	checkpointDelta := uint64(16<<10-v2GenerationCheckpointMaximum) * legacyReservation.ReservedCheckpointRecords
	if v2Reservation.ReservedRecords != legacyReservation.ReservedRecords || v2Reservation.ReservedJournalBytes != legacyReservation.ReservedJournalBytes || v2Reservation.ReservedSegments != legacyReservation.ReservedSegments || v2Reservation.ReservedCheckpointRecords != legacyReservation.ReservedCheckpointRecords || v2Reservation.ReservedIndexRecords != legacyReservation.ReservedIndexRecords || v2Reservation.ReservedIndexBytes+checkpointDelta != legacyReservation.ReservedIndexBytes || v2Reservation.ReservedBytes+checkpointDelta != legacyReservation.ReservedBytes {
		t.Fatalf("v1/v2 profile arithmetic diverged outside checkpoint bytes: v1=%+v v2=%+v delta=%d", legacyReservation, v2Reservation, checkpointDelta)
	}
	v3 := v2
	v3.lineageQuotaProfile = LineageQuotaProfileV3
	v3.canonical = quotaBundleFactsDigest(v3.lineageQuotaProfile, v3.schemaBundleDigest, v3.maxAttempts, v3.statementCounts, v3.runtimeInputs, v3.outerArtifactDigest, v3.outerArtifactSize)
	for name, value := range map[string]struct {
		digest [32]byte
		want   string
	}{
		"v1": {legacy.canonical, "a3f1fd3860da0391d78ede13d1c1cda9ec6947e9d312ba80190a75e20f830cd3"},
		"v2": {v2.canonical, "0b5b05b75946f95c371a3ec0aee85d5cb520746618c200650fa4bcf704496297"},
		"v3": {v3.canonical, "e8e685f7046ad0b032ffb7b13754ef4d0eccc6866b2dfe57e993a707031c18f6"},
	} {
		if got := hex.EncodeToString(value.digest[:]); got != value.want {
			t.Fatalf("%s quota bundle facts digest drifted: %s", name, got)
		}
	}
	v3Reservation, err := calculateEvidenceQuotaReservationForFacts(v3, rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if v3Reservation != (evidenceQuotaReservation{lineageQuotaProfile: LineageQuotaProfileV3, ReservedRecords: v2Reservation.ReservedRecords, ReservedJournalBytes: v2Reservation.ReservedJournalBytes, ReservedSegments: v2Reservation.ReservedSegments, ReservedCheckpointRecords: v2Reservation.ReservedCheckpointRecords, ReservedIndexRecords: v2Reservation.ReservedIndexRecords, ReservedIndexBytes: v2Reservation.ReservedIndexBytes, ReservedBytes: v2Reservation.ReservedBytes}) {
		t.Fatalf("v3 arithmetic drifted below the v2 capacity ceiling: v2=%+v v3=%+v", v2Reservation, v3Reservation)
	}
	if _, err := calculateEvidenceQuotaReservationForFacts(verifiedQuotaBundleFacts{lineageQuotaProfile: "future", maxAttempts: 1, statementCounts: []uint64{1}}, rootFactsForTest(t, nil)); err == nil {
		t.Fatal("unknown quota profile was accepted by arithmetic")
	}
}

func TestEvidenceQuotaRotationAndSegmentLimit(t *testing.T) {
	// One statement/attempt consumes five caller records. Find a bounded shape
	// that rotates and assert first-fit state exactly.
	got, err := calculateEvidenceQuotaReservationForFacts(quotaFactsForTest([]uint64{52}, 1), rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.ReservedSegments <= 1 {
		t.Fatalf("expected rotation: %+v", got)
	}
	if got.ReservedRecords != 1+52*2+3+uint64(got.ReservedSegments-1) {
		t.Fatalf("rotation headers missing: %+v", got)
	}

	if _, err := calculateEvidenceQuotaReservationForFacts(quotaFactsForTest([]uint64{4096}, 16), rootFactsForTest(t, nil)); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("segment 17/excess was admitted: %v", err)
	}
	v3 := quotaFactsForTest([]uint64{527}, 3)
	v3.lineageQuotaProfile = LineageQuotaProfileV3
	v3.canonical = quotaBundleFactsDigest(v3.lineageQuotaProfile, v3.schemaBundleDigest, v3.maxAttempts, v3.statementCounts, v3.runtimeInputs, v3.outerArtifactDigest, v3.outerArtifactSize)
	v3Reservation, err := calculateEvidenceQuotaReservationForFacts(v3, rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if v3Reservation.ReservedSegments != maxSupportedEvidenceReservedSegments || v3Reservation.ReservedRecords != 3203 || v3Reservation.ReservedJournalBytes != 519700480 || v3Reservation.ReservedIndexRecords != 3206 || v3Reservation.ReservedIndexBytes != 13410304 || v3Reservation.ReservedBytes != 533110784 {
		t.Fatalf("exact v3 32-segment arithmetic drifted: %+v", v3Reservation)
	}
	v2 := v3
	v2.lineageQuotaProfile = LineageQuotaProfileV2
	v2.canonical = quotaBundleFactsDigest(v2.lineageQuotaProfile, v2.schemaBundleDigest, v2.maxAttempts, v2.statementCounts, v2.runtimeInputs, v2.outerArtifactDigest, v2.outerArtifactSize)
	if _, err := calculateEvidenceQuotaReservationForFacts(v2, rootFactsForTest(t, nil)); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("v2 admitted a v3-only 32-segment reservation: %v", err)
	}
	v3.statementCounts = []uint64{544}
	v3.canonical = quotaBundleFactsDigest(v3.lineageQuotaProfile, v3.schemaBundleDigest, v3.maxAttempts, v3.statementCounts, v3.runtimeInputs, v3.outerArtifactDigest, v3.outerArtifactSize)
	if _, err := calculateEvidenceQuotaReservationForFacts(v3, rootFactsForTest(t, nil)); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("v3 admitted segment 33/whole-bundle +1 capacity: %v", err)
	}
}

func TestEvidenceQuotaFactsDetectAliasAndOverflow(t *testing.T) {
	facts := quotaFactsForTest([]uint64{1}, 1)
	facts.statementCounts[0] = 2
	if _, err := calculateEvidenceQuotaReservationForFacts(facts, rootFactsForTest(t, nil)); !IsCode(err, CodeUntrusted) {
		t.Fatalf("aliased facts: %v", err)
	}
	facts = quotaFactsForTest([]uint64{math.MaxUint64}, math.MaxUint64)
	if _, err := calculateEvidenceQuotaReservationForFacts(facts, rootFactsForTest(t, nil)); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("overflow: %v", err)
	}
}

func TestCheckedInBundleQuotaReservationExact(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	bundle, err := LoadRuntimeBundle(raw, testTrustDecision(raw, manifest))
	if err != nil {
		t.Fatal(err)
	}
	if !bundle.quotaFacts.valid() || bundle.quotaFacts.lineageQuotaProfile != LineageQuotaProfileV3 || bundle.quotaFacts.maxAttempts != 3 || !canonicalEqual(bundle.quotaFacts.statementCounts, []uint64{20, 71, 46, 20, 1, 1, 89, 34, 30}) {
		t.Fatalf("unexpected current facts: %+v", bundle.quotaFacts)
	}
	through000008 := quotaFactsForTest(bundle.quotaFacts.statementCounts[:8], bundle.quotaFacts.maxAttempts)
	through000008.lineageQuotaProfile = LineageQuotaProfileV3
	through000008.canonical = quotaBundleFactsDigest(through000008.lineageQuotaProfile, through000008.schemaBundleDigest, through000008.maxAttempts, through000008.statementCounts, through000008.runtimeInputs, through000008.outerArtifactDigest, through000008.outerArtifactSize)
	previous, err := calculateEvidenceQuotaReservationForFacts(through000008, rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if previous.ReservedSegments != 17 || previous.ReservedRecords != 1781 || previous.ReservedCheckpointRecords != 1780 || previous.ReservedJournalBytes != 282492928 || previous.ReservedIndexRecords != 1784 || previous.ReservedIndexBytes != 7585792 || previous.ReservedBytes != 290078720 {
		t.Fatalf("000008 v3 reservation drift: %+v", previous)
	}
	through000008.lineageQuotaProfile = LineageQuotaProfileV2
	through000008.canonical = quotaBundleFactsDigest(through000008.lineageQuotaProfile, through000008.schemaBundleDigest, through000008.maxAttempts, through000008.statementCounts, through000008.runtimeInputs, through000008.outerArtifactDigest, through000008.outerArtifactSize)
	if _, err := calculateEvidenceQuotaReservationForFacts(through000008, rootFactsForTest(t, nil)); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("000008 reservation was admitted under v2: %v", err)
	}
	reservation, err := calculateEvidenceQuotaReservationForFacts(bundle.quotaFacts, rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if reservation.lineageQuotaProfile != LineageQuotaProfileV3 || reservation.ReservedSegments != 19 || reservation.ReservedRecords != 1972 || reservation.ReservedCheckpointRecords != 1971 || reservation.ReservedJournalBytes != 312639488 || reservation.ReservedIndexRecords != 1975 || reservation.ReservedIndexBytes != 8368128 || reservation.ReservedBytes != 321007616 {
		t.Fatalf("checked-in reservation drift: %+v", reservation)
	}
	ownedFacts, err := bundle.quotaFactsForAdmission()
	if err != nil {
		t.Fatal(err)
	}
	bundle.Manifest.ExecutionPolicy.MaxAttempts++
	bundle.Manifest.SchemaBundle.Migrations[0].SQLArtifact.SizeBytes++
	bundle.Files[bundle.ownedInputs.manifest.SchemaBundle.Migrations[0].SQLArtifact.Path][0] ^= 0x01
	rechecked, err := bundle.quotaFactsForAdmission()
	if err != nil || rechecked.canonical != ownedFacts.canonical {
		t.Fatalf("public bundle mutation changed owned quota authority: got=%+v err=%v", rechecked, err)
	}
	bundle.quotaFacts.statementCounts[0]++
	if _, err := bundle.quotaFactsForAdmission(); !IsCode(err, CodeUntrusted) {
		t.Fatalf("private quota authority mutation was accepted: %v", err)
	}
}

func TestRootQuotaAdmissionDedupeExactAndOneShot(t *testing.T) {
	bundle := quotaAdmissionBundleForTest(t)
	candidate := quotaCandidateForBundle(t, bundle, make([]byte, maxDecisionRecoveryArtifactBytes))
	runtime := candidate.runtimeArtifact
	facts := rootFactsForTest(t, []rootQuotaObjectFact{{digest: runtime.digest, size: runtime.sizeBytes}})
	got, err := calculateRootQuotaAdmission(facts, bundle, candidate)
	if err != nil {
		t.Fatal(err)
	}
	historical, err := calculateRootQuotaAdmissionForArtifacts(facts, bundle.quotaFacts, candidate.runtimeArtifact, candidate.decisionRecoveryArtifact)
	if err != nil || historical != got {
		t.Fatalf("historical artifact quota differs from current-candidate quota: current=%+v historical=%+v err=%v", got, historical, err)
	}
	if got.finalObjectCount != 2 || got.finalObjectBytes != runtime.sizeBytes+candidate.decisionRecoveryArtifact.sizeBytes {
		t.Fatalf("dedupe=%+v", got)
	}
	reservation, err := calculateEvidenceQuotaReservationForFacts(bundle.quotaFacts, facts)
	if err != nil {
		t.Fatal(err)
	}
	if got.journalReservedBytes != facts.journalReservedBytes+reservation.ReservedJournalBytes || got.indexReservedBytes != facts.indexActualBytes+facts.indexReservedBytes+reservation.ReservedIndexBytes {
		t.Fatalf("journal/index reservation components were not debited exactly once: admission=%+v reservation=%+v", got, reservation)
	}
	facts = rootFactsForTest(t, nil)
	tinyCandidate := candidate
	tinyCandidate.runtimeArtifact = VerifiedRuntimeArtifact{owner: runtime.owner, bytes: []byte("tiny"), digest: DigestBytes([]byte("tiny")), sizeBytes: 4}
	if _, err := calculateRootQuotaAdmission(facts, bundle, tinyCandidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("runtime tiny swap: %v", err)
	}
	facts = rootFactsForTest(t, nil)
	candidate = quotaCandidateForBundle(t, bundle, make([]byte, maxDecisionRecoveryArtifactBytes+1))
	if _, err := calculateRootQuotaAdmission(facts, bundle, candidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery +1: %v", err)
	}
	facts = rootFactsForTest(t, nil)
	candidate = quotaCandidateForBundle(t, bundle, []byte("recovery"))
	candidate.decisionRecoveryArtifact.owner = &recoveryVerifierOwner{token: &evidenceOwnerToken{}}
	if _, err := calculateRootQuotaAdmission(facts, bundle, candidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery owner swap: %v", err)
	}
	facts = rootFactsForTest(t, nil)
	candidate = quotaCandidateForBundle(t, bundle, []byte("recovery"))
	candidate.decisionRecoveryArtifact.decision = DigestBytes([]byte("other-decision"))
	if _, err := calculateRootQuotaAdmission(facts, bundle, candidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery decision swap: %v", err)
	}
}

func TestRootQuotaUsageExactMaximaAndPlusOne(t *testing.T) {
	objects := make([]rootQuotaObjectFact, rootFinalObjectMaximumCount)
	for index := range objects {
		objects[index] = rootQuotaObjectFact{digest: DigestBytes([]byte{byte(index)}), size: rootTempMaximumEachBytes}
	}
	objects = canonicalRootObjects(objects)
	facts := rootQuotaUsageFacts{finalObjects: objects, finalObjectBytes: rootFinalObjectMaximumBytes}
	facts.tempCount, facts.tempBytes, facts.largestTempBytes = rootTempMaximumCount, rootTempMaximumTotalBytes, rootTempMaximumEachBytes
	facts.journalCount, facts.journalReservedBytes = rootJournalMaximumCount, rootJournalMaximumBytes
	facts.indexCount, facts.indexActualBytes = rootIndexMaximumCount, rootIndexMaximumBytes
	facts.targetIndexPresent, facts.targetIndexRecords, facts.targetIndexBytes = true, lineageIndexMaximumRecords, lineageIndexMaximumBytes
	if !facts.valid() {
		t.Fatal("exact inclusive root maxima rejected")
	}

	mutations := []func(*rootQuotaUsageFacts){
		func(v *rootQuotaUsageFacts) { v.finalObjectBytes++ },
		func(v *rootQuotaUsageFacts) { v.tempCount++ },
		func(v *rootQuotaUsageFacts) { v.tempBytes++ },
		func(v *rootQuotaUsageFacts) { v.largestTempBytes++ },
		func(v *rootQuotaUsageFacts) { v.journalCount++ },
		func(v *rootQuotaUsageFacts) { v.journalReservedBytes++ },
		func(v *rootQuotaUsageFacts) { v.indexCount++ },
		func(v *rootQuotaUsageFacts) { v.indexActualBytes++ },
		func(v *rootQuotaUsageFacts) { v.indexReservedBytes++ },
		func(v *rootQuotaUsageFacts) { v.targetIndexRecords++ },
		func(v *rootQuotaUsageFacts) { v.targetIndexBytes++ },
		func(v *rootQuotaUsageFacts) { v.targetIndexReservedRecords++ },
		func(v *rootQuotaUsageFacts) { v.targetIndexReservedBytes++ },
	}
	for index, mutate := range mutations {
		copyFacts := facts
		copyFacts.finalObjects = append([]rootQuotaObjectFact(nil), facts.finalObjects...)
		mutate(&copyFacts)
		if copyFacts.valid() {
			t.Fatalf("+1 mutation %d admitted", index)
		}
	}
	tooMany := append(append([]rootQuotaObjectFact(nil), objects...), rootQuotaObjectFact{digest: DigestBytes([]byte("object-65")), size: 1})
	tooMany = canonicalRootObjects(tooMany)
	if root := (rootQuotaUsageFacts{finalObjects: tooMany, finalObjectBytes: rootFinalObjectMaximumBytes + 1}); root.valid() {
		t.Fatal("65th final object was accepted")
	}
	oversized := rootQuotaUsageFacts{finalObjects: []rootQuotaObjectFact{{digest: DigestBytes([]byte("object-too-large")), size: rootTempMaximumEachBytes + 1}}, finalObjectBytes: rootTempMaximumEachBytes + 1}
	if oversized.valid() {
		t.Fatal("64 MiB + 1 final object was accepted")
	}
}

func TestRootQuotaAdmissionIncludesHistoricalIndexReservation(t *testing.T) {
	bundle := quotaAdmissionBundleForTest(t)
	candidate := quotaCandidateForBundle(t, bundle, []byte("recovery"))
	facts := rootFactsForTest(t, nil)
	facts.indexCount = 1
	facts.indexActualBytes = 11
	facts.indexReservedBytes = 13
	facts.targetIndexPresent = true
	facts.targetIndexRecords = 1
	facts.targetIndexBytes = 11
	facts.targetIndexReservedRecords = 2
	facts.targetIndexReservedBytes = 13
	if !facts.valid() {
		t.Fatal("historical reservation fixture is invalid")
	}
	reservation, err := calculateEvidenceQuotaReservationForFacts(bundle.quotaFacts, facts)
	if err != nil {
		t.Fatal(err)
	}
	got, err := calculateRootQuotaAdmission(facts, bundle, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.indexReservedBytes != facts.indexActualBytes+facts.indexReservedBytes+reservation.ReservedIndexBytes || got.targetIndexRecords != facts.targetIndexRecords+facts.targetIndexReservedRecords+reservation.ReservedIndexRecords || got.targetIndexReservedBytes != facts.targetIndexBytes+facts.targetIndexReservedBytes+reservation.ReservedIndexBytes {
		t.Fatalf("historical index reservation was not debited: admission=%+v reservation=%+v", got, reservation)
	}

	overRoot := facts
	overRoot.indexActualBytes = rootIndexMaximumBytes
	if overRoot.valid() {
		t.Fatal("root actual plus historical reservation overflow was accepted")
	}
	overTargetRecords := facts
	overTargetRecords.targetIndexRecords = lineageIndexMaximumRecords
	if overTargetRecords.valid() {
		t.Fatal("target records plus historical reservation overflow was accepted")
	}
	overTargetBytes := facts
	overTargetBytes.targetIndexBytes = lineageIndexMaximumBytes
	if overTargetBytes.valid() {
		t.Fatal("target bytes plus historical reservation overflow was accepted")
	}
}

func TestTypedRuntimeArtifactInclusiveMaximumAndPlusOne(t *testing.T) {
	owner := &evidenceOwnerToken{}
	runtimeBytes := make([]byte, maxRuntimeTarSize)
	facts := quotaFactsForTest([]uint64{1}, 1)
	facts.outerArtifactDigest = DigestBytes(runtimeBytes)
	facts.outerArtifactSize = uint64(len(runtimeBytes))
	facts.canonical = quotaBundleFactsDigest(facts.lineageQuotaProfile, facts.schemaBundleDigest, facts.maxAttempts, facts.statementCounts, facts.runtimeInputs, facts.outerArtifactDigest, facts.outerArtifactSize)
	if _, err := quotaRuntimeObject(facts, VerifiedRuntimeArtifact{owner: owner, bytes: runtimeBytes, digest: facts.outerArtifactDigest, sizeBytes: facts.outerArtifactSize}); err != nil {
		t.Fatalf("exact 64 MiB runtime rejected: %v", err)
	}
	runtimeBytes = append(runtimeBytes, 0)
	facts.outerArtifactDigest = DigestBytes(runtimeBytes)
	facts.outerArtifactSize = uint64(len(runtimeBytes))
	facts.canonical = quotaBundleFactsDigest(facts.lineageQuotaProfile, facts.schemaBundleDigest, facts.maxAttempts, facts.statementCounts, facts.runtimeInputs, facts.outerArtifactDigest, facts.outerArtifactSize)
	if _, err := quotaRuntimeObject(facts, VerifiedRuntimeArtifact{owner: owner, bytes: runtimeBytes, digest: facts.outerArtifactDigest, sizeBytes: facts.outerArtifactSize}); !IsCode(err, CodeUntrusted) {
		t.Fatalf("64 MiB + 1 runtime admitted: %v", err)
	}
}
