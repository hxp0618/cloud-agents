package identitytrust_test

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identitytrust"
)

func TestHTTPFetcherUsesPinnedTLSAndBoundedAuthorityResponse(t *testing.T) {
	const authority = `{"keys":[],"cloudAgentsAuthority":{"issuer":"https://issuer.example","revision":"1","securityEpoch":"1","notBefore":1,"expiresAt":2,"lineage":[]}}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != "application/json" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = writer.Write([]byte(authority))
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())

	body, err := identitytrust.NewHTTPFetcher(roots)(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != authority {
		t.Fatalf("authority body = %q", body)
	}
}

func TestHTTPFetcherRejectsAuthorityProtocolRedirectSizeAndContent(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "redirect", handler: func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, "/other", http.StatusFound)
		}},
		{name: "oversize", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(strings.Repeat("x", (1<<20)+1)))
		}},
		{name: "invalid content type", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("{}"))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(test.handler)
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			if _, err := identitytrust.NewHTTPFetcher(roots)(context.Background(), server.URL); !errors.Is(err, identitytrust.ErrInvalidAuthority) || errors.Is(err, identitytrust.ErrTransportUnavailable) {
				t.Fatalf("unsafe response error = %v", err)
			}
		})
	}
	if _, err := identitytrust.NewHTTPFetcher(nil)(context.Background(), "http://127.0.0.1/.well-known/jwks.json"); !errors.Is(err, identitytrust.ErrInvalidAuthority) || errors.Is(err, identitytrust.ErrTransportUnavailable) {
		t.Fatalf("plain HTTP error = %v", err)
	}
}

func TestHTTPFetcherClassifiesOnlyTransientFailureAsUnavailable(t *testing.T) {
	for _, test := range []struct {
		status      int
		unavailable bool
	}{
		{status: http.StatusServiceUnavailable, unavailable: true},
		{status: http.StatusBadGateway, unavailable: true},
		{status: http.StatusTooManyRequests, unavailable: true},
		{status: http.StatusRequestTimeout, unavailable: true},
		{status: http.StatusNotFound},
		{status: http.StatusForbidden},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/html")
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			_, err := identitytrust.NewHTTPFetcher(roots)(context.Background(), server.URL)
			if errors.Is(err, identitytrust.ErrTransportUnavailable) != test.unavailable || errors.Is(err, identitytrust.ErrInvalidAuthority) == test.unavailable {
				t.Fatalf("status %d error = %v", test.status, err)
			}
		})
	}

	untrusted := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("{}"))
	}))
	defer untrusted.Close()
	if _, err := identitytrust.NewHTTPFetcher(nil)(context.Background(), untrusted.URL); !errors.Is(err, identitytrust.ErrInvalidAuthority) || errors.Is(err, identitytrust.ErrTransportUnavailable) {
		t.Fatalf("untrusted TLS error = %v", err)
	}

	unavailable := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := unavailable.URL
	cert := unavailable.Certificate()
	unavailable.Close()
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	if _, err := identitytrust.NewHTTPFetcher(roots)(context.Background(), url); !errors.Is(err, identitytrust.ErrTransportUnavailable) {
		t.Fatalf("network failure error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := identitytrust.NewHTTPFetcher(roots)(ctx, url); !errors.Is(err, context.Canceled) || errors.Is(err, identitytrust.ErrTransportUnavailable) {
		t.Fatalf("canceled fetch error = %v", err)
	}
}
