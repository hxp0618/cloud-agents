package browserauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	providerHTTPTimeout      = 10 * time.Second
	maximumProviderResponse  = 128 << 10
	maximumAuthorizationCode = 4096
)

var (
	errInvalidProvider        = errors.New("invalid login provider")
	errInvalidProviderRequest = errors.New("invalid login provider request")
	errProviderResponse       = errors.New("invalid login provider response")
)

type providerRoundTripper struct{ base http.RoundTripper }

func (transport providerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.URL.Scheme != "https" || request.URL.Host == "" || request.URL.User != nil {
		return nil, errProviderResponse
	}
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > maximumProviderResponse {
		response.Body.Close()
		return nil, errProviderResponse
	}
	response.Body = &boundedProviderBody{reader: io.LimitReader(response.Body, maximumProviderResponse+1), closer: response.Body, remaining: maximumProviderResponse}
	return response, nil
}

type boundedProviderBody struct {
	reader    io.Reader
	closer    io.Closer
	remaining int64
}

func (body *boundedProviderBody) Read(target []byte) (int, error) {
	if body.remaining == 0 {
		var extra [1]byte
		if n, err := body.reader.Read(extra[:]); n > 0 || err == nil {
			return 0, errProviderResponse
		} else {
			return 0, err
		}
	}
	if int64(len(target)) > body.remaining {
		target = target[:body.remaining]
	}
	n, err := body.reader.Read(target)
	body.remaining -= int64(n)
	return n, err
}

func (body *boundedProviderBody) Close() error { return body.closer.Close() }

func newProviderHTTPClient(configured *http.Client) (*http.Client, error) {
	base := http.DefaultTransport
	if configured != nil && configured.Transport != nil {
		base = configured.Transport
	}
	if base == nil {
		return nil, errInvalidProvider
	}
	return &http.Client{
		Transport: providerRoundTripper{base: base},
		Timeout:   providerHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func postProviderJSON(ctx context.Context, client *http.Client, endpoint string, requestBody, responseBody any) error {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return errProviderResponse
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errProviderResponse
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	return doProviderJSON(client, request, responseBody)
}

func getProviderJSON(ctx context.Context, client *http.Client, endpoint string, headers http.Header, responseBody any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errProviderResponse
	}
	request.Header.Set("Accept", "application/json")
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	return doProviderJSON(client, request, responseBody)
}

func doProviderJSON(client *http.Client, request *http.Request, target any) error {
	response, err := client.Do(request)
	if err != nil {
		return ErrUnauthorized
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrUnauthorized
	}
	mediaType, _, contentTypeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if len(response.Header.Values("Content-Type")) != 1 || contentTypeErr != nil || mediaType != "application/json" {
		return ErrUnauthorized
	}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(target); err != nil {
		return ErrUnauthorized
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ErrUnauthorized
	}
	return nil
}

func validProof(value string) bool {
	_, err := ProofDigest(value)
	return err == nil
}

func validAuthorizationCode(code string) bool {
	if code == "" || len(code) > maximumAuthorizationCode || !utf8.ValidString(code) || strings.TrimSpace(code) != code {
		return false
	}
	for _, character := range code {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func validProviderIdentifier(value string) bool {
	if value == "" || len(value) > 512 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func validScopes(scopes []string) bool {
	if len(scopes) > 32 {
		return false
	}
	for _, scope := range scopes {
		if !validProviderIdentifier(scope) || len(scope) > 128 {
			return false
		}
	}
	return true
}

func uniqueScopes(scopes []string) []string {
	result := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if _, exists := seen[scope]; exists {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	return result
}

func providerEmail(assertion string, verified bool) (string, bool) {
	if assertion == "" {
		return "", false
	}
	domain, err := NormalizeEmailDomain(assertion)
	if err != nil {
		return "", false
	}
	return assertion[:strings.LastIndexByte(assertion, '@')+1] + domain, verified
}

func queryURL(endpoint string, values url.Values) string {
	parsed, _ := url.Parse(endpoint)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}
