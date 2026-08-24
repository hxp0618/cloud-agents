package authn

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestProductionSurfaceAndDependencyClosure(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	exported := make([]string, 0)
	allowedImports := map[string]struct{}{
		"bytes": {}, "crypto": {}, "crypto/rsa": {}, "crypto/sha256": {},
		"encoding/base64": {}, "encoding/hex": {}, "encoding/json": {}, "io": {},
		"math/big": {}, "regexp": {}, "sort": {}, "strconv": {}, "strings": {},
		"sync": {}, "sync/atomic": {}, "time": {}, "unicode/utf8": {},
	}
	typeFields := make(map[string][]string)
	interfaceMethods := make(map[string][]string)
	receiverMethods := make(map[string][]string)
	productionReferences := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if _, allowed := allowedImports[path]; !allowed {
				t.Fatalf("production authn import is not exact standard-library allowlist: %q", path)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "ConsumeVerifiedPrincipal" || identifier.Name == "verifyAccessToken") {
				productionReferences++
			}
			return true
		})
		for _, declaration := range file.Decls {
			switch item := declaration.(type) {
			case *ast.FuncDecl:
				receiver := receiverTypeName(item)
				if receiver != "" {
					receiverMethods[receiver] = append(receiverMethods[receiver], item.Name.Name)
				}
				if item.Recv == nil && ast.IsExported(item.Name.Name) {
					exported = append(exported, item.Name.Name)
				}
				if item.Recv == nil && (item.Name.Name == "ConsumeVerifiedPrincipal" || item.Name.Name == "verifyAccessToken") {
					productionReferences--
				}
			case *ast.GenDecl:
				for _, specification := range item.Specs {
					if typeSpecification, ok := specification.(*ast.TypeSpec); ok {
						if ast.IsExported(typeSpecification.Name.Name) {
							exported = append(exported, typeSpecification.Name.Name)
						}
						switch typeSpecification.Name.Name {
						case "VerifiedPrincipal", "verifiedPrincipalView":
							structure, ok := typeSpecification.Type.(*ast.StructType)
							if !ok {
								t.Fatalf("%s is no longer a struct", typeSpecification.Name.Name)
							}
							typeFields[typeSpecification.Name.Name] = exactNamedFields(t, typeSpecification.Name.Name, structure.Fields.List)
						case "VerifiedPrincipalView":
							contract, ok := typeSpecification.Type.(*ast.InterfaceType)
							if !ok {
								t.Fatal("VerifiedPrincipalView is no longer an interface")
							}
							interfaceMethods[typeSpecification.Name.Name] = exactNamedFields(t, typeSpecification.Name.Name, contract.Methods.List)
						}
					}
					if valueSpecification, ok := specification.(*ast.ValueSpec); ok {
						for _, identifier := range valueSpecification.Names {
							if ast.IsExported(identifier.Name) {
								exported = append(exported, identifier.Name)
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(exported)
	want := []string{"ConsumeVerifiedPrincipal", "VerifiedPrincipal", "VerifiedPrincipalView"}
	if strings.Join(exported, ",") != strings.Join(want, ",") {
		t.Fatalf("production export surface changed: got=%v want=%v", exported, want)
	}
	assertExactNames(t, "VerifiedPrincipal fields", typeFields["VerifiedPrincipal"], []string{
		"audience", "clientID", "clock", "consumed", "expiresAt", "generation", "hasNotBefore", "hasTokenProject",
		"issuedAt", "issuer", "keyID", "lineage", "notBefore", "principalDigest", "profileDigest", "registryDigest",
		"requiredPermission", "scopes", "securityEpoch", "self", "snapshotDigest", "snapshotGeneration", "subjectKind",
		"subjectValue", "targetResourceID", "targetResourceLevel", "targetTenantID", "tokenID", "tokenInputDigest",
		"tokenProjectID", "tokenType",
	})
	assertExactNames(t, "VerifiedPrincipal methods", receiverMethods["VerifiedPrincipal"], []string{"matchesSnapshot", "selfBound"})
	assertExactNames(t, "VerifiedPrincipalView methods", interfaceMethods["VerifiedPrincipalView"], []string{
		"Actor", "AuthorizationContext", "Check", "sealedVerifiedPrincipalView",
	})
	assertExactNames(t, "verifiedPrincipalView fields", typeFields["verifiedPrincipalView"], []string{"live", "principal"})
	assertExactNames(t, "verifiedPrincipalView methods", receiverMethods["verifiedPrincipalView"], []string{
		"Actor", "AuthorizationContext", "Check", "sealedVerifiedPrincipalView",
	})
	if productionReferences != 0 {
		t.Fatalf("Slice B has %d production principal/verifier identifier references", productionReferences)
	}
	consumeCallers := make([]string, 0)
	verifyCallers := make([]string, 0)
	err = filepath.WalkDir("../..", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasPrefix(path, "../../internal/authn/") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && identifier.Name == "ConsumeVerifiedPrincipal" {
				consumeCallers = append(consumeCallers, path)
			}
			if ok && identifier.Name == "verifyAccessToken" {
				verifyCallers = append(verifyCallers, path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(consumeCallers) != 1 || filepath.Clean(consumeCallers[0]) != filepath.Clean("../../internal/authz/rbac.go") {
		t.Fatalf("Slice C ConsumeVerifiedPrincipal closure changed: %v", consumeCallers)
	}
	if len(verifyCallers) != 0 {
		t.Fatalf("production verifyAccessToken caller escaped authn: %v", verifyCallers)
	}
	assertAuthzBinderSurface(t)
}

func assertAuthzBinderSurface(t *testing.T) {
	t.Helper()
	path := filepath.Clean("../authz/rbac.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	imports := make([]string, 0, len(file.Imports))
	for _, imported := range file.Imports {
		value, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			t.Fatal(unquoteErr)
		}
		imports = append(imports, value)
	}
	sort.Strings(imports)
	wantImports := []string{
		"bytes", "crypto/sha256", "encoding/binary", "encoding/hex", "encoding/json", "errors", "fmt",
		"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn", "strings", "sync", "sync/atomic", "time", "unicode/utf8",
	}
	sort.Strings(wantImports)
	if strings.Join(imports, ",") != strings.Join(wantImports, ",") {
		t.Fatalf("authz dependency closure changed: got=%v want=%v", imports, wantImports)
	}

	exported := make([]string, 0)
	methods := make(map[string][]string)
	for _, declaration := range file.Decls {
		switch item := declaration.(type) {
		case *ast.FuncDecl:
			receiver := receiverTypeName(item)
			if receiver != "" && ast.IsExported(item.Name.Name) {
				methods[receiver] = append(methods[receiver], item.Name.Name)
			} else if item.Recv == nil && ast.IsExported(item.Name.Name) {
				exported = append(exported, item.Name.Name)
			}
		case *ast.GenDecl:
			for _, specification := range item.Specs {
				switch value := specification.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(value.Name.Name) {
						exported = append(exported, value.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range value.Names {
						if ast.IsExported(name.Name) {
							exported = append(exported, name.Name)
						}
					}
				}
			}
		}
	}
	assertExactNames(t, "authz exports", exported, []string{
		"BindingActive", "BindingRevoked", "Candidate", "Catalog", "ErrCatalogDrift", "ErrInvalidRequest",
		"ErrOperationDenied", "ErrScopeUnresolved", "ErrSnapshotMalformed", "MembershipActive", "MembershipFact",
		"MembershipRevoked", "MembershipSuspended", "Role", "RoleBindingFact", "ScopeLevel", "ScopeOrganization",
		"ScopePath", "ScopePlatform", "ScopeProject", "ScopeRef", "ScopeTenant", "Snapshot", "SubjectRef",
		"VerifiedOperation", "VerifiedOperationBinder", "WithVerifiedOperation",
	})
	assertExactNames(t, "VerifiedOperation methods", methods["VerifiedOperation"], []string{"Actor", "Execute"})
	assertExactNames(t, "VerifiedOperationBinder methods", methods["VerifiedOperationBinder"], []string{"Bind"})
	for _, forbidden := range []string{"Request", "Decision", "Evaluate", "DenyReason"} {
		for _, name := range exported {
			if name == forbidden {
				t.Fatalf("raw authorization bypass remains exported: %s", forbidden)
			}
		}
	}
}

func receiverTypeName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return ""
	}
	receiver := function.Recv.List[0].Type
	if pointer, ok := receiver.(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	identifier, _ := receiver.(*ast.Ident)
	if identifier == nil {
		return ""
	}
	return identifier.Name
}

func exactNamedFields(t *testing.T, owner string, fields []*ast.Field) []string {
	t.Helper()
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field.Names) != 1 {
			t.Fatalf("%s contains embedded or multi-name field/method", owner)
		}
		name := field.Names[0].Name
		if owner == "VerifiedPrincipal" && ast.IsExported(name) {
			t.Fatalf("VerifiedPrincipal exports field %s", name)
		}
		names = append(names, name)
	}
	return names
}

func assertExactNames(t *testing.T, owner string, got, want []string) {
	t.Helper()
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s changed: got=%v want=%v", owner, got, want)
	}
}

func TestCanonicalPrincipalPayloadHasNoAuthorityMechanics(t *testing.T) {
	_, err := parser.ParseFile(token.NewFileSet(), "principal.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var forbidden []string
	source, err := os.ReadFile(filepath.Clean("principal.go"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalStart := strings.Index(string(source), "func principalCanonical")
	if canonicalStart < 0 {
		t.Fatal("principal canonical projection missing")
	}
	projection := string(source[canonicalStart:])
	for _, name := range []string{"consumed", "generation.lease", "lineage", "rawToken", "signature", "jwk", "unknownClaims"} {
		if strings.Contains(projection, name) {
			forbidden = append(forbidden, name)
		}
	}
	if len(forbidden) != 0 {
		t.Fatalf("canonical principal projection contains authority mechanics: %v", forbidden)
	}
}
