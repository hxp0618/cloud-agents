package server

import (
	"context"
	"errors"

	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var ErrInvalidManagedAgentRBACReadService = errors.New("managed agent RBAC read service is nil")

type ManagedAgentRBACReader interface {
	ResolveMembershipScope(context.Context, string, string) (authz.ScopeRef, error)
	ResolveRoleBindingScope(context.Context, string, string) (authz.ScopeRef, error)
	GetMembership(context.Context, string, *authn.VerifiedPrincipal, string, authz.ScopeRef) (postgres.Membership, error)
	ListMemberships(context.Context, string, *authn.VerifiedPrincipal, string, int) (postgres.MembershipPage, error)
	GetRoleBinding(context.Context, string, *authn.VerifiedPrincipal, string, authz.ScopeRef) (postgres.RoleBinding, error)
}

type ManagedAgentGetMembershipRequest struct {
	TenantID     string
	MembershipID string
	RequestID    string
	Scope        authz.ScopeRef
}

func GetMembership(ctx context.Context, reader ManagedAgentRBACReader, principal *authn.VerifiedPrincipal, request ManagedAgentGetMembershipRequest) (postgres.Membership, error) {
	if reader == nil {
		return postgres.Membership{}, ErrInvalidManagedAgentRBACReadService
	}
	validated, err := openapiv1.ValidateGetServerRequest(request.TenantID, request.MembershipID, request.RequestID)
	if err != nil {
		return postgres.Membership{}, err
	}
	return reader.GetMembership(ctx, validated.TenantID, principal, validated.ResourceID, request.Scope)
}

type ManagedAgentGetRoleBindingRequest struct {
	TenantID      string
	RoleBindingID string
	RequestID     string
	Scope         authz.ScopeRef
}

func GetRoleBinding(ctx context.Context, reader ManagedAgentRBACReader, principal *authn.VerifiedPrincipal, request ManagedAgentGetRoleBindingRequest) (postgres.RoleBinding, error) {
	if reader == nil {
		return postgres.RoleBinding{}, ErrInvalidManagedAgentRBACReadService
	}
	validated, err := openapiv1.ValidateGetServerRequest(request.TenantID, request.RoleBindingID, request.RequestID)
	if err != nil {
		return postgres.RoleBinding{}, err
	}
	return reader.GetRoleBinding(ctx, validated.TenantID, principal, validated.ResourceID, request.Scope)
}
