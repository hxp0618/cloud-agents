package worker

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"strings"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
)

var errTLSIdentityMissing = errors.New("worker/transport_identity_missing")

type tlsIdentityContextKey struct{}

// TLSIdentityProvider reads the peer identity installed by NewTLSHandler.
// The identity is derived from a certificate already verified by tls.Config;
// request fields never participate in authentication.
type TLSIdentityProvider struct{}

func (TLSIdentityProvider) ClientIdentity(ctx context.Context) (*workerv1alpha1.WorkloadIdentity, error) {
	if ctx == nil {
		return nil, errTLSIdentityMissing
	}
	identity, ok := ctx.Value(tlsIdentityContextKey{}).(*workerv1alpha1.WorkloadIdentity)
	if !ok || identity == nil {
		return nil, errTLSIdentityMissing
	}
	return cloneIdentity(identity), nil
}

// NewTLSHandler binds a verified TLS client certificate to the Worker request
// context. It rejects cleartext or unverified requests before the Connect
// handler can process a message.
func NewTLSHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if next == nil || request == nil || request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.PeerCertificates) == 0 {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		identity, err := PeerIdentityFromCertificate(request.TLS.PeerCertificates[0])
		if err != nil {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(request.Context(), tlsIdentityContextKey{}, identity)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// PeerIdentityFromCertificate derives the verified peer identity used by the
// Worker transport and by Supervisor server-certificate verification.
func PeerIdentityFromCertificate(certificate *x509.Certificate) (*workerv1alpha1.WorkloadIdentity, error) {
	if certificate == nil || len(certificate.URIs) != 1 {
		return nil, errTLSIdentityMissing
	}
	spiffe := certificate.URIs[0]
	if spiffe.Scheme != "spiffe" || spiffe.Host == "" || spiffe.Path == "" || strings.Contains(spiffe.Host, "/") || spiffe.RawQuery != "" || spiffe.Fragment != "" || spiffe.User != nil {
		return nil, errTLSIdentityMissing
	}
	if _, err := url.ParseRequestURI(spiffe.String()); err != nil {
		return nil, errTLSIdentityMissing
	}
	digest := sha256.Sum256(certificate.Raw)
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: spiffe.String(), TrustDomain: spiffe.Host, LeafCertificateSha256: digest[:]}
	if err := validateIdentity(identity); err != nil {
		return nil, errTLSIdentityMissing
	}
	return identity, nil
}
