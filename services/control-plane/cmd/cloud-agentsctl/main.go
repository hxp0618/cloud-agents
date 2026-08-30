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
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
)

type globalOptions struct {
	endpoint       string
	token          string
	tokenFile      string
	tenant         string
	organization   string
	project        string
	membership     string
	role           string
	roleBinding    string
	session        string
	turn           string
	execution      string
	lease          string
	requestID      string
	idempotencyKey string
	timeout        time.Duration
}

const (
	maxBearerTokenFileBytes = 16 << 10
	defaultRequestTimeout   = 6 * time.Minute
)

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
	token := options.token
	if options.tokenFile != "" {
		file, openErr := os.Open(options.tokenFile)
		if openErr != nil {
			return errors.New("cannot read bearer token file")
		}
		contents, readErr := io.ReadAll(io.LimitReader(file, maxBearerTokenFileBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(contents) > maxBearerTokenFileBytes {
			return errors.New("cannot read bearer token file")
		}
		token = strings.TrimSuffix(strings.TrimSuffix(string(contents), "\n"), "\r")
	}
	client, err := openapi.NewHTTPClient(options.endpoint, token)
	if err != nil {
		return errors.New("invalid endpoint or bearer token")
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.timeout)
	defer cancel()
	var value any
	switch command + " " + action {
	case "tenant get":
		value, err = client.GetPlatformTenant(ctx, options.tenant, options.requestID)
	case "organization get":
		value, err = client.GetOrganization(ctx, options.tenant, options.organization, options.requestID)
	case "organization list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("organization list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum organizations to return")
			set.StringVar(&pageToken, "page-token", "", "opaque organization page token")
		}); err == nil {
			value, err = client.ListOrganizations(ctx, options.tenant, options.requestID, pageSize, pageToken)
		}
	case "organization create":
		var flags struct {
			expectedTenantRevision int64
			name                   string
			displayName            string
			auditFactUID           string
			reasonCode             string
		}
		if err = parseActionFlags("organization create", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&flags.expectedTenantRevision, "expected-tenant-revision", 0, "expected tenant resource revision")
			set.StringVar(&flags.name, "name", "", "organization name")
			set.StringVar(&flags.displayName, "display-name", "", "organization display name")
			set.StringVar(&flags.auditFactUID, "audit-fact-uid", "", "audit fact identifier")
			set.StringVar(&flags.reasonCode, "reason-code", "", "mutation reason code")
		}); err == nil {
			value, err = client.CreateOrganization(ctx, options.tenant, options.requestID, platform.OrganizationCreateRequest{
				ExpectedTenantRevision: flags.expectedTenantRevision,
				OrganizationID:         options.organization,
				Name:                   flags.name,
				DisplayName:            flags.displayName,
				AuditFactUID:           flags.auditFactUID,
				ReasonCode:             flags.reasonCode,
			})
		}
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
	case "membership create":
		var flags membershipCreateFlags
		if err = parseActionFlags("membership create", actionArgs, defineMembershipCreateFlags(&flags)); err == nil {
			var scope common.AuthorizationScope
			scope, err = cliAuthorizationScope(options.tenant, flags.scopeLevel, flags.scopeID)
			if err == nil {
				value, err = client.CreateMembership(ctx, options.tenant, options.requestID, platform.MembershipCreateRequest{
					ExpectedTenantRevision: flags.expectedTenantRevision,
					MembershipID:           options.membership,
					MembershipName:         flags.name,
					Subject:                common.SubjectRef{Kind: flags.subjectKind, Issuer: flags.subjectIssuer, Subject: flags.subject},
					Scope:                  scope,
					ExpiresAt:              flags.expiresAt,
					AuditFactUID:           flags.auditFactUID,
					ReasonCode:             flags.reasonCode,
				})
			}
		}
	case "membership suspend", "membership revoke":
		var flags rbacTransitionFlags
		if err = parseActionFlags("membership "+action, actionArgs, defineRBACTransitionFlags(&flags)); err == nil {
			body := platform.MembershipTransitionRequest{
				ExpectedTenantRevision:  flags.expectedTenantRevision,
				ExpectedResourceVersion: flags.expectedResourceVersion,
				AuditFactUID:            flags.auditFactUID,
				ReasonCode:              flags.reasonCode,
			}
			if action == "suspend" {
				value, err = client.SuspendMembership(ctx, options.tenant, options.membership, options.requestID, body)
			} else {
				value, err = client.RevokeMembership(ctx, options.tenant, options.membership, options.requestID, body)
			}
		}
	case "role get":
		value, err = client.GetRole(ctx, options.tenant, options.role, options.requestID)
	case "role-binding get":
		value, err = client.GetRoleBinding(ctx, options.tenant, options.roleBinding, options.requestID)
	case "role-binding create":
		var flags roleBindingCreateFlags
		if err = parseActionFlags("role-binding create", actionArgs, defineRoleBindingCreateFlags(&flags)); err == nil {
			var scope common.AuthorizationScope
			scope, err = cliAuthorizationScope(options.tenant, flags.scopeLevel, flags.scopeID)
			if err == nil {
				value, err = client.BindRole(ctx, options.tenant, options.requestID, platform.RoleBindingCreateRequest{
					ExpectedTenantRevision: flags.expectedTenantRevision,
					RoleBindingID:          options.roleBinding,
					RoleBindingName:        flags.name,
					Subject:                common.SubjectRef{Kind: flags.subjectKind, Issuer: flags.subjectIssuer, Subject: flags.subject},
					RoleName:               flags.roleName,
					RoleVersion:            flags.roleVersion,
					Scope:                  scope,
					ExpiresAt:              flags.expiresAt,
					AuditFactUID:           flags.auditFactUID,
					ReasonCode:             flags.reasonCode,
				})
			}
		}
	case "role-binding revoke":
		var flags rbacTransitionFlags
		if err = parseActionFlags("role-binding revoke", actionArgs, defineRBACTransitionFlags(&flags)); err == nil {
			value, err = client.RevokeRoleBinding(ctx, options.tenant, options.roleBinding, options.requestID, platform.RoleBindingRevokeRequest{
				ExpectedTenantRevision:  flags.expectedTenantRevision,
				ExpectedResourceVersion: flags.expectedResourceVersion,
				AuditFactUID:            flags.auditFactUID,
				ReasonCode:              flags.reasonCode,
			})
		}
	case "managed-host-project get":
		value, err = client.GetProjectContext(ctx, options.tenant, options.project, options.requestID)
	case "managed-host-role-binding get":
		value, err = client.GetManagedHostRoleBinding(ctx, options.tenant, options.roleBinding, options.requestID)
	case "environment-lease create":
		var flags struct {
			name          string
			releaseDigest string
			ttlSeconds    int64
		}
		if err = parseActionFlags("environment-lease create", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&flags.name, "name", "", "lease name")
			set.StringVar(&flags.releaseDigest, "release-digest", "", "release artifact digest")
			set.Int64Var(&flags.ttlSeconds, "ttl-seconds", 0, "lease lifetime in seconds")
		}); err == nil {
			value, err = client.CreateManagedHostEnvironmentLease(ctx, options.tenant, options.project, options.requestID, options.idempotencyKey, platform.EnvironmentLeaseCreateRequest{
				LeaseID: options.lease, LeaseName: flags.name, ReleaseDigest: flags.releaseDigest, TTLSeconds: flags.ttlSeconds,
			})
		}
	case "environment-lease get":
		value, err = client.GetManagedHostEnvironmentLease(ctx, options.tenant, options.project, options.lease, options.requestID)
	case "environment-lease terminate":
		var generation int64
		if err = parseActionFlags("environment-lease terminate", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&generation, "generation", 0, "lease fencing generation")
		}); err == nil && generation <= 0 {
			err = errors.New("--generation must be greater than zero")
		} else if err == nil {
			value, err = client.TerminateManagedHostEnvironmentLease(ctx, options.tenant, options.project, options.lease, options.requestID, options.idempotencyKey, platform.EnvironmentLeaseTerminateRequest{ExpectedGeneration: generation})
		}
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
	set.StringVar(&options.tokenFile, "token-file", "", "file containing the bearer token")
	set.StringVar(&options.tenant, "tenant", "", "tenant identifier")
	set.StringVar(&options.organization, "organization", "", "organization identifier")
	set.StringVar(&options.project, "project", "", "project identifier")
	set.StringVar(&options.membership, "membership", "", "membership identifier")
	set.StringVar(&options.role, "role", "", "role identifier")
	set.StringVar(&options.roleBinding, "role-binding", "", "role binding identifier")
	set.StringVar(&options.session, "session", "", "session identifier")
	set.StringVar(&options.turn, "turn", "", "turn identifier")
	set.StringVar(&options.execution, "execution", "", "execution identifier")
	set.StringVar(&options.lease, "lease", "", "environment lease identifier")
	set.StringVar(&options.requestID, "request-id", "", "request identifier")
	set.StringVar(&options.idempotencyKey, "idempotency-key", "", "idempotency key")
	set.DurationVar(&options.timeout, "timeout", defaultRequestTimeout, "request timeout")
	if err := set.Parse(args); err != nil {
		return globalOptions{}, "", "", nil, err
	}
	remaining := set.Args()
	if len(remaining) < 2 {
		return globalOptions{}, "", "", nil, errors.New(usage)
	}
	for name, value := range map[string]string{"endpoint": options.endpoint, "tenant": options.tenant, "request-id": options.requestID} {
		if strings.TrimSpace(value) != value || value == "" {
			return globalOptions{}, "", "", nil, fmt.Errorf("--%s is required", name)
		}
	}
	if options.token == "" && options.tokenFile == "" {
		return globalOptions{}, "", "", nil, errors.New("--token or --token-file is required")
	}
	if options.token != "" && options.tokenFile != "" {
		return globalOptions{}, "", "", nil, errors.New("--token and --token-file are mutually exclusive")
	}
	if strings.TrimSpace(options.token) != options.token || strings.TrimSpace(options.tokenFile) != options.tokenFile {
		return globalOptions{}, "", "", nil, errors.New("bearer token input is invalid")
	}
	if options.timeout <= 0 {
		return globalOptions{}, "", "", nil, errors.New("--timeout must be greater than zero")
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
	if requiresLease(command, action) && options.lease == "" {
		return globalOptions{}, "", "", nil, errors.New("--lease is required")
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
	case openapi.OrganizationPageResult:
		return result.Value
	case openapi.MembershipResult:
		return result.Value
	case openapi.RoleResult:
		return result.Value
	case openapi.RoleBindingResult:
		return result.Value
	case openapi.EnvironmentLeaseResult:
		return result.Value
	case openapi.RBACMutationResult:
		return result.Value
	default:
		return value
	}
}

func knownCommand(command, action string) bool {
	switch command + " " + action {
	case "tenant get", "organization get", "organization list", "organization create", "project get", "project create", "session create", "session get", "session close", "turn create", "turn get", "execution execute", "execution get", "execution cancel", "execution interrupt", "events list", "membership get", "membership create", "membership suspend", "membership revoke", "role get", "role-binding get", "role-binding create", "role-binding revoke", "managed-host-project get", "managed-host-role-binding get", "environment-lease create", "environment-lease get", "environment-lease terminate":
		return true
	default:
		return false
	}
}

func requiresProject(command, action string) bool {
	return command == "project" && action == "get" || command == "session" || command == "turn" || command == "execution" || command == "events" || command == "managed-host-project" || command == "environment-lease"
}
func requiresOrganization(command, action string) bool {
	return command == "organization" && action != "list"
}
func requiresMembership(command, action string) bool { return command == "membership" }
func requiresRole(command, action string) bool       { return command == "role" }
func requiresRoleBinding(command, action string) bool {
	return command == "role-binding" || command == "managed-host-role-binding"
}
func requiresSession(command, action string) bool {
	return command == "session" || command == "turn" || command == "execution" || command == "events"
}
func requiresTurn(command, action string) bool      { return command == "turn" || command == "execution" }
func requiresExecution(command, action string) bool { return command == "execution" }
func requiresLease(command, action string) bool     { return command == "environment-lease" }
func requiresIdempotency(command, action string) bool {
	return (command == "project" && action == "create") || (command == "session" && (action == "create" || action == "close")) || (command == "turn" && action == "create") || (command == "execution" && (action == "execute" || action == "cancel" || action == "interrupt")) || (command == "environment-lease" && (action == "create" || action == "terminate"))
}

const usage = `usage: cloud-agentsctl --endpoint URL (--token TOKEN | --token-file PATH) --tenant ID --request-id ID <resource> <action> [flags]`

type membershipCreateFlags struct {
	expectedTenantRevision int64
	name                   string
	subjectKind            string
	subjectIssuer          string
	subject                string
	scopeLevel             string
	scopeID                string
	expiresAt              string
	auditFactUID           string
	reasonCode             string
}

func defineMembershipCreateFlags(flags *membershipCreateFlags) func(*flag.FlagSet) {
	return func(set *flag.FlagSet) {
		set.Int64Var(&flags.expectedTenantRevision, "expected-tenant-revision", 0, "expected tenant resource revision")
		set.StringVar(&flags.name, "name", "", "membership name")
		set.StringVar(&flags.subjectKind, "subject-kind", "", "subject kind")
		set.StringVar(&flags.subjectIssuer, "subject-issuer", "", "subject issuer URL")
		set.StringVar(&flags.subject, "subject", "", "subject identifier")
		set.StringVar(&flags.scopeLevel, "scope-level", "", "authorization scope level")
		set.StringVar(&flags.scopeID, "scope-id", "", "authorization scope identifier")
		set.StringVar(&flags.expiresAt, "expires-at", "", "optional RFC3339 expiration")
		set.StringVar(&flags.auditFactUID, "audit-fact-uid", "", "audit fact identifier")
		set.StringVar(&flags.reasonCode, "reason-code", "", "mutation reason code")
	}
}

type roleBindingCreateFlags struct {
	expectedTenantRevision int64
	name                   string
	subjectKind            string
	subjectIssuer          string
	subject                string
	roleName               string
	roleVersion            int64
	scopeLevel             string
	scopeID                string
	expiresAt              string
	auditFactUID           string
	reasonCode             string
}

func defineRoleBindingCreateFlags(flags *roleBindingCreateFlags) func(*flag.FlagSet) {
	return func(set *flag.FlagSet) {
		set.Int64Var(&flags.expectedTenantRevision, "expected-tenant-revision", 0, "expected tenant resource revision")
		set.StringVar(&flags.name, "name", "", "role binding name")
		set.StringVar(&flags.subjectKind, "subject-kind", "", "subject kind")
		set.StringVar(&flags.subjectIssuer, "subject-issuer", "", "subject issuer URL")
		set.StringVar(&flags.subject, "subject", "", "subject identifier")
		set.StringVar(&flags.roleName, "role-name", "", "role name")
		set.Int64Var(&flags.roleVersion, "role-version", 0, "role version")
		set.StringVar(&flags.scopeLevel, "scope-level", "", "authorization scope level")
		set.StringVar(&flags.scopeID, "scope-id", "", "authorization scope identifier")
		set.StringVar(&flags.expiresAt, "expires-at", "", "optional RFC3339 expiration")
		set.StringVar(&flags.auditFactUID, "audit-fact-uid", "", "audit fact identifier")
		set.StringVar(&flags.reasonCode, "reason-code", "", "mutation reason code")
	}
}

type rbacTransitionFlags struct {
	expectedTenantRevision  int64
	expectedResourceVersion int64
	auditFactUID            string
	reasonCode              string
}

func defineRBACTransitionFlags(flags *rbacTransitionFlags) func(*flag.FlagSet) {
	return func(set *flag.FlagSet) {
		set.Int64Var(&flags.expectedTenantRevision, "expected-tenant-revision", 0, "expected tenant resource revision")
		set.Int64Var(&flags.expectedResourceVersion, "expected-resource-version", 0, "expected resource version")
		set.StringVar(&flags.auditFactUID, "audit-fact-uid", "", "audit fact identifier")
		set.StringVar(&flags.reasonCode, "reason-code", "", "mutation reason code")
	}
}

func cliAuthorizationScope(tenantID, level, id string) (common.AuthorizationScope, error) {
	if strings.TrimSpace(level) != level || strings.TrimSpace(id) != id || level == "" {
		return common.AuthorizationScope{}, errors.New("--scope-level and --scope-id are required")
	}
	if level == "tenant" && id == "" {
		id = tenantID
	}
	if id == "" {
		return common.AuthorizationScope{}, errors.New("--scope-id is required")
	}
	var reference any
	switch level {
	case "tenant":
		reference = common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: id}
	case "organization":
		reference = common.OrganizationRef{Namespace: "cloud-agents", Kind: "organization", ID: id}
	case "project":
		reference = common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: id}
	default:
		return common.AuthorizationScope{}, errors.New("--scope-level must be tenant, organization, or project")
	}
	raw, err := json.Marshal(reference)
	if err != nil {
		return common.AuthorizationScope{}, errors.New("authorization scope cannot be encoded")
	}
	message := json.RawMessage(raw)
	return common.AuthorizationScope{Level: level, Ref: &message}, nil
}
