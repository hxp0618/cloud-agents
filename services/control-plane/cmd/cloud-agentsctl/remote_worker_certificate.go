package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
)

func newRemoteWorkerCertificateRequest(enrollmentID, incarnationID string, expectedResourceVersion int64) (platform.RemoteWorkerCertificateIssueRequest, []byte, error) {
	if enrollmentID == "" || incarnationID == "" || expectedResourceVersion < 1 {
		return platform.RemoteWorkerCertificateIssueRequest{}, nil, errors.New("RemoteWorker certificate input is invalid")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return platform.RemoteWorkerCertificateIssueRequest{}, nil, errors.New("cannot generate RemoteWorker private key")
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: enrollmentID}}, key)
	if err != nil {
		return platform.RemoteWorkerCertificateIssueRequest{}, nil, errors.New("cannot generate RemoteWorker CSR")
	}
	keyRaw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return platform.RemoteWorkerCertificateIssueRequest{}, nil, errors.New("cannot encode RemoteWorker private key")
	}
	return platform.RemoteWorkerCertificateIssueRequest{ExpectedResourceVersion: strconv.FormatInt(expectedResourceVersion, 10), ConfirmedEnrollmentID: enrollmentID, IncarnationID: incarnationID, CertificateSigningRequestPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))}, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyRaw}), nil
}

func reserveRemoteWorkerIdentityFile(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || strings.TrimSpace(path) != path {
		return nil, errors.New("--identity-file must be a new absolute path")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, errors.New("cannot create RemoteWorker identity file")
	}
	return file, nil
}

func writeRemoteWorkerIdentityFile(file *os.File, privateKey []byte, certificate platform.RemoteWorkerCertificate) error {
	if file == nil || len(privateKey) == 0 {
		return errors.New("cannot write RemoteWorker identity file")
	}
	if _, err := tls.X509KeyPair([]byte(certificate.CertificateChainPEM), privateKey); err != nil {
		return errors.New("issued certificate does not match the generated private key")
	}
	contents := append([]byte(certificate.CertificateChainPEM), privateKey...)
	if _, err := file.Write(contents); err != nil || file.Sync() != nil || file.Close() != nil {
		return errors.New("cannot write RemoteWorker identity file")
	}
	return nil
}
