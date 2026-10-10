package identityaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
)

const liveStatusTimeout = 5 * time.Second

var (
	ErrInvalidVerifier = errors.New("identity access verifier configuration is invalid")
	ErrTokenRejected   = errors.New("access token rejected")
)

type OfflineVerifier interface {
	Verify(string, authn.VerificationRequest) (*authn.VerifiedPrincipal, error)
}

type TokenStatusChecker interface {
	CheckTokenStatus(context.Context, string, api.TokenStatusRequest) (api.TokenStatus, error)
}

type Verifier struct {
	offline          OfflineVerifier
	status           TokenStatusChecker
	application      api.IdentityApplication
	allowedClientIDs map[string]struct{}
}

func NewVerifier(offline OfflineVerifier, status TokenStatusChecker, application api.IdentityApplication) (*Verifier, error) {
	allowedClientIDs, ok := applicationClientIDs(application)
	if offline == nil || status == nil || !ok {
		return nil, ErrInvalidVerifier
	}
	return &Verifier{offline: offline, status: status, application: application, allowedClientIDs: allowedClientIDs}, nil
}

func (verifier *Verifier) Verify(token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	return verifier.VerifyContext(context.Background(), token, request)
}

func (verifier *Verifier) VerifyContext(ctx context.Context, token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	if verifier == nil || verifier.offline == nil || verifier.status == nil || ctx == nil {
		return nil, ErrTokenRejected
	}
	var principal *authn.VerifiedPrincipal
	var err error
	if offline, ok := verifier.offline.(interface {
		VerifyContext(context.Context, string, authn.VerificationRequest) (*authn.VerifiedPrincipal, error)
	}); ok {
		principal, err = offline.VerifyContext(ctx, token, request)
	} else {
		principal, err = verifier.offline.Verify(token, request)
	}
	if err != nil || principal == nil || ctx.Err() != nil {
		return nil, ErrTokenRejected
	}
	clientID, ok := verifiedTokenClientID(token)
	if _, allowed := verifier.allowedClientIDs[clientID]; !ok || !allowed {
		return nil, ErrTokenRejected
	}
	digest := sha256.Sum256([]byte(token))
	requestID, ok := liveStatusRequestID()
	if !ok {
		return nil, ErrTokenRejected
	}
	expectedProjectID := ""
	if request.ResourceLevel == "project" {
		expectedProjectID = request.ResourceID
	}
	statusCtx, cancel := context.WithTimeout(ctx, liveStatusTimeout)
	defer cancel()
	status, err := verifier.status.CheckTokenStatus(statusCtx, requestID, api.TokenStatusRequest{
		TokenSHA256:         "sha256:" + hex.EncodeToString(digest[:]),
		ExpectedClientID:    clientID,
		ExpectedApplication: verifier.application,
		ExpectedTenantID:    request.TenantID,
		ExpectedProjectID:   expectedProjectID,
	})
	if err != nil || statusCtx.Err() != nil || status.Status != "active" {
		return nil, ErrTokenRejected
	}
	return principal, nil
}

func applicationClientIDs(application api.IdentityApplication) (map[string]struct{}, bool) {
	shared := map[string]struct{}{identity.CLIClientID: {}, identity.AutomationClientID: {}}
	switch application {
	case api.IdentityApplicationAdmin:
		shared[identity.AdminWebClientID] = struct{}{}
		return shared, true
	case api.IdentityApplicationUser:
		shared[identity.UserWebClientID] = struct{}{}
		return shared, true
	default:
		return nil, false
	}
}

func verifiedTokenClientID(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != parts[1] {
		return "", false
	}
	var claims struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.ClientID == "" {
		return "", false
	}
	return claims.ClientID, true
}

func liveStatusRequestID() (string, bool) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", false
	}
	return "token-status-" + hex.EncodeToString(random[:]), true
}
