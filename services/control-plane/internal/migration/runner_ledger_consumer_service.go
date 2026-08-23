package migration

import (
	"context"
	"errors"
)

type runnerLedgerPreflightStepKind uint8

const (
	runnerLedgerPreflightStepComplete runnerLedgerPreflightStepKind = iota + 1
	runnerLedgerPreflightStepEntryCommitted
	runnerLedgerPreflightStepReenter
)

type runnerLedgerPreflightStep struct {
	kind               runnerLedgerPreflightStepKind
	prefixLength       uint32
	nextEntryID        string
	result             RunResult
	outcome            runnerLedgerEntrySuccessOutcome
	ambiguousRecovered bool
}

// consumeRunnerLedgerPreflight is the only production entry loop for the
// generated runner-ledger consumer and success-writer profiles. Every loop
// iteration mints and consumes a fresh preflight claim, obtains at most one
// fresh locked database session, and consumes at most one execution permit.
// A committed next-entry result can only re-enter through that complete path;
// no ordinary outcome is reused as writer authority.
func (runner *Runner) consumeRunnerLedgerPreflight(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate) (RunResult, error) {
	inputBundle := bundle
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil {
		return RunResult{}, err
	}
	bundle = verifiedBundle
	entries := bundle.Manifest.SchemaBundle.Migrations
	policy := bundle.Manifest.ExecutionPolicy
	maxIterations, err := runnerLedgerRecoveryIterationLimit(len(entries), policy)
	if err != nil {
		return RunResult{}, err
	}
	applied := make([]string, 0, len(entries))
	ambiguousRecovered := make([]string, 0, len(entries))
	var expectedPrefix uint32
	haveExpectedPrefix := false
	for iteration := uint64(0); iteration < maxIterations; iteration++ {
		step, stepErr := runner.consumeRunnerLedgerPreflightStep(ctx, dsn, inputBundle, plans, evidence, candidate)
		if stepErr != nil {
			return RunResult{}, stepErr
		}
		switch step.kind {
		case runnerLedgerPreflightStepComplete:
			if step.prefixLength != uint32(len(entries)) ||
				step.result.SchemaBundleDigest != bundle.Manifest.SchemaBundleDigest ||
				step.result.ManifestDigest != bundle.Manifest.ManifestDigest ||
				step.result.FinalHead != entries[len(entries)-1].ID || step.result.Applied == nil ||
				step.result.AmbiguousRecovered == nil || len(step.result.Applied) != 0 ||
				len(step.result.AmbiguousRecovered) != 0 || haveExpectedPrefix && expectedPrefix != uint32(len(entries)) {
				return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "complete ledger step is contradictory", nil)
			}
			return RunResult{
				SchemaBundleDigest: bundle.Manifest.SchemaBundleDigest,
				ManifestDigest:     bundle.Manifest.ManifestDigest,
				FinalHead:          entries[len(entries)-1].ID,
				Applied:            append([]string{}, applied...),
				AmbiguousRecovered: append([]string{}, ambiguousRecovered...),
			}, nil
		case runnerLedgerPreflightStepEntryCommitted:
			if !step.outcome.valid() || step.prefixLength >= uint32(len(entries)) ||
				step.nextEntryID != entries[step.prefixLength].ID || step.outcome.migrationID != step.nextEntryID ||
				step.outcome.ledgerHead != step.nextEntryID || step.outcome.ledgerLength != step.prefixLength+1 ||
				haveExpectedPrefix && step.prefixLength != expectedPrefix {
				return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "committed entry step is contradictory", nil)
			}
			if step.ambiguousRecovered {
				ambiguousRecovered = append(ambiguousRecovered, step.outcome.migrationID)
			} else {
				applied = append(applied, step.outcome.migrationID)
			}
			expectedPrefix, haveExpectedPrefix = step.outcome.ledgerLength, true
			complete := expectedPrefix == uint32(len(entries))
			if complete != (step.outcome.state == runnerLedgerEntrySuccessEntryCommittedComplete) ||
				!complete && step.outcome.state != runnerLedgerEntrySuccessEntryCommittedNextEntry {
				return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "committed entry classification differs from the verified order", nil)
			}
		case runnerLedgerPreflightStepReenter:
			if step.outcome.valid() || step.result.SchemaBundleDigest != "" || step.result.ManifestDigest != "" ||
				step.result.FinalHead != "" || step.result.Applied != nil || step.result.AmbiguousRecovered != nil || step.nextEntryID != "" ||
				step.prefixLength > uint32(len(entries)) || haveExpectedPrefix && step.prefixLength != expectedPrefix {
				return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "re-entry step is contradictory", nil)
			}
			expectedPrefix, haveExpectedPrefix = step.prefixLength, true
		default:
			return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "runner ledger step is outside the closed state machine", nil)
		}
	}
	return RunResult{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "runner ledger recovery loop exceeded the verified bound", nil)
}

func runnerLedgerRecoveryIterationLimit(entryCount int, policy ExecutionPolicy) (uint64, error) {
	if entryCount <= 0 || uint64(entryCount) > uint64(^uint32(0)) {
		return 0, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "verified runtime entry order is unavailable", nil)
	}
	if policy.Validate() != nil || policy.MaxAttempts == 0 || policy.MaxAttempts > uint64(^uint32(0)) {
		return 0, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "verified execution policy is unavailable", nil)
	}
	perEntry := policy.MaxAttempts*3 + 2
	if perEntry < policy.MaxAttempts || uint64(entryCount) > (maxJSONInteger-1)/perEntry {
		return 0, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "verified recovery iteration bound overflows", nil)
	}
	maximum := uint64(entryCount)*perEntry + 1
	if maximum == 0 || maximum > maxJSONInteger {
		return 0, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "verified recovery iteration bound is invalid", nil)
	}
	return maximum, nil
}

// consumeRunnerLedgerPreflightStep obtains ordinary dispatch data only by
// minting and consuming one exact same-verifier claim inside this closed call.
// It either returns a complete-ledger no-op or consumes one generated
// first-attempt execution permit through the independently reviewed success
// kernel. Unsupported entry and recovery pairs remain closed errors.
func (runner *Runner) consumeRunnerLedgerPreflightStep(ctx context.Context, dsn string, bundle *RuntimeBundle, plans []StatementPlan, evidence EvidenceSession, candidate OwnedCurrentCandidate) (runnerLedgerPreflightStep, error) {
	if runner == nil || ctx == nil || bundle == nil || bundle.Manifest == nil {
		return runnerLedgerPreflightStep{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer", "runner ledger consumer inputs are unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerPreflightStep{}, err
	}
	claim, err := runner.prepareRunnerLedgerPreflightClaim(ctx, dsn, bundle, plans, evidence, candidate)
	if err != nil {
		return runnerLedgerPreflightStep{}, err
	}
	// The evidence binder normally removes a consumed claim. The unconditional
	// revoke also closes cancellation, binder-error, and future validation
	// paths without leaving a live one-shot registry entry.
	defer revokeRunnerLedgerPreflightClaim(claim)

	dispatch, err := runner.claimRunnerLedgerPreflightDispatch(ctx, evidence, candidate, claim)
	if err != nil {
		return runnerLedgerPreflightStep{}, err
	}
	verifiedBundle, err := verifiedRunnerLedgerCatalogBundle(bundle)
	if err != nil {
		return runnerLedgerPreflightStep{}, err
	}
	bundle = verifiedBundle
	if !validOwnedCurrentCandidate(candidate) || bundle.Manifest.ManifestDigest != candidate.verifiedRun.manifestDigest ||
		bundle.Manifest.SchemaBundleDigest != candidate.verifiedRun.schemaBundleDigest ||
		!runnerLedgerConsumerDispatchMatchesCandidate(dispatch, evidence, candidate) {
		return runnerLedgerPreflightStep{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer", "verified runtime changed after claim consumption", nil)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, bundle.Manifest.ManifestDigest)
	if err != nil {
		return runnerLedgerPreflightStep{}, err
	}
	if err := contextAdmissionError(ctx); err != nil {
		return runnerLedgerPreflightStep{}, err
	}
	if !fact.valid() || fact.manifestDigest != bundle.Manifest.ManifestDigest ||
		!runnerLedgerConsumerFactMatchesBundle(fact, evidence, candidate, bundle) {
		return runnerLedgerPreflightStep{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer", "generated consumer fact differs from the verified runtime", nil)
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
			return runnerLedgerPreflightStep{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer-result", "complete ledger result is unavailable or contradictory", nil)
		}
		return runnerLedgerPreflightStep{
			kind:         runnerLedgerPreflightStepComplete,
			prefixLength: fact.dispatch.fact.orderedMigrationPrefixLength,
			result: RunResult{
				SchemaBundleDigest: bundle.Manifest.SchemaBundleDigest,
				ManifestDigest:     bundle.Manifest.ManifestDigest,
				FinalHead:          *head,
				Applied:            []string{},
				AmbiguousRecovered: []string{},
			},
		}, nil
	case runnerLedgerConsumerEntryNotImplemented:
		if _, ok := generatedRunnerLedgerRecoveryAdmissionAction(
			fact.dispatch.fact.disposition, fact.dispatch.fact.recovery.State, fact.dispatch.fact.recovery.Action,
		); ok {
			result, err := runner.consumeRunnerLedgerRecoveryAction(ctx, dsn, bundle, plans, evidence, candidate, fact)
			if err != nil {
				return runnerLedgerPreflightStep{}, err
			}
			return runnerLedgerRecoveryStep(fact, bundle, result)
		}
		permit, err := runner.prepareRunnerLedgerEntryExecutionAdmission(ctx, dsn, bundle, plans, evidence, candidate, fact)
		if err != nil {
			if runnerLedgerEntryExecutionAdmissionUnsupported(err) {
				return runnerLedgerPreflightStep{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer-entry", "entry writer is not implemented", nil)
			}
			return runnerLedgerPreflightStep{}, err
		}
		use, binder := permit.use, permit.evidenceBinder
		useSubject, useBoundary := permit.consumerFactSubject, permit.evidenceBoundary
		outcome, err := runner.executeRunnerLedgerEntrySuccess(ctx, permit, bundle, plans)
		if err != nil {
			return runnerLedgerPreflightStep{}, err
		}
		entries := bundle.Manifest.SchemaBundle.Migrations
		prefixLength := fact.dispatch.fact.orderedMigrationPrefixLength
		expectedState := runnerLedgerEntrySuccessEntryCommittedNextEntry
		if uint64(prefixLength)+1 == uint64(len(entries)) {
			expectedState = runnerLedgerEntrySuccessEntryCommittedComplete
		}
		if !outcome.valid() || fact.dispatch.fact.nextEntry == nil || uint64(prefixLength) >= uint64(len(entries)) ||
			outcome.migrationID != fact.dispatch.fact.nextEntry.MigrationID ||
			outcome.ledgerHead != fact.dispatch.fact.nextEntry.MigrationID ||
			outcome.ledgerLength != prefixLength+1 || outcome.state != expectedState {
			return runnerLedgerPreflightStep{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "committed writer outcome differs from the consumed entry fact", nil)
		}
		if !retireRunnerLedgerEntryExecutionAdmissionUse(binder, use, useSubject, useBoundary) {
			return runnerLedgerPreflightStep{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-entry-loop", "committed writer admission could not be retired", nil)
		}
		return runnerLedgerPreflightStep{
			kind:         runnerLedgerPreflightStepEntryCommitted,
			prefixLength: prefixLength,
			nextEntryID:  fact.dispatch.fact.nextEntry.MigrationID,
			outcome:      outcome,
		}, nil
	case runnerLedgerConsumerRecoveryNotImplemented:
		if _, ok := generatedRunnerLedgerRecoveryAdmissionAction(
			fact.dispatch.fact.disposition, fact.dispatch.fact.recovery.State, fact.dispatch.fact.recovery.Action,
		); ok {
			result, err := runner.consumeRunnerLedgerRecoveryAction(ctx, dsn, bundle, plans, evidence, candidate, fact)
			if err != nil {
				return runnerLedgerPreflightStep{}, err
			}
			return runnerLedgerRecoveryStep(fact, bundle, result)
		}
		return runnerLedgerPreflightStep{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer-recovery", "recovery consumer and writer are not implemented", nil)
	default:
		return runnerLedgerPreflightStep{}, fail(CodeProjectionNotImplemented, "runner-ledger-consumer", "runner ledger consumer action is not implemented", nil)
	}
}

func runnerLedgerRecoveryStep(fact runnerLedgerConsumerFact, bundle *RuntimeBundle, result runnerLedgerRecoveryActionResult) (runnerLedgerPreflightStep, error) {
	if !fact.valid() || bundle == nil || bundle.Manifest == nil || !result.valid() {
		return runnerLedgerPreflightStep{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer-recovery", "recovery action returned an invalid ordinary result", nil)
	}
	prefixLength := fact.dispatch.fact.orderedMigrationPrefixLength
	switch result.kind {
	case runnerLedgerRecoveryActionReenter:
		return runnerLedgerPreflightStep{kind: runnerLedgerPreflightStepReenter, prefixLength: prefixLength}, nil
	case runnerLedgerRecoveryActionEntryCommitted:
		entries := bundle.Manifest.SchemaBundle.Migrations
		if prefixLength >= uint32(len(entries)) || fact.dispatch.fact.nextEntry != nil && fact.dispatch.fact.nextEntry.MigrationID != result.outcome.migrationID ||
			result.outcome.migrationID != entries[prefixLength].ID || result.outcome.ledgerLength != prefixLength+1 {
			return runnerLedgerPreflightStep{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer-recovery", "recovery action result differs from the verified entry order", nil)
		}
		return runnerLedgerPreflightStep{
			kind: runnerLedgerPreflightStepEntryCommitted, prefixLength: prefixLength,
			nextEntryID: result.outcome.migrationID, outcome: result.outcome,
			ambiguousRecovered: result.ambiguousRecovered,
		}, nil
	default:
		return runnerLedgerPreflightStep{}, fail(CodeEvidenceRecoveryRequired, "runner-ledger-consumer-recovery", "recovery action result is outside the closed state machine", nil)
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
