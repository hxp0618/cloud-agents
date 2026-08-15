package migration

import "context"

// runCurrentSingleEntry is the only orchestration seam that may consume the
// chain of runner authorities after a durable StatementIntent. It never holds
// two independently usable authorities for the same transition and returns no
// database or evidence capability.
func (runner *Runner) runCurrentSingleEntry(ctx context.Context, durable *runnerDurableCurrentStatementIntent, bundle *RuntimeBundle, plans []StatementPlan) (RunResult, error) {
	failDurable := func(primary error) (RunResult, error) {
		return RunResult{}, closeRunnerDurableCurrentStatementIntent(durable, primary)
	}
	if runner == nil || ctx == nil {
		return failDurable(fail(CodeTransactionBoundary, "runner-current-execution", "current execution context or runner is unavailable", nil))
	}
	if err := validateRunnerCurrentExecutionScope(bundle, plans); err != nil {
		return failDurable(err)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return failDurable(mapRunnerStatementExecutionError(contextErr))
	}

	executed, err := runner.executeCurrentStatement(ctx, durable)
	if err != nil {
		return RunResult{}, err
	}
	after, err := runner.projectCurrentStatementAfter(ctx, executed)
	if err != nil {
		return RunResult{}, err
	}
	preledger, err := runner.projectCurrentPreledger(ctx, after)
	if err != nil {
		return RunResult{}, err
	}
	intermediate, err := runner.appendCurrentFinalIntermediate(ctx, preledger)
	if err != nil {
		return RunResult{}, err
	}
	readback, err := runner.insertAndReadbackCurrentLedger(ctx, intermediate)
	if err != nil {
		return RunResult{}, err
	}
	if !validRunnerReadbackCurrentLedger(readback) || readback.plan.MigrationID != bundle.Manifest.SchemaBundle.Migrations[0].ID || readback.ledgerHead == "" || readback.ledgerLength != 1 {
		return RunResult{}, closeRunnerReadbackCurrentLedger(readback, fail(CodeTransactionBoundary, "runner-current-execution-result", "current ledger result is unavailable or contradictory", nil))
	}
	migrationID := readback.plan.MigrationID
	finalHead := readback.ledgerHead
	commitIntent, err := runner.appendCurrentCommitIntent(ctx, readback)
	if err != nil {
		return RunResult{}, err
	}
	closed, err := runner.commitCurrentTransaction(ctx, commitIntent)
	if err != nil {
		return RunResult{}, err
	}
	terminal, err := runner.appendCommittedTerminal(ctx, closed)
	if err != nil {
		return RunResult{}, err
	}
	if !validRunnerDurableCommittedTerminal(terminal) || !terminal.bundleComplete || terminal.nextAction != RecoveryReturnSuccess || terminal.terminal.MigrationID != migrationID || terminal.terminal.Outcome != "committed" {
		return RunResult{}, closeRunnerDurableCommittedTerminal(terminal, fail(CodeEvidenceJournalFailed, "runner-current-execution-result", "committed terminal does not prove bundle completion", nil))
	}
	result := RunResult{
		SchemaBundleDigest: bundle.Manifest.SchemaBundleDigest,
		ManifestDigest:     bundle.Manifest.ManifestDigest,
		FinalHead:          finalHead,
		Applied:            []string{migrationID},
	}
	if err := closeRunnerDurableCommittedTerminal(terminal, nil); err != nil {
		return RunResult{}, err
	}
	return result, nil
}

func validateRunnerCurrentExecutionScope(bundle *RuntimeBundle, plans []StatementPlan) error {
	if bundle == nil || bundle.Manifest == nil {
		return fail(CodeInvalidManifest, "runner-current-execution-scope", "verified runtime bundle is unavailable", nil)
	}
	entries := bundle.Manifest.SchemaBundle.Migrations
	if len(entries) != 1 || len(plans) != 1 {
		return fail(CodeProjectionNotImplemented, "runner-current-execution-scope", "only one exact statement in one migration is implemented", nil)
	}
	plan, err := cloneRunnerStatementIntentPlan(plans[0])
	if err != nil || plan.MigrationID != entries[0].ID || plan.StatementIndex != 0 {
		return fail(CodeUntrusted, "runner-current-execution-scope", "exact statement plan differs from the sole migration", nil)
	}
	return nil
}
