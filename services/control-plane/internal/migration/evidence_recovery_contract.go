package migration

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	historicalRecoveryPolicyDomain     = "cloud-agents-platform-historical-recovery-policy/v1"
	recoveryExecutionBindingsDomain    = "cloud-agents-platform-recovery-execution-bindings/v1"
	lineageSupersessionAuthorityDomain = "cloud-agents-platform-lineage-supersession-authority/v1"
	oldAttemptRecoveryActionsProfile   = "cloud-agents-platform-old-attempt-exact-recovery/v1"
)

// These structs are exact canonical inputs owned by the verifier/evidence
// package. None is an EvidenceRecord branch or a caller-facing authority.
type decisionRecoveryProjectionSubjectInput struct {
	Kind                               string `json:"kind"`
	SubjectDigest                      Digest `json:"subject_digest"`
	SubjectBase64URLNoPadding          string `json:"subject_base64url_no_padding"`
	DetachedEnvelopeBase64URLNoPadding string `json:"detached_envelope_base64url_no_padding"`
}

type decisionRecoveryVerificationInputs struct {
	FormatVersion                               string                                   `json:"format_version"`
	ProfileDigest                               Digest                                   `json:"profile_digest"`
	OldRunnerProjectionDecisionDigest           Digest                                   `json:"old_runner_projection_decision_digest"`
	RepositoryIdentity                          string                                   `json:"repository_identity"`
	ReleaseIdentity                             string                                   `json:"release_identity"`
	CandidateSubjectBase64URLNoPadding          string                                   `json:"candidate_subject_base64url_no_padding"`
	CandidateDetachedEnvelopeBase64URLNoPadding string                                   `json:"candidate_detached_envelope_base64url_no_padding"`
	ProjectionSubjectInputs                     []decisionRecoveryProjectionSubjectInput `json:"projection_subject_inputs"`
}

type lineageContinuationIdentity struct {
	StartAction     string `json:"start_action"`
	MigrationID     string `json:"migration_id"`
	AttemptIndex    uint32 `json:"attempt_index"`
	PreviousAttempt string `json:"previous_attempt"`
}

type historicalOutcomeContinuation struct {
	Kind     string                       `json:"kind"`
	Identity *lineageContinuationIdentity `json:"identity,omitempty"`
}

func (c *historicalOutcomeContinuation) UnmarshalJSON(data []byte) error {
	value, err := ParseStrictJSON(data)
	if err != nil {
		return err
	}
	object, ok := value.(map[string]JSONValue)
	if !ok {
		return invalidEvidence("historical-policy", "continuation object")
	}
	kind, ok := object["kind"].(string)
	if !ok {
		return invalidEvidence("historical-policy", "continuation kind")
	}
	switch kind {
	case "must_be_null", "exact_carry_old_generation":
		var branch struct {
			Kind string `json:"kind"`
		}
		if _, err := DecodeStrict(data, &branch); err != nil {
			return err
		}
		*c = historicalOutcomeContinuation{Kind: branch.Kind}
		return nil
	case "exact_identity":
		var branch struct {
			Kind     string                      `json:"kind"`
			Identity lineageContinuationIdentity `json:"identity"`
		}
		if _, err := DecodeStrict(data, &branch); err != nil {
			return err
		}
		*c = historicalOutcomeContinuation{Kind: branch.Kind, Identity: &branch.Identity}
		return nil
	default:
		return invalidEvidence("historical-policy", "continuation kind")
	}
}

// Custom marshaling keeps identity absent, not null, for the two no-identity
// branches frozen by ADR-0010.
func (c historicalOutcomeContinuation) MarshalJSON() ([]byte, error) {
	if c.Kind == "exact_identity" {
		return json.Marshal(struct {
			Kind     string                      `json:"kind"`
			Identity lineageContinuationIdentity `json:"identity"`
		}{c.Kind, *c.Identity})
	}
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{c.Kind})
}

type historicalOutcomeConstraint struct {
	Outcome      string                        `json:"outcome"`
	Continuation historicalOutcomeContinuation `json:"continuation"`
}

type historicalRecoveryPolicySubject struct {
	RecoveryPolicySubjectDigest             Digest                        `json:"recovery_policy_subject_digest"`
	ExecutionLineageDigest                  Digest                        `json:"execution_lineage_digest"`
	OldJournalIdentityDigest                Digest                        `json:"old_journal_identity_digest"`
	OldRunnerProjectionDecisionDigest       Digest                        `json:"old_runner_projection_decision_digest"`
	OldSchemaBundleDigest                   Digest                        `json:"old_schema_bundle_digest"`
	OldDecisionRecoveryArtifactSHA256       Digest                        `json:"old_decision_recovery_artifact_sha256"`
	OldDecisionRecoveryArtifactSizeBytes    uint64                        `json:"old_decision_recovery_artifact_size_bytes"`
	SuccessorRunnerProjectionDecisionDigest Digest                        `json:"successor_runner_projection_decision_digest"`
	SuccessorSchemaBundleDigest             Digest                        `json:"successor_schema_bundle_digest"`
	AllowedOutcomes                         []string                      `json:"allowed_outcomes"`
	OutcomeConstraints                      []historicalOutcomeConstraint `json:"outcome_constraints"`
}

type recoveryExecutionBindingsSubject struct {
	HistoricalRecoveryPolicyDigest        Digest `json:"historical_recovery_policy_digest"`
	ExecutionLineageDigest                Digest `json:"execution_lineage_digest"`
	CurrentRunnerProjectionDecisionDigest Digest `json:"current_runner_projection_decision_digest"`
	OldRunnerProjectionDecisionDigest     Digest `json:"old_runner_projection_decision_digest"`
	OldJournalIdentityDigest              Digest `json:"old_journal_identity_digest"`
	OldSchemaBundleDigest                 Digest `json:"old_schema_bundle_digest"`
	OldDecisionRecoveryArtifactSHA256     Digest `json:"old_decision_recovery_artifact_sha256"`
	OldDecisionRecoveryArtifactSizeBytes  uint64 `json:"old_decision_recovery_artifact_size_bytes"`
	OldJournalReplayTailDigest            Digest `json:"old_journal_replay_tail_digest"`
	OldRecoveryState                      string `json:"old_recovery_state"`
	ActionsProfile                        string `json:"actions_profile"`
}

type lineageSupersessionAuthoritySubject struct {
	HistoricalRecoveryPolicyDigest          Digest                      `json:"historical_recovery_policy_digest"`
	RecoveryExecutionBindingsDigest         Digest                      `json:"recovery_execution_bindings_digest"`
	ExecutionLineageDigest                  Digest                      `json:"execution_lineage_digest"`
	OldJournalIdentityDigest                Digest                      `json:"old_journal_identity_digest"`
	OldRunnerProjectionDecisionDigest       Digest                      `json:"old_runner_projection_decision_digest"`
	OldSchemaBundleDigest                   Digest                      `json:"old_schema_bundle_digest"`
	OldCheckpointRecordDigest               *Digest                     `json:"old_checkpoint_record_digest"`
	OldActivationRecordDigest               *Digest                     `json:"old_activation_record_digest"`
	OldInitialJournalTailDigest             *Digest                     `json:"old_initial_journal_tail_digest"`
	OldTerminalDigest                       *Digest                     `json:"old_terminal_digest"`
	OldResolutionDigest                     *Digest                     `json:"old_resolution_digest"`
	ObservedOutcome                         string                      `json:"observed_outcome"`
	SuccessorRunnerProjectionDecisionDigest Digest                      `json:"successor_runner_projection_decision_digest"`
	SuccessorSchemaBundleDigest             Digest                      `json:"successor_schema_bundle_digest"`
	Continuation                            *LineageContinuationContext `json:"continuation"`
}

type ownedHistoricalContentReceipt struct {
	decision          Digest
	runtimeSHA256     Digest
	runtimeSizeBytes  uint64
	recoverySHA256    Digest
	recoverySizeBytes uint64
}
type ownedHistoricalTransition struct {
	oldDecision              Digest
	successorDecision        Digest
	historical               historicalRecoveryPolicySubject
	execution                recoveryExecutionBindingsSubject
	authority                lineageSupersessionAuthoritySubject
	planned                  GenerationReserved
	plannedReservationDigest Digest
}
type verifiedHistoricalRecoveryChain struct {
	currentDecision Digest
	authorities     map[Digest]lineageSupersessionAuthoritySubject
}

func bindHistoricalRecoveryChain(currentDecision Digest, currentPolicy recoveryPolicySignedSubject, receipts []ownedHistoricalContentReceipt, transitions []ownedHistoricalTransition) (verifiedHistoricalRecoveryChain, error) {
	policyDigest, err := recoveryPolicySubjectDigestFromSigned(currentPolicy)
	if err != nil || len(transitions) == 0 {
		return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "policy or transitions")
	}
	authorized := map[Digest]bool{}
	for _, authorization := range currentPolicy.OldDecisionAuthorizations {
		authorized[authorization.OldRunnerProjectionDecisionDigest] = true
	}
	receiptByDecision := map[Digest]ownedHistoricalContentReceipt{}
	for _, receipt := range receipts {
		if receipt.decision.Validate() != nil || requireEvidenceDigests(receipt.runtimeSHA256, receipt.recoverySHA256) != nil || receipt.runtimeSizeBytes == 0 || receipt.recoverySizeBytes == 0 || receipt.recoverySizeBytes > maxDecisionRecoveryArtifactBytes {
			return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "content receipt")
		}
		if _, duplicate := receiptByDecision[receipt.decision]; duplicate {
			return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "duplicate receipt")
		}
		receiptByDecision[receipt.decision] = receipt
	}
	authorities := map[Digest]lineageSupersessionAuthoritySubject{}
	var expectedOld *Digest
	for index := range transitions {
		transition := transitions[index]
		if !authorized[transition.oldDecision] || expectedOld != nil && transition.oldDecision != *expectedOld || transition.historical.RecoveryPolicySubjectDigest != policyDigest || transition.historical.OldRunnerProjectionDecisionDigest != transition.oldDecision || transition.historical.SuccessorRunnerProjectionDecisionDigest != transition.successorDecision {
			return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "ordered authorization")
		}
		oldReceipt, present := receiptByDecision[transition.oldDecision]
		if !present || transition.historical.OldDecisionRecoveryArtifactSHA256 != oldReceipt.recoverySHA256 || transition.historical.OldDecisionRecoveryArtifactSizeBytes != oldReceipt.recoverySizeBytes {
			return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "old artifact receipt")
		}
		if err := validateRecoveryAuthorityBindings(currentDecision, transition.historical, transition.execution, transition.authority); err != nil {
			return verifiedHistoricalRecoveryChain{}, err
		}
		plannedCanonical, err := canonicalContractKey(transition.planned)
		if err != nil || DigestBytes([]byte(plannedCanonical)) != transition.plannedReservationDigest {
			return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "planned reservation digest")
		}
		if err := transition.planned.Validate(); err != nil || transition.planned.RunnerProjectionDecisionDigest != transition.successorDecision || transition.planned.SchemaBundleDigest != transition.authority.SuccessorSchemaBundleDigest || !canonicalEqual(transition.planned.Continuation, transition.authority.Continuation) {
			return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "planned reservation")
		}
		if successorReceipt, ok := receiptByDecision[transition.successorDecision]; ok {
			header := transition.planned.PlannedSegment0Header
			if header.OuterArtifactDigest != successorReceipt.runtimeSHA256 || header.OuterArtifactSizeBytes != successorReceipt.runtimeSizeBytes || header.DecisionRecoveryArtifactSHA256 != successorReceipt.recoverySHA256 || header.DecisionRecoveryArtifactSizeBytes != successorReceipt.recoverySizeBytes {
				return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "successor artifact receipt")
			}
		}
		authorityDigest, _ := transition.authority.ComputeDigest()
		authorities[authorityDigest] = transition.authority
		expectedOld = &transition.successorDecision
	}
	if expectedOld == nil || *expectedOld != currentDecision {
		return verifiedHistoricalRecoveryChain{}, invalidEvidence("recovery-chain", "current terminus")
	}
	return verifiedHistoricalRecoveryChain{currentDecision, authorities}, nil
}

func decodeDecisionRecoveryVerificationInputs(raw []byte) (decisionRecoveryVerificationInputs, error) {
	var value decisionRecoveryVerificationInputs
	if _, err := DecodeStrict(raw, &value); err != nil {
		return value, err
	}
	if err := value.validate(); err != nil {
		return value, err
	}
	return value, nil
}

func (v decisionRecoveryVerificationInputs) validate() error {
	if v.FormatVersion != decisionRecoveryArtifactFormatVersion || v.ProfileDigest != decisionRecoveryArtifactProfileDigest {
		return invalidEvidence("decision-recovery", "format or profile")
	}
	if err := requireEvidenceDigests(v.OldRunnerProjectionDecisionDigest); err != nil {
		return err
	}
	if !boundedNFC(v.RepositoryIdentity, 1024) || !boundedNFC(v.ReleaseIdentity, 1024) {
		return invalidEvidence("decision-recovery", "identity")
	}
	if _, err := canonicalBase64URL(v.CandidateSubjectBase64URLNoPadding); err != nil {
		return err
	}
	if _, err := canonicalBase64URL(v.CandidateDetachedEnvelopeBase64URLNoPadding); err != nil {
		return err
	}
	if len(v.ProjectionSubjectInputs) < 3 || len(v.ProjectionSubjectInputs) > 4099 {
		return invalidEvidence("decision-recovery", "input count")
	}
	rank := map[string]int{"release": 0, "authority_profile": 1, "authority_binding": 2, "catalog": 3}
	counts := map[string]int{}
	previousRank := -1
	previousDigest := ""
	for _, entry := range v.ProjectionSubjectInputs {
		r, ok := rank[entry.Kind]
		if !ok {
			return invalidEvidence("decision-recovery", "kind")
		}
		subject, err := canonicalBase64URL(entry.SubjectBase64URLNoPadding)
		if err != nil {
			return err
		}
		if _, err = canonicalBase64URL(entry.DetachedEnvelopeBase64URLNoPadding); err != nil {
			return err
		}
		if DigestBytes(subject) != entry.SubjectDigest {
			return invalidEvidence("decision-recovery", "subject digest")
		}
		if r < previousRank || r == previousRank && string(entry.SubjectDigest) <= previousDigest {
			return invalidEvidence("decision-recovery", "sort")
		}
		previousRank = r
		previousDigest = string(entry.SubjectDigest)
		counts[entry.Kind]++
	}
	if counts["release"] != 1 || counts["authority_profile"] != 1 || counts["authority_binding"] != 1 || counts["catalog"] > 4096 {
		return invalidEvidence("decision-recovery", "required kinds")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	parsed, err := ParseStrictJSON(raw)
	if err != nil {
		return err
	}
	canonical, err := CanonicalJSON(parsed)
	if err != nil {
		return err
	}
	if len(canonical) > 4<<20 {
		return invalidEvidence("decision-recovery", "size")
	}
	return nil
}

func boundedNFC(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && len([]byte(value)) <= limit && norm.NFC.IsNormalString(value)
}
func canonicalBase64URL(value string) ([]byte, error) {
	if value == "" || len([]byte(value)) > 1<<20 || strings.Contains(value, "=") {
		return nil, invalidEvidence("base64url", "shape")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, invalidEvidence("base64url", "canonical")
	}
	return decoded, nil
}

func (v historicalRecoveryPolicySubject) ComputeDigest() (Digest, error) {
	if err := v.validate(); err != nil {
		return "", err
	}
	return digestFlatDomain(historicalRecoveryPolicyDomain, v, "")
}
func (v recoveryExecutionBindingsSubject) ComputeDigest() (Digest, error) {
	if err := v.validate(); err != nil {
		return "", err
	}
	return digestFlatDomain(recoveryExecutionBindingsDomain, v, "")
}
func (v lineageSupersessionAuthoritySubject) ComputeDigest() (Digest, error) {
	if err := v.validate(); err != nil {
		return "", err
	}
	return digestFlatDomain(lineageSupersessionAuthorityDomain, v, "")
}

func (v historicalRecoveryPolicySubject) validate() error {
	if err := requireEvidenceDigests(v.RecoveryPolicySubjectDigest, v.ExecutionLineageDigest, v.OldJournalIdentityDigest, v.OldRunnerProjectionDecisionDigest, v.OldSchemaBundleDigest, v.OldDecisionRecoveryArtifactSHA256, v.SuccessorRunnerProjectionDecisionDigest, v.SuccessorSchemaBundleDigest); err != nil {
		return err
	}
	if v.OldDecisionRecoveryArtifactSizeBytes == 0 || v.OldDecisionRecoveryArtifactSizeBytes > maxDecisionRecoveryArtifactBytes {
		return invalidEvidence("historical-policy", "artifact size")
	}
	if len(v.AllowedOutcomes) == 0 || len(v.AllowedOutcomes) != len(v.OutcomeConstraints) || !sort.StringsAreSorted(v.AllowedOutcomes) {
		return invalidEvidence("historical-policy", "outcomes")
	}
	for i, o := range v.AllowedOutcomes {
		if i > 0 && o == v.AllowedOutcomes[i-1] || !knownRecoveryOutcome(o) {
			return invalidEvidence("historical-policy", "outcome")
		}
		c := v.OutcomeConstraints[i]
		if c.Outcome != o {
			return invalidEvidence("historical-policy", "constraint order")
		}
		if err := c.Continuation.validateForOutcome(o); err != nil {
			return err
		}
	}
	return nil
}
func (v historicalOutcomeContinuation) validateForOutcome(outcome string) error {
	if err := v.validate(); err != nil {
		return err
	}
	switch outcome {
	case "exact_committed_bundle_complete", "confirmed_abort_terminal", "terminal_failure", "divergent_terminal":
		if v.Kind != "must_be_null" {
			return invalidEvidence("historical-policy", "outcome requires null continuation")
		}
	case "exact_committed_continue_successor":
		if v.Kind != "exact_identity" || v.Identity == nil || v.Identity.StartAction != "begin_first_attempt_next_entry" {
			return invalidEvidence("historical-policy", "successor continuation identity")
		}
	case "precommit_aborted_retryable", "exact_pending", "resolved_pending":
		if v.Kind != "exact_identity" || v.Identity == nil || v.Identity.StartAction != "begin_next_attempt" {
			return invalidEvidence("historical-policy", "retry continuation identity")
		}
	case "activated_no_migration_progress":
		if v.Kind != "exact_carry_old_generation" {
			return invalidEvidence("historical-policy", "activation continuation carry")
		}
	default:
		return invalidEvidence("historical-policy", "unknown outcome")
	}
	return nil
}
func (v historicalOutcomeContinuation) validate() error {
	switch v.Kind {
	case "must_be_null", "exact_carry_old_generation":
		if v.Identity != nil {
			return invalidEvidence("historical-policy", "unexpected identity")
		}
	case "exact_identity":
		if v.Identity == nil {
			return invalidEvidence("historical-policy", "missing identity")
		}
		return v.Identity.validate()
	default:
		return invalidEvidence("historical-policy", "constraint kind")
	}
	return nil
}
func (v lineageContinuationIdentity) validate() error {
	if !migrationIDPattern.MatchString(v.MigrationID) || v.AttemptIndex == 0 {
		return invalidEvidence("continuation-identity", "identity")
	}
	switch v.StartAction {
	case "begin_first_attempt_next_entry":
		if v.AttemptIndex != 1 || v.PreviousAttempt != "null" {
			return invalidEvidence("continuation-identity", "next entry")
		}
	case "begin_next_attempt":
		if v.AttemptIndex < 2 || v.PreviousAttempt != "owned_old_terminal" {
			return invalidEvidence("continuation-identity", "next attempt")
		}
	default:
		return invalidEvidence("continuation-identity", "action")
	}
	return nil
}
func (v recoveryExecutionBindingsSubject) validate() error {
	if err := requireEvidenceDigests(v.HistoricalRecoveryPolicyDigest, v.ExecutionLineageDigest, v.CurrentRunnerProjectionDecisionDigest, v.OldRunnerProjectionDecisionDigest, v.OldJournalIdentityDigest, v.OldSchemaBundleDigest, v.OldDecisionRecoveryArtifactSHA256, v.OldJournalReplayTailDigest); err != nil {
		return err
	}
	if v.OldDecisionRecoveryArtifactSizeBytes == 0 || v.OldDecisionRecoveryArtifactSizeBytes > maxDecisionRecoveryArtifactBytes || v.ActionsProfile != oldAttemptRecoveryActionsProfile || !knownRecoveryState(v.OldRecoveryState) {
		return invalidEvidence("recovery-bindings", "shape")
	}
	return nil
}
func (v lineageSupersessionAuthoritySubject) validate() error {
	if err := requireEvidenceDigests(v.HistoricalRecoveryPolicyDigest, v.RecoveryExecutionBindingsDigest, v.ExecutionLineageDigest, v.OldJournalIdentityDigest, v.OldRunnerProjectionDecisionDigest, v.OldSchemaBundleDigest, v.SuccessorRunnerProjectionDecisionDigest, v.SuccessorSchemaBundleDigest); err != nil {
		return err
	}
	if err := validateOptionalDigests(v.OldCheckpointRecordDigest, v.OldActivationRecordDigest, v.OldInitialJournalTailDigest, v.OldTerminalDigest, v.OldResolutionDigest); err != nil {
		return err
	}
	if v.Continuation != nil {
		if err := v.Continuation.Validate(); err != nil {
			return err
		}
	}
	switch v.ObservedOutcome {
	case "exact_committed_bundle_complete", "confirmed_abort_terminal", "terminal_failure", "divergent_terminal":
		if v.Continuation != nil {
			return invalidEvidence("supersession-authority", "terminal outcome continuation")
		}
	case "exact_committed_continue_successor":
		if v.Continuation == nil || v.Continuation.StartAction != "begin_first_attempt_next_entry" {
			return invalidEvidence("supersession-authority", "successor continuation")
		}
	case "precommit_aborted_retryable", "exact_pending", "resolved_pending":
		if v.Continuation == nil || v.Continuation.StartAction != "begin_next_attempt" {
			return invalidEvidence("supersession-authority", "retry continuation")
		}
	case "activated_no_migration_progress":
		// Both null and non-null are legal here. The latter is byte-exact checked
		// against the old reserved generation by the ordered-chain witness.
	default:
		return invalidEvidence("supersession-authority", "outcome")
	}
	return nil
}

// validateRecoveryAuthorityBindings performs the total cross-bind between the
// three private recovery digest subjects. It consumes typed verifier-owned
// values only; no loose JSON can be promoted through this seam.
func validateRecoveryAuthorityBindings(currentDecision Digest, historical historicalRecoveryPolicySubject, execution recoveryExecutionBindingsSubject, authority lineageSupersessionAuthoritySubject) error {
	if err := currentDecision.Validate(); err != nil {
		return err
	}
	historicalDigest, err := historical.ComputeDigest()
	if err != nil {
		return err
	}
	executionDigest, err := execution.ComputeDigest()
	if err != nil {
		return err
	}
	if err := authority.validate(); err != nil {
		return err
	}
	if execution.HistoricalRecoveryPolicyDigest != historicalDigest ||
		execution.CurrentRunnerProjectionDecisionDigest != currentDecision ||
		execution.ExecutionLineageDigest != historical.ExecutionLineageDigest ||
		execution.OldJournalIdentityDigest != historical.OldJournalIdentityDigest ||
		execution.OldRunnerProjectionDecisionDigest != historical.OldRunnerProjectionDecisionDigest ||
		execution.OldSchemaBundleDigest != historical.OldSchemaBundleDigest ||
		execution.OldDecisionRecoveryArtifactSHA256 != historical.OldDecisionRecoveryArtifactSHA256 ||
		execution.OldDecisionRecoveryArtifactSizeBytes != historical.OldDecisionRecoveryArtifactSizeBytes {
		return invalidEvidence("recovery-bindings", "historical total cross-bind")
	}
	if authority.HistoricalRecoveryPolicyDigest != historicalDigest ||
		authority.RecoveryExecutionBindingsDigest != executionDigest ||
		authority.ExecutionLineageDigest != execution.ExecutionLineageDigest ||
		authority.OldJournalIdentityDigest != execution.OldJournalIdentityDigest ||
		authority.OldRunnerProjectionDecisionDigest != execution.OldRunnerProjectionDecisionDigest ||
		authority.OldSchemaBundleDigest != execution.OldSchemaBundleDigest ||
		authority.SuccessorRunnerProjectionDecisionDigest != historical.SuccessorRunnerProjectionDecisionDigest ||
		authority.SuccessorSchemaBundleDigest != historical.SuccessorSchemaBundleDigest {
		return invalidEvidence("supersession-authority", "execution total cross-bind")
	}
	if authority.ObservedOutcome == "activated_no_migration_progress" {
		if authority.OldCheckpointRecordDigest != nil || authority.OldActivationRecordDigest == nil || authority.OldInitialJournalTailDigest == nil || authority.OldTerminalDigest != nil || authority.OldResolutionDigest != nil || *authority.OldInitialJournalTailDigest != execution.OldJournalReplayTailDigest {
			return invalidEvidence("supersession-authority", "activation boundary")
		}
	} else if authority.OldCheckpointRecordDigest == nil || authority.OldActivationRecordDigest != nil || authority.OldInitialJournalTailDigest != nil {
		return invalidEvidence("supersession-authority", "checkpoint boundary")
	}
	var selected *historicalOutcomeConstraint
	for i := range historical.OutcomeConstraints {
		if historical.OutcomeConstraints[i].Outcome == authority.ObservedOutcome {
			selected = &historical.OutcomeConstraints[i]
			break
		}
	}
	if selected == nil || !recoveryContinuationSatisfies(selected.Continuation, authority.Continuation) {
		return invalidEvidence("supersession-authority", "outcome authorization")
	}
	return nil
}

func recoveryContinuationSatisfies(constraint historicalOutcomeContinuation, context *LineageContinuationContext) bool {
	switch constraint.Kind {
	case "must_be_null":
		return context == nil
	case "exact_identity":
		if constraint.Identity == nil || context == nil {
			return false
		}
		identity := constraint.Identity
		previous := "null"
		if context.PreviousAttemptTerminalDigest != nil {
			previous = "owned_old_terminal"
		}
		return identity.StartAction == context.StartAction && identity.MigrationID == context.MigrationID && identity.AttemptIndex == context.AttemptIndex && identity.PreviousAttempt == previous
	case "exact_carry_old_generation":
		return true
	default:
		return false
	}
}

// recoveryPolicySubjectDigestFromSigned keeps the current signed-policy
// same-bits body aligned with TS while preserving the existing opaque verifier
// type. The domain is checked, then inserted as a flat sibling for hashing.
func recoveryPolicySubjectDigestFromSigned(subject recoveryPolicySignedSubject) (Digest, error) {
	if subject.Domain != recoveryPolicySubjectDomain {
		return "", invalidEvidence("recovery-policy", "domain")
	}
	if _, err := parseCanonicalUTCTime(subject.ExpiresAt); err != nil {
		return "", invalidEvidence("recovery-policy", "expiry")
	}
	if subject.SecurityEpoch == 0 || subject.MinimumOldSecurityEpoch == 0 || subject.SecurityEpoch > maxJSONInteger || subject.MinimumOldSecurityEpoch > maxJSONInteger {
		return "", invalidEvidence("recovery-policy", "epoch")
	}
	if err := requireEvidenceDigests(subject.IssuerKeyIdentityDigest, subject.OldRevocationPolicyDigest); err != nil {
		return "", err
	}
	if subject.OldDecisionAuthorizations == nil {
		return "", invalidEvidence("recovery-policy", "authorizations")
	}
	for i := range subject.OldDecisionAuthorizations {
		authorization := subject.OldDecisionAuthorizations[i]
		if err := authorization.OldRunnerProjectionDecisionDigest.Validate(); err != nil {
			return "", err
		}
		if i > 0 && subject.OldDecisionAuthorizations[i-1].OldRunnerProjectionDecisionDigest >= authorization.OldRunnerProjectionDecisionDigest {
			return "", invalidEvidence("recovery-policy", "authorization order")
		}
	}
	body := struct {
		IssuerKeyIdentityDigest   Digest                     `json:"issuer_key_identity_digest"`
		ExpiresAt                 string                     `json:"expires_at"`
		SecurityEpoch             uint64                     `json:"security_epoch"`
		MinimumOldSecurityEpoch   uint64                     `json:"minimum_old_security_epoch"`
		OldRevocationPolicyDigest Digest                     `json:"old_revocation_policy_digest"`
		OldDecisionAuthorizations []oldDecisionAuthorization `json:"old_decision_authorizations"`
	}{subject.IssuerKeyIdentityDigest, subject.ExpiresAt, subject.SecurityEpoch, subject.MinimumOldSecurityEpoch, subject.OldRevocationPolicyDigest, subject.OldDecisionAuthorizations}
	return digestFlatDomain(recoveryPolicySubjectDomain, body, "")
}
func knownRecoveryState(v string) bool {
	return stringIn(v, "brand_new", "brand_new_inherited", "completed", "dangling_statement_intent", "dangling_intermediate", "dangling_commit_intent", "ambiguous_unresolved", "terminal", "divergent")
}
func knownRecoveryOutcome(v string) bool {
	return stringIn(v, "exact_committed_bundle_complete", "exact_committed_continue_successor", "precommit_aborted_retryable", "exact_pending", "resolved_pending", "confirmed_abort_terminal", "terminal_failure", "divergent_terminal", "activated_no_migration_progress")
}
