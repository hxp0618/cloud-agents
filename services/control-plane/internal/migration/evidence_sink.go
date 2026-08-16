package migration

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// evidenceFSSink stores only the canonical configured locator. Each Open
// obtains fresh evidencefs mount authority; the string itself never authorizes
// a scan, publication, reservation, handoff, cursor, or session.
type evidenceFSSink struct {
	self     *evidenceFSSink
	seal     *struct{}
	rootPath string
}

var _ EvidenceSink = (*evidenceFSSink)(nil)

// NewEvidenceSink validates the configured locator without touching the
// filesystem. Production authority is acquired afresh by every Open, so a
// revoked mount claim prevents all later sessions even when this value lives
// longer than the claim.
func NewEvidenceSink(rootPath string) (EvidenceSink, error) {
	if rootPath == "" || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath || rootPath == string(filepath.Separator) {
		return nil, fail(CodeEvidenceJournalFailed, "evidence-sink", "evidence root locator is invalid", nil)
	}
	sink := &evidenceFSSink{seal: &struct{}{}, rootPath: rootPath}
	sink.self = sink
	return sink, nil
}

func (s *evidenceFSSink) evidenceSinkSealed() {}

func (s *evidenceFSSink) valid() bool {
	return s != nil && s.self == s && s.seal != nil && s.rootPath != "" && filepath.IsAbs(s.rootPath) && filepath.Clean(s.rootPath) == s.rootPath && s.rootPath != string(filepath.Separator)
}

// Open is the sole production composition root from trusted evidencefs mount
// authority to a runnable migration EvidenceSession. It never accepts a Store,
// lease, inventory, token, path descriptor, or replay fact from the caller.
func (s *evidenceFSSink) Open(ctx context.Context, run VerifiedEvidenceRun, runtime VerifiedRuntimeArtifact) (session EvidenceSession, snapshot *RecoverySnapshot, resultErr error) {
	if !s.valid() {
		return nil, nil, fail(CodeEvidenceJournalFailed, "evidence-sink-open", "evidence sink authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, nil, err
	}
	candidate, err := ownedCurrentCandidateFromEvidenceRun(run, runtime)
	if err != nil {
		return nil, nil, err
	}
	store, err := evidencefs.OpenStore(ctx, s.rootPath)
	if err != nil {
		return nil, nil, mapEvidenceAdmissionError(err, "evidence-sink-store-open")
	}
	target := digestRaw(candidate.verifiedRun.executionLineageDigest)
	lease, inventory, err := store.AcquireAdmission(ctx, target)
	if err != nil || lease == nil || inventory == nil {
		if err == nil {
			err = evidencefs.ErrUnknown
		}
		return nil, nil, mapEvidenceAdmissionError(err, "evidence-sink-admission")
	}
	cleanup := evidenceSinkOpenCleanup{admission: lease}
	defer func() {
		if cleanup.committed {
			return
		}
		session, snapshot = nil, nil
		if cleanupErr := cleanup.close(); cleanupErr != nil {
			resultErr = cleanupErr
		} else if resultErr == nil {
			resultErr = admissionFailed("evidence-sink-open", "evidence session was not committed", nil)
		}
	}()

	history, historyErr := bindVerifiedAdmissionHistory(ctx, inventory, candidate)
	if historyErr != nil {
		if !IsCode(historyErr, CodeEvidenceRecoveryRequired) {
			return nil, nil, historyErr
		}
		session, resultErr = openHistoricalSupersededEvidenceSession(ctx, inventory, candidate, &cleanup)
		if resultErr != nil {
			if IsCode(resultErr, CodeEvidenceRecoveryRequired) {
				return nil, nil, historyErr
			}
			return nil, nil, resultErr
		}
	} else {
		switch history.targetState {
		case "", admissionLineageEmpty:
			session, resultErr = openBrandNewEvidenceSession(ctx, inventory, history, candidate, &cleanup)
		case admissionLineageReservedUnregistered, admissionLineageReservedHeader:
			session, resultErr = openGenerationPrefixEvidenceSession(ctx, history, candidate, &cleanup)
		case admissionLineageActiveInitial, admissionLineageActiveCheckpointed, admissionLineageActiveUnknownExtension:
			session, resultErr = openRegisteredEvidenceSession(ctx, history, candidate, &cleanup)
		default:
			cleanup.revoke = func() { revokeEvidenceSinkHistory(history) }
			resultErr = admissionCorrupt("evidence-sink-open", "target lineage has no closed open transition", nil)
		}
		if resultErr != nil {
			return nil, nil, resultErr
		}
	}
	if session == nil {
		return nil, nil, admissionFailed("evidence-sink-open", "evidence session authority is unavailable", nil)
	}
	snapshot = session.RecoverySnapshot()
	current := session.CurrentCandidate()
	if snapshot == nil || !validOwnedCurrentCandidate(current) || current.binding != candidate.binding {
		closeErr := session.Close(context.Background())
		if closeErr != nil {
			return nil, nil, fail(CodeEvidenceJournalFailed, "evidence-sink-open-cleanup", "evidence session cleanup failed", nil)
		}
		return nil, nil, admissionFailed("evidence-sink-open", "evidence session result is incomplete", nil)
	}
	cleanup.committed = true
	return session, snapshot, nil
}

type evidenceSinkOpenCleanup struct {
	admission *evidencefs.AdmissionLease
	release   func() error
	revoke    func()
	committed bool
}

func (c *evidenceSinkOpenCleanup) close() error {
	if c == nil {
		return nil
	}
	var cleanupErr error
	if c.release != nil {
		cleanupErr = c.release()
	} else if c.admission != nil {
		cleanupErr = c.admission.Close()
		if errors.Is(cleanupErr, evidencefs.ErrLeaseInvalid) && !c.admission.Active() {
			cleanupErr = nil
		} else if cleanupErr != nil {
			cleanupErr = mapEvidenceAdmissionError(cleanupErr, "evidence-sink-open-cleanup")
		}
	}
	if c.revoke != nil {
		c.revoke()
	}
	return cleanupErr
}

type brandNewEvidenceOpenChain struct {
	history           *VerifiedAdmissionHistory
	plan              *VerifiedAdmissionPlan
	permit            *AdmissionPermit
	registered        *RegisteredAdmissionPermit
	runtimePublished  *RuntimePublishedPermit
	runtimeBound      *RuntimeBoundPermit
	recoveryPublished *RecoveryPublishedPermit
	recoveryBound     *RecoveryBoundPermit
	reserveReady      *ReserveReady
	receiptBound      *ReceiptBoundReady
	reserved          *ReservedDurablePermit
	header            *HeaderDurablePermit
	generation        *GenerationReadyPermit
	handoff           *GenerationHandoffReady
	replay            *GenerationReplayReady
	recovery          *GenerationRecoveryReady
}

func openBrandNewEvidenceSession(ctx context.Context, inventory *evidencefs.AdmissionInventory, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate, cleanup *evidenceSinkOpenCleanup) (EvidenceSession, error) {
	chain := &brandNewEvidenceOpenChain{history: history}
	cleanup.revoke = chain.revoke
	plan, err := bindVerifiedAdmissionPlan(ctx, history, candidate)
	if err != nil {
		return nil, err
	}
	chain.plan = plan
	token, err := inventory.MutationToken()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "evidence-sink-brand-new-token")
	}
	permit, err := bindAdmissionPermit(ctx, inventory, token, history, plan, candidate)
	if err != nil {
		return nil, err
	}
	chain.permit = permit
	registeredResult, err := permit.CreateTargetLineage(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.registered = registeredResult.Next()
	if registeredResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.registered == nil {
		return nil, admissionFailed("evidence-sink-brand-new-register", "durable target registration authority is unavailable", nil)
	}
	runtimePublishedResult, err := chain.registered.PublishRuntime(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.runtimePublished = runtimePublishedResult.Next()
	if runtimePublishedResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.runtimePublished == nil {
		return nil, admissionFailed("evidence-sink-brand-new-runtime-publish", "durable runtime publication authority is unavailable", nil)
	}
	runtimeBoundResult, err := chain.runtimePublished.BindRuntime(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.runtimeBound = runtimeBoundResult.Next()
	if runtimeBoundResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.runtimeBound == nil {
		return nil, admissionFailed("evidence-sink-brand-new-runtime-bind", "durable runtime binding authority is unavailable", nil)
	}
	recoveryPublishedResult, err := chain.runtimeBound.PublishDecisionRecovery(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.recoveryPublished = recoveryPublishedResult.Next()
	if recoveryPublishedResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.recoveryPublished == nil {
		return nil, admissionFailed("evidence-sink-brand-new-recovery-publish", "durable recovery publication authority is unavailable", nil)
	}
	recoveryBoundResult, err := chain.recoveryPublished.BindDecisionRecovery(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.recoveryBound = recoveryBoundResult.Next()
	if recoveryBoundResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.recoveryBound == nil {
		return nil, admissionFailed("evidence-sink-brand-new-recovery-bind", "durable recovery binding authority is unavailable", nil)
	}
	reserveReadyResult, err := chain.recoveryBound.SealReserveReady(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.reserveReady = reserveReadyResult.Next()
	if reserveReadyResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.reserveReady == nil {
		return nil, admissionFailed("evidence-sink-brand-new-reserve-ready", "reserve-ready authority is unavailable", nil)
	}
	chain.receiptBound, err = chain.reserveReady.BindReceiptPair(candidate)
	if err != nil {
		return nil, err
	}
	if chain.receiptBound == nil {
		return nil, admissionFailed("evidence-sink-brand-new-receipts", "typed receipt-pair authority is unavailable", nil)
	}
	reservedResult, err := chain.receiptBound.AppendGenerationReserved(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.reserved = reservedResult.Next()
	if reservedResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.reserved == nil {
		return nil, admissionFailed("evidence-sink-brand-new-reserve", "durable generation reservation authority is unavailable", nil)
	}
	headerResult, err := chain.reserved.CreateGenerationHeader(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.header = headerResult.Next()
	if headerResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.header == nil {
		return nil, admissionFailed("evidence-sink-brand-new-header", "durable generation header authority is unavailable", nil)
	}
	activationResult, err := chain.header.AppendGenerationActivated(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.generation = activationResult.Next()
	if activationResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.generation == nil {
		return nil, admissionFailed("evidence-sink-brand-new-activate", "durable generation activation authority is unavailable", nil)
	}
	handoffResult, err := chain.generation.Handoff(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.handoff = handoffResult.Next()
	if handoffResult.Outcome() != evidencefs.AdmissionTransitionDurable || chain.handoff == nil {
		return nil, admissionFailed("evidence-sink-brand-new-handoff", "generation handoff authority is unavailable", nil)
	}
	cleanup.admission = nil
	cleanup.release = liveGenerationHandoffCloser(chain.handoff)
	replayResult, err := chain.handoff.Replay(ctx, candidate)
	if err != nil {
		return nil, err
	}
	chain.replay = replayResult.Next()
	if chain.replay == nil {
		return nil, admissionFailed("evidence-sink-brand-new-replay", "generation replay authority is unavailable", nil)
	}
	cleanup.release = liveGenerationReplayCloser(chain.replay)
	chain.recovery, err = chain.replay.BindRecovery(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if chain.recovery == nil {
		return nil, admissionFailed("evidence-sink-brand-new-recovery", "generation recovery authority is unavailable", nil)
	}
	cleanup.release = liveGenerationRecoveryCloser(chain.recovery)
	session, err := chain.recovery.BindSession(ctx, candidate)
	return finishEvidenceSessionBind(session, err, cleanup, "evidence-sink-brand-new-session")
}

func (c *brandNewEvidenceOpenChain) revoke() {
	if c == nil {
		return
	}
	if c.recovery != nil {
		generationRecoveryReadyRegistry.Delete(c.recovery)
		if c.recovery.cursor.valid != nil {
			c.recovery.cursor.valid.Store(false)
		}
	}
	if c.replay != nil {
		generationReplayReadyRegistry.Delete(c.replay)
	}
	if c.handoff != nil {
		generationHandoffReadyRegistry.Delete(c.handoff)
	}
	if c.generation != nil {
		generationReadyPermitRegistry.Delete(c.generation)
	}
	if c.header != nil && c.header.binding != nil {
		headerDurablePermitRegistry.Delete(c.header.binding)
	}
	if c.reserved != nil && c.reserved.binding != nil {
		reservedDurablePermitRegistry.Delete(c.reserved.binding)
	}
	if c.receiptBound != nil && c.receiptBound.binding != nil {
		receiptBoundReadyRegistry.Delete(c.receiptBound.binding)
	}
	if c.reserveReady != nil && c.reserveReady.binding != nil {
		reserveReadyRegistry.Delete(c.reserveReady.binding)
	}
	if c.recoveryBound != nil && c.recoveryBound.binding != nil {
		recoveryBoundPermitRegistry.Delete(c.recoveryBound.binding)
	}
	if c.recoveryPublished != nil && c.recoveryPublished.binding != nil {
		recoveryPublishedPermitRegistry.Delete(c.recoveryPublished.binding)
	}
	if c.runtimeBound != nil && c.runtimeBound.binding != nil {
		runtimeBoundPermitRegistry.Delete(c.runtimeBound.binding)
	}
	if c.runtimePublished != nil && c.runtimePublished.binding != nil {
		runtimePublishedPermitRegistry.Delete(c.runtimePublished.binding)
	}
	if c.registered != nil && c.registered.binding != nil {
		registeredAdmissionPermitRegistry.Delete(c.registered.binding)
	}
	if c.permit != nil && c.permit.binding != nil {
		admissionPermitRegistry.Delete(c.permit.binding)
	}
	if c.plan != nil && c.plan.binding != nil {
		verifiedAdmissionPlanRegistry.Delete(c.plan.binding)
	}
	if c.receiptBound != nil {
		if c.receiptBound.runtimeReceipt.binding != nil {
			verifiedContentReceiptRegistry.Delete(c.receiptBound.runtimeReceipt.binding)
		}
		if c.receiptBound.recoveryReceipt.binding != nil {
			verifiedDecisionRecoveryReceiptRegistry.Delete(c.receiptBound.recoveryReceipt.binding)
		}
	}
	revokeEvidenceSinkHistory(c.history)
}

func openRegisteredEvidenceSession(ctx context.Context, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate, cleanup *evidenceSinkOpenCleanup) (EvidenceSession, error) {
	var permit *RegisteredGenerationHandoffPermit
	cleanup.revoke = func() {
		revokeRegisteredGenerationHandoffPermit(permit)
		revokeEvidenceSinkHistory(history)
	}
	var err error
	permit, err = bindRegisteredGenerationHandoff(ctx, history, candidate)
	if err != nil {
		return nil, err
	}
	result, err := permit.Handoff(ctx, candidate)
	if err != nil {
		return nil, err
	}
	ready := result.Next()
	if result.Outcome() != evidencefs.AdmissionTransitionDurable || ready == nil {
		return nil, admissionFailed("evidence-sink-registered-handoff", "registered generation handoff authority is unavailable", nil)
	}
	cleanup.admission = nil
	cleanup.release = liveRegisteredGenerationRecoveryCloser(ready)
	session, err := ready.BindSession(ctx, candidate)
	return finishEvidenceSessionBind(session, err, cleanup, "evidence-sink-registered-session")
}

func openGenerationPrefixEvidenceSession(ctx context.Context, history *VerifiedAdmissionHistory, candidate OwnedCurrentCandidate, cleanup *evidenceSinkOpenCleanup) (EvidenceSession, error) {
	var prefix *GenerationPrefixRecoveryPermit
	var recovered *RecoveredHeaderDurablePermit
	var handoffPermit *RegisteredGenerationHandoffPermit
	cleanup.revoke = func() {
		if recovered != nil {
			retireRecoveredHeaderDurablePermit(recovered)
		} else if prefix != nil {
			failGenerationPrefixRecoveryPermit(prefix)
		}
		revokeRegisteredGenerationHandoffPermit(handoffPermit)
		revokeEvidenceSinkHistory(history)
	}
	var err error
	prefix, err = bindGenerationPrefixRecoveryPermit(ctx, history, candidate)
	if err != nil {
		return nil, err
	}
	headerResult, err := prefix.RecoverGenerationHeader(ctx, candidate)
	if err != nil {
		return nil, err
	}
	recovered = headerResult.Next()
	if headerResult.Outcome() != evidencefs.AdmissionTransitionDurable || recovered == nil {
		return nil, admissionFailed("evidence-sink-prefix-header", "recovered generation header authority is unavailable", nil)
	}
	activationResult, err := recovered.AppendGenerationActivated(ctx, candidate)
	if err != nil {
		return nil, err
	}
	handoffPermit = activationResult.Next()
	if activationResult.Outcome() != evidencefs.AdmissionTransitionDurable || handoffPermit == nil {
		return nil, admissionFailed("evidence-sink-prefix-activate", "recovered generation activation authority is unavailable", nil)
	}
	handoffResult, err := handoffPermit.Handoff(ctx, candidate)
	if err != nil {
		return nil, err
	}
	ready := handoffResult.Next()
	if handoffResult.Outcome() != evidencefs.AdmissionTransitionDurable || ready == nil {
		return nil, admissionFailed("evidence-sink-prefix-handoff", "recovered generation handoff authority is unavailable", nil)
	}
	cleanup.admission = nil
	cleanup.release = liveRegisteredGenerationRecoveryCloser(ready)
	session, err := ready.BindSession(ctx, candidate)
	return finishEvidenceSessionBind(session, err, cleanup, "evidence-sink-prefix-session")
}

func openHistoricalSupersededEvidenceSession(ctx context.Context, inventory *evidencefs.AdmissionInventory, candidate OwnedCurrentCandidate, cleanup *evidenceSinkOpenCleanup) (EvidenceSession, error) {
	var adjacent *HistoricalSupersessionAdjacentReserveReady
	var reserved *HistoricalSuccessorReservedDurablePermit
	var header *HistoricalSuccessorHeaderDurablePermit
	var generation *HistoricalSuccessorGenerationReadyPermit
	var handoff *HistoricalSuccessorGenerationHandoffReady
	var replay *HistoricalSuccessorGenerationReplayReady
	var recovery *HistoricalSuccessorGenerationRecoveryReady
	cleanup.revoke = func() {
		if recovery != nil && recovery.cursor.valid != nil {
			recovery.cursor.valid.Store(false)
		}
		if recovery != nil {
			historicalSuccessorGenerationRecoveryRegistry.Delete(recovery)
		}
		if replay != nil {
			historicalSuccessorGenerationReplayRegistry.Delete(replay)
		}
		if handoff != nil {
			historicalSuccessorGenerationHandoffRegistry.Delete(handoff)
		}
		revokeHistoricalSuccessorGenerationReadyPermit(generation)
		revokeHistoricalSuccessorHeaderPermit(header)
		revokeHistoricalSuccessorReservedPermit(reserved)
		revokeHistoricalSupersessionAdjacentReady(adjacent)
	}
	var err error
	adjacent, err = bindHistoricalSupersessionAdjacentReserveReady(ctx, inventory, candidate)
	if err != nil {
		return nil, err
	}
	reservedResult, err := adjacent.AppendGenerationReserved(ctx, candidate)
	if err != nil {
		return nil, err
	}
	reserved = reservedResult.Next()
	if reservedResult.Outcome() != evidencefs.AdmissionTransitionDurable || reserved == nil {
		return nil, admissionFailed("evidence-sink-historical-reserve", "historical adjacent reservation authority is unavailable", nil)
	}
	headerResult, err := reserved.CreateGenerationHeader(ctx, candidate)
	if err != nil {
		return nil, err
	}
	header = headerResult.Next()
	if headerResult.Outcome() != evidencefs.AdmissionTransitionDurable || header == nil {
		return nil, admissionFailed("evidence-sink-historical-header", "historical successor header authority is unavailable", nil)
	}
	activationResult, err := header.AppendGenerationActivated(ctx, candidate)
	if err != nil {
		return nil, err
	}
	generation = activationResult.Next()
	if activationResult.Outcome() != evidencefs.AdmissionTransitionDurable || generation == nil {
		return nil, admissionFailed("evidence-sink-historical-activate", "historical successor activation authority is unavailable", nil)
	}
	handoffResult, err := generation.Handoff(ctx, candidate)
	if err != nil {
		return nil, err
	}
	handoff = handoffResult.Next()
	if handoffResult.Outcome() != evidencefs.AdmissionTransitionDurable || handoff == nil {
		return nil, admissionFailed("evidence-sink-historical-handoff", "historical successor handoff authority is unavailable", nil)
	}
	cleanup.admission = nil
	cleanup.release = liveHistoricalHandoffCloser(handoff)
	replayResult, err := handoff.Replay(ctx, candidate)
	if err != nil {
		return nil, err
	}
	replay = replayResult.Next()
	if replay == nil {
		return nil, admissionFailed("evidence-sink-historical-replay", "historical successor replay authority is unavailable", nil)
	}
	cleanup.release = liveHistoricalReplayCloser(replay)
	recovery, err = replay.BindRecovery(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if recovery == nil {
		return nil, admissionFailed("evidence-sink-historical-recovery", "historical successor recovery authority is unavailable", nil)
	}
	cleanup.release = liveHistoricalRecoveryCloser(recovery)
	session, err := recovery.BindSession(ctx, candidate)
	return finishEvidenceSessionBind(session, err, cleanup, "evidence-sink-historical-session")
}

// finishEvidenceSessionBind transfers retained filesystem cleanup to a
// concrete session only after the binder returns the closed success shape.
// A defensive nonnil-session/error result is closed immediately; cleanup
// failure dominates without allowing a session to escape beside an error.
func finishEvidenceSessionBind(session EvidenceSession, bindErr error, cleanup *evidenceSinkOpenCleanup, operation string) (EvidenceSession, error) {
	if bindErr != nil {
		if session != nil {
			if closeErr := session.Close(context.Background()); closeErr != nil {
				return nil, fail(CodeEvidenceJournalFailed, operation+"-cleanup", "evidence session cleanup failed", nil)
			}
		}
		return nil, bindErr
	}
	if session == nil {
		return nil, admissionFailed(operation, "evidence session authority is unavailable", nil)
	}
	cleanup.release = nil
	return session, nil
}

func revokeRegisteredGenerationHandoffPermit(permit *RegisteredGenerationHandoffPermit) {
	if permit == nil {
		return
	}
	registeredGenerationHandoffPermitRegistry.Delete(permit)
	if permit.registered != nil {
		revokeVerifiedAdmissionRegisteredGeneration(permit.registered)
	}
	if permit.history != nil && permit.history.binding != nil {
		verifiedAdmissionHistoryRegistry.Delete(permit.history.binding)
	}
}

func revokeEvidenceSinkHistory(history *VerifiedAdmissionHistory) {
	if history == nil {
		return
	}
	if history.binding != nil {
		verifiedAdmissionHistoryRegistry.Delete(history.binding)
	}
	revokeVerifiedAdmissionRegisteredGeneration(history.targetGeneration)
}

func liveGenerationHandoffCloser(value *GenerationHandoffReady) func() error {
	return func() error {
		if value != nil && value.consumed != nil && !value.consumed.Load() {
			return value.Close()
		}
		return nil
	}
}
func liveGenerationReplayCloser(value *GenerationReplayReady) func() error {
	return func() error {
		if value != nil && value.consumed != nil && !value.consumed.Load() {
			return value.Close()
		}
		return nil
	}
}
func liveGenerationRecoveryCloser(value *GenerationRecoveryReady) func() error {
	return func() error {
		if value != nil && value.consumed != nil && !value.consumed.Load() {
			return value.Close()
		}
		return nil
	}
}
func liveRegisteredGenerationRecoveryCloser(value *RegisteredGenerationRecoveryReady) func() error {
	return func() error {
		if value != nil && value.consumed != nil && !value.consumed.Load() {
			return value.Close()
		}
		return nil
	}
}
func liveHistoricalHandoffCloser(value *HistoricalSuccessorGenerationHandoffReady) func() error {
	return func() error {
		if value != nil && value.consumed != nil && !value.consumed.Load() {
			return value.Close()
		}
		return nil
	}
}
func liveHistoricalReplayCloser(value *HistoricalSuccessorGenerationReplayReady) func() error {
	return func() error {
		if value != nil && value.consumed != nil && !value.consumed.Load() {
			return value.Close()
		}
		return nil
	}
}
func liveHistoricalRecoveryCloser(value *HistoricalSuccessorGenerationRecoveryReady) func() error {
	return func() error {
		if value != nil && value.consumed != nil && !value.consumed.Load() {
			return value.Close()
		}
		return nil
	}
}
