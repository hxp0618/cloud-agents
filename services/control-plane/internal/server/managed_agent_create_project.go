// Package server owns transport-neutral request admission boundaries.
package server

import (
	"context"
	"errors"

	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var ErrNilManagedAgentCreateProjectService = errors.New("managed agent create project service is nil")
var ErrNilManagedAgentProjectReadService = errors.New("managed agent project read service is nil")

// ManagedAgentCreateProjectRequest is the complete transport-neutral input to
// the claim-only managedAgentCreateProject admission boundary.
type ManagedAgentCreateProjectRequest struct {
	RouteTenantID  string
	RequestID      string
	IdempotencyKey string
	Body           []byte
}

// ManagedAgentCreateProjectServer admits only the idempotency claim. It does
// not create or complete a project and has no transport or provider surface.
type ManagedAgentCreateProjectServer struct {
	service *postgres.DurableCoordinationService
}

func NewManagedAgentCreateProjectServer(
	service *postgres.DurableCoordinationService,
) (*ManagedAgentCreateProjectServer, error) {
	if service == nil {
		return nil, ErrNilManagedAgentCreateProjectService
	}
	return &ManagedAgentCreateProjectServer{service: service}, nil
}

// Claim validates generated wire authority, maps that exact typed value, and
// passes the original verified-principal pointer to the concrete PostgreSQL
// service. The route tenant and request ID are never sourced from the body,
// principal, configuration, or another adapter.
func (server *ManagedAgentCreateProjectServer) Claim(
	ctx context.Context,
	principal *authn.VerifiedPrincipal,
	request ManagedAgentCreateProjectRequest,
) (postgres.IdempotencyClaimResult, error) {
	if server == nil || server.service == nil {
		return postgres.IdempotencyClaimResult{}, ErrNilManagedAgentCreateProjectService
	}
	validated, err := openapiv1.ValidateCreateProjectServerRequest(
		request.RouteTenantID,
		request.RequestID,
		request.IdempotencyKey,
		request.Body,
	)
	if err != nil {
		return postgres.IdempotencyClaimResult{}, err
	}
	claim := mapManagedAgentCreateProjectClaim(validated)
	return server.service.ClaimIdempotency(ctx, validated.TenantID, principal, claim)
}

func mapManagedAgentCreateProjectClaim(
	validated openapiv1.CreateProjectServerInput,
) postgres.IdempotencyClaimInput {
	return postgres.IdempotencyClaimInput{
		Profile: coordination.ManagedAgentCreateProject(),
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
	}
}

type ManagedAgentGetProjectRequest struct {
	TenantID  string
	ProjectID string
	RequestID string
}

type ManagedAgentProjectReader interface {
	GetProject(context.Context, string, *authn.VerifiedPrincipal, string) (postgres.Project, error)
	ListProjects(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.ProjectPage, error)
}

func GetProject(
	ctx context.Context,
	reader ManagedAgentProjectReader,
	principal *authn.VerifiedPrincipal,
	request ManagedAgentGetProjectRequest,
) (postgres.Project, error) {
	if reader == nil {
		return postgres.Project{}, ErrNilManagedAgentProjectReadService
	}
	validated, err := openapiv1.ValidateGetServerRequest(request.TenantID, request.ProjectID, request.RequestID)
	if err != nil {
		return postgres.Project{}, err
	}
	return reader.GetProject(ctx, validated.TenantID, principal, validated.ResourceID)
}
