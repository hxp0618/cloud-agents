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

func TestRunnerLedgerEntryWriterProfilesBindImmutablePredecessors(t *testing.T) {
	if !generatedRunnerLedgerEntryExecutionAdmissionProfile.valid() {
		t.Fatal("generated runner ledger entry execution-admission profile is invalid")
	}
	if !generatedRunnerLedgerEntrySuccessWriterProfile.valid() {
		t.Fatal("generated runner ledger entry success-writer profile is invalid")
	}
	if runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionRegistryDigest != runnerLedgerEntryAdmissionRegistryDigest ||
		runnerLedgerEntryExecutionAdmissionBoundEntryAdmissionProfileDigest != generatedRunnerLedgerEntryAdmissionProfile.profileDigest {
		t.Fatal("execution-admission profile does not bind immutable entry-admission v1")
	}
	if runnerLedgerEntrySuccessWriterBoundExecutionAdmissionRegistryDigest != runnerLedgerEntryExecutionAdmissionRegistryDigest ||
		runnerLedgerEntrySuccessWriterBoundExecutionAdmissionProfileDigest != generatedRunnerLedgerEntryExecutionAdmissionProfile.profileDigest {
		t.Fatal("success-writer profile does not bind generated execution-admission v1")
	}
}

func TestRunnerLedgerEntryExecutionAdmissionMapsOnlyFourFirstAttemptPairs(t *testing.T) {
	type pair struct {
		disposition runnerLedgerPreflightDisposition
		state       RecoveryState
		action      RecoveryAction
	}
	allowed := [...]pair{
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt},
		{runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginFirstAttempt},
		{runnerLedgerPreflightPartialNextEntry, RecoveryBrandNewInherited, RecoveryBeginFirstAttemptNextEntry},
		{runnerLedgerPreflightPartialNextEntry, RecoveryTerminal, RecoveryBeginFirstAttemptNextEntry},
	}
	if len(allowed) != generatedRunnerLedgerEntryExecutionAdmissionPairCount {
		t.Fatalf("allowed pair count = %d; generated = %d", len(allowed), generatedRunnerLedgerEntryExecutionAdmissionPairCount)
	}
	for _, item := range allowed {
		got, ok := generatedRunnerLedgerEntryExecutionAdmissionAction(item.disposition, item.state, item.action)
		if !ok || got != runnerLedgerEntryExecutionAdmissionPrepare {
			t.Fatalf("first-attempt pair %q/%q/%q = %q,%v", item.disposition, item.state, item.action, got, ok)
		}
	}
	if got, ok := generatedRunnerLedgerEntryExecutionAdmissionAction(runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt); ok || got != "" {
		t.Fatalf("retry pair entered execution-admission matrix: %q,%v", got, ok)
	}
	if got, ok := generatedRunnerLedgerEntrySuccessWriterAction(runnerLedgerEntryExecutionAdmissionPrepare); !ok || got != runnerLedgerEntrySuccessWriterExecute {
		t.Fatalf("success-writer action = %q,%v", got, ok)
	}
	if got, ok := generatedRunnerLedgerEntrySuccessWriterAction(""); ok || got != "" {
		t.Fatalf("unknown execution action entered success writer: %q,%v", got, ok)
	}
}

func TestRunnerLedgerEntryWriterProfilesRejectMutation(t *testing.T) {
	executionMutations := []func(*runnerLedgerEntryExecutionAdmissionProfile){
		func(value *runnerLedgerEntryExecutionAdmissionProfile) { value.profileID = "caller/v1" },
		func(value *runnerLedgerEntryExecutionAdmissionProfile) {
			value.profileDigest = testDigest("foreign-execution").String()
		},
		func(value *runnerLedgerEntryExecutionAdmissionProfile) { value.identityBindings[0] = "caller_selected" },
		func(value *runnerLedgerEntryExecutionAdmissionProfile) { value.errorPrecedence[4] = "ignore_cleanup" },
		func(value *runnerLedgerEntryExecutionAdmissionProfile) {
			value.implementationBoundary[7] = "transaction_opened"
		},
		func(value *runnerLedgerEntryExecutionAdmissionProfile) {
			value.implementationBoundary[15] = "authorized"
		},
		func(value *runnerLedgerEntryExecutionAdmissionProfile) { value.implementationBoundary[18] = "closed" },
	}
	for index, mutate := range executionMutations {
		candidate := generatedRunnerLedgerEntryExecutionAdmissionProfile
		mutate(&candidate)
		if candidate.valid() {
			t.Fatalf("execution-admission profile mutation %d remained valid", index)
		}
	}
	writerMutations := []func(*runnerLedgerEntrySuccessWriterProfile){
		func(value *runnerLedgerEntrySuccessWriterProfile) { value.profileID = "caller/v1" },
		func(value *runnerLedgerEntrySuccessWriterProfile) {
			value.profileDigest = testDigest("foreign-writer").String()
		},
		func(value *runnerLedgerEntrySuccessWriterProfile) { value.identityBindings[1] = "ordinary_fact" },
		func(value *runnerLedgerEntrySuccessWriterProfile) { value.errorPrecedence[4] = "retry_unknown" },
		func(value *runnerLedgerEntrySuccessWriterProfile) { value.implementationBoundary[0] = "enabled" },
		func(value *runnerLedgerEntrySuccessWriterProfile) { value.implementationBoundary[4] = "implemented" },
		func(value *runnerLedgerEntrySuccessWriterProfile) { value.implementationBoundary[17] = "authorized" },
		func(value *runnerLedgerEntrySuccessWriterProfile) { value.implementationBoundary[20] = "closed" },
	}
	for index, mutate := range writerMutations {
		candidate := generatedRunnerLedgerEntrySuccessWriterProfile
		mutate(&candidate)
		if candidate.valid() {
			t.Fatalf("success-writer profile mutation %d remained valid", index)
		}
	}
}

func TestRunnerLedgerEntryWriterKeepsAllHistoricalV1ArtifactsSameBits(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	expected := map[string]string{
		"contracts/platform/v1alpha1/fixtures/golden/runner-ledger-preflight-registry-source-v1.json":       "bd1a9e57fd5f1014a7afead056d6c03f1b0a8501e9767e1eb0308aef9065bd71",
		"contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-source-v1.schema.json":        "2c48a4db4641de750336fb2cfb454da98998a494002342f29201c17dfdbc7204",
		"contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-v1.schema.json":               "829b9e7aefaf16642090051b93babb311790a3be354f91ed91520cca39079c5c",
		"contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json":                    "2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c",
		"services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go":            "599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112",
		"contracts/platform/v1alpha1/fixtures/golden/runner-ledger-consumer-registry-source-v1.json":        "3b81553a58077bb1e748f7f4f6474c59ac9d8dcfb5fdbffd1cab00d7d4361b64",
		"contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-source-v1.schema.json":         "c1a82d48448a38c94d613d05b28a933ac95986ea038e85f35bac4d0590387120",
		"contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-v1.schema.json":                "bb8f9557621825f45150a4f1b3b7708566f3a0e790077307406fd287b6a86ae6",
		"contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json":                     "fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852",
		"services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go":             "afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928",
		"contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-admission-registry-source-v1.json": "56fcaa4806731ff968c4614f710502b39dad66ee64784662bf962f58a37c3b88",
		"contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-source-v1.schema.json":  "5bd4c267e62d87287ae68a0f6f4e8a2d2dbf3e65af0a96d522019342e2abb17b",
		"contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-v1.schema.json":         "bbe0c63c2942b8286fca6daca546c54b4c43efd0ab7193cd0c4e10ef6e27d409",
		"contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json":              "2dc0210f1aad1dd6cff1183324837ab7e88cc5491e9046ae07302b25a1f9e372",
		"services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go":      "c95850d99a5cbc9d82480d2c63befd6a39ffaeeb2f2c2f1374ce21091ff806c6",
		"services/control-plane/internal/migration/runner_ledger_entry_admission_permit.go":                 "255088e37e40d897d76ba589dbf2afd9dbb7dcf3e9d17e6b9d752735f4306714",
	}
	for path, want := range expected {
		bytes, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(bytes)); got != want {
			t.Fatalf("immutable runner ledger v1 artifact %s changed: got %s want %s", path, got, want)
		}
	}
}

func TestRunnerLedgerEntryWriterProfilesHaveNoUnreviewedProductionConsumer(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	executionCalls := 0
	writerCalls := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if name == "runner_ledger_entry_writer_profile.go" || name == "runner_ledger_entry_writer_profile_generated.go" {
			for _, imported := range file.Imports {
				path := strings.Trim(imported.Path.Value, `"`)
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
			if !ok {
				return true
			}
			switch identifier.Name {
			case "generatedRunnerLedgerEntryExecutionAdmissionAction":
				if name != "runner_ledger_entry_writer_profile.go" {
					t.Fatalf("execution-admission selector has unreviewed production caller in %s", name)
				}
				executionCalls++
			case "generatedRunnerLedgerEntrySuccessWriterAction":
				if name != "runner_ledger_entry_writer_profile.go" {
					t.Fatalf("success-writer selector has unreviewed production caller in %s", name)
				}
				writerCalls++
			}
			return true
		})
	}
	if executionCalls != 2 || writerCalls != 2 {
		t.Fatalf("generated selectors have execution=%d writer=%d profile-only calls; want 2/2", executionCalls, writerCalls)
	}
}
