package server

import (
	"context"
	"errors"

	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var ErrInvalidManagedAgentRoleReadService = errors.New("managed agent role read service is nil")

type ManagedAgentRoleReader interface {
	GetRole(context.Context, string, *authn.VerifiedPrincipal, string) (postgres.Role, error)
}

type ManagedAgentGetRoleRequest struct {
	TenantID  string
	RoleID    string
	RequestID string
}

func GetRole(ctx context.Context, reader ManagedAgentRoleReader, principal *authn.VerifiedPrincipal, request ManagedAgentGetRoleRequest) (postgres.Role, error) {
	if reader == nil {
		return postgres.Role{}, ErrInvalidManagedAgentRoleReadService
	}
	validated, err := openapiv1.ValidateGetServerRequest(request.TenantID, request.RoleID, request.RequestID)
	if err != nil {
		return postgres.Role{}, err
	}
	return reader.GetRole(ctx, validated.TenantID, principal, validated.ResourceID)
}
