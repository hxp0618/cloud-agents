package migration

import (
	"crypto/sha256"
	"encoding/base64"
	"math"
	"sync"
	"testing"
)

func quotaFactsForTest(counts []uint64, attempts uint64) verifiedQuotaBundleFacts {
	runtimeInputs := sha256.Sum256([]byte("owned-runtime-inputs"))
	outer := []byte("owned-runtime-artifact")
	facts := verifiedQuotaBundleFacts{schemaBundleDigest: DigestBytes([]byte("quota-schema")), maxAttempts: attempts, statementCounts: append([]uint64(nil), counts...), runtimeInputs: runtimeInputs, outerArtifactDigest: DigestBytes(outer), outerArtifactSize: uint64(len(outer))}
	facts.canonical = quotaBundleFactsDigest(facts.schemaBundleDigest, facts.maxAttempts, facts.statementCounts, facts.runtimeInputs, facts.outerArtifactDigest, facts.outerArtifactSize)
	return facts
}

func rootFactsForTest(t *testing.T, objects []rootQuotaObjectFact) *verifiedRootQuotaState {
	t.Helper()
	facts := rootQuotaUsageFacts{finalObjects: canonicalRootObjects(objects)}
	for _, object := range objects {
		facts.finalObjectBytes += object.size
	}
	fs := newFakeEvidenceFS()
	lock := &evidenceLineageLock{
		root: testEvidenceLockFile(fs, 101, 101, evidenceRootLockKind), lineage: testEvidenceLockFile(fs, 102, 102, evidenceLineageLockKind), rootHeld: true, lineageHeld: true,
	}
	if facts.exceedsLimits() || !facts.valid() {
		t.Fatal("invalid test-only root quota facts")
	}
	owned := facts
	owned.finalObjects = append([]rootQuotaObjectFact(nil), facts.finalObjects...)
	return &verifiedRootQuotaState{facts: owned, canonical: rootQuotaFactsDigest(owned), lock: lock}
}

func TestProductionRootQuotaStateBinderFailsClosed(t *testing.T) {
	fs := newFakeEvidenceFS()
	lock := &evidenceLineageLock{
		root: testEvidenceLockFile(fs, 201, 201, evidenceRootLockKind), lineage: testEvidenceLockFile(fs, 202, 202, evidenceLineageLockKind), rootHeld: true, lineageHeld: true,
	}
	if state, err := bindVerifiedRootQuotaState(lock, verifiedRootQuotaScan{}); state != nil || !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("production root quota state binder did not fail closed: state=%+v err=%v", state, err)
	}
}

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
	got, err := calculateEvidenceQuotaReservation(facts, brandNew)
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
	existingState.facts.targetIndexPresent = true
	existingState.facts.targetIndexRecords = 1
	existingState.facts.targetIndexBytes = lineageRecordFrameLimits[LineageRecordHeader]
	existingState.canonical = rootQuotaFactsDigest(existingState.facts)
	existing, err := calculateEvidenceQuotaReservation(facts, existingState)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReservedIndexRecords != existing.ReservedIndexRecords+1 || got.ReservedIndexBytes != existing.ReservedIndexBytes+lineageRecordFrameLimits[LineageRecordHeader] {
		t.Fatalf("brand-new delta got=%+v existing=%+v", got, existing)
	}
}

func TestEvidenceQuotaRotationAndSegmentLimit(t *testing.T) {
	// One statement/attempt consumes five caller records. Find a bounded shape
	// that rotates and assert first-fit state exactly.
	got, err := calculateEvidenceQuotaReservation(quotaFactsForTest([]uint64{52}, 1), rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.ReservedSegments <= 1 {
		t.Fatalf("expected rotation: %+v", got)
	}
	if got.ReservedRecords != 1+52*2+3+uint64(got.ReservedSegments-1) {
		t.Fatalf("rotation headers missing: %+v", got)
	}

	if _, err := calculateEvidenceQuotaReservation(quotaFactsForTest([]uint64{4096}, 16), rootFactsForTest(t, nil)); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("segment 17/excess was admitted: %v", err)
	}
}

func TestEvidenceQuotaFactsDetectAliasAndOverflow(t *testing.T) {
	facts := quotaFactsForTest([]uint64{1}, 1)
	facts.statementCounts[0] = 2
	if _, err := calculateEvidenceQuotaReservation(facts, rootFactsForTest(t, nil)); !IsCode(err, CodeUntrusted) {
		t.Fatalf("aliased facts: %v", err)
	}
	facts = quotaFactsForTest([]uint64{math.MaxUint64}, math.MaxUint64)
	if _, err := calculateEvidenceQuotaReservation(facts, rootFactsForTest(t, nil)); !IsCode(err, CodeEvidenceJournalLimitExceeded) {
		t.Fatalf("overflow: %v", err)
	}
}

func TestCheckedInBundleQuotaReservationExact(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	bundle, err := LoadRuntimeBundle(raw, testTrustDecision(raw, manifest))
	if err != nil {
		t.Fatal(err)
	}
	if !bundle.quotaFacts.valid() || bundle.quotaFacts.maxAttempts != 3 || len(bundle.quotaFacts.statementCounts) != 2 || bundle.quotaFacts.statementCounts[0] != 20 || bundle.quotaFacts.statementCounts[1] != 71 {
		t.Fatalf("unexpected current facts: %+v", bundle.quotaFacts)
	}
	reservation, err := calculateEvidenceQuotaReservation(bundle.quotaFacts, rootFactsForTest(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if reservation.ReservedSegments != 6 || reservation.ReservedRecords != 570 || reservation.ReservedCheckpointRecords != 569 || reservation.ReservedJournalBytes != 90832896 || reservation.ReservedIndexRecords != 573 || reservation.ReservedIndexBytes != 9617408 || reservation.ReservedBytes != 100450304 {
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
	got, err := admitRootQuota(facts, bundle, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.finalObjectCount != 2 || got.finalObjectBytes != runtime.sizeBytes+candidate.decisionRecoveryArtifact.sizeBytes {
		t.Fatalf("dedupe=%+v", got)
	}
	if _, err := admitRootQuota(facts, bundle, candidate); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("one-shot reused: %v", err)
	}

	facts = rootFactsForTest(t, nil)
	tinyCandidate := candidate
	tinyCandidate.runtimeArtifact = VerifiedRuntimeArtifact{owner: runtime.owner, bytes: []byte("tiny"), digest: DigestBytes([]byte("tiny")), sizeBytes: 4}
	if _, err := admitRootQuota(facts, bundle, tinyCandidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("runtime tiny swap: %v", err)
	}
	facts = rootFactsForTest(t, nil)
	candidate = quotaCandidateForBundle(t, bundle, make([]byte, maxDecisionRecoveryArtifactBytes+1))
	if _, err := admitRootQuota(facts, bundle, candidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery +1: %v", err)
	}
	facts = rootFactsForTest(t, nil)
	candidate = quotaCandidateForBundle(t, bundle, []byte("recovery"))
	candidate.decisionRecoveryArtifact.owner = &recoveryVerifierOwner{token: &evidenceOwnerToken{}}
	if _, err := admitRootQuota(facts, bundle, candidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery owner swap: %v", err)
	}
	facts = rootFactsForTest(t, nil)
	candidate = quotaCandidateForBundle(t, bundle, []byte("recovery"))
	candidate.decisionRecoveryArtifact.decision = DigestBytes([]byte("other-decision"))
	if _, err := admitRootQuota(facts, bundle, candidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
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
		func(v *rootQuotaUsageFacts) { v.targetIndexRecords++ },
		func(v *rootQuotaUsageFacts) { v.targetIndexBytes++ },
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

func TestTypedRuntimeArtifactInclusiveMaximumAndPlusOne(t *testing.T) {
	owner := &evidenceOwnerToken{}
	runtimeBytes := make([]byte, maxRuntimeTarSize)
	facts := quotaFactsForTest([]uint64{1}, 1)
	facts.outerArtifactDigest = DigestBytes(runtimeBytes)
	facts.outerArtifactSize = uint64(len(runtimeBytes))
	facts.canonical = quotaBundleFactsDigest(facts.schemaBundleDigest, facts.maxAttempts, facts.statementCounts, facts.runtimeInputs, facts.outerArtifactDigest, facts.outerArtifactSize)
	if _, err := quotaRuntimeObject(facts, VerifiedRuntimeArtifact{owner: owner, bytes: runtimeBytes, digest: facts.outerArtifactDigest, sizeBytes: facts.outerArtifactSize}); err != nil {
		t.Fatalf("exact 64 MiB runtime rejected: %v", err)
	}
	runtimeBytes = append(runtimeBytes, 0)
	facts.outerArtifactDigest = DigestBytes(runtimeBytes)
	facts.outerArtifactSize = uint64(len(runtimeBytes))
	facts.canonical = quotaBundleFactsDigest(facts.schemaBundleDigest, facts.maxAttempts, facts.statementCounts, facts.runtimeInputs, facts.outerArtifactDigest, facts.outerArtifactSize)
	if _, err := quotaRuntimeObject(facts, VerifiedRuntimeArtifact{owner: owner, bytes: runtimeBytes, digest: facts.outerArtifactDigest, sizeBytes: facts.outerArtifactSize}); !IsCode(err, CodeUntrusted) {
		t.Fatalf("64 MiB + 1 runtime admitted: %v", err)
	}
}

func TestRootQuotaAdmissionCASAllowsOneWinner(t *testing.T) {
	state := rootFactsForTest(t, nil)
	bundle := quotaAdmissionBundleForTest(t)
	candidate := quotaCandidateForBundle(t, bundle, []byte("recovery"))
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := admitRootQuota(state, bundle, candidate)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !IsCode(err, CodeEvidenceJournalFailed) {
			t.Fatalf("unexpected concurrent admission error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent admissions succeeded %d times", successes)
	}
}
