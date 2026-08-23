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

func TestRunnerLedgerRecoveryProfilesCloseExactPairMapping(t *testing.T) {
	if !validGeneratedRunnerLedgerRecoveryProfiles() {
		t.Fatal("generated runner ledger recovery profiles are invalid")
	}
	wantFamilies := [...]string{
		"recovery_admission",
		"abort_terminal_writer",
		"commit_observation_writer",
		"ambiguous_resolution_writer",
		"retry_handoff",
		"recovery_execution_admission",
		"recovery_success_writer",
		"return_failure",
	}
	wantCounts := [...]uint8{12, 4, 1, 1, 1, 3, 0, 2}
	profileByAction := map[runnerLedgerRecoveryAction]string{
		"append_abort_terminal":              "runner-ledger-abort-terminal-writer/v1",
		"append_commit_observation_terminal": "runner-ledger-commit-observation-writer/v1",
		"append_ambiguous_resolution":        "runner-ledger-ambiguous-resolution-writer/v1",
		"prepare_retry_handoff":              "runner-ledger-retry-handoff/v1",
		"prepare_recovery_execution":         "runner-ledger-recovery-execution-admission/v1",
		"return_typed_failure":               "runner-ledger-return-failure/v1",
	}
	for i := range generatedRunnerLedgerRecoveryProfiles {
		profile := generatedRunnerLedgerRecoveryProfiles[i]
		if profile.family != wantFamilies[i] || profile.pairCount != wantCounts[i] {
			t.Fatalf("profile %d = %q/%d; want %q/%d", i, profile.family, profile.pairCount, wantFamilies[i], wantCounts[i])
		}
	}
	common := generatedRunnerLedgerRecoveryProfiles[0]
	seen := make(map[string]struct{}, common.pairCount)
	for i := uint8(0); i < common.pairCount; i++ {
		pair := common.pairs[i]
		key := string(pair.disposition) + "\x00" + string(pair.state) + "\x00" + string(pair.action)
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate closed pair %q", key)
		}
		seen[key] = struct{}{}
		got, ok := generatedRunnerLedgerRecoveryAdmissionAction(pair.disposition, pair.state, pair.action)
		if !ok || got != pair.profileAction {
			t.Fatalf("admission pair %q = %q,%v; want %q,true", key, got, ok, pair.profileAction)
		}
		if !generatedRunnerLedgerRecoveryProfileAllows(common.profileID, pair.disposition, pair.state, pair.action) {
			t.Fatalf("common profile rejected closed pair %q", key)
		}
		profileID := profileByAction[pair.profileAction]
		if profileID == "" || !generatedRunnerLedgerRecoveryProfileAllows(profileID, pair.disposition, pair.state, pair.action) {
			t.Fatalf("action profile %q rejected closed pair %q", profileID, key)
		}
	}
	if len(seen) != 12 {
		t.Fatalf("closed pair cardinality = %d; want 12", len(seen))
	}
	if got, ok := generatedRunnerLedgerRecoveryAdmissionAction(runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNew, RecoveryBeginFirstAttempt); ok || got != "" {
		t.Fatalf("unknown pair selected %q,%v", got, ok)
	}
	if generatedRunnerLedgerRecoveryProfileAllows("runner-ledger-recovery-success-writer/v1", runnerLedgerPreflightEmptyBrandNew, RecoveryBrandNewInherited, RecoveryBeginNextAttempt) ||
		generatedRunnerLedgerRecoveryProfileAllows("runner-ledger-return-failure/v1", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryBeginNextAttempt) ||
		generatedRunnerLedgerRecoveryProfileAllows("caller/v1", runnerLedgerPreflightPartialRetryOrRecovery, RecoveryTerminal, RecoveryReturnFailure) {
		t.Fatal("unknown or cross-profile pair entered a generated recovery profile")
	}
}

func TestRunnerLedgerRecoveryProfilesSeparateExecutionAdmissionAndSuccessWriter(t *testing.T) {
	execution := generatedRunnerLedgerRecoveryProfiles[5]
	writer := generatedRunnerLedgerRecoveryProfiles[6]
	if execution.profileID != "runner-ledger-recovery-execution-admission/v1" ||
		writer.profileID != "runner-ledger-recovery-success-writer/v1" || writer.pairCount != 0 {
		t.Fatal("recovery execution-admission and success-writer identities are not distinct")
	}
	if execution.registryID == writer.registryID || execution.registryDigest == writer.registryDigest ||
		execution.profileDigest == writer.profileDigest || execution.stateMachineDigest == writer.stateMachineDigest ||
		execution.policyDigest == writer.policyDigest {
		t.Fatal("recovery execution-admission and success-writer identities share a digest or registry")
	}
	if writer.predecessor != execution.registryBinding() || writer.permitFromProfileID != execution.profileID {
		t.Fatal("recovery success-writer does not bind the exact execution-admission predecessor")
	}
	if got, ok := generatedRunnerLedgerRecoverySuccessWriterAction(execution.action); !ok || got != writer.action {
		t.Fatalf("success-writer action = %q,%v; want %q,true", got, ok, writer.action)
	}
	if got, ok := generatedRunnerLedgerRecoverySuccessWriterAction(generatedRunnerLedgerRecoveryProfiles[0].action); ok || got != "" {
		t.Fatalf("ordinary recovery-admission action entered success writer: %q,%v", got, ok)
	}
}

func TestRunnerLedgerRecoveryProfilesRejectLiteralAndGraphMutation(t *testing.T) {
	mutations := []func(*[8]runnerLedgerRecoveryProfile){
		func(value *[8]runnerLedgerRecoveryProfile) { value[0].family = "caller" },
		func(value *[8]runnerLedgerRecoveryProfile) {
			value[0].registryDigest = testDigest("foreign-registry").String()
		},
		func(value *[8]runnerLedgerRecoveryProfile) { value[0].predecessor.profileID = "caller/v1" },
		func(value *[8]runnerLedgerRecoveryProfile) {
			value[0].historicalBindings[4] = value[0].historicalBindings[3]
		},
		func(value *[8]runnerLedgerRecoveryProfile) { value[0].pairs[0].profileAction = "return_typed_failure" },
		func(value *[8]runnerLedgerRecoveryProfile) { value[1].predecessor = value[5].registryBinding() },
		func(value *[8]runnerLedgerRecoveryProfile) { value[5].permitFromProfileID = value[6].profileID },
		func(value *[8]runnerLedgerRecoveryProfile) { value[6].pairCount = 1 },
		func(value *[8]runnerLedgerRecoveryProfile) {
			value[6].predecessor.registryDigest = value[5].profileDigest
		},
		func(value *[8]runnerLedgerRecoveryProfile) { value[7].profileDigest = value[4].profileDigest },
		func(value *[8]runnerLedgerRecoveryProfile) { value[7].transitions[0].event = "" },
		func(value *[8]runnerLedgerRecoveryProfile) {
			value[7].transitions[value[7].transitionCount] = runnerLedgerRecoveryTransition{from: "caller", event: "caller", to: "caller"}
		},
	}
	for index, mutate := range mutations {
		candidate := generatedRunnerLedgerRecoveryProfiles
		mutate(&candidate)
		if validRunnerLedgerRecoveryProfiles(candidate) {
			t.Fatalf("profile graph mutation %d remained valid", index)
		}
	}
	if (runnerLedgerRecoveryProfile{}).valid() || validRunnerLedgerRecoveryProfiles([8]runnerLedgerRecoveryProfile{}) {
		t.Fatal("ordinary zero-value literal remained valid")
	}
}

func TestRunnerLedgerRecoveryProfilesKeepHistoricalArtifactsSameBits(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	expected := map[string]string{
		"contracts/platform/v1alpha1/fixtures/golden/runner-ledger-preflight-registry-source-v1.json":                 "bd1a9e57fd5f1014a7afead056d6c03f1b0a8501e9767e1eb0308aef9065bd71",
		"contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-source-v1.schema.json":                  "2c48a4db4641de750336fb2cfb454da98998a494002342f29201c17dfdbc7204",
		"contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-v1.schema.json":                         "829b9e7aefaf16642090051b93babb311790a3be354f91ed91520cca39079c5c",
		"contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json":                              "2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c",
		"services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go":                      "599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112",
		"contracts/platform/v1alpha1/fixtures/golden/runner-ledger-consumer-registry-source-v1.json":                  "3b81553a58077bb1e748f7f4f6474c59ac9d8dcfb5fdbffd1cab00d7d4361b64",
		"contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-source-v1.schema.json":                   "c1a82d48448a38c94d613d05b28a933ac95986ea038e85f35bac4d0590387120",
		"contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-v1.schema.json":                          "bb8f9557621825f45150a4f1b3b7708566f3a0e790077307406fd287b6a86ae6",
		"contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json":                               "fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852",
		"services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go":                       "afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928",
		"contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-admission-registry-source-v1.json":           "56fcaa4806731ff968c4614f710502b39dad66ee64784662bf962f58a37c3b88",
		"contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-source-v1.schema.json":            "5bd4c267e62d87287ae68a0f6f4e8a2d2dbf3e65af0a96d522019342e2abb17b",
		"contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-v1.schema.json":                   "bbe0c63c2942b8286fca6daca546c54b4c43efd0ab7193cd0c4e10ef6e27d409",
		"contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json":                        "2dc0210f1aad1dd6cff1183324837ab7e88cc5491e9046ae07302b25a1f9e372",
		"services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go":                "c95850d99a5cbc9d82480d2c63befd6a39ffaeeb2f2c2f1374ce21091ff806c6",
		"contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-execution-admission-registry-source-v1.json": "88bbb305ced88107407a830b195208c1c02bb1f5bc7a321c2c4a17042b37ecbb",
		"contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-source-v1.schema.json":  "505fdcd72f113d9156c5549ac0ef02c97c4d9e40286495082753cb98d7ae8d9a",
		"contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-v1.schema.json":         "96eb821ce315b23540146a7e9b77cfbac9b68e12da1367d9f6f054ed61b20d97",
		"contracts/generated/platform/v1alpha1/runner-ledger-entry-execution-admission-registry-v1.json":              "9ef15ce291207580d7bc0426d22d7e4e5a43260a89ea5375c5f8e10e08c0dc96",
		"contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-success-writer-registry-source-v1.json":      "ee114d994062d0f3c6ee9f96a1d962621a5f95f19eba3b21a5de6bfeb1700db9",
		"contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-source-v1.schema.json":       "ee23116e5de2d052f8f25fb2addd8cb98bd055901f34f222aa9561437e5d3274",
		"contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-v1.schema.json":              "2e6f4a49f734983b2e3f57074814c57be3ff7f596e8df35cfb527436b0274beb",
		"contracts/generated/platform/v1alpha1/runner-ledger-entry-success-writer-registry-v1.json":                   "0025cb5a4f38644848bf5317f37b8b849fc5861f56872ff6c2bd860bd841a5e6",
		"services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go":                   "63b2e2ac4aec2f02ba9bfc5e90ef716d3659decbbb2ffe716cfe50f189b77c5d",
	}
	for path, want := range expected {
		bytes, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(bytes)); got != want {
			t.Fatalf("immutable runner ledger artifact %s changed: got %s want %s", path, got, want)
		}
	}
}

func TestRunnerLedgerRecoveryProfilesHaveOnlyApprovedConsumersAndNoExternalSideEffectSurface(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	admissionCalls := 0
	writerCalls := 0
	profileAllowsCalls := 0
	generatedSymbols := map[string]bool{
		"generatedRunnerLedgerRecoveryProfiles":            true,
		"generatedRunnerLedgerRecoveryAdmissionAction":     true,
		"generatedRunnerLedgerRecoveryProfileAllows":       true,
		"generatedRunnerLedgerRecoverySuccessWriterAction": true,
	}
	allowedProductionSymbols := map[string]map[string]bool{
		"generatedRunnerLedgerRecoveryProfiles": {
			"runner_ledger_recovery_abort_terminal.go":        true,
			"runner_ledger_recovery_admission_claim.go":       true,
			"runner_ledger_recovery_admission_permit.go":      true,
			"runner_ledger_recovery_commit_reconciliation.go": true,
			"runner_ledger_recovery_retry_handoff.go":         true,
		},
		"generatedRunnerLedgerRecoveryAdmissionAction": {
			"runner_ledger_consumer_service.go":          true,
			"runner_ledger_recovery_admission_claim.go":  true,
			"runner_ledger_recovery_admission_permit.go": true,
		},
		"generatedRunnerLedgerRecoveryProfileAllows": {
			"runner_ledger_recovery_admission_permit.go": true,
		},
		"generatedRunnerLedgerRecoverySuccessWriterAction": {},
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if name == "runner_ledger_recovery_profile.go" || name == "runner_ledger_recovery_profile_generated.go" ||
			name == "runner_ledger_recovery_abort_terminal.go" || name == "runner_ledger_recovery_admission_claim.go" ||
			name == "runner_ledger_recovery_admission_permit.go" || name == "runner_ledger_recovery_commit_reconciliation.go" ||
			name == "runner_ledger_recovery_retry_handoff.go" {
			for _, imported := range file.Imports {
				path := strings.Trim(imported.Path.Value, `"`)
				if path == "database/sql" || path == "net/http" || strings.Contains(path, "pgx") {
					t.Fatalf("recovery profile or admission imports forbidden authority package %q", path)
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.Ident); ok && generatedSymbols[identifier.Name] &&
				name != "runner_ledger_recovery_profile.go" && name != "runner_ledger_recovery_profile_generated.go" &&
				!allowedProductionSymbols[identifier.Name][name] {
				t.Fatalf("generated recovery profile symbol %s has production consumer in %s", identifier.Name, name)
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			switch identifier.Name {
			case "generatedRunnerLedgerRecoveryAdmissionAction":
				if name != "runner_ledger_recovery_profile.go" && !allowedProductionSymbols[identifier.Name][name] {
					t.Fatalf("recovery-admission selector has production caller in %s", name)
				}
				admissionCalls++
			case "generatedRunnerLedgerRecoverySuccessWriterAction":
				if name != "runner_ledger_recovery_profile.go" {
					t.Fatalf("recovery success-writer selector has production caller in %s", name)
				}
				writerCalls++
			case "generatedRunnerLedgerRecoveryProfileAllows":
				if !allowedProductionSymbols[identifier.Name][name] {
					t.Fatalf("recovery action-profile selector has production caller in %s", name)
				}
				profileAllowsCalls++
			}
			return true
		})
	}
	if admissionCalls != 5 || writerCalls != 1 || profileAllowsCalls != 2 {
		t.Fatalf("generated recovery selectors have admission=%d writer=%d profile=%d production calls; want 5/1/2", admissionCalls, writerCalls, profileAllowsCalls)
	}
}

func TestRunnerLedgerRecoveryAdmissionProductionGraphHasOnlyApprovedWriterEdges(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	admissionCalls := 0
	abortWriterCalls := 0
	commitObservationWriterCalls := 0
	ambiguousResolutionWriterCalls := 0
	retryHandoffCalls := 0
	successorCalls := 0
	forbidden := map[string]bool{
		"Append": true, "AppendDurable": true, "AppendGenerationSuperseded": true,
		"AppendGenerationReserved": true, "AppendGenerationActivated": true,
		"BeginMigration": true, "ExecuteStatement": true, "Commit": true,
		"ReserveAndActivateSuccessor": true, "executeRunnerLedgerEntrySuccess": true,
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "admitRunnerLedgerRecoveryAction" {
				admissionCalls++
				if name != "runner_ledger_consumer_service.go" {
					t.Errorf("recovery admission has production caller in %s", name)
				}
			}
			if ok && selector.Sel.Name == "appendRunnerLedgerRecoveryAbortTerminal" {
				abortWriterCalls++
				if name != "runner_ledger_recovery_admission_permit.go" {
					t.Errorf("abort-terminal writer has production caller in %s", name)
				}
			}
			if ok && selector.Sel.Name == "appendRunnerLedgerRecoveryCommitObservation" {
				commitObservationWriterCalls++
				if name != "runner_ledger_recovery_admission_permit.go" {
					t.Errorf("commit-observation writer has production caller in %s", name)
				}
			}
			if ok && selector.Sel.Name == "appendRunnerLedgerRecoveryAmbiguousResolution" {
				ambiguousResolutionWriterCalls++
				if name != "runner_ledger_recovery_admission_permit.go" {
					t.Errorf("ambiguous-resolution writer has production caller in %s", name)
				}
			}
			if ok && selector.Sel.Name == "prepareRunnerLedgerRetryHandoff" {
				retryHandoffCalls++
				if name != "runner_ledger_recovery_admission_permit.go" {
					t.Errorf("retry-handoff service has production caller in %s", name)
				}
			}
			if ok && selector.Sel.Name == "ReserveAndActivateSuccessor" {
				successorCalls++
				if name != "runner_ledger_recovery_retry_handoff.go" {
					t.Errorf("successor-generation transition has recovery caller in %s", name)
				}
			}
			return true
		})
		if name != "runner_ledger_recovery_admission_claim.go" && name != "runner_ledger_recovery_admission_permit.go" &&
			name != "runner_ledger_recovery_retry_handoff.go" && name != "evidence_session.go" {
			continue
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			if name == "evidence_session.go" && function.Name.Name != "refreshRunnerLedgerRecoveryEvidence" &&
				function.Name.Name != "detachForRunnerLedgerRecoveryLocked" && function.Name.Name != "installRunnerLedgerRecoveryLocked" &&
				function.Name.Name != "bindRunnerLedgerRecoveryAdmissionClaim" && function.Name.Name != "consumeRunnerLedgerRecoveryAdmissionClaim" {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				called := ""
				switch value := call.Fun.(type) {
				case *ast.Ident:
					called = value.Name
				case *ast.SelectorExpr:
					called = value.Sel.Name
				}
				if forbidden[called] && !(name == "runner_ledger_recovery_retry_handoff.go" &&
					function.Name.Name == "bindRunnerLedgerRetryHandoff" && called == "ReserveAndActivateSuccessor") {
					t.Errorf("%s.%s acquired forbidden writer edge %s", name, function.Name.Name, called)
				}
				return true
			})
		}
	}
	if admissionCalls != 2 {
		t.Fatalf("recovery close-only production calls=%d want=2", admissionCalls)
	}
	if abortWriterCalls != 1 {
		t.Fatalf("abort-terminal writer production calls=%d want=1", abortWriterCalls)
	}
	if commitObservationWriterCalls != 1 || ambiguousResolutionWriterCalls != 1 {
		t.Fatalf("reconciliation writer production calls=%d/%d want=1/1", commitObservationWriterCalls, ambiguousResolutionWriterCalls)
	}
	if retryHandoffCalls != 1 || successorCalls != 1 {
		t.Fatalf("retry-handoff production calls=%d successor transitions=%d want=1/1", retryHandoffCalls, successorCalls)
	}
}
