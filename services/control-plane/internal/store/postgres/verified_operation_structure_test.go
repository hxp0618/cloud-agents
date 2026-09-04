package postgres

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type productionAST struct {
	files     map[string]*ast.File
	functions map[string]*ast.FuncDecl
}

func parseProductionAST(t *testing.T, names ...string) productionAST {
	t.Helper()
	result := productionAST{files: make(map[string]*ast.File), functions: make(map[string]*ast.FuncDecl)}
	fileSet := token.NewFileSet()
	for _, name := range names {
		file, err := parser.ParseFile(fileSet, name, nil, parser.AllErrors)
		if err != nil {
			t.Fatal(err)
		}
		result.files[name] = file
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			key := function.Name.Name
			if function.Recv != nil {
				key = expressionText(t, function.Recv.List[0].Type) + "." + key
			}
			if _, duplicate := result.functions[key]; duplicate {
				t.Fatalf("duplicate production function key %s", key)
			}
			result.functions[key] = function
		}
	}
	return result
}

func parseAllProductionAST(t *testing.T) productionAST {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	productionNames := names[:0]
	for _, name := range names {
		if !strings.HasSuffix(name, "_test.go") {
			productionNames = append(productionNames, name)
		}
	}
	if len(productionNames) == 0 {
		t.Fatal("postgres production source set is empty")
	}
	return parseProductionAST(t, productionNames...)
}

func expressionText(t *testing.T, node any) string {
	t.Helper()
	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), node); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func flattenedFields(t *testing.T, fields *ast.FieldList, requireNames bool) []string {
	t.Helper()
	if fields == nil {
		return nil
	}
	var result []string
	for _, field := range fields.List {
		typeName := expressionText(t, field.Type)
		if len(field.Names) == 0 {
			if requireNames {
				t.Fatalf("unnamed field with type %s", typeName)
			}
			result = append(result, typeName)
			continue
		}
		for _, name := range field.Names {
			result = append(result, name.Name+" "+typeName)
		}
	}
	return result
}

func requireSignature(t *testing.T, function *ast.FuncDecl, receiver string, parameters, results []string) {
	t.Helper()
	if function == nil {
		t.Fatal("required function is absent")
	}
	actualReceiver := ""
	if function.Recv != nil && len(function.Recv.List) == 1 {
		actualReceiver = expressionText(t, function.Recv.List[0].Type)
	}
	if actualReceiver != receiver {
		t.Fatalf("%s receiver = %q, want %q", function.Name.Name, actualReceiver, receiver)
	}
	if got := flattenedFields(t, function.Type.Params, true); strings.Join(got, "|") != strings.Join(parameters, "|") {
		t.Fatalf("%s parameters = %#v, want %#v", function.Name.Name, got, parameters)
	}
	if got := flattenedFields(t, function.Type.Results, false); strings.Join(got, "|") != strings.Join(results, "|") {
		t.Fatalf("%s results = %#v, want %#v", function.Name.Name, got, results)
	}
}

func terminalCallName(call *ast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name
	case *ast.SelectorExpr:
		return function.Sel.Name
	default:
		return ""
	}
}

func callsNamed(node ast.Node, name string) []*ast.CallExpr {
	var result []*ast.CallExpr
	ast.Inspect(node, func(candidate ast.Node) bool {
		call, ok := candidate.(*ast.CallExpr)
		if ok && terminalCallName(call) == name {
			result = append(result, call)
		}
		return true
	})
	return result
}

func requireOneCall(t *testing.T, node ast.Node, name string) *ast.CallExpr {
	t.Helper()
	calls := callsNamed(node, name)
	if len(calls) != 1 {
		t.Fatalf("%s call count = %d, want 1", name, len(calls))
	}
	return calls[0]
}

func requireCallbackArgument(t *testing.T, call *ast.CallExpr, index int) *ast.FuncLit {
	t.Helper()
	if index >= len(call.Args) {
		t.Fatalf("%s has %d arguments, callback index %d is absent", terminalCallName(call), len(call.Args), index)
	}
	callback, ok := call.Args[index].(*ast.FuncLit)
	if !ok {
		t.Fatalf("%s argument %d is %T, want function literal", terminalCallName(call), index, call.Args[index])
	}
	return callback
}

func requirePositionOrder(t *testing.T, label string, nodes ...ast.Node) {
	t.Helper()
	for index := 1; index < len(nodes); index++ {
		if nodes[index-1].Pos() >= nodes[index].Pos() {
			t.Fatalf("%s order drift between item %d and %d", label, index-1, index)
		}
	}
}

func productionCallers(tree productionAST, callee string) []string {
	var callers []string
	for key, function := range tree.functions {
		if len(callsNamed(function.Body, callee)) != 0 {
			callers = append(callers, key)
		}
	}
	sort.Strings(callers)
	return callers
}

func requireCallerClosure(t *testing.T, tree productionAST, callee string, expected ...string) {
	t.Helper()
	sort.Strings(expected)
	actual := productionCallers(tree, callee)
	if strings.Join(actual, "|") != strings.Join(expected, "|") {
		t.Fatalf("production callers of %s = %#v, want %#v", callee, actual, expected)
	}
}

func TestRBACMutationVerifiedOperationCallGraphIsClosed(t *testing.T) {
	tree := parseAllProductionAST(t)
	want := map[string]struct {
		input  string
		result []string
	}{
		"CreateMembership":  {input: "CreateMembershipInput", result: []string{"MutationResult", "error"}},
		"ResumeMembership":  {input: "MembershipTransitionInput", result: []string{"MutationResult", "error"}},
		"SuspendMembership": {input: "MembershipTransitionInput", result: []string{"MutationResult", "error"}},
		"RevokeMembership":  {input: "MembershipTransitionInput", result: []string{"MutationResult", "error"}},
		"BindRole":          {input: "BindRoleInput", result: []string{"MutationResult", "error"}},
		"RevokeRoleBinding": {input: "RevokeRoleBindingInput", result: []string{"MutationResult", "error"}},
	}
	found := make(map[string]bool)
	for _, function := range tree.functions {
		if function.Recv == nil || expressionText(t, function.Recv.List[0].Type) != "*RBACMutationService" || !ast.IsExported(function.Name.Name) {
			continue
		}
		contract, allowed := want[function.Name.Name]
		if !allowed {
			t.Fatalf("unexpected exported RBAC mutation method %s", function.Name.Name)
		}
		requireSignature(t, function, "*RBACMutationService", []string{
			"ctx context.Context", "tenantID string", "principal *authn.VerifiedPrincipal", "input " + contract.input,
		}, contract.result)
		if found[function.Name.Name] {
			t.Fatalf("duplicate RBAC mutation method %s", function.Name.Name)
		}
		found[function.Name.Name] = true
	}
	for name := range want {
		if !found[name] {
			t.Fatalf("required RBAC mutation method %s is absent", name)
		}
	}

	create := tree.functions["*RBACMutationService.CreateMembership"]
	bind := tree.functions["*RBACMutationService.BindRole"]
	revokeBinding := tree.functions["*RBACMutationService.RevokeRoleBinding"]
	transition := tree.functions["*RBACMutationService.transitionMembership"]
	checkOuterPath := func(method *ast.FuncDecl, helper, kernel string) {
		t.Helper()
		outer := requireOneCall(t, method.Body, "WithVerifiedOperation")
		outerCallback := requireCallbackArgument(t, outer, 1)
		helperCall := requireOneCall(t, outerCallback.Body, helper)
		kernelCallback := requireCallbackArgument(t, helperCall, len(helperCall.Args)-1)
		kernelCall := requireOneCall(t, kernelCallback.Body, kernel)
		requirePositionOrder(t, method.Name.Name, outer, helperCall, kernelCall)
		if len(callsNamed(method.Body, "withTenantMutation")) != 0 || len(callsNamed(method.Body, "executeVerifiedRBACOperation")) != 0 {
			t.Fatalf("%s bypasses the closed scope helper", method.Name.Name)
		}
	}
	checkOuterPath(create, "withKnownScopeMutation", "createMembershipInTransaction")
	checkOuterPath(bind, "withKnownScopeMutation", "bindRoleInTransaction")
	checkOuterPath(revokeBinding, "withStoredScopeMutation", "revokeRoleBindingInTransaction")
	checkOuterPath(transition, "withStoredScopeMutation", "transitionMembershipInTransaction")

	for _, name := range []string{"ResumeMembership", "SuspendMembership", "RevokeMembership"} {
		method := tree.functions["*RBACMutationService."+name]
		call := requireOneCall(t, method.Body, "transitionMembership")
		if len(callsNamed(method.Body, "WithVerifiedOperation")) != 0 || len(call.Args) != 7 {
			t.Fatalf("%s is not an exact thin delegation into the sealed transition path", name)
		}
	}

	known := tree.functions["*RBACMutationService.withKnownScopeMutation"]
	knownBind := requireOneCall(t, known.Body, "Bind")
	knownTransaction := requireOneCall(t, known.Body, "withTenantMutation")
	knownTransactionCallback := requireCallbackArgument(t, knownTransaction, 2)
	knownExecute := requireOneCall(t, knownTransactionCallback.Body, "executeVerifiedRBACOperation")
	knownExecuteCallback := requireCallbackArgument(t, knownExecute, 4)
	knownOperation := requireOneCall(t, knownExecuteCallback.Body, "operation")
	requirePositionOrder(t, "known-scope authorization transaction", knownBind, knownTransaction, knownExecute, knownOperation)

	stored := tree.functions["*RBACMutationService.withStoredScopeMutation"]
	storedTransaction := requireOneCall(t, stored.Body, "withTenantMutation")
	storedTransactionCallback := requireCallbackArgument(t, storedTransaction, 2)
	readScope := requireOneCall(t, storedTransactionCallback.Body, "readMutationScope")
	storedBind := requireOneCall(t, storedTransactionCallback.Body, "Bind")
	storedExecute := requireOneCall(t, storedTransactionCallback.Body, "executeVerifiedRBACOperation")
	storedExecuteCallback := requireCallbackArgument(t, storedExecute, 4)
	storedOperation := requireOneCall(t, storedExecuteCallback.Body, "operation")
	requirePositionOrder(t, "stored-scope authorization transaction", storedTransaction, readScope, storedBind, storedExecute, storedOperation)

	requireCallerClosure(t, tree, "createMembershipInTransaction", "*RBACMutationService.CreateMembership")
	requireCallerClosure(t, tree, "transitionMembershipInTransaction", "*RBACMutationService.transitionMembership")
	requireCallerClosure(t, tree, "bindRoleInTransaction", "*RBACMutationService.BindRole")
	requireCallerClosure(t, tree, "revokeRoleBindingInTransaction", "*RBACMutationService.RevokeRoleBinding")
	requireCallerClosure(t, tree, "withKnownScopeMutation", "*RBACMutationService.BindRole", "*RBACMutationService.CreateMembership")
	requireCallerClosure(t, tree, "withStoredScopeMutation", "*RBACMutationService.RevokeRoleBinding", "*RBACMutationService.transitionMembership")
	requireCallerClosure(t, tree, "transitionMembership", "*RBACMutationService.ResumeMembership", "*RBACMutationService.RevokeMembership", "*RBACMutationService.SuspendMembership")
}

func TestJWTUserDurableCoordinationVerifiedOperationCallGraphIsClosed(t *testing.T) {
	tree := parseAllProductionAST(t)
	contracts := map[string]struct {
		input      string
		result     string
		kernel     string
		settlement string
		profile    string
	}{
		"ClaimIdempotency":           {"IdempotencyClaimInput", "IdempotencyClaimResult", "claimIdempotencyTransaction", "settleIdempotencyClaim", "bindProfile"},
		"CompleteIdempotencySuccess": {"IdempotencySuccessInput", "IdempotencySuccessResult", "completeIdempotencySuccessTransaction", "settleIdempotencySuccess", "bindProfile"},
		"CompleteIdempotencyFailure": {"IdempotencyFailureInput", "IdempotencyFailureResult", "completeIdempotencyFailureTransaction", "settleIdempotencyFailure", "bindProfile"},
		"CreateProjectDurable":       {"DurableProjectCreateInput", "DurableProjectCreateResult", "createDurableProjectTransaction", "settleDurableProjectCreate", "bindDurableProjectCreateProfile"},
	}
	for name, contract := range contracts {
		method := tree.functions["*DurableCoordinationService."+name]
		requireSignature(t, method, "*DurableCoordinationService", []string{
			"ctx context.Context", "tenantID string", "principal *authn.VerifiedPrincipal", "input " + contract.input,
		}, []string{contract.result, "error"})
		outer := requireOneCall(t, method.Body, "WithVerifiedOperation")
		outerCallback := requireCallbackArgument(t, outer, 1)
		profile := requireOneCall(t, outerCallback.Body, contract.profile)
		bind := requireOneCall(t, outerCallback.Body, "Bind")
		actor := requireOneCall(t, outerCallback.Body, "Actor")
		transaction := requireOneCall(t, outerCallback.Body, "withTenantMutation")
		transactionCallback := requireCallbackArgument(t, transaction, 2)
		execute := requireOneCall(t, transactionCallback.Body, "executeVerifiedRBACOperation")
		executeCallback := requireCallbackArgument(t, execute, 4)
		kernel := requireOneCall(t, executeCallback.Body, contract.kernel)
		settlement := requireOneCall(t, outerCallback.Body, contract.settlement)
		requirePositionOrder(t, name, outer, profile, bind, actor, transaction, execute, kernel, settlement)
		if len(callsNamed(method.Body, "authorizeMutation")) != 0 || len(callsNamed(method.Body, "bindAuthorizedProfile")) != 0 {
			t.Fatalf("%s retains a raw-actor authorization bypass", name)
		}
		requireCallerClosure(t, tree, contract.kernel, "*DurableCoordinationService."+name)
	}

	requireCallerClosure(t, tree, "WithVerifiedOperation",
		"*DurableCoordinationService.BeginDeploymentTargetCleanup",
		"*DurableCoordinationService.BeginDeploymentTargetProbe",
		"*DurableCoordinationService.BeginManagedHostEnvironmentLeaseUpgrade",
		"*DurableCoordinationService.ClaimIdempotency",
		"*DurableCoordinationService.CloseManagedAgentSession",
		"*DurableCoordinationService.CompleteDeploymentTargetProbe",
		"*DurableCoordinationService.CompleteDeploymentTargetCleanup",
		"*DurableCoordinationService.CompleteManagedHostEnvironmentLeaseDeployment",
		"*DurableCoordinationService.CompleteManagedHostEnvironmentLeaseTermination",
		"*DurableCoordinationService.CompleteIdempotencyFailure",
		"*DurableCoordinationService.CompleteIdempotencySuccess",
		"*DurableCoordinationService.CreateEnvironmentProfile",
		"*DurableCoordinationService.CreateManagedAgentSession",
		"*DurableCoordinationService.CreateManagedAgentTurn",
		"*DurableCoordinationService.CreateManagedHostEnvironmentLease",
		"*DurableCoordinationService.CreateOrganization",
		"*DurableCoordinationService.CreateProjectDurable",
		"*DurableCoordinationService.CreateUserEnvironment",
		"*DurableCoordinationService.GetManagedAgentEvents",
		"*DurableCoordinationService.GetDeploymentTarget",
		"*DurableCoordinationService.GetEnvironmentProfile",
		"*DurableCoordinationService.GetManagedAgentSession",
		"*DurableCoordinationService.getManagedAgentSessionForRuntime",
		"*DurableCoordinationService.readManagedAgentTurn",
		"*DurableCoordinationService.ListManagedAgentTurns",
		"*DurableCoordinationService.GetManagedAgentExecution",
		"*DurableCoordinationService.ListManagedAgentExecutions",
		"*DurableCoordinationService.GetManagedHostEnvironmentLease",
		"*DurableCoordinationService.ListManagedHostEnvironmentLeases",
		"*DurableCoordinationService.ListMaintenanceOperations",
		"*DurableCoordinationService.ListDeploymentTargetAuditEvents",
		"*DurableCoordinationService.ListDeploymentTargetOperations",
		"*DurableCoordinationService.ListDeploymentTargets",
		"*DurableCoordinationService.ListEnvironmentProfileAuditEvents",
		"*DurableCoordinationService.ListEnvironmentProfiles",
		"*DurableCoordinationService.ListPublishedEnvironmentProfiles",
		"*DurableCoordinationService.PreviewDeploymentTargetScheduling",
		"*DurableCoordinationService.GetOrganization",
		"*DurableCoordinationService.GetPlatformTenant",
		"*DurableCoordinationService.GetProject",
		"*DurableCoordinationService.GetUserEnvironment",
		"*DurableCoordinationService.ListAdminWorkers",
		"*DurableCoordinationService.ListOrganizations",
		"*DurableCoordinationService.ListManagedAgentSessions",
		"*DurableCoordinationService.ListProjects",
		"*DurableCoordinationService.GetRole",
		"*DurableCoordinationService.ListRoles",
		"*DurableCoordinationService.RegisterDeploymentTarget",
		"*DurableCoordinationService.GetMembership",
		"*DurableCoordinationService.ListMemberships",
		"*DurableCoordinationService.ListRoleBindings",
		"*DurableCoordinationService.GetRoleBinding",
		"*DurableCoordinationService.TerminateManagedHostEnvironmentLease",
		"*DurableCoordinationService.TransitionDeploymentTargetScheduling",
		"*DurableCoordinationService.TransitionEnvironmentProfile",
		"*RBACMutationService.BindRole",
		"*RBACMutationService.CreateMembership",
		"*RBACMutationService.RevokeRoleBinding",
		"*RBACMutationService.transitionMembership",
		"withManagedAgentProjectMutation",
	)
	requireCallerClosure(t, tree, "executeVerifiedRBACOperation",
		"*DurableCoordinationService.BeginDeploymentTargetCleanup",
		"*DurableCoordinationService.BeginDeploymentTargetProbe",
		"*DurableCoordinationService.BeginManagedHostEnvironmentLeaseUpgrade",
		"*DurableCoordinationService.ClaimIdempotency",
		"*DurableCoordinationService.CloseManagedAgentSession",
		"*DurableCoordinationService.CompleteDeploymentTargetProbe",
		"*DurableCoordinationService.CompleteDeploymentTargetCleanup",
		"*DurableCoordinationService.CompleteManagedHostEnvironmentLeaseDeployment",
		"*DurableCoordinationService.CompleteManagedHostEnvironmentLeaseTermination",
		"*DurableCoordinationService.CompleteIdempotencyFailure",
		"*DurableCoordinationService.CompleteIdempotencySuccess",
		"*DurableCoordinationService.CreateEnvironmentProfile",
		"*DurableCoordinationService.CreateManagedAgentSession",
		"*DurableCoordinationService.CreateManagedAgentTurn",
		"*DurableCoordinationService.CreateManagedHostEnvironmentLease",
		"*DurableCoordinationService.CreateOrganization",
		"*DurableCoordinationService.CreateProjectDurable",
		"*DurableCoordinationService.CreateUserEnvironment",
		"*DurableCoordinationService.GetManagedAgentEvents",
		"*DurableCoordinationService.GetDeploymentTarget",
		"*DurableCoordinationService.GetEnvironmentProfile",
		"*DurableCoordinationService.GetManagedAgentSession",
		"*DurableCoordinationService.getManagedAgentSessionForRuntime",
		"*DurableCoordinationService.readManagedAgentTurn",
		"*DurableCoordinationService.ListManagedAgentTurns",
		"*DurableCoordinationService.GetManagedAgentExecution",
		"*DurableCoordinationService.ListManagedAgentExecutions",
		"*DurableCoordinationService.GetManagedHostEnvironmentLease",
		"*DurableCoordinationService.ListManagedHostEnvironmentLeases",
		"*DurableCoordinationService.ListMaintenanceOperations",
		"*DurableCoordinationService.ListDeploymentTargetAuditEvents",
		"*DurableCoordinationService.ListDeploymentTargetOperations",
		"*DurableCoordinationService.ListDeploymentTargets",
		"*DurableCoordinationService.ListEnvironmentProfileAuditEvents",
		"*DurableCoordinationService.ListEnvironmentProfiles",
		"*DurableCoordinationService.ListPublishedEnvironmentProfiles",
		"*DurableCoordinationService.PreviewDeploymentTargetScheduling",
		"*DurableCoordinationService.GetOrganization",
		"*DurableCoordinationService.GetPlatformTenant",
		"*DurableCoordinationService.GetProject",
		"*DurableCoordinationService.GetUserEnvironment",
		"*DurableCoordinationService.ListAdminWorkers",
		"*DurableCoordinationService.ListOrganizations",
		"*DurableCoordinationService.ListManagedAgentSessions",
		"*DurableCoordinationService.ListProjects",
		"*DurableCoordinationService.GetRole",
		"*DurableCoordinationService.ListRoles",
		"*DurableCoordinationService.RegisterDeploymentTarget",
		"*DurableCoordinationService.GetMembership",
		"*DurableCoordinationService.ListMemberships",
		"*DurableCoordinationService.ListRoleBindings",
		"*DurableCoordinationService.GetRoleBinding",
		"*DurableCoordinationService.TerminateManagedHostEnvironmentLease",
		"*DurableCoordinationService.TransitionDeploymentTargetScheduling",
		"*DurableCoordinationService.TransitionEnvironmentProfile",
		"*RBACMutationService.withKnownScopeMutation",
		"*RBACMutationService.withStoredScopeMutation",
		"withManagedAgentProjectMutation",
	)
	for _, forbidden := range []string{"authorizeMutation", "bindAuthorizedProfile"} {
		if callers := productionCallers(tree, forbidden); len(callers) != 0 {
			t.Fatalf("forbidden bypass %s callers = %#v", forbidden, callers)
		}
		if _, declared := tree.functions[forbidden]; declared {
			t.Fatalf("forbidden bypass %s is declared", forbidden)
		}
	}
}

func TestLeaderOutboxFencingProductionSectionMatchesSliceCBase(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "durable_coordination.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{
		"acquireCoordinationLeaderSQL": true, "renewCoordinationLeaderSQL": true,
		"claimOutboxEventSQL": true, "acknowledgeOutboxEventSQL": true, "retryOutboxEventSQL": true,
		"deadLetterOutboxEventSQL": true, "reapExpiredOutboxClaimSQL": true,
		"LeaderLeaseInput": true, "LeaderLeaseResult": true, "OutboxClaimInput": true, "OutboxClaim": true,
		"OutboxClaimResult": true, "OutboxSettlementInput": true, "OutboxSettlementResult": true,
		"OutboxReapInput": true, "OutboxReapResult": true,
		"AcquireLeader": true, "RenewLeader": true, "ClaimOutbox": true, "AcknowledgeOutbox": true,
		"RetryOutbox": true, "DeadLetterOutbox": true, "ReapExpiredOutbox": true, "settleOutbox": true,
		"settleLeaderResult": true, "validLeaderInput": true, "validOutboxClaim": true,
		"validSettledOutboxState": true, "newOutboxDispatcher": true, "dispatchOne": true,
	}
	var canonical bytes.Buffer
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if selected[declaration.Name.Name] {
				canonical.WriteString(expressionText(t, declaration))
				canonical.WriteByte('\n')
				delete(selected, declaration.Name.Name)
			}
		case *ast.GenDecl:
			for _, specification := range declaration.Specs {
				switch specification := specification.(type) {
				case *ast.TypeSpec:
					if selected[specification.Name.Name] {
						canonical.WriteString("type ")
						canonical.WriteString(expressionText(t, specification))
						canonical.WriteByte('\n')
						delete(selected, specification.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range specification.Names {
						if selected[name.Name] {
							canonical.WriteString("const ")
							canonical.WriteString(expressionText(t, specification))
							canonical.WriteByte('\n')
							delete(selected, name.Name)
						}
					}
				}
			}
		}
	}
	if len(selected) != 0 {
		missing := make([]string, 0, len(selected))
		for name := range selected {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		t.Fatalf("machine production declarations absent: %#v", missing)
	}
	digest := sha256.Sum256(canonical.Bytes())
	const want = "5006b085e62a12d65b82dff60b16124ac865cda728ad73301888aee829d70899"
	if actual := hex.EncodeToString(digest[:]); actual != want {
		t.Fatalf("leader/outbox/fencing production section digest = %s, want %s (base d2e464b)", actual, want)
	}
}

func TestProtectedTransactionKernelProductionCallerSetIsExact(t *testing.T) {
	tree := parseAllProductionAST(t)
	want := map[string][]string{
		"claimIdempotencyTransaction":           {"*DurableCoordinationService.ClaimIdempotency"},
		"completeIdempotencySuccessTransaction": {"*DurableCoordinationService.CompleteIdempotencySuccess"},
		"completeIdempotencyFailureTransaction": {"*DurableCoordinationService.CompleteIdempotencyFailure"},
		"createDurableProjectTransaction":       {"*DurableCoordinationService.CreateProjectDurable"},
		"createMembershipInTransaction":         {"*RBACMutationService.CreateMembership"},
		"transitionMembershipInTransaction":     {"*RBACMutationService.transitionMembership"},
		"bindRoleInTransaction":                 {"*RBACMutationService.BindRole"},
		"revokeRoleBindingInTransaction":        {"*RBACMutationService.RevokeRoleBinding"},
	}
	for kernel, callers := range want {
		if function := tree.functions[kernel]; function == nil || function.Recv != nil || ast.IsExported(kernel) {
			t.Fatalf("transaction kernel %s is absent, a method, or exported", kernel)
		}
		requireCallerClosure(t, tree, kernel, callers...)
	}
}
