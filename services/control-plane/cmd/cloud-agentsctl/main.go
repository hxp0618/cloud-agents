package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
)

type globalOptions struct {
	endpoint       string
	caFile         string
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
	sandbox        string
	grant          string
	ptySession     string
	lease          string
	target         string
	enrollment     string
	requestID      string
	idempotencyKey string
	timeout        time.Duration
}

const (
	maxBearerTokenFileBytes  = 16 << 10
	maxCAFileBytes           = 1 << 20
	defaultRequestTimeout    = 6 * time.Minute
	defaultEventPollInterval = time.Second
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cloud-agentsctl:", err)
		os.Exit(2)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 1 && args[0] == "--version" {
		_, err := fmt.Fprintf(stdout, "cloud-agentsctl %s\n", version)
		return err
	}
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		_, err := fmt.Fprintln(stdout, help)
		return err
	}
	options, command, action, actionArgs, err := parseArgs(args)
	if err != nil {
		return err
	}
	localTargetPreflight := command == "target" && action == "preflight"
	var client *openapi.Client
	if !actionHelpRequested(actionArgs) && !localTargetPreflight {
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
		client, err = newHTTPClient(options, token)
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.timeout)
	defer cancel()
	var value any
	switch command + " " + action {
	case "target preflight":
		var kind, socketPath string
		if err = parseActionFlags("target preflight", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&kind, "kind", "", "target kind (docker)")
			set.StringVar(&socketPath, "socket", "", "absolute Docker Engine socket path")
		}); err == nil && kind != "docker" {
			err = errors.New("--kind must be docker")
		} else if err == nil {
			value, err = dockertarget.ProbeUnixSocket(ctx, socketPath)
		}
	case "target register":
		var targetName, kind, targetEndpoint, credentialRef string
		if err = parseActionFlags("target register", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&targetName, "target-name", "", "deployment target name")
			set.StringVar(&kind, "kind", "", "target kind (docker, kubernetes, or ssh)")
			set.StringVar(&targetEndpoint, "target-endpoint", "", "target HTTPS or SSH endpoint")
			set.StringVar(&credentialRef, "credential-ref", "", "deployment-owned credential reference")
		}); err == nil {
			value, err = client.RegisterDeploymentTarget(ctx, options.tenant, options.project, options.requestID, options.idempotencyKey, platform.DeploymentTargetRegisterRequest{TargetID: options.target, TargetName: targetName, TargetKind: kind, Endpoint: targetEndpoint, CredentialRef: credentialRef})
		}
	case "target get":
		if err = parseActionFlags("target get", actionArgs, nil); err == nil {
			value, err = client.GetDeploymentTarget(ctx, options.tenant, options.project, options.target, options.requestID)
		}
	case "target probe":
		var generation int64
		if err = parseActionFlags("target probe", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&generation, "expected-generation", 0, "deployment target fencing generation")
		}); err == nil && generation < 1 {
			err = errors.New("--expected-generation must be greater than zero")
		} else if err == nil {
			value, err = client.ProbeDeploymentTarget(ctx, options.tenant, options.project, options.target, options.requestID, options.idempotencyKey, platform.DeploymentTargetProbeRequest{ExpectedGeneration: generation})
		}
	case "target cleanup":
		var generation int64
		if err = parseActionFlags("target cleanup", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&generation, "expected-generation", 0, "deployment target fencing generation")
		}); err == nil && generation < 1 {
			err = errors.New("--expected-generation must be greater than zero")
		} else if err == nil {
			value, err = client.CleanupDeploymentTarget(ctx, options.tenant, options.project, options.target, options.requestID, options.idempotencyKey, platform.DeploymentTargetProbeRequest{ExpectedGeneration: generation})
		}
	case "remote-worker-enrollment claim-secret":
		var expectedResourceVersion int64
		if err = parseActionFlags("remote-worker-enrollment claim-secret", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&expectedResourceVersion, "expected-resource-version", 0, "enrollment resource version")
		}); err == nil && expectedResourceVersion < 1 {
			err = errors.New("--expected-resource-version must be greater than zero")
		} else if err == nil {
			value, err = client.ClaimRemoteWorkerEnrollmentSecret(ctx, options.tenant, options.project, options.enrollment, options.requestID, options.idempotencyKey, platform.RemoteWorkerEnrollmentSecretClaimRequest{ExpectedResourceVersion: strconv.FormatInt(expectedResourceVersion, 10), ConfirmedEnrollmentID: options.enrollment})
		}
	case "tenant get":
		if err = parseActionFlags("tenant get", actionArgs, nil); err == nil {
			value, err = client.GetPlatformTenant(ctx, options.tenant, options.requestID)
		}
	case "organization get":
		if err = parseActionFlags("organization get", actionArgs, nil); err == nil {
			value, err = client.GetOrganization(ctx, options.tenant, options.organization, options.requestID)
		}
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
		if err = parseActionFlags("project get", actionArgs, nil); err == nil {
			value, err = client.GetProject(ctx, options.tenant, options.project, options.requestID)
		}
	case "project list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("project list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum projects to return")
			set.StringVar(&pageToken, "page-token", "", "opaque project page token")
		}); err == nil {
			value, err = client.ListProjects(ctx, options.tenant, options.organization, options.requestID, pageSize, pageToken)
		}
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
			value, err = client.CreateManagedAgentSession(ctx, options.tenant, options.project, options.requestID, options.idempotencyKey, openapi.ManagedAgentSessionCreateRequest{SessionID: options.session, ProviderKind: provider, EnvironmentLeaseID: options.lease})
		}
	case "sandbox exec":
		var generation, timeoutSeconds int64
		var command string
		if err = parseActionFlags("sandbox exec", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&generation, "expected-generation", 0, "Sandbox fencing generation")
			set.StringVar(&command, "command", "", "foreground shell command")
			set.Int64Var(&timeoutSeconds, "timeout-seconds", 30, "command timeout from 1 to 60 seconds")
		}); err == nil && generation < 1 {
			err = errors.New("--expected-generation must be greater than zero")
		} else if err == nil && command == "" {
			err = errors.New("--command is required")
		} else if err == nil && (timeoutSeconds < 1 || timeoutSeconds > 60) {
			err = errors.New("--timeout-seconds must be between 1 and 60")
		} else if err == nil {
			value, err = client.ExecSandbox(ctx, options.tenant, options.project, options.sandbox, options.requestID,
				platform.SandboxExecRequest{ExpectedGeneration: generation, Command: command, TimeoutSeconds: timeoutSeconds})
		}
	case "sandbox grant":
		var generation, ttlSeconds int64
		if err = parseActionFlags("sandbox grant", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&generation, "expected-generation", 0, "Sandbox fencing generation")
			set.Int64Var(&ttlSeconds, "ttl-seconds", 300, "Grant lifetime from 60 to 900 seconds")
		}); err == nil && generation < 1 {
			err = errors.New("--expected-generation must be greater than zero")
		} else if err == nil && (ttlSeconds < 60 || ttlSeconds > 900) {
			err = errors.New("--ttl-seconds must be between 60 and 900")
		} else if err == nil {
			value, err = client.CreateSandboxAccessGrant(ctx, options.tenant, options.project, options.sandbox,
				options.requestID, options.idempotencyKey, platform.SandboxAccessGrantCreateRequest{
					ExpectedGeneration: generation, TTLSeconds: ttlSeconds,
				})
		}
	case "pty create":
		if err = parseActionFlags("pty create", actionArgs, nil); err == nil {
			value, err = client.CreatePTYSession(ctx, options.tenant, options.project, options.grant, options.requestID)
		}
	case "pty get":
		if err = parseActionFlags("pty get", actionArgs, nil); err == nil {
			value, err = client.GetPTYSession(ctx, options.tenant, options.project, options.grant, options.ptySession, options.requestID)
		}
	case "pty delete":
		if err = parseActionFlags("pty delete", actionArgs, nil); err == nil {
			err = client.DeletePTYSession(ctx, options.tenant, options.project, options.grant, options.ptySession, options.requestID)
			value = map[string]string{"status": "deleted"}
		}
	case "pty attach":
		var since int64
		var takeover bool
		if err = parseActionFlags("pty attach", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&since, "since", 0, "absolute replay byte offset")
			set.BoolVar(&takeover, "takeover", false, "replace the current read/write holder")
		}); err == nil && since < 0 {
			err = errors.New("--since must not be negative")
		} else if err == nil {
			return attachPTY(ctx, options, client, since, takeover, os.Stdin, stdout)
		}
	case "files list":
		path := "."
		if err = parseActionFlags("files list", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&path, "path", ".", "Workspace-relative directory path")
		}); err == nil {
			value, err = client.ListSandboxFiles(ctx, options.tenant, options.project, options.grant, options.requestID, path)
		}
	case "files read":
		var path, fileVersion string
		var offset int64
		limit := 1 << 20
		if err = parseActionFlags("files read", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&path, "path", "", "Workspace-relative file path")
			set.Int64Var(&offset, "offset", 0, "byte offset")
			set.IntVar(&limit, "limit", 1<<20, "maximum bytes to return")
			set.StringVar(&fileVersion, "file-version", "", "version returned by the first page")
		}); err == nil && path == "" {
			err = errors.New("--path is required")
		} else if err == nil {
			value, err = client.ReadSandboxFile(ctx, options.tenant, options.project, options.grant, options.requestID, path, offset, limit, fileVersion)
		}
	case "files write":
		var path, content string
		if err = parseActionFlags("files write", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&path, "path", "", "Workspace-relative file path")
			set.StringVar(&content, "content-base64url", "", "unpadded base64url file content")
		}); err == nil && path == "" {
			err = errors.New("--path is required")
		} else if err == nil {
			value, err = client.WriteSandboxFile(ctx, options.tenant, options.project, options.grant, options.requestID, platform.SandboxFileWriteRequest{Path: path, ContentBase64URL: content})
		}
	case "files delete":
		var path string
		if err = parseActionFlags("files delete", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&path, "path", "", "Workspace-relative file path")
		}); err == nil && path == "" {
			err = errors.New("--path is required")
		} else if err == nil {
			err = client.DeleteSandboxFile(ctx, options.tenant, options.project, options.grant, options.requestID, path)
			value = map[string]string{"status": "deleted"}
		}
	case "preview register", "preview revoke":
		var port int
		if err = parseActionFlags("preview "+action, actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&port, "port", 0, "private Sandbox HTTP port")
		}); err == nil && (port < 1024 || port > 65535 || port == 44772) {
			err = errors.New("--port must be between 1024 and 65535 and cannot be 44772")
		} else if err == nil && action == "register" {
			value, err = client.RegisterSandboxPreviewPort(ctx, options.tenant, options.project, options.grant, options.requestID, int32(port))
		} else if err == nil {
			err = client.RevokeSandboxPreviewPort(ctx, options.tenant, options.project, options.grant, options.requestID, int32(port))
			value = map[string]string{"status": "revoked"}
		}
	case "session list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("session list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum sessions to return")
			set.StringVar(&pageToken, "page-token", "", "opaque session page token")
		}); err == nil {
			value, err = client.ListManagedAgentSessions(ctx, options.tenant, options.project, options.requestID, pageSize, pageToken)
		}
	case "session get":
		if err = parseActionFlags("session get", actionArgs, nil); err == nil {
			value, err = client.GetManagedAgentSession(ctx, options.tenant, options.project, options.session, options.requestID)
		}
	case "session close":
		if err = parseActionFlags("session close", actionArgs, nil); err == nil {
			value, err = client.CloseManagedAgentSession(ctx, options.tenant, options.project, options.session, options.requestID, options.idempotencyKey)
		}
	case "turn create":
		var inputText string
		if err = parseActionFlags("turn create", actionArgs, func(set *flag.FlagSet) { set.StringVar(&inputText, "input", "", "turn input text") }); err == nil {
			value, err = client.CreateManagedAgentTurn(ctx, options.tenant, options.project, options.session, options.requestID, options.idempotencyKey, openapi.ManagedAgentTurnCreateRequest{TurnID: options.turn, InputText: inputText})
		}
	case "turn list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("turn list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum turns to return")
			set.StringVar(&pageToken, "page-token", "", "opaque turn page token")
		}); err == nil {
			value, err = client.ListManagedAgentTurns(ctx, options.tenant, options.project, options.session, options.requestID, pageSize, pageToken)
		}
	case "turn get":
		if err = parseActionFlags("turn get", actionArgs, nil); err == nil {
			value, err = client.GetManagedAgentTurn(ctx, options.tenant, options.project, options.session, options.turn, options.requestID)
		}
	case "execution list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("execution list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum executions to return")
			set.StringVar(&pageToken, "page-token", "", "opaque execution page token")
		}); err == nil {
			value, err = client.ListManagedAgentExecutions(ctx, options.tenant, options.project, options.session, options.requestID, pageSize, pageToken)
		}
	case "execution execute":
		var model, runtimeMode, interactionMode, inputText string
		if err = parseActionFlags("execution execute", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&model, "model", "", "model identifier")
			set.StringVar(&runtimeMode, "runtime-mode", "", "runtime permission mode: approval-required or full-access")
			set.StringVar(&interactionMode, "interaction-mode", "", "interaction mode: default or plan")
			set.StringVar(&inputText, "input", "", "turn input text")
		}); err == nil {
			value, err = client.ExecuteManagedAgent(ctx, options.tenant, options.project, options.session, options.requestID, options.idempotencyKey, openapi.ManagedAgentExecutionCreateRequest{TurnID: options.turn, ExecutionID: options.execution, Model: model, RuntimeMode: runtimeMode, InteractionMode: interactionMode, InputText: inputText})
		}
	case "execution get":
		if err = parseActionFlags("execution get", actionArgs, nil); err == nil {
			value, err = client.GetManagedAgentExecution(ctx, options.tenant, options.project, options.session, options.turn, options.execution, options.requestID)
		}
	case "execution download-artifact":
		messageIndex := -1
		if err = parseActionFlags("execution download-artifact", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&messageIndex, "message-index", -1, "ArtifactCandidate message index")
		}); err == nil && (messageIndex < 0 || messageIndex >= 64) {
			err = errors.New("--message-index must be between 0 and 63")
		} else if err == nil {
			value, err = client.DownloadManagedAgentArtifact(ctx, options.tenant, options.project, options.session, options.turn, options.execution, options.requestID, messageIndex)
		}
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
	case "execution resolve-approval":
		var generation uint64
		var interactionRequest, decision string
		if err = parseActionFlags("execution resolve-approval", actionArgs, func(set *flag.FlagSet) {
			set.Uint64Var(&generation, "generation", 0, "execution fencing generation")
			set.StringVar(&interactionRequest, "interaction-request", "", "pending interaction request identifier")
			set.StringVar(&decision, "decision", "", "approval decision: accept or decline")
		}); err == nil && generation == 0 {
			err = errors.New("--generation must be greater than zero")
		} else if err == nil {
			err = client.ResolveManagedAgentApproval(ctx, options.tenant, options.project, options.session, options.turn, options.execution, options.requestID, openapi.ManagedAgentApprovalResolutionRequest{Generation: generation, RequestID: interactionRequest, Decision: decision})
			value = map[string]bool{"resolved": err == nil}
		}
	case "execution resolve-user-input":
		var generation uint64
		var interactionRequest, answersJSON string
		if err = parseActionFlags("execution resolve-user-input", actionArgs, func(set *flag.FlagSet) {
			set.Uint64Var(&generation, "generation", 0, "execution fencing generation")
			set.StringVar(&interactionRequest, "interaction-request", "", "pending interaction request identifier")
			set.StringVar(&answersJSON, "answers-json", "", "JSON object mapping question identifiers to string arrays")
		}); err == nil && generation == 0 {
			err = errors.New("--generation must be greater than zero")
		} else if err == nil {
			var answers map[string][]string
			if err = json.Unmarshal([]byte(answersJSON), &answers); err == nil {
				err = client.ResolveManagedAgentUserInput(ctx, options.tenant, options.project, options.session, options.turn, options.execution, options.requestID, openapi.ManagedAgentUserInputResolutionRequest{Generation: generation, RequestID: interactionRequest, Answers: answers})
				value = map[string]bool{"resolved": err == nil}
			} else {
				err = errors.New("--answers-json must be a JSON object mapping question identifiers to string arrays")
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
	case "events watch":
		var cursor string
		limit := 64
		pollInterval := defaultEventPollInterval
		var untilTerminal bool
		if err = parseActionFlags("events watch", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&cursor, "cursor", "", "opaque event cursor")
			set.IntVar(&limit, "limit", 64, "maximum events to return per request")
			set.DurationVar(&pollInterval, "poll-interval", defaultEventPollInterval, "delay after an empty event page")
			set.BoolVar(&untilTerminal, "until-terminal", false, "stop after the selected execution reaches a terminal state")
		}); err == nil && (limit < 1 || limit > 64) {
			err = errors.New("--limit must be between 1 and 64")
		} else if err == nil && pollInterval <= 0 {
			err = errors.New("--poll-interval must be greater than zero")
		} else if err == nil && untilTerminal && options.execution == "" {
			err = errors.New("--execution is required with --until-terminal")
		} else if err == nil {
			terminalExecutionID := ""
			if untilTerminal {
				terminalExecutionID = options.execution
			}
			return watchManagedAgentEvents(ctx, client, stdout, options, cursor, limit, pollInterval, terminalExecutionID)
		}
	case "membership get":
		if err = parseActionFlags("membership get", actionArgs, nil); err == nil {
			value, err = client.GetMembership(ctx, options.tenant, options.membership, options.requestID)
		}
	case "membership list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("membership list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum memberships to return")
			set.StringVar(&pageToken, "page-token", "", "opaque membership page token")
		}); err == nil {
			value, err = client.ListMemberships(ctx, options.tenant, options.requestID, pageSize, pageToken)
		}
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
	case "membership resume", "membership suspend", "membership revoke":
		var flags rbacTransitionFlags
		if err = parseActionFlags("membership "+action, actionArgs, defineRBACTransitionFlags(&flags)); err == nil {
			body := platform.MembershipTransitionRequest{
				ExpectedTenantRevision:  flags.expectedTenantRevision,
				ExpectedResourceVersion: flags.expectedResourceVersion,
				AuditFactUID:            flags.auditFactUID,
				ReasonCode:              flags.reasonCode,
			}
			if action == "resume" {
				value, err = client.ResumeMembership(ctx, options.tenant, options.membership, options.requestID, body)
			} else if action == "suspend" {
				value, err = client.SuspendMembership(ctx, options.tenant, options.membership, options.requestID, body)
			} else {
				value, err = client.RevokeMembership(ctx, options.tenant, options.membership, options.requestID, body)
			}
		}
	case "role get":
		if err = parseActionFlags("role get", actionArgs, nil); err == nil {
			value, err = client.GetRole(ctx, options.tenant, options.role, options.requestID)
		}
	case "role list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("role list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum roles to return")
			set.StringVar(&pageToken, "page-token", "", "opaque role page token")
		}); err == nil {
			value, err = client.ListRoles(ctx, options.tenant, options.requestID, pageSize, pageToken)
		}
	case "role-binding get":
		if err = parseActionFlags("role-binding get", actionArgs, nil); err == nil {
			value, err = client.GetRoleBinding(ctx, options.tenant, options.roleBinding, options.requestID)
		}
	case "role-binding list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("role-binding list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum role bindings to return")
			set.StringVar(&pageToken, "page-token", "", "opaque role binding page token")
		}); err == nil {
			value, err = client.ListRoleBindings(ctx, options.tenant, options.requestID, pageSize, pageToken)
		}
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
		if err = parseActionFlags("managed-host-project get", actionArgs, nil); err == nil {
			value, err = client.GetProjectContext(ctx, options.tenant, options.project, options.requestID)
		}
	case "managed-host-role-binding get":
		if err = parseActionFlags("managed-host-role-binding get", actionArgs, nil); err == nil {
			value, err = client.GetManagedHostRoleBinding(ctx, options.tenant, options.roleBinding, options.requestID)
		}
	case "environment-lease list":
		var pageSize int
		var pageToken string
		if err = parseActionFlags("environment-lease list", actionArgs, func(set *flag.FlagSet) {
			set.IntVar(&pageSize, "page-size", 0, "maximum environment leases to return")
			set.StringVar(&pageToken, "page-token", "", "opaque environment lease page token")
		}); err == nil {
			value, err = client.ListManagedHostEnvironmentLeases(ctx, options.tenant, options.project, options.requestID, pageSize, pageToken)
		}
	case "environment-lease create":
		var flags struct {
			name                     string
			releaseDigest            string
			providerCredentialRef    string
			expectedTargetGeneration int64
			cpuLimitMillis           int64
			memoryLimitBytes         int64
			ttlSeconds               int64
		}
		if err = parseActionFlags("environment-lease create", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&flags.name, "name", "", "lease name")
			set.StringVar(&flags.releaseDigest, "release-digest", "", "release artifact digest")
			set.StringVar(&flags.providerCredentialRef, "provider-credential-ref", "", "target-side Provider credential reference")
			set.Int64Var(&flags.expectedTargetGeneration, "expected-target-generation", 0, "deployment target fencing generation")
			set.Int64Var(&flags.cpuLimitMillis, "cpu-limit-millis", 0, "Worker CPU limit in millicores")
			set.Int64Var(&flags.memoryLimitBytes, "memory-limit-bytes", 0, "Worker memory limit in bytes")
			set.Int64Var(&flags.ttlSeconds, "ttl-seconds", 0, "lease lifetime in seconds")
		}); err == nil && flags.expectedTargetGeneration <= 0 {
			err = errors.New("--expected-target-generation must be greater than zero")
		} else if err == nil {
			value, err = client.CreateManagedHostEnvironmentLease(ctx, options.tenant, options.project, options.requestID, options.idempotencyKey, platform.EnvironmentLeaseCreateRequest{
				LeaseID: options.lease, LeaseName: flags.name, ReleaseDigest: flags.releaseDigest, TargetID: options.target,
				ExpectedTargetGeneration: flags.expectedTargetGeneration, ProviderCredentialRef: flags.providerCredentialRef,
				CPULimitMillis: flags.cpuLimitMillis, MemoryLimitBytes: flags.memoryLimitBytes, TTLSeconds: flags.ttlSeconds,
			})
		}
	case "environment-lease get":
		if err = parseActionFlags("environment-lease get", actionArgs, nil); err == nil {
			value, err = client.GetManagedHostEnvironmentLease(ctx, options.tenant, options.project, options.lease, options.requestID)
		}
	case "environment-lease terminate":
		var generation int64
		if err = parseActionFlags("environment-lease terminate", actionArgs, func(set *flag.FlagSet) {
			set.Int64Var(&generation, "generation", 0, "lease fencing generation")
		}); err == nil && generation <= 0 {
			err = errors.New("--generation must be greater than zero")
		} else if err == nil {
			value, err = client.TerminateManagedHostEnvironmentLease(ctx, options.tenant, options.project, options.lease, options.requestID, options.idempotencyKey, platform.EnvironmentLeaseTerminateRequest{ExpectedGeneration: generation})
		}
	case "environment-lease upgrade":
		var releaseDigest string
		var generation int64
		if err = parseActionFlags("environment-lease upgrade", actionArgs, func(set *flag.FlagSet) {
			set.StringVar(&releaseDigest, "release-digest", "", "new release artifact digest")
			set.Int64Var(&generation, "generation", 0, "lease fencing generation")
		}); err == nil && generation <= 0 {
			err = errors.New("--generation must be greater than zero")
		} else if err == nil {
			value, err = client.UpgradeManagedHostEnvironmentLease(ctx, options.tenant, options.project, options.lease, options.requestID, options.idempotencyKey, platform.EnvironmentLeaseUpgradeRequest{ReleaseDigest: releaseDigest, ExpectedGeneration: generation})
		}
	default:
		return fmt.Errorf("unknown command %q; use cloud-agentsctl help", command+" "+action)
	}
	var actionHelp actionHelpOutput
	if errors.As(err, &actionHelp) {
		_, writeErr := io.WriteString(stdout, string(actionHelp))
		return writeErr
	}
	if err != nil {
		return err
	}
	if artifact, ok := value.(openapi.ManagedAgentArtifactResult); ok {
		_, err = stdout.Write(artifact.Data)
		return err
	}
	return json.NewEncoder(stdout).Encode(responseValue(value))
}

func newHTTPClient(options globalOptions, token string) (*openapi.Client, error) {
	if options.caFile == "" {
		client, err := openapi.NewHTTPClient(options.endpoint, token)
		if err != nil {
			return nil, errors.New("invalid endpoint or bearer token")
		}
		return client, nil
	}
	file, err := os.Open(options.caFile)
	if err != nil {
		return nil, errors.New("cannot read CA file")
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, maxCAFileBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(contents) > maxCAFileBytes {
		return nil, errors.New("cannot read CA file")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(contents) {
		return nil, errors.New("CA file contains no certificates")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	client, err := openapi.NewHTTPClientWithClient(options.endpoint, token, &http.Client{Transport: transport})
	if err != nil {
		return nil, errors.New("invalid endpoint or bearer token")
	}
	return client, nil
}

func parseArgs(args []string) (globalOptions, string, string, []string, error) {
	set := flag.NewFlagSet("cloud-agentsctl", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var options globalOptions
	set.StringVar(&options.endpoint, "endpoint", "", "Control Plane URL")
	set.StringVar(&options.caFile, "ca-file", "", "PEM CA bundle for the Control Plane")
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
	set.StringVar(&options.sandbox, "sandbox", "", "Sandbox identifier")
	set.StringVar(&options.grant, "grant", "", "Sandbox access Grant identifier")
	set.StringVar(&options.ptySession, "pty-session", "", "PTY session identifier")
	set.StringVar(&options.lease, "lease", "", "environment lease identifier")
	set.StringVar(&options.target, "target", "", "deployment target identifier")
	set.StringVar(&options.enrollment, "enrollment", "", "RemoteWorker enrollment identifier")
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
	command, action := remaining[0], remaining[1]
	if !knownCommand(command, action) {
		return globalOptions{}, "", "", nil, errors.New(usage)
	}
	actionArgs := remaining[2:]
	if actionHelpRequested(actionArgs) {
		return options, command, action, actionArgs, nil
	}
	if options.timeout <= 0 {
		return globalOptions{}, "", "", nil, errors.New("--timeout must be greater than zero")
	}
	if command == "target" && action == "preflight" {
		return options, command, action, actionArgs, nil
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
	if strings.TrimSpace(options.caFile) != options.caFile {
		return globalOptions{}, "", "", nil, errors.New("CA file input is invalid")
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
	if requiresSandbox(command, action) && options.sandbox == "" {
		return globalOptions{}, "", "", nil, errors.New("--sandbox is required")
	}
	if requiresGrant(command, action) && options.grant == "" {
		return globalOptions{}, "", "", nil, errors.New("--grant is required")
	}
	if requiresPTYSession(command, action) && options.ptySession == "" {
		return globalOptions{}, "", "", nil, errors.New("--pty-session is required")
	}
	if requiresLease(command, action) && options.lease == "" {
		return globalOptions{}, "", "", nil, errors.New("--lease is required")
	}
	if requiresTarget(command, action) && options.target == "" {
		return globalOptions{}, "", "", nil, errors.New("--target is required")
	}
	if command == "remote-worker-enrollment" && options.enrollment == "" {
		return globalOptions{}, "", "", nil, errors.New("--enrollment is required")
	}
	if requiresIdempotency(command, action) && options.idempotencyKey == "" {
		return globalOptions{}, "", "", nil, errors.New("--idempotency-key is required")
	}
	return options, command, action, actionArgs, nil
}

func actionHelpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help")
}

type actionHelpOutput string

func (output actionHelpOutput) Error() string { return string(output) }

func parseActionFlags(name string, args []string, define func(*flag.FlagSet)) error {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	if define != nil {
		define(set)
	}
	if actionHelpRequested(args) {
		var output strings.Builder
		_, _ = fmt.Fprintf(&output, "usage: cloud-agentsctl [global flags] %s [flags]\n", name)
		set.SetOutput(&output)
		set.PrintDefaults()
		return actionHelpOutput(output.String())
	}
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
	case openapi.ProjectPageResult:
		return result.Value
	case openapi.ManagedAgentSessionResult:
		return result.Value
	case openapi.ManagedAgentSessionPageResult:
		return result.Value
	case openapi.ManagedAgentTurnResult:
		return result.Value
	case openapi.ManagedAgentTurnPageResult:
		return result.Value
	case openapi.ManagedAgentExecutionResult:
		return result.Value
	case openapi.ManagedAgentExecutionPageResult:
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
	case openapi.MembershipPageResult:
		return result.Value
	case openapi.RoleResult:
		return result.Value
	case openapi.RolePageResult:
		return result.Value
	case openapi.RoleBindingResult:
		return result.Value
	case openapi.RoleBindingPageResult:
		return result.Value
	case openapi.EnvironmentLeaseResult:
		return result.Value
	case openapi.EnvironmentLeasePageResult:
		return result.Value
	case openapi.DeploymentTargetResult:
		return result.Value
	case openapi.SandboxExecResult:
		return result.Value
	case openapi.SandboxAccessGrantResult:
		return result.Value
	case openapi.SandboxPTYSessionResult:
		return result.Value
	case openapi.SandboxPreviewPortResult:
		return result.Value
	case openapi.RBACMutationResult:
		return result.Value
	default:
		return value
	}
}

func watchManagedAgentEvents(ctx context.Context, client *openapi.Client, stdout io.Writer, options globalOptions, cursor string, limit int, pollInterval time.Duration, terminalExecutionID string) error {
	encoder := json.NewEncoder(stdout)
	for {
		result, err := client.ListManagedAgentEvents(ctx, options.tenant, options.project, options.session, options.requestID, cursor, limit)
		if err != nil {
			return err
		}
		page := result.Value
		if len(page.Events) > 0 && (page.NextCursor == "" || page.NextCursor == cursor) {
			return errors.New("event watch cursor did not advance")
		}
		if page.HasMore && len(page.Events) == 0 {
			return errors.New("event watch returned an empty continuation page")
		}
		for _, event := range page.Events {
			if err := encoder.Encode(event); err != nil {
				return err
			}
			if terminalExecutionID != "" && event.Spec.ExecutionID == terminalExecutionID {
				switch event.Spec.Operation {
				case "execution.complete", "execution.fail", "turn.interrupt", "turn.cancel":
					return nil
				}
			}
		}
		if page.NextCursor != "" {
			cursor = page.NextCursor
		}
		if page.HasMore {
			continue
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func knownCommand(command, action string) bool {
	switch command + " " + action {
	case "target preflight", "target register", "target get", "target probe", "target cleanup", "remote-worker-enrollment claim-secret", "tenant get", "organization get", "organization list", "organization create", "project get", "project list", "project create", "sandbox exec", "sandbox grant", "pty create", "pty get", "pty delete", "pty attach", "files list", "files read", "files write", "files delete", "preview register", "preview revoke", "session create", "session list", "session get", "session close", "turn create", "turn list", "turn get", "execution list", "execution execute", "execution get", "execution download-artifact", "execution cancel", "execution interrupt", "execution resolve-approval", "execution resolve-user-input", "events list", "events watch", "membership get", "membership list", "membership create", "membership resume", "membership suspend", "membership revoke", "role get", "role list", "role-binding get", "role-binding list", "role-binding create", "role-binding revoke", "managed-host-project get", "managed-host-role-binding get", "environment-lease list", "environment-lease create", "environment-lease get", "environment-lease terminate", "environment-lease upgrade":
		return true
	default:
		return false
	}
}

func requiresProject(command, action string) bool {
	return command == "target" && action != "preflight" || command == "remote-worker-enrollment" || command == "project" && action == "get" || command == "sandbox" || command == "pty" || command == "files" || command == "preview" || command == "session" || command == "turn" || command == "execution" || command == "events" || command == "managed-host-project" || command == "environment-lease"
}
func requiresOrganization(command, action string) bool {
	return command == "organization" && action != "list" || command == "project" && action == "list"
}
func requiresMembership(command, action string) bool {
	return command == "membership" && action != "list"
}
func requiresRole(command, action string) bool { return command == "role" && action == "get" }
func requiresRoleBinding(command, action string) bool {
	return command == "role-binding" && action != "list" || command == "managed-host-role-binding"
}
func requiresSession(command, action string) bool {
	return command == "session" && action != "list" || command == "turn" || command == "execution" || command == "events"
}
func requiresTurn(command, action string) bool {
	return command == "turn" && action != "list" || command == "execution" && action != "list"
}
func requiresExecution(command, action string) bool {
	return command == "execution" && action != "list"
}
func requiresSandbox(command, action string) bool { return command == "sandbox" }
func requiresGrant(command, action string) bool {
	return command == "pty" || command == "files" || command == "preview"
}
func requiresPTYSession(command, action string) bool {
	return command == "pty" && action != "create"
}
func requiresLease(command, action string) bool {
	return command == "environment-lease" && action != "list" || command == "session" && action == "create"
}
func requiresTarget(command, action string) bool {
	return command == "target" && action != "preflight" || command == "environment-lease" && action == "create"
}
func requiresIdempotency(command, action string) bool {
	return command == "remote-worker-enrollment" || (command == "target" && (action == "register" || action == "probe" || action == "cleanup")) || (command == "project" && action == "create") || (command == "sandbox" && action == "grant") || (command == "session" && (action == "create" || action == "close")) || (command == "turn" && action == "create") || (command == "execution" && (action == "execute" || action == "cancel" || action == "interrupt")) || (command == "environment-lease" && (action == "create" || action == "terminate" || action == "upgrade"))
}

const usage = `usage: cloud-agentsctl --endpoint URL [--ca-file PATH] (--token TOKEN | --token-file PATH) --tenant ID --request-id ID <resource> <action> [flags]
	   cloud-agentsctl [--timeout DURATION] target preflight --kind docker --socket /absolute/path/to/docker.sock
       cloud-agentsctl --version`

const help = usage + `

resources and actions:
  target preflight|register|get|probe|cleanup
  remote-worker-enrollment claim-secret
  tenant get
  organization get|list|create
  project get|list|create

  sandbox exec|grant
  pty create|get|attach|delete
  files list|read|write|delete
  preview register|revoke
  session get|list|create|close
  turn get|list|create
  execution get|list|execute|download-artifact|cancel|interrupt|resolve-approval|resolve-user-input
  events list|watch
  membership get|list|create|resume|suspend|revoke
  role get|list
  role-binding get|list|create|revoke
  managed-host-project get
  managed-host-role-binding get
  environment-lease get|list|create|upgrade|terminate`

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
