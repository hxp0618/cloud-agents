package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

const (
	providerFlowVerifierDomain = "cloud-agents.identity.provider-flow.verifier.v1\x00"
	providerFlowNonceDomain    = "cloud-agents.identity.provider-flow.nonce.v1\x00"
	providerFlowLifetime       = 10 * time.Minute
	githubCallbackIssuer       = "https://github.com/login/oauth"
)

var (
	ErrProviderInput    = errors.New("identity provider input invalid")
	ErrProviderConflict = errors.New("identity provider state conflict")
)

type ProviderPurpose string

const (
	ProviderPurposeLogin      ProviderPurpose = "login"
	ProviderPurposeInvitation ProviderPurpose = "invitation"
	ProviderPurposeReauth     ProviderPurpose = "reauth"
	ProviderPurposeLink       ProviderPurpose = "link"
)

// ProviderClientSnapshot is the non-secret durable configuration that selects
// one provider adapter. SecretRef and RootCARef are resolved only by the
// runtime-owned factory and are never returned over the browser API.
type ProviderClientSnapshot struct {
	ID                     string
	Application            api.IdentityApplication
	Kind                   string
	Issuer                 string
	ClientID               string
	RedirectURL            string
	SecretRef              string
	RootCARef              string
	AgentID                string
	Scopes                 []string
	TrustProviderEmail     bool
	AllowedOrganizationIDs []string
	Revision               int64
}

type ProviderFactory interface {
	Provider(context.Context, ProviderClientSnapshot) (browserauth.LoginProvider, error)
}

type ProviderAuthorizationInput struct {
	Application    api.IdentityApplication
	ProviderID     string
	Purpose        ProviderPurpose
	SessionDigest  *[sha256.Size]byte
	InvitationCode string
	ReauthProof    string
	DisplayName    string
	ClientIP       netip.Addr
}

type ProviderAuthorization struct {
	State            string
	AuthorizationURL string
	ExpiresAt        time.Time
}

type ProviderCallbackInput struct {
	Application  api.IdentityApplication
	State        string
	Code         string
	Issuer       string
	SessionState string
}

type ProviderLoginMethod struct {
	ID         string
	ProviderID string
	Issuer     string
	Subject    string
	CreatedAt  time.Time
}

// ProviderCallbackResult is a tagged result. Exactly one payload is set:
// Login for login/invitation, ReauthProof for reauth, or LoginMethod for link.
type ProviderCallbackResult struct {
	Purpose         ProviderPurpose
	Login           *api.IdentityLoginResult
	SessionHandle   string
	ReauthProof     string
	ReauthExpiresAt time.Time
	LoginMethod     *ProviderLoginMethod
}

type providerFlowStart struct {
	Application      api.IdentityApplication
	ProviderID       string
	Purpose          ProviderPurpose
	StateDigest      [sha256.Size]byte
	VerifierDigest   [sha256.Size]byte
	NonceDigest      [sha256.Size]byte
	SessionDigest    *[sha256.Size]byte
	InvitationDigest *[sha256.Size]byte
	InvitationIP     *[sha256.Size]byte
	ReauthDigest     *[sha256.Size]byte
	DisplayName      string
	ExpiresAt        time.Time
	CorrelationID    string
}

type providerFlow struct {
	ProviderClientSnapshot
	StateDigest      [sha256.Size]byte
	Purpose          ProviderPurpose
	SessionDigest    *[sha256.Size]byte
	InvitationDigest *[sha256.Size]byte
	ReauthDigest     *[sha256.Size]byte
	DisplayName      string
}

type providerFinishInput struct {
	Flow             providerFlow
	Identity         browserauth.ProviderIdentity
	Application      api.IdentityApplication
	SessionDigest    [sha256.Size]byte
	UserID           string
	LoginMethodID    string
	ReauthDigest     [sha256.Size]byte
	MembershipID     string
	RoleBindingID    string
	MembershipAudit  string
	RoleBindingAudit string
	EventID          string
	CorrelationID    string
}

type providerFinishResult struct {
	Purpose         ProviderPurpose
	UserID          string
	Email           string
	DisplayName     string
	PlatformAdmin   bool
	LoginMethod     *ProviderLoginMethod
	ReauthExpiresAt time.Time
}

type providerPersistence interface {
	ReadProviderClient(context.Context, api.IdentityApplication, string) (ProviderClientSnapshot, error)
	CreateProviderFlow(context.Context, ProviderClientSnapshot, providerFlowStart) error
	ConsumeProviderFlow(context.Context, api.IdentityApplication, [sha256.Size]byte, [sha256.Size]byte, [sha256.Size]byte) (providerFlow, error)
	FinishProviderCallback(context.Context, providerFinishInput) (providerFinishResult, error)
	ReadTenantPage(context.Context, api.IdentityApplication, [sha256.Size]byte) (api.BrowserTenantPage, error)
}

// ProviderStore coordinates durable one-use OAuth/OIDC flows. The persistence
// implementation is the sole account/session/link mutation authority; provider
// adapters only authenticate an issuer/subject assertion.
type ProviderStore struct {
	passwords   *PasswordStore
	persistence providerPersistence
	factory     ProviderFactory
	flowKey     []byte
	now         func() time.Time
}

func newProviderStore(passwords *PasswordStore, persistence providerPersistence, factory ProviderFactory, flowKey []byte) (*ProviderStore, error) {
	if passwords == nil || persistence == nil || factory == nil || len(flowKey) != sha256.Size {
		return nil, errInvalidConfiguration
	}
	return &ProviderStore{
		passwords: passwords, persistence: persistence, factory: factory,
		flowKey: append([]byte(nil), flowKey...), now: time.Now,
	}, nil
}

func NewProviderStore(passwords *PasswordStore, factory ProviderFactory, flowKey []byte) (*ProviderStore, error) {
	if passwords == nil || passwords.pool == nil {
		return nil, errInvalidConfiguration
	}
	persistence := &postgresProviderPersistence{pool: passwords.pool, passwords: passwords}
	return newProviderStore(passwords, persistence, factory, flowKey)
}

func (store *ProviderStore) StartAuthorization(ctx context.Context, input ProviderAuthorizationInput) (ProviderAuthorization, error) {
	if ctx == nil || !validProviderAuthorizationInput(input) {
		return ProviderAuthorization{}, ErrProviderInput
	}
	if err := ctx.Err(); err != nil {
		return ProviderAuthorization{}, err
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return ProviderAuthorization{}, ErrUnavailable
	}
	state, stateDigest, err := browserauth.NewProof()
	if err != nil {
		return ProviderAuthorization{}, ErrUnavailable
	}
	verifier := store.flowProof(providerFlowVerifierDomain, state)
	nonce := store.flowProof(providerFlowNonceDomain, state)
	verifierDigest, _ := browserauth.ProofDigest(verifier)
	nonceDigest, _ := browserauth.ProofDigest(nonce)

	start := providerFlowStart{
		Application: input.Application, ProviderID: input.ProviderID, Purpose: input.Purpose,
		StateDigest: stateDigest, VerifierDigest: verifierDigest, NonceDigest: nonceDigest,
		SessionDigest: input.SessionDigest, DisplayName: input.DisplayName,
		ExpiresAt: store.now().UTC().Add(providerFlowLifetime), CorrelationID: correlationID,
	}
	if input.InvitationCode != "" {
		digest, digestErr := browserauth.ProofDigest(input.InvitationCode)
		if digestErr != nil {
			return ProviderAuthorization{}, ErrProviderInput
		}
		start.InvitationDigest = &digest
		bucket, ok := invitationIPBucket(input.ClientIP)
		if !ok {
			return ProviderAuthorization{}, ErrProviderInput
		}
		start.InvitationIP = &bucket
	}
	if input.ReauthProof != "" {
		digest, digestErr := browserauth.ProofDigest(input.ReauthProof)
		if digestErr != nil {
			return ProviderAuthorization{}, ErrProviderInput
		}
		start.ReauthDigest = &digest
	}

	client, err := store.persistence.ReadProviderClient(ctx, input.Application, input.ProviderID)
	if err != nil {
		return ProviderAuthorization{}, providerStoreError(ctx, err)
	}
	if !validProviderClientSnapshot(client) || client.ID != input.ProviderID || client.Application != input.Application {
		return ProviderAuthorization{}, ErrUnavailable
	}
	if err := store.persistence.CreateProviderFlow(ctx, client, start); err != nil {
		return ProviderAuthorization{}, providerStoreError(ctx, err)
	}
	provider, err := store.factory.Provider(ctx, client)
	if err != nil {
		return ProviderAuthorization{}, ErrUnavailable
	}
	challengeDigest := sha256.Sum256([]byte(verifier))
	authorizationURL, err := provider.AuthorizationURL(browserauth.AuthorizationRequest{
		State: state, Nonce: nonce, CodeChallenge: base64.RawURLEncoding.EncodeToString(challengeDigest[:]),
	})
	if err != nil {
		return ProviderAuthorization{}, ErrUnavailable
	}
	return ProviderAuthorization{State: state, AuthorizationURL: authorizationURL, ExpiresAt: start.ExpiresAt}, nil
}

func (store *ProviderStore) CompleteAuthorization(ctx context.Context, input ProviderCallbackInput) (ProviderCallbackResult, error) {
	if ctx == nil || !validProviderCallbackInput(input) {
		return ProviderCallbackResult{}, ErrProviderInput
	}
	if err := ctx.Err(); err != nil {
		return ProviderCallbackResult{}, err
	}
	stateDigest, err := browserauth.ProofDigest(input.State)
	if err != nil {
		return ProviderCallbackResult{}, ErrProviderInput
	}
	verifier := store.flowProof(providerFlowVerifierDomain, input.State)
	nonce := store.flowProof(providerFlowNonceDomain, input.State)
	verifierDigest, _ := browserauth.ProofDigest(verifier)
	nonceDigest, _ := browserauth.ProofDigest(nonce)

	// Consumption precedes all provider network traffic. Failed exchanges and
	// canceled callbacks therefore cannot leave a reusable authorization code.
	flow, err := store.persistence.ConsumeProviderFlow(ctx, input.Application, stateDigest, verifierDigest, nonceDigest)
	if err != nil {
		return ProviderCallbackResult{}, providerStoreError(ctx, err)
	}
	if !validConsumedProviderFlow(flow, input.Application) {
		return ProviderCallbackResult{}, ErrUnavailable
	}
	if input.Issuer != "" {
		issuerMatches := input.Issuer == flow.Issuer
		if flow.Kind == "github" {
			issuerMatches = issuerMatches || input.Issuer == githubCallbackIssuer
		}
		if (flow.Kind != "oidc" && flow.Kind != "gitlab" && flow.Kind != "github") || !issuerMatches {
			return ProviderCallbackResult{}, browserauth.ErrUnauthorized
		}
	}
	provider, err := store.factory.Provider(ctx, flow.ProviderClientSnapshot)
	if err != nil {
		return ProviderCallbackResult{}, ErrUnavailable
	}
	assertion, err := provider.Exchange(ctx, browserauth.AuthorizationCallback{Code: input.Code, CodeVerifier: verifier, Nonce: nonce})
	if err != nil {
		return ProviderCallbackResult{}, browserauth.ErrUnauthorized
	}
	if assertion.Issuer == "" || assertion.Subject == "" || assertion.Issuer != flow.Issuer {
		return ProviderCallbackResult{}, browserauth.ErrUnauthorized
	}

	finish, sessionHandle, reauthProof, err := newProviderFinishInput(ctx, input.Application, flow, assertion)
	if err != nil {
		return ProviderCallbackResult{}, err
	}
	result, err := store.persistence.FinishProviderCallback(ctx, finish)
	if err != nil {
		return ProviderCallbackResult{}, providerStoreError(ctx, err)
	}
	if result.Purpose != flow.Purpose || result.UserID == "" {
		return ProviderCallbackResult{}, ErrUnavailable
	}

	response := ProviderCallbackResult{Purpose: flow.Purpose}
	switch flow.Purpose {
	case ProviderPurposeLogin, ProviderPurposeInvitation:
		page, pageErr := store.persistence.ReadTenantPage(ctx, input.Application, finish.SessionDigest)
		if pageErr != nil {
			return ProviderCallbackResult{}, providerStoreError(ctx, pageErr)
		}
		session := browserSession(input.Application, result.UserID, result.Email, result.DisplayName, result.PlatformAdmin, store.passwords.csrfProof(finish.SessionDigest, input.Application))
		session.Tenants, session.NextPageToken = page.Tenants, page.NextPageToken
		response.Login = &api.IdentityLoginResult{Session: session, SessionHandle: sessionHandle}
	case ProviderPurposeReauth:
		response.SessionHandle = sessionHandle
		response.ReauthProof = reauthProof
		response.ReauthExpiresAt = result.ReauthExpiresAt
	case ProviderPurposeLink:
		if result.LoginMethod == nil {
			return ProviderCallbackResult{}, ErrUnavailable
		}
		response.LoginMethod = result.LoginMethod
	default:
		return ProviderCallbackResult{}, ErrUnavailable
	}
	if !validProviderCallbackResult(response) {
		return ProviderCallbackResult{}, ErrUnavailable
	}
	return response, nil
}

func (store *ProviderStore) flowProof(domain, state string) string {
	mac := hmac.New(sha256.New, store.flowKey)
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte(state))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func newProviderFinishInput(ctx context.Context, application api.IdentityApplication, flow providerFlow, assertion browserauth.ProviderIdentity) (providerFinishInput, string, string, error) {
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}
	finish := providerFinishInput{Flow: flow, Identity: assertion, Application: application, CorrelationID: correlationID}
	finish.EventID, err = newAuditEventID()
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}
	finish.UserID, err = newInvitationIdentifier("account-")
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}
	finish.LoginMethodID, err = newInvitationIdentifier("login-method-")
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}
	finish.MembershipID, err = newInvitationIdentifier("membership-")
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}
	finish.RoleBindingID, err = newInvitationIdentifier("role-binding-")
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}
	finish.MembershipAudit, err = newInvitationIdentifier("audit-membership-")
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}
	finish.RoleBindingAudit, err = newInvitationIdentifier("audit-role-binding-")
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}

	var sessionHandle, reauthProof string
	if flow.Purpose == ProviderPurposeLogin || flow.Purpose == ProviderPurposeInvitation || flow.Purpose == ProviderPurposeReauth {
		sessionHandle, finish.SessionDigest, err = browserauth.NewProof()
	}
	if err == nil && flow.Purpose == ProviderPurposeReauth {
		reauthProof, finish.ReauthDigest, err = browserauth.NewProof()
	}
	if err != nil {
		return providerFinishInput{}, "", "", ErrUnavailable
	}
	return finish, sessionHandle, reauthProof, nil
}

func validProviderAuthorizationInput(input ProviderAuthorizationInput) bool {
	if !validApplication(input.Application) || !validProviderText(input.ProviderID, 128) || len(input.DisplayName) > 160 {
		return false
	}
	switch input.Purpose {
	case ProviderPurposeLogin:
		return input.SessionDigest == nil && input.InvitationCode == "" && input.ReauthProof == "" && input.DisplayName == "" && !input.ClientIP.IsValid()
	case ProviderPurposeInvitation:
		return input.SessionDigest == nil && input.InvitationCode != "" && input.ReauthProof == "" && input.ClientIP.IsValid() && input.ClientIP.Zone() == "" && validProviderText(input.DisplayName, 160)
	case ProviderPurposeReauth:
		return input.SessionDigest != nil && input.InvitationCode == "" && input.ReauthProof == "" && input.DisplayName == "" && !input.ClientIP.IsValid()
	case ProviderPurposeLink:
		return input.SessionDigest != nil && input.InvitationCode == "" && input.ReauthProof != "" && input.DisplayName == "" && !input.ClientIP.IsValid()
	default:
		return false
	}
}

func validProviderCallbackInput(input ProviderCallbackInput) bool {
	if !validApplication(input.Application) || input.Code == "" || len(input.Code) > 4096 ||
		input.Issuer != "" && !validProviderText(input.Issuer, 512) ||
		input.SessionState != "" && !validProviderText(input.SessionState, 512) {
		return false
	}
	return true
}

func validProviderClientSnapshot(client ProviderClientSnapshot) bool {
	if !validProviderText(client.ID, 128) || !validApplication(client.Application) || !validProviderText(client.Kind, 64) ||
		client.Issuer == "" || len(client.Issuer) > 512 || client.ClientID == "" || len(client.ClientID) > 512 ||
		client.RedirectURL == "" || len(client.RedirectURL) > 2048 || !validProviderText(client.SecretRef, 128) ||
		client.RootCARef != "" && !validProviderText(client.RootCARef, 128) || client.Revision < 1 || len(client.Scopes) > 32 || len(client.AllowedOrganizationIDs) > 32 {
		return false
	}
	if fixedScopes, fixed := browserauth.FixedProviderScopes(client.Kind); fixed && !slices.Equal(client.Scopes, fixedScopes) {
		return false
	}
	return true
}

func validConsumedProviderFlow(flow providerFlow, application api.IdentityApplication) bool {
	if !validProviderClientSnapshot(flow.ProviderClientSnapshot) || flow.Application != application {
		return false
	}
	switch flow.Purpose {
	case ProviderPurposeLogin:
		return flow.SessionDigest == nil && flow.InvitationDigest == nil && flow.ReauthDigest == nil && flow.DisplayName == ""
	case ProviderPurposeInvitation:
		return flow.SessionDigest == nil && flow.InvitationDigest != nil && flow.ReauthDigest == nil && validProviderText(flow.DisplayName, 160)
	case ProviderPurposeReauth:
		return flow.SessionDigest != nil && flow.InvitationDigest == nil && flow.ReauthDigest == nil && flow.DisplayName == ""
	case ProviderPurposeLink:
		return flow.SessionDigest != nil && flow.InvitationDigest == nil && flow.ReauthDigest != nil && flow.DisplayName == ""
	default:
		return false
	}
}

func validProviderCallbackResult(result ProviderCallbackResult) bool {
	payloads := 0
	if result.Login != nil {
		payloads++
	}
	if result.ReauthProof != "" {
		payloads++
	}
	if result.LoginMethod != nil {
		payloads++
	}
	if payloads != 1 {
		return false
	}
	switch result.Purpose {
	case ProviderPurposeLogin, ProviderPurposeInvitation:
		return result.Login != nil
	case ProviderPurposeReauth:
		return result.SessionHandle != "" && result.ReauthProof != "" && !result.ReauthExpiresAt.IsZero()
	case ProviderPurposeLink:
		return result.LoginMethod != nil
	default:
		return false
	}
}

func validProviderText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}

func providerStoreError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, browserauth.ErrUnauthorized) {
		return browserauth.ErrUnauthorized
	}
	if errors.Is(err, ErrProviderInput) || errors.Is(err, ErrProviderConflict) || errors.Is(err, ErrRateLimited) {
		return err
	}
	return ErrUnavailable
}
