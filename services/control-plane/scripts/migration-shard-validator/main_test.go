package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixturePackage = "example.invalid/internal/migration"

func TestValidateJSONClosesPlannedRunSet(t *testing.T) {
	directory := t.TempDir()
	testsPath := filepath.Join(directory, "tests.txt")
	jsonPath := filepath.Join(directory, "go-test.jsonl")
	outputPath := filepath.Join(directory, "validation.tsv")
	writeFixture(t, testsPath, "TestAlpha\nTestBeta\n")
	writeEvents(t, jsonPath,
		event("start", ""),
		event("run", "TestAlpha"),
		event("run", "TestAlpha/nested"),
		event("pass", "TestAlpha/nested"),
		event("pass", "TestAlpha"),
		event("run", "TestBeta"),
		event("skip", "TestBeta"),
		event("output", ""),
		event("pass", ""),
	)

	if err := run(testsPath, jsonPath, fixturePackage, outputPath); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"status\tPASS\n", "planned_count\t2\n", "run_count\t2\n",
		"pass_count\t1\n", "skip_count\t1\n", "fail_count\t0\n",
		"package_start_count\t1\n", "package_pass_count\t1\n",
	} {
		if !strings.Contains(string(contents), expected) {
			t.Fatalf("validation output missing %q:\n%s", expected, contents)
		}
	}
}

func TestValidateJSONRejectsIncompleteOrUnexpectedResults(t *testing.T) {
	tests := []struct {
		name   string
		events []testEvent
	}{
		{
			name: "missing planned test",
			events: []testEvent{
				event("start", ""), event("run", "TestAlpha"),
				event("pass", "TestAlpha"), event("pass", ""),
			},
		},
		{
			name: "duplicate run",
			events: []testEvent{
				event("start", ""), event("run", "TestAlpha"),
				event("run", "TestAlpha"), event("pass", "TestAlpha"),
			},
		},
		{
			name: "unexpected test",
			events: []testEvent{
				event("start", ""), event("run", "TestGamma"),
			},
		},
		{
			name: "top level failure",
			events: []testEvent{
				event("start", ""), event("run", "TestAlpha"),
				event("fail", "TestAlpha"), event("fail", ""),
			},
		},
		{
			name: "unknown action",
			events: []testEvent{
				event("start", ""), event("run", "TestAlpha"),
				event("invented", "TestAlpha"),
			},
		},
		{
			name: "package pass not terminal",
			events: []testEvent{
				event("start", ""), event("run", "TestAlpha"),
				event("pass", "TestAlpha"), event("run", "TestBeta"),
				event("pass", "TestBeta"), event("pass", ""), event("output", ""),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			jsonPath := filepath.Join(directory, "go-test.jsonl")
			writeEvents(t, jsonPath, test.events...)
			if _, err := validateJSON(jsonPath, fixturePackage, []string{"TestAlpha", "TestBeta"}); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestDecodeEventRejectsDuplicateUnknownAndTrailingJSON(t *testing.T) {
	for _, line := range []string{
		`{"Time":"2026-08-22T00:00:00Z","Action":"start","Action":"pass","Package":"example.invalid/internal/migration"}`,
		`{"Time":"2026-08-22T00:00:00Z","Action":"start","Package":"example.invalid/internal/migration","Unknown":true}`,
		`{"Time":"2026-08-22T00:00:00Z","Action":"start","Package":"example.invalid/internal/migration"} {}`,
	} {
		if _, err := decodeEvent([]byte(line)); err == nil {
			t.Fatalf("expected strict JSON rejection for %s", line)
		}
	}
}

func TestReadPlannedTestsRequiresStrictCanonicalOrder(t *testing.T) {
	for _, contents := range []string{
		"", "TestBeta\nTestAlpha\n", "TestAlpha\nTestAlpha\n", "TestAlpha/child\n", "Test-Alpha\n",
	} {
		path := filepath.Join(t.TempDir(), "tests.txt")
		writeFixture(t, path, contents)
		if _, err := readPlannedTests(path); err == nil {
			t.Fatalf("expected planned-list rejection for %q", contents)
		}
	}
}

func event(action, test string) testEvent {
	return testEvent{
		Time:    "2026-08-22T00:00:00Z",
		Action:  action,
		Package: fixturePackage,
		Test:    test,
	}
}

func writeEvents(t *testing.T, path string, events ...testEvent) {
	t.Helper()
	var contents strings.Builder
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		contents.Write(encoded)
		contents.WriteByte('\n')
	}
	writeFixture(t, path, contents.String())
}

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
