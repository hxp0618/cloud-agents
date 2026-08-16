// Command modpolicy strictly parses the platform Go workspace and module files.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

type request struct {
	GoVersion string        `json:"goVersion"`
	Toolchain string        `json:"toolchain"`
	Workspace workspaceSpec `json:"workspace"`
	Modules   []moduleSpec  `json:"modules"`
}

type workspaceSpec struct {
	File         string   `json:"file"`
	Source       string   `json:"source"`
	ExpectedUses []string `json:"expectedUses"`
}

type moduleSpec struct {
	File            string            `json:"file"`
	Source          string            `json:"source"`
	ExpectedModule  string            `json:"expectedModule"`
	MinimumRequires []requirementSpec `json:"minimumRequires"`
}

type requirementSpec struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Direct  bool   `json:"direct"`
}

func main() {
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
	decoder.DisallowUnknownFields()
	var input request
	if err := decoder.Decode(&input); err != nil {
		fail("decode request: %v", err)
	}
	if err := ensureEOF(decoder); err != nil {
		fail("decode request: %v", err)
	}
	if err := validateRequest(input); err != nil {
		fail("%v", err)
	}
	fmt.Printf("module-policy: %d modules PASS\n", len(input.Modules))
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values")
}

func validateRequest(input request) error {
	if input.GoVersion == "" || input.Toolchain == "" {
		return errors.New("goVersion and toolchain are required")
	}
	if err := validateWorkspace(input.Workspace, input.GoVersion, input.Toolchain); err != nil {
		return err
	}
	if len(input.Modules) == 0 {
		return errors.New("at least one module is required")
	}
	if len(input.Modules) != len(input.Workspace.ExpectedUses) {
		return fmt.Errorf("module count %d does not match workspace use count %d", len(input.Modules), len(input.Workspace.ExpectedUses))
	}
	seen := make(map[string]struct{}, len(input.Modules))
	for _, module := range input.Modules {
		if _, ok := seen[module.ExpectedModule]; ok {
			return fmt.Errorf("duplicate expected module %q", module.ExpectedModule)
		}
		seen[module.ExpectedModule] = struct{}{}
		if err := validateModule(module, input.GoVersion, input.Toolchain); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkspace(spec workspaceSpec, goVersion, toolchain string) error {
	if spec.File == "" || spec.Source == "" {
		return errors.New("workspace file and source are required")
	}
	parsed, err := modfile.ParseWork(spec.File, []byte(spec.Source), nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", spec.File, err)
	}
	if parsed.Go == nil || parsed.Go.Version != goVersion || parsed.Toolchain == nil || parsed.Toolchain.Name != toolchain {
		return fmt.Errorf("%s must pin go %s and toolchain %s", spec.File, goVersion, toolchain)
	}
	if len(parsed.Replace) != 0 || len(parsed.Godebug) != 0 {
		return fmt.Errorf("%s must not contain replace or godebug directives", spec.File)
	}
	actualUses := make([]string, 0, len(parsed.Use))
	for _, use := range parsed.Use {
		actualUses = append(actualUses, use.Path)
	}
	expectedUses := slices.Clone(spec.ExpectedUses)
	slices.Sort(actualUses)
	slices.Sort(expectedUses)
	if !slices.Equal(actualUses, expectedUses) {
		return fmt.Errorf("%s use set mismatch: got %v, want %v", spec.File, actualUses, expectedUses)
	}
	return nil
}

func validateModule(spec moduleSpec, goVersion, toolchain string) error {
	if spec.File == "" || spec.Source == "" || spec.ExpectedModule == "" {
		return errors.New("module file, source, and expectedModule are required")
	}
	parsed, err := modfile.Parse(spec.File, []byte(spec.Source), nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", spec.File, err)
	}
	if parsed.Module == nil || parsed.Module.Mod.Path != spec.ExpectedModule {
		return fmt.Errorf("%s module mismatch: got %q, want %q", spec.File, modulePath(parsed), spec.ExpectedModule)
	}
	if parsed.Go == nil || parsed.Go.Version != goVersion || parsed.Toolchain == nil || parsed.Toolchain.Name != toolchain {
		return fmt.Errorf("%s must pin go %s and toolchain %s", spec.File, goVersion, toolchain)
	}
	if len(parsed.Replace) != 0 || len(parsed.Exclude) != 0 || len(parsed.Retract) != 0 || len(parsed.Godebug) != 0 || len(parsed.Tool) != 0 || len(parsed.Ignore) != 0 {
		return fmt.Errorf("%s must not contain replace, exclude, retract, godebug, tool, or ignore directives", spec.File)
	}
	if err := validateMinimumRequires(spec.File, parsed.Require, spec.MinimumRequires); err != nil {
		return err
	}
	return nil
}

func validateMinimumRequires(file string, actual []*modfile.Require, minimums []requirementSpec) error {
	seen := make(map[string]struct{}, len(minimums))
	for _, minimum := range minimums {
		if minimum.Path == "" || !semver.IsValid(minimum.Version) {
			return fmt.Errorf("%s has invalid minimum requirement %q@%q", file, minimum.Path, minimum.Version)
		}
		if _, ok := seen[minimum.Path]; ok {
			return fmt.Errorf("%s has duplicate minimum requirement %q", file, minimum.Path)
		}
		seen[minimum.Path] = struct{}{}
		var matched *modfile.Require
		for _, requirement := range actual {
			if requirement.Mod.Path == minimum.Path {
				matched = requirement
				break
			}
		}
		if matched == nil || semver.Compare(matched.Mod.Version, minimum.Version) < 0 {
			return fmt.Errorf("%s must require %s at %s or newer", file, minimum.Path, minimum.Version)
		}
		if minimum.Direct && matched.Indirect {
			return fmt.Errorf("%s must require %s directly", file, minimum.Path)
		}
	}
	return nil
}

func modulePath(parsed *modfile.File) string {
	if parsed.Module == nil {
		return ""
	}
	return parsed.Module.Mod.Path
}

func fail(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
