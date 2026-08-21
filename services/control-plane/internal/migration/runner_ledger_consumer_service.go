package migration

import (
	"context"
	"errors"
)

// consumeRunnerLedgerPreflight is the only production consumer of the
// generated runner-ledger consumer profile. It obtains ordinary dispatch data
// only by minting and consuming one exact same-verifier claim inside this
// closed call. The function owns no writer seam and never returns the dispatch
// or generated fact to its caller.
func (runner *Runner) consumeRunnerLedgerPreflight(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate) (RunResult, error) {
	if runner == nil || ctx == nil || bundle == nil || bundle.Manifest == nil {
		return RunResult{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer", "runner ledger consumer inputs are unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return RunResult{}, err
	}
	claim, err := runner.prepareRunnerLedgerPreflightClaim(ctx, dsn, bundle, plans, evidence, candidate)
	if err != nil {
		return RunResult{}, err
	}
	// The evidence binder normally removes a consumed claim. The unconditional
	// revoke also closes cancellation, binder-error, and future validation
	// paths without leaving a live one-shot registry entry.
	defer revokeRunnerLedgerPreflightClaim(claim)

	dispatch, err := runner.claimRunnerLedgerPreflightDispatch(ctx, evidence, candidate, claim)
	if err != nil {
		return RunResult{}, err
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil {
		return RunResult{}, err
	}
	bundle = verifiedBundle
	if !validOwnedCurrentCandidate(candidate) || bundle.Manifest.ManifestDigest != candidate.verifiedRun.manifestDigest ||
		bundle.Manifest.SchemaBundleDigest != candidate.verifiedRun.schemaBundleDigest ||
		dispatch.runnerProjectionDecisionDigest != candidate.verifiedRun.runnerProjectionDecisionDigest ||
		dispatch.fact.executionLineageDigest != candidate.verifiedRun.executionLineageDigest {
		return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer", "verified runtime changed after claim consumption", nil)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, bundle.Manifest.ManifestDigest)
	if err != nil {
		return RunResult{}, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return RunResult{}, err
	}
	if !fact.valid() || fact.manifestDigest != bundle.Manifest.ManifestDigest ||
		fact.dispatch.fact.schemaBundleDigest != bundle.Manifest.SchemaBundleDigest {
		return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer", "generated consumer fact differs from the verified runtime", nil)
	}

	switch fact.action {
	case runnerLedgerConsumerReturnSuccessNoop:
		entries := bundle.Manifest.SchemaBundle.Migrations
		head := fact.dispatch.fact.orderedMigrationPrefixHead
		if len(entries) == 0 || head == nil || *head != entries[len(entries)-1].ID ||
			fact.dispatch.fact.orderedMigrationPrefixLength != uint32(len(entries)) ||
			fact.dispatch.kind != runnerLedgerPreflightDispatchReturnSuccess ||
			fact.dispatch.fact.disposition != runnerLedgerPreflightCompleteReturnSuccess ||
			fact.dispatch.fact.nextEntry != nil || fact.dispatch.fact.recovery == nil ||
			fact.dispatch.fact.recovery.State != RecoveryCompleted ||
			fact.dispatch.fact.recovery.Action != RecoveryReturnSuccess {
			return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer-result", "complete ledger result is unavailable or contradictory", nil)
		}
		return RunResult{
			SchemaBundleDigest: bundle.Manifest.SchemaBundleDigest,
			ManifestDigest:     bundle.Manifest.ManifestDigest,
			FinalHead:          *head,
			Applied:            []string{},
			AmbiguousRecovered: []string{},
		}, nil
	case runnerLedgerConsumerEntryNotImplemented:
		permit, err := runner.prepareRunnerLedgerEntryExecutionAdmission(ctx, dsn, bundle, plans, evidence, candidate, fact)
		if err != nil {
			if runnerLedgerEntryExecutionAdmissionUnsupported(err) {
				return RunResult{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer-entry", "entry writer is not implemented", nil)
			}
			return RunResult{}, err
		}
		if err := closeRunnerLedgerEntryExecutionPermit(permit, nil); err != nil {
			return RunResult{}, err
		}
		return RunResult{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer-entry", "entry writer is not implemented", nil)
	case runnerLedgerConsumerRecoveryNotImplemented:
		return RunResult{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer-recovery", "recovery consumer and writer are not implemented", nil)
	default:
		return RunResult{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer", "runner ledger consumer action is not implemented", nil)
	}
}

func runnerLedgerEntryExecutionAdmissionUnsupported(err error) bool {
	if !IsCode(err, CodeProjectionNotImplemented) {
		return false
	}
	var stable *Error
	return errors.As(err, &stable) && stable.Op == "runner-ledger-entry-execution-admission-selection"
}

func runnerLedgerConsumerEligible(err error, allowedOps ...string) bool {
	if !IsCode(err, CodeProjectionNotImplemented) {
		return false
	}
	var stable *Error
	if !errors.As(err, &stable) {
		return false
	}
	for _, op := range allowedOps {
		if stable.Op == op {
			return true
		}
	}
	return false
}
