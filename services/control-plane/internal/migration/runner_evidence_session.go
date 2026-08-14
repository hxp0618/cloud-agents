package migration

import (
	"context"
	"errors"
)

func openRunnerEvidenceSession(ctx context.Context, sink EvidenceSink, run VerifiedEvidenceRun, runtime VerifiedRuntimeArtifact, candidate OwnedCurrentCandidate) (EvidenceSession, *RecoverySnapshot, error) {
	session, snapshot, openErr := sink.Open(ctx, run, runtime)
	if openErr != nil {
		if session != nil || snapshot != nil {
			if cleanupErr := closeRunnerEvidenceOwnership(session, candidate); cleanupErr != nil {
				return nil, nil, cleanupErr
			}
			return nil, nil, fail(CodeEvidenceJournalFailed, "runner-evidence-open", "evidence sink violated the closed error result", nil)
		}
		if cleanupErr := closeRunnerEvidenceOwnership(nil, candidate); cleanupErr != nil {
			return nil, nil, cleanupErr
		}
		return nil, nil, mapRunnerEvidenceSessionError(openErr, "runner-evidence-open")
	}
	if session == nil || snapshot == nil {
		if cleanupErr := closeRunnerEvidenceOwnership(session, candidate); cleanupErr != nil {
			return nil, nil, cleanupErr
		}
		return nil, nil, fail(CodeEvidenceJournalFailed, "runner-evidence-open", "evidence sink did not return the closed success result", nil)
	}
	if err := validateRunnerEvidenceSession(ctx, session, snapshot, candidate); err != nil {
		if cleanupErr := closeRunnerEvidenceOwnership(session, candidate); cleanupErr != nil {
			return nil, nil, cleanupErr
		}
		return nil, nil, err
	}
	return session, snapshot, nil
}

func validateRunnerEvidenceSession(ctx context.Context, session EvidenceSession, snapshot *RecoverySnapshot, candidate OwnedCurrentCandidate) error {
	current := session.CurrentCandidate()
	journal := session.Journal()
	active := session.ActiveGeneration()
	ownedSnapshot := session.RecoverySnapshot()
	if !validOwnedCurrentCandidate(current) || current.binding != candidate.binding || journal == nil || active.journal != journal || active.identity.owner != candidate.owner || active.identity.executionLineageDigest != candidate.verifiedRun.executionLineageDigest || active.identity.schemaBundleDigest != candidate.verifiedRun.schemaBundleDigest || active.ownedDecision.owner != candidate.verifiedRun.currentDecision.owner || active.ownedDecision.digest != active.identity.runnerProjectionDecisionDigest || snapshot == nil || ownedSnapshot == nil || !sameGenerationIdentity(snapshot.generation, active.identity) || snapshot.owner != candidate.owner {
		return fail(CodeEvidenceJournalFailed, "runner-evidence-session", "evidence session authority is unavailable or mismatched", nil)
	}
	switch active.kind {
	case activeGenerationCurrent:
		if active.ownedDecision.digest != candidate.verifiedRun.currentDecision.digest || active.ownedDecision.capability.owner != candidate.verifiedRun.currentDecision.capability.owner || !active.ownedDecision.decision.exactlyMatches(candidate.verifiedRun.currentDecision.decision) || active.recoveryExecutionBindings != nil {
			return fail(CodeEvidenceJournalFailed, "runner-evidence-session", "current generation authority is mismatched", nil)
		}
	case activeGenerationAncestorRecovery:
		if active.ownedDecision.digest == candidate.verifiedRun.currentDecision.digest || active.ownedDecision.capability.owner != candidate.verifiedRun.currentDecision.capability.owner || active.recoveryExecutionBindings == nil || !sameRecoveryExecutionBindings(active.recoveryExecutionBindings, active.recoveryExecutionBindings, active.identity, candidate.verifiedRun.currentDecision.digest) {
			return fail(CodeEvidenceJournalFailed, "runner-evidence-session", "ancestor recovery authority is mismatched", nil)
		}
	default:
		return fail(CodeEvidenceJournalFailed, "runner-evidence-session", "active generation kind is unavailable", nil)
	}
	snapshotDigest := generationJournalRecoveryDigest(snapshot)
	if snapshotDigest == ([32]byte{}) || generationJournalRecoveryDigest(ownedSnapshot) != snapshotDigest || !sameCursorIdentity(snapshot.cursor, ownedSnapshot.cursor) {
		return fail(CodeEvidenceJournalFailed, "runner-evidence-session", "evidence session snapshot is unavailable or mismatched", nil)
	}
	cursor, replayed, err := journal.Replay(ctx)
	if err != nil {
		return mapRunnerEvidenceSessionError(err, "runner-evidence-replay")
	}
	if replayed == nil || !cursor.Valid() || !validRecoverySnapshotForJournal(replayed, active.identity, cursor) || generationJournalRecoveryDigest(replayed) != snapshotDigest || !sameCursorIdentity(cursor, snapshot.cursor) {
		return fail(CodeEvidenceJournalFailed, "runner-evidence-replay", "journal replay differs from the opened snapshot", nil)
	}
	return nil
}

func closeRunnerEvidenceOwnership(session EvidenceSession, candidate OwnedCurrentCandidate) error {
	var closeFailed bool
	if session != nil {
		closeCtx, cancel := cleanupContext()
		if err := session.Close(closeCtx); err != nil {
			closeFailed = true
		}
		cancel()
	}
	revoked := revokeOwnedCurrentCandidate(candidate)
	if !revoked && candidate.binding != nil {
		_, stillLive := verifiedEvidenceRunBindingRegistry.Load(candidate.binding)
		revoked = !stillLive
	}
	if closeFailed || !revoked {
		return fail(CodeEvidenceJournalFailed, "runner-evidence-close", "evidence session cleanup could not be proven complete", nil)
	}
	return nil
}

func mapRunnerEvidenceSessionError(err error, op string) error {
	var stable *Error
	if errors.As(err, &stable) {
		switch stable.Code {
		case CodeEvidenceJournalFailed, CodeEvidenceJournalCorrupt, CodeEvidenceJournalLimitExceeded, CodeEvidenceRecoveryRequired, CodeContextCanceled, CodeDeadlineExceeded:
			return fail(stable.Code, op, "evidence session operation failed", nil)
		default:
			return fail(CodeEvidenceJournalFailed, op, "evidence session operation failed", nil)
		}
	}
	if errors.Is(err, context.Canceled) {
		return fail(CodeContextCanceled, op, "evidence session operation was canceled", nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, op, "evidence session deadline was exceeded", nil)
	}
	return fail(CodeEvidenceJournalFailed, op, "evidence session operation failed", nil)
}
