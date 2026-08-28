package server

import (
	"context"
	"errors"

	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var ErrNilManagedAgentTenantReadService = errors.New("managed agent tenant read service is nil")

type ManagedAgentTenantReader interface {
	GetPlatformTenant(context.Context, *authn.VerifiedPrincipal, string) (postgres.PlatformTenant, error)
}

type ManagedAgentGetTenantRequest struct {
	TenantID  string
	RequestID string
}

func GetPlatformTenant(
	ctx context.Context,
	reader ManagedAgentTenantReader,
	principal *authn.VerifiedPrincipal,
	request ManagedAgentGetTenantRequest,
) (postgres.PlatformTenant, error) {
	if reader == nil {
		return postgres.PlatformTenant{}, ErrNilManagedAgentTenantReadService
	}
	validated, err := openapiv1.ValidateGetServerRequest(request.TenantID, request.TenantID, request.RequestID)
	if err != nil {
		return postgres.PlatformTenant{}, err
	}
	return reader.GetPlatformTenant(ctx, principal, validated.TenantID)
}
