package server

import (
	"context"
	"errors"

	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var ErrNilManagedAgentOrganizationReadService = errors.New("managed agent organization read service is nil")

type ManagedAgentOrganizationReader interface {
	GetOrganization(context.Context, string, *authn.VerifiedPrincipal, string) (postgres.Organization, error)
	CreateOrganization(context.Context, string, *authn.VerifiedPrincipal, postgres.CreateOrganizationInput) (postgres.Organization, error)
}

type ManagedAgentGetOrganizationRequest struct {
	TenantID       string
	OrganizationID string
	RequestID      string
}

func GetOrganization(
	ctx context.Context,
	reader ManagedAgentOrganizationReader,
	principal *authn.VerifiedPrincipal,
	request ManagedAgentGetOrganizationRequest,
) (postgres.Organization, error) {
	if reader == nil {
		return postgres.Organization{}, ErrNilManagedAgentOrganizationReadService
	}
	validated, err := openapiv1.ValidateGetServerRequest(request.TenantID, request.OrganizationID, request.RequestID)
	if err != nil {
		return postgres.Organization{}, err
	}
	return reader.GetOrganization(ctx, validated.TenantID, principal, validated.ResourceID)
}
