package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
)

const defaultCertificateRotationBefore = 5 * time.Minute

type pendingCertificateRotation struct {
	ExpectedResourceVersion int64                                        `json:"expectedResourceVersion"`
	IdempotencyKey          string                                       `json:"idempotencyKey"`
	Request                 platform.RemoteWorkerCertificateIssueRequest `json:"request"`
	PrivateKeyPEM           string                                       `json:"privateKeyPem"`
}

func certificateNotAfter(certificateFile, privateKeyFile string) (time.Time, error) {
	pair, err := tls.LoadX509KeyPair(certificateFile, privateKeyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return time.Time{}, errInvalidRemoteWorkerConfig
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return time.Time{}, errInvalidRemoteWorkerConfig
	}
	return certificate.NotAfter, nil
}

func certificateRotationDue(expiresAt, now time.Time, rotateBefore time.Duration, force bool) bool {
	return force || !now.Add(rotateBefore).Before(expiresAt)
}

func readCertificateResourceVersion(path string) (int64, error) {
	contents, err := os.ReadFile(path)
	if err != nil || len(contents) == 0 || len(contents) > 64 {
		return 0, errInvalidRemoteWorkerConfig
	}
	version, err := strconv.ParseInt(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil || version < 1 {
		return 0, errInvalidRemoteWorkerConfig
	}
	return version, nil
}

func loadOrCreatePendingCertificateRotation(value config, expectedResourceVersion int64) (pendingCertificateRotation, error) {
	path := value.certificateResourceVersionFile + ".pending"
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		request, privateKey, requestErr := internalremoteworker.NewCertificateIssueRequest(value.enrollmentID, value.incarnationID, expectedResourceVersion)
		if requestErr != nil {
			return pendingCertificateRotation{}, requestErr
		}
		pending := pendingCertificateRotation{
			ExpectedResourceVersion: expectedResourceVersion,
			IdempotencyKey:          "remote-worker-certificate-" + strconv.FormatInt(expectedResourceVersion, 10),
			Request:                 request,
			PrivateKeyPEM:           string(privateKey),
		}
		raw, marshalErr := json.Marshal(pending)
		if marshalErr != nil || writePrivateFile(path, append(raw, '\n')) != nil {
			return pendingCertificateRotation{}, errInvalidRemoteWorkerConfig
		}
		return pending, nil
	}
	if err != nil || len(contents) == 0 || len(contents) > 128<<10 {
		return pendingCertificateRotation{}, errInvalidRemoteWorkerConfig
	}
	var pending pendingCertificateRotation
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pending) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		pending.ExpectedResourceVersion != expectedResourceVersion && pending.ExpectedResourceVersion+1 != expectedResourceVersion ||
		pending.IdempotencyKey != "remote-worker-certificate-"+strconv.FormatInt(pending.ExpectedResourceVersion, 10) ||
		pending.Request.ExpectedResourceVersion != strconv.FormatInt(pending.ExpectedResourceVersion, 10) ||
		pending.Request.ConfirmedEnrollmentID != value.enrollmentID || pending.Request.IncarnationID != value.incarnationID ||
		internalremoteworker.ValidateCertificateIssuePrivateKey(pending.Request, []byte(pending.PrivateKeyPEM)) != nil {
		return pendingCertificateRotation{}, errInvalidRemoteWorkerConfig
	}
	if pending.ExpectedResourceVersion+1 == expectedResourceVersion {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return pendingCertificateRotation{}, err
		}
		return loadOrCreatePendingCertificateRotation(value, expectedResourceVersion)
	}
	return pending, nil
}

func rotateCertificate(ctx context.Context, client *api.Client, value config) (*api.Client, time.Time, error) {
	version, err := readCertificateResourceVersion(value.certificateResourceVersionFile)
	if err != nil {
		return nil, time.Time{}, err
	}
	pending, err := loadOrCreatePendingCertificateRotation(value, version)
	if err != nil {
		return nil, time.Time{}, err
	}
	result, err := client.RotateRemoteWorkerCertificate(ctx, value.tenantID, value.projectID, value.enrollmentID,
		"remote-worker-certificate-"+strconv.FormatInt(version, 10), pending.IdempotencyKey, pending.Request)
	if err != nil {
		return nil, time.Time{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, result.Value.ExpiresAt)
	if err != nil {
		return nil, time.Time{}, errInvalidRemoteWorkerConfig
	}
	identity := append([]byte(result.Value.CertificateChainPEM), []byte(pending.PrivateKeyPEM)...)
	if _, err := tls.X509KeyPair([]byte(result.Value.CertificateChainPEM), []byte(pending.PrivateKeyPEM)); err != nil ||
		writePrivateFile(value.certificate, identity) != nil {
		return nil, time.Time{}, errInvalidRemoteWorkerConfig
	}
	replacementClient, err := newClient(value)
	if err != nil || writePrivateFile(value.certificateResourceVersionFile, []byte(strconv.FormatInt(version+1, 10)+"\n")) != nil {
		return nil, time.Time{}, errInvalidRemoteWorkerConfig
	}
	_ = os.Remove(value.certificateResourceVersionFile + ".pending")
	return replacementClient, expiresAt, nil
}

func writePrivateFile(path string, contents []byte) error {
	if path == "" {
		return errInvalidRemoteWorkerConfig
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".remote-worker-private-*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(contents)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporaryPath, path)
	}
	if err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = dir.Sync()
	if closeErr := dir.Close(); err == nil {
		err = closeErr
	}
	return err
}
