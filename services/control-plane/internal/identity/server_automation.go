package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

var ErrAutomationInvalid = errors.New("identity automation request invalid")

const (
	automationTenantTokenRoute = "/v1/identity/automation/tenant-token"
	automationCredentialHeader = "X-Cloud-Agents-Automation-Credential"
)

type automationHTTPStore interface {
	IssueAutomationTenantToken(context.Context, api.IdentityApplication, [sha256.Size]byte, api.TenantTokenIssueRequest) (api.TenantToken, error)
}

func (server *Server) automationOperation(
	ctx context.Context,
	request *http.Request,
	body []byte,
	application api.IdentityApplication,
) ([]byte, int, error) {
	if server.automation == nil || !validApplication(application) {
		return nil, 0, ErrForbidden
	}
	if request.Method != http.MethodPost {
		return nil, 0, errIdentityRouteNotFound
	}
	credentialDigest, err := exactAutomationCredentialDigest(request)
	if err != nil {
		return nil, 0, browserauth.ErrUnauthorized
	}
	input, err := api.DecodeTenantTokenIssueRequestJSON(body)
	if err != nil {
		return nil, 0, ErrAutomationInvalid
	}
	result, err := server.automation.IssueAutomationTenantToken(ctx, application, credentialDigest, input)
	if err != nil {
		return nil, 0, err
	}
	encoded, err := api.EncodeTenantTokenJSON(result)
	return encoded, http.StatusOK, err
}

func exactAutomationCredentialDigest(request *http.Request) ([sha256.Size]byte, error) {
	if len(request.Header.Values(automationCredentialHeader)) != 1 {
		return [sha256.Size]byte{}, browserauth.ErrUnauthorized
	}
	return browserauth.ProofDigest(request.Header.Get(automationCredentialHeader))
}
