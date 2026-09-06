package remoteworker

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net/url"
	"strings"
	"time"
)

const CertificateLifetime = 15 * time.Minute

var (
	ErrInvalidCertificateRequest       = errors.New("remote worker certificate request is invalid")
	ErrCertificateAuthorityUnavailable = errors.New("remote worker certificate authority is unavailable")
)

type CertificateAuthority struct {
	certificate *x509.Certificate
	signer      crypto.Signer
	chainPEM    string
	trustDomain string
	clock       func() time.Time
}

type CertificateInput struct {
	Scope         Scope
	EnrollmentID  string
	IncarnationID string
	CSRPEM        string
}

type Certificate struct {
	IncarnationID string
	SPIFFEID      string
	ChainPEM      string
	CSRSHA256     string
	SHA256        string
	Serial        string
	NotBefore     time.Time
	NotAfter      time.Time
}

type PeerIdentity struct {
	Scope             Scope
	EnrollmentID      string
	IncarnationID     string
	CertificateSHA256 string
}

func NewCertificateAuthority(certificatePEM, privateKeyPEM []byte, trustDomain string) (*CertificateAuthority, error) {
	identity, err := url.Parse("spiffe://" + trustDomain + "/remote-worker")
	if err != nil || trustDomain == "" || len(trustDomain) > 255 || trustDomain != strings.ToLower(trustDomain) || identity.Host != trustDomain || identity.Hostname() != trustDomain || identity.User != nil || identity.RawQuery != "" || identity.Fragment != "" || strings.Contains(trustDomain, "/") {
		return nil, ErrInvalidCertificateRequest
	}
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return nil, ErrInvalidCertificateRequest
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	signer, ok := pair.PrivateKey.(crypto.Signer)
	now := time.Now()
	if err != nil || !ok || !certificate.IsCA || certificate.KeyUsage&x509.KeyUsageCertSign == 0 || certificate.NotBefore.After(now) || !certificate.NotAfter.After(now) {
		return nil, ErrInvalidCertificateRequest
	}
	var chain strings.Builder
	for _, raw := range pair.Certificate {
		if err := pem.Encode(&chain, &pem.Block{Type: "CERTIFICATE", Bytes: raw}); err != nil {
			return nil, ErrInvalidCertificateRequest
		}
	}
	if chain.Len() > 32768 {
		return nil, ErrInvalidCertificateRequest
	}
	return &CertificateAuthority{certificate: certificate, signer: signer, chainPEM: chain.String(), trustDomain: trustDomain, clock: time.Now}, nil
}

func NewEphemeralCertificateAuthority(trustDomain string) (*CertificateAuthority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: trustDomain + " RemoteWorker localdev CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyRaw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return NewCertificateAuthority(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyRaw}), trustDomain)
}

func (authority *CertificateAuthority) ClientCAPool() (*x509.CertPool, error) {
	if authority == nil || authority.chainPEM == "" {
		return nil, ErrCertificateAuthorityUnavailable
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(authority.chainPEM)) {
		return nil, ErrCertificateAuthorityUnavailable
	}
	return pool, nil
}

func (authority *CertificateAuthority) Issue(input CertificateInput) (Certificate, error) {
	if authority == nil || authority.certificate == nil || authority.signer == nil || authority.clock == nil ||
		invalidIdentifier(input.Scope.TenantID) || invalidIdentifier(input.Scope.ProjectID) || invalidIdentifier(input.EnrollmentID) || invalidIdentifier(input.IncarnationID) || len(input.CSRPEM) == 0 || len(input.CSRPEM) > 32768 {
		return Certificate{}, ErrInvalidCertificateRequest
	}
	block, rest := pem.Decode([]byte(input.CSRPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(block.Headers) != 0 || len(strings.TrimSpace(string(rest))) != 0 {
		return Certificate{}, ErrInvalidCertificateRequest
	}
	request, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || request.CheckSignature() != nil || !supportedPublicKey(request.PublicKey) {
		return Certificate{}, ErrInvalidCertificateRequest
	}
	now := authority.clock().UTC()
	if now.Before(authority.certificate.NotBefore) || authority.certificate.NotAfter.Before(now.Add(CertificateLifetime)) {
		return Certificate{}, ErrCertificateAuthorityUnavailable
	}
	identity, err := url.Parse("spiffe://" + authority.trustDomain + "/remote-worker/" + input.Scope.TenantID + "/" + input.Scope.ProjectID + "/" + input.EnrollmentID + "/" + input.IncarnationID)
	if err != nil {
		return Certificate{}, ErrInvalidCertificateRequest
	}
	serial, err := randomSerial()
	if err != nil {
		return Certificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: input.EnrollmentID},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(CertificateLifetime),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true, URIs: []*url.URL{identity},
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, authority.certificate, request.PublicKey, authority.signer)
	if err != nil {
		return Certificate{}, err
	}
	digest := sha256.Sum256(raw)
	csrDigest := sha256.Sum256(block.Bytes)
	chain := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})) + authority.chainPEM
	if len(chain) > 32768 {
		return Certificate{}, ErrInvalidCertificateRequest
	}
	return Certificate{IncarnationID: input.IncarnationID, SPIFFEID: identity.String(), ChainPEM: chain, CSRSHA256: "sha256:" + hex.EncodeToString(csrDigest[:]), SHA256: "sha256:" + hex.EncodeToString(digest[:]), Serial: serial.Text(16), NotBefore: template.NotBefore, NotAfter: template.NotAfter}, nil
}

func (authority *CertificateAuthority) PeerIdentity(state *tls.ConnectionState) (PeerIdentity, error) {
	if authority == nil || authority.clock == nil || state == nil || len(state.PeerCertificates) < 1 || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 || !state.PeerCertificates[0].Equal(state.VerifiedChains[0][0]) {
		return PeerIdentity{}, ErrInvalidCertificateRequest
	}
	certificate := state.PeerCertificates[0]
	now := authority.clock().UTC()
	if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) || len(certificate.URIs) != 1 || len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		return PeerIdentity{}, ErrInvalidCertificateRequest
	}
	identity := certificate.URIs[0]
	parts := strings.Split(strings.TrimPrefix(identity.Path, "/"), "/")
	if identity.Scheme != "spiffe" || identity.Host != authority.trustDomain || identity.User != nil || identity.RawPath != "" || identity.RawQuery != "" || identity.Fragment != "" || len(parts) != 5 || parts[0] != "remote-worker" || invalidIdentifier(parts[1]) || invalidIdentifier(parts[2]) || invalidIdentifier(parts[3]) || invalidIdentifier(parts[4]) {
		return PeerIdentity{}, ErrInvalidCertificateRequest
	}
	digest := sha256.Sum256(certificate.Raw)
	return PeerIdentity{Scope: Scope{TenantID: parts[1], ProjectID: parts[2]}, EnrollmentID: parts[3], IncarnationID: parts[4], CertificateSHA256: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

func BootstrapActorDigest(secretDigest string) (string, error) {
	if !digest(secretDigest) {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256([]byte("remote-worker-bootstrap:" + secretDigest))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func supportedPublicKey(value any) bool {
	switch key := value.(type) {
	case *ecdsa.PublicKey:
		return key.Curve == elliptic.P256() || key.Curve == elliptic.P384()
	case *rsa.PublicKey:
		return key.N.BitLen() >= 2048 && key.N.BitLen() <= 4096 && key.E >= 65537
	default:
		return false
	}
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil || serial.Sign() == 0 {
		return nil, errors.New("remote worker certificate serial generation failed")
	}
	return serial, nil
}
