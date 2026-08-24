package migration

import "github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"

func bindRegisteredReceiptPair(old OwnedVerifiedDecision, bindings RunnerProjectionBindings, generation GenerationDescriptor, artifact VerifiedDecisionRecoveryArtifact, runtimePublication, recoveryPublication *evidencefs.RegisteredPublication) (VerifiedContentReceipt, VerifiedDecisionRecoveryReceipt, error) {
	if runtimePublication == nil || recoveryPublication == nil || !runtimePublication.SameStore(recoveryPublication) {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, fail(CodeEvidenceRecoveryRequired, "registered-receipt-pair", "registered publications do not share one store", nil)
	}
	runtime, err := bindRegisteredRuntimeReceipt(old, bindings, generation, runtimePublication)
	if err != nil {
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, err
	}
	recovery, err := bindRegisteredDecisionRecoveryReceipt(old, bindings, generation, artifact, recoveryPublication)
	if err != nil {
		verifiedContentReceiptRegistry.Delete(runtime.binding)
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, err
	}
	if !registeredReceiptsSameStore(runtime, recovery) {
		verifiedContentReceiptRegistry.Delete(runtime.binding)
		verifiedDecisionRecoveryReceiptRegistry.Delete(recovery.binding)
		return VerifiedContentReceipt{}, VerifiedDecisionRecoveryReceipt{}, fail(CodeEvidenceRecoveryRequired, "registered-receipt-pair", "registered receipt pair could not be sealed", nil)
	}
	return runtime, recovery, nil
}

// bindRegisteredRuntimeReceipt is recovery-only. Its caller must already have
// recovered the historical decision and loaded the exact registered runtime
// bundle under that same verifier. The distinct RegisteredPublication type can
// never enter the fresh publish/bind receipt constructor.
func bindRegisteredRuntimeReceipt(old OwnedVerifiedDecision, bindings RunnerProjectionBindings, generation GenerationDescriptor, publication *evidencefs.RegisteredPublication) (VerifiedContentReceipt, error) {
	if old.owner == nil || old.owner.token == nil || old.capability.owner != old.owner || old.digest != bindings.runnerProjectionDecisionDigest || old.decision.validateHistorical(bindings) != nil || generation.identity.owner != old.owner.token || generation.identity.runnerProjectionDecisionDigest != old.digest || generation.header.Validate() != nil || !sameGenerationHeader(generation.identity, generation.header) || generation.header.OuterArtifactDigest != old.decision.expectedOuterArtifactDigest || generation.header.OuterArtifactSizeBytes == 0 || generation.header.OuterArtifactSizeBytes > maxRuntimeTarSize || publication == nil || !publication.Matches(digestRaw(generation.header.OuterArtifactDigest), generation.header.OuterArtifactSizeBytes) {
		return VerifiedContentReceipt{}, fail(CodeEvidenceRecoveryRequired, "registered-runtime-receipt", "registered runtime publication authority is unavailable or mismatched", nil)
	}
	binding := &verifiedContentReceiptBinding{
		owner: old.owner.token, kind: durableRuntimeContentObject, digest: generation.header.OuterArtifactDigest,
		sizeBytes: generation.header.OuterArtifactSizeBytes, registeredPublication: publication,
	}
	receipt := VerifiedContentReceipt{
		owner: old.owner.token, kind: binding.kind, digest: binding.digest, sizeBytes: binding.sizeBytes,
		registeredPublication: publication, binding: binding,
	}
	verifiedContentReceiptRegistry.Store(binding, binding)
	if !validRegisteredRuntimeReceipt(receipt, old.owner.token, binding.digest, binding.sizeBytes) {
		verifiedContentReceiptRegistry.Delete(binding)
		return VerifiedContentReceipt{}, fail(CodeEvidenceRecoveryRequired, "registered-runtime-receipt", "registered runtime receipt could not be sealed", nil)
	}
	return receipt, nil
}

func bindRegisteredDecisionRecoveryReceipt(old OwnedVerifiedDecision, bindings RunnerProjectionBindings, generation GenerationDescriptor, artifact VerifiedDecisionRecoveryArtifact, publication *evidencefs.RegisteredPublication) (VerifiedDecisionRecoveryReceipt, error) {
	if !validRegisteredDecisionRecoveryArtifact(old, bindings, generation, artifact) || publication == nil || !publication.Matches(digestRaw(artifact.digest), artifact.sizeBytes) {
		return VerifiedDecisionRecoveryReceipt{}, fail(CodeEvidenceRecoveryRequired, "registered-recovery-receipt", "registered decision-recovery publication authority is unavailable or mismatched", nil)
	}
	binding := &verifiedDecisionRecoveryReceiptBinding{
		owner: old.owner.token, kind: durableDecisionRecoveryContentObject, digest: artifact.digest,
		sizeBytes: artifact.sizeBytes, registeredPublication: publication,
	}
	receipt := VerifiedDecisionRecoveryReceipt{
		owner: old.owner.token, kind: binding.kind, digest: binding.digest, sizeBytes: binding.sizeBytes,
		registeredPublication: publication, binding: binding,
	}
	verifiedDecisionRecoveryReceiptRegistry.Store(binding, binding)
	if !validRegisteredDecisionRecoveryReceipt(receipt, old.owner.token, binding.digest, binding.sizeBytes) {
		verifiedDecisionRecoveryReceiptRegistry.Delete(binding)
		return VerifiedDecisionRecoveryReceipt{}, fail(CodeEvidenceRecoveryRequired, "registered-recovery-receipt", "registered decision-recovery receipt could not be sealed", nil)
	}
	return receipt, nil
}

func validRegisteredDecisionRecoveryArtifact(old OwnedVerifiedDecision, bindings RunnerProjectionBindings, generation GenerationDescriptor, artifact VerifiedDecisionRecoveryArtifact) bool {
	if old.owner == nil || old.owner.token == nil || old.capability.owner != old.owner || old.digest != bindings.runnerProjectionDecisionDigest || old.decision.validateHistorical(bindings) != nil || artifact.owner != old.owner || artifact.decision != old.digest || generation.identity.owner != old.owner.token || generation.identity.runnerProjectionDecisionDigest != old.digest || generation.header.Validate() != nil || !sameGenerationHeader(generation.identity, generation.header) || generation.recoveryArtifactDigest != artifact.digest || generation.recoveryArtifactSize != artifact.sizeBytes || generation.header.DecisionRecoveryArtifactSHA256 != artifact.digest || generation.header.DecisionRecoveryArtifactSizeBytes != artifact.sizeBytes || artifact.sizeBytes == 0 || artifact.sizeBytes > maxDecisionRecoveryArtifactBytes || uint64(len(artifact.bytes)) != artifact.sizeBytes || DigestBytes(artifact.bytes) != artifact.digest {
		return false
	}
	inputs, decodeErr := decodeDecisionRecoveryVerificationInputs(artifact.bytes)
	canonical, canonicalErr := canonicalContractKey(inputs)
	return decodeErr == nil && canonicalErr == nil && canonical == string(artifact.bytes) && inputs.OldRunnerProjectionDecisionDigest == old.digest && inputs.ProfileDigest == bindings.decisionRecoveryArtifactProfileDigest && inputs.RepositoryIdentity == old.decision.repositoryIdentity && inputs.ReleaseIdentity == old.decision.releaseIdentity
}

func validRegisteredRuntimeReceipt(receipt VerifiedContentReceipt, owner *evidenceOwnerToken, digest Digest, size uint64) bool {
	if receipt.owner != owner || owner == nil || receipt.kind != durableRuntimeContentObject || receipt.digest != digest || receipt.sizeBytes != size || receipt.publication != nil || receipt.registeredPublication == nil || receipt.binding == nil || receipt.binding.owner != owner || receipt.binding.kind != receipt.kind || receipt.binding.digest != digest || receipt.binding.sizeBytes != size || receipt.binding.publication != nil || receipt.binding.registeredPublication != receipt.registeredPublication || !receipt.registeredPublication.Matches(digestRaw(digest), size) {
		return false
	}
	registered, ok := verifiedContentReceiptRegistry.Load(receipt.binding)
	return ok && registered == receipt.binding
}

func validRegisteredDecisionRecoveryReceipt(receipt VerifiedDecisionRecoveryReceipt, owner *evidenceOwnerToken, digest Digest, size uint64) bool {
	if receipt.owner != owner || owner == nil || receipt.kind != durableDecisionRecoveryContentObject || receipt.digest != digest || receipt.sizeBytes != size || receipt.publication != nil || receipt.registeredPublication == nil || receipt.binding == nil || receipt.binding.owner != owner || receipt.binding.kind != receipt.kind || receipt.binding.digest != digest || receipt.binding.sizeBytes != size || receipt.binding.publication != nil || receipt.binding.registeredPublication != receipt.registeredPublication || !receipt.registeredPublication.Matches(digestRaw(digest), size) {
		return false
	}
	registered, ok := verifiedDecisionRecoveryReceiptRegistry.Load(receipt.binding)
	return ok && registered == receipt.binding
}

func registeredReceiptsSameStore(runtime VerifiedContentReceipt, recovery VerifiedDecisionRecoveryReceipt) bool {
	return runtime.registeredPublication != nil && recovery.registeredPublication != nil && runtime.publication == nil && recovery.publication == nil && runtime.registeredPublication.SameStore(recovery.registeredPublication)
}
