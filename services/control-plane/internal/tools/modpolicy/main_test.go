package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const validWorkspace = `go 1.26.0

toolchain go1.26.6

use (
	./services/control-plane
)
`

const validModule = `module github.com/hxp0618/cloud-agents/services/control-plane

go 1.26.0

toolchain go1.26.6

require golang.org/x/mod v0.40.0
`

func TestValidateRequest(t *testing.T) {
	t.Parallel()
	input := validRequest()
	if err := validateRequest(input); err != nil {
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

func TestEnsureEOF(t *testing.T) {
	t.Parallel()
	decoder := jsonDecoder("{} {}")
	var first map[string]any
	if err := decoder.Decode(&first); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if err := ensureEOF(decoder); err == nil || !strings.Contains(err.Error(), "multiple JSON") {
		t.Fatalf("ensureEOF() error = %v, want multiple JSON values", err)
	}
}

func validRequest() request {
	return request{
		GoVersion: "1.26.0",
		Toolchain: "go1.26.6",
		Workspace: workspaceSpec{
			File:         "go.work",
			Source:       validWorkspace,
			ExpectedUses: []string{"./services/control-plane"},
		},
		Modules: []moduleSpec{
			{
				File:            "services/control-plane/go.mod",
				Source:          validModule,
				ExpectedModule:  "github.com/hxp0618/cloud-agents/services/control-plane",
				MinimumRequires: []requirementSpec{{Path: "golang.org/x/mod", Version: "v0.40.0", Direct: true}},
			},
		},
	}
}

func jsonDecoder(source string) *json.Decoder {
	return json.NewDecoder(strings.NewReader(source))
}
