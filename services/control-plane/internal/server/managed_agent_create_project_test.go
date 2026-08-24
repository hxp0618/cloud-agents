package server

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"testing"

	commonv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

func TestManagedAgentCreateProjectClaimMappingIsExact(t *testing.T) {
	validated := openapiv1.CreateProjectServerInput{
		TenantID:       "tenant-route",
		RequestID:      "request-route",
		IdempotencyKey: "idempotency-route-01",
		Body: platformv1.ProjectCreateRequest{
			Name: "project-alpha",
			OrganizationRef: commonv1.OrganizationRef{
				Namespace: "cloud-agents", Kind: "organization", ID: "organization-alpha",
			},
			DisplayName: "Project Alpha",
		},
	}
	got := mapManagedAgentCreateProjectClaim(validated)
	want := postgres.IdempotencyClaimInput{
		Profile: coordination.ManagedAgentCreateProject(),
		Request: coordination.ManagedAgentCreateProjectRequest{
			Name: "project-alpha",
			OrganizationRef: coordination.OrganizationRef{
				Namespace: "cloud-agents", Kind: "organization", ID: "organization-alpha",
			},
			DisplayName: "Project Alpha",
		},
		IdempotencyKey: "idempotency-route-01",
		AuditFactID:    "request-route",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("claim mapping = %#v, want %#v", got, want)
	}
}

func TestManagedAgentCreateProjectServerFailsClosed(t *testing.T) {
	if server, err := NewManagedAgentCreateProjectServer(nil); server != nil || !errors.Is(err, ErrNilManagedAgentCreateProjectService) {
		t.Fatalf("nil service constructor = %#v/%v", server, err)
	}
	var server *ManagedAgentCreateProjectServer
	if _, err := server.Claim(context.Background(), nil, ManagedAgentCreateProjectRequest{}); !errors.Is(err, ErrNilManagedAgentCreateProjectService) {
		t.Fatalf("nil server claim error = %v", err)
	}
}

func TestGeneratedValidationRejectsBeforeConcreteService(t *testing.T) {
	server, err := NewManagedAgentCreateProjectServer(&postgres.DurableCoordinationService{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.Claim(context.Background(), nil, ManagedAgentCreateProjectRequest{
		RouteTenantID: "tenant-alpha",
		RequestID:     "request-alpha",
		// Invalid before JSON decoding and before the concrete service can
		// reject its deliberately empty runner.
		IdempotencyKey: "short",
		Body:           []byte(`{}`),
	})
	if err == nil || errors.Is(err, postgres.ErrNilCoordinationRunner) {
		t.Fatalf("generated validation boundary error = %v", err)
	}
}

func TestManagedAgentCreateProjectServerASTClosure(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "managed_agent_create_project.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}

	forbiddenImports := map[string]bool{
		"net/http": true, "reflect": true, "unsafe": true,
	}
	for _, imported := range file.Imports {
		path, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			t.Fatal(unquoteErr)
		}
		if forbiddenImports[path] {
			t.Fatalf("forbidden server import %q", path)
		}
	}

	fieldIsConcrete := false
	requestIsExact := false
	var claim *ast.FuncDecl
	for _, declaration := range file.Decls {
		switch node := declaration.(type) {
		case *ast.GenDecl:
			for _, specification := range node.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if typeSpec.Name.Name == "ManagedAgentCreateProjectRequest" {
					requestIsExact = exactManagedAgentCreateProjectRequest(typeSpec)
					continue
				}
				if typeSpec.Name.Name != "ManagedAgentCreateProjectServer" {
					continue
				}
				structure, ok := typeSpec.Type.(*ast.StructType)
				if !ok || len(structure.Fields.List) != 1 {
					t.Fatal("server authority must be one concrete field")
				}
				field := structure.Fields.List[0]
				star, ok := field.Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				selector, selectorOK := star.X.(*ast.SelectorExpr)
				if !selectorOK {
					continue
				}
				packageName, packageOK := selector.X.(*ast.Ident)
				fieldIsConcrete = packageOK && len(field.Names) == 1 &&
					field.Names[0].Name == "service" && packageName.Name == "postgres" &&
					selector.Sel.Name == "DurableCoordinationService"
			}
		case *ast.FuncDecl:
			if node.Name.Name == "Claim" {
				claim = node
			}
		}
	}
	if !fieldIsConcrete {
		t.Fatal("server service field is not *postgres.DurableCoordinationService")
	}
	if !requestIsExact {
		t.Fatal("transport-neutral request is not the exact four-field authority")
	}
	if claim == nil || claim.Body == nil {
		t.Fatal("Claim method is absent")
	}

	positions := map[string]token.Pos{}
	ast.Inspect(claim.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.GoStmt:
			t.Fatal("Claim must not start a goroutine")
		case *ast.DeferStmt:
			t.Fatal("Claim must not defer hidden work")
		case *ast.CallExpr:
			selector, ok := typed.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "ValidateCreateProjectServerRequest", "ClaimIdempotency":
				if positions[selector.Sel.Name] != token.NoPos {
					t.Fatalf("duplicate %s call", selector.Sel.Name)
				}
				positions[selector.Sel.Name] = typed.Pos()
			case "CompleteIdempotencySuccess", "CompleteIdempotencyFailure", "BindManagedAgentCreateProject":
				t.Fatalf("forbidden server call %s", selector.Sel.Name)
			}
		case *ast.Ident:
			if typed.Name == "mapManagedAgentCreateProjectClaim" {
				positions[typed.Name] = typed.Pos()
			}
		}
		return true
	})
	validatorPosition := positions["ValidateCreateProjectServerRequest"]
	mapperPosition := positions["mapManagedAgentCreateProjectClaim"]
	claimPosition := positions["ClaimIdempotency"]
	if validatorPosition == token.NoPos || mapperPosition == token.NoPos || claimPosition == token.NoPos ||
		!(validatorPosition < mapperPosition && mapperPosition < claimPosition) {
		t.Fatalf("validator/map/claim order = %d/%d/%d", validatorPosition, mapperPosition, claimPosition)
	}

	requireExactClaimCall(t, claim)
}

func exactManagedAgentCreateProjectRequest(specification *ast.TypeSpec) bool {
	structure, ok := specification.Type.(*ast.StructType)
	if !ok || len(structure.Fields.List) != 4 {
		return false
	}
	want := []struct {
		name     string
		typeName string
	}{{"RouteTenantID", "string"}, {"RequestID", "string"}, {"IdempotencyKey", "string"}, {"Body", "byte"}}
	for index, field := range structure.Fields.List {
		if len(field.Names) != 1 || field.Names[0].Name != want[index].name {
			return false
		}
		if want[index].name == "Body" {
			array, arrayOK := field.Type.(*ast.ArrayType)
			if !arrayOK {
				return false
			}
			element, elementOK := array.Elt.(*ast.Ident)
			if !elementOK || array.Len != nil || element.Name != want[index].typeName {
				return false
			}
			continue
		}
		identifier, identifierOK := field.Type.(*ast.Ident)
		if !identifierOK || identifier.Name != want[index].typeName {
			return false
		}
	}
	return true
}

func requireExactClaimCall(t *testing.T, declaration *ast.FuncDecl) {
	t.Helper()
	found := false
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "ClaimIdempotency" {
			return true
		}
		if found || len(call.Args) != 4 {
			t.Fatal("ClaimIdempotency must have one exact four-argument call")
		}
		found = true
		ctx, ctxOK := call.Args[0].(*ast.Ident)
		tenant, tenantOK := call.Args[1].(*ast.SelectorExpr)
		validated, validatedOK := tenant.X.(*ast.Ident)
		principal, principalOK := call.Args[2].(*ast.Ident)
		claim, claimOK := call.Args[3].(*ast.Ident)
		if !ctxOK || ctx.Name != "ctx" || !tenantOK || !validatedOK || validated.Name != "validated" || tenant.Sel.Name != "TenantID" ||
			!principalOK || principal.Name != "principal" || !claimOK || claim.Name != "claim" {
			t.Fatal("ClaimIdempotency arguments are not ctx, validated.TenantID, principal, claim")
		}
		return true
	})
	if !found {
		t.Fatal("ClaimIdempotency call is absent")
	}
}
