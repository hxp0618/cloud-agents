package remoteworker

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"
)

func TestCertificateAuthorityIssuesBoundClientIdentity(t *testing.T) {
	authority, err := NewEphemeralCertificateAuthority("remote-worker.test")
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requestedURI, _ := url.Parse("spiffe://attacker.invalid/admin")
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "attacker"}, URIs: []*url.URL{requestedURI}}, key)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := authority.Issue(CertificateInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, EnrollmentID: "enrollment-remote-1",
		IncarnationID: "incarnation-remote-1", CSRPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})),
	})
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(issued.ChainPEM))
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if issued.SPIFFEID != "spiffe://remote-worker.test/remote-worker/tenant/project/enrollment-remote-1/incarnation-remote-1" || len(certificate.URIs) != 1 || certificate.URIs[0].String() != issued.SPIFFEID || len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth || certificate.NotAfter.Sub(certificate.NotBefore) != CertificateLifetime+time.Minute || !certificate.PublicKey.(*ecdsa.PublicKey).Equal(&key.PublicKey) {
		t.Fatalf("issued certificate escaped authority: %+v", issued)
	}
	csr[len(csr)-1] ^= 1
	if _, err := authority.Issue(CertificateInput{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, EnrollmentID: "enrollment-remote-1", IncarnationID: "incarnation-remote-1", CSRPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))}); err == nil {
		t.Fatal("tampered CSR accepted")
	}
	csr[len(csr)-1] ^= 1
	authority.certificate.NotAfter = authority.clock().Add(CertificateLifetime - time.Second)
	if _, err := authority.Issue(CertificateInput{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, EnrollmentID: "enrollment-remote-1", IncarnationID: "incarnation-remote-1", CSRPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))}); err == nil {
		t.Fatal("certificate outliving its CA was issued")
	}
}

func TestCertificateAuthorityTrustsOverlappingRootsAndSignsWithFirst(t *testing.T) {
	oldCertificate, _ := testCertificateAuthorityPEM(t, "old")
	newCertificate, newKey := testCertificateAuthorityPEM(t, "new")
	overlap := append(append([]byte{}, newCertificate...), oldCertificate...)
	authority, err := NewCertificateAuthority(overlap, newKey, "remote-worker.test")
	if err != nil {
		t.Fatal(err)
	}
	request, _, err := NewCertificateIssueRequest("enrollment-remote-1", "incarnation-remote-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := authority.Issue(CertificateInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, EnrollmentID: "enrollment-remote-1",
		IncarnationID: "incarnation-remote-1", CSRPEM: request.CertificateSigningRequestPEM,
	})
	if err != nil {
		t.Fatal(err)
	}
	leafBlock, _ := pem.Decode([]byte(issued.ChainPEM))
	leaf, err := x509.ParseCertificate(leafBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := authority.ClientCAPool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("new signing root was not trusted: %v", err)
	}
	oldRoot, _ := pem.Decode(oldCertificate)
	oldParsed, err := x509.ParseCertificate(oldRoot.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldParsed.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		t.Fatalf("old root was not trusted during overlap: %v", err)
	}
	newOnly, err := NewCertificateAuthority(newCertificate, newKey, "remote-worker.test")
	if err != nil {
		t.Fatal(err)
	}
	newOnlyRoots, err := newOnly.ClientCAPool()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldParsed.Verify(x509.VerifyOptions{Roots: newOnlyRoots}); err == nil {
		t.Fatal("old root remained trusted after overlap removal")
	}
	newRoot, _ := pem.Decode(newCertificate)
	newParsed, err := x509.ParseCertificate(newRoot.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.CheckSignatureFrom(newParsed); err != nil {
		t.Fatalf("certificate was not signed by the first root: %v", err)
	}
}

func testCertificateAuthorityPEM(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyRaw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyRaw})
}

func TestCertificateIssueRequestBindsPrivateKey(t *testing.T) {
	request, privateKey, err := NewCertificateIssueRequest("enrollment-alpha", "incarnation-alpha", 3)
	if err != nil || ValidateCertificateIssuePrivateKey(request, privateKey) != nil {
		t.Fatalf("generated request/key rejected: %v", err)
	}
	_, otherPrivateKey, err := NewCertificateIssueRequest("enrollment-alpha", "incarnation-alpha", 3)
	if err != nil || ValidateCertificateIssuePrivateKey(request, otherPrivateKey) == nil {
		t.Fatal("request accepted an unrelated private key")
	}
}
