package coordination

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestGeneratedManagedAgentCreateProjectProfileIsExactAndOpaque(t *testing.T) {
	profile := ManagedAgentCreateProject()
	if !profile.Valid() || profile.ProfileID() != "managedAgentCreateProject/v1alpha1" ||
		profile.ProfileDigest() != "sha256:cdd54c59c0c363687eeb0e0e48aff00221cc4d42780bdf935c4b47bcd297f308" ||
		profile.OperationID() != "managedAgentCreateProject" || profile.OutboxEventClass() != "resource_change" ||
		profile.RequiredPermission() != "projects.create" || profile.RequiredScopeLevel() != "organization" ||
		profile.ResultResourceKind() != "project" || profile.ReplayTTLSeconds() != 86400 ||
		profile.ScopeIdentitySchemaID() != "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/managed-agent-create-project-organization-ref.schema.json" ||
		profile.ScopeIdentifierProfile() != "cloud-agents-authorization-scope-identifier/ascii-v1" ||
		profile.ScopeIdentityComparison() != "exact_string_no_rewrite" ||
		profile.CreatesPlatformOperation() || profile.ExternalSideEffectAllowed() {
		t.Fatalf("generated profile drift = %#v", profile)
	}
	if (Profile{}).Valid() {
		t.Fatal("zero profile became valid")
	}
	if _, err := profileForOperation("unknownOperation"); err != ErrUnknownOperation {
		t.Fatalf("unknown operation error = %v", err)
	}
	if RegistryDigest != "sha256:ca5703cbbc68f7501e6fb4da0a0f09bc9fdd6e52bc48f080627bec64fd1b635a" ||
		StateMachineDigest != "sha256:5c4fa5c0cfac253b45a41c2e49ee7e863b9efbe124e5d743e041f5e01f5c6f15" ||
		PolicyDigest != "sha256:95023973eb007a958a3c5aea3ac61b6caa7cd8955b9a24fcef3ad269230c64e8" {
		t.Fatal("generated registry identities drifted")
	}
}

func TestOnlyGeneratedSourceConstructsOperationProfiles(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			identifier, ok := literal.Type.(*ast.Ident)
			if !ok {
				return true
			}
			switch identifier.Name {
			case "operationProfile":
				if name != "registry_generated.go" && name != "registry_v2_generated.go" && !(name == "registry.go" && len(literal.Elts) == 0) {
					t.Errorf("hand-written operationProfile construction in %s", name)
				}
			case "Profile":
				if name != "registry.go" {
					t.Errorf("unexpected Profile construction in %s", name)
				}
			}
			return true
		})
	}
}
