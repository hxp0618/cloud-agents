package main

import (
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
)

func newRemoteWorkerCertificateRequest(enrollmentID, incarnationID string, expectedResourceVersion int64) (platform.RemoteWorkerCertificateIssueRequest, []byte, error) {
	return internalremoteworker.NewCertificateIssueRequest(enrollmentID, incarnationID, expectedResourceVersion)
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
