package controlplane_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/targetcredential"
)

func TestCredentialDirectoryProbesKubernetesVersion(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/version" || request.Header.Get("Authorization") != "Bearer service-account-token" {
			t.Fatalf("request path=%q authorization=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{"major":"1","minor":"34+","gitVersion":"v1.34.2","platform":"linux/arm64"}`))
	}))
	defer server.Close()
	directory := t.TempDir()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(directory, "cluster-alpha.ca.crt"), certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "cluster-alpha.token"), []byte("service-account-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials, err := kubernetestarget.NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	result, err := credentials.Probe(context.Background(), server.URL, "cluster-alpha")
	if err != nil || result.APIVersion != "1.34" || result.EngineVersion != "v1.34.2" || result.OS != "linux" || result.Architecture != "arm64" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := credentials.Probe(context.Background(), server.URL, "missing"); !errors.Is(err, kubernetestarget.ErrCredentialUnavailable) {
		t.Fatalf("missing credential error=%v", err)
	}
}

type sealedKubernetesCredentialFake struct {
	keyID  string
	sealed []byte
}

func (fake *sealedKubernetesCredentialFake) LoadDeploymentTargetCredential(_ context.Context, tenantID, projectID, targetID string) (string, []byte, bool, error) {
	if fake.sealed == nil || tenantID != "tenant-alpha" || projectID != "project-alpha" || targetID != "target-alpha" {
		return "", nil, false, nil
	}
	return fake.keyID, fake.sealed, true, nil
}

func TestStoredKubernetesCredentialUsesClientCertificateAndFailsClosed(t *testing.T) {
	clientCertificate, clientKey := selfSignedPEM(t, "kubeconfig-admin")
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" || len(request.TLS.PeerCertificates) != 1 || request.TLS.PeerCertificates[0].Subject.CommonName != "kubeconfig-admin" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write([]byte(`{"major":"1","minor":"33","gitVersion":"v1.33.1","platform":"linux/amd64"}`))
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()
	authority := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	encode := base64.StdEncoding.EncodeToString
	if _, err := kubernetestarget.NewStoredCredential(authority, "token", encode(clientCertificate), encode(clientKey)); !errors.Is(err, kubernetestarget.ErrCredentialInvalid) {
		t.Fatalf("token plus client certificate error=%v", err)
	}
	if _, err := kubernetestarget.NewStoredCredential(encode([]byte("not a certificate")), "token", "", ""); !errors.Is(err, kubernetestarget.ErrCredentialInvalid) {
		t.Fatalf("invalid authority error=%v", err)
	}
	stored, err := kubernetestarget.NewStoredCredential(authority, "", encode(clientCertificate), encode(clientKey))
	if err != nil {
		t.Fatal(err)
	}
	directory, err := kubernetestarget.NewCredentialDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := targetcredential.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := &sealedKubernetesCredentialFake{}
	directory.UseSealedCredentials(store, keyring)
	sealed, err := directory.Seal("tenant-alpha", "project-alpha", "target-alpha", stored)
	if err != nil || bytes.Contains(sealed.Sealed, []byte("PRIVATE KEY")) {
		t.Fatalf("seal error=%v", err)
	}
	store.keyID, store.sealed = sealed.KeyID, sealed.Sealed
	bound, err := directory.ForTarget(context.Background(), "tenant-alpha", "project-alpha", "target-alpha")
	if err != nil {
		t.Fatal(err)
	}
	result, err := bound.Probe(context.Background(), server.URL, "cluster-alpha")
	if err != nil || result.EngineVersion != "v1.33.1" || result.Architecture != "amd64" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	// Targets without a stored credential keep using the credential directory.
	unbound, err := directory.ForTarget(context.Background(), "tenant-alpha", "project-alpha", "target-beta")
	if err != nil || unbound != directory {
		t.Fatalf("unbound=%p directory=%p err=%v", unbound, directory, err)
	}
	directory.UseSealedCredentials(store, nil)
	if _, err := directory.ForTarget(context.Background(), "tenant-alpha", "project-alpha", "target-alpha"); !errors.Is(err, kubernetestarget.ErrCredentialUnavailable) {
		t.Fatalf("missing key error=%v", err)
	}
	if _, err := directory.Seal("tenant-alpha", "project-alpha", "target-alpha", stored); !errors.Is(err, kubernetestarget.ErrCredentialUnavailable) {
		t.Fatalf("seal without key error=%v", err)
	}
}

func selfSignedPEM(t *testing.T, commonName string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: commonName}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
