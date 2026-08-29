package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
)

type globalOptions struct {
	endpoint       string
	token          string
	tenant         string
	organization   string
	project        string
	membership     string
	role           string
	roleBinding    string
	session        string
	turn           string
	execution      string
	requestID      string
	idempotencyKey string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cloud-agentsctl:", err)
		os.Exit(2)
	}
}

func run(args []string, stdout io.Writer) error {
	options, command, action, actionArgs, err := parseArgs(args)
	if err != nil {
		return err
	}
	client, err := openapi.NewHTTPClient(options.endpoint, options.token)
	if err != nil {
		return errors.New("invalid endpoint or bearer token")
	}
	ctx := context.Background()
	var value any
	switch command + " " + action {
	case "tenant get":
		value, err = client.GetPlatformTenant(ctx, options.tenant, options.requestID)
	case "organization get":
		value, err = client.GetOrganization(ctx, options.tenant, options.organization, options.requestID)
	case "project get":
		value, err = client.GetProject(ctx, options.tenant, options.project, options.requestID)
	case "project create":
		var flags struct {
			name           string
			organizationID string
			displayName    string
		}
		if err = parseActionFlags("project create", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&flags.name, "name", "", "project identifier")
			set.StringVar(&flags.organizationID, "organization-id", "", "organization identifier")
			set.StringVar(&flags.displayName, "display-name", "", "project display name")
		}); err == nil {
			value, err = client.CreateProject(ctx, options.tenant, options.requestID, options.idempotencyKey, platform.ProjectCreateRequest{
				Name: flags.name, DisplayName: flags.displayName,
				OrganizationRef: common.OrganizationRef{Namespace: "cloud-agents", Kind: "organization", ID: flags.organizationID},
			})
		}
	case "session create":
		var provider string
		if err = parseActionFlags("session create", actionArgs, func(set *flag.FlagSet) { set.StringVar(&provider, "provider", "", "provider kind") }); err == nil {
			value, err = client.CreateManagedAgentSession(ctx, options.tenant, options.project, options.requestID, options.idempotencyKey, openapi.ManagedAgentSessionCreateRequest{SessionID: options.session, ProviderKind: provider})
		}
	case "session get":
		value, err = client.GetManagedAgentSession(ctx, options.tenant, options.project, options.session, options.requestID)
	case "session close":
		value, err = client.CloseManagedAgentSession(ctx, options.tenant, options.project, options.session, options.requestID, options.idempotencyKey)
	case "turn create":
		var inputText string
		if err = parseActionFlags("turn create", actionArgs, func(set *flag.FlagSet) { set.StringVar(&inputText, "input", "", "turn input text") }); err == nil {
			value, err = client.CreateManagedAgentTurn(ctx, options.tenant, options.project, options.session, options.requestID, options.idempotencyKey, openapi.ManagedAgentTurnCreateRequest{TurnID: options.turn, InputText: inputText})
		}
	case "turn get":
		value, err = client.GetManagedAgentTurn(ctx, options.tenant, options.project, options.session, options.turn, options.requestID)
	case "execution execute":
		var model, inputText string
		if err = parseActionFlags("execution execute", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&model, "model", "", "model identifier")
			set.StringVar(&inputText, "input", "", "turn input text")
		}); err == nil {
			value, err = client.ExecuteManagedAgent(ctx, options.tenant, options.project, options.session, options.requestID, options.idempotencyKey, openapi.ManagedAgentExecutionCreateRequest{TurnID: options.turn, ExecutionID: options.execution, Model: model, InputText: inputText})
		}
	case "execution get":
		value, err = client.GetManagedAgentExecution(ctx, options.tenant, options.project, options.session, options.turn, options.execution, options.requestID)
	case "execution cancel", "execution interrupt":
		var generation uint64
		if err = parseActionFlags("execution "+action, actionArgs, func(set *flag.FlagSet) {
			set.Uint64Var(&generation, "generation", 0, "execution fencing generation")
		}); err == nil && generation == 0 {
			err = errors.New("--generation must be greater than zero")
		} else if err == nil {
			if action == "interrupt" {
				value, err = client.InterruptManagedAgentExecution(ctx, options.tenant, options.project, options.session, options.turn, options.execution, options.requestID, options.idempotencyKey, openapi.ManagedAgentExecutionInterruptRequest{Generation: generation})
			} else {
				value, err = client.CancelManagedAgentExecution(ctx, options.tenant, options.project, options.session, options.turn, options.execution, options.requestID, options.idempotencyKey, openapi.ManagedAgentExecutionCancelRequest{Generation: generation})
			}
		}
	case "events list":
		var cursor string
		limit := 0
		if err = parseActionFlags("events list", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&cursor, "cursor", "", "opaque event cursor")
			set.IntVar(&limit, "limit", 0, "maximum events to return")
		}); err == nil {
			value, err = client.ListManagedAgentEvents(ctx, options.tenant, options.project, options.session, options.requestID, cursor, limit)
		}
	case "membership get":
		value, err = client.GetMembership(ctx, options.tenant, options.membership, options.requestID)
	case "role get":
		value, err = client.GetRole(ctx, options.tenant, options.role, options.requestID)
	case "role-binding get":
		value, err = client.GetRoleBinding(ctx, options.tenant, options.roleBinding, options.requestID)
	case "managed-host-project get":
		value, err = client.GetProjectContext(ctx, options.tenant, options.project, options.requestID)
	case "managed-host-role-binding get":
		value, err = client.GetManagedHostRoleBinding(ctx, options.tenant, options.roleBinding, options.requestID)
	default:
		return fmt.Errorf("unknown command %q; use cloud-agentsctl help", command+" "+action)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(responseValue(value))
}

func parseArgs(args []string) (globalOptions, string, string, []string, error) {
	set := flag.NewFlagSet("cloud-agentsctl", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var options globalOptions
	set.StringVar(&options.endpoint, "endpoint", "", "Control Plane URL")
	set.StringVar(&options.token, "token", "", "bearer token")
	set.StringVar(&options.tenant, "tenant", "", "tenant identifier")
	set.StringVar(&options.organization, "organization", "", "organization identifier")
	set.StringVar(&options.project, "project", "", "project identifier")
	set.StringVar(&options.membership, "membership", "", "membership identifier")
	set.StringVar(&options.role, "role", "", "role identifier")
	set.StringVar(&options.roleBinding, "role-binding", "", "role binding identifier")
	set.StringVar(&options.session, "session", "", "session identifier")
	set.StringVar(&options.turn, "turn", "", "turn identifier")
	set.StringVar(&options.execution, "execution", "", "execution identifier")
	set.StringVar(&options.requestID, "request-id", "", "request identifier")
	set.StringVar(&options.idempotencyKey, "idempotency-key", "", "idempotency key")
	if err := set.Parse(args); err != nil {
		return globalOptions{}, "", "", nil, err
	}
	remaining := set.Args()
	if len(remaining) < 2 {
		return globalOptions{}, "", "", nil, errors.New(usage)
	}
	for name, value := range map[string]string{"endpoint": options.endpoint, "token": options.token, "tenant": options.tenant, "request-id": options.requestID} {
		if strings.TrimSpace(value) != value || value == "" {
			return globalOptions{}, "", "", nil, fmt.Errorf("--%s is required", name)
		}
	}
	command, action := remaining[0], remaining[1]
	if !knownCommand(command, action) {
		return globalOptions{}, "", "", nil, errors.New(usage)
	}
	if requiresProject(command, action) && options.project == "" {
		return globalOptions{}, "", "", nil, errors.New("--project is required")
	}
	if requiresOrganization(command, action) && options.organization == "" {
		return globalOptions{}, "", "", nil, errors.New("--organization is required")
	}
	if requiresMembership(command, action) && options.membership == "" {
		return globalOptions{}, "", "", nil, errors.New("--membership is required")
	}
	if requiresRole(command, action) && options.role == "" {
		return globalOptions{}, "", "", nil, errors.New("--role is required")
	}
	if requiresRoleBinding(command, action) && options.roleBinding == "" {
		return globalOptions{}, "", "", nil, errors.New("--role-binding is required")
	}
	if requiresSession(command, action) && options.session == "" {
		return globalOptions{}, "", "", nil, errors.New("--session is required")
	}
	if requiresTurn(command, action) && options.turn == "" {
		return globalOptions{}, "", "", nil, errors.New("--turn is required")
	}
	if requiresExecution(command, action) && options.execution == "" {
		return globalOptions{}, "", "", nil, errors.New("--execution is required")
	}
	if requiresIdempotency(command, action) && options.idempotencyKey == "" {
		return globalOptions{}, "", "", nil, errors.New("--idempotency-key is required")
	}
	return options, command, action, remaining[2:], nil
}

func parseActionFlags(name string, args []string, define func(*flag.FlagSet)) error {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	define(set)
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("%s does not accept positional arguments", name)
	}
	return nil
}

func responseValue(value any) any {
	switch result := value.(type) {
	case openapi.ProjectResult:
		return result.Value
	case openapi.ManagedAgentSessionResult:
		return result.Value
	case openapi.ManagedAgentTurnResult:
		return result.Value
	case openapi.ManagedAgentExecutionResult:
		return result.Value
	case openapi.ManagedAgentEventPageResult:
		return result.Value
	case openapi.TenantResult:
		return result.Value
	case openapi.OrganizationResult:
		return result.Value
	case openapi.MembershipResult:
		return result.Value
	case openapi.RoleResult:
		return result.Value
	case openapi.RoleBindingResult:
		return result.Value
	default:
		return value
	}
}

func knownCommand(command, action string) bool {
	switch command + " " + action {
	case "tenant get", "organization get", "project get", "project create", "session create", "session get", "session close", "turn create", "turn get", "execution execute", "execution get", "execution cancel", "execution interrupt", "events list", "membership get", "role get", "role-binding get", "managed-host-project get", "managed-host-role-binding get":
		return true
	default:
		return false
	}
}

func requiresProject(command, action string) bool {
	return command == "project" && action == "get" || command == "session" || command == "turn" || command == "execution" || command == "events" || command == "managed-host-project"
}
func requiresOrganization(command, action string) bool { return command == "organization" }
func requiresMembership(command, action string) bool   { return command == "membership" }
func requiresRole(command, action string) bool         { return command == "role" }
func requiresRoleBinding(command, action string) bool {
	return command == "role-binding" || command == "managed-host-role-binding"
}
func requiresSession(command, action string) bool {
	return command == "session" || command == "turn" || command == "execution" || command == "events"
}
func requiresTurn(command, action string) bool      { return command == "turn" || command == "execution" }
func requiresExecution(command, action string) bool { return command == "execution" }
func requiresIdempotency(command, action string) bool {
	return (command == "project" && action == "create") || (command == "session" && (action == "create" || action == "close")) || (command == "turn" && action == "create") || (command == "execution" && (action == "execute" || action == "cancel" || action == "interrupt"))
}

const usage = `usage: cloud-agentsctl --endpoint URL --token TOKEN --tenant ID --request-id ID <resource> <action> [flags]`
