package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

var ErrManagedAgentEventsNotFound = errors.New("managed agent event session was not found")

const appendManagedAgentEventSQL = `SELECT cloud_agents.append_managed_agent_event_v1(
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

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
    'occurred_at', event.occurred_at
) ORDER BY event.event_sequence), '[]'::jsonb)
FROM (
    SELECT event_uid, event_sequence, operation, resource, turn_uid,
        execution_uid, generation, mutation_digest, input_digest,
        result_digest, error_code, changes, occurred_at
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
					if event.EventID == "" || event.Sequence == 0 || event.OccurredAt.IsZero() || event.MutationDigest == "" || len(event.Changes) == 0 {
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
	OccurredAt     time.Time                                   `json:"occurred_at"`
}

func (row managedAgentEventRow) snapshot(scope internalmanagedagent.Scope) internalmanagedagent.LifecycleEvent {
	changes := make([]internalmanagedagent.LifecycleStateChange, 0, len(row.Changes))
	for _, change := range row.Changes {
		change.Resource = internalEventResourceKind(string(change.Resource))
		changes = append(changes, change)
	}
	return internalmanagedagent.LifecycleEvent{EventID: row.EventID, Sequence: row.Sequence, Scope: scope, Operation: row.Operation, Resource: internalEventResourceKind(row.Resource), TurnID: row.TurnID, ExecutionID: row.ExecutionID, Generation: row.Generation, OccurredAt: row.OccurredAt, MutationDigest: row.MutationDigest, InputDigest: row.InputDigest, ResultDigest: row.ResultDigest, ErrorCode: row.ErrorCode, Changes: changes}
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
	default:
		return ""
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
