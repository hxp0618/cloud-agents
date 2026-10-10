package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/targetcredential"
)

// StoredCredential is the static connection an administrator selected from a
// kubeconfig context. It is sealed before persistence and never logged.
type StoredCredential struct {
	CertificateAuthority []byte `json:"certificateAuthority"`
	Token                string `json:"token,omitempty"`
	ClientCertificate    []byte `json:"clientCertificate,omitempty"`
	ClientKey            []byte `json:"clientKey,omitempty"`
}

func (StoredCredential) String() string         { return "StoredCredential{redacted}" }
func (value StoredCredential) GoString() string { return value.String() }

// NewStoredCredential decodes the base64 kubeconfig fields and rejects
// material that cannot form a TLS connection.
func NewStoredCredential(certificateAuthorityData, token, clientCertificateData, clientKeyData string) (StoredCredential, error) {
	var value StoredCredential
	var err error
	if value.CertificateAuthority, err = base64.StdEncoding.Strict().DecodeString(certificateAuthorityData); err != nil {
		return StoredCredential{}, ErrCredentialInvalid
	}
	value.Token = token
	if clientCertificateData != "" || clientKeyData != "" {
		if value.ClientCertificate, err = base64.StdEncoding.Strict().DecodeString(clientCertificateData); err != nil {
			return StoredCredential{}, ErrCredentialInvalid
		}
		if value.ClientKey, err = base64.StdEncoding.Strict().DecodeString(clientKeyData); err != nil {
			return StoredCredential{}, ErrCredentialInvalid
		}
	}
	if _, err := value.connection(); err != nil {
		return StoredCredential{}, err
	}
	return value, nil
}

func (value StoredCredential) Marshal() ([]byte, error) {
	if _, err := value.connection(); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func parseStoredCredential(plaintext []byte) (StoredCredential, error) {
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var value StoredCredential
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return StoredCredential{}, ErrCredentialInvalid
	}
	if _, err := value.connection(); err != nil {
		return StoredCredential{}, err
	}
	return value, nil
}

func (value StoredCredential) connection() (connection, error) {
	roots := x509.NewCertPool()
	hasToken, hasCertificate := value.Token != "", len(value.ClientCertificate) > 0 || len(value.ClientKey) > 0
	if !roots.AppendCertsFromPEM(value.CertificateAuthority) || hasToken == hasCertificate ||
		hasToken && (len(value.Token) > 16384 || strings.TrimSpace(value.Token) != value.Token || strings.IndexFunc(value.Token, func(character rune) bool { return character < 0x21 || character > 0x7e }) >= 0) {
		return connection{}, ErrCredentialInvalid
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	if hasCertificate {
		certificate, err := tls.X509KeyPair(value.ClientCertificate, value.ClientKey)
		if err != nil {
			return connection{}, ErrCredentialInvalid
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return connection{tls: config, token: value.Token}, nil
}

// SealedCredentialStore reads the sealed credential row owned by one target.
type SealedCredentialStore interface {
	LoadDeploymentTargetCredential(ctx context.Context, tenantID, projectID, targetID string) (keyID string, sealed []byte, found bool, err error)
}

// UseSealedCredentials makes ForTarget consult stored credentials. A nil
// keyring keeps targets with stored credentials unavailable instead of
// falling back to the credential directory.
func (directory *CredentialDirectory) UseSealedCredentials(store SealedCredentialStore, keyring *targetcredential.Keyring) {
	if directory != nil {
		directory.sealedStore, directory.keyring = store, keyring
	}
}

// Seal encrypts a credential for one target with the configured keyring.
func (directory *CredentialDirectory) Seal(tenantID, projectID, targetID string, credential StoredCredential) (deploymenttarget.SealedCredential, error) {
	if directory == nil || directory.keyring == nil {
		return deploymenttarget.SealedCredential{}, ErrCredentialUnavailable
	}
	plaintext, err := credential.Marshal()
	if err != nil {
		return deploymenttarget.SealedCredential{}, err
	}
	fingerprint, err := directory.keyring.Fingerprint(plaintext)
	if err != nil {
		return deploymenttarget.SealedCredential{}, ErrCredentialInvalid
	}
	keyID, sealed, err := directory.keyring.Seal(tenantID, projectID, targetID, plaintext)
	if err != nil {
		return deploymenttarget.SealedCredential{}, ErrCredentialInvalid
	}
	return deploymenttarget.SealedCredential{KeyID: keyID, Sealed: sealed, Fingerprint: fingerprint}, nil
}

// ForTarget returns the directory bound to the target's stored credential
// when one was registered, or the directory itself otherwise.
func (directory *CredentialDirectory) ForTarget(ctx context.Context, tenantID, projectID, targetID string) (*CredentialDirectory, error) {
	if directory == nil || ctx == nil {
		return nil, ErrInvalidDirectory
	}
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(targetID, "/targetId") != nil {
		return nil, ErrInvalidEndpoint
	}
	if directory.sealedStore == nil {
		return directory, nil
	}
	keyID, sealed, found, err := directory.sealedStore.LoadDeploymentTargetCredential(ctx, tenantID, projectID, targetID)
	if err != nil {
		return nil, ErrCredentialUnavailable
	}
	if !found {
		return directory, nil
	}
	if directory.keyring == nil {
		return nil, ErrCredentialUnavailable
	}
	plaintext, err := directory.keyring.Open(tenantID, projectID, targetID, keyID, sealed)
	if err != nil {
		return nil, ErrCredentialInvalid
	}
	stored, err := parseStoredCredential(plaintext)
	if err != nil {
		return nil, err
	}
	return &CredentialDirectory{path: directory.path, stored: &stored}, nil
}
