package migration

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSemanticFaultInventoryExecutesProductionValidators(t *testing.T) {
	fixture := fixtureObject(t, migrationFixturePath(t, "negative/evidence-semantic-faults-v1.json"))
	cases := fixture["cases"].([]JSONValue)
	if len(cases) != 38 {
		t.Fatalf("semantic cases=%d", len(cases))
	}
	for _, raw := range cases {
		entry := fixtureObjectValue(t, raw, "semantic case")
		name := entry["name"].(string)
		t.Run(name, func(t *testing.T) {
			if err := dispatchSemanticFault(t, name); err == nil {
				t.Fatal("fault reached production validator without rejection")
			}
		})
	}
}

func TestLimitFaultInventoryExecutesProductionValidators(t *testing.T) {
	fixture := fixtureObject(t, migrationFixturePath(t, "negative/evidence-limits-faults-v1.json"))
	boundaries := fixture["boundaries"].([]JSONValue)
	invalid := fixture["invalid_cases"].([]JSONValue)
	if len(boundaries) != 16 || len(invalid) != 4 {
		t.Fatalf("limits %d+%d", len(boundaries), len(invalid))
	}
	for _, raw := range boundaries {
		entry := fixtureObjectValue(t, raw, "boundary")
		name := entry["name"].(string)
		exact := fixtureUint64(t, entry["exact_max"])
		plus := fixtureUint64(t, entry["max_plus_one"])
		t.Run(name, func(t *testing.T) {
			if err := dispatchLimitBoundary(name, exact); err != nil {
				t.Fatalf("exact max: %v", err)
			}
			if err := dispatchLimitBoundary(name, plus); err == nil {
				t.Fatal("max+1 accepted")
			}
		})
	}
	for _, raw := range invalid {
		entry := fixtureObjectValue(t, raw, "invalid")
		name := entry["name"].(string)
		t.Run(name, func(t *testing.T) {
			var err error
			switch name {
			case "negative_usage", "fractional_usage":
				_, err = ParseStrictJSON([]byte(entry["value"].(string)))
			case "non_nfc_identity":
				err = func() error {
					_, _, _, transitions := recoveryChainInputs(t)
					input := decisionRecoveryVerificationInputs{RepositoryIdentity: entry["value"].(string)}
					_ = transitions
					if boundedNFC(input.RepositoryIdentity, 1024) {
						return nil
					}
					return invalidEvidence("identity", "nfc")
				}()
			case "invalid_policy_expiry":
				_, err = parseCanonicalUTCTime(entry["value"].(string))
			}
			if err == nil {
				t.Fatal("invalid limit case accepted")
			}
		})
	}
}

func fixtureUint64(t *testing.T, value JSONValue) uint64 {
	t.Helper()
	switch v := value.(type) {
	case uint64:
		return v
	case string:
		parsed, err := json.Number(v).Int64()
		if err != nil || parsed < 0 {
			t.Fatal("limit integer")
		}
		return uint64(parsed)
	default:
		t.Fatal("limit type")
	}
	return 0
}

func dispatchLimitBoundary(name string, value uint64) error {
	mapping := map[string]string{
		"stable_failure_major": "uint16", "terminal_attempt_index": "uint32", "journal_outer_artifact_size": "uint64_json_safe",
		"quota_reserved_records": "quota_reserved_records", "quota_reserved_bytes": "quota_reserved_bytes",
		"evidence_intermediate_framed_bytes": "evidence_intermediate_framed_bytes", "evidence_segment_bytes": "evidence_segment_bytes",
		"evidence_segment_records": "evidence_segment_records", "journal_reserved_segments": "evidence_journal_segments",
		"lineage_checkpoint_framed_bytes": "lineage_checkpoint_framed_bytes", "lineage_superseded_framed_bytes": "lineage_superseded_framed_bytes", "lineage_index_bytes": "lineage_index_bytes",
		"lineage_index_records": "lineage_index_records", "decision_recovery_identity_bytes": "decision_recovery_identity_bytes",
		"decision_recovery_encoded_bytes": "decision_recovery_encoded_bytes", "decision_recovery_input_count": "decision_recovery_input_count",
	}
	if mapping[name] == "evidence_intermediate_framed_bytes" {
		if value > evidenceRecordFrameLimits[EvidenceRecordIntermediate] {
			return invalidEvidence("limit", "intermediate")
		}
		return nil
	}
	if mapping[name] == "lineage_superseded_framed_bytes" {
		return validateFramedSizeLimit(value, maxLineageFrameBytes, lineageRecordFrameLimits[LineageRecordGenerationSuperseded])
	}
	if mapping[name] == "lineage_checkpoint_framed_bytes" {
		return validateFramedSizeLimit(value, maxLineageFrameBytes, lineageRecordFrameLimits[LineageRecordGenerationCheckpoint])
	}
	return validateEvidenceLimitBoundary(mapping[name], value)
}

func dispatchSemanticFault(t *testing.T, name string) error {
	t.Helper()
	recordDocument := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	recordFrames := decodeEvidenceFrames(t, recordDocument["frames"])
	context := fixtureObjectValue(t, recordDocument["validation_context"], "validation context")
	switch name {
	case "unknown_member", "missing_member", "digest_mutation":
		raw, _ := json.Marshal(recordFrames[0])
		if name == "unknown_member" {
			raw = bytes.Replace(raw, []byte(`{"format_version"`), []byte(`{"unknown":true,"format_version"`), 1)
		} else if name == "missing_member" {
			var object map[string]JSONValue
			_, _ = DecodeStrict(raw, &object)
			delete(object, "record")
			raw, _ = CanonicalJSON(object)
		} else {
			raw = bytes.Replace(raw, []byte(recordFrames[0].RecordDigest), []byte(projectionTestDigest), 1)
		}
		var frame EvidenceFrame
		_, err := DecodeStrict(raw, &frame)
		if err == nil {
			err = frame.Validate()
		}
		return err
	case "stable_failure_tuple":
		terminalFixture := fixtureObject(t, migrationFixturePath(t, "golden/terminal-outcomes-v1.json"))
		outcomes := terminalFixture["outcomes"].([]JSONValue)
		var terminal AttemptTerminalState
		decodeFixtureValue(t, outcomes[1], &terminal)
		terminal.FailureEvidence.Path = "transaction"
		return terminal.Validate()
	case "retry_proof_predecessor":
		terminalFixture := fixtureObject(t, migrationFixturePath(t, "golden/terminal-outcomes-v1.json"))
		proofs := terminalFixture["retry_proofs"].([]JSONValue)
		var proof RetryProofEvidence
		decodeFixtureValue(t, proofs[0], &proof)
		proof.ObservedCatalogDigest = projectionTestDigest
		return proof.Validate()
	case "duplicate_statement_intent", "committed_without_commit_intent", "duplicate_segment0_header", "second_terminal_same_attempt", "post_terminal_intermediate_same_attempt":
		frames := append([]EvidenceFrame(nil), recordFrames...)
		switch name {
		case "duplicate_statement_intent":
			frames = append(frames[:2], append([]EvidenceFrame{frames[1]}, frames[2:]...)...)
		case "committed_without_commit_intent":
			frames = append(frames[:3], frames[4:]...)
		case "duplicate_segment0_header":
			frames = append(frames[:1], append([]EvidenceFrame{frames[0]}, frames[1:]...)...)
		case "second_terminal_same_attempt":
			frames = append(frames, frames[len(frames)-1])
		case "post_terminal_intermediate_same_attempt":
			frames = append(frames, frames[2])
		}
		redigestEvidenceFrames(t, frames)
		return validateEvidenceChainWithWitness(frames, buildEvidenceWitness(t, frames, context))
	case "second_lineage_header", "activation_schema_mismatch", "duplicate_generation_activation", "checkpoint_after_superseded", "duplicate_generation_superseded", "checkpoint_tail", "segment0_wrong_format":
		return dispatchLineageFault(t, name)
	case "old_handle_still_usable", "retry_receipt_cross_lineage", "connection_lifecycle_same", "connection_lifecycle_reversed", "rollback_not_succeeded", "other_error_missing_ready_for_query", "forged_commit_rejected_without_commit_intent":
		return dispatchRetryReceiptFault(t, name, context)
	default:
		return dispatchHistoricalFault(t, name)
	}
}

func dispatchRetryReceiptFault(t *testing.T, name string, context map[string]JSONValue) error {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-retry-chains-v1.json"))
	chains := document["chains"].([]JSONValue)
	selected := fixtureObjectValue(t, chains[0], "retry chain")
	if name == "other_error_missing_ready_for_query" || name == "forged_commit_rejected_without_commit_intent" {
		selected = fixtureObjectValue(t, chains[3], "commit retry chain")
	} else if name == "rollback_not_succeeded" {
		selected = fixtureObjectValue(t, chains[1], "rollback retry chain")
	}
	frames := decodeEvidenceFrames(t, selected["frames"])
	var oracle retryReceiptFixture
	decodeFixtureValue(t, selected["owned_retry_receipt_pair_oracle"], &oracle)
	switch name {
	case "old_handle_still_usable":
		oracle.OldHandleIrrevocablyClosed = false
	case "retry_receipt_cross_lineage":
		oracle.ExecutionLineageDigest = projectionTestDigest
	case "connection_lifecycle_same":
		oracle.NewConnectionLifecycleID = oracle.OldConnectionLifecycleID
	case "connection_lifecycle_reversed":
		oracle.OldBeforeNew = false
	case "rollback_not_succeeded":
		value := false
		oracle.RollbackSucceeded = &value
	case "other_error_missing_ready_for_query":
		value := false
		oracle.ReadyForQuery = &value
	case "forged_commit_rejected_without_commit_intent":
		for index := range frames {
			if frames[index].RecordKind == EvidenceRecordCommitIntent {
				frames = append(frames[:index], frames[index+1:]...)
				break
			}
		}
		redigestEvidenceFrames(t, frames)
	}
	receipt, err := bindRetryOracle(oracle)
	if err != nil {
		return err
	}
	witness := buildEvidenceWitness(t, frames, context)
	terminal := terminalFrame(t, frames)
	witness.retryReceipts[terminal.Record.AttemptTerminal.TerminalDigest] = receipt
	return validateEvidenceChainWithWitness(frames, witness)
}

func bindRetryOracle(f retryReceiptFixture) (verifiedRetryReceipt, error) {
	identity := ownedRetryIdentity{f.MigrationID, f.AttemptIndex, f.ExecutionLineageDigest, f.JournalIdentityDigest}
	orderToken := &retryLifecycleOrderToken{verifierNonce: [16]byte{1}}
	oldOrder := ownedLifecycleOrderAuthority{orderToken, 1}
	newOrder := ownedLifecycleOrderAuthority{orderToken, 2}
	if !f.OldBeforeNew {
		newOrder.ordinal = 1
	}
	r := f.RecoveryPredecessor
	p := ownedRecoveryPredecessorReceipt{identity, f.NewConnectionLifecycleID, newOrder, r.OrderedLedgerRows, r.LedgerPrefixDigest, r.AttemptPredecessorCatalogDigest, r.ObservedCatalogDigest, r.AuthorityResultDigest}
	switch f.OldReceiptKind {
	case "owned_rollback":
		if f.RollbackSucceeded == nil {
			return nil, invalidEvidence("test-oracle", "rollback")
		}
		return bindRollbackRetryReceipt(f.ProofKind, ownedRollbackReceipt{identity, f.OldConnectionLifecycleID, oldOrder, *f.RollbackSucceeded, f.OldHandleIrrevocablyClosed}, p)
	case "owned_precommit_connection_terminated":
		return bindPrecommitTerminatedRetryReceipt(ownedPrecommitTerminatedReceipt{identity, f.OldConnectionLifecycleID, oldOrder, f.OldHandleIrrevocablyClosed}, p)
	case "owned_commit_rejected":
		if f.ReadyForQuery == nil || f.CommitRejectedReason == nil || f.CommitIntentRecordDigest == nil {
			return nil, invalidEvidence("test-oracle", "commit")
		}
		return bindCommitRejectedRetryReceipt(ownedCommitRejectedReceipt{identity, f.OldConnectionLifecycleID, oldOrder, f.OldHandleIrrevocablyClosed, *f.ReadyForQuery, *f.CommitRejectedReason, *f.CommitIntentRecordDigest}, p)
	default:
		return nil, invalidEvidence("test-oracle", "kind")
	}
}

func dispatchLineageFault(t *testing.T, name string) error {
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	journalHeader := decodeOneEvidenceFrame(t, fixture["journal_header_frame"])
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])
	authorityObject := fixtureObjectValue(t, fixture["supersession_authority_oracle"], "authority")
	delete(authorityObject, "domain")
	var authority lineageSupersessionAuthoritySubject
	decodeFixtureValue(t, authorityObject, &authority)
	authorityDigest, _ := authority.ComputeDigest()
	witness := verifiedLineageChainWitness{
		header:             *frames[0].Record.Header,
		actualSegment0:     map[Digest]EvidenceFrame{journalHeader.Record.Header.JournalIdentityDigest: journalHeader},
		journals:           map[Digest][]EvidenceFrame{journalHeader.Record.Header.JournalIdentityDigest: journal},
		historicalRecovery: verifiedHistoricalRecoveryChain{authorities: map[Digest]lineageSupersessionAuthoritySubject{authorityDigest: authority}},
	}
	switch name {
	case "second_lineage_header":
		frames = append(frames[:1], append([]LineageIndexFrame{frames[0]}, frames[1:]...)...)
	case "activation_schema_mismatch":
		frames[2].Record.Activated.SchemaBundleDigest = projectionTestDigest
	case "duplicate_generation_activation":
		frames = append(frames[:3], append([]LineageIndexFrame{frames[2]}, frames[3:]...)...)
	case "checkpoint_after_superseded":
		frames = append(frames, frames[3])
	case "duplicate_generation_superseded":
		frames = append(frames, frames[len(frames)-1])
	case "checkpoint_tail":
		frames[3].Record.Checkpoint.JournalTailDigest = projectionTestDigest
	case "segment0_wrong_format":
		actual := witness.actualSegment0[journalHeader.Record.Header.JournalIdentityDigest]
		actual.FormatVersion = "wrong"
		witness.actualSegment0[journalHeader.Record.Header.JournalIdentityDigest] = actual
	}
	redigestLineageFrames(t, frames)
	return validateLineageChainWithWitness(frames, witness)
}

func redigestLineageFrames(t *testing.T, frames []LineageIndexFrame) {
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

func dispatchHistoricalFault(t *testing.T, name string) error {
	current, policy, receipts, transitions := recoveryChainInputs(t)
	switch name {
	case "historical_artifact_a_missing":
		receipts = receipts[1:]
	case "historical_artifact_b_missing":
		receipts = append(receipts[:1], receipts[2:]...)
	case "current_policy_authorizes_only_a":
		policy.OldDecisionAuthorizations = policy.OldDecisionAuthorizations[:1]
	case "historical_authority_rebuild_mismatch":
		transitions[0].authority.SuccessorSchemaBundleDigest = projectionTestDigest
	case "historical_authority_policy_digest_mismatch_transition_0":
		transitions[0].authority.HistoricalRecoveryPolicyDigest = projectionTestDigest
	case "historical_authority_policy_digest_mismatch_transition_1":
		transitions[1].authority.HistoricalRecoveryPolicyDigest = projectionTestDigest
	case "historical_planned_body_mismatch":
		transitions[0].plannedReservationDigest = projectionTestDigest
	case "historical_execution_current_decision_mismatch":
		transitions[0].execution.CurrentRunnerProjectionDecisionDigest = projectionTestDigest
	case "historical_execution_old_journal_mismatch":
		transitions[0].execution.OldJournalIdentityDigest = projectionTestDigest
	case "historical_artifact_receipt_mismatch":
		receipts[0].recoverySHA256 = projectionTestDigest
	case "historical_policy_disallows_observed_outcome":
		transitions[0].authority.ObservedOutcome = "terminal_failure"
		transitions[0].authority.Continuation = nil
	case "historical_policy_wrong_constraint":
		transitions[0].historical.OutcomeConstraints[0].Continuation = historicalOutcomeContinuation{Kind: "must_be_null"}
	case "historical_policy_closed_matrix":
		transitions[0].historical.AllowedOutcomes = []string{"confirmed_abort_terminal"}
		identity := lineageContinuationIdentity{"begin_next_attempt", "000001", 2, "owned_old_terminal"}
		transitions[0].historical.OutcomeConstraints = []historicalOutcomeConstraint{{"confirmed_abort_terminal", historicalOutcomeContinuation{Kind: "exact_identity", Identity: &identity}}}
	case "historical_exact_identity_mismatch":
		identity := lineageContinuationIdentity{"begin_next_attempt", "000002", 2, "owned_old_terminal"}
		transitions[0].historical.AllowedOutcomes = []string{"precommit_aborted_retryable"}
		transitions[0].historical.OutcomeConstraints = []historicalOutcomeConstraint{{"precommit_aborted_retryable", historicalOutcomeContinuation{Kind: "exact_identity", Identity: &identity}}}
		transitions[0].authority.ObservedOutcome = "precommit_aborted_retryable"
	}
	_, err := bindHistoricalRecoveryChain(current, policy, receipts, transitions)
	return err
}

func recoveryChainInputs(t *testing.T) (Digest, recoveryPolicySignedSubject, []ownedHistoricalContentReceipt, []ownedHistoricalTransition) {
	t.Helper()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/recovery-policy-chain-v1.json"))
	current := fixtureDigest(t, fixture, "current_decision")
	policyVector := fixtureObjectValue(t, fixture["current_signed_policy_subject"], "policy")
	var input recoveryPolicyFixtureInput
	decodeSameBitsInput(t, policyVector, &input)
	policy := recoveryPolicySignedSubject{recoveryPolicySubjectDomain, input.IssuerKeyIdentityDigest, input.ExpiresAt, input.SecurityEpoch, input.MinimumOldSecurityEpoch, input.OldRevocationPolicyDigest, input.OldDecisionAuthorizations}
	receiptValues := fixture["durable_artifact_receipts"].([]JSONValue)
	receipts := make([]ownedHistoricalContentReceipt, len(receiptValues))
	for i, raw := range receiptValues {
		o := fixtureObjectValue(t, raw, "receipt")
		receipts[i] = ownedHistoricalContentReceipt{fixtureDigest(t, o, "decision"), fixtureDigest(t, o, "runtime_sha256"), o["runtime_size_bytes"].(uint64), fixtureDigest(t, o, "recovery_sha256"), o["recovery_size_bytes"].(uint64)}
	}
	values := fixture["transitions"].([]JSONValue)
	transitions := make([]ownedHistoricalTransition, len(values))
	for i, raw := range values {
		o := fixtureObjectValue(t, raw, "transition")
		var h historicalRecoveryPolicySubject
		decodeSameBitsInput(t, fixtureObjectValue(t, o["historical_policy"], "h"), &h)
		var e recoveryExecutionBindingsSubject
		decodeSameBitsInput(t, fixtureObjectValue(t, o["recovery_execution_bindings"], "e"), &e)
		var a lineageSupersessionAuthoritySubject
		decodeSameBitsInput(t, fixtureObjectValue(t, o["supersession_authority"], "a"), &a)
		var p GenerationReserved
		decodeFixtureValue(t, o["planned_generation_reserved"], &p)
		transitions[i] = ownedHistoricalTransition{fixtureDigest(t, o, "old_decision"), fixtureDigest(t, o, "successor_decision"), h, e, a, p, fixtureDigest(t, o, "planned_generation_reserved_digest")}
	}
	return current, policy, receipts, transitions
}
