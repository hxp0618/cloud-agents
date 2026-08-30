package postgres

import (
	"context"
	"errors"
	"math"
	"unicode/utf8"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
)

const createOrganizationSQL = `SELECT resource_uid, resource_version, resource_state
FROM cloud_agents.create_organization($1, $2, $3, $4, $5, $6, $7)`

type CreateOrganizationInput struct {
	ExpectedTenantRevision int64
	OrganizationUID        string
	OrganizationName       string
	DisplayName            string
	AuditFactUID           string
	ReasonCode             string
}

func (service *DurableCoordinationService) CreateOrganization(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input CreateOrganizationInput,
) (Organization, error) {
	if service == nil || service.runner == nil {
		return Organization{}, ErrNilCoordinationRunner
	}
	if ctx == nil {
		return Organization{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return Organization{}, err
	}
	if !validMutationIdentifier(tenantID) ||
		input.ExpectedTenantRevision < 1 || input.ExpectedTenantRevision == math.MaxInt64 ||
		!validMutationIdentifier(input.OrganizationUID) || !validMutationIdentifier(input.OrganizationName) ||
		!utf8.ValidString(input.DisplayName) || utf8.RuneCountInString(input.DisplayName) < 1 || utf8.RuneCountInString(input.DisplayName) > 160 ||
		!validMutationIdentifier(input.AuditFactUID) || !validMutationIdentifier(input.ReasonCode) {
		return Organization{}, ErrMutationInvalidInput
	}

	var result Organization
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
		operation, err := binder.Bind(tenantID, scope, "organizations.create")
		if err != nil {
			return mapVerifiedMutationError(err)
		}
		err = service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				result, err = createOrganizationInTransaction(ctx, handle, tenantID, input)
				return err
			})
		})
		return mapVerifiedMutationError(err)
	})
	if err != nil {
		return Organization{}, err
	}
	return result, nil
}

func createOrganizationInTransaction(
	ctx context.Context,
	handle *tenantReadHandle,
	tenantID string,
	input CreateOrganizationInput,
) (Organization, error) {
	if _, err := scanMutationResult(
		handle.transaction.queryRow(ctx, createOrganizationSQL,
			tenantID, input.ExpectedTenantRevision, input.OrganizationUID, input.OrganizationName,
			input.DisplayName, input.AuditFactUID, input.ReasonCode,
		),
		tenantID, input.OrganizationUID, input.ExpectedTenantRevision, "active", "create organization",
	); err != nil {
		return Organization{}, err
	}
	var organization Organization
	if err := scanOrganization(handle.transaction.queryRow(ctx, getOrganizationSQL, input.OrganizationUID), &organization); err != nil {
		if errors.Is(err, ErrOrganizationNotFound) {
			return Organization{}, ErrMutationResultDrift
		}
		return Organization{}, err
	}
	return organization, nil
}
