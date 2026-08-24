package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

type generationPrefixPhysicalState string

const (
	generationPrefixPhysicalAbsent    generationPrefixPhysicalState = "generation_absent"
	generationPrefixPhysicalDirectory generationPrefixPhysicalState = "generation_prefix_directory"
	generationPrefixPhysicalLock      generationPrefixPhysicalState = "generation_prefix_lock"
	generationPrefixPhysicalSegment   generationPrefixPhysicalState = "generation_segment_prefix"
	generationPrefixPhysicalComplete  generationPrefixPhysicalState = "generation_segment_complete"
)

type generationPrefixRecoveryInput struct {
	target, fullSet              [32]byte
	revision                     uint64
	indexDigest, indexIdentity   [32]byte
	indexSize, indexRecords      uint64
	indexTail                    Digest
	reservedFrame                LineageIndexFrame
	activationHeader             ownedActivationHeader
	headerFrame                  EvidenceFrame
	headerBytes                  []byte
	headerBytesDigest            [32]byte
	journal                      Digest
	physical                     generationPrefixPhysicalState
	prefixSize                   uint64
	prefixDigest, prefixIdentity [32]byte
	canonical                    [32]byte
}

// GenerationPrefixRecoveryPermit is the migration/evidencefs composite
// authority for a crash-reopened GenerationReserved whose segment-0 header is
// absent, physically prefixed, or complete but not yet activated. It retains
// same-verifier registered receipts and one current evidencefs mutation token;
// neither the filesystem prefix nor the stored planned header can mint it.
type GenerationPrefixRecoveryPermit struct {
	self             *GenerationPrefixRecoveryPermit
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	input            generationPrefixRecoveryInput
	binding          *generationPrefixRecoveryPermitBinding
	consumed         *atomic.Bool
}

type generationPrefixRecoveryPermitBinding struct {
	permit           *GenerationPrefixRecoveryPermit
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	canonical        [32]byte
}

type generationPrefixRecoveryPermitRegistryRecord struct {
	permit           *GenerationPrefixRecoveryPermit
	binding          *generationPrefixRecoveryPermitBinding
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	handoffConsumed  *atomic.Bool
	permitConsumed   *atomic.Bool
	runtimeBinding   *verifiedContentReceiptBinding
	recoveryBinding  *verifiedDecisionRecoveryReceiptBinding
	historyCanonical [32]byte
	sourceCanonical  [32]byte
	inputCanonical   [32]byte
	canonical        [32]byte
}

var generationPrefixRecoveryPermitRegistry sync.Map

// RecoveredHeaderDurablePermit proves that the exact stored planned header is
// durable under the retained generation lock. Its only mutation consumer is
// the adjacent activation bridge; it is not itself runtime, DB, cursor,
// handoff, or session authority.
type RecoveredHeaderDurablePermit struct {
	self             *RecoveredHeaderDurablePermit
	prior            *GenerationPrefixRecoveryPermit
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	input            generationPrefixRecoveryInput
	fsResult         evidencefs.AdmissionJournalTransitionResult
	fullSet          [32]byte
	revision         uint64
	binding          *recoveredHeaderDurablePermitBinding
	consumed         *atomic.Bool
}

type recoveredHeaderDurablePermitBinding struct {
	permit           *RecoveredHeaderDurablePermit
	prior            *GenerationPrefixRecoveryPermit
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	canonical        [32]byte
}

type recoveredHeaderDurablePermitRegistryRecord struct {
	permit           *RecoveredHeaderDurablePermit
	binding          *recoveredHeaderDurablePermitBinding
	prior            *GenerationPrefixRecoveryPermit
	history          *VerifiedAdmissionHistory
	registered       *verifiedAdmissionRegisteredGeneration
	candidateBinding *verifiedEvidenceRunBinding
	inventory        *evidencefs.AdmissionInventory
	mutation         *evidencefs.AdmissionMutationToken
	permitConsumed   *atomic.Bool
	nextConsumed     *atomic.Bool
	runtimeBinding   *verifiedContentReceiptBinding
	recoveryBinding  *verifiedDecisionRecoveryReceiptBinding
	priorCanonical   [32]byte
	inputCanonical   [32]byte
	canonical        [32]byte
}

var recoveredHeaderDurablePermitRegistry sync.Map

// GenerationPrefixRecoveryTransitionResult is a closed result. Unknown never
// carries continuing authority; Durable alone returns a header-durable permit.
type GenerationPrefixRecoveryTransitionResult struct {
	outcome            evidencefs.AdmissionTransitionOutcome
	next               *RecoveredHeaderDurablePermit
	candidateDigest    [32]byte
	candidateSequence  uint64
	candidateRevision  uint64
	previousRevision   uint64
	journal            Digest
	headerRecordDigest Digest
	headerBytesDigest  [32]byte
	headerSize         uint64
}

func (r GenerationPrefixRecoveryTransitionResult) Outcome() evidencefs.AdmissionTransitionOutcome {
	return r.outcome
}
func (r GenerationPrefixRecoveryTransitionResult) Next() *RecoveredHeaderDurablePermit {
	return r.next
}
func (r GenerationPrefixRecoveryTransitionResult) CandidateKind() string {
	return "generation_header_recovery"
}
func (r GenerationPrefixRecoveryTransitionResult) CandidateDigest() [32]byte {
	return r.candidateDigest
}
func (r GenerationPrefixRecoveryTransitionResult) CandidateSequence() uint64 {
	return r.candidateSequence
}
func (r GenerationPrefixRecoveryTransitionResult) CandidateRevision() uint64 {
	return r.candidateRevision
}
func (r GenerationPrefixRecoveryTransitionResult) PreviousRevision() uint64 {
	return r.previousRevision
}
func (r GenerationPrefixRecoveryTransitionResult) Journal() Digest { return r.journal }
func (r GenerationPrefixRecoveryTransitionResult) HeaderRecordDigest() Digest {
	return r.headerRecordDigest
}
func (r GenerationPrefixRecoveryTransitionResult) HeaderBytesDigest() [32]byte {
	return r.headerBytesDigest
}
func (r GenerationPrefixRecoveryTransitionResult) HeaderSize() uint64 { return r.headerSize }

func bindGenerationPrefixRecoveryPermit(ctx context.Context, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate) (*GenerationPrefixRecoveryPermit, error) {
	if !validVerifiedAdmissionHistory(history, candidate) || history.targetGeneration == nil || history.targetGeneration.replay != nil || !generationPrefixRecoveryState(history.targetState) {
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-prefix-recovery-bind", "verified reserved generation authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if err := history.inventory.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "generation-prefix-recovery-bind-revalidate")
	}
	input, err := inspectGenerationPrefixRecoveryInput(ctx, history, history.targetGeneration, candidate, false)
	if err != nil {
		return nil, err
	}
	registered := history.targetGeneration
	if registered.handoffConsumed == nil || !registered.handoffConsumed.CompareAndSwap(false, true) {
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-prefix-recovery-bind", "verified reserved generation authority is consumed", nil)
	}
	token, err := history.inventory.MutationToken()
	if err != nil {
		registered.handoffConsumed.CompareAndSwap(true, false)
		return nil, mapEvidenceAdmissionError(err, "generation-prefix-recovery-bind-token")
	}
	permit := &GenerationPrefixRecoveryPermit{
		history: history, registered: registered, candidateBinding: candidate.binding,
		inventory: history.inventory, mutation: token, input: cloneGenerationPrefixRecoveryInput(input), consumed: &atomic.Bool{},
	}
	permit.self = permit
	permit.binding = &generationPrefixRecoveryPermitBinding{
		permit: permit, history: history, registered: registered, candidateBinding: candidate.binding,
		inventory: history.inventory, mutation: token,
	}
	permit.binding.canonical = generationPrefixRecoveryPermitDigest(permit)
	generationPrefixRecoveryPermitRegistry.Store(permit, generationPrefixRecoveryPermitRegistryRecord{
		permit: permit, binding: permit.binding, history: history, registered: registered,
		candidateBinding: candidate.binding, inventory: history.inventory, mutation: token,
		handoffConsumed: registered.handoffConsumed, permitConsumed: permit.consumed,
		runtimeBinding: registered.runtimeReceipt.binding, recoveryBinding: registered.recoveryReceipt.binding,
		historyCanonical: history.binding.canonical, sourceCanonical: registered.canonical,
		inputCanonical: input.canonical, canonical: permit.binding.canonical,
	})
	if !validGenerationPrefixRecoveryPermit(permit, candidate) {
		generationPrefixRecoveryPermitRegistry.Delete(permit)
		revokeVerifiedAdmissionRegisteredGeneration(registered)
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-prefix-recovery-bind", "generation prefix recovery permit could not be sealed", nil)
	}
	return permit, nil
}

// RecoverGenerationHeader consumes the exact verified prefix permit. Physical
// absence uses the no-replace create path; every observed prefix or complete
// header uses the idempotent recovery path that replays durability barriers.
func (p *GenerationPrefixRecoveryPermit) RecoverGenerationHeader(ctx context.Context, candidate OwnedCurrentCandidate) (GenerationPrefixRecoveryTransitionResult, error) {
	pre := GenerationPrefixRecoveryTransitionResult{outcome: evidencefs.AdmissionTransitionPreMutationFailure, candidateSequence: 7}
	if p == nil || !validGenerationPrefixRecoveryPermit(p, candidate) {
		return pre, fail(CodeEvidenceRecoveryRequired, "generation-prefix-recovery", "generation prefix recovery permit is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return pre, err
	}
	pre.previousRevision = p.input.revision
	pre.candidateRevision = p.input.revision + 1
	pre.journal = p.input.journal
	pre.headerRecordDigest = p.input.headerFrame.RecordDigest
	pre.headerBytesDigest = p.input.headerBytesDigest
	pre.headerSize = uint64(len(p.input.headerBytes))
	if err := p.inventory.Revalidate(ctx); err != nil {
		mapped := mapEvidenceAdmissionError(err, "generation-prefix-recovery-revalidate")
		if !generationPrefixContextError(mapped) {
			failGenerationPrefixRecoveryPermit(p)
		}
		return pre, mapped
	}
	current, err := inspectGenerationPrefixRecoveryInput(ctx, p.history, p.registered, candidate, true)
	if err != nil {
		if !generationPrefixContextError(err) {
			failGenerationPrefixRecoveryPermit(p)
		}
		return pre, err
	}
	if current.canonical != p.input.canonical {
		failGenerationPrefixRecoveryPermit(p)
		return pre, fail(CodeEvidenceRecoveryRequired, "generation-prefix-recovery", "verified generation prefix changed before recovery", nil)
	}
	if !p.consumed.CompareAndSwap(false, true) {
		return pre, fail(CodeEvidenceRecoveryRequired, "generation-prefix-recovery", "generation prefix recovery permit is consumed", nil)
	}
	var fsResult evidencefs.AdmissionJournalTransitionResult
	var transitionErr error
	if p.input.physical == generationPrefixPhysicalAbsent {
		fsResult, transitionErr = p.mutation.CreateGenerationHeader(ctx, p.inventory, digestRaw(p.input.journal), p.input.headerBytes)
	} else {
		fsResult, transitionErr = p.mutation.RecoverGenerationHeader(ctx, p.inventory, digestRaw(p.input.journal), p.input.headerBytes)
	}
	result := GenerationPrefixRecoveryTransitionResult{
		outcome: fsResult.Outcome(), candidateDigest: fsResult.CandidateDigest(), candidateSequence: 7,
		candidateRevision: fsResult.CandidateRevision(), previousRevision: fsResult.PreviousRevision(),
		journal: p.input.journal, headerRecordDigest: p.input.headerFrame.RecordDigest,
		headerBytesDigest: p.input.headerBytesDigest, headerSize: uint64(len(p.input.headerBytes)),
	}
	if fsResult.Outcome() != evidencefs.AdmissionTransitionDurable {
		mapped := mapAdmissionMutationError(transitionErr, "generation-prefix-recovery")
		if mapped == nil {
			mapped = admissionFailed("generation-prefix-recovery", "filesystem transition returned no durable authority", nil)
		}
		if fsResult.Outcome() == evidencefs.AdmissionTransitionPreMutationFailure {
			if generationPrefixContextError(mapped) {
				p.consumed.CompareAndSwap(true, false)
			} else {
				failGenerationPrefixRecoveryPermit(p)
			}
		} else {
			failGenerationPrefixRecoveryPermit(p)
		}
		return result, mapped
	}
	if transitionErr != nil || fsResult.CandidateKind() != "generation_header" || fsResult.CandidateSequence() != 0 || fsResult.CandidateDigest() == ([32]byte{}) || fsResult.PreviousRevision() != pre.previousRevision || fsResult.CandidateRevision() != pre.candidateRevision || fsResult.Journal() != digestRaw(p.input.journal) || fsResult.HeaderDigest() != p.input.headerBytesDigest || fsResult.HeaderSize() != uint64(len(p.input.headerBytes)) || fsResult.Inventory() == nil || !fsResult.ValidFor(fsResult.Inventory()) {
		_ = fsResult.Invalidate()
		failGenerationPrefixRecoveryPermit(p)
		return generationPrefixRecoveryUnknown(result), admissionPostMutationFailure("generation-prefix-recovery-result")
	}
	nextInventory := fsResult.Inventory()
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		failGenerationPrefixRecoveryPermit(p)
		return generationPrefixRecoveryUnknown(result), admissionPostMutationFailure("generation-prefix-recovery-revalidate")
	}
	revision, revisionErr := nextInventory.Revision()
	target, targetErr := nextInventory.Target()
	fullSet, fullSetErr := nextInventory.FullSetDigest()
	if revisionErr != nil || targetErr != nil || fullSetErr != nil || revision != pre.candidateRevision || target != p.input.target || fullSet == ([32]byte{}) || fullSet == p.input.fullSet {
		_ = fsResult.Invalidate()
		failGenerationPrefixRecoveryPermit(p)
		return generationPrefixRecoveryUnknown(result), admissionPostMutationFailure("generation-prefix-recovery-boundary")
	}
	if err := validateRecoveredHeaderInventory(ctx, nextInventory, p.input); err != nil {
		_ = fsResult.Invalidate()
		failGenerationPrefixRecoveryPermit(p)
		return generationPrefixRecoveryUnknown(result), admissionPostMutationFailure("generation-prefix-recovery-inventory")
	}
	if !validRegisteredRuntimeReceipt(p.registered.runtimeReceipt, p.registered.descriptor.identity.owner, p.registered.descriptor.header.OuterArtifactDigest, p.registered.descriptor.header.OuterArtifactSizeBytes) || !validRegisteredDecisionRecoveryReceipt(p.registered.recoveryReceipt, p.registered.descriptor.identity.owner, p.registered.descriptor.header.DecisionRecoveryArtifactSHA256, p.registered.descriptor.header.DecisionRecoveryArtifactSizeBytes) || !registeredReceiptsSameStore(p.registered.runtimeReceipt, p.registered.recoveryReceipt) {
		_ = fsResult.Invalidate()
		failGenerationPrefixRecoveryPermit(p)
		return generationPrefixRecoveryUnknown(result), admissionPostMutationFailure("generation-prefix-recovery-receipts")
	}
	if err := nextInventory.Revalidate(ctx); err != nil {
		_ = fsResult.Invalidate()
		failGenerationPrefixRecoveryPermit(p)
		return generationPrefixRecoveryUnknown(result), admissionPostMutationFailure("generation-prefix-recovery-terminal")
	}
	nextToken, err := nextInventory.MutationToken()
	if err != nil || !nextToken.ValidFor(nextInventory) {
		_ = fsResult.Invalidate()
		failGenerationPrefixRecoveryPermit(p)
		return generationPrefixRecoveryUnknown(result), admissionPostMutationFailure("generation-prefix-recovery-token")
	}
	next := &RecoveredHeaderDurablePermit{
		prior: p, history: p.history, registered: p.registered, candidateBinding: candidate.binding,
		inventory: nextInventory, mutation: nextToken, input: cloneGenerationPrefixRecoveryInput(p.input),
		fsResult: fsResult, fullSet: fullSet, revision: revision, consumed: &atomic.Bool{},
	}
	next.self = next
	next.binding = &recoveredHeaderDurablePermitBinding{
		permit: next, prior: p, history: p.history, registered: p.registered,
		candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
	}
	next.binding.canonical = recoveredHeaderDurablePermitDigest(next)
	recoveredHeaderDurablePermitRegistry.Store(next, recoveredHeaderDurablePermitRegistryRecord{
		permit: next, binding: next.binding, prior: p, history: p.history, registered: p.registered,
		candidateBinding: candidate.binding, inventory: nextInventory, mutation: nextToken,
		permitConsumed: p.consumed, nextConsumed: next.consumed,
		runtimeBinding: p.registered.runtimeReceipt.binding, recoveryBinding: p.registered.recoveryReceipt.binding,
		priorCanonical: p.binding.canonical, inputCanonical: p.input.canonical, canonical: next.binding.canonical,
	})
	if !validRecoveredHeaderDurablePermit(next, candidate) {
		recoveredHeaderDurablePermitRegistry.Delete(next)
		_ = fsResult.Invalidate()
		failGenerationPrefixRecoveryPermit(p)
		return generationPrefixRecoveryUnknown(result), admissionPostMutationFailure("generation-prefix-recovery-seal")
	}
	generationPrefixRecoveryPermitRegistry.Delete(p)
	result.next = next
	return result, nil
}

func inspectGenerationPrefixRecoveryInput(ctx context.Context, history *VerifiedAdmissionHistory, registered *verifiedAdmissionRegisteredGeneration, candidate OwnedCurrentCandidate, consumed bool) (generationPrefixRecoveryInput, error) {
	var zero generationPrefixRecoveryInput
	validHistory := validVerifiedAdmissionHistory(history, candidate)
	if consumed {
		validHistory = validConsumedGenerationPrefixHistory(history, registered, candidate)
	}
	if !validHistory || history.targetGeneration != registered || registered == nil || registered.replay != nil || !generationPrefixRecoveryState(history.targetState) {
		return zero, fail(CodeEvidenceRecoveryRequired, "generation-prefix-recovery-input", "verified reserved generation input is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return zero, err
	}
	lineage, err := history.inventory.Lineage(history.target)
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "generation-prefix-recovery-lineage")
	}
	lineageID, lineageIDErr := lineage.ID()
	index, indexErr := lineage.Index()
	journals, journalsErr := lineage.Journals()
	registrations, registrationsErr := lineage.GenerationRegistrations()
	absent, absentErr := history.inventory.TargetAbsent()
	for _, accessorErr := range []error{lineageIDErr, indexErr, journalsErr, registrationsErr, absentErr} {
		if accessorErr != nil {
			return zero, mapEvidenceAdmissionError(accessorErr, "generation-prefix-recovery-inventory")
		}
	}
	if lineageID != history.target || absent != nil {
		return zero, admissionCorrupt("generation-prefix-recovery-input", "target lineage binding is invalid", nil)
	}
	indexRaw, err := index.ReadAll(ctx)
	if err != nil {
		return zero, mapEvidenceAdmissionError(err, "generation-prefix-recovery-index-read")
	}
	indexSize, sizeErr := index.Size()
	indexDigest, digestErr := index.Digest()
	indexIdentity, identityErr := index.IdentityDigest()
	for _, accessorErr := range []error{sizeErr, digestErr, identityErr} {
		if accessorErr != nil {
			return zero, mapEvidenceAdmissionError(accessorErr, "generation-prefix-recovery-index")
		}
	}
	frames, err := decodeAdmissionLineageFrames(indexRaw)
	if err != nil || len(frames) == 0 {
		return zero, admissionCorrupt("generation-prefix-recovery-index", "target index cannot be decoded", err)
	}
	if _, err := scanLineageChainStructure(frames); err != nil {
		return zero, admissionCorrupt("generation-prefix-recovery-index", "target index structure is invalid", err)
	}
	reservedFrame := cloneProjectionValue(frames[len(frames)-1])
	if reservedFrame.RecordKind != LineageRecordGenerationReserved || reservedFrame.Record.Reserved == nil || reservedFrame.Sequence != uint64(len(frames)-1) || reservedFrame.RecordDigest != history.targetIndexTail || uint64(len(frames)) != history.targetIndexRecords || indexSize != uint64(len(indexRaw)) || indexDigest != sha256.Sum256(indexRaw) || indexDigest == ([32]byte{}) || indexIdentity == ([32]byte{}) {
		return zero, admissionCorrupt("generation-prefix-recovery-index", "target index tail is not the exact durable reservation", nil)
	}
	reserved := cloneProjectionValue(*reservedFrame.Record.Reserved)
	descriptor := registered.descriptor
	if reserved.Validate() != nil || descriptor.header.Validate() != nil || reserved.ExecutionLineageDigest != descriptor.identity.executionLineageDigest || reserved.JournalIdentityDigest != descriptor.identity.journalIdentityDigest || reserved.RunnerProjectionDecisionDigest != descriptor.identity.runnerProjectionDecisionDigest || reserved.SchemaBundleDigest != descriptor.identity.schemaBundleDigest || !canonicalEqual(reserved.PlannedSegment0Header, descriptor.header) || reserved.ExpectedSegment0HeaderDigest != descriptor.replayTailDigest {
		return zero, admissionCorrupt("generation-prefix-recovery-index", "durable reservation differs from recovered generation authority", nil)
	}
	activationHeader, err := bindRegisteredActivationHeaderForOperation("generation-prefix-recovery-header", descriptor.identity, reserved, registered.runtimeReceipt, registered.recoveryReceipt)
	if err != nil {
		return zero, err
	}
	headerFrame, headerBytes, err := encodeAdmissionActivationHeader(activationHeader)
	if err != nil || headerFrame.RecordDigest != reserved.ExpectedSegment0HeaderDigest || len(headerBytes) == 0 {
		return zero, admissionCorrupt("generation-prefix-recovery-header", "planned activation header cannot be reconstructed", err)
	}
	input := generationPrefixRecoveryInput{
		target: history.target, fullSet: history.fullSet, revision: history.revision,
		indexDigest: indexDigest, indexIdentity: indexIdentity, indexSize: indexSize,
		indexRecords: uint64(len(frames)), indexTail: reservedFrame.RecordDigest,
		reservedFrame: reservedFrame, activationHeader: activationHeader,
		headerFrame: headerFrame, headerBytes: append([]byte(nil), headerBytes...),
		headerBytesDigest: sha256.Sum256(headerBytes), journal: reserved.JournalIdentityDigest,
		physical: generationPrefixPhysicalAbsent,
	}
	journalRaw := digestRaw(input.journal)
	matchingFacts := 0
	for _, fact := range registrations {
		state, stateErr := fact.State()
		lineageValue, lineageErr := fact.Lineage()
		journalValue, journalErr := fact.Journal()
		fullSet, fullSetErr := fact.FullSetDigest()
		for _, accessorErr := range []error{stateErr, lineageErr, journalErr, fullSetErr} {
			if accessorErr != nil {
				return zero, mapEvidenceAdmissionError(accessorErr, "generation-prefix-recovery-fact")
			}
		}
		if journalValue != journalRaw {
			continue
		}
		matchingFacts++
		if lineageValue != history.target || fullSet != history.fullSet {
			return zero, admissionCorrupt("generation-prefix-recovery-fact", "generation prefix fact binding differs", nil)
		}
		switch state {
		case evidencefs.GenerationRegistrationPrefixDirectory:
			input.physical = generationPrefixPhysicalDirectory
		case evidencefs.GenerationRegistrationPrefixLock:
			input.physical = generationPrefixPhysicalLock
		default:
			return zero, admissionCorrupt("generation-prefix-recovery-fact", "generation prefix fact state is invalid", nil)
		}
	}
	matchingJournals := 0
	for _, journal := range journals {
		journalID, err := journal.ID()
		if err != nil {
			return zero, mapEvidenceAdmissionError(err, "generation-prefix-recovery-journal")
		}
		if journalID != journalRaw {
			continue
		}
		matchingJournals++
		segments, err := journal.Segments()
		if err != nil {
			return zero, mapEvidenceAdmissionError(err, "generation-prefix-recovery-segments")
		}
		if len(segments) != 1 {
			return zero, admissionCorrupt("generation-prefix-recovery-journal", "recoverable generation does not have one segment", nil)
		}
		ordinal, ordinalErr := segments[0].Ordinal()
		prefixSize, prefixSizeErr := segments[0].Size()
		prefixDigest, prefixDigestErr := segments[0].Digest()
		prefixIdentity, prefixIdentityErr := segments[0].IdentityDigest()
		prefix, readErr := segments[0].ReadAll(ctx)
		for _, accessorErr := range []error{ordinalErr, prefixSizeErr, prefixDigestErr, prefixIdentityErr, readErr} {
			if accessorErr != nil {
				return zero, mapEvidenceAdmissionError(accessorErr, "generation-prefix-recovery-segment")
			}
		}
		if ordinal != 0 || prefixSize != uint64(len(prefix)) || prefixDigest != sha256.Sum256(prefix) || prefixIdentity == ([32]byte{}) || len(prefix) > len(headerBytes) || !bytes.Equal(prefix, headerBytes[:len(prefix)]) {
			return zero, admissionCorrupt("generation-prefix-recovery-segment", "segment-0 is not an exact planned-header prefix", nil)
		}
		input.prefixSize, input.prefixDigest, input.prefixIdentity = prefixSize, prefixDigest, prefixIdentity
		if len(prefix) == len(headerBytes) {
			input.physical = generationPrefixPhysicalComplete
		} else {
			input.physical = generationPrefixPhysicalSegment
		}
	}
	if matchingFacts > 1 || matchingJournals > 1 || matchingFacts != 0 && matchingJournals != 0 {
		return zero, admissionCorrupt("generation-prefix-recovery-input", "generation prefix identity is duplicated", nil)
	}
	if history.targetState == admissionLineageReservedHeader {
		if input.physical != generationPrefixPhysicalComplete {
			return zero, admissionCorrupt("generation-prefix-recovery-input", "header-unactivated state has no exact header", nil)
		}
	} else if input.physical == generationPrefixPhysicalComplete {
		return zero, admissionCorrupt("generation-prefix-recovery-input", "reserved-no-header state already has a complete header", nil)
	}
	if err := history.inventory.Revalidate(ctx); err != nil {
		return zero, mapEvidenceAdmissionError(err, "generation-prefix-recovery-terminal-revalidate")
	}
	input.canonical = generationPrefixRecoveryInputDigest(input)
	if input.canonical == ([32]byte{}) {
		return zero, admissionCorrupt("generation-prefix-recovery-input", "generation prefix recovery input is invalid", nil)
	}
	return input, nil
}

func validateRecoveredHeaderInventory(ctx context.Context, inventory *evidencefs.AdmissionInventory, input generationPrefixRecoveryInput) error {
	if inventory == nil || input.canonical == ([32]byte{}) || input.canonical != generationPrefixRecoveryInputDigest(input) {
		return admissionCorrupt("generation-prefix-recovery-inventory", "header inventory expectation is invalid", nil)
	}
	lineage, err := inventory.Lineage(input.target)
	if err != nil {
		return mapEvidenceAdmissionError(err, "generation-prefix-recovery-lineage")
	}
	lineageID, lineageIDErr := lineage.ID()
	index, indexErr := lineage.Index()
	journals, journalsErr := lineage.Journals()
	registrations, registrationsErr := lineage.GenerationRegistrations()
	absent, absentErr := inventory.TargetAbsent()
	for _, accessorErr := range []error{lineageIDErr, indexErr, journalsErr, registrationsErr, absentErr} {
		if accessorErr != nil {
			return mapEvidenceAdmissionError(accessorErr, "generation-prefix-recovery-inventory")
		}
	}
	if lineageID != input.target || absent != nil {
		return admissionCorrupt("generation-prefix-recovery-inventory", "target lineage changed after header recovery", nil)
	}
	indexRaw, err := index.ReadAll(ctx)
	if err != nil {
		return mapEvidenceAdmissionError(err, "generation-prefix-recovery-index-read")
	}
	indexSize, sizeErr := index.Size()
	indexDigest, digestErr := index.Digest()
	indexIdentity, identityErr := index.IdentityDigest()
	for _, accessorErr := range []error{sizeErr, digestErr, identityErr} {
		if accessorErr != nil {
			return mapEvidenceAdmissionError(accessorErr, "generation-prefix-recovery-index")
		}
	}
	if indexSize != input.indexSize || indexDigest != input.indexDigest || indexIdentity != input.indexIdentity || uint64(len(indexRaw)) != input.indexSize || sha256.Sum256(indexRaw) != input.indexDigest {
		return admissionCorrupt("generation-prefix-recovery-index", "durable reservation index changed during header recovery", nil)
	}
	frames, err := decodeAdmissionLineageFrames(indexRaw)
	if err != nil || uint64(len(frames)) != input.indexRecords || len(frames) == 0 || !canonicalEqual(frames[len(frames)-1], input.reservedFrame) {
		return admissionCorrupt("generation-prefix-recovery-index", "durable reservation tail changed during header recovery", err)
	}
	matchingFacts, matchingJournals := 0, 0
	for _, fact := range registrations {
		journal, err := fact.Journal()
		if err != nil {
			return mapEvidenceAdmissionError(err, "generation-prefix-recovery-fact")
		}
		if journal == digestRaw(input.journal) {
			matchingFacts++
		}
	}
	for _, journal := range journals {
		journalID, err := journal.ID()
		if err != nil {
			return mapEvidenceAdmissionError(err, "generation-prefix-recovery-journal")
		}
		if journalID != digestRaw(input.journal) {
			continue
		}
		matchingJournals++
		segments, err := journal.Segments()
		if err != nil {
			return mapEvidenceAdmissionError(err, "generation-prefix-recovery-segments")
		}
		if len(segments) != 1 {
			return admissionCorrupt("generation-prefix-recovery-segments", "recovered generation does not have one segment", nil)
		}
		ordinal, ordinalErr := segments[0].Ordinal()
		size, sizeErr := segments[0].Size()
		digest, digestErr := segments[0].Digest()
		raw, readErr := segments[0].ReadAll(ctx)
		for _, accessorErr := range []error{ordinalErr, sizeErr, digestErr, readErr} {
			if accessorErr != nil {
				return mapEvidenceAdmissionError(accessorErr, "generation-prefix-recovery-segment")
			}
		}
		if ordinal != 0 || size != uint64(len(input.headerBytes)) || digest != input.headerBytesDigest || !bytes.Equal(raw, input.headerBytes) {
			return admissionCorrupt("generation-prefix-recovery-segment", "recovered segment differs from exact planned header", nil)
		}
	}
	if matchingFacts != 0 || matchingJournals != 1 {
		return admissionCorrupt("generation-prefix-recovery-inventory", "recovered header did not replace the exact prefix", nil)
	}
	return nil
}

func generationPrefixRecoveryState(state admissionReplayLineageState) bool {
	return state == admissionLineageReservedUnregistered || state == admissionLineageReservedHeader
}

func generationPrefixContextError(err error) bool {
	return IsCode(err, CodeContextCanceled) || IsCode(err, CodeDeadlineExceeded)
}

func generationPrefixRecoveryUnknown(value GenerationPrefixRecoveryTransitionResult) GenerationPrefixRecoveryTransitionResult {
	value.outcome, value.next = evidencefs.AdmissionTransitionUnknown, nil
	return value
}

func cloneGenerationPrefixRecoveryInput(value generationPrefixRecoveryInput) generationPrefixRecoveryInput {
	owned := value
	owned.reservedFrame = cloneProjectionValue(value.reservedFrame)
	owned.activationHeader.header = cloneProjectionValue(value.activationHeader.header)
	owned.activationHeader.reserved = cloneProjectionValue(value.activationHeader.reserved)
	owned.headerFrame = cloneProjectionValue(value.headerFrame)
	owned.headerBytes = append([]byte(nil), value.headerBytes...)
	return owned
}

func generationPrefixRecoveryInputDigest(input generationPrefixRecoveryInput) [32]byte {
	headerBytes, headerErr := EncodeCanonicalEvidenceFrame(input.headerFrame)
	if input.target == ([32]byte{}) || input.fullSet == ([32]byte{}) || input.indexDigest == ([32]byte{}) || input.indexIdentity == ([32]byte{}) || input.indexSize == 0 || input.indexRecords == 0 || input.indexTail.Validate() != nil || input.reservedFrame.Validate() != nil || input.reservedFrame.Record.Reserved == nil || input.reservedFrame.RecordDigest != input.indexTail || input.headerFrame.Validate() != nil || input.headerFrame.Record.Header == nil || len(input.headerBytes) == 0 || input.headerBytesDigest != sha256.Sum256(input.headerBytes) || headerErr != nil || !bytes.Equal(headerBytes, input.headerBytes) || input.journal.Validate() != nil || input.headerFrame.RecordDigest != input.reservedFrame.Record.Reserved.ExpectedSegment0HeaderDigest || !canonicalEqual(*input.headerFrame.Record.Header, input.reservedFrame.Record.Reserved.PlannedSegment0Header) || !canonicalEqual(input.activationHeader.header, *input.headerFrame.Record.Header) || !canonicalEqual(input.activationHeader.reserved, *input.reservedFrame.Record.Reserved) || !sameGenerationHeader(input.activationHeader.generation, input.activationHeader.header) || !validGenerationPrefixPhysicalInput(input) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-prefix-recovery-input/v1\x00"))
	h.Write(input.target[:])
	h.Write(input.fullSet[:])
	h.Write(input.indexDigest[:])
	h.Write(input.indexIdentity[:])
	writeAdmissionUint(h, input.revision)
	writeAdmissionUint(h, input.indexSize)
	writeAdmissionUint(h, input.indexRecords)
	writeAdmissionString(h, input.indexTail.String())
	writeAdmissionString(h, input.journal.String())
	writeAdmissionString(h, string(input.physical))
	writeAdmissionUint(h, input.prefixSize)
	h.Write(input.prefixDigest[:])
	h.Write(input.prefixIdentity[:])
	h.Write(input.headerBytesDigest[:])
	writeAdmissionString(h, string(input.headerBytes))
	for _, value := range []any{input.reservedFrame, input.headerFrame, input.activationHeader.header, input.activationHeader.reserved} {
		canonical, err := canonicalContractKey(value)
		if err != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	for _, digest := range []Digest{
		input.activationHeader.generation.executionLineageDigest,
		input.activationHeader.generation.journalIdentityDigest,
		input.activationHeader.generation.runnerProjectionDecisionDigest,
		input.activationHeader.generation.schemaBundleDigest,
	} {
		writeAdmissionString(h, digest.String())
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validGenerationPrefixPhysicalInput(input generationPrefixRecoveryInput) bool {
	zeroDigest := [32]byte{}
	switch input.physical {
	case generationPrefixPhysicalAbsent, generationPrefixPhysicalDirectory, generationPrefixPhysicalLock:
		return input.prefixSize == 0 && input.prefixDigest == zeroDigest && input.prefixIdentity == zeroDigest
	case generationPrefixPhysicalSegment:
		return input.prefixSize < uint64(len(input.headerBytes)) && input.prefixDigest != zeroDigest && input.prefixIdentity != zeroDigest
	case generationPrefixPhysicalComplete:
		return input.prefixSize == uint64(len(input.headerBytes)) && input.prefixDigest == input.headerBytesDigest && input.prefixIdentity != zeroDigest
	default:
		return false
	}
}

func generationPrefixRecoveryPermitDigest(permit *GenerationPrefixRecoveryPermit) [32]byte {
	if permit == nil || permit.self != permit || permit.history == nil || permit.registered == nil || permit.candidateBinding == nil || permit.inventory == nil || permit.mutation == nil || permit.binding == nil || permit.history.binding == nil || permit.input.canonical == ([32]byte{}) || permit.input.canonical != generationPrefixRecoveryInputDigest(permit.input) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-prefix-recovery-permit/v1\x00"))
	h.Write(permit.history.binding.canonical[:])
	h.Write(permit.registered.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.input.canonical[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func recoveredHeaderDurablePermitDigest(permit *RecoveredHeaderDurablePermit) [32]byte {
	if permit == nil || permit.self != permit || permit.prior == nil || permit.prior.binding == nil || permit.history == nil || permit.registered == nil || permit.candidateBinding == nil || permit.inventory == nil || permit.mutation == nil || permit.binding == nil || permit.input.canonical == ([32]byte{}) || permit.input.canonical != generationPrefixRecoveryInputDigest(permit.input) || permit.fullSet == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-recovered-header-durable-permit/v1\x00"))
	h.Write(permit.prior.binding.canonical[:])
	h.Write(permit.history.binding.canonical[:])
	h.Write(permit.registered.canonical[:])
	h.Write(permit.candidateBinding.canonical[:])
	h.Write(permit.input.canonical[:])
	h.Write(permit.fullSet[:])
	writeAdmissionUint(h, permit.revision)
	writeAdmissionString(h, string(permit.fsResult.Outcome()))
	writeAdmissionString(h, permit.fsResult.CandidateKind())
	fsCandidate := permit.fsResult.CandidateDigest()
	fsJournal := permit.fsResult.Journal()
	fsHeader := permit.fsResult.HeaderDigest()
	h.Write(fsCandidate[:])
	h.Write(fsJournal[:])
	h.Write(fsHeader[:])
	writeAdmissionUint(h, permit.fsResult.CandidateSequence())
	writeAdmissionUint(h, permit.fsResult.PreviousRevision())
	writeAdmissionUint(h, permit.fsResult.CandidateRevision())
	writeAdmissionUint(h, permit.fsResult.HeaderSize())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validGenerationPrefixRecoveryPermit(permit *GenerationPrefixRecoveryPermit, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.history == nil || permit.registered == nil || permit.history.targetGeneration != permit.registered || permit.candidateBinding != candidate.binding || permit.binding.history != permit.history || permit.binding.registered != permit.registered || permit.binding.candidateBinding != candidate.binding || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.inventory != permit.history.inventory || permit.mutation == nil || permit.consumed == nil || permit.consumed.Load() || !permit.mutation.ValidFor(permit.inventory) || !validConsumedGenerationPrefixHistory(permit.history, permit.registered, candidate) || permit.input.target != permit.history.target || permit.input.fullSet != permit.history.fullSet || permit.input.revision != permit.history.revision || permit.input.canonical == ([32]byte{}) || permit.input.canonical != generationPrefixRecoveryInputDigest(permit.input) || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != generationPrefixRecoveryPermitDigest(permit) {
		return false
	}
	recordValue, ok := generationPrefixRecoveryPermitRegistry.Load(permit)
	if !ok {
		return false
	}
	record, ok := recordValue.(generationPrefixRecoveryPermitRegistryRecord)
	return ok && record.permit == permit && record.binding == permit.binding && record.history == permit.history && record.registered == permit.registered && record.candidateBinding == candidate.binding && record.inventory == permit.inventory && record.mutation == permit.mutation && record.handoffConsumed == permit.registered.handoffConsumed && record.permitConsumed == permit.consumed && record.runtimeBinding == permit.registered.runtimeReceipt.binding && record.recoveryBinding == permit.registered.recoveryReceipt.binding && record.historyCanonical == permit.history.binding.canonical && record.sourceCanonical == permit.registered.canonical && record.inputCanonical == permit.input.canonical && record.canonical == permit.binding.canonical
}

func validRecoveredHeaderDurablePermit(permit *RecoveredHeaderDurablePermit, candidate OwnedCurrentCandidate) bool {
	if permit == nil || permit.self != permit || permit.binding == nil || permit.binding.permit != permit || permit.prior == nil || permit.prior.binding == nil || permit.history != permit.prior.history || permit.registered != permit.prior.registered || permit.candidateBinding != candidate.binding || permit.binding.prior != permit.prior || permit.binding.history != permit.history || permit.binding.registered != permit.registered || permit.binding.candidateBinding != candidate.binding || permit.binding.inventory != permit.inventory || permit.binding.mutation != permit.mutation || permit.inventory == nil || permit.mutation == nil || permit.consumed == nil || permit.consumed.Load() || permit.prior.consumed == nil || !permit.prior.consumed.Load() || !permit.mutation.ValidFor(permit.inventory) || !validRetiredGenerationPrefixHistory(permit.history, permit.registered, candidate) || permit.input.canonical != permit.prior.input.canonical || permit.fsResult.Inventory() != permit.inventory || !permit.fsResult.ValidFor(permit.inventory) || permit.fsResult.Outcome() != evidencefs.AdmissionTransitionDurable || permit.fsResult.Journal() != digestRaw(permit.input.journal) || permit.fsResult.HeaderDigest() != permit.input.headerBytesDigest || permit.fsResult.HeaderSize() != uint64(len(permit.input.headerBytes)) || permit.fsResult.PreviousRevision() != permit.input.revision || permit.fsResult.CandidateRevision() != permit.revision || permit.revision != permit.input.revision+1 || permit.fullSet == permit.input.fullSet || permit.binding.canonical == ([32]byte{}) || permit.binding.canonical != recoveredHeaderDurablePermitDigest(permit) {
		return false
	}
	recordValue, ok := recoveredHeaderDurablePermitRegistry.Load(permit)
	if !ok {
		return false
	}
	record, ok := recordValue.(recoveredHeaderDurablePermitRegistryRecord)
	if !ok || record.permit != permit || record.binding != permit.binding || record.prior != permit.prior || record.history != permit.history || record.registered != permit.registered || record.candidateBinding != candidate.binding || record.inventory != permit.inventory || record.mutation != permit.mutation || record.permitConsumed != permit.prior.consumed || record.nextConsumed != permit.consumed || record.runtimeBinding != permit.registered.runtimeReceipt.binding || record.recoveryBinding != permit.registered.recoveryReceipt.binding || record.priorCanonical != permit.prior.binding.canonical || record.inputCanonical != permit.input.canonical || record.canonical != permit.binding.canonical {
		return false
	}
	revision, err := permit.inventory.Revision()
	if err != nil || revision != permit.revision {
		return false
	}
	fullSet, err := permit.inventory.FullSetDigest()
	return err == nil && fullSet == permit.fullSet
}

func validConsumedGenerationPrefixHistory(history *VerifiedAdmissionHistory, registered *verifiedAdmissionRegisteredGeneration, candidate OwnedCurrentCandidate) bool {
	return validGenerationPrefixHistoryState(history, registered, candidate, true)
}

// validRetiredGenerationPrefixHistory keeps the exact immutable verifier,
// receipt, and pass-one provenance after evidencefs has advanced away from the
// source inventory revision. Requiring the old AdmissionInventory to remain
// current here would make every durable header transition impossible to seal.
func validRetiredGenerationPrefixHistory(history *VerifiedAdmissionHistory, registered *verifiedAdmissionRegisteredGeneration, candidate OwnedCurrentCandidate) bool {
	return validGenerationPrefixHistoryState(history, registered, candidate, false)
}

func validGenerationPrefixHistoryState(history *VerifiedAdmissionHistory, registered *verifiedAdmissionRegisteredGeneration, candidate OwnedCurrentCandidate, requireCurrentInventory bool) bool {
	if history == nil || history.binding == nil || registered == nil || history.targetGeneration != registered || registered.handoffConsumed == nil || !registered.handoffConsumed.Load() || registered.replay != nil || !generationPrefixRecoveryState(history.targetState) || history.owner == nil || history.owner != candidate.verifiedRun.currentDecision.owner || history.candidateBinding != candidate.binding || history.binding.owner != history.owner || history.binding.candidateBinding != candidate.binding || history.binding.inventory != history.inventory || history.binding.history != history || history.inventory == nil || !validOwnedCurrentCandidate(candidate) || admissionRecoveryFactsDigest(history.currentFacts) == ([32]byte{}) || !history.rootFacts.valid() || history.binding.canonical == ([32]byte{}) || history.binding.canonical != admissionHistoryDigest(history) || !validVerifiedAdmissionRegisteredGenerationFacts(registered, candidate.verifiedRun.currentDecision) || digestRaw(registered.descriptor.identity.executionLineageDigest) != history.target {
		return false
	}
	stored, ok := verifiedAdmissionHistoryRegistry.Load(history.binding)
	if !ok || stored != history.binding.canonical {
		return false
	}
	if !requireCurrentInventory {
		return true
	}
	revision, err := history.inventory.Revision()
	if err != nil || revision != history.revision {
		return false
	}
	target, err := history.inventory.Target()
	if err != nil || target != history.target {
		return false
	}
	fullSet, err := history.inventory.FullSetDigest()
	return err == nil && fullSet == history.fullSet
}

func failGenerationPrefixRecoveryPermit(permit *GenerationPrefixRecoveryPermit) {
	if permit == nil {
		return
	}
	generationPrefixRecoveryPermitRegistry.Delete(permit)
	revokeVerifiedAdmissionRegisteredGeneration(permit.registered)
}
