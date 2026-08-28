package worker

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
)

func TestTLSIdentityProviderRequiresHandlerBoundIdentity(t *testing.T) {
	provider := TLSIdentityProvider{}
	if _, err := provider.ClientIdentity(context.Background()); err == nil {
		t.Fatal("missing TLS identity was accepted")
	}
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://example.test/worker", TrustDomain: "example.test"}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(context.WithValue(request.Context(), tlsIdentityContextKey{}, identity))
	got, err := provider.ClientIdentity(request.Context())
	if err != nil || got.GetSpiffeId() != identity.GetSpiffeId() || got == identity {
		t.Fatalf("identity=%#v err=%v", got, err)
	}
}

func TestTLSHandlerRejectsUnverifiedRequests(t *testing.T) {
	called := false
	handler := NewTLSHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
	if _, err := PeerIdentityFromCertificate(&x509.Certificate{}); err == nil {
		t.Fatal("certificate without SPIFFE URI was accepted")
	}
}

func TestTLSHandlerBindsIdentityAcrossVerifiedCertificateChain(t *testing.T) {
	caCert, caKey := newTestCA(t)
	serverCert, _ := newTestLeaf(t, caCert, caKey, "spiffe://cloud-agents.example/worker", true)
	clientCert, clientLeaf := newTestLeaf(t, caCert, caKey, "spiffe://cloud-agents.example/supervisor", false)

	seen := make(chan *workerv1alpha1.WorkloadIdentity, 1)
	handler := NewTLSHandler(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		identity, err := (TLSIdentityProvider{}).ClientIdentity(request.Context())
		if err != nil {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		seen <- identity
		response.WriteHeader(http.StatusNoContent)
	}))
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool(caCert),
	}
	server.StartTLS()
	defer server.Close()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:   tls.VersionTLS13,
		RootCAs:      certPool(caCert),
		Certificates: []tls.Certificate{clientCert},
		ServerName:   "example.test",
	}}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	identity := <-seen
	want := "spiffe://cloud-agents.example/supervisor"
	if identity.GetSpiffeId() != want || identity.GetTrustDomain() != "cloud-agents.example" || string(identity.GetLeafCertificateSha256()) != string(leafDigest(clientLeaf)) {
		t.Fatalf("identity = %#v, want SPIFFE=%q and client leaf digest", identity, want)
	}
}

func newTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cloud-agents test CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key
}

func newTestLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, identity string, server bool) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(identity)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(int64(len(identity) + 1)), Subject: pkix.Name{CommonName: identity}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"example.test"}, URIs: []*url.URL{uri}, KeyUsage: x509.KeyUsageDigitalSignature}
	if server {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}, certificate
}

func certPool(certificate *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	return pool
}

func leafDigest(certificate *x509.Certificate) []byte {
	digest := sha256.Sum256(certificate.Raw)
	return digest[:]
}
