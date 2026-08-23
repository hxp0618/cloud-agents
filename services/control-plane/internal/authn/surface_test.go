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
		escapedReferences := 0
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "ConsumeVerifiedPrincipal" || identifier.Name == "verifyAccessToken") {
				escapedReferences++
			}
			return true
		})
		if escapedReferences != 0 {
			t.Fatalf("Slice B production caller escaped authn: %s (%d identifier references)", path, escapedReferences)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
