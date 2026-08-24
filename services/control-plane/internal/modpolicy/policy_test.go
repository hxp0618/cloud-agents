package modpolicy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

const (
	platformGoVersion = "1.26.0"
	platformToolchain = "go1.26.6"
)

type request struct {
	GoVersion string
	Toolchain string
	Workspace workspaceSpec
	Modules   []moduleSpec
}

type workspaceSpec struct {
	File         string
	Source       string
	ExpectedUses []string
}

type moduleSpec struct {
	File            string
	Source          string
	ExpectedModule  string
	MinimumRequires []requirementSpec
}

type requirementSpec struct {
	Path    string
	Version string
	Direct  bool
}

func TestRepositoryModulePolicy(t *testing.T) {
	t.Parallel()
	input, err := repositoryRequest()
	if err != nil {
		t.Fatalf("repositoryRequest() error = %v", err)
	}
	if err := validateRequest(input); err != nil {
		t.Fatalf("validateRequest() error = %v", err)
	}
}

func TestValidateRequest(t *testing.T) {
	t.Parallel()
	if err := validateRequest(validRequest()); err != nil {
		t.Fatalf("validateRequest() error = %v", err)
	}

	tests := []struct {
		name string
		edit func(*request)
		want string
	}{
		{
			name: "workspace replacement",
			edit: func(input *request) {
				input.Workspace.Source += "\nreplace example.invalid/old => ./local\n"
			},
			want: "replace or godebug directives",
		},
		{
			name: "workspace use mismatch",
			edit: func(input *request) {
				input.Workspace.ExpectedUses = []string{"./sdk/go"}
			},
			want: "use set mismatch",
		},
		{
			name: "module replacement",
			edit: func(input *request) {
				input.Modules[0].Source += "\nreplace example.invalid/old => ./local\n"
			},
			want: "replace, exclude, retract, godebug, tool, or ignore",
		},
		{
			name: "module exclusion",
			edit: func(input *request) {
				input.Modules[0].Source += "\nexclude example.invalid/dependency v1.0.0\n"
			},
			want: "replace, exclude, retract, godebug, tool, or ignore",
		},
		{
			name: "module retraction",
			edit: func(input *request) {
				input.Modules[0].Source += "\nretract v1.0.0\n"
			},
			want: "replace, exclude, retract, godebug, tool, or ignore",
		},
		{
			name: "module tool directive",
			edit: func(input *request) {
				input.Modules[0].Source += "\ntool example.invalid/tool\n"
			},
			want: "replace, exclude, retract, godebug, tool, or ignore",
		},
		{
			name: "security floor downgrade",
			edit: func(input *request) {
				input.Modules[0].Source = strings.ReplaceAll(input.Modules[0].Source, "v0.40.0", "v0.39.0")
			},
			want: "v0.40.0 or newer",
		},
		{
			name: "security floor made indirect",
			edit: func(input *request) {
				input.Modules[0].Source = strings.ReplaceAll(input.Modules[0].Source, "v0.40.0", "v0.40.0 // indirect")
			},
			want: "directly",
		},
		{
			name: "wrong module",
			edit: func(input *request) {
				input.Modules[0].ExpectedModule = "example.invalid/control-plane"
			},
			want: "module mismatch",
		},
		{
			name: "wrong toolchain",
			edit: func(input *request) {
				input.Toolchain = "go1.26.5"
			},
			want: "must pin",
		},
		{
			name: "malformed module",
			edit: func(input *request) {
				input.Modules[0].Source = "module\n"
			},
			want: "parse",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate := validRequest()
			test.edit(&candidate)
			err := validateRequest(candidate)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateRequest() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func repositoryRequest() (request, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return request{}, errors.New("locate policy source")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../.."))
	read := func(file string) (string, error) {
		contents, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(file)))
		if err != nil {
			return "", fmt.Errorf("read %s: %w", file, err)
		}
		return string(contents), nil
	}

	workspaceSource, err := read("go.work")
	if err != nil {
		return request{}, err
	}
	input := request{
		GoVersion: platformGoVersion,
		Toolchain: platformToolchain,
		Workspace: workspaceSpec{
			File:         "go.work",
			Source:       workspaceSource,
			ExpectedUses: []string{"./sdk/go", "./services/control-plane", "./services/worker"},
		},
		Modules: []moduleSpec{
			{File: "sdk/go/go.mod", ExpectedModule: "github.com/hxp0618/cloud-agents/sdk/go"},
			{
				File:           "services/control-plane/go.mod",
				ExpectedModule: "github.com/hxp0618/cloud-agents/services/control-plane",
				MinimumRequires: []requirementSpec{
					{Path: "golang.org/x/mod", Version: "v0.40.0", Direct: true},
				},
			},
			{File: "services/worker/go.mod", ExpectedModule: "github.com/hxp0618/cloud-agents/services/worker"},
		},
	}
	for index := range input.Modules {
		source, err := read(input.Modules[index].File)
		if err != nil {
			return request{}, err
		}
		input.Modules[index].Source = source
	}
	return input, nil
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
	expectedFiles := make(map[string]struct{}, len(input.Workspace.ExpectedUses))
	for _, use := range input.Workspace.ExpectedUses {
		if !strings.HasPrefix(use, "./") {
			return fmt.Errorf("workspace use must be repository-relative: %q", use)
		}
		expectedFiles[strings.TrimPrefix(use, "./")+"/go.mod"] = struct{}{}
	}
	seenModules := make(map[string]struct{}, len(input.Modules))
	seenFiles := make(map[string]struct{}, len(input.Modules))
	for _, module := range input.Modules {
		if _, ok := expectedFiles[module.File]; !ok {
			return fmt.Errorf("module file %q is not in the workspace use set", module.File)
		}
		if _, ok := seenFiles[module.File]; ok {
			return fmt.Errorf("duplicate module file %q", module.File)
		}
		seenFiles[module.File] = struct{}{}
		if _, ok := seenModules[module.ExpectedModule]; ok {
			return fmt.Errorf("duplicate expected module %q", module.ExpectedModule)
		}
		seenModules[module.ExpectedModule] = struct{}{}
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
	return validateMinimumRequires(spec.File, parsed.Require, spec.MinimumRequires)
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

func validRequest() request {
	return request{
		GoVersion: platformGoVersion,
		Toolchain: platformToolchain,
		Workspace: workspaceSpec{
			File:         "go.work",
			Source:       "go 1.26.0\n\ntoolchain go1.26.6\n\nuse ./services/control-plane\n",
			ExpectedUses: []string{"./services/control-plane"},
		},
		Modules: []moduleSpec{
			{
				File:            "services/control-plane/go.mod",
				Source:          "module github.com/hxp0618/cloud-agents/services/control-plane\n\ngo 1.26.0\n\ntoolchain go1.26.6\n\nrequire golang.org/x/mod v0.40.0\n",
				ExpectedModule:  "github.com/hxp0618/cloud-agents/services/control-plane",
				MinimumRequires: []requirementSpec{{Path: "golang.org/x/mod", Version: "v0.40.0", Direct: true}},
			},
		},
	}
}
