package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalnetworkpolicy "github.com/hxp0618/cloud-agents/services/control-plane/internal/networkpolicy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type NetworkPolicyPage struct {
	NetworkPolicies     []internalnetworkpolicy.Snapshot
	NextNetworkPolicyID string
}

type NetworkPolicyAuditPage struct {
	Events         []internalnetworkpolicy.AuditEvent
	NextOccurredAt *time.Time
	NextEventID    string
}

type networkPolicyPageRow struct {
	TenantID           string    `json:"tenant_id"`
	ProjectID          string    `json:"project_uid"`
	PolicyID           string    `json:"policy_uid"`
	PolicyName         string    `json:"policy_name"`
	UserSummary        string    `json:"user_summary"`
	DefaultEgress      string    `json:"default_egress"`
	AllowlistPolicyRef string    `json:"allowlist_policy_ref"`
	IngressEnabled     bool      `json:"ingress_enabled"`
	PreviewEnabled     bool      `json:"preview_enabled"`
	DNSPolicyRef       string    `json:"dns_policy_ref"`
	ProxyPolicyRef     string    `json:"proxy_policy_ref"`
	ResourceVersion    int64     `json:"resource_version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type networkPolicyAuditRow struct {
	TenantID              string    `json:"tenant_id"`
	ProjectID             string    `json:"project_uid"`
	PolicyID              string    `json:"policy_uid"`
	EventID               string    `json:"event_uid"`
	OperationID           string    `json:"operation_uid"`
	Actor                 string    `json:"subject_digest"`
	Action                string    `json:"action"`
	PolicyResourceVersion int64     `json:"policy_resource_version"`
	Result                string    `json:"result"`
	RequestID             string    `json:"request_id"`
	OccurredAt            time.Time `json:"occurred_at"`
}

const networkPolicyColumns = `policy_uid, policy_name, user_summary, default_egress,
    COALESCE(allowlist_policy_ref, ''), ingress_enabled, preview_enabled,
    COALESCE(dns_policy_ref, ''), COALESCE(proxy_policy_ref, ''), resource_version, created_at, updated_at`

var (
	setNetworkPolicySQL = `SELECT ` + networkPolicyColumns + `
FROM cloud_agents.set_network_policy_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`
	getNetworkPolicySQL = `SELECT ` + networkPolicyColumns + `
FROM cloud_agents.network_policies
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid = $2`
	networkPolicyPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.network_policies
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid = $2`
	listNetworkPoliciesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(policy_row)
    ORDER BY policy_row.policy_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, policy_uid, policy_name, user_summary, default_egress,
        COALESCE(allowlist_policy_ref, '') AS allowlist_policy_ref,
        ingress_enabled, preview_enabled,
        COALESCE(dns_policy_ref, '') AS dns_policy_ref,
        COALESCE(proxy_policy_ref, '') AS proxy_policy_ref,
        resource_version, created_at, updated_at
    FROM cloud_agents.network_policies
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid > $2
    ORDER BY policy_uid
    LIMIT $3
) AS policy_row`
	networkPolicyAuditCursorIdentitySQL = `SELECT 1
FROM cloud_agents.network_policy_activity
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
    AND policy_uid = $2 AND event_uid = $3 AND occurred_at = $4`
	networkPolicyAuditPolicyIdentitySQL = `SELECT 1
FROM cloud_agents.network_policies
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid = $2`
	listNetworkPolicyAuditSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(audit_row)
    ORDER BY audit_row.occurred_at DESC, audit_row.event_uid DESC), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, policy_uid, event_uid, operation_uid,
        subject_digest, action, policy_resource_version, result, request_id, occurred_at
    FROM cloud_agents.network_policy_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid = $2
        AND ($3::timestamptz IS NULL OR (occurred_at, event_uid) < ($3, $4))
    ORDER BY occurred_at DESC, event_uid DESC
    LIMIT $5
) AS audit_row`
)

func (service *DurableCoordinationService) SetNetworkPolicy(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalnetworkpolicy.SetInput,
) (internalnetworkpolicy.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalnetworkpolicy.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalnetworkpolicy.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalnetworkpolicy.MutationDigest(input)
	if err != nil {
		return internalnetworkpolicy.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalnetworkpolicy.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		actor, ok := operation.Actor()
		if !ok {
			return authz.ErrOperationDenied
		}
		subjectDigest, digestErr := actor.Digest()
		if digestErr != nil {
			return authz.ErrOperationDenied
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				return scanNetworkPolicy(handle.transaction.queryRow(ctx, setNetworkPolicySQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.PolicyID, input.PolicyName,
					input.UserSummary, input.DefaultEgress, input.AllowlistPolicyRef,
					input.IngressEnabled, input.PreviewEnabled, input.DNSPolicyRef, input.ProxyPolicyRef,
					input.ExpectedResourceVersion, input.Mutation.IdempotencyKey,
					digest, input.Mutation.RequestID, subjectDigest), input.Scope, &result)
			})
		})
	})
	return result, mapNetworkPolicyError(err)
}

func (service *DurableCoordinationService) GetNetworkPolicy(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, policyID string,
) (internalnetworkpolicy.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalnetworkpolicy.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(policyID) {
		return internalnetworkpolicy.Snapshot{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result internalnetworkpolicy.Snapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				return scanNetworkPolicy(handle.transaction.queryRow(readContext, getNetworkPolicySQL, projectID, policyID),
					internalnetworkpolicy.Scope{TenantID: tenantID, ProjectID: projectID}, &result)
			})
		})
	})
	return result, mapNetworkPolicyError(err)
}

func (service *DurableCoordinationService) ListNetworkPolicies(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterPolicyID string, limit int,
) (NetworkPolicyPage, error) {
	if service == nil || service.runner == nil {
		return NetworkPolicyPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterPolicyID != "" && !validMutationIdentifier(afterPolicyID) || limit < 1 || limit > 200 {
		return NetworkPolicyPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result NetworkPolicyPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterPolicyID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, networkPolicyPageCursorIdentitySQL, projectID, afterPolicyID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listNetworkPoliciesSQL, projectID, afterPolicyID, limit+1).Scan(&raw); err != nil {
					return err
				}
				var decodeErr error
				result, decodeErr = decodeNetworkPolicyRows(raw, tenantID, projectID, limit)
				return decodeErr
			})
		})
	})
	return result, mapNetworkPolicyError(err)
}

func (service *DurableCoordinationService) ListNetworkPolicyAuditEvents(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, policyID string,
	afterOccurredAt *time.Time, afterEventID string, limit int,
) (NetworkPolicyAuditPage, error) {
	if service == nil || service.runner == nil {
		return NetworkPolicyAuditPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(policyID) || (afterOccurredAt == nil) != (afterEventID == "") ||
		afterEventID != "" && !validMutationIdentifier(afterEventID) || limit < 1 || limit > 200 {
		return NetworkPolicyAuditPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result NetworkPolicyAuditPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				var exists int
				if err := handle.transaction.queryRow(readContext, networkPolicyAuditPolicyIdentitySQL, projectID, policyID).Scan(&exists); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return ErrNetworkPolicyNotFound
					}
					return err
				}
				if afterOccurredAt != nil {
					var exists int
					if err := handle.transaction.queryRow(readContext, networkPolicyAuditCursorIdentitySQL,
						projectID, policyID, afterEventID, *afterOccurredAt).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listNetworkPolicyAuditSQL,
					projectID, policyID, afterOccurredAt, afterEventID, limit+1).Scan(&raw); err != nil {
					return err
				}
				var decodeErr error
				result, decodeErr = decodeNetworkPolicyAuditRows(raw, tenantID, projectID, policyID, limit)
				return decodeErr
			})
		})
	})
	return result, mapNetworkPolicyError(err)
}

func scanNetworkPolicy(row rowScanner, scope internalnetworkpolicy.Scope, result *internalnetworkpolicy.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.PolicyID, &result.PolicyName, &result.UserSummary,
		&result.DefaultEgress, &result.AllowlistPolicyRef, &result.IngressEnabled,
		&result.PreviewEnabled, &result.DNSPolicyRef, &result.ProxyPolicyRef, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return fmt.Errorf("%w: network policy projection", ErrCoordinationResultDrift)
	}
	return nil
}

func decodeNetworkPolicyRows(raw []byte, tenantID, projectID string, limit int) (NetworkPolicyPage, error) {
	var rows []networkPolicyPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return NetworkPolicyPage{}, ErrCoordinationResultDrift
	}
	policies := make([]internalnetworkpolicy.Snapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := internalnetworkpolicy.Snapshot{
			Scope:    internalnetworkpolicy.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			PolicyID: row.PolicyID, PolicyName: row.PolicyName, UserSummary: row.UserSummary,
			DefaultEgress: row.DefaultEgress, AllowlistPolicyRef: row.AllowlistPolicyRef,
			IngressEnabled: row.IngressEnabled, PreviewEnabled: row.PreviewEnabled,
			DNSPolicyRef: row.DNSPolicyRef, ProxyPolicyRef: row.ProxyPolicyRef, ResourceVersion: row.ResourceVersion,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || snapshot.Validate() != nil {
			return NetworkPolicyPage{}, ErrCoordinationResultDrift
		}
		policies = append(policies, snapshot)
	}
	result := NetworkPolicyPage{NetworkPolicies: policies}
	if len(policies) > limit {
		result.NetworkPolicies = policies[:limit]
		result.NextNetworkPolicyID = result.NetworkPolicies[len(result.NetworkPolicies)-1].PolicyID
	}
	return result, nil
}

func decodeNetworkPolicyAuditRows(raw []byte, tenantID, projectID, policyID string, limit int) (NetworkPolicyAuditPage, error) {
	var rows []networkPolicyAuditRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return NetworkPolicyAuditPage{}, ErrCoordinationResultDrift
	}
	events := make([]internalnetworkpolicy.AuditEvent, 0, len(rows))
	for _, row := range rows {
		event := internalnetworkpolicy.AuditEvent{
			Scope:   internalnetworkpolicy.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			EventID: row.EventID, OperationID: row.OperationID, Actor: row.Actor, Action: row.Action,
			PolicyID: row.PolicyID, PolicyResourceVersion: row.PolicyResourceVersion,
			Result: row.Result, RequestID: row.RequestID, OccurredAt: row.OccurredAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || row.PolicyID != policyID || event.Validate() != nil {
			return NetworkPolicyAuditPage{}, ErrCoordinationResultDrift
		}
		events = append(events, event)
	}
	result := NetworkPolicyAuditPage{Events: events}
	if len(events) > limit {
		result.Events = events[:limit]
		last := result.Events[len(result.Events)-1]
		occurredAt := last.OccurredAt
		result.NextOccurredAt, result.NextEventID = &occurredAt, last.EventID
	}
	return result, nil
}

func mapNetworkPolicyError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "network policy idempotency conflict":
			return ErrNetworkPolicyIdempotencyConflict
		case "network policy resource version conflict":
			return ErrNetworkPolicyResourceVersionConflict
		case "network policy is referenced":
			return ErrNetworkPolicyReferenced
		case "network policy was not found":
			return ErrNetworkPolicyNotFound
		}
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNetworkPolicyNotFound
	case err == nil:
		return nil
	default:
		return mapCoordinationDatabaseError("network policy", err)
	}
}

var (
	ErrNetworkPolicyNotFound                = errors.New("network policy was not found")
	ErrNetworkPolicyIdempotencyConflict     = errors.New("network policy idempotency key conflicts")
	ErrNetworkPolicyResourceVersionConflict = errors.New("network policy resource version conflicts")
	ErrNetworkPolicyReferenced              = errors.New("network policy is referenced by an environment profile")
)
