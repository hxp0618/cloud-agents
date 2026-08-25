package server

import (
	"context"
	"errors"

	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var ErrNilDurableProjectCreateService = errors.New("durable project create service is nil")

type DurableProjectCreateServer struct {
	service *postgres.DurableCoordinationService
}

func NewDurableProjectCreateServer(service *postgres.DurableCoordinationService) (*DurableProjectCreateServer, error) {
	if service == nil {
		return nil, ErrNilDurableProjectCreateService
	}
	return &DurableProjectCreateServer{service: service}, nil
}

// Create validates the existing strict ProjectCreate body, then binds it to
// the distinct generated durable profile. It is transport-neutral; only the
// localdev HTTP adapter below exposes the versioned route.
func (server *DurableProjectCreateServer) Create(
	ctx context.Context,
	principal *authn.VerifiedPrincipal,
	request ManagedAgentCreateProjectRequest,
) (postgres.DurableProjectCreateResult, error) {
	if server == nil || server.service == nil {
		return postgres.DurableProjectCreateResult{}, ErrNilDurableProjectCreateService
	}
	validated, err := openapiv1.ValidateCreateProjectServerRequest(
		request.RouteTenantID, request.RequestID, request.IdempotencyKey, request.Body,
	)
	if err != nil {
		return postgres.DurableProjectCreateResult{}, err
	}
	return server.service.CreateProjectDurable(ctx, validated.TenantID, principal, postgres.DurableProjectCreateInput{
		Profile: coordination.ManagedAgentCreateProjectDurable(),
		Request: coordination.ManagedAgentCreateProjectRequest{
			Name: validated.Body.Name,
			OrganizationRef: coordination.OrganizationRef{
				Namespace: validated.Body.OrganizationRef.Namespace,
				Kind:      validated.Body.OrganizationRef.Kind,
				ID:        validated.Body.OrganizationRef.ID,
			},
			DisplayName: validated.Body.DisplayName,
		},
		IdempotencyKey: validated.IdempotencyKey,
		AuditFactID:    validated.RequestID,
	})
}
