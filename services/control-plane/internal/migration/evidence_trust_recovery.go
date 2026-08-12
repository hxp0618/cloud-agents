package migration

import (
	"context"
	"sync/atomic"
)

// bindVerifierOwnedDecision is the sole first-verification constructor for the
// decision and its deterministic recovery artifact. It keeps both values under
// one verifier owner, avoiding any loose artifact overlay seam.
func bindVerifierOwnedDecision(verifier TrustVerifier, decision VerifiedTrustDecision, decisionDigest Digest, artifactBytes []byte) (OwnedVerifiedDecision, VerifiedDecisionRecoveryArtifact, error) {
	historical, ok := verifier.(historicalRecoveryVerifier)
	bindings, bindingErr := decision.runnerProjectionBindings()
	inputs, decodeErr := decodeDecisionRecoveryVerificationInputs(artifactBytes)
	if !ok || decision.validate() != nil || bindingErr != nil || decodeErr != nil || decisionDigest.Validate() != nil || decisionDigest != bindings.runnerProjectionDecisionDigest || inputs.OldRunnerProjectionDecisionDigest != decisionDigest || inputs.ProfileDigest != bindings.decisionRecoveryArtifactProfileDigest || inputs.RepositoryIdentity != decision.repositoryIdentity || inputs.ReleaseIdentity != decision.releaseIdentity || len(artifactBytes) == 0 || uint64(len(artifactBytes)) > maxDecisionRecoveryArtifactBytes {
		return OwnedVerifiedDecision{}, VerifiedDecisionRecoveryArtifact{}, fail(CodeEvidenceRecoveryRequired, "decision-recovery", "verifier cannot own historical recovery", nil)
	}
	canonical, err := canonicalContractKey(inputs)
	if err != nil || canonical != string(artifactBytes) {
		return OwnedVerifiedDecision{}, VerifiedDecisionRecoveryArtifact{}, fail(CodeEvidenceRecoveryRequired, "decision-recovery", "recovery artifact is not exact canonical bytes", err)
	}
	owner := &recoveryVerifierOwner{verifier: historical, token: &evidenceOwnerToken{}}
	artifact := VerifiedDecisionRecoveryArtifact{owner: owner, bytes: append([]byte(nil), artifactBytes...), digest: DigestBytes(artifactBytes), sizeBytes: uint64(len(artifactBytes)), decision: decisionDigest}
	owned := OwnedVerifiedDecision{owner: owner, decision: decision, digest: decisionDigest, capability: sameVerifierRecoveryCapability{owner}}
	return owned, artifact, nil
}

// historicalRecoveryVerifier is deliberately package-private. A verifier that
// produced the current decision may implement it, but callers cannot inject a
// second verifier through the runtime ABI.
type historicalRecoveryVerifier interface {
	recoverHistoricalDecision(context.Context, VerifiedTrustDecision, GenerationDescriptor, VerifiedDecisionRecoveryArtifact) (VerifiedTrustDecision, RunnerProjectionBindings, historicalRecoveryPolicySubject, error)
	recoverHistoricalSupersession(context.Context, VerifiedTrustDecision, *VerifiedLineageSupersessionAuthority, GenerationSuperseded, VerifiedDecisionRecoveryArtifact, VerifiedRuntimeArtifact, VerifiedContentReceipt, VerifiedDecisionRecoveryArtifact, VerifiedDecisionRecoveryReceipt) (*verifiedHistoricalSupersessionReceipt, error)
}

type recoveryVerifierOwner struct {
	verifier historicalRecoveryVerifier
	token    *evidenceOwnerToken
}
type sameVerifierRecoveryCapability struct{ owner *recoveryVerifierOwner }

type VerifiedDecisionRecoveryArtifact struct {
	owner     *recoveryVerifierOwner
	bytes     []byte
	digest    Digest
	sizeBytes uint64
	decision  Digest
}

type OwnedVerifiedDecision struct {
	owner      *recoveryVerifierOwner
	decision   VerifiedTrustDecision
	digest     Digest
	capability sameVerifierRecoveryCapability
}

func bindOwnedVerifiedDecision(verifier TrustVerifier, decision VerifiedTrustDecision, decisionDigest Digest, artifact VerifiedDecisionRecoveryArtifact) (OwnedVerifiedDecision, error) {
	historical, ok := verifier.(historicalRecoveryVerifier)
	inputs, decodeErr := decodeDecisionRecoveryVerificationInputs(artifact.bytes)
	bindings, bindingErr := decision.runnerProjectionBindings()
	if !ok || decodeErr != nil || bindingErr != nil || decision.validate() != nil || decisionDigest.Validate() != nil || decisionDigest != bindings.runnerProjectionDecisionDigest || inputs.OldRunnerProjectionDecisionDigest != decisionDigest || inputs.ProfileDigest != bindings.decisionRecoveryArtifactProfileDigest || inputs.RepositoryIdentity != decision.repositoryIdentity || inputs.ReleaseIdentity != decision.releaseIdentity || artifact.owner == nil || artifact.owner.verifier != historical || artifact.decision != decisionDigest || artifact.digest.Validate() != nil || uint64(len(artifact.bytes)) != artifact.sizeBytes || artifact.sizeBytes == 0 || artifact.sizeBytes > maxDecisionRecoveryArtifactBytes || DigestBytes(artifact.bytes) != artifact.digest {
		return OwnedVerifiedDecision{}, fail(CodeEvidenceRecoveryRequired, "decision-recovery", "same-verifier recovery capability is unavailable or mismatched", nil)
	}
	return OwnedVerifiedDecision{artifact.owner, decision, decisionDigest, sameVerifierRecoveryCapability{artifact.owner}}, nil
}

func (d OwnedVerifiedDecision) recoverHistoricalDecision(ctx context.Context, generation GenerationDescriptor, artifact VerifiedDecisionRecoveryArtifact) (OwnedVerifiedDecision, RunnerProjectionBindings, VerifiedHistoricalRecoveryPolicy, error) {
	if d.owner == nil || d.capability.owner != d.owner || d.owner.verifier == nil || artifact.owner != d.owner || generation.identity.owner != d.owner.token || artifact.decision != generation.header.RunnerProjectionDecisionDigest || generation.recoveryArtifactDigest != artifact.digest || generation.recoveryArtifactSize != artifact.sizeBytes || generation.header.DecisionRecoveryArtifactSHA256 != artifact.digest || generation.header.DecisionRecoveryArtifactSizeBytes != artifact.sizeBytes || !sameGenerationHeader(generation.identity, generation.header) {
		return OwnedVerifiedDecision{}, RunnerProjectionBindings{}, VerifiedHistoricalRecoveryPolicy{}, fail(CodeEvidenceRecoveryRequired, "decision-recovery", "historical inputs are not owned by the same verifier", nil)
	}
	old, bindings, subject, err := d.owner.verifier.recoverHistoricalDecision(ctx, d.decision, generation, artifact)
	if err != nil {
		return OwnedVerifiedDecision{}, RunnerProjectionBindings{}, VerifiedHistoricalRecoveryPolicy{}, err
	}
	// Historical decisions may be expired. The recovery-only verifier owns the
	// signature/epoch/revocation checks; do not route its result through the
	// ordinary current-unexpired validation path here.
	if !old.verified || subject.OldRunnerProjectionDecisionDigest != artifact.decision || subject.CurrentDecisionMismatch(d.digest) {
		return OwnedVerifiedDecision{}, RunnerProjectionBindings{}, VerifiedHistoricalRecoveryPolicy{}, fail(CodeEvidenceRecoveryRequired, "decision-recovery", "historical verifier output is not totally bound", nil)
	}
	digest, err := subject.ComputeDigest()
	if err != nil {
		return OwnedVerifiedDecision{}, RunnerProjectionBindings{}, VerifiedHistoricalRecoveryPolicy{}, err
	}
	ownedOld := OwnedVerifiedDecision{d.owner, old, subject.OldRunnerProjectionDecisionDigest, sameVerifierRecoveryCapability{d.owner}}
	return ownedOld, bindings.ownedCopy(), VerifiedHistoricalRecoveryPolicy{d.owner, subject, digest}, nil
}

func (d OwnedVerifiedDecision) recoverHistoricalSupersession(ctx context.Context, authority *VerifiedLineageSupersessionAuthority, superseded GenerationSuperseded, oldArtifact VerifiedDecisionRecoveryArtifact, plannedRuntime VerifiedRuntimeArtifact, plannedRuntimeReceipt VerifiedContentReceipt, plannedRecovery VerifiedDecisionRecoveryArtifact, plannedRecoveryReceipt VerifiedDecisionRecoveryReceipt) (*verifiedHistoricalSupersessionReceipt, error) {
	if d.owner == nil || d.capability.owner != d.owner || d.owner.verifier == nil || authority == nil || authority.owner != d.owner || oldArtifact.owner != d.owner || plannedRecovery.owner != d.owner || plannedRuntime.owner != d.owner.token || plannedRuntimeReceipt.owner != d.owner.token || plannedRecoveryReceipt.owner != d.owner.token || plannedRuntimeReceipt.kind != "runtime" || plannedRecoveryReceipt.kind != "decision_recovery" {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession", "inputs are not owned by the same verifier", nil)
	}
	if err := superseded.Validate(); err != nil || superseded.LineageSupersessionAuthorityDigest != authority.digest || plannedRuntime.digest != plannedRuntimeReceipt.digest || plannedRuntime.sizeBytes != plannedRuntimeReceipt.sizeBytes || plannedRecovery.digest != plannedRecoveryReceipt.digest || plannedRecovery.sizeBytes != plannedRecoveryReceipt.sizeBytes {
		return nil, invalidEvidence("historical-supersession", "authority, body, or typed receipt mismatch")
	}
	receipt, err := d.owner.verifier.recoverHistoricalSupersession(ctx, d.decision, authority, superseded, oldArtifact, plannedRuntime, plannedRuntimeReceipt, plannedRecovery, plannedRecoveryReceipt)
	if err != nil {
		return nil, err
	}
	if receipt == nil || receipt.owner != d.owner || receipt.authorityDigest != authority.digest || !validRuntimeReceipt(receipt.runtimeReceipt, d.owner.token, plannedRuntimeReceipt.digest, plannedRuntimeReceipt.sizeBytes) || receipt.runtimeReceipt.identity != plannedRuntimeReceipt.identity || !validDecisionRecoveryReceipt(receipt.recoveryReceipt, d.owner.token, plannedRecoveryReceipt.digest, plannedRecoveryReceipt.sizeBytes) || receipt.recoveryReceipt.identity != plannedRecoveryReceipt.identity {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-supersession", "verifier returned a foreign receipt", nil)
	}
	return receipt, nil
}

func (s historicalRecoveryPolicySubject) CurrentDecisionMismatch(current Digest) bool {
	return s.SuccessorRunnerProjectionDecisionDigest != current
}

type VerifiedHistoricalRecoveryPolicy struct {
	owner   *recoveryVerifierOwner
	subject historicalRecoveryPolicySubject
	digest  Digest
}
type VerifiedRecoveryExecutionBindings struct {
	owner      *recoveryVerifierOwner
	session    *evidenceOwnerToken
	generation generationIdentity
	tailDigest Digest
	snapshot   *RecoverySnapshot
	policy     historicalRecoveryPolicySubject
	subject    recoveryExecutionBindingsSubject
	digest     Digest
}

func bindRecoveryExecution(policy VerifiedHistoricalRecoveryPolicy, current, old OwnedVerifiedDecision, oldBindings RunnerProjectionBindings, generation GenerationDescriptor, snapshot *RecoverySnapshot) (VerifiedRecoveryExecutionBindings, error) {
	if policy.owner == nil || current.owner != policy.owner || old.owner != policy.owner || policy.subject.OldRunnerProjectionDecisionDigest != old.digest || policy.subject.SuccessorRunnerProjectionDecisionDigest != current.digest || snapshot == nil || snapshot.owner != generation.identity.owner || !sameGenerationIdentity(snapshot.generation, generation.identity) || snapshot.tailDigest != generation.replayTailDigest || oldBindings.runnerProjectionDecisionDigest != old.digest {
		return VerifiedRecoveryExecutionBindings{}, invalidEvidence("recovery-bindings", "decision, generation, or snapshot mismatch")
	}
	subject := recoveryExecutionBindingsSubject{
		HistoricalRecoveryPolicyDigest: policy.digest, ExecutionLineageDigest: policy.subject.ExecutionLineageDigest,
		CurrentRunnerProjectionDecisionDigest: current.digest, OldRunnerProjectionDecisionDigest: old.digest,
		OldJournalIdentityDigest: generation.identity.journalIdentityDigest, OldSchemaBundleDigest: generation.identity.schemaBundleDigest,
		OldDecisionRecoveryArtifactSHA256: generation.recoveryArtifactDigest, OldDecisionRecoveryArtifactSizeBytes: generation.recoveryArtifactSize,
		OldJournalReplayTailDigest: snapshot.tailDigest, OldRecoveryState: string(snapshot.state), ActionsProfile: oldAttemptRecoveryActionsProfile,
	}
	digest, err := subject.ComputeDigest()
	if err != nil {
		return VerifiedRecoveryExecutionBindings{}, err
	}
	return VerifiedRecoveryExecutionBindings{policy.owner, generation.identity.owner, generation.identity, snapshot.tailDigest, cloneRecoverySnapshot(snapshot), cloneProjectionValue(policy.subject), subject, digest}, nil
}

type ownedSupersessionEvidence interface {
	supersessionEvidenceSealed()
	bindSubject(VerifiedRecoveryExecutionBindings) (lineageSupersessionAuthoritySubject, error)
}

type ownedCheckpointSupersessionEvidence struct {
	owner            *evidenceOwnerToken
	generation       generationIdentity
	tailDigest       Digest
	checkpointDigest Digest
	terminalDigest   *Digest
	resolutionDigest *Digest
	outcome          string
	continuation     *LineageContinuationContext
	consumed         atomic.Bool
}
type ownedHeaderOnlySupersessionEvidence struct {
	owner             *evidenceOwnerToken
	generation        generationIdentity
	tailDigest        Digest
	activationDigest  Digest
	initialTailDigest Digest
	continuation      *LineageContinuationContext
	consumed          atomic.Bool
}

func (*ownedCheckpointSupersessionEvidence) supersessionEvidenceSealed() {}
func (*ownedHeaderOnlySupersessionEvidence) supersessionEvidenceSealed() {}

func (e *ownedCheckpointSupersessionEvidence) bindSubject(bindings VerifiedRecoveryExecutionBindings) (lineageSupersessionAuthoritySubject, error) {
	if e == nil || e.consumed.Load() || e.owner != bindings.session || !sameGenerationIdentity(e.generation, bindings.generation) || e.tailDigest != bindings.tailDigest || e.checkpointDigest.Validate() != nil || !knownRecoveryOutcome(e.outcome) || e.outcome == "activated_no_migration_progress" {
		return lineageSupersessionAuthoritySubject{}, invalidEvidence("supersession-evidence", "checkpoint boundary")
	}
	if !e.consumed.CompareAndSwap(false, true) {
		return lineageSupersessionAuthoritySubject{}, invalidEvidence("supersession-evidence", "already consumed")
	}
	return supersessionSubject(bindings, e.checkpointDigest, "", "", e.terminalDigest, e.resolutionDigest, e.outcome, e.continuation), nil
}
func (e *ownedHeaderOnlySupersessionEvidence) bindSubject(bindings VerifiedRecoveryExecutionBindings) (lineageSupersessionAuthoritySubject, error) {
	if e == nil || e.consumed.Load() || e.owner != bindings.session || !sameGenerationIdentity(e.generation, bindings.generation) || e.tailDigest != bindings.tailDigest || e.initialTailDigest != bindings.tailDigest || requireEvidenceDigests(e.activationDigest, e.initialTailDigest) != nil {
		return lineageSupersessionAuthoritySubject{}, invalidEvidence("supersession-evidence", "header-only boundary")
	}
	if !e.consumed.CompareAndSwap(false, true) {
		return lineageSupersessionAuthoritySubject{}, invalidEvidence("supersession-evidence", "already consumed")
	}
	return supersessionSubject(bindings, "", e.activationDigest, e.initialTailDigest, nil, nil, "activated_no_migration_progress", e.continuation), nil
}

func supersessionSubject(b VerifiedRecoveryExecutionBindings, checkpoint, activation, initial Digest, terminal, resolution *Digest, outcome string, continuation *LineageContinuationContext) lineageSupersessionAuthoritySubject {
	optional := func(d Digest) *Digest {
		if d == "" {
			return nil
		}
		return digestPointer(d)
	}
	return lineageSupersessionAuthoritySubject{
		HistoricalRecoveryPolicyDigest: b.subject.HistoricalRecoveryPolicyDigest, RecoveryExecutionBindingsDigest: b.digest,
		ExecutionLineageDigest: b.subject.ExecutionLineageDigest, OldJournalIdentityDigest: b.subject.OldJournalIdentityDigest,
		OldRunnerProjectionDecisionDigest: b.subject.OldRunnerProjectionDecisionDigest, OldSchemaBundleDigest: b.subject.OldSchemaBundleDigest,
		OldCheckpointRecordDigest: optional(checkpoint), OldActivationRecordDigest: optional(activation), OldInitialJournalTailDigest: optional(initial),
		OldTerminalDigest: cloneDigestPointer(terminal), OldResolutionDigest: cloneDigestPointer(resolution), ObservedOutcome: outcome,
		SuccessorRunnerProjectionDecisionDigest: b.policy.SuccessorRunnerProjectionDecisionDigest, SuccessorSchemaBundleDigest: b.policy.SuccessorSchemaBundleDigest, Continuation: cloneProjectionValue(continuation),
	}
}

type VerifiedLineageSupersessionAuthority struct {
	owner      *recoveryVerifierOwner
	session    *evidenceOwnerToken
	generation generationIdentity
	tailDigest Digest
	subject    lineageSupersessionAuthoritySubject
	digest     Digest
	consumed   atomic.Bool
}

func (a *VerifiedLineageSupersessionAuthority) consume(session *evidenceOwnerToken, generation generationIdentity, tail Digest) (lineageSupersessionAuthoritySubject, error) {
	if a == nil || a.consumed.Load() || a.owner == nil || a.session == nil || a.session != session || a.tailDigest != tail || !sameGenerationIdentity(a.generation, generation) {
		return lineageSupersessionAuthoritySubject{}, invalidEvidence("supersession-authority", "session, generation, tail, or one-shot binding")
	}
	if !a.consumed.CompareAndSwap(false, true) {
		return lineageSupersessionAuthoritySubject{}, invalidEvidence("supersession-authority", "already consumed")
	}
	return cloneProjectionValue(a.subject), nil
}

func bindLineageSupersession(policy VerifiedHistoricalRecoveryPolicy, bindings VerifiedRecoveryExecutionBindings, evidence ownedSupersessionEvidence) (*VerifiedLineageSupersessionAuthority, error) {
	if policy.owner == nil || policy.owner != bindings.owner || policy.digest != bindings.subject.HistoricalRecoveryPolicyDigest || evidence == nil {
		return nil, invalidEvidence("supersession-authority", "policy or execution ownership")
	}
	subject, err := evidence.bindSubject(bindings)
	if err != nil {
		return nil, err
	}
	subject.SuccessorRunnerProjectionDecisionDigest = policy.subject.SuccessorRunnerProjectionDecisionDigest
	subject.SuccessorSchemaBundleDigest = policy.subject.SuccessorSchemaBundleDigest
	if err := validateRecoveryAuthorityBindings(bindings.subject.CurrentRunnerProjectionDecisionDigest, policy.subject, bindings.subject, subject); err != nil {
		return nil, err
	}
	digest, err := subject.ComputeDigest()
	if err != nil {
		return nil, err
	}
	return &VerifiedLineageSupersessionAuthority{owner: policy.owner, session: bindings.session, generation: bindings.generation, tailDigest: bindings.tailDigest, subject: subject, digest: digest}, nil
}

type verifiedHistoricalSupersessionReceipt struct {
	owner           *recoveryVerifierOwner
	authorityDigest Digest
	runtimeReceipt  VerifiedContentReceipt
	recoveryReceipt VerifiedDecisionRecoveryReceipt
	consumed        atomic.Bool
}

func (r *verifiedHistoricalSupersessionReceipt) consume(owner *recoveryVerifierOwner, authority Digest) error {
	if r == nil || r.consumed.Load() || r.owner == nil || r.owner != owner || r.authorityDigest != authority || r.runtimeReceipt.owner != owner.token || r.recoveryReceipt.owner != owner.token {
		return invalidEvidence("historical-supersession-receipt", "owner, authority, typed receipt, or one-shot mismatch")
	}
	if !r.consumed.CompareAndSwap(false, true) {
		return invalidEvidence("historical-supersession-receipt", "already consumed")
	}
	return nil
}

func cloneRecoverySnapshot(s *RecoverySnapshot) *RecoverySnapshot {
	if s == nil {
		return nil
	}
	copy := *s
	copy.cursor = s.cursor.clone()
	copy.migrationID = cloneStringPointer(s.migrationID)
	copy.attemptIndex = cloneUint32Pointer(s.attemptIndex)
	copy.lineageContinuation = cloneOwnedRecovered(s.lineageContinuation)
	copy.lastStatementIntent = cloneOwnedRecovered(s.lastStatementIntent)
	copy.lastIntermediateEvidence = cloneOwnedRecovered(s.lastIntermediateEvidence)
	copy.commitIntent = cloneOwnedRecovered(s.commitIntent)
	copy.lastTerminal = cloneOwnedRecovered(s.lastTerminal)
	copy.lastResolution = cloneOwnedRecovered(s.lastResolution)
	return &copy
}
