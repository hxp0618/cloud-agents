package identitytrust

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const authorityFetchTimeout = 5 * time.Second

// NewHTTPFetcher returns the production transport for the configured authority
// URL. A nil pool uses the host's system roots.
func NewHTTPFetcher(rootCAs *x509.CertPool) FetchOperation {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if rootCAs != nil {
		transport.TLSClientConfig.RootCAs = rootCAs.Clone()
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   authorityFetchTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return func(ctx context.Context, rawURL string) ([]byte, error) {
		if ctx == nil {
			return nil, ErrInvalidAuthority
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || strings.TrimSpace(rawURL) != rawURL {
			return nil, ErrInvalidAuthority
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, ErrInvalidAuthority
		}
		request.Header.Set("Accept", "application/json")
		response, err := client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if networkUnavailable(err) {
				return nil, ErrTransportUnavailable
			}
			return nil, ErrInvalidAuthority
		}
		defer response.Body.Close()
		if transientStatus(response.StatusCode) {
			return nil, ErrTransportUnavailable
		}
		mediaType, _, contentTypeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if response.StatusCode != http.StatusOK || contentTypeErr != nil || mediaType != "application/json" || response.ContentLength > maximumAuthorityDocument {
			return nil, ErrInvalidAuthority
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maximumAuthorityDocument+1))
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil || len(body) == 0 || len(body) > maximumAuthorityDocument {
			return nil, ErrInvalidAuthority
		}
		return body, nil
	}
}

// A restarting identity service or a gateway in front of it answers with these
// statuses; they say nothing about the authority, so the checkpoint stays usable.
func transientStatus(status int) bool {
	return status >= http.StatusInternalServerError || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout
}

func networkUnavailable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var dnsError *net.DNSError
	return errors.As(err, &dnsError)
}
