package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PrincipalTokenAuthorizer interface {
	AuthorizePrincipalToken(context.Context, string, api.PrincipalTokenAuthorizationRequest) (api.PrincipalTokenAuthorization, error)
}

// AutomationStore signs only after the Control Plane has projected current
// service-account RBAC. It then records the token against the exact credential
// in the same database used by the online token-status check.
type AutomationStore struct {
	pool       *pgxpool.Pool
	signer     *TokenSigner
	authorizer PrincipalTokenAuthorizer
	authority  TokenSigningAuthorityProvider
}

func NewAutomationStore(pool *pgxpool.Pool, signer *TokenSigner, authorizer PrincipalTokenAuthorizer, authority TokenSigningAuthorityProvider) (*AutomationStore, error) {
	if pool == nil || signer == nil || authorizer == nil || authority == nil {
		return nil, errInvalidConfiguration
	}
	return &AutomationStore{pool: pool, signer: signer, authorizer: authorizer, authority: authority}, nil
}

func (store *AutomationStore) IssueAutomationTenantToken(
	ctx context.Context,
	application api.IdentityApplication,
	credentialDigest [sha256.Size]byte,
	request api.TenantTokenIssueRequest,
) (api.TenantToken, error) {
	if ctx == nil {
		return api.TenantToken{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return api.TenantToken{}, err
	}
	if store == nil || store.pool == nil || store.signer == nil || store.authorizer == nil || store.authority == nil {
		return api.TenantToken{}, ErrUnavailable
	}
	if !validApplication(application) || common.ValidateIdentifier(request.TenantID, "/tenantId") != nil ||
		request.ProjectID != "" && common.ValidateIdentifier(request.ProjectID, "/projectId") != nil {
		return api.TenantToken{}, ErrForbidden
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	authorizationCtx, cancelAuthorization := context.WithTimeout(ctx, databaseOperationTimeout)
	authorization, err := store.authorizer.AuthorizePrincipalToken(authorizationCtx, correlationID, api.PrincipalTokenAuthorizationRequest{
		Application: application, ClientID: AutomationClientID,
		CredentialSHA256: "sha256:" + hex.EncodeToString(credentialDigest[:]),
		TenantID:         request.TenantID, ProjectID: request.ProjectID,
	})
	cancelAuthorization()
	if err != nil {
		if ctx.Err() != nil {
			return api.TenantToken{}, ctx.Err()
		}
		var clientError *api.ClientError
		if errors.As(err, &clientError) && clientError.Status == http.StatusForbidden &&
			clientError.Problem != nil && clientError.Problem.Error.Code == "AUTHORIZATION_DENIED" {
			return api.TenantToken{}, ErrForbidden
		}
		return api.TenantToken{}, ErrUnavailable
	}
	if authorization.Application != application || authorization.TenantID != request.TenantID ||
		authorization.ProjectID != request.ProjectID || authorization.Issuer != store.signer.issuer ||
		common.ValidateIdentifier(authorization.PrincipalID, "/principalId") != nil ||
		authorization.Subject.Kind != "serviceAccount" || authorization.Subject.Issuer != authorization.Issuer ||
		authorization.Subject.Subject != "service-"+authorization.PrincipalID || authorization.Subject.Validate() != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return api.TenantToken{}, err
	}
	authority, err := store.authority(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return api.TenantToken{}, ctx.Err()
		}
		return api.TenantToken{}, ErrUnavailable
	}
	signingRequest, err := NewTokenSigningRequest(
		TokenClientAutomation, application, authorization.PrincipalID, request.TenantID,
		request.ProjectID, authorization.Scopes, authority, time.Now().UTC(),
	)
	if err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	signed, err := store.signer.Sign(signingRequest)
	if err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	tokenDigest, ok := parseTokenSHA256(signed.SHA256)
	if !ok {
		return api.TenantToken{}, ErrUnavailable
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancelDatabase()
	tx, err := store.pool.BeginTx(databaseCtx, pgx.TxOptions{})
	if err != nil {
		if ctx.Err() != nil {
			return api.TenantToken{}, ctx.Err()
		}
		return api.TenantToken{}, ErrUnavailable
	}
	defer rollbackTransaction(tx)
	var projectID any
	if request.ProjectID != "" {
		projectID = request.ProjectID
	}
	var recorded bool
	if err := tx.QueryRow(databaseCtx, `SELECT cloud_agents_identity.record_service_account_issued_token(
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11
	)`, credentialDigest[:], authorization.PrincipalID, authorization.Issuer, string(application), request.TenantID,
		projectID, signed.JTI, tokenDigest[:], signed.ExpiresAt, eventID, correlationID).Scan(&recorded); err != nil {
		if ctx.Err() != nil {
			return api.TenantToken{}, ctx.Err()
		}
		return api.TenantToken{}, ErrUnavailable
	}
	if !recorded {
		return api.TenantToken{}, browserauth.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return api.TenantToken{}, err
	}
	if err := tx.Commit(databaseCtx); err != nil {
		if ctx.Err() != nil {
			return api.TenantToken{}, ctx.Err()
		}
		return api.TenantToken{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return api.TenantToken{}, err
	}
	if err := databaseCtx.Err(); err != nil {
		return api.TenantToken{}, ErrUnavailable
	}
	return api.TenantToken{AccessToken: signed.Token, TokenType: "Bearer", ExpiresAt: signed.ExpiresAt.Format(time.RFC3339)}, nil
}
