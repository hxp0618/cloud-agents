package migration

import "testing"

type signedStatementPlanFixture struct {
	MigrationID              string                      `json:"migration_id"`
	StatementIndex           uint32                      `json:"statement_index"`
	SQLArtifactSHA256        Digest                      `json:"sql_artifact_sha256"`
	SQLArtifactSizeBytes     uint64                      `json:"sql_artifact_size_bytes"`
	StartOffset              uint64                      `json:"start_offset"`
	EndOffset                uint64                      `json:"end_offset"`
	StatementSHA256          Digest                      `json:"statement_sha256"`
	Classification           SQLClassificationDescriptor `json:"classification"`
	ExpectedTransitionDigest Digest                      `json:"expected_transition_digest"`
}

type contentReceiptFixture struct {
	Kind      string `json:"kind"`
	SHA256    Digest `json:"sha256"`
	SizeBytes uint64 `json:"size_bytes"`
}

type recoveryPredecessorFixture struct {
	OrderedLedgerRows               []CommitIntentLedgerRow `json:"ordered_ledger_rows"`
	LedgerPrefixDigest              Digest                  `json:"ledger_prefix_digest"`
	AttemptPredecessorCatalogDigest Digest                  `json:"attempt_predecessor_catalog_digest"`
	ObservedCatalogDigest           Digest                  `json:"observed_catalog_digest"`
	AuthorityResultDigest           Digest                  `json:"authority_result_digest"`
}

type retryReceiptFixture struct {
	OracleKind                 string                     `json:"oracle_kind"`
	OldReceiptKind             string                     `json:"old_receipt_kind"`
	ProofKind                  string                     `json:"proof_kind"`
	MigrationID                string                     `json:"migration_id"`
	AttemptIndex               uint32                     `json:"attempt_index"`
	ExecutionLineageDigest     Digest                     `json:"execution_lineage_digest"`
	JournalIdentityDigest      Digest                     `json:"journal_identity_digest"`
	OldConnectionLifecycleID   string                     `json:"old_connection_lifecycle_id"`
	NewConnectionLifecycleID   string                     `json:"new_connection_lifecycle_id"`
	OldBeforeNew               bool                       `json:"old_before_new"`
	CommitCalled               bool                       `json:"commit_called"`
	RollbackSucceeded          *bool                      `json:"rollback_succeeded"`
	OldHandleIrrevocablyClosed bool                       `json:"old_handle_irrevocably_closed"`
	ReadyForQuery              *bool                      `json:"ready_for_query"`
	CommitRejectedReason       *string                    `json:"commit_rejected_reason"`
	CommitIntentRecordDigest   *Digest                    `json:"commit_intent_record_digest"`
	RecoveryPredecessor        recoveryPredecessorFixture `json:"recovery_predecessor"`
}

type ambiguousBoundaryFixture struct {
	OracleKind                    string `json:"oracle_kind"`
	MigrationID                   string `json:"migration_id"`
	AttemptIndex                  uint32 `json:"attempt_index"`
	CommitCalled                  bool   `json:"commit_called"`
	FinalIntermediateRecordDigest Digest `json:"final_intermediate_record_digest"`
	CommitIntentRecordDigest      Digest `json:"commit_intent_record_digest"`
}

func TestEvidenceChainsConsumePrivateTypedWitnesses(t *testing.T) {
	t.Parallel()
	base := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	baseFrames := decodeEvidenceFrames(t, base["frames"])
	context := fixtureObjectValue(t, base["validation_context"], "validation context")
	baseWitness := buildEvidenceWitness(t, baseFrames, context)
	if err := validateEvidenceChainWithWitness(baseFrames, baseWitness); err != nil {
		t.Fatalf("base evidence chain: %v", err)
	}

	retryFixture := fixtureObject(t, migrationFixturePath(t, "golden/evidence-retry-chains-v1.json"))
	retryChains, ok := retryFixture["chains"].([]JSONValue)
	if !ok || len(retryChains) != 4 {
		t.Fatal("retry fixture does not contain all four lifecycle chains")
	}
	for _, rawChain := range retryChains {
		chain := fixtureObjectValue(t, rawChain, "retry chain")
		name, _ := chain["name"].(string)
		t.Run(name, func(t *testing.T) {
			frames := decodeEvidenceFrames(t, chain["frames"])
			witness := buildEvidenceWitness(t, frames, context)
			terminal := terminalFrame(t, frames)
			witness.retryReceipts[terminal.Record.AttemptTerminal.TerminalDigest] = buildRetryReceiptWitness(t, chain["owned_retry_receipt_pair_oracle"])
			if err := validateEvidenceChainWithWitness(frames, witness); err != nil {
				t.Fatal(err)
			}
		})
	}

	ambiguousFixture := fixtureObject(t, migrationFixturePath(t, "golden/evidence-ambiguous-chain-v1.json"))
	ambiguousFrames := decodeEvidenceFrames(t, ambiguousFixture["frames"])
	ambiguousWitness := buildEvidenceWitness(t, ambiguousFrames, context)
	terminal := terminalFrame(t, ambiguousFrames)
	ambiguousWitness.ambiguousBoundaries[terminal.Record.AttemptTerminal.TerminalDigest] = buildAmbiguousBoundaryWitness(t, ambiguousFixture["owned_ambiguous_boundary_oracle"])
	if err := validateEvidenceChainWithWitness(ambiguousFrames, ambiguousWitness); err != nil {
		t.Fatalf("ambiguous evidence chain: %v", err)
	}
}

func TestRetryBinderRejectsCrossNewConnectionPredecessorSwap(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-retry-chains-v1.json"))
	chains := document["chains"].([]JSONValue)
	var first, second retryReceiptFixture
	decodeFixtureValue(t, fixtureObjectValue(t, chains[0], "first")["owned_retry_receipt_pair_oracle"], &first)
	decodeFixtureValue(t, fixtureObjectValue(t, chains[1], "second")["owned_retry_receipt_pair_oracle"], &second)
	identity := ownedRetryIdentity{first.MigrationID, first.AttemptIndex, first.ExecutionLineageDigest, first.JournalIdentityDigest}
	tokenA := &retryLifecycleOrderToken{verifierNonce: [16]byte{1}}
	tokenB := &retryLifecycleOrderToken{verifierNonce: [16]byte{2}}
	old := ownedRollbackReceipt{identity, first.OldConnectionLifecycleID, ownedLifecycleOrderAuthority{tokenA, 1}, true, true}
	r := second.RecoveryPredecessor
	swapped := ownedRecoveryPredecessorReceipt{identity, second.NewConnectionLifecycleID, ownedLifecycleOrderAuthority{tokenB, 2}, r.OrderedLedgerRows, r.LedgerPrefixDigest, r.AttemptPredecessorCatalogDigest, r.ObservedCatalogDigest, r.AuthorityResultDigest}
	if _, err := bindRollbackRetryReceipt(first.ProofKind, old, swapped); err == nil {
		t.Fatal("accepted recovery predecessor from another new connection authority")
	}
}

func TestEvidenceJournalMultiSegmentAndStatementIndexFSM(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	base := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	header0 := *base[0].Record.Header
	header0.ReservedSegments = 2
	header0.QuotaReservationDigest = projectionTestDigest
	base[0].Record.Header = &header0
	redigestEvidenceFrames(t, base)
	header1 := header0
	header1.SegmentIndex = 1
	header1.PreviousSegmentRecordDigest = digestPointer(base[len(base)-1].RecordDigest)
	segmentFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: uint64(len(base)), PreviousRecordDigest: digestPointer(base[len(base)-1].RecordDigest), RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &header1}}
	segmentFrame.RecordDigest, _ = segmentFrame.ComputeDigest()
	frames := append(base, segmentFrame)
	witness := buildEvidenceWitness(t, frames, context)
	if err := validateEvidenceChainWithWitness(frames, witness); err != nil {
		t.Fatalf("valid second segment: %v", err)
	}

	badSegment := append([]EvidenceFrame(nil), frames...)
	badHeader := *badSegment[len(badSegment)-1].Record.Header
	badHeader.SegmentIndex = 2
	badSegment[len(badSegment)-1].Record.Header = &badHeader
	badSegment[len(badSegment)-1].RecordDigest, _ = badSegment[len(badSegment)-1].ComputeDigest()
	if err := validateEvidenceChainWithWitness(badSegment, witness); err == nil {
		t.Fatal("accepted segment index gap")
	}

	gap := append([]EvidenceFrame(nil), base...)
	intent := *gap[1].Record.StatementIntent
	intent.StatementIndex = 2
	gap[1].Record.StatementIntent = &intent
	redigestEvidenceFrames(t, gap)
	if err := validateEvidenceChainWithWitness(gap, buildEvidenceWitness(t, gap, context)); err == nil {
		t.Fatal("accepted first statement index 2")
	}
}

func redigestEvidenceFrames(t *testing.T, frames []EvidenceFrame) {
	t.Helper()
	var previous *Digest
	for index := range frames {
		frames[index].Sequence = uint64(index)
		frames[index].PreviousRecordDigest = cloneDigestPointer(previous)
		digest, err := frames[index].ComputeDigest()
		if err != nil {
			t.Fatal(err)
		}
		frames[index].RecordDigest = digest
		previous = digestPointer(digest)
	}
}

func TestLineageChainConsumesCheckpointTailAndAuthorityWitness(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	journalHeader := decodeOneEvidenceFrame(t, fixture["journal_header_frame"])
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])
	authorityObject := fixtureObjectValue(t, fixture["supersession_authority_oracle"], "supersession authority")
	if domain, _ := authorityObject["domain"].(string); domain != lineageSupersessionAuthorityDomain {
		t.Fatal("supersession authority domain")
	}
	authorityBody := make(map[string]JSONValue, len(authorityObject)-1)
	for key, value := range authorityObject {
		if key != "domain" {
			authorityBody[key] = value
		}
	}
	var authority lineageSupersessionAuthoritySubject
	decodeFixtureValue(t, authorityBody, &authority)
	authorityDigest, err := authority.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	witness := verifiedLineageChainWitness{
		header:         *frames[0].Record.Header,
		actualSegment0: map[Digest]EvidenceFrame{journalHeader.Record.Header.JournalIdentityDigest: journalHeader},
		journals:       map[Digest][]EvidenceFrame{journalHeader.Record.Header.JournalIdentityDigest: journal},
		historicalRecovery: verifiedHistoricalRecoveryChain{authorities: map[Digest]lineageSupersessionAuthoritySubject{
			authorityDigest: authority,
		}},
	}
	if err := validateLineageChainWithWitness(frames, witness); err != nil {
		t.Fatal(err)
	}

	mutated := append([]LineageIndexFrame(nil), frames...)
	checkpoint := *mutated[3].Record.Checkpoint
	checkpoint.RecoveryState = "dangling_commit_intent"
	mutated[3].Record.Checkpoint = &checkpoint
	mutated[3].RecordDigest, _ = mutated[3].ComputeDigest()
	if err := validateLineageChainWithWitness(mutated, witness); err == nil {
		t.Fatal("accepted checkpoint summary fault")
	}
}

func buildEvidenceWitness(t *testing.T, frames []EvidenceFrame, context map[string]JSONValue) verifiedEvidenceChainWitness {
	t.Helper()
	witness := verifiedEvidenceChainWitness{
		maxAttempts: map[string]uint32{}, finalStatementIndex: map[string]uint32{}, finalCatalogDigest: map[string]Digest{},
		plans: map[string]exactStatementEvidenceWitness{}, retryReceipts: map[Digest]verifiedRetryReceipt{}, ambiguousBoundaries: map[Digest]ownedAmbiguousBoundaryWitness{},
	}
	decodeUint32Map(t, context["max_attempts_by_migration"], witness.maxAttempts)
	decodeUint32Map(t, context["final_statement_index_by_migration"], witness.finalStatementIndex)
	decodeDigestMap(t, context["final_catalog_digest_by_migration"], witness.finalCatalogDigest)
	var runtime, recovery contentReceiptFixture
	decodeFixtureValue(t, context["owned_runtime_receipt_oracle"], &runtime)
	decodeFixtureValue(t, context["owned_decision_recovery_receipt_oracle"], &recovery)
	witness.runtimeReceipt = ownedContentReceiptWitness{runtime.Kind, runtime.SHA256, runtime.SizeBytes}
	witness.recoveryReceipt = ownedContentReceiptWitness{recovery.Kind, recovery.SHA256, recovery.SizeBytes}
	plans, ok := context["signed_statement_plans"].([]JSONValue)
	if !ok {
		t.Fatal("signed plans")
	}
	for _, rawPlan := range plans {
		var plan signedStatementPlanFixture
		decodeFixtureValue(t, rawPlan, &plan)
		classification, err := canonicalContractKey(plan.Classification)
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			if frame.Record.StatementIntent == nil || frame.Record.StatementIntent.MigrationID != plan.MigrationID || frame.Record.StatementIntent.StatementIndex != plan.StatementIndex {
				continue
			}
			attempt := frame.Record.StatementIntent.AttemptIndex
			witness.plans[evidenceStatementKey(plan.MigrationID, attempt, plan.StatementIndex)] = exactStatementEvidenceWitness{
				plan.MigrationID, attempt, plan.StatementIndex, plan.SQLArtifactSHA256, plan.SQLArtifactSizeBytes,
				plan.StartOffset, plan.EndOffset, plan.StatementSHA256, classification, plan.ExpectedTransitionDigest,
			}
		}
	}
	return witness
}

func buildRetryReceiptWitness(t *testing.T, value JSONValue) verifiedRetryReceipt {
	t.Helper()
	var fixture retryReceiptFixture
	decodeFixtureValue(t, value, &fixture)
	if fixture.OracleKind != "owned_retry_receipt_pair/v1" {
		t.Fatal("retry oracle kind")
	}
	identity := ownedRetryIdentity{fixture.MigrationID, fixture.AttemptIndex, fixture.ExecutionLineageDigest, fixture.JournalIdentityDigest}
	orderToken := &retryLifecycleOrderToken{verifierNonce: [16]byte{1}}
	oldOrder := ownedLifecycleOrderAuthority{orderToken, 1}
	newOrder := ownedLifecycleOrderAuthority{orderToken, 2}
	r := fixture.RecoveryPredecessor
	predecessor := ownedRecoveryPredecessorReceipt{identity, fixture.NewConnectionLifecycleID, newOrder, r.OrderedLedgerRows, r.LedgerPrefixDigest, r.AttemptPredecessorCatalogDigest, r.ObservedCatalogDigest, r.AuthorityResultDigest}
	var receipt verifiedRetryReceipt
	var err error
	switch fixture.OldReceiptKind {
	case "owned_rollback":
		if fixture.RollbackSucceeded == nil || fixture.CommitCalled || fixture.ReadyForQuery != nil || fixture.CommitIntentRecordDigest != nil {
			t.Fatal("rollback oracle shape")
		}
		receipt, err = bindRollbackRetryReceipt(fixture.ProofKind, ownedRollbackReceipt{identity, fixture.OldConnectionLifecycleID, oldOrder, *fixture.RollbackSucceeded, fixture.OldHandleIrrevocablyClosed}, predecessor)
	case "owned_precommit_connection_terminated":
		if fixture.RollbackSucceeded != nil || fixture.CommitCalled || fixture.ReadyForQuery != nil || fixture.CommitIntentRecordDigest != nil {
			t.Fatal("terminated oracle shape")
		}
		receipt, err = bindPrecommitTerminatedRetryReceipt(ownedPrecommitTerminatedReceipt{identity, fixture.OldConnectionLifecycleID, oldOrder, fixture.OldHandleIrrevocablyClosed}, predecessor)
	case "owned_commit_rejected":
		if fixture.RollbackSucceeded != nil || !fixture.CommitCalled || fixture.ReadyForQuery == nil || fixture.CommitRejectedReason == nil || fixture.CommitIntentRecordDigest == nil {
			t.Fatal("commit rejected oracle shape")
		}
		receipt, err = bindCommitRejectedRetryReceipt(ownedCommitRejectedReceipt{identity, fixture.OldConnectionLifecycleID, oldOrder, fixture.OldHandleIrrevocablyClosed, *fixture.ReadyForQuery, *fixture.CommitRejectedReason, *fixture.CommitIntentRecordDigest}, predecessor)
	default:
		t.Fatal("unknown concrete receipt")
	}
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func buildAmbiguousBoundaryWitness(t *testing.T, value JSONValue) ownedAmbiguousBoundaryWitness {
	t.Helper()
	var fixture ambiguousBoundaryFixture
	decodeFixtureValue(t, value, &fixture)
	if fixture.OracleKind != "owned_ambiguous_commit_boundary/v1" {
		t.Fatal("ambiguous oracle kind")
	}
	return ownedAmbiguousBoundaryWitness{fixture.MigrationID, fixture.AttemptIndex, fixture.CommitCalled, fixture.FinalIntermediateRecordDigest, fixture.CommitIntentRecordDigest}
}

func decodeEvidenceFrames(t *testing.T, value JSONValue) []EvidenceFrame {
	t.Helper()
	items, ok := value.([]JSONValue)
	if !ok {
		t.Fatal("frames")
	}
	frames := make([]EvidenceFrame, len(items))
	for index := range items {
		decodeFixtureValue(t, items[index], &frames[index])
	}
	return frames
}

func decodeOneEvidenceFrame(t *testing.T, value JSONValue) EvidenceFrame {
	t.Helper()
	var frame EvidenceFrame
	decodeFixtureValue(t, value, &frame)
	return frame
}

func decodeLineageFrames(t *testing.T, value JSONValue) []LineageIndexFrame {
	t.Helper()
	items, ok := value.([]JSONValue)
	if !ok {
		t.Fatal("lineage frames")
	}
	frames := make([]LineageIndexFrame, len(items))
	for index := range items {
		decodeFixtureValue(t, items[index], &frames[index])
	}
	return frames
}

func terminalFrame(t *testing.T, frames []EvidenceFrame) EvidenceFrame {
	t.Helper()
	for _, frame := range frames {
		if frame.Record.AttemptTerminal != nil {
			return frame
		}
	}
	t.Fatal("terminal frame missing")
	return EvidenceFrame{}
}

func decodeUint32Map(t *testing.T, value JSONValue, target map[string]uint32) {
	t.Helper()
	for key, raw := range fixtureObjectValue(t, value, "uint32 map") {
		number, ok := raw.(uint64)
		if !ok || number > uint64(^uint32(0)) {
			t.Fatal("uint32 map value")
		}
		target[key] = uint32(number)
	}
}

func decodeDigestMap(t *testing.T, value JSONValue, target map[string]Digest) {
	t.Helper()
	for key, raw := range fixtureObjectValue(t, value, "digest map") {
		text, ok := raw.(string)
		if !ok {
			t.Fatal("digest map value")
		}
		digest, err := ParseDigest(text)
		if err != nil {
			t.Fatal(err)
		}
		target[key] = digest
	}
}
