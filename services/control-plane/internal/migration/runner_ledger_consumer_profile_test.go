package migration

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerLedgerConsumerKeepsGeneratedPreflightV1SameBits(t *testing.T) {
	bytes, err := os.ReadFile("runner_ledger_preflight_profile_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprintf("%x", sha256.Sum256(bytes)), "599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112"; got != want {
		t.Fatalf("immutable preflight v1 Go profile changed: got %s want %s", got, want)
	}
}

func TestRunnerLedgerConsumerGeneratedProfileBindsImmutablePreflightV1(t *testing.T) {
	if !generatedRunnerLedgerConsumerProfile.valid() {
		t.Fatal("generated consumer profile is invalid")
	}
	for _, value := range []string{
		generatedRunnerLedgerConsumerProfile.profileDigest,
		runnerLedgerConsumerRegistryDigest,
		runnerLedgerConsumerStateMachineDigest,
		runnerLedgerConsumerPolicyDigest,
		runnerLedgerConsumerBoundPreflightRegistryDigest,
		runnerLedgerConsumerBoundPreflightStateMachineDigest,
		runnerLedgerConsumerBoundPreflightPolicyDigest,
		runnerLedgerConsumerBoundPreflightProfileDigest,
	} {
		if Digest(value).Validate() != nil {
			t.Fatalf("invalid generated digest %q", value)
		}
	}
	if runnerLedgerConsumerBoundPreflightRegistryID != "cloud-agents/platform/runner-ledger-preflight" ||
		runnerLedgerConsumerBoundPreflightRegistryDigest != runnerLedgerPreflightRegistryDigest ||
		runnerLedgerConsumerBoundPreflightStateMachineDigest != runnerLedgerPreflightStateMachineDigest ||
		runnerLedgerConsumerBoundPreflightPolicyDigest != runnerLedgerPreflightPolicyDigest ||
		runnerLedgerConsumerBoundPreflightProfileID != generatedRunnerLedgerPreflightProfile.profileID ||
		runnerLedgerConsumerBoundPreflightProfileDigest != generatedRunnerLedgerPreflightProfile.profileDigest {
		t.Fatal("consumer profile does not bind the exact immutable preflight v1 identity")
	}
}

func TestRunnerLedgerConsumerGeneratedMatrixMapsOneFiveAndEleven(t *testing.T) {
	type pair struct {
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
		want        runnerLedgerConsumerAction
	}
	pairs := []pair{
		{runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess, runnerLedgerConsumerReturnSuccessNoop},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt, runnerLedgerConsumerEntryNotImplemented},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt, runnerLedgerConsumerEntryNotImplemented},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt, runnerLedgerConsumerEntryNotImplemented},
		{runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry, runnerLedgerConsumerEntryNotImplemented},
		{runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry, runnerLedgerConsumerEntryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryBrandNewInherited, RecoveryBeginNextAttempt, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingStatementIntent, RecoveryAppendAbortedTerminal, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingIntermediate, RecoveryAppendAbortedRetryable, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingIntermediate, RecoveryAppendAbortedTerminal, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDanglingCommitIntent, RecoveryReconcileCommit, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryAmbiguousUnresolved, RecoveryReconcileCommit, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryReturnFailure, runnerLedgerConsumerRecoveryNotImplemented},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDivergent, RecoveryReturnFailure, runnerLedgerConsumerRecoveryNotImplemented},
	}
	counts := map[runnerLedgerConsumerAction]int{}
	for _, pair := range pairs {
		got, ok := generatedRunnerLedgerConsumerAction(pair.disposition, pair.state, pair.action)
		if !ok || got != pair.want {
			t.Fatalf("mapping %q/%q/%q = %q,%v; want %q,true", pair.disposition, pair.state, pair.action, got, ok, pair.want)
		}
		counts[got]++
	}
	if len(pairs) != 17 || counts[runnerLedgerConsumerReturnSuccessNoop] != 1 ||
		counts[runnerLedgerConsumerEntryNotImplemented] != 5 || counts[runnerLedgerConsumerRecoveryNotImplemented] != 11 {
		t.Fatalf("closed matrix count drifted: total=%d counts=%v", len(pairs), counts)
	}
	if got, ok := generatedRunnerLedgerConsumerAction(runnerLedgerPreflightCompleteReturnSuccess, RecoveryTerminal, RecoveryReturnSuccess); ok || got != "" {
		t.Fatalf("unlisted pair entered consumer state machine: %q,%v", got, ok)
	}
}

func TestRunnerLedgerConsumerFactClosedActionTable(t *testing.T) {
	manifest := testDigest("runner-ledger-consumer-manifest")
	for _, tc := range []struct {
		name     string
		dispatch runnerLedgerPreflightDispatch
		want     runnerLedgerConsumerAction
	}{
		{"complete", testRunnerLedgerConsumerDispatch(t, runnerLedgerPreflightCompleteReturnSuccess), runnerLedgerConsumerReturnSuccessNoop},
		{"entry", testRunnerLedgerConsumerDispatch(t, runnerLedgerPreflightEmptyBrandNew), runnerLedgerConsumerEntryNotImplemented},
		{"recovery", testRunnerLedgerConsumerDispatch(t, runnerLedgerPreflightPartialRetryOrRecovery), runnerLedgerConsumerRecoveryNotImplemented},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, tc.dispatch, manifest)
			if err != nil || !fact.valid() || fact.action != tc.want || len(fact.canonicalBytes()) == 0 {
				t.Fatalf("bind = %+v,%v; want valid %q", fact, err, tc.want)
			}
			if tc.dispatch.recoveryMigrationID != nil {
				*tc.dispatch.recoveryMigrationID = "999999"
			}
			if tc.dispatch.fact.orderedMigrationPrefixHead != nil {
				*tc.dispatch.fact.orderedMigrationPrefixHead = "999999"
			}
			if !fact.valid() {
				t.Fatal("caller dispatch mutation changed the bound fact")
			}
			copy := fact
			clone := fact.clone()
			if !copy.valid() || !clone.valid() || clone.subjectDigest != fact.subjectDigest {
				t.Fatal("ordinary copy or clone lost its exact commitment")
			}
			owned := fact.canonicalBytes()
			owned[0] ^= 0xff
			if !fact.valid() || string(owned) == string(fact.canonicalBytes()) {
				t.Fatal("canonicalBytes did not return an owned copy")
			}
		})
	}
}

func TestRunnerLedgerConsumerFactRejectsZeroCrossProfileAndEveryBoundMutation(t *testing.T) {
	if (runnerLedgerConsumerFact{}).valid() || (runnerLedgerConsumerFact{}).canonicalBytes() != nil {
		t.Fatal("zero consumer fact is valid")
	}
	dispatch := testRunnerLedgerConsumerDispatch(t, runnerLedgerPreflightCompleteReturnSuccess)
	profile := generatedRunnerLedgerConsumerProfile
	profile.profileDigest = testDigest("foreign-consumer-profile").String()
	if fact, err := bindRunnerLedgerConsumerFact(profile, dispatch, testDigest("manifest")); fact != (runnerLedgerConsumerFact{}) || !IsCode(err, CodeUntrusted) {
		t.Fatalf("cross-profile bind = %+v,%v", fact, err)
	}
	fact, err := bindRunnerLedgerConsumerFact(generatedRunnerLedgerConsumerProfile, dispatch, testDigest("manifest"))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*runnerLedgerConsumerFact){
		func(value *runnerLedgerConsumerFact) { value.profileID = "caller/v1" },
		func(value *runnerLedgerConsumerFact) { value.profileDigest = testDigest("other").String() },
		func(value *runnerLedgerConsumerFact) { value.registryDigest = testDigest("other").String() },
		func(value *runnerLedgerConsumerFact) { value.stateMachineDigest = testDigest("other").String() },
		func(value *runnerLedgerConsumerFact) { value.policyDigest = testDigest("other").String() },
		func(value *runnerLedgerConsumerFact) { value.boundPreflightRegistryID = "caller/preflight" },
		func(value *runnerLedgerConsumerFact) {
			value.boundPreflightRegistryDigest = testDigest("other").String()
		},
		func(value *runnerLedgerConsumerFact) {
			value.boundPreflightStateMachineDigest = testDigest("other").String()
		},
		func(value *runnerLedgerConsumerFact) { value.boundPreflightPolicyDigest = testDigest("other").String() },
		func(value *runnerLedgerConsumerFact) { value.boundPreflightProfileID = "caller/v1" },
		func(value *runnerLedgerConsumerFact) {
			value.boundPreflightProfileDigest = testDigest("other").String()
		},
		func(value *runnerLedgerConsumerFact) { value.action = runnerLedgerConsumerEntryNotImplemented },
		func(value *runnerLedgerConsumerFact) { value.dispatch.subjectDigest = testDigest("other") },
		func(value *runnerLedgerConsumerFact) { value.manifestDigest = testDigest("other") },
		func(value *runnerLedgerConsumerFact) { value.canonical += " " },
		func(value *runnerLedgerConsumerFact) { value.subjectDigest = testDigest("other") },
	}
	for index, mutate := range mutations {
		candidate := fact.clone()
		mutate(&candidate)
		if candidate.valid() {
			t.Fatalf("mutation %d remained valid", index)
		}
	}
}

func TestRunnerLedgerConsumerSliceBHasOneClosedProductionConsumerAndNoAuthoritySurface(t *testing.T) {
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
			if name == "runner_ledger_consumer_profile.go" || name == "runner_ledger_consumer_profile_generated.go" {
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
			if ok && identifier.Name == "bindRunnerLedgerConsumerFact" {
				if name != "runner_ledger_consumer_service.go" {
					t.Fatalf("generated consumer fact has an unreviewed caller in %s", name)
				}
				consumerCalls++
			}
			return true
		})
	}
	if consumerCalls != 1 {
		t.Fatalf("generated consumer fact has %d production consumers in Slice B; want one", consumerCalls)
	}
}

func testRunnerLedgerConsumerDispatch(t *testing.T, disposition runnerLedgerPreflightDisposition) runnerLedgerPreflightDispatch {
	t.Helper()
	head := "000001"
	input := runnerLedgerPreflightFactInput{
		SchemaBundleDigest: testDigest("consumer-schema"), ExecutionLineageDigest: testDigest("consumer-lineage"),
		OrderedMigrationPrefixDigest: testDigest("consumer-prefix"), LastAppliedCatalogContractDigest: testDigest("consumer-catalog"),
	}
	dispatch := runnerLedgerPreflightDispatch{
		journalIdentityDigest: testDigest("consumer-journal"), runnerProjectionDecisionDigest: testDigest("consumer-projection"),
		recoverySnapshotDigest: testDigest("consumer-recovery-snapshot"), recoveryTailDigest: testDigest("consumer-recovery-tail"),
	}
	switch disposition {
	case runnerLedgerPreflightCompleteReturnSuccess:
		input.OrderedMigrationPrefixLength, input.OrderedMigrationPrefixHead = 1, &head
		input.Recovery = &runnerLedgerPreflightRecoveryDisposition{State: RecoveryCompleted, Action: RecoveryReturnSuccess}
		dispatch.kind = runnerLedgerPreflightDispatchReturnSuccess
		dispatch.recoveryMigrationID = &head
		attempt := uint32(1)
		dispatch.recoveryAttemptIndex = &attempt
	case runnerLedgerPreflightEmptyBrandNew:
		input.NextEntry = &runnerLedgerPreflightNextEntry{MigrationID: "000001", EntryDigest: testDigest("consumer-entry")}
		input.Recovery = &runnerLedgerPreflightRecoveryDisposition{State: RecoveryBrandNew, Action: RecoveryBeginFirstAttempt}
		dispatch.kind = runnerLedgerPreflightDispatchEntry
	case runnerLedgerPreflightPartialRetryOrRecovery:
		input.OrderedMigrationPrefixLength, input.OrderedMigrationPrefixHead = 1, &head
		input.Recovery = &runnerLedgerPreflightRecoveryDisposition{State: RecoveryDanglingCommitIntent, Action: RecoveryReconcileCommit}
		dispatch.kind = runnerLedgerPreflightDispatchRecovery
		migration := "000002"
		attempt := uint32(1)
		dispatch.recoveryMigrationID, dispatch.recoveryAttemptIndex = &migration, &attempt
	default:
		t.Fatalf("unsupported test disposition %q", disposition)
	}
	fact, err := bindRunnerLedgerPreflightFact(generatedRunnerLedgerPreflightProfile, disposition, input)
	if err != nil {
		t.Fatal(err)
	}
	dispatch.fact = fact
	dispatch.subjectDigest = runnerLedgerPreflightDispatchSubjectDigest(dispatch)
	if !dispatch.valid() {
		t.Fatalf("test dispatch for %q is invalid: %+v", disposition, dispatch)
	}
	return dispatch
}
