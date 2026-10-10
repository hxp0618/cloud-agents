package identity

import (
	"context"
	"crypto/sha256"
	"errors"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresProviderPersistence struct {
	pool      *pgxpool.Pool
	passwords *PasswordStore
}

func (store *postgresProviderPersistence) ReadProviderClient(ctx context.Context, application api.IdentityApplication, providerID string) (ProviderClientSnapshot, error) {
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var client ProviderClientSnapshot
	var rootCA, agentID pgtype.Text
	err := store.pool.QueryRow(databaseCtx, `SELECT provider_id,application,display_name,provider_kind,
		issuer,client_id,redirect_uri,secret_ref,root_ca_ref,agent_id,scopes,
		trust_provider_email,allowed_organization_ids,resource_version
		FROM cloud_agents_identity.read_provider_client($1,$2)`, string(application), providerID).Scan(
		&client.ID, &client.Application, new(string), &client.Kind,
		&client.Issuer, &client.ClientID, &client.RedirectURL, &client.SecretRef,
		&rootCA, &agentID, &client.Scopes, &client.TrustProviderEmail,
		&client.AllowedOrganizationIDs, &client.Revision,
	)
	if err != nil {
		return ProviderClientSnapshot{}, postgresProviderError(ctx, err)
	}
	if rootCA.Valid {
		client.RootCARef = rootCA.String
	}
	if agentID.Valid {
		client.AgentID = agentID.String
	}
	return client, nil
}

func (store *postgresProviderPersistence) CreateProviderFlow(ctx context.Context, client ProviderClientSnapshot, start providerFlowStart) error {
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var session, invitation, invitationIP, reauth, display any
	if start.SessionDigest != nil {
		session = start.SessionDigest[:]
	}
	if start.InvitationDigest != nil {
		invitation = start.InvitationDigest[:]
	}
	if start.InvitationIP != nil {
		invitationIP = start.InvitationIP[:]
	}
	if start.ReauthDigest != nil {
		reauth = start.ReauthDigest[:]
	}
	if start.DisplayName != "" {
		display = start.DisplayName
	}
	var outcome string
	err := store.pool.QueryRow(databaseCtx, `SELECT cloud_agents_identity.create_provider_flow(
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		start.StateDigest[:], start.VerifierDigest[:], start.NonceDigest[:],
		string(start.Application), start.ProviderID, client.Revision, string(start.Purpose),
		session, invitation, invitationIP, reauth, display, start.ExpiresAt,
	).Scan(&outcome)
	if err != nil {
		return postgresProviderError(ctx, err)
	}
	switch outcome {
	case "created":
		return nil
	case "denied":
		return browserauth.ErrUnauthorized
	case "rate_limited":
		return ErrRateLimited
	default:
		return ErrUnavailable
	}
}

func (store *postgresProviderPersistence) ConsumeProviderFlow(ctx context.Context, application api.IdentityApplication, stateDigest, verifierDigest, nonceDigest [sha256.Size]byte) (providerFlow, error) {
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var flow providerFlow
	var rootCA, agentID, display pgtype.Text
	var state, session, invitation, reauth []byte
	err := store.pool.QueryRow(databaseCtx, `SELECT state_digest,provider_id,application,display_name,
		provider_kind,issuer,client_id,redirect_uri,secret_ref,root_ca_ref,agent_id,scopes,
		trust_provider_email,allowed_organization_ids,resource_version,purpose,
		session_digest,invitation_digest,reauth_digest,invitation_display_name
		FROM cloud_agents_identity.consume_provider_flow($1,$2,$3,$4)`,
		string(application), stateDigest[:], verifierDigest[:], nonceDigest[:],
	).Scan(
		&state, &flow.ID, &flow.Application, new(string), &flow.Kind,
		&flow.Issuer, &flow.ClientID, &flow.RedirectURL, &flow.SecretRef, &rootCA,
		&agentID, &flow.Scopes, &flow.TrustProviderEmail, &flow.AllowedOrganizationIDs,
		&flow.Revision, &flow.Purpose, &session, &invitation, &reauth, &display,
	)
	if err != nil {
		return providerFlow{}, postgresProviderError(ctx, err)
	}
	if rootCA.Valid {
		flow.RootCARef = rootCA.String
	}
	if agentID.Valid {
		flow.AgentID = agentID.String
	}
	if display.Valid {
		flow.DisplayName = display.String
	}
	consumedState, err := optionalProviderDigest(state)
	if err != nil || consumedState == nil {
		return providerFlow{}, ErrUnavailable
	}
	flow.StateDigest = *consumedState
	if flow.SessionDigest, err = optionalProviderDigest(session); err != nil {
		return providerFlow{}, ErrUnavailable
	}
	if flow.InvitationDigest, err = optionalProviderDigest(invitation); err != nil {
		return providerFlow{}, ErrUnavailable
	}
	if flow.ReauthDigest, err = optionalProviderDigest(reauth); err != nil {
		return providerFlow{}, ErrUnavailable
	}
	return flow, nil
}

func (store *postgresProviderPersistence) FinishProviderCallback(ctx context.Context, input providerFinishInput) (providerFinishResult, error) {
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var assertedEmail any
	if input.Identity.Email != "" {
		assertedEmail = input.Identity.Email
	}
	var session, reauth any
	if input.Flow.Purpose == ProviderPurposeLogin || input.Flow.Purpose == ProviderPurposeInvitation || input.Flow.Purpose == ProviderPurposeReauth {
		session = input.SessionDigest[:]
	}
	if input.Flow.Purpose == ProviderPurposeReauth {
		reauth = input.ReauthDigest[:]
	}
	var result providerFinishResult
	var methodID, methodProvider, methodIssuer, methodSubject pgtype.Text
	var methodCreatedAt, reauthExpiresAt pgtype.Timestamptz
	err := store.pool.QueryRow(databaseCtx, `SELECT purpose,user_id,email,display_name,
		platform_admin,login_method_id,login_method_provider_id,
		login_method_issuer,login_method_subject,login_method_created_at,reauth_expires_at
		FROM cloud_agents_identity.finish_provider_callback(
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		input.Flow.StateDigest[:], string(input.Application), input.Identity.Issuer,
		input.Identity.Subject, assertedEmail, input.Identity.EmailVerified,
		session, input.UserID, input.LoginMethodID, reauth,
		input.MembershipID, input.RoleBindingID, input.MembershipAudit,
		input.RoleBindingAudit, input.EventID, input.CorrelationID,
	).Scan(
		&result.Purpose, &result.UserID, &result.Email, &result.DisplayName,
		&result.PlatformAdmin, &methodID, &methodProvider, &methodIssuer, &methodSubject,
		&methodCreatedAt, &reauthExpiresAt,
	)
	if err != nil {
		return providerFinishResult{}, postgresProviderError(ctx, err)
	}
	if methodID.Valid || methodProvider.Valid || methodIssuer.Valid || methodSubject.Valid || methodCreatedAt.Valid {
		if !(methodID.Valid && methodProvider.Valid && methodIssuer.Valid && methodSubject.Valid && methodCreatedAt.Valid) {
			return providerFinishResult{}, ErrUnavailable
		}
		result.LoginMethod = &ProviderLoginMethod{
			ID: methodID.String, ProviderID: methodProvider.String,
			Issuer: methodIssuer.String, Subject: methodSubject.String, CreatedAt: methodCreatedAt.Time.UTC(),
		}
	}
	if reauthExpiresAt.Valid {
		result.ReauthExpiresAt = reauthExpiresAt.Time.UTC()
	}
	return result, nil
}

func (store *postgresProviderPersistence) ReadTenantPage(ctx context.Context, application api.IdentityApplication, sessionDigest [sha256.Size]byte) (api.BrowserTenantPage, error) {
	return store.passwords.Tenants(ctx, application, sessionDigest, initialTenantPageSize, "")
}

func optionalProviderDigest(value []byte) (*[sha256.Size]byte, error) {
	if value == nil {
		return nil, nil
	}
	if len(value) != sha256.Size {
		return nil, errors.New("invalid provider digest")
	}
	var digest [sha256.Size]byte
	copy(digest[:], value)
	return &digest, nil
}

func postgresProviderError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22023":
			return ErrProviderInput
		case "23505", "40001":
			return ErrProviderConflict
		case "28000", "42501", "P0002":
			return browserauth.ErrUnauthorized
		}
	}
	return ErrUnavailable
}
