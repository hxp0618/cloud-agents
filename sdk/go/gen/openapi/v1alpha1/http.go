package v1alpha1

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

const (
	maxHTTPJSONResponseBytes              = 2 * runtimeprotocol.MaxMessageBytes
	maxRemoteWorkerHeartbeatResponseBytes = 24 << 20
)

var ErrInvalidHTTPClientConfig = errors.New("invalid Cloud Agents HTTP client configuration")

// NewHTTPClient creates the public SDK client for a Cloud Agents Control
// Plane endpoint. Plain HTTP is accepted only for literal loopback addresses.
// The bearer is attached by the transport, never by an operation caller, and
// redirects are disabled to prevent credential leaks.
func NewHTTPClient(baseURL, bearerToken string) (*Client, error) {
	return NewHTTPClientWithClient(baseURL, bearerToken, &http.Client{})
}

// NewHTTPClientWithClient creates the public SDK client with a caller-provided
// HTTP client. Redirects remain disabled so bearer credentials cannot leak.
func NewHTTPClientWithClient(baseURL, bearerToken string, client *http.Client) (*Client, error) {
	return newAuthorizedHTTPClient(baseURL, "Bearer "+bearerToken, bearerToken, client)
}

// NewIdentityServiceHTTPClient creates the server-only identity client. The
// configured service credential is attached by the transport and cannot be
// replaced by an operation caller.
func NewIdentityServiceHTTPClient(baseURL, serviceCredential string) (*IdentityServiceClient, error) {
	return NewIdentityServiceHTTPClientWithClient(baseURL, serviceCredential, &http.Client{})
}

// NewIdentityServiceHTTPClientWithClient creates the server-only identity
// client with a caller-provided HTTP client. It preserves the public client's
// endpoint, credential, response-size, and redirect checks.
func NewIdentityServiceHTTPClientWithClient(baseURL, serviceCredential string, client *http.Client) (*IdentityServiceClient, error) {
	base, err := newAuthorizedHTTPClient(baseURL, "Bearer "+serviceCredential, serviceCredential, client)
	if err != nil {
		return nil, err
	}
	return NewIdentityServiceClient(base.transport)
}

// NewIdentityAuthorizationHTTPClient creates the dedicated Identity Service to
// Control Plane authorization client. Its service credential is transport
// configuration and cannot be supplied or replaced by an operation caller.
func NewIdentityAuthorizationHTTPClient(baseURL, serviceCredential string) (*IdentityAuthorizationClient, error) {
	return NewIdentityAuthorizationHTTPClientWithClient(baseURL, serviceCredential, &http.Client{})
}

// NewIdentityAuthorizationHTTPClientWithClient creates the dedicated
// authorization client with a caller-provided HTTP client. It preserves the
// endpoint, credential, response-size, and redirect checks.
func NewIdentityAuthorizationHTTPClientWithClient(baseURL, serviceCredential string, client *http.Client) (*IdentityAuthorizationClient, error) {
	base, err := newAuthorizedHTTPClient(baseURL, "Bearer "+serviceCredential, serviceCredential, client)
	if err != nil {
		return nil, err
	}
	return NewIdentityAuthorizationClient(base.transport)
}

func NewCLIIdentityHTTPClient(baseURL string) (*CLIIdentityClient, error) {
	return NewCLIIdentityHTTPClientWithClient(baseURL, &http.Client{})
}

// NewCLIIdentityHTTPClientWithClient creates the public CLI-to-Web-BFF client.
// Start and exchange are anonymous; grant-authenticated operations attach their
// credential at the individual method boundary.
func NewCLIIdentityHTTPClientWithClient(baseURL string, client *http.Client) (*CLIIdentityClient, error) {
	base, err := newHTTPClient(baseURL, "", true, client)
	if err != nil {
		return nil, err
	}
	return NewCLIIdentityClient(base.transport)
}

// NewRemoteWorkerBootstrapHTTPClient creates a client authenticated only by a
// one-time enrollment secret. The server accepts it solely on bootstrap routes.
func NewRemoteWorkerBootstrapHTTPClient(baseURL, enrollmentSecret string) (*Client, error) {
	return NewRemoteWorkerBootstrapHTTPClientWithClient(baseURL, enrollmentSecret, &http.Client{})
}

func NewRemoteWorkerBootstrapHTTPClientWithClient(baseURL, enrollmentSecret string, client *http.Client) (*Client, error) {
	if !validEnrollmentSecret(enrollmentSecret) {
		return nil, ErrInvalidHTTPClientConfig
	}
	return newAuthorizedHTTPClient(baseURL, "RemoteWorkerEnrollment "+enrollmentSecret, enrollmentSecret, client)
}

// NewRemoteWorkerMTLSHTTPClientWithClient creates a node client whose caller-
// provided transport owns the short-lived client certificate. No bearer or
// enrollment secret is attached, and HTTPS is mandatory.
func NewRemoteWorkerMTLSHTTPClientWithClient(baseURL string, client *http.Client) (*Client, error) {
	return newHTTPClient(baseURL, "", true, client)
}

func newAuthorizedHTTPClient(baseURL, authorization, credential string, client *http.Client) (*Client, error) {
	if strings.TrimSpace(credential) != credential || credential == "" || strings.ContainsAny(credential, " \t\r\n") {
		return nil, ErrInvalidHTTPClientConfig
	}
	return newHTTPClient(baseURL, authorization, false, client)
}

func newHTTPClient(baseURL, authorization string, requireTLS bool, client *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || strings.HasSuffix(parsed.Path, "/") || client == nil {
		return nil, ErrInvalidHTTPClientConfig
	}
	if parsed.Scheme == "http" {
		ip := net.ParseIP(parsed.Hostname())
		if requireTLS || ip == nil || !ip.IsLoopback() {
			return nil, ErrInvalidHTTPClientConfig
		}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return NewClient(httpTransport{baseURL: strings.TrimSuffix(baseURL, "/"), authorization: authorization, client: &clientCopy})
}

type httpTransport struct {
	baseURL       string
	authorization string
	client        *http.Client
}

func (transport httpTransport) RoundTrip(ctx context.Context, input Request) (Response, error) {
	if ctx == nil || transport.client == nil || transport.baseURL == "" || input.Method == "" || !strings.HasPrefix(input.Path, "/") || strings.ContainsAny(input.Path, "\r\n") {
		return Response{}, ErrInvalidHTTPClientConfig
	}
	request, err := http.NewRequestWithContext(ctx, input.Method, transport.baseURL+input.Path, bytes.NewReader(input.Body))
	if err != nil {
		return Response{}, err
	}
	for name, value := range input.Headers {
		request.Header.Set(name, value)
	}
	if len(input.Body) != 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if transport.authorization != "" {
		request.Header.Set("Authorization", transport.authorization)
	}
	response, err := transport.client.Do(request)
	if err != nil {
		return Response{}, err
	}
	defer response.Body.Close()
	maxResponseBytes := maxHTTPJSONResponseBytes
	if response.StatusCode == http.StatusOK && strings.HasSuffix(input.Path, "/artifact") {
		maxResponseBytes = MaxManagedAgentArtifactBytes
	} else if response.StatusCode == http.StatusOK && input.Method == http.MethodPost &&
		strings.HasPrefix(input.Path, "/v1/remote-workers/tenants/") && strings.HasSuffix(input.Path, ":heartbeat") {
		maxResponseBytes = maxRemoteWorkerHeartbeatResponseBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maxResponseBytes)+1))
	if err != nil {
		return Response{}, err
	}
	if len(body) > maxResponseBytes {
		return Response{}, errors.New("Cloud Agents HTTP response exceeds the SDK limit")
	}
	headers := make(map[string]string, len(response.Header))
	for name, values := range response.Header {
		if len(values) != 0 {
			headers[name] = values[0]
		}
	}
	return Response{Status: response.StatusCode, Headers: headers, Body: body}, nil
}

func validEnrollmentSecret(secret string) bool {
	if !strings.HasPrefix(secret, "carw1_") || len(secret) != 49 {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(secret[6:])
	return err == nil && len(raw) == 32
}
