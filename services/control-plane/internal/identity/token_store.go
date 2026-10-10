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

type TenantTokenAuthorizer interface {
	AuthorizeTenantToken(context.Context, string, api.TenantTokenAuthorizationRequest) (api.TenantTokenAuthorization, error)
}

type TokenSigningAuthorityProvider func(context.Context) (TokenSigningAuthority, error)

type TokenStore struct {
	pool       *pgxpool.Pool
	signer     *TokenSigner
	authorizer TenantTokenAuthorizer
	authority  TokenSigningAuthorityProvider
}

func NewTokenStore(pool *pgxpool.Pool, signer *TokenSigner, authorizer TenantTokenAuthorizer, authority TokenSigningAuthorityProvider) (*TokenStore, error) {
	if pool == nil || signer == nil || authorizer == nil || authority == nil {
		return nil, errInvalidConfiguration
	}
	return &TokenStore{pool: pool, signer: signer, authorizer: authorizer, authority: authority}, nil
}

func (store *TokenStore) IssueTenantToken(ctx context.Context, application api.IdentityApplication, sessionDigest [sha256.Size]byte, request api.TenantTokenIssueRequest) (api.TenantToken, error) {
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
	authorization, err := store.authorizer.AuthorizeTenantToken(authorizationCtx, correlationID, api.TenantTokenAuthorizationRequest{
		Application: application, SessionSHA256: "sha256:" + hex.EncodeToString(sessionDigest[:]),
		TenantID: request.TenantID, ProjectID: request.ProjectID,
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
		common.ValidateIdentifier(authorization.UserID, "/userId") != nil {
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
		TokenClientWeb, application, authorization.UserID, request.TenantID,
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
	if err := tx.QueryRow(databaseCtx, `SELECT cloud_agents_identity.record_issued_token(
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
	)`, sessionDigest[:], authorization.UserID, authorization.Issuer, string(application), request.TenantID,
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

func (store *TokenStore) TokenStatus(ctx context.Context, request api.TokenStatusRequest) (api.TokenStatus, error) {
	if err := ctx.Err(); err != nil {
		return api.TokenStatus{}, err
	}
	if store == nil || store.pool == nil {
		return api.TokenStatus{}, ErrUnavailable
	}
	digest, ok := parseTokenSHA256(request.TokenSHA256)
	if !ok || !validTokenClientID(request.ExpectedClientID, request.ExpectedApplication) ||
		common.ValidateIdentifier(request.ExpectedTenantID, "/expectedTenantId") != nil ||
		request.ExpectedProjectID != "" && common.ValidateIdentifier(request.ExpectedProjectID, "/expectedProjectId") != nil {
		return api.TokenStatus{}, ErrForbidden
	}
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancelDatabase()
	var projectID any
	if request.ExpectedProjectID != "" {
		projectID = request.ExpectedProjectID
	}
	var active bool
	if err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.access_token_is_active_v2($1, $2, $3, $4, $5)`,
		digest[:], request.ExpectedClientID, string(request.ExpectedApplication), request.ExpectedTenantID, projectID).Scan(&active); err != nil {
		if ctx.Err() != nil {
			return api.TokenStatus{}, ctx.Err()
		}
		return api.TokenStatus{}, ErrUnavailable
	}
	status := "inactive"
	if active {
		status = "active"
	}
	return api.TokenStatus{Status: status}, nil
}

func validTokenClientID(clientID string, application api.IdentityApplication) bool {
	if clientID == CLIClientID || clientID == AutomationClientID {
		return validApplication(application)
	}
	return clientID == AdminWebClientID && application == api.IdentityApplicationAdmin ||
		clientID == UserWebClientID && application == api.IdentityApplicationUser
}

func parseTokenSHA256(value string) ([sha256.Size]byte, bool) {
	var digest [sha256.Size]byte
	if len(value) != len("sha256:")+hex.EncodedLen(sha256.Size) || value[:len("sha256:")] != "sha256:" {
		return digest, false
	}
	decoded, err := hex.DecodeString(value[len("sha256:"):])
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != value[len("sha256:"):] {
		return digest, false
	}
	copy(digest[:], decoded)
	return digest, true
}
