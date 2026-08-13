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

// bindHistoricalRecoveryVerifierInput binds canonical recovery bytes to the
// current decision's sole recovery verifier owner. It does not prove filesystem
// registration and does not verify or authorize the historical decision. A
// future admission-plan binder must first mint the owned generation descriptor;
// only recoverHistoricalDecision may then perform historical authorization.
func bindHistoricalRecoveryVerifierInput(current OwnedVerifiedDecision, generation GenerationDescriptor, artifactBytes []byte) (VerifiedDecisionRecoveryArtifact, error) {
	bindings, bindingErr := current.decision.runnerProjectionBindings()
	if bindingErr != nil || !validOwnedCurrentDecision(current, bindings) || generation.identity.owner != current.owner.token || generation.header.Validate() != nil || !sameGenerationHeader(generation.identity, generation.header) || generation.recoveryArtifactDigest != generation.header.DecisionRecoveryArtifactSHA256 || generation.recoveryArtifactSize != generation.header.DecisionRecoveryArtifactSizeBytes || generation.recoveryArtifactSize == 0 || generation.recoveryArtifactSize > maxDecisionRecoveryArtifactBytes || uint64(len(artifactBytes)) != generation.recoveryArtifactSize || DigestBytes(artifactBytes) != generation.recoveryArtifactDigest {
		return VerifiedDecisionRecoveryArtifact{}, fail(CodeEvidenceRecoveryRequired, "historical-recovery-artifact", "registered historical recovery input is unavailable or mismatched", nil)
	}
	inputs, err := decodeDecisionRecoveryVerificationInputs(artifactBytes)
	if err != nil || inputs.ProfileDigest != bindings.decisionRecoveryArtifactProfileDigest || inputs.OldRunnerProjectionDecisionDigest != generation.identity.runnerProjectionDecisionDigest {
		return VerifiedDecisionRecoveryArtifact{}, fail(CodeEvidenceRecoveryRequired, "historical-recovery-artifact", "registered historical recovery input is unavailable or mismatched", nil)
	}
	canonical, err := canonicalContractKey(inputs)
	if err != nil || canonical != string(artifactBytes) {
		return VerifiedDecisionRecoveryArtifact{}, fail(CodeEvidenceRecoveryRequired, "historical-recovery-artifact", "registered historical recovery input is not canonical", err)
	}
	return VerifiedDecisionRecoveryArtifact{owner: current.owner, bytes: append([]byte(nil), artifactBytes...), digest: generation.recoveryArtifactDigest, sizeBytes: generation.recoveryArtifactSize, decision: generation.identity.runnerProjectionDecisionDigest}, nil
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
	currentBindings, currentErr := d.decision.runnerProjectionBindings()
	artifactInputs, inputErr := decodeDecisionRecoveryVerificationInputs(artifact.bytes)
	canonical, canonicalErr := canonicalContractKey(artifactInputs)
	oldAuthorized := false
	if currentErr == nil {
		for _, authorization := range currentBindings.verifiedRecoveryPolicy.subject.OldDecisionAuthorizations {
			if authorization.OldRunnerProjectionDecisionDigest == artifact.decision {
				oldAuthorized = true
				break
			}
		}
	}
	if currentErr != nil || !validOwnedCurrentDecision(d, currentBindings) || artifact.owner != d.owner || generation.identity.owner != d.owner.token || artifact.decision == d.digest || artifact.decision != generation.header.RunnerProjectionDecisionDigest || generation.header.Validate() != nil || generation.recoveryArtifactDigest != artifact.digest || generation.recoveryArtifactSize != artifact.sizeBytes || generation.header.DecisionRecoveryArtifactSHA256 != artifact.digest || generation.header.DecisionRecoveryArtifactSizeBytes != artifact.sizeBytes || !sameGenerationHeader(generation.identity, generation.header) || artifact.sizeBytes == 0 || artifact.sizeBytes > maxDecisionRecoveryArtifactBytes || uint64(len(artifact.bytes)) != artifact.sizeBytes || DigestBytes(artifact.bytes) != artifact.digest || inputErr != nil || canonicalErr != nil || canonical != string(artifact.bytes) || artifactInputs.ProfileDigest != currentBindings.decisionRecoveryArtifactProfileDigest || artifactInputs.OldRunnerProjectionDecisionDigest != artifact.decision || !oldAuthorized {
		return OwnedVerifiedDecision{}, RunnerProjectionBindings{}, VerifiedHistoricalRecoveryPolicy{}, fail(CodeEvidenceRecoveryRequired, "decision-recovery", "historical inputs are not owned by the same verifier", nil)
	}
	old, bindings, subject, err := d.owner.verifier.recoverHistoricalDecision(ctx, d.decision, generation, artifact)
	if err != nil {
		return OwnedVerifiedDecision{}, RunnerProjectionBindings{}, VerifiedHistoricalRecoveryPolicy{}, err
	}
	// Historical decisions may be expired. The recovery-only verifier owns the
	// signature/epoch/revocation checks; do not route its result through the
	// ordinary current-unexpired validation path here.
	if old.validateHistorical(bindings) != nil || artifactInputs.RepositoryIdentity != old.repositoryIdentity || artifactInputs.ReleaseIdentity != old.releaseIdentity || subject.OldRunnerProjectionDecisionDigest != artifact.decision || subject.CurrentDecisionMismatch(d.digest) || subject.RecoveryPolicySubjectDigest != currentBindings.recoveryPolicySubjectDigest || subject.ExecutionLineageDigest != generation.identity.executionLineageDigest || subject.OldJournalIdentityDigest != generation.identity.journalIdentityDigest || subject.OldSchemaBundleDigest != generation.identity.schemaBundleDigest || subject.OldDecisionRecoveryArtifactSHA256 != artifact.digest || subject.OldDecisionRecoveryArtifactSizeBytes != artifact.sizeBytes || subject.SuccessorSchemaBundleDigest != d.decision.expectedSchemaBundleDigest || bindings.runnerProjectionDecisionDigest != artifact.decision || bindings.executionLineageDigest != generation.identity.executionLineageDigest || bindings.schemaBundleDigest != generation.identity.schemaBundleDigest || bindings.releaseTrustDecisionDigest != generation.header.ReleaseTrustDecisionDigest || bindings.authorityProfileDigest != generation.header.AuthorityProfileDigest || bindings.authorityBindingDigest != generation.header.AuthorityBindingDigest || old.expectedManifestDigest != generation.header.ManifestDigest || old.expectedOuterArtifactDigest != generation.header.OuterArtifactDigest || old.expectedRunnerReleaseDigest != generation.header.RunnerReleaseDigest {
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
	// Durable content receipts are an external precondition to invoking the
	// historical verifier. Until the content-store publication authority exists,
	// even a self-consistent same-package literal must stop here with zero
	// verifier side effects.
	if !validRuntimeReceipt(plannedRuntimeReceipt, d.owner.token, plannedRuntime.digest, plannedRuntime.sizeBytes) || !validDecisionRecoveryReceipt(plannedRecoveryReceipt, d.owner.token, plannedRecovery.digest, plannedRecovery.sizeBytes) {
		return nil, fail(CodeProjectionNotImplemented, "historical-supersession", "durable content publication authority is not implemented", nil)
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

// loadHistoricalRuntimeBundle is recovery-only. It accepts neither a raw old
// decision nor loose bundle facts: current/old verifier ownership, current
// policy, recovered bindings, generation header, and exact bytes must all
// remain cross-bound before the ordinary runtime bundle can be constructed.
func loadHistoricalRuntimeBundle(current, old OwnedVerifiedDecision, oldBindings RunnerProjectionBindings, policy VerifiedHistoricalRecoveryPolicy, generation GenerationDescriptor, raw []byte) (*RuntimeBundle, error) {
	currentBindings, currentErr := current.decision.runnerProjectionBindings()
	policyDigest, policyErr := policy.subject.ComputeDigest()
	if currentErr != nil || !validOwnedCurrentDecision(current, currentBindings) || old.owner != current.owner || old.capability.owner != current.owner || policy.owner != current.owner || policy.digest == "" || policyErr != nil || policy.digest != policyDigest || generation.identity.owner != current.owner.token || policy.subject.OldRunnerProjectionDecisionDigest != old.digest || policy.subject.SuccessorRunnerProjectionDecisionDigest != current.digest || policy.subject.RecoveryPolicySubjectDigest != currentBindings.recoveryPolicySubjectDigest || policy.subject.ExecutionLineageDigest != generation.identity.executionLineageDigest || policy.subject.OldJournalIdentityDigest != generation.identity.journalIdentityDigest || policy.subject.OldSchemaBundleDigest != generation.identity.schemaBundleDigest || policy.subject.OldDecisionRecoveryArtifactSHA256 != generation.recoveryArtifactDigest || policy.subject.OldDecisionRecoveryArtifactSizeBytes != generation.recoveryArtifactSize || old.decision.validateHistorical(oldBindings) != nil || oldBindings.runnerProjectionDecisionDigest != generation.identity.runnerProjectionDecisionDigest || oldBindings.releaseTrustDecisionDigest != generation.header.ReleaseTrustDecisionDigest || oldBindings.authorityProfileDigest != generation.header.AuthorityProfileDigest || oldBindings.authorityBindingDigest != generation.header.AuthorityBindingDigest || old.decision.expectedOuterArtifactDigest != generation.header.OuterArtifactDigest || old.decision.expectedManifestDigest != generation.header.ManifestDigest || old.decision.expectedSchemaBundleDigest != generation.header.SchemaBundleDigest || old.decision.expectedRunnerReleaseDigest != generation.header.RunnerReleaseDigest {
		return nil, fail(CodeEvidenceRecoveryRequired, "historical-runtime", "historical runtime authority is unavailable or mismatched", nil)
	}
	if generation.header.Validate() != nil || !sameGenerationHeader(generation.identity, generation.header) || len(raw) == 0 || uint64(len(raw)) != generation.header.OuterArtifactSizeBytes || DigestBytes(raw) != generation.header.OuterArtifactDigest {
		return nil, fail(CodeEvidenceJournalCorrupt, "historical-runtime", "registered historical runtime bytes differ from their stored header", nil)
	}
	decoded, err := decodeRuntimeBundleWithManifestCheck(raw, func(manifest *Manifest) error {
		if manifest.ManifestDigest != old.decision.expectedManifestDigest || manifest.SchemaBundleDigest != old.decision.expectedSchemaBundleDigest || manifest.BootstrapBundleDigest != old.decision.expectedBootstrapBundleDigest {
			return fail(CodeEvidenceJournalCorrupt, "historical-runtime", "registered historical runtime manifest differs from its recovered decision", nil)
		}
		return nil
	})
	if err != nil {
		return nil, fail(CodeEvidenceJournalCorrupt, "historical-runtime", "registered historical runtime bundle is invalid", err)
	}
	bundle, err := bindDecodedRuntimeBundle(decoded, generation.header.OuterArtifactDigest, generation.header.OuterArtifactSizeBytes)
	if err != nil {
		return nil, fail(CodeEvidenceJournalCorrupt, "historical-runtime", "registered historical runtime bundle cannot be owned", err)
	}
	facts, err := bundle.quotaFactsForAdmission()
	if err != nil || facts.schemaBundleDigest != generation.header.SchemaBundleDigest || facts.outerArtifactDigest != generation.header.OuterArtifactDigest || facts.outerArtifactSize != generation.header.OuterArtifactSizeBytes {
		return nil, fail(CodeEvidenceJournalCorrupt, "historical-runtime", "registered historical runtime quota closure differs from its generation", err)
	}
	return bundle, nil
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
