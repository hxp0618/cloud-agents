package identity

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
)

const (
	AdminWebClientID   = "cloud-agents-admin-web"
	UserWebClientID    = "cloud-agents-user-web"
	CLIClientID        = "cloud-agents-cli"
	AutomationClientID = "cloud-agents-automation"

	tenantTokenLifetime = 15 * time.Minute
	maximumTokenScopes  = 64
	maximumNumericDate  = int64(253402300799)
	maximumEpoch        = int64(9007199254740991)
)

const tokenProfileV1 = "cloud-agents-access-token/v1"

var (
	ErrInvalidTokenSigner         = errors.New("identity token signer configuration is invalid")
	ErrInvalidTokenSigningRequest = errors.New("identity token signing request is invalid")
	ErrTokenSigningFailed         = errors.New("identity token signing failed")

	tokenScopePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*\.(create|get|list|watch|update|delete|act|bind)$`)
)

type TokenSignerConfig struct {
	Issuer        string
	AdminAudience string
	UserAudience  string
	KeyID         string
	PrivateKey    *rsa.PrivateKey
}

type TokenSigningAuthority struct {
	SecurityEpoch int64
	NotBefore     time.Time
	ExpiresAt     time.Time
	KeyNotBefore  time.Time
	KeyNotAfter   time.Time
}

type TokenClientPurpose string

const (
	TokenClientWeb        TokenClientPurpose = "web"
	TokenClientCLI        TokenClientPurpose = "cli"
	TokenClientAutomation TokenClientPurpose = "automation"
)

type TokenSigningRequest struct {
	application api.IdentityApplication
	subjectID   string
	subjectKind string
	subject     string
	clientID    string
	tenantID    string
	projectID   string
	scopes      []string
	authority   TokenSigningAuthority
	now         time.Time
	initialized bool
}

type SignedTenantToken struct {
	Token     string
	JTI       string
	ExpiresAt time.Time
	SHA256    string
}

type TokenSigner struct {
	issuer        string
	adminAudience string
	userAudience  string
	keyID         string
	privateKey    *rsa.PrivateKey
}

func NewTokenSigner(config TokenSignerConfig) (*TokenSigner, error) {
	if !validAuthorityURI(config.Issuer) || !validAuthorityURI(config.AdminAudience) ||
		!validAuthorityURI(config.UserAudience) || config.AdminAudience == config.UserAudience ||
		common.ValidateIdentifier(config.KeyID, "/keyId") != nil || !validSigningKey(config.PrivateKey) {
		return nil, ErrInvalidTokenSigner
	}
	return &TokenSigner{
		issuer: config.Issuer, adminAudience: config.AdminAudience, userAudience: config.UserAudience,
		keyID: config.KeyID, privateKey: config.PrivateKey,
	}, nil
}

func NewTokenSigningRequest(
	purpose TokenClientPurpose,
	application api.IdentityApplication,
	subjectID string,
	tenantID string,
	projectID string,
	scopes []string,
	authority TokenSigningAuthority,
	now time.Time,
) (TokenSigningRequest, error) {
	request := TokenSigningRequest{
		application: application,
		subjectID:   subjectID,
		tenantID:    tenantID,
		projectID:   projectID,
		scopes:      append([]string(nil), scopes...),
		authority:   authority,
		now:         now,
		initialized: true,
	}
	switch purpose {
	case TokenClientWeb:
		request.subjectKind = "user"
		request.subject = "user-" + subjectID
		switch application {
		case api.IdentityApplicationAdmin:
			request.clientID = AdminWebClientID
		case api.IdentityApplicationUser:
			request.clientID = UserWebClientID
		default:
			return TokenSigningRequest{}, ErrInvalidTokenSigningRequest
		}
	case TokenClientCLI:
		request.subjectKind = "user"
		request.subject = "user-" + subjectID
		request.clientID = CLIClientID
	case TokenClientAutomation:
		request.subjectKind = "serviceAccount"
		request.subject = "service-" + subjectID
		request.clientID = AutomationClientID
	default:
		return TokenSigningRequest{}, ErrInvalidTokenSigningRequest
	}
	if !validTokenRequest("https://constructor.invalid", request) {
		return TokenSigningRequest{}, ErrInvalidTokenSigningRequest
	}
	return request, nil
}

func (signer *TokenSigner) Sign(request TokenSigningRequest) (SignedTenantToken, error) {
	if signer == nil || signer.privateKey == nil {
		return SignedTenantToken{}, ErrInvalidTokenSigner
	}
	audience, ok := signer.audience(request.application)
	if !ok || !validTokenRequest(signer.issuer, request) {
		return SignedTenantToken{}, ErrInvalidTokenSigningRequest
	}
	now := request.now.UTC().Unix()
	expiresAt := now + int64(tenantTokenLifetime/time.Second)
	if request.authority.ExpiresAt.Unix() < expiresAt {
		expiresAt = request.authority.ExpiresAt.Unix()
	}
	if request.authority.KeyNotAfter.Unix() < expiresAt {
		expiresAt = request.authority.KeyNotAfter.Unix()
	}
	if expiresAt <= now {
		return SignedTenantToken{}, ErrInvalidTokenSigningRequest
	}
	tokenIDBytes := make([]byte, 16)
	if _, err := rand.Read(tokenIDBytes); err != nil {
		return SignedTenantToken{}, ErrTokenSigningFailed
	}
	tokenID := base64.RawURLEncoding.EncodeToString(tokenIDBytes)
	header, err := json.Marshal(tokenHeader{Algorithm: "RS256", KeyID: signer.keyID, Type: "at+jwt"})
	if err != nil {
		return SignedTenantToken{}, ErrTokenSigningFailed
	}
	claims := tokenClaims{
		Issuer: signer.issuer, Subject: request.subject, Audience: audience, ExpiresAt: expiresAt,
		IssuedAt: now, TokenID: tokenID, ClientID: request.clientID, Scope: strings.Join(request.scopes, " "),
		SubjectKind: request.subjectKind, TenantID: request.tenantID, ProjectID: request.projectID,
		SecurityEpoch: request.authority.SecurityEpoch, TokenProfile: tokenProfileV1,
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return SignedTenantToken{}, ErrTokenSigningFailed
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, signer.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return SignedTenantToken{}, ErrTokenSigningFailed
	}
	token := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	tokenDigest := sha256.Sum256([]byte(token))
	return SignedTenantToken{
		Token: token, JTI: tokenID, ExpiresAt: time.Unix(expiresAt, 0).UTC(),
		SHA256: "sha256:" + hex.EncodeToString(tokenDigest[:]),
	}, nil
}

func (signer *TokenSigner) audience(application api.IdentityApplication) (audience string, ok bool) {
	switch application {
	case api.IdentityApplicationAdmin:
		return signer.adminAudience, true
	case api.IdentityApplicationUser:
		return signer.userAudience, true
	default:
		return "", false
	}
}

func validAuthorityURI(value string) bool {
	return (authz.SubjectRef{Kind: "user", Issuer: value, Subject: "user-validation"}).Validate() == nil
}

func validSigningKey(key *rsa.PrivateKey) bool {
	return key != nil && key.N != nil && key.E == 65537 && key.N.BitLen() >= 2048 && key.N.BitLen() <= 4096 && key.Validate() == nil
}

func validTokenRequest(issuer string, request TokenSigningRequest) bool {
	if !request.initialized ||
		common.ValidateIdentifier(request.subjectID, "/subjectId") != nil ||
		(request.subjectKind != "user" || request.subject != "user-"+request.subjectID) &&
			(request.subjectKind != "serviceAccount" || request.subject != "service-"+request.subjectID) ||
		(authz.SubjectRef{Kind: request.subjectKind, Issuer: issuer, Subject: request.subject}).Validate() != nil ||
		(authz.ScopeRef{Level: authz.ScopeTenant, ID: request.tenantID}).Validate(request.tenantID) != nil {
		return false
	}
	if request.clientID != AdminWebClientID && request.clientID != UserWebClientID &&
		request.clientID != CLIClientID && request.clientID != AutomationClientID {
		return false
	}
	if (request.subjectKind == "serviceAccount") != (request.clientID == AutomationClientID) ||
		request.clientID == AdminWebClientID && request.application != api.IdentityApplicationAdmin ||
		request.clientID == UserWebClientID && request.application != api.IdentityApplicationUser {
		return false
	}
	if request.application != api.IdentityApplicationAdmin && request.application != api.IdentityApplicationUser {
		return false
	}
	if request.projectID != "" && (authz.ScopeRef{Level: authz.ScopeProject, ID: request.projectID}).Validate(request.tenantID) != nil {
		return false
	}
	if !validTokenScopes(request.scopes) || request.authority.SecurityEpoch < 1 || request.authority.SecurityEpoch > maximumEpoch {
		return false
	}
	now := request.now.UTC().Unix()
	authorityNotBefore := request.authority.NotBefore.UTC().Unix()
	authorityExpiresAt := request.authority.ExpiresAt.UTC().Unix()
	keyNotBefore := request.authority.KeyNotBefore.UTC().Unix()
	keyNotAfter := request.authority.KeyNotAfter.UTC().Unix()
	return validNumericDate(now) && validNumericDate(authorityNotBefore) && validNumericDate(authorityExpiresAt) &&
		validNumericDate(keyNotBefore) && validNumericDate(keyNotAfter) &&
		authorityNotBefore <= now && now < authorityExpiresAt && authorityExpiresAt-authorityNotBefore <= int64(24*time.Hour/time.Second) &&
		keyNotBefore <= now && now < keyNotAfter
}

func validNumericDate(value int64) bool {
	return value >= 0 && value <= maximumNumericDate
}

func validTokenScopes(scopes []string) bool {
	if len(scopes) == 0 || len(scopes) > maximumTokenScopes {
		return false
	}
	for index, scope := range scopes {
		if len(scope) < 5 || len(scope) > 128 || !tokenScopePattern.MatchString(scope) || index > 0 && scopes[index-1] >= scope {
			return false
		}
	}
	return true
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ"`
}

type tokenClaims struct {
	Issuer        string `json:"iss"`
	Subject       string `json:"sub"`
	Audience      string `json:"aud"`
	ExpiresAt     int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
	TokenID       string `json:"jti"`
	ClientID      string `json:"client_id"`
	Scope         string `json:"scope"`
	SubjectKind   string `json:"https://schemas.cloud-agents.dev/claims/subject-kind"`
	TenantID      string `json:"https://schemas.cloud-agents.dev/claims/tenant-id"`
	ProjectID     string `json:"https://schemas.cloud-agents.dev/claims/project-id,omitempty"`
	SecurityEpoch int64  `json:"https://schemas.cloud-agents.dev/claims/security-epoch"`
	TokenProfile  string `json:"https://schemas.cloud-agents.dev/claims/token-profile"`
}
