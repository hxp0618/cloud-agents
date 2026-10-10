package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const readIdentitySessionSubjectSQL = `SELECT user_id, subject_kind, subject_issuer, subject_value, subject_digest
FROM cloud_agents_identity.read_session_subject($1, $2)`

var (
	ErrTokenAuthorizationInvalidInput = errors.New("postgres token authorization input is invalid")
	ErrTokenAuthorizationDenied       = errors.New("postgres token authorization is denied")
	ErrTokenAuthorizationAuthority    = errors.New("postgres token authorization authority is invalid")
)

// TokenAuthorizationService owns the Control Plane authorization decision used
// by the identity service before it signs one tenant-bound token.
type TokenAuthorizationService struct {
	userRunner  *TenantTransactionRunner
	adminRunner *TenantTransactionRunner
}

func NewTokenAuthorizationService(pool *pgxpool.Pool) (*TokenAuthorizationService, error) {
	userRunner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		return nil, err
	}
	adminRunner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		return nil, err
	}
	adminRunner.application = "admin"
	return newTokenAuthorizationService(userRunner, adminRunner)
}

func newTokenAuthorizationService(userRunner, adminRunner *TenantTransactionRunner) (*TokenAuthorizationService, error) {
	if userRunner == nil || adminRunner == nil || userRunner.application != "user" || adminRunner.application != "admin" {
		return nil, ErrTokenAuthorizationAuthority
	}
	return &TokenAuthorizationService{userRunner: userRunner, adminRunner: adminRunner}, nil
}

func (service *TokenAuthorizationService) AuthorizeTenantToken(
	ctx context.Context,
	request api.TenantTokenAuthorizationRequest,
) (api.TenantTokenAuthorization, error) {
	if ctx == nil {
		return api.TenantTokenAuthorization{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return api.TenantTokenAuthorization{}, err
	}
	if service == nil || service.userRunner == nil || service.adminRunner == nil {
		return api.TenantTokenAuthorization{}, ErrTokenAuthorizationAuthority
	}
	digest, ok := parseIdentitySessionSHA256(request.SessionSHA256)
	if !ok || !validMutationIdentifier(request.TenantID) ||
		request.ProjectID != "" && !validMutationIdentifier(request.ProjectID) {
		return api.TenantTokenAuthorization{}, ErrTokenAuthorizationInvalidInput
	}
	runner, err := service.runner(request.Application)
	if err != nil {
		return api.TenantTokenAuthorization{}, err
	}
	if runner.application != string(request.Application) {
		return api.TenantTokenAuthorization{}, ErrTokenAuthorizationAuthority
	}
	if runner.pool == nil || runner.clock == nil {
		return api.TenantTokenAuthorization{}, ErrTokenAuthorizationAuthority
	}

	var authorization api.TenantTokenAuthorization
	err = runner.withTenantReadBinder(ctx, request.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok || handle == nil {
			return ErrTokenAuthorizationAuthority
		}
		handle.mutex.Lock()
		defer handle.mutex.Unlock()

		userID, subject, err := handle.readIdentitySessionSubject(readContext, digest, string(request.Application))
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
			return fmt.Errorf("evaluate tenant token scopes: %w", err)
		}
		authorization = api.TenantTokenAuthorization{
			UserID: userID, Issuer: subject.Issuer, TenantID: request.TenantID,
			ProjectID: request.ProjectID, Application: request.Application, Scopes: scopes,
		}
		return nil
	}, bindTenantSetting)
	if err != nil {
		return api.TenantTokenAuthorization{}, err
	}
	return authorization, nil
}

func (service *TokenAuthorizationService) runner(application api.IdentityApplication) (*TenantTransactionRunner, error) {
	switch application {
	case api.IdentityApplicationUser:
		return service.userRunner, nil
	case api.IdentityApplicationAdmin:
		return service.adminRunner, nil
	default:
		return nil, ErrTokenAuthorizationInvalidInput
	}
}

func (handle *tenantReadHandle) readIdentitySessionSubject(
	ctx context.Context,
	digest [sha256.Size]byte,
	application string,
) (string, authz.SubjectRef, error) {
	var userID string
	var subject authz.SubjectRef
	var subjectDigest string
	err := handle.transaction.queryRow(ctx, readIdentitySessionSubjectSQL, digest[:], application).Scan(
		&userID, &subject.Kind, &subject.Issuer, &subject.Subject, &subjectDigest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", authz.SubjectRef{}, ErrTokenAuthorizationDenied
	}
	if err != nil {
		return "", authz.SubjectRef{}, fmt.Errorf("read identity session subject: %w", err)
	}
	expectedDigest, err := subject.Digest()
	if err != nil || !validMutationIdentifier(userID) || subject.Kind != "user" || subject.Subject != "user-"+userID || subjectDigest != expectedDigest {
		return "", authz.SubjectRef{}, ErrTokenAuthorizationAuthority
	}
	return userID, subject, nil
}

func parseIdentitySessionSHA256(value string) ([sha256.Size]byte, bool) {
	var digest [sha256.Size]byte
	const prefix = "sha256:"
	if len(value) != len(prefix)+hex.EncodedLen(sha256.Size) || value[:len(prefix)] != prefix {
		return digest, false
	}
	decoded, err := hex.DecodeString(value[len(prefix):])
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != value[len(prefix):] {
		return digest, false
	}
	copy(digest[:], decoded)
	return digest, true
}
