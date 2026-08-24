package mountauthority

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMountAuthorityProductionConsumerFirewall(t *testing.T) {
	if authorityDirectory != "/run/cloud-agents/evidencefs-mounts" {
		t.Fatalf("authority directory=%q", authorityDirectory)
	}
	wants := map[string]map[string]bool{
		"Load":      {filepath.FromSlash("internal/evidencefs/open_linux.go"): true},
		"ObserveFD": {filepath.FromSlash("internal/evidencefs/backend_linux.go"): true},
		"Provision": {filepath.FromSlash("cmd/cloud-agents-evidencefs-provision/main.go"): true},
		"Revoke":    {filepath.FromSlash("cmd/cloud-agents-evidencefs-provision/main.go"): true},
	}
	seen := map[string]map[string]bool{}
	for symbol := range wants {
		seen[symbol] = map[string]bool{}
	}
	moduleRoot := filepath.Clean(filepath.Join("..", ".."))
	if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); errors.Is(err, os.ErrNotExist) {
		t.Skip("source tree is unavailable to the compiled test binary")
	} else if err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" || entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || filepath.Base(path) == "firewall_test.go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		aliases := map[string]bool{}
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil || importPath != "github.com/hxp0618/cloud-agents/services/control-plane/internal/mountauthority" {
				continue
			}
			alias := "mountauthority"
			if imported.Name != nil {
				alias = imported.Name.Name
			}
			if alias == "." {
				t.Errorf("dot import bypass in %s", path)
				continue
			}
			aliases[alias] = true
		}
		if len(aliases) == 0 {
			return nil
		}
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			identifier, ok := selector.X.(*ast.Ident)
			if !ok || !aliases[identifier.Name] {
				return true
			}
			if allowed, tracked := wants[selector.Sel.Name]; tracked {
				if !allowed[relative] {
					t.Errorf("%s has unreviewed consumer %s", selector.Sel.Name, relative)
				}
				seen[selector.Sel.Name][relative] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for symbol, files := range wants {
		for file := range files {
			if !seen[symbol][file] {
				t.Errorf("%s missing reviewed consumer %s", symbol, file)
			}
		}
	}
}

func TestMountAuthorityHasNoReverseEvidenceDependencyOrEnvironmentOverride(t *testing.T) {
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Skip("source tree is unavailable to the compiled test binary")
	}
	claimCalls := map[string]int{}
	claimLiterals := map[string]int{}
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(importPath, "/internal/evidencefs") || strings.HasSuffix(importPath, "/internal/migration") {
				t.Fatalf("reverse authority dependency in %s: %s", path, importPath)
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if identifier, ok := call.Fun.(*ast.Ident); ok && identifier.Name == "newClaim" {
					claimCalls[path]++
				}
			}
			if literal, ok := node.(*ast.CompositeLit); ok {
				if identifier, ok := literal.Type.(*ast.Ident); ok && identifier.Name == "Claim" {
					claimLiterals[path]++
				}
			}
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Getenv" {
				return true
			}
			if identifier, ok := selector.X.(*ast.Ident); ok && identifier.Name == "os" {
				t.Errorf("environment override in %s", path)
			}
			return true
		})
	}
	if claimCalls["authority_linux.go"] != 1 || len(claimCalls) != 1 {
		t.Fatalf("claim mint calls=%v", claimCalls)
	}
	if claimLiterals["authority.go"] != 1 || len(claimLiterals) != 1 {
		t.Fatalf("claim literals=%v", claimLiterals)
	}
}
