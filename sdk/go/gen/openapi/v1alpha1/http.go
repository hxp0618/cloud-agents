package v1alpha1

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

const maxHTTPJSONResponseBytes = 2 * runtimeprotocol.MaxMessageBytes

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
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || strings.HasSuffix(parsed.Path, "/") || client == nil {
		return nil, ErrInvalidHTTPClientConfig
	}
	if parsed.Scheme == "http" {
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return nil, ErrInvalidHTTPClientConfig
		}
	}
	if strings.TrimSpace(bearerToken) != bearerToken || bearerToken == "" || strings.ContainsAny(bearerToken, " \t\r\n") {
		return nil, ErrInvalidHTTPClientConfig
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return NewClient(httpTransport{baseURL: strings.TrimSuffix(baseURL, "/"), bearerToken: bearerToken, client: &clientCopy})
}

type httpTransport struct {
	baseURL     string
	bearerToken string
	client      *http.Client
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
	request.Header.Set("Authorization", "Bearer "+transport.bearerToken)
	response, err := transport.client.Do(request)
	if err != nil {
		return Response{}, err
	}
	defer response.Body.Close()
	maxResponseBytes := maxHTTPJSONResponseBytes
	if response.StatusCode == http.StatusOK && strings.HasSuffix(input.Path, "/artifact") {
		maxResponseBytes = MaxManagedAgentArtifactBytes
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
