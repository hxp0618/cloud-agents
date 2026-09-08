package remoteworker

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
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
