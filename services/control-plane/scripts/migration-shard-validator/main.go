package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxJSONEventBytes = 8 << 20
	maxJSONEvents     = 10_000_000
)

var topLevelTestName = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)

type testEvent struct {
	Time        string
	Action      string
	Package     string
	Test        string
	Elapsed     *float64
	Output      *string
	FailedBuild string
	Key         string
	Value       string
	Path        string
}

type testState struct {
	runs      int
	terminals int
	passes    int
	skips     int
}

type validationResult struct {
	Package           string
	PlannedCount      int
	RunCount          int
	PassCount         int
	SkipCount         int
	FailCount         int
	PackageStartCount int
	PackagePassCount  int
	JSONEventCount    int
}

func main() {
	testsPath := flag.String("tests", "", "path to the planned top-level test list")
	jsonPath := flag.String("json", "", "path to go test -json output")
	packagePath := flag.String("package", "", "exact expected Go package import path")
	outputPath := flag.String("output", "", "new validation TSV path")
	flag.Parse()
	if flag.NArg() != 0 || *testsPath == "" || *jsonPath == "" || *packagePath == "" || *outputPath == "" {
		fmt.Fprintln(os.Stderr, "usage: migration-shard-validator --tests PATH --json PATH --package IMPORT_PATH --output PATH")
		os.Exit(2)
	}
	if err := run(*testsPath, *jsonPath, *packagePath, *outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "migration shard JSON validation failed: %v\n", err)
		os.Exit(1)
	}
}

func run(testsPath, jsonPath, packagePath, outputPath string) error {
	planned, err := readPlannedTests(testsPath)
	if err != nil {
		return err
	}
	result, err := validateJSON(jsonPath, packagePath, planned)
	if err != nil {
		return err
	}
	return writeValidation(outputPath, result)
}

func readPlannedTests(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open planned tests: %w", err)
	}
	defer file.Close()

	var tests []string
	scanner := bufio.NewScanner(file)
	previous := ""
	for scanner.Scan() {
		name := scanner.Text()
		if !topLevelTestName.MatchString(name) {
			return nil, fmt.Errorf("planned test %q is outside the closed grammar", name)
		}
		if previous != "" && name <= previous {
			return nil, fmt.Errorf("planned tests are not strictly sorted and unique at %q", name)
		}
		tests = append(tests, name)
		previous = name
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read planned tests: %w", err)
	}
	if len(tests) == 0 {
		return nil, errors.New("planned test list is empty")
	}
	return tests, nil
}

func validateJSON(path, packagePath string, planned []string) (validationResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return validationResult{}, fmt.Errorf("open JSON log: %w", err)
	}
	defer file.Close()

	states := make(map[string]*testState, len(planned))
	for _, name := range planned {
		states[name] = &testState{}
	}
	result := validationResult{Package: packagePath, PlannedCount: len(planned)}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxJSONEventBytes)
	packageEnded := false
	for scanner.Scan() {
		result.JSONEventCount++
		if result.JSONEventCount > maxJSONEvents {
			return validationResult{}, fmt.Errorf("JSON event count exceeds %d", maxJSONEvents)
		}
		event, err := decodeEvent(scanner.Bytes())
		if err != nil {
			return validationResult{}, fmt.Errorf("event %d: %w", result.JSONEventCount, err)
		}
		if packageEnded {
			return validationResult{}, fmt.Errorf("event %d appears after the package terminal", result.JSONEventCount)
		}
		if event.Package != packagePath {
			return validationResult{}, fmt.Errorf("event %d package %q does not match %q", result.JSONEventCount, event.Package, packagePath)
		}
		if event.FailedBuild != "" {
			return validationResult{}, fmt.Errorf("event %d reports failed build %q", result.JSONEventCount, event.FailedBuild)
		}
		switch event.Action {
		case "start", "run", "pause", "cont", "pass", "fail", "output", "skip":
		default:
			return validationResult{}, fmt.Errorf("event %d has unknown action %q", result.JSONEventCount, event.Action)
		}
		if event.Test == "" {
			switch event.Action {
			case "start":
				result.PackageStartCount++
				if result.JSONEventCount != 1 || result.PackageStartCount != 1 {
					return validationResult{}, errors.New("package start is not the unique first event")
				}
			case "pass":
				result.PackagePassCount++
				if result.PackagePassCount != 1 {
					return validationResult{}, errors.New("package pass is not unique")
				}
				if err := validateCompleteStates(states); err != nil {
					return validationResult{}, fmt.Errorf("package passed before the planned set closed: %w", err)
				}
				packageEnded = true
			case "fail", "skip":
				result.FailCount++
				return validationResult{}, fmt.Errorf("package terminal action is %q", event.Action)
			case "output":
				if result.PackageStartCount != 1 {
					return validationResult{}, errors.New("package output appeared before package start")
				}
			default:
				return validationResult{}, fmt.Errorf("package event has invalid action %q", event.Action)
			}
			continue
		}
		if result.PackageStartCount != 1 {
			return validationResult{}, errors.New("test event appeared before package start")
		}
		root := event.Test
		if slash := strings.IndexByte(root, '/'); slash >= 0 {
			root = root[:slash]
		}
		state, ok := states[root]
		if !ok {
			return validationResult{}, fmt.Errorf("unexpected test %q", event.Test)
		}
		if event.Test != root {
			if state.runs != 1 || state.terminals != 0 {
				return validationResult{}, fmt.Errorf("subtest %q appeared outside its active planned top-level run", event.Test)
			}
			if event.Action == "fail" {
				result.FailCount++
				return validationResult{}, fmt.Errorf("subtest %q failed", event.Test)
			}
			continue
		}
		switch event.Action {
		case "run":
			state.runs++
			result.RunCount++
			if state.runs != 1 || state.terminals != 0 {
				return validationResult{}, fmt.Errorf("top-level test %q did not run exactly once", root)
			}
		case "pass", "skip", "fail":
			if state.runs != 1 || state.terminals != 0 {
				return validationResult{}, fmt.Errorf("top-level test %q has an invalid terminal ordering", root)
			}
			state.terminals++
			if event.Action == "pass" {
				state.passes++
				result.PassCount++
			} else if event.Action == "skip" {
				state.skips++
				result.SkipCount++
			} else {
				result.FailCount++
				return validationResult{}, fmt.Errorf("top-level test %q failed", root)
			}
		case "pause", "cont", "output":
			if state.runs != 1 || state.terminals != 0 {
				return validationResult{}, fmt.Errorf("top-level test %q action %q appeared outside its active run", root, event.Action)
			}
		default:
			return validationResult{}, fmt.Errorf("top-level test %q has invalid action %q", root, event.Action)
		}
	}
	if err := scanner.Err(); err != nil {
		return validationResult{}, fmt.Errorf("read JSON log: %w", err)
	}
	if result.JSONEventCount == 0 {
		return validationResult{}, errors.New("JSON log is empty")
	}
	if !packageEnded || result.PackageStartCount != 1 || result.PackagePassCount != 1 {
		return validationResult{}, errors.New("package start/pass boundary is incomplete")
	}
	if err := validateCompleteStates(states); err != nil {
		return validationResult{}, err
	}
	return result, nil
}

func decodeEvent(line []byte) (testEvent, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	token, err := decoder.Token()
	if err != nil {
		return testEvent{}, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return testEvent{}, errors.New("event is not a JSON object")
	}
	allowed := map[string]bool{
		"Time": true, "Action": true, "Package": true, "Test": true,
		"Elapsed": true, "Output": true, "FailedBuild": true,
		"Key": true, "Value": true, "Path": true,
	}
	seen := make(map[string]bool, len(allowed))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return testEvent{}, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return testEvent{}, errors.New("event key is not a string")
		}
		if !allowed[key] {
			return testEvent{}, fmt.Errorf("unknown event field %q", key)
		}
		if seen[key] {
			return testEvent{}, fmt.Errorf("duplicate event field %q", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return testEvent{}, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return testEvent{}, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return testEvent{}, err
		}
		return testEvent{}, fmt.Errorf("unexpected JSON token %v after event", token)
	}

	var event testEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return testEvent{}, err
	}
	if event.Time == "" {
		return testEvent{}, errors.New("event time is empty")
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Time); err != nil {
		return testEvent{}, fmt.Errorf("event time is invalid: %w", err)
	}
	if event.Action == "" {
		return testEvent{}, errors.New("event action is empty")
	}
	return event, nil
}

func validateCompleteStates(states map[string]*testState) error {
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		state := states[name]
		if state.runs != 1 || state.terminals != 1 || state.passes+state.skips != 1 {
			return fmt.Errorf("planned test %q has runs=%d terminals=%d passes=%d skips=%d", name, state.runs, state.terminals, state.passes, state.skips)
		}
	}
	return nil
}

func writeValidation(path string, result validationResult) error {
	if filepath.IsAbs(path) == false {
		return errors.New("validation output path must be absolute")
	}
	if _, err := os.Stat(path); err == nil {
		return errors.New("validation output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat validation output: %w", err)
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".migration-shard-validation.*")
	if err != nil {
		return fmt.Errorf("create validation output: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := fmt.Fprintf(temporary,
		"key\tvalue\nstatus\tPASS\npackage\t%s\nplanned_count\t%d\nrun_count\t%d\npass_count\t%d\nskip_count\t%d\nfail_count\t%d\npackage_start_count\t%d\npackage_pass_count\t%d\njson_event_count\t%d\n",
		result.Package, result.PlannedCount, result.RunCount, result.PassCount,
		result.SkipCount, result.FailCount, result.PackageStartCount,
		result.PackagePassCount, result.JSONEventCount); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write validation output: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync validation output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close validation output: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish validation output: %w", err)
	}
	removeTemporary = false
	return nil
}
