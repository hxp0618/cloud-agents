package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const invitationCursorDomain = "cloud-agents.identity.invitation-cursor.v1\x00"

var (
	ErrInvitationInvalid  = errors.New("identity invitation input invalid")
	ErrInvitationConflict = errors.New("identity invitation state conflict")
)

// InvitationStore reaches invitation state only through administrator- and
// proof-gated SECURITY DEFINER functions.
type InvitationStore struct {
	pool      *pgxpool.Pool
	passwords *PasswordStore
}

func NewInvitationStore(pool *pgxpool.Pool, passwords *PasswordStore) (*InvitationStore, error) {
	if pool == nil || passwords == nil || passwords.pool != pool {
		return nil, errInvalidConfiguration
	}
	return &InvitationStore{pool: pool, passwords: passwords}, nil
}

func (store *InvitationStore) CreateInvitation(ctx context.Context, sessionDigest [32]byte, tenantID string, request api.InvitationCreateRequest) (api.InvitationCreated, error) {
	if ctx == nil || common.ValidateIdentifier(tenantID, "/tenantId") != nil {
		return api.InvitationCreated{}, ErrInvitationInvalid
	}
	if err := ctx.Err(); err != nil {
		return api.InvitationCreated{}, err
	}
	if _, err := api.EncodeInvitationCreateRequestJSON(request); err != nil {
		return api.InvitationCreated{}, ErrInvitationInvalid
	}
	domain, err := browserauth.NormalizeEmailDomain(request.Email)
	if err != nil {
		return api.InvitationCreated{}, ErrInvitationInvalid
	}
	at := strings.LastIndexByte(request.Email, '@')
	request.Email = request.Email[:at] + "@" + domain
	if request.ScopeLevel == "tenant" && request.ScopeID != tenantID {
		return api.InvitationCreated{}, ErrInvitationInvalid
	}

	invitationID, err := newInvitationIdentifier("invitation-")
	if err != nil {
		return api.InvitationCreated{}, ErrUnavailable
	}
	invitationCode, codeDigest, err := browserauth.NewProof()
	if err != nil {
		return api.InvitationCreated{}, ErrUnavailable
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return api.InvitationCreated{}, ErrUnavailable
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return api.InvitationCreated{}, ErrUnavailable
	}

	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var invitation api.Invitation
	var createdAt, expiresAt time.Time
	err = store.pool.QueryRow(databaseCtx, `SELECT id,tenant_id,email,role_name,scope_level,scope_id,verification,state,created_at,expires_at
		FROM cloud_agents_identity.create_invitation($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		sessionDigest[:], tenantID, invitationID, codeDigest[:], request.Email, domain,
		request.RoleName, request.ScopeLevel, request.ScopeID, request.Verification,
		eventID, correlationID,
	).Scan(&invitation.ID, &invitation.TenantID, &invitation.Email, &invitation.RoleName,
		&invitation.ScopeLevel, &invitation.ScopeID, &invitation.Verification, &invitation.State,
		&createdAt, &expiresAt)
	if err != nil {
		return api.InvitationCreated{}, invitationError(ctx, err)
	}
	invitation.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	invitation.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	created := api.InvitationCreated{Invitation: invitation, InvitationCode: invitationCode}
	if _, err := api.EncodeInvitationCreatedJSON(created); err != nil {
		return api.InvitationCreated{}, ErrUnavailable
	}
	return created, nil
}

func (store *InvitationStore) ListInvitations(ctx context.Context, sessionDigest [32]byte, tenantID string, pageSize int, pageToken string) (api.InvitationPage, error) {
	if ctx == nil || common.ValidateIdentifier(tenantID, "/tenantId") != nil || pageSize < 1 || pageSize > 200 {
		return api.InvitationPage{}, ErrInvitationInvalid
	}
	if err := ctx.Err(); err != nil {
		return api.InvitationPage{}, err
	}
	after, err := store.invitationCursorAfter(sessionDigest, tenantID, pageToken)
	if err != nil {
		return api.InvitationPage{}, err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	rows, err := store.pool.Query(databaseCtx, `SELECT id,tenant_id,email,role_name,scope_level,scope_id,verification,state,created_at,expires_at
		FROM cloud_agents_identity.list_invitations($1,$2,$3,$4)`, sessionDigest[:], tenantID, after, pageSize+1)
	if err != nil {
		return api.InvitationPage{}, invitationError(ctx, err)
	}
	defer rows.Close()
	page := api.InvitationPage{Invitations: make([]api.Invitation, 0, pageSize)}
	for rows.Next() {
		var invitation api.Invitation
		var createdAt, expiresAt time.Time
		if err := rows.Scan(&invitation.ID, &invitation.TenantID, &invitation.Email,
			&invitation.RoleName, &invitation.ScopeLevel, &invitation.ScopeID,
			&invitation.Verification, &invitation.State, &createdAt, &expiresAt); err != nil {
			return api.InvitationPage{}, ErrUnavailable
		}
		if len(page.Invitations) == pageSize {
			page.NextPageToken = store.invitationCursor(sessionDigest, tenantID, page.Invitations[len(page.Invitations)-1].ID)
			break
		}
		invitation.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		invitation.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
		page.Invitations = append(page.Invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return api.InvitationPage{}, invitationError(ctx, err)
	}
	if _, err := api.EncodeInvitationPageJSON(page); err != nil {
		return api.InvitationPage{}, ErrUnavailable
	}
	return page, nil
}

func (store *InvitationStore) RevokeInvitation(ctx context.Context, sessionDigest [32]byte, tenantID, invitationID string) error {
	if ctx == nil || common.ValidateIdentifier(tenantID, "/tenantId") != nil || common.ValidateIdentifier(invitationID, "/invitationId") != nil {
		return ErrInvitationInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return ErrUnavailable
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return ErrUnavailable
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var revoked bool
	if err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.revoke_invitation($1,$2,$3,$4,$5)`,
		sessionDigest[:], tenantID, invitationID, eventID, correlationID).Scan(&revoked); err != nil {
		return invitationError(ctx, err)
	}
	if !revoked {
		return ErrUnavailable
	}
	return nil
}

func (store *InvitationStore) AcceptInvitation(ctx context.Context, application api.IdentityApplication, sessionDigest *[32]byte, clientIP netip.Addr, request api.InvitationAcceptRequest) error {
	if ctx == nil || !validApplication(application) || !clientIP.IsValid() || clientIP.Zone() != "" {
		return ErrInvitationInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := api.EncodeInvitationAcceptRequestJSON(request); err != nil {
		return ErrInvitationInvalid
	}
	codeDigest, err := browserauth.ProofDigest(request.InvitationCode)
	if err != nil {
		return ErrInvitationInvalid
	}
	ipBucket, err := store.recordInvitationAcceptAttempt(ctx, codeDigest, clientIP)
	if err != nil {
		return err
	}
	var sessionArgument any
	if sessionDigest != nil {
		sessionArgument = sessionDigest[:]
	}

	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	tx, err := store.pool.BeginTx(databaseCtx, pgx.TxOptions{})
	if err != nil {
		return ErrUnavailable
	}
	defer rollbackTransaction(tx)
	var invitationID, email string
	var existingUser pgtype.Text
	if err := tx.QueryRow(databaseCtx, `SELECT invitation_id,email,existing_user_id
		FROM cloud_agents_identity.prepare_invitation_accept($1,$2,$3)`,
		codeDigest[:], string(application), sessionArgument).Scan(&invitationID, &email, &existingUser); err != nil {
		return invitationError(ctx, err)
	}
	if common.ValidateIdentifier(invitationID, "/invitationId") != nil {
		return ErrUnavailable
	}
	if _, err := browserauth.NormalizeEmailDomain(email); err != nil {
		return ErrUnavailable
	}

	userID := ""
	var passwordHash, displayName any
	if sessionDigest == nil {
		if existingUser.Valid {
			return browserauth.ErrUnauthorized
		}
		if request.Password == "" || request.DisplayName == "" {
			return ErrInvitationInvalid
		}
		userID, err = newInvitationIdentifier("account-")
		if err != nil {
			return ErrUnavailable
		}
		if err := store.passwords.acquireArgon2(ctx); err != nil {
			return err
		}
		defer store.passwords.releaseArgon2()
		hashed, hashErr := browserauth.HashPassword(request.Password)
		if hashErr != nil {
			return ErrInvitationInvalid
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		passwordHash, displayName = hashed, request.DisplayName
	} else {
		if !existingUser.Valid || request.Password != "" || request.DisplayName != "" {
			return ErrInvitationInvalid
		}
		userID = existingUser.String
	}

	membershipID, err := newInvitationIdentifier("membership-")
	if err != nil {
		return ErrUnavailable
	}
	roleBindingID, err := newInvitationIdentifier("role-binding-")
	if err != nil {
		return ErrUnavailable
	}
	membershipAuditID, err := newInvitationIdentifier("audit-membership-")
	if err != nil {
		return ErrUnavailable
	}
	roleBindingAuditID, err := newInvitationIdentifier("audit-role-binding-")
	if err != nil {
		return ErrUnavailable
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return ErrUnavailable
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return ErrUnavailable
	}
	var accepted bool
	if err := tx.QueryRow(databaseCtx, `SELECT cloud_agents_identity.finish_invitation_accept(
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		codeDigest[:], string(application), sessionArgument, userID, passwordHash, displayName,
		membershipID, roleBindingID, membershipAuditID, roleBindingAuditID, eventID, correlationID,
	).Scan(&accepted); err != nil {
		return invitationError(ctx, err)
	}
	if !accepted {
		return ErrUnavailable
	}
	var cleared bool
	if err := tx.QueryRow(databaseCtx, `SELECT cloud_agents_identity.clear_invitation_accept_attempt($1,$2)`, codeDigest[:], ipBucket[:]).Scan(&cleared); err != nil {
		return invitationError(ctx, err)
	}
	if !cleared {
		return ErrUnavailable
	}
	if err := tx.Commit(databaseCtx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	return nil
}

func (store *InvitationStore) invitationCursor(sessionDigest [32]byte, tenantID, after string) string {
	return base64.RawURLEncoding.EncodeToString(append([]byte(after), store.invitationCursorMAC(sessionDigest, tenantID, after)...))
}

func (store *InvitationStore) invitationCursorAfter(sessionDigest [32]byte, tenantID, cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) > 256 {
		return "", ErrInvitationInvalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != cursor {
		return "", ErrInvitationInvalid
	}
	after := string(raw[:len(raw)-sha256.Size])
	if common.ValidateIdentifier(after, "/pageToken") != nil || !hmac.Equal(raw[len(raw)-sha256.Size:], store.invitationCursorMAC(sessionDigest, tenantID, after)) {
		return "", ErrInvitationInvalid
	}
	return after, nil
}

func (store *InvitationStore) invitationCursorMAC(sessionDigest [32]byte, tenantID, after string) []byte {
	mac := hmac.New(sha256.New, store.passwords.csrfKey)
	_, _ = mac.Write([]byte(invitationCursorDomain))
	_, _ = mac.Write(sessionDigest[:])
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(tenantID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(after))
	return mac.Sum(nil)
}

func newInvitationIdentifier(prefix string) (string, error) {
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(randomID[:]), nil
}

func invitationError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22023":
			return ErrInvitationInvalid
		case "28000":
			return browserauth.ErrUnauthorized
		case "42501":
			return ErrForbidden
		case "40001", "23505":
			return ErrInvitationConflict
		}
	}
	return ErrUnavailable
}
