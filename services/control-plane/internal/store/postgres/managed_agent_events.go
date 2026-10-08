package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

var ErrManagedAgentEventsNotFound = errors.New("managed agent event session was not found")

const appendManagedAgentEventSQL = `SELECT cloud_agents.append_managed_agent_event_v1(
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

const appendManagedAgentCapabilityEventSQL = `SELECT cloud_agents.append_managed_agent_capability_event_v1(
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

type managedAgentEventInput struct {
	Scope          internalmanagedagent.Scope
	SessionID      string
	Operation      string
	Resource       internalmanagedagent.ResourceKind
	TurnID         string
	ExecutionID    string
	Generation     uint64
	MutationDigest string
	InputDigest    string
	ResultDigest   string
	ErrorCode      string
	Changes        []internalmanagedagent.LifecycleStateChange
}

func appendManagedAgentEvent(ctx context.Context, transaction tenantTransaction, input managedAgentEventInput) error {
	primaryResource := durableEventResourceName(input.Resource)
	if ctx == nil || transaction == nil || input.Scope.ValidateForAPI() != nil || input.SessionID == "" || input.Operation == "" || primaryResource == "" || input.MutationDigest == "" || len(input.Changes) == 0 || len(input.Changes) > 4 || input.Generation > math.MaxInt64 {
		return ErrCoordinationInvalidInput
	}
	changes := make([]managedAgentEventChange, 0, len(input.Changes))
	for _, change := range input.Changes {
		resource := durableEventResourceName(change.Resource)
		if resource == "" || change.From == "" && change.To == "" || change.Version == 0 || change.Version > math.MaxInt64 {
			return ErrCoordinationInvalidInput
		}
		changes = append(changes, managedAgentEventChange{Resource: resource, From: change.From, To: change.To, Version: change.Version})
	}
	encodedChanges, err := json.Marshal(changes)
	if err != nil {
		return ErrCoordinationInvalidInput
	}
	var eventID string
	if err := transaction.queryRow(ctx, appendManagedAgentEventSQL, input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.Operation, primaryResource, nullableString(input.TurnID), nullableString(input.ExecutionID), int64(input.Generation), input.MutationDigest, nullableString(input.InputDigest), nullableString(input.ResultDigest), nullableString(input.ErrorCode), encodedChanges).Scan(&eventID); err != nil {
		return mapMutationDatabaseError("managed agent event", err)
	}
	if eventID == "" {
		return ErrCoordinationResultDrift
	}
	return nil
}

// RecordManagedAgentCapabilityEvent appends one redacted MCP/Skill fact to
// the existing per-session durable event stream. Payloads, source bytes,
// endpoint material, and credentials never enter this projection.
func (service *DurableCoordinationService) RecordManagedAgentCapabilityEvent(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CapabilityEventInput,
) error {
	if service == nil || service.runner == nil || ctx == nil || input.Scope.TenantID != tenantID ||
		input.Scope.ValidateForAPI() != nil || input.SessionID == "" || input.Generation == 0 ||
		!validMutationIdentifier(input.ResourceID) || !validMutationIdentifier(input.Version) ||
		!validCoordinationDigest(input.Digest) || !validCoordinationDigest(input.MutationDigest) ||
		input.InputDigest != "" && !validCoordinationDigest(input.InputDigest) ||
		input.ResultDigest != "" && !validCoordinationDigest(input.ResultDigest) {
		return ErrCoordinationInvalidInput
	}
	if input.Resource != internalmanagedagent.ResourceMcpServer && input.Resource != internalmanagedagent.ResourceSkillBundle {
		return ErrCoordinationInvalidInput
	}
	if input.Operation != "mcp.call" && input.Operation != "mcp.fail" && input.Operation != "mcp.revoke" && input.Operation != "skill.load" && input.Operation != "skill.fail" && input.Operation != "skill.revoke" {
		return ErrCoordinationInvalidInput
	}
	if input.Resource == internalmanagedagent.ResourceMcpServer && !strings.HasPrefix(input.Operation, "mcp.") || input.Resource == internalmanagedagent.ResourceSkillBundle && !strings.HasPrefix(input.Operation, "skill.") {
		return ErrCoordinationInvalidInput
	}
	if input.Result != "accepted" && input.Result != "succeeded" && input.Result != "failed" && input.Result != "revoked" {
		return ErrCoordinationInvalidInput
	}
	if (input.Operation == "mcp.revoke" || input.Operation == "skill.revoke") && input.Result != "revoked" ||
		(input.Operation == "mcp.fail" || input.Operation == "skill.fail") && input.Result != "failed" {
		return ErrCoordinationInvalidInput
	}
	var eventID string
	err := withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, appendManagedAgentCapabilityEventSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.Operation,
			durableEventResourceName(input.Resource), nullableString(input.TurnID), nullableString(input.ExecutionID),
			int64(input.Generation), input.MutationDigest, nullableString(input.InputDigest), nullableString(input.ResultDigest), nullableString(input.ErrorCode),
			input.ResourceID, input.Version, input.Digest, input.Result).Scan(&eventID)
	})
	if err != nil {
		return err
	}
	if eventID == "" {
		return ErrCoordinationResultDrift
	}
	return nil
}

const (
	managedAgentEventSessionExistsSQL = `SELECT 1
FROM cloud_agents.managed_agent_sessions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2`
	listManagedAgentEventsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
    'event_uid', event.event_uid, 'event_sequence', event.event_sequence,
    'operation', event.operation, 'resource', event.resource,
    'turn_uid', event.turn_uid, 'execution_uid', event.execution_uid,
    'generation', event.generation, 'mutation_digest', event.mutation_digest,
    'input_digest', event.input_digest, 'result_digest', event.result_digest,
    'error_code', event.error_code, 'changes', event.changes,
    'capability_server_uid', event.capability_server_uid, 'capability_bundle_uid', event.capability_bundle_uid,
    'capability_version', event.capability_version, 'capability_digest', event.capability_digest,
    'capability_result', event.capability_result,
    'occurred_at', event.occurred_at
) ORDER BY event.event_sequence), '[]'::jsonb)
FROM (
    SELECT event_uid, event_sequence, operation, resource, turn_uid,
        execution_uid, generation, mutation_digest, input_digest,
        result_digest, error_code, changes, capability_server_uid,
        capability_bundle_uid, capability_version, capability_digest, capability_result, occurred_at
    FROM cloud_agents.managed_agent_events
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1 AND session_uid = $2 AND event_sequence > $3
    ORDER BY event_sequence
    LIMIT $4
) AS event`
	managedAgentEventCursorIdentitySQL = `SELECT event_uid
FROM cloud_agents.managed_agent_events
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND event_sequence = $3`
)

// GetManagedAgentEvents reads the durable lifecycle stream for one session.
// The cursor is checked against the exact tenant/project/session event row
// before it is used as a lower bound.
func (service *DurableCoordinationService) GetManagedAgentEvents(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	after internalmanagedagent.EventCursor,
	limit int,
) (internalmanagedagent.EventPage, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.EventPage{}, ErrNilCoordinationRunner
	}
	scope := internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}
	if ctx == nil || scope.ValidateForAPI() != nil || sessionID == "" || limit < 1 || limit > 64 {
		return internalmanagedagent.EventPage{}, ErrCoordinationInvalidInput
	}
	if after != (internalmanagedagent.EventCursor{}) && after.Scope != scope {
		return internalmanagedagent.EventPage{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.EventPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(scope.TenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: scope.ProjectID}, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, scope.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: scope.ProjectID}, func() error {
				var exists int
				if err := handle.transaction.queryRow(readContext, managedAgentEventSessionExistsSQL, projectID, sessionID).Scan(&exists); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return ErrManagedAgentEventsNotFound
					}
					return mapMutationDatabaseError("managed agent event session", err)
				}
				if after != (internalmanagedagent.EventCursor{}) {
					var eventID string
					if err := handle.transaction.queryRow(readContext, managedAgentEventCursorIdentitySQL, projectID, sessionID, after.Sequence).Scan(&eventID); err != nil {
						return fmt.Errorf("%w: event cursor", ErrCoordinationInvalidInput)
					}
					if eventID != after.EventID {
						return fmt.Errorf("%w: event cursor identity", ErrCoordinationInvalidInput)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listManagedAgentEventsSQL, projectID, sessionID, after.Sequence, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("managed agent events", err)
				}
				var rows []managedAgentEventRow
				if err := json.Unmarshal(raw, &rows); err != nil {
					return ErrCoordinationResultDrift
				}
				result.Events = make([]internalmanagedagent.LifecycleEvent, 0, limit)
				for index, row := range rows {
					if index == limit {
						result.HasMore = true
						break
					}
					event := row.snapshot(scope)
					if event.EventID == "" || event.Sequence == 0 || event.OccurredAt.IsZero() || event.MutationDigest == "" || len(event.Changes) == 0 ||
						!validManagedAgentEventCapability(event) {
						return ErrCoordinationResultDrift
					}
					result.Events = append(result.Events, event)
				}
				result.NextCursor = after
				if len(result.Events) > 0 {
					last := result.Events[len(result.Events)-1]
					result.NextCursor = internalmanagedagent.EventCursor{Scope: scope, Sequence: last.Sequence, EventID: last.EventID, ProfileID: internalmanagedagent.ManagedAgentLifecycleEventProfile().ID, ProfileDigest: internalmanagedagent.ManagedAgentLifecycleEventProfile().Digest}
				}
				return nil
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func validManagedAgentEventCapability(event internalmanagedagent.LifecycleEvent) bool {
	switch event.Resource {
	case internalmanagedagent.ResourceMcpServer:
		return strings.HasPrefix(event.Operation, "mcp.") && event.ServerID != "" && event.BundleID == "" && validMutationIdentifier(event.ServerID) && validMutationIdentifier(event.Version) && validCoordinationDigest(event.Digest) && validCapabilityEventResult(event)
	case internalmanagedagent.ResourceSkillBundle:
		return strings.HasPrefix(event.Operation, "skill.") && event.BundleID != "" && event.ServerID == "" && validMutationIdentifier(event.BundleID) && validMutationIdentifier(event.Version) && validCoordinationDigest(event.Digest) && validCapabilityEventResult(event)
	case internalmanagedagent.ResourceSession, internalmanagedagent.ResourceTurn, internalmanagedagent.ResourceExecution:
		return event.ServerID == "" && event.BundleID == "" && event.Version == "" && event.Digest == "" && event.Result == ""
	default:
		return false
	}
}

func validCapabilityEventResult(event internalmanagedagent.LifecycleEvent) bool {
	if event.Result != "accepted" && event.Result != "succeeded" && event.Result != "failed" && event.Result != "revoked" {
		return false
	}
	if (event.Operation == "mcp.revoke" || event.Operation == "skill.revoke") && event.Result != "revoked" {
		return false
	}
	return (event.Operation != "mcp.fail" && event.Operation != "skill.fail") || event.Result == "failed"
}

type managedAgentEventRow struct {
	EventID        string                                      `json:"event_uid"`
	Sequence       uint64                                      `json:"event_sequence"`
	Operation      string                                      `json:"operation"`
	Resource       string                                      `json:"resource"`
	TurnID         string                                      `json:"turn_uid"`
	ExecutionID    string                                      `json:"execution_uid"`
	Generation     uint64                                      `json:"generation"`
	MutationDigest string                                      `json:"mutation_digest"`
	InputDigest    string                                      `json:"input_digest"`
	ResultDigest   string                                      `json:"result_digest"`
	ErrorCode      string                                      `json:"error_code"`
	Changes        []internalmanagedagent.LifecycleStateChange `json:"changes"`
	ServerID       string                                      `json:"capability_server_uid"`
	BundleID       string                                      `json:"capability_bundle_uid"`
	Version        string                                      `json:"capability_version"`
	Digest         string                                      `json:"capability_digest"`
	Result         string                                      `json:"capability_result"`
	OccurredAt     time.Time                                   `json:"occurred_at"`
}

func (row managedAgentEventRow) snapshot(scope internalmanagedagent.Scope) internalmanagedagent.LifecycleEvent {
	changes := make([]internalmanagedagent.LifecycleStateChange, 0, len(row.Changes))
	for _, change := range row.Changes {
		change.Resource = internalEventResourceKind(string(change.Resource))
		changes = append(changes, change)
	}
	return internalmanagedagent.LifecycleEvent{EventID: row.EventID, Sequence: row.Sequence, Scope: scope, Operation: row.Operation, Resource: internalEventResourceKind(row.Resource), TurnID: row.TurnID, ExecutionID: row.ExecutionID, Generation: row.Generation, OccurredAt: row.OccurredAt, MutationDigest: row.MutationDigest, InputDigest: row.InputDigest, ResultDigest: row.ResultDigest, ErrorCode: row.ErrorCode, ServerID: row.ServerID, BundleID: row.BundleID, Version: row.Version, Digest: row.Digest, Result: row.Result, Changes: changes}
}

type managedAgentEventChange struct {
	Resource string `json:"resource"`
	From     string `json:"from"`
	To       string `json:"to"`
	Version  uint64 `json:"version"`
}

func durableEventResourceName(resource internalmanagedagent.ResourceKind) string {
	switch resource {
	case internalmanagedagent.ResourceSession:
		return "Session"
	case internalmanagedagent.ResourceTurn:
		return "Turn"
	case internalmanagedagent.ResourceExecution:
		return "Execution"
	case internalmanagedagent.ResourceMcpServer:
		return "McpServer"
	case internalmanagedagent.ResourceSkillBundle:
		return "SkillBundle"
	default:
		return ""
	}
}

func internalEventResourceKind(resource string) internalmanagedagent.ResourceKind {
	switch resource {
	case "Session":
		return internalmanagedagent.ResourceSession
	case "Turn":
		return internalmanagedagent.ResourceTurn
	case "Execution":
		return internalmanagedagent.ResourceExecution
	case "McpServer":
		return internalmanagedagent.ResourceMcpServer
	case "SkillBundle":
		return internalmanagedagent.ResourceSkillBundle
	default:
		return ""
	}
}
