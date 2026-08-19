package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

// GenerationRecoveryReady is the same-verifier recovery result for one
// strictly replayed brand-new generation. It owns an internal cursor and
// RecoverySnapshot, but deliberately does not implement EvidenceJournal and
// exposes neither cursor nor append authority to callers.
type GenerationRecoveryReady struct {
	self             *GenerationRecoveryReady
	prior            *GenerationReplayReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	generation       generationIdentity
	cursor           JournalCursor
	recovery         *RecoverySnapshot
	factsDigest      [32]byte
	binding          *generationRecoveryReadyBinding
	consumed         *atomic.Bool
}

type generationRecoveryReadyBinding struct {
	ready            *GenerationRecoveryReady
	prior            *GenerationReplayReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	canonical        [32]byte
}

type generationRecoveryReadyRegistryRecord struct {
	ready            *GenerationRecoveryReady
	binding          *generationRecoveryReadyBinding
	prior            *GenerationReplayReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	cursorValid      *atomic.Bool
	canonical        [32]byte
}

var generationRecoveryReadyRegistry sync.Map

// BindRecovery consumes strict replay authority and reconstructs all
// brand-new recovery inputs from the same OwnedCurrentCandidate. No database,
// runner, append, or public JournalCursor authority is minted here.
func (r *GenerationReplayReady) BindRecovery(ctx context.Context, candidate OwnedCurrentCandidate) (*GenerationRecoveryReady, error) {
	if r == nil || !validGenerationReplayReady(r, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-recovery", "strict replay authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-recovery", "strict replay authority is consumed", nil)
	}
	headerRaw, err := r.snapshot.ReadSegment(ctx, 0)
	if err != nil {
		return r.failRecovery(err, "generation-recovery-header")
	}
	frames, err := decodeGenerationRecoveryFrames(headerRaw)
	if err != nil {
		return r.failRecovery(err, "generation-recovery-header")
	}
	if len(frames) != 1 || frames[0].RecordKind != EvidenceRecordHeader || frames[0].Record.Header == nil || frames[0].RecordDigest != r.headerDigest {
		return r.failRecovery(admissionCorrupt("generation-recovery-header", "strict replay is not brand-new header-only", nil), "generation-recovery-header")
	}
	if !validGenerationReplayReceiptAuthority(r, candidate, *frames[0].Record.Header) {
		return r.failRecovery(fail(CodeEvidenceRecoveryRequired, "generation-recovery-receipts", "typed publication receipts are unavailable", nil), "generation-recovery-receipts")
	}
	bindings, err := candidate.verifiedRun.currentDecision.decision.runnerProjectionBindings()
	if err != nil {
		return r.failRecovery(fail(CodeEvidenceRecoveryRequired, "generation-recovery-bindings", "current verifier bindings are unavailable", nil), "generation-recovery-bindings")
	}
	if r.plan == nil || r.plan.reservedFrame.Record.Reserved == nil {
		return r.failRecovery(admissionCorrupt("generation-recovery-plan", "reserved generation plan is unavailable", nil), "generation-recovery-plan")
	}
	facts := cloneAdmissionHistoricalVerificationFacts(r.history.currentFacts)
	if !validAdmissionRecoveryFacts(facts) || facts.lineageQuotaProfile != frames[0].Record.Header.LimitsProfile || facts.runnerProjectionDecisionDigest != bindings.runnerProjectionDecisionDigest || facts.schemaBundleDigest != bindings.schemaBundleDigest || facts.manifestDigest != candidate.verifiedRun.manifestDigest || facts.authorityProfileDigest != bindings.authorityProfileDigest || facts.authorityBindingDigest != bindings.authorityBindingDigest {
		return r.failRecovery(fail(CodeEvidenceRecoveryRequired, "generation-recovery-facts", "admission-history verifier facts are unavailable", nil), "generation-recovery-facts")
	}
	generation := generationIdentity{candidate.owner, r.plan.reservedFrame.Record.Reserved.ExecutionLineageDigest, r.journal, bindings.runnerProjectionDecisionDigest, bindings.schemaBundleDigest}
	if !sameGenerationHeader(generation, *frames[0].Record.Header) || generation.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest || generation.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest {
		return r.failRecovery(admissionCorrupt("generation-recovery-header", "header differs from same-verifier generation", nil), "generation-recovery-header")
	}
	witness, schema, err := buildBrandNewRecoveryWitness(candidate, generation, facts)
	if err != nil {
		return r.failRecovery(err, "generation-recovery-witness")
	}
	if err := validateEvidenceChainWithWitness(frames, witness); err != nil {
		return r.failRecovery(admissionCorrupt("generation-recovery-witness", "header receipt or verifier witness differs", err), "generation-recovery-witness")
	}
	previous := r.journalTail
	cursor := JournalCursor{
		owner: candidate.owner, generation: generation, segmentIndex: r.segmentCount - 1,
		nextSequence: r.journalRecords, previousRecordDigest: &previous,
		lineageIndexNextSequence: r.indexRecords, lineageIndexPreviousRecordDigest: r.activationDigest,
		valid: &atomic.Bool{},
	}
	cursor.valid.Store(true)
	recovery, err := buildRecoverySnapshot(frames, cursor, generation, recoveredContinuation{}, schema)
	if err != nil || recovery == nil || recovery.State() != RecoveryBrandNew || recovery.NextAction() != RecoveryBeginFirstAttempt || recovery.TailDigest() != r.journalTail {
		cursor.valid.Store(false)
		return r.failRecovery(fail(CodeEvidenceRecoveryRequired, "generation-recovery-snapshot", "brand-new recovery snapshot cannot be bound", nil), "generation-recovery-snapshot")
	}
	if err := r.snapshot.Revalidate(ctx); err != nil {
		cursor.valid.Store(false)
		return r.failRecovery(err, "generation-recovery-terminal")
	}
	ready := &GenerationRecoveryReady{
		prior: r, plan: r.plan, history: r.history, candidateBinding: candidate.binding,
		generation: generation, cursor: cursor, recovery: recovery, factsDigest: admissionRecoveryFactsDigest(facts), consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &generationRecoveryReadyBinding{ready: ready, prior: r, plan: r.plan, history: r.history, candidateBinding: candidate.binding}
	ready.binding.canonical = generationRecoveryReadyDigest(ready)
	generationRecoveryReadyRegistry.Store(ready, generationRecoveryReadyRegistryRecord{
		ready: ready, binding: ready.binding, prior: r, plan: r.plan, history: r.history,
		candidateBinding: candidate.binding, cursorValid: cursor.valid, canonical: ready.binding.canonical,
	})
	if !validGenerationRecoveryReady(ready, candidate) {
		generationRecoveryReadyRegistry.Delete(ready)
		cursor.valid.Store(false)
		if cleanupErr := closeRegisteredGenerationReplay(r, "generation-recovery-seal-cleanup"); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, admissionFailed("generation-recovery-seal", "recovery-ready authority could not be sealed", nil)
	}
	return ready, nil
}

func buildBrandNewRecoveryWitness(candidate OwnedCurrentCandidate, generation generationIdentity, facts *admissionHistoricalVerificationFacts) (verifiedEvidenceChainWitness, verifiedRecoverySchemaWitness, error) {
	if !validOwnedCurrentCandidate(candidate) || !validAdmissionRecoveryFacts(facts) || generation.owner != candidate.owner || generation.executionLineageDigest != candidate.verifiedRun.executionLineageDigest || generation.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest || generation.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest || facts.manifestDigest != candidate.verifiedRun.manifestDigest || facts.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest || facts.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest || facts.authorityProfileDigest != candidate.verifiedRun.authorityProfileDigest || facts.authorityBindingDigest != candidate.verifiedRun.authorityBindingDigest {
		return verifiedEvidenceChainWitness{}, verifiedRecoverySchemaWitness{}, fail(CodeEvidenceRecoveryRequired, "generation-recovery-witness", "same-verifier facts are incomplete", nil)
	}
	return buildBrandNewRecoveryWitnessFromFacts(
		generation,
		facts,
		ownedContentReceiptWitness{"runtime", candidate.verifiedRun.outerArtifactDigest, candidate.verifiedRun.outerArtifactSizeBytes},
		ownedContentReceiptWitness{"decision_recovery", candidate.verifiedRun.decisionRecoveryArtifactSHA256, candidate.verifiedRun.decisionRecoveryArtifactSizeBytes},
	)
}

func buildBrandNewRecoveryWitnessFromFacts(generation generationIdentity, facts *admissionHistoricalVerificationFacts, runtimeReceipt, recoveryReceipt ownedContentReceiptWitness) (verifiedEvidenceChainWitness, verifiedRecoverySchemaWitness, error) {
	if generation.owner == nil || generation.executionLineageDigest.Validate() != nil || generation.journalIdentityDigest.Validate() != nil || generation.runnerProjectionDecisionDigest.Validate() != nil || generation.schemaBundleDigest.Validate() != nil || !validAdmissionRecoveryFacts(facts) || facts.runnerProjectionDecisionDigest != generation.runnerProjectionDecisionDigest || facts.schemaBundleDigest != generation.schemaBundleDigest || runtimeReceipt.kind != "runtime" || runtimeReceipt.digest.Validate() != nil || runtimeReceipt.sizeBytes == 0 || runtimeReceipt.sizeBytes > maxRuntimeTarSize || recoveryReceipt.kind != "decision_recovery" || recoveryReceipt.digest.Validate() != nil || recoveryReceipt.sizeBytes == 0 || recoveryReceipt.sizeBytes > maxDecisionRecoveryArtifactBytes {
		return verifiedEvidenceChainWitness{}, verifiedRecoverySchemaWitness{}, fail(CodeEvidenceRecoveryRequired, "generation-recovery-witness", "same-verifier facts or receipt facts are incomplete", nil)
	}
	chain := verifiedEvidenceChainWitness{
		maxAttempts: map[string]uint32{}, finalStatementIndex: map[string]uint32{}, finalCatalogDigest: map[string]Digest{},
		plans: map[string]exactStatementEvidenceWitness{}, retryReceipts: map[Digest]verifiedRetryReceipt{}, ambiguousBoundaries: map[Digest]ownedAmbiguousBoundaryWitness{},
		runtimeReceipt: runtimeReceipt, recoveryReceipt: recoveryReceipt,
	}
	for _, migration := range facts.orderedMigrations {
		subjects := facts.statementSubjects[migration]
		if len(subjects) == 0 || facts.finalCatalogDigest[migration] == ([32]byte{}) {
			return verifiedEvidenceChainWitness{}, verifiedRecoverySchemaWitness{}, fail(CodeEvidenceRecoveryRequired, "generation-recovery-witness", "same-verifier statement facts are incomplete", nil)
		}
		chain.maxAttempts[migration] = facts.maxAttempts
		chain.finalStatementIndex[migration] = uint32(len(subjects) - 1)
		chain.finalCatalogDigest[migration] = digestString(facts.finalCatalogDigest[migration])
	}
	signed := cloneProjectionValue(facts.ledgerRows)
	signedDigest, err := LedgerPrefixDigest(signed)
	if err != nil {
		return verifiedEvidenceChainWitness{}, verifiedRecoverySchemaWitness{}, err
	}
	emptyPrefix := []CommitIntentLedgerRow{}
	emptyDigest, err := LedgerPrefixDigest(emptyPrefix)
	if err != nil {
		return verifiedEvidenceChainWitness{}, verifiedRecoverySchemaWitness{}, err
	}
	schema := verifiedRecoverySchemaWitness{
		owner: generation.owner, generation: generation, finalStatementIndex: cloneUint32Map(chain.finalStatementIndex),
		maxAttempts: cloneUint32Map(chain.maxAttempts), orderedMigrations: append([]string(nil), facts.orderedMigrations...),
		signedExpectedLedgerRows: signed, signedExpectedLedgerDigest: signedDigest,
		durableObservedLedgerPrefix: emptyPrefix, durableObservedLedgerDigest: emptyDigest,
		finalCatalogDigest: chain.finalCatalogDigest[facts.orderedMigrations[len(facts.orderedMigrations)-1]], chainWitness: chain,
	}
	return chain, schema, nil
}

func buildRegisteredBrandNewRecoveryWitness(planned *verifiedAdmissionRegisteredGeneration, current OwnedVerifiedDecision, header JournalHeader) (*admissionHistoricalVerificationFacts, verifiedEvidenceChainWitness, verifiedRecoverySchemaWitness, error) {
	if planned == nil || header.Validate() != nil || !validVerifiedAdmissionRegisteredGeneration(planned, current) || !canonicalEqual(header, planned.descriptor.header) {
		return nil, verifiedEvidenceChainWitness{}, verifiedRecoverySchemaWitness{}, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-facts", "registered successor facts are unavailable", nil)
	}
	facts, err := buildHistoricalVerificationFacts(planned.bundle, planned.bindings)
	if err != nil || !validAdmissionRecoveryFacts(facts) || facts.manifestDigest != header.ManifestDigest || facts.lineageQuotaProfile != header.LimitsProfile || facts.runnerProjectionDecisionDigest != header.RunnerProjectionDecisionDigest || facts.schemaBundleDigest != header.SchemaBundleDigest || facts.authorityProfileDigest != header.AuthorityProfileDigest || facts.authorityBindingDigest != header.AuthorityBindingDigest {
		return nil, verifiedEvidenceChainWitness{}, verifiedRecoverySchemaWitness{}, fail(CodeEvidenceRecoveryRequired, "historical-successor-recovery-facts", "registered successor facts differ from the durable header", nil)
	}
	chain, schema, err := buildBrandNewRecoveryWitnessFromFacts(
		planned.descriptor.identity,
		facts,
		ownedContentReceiptWitness{"runtime", planned.runtimeReceipt.digest, planned.runtimeReceipt.sizeBytes},
		ownedContentReceiptWitness{"decision_recovery", planned.recoveryReceipt.digest, planned.recoveryReceipt.sizeBytes},
	)
	if err != nil {
		return nil, verifiedEvidenceChainWitness{}, verifiedRecoverySchemaWitness{}, err
	}
	return facts, chain, schema, nil
}

func validGenerationReplayReceiptAuthority(replay *GenerationReplayReady, candidate OwnedCurrentCandidate, header JournalHeader) bool {
	_, _, ok := generationReplayReceipts(replay, candidate, header)
	return ok
}

// generationReplayReceipts is the only bridge from the consumed admission
// chain into normal-run receipt ownership. The handoff registry, rather than
// mutable predecessor fields alone, proves that both exact receipt bindings
// crossed the filesystem handoff with this replay lease.
func generationReplayReceipts(replay *GenerationReplayReady, candidate OwnedCurrentCandidate, header JournalHeader) (VerifiedContentReceipt, VerifiedDecisionRecoveryReceipt, bool) {
	if replay == nil || replay.prior == nil || replay.prior.prior == nil || replay.plan == nil || replay.plan.reservedFrame.Record.Reserved == nil || !validOwnedCurrentCandidate(candidate) || header.Validate() != nil || !canonicalEqual(header, replay.plan.reservedFrame.Record.Reserved.PlannedSegment0Header) {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	registered, ok := generationHandoffReadyRegistry.Load(replay.prior)
	record, recordOK := registered.(generationHandoffReadyRegistryRecord)
	if !ok || !recordOK || record.ready != replay.prior || record.binding != replay.prior.binding || record.prior != replay.prior.prior || record.candidateBinding != candidate.binding || record.lease != replay.lease || record.canonical == ([32]byte{}) || record.canonical != replay.prior.binding.canonical || record.runtimeReceipt.binding == nil || record.recoveryReceipt.binding == nil || record.runtimeReceipt.binding != record.prior.runtimeReceipt.binding || record.recoveryReceipt.binding != record.prior.recoveryReceipt.binding {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	if !validRuntimeReceipt(record.runtimeReceipt, candidate.owner, header.OuterArtifactDigest, header.OuterArtifactSizeBytes) ||
		!validDecisionRecoveryReceipt(record.recoveryReceipt, candidate.owner, header.DecisionRecoveryArtifactSHA256, header.DecisionRecoveryArtifactSizeBytes) ||
		!record.runtimeReceipt.publication.SameStore(record.recoveryReceipt.publication) {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, false
	}
	return record.runtimeReceipt, record.recoveryReceipt, true
}

func cloneUint32Map(input map[string]uint32) map[string]uint32 {
	result := make(map[string]uint32, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func decodeGenerationRecoveryFrames(raw []byte) ([]EvidenceFrame, error) {
	var frames []EvidenceFrame
	err := decodeAdmissionFramedBytes(raw, 16<<20, 4096, maxEvidenceFrameBytes, func(framed []byte) error {
		frame, err := DecodeCanonicalEvidenceFrame(framed)
		if err != nil {
			return err
		}
		frames = append(frames, cloneProjectionValue(*frame))
		return nil
	})
	if err != nil {
		return nil, admissionCorrupt("generation-recovery-decode", "stored generation frames are invalid", err)
	}
	return frames, nil
}

func admissionRecoveryFactsDigest(facts *admissionHistoricalVerificationFacts) [32]byte {
	if !validAdmissionRecoveryFacts(facts) {
		return [32]byte{}
	}
	h := sha256.New()
	if facts.lineageQuotaProfile == LineageQuotaProfileV2 {
		h.Write([]byte("cloud-agents-platform-generation-recovery-facts/v2\x00"))
		writeAdmissionString(h, facts.lineageQuotaProfile)
	} else if facts.lineageQuotaProfile == LineageQuotaProfileV3 {
		h.Write([]byte("cloud-agents-platform-generation-recovery-facts/v3\x00"))
		writeAdmissionString(h, facts.lineageQuotaProfile)
	} else {
		// Historical v1 facts retain their exact digest subject.
		h.Write([]byte("cloud-agents-platform-generation-recovery-facts/v1\x00"))
	}
	writeAdmissionUint(h, uint64(facts.maxAttempts))
	for _, value := range []Digest{facts.manifestDigest, facts.runnerProjectionDecisionDigest, facts.schemaBundleDigest, facts.authorityProfileDigest, facts.authorityBindingDigest} {
		writeAdmissionString(h, value.String())
	}
	for index, migration := range facts.orderedMigrations {
		writeAdmissionString(h, migration)
		writeAdmissionUint(h, uint64(len(facts.statementSubjects[migration])))
		for _, subject := range facts.statementSubjects[migration] {
			h.Write(subject[:])
		}
		for _, value := range [][32]byte{facts.finalCatalogDigest[migration], facts.catalogContractDigest[migration], facts.attemptPredecessorCatalog[migration]} {
			h.Write(value[:])
		}
		canonical, err := canonicalContractKey(facts.ledgerRows[index])
		if err != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validAdmissionRecoveryFacts(facts *admissionHistoricalVerificationFacts) bool {
	if facts == nil || !validEvidenceLimitsProfile(facts.lineageQuotaProfile) || facts.maxAttempts == 0 || len(facts.orderedMigrations) == 0 || len(facts.ledgerRows) != len(facts.orderedMigrations) || len(facts.statementSubjects) != len(facts.orderedMigrations) || len(facts.finalCatalogDigest) != len(facts.orderedMigrations) || len(facts.catalogContractDigest) != len(facts.orderedMigrations) || len(facts.attemptPredecessorCatalog) != len(facts.orderedMigrations) {
		return false
	}
	for _, value := range []Digest{facts.manifestDigest, facts.runnerProjectionDecisionDigest, facts.schemaBundleDigest, facts.authorityProfileDigest, facts.authorityBindingDigest} {
		if value.Validate() != nil {
			return false
		}
	}
	seen := make(map[string]bool, len(facts.orderedMigrations))
	for index, migration := range facts.orderedMigrations {
		subjects, subjectsOK := facts.statementSubjects[migration]
		final, finalOK := facts.finalCatalogDigest[migration]
		catalog, catalogOK := facts.catalogContractDigest[migration]
		predecessor, predecessorOK := facts.attemptPredecessorCatalog[migration]
		if !migrationIDPattern.MatchString(migration) || seen[migration] || !subjectsOK || len(subjects) == 0 || !finalOK || !catalogOK || !predecessorOK || final == ([32]byte{}) || catalog == ([32]byte{}) || predecessor == ([32]byte{}) || facts.ledgerRows[index].MigrationID != migration || facts.ledgerRows[index].BundleDigest != facts.schemaBundleDigest || facts.ledgerRows[index].Validate() != nil {
			return false
		}
		seen[migration] = true
		for _, subject := range subjects {
			if subject == ([32]byte{}) {
				return false
			}
		}
	}
	return true
}

func generationRecoveryReadyDigest(ready *GenerationRecoveryReady) [32]byte {
	if ready == nil || ready.self != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding == nil || ready.prior.binding == nil || ready.plan.binding == nil || ready.history.binding == nil || ready.factsDigest == ([32]byte{}) || ready.factsDigest != admissionRecoveryFactsDigest(ready.history.currentFacts) || !validBrandNewRecoverySnapshot(ready.recovery, ready.generation, ready.cursor, ready.prior.journalTail) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-recovery-ready/v1\x00"))
	h.Write(ready.prior.binding.canonical[:])
	h.Write(ready.plan.binding.canonical[:])
	h.Write(ready.history.binding.canonical[:])
	h.Write(ready.candidateBinding.canonical[:])
	h.Write(ready.factsDigest[:])
	writeAdmissionString(h, ready.generation.executionLineageDigest.String())
	writeAdmissionString(h, ready.generation.journalIdentityDigest.String())
	writeAdmissionString(h, ready.generation.runnerProjectionDecisionDigest.String())
	writeAdmissionString(h, ready.generation.schemaBundleDigest.String())
	writeAdmissionUint(h, uint64(ready.cursor.segmentIndex))
	writeAdmissionUint(h, ready.cursor.nextSequence)
	writeAdmissionUint(h, ready.cursor.lineageIndexNextSequence)
	writeAdmissionString(h, ready.cursor.lineageIndexPreviousRecordDigest.String())
	if ready.cursor.previousRecordDigest == nil {
		return [32]byte{}
	}
	writeAdmissionString(h, ready.cursor.previousRecordDigest.String())
	writeAdmissionString(h, string(ready.recovery.State()))
	writeAdmissionString(h, string(ready.recovery.NextAction()))
	writeAdmissionString(h, ready.recovery.TailDigest().String())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validBrandNewRecoverySnapshot(snapshot *RecoverySnapshot, generation generationIdentity, cursor JournalCursor, tail Digest) bool {
	return snapshot != nil && cursor.Valid() && snapshot.cursor.Valid() && tail.Validate() == nil && snapshot.owner == generation.owner && sameGenerationIdentity(snapshot.generation, generation) && sameCursorIdentity(snapshot.cursor, cursor) && snapshot.tailDigest == tail && snapshot.state == RecoveryBrandNew && snapshot.nextPermittedAction == RecoveryBeginFirstAttempt && snapshot.migrationID == nil && snapshot.attemptIndex == nil && snapshot.lineageContinuation == nil && snapshot.lastStatementIntent == nil && snapshot.lastIntermediateEvidence == nil && snapshot.commitIntent == nil && snapshot.lastTerminal == nil && snapshot.lastResolution == nil && snapshot.lastTerminalDigest == nil && snapshot.lastResolutionDigest == nil && snapshot.lastStatementIntentRecordDigest == nil && snapshot.lastIntermediateEvidenceRecordDigest == nil && snapshot.lastCommitIntentRecordDigest == nil && snapshot.previousAttemptTerminalDigest == nil && snapshot.lastIntermediateStateDigest == nil
}

func validGenerationRecoveryReady(ready *GenerationRecoveryReady, candidate OwnedCurrentCandidate) bool {
	if ready == nil || ready.self != ready || ready.binding == nil || ready.binding.ready != ready || ready.prior == nil || ready.plan == nil || ready.history == nil || ready.candidateBinding != candidate.binding || ready.binding.prior != ready.prior || ready.binding.plan != ready.plan || ready.binding.history != ready.history || ready.binding.candidateBinding != ready.candidateBinding || ready.consumed == nil || ready.consumed.Load() || !validOwnedCurrentCandidate(candidate) || ready.prior.consumed == nil || !ready.prior.consumed.Load() || ready.plan != ready.prior.plan || ready.history != ready.prior.history || ready.candidateBinding != ready.prior.candidateBinding || !sameGenerationIdentity(ready.generation, ready.cursor.generation) || ready.generation.owner != candidate.owner || ready.generation.executionLineageDigest != candidate.verifiedRun.executionLineageDigest || ready.generation.journalIdentityDigest != ready.prior.journal || ready.generation.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest || ready.generation.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest || !ready.cursor.Valid() || ready.cursor.segmentIndex != ready.prior.segmentCount-1 || ready.cursor.nextSequence != ready.prior.journalRecords || ready.cursor.previousRecordDigest == nil || *ready.cursor.previousRecordDigest != ready.prior.journalTail || ready.cursor.lineageIndexNextSequence != ready.prior.indexRecords || ready.cursor.lineageIndexPreviousRecordDigest != ready.prior.activationDigest || ready.cursor.latestCheckpointRecordDigest != nil || !validBrandNewRecoverySnapshot(ready.recovery, ready.generation, ready.cursor, ready.prior.journalTail) || ready.factsDigest != admissionRecoveryFactsDigest(ready.history.currentFacts) || ready.plan.reservedFrame.Record.Reserved == nil || !validGenerationReplayReceiptAuthority(ready.prior, candidate, ready.plan.reservedFrame.Record.Reserved.PlannedSegment0Header) || ready.binding.canonical == ([32]byte{}) || ready.binding.canonical != generationRecoveryReadyDigest(ready) {
		return false
	}
	registered, ok := generationRecoveryReadyRegistry.Load(ready)
	record, recordOK := registered.(generationRecoveryReadyRegistryRecord)
	return ok && recordOK && record.ready == ready && record.binding == ready.binding && record.prior == ready.prior && record.plan == ready.plan && record.history == ready.history && record.candidateBinding == ready.candidateBinding && record.cursorValid == ready.cursor.valid && record.canonical == ready.binding.canonical
}

func (r *GenerationReplayReady) failRecovery(cause error, operation string) (*GenerationRecoveryReady, error) {
	if cleanupErr := closeRegisteredGenerationReplay(r, operation+"-cleanup"); cleanupErr != nil {
		return nil, cleanupErr
	}
	if IsCode(cause, CodeEvidenceJournalCorrupt) || IsCode(cause, CodeEvidenceJournalFailed) || IsCode(cause, CodeEvidenceRecoveryRequired) || IsCode(cause, CodeContextCanceled) || IsCode(cause, CodeDeadlineExceeded) {
		return nil, cause
	}
	return nil, mapEvidenceAdmissionError(cause, operation)
}

// Close invalidates the internal cursor and releases the exact retained
// filesystem locks through the immutable replay predecessor chain.
func (r *GenerationRecoveryReady) Close() error {
	if r == nil || r.self != r || r.consumed == nil || !r.consumed.CompareAndSwap(false, true) {
		return admissionFailed("generation-recovery-close", "generation recovery authority is unavailable", nil)
	}
	registered, ok := generationRecoveryReadyRegistry.Load(r)
	record, recordOK := registered.(generationRecoveryReadyRegistryRecord)
	if !ok || !recordOK || record.ready != r || record.prior == nil || record.cursorValid == nil || record.canonical == ([32]byte{}) {
		return admissionFailed("generation-recovery-close", "immutable generation replay authority is unavailable", nil)
	}
	generationRecoveryReadyRegistry.Delete(r)
	record.cursorValid.Store(false)
	return closeRegisteredGenerationReplay(record.prior, "generation-recovery-close")
}
