package migration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerLedgerPreflightGeneratedProfile(t *testing.T) {
	if !generatedRunnerLedgerPreflightProfile.valid() {
		t.Fatal("generated profile is invalid")
	}
	for _, value := range []string{
		generatedRunnerLedgerPreflightProfile.profileDigest,
		runnerLedgerPreflightRegistryDigest,
		runnerLedgerPreflightStateMachineDigest,
		runnerLedgerPreflightPolicyDigest,
	} {
		if Digest(value).Validate() != nil {
			t.Fatalf("invalid generated digest %q", value)
		}
	}
	want := map[string]runnerLedgerPreflightDisposition{
		"observe_complete":                  runnerLedgerPreflightCompleteReturnSuccess,
		"observe_empty":                     runnerLedgerPreflightEmptyBrandNew,
		"observe_partial_next_entry":        runnerLedgerPreflightPartialNextEntry,
		"observe_partial_retry_or_recovery": runnerLedgerPreflightPartialRetryOrRecovery,
		"observe_unknown_or_failed":         runnerLedgerPreflightUnknownOrFailed,
	}
	for event, disposition := range want {
		got, ok := runnerLedgerPreflightDispositionForEvent(event)
		if !ok || got != disposition {
			t.Fatalf("transition %q = %q,%v, want %q,true", event, got, ok, disposition)
		}
	}
	if got, ok := runnerLedgerPreflightDispositionForEvent("caller_selected"); ok || got != "" {
		t.Fatalf("unknown event entered state machine: %q,%v", got, ok)
	}
}

func TestRunnerLedgerPreflightFactClosedStateTable(t *testing.T) {
	head := "000001"
	next := runnerLedgerPreflightNextEntry{MigrationID: "000002", EntryDigest: testDigest("entry-2")}
	emptyNext := runnerLedgerPreflightNextEntry{MigrationID: "000001", EntryDigest: testDigest("entry-1")}
	base := runnerLedgerPreflightFactInput{
		SchemaBundleDigest:               testDigest("schema"),
		ExecutionLineageDigest:           testDigest("lineage"),
		OrderedMigrationPrefixDigest:     testDigest("prefix"),
		LastAppliedCatalogContractDigest: testDigest("catalog"),
	}
	cases := []struct {
		name        string
		disposition runnerLedgerPreflightDisposition
		input       runnerLedgerPreflightFactInput
	}{
		{
			name: "empty-brand-new", disposition: runnerLedgerPreflightEmptyBrandNew,
			input: withRunnerLedgerPreflight(base, 0, nil, &emptyNext, &runnerLedgerPreflightRecoveryDisposition{RecoveryBrandNew, RecoveryBeginFirstAttempt}),
		},
		{
			name: "partial-next-entry", disposition: runnerLedgerPreflightPartialNextEntry,
			input: withRunnerLedgerPreflight(base, 1, &head, &next, &runnerLedgerPreflightRecoveryDisposition{RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry}),
		},
		{
			name: "partial-retry", disposition: runnerLedgerPreflightPartialRetryOrRecovery,
			input: withRunnerLedgerPreflight(base, 1, &head, nil, &runnerLedgerPreflightRecoveryDisposition{RecoveryDanglingCommitIntent, RecoveryReconcileCommit}),
		},
		{
			name: "complete", disposition: runnerLedgerPreflightCompleteReturnSuccess,
			input: withRunnerLedgerPreflight(base, 1, &head, nil, &runnerLedgerPreflightRecoveryDisposition{RecoveryCompleted, RecoveryReturnSuccess}),
		},
		{
			name: "unknown", disposition: runnerLedgerPreflightUnknownOrFailed,
			input: withRunnerLedgerPreflight(base, 1, &head, nil, nil),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fact, err := bindRunnerLedgerPreflightFact(generatedRunnerLedgerPreflightProfile, tc.disposition, tc.input)
			if err != nil || !fact.valid() || len(fact.canonicalBytes()) == 0 {
				t.Fatalf("bind failed fact=%+v err=%v", fact, err)
			}
			copy := fact
			if !copy.valid() || copy.subjectDigest != fact.subjectDigest {
				t.Fatal("ordinary value copy lost its exact commitment")
			}
			clone := fact.clone()
			if !clone.valid() || clone.subjectDigest != fact.subjectDigest {
				t.Fatal("clone lost its exact commitment")
			}
			canonical := fact.canonicalBytes()
			canonical[0] ^= 0xff
			if !fact.valid() || string(fact.canonicalBytes()) == string(canonical) {
				t.Fatal("canonicalBytes did not return an owned copy")
			}
			if tc.input.OrderedMigrationPrefixHead != nil {
				*tc.input.OrderedMigrationPrefixHead = "999999"
			}
			if tc.input.NextEntry != nil {
				tc.input.NextEntry.MigrationID = "999999"
			}
			if !fact.valid() {
				t.Fatal("caller input mutation changed the bound fact")
			}
		})
	}
}

func TestRunnerLedgerPreflightFactRejectsZeroCrossProfileAndTamper(t *testing.T) {
	if (runnerLedgerPreflightFact{}).valid() || (runnerLedgerPreflightFact{}).canonicalBytes() != nil {
		t.Fatal("zero fact is valid")
	}
	profile := generatedRunnerLedgerPreflightProfile
	profile.profileDigest = testDigest("foreign-profile").String()
	if fact, err := bindRunnerLedgerPreflightFact(profile, runnerLedgerPreflightUnknownOrFailed, runnerLedgerPreflightFactInput{}); fact != (runnerLedgerPreflightFact{}) || !IsCode(err, CodeUntrusted) {
		t.Fatalf("cross-profile bind = %+v,%v", fact, err)
	}

	head := "000001"
	input := runnerLedgerPreflightFactInput{
		SchemaBundleDigest: testDigest("schema"), ExecutionLineageDigest: testDigest("lineage"),
		OrderedMigrationPrefixDigest: testDigest("prefix"), OrderedMigrationPrefixLength: 1,
		OrderedMigrationPrefixHead: &head, LastAppliedCatalogContractDigest: testDigest("catalog"),
	}
	fact, err := bindRunnerLedgerPreflightFact(generatedRunnerLedgerPreflightProfile, runnerLedgerPreflightUnknownOrFailed, input)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*runnerLedgerPreflightFact){
		func(value *runnerLedgerPreflightFact) { value.profileID = "caller/v1" },
		func(value *runnerLedgerPreflightFact) { value.profileDigest = testDigest("other").String() },
		func(value *runnerLedgerPreflightFact) { value.registryDigest = testDigest("other").String() },
		func(value *runnerLedgerPreflightFact) { value.schemaBundleDigest = testDigest("other") },
		func(value *runnerLedgerPreflightFact) { value.orderedMigrationPrefixLength = 0 },
		func(value *runnerLedgerPreflightFact) { value.disposition = runnerLedgerPreflightCompleteReturnSuccess },
		func(value *runnerLedgerPreflightFact) { value.canonical += " " },
		func(value *runnerLedgerPreflightFact) { value.subjectDigest = testDigest("other") },
	}
	for index, mutate := range mutations {
		candidate := fact.clone()
		mutate(&candidate)
		if candidate.valid() {
			t.Fatalf("mutation %d remained valid", index)
		}
	}
}

func TestRunnerLedgerPreflightFactRejectsDispositionCrossBinding(t *testing.T) {
	head := "000001"
	base := runnerLedgerPreflightFactInput{
		SchemaBundleDigest: testDigest("schema"), ExecutionLineageDigest: testDigest("lineage"),
		OrderedMigrationPrefixDigest: testDigest("prefix"), OrderedMigrationPrefixLength: 1,
		OrderedMigrationPrefixHead: &head, LastAppliedCatalogContractDigest: testDigest("catalog"),
		Recovery: &runnerLedgerPreflightRecoveryDisposition{RecoveryCompleted, RecoveryReturnSuccess},
	}
	for _, disposition := range []runnerLedgerPreflightDisposition{
		runnerLedgerPreflightEmptyBrandNew,
		runnerLedgerPreflightPartialNextEntry,
		runnerLedgerPreflightPartialRetryOrRecovery,
		runnerLedgerPreflightUnknownOrFailed,
		"caller_selected",
	} {
		if fact, err := bindRunnerLedgerPreflightFact(generatedRunnerLedgerPreflightProfile, disposition, base); fact.valid() || err == nil {
			t.Fatalf("cross-bound disposition %q accepted: %+v,%v", disposition, fact, err)
		}
	}
}

func TestRunnerLedgerPreflightRecoveryDispositionMatrixIsClosed(t *testing.T) {
	allowed := map[string]bool{
		"complete_return_success\x00completed\x00return_success":                             true,
		"empty_brand_new\x00brand_new\x00begin_first_attempt":                                true,
		"empty_brand_new\x00brand_new_inherited\x00begin_first_attempt":                      true,
		"empty_brand_new\x00brand_new_inherited\x00begin_next_attempt":                       true,
		"partial_next_entry\x00brand_new_inherited\x00begin_first_attempt_next_entry":        true,
		"partial_next_entry\x00terminal\x00begin_first_attempt_next_entry":                   true,
		"partial_retry_or_recovery\x00brand_new_inherited\x00begin_first_attempt":            true,
		"partial_retry_or_recovery\x00brand_new_inherited\x00begin_next_attempt":             true,
		"partial_retry_or_recovery\x00dangling_statement_intent\x00append_aborted_retryable": true,
		"partial_retry_or_recovery\x00dangling_statement_intent\x00append_aborted_terminal":  true,
		"partial_retry_or_recovery\x00dangling_intermediate\x00append_aborted_retryable":     true,
		"partial_retry_or_recovery\x00dangling_intermediate\x00append_aborted_terminal":      true,
		"partial_retry_or_recovery\x00dangling_commit_intent\x00reconcile_commit":            true,
		"partial_retry_or_recovery\x00ambiguous_unresolved\x00reconcile_commit":              true,
		"partial_retry_or_recovery\x00terminal\x00begin_next_attempt":                        true,
		"partial_retry_or_recovery\x00terminal\x00return_failure":                            true,
		"partial_retry_or_recovery\x00divergent\x00return_failure":                           true,
	}
	dispositions := []runnerLedgerPreflightDisposition{
		runnerLedgerPreflightCompleteReturnSuccess,
		runnerLedgerPreflightEmptyBrandNew,
		runnerLedgerPreflightPartialNextEntry,
		runnerLedgerPreflightPartialRetryOrRecovery,
	}
	states := []RecoveryState{
		RecoveryBrandNew, RecoveryBrandNewInherited, RecoveryCompleted,
		RecoveryDanglingStatementIntent, RecoveryDanglingIntermediate,
		RecoveryDanglingCommitIntent, RecoveryAmbiguousUnresolved,
		RecoveryTerminal, RecoveryDivergent,
	}
	actions := []RecoveryAction{
		RecoveryBeginFirstAttempt, RecoveryAppendAbortedRetryable,
		RecoveryAppendAbortedTerminal, RecoveryReconcileCommit,
		RecoveryBeginNextAttempt, RecoveryBeginFirstAttemptNextEntry,
		RecoveryReturnSuccess, RecoveryReturnFailure,
	}
	base := runnerLedgerPreflightFactInput{
		SchemaBundleDigest: testDigest("schema"), ExecutionLineageDigest: testDigest("lineage"),
		OrderedMigrationPrefixDigest: testDigest("prefix"), LastAppliedCatalogContractDigest: testDigest("catalog"),
	}
	for _, disposition := range dispositions {
		for _, state := range states {
			for _, action := range actions {
				input := base
				head := "000001"
				input.OrderedMigrationPrefixLength = 1
				input.OrderedMigrationPrefixHead = &head
				switch disposition {
				case runnerLedgerPreflightEmptyBrandNew:
					input.OrderedMigrationPrefixLength = 0
					input.OrderedMigrationPrefixHead = nil
					next := runnerLedgerPreflightNextEntry{MigrationID: "000002", EntryDigest: testDigest("entry-2")}
					input.NextEntry = &next
				case runnerLedgerPreflightPartialNextEntry:
					next := runnerLedgerPreflightNextEntry{MigrationID: "000002", EntryDigest: testDigest("entry-2")}
					input.NextEntry = &next
				}
				input.Recovery = &runnerLedgerPreflightRecoveryDisposition{State: state, Action: action}
				key := string(disposition) + "\x00" + string(state) + "\x00" + string(action)
				fact, err := bindRunnerLedgerPreflightFact(generatedRunnerLedgerPreflightProfile, disposition, input)
				if allowed[key] {
					if err != nil || !fact.valid() {
						t.Fatalf("allowed recovery pair %q rejected: fact=%+v err=%v", key, fact, err)
					}
				} else if err == nil || fact.valid() {
					t.Fatalf("unlisted recovery pair %q accepted: fact=%+v err=%v", key, fact, err)
				}
			}
		}
	}

	unknown := base
	head := "000001"
	unknown.OrderedMigrationPrefixLength = 1
	unknown.OrderedMigrationPrefixHead = &head
	unknown.Recovery = nil
	if fact, err := bindRunnerLedgerPreflightFact(generatedRunnerLedgerPreflightProfile, runnerLedgerPreflightUnknownOrFailed, unknown); err != nil || !fact.valid() {
		t.Fatalf("unknown/failed nil recovery rejected: fact=%+v err=%v", fact, err)
	}
	for _, state := range states {
		for _, action := range actions {
			candidate := unknown
			candidate.Recovery = &runnerLedgerPreflightRecoveryDisposition{State: state, Action: action}
			if fact, err := bindRunnerLedgerPreflightFact(generatedRunnerLedgerPreflightProfile, runnerLedgerPreflightUnknownOrFailed, candidate); err == nil || fact.valid() {
				t.Fatalf("unknown/failed recovery pair accepted: state=%q action=%q fact=%+v err=%v", state, action, fact, err)
			}
		}
	}
}

func TestRunnerLedgerPreflightSliceAHasNoProductionConsumerOrAuthoritySurface(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	consumerCalls := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if name == "runner_ledger_preflight_profile.go" || name == "runner_ledger_preflight_profile_generated.go" {
				if path == "database/sql" || path == "net/http" || strings.Contains(path, "pgx") {
					t.Fatalf("Slice A imports forbidden authority package %q", path)
				}
			}
		}
		file, err = parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "bindRunnerLedgerPreflightFact" {
				consumerCalls++
			}
			return true
		})
	}
	if consumerCalls != 1 {
		t.Fatalf("generated fact has %d production consumers; want the one reviewed Slice C binder", consumerCalls)
	}
}

func withRunnerLedgerPreflight(base runnerLedgerPreflightFactInput, length uint32, head *string, next *runnerLedgerPreflightNextEntry, recovery *runnerLedgerPreflightRecoveryDisposition) runnerLedgerPreflightFactInput {
	base.OrderedMigrationPrefixLength = length
	base.OrderedMigrationPrefixHead = head
	base.NextEntry = next
	base.Recovery = recovery
	return base
}
