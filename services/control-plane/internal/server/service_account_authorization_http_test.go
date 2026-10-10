package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type principalTokenAuthorizerFake struct {
	request api.PrincipalTokenAuthorizationRequest
	result  api.PrincipalTokenAuthorization
	err     error
	calls   int
}

func (fake *principalTokenAuthorizerFake) AuthorizePrincipalToken(_ context.Context, request api.PrincipalTokenAuthorizationRequest) (api.PrincipalTokenAuthorization, error) {
	fake.calls++
	fake.request = request
	return fake.result, fake.err
}

func TestIdentityPrincipalAuthorizationHTTPServerUsesDedicatedCredential(t *testing.T) {
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &principalTokenAuthorizerFake{result: api.PrincipalTokenAuthorization{
		PrincipalID: "automation-one",
		Subject:     common.SubjectRef{Kind: "serviceAccount", Issuer: "https://identity.example.test", Subject: "service-automation-one"},
		Issuer:      "https://identity.example.test",
		TenantID:    "tenant-one",
		Application: api.IdentityApplicationAdmin,
		Scopes:      []string{"agents.get"},
	}}
	server, err := NewIdentityPrincipalAuthorizationHTTPServer(authorizer, credential)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"application":"admin","clientId":"cloud-agents-automation","credentialSha256":"sha256:` + strings.Repeat("a", 64) + `","tenantId":"tenant-one"}`
	request := httptest.NewRequest(http.MethodPost, IdentityPrincipalTokenAuthorizationRoute, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "request-one")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || authorizer.calls != 1 || authorizer.request.ClientID != "cloud-agents-automation" || authorizer.request.Application != api.IdentityApplicationAdmin {
		t.Fatalf("status=%d calls=%d request=%#v body=%s", response.Code, authorizer.calls, authorizer.request, response.Body.String())
	}
	decoded, err := api.DecodePrincipalTokenAuthorizationJSON(response.Body.Bytes())
	if err != nil || decoded.PrincipalID != "automation-one" {
		t.Fatalf("response=%#v error=%v", decoded, err)
	}

	wrong := httptest.NewRequest(http.MethodPost, IdentityPrincipalTokenAuthorizationRoute, strings.NewReader(body))
	wrong.Header.Set("Authorization", "Bearer wrong-credential-with-enough-length")
	wrong.Header.Set("Content-Type", "application/json")
	wrong.Header.Set("X-Request-ID", "request-two")
	wrongResponse := httptest.NewRecorder()
	server.ServeHTTP(wrongResponse, wrong)
	if wrongResponse.Code != http.StatusUnauthorized || authorizer.calls != 1 {
		t.Fatalf("wrong credential status=%d calls=%d", wrongResponse.Code, authorizer.calls)
	}
}

func TestIdentityPrincipalAuthorizationHTTPServerPreservesDenialAndAuthorityMismatch(t *testing.T) {
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	body := `{"application":"admin","clientId":"cloud-agents-automation","credentialSha256":"sha256:` + strings.Repeat("b", 64) + `","tenantId":"tenant-one"}`
	for _, test := range []struct {
		name   string
		result api.PrincipalTokenAuthorization
		err    error
		want   int
	}{
		{name: "current denial", err: postgres.ErrTokenAuthorizationDenied, want: http.StatusForbidden},
		{name: "tenant mismatch", result: api.PrincipalTokenAuthorization{
			PrincipalID: "automation-one", Subject: common.SubjectRef{Kind: "serviceAccount", Issuer: "https://identity.example.test", Subject: "service-automation-one"},
			Issuer: "https://identity.example.test", TenantID: "tenant-other", Application: api.IdentityApplicationAdmin, Scopes: []string{"agents.get"},
		}, want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &principalTokenAuthorizerFake{result: test.result, err: test.err}
			server, err := NewIdentityPrincipalAuthorizationHTTPServer(authorizer, credential)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, IdentityPrincipalTokenAuthorizationRoute, strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+credential)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Request-ID", "request-one")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
