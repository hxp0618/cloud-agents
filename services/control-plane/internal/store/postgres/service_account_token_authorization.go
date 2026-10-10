package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

const (
	readCLIGrantSubjectSQL = `SELECT user_id, subject_kind, subject_issuer, subject_value, subject_digest
FROM cloud_agents_identity.read_cli_grant_subject($1, $2)`
	readServiceAccountSubjectSQL = `SELECT service_account_id, subject_kind, subject_issuer, subject_value, subject_digest
FROM cloud_agents_identity.read_service_account_subject($1, $2, $3)`
)

// AuthorizePrincipalToken is the single Control Plane RBAC decision for CLI
// grants and service-account credentials. Credential proof establishes only a
// stable subject; current memberships and role bindings still come from the
// same tenant snapshot used for browser tokens.
func (service *TokenAuthorizationService) AuthorizePrincipalToken(
	ctx context.Context,
	request api.PrincipalTokenAuthorizationRequest,
) (api.PrincipalTokenAuthorization, error) {
	if ctx == nil {
		return api.PrincipalTokenAuthorization{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return api.PrincipalTokenAuthorization{}, err
	}
	if service == nil || service.userRunner == nil || service.adminRunner == nil {
		return api.PrincipalTokenAuthorization{}, ErrTokenAuthorizationAuthority
	}
	digest, ok := parseIdentitySessionSHA256(request.CredentialSHA256)
	if !ok || !validMutationIdentifier(request.TenantID) || request.ProjectID != "" && !validMutationIdentifier(request.ProjectID) ||
		request.ClientID != "cloud-agents-cli" && request.ClientID != "cloud-agents-automation" {
		return api.PrincipalTokenAuthorization{}, ErrTokenAuthorizationInvalidInput
	}
	runner, err := service.runner(request.Application)
	if err != nil {
		return api.PrincipalTokenAuthorization{}, err
	}
	if runner.application != string(request.Application) || runner.pool == nil || runner.clock == nil {
		return api.PrincipalTokenAuthorization{}, ErrTokenAuthorizationAuthority
	}

	var authorization api.PrincipalTokenAuthorization
	err = runner.withTenantReadBinder(ctx, request.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok || handle == nil {
			return ErrTokenAuthorizationAuthority
		}
		handle.mutex.Lock()
		defer handle.mutex.Unlock()

		principalID, subject, err := handle.readPrincipalCredentialSubject(
			readContext, digest, request.ClientID, string(request.Application), request.TenantID,
		)
		if err != nil {
			return err
		}
		resource := authz.ScopeRef{Level: authz.ScopeTenant, ID: request.TenantID}
		if request.ProjectID != "" {
			resource = authz.ScopeRef{Level: authz.ScopeProject, ID: request.ProjectID}
		}
		snapshot, now, err := handle.authorizationSnapshot(readContext, subject, resource)
		if err != nil {
			return err
		}
		scopes, err := authz.EvaluateTokenScopes(snapshot, subject, now)
		if errors.Is(err, authz.ErrOperationDenied) || errors.Is(err, authz.ErrScopeUnresolved) {
			return ErrTokenAuthorizationDenied
		}
		if err != nil {
			return fmt.Errorf("evaluate principal token scopes: %w", err)
		}
		scopes = projectAutomationScopes(request, scopes)
		authorization = api.PrincipalTokenAuthorization{
			PrincipalID: principalID,
			Subject: common.SubjectRef{
				Kind: subject.Kind, Issuer: subject.Issuer, Subject: subject.Subject,
			},
			Issuer: subject.Issuer, TenantID: request.TenantID, ProjectID: request.ProjectID,
			Application: request.Application, Scopes: scopes,
		}
		return nil
	}, bindTenantSetting)
	if err != nil {
		return api.PrincipalTokenAuthorization{}, err
	}
	return authorization, nil
}

func projectAutomationScopes(request api.PrincipalTokenAuthorizationRequest, scopes []string) []string {
	if request.ClientID != "cloud-agents-automation" || request.Application != api.IdentityApplicationAdmin || request.ProjectID == "" || !slices.Contains(scopes, "projects.act") {
		return scopes
	}
	index, found := slices.BinarySearch(scopes, "remote-worker-bootstrap.act")
	if found {
		return scopes
	}
	return slices.Insert(scopes, index, "remote-worker-bootstrap.act")
}

func (handle *tenantReadHandle) readPrincipalCredentialSubject(
	ctx context.Context,
	digest [sha256.Size]byte,
	clientID, application, tenantID string,
) (string, authz.SubjectRef, error) {
	var principalID, subjectDigest string
	var subject authz.SubjectRef
	var row rowScanner
	switch clientID {
	case "cloud-agents-cli":
		row = handle.transaction.queryRow(ctx, readCLIGrantSubjectSQL, digest[:], application)
	case "cloud-agents-automation":
		row = handle.transaction.queryRow(ctx, readServiceAccountSubjectSQL, digest[:], application, tenantID)
	default:
		return "", authz.SubjectRef{}, ErrTokenAuthorizationInvalidInput
	}
	if err := row.Scan(&principalID, &subject.Kind, &subject.Issuer, &subject.Subject, &subjectDigest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", authz.SubjectRef{}, ErrTokenAuthorizationDenied
		}
		return "", authz.SubjectRef{}, fmt.Errorf("read principal credential subject: %w", err)
	}
	expectedKind, expectedSubjectPrefix := "user", "user-"
	if clientID == "cloud-agents-automation" {
		expectedKind, expectedSubjectPrefix = "serviceAccount", "service-"
	}
	expectedDigest, err := subject.Digest()
	if err != nil || !validMutationIdentifier(principalID) || subject.Kind != expectedKind ||
		subject.Subject != expectedSubjectPrefix+principalID || subjectDigest != expectedDigest {
		return "", authz.SubjectRef{}, ErrTokenAuthorizationAuthority
	}
	return principalID, subject, nil
}
