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

func TestRunnerLedgerEntryAdmissionKeepsPreflightAndConsumerV1SameBits(t *testing.T) {
	for path, want := range map[string]string{
		"runner_ledger_preflight_profile_generated.go": "599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112",
		"runner_ledger_consumer_profile_generated.go":  "afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928",
	} {
		bytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(bytes)); got != want {
			t.Fatalf("immutable runner ledger v1 profile %s changed: got %s want %s", path, got, want)
		}
	}
}

func TestRunnerLedgerEntryAdmissionGeneratedProfileBindsConsumerV1(t *testing.T) {
	if !generatedRunnerLedgerEntryAdmissionProfile.valid() {
		t.Fatal("generated entry-admission profile is invalid")
	}
	if runnerLedgerEntryAdmissionBoundConsumerRegistryID != "cloud-agents/platform/runner-ledger-consumer" ||
		runnerLedgerEntryAdmissionBoundConsumerRegistryDigest != runnerLedgerConsumerRegistryDigest ||
		runnerLedgerEntryAdmissionBoundConsumerStateMachineDigest != runnerLedgerConsumerStateMachineDigest ||
		runnerLedgerEntryAdmissionBoundConsumerPolicyDigest != runnerLedgerConsumerPolicyDigest ||
		runnerLedgerEntryAdmissionBoundConsumerProfileID != generatedRunnerLedgerConsumerProfile.profileID ||
		runnerLedgerEntryAdmissionBoundConsumerProfileDigest != generatedRunnerLedgerConsumerProfile.profileDigest {
		t.Fatal("entry-admission profile does not bind exact immutable consumer v1")
	}
}

func TestRunnerLedgerEntryAdmissionGeneratedMatrixMapsExactlyFiveEntryPairs(t *testing.T) {
	type pair struct {
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
	}
	pairs := []pair{
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt},
		{runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry},
		{runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry},
	}
	for _, pair := range pairs {
		got, ok := generatedRunnerLedgerEntryAdmissionAction(pair.disposition, pair.state, pair.action)
		if !ok || got != runnerLedgerEntryAdmissionPrepare {
			t.Fatalf("entry pair %q/%q/%q = %q,%v", pair.disposition, pair.state, pair.action, got, ok)
		}
	}
	if len(pairs) != generatedRunnerLedgerEntryAdmissionPairCount {
		t.Fatalf("pair count = %d; generated=%d", len(pairs), generatedRunnerLedgerEntryAdmissionPairCount)
	}
	for _, rejected := range []pair{
		{runnerLedgerPreflightCompleteReturnSuccess, RecoveryCompleted, RecoveryReturnSuccess},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt},
		{runnerLedgerPreflightPartialRetryOrRecovery, RecoveryDivergent, RecoveryReturnFailure},
	} {
		if got, ok := generatedRunnerLedgerEntryAdmissionAction(rejected.disposition, rejected.state, rejected.action); ok || got != "" {
			t.Fatalf("non-entry pair entered admission matrix: %q/%q/%q = %q,%v", rejected.disposition, rejected.state, rejected.action, got, ok)
		}
	}
}

func TestRunnerLedgerEntryAdmissionProfileRejectsIdentityAndBoundaryMutation(t *testing.T) {
	mutations := []func(*runnerLedgerEntryAdmissionProfile){
		func(value *runnerLedgerEntryAdmissionProfile) { value.profileID = "caller/v1" },
		func(value *runnerLedgerEntryAdmissionProfile) {
			value.profileDigest = testDigest("foreign-profile").String()
		},
		func(value *runnerLedgerEntryAdmissionProfile) { value.consumerProfileBinding = "caller_selected" },
		func(value *runnerLedgerEntryAdmissionProfile) {
			value.consumedConsumerFactBinding = "ordinary_dispatch"
		},
		func(value *runnerLedgerEntryAdmissionProfile) { value.currentEvidenceBinding = "before_only" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.selectedEntryBinding = "caller_selected" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.planClosureBinding = "unbound" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.databaseSessionBinding = "any_session" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.ledgerPrefixBinding = "before_only" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.catalogProjectionBinding = "ordinary_result" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.advisoryLockBinding = "released_before_bind" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.closeUnlockUnknownPrecedence = "ignore" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.entryWriterBoundary = "implemented" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.databaseTransactionBoundary = "allowed" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.beginMigrationBoundary = "allowed" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.ledgerMutationBoundary = "allowed" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.evidenceMutationBoundary = "allowed" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.permitConsumerBoundary = "writer" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.productionDatabaseWritesBoundary = "authorized" },
		func(value *runnerLedgerEntryAdmissionProfile) { value.gateStatusBoundary = "closed" },
	}
	for index, mutate := range mutations {
		candidate := generatedRunnerLedgerEntryAdmissionProfile
		mutate(&candidate)
		if candidate.valid() {
			t.Fatalf("profile mutation %d remained valid", index)
		}
	}
}

func TestRunnerLedgerEntryAdmissionHasOnlyReviewedRuntimeConsumersAndNoForbiddenAuthorityImport(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	productionCalls := 0
	allowedCallers := map[string]bool{
		"runner_ledger_entry_admission_profile.go": true,
		"runner_ledger_entry_admission_claim.go":   true,
		"runner_ledger_entry_admission_permit.go":  true,
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if allowedCallers[name] || name == "runner_ledger_entry_admission_profile_generated.go" {
				if path == "database/sql" || path == "net/http" || strings.Contains(path, "pgx") {
					t.Fatalf("Slice A profile imports forbidden authority package %q", path)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "generatedRunnerLedgerEntryAdmissionAction" {
				if !allowedCallers[name] {
					t.Fatalf("entry-admission generated selector has an unreviewed production caller in %s", name)
				}
				productionCalls++
			}
			return true
		})
	}
	if productionCalls != 4 {
		t.Fatalf("generated entry-admission selector has %d production calls; want two profile checks plus claim and permit checks", productionCalls)
	}
}
