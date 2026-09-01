package kubernetestarget

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const (
	maxResponseBytes   = 64 << 10
	maxCredentialBytes = 1 << 20
)

var (
	ErrInvalidEndpoint       = errors.New("Kubernetes target endpoint is invalid")
	ErrInvalidDirectory      = errors.New("Kubernetes target credential directory is invalid")
	ErrCredentialUnavailable = errors.New("Kubernetes target credential is unavailable")
	ErrCredentialInvalid     = errors.New("Kubernetes target credential is invalid")
	ErrUnavailable           = errors.New("Kubernetes target is unavailable")
	ErrInvalidResponse       = errors.New("Kubernetes target returned an invalid response")
	versionPartPattern       = regexp.MustCompile(`^[0-9]+$`)
)

type CredentialDirectory struct{ path string }

type ProbeResult struct {
	APIVersion    string
	EngineVersion string
	OS            string
	Architecture  string
}

func NewCredentialDirectory(path string) (*CredentialDirectory, error) {
	info, err := os.Lstat(path)
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidDirectory
	}
	return &CredentialDirectory{path: path}, nil
}

func (directory *CredentialDirectory) Probe(ctx context.Context, endpoint, credentialRef string) (ProbeResult, error) {
	if directory == nil || ctx == nil || !validEndpoint(endpoint) || commonv1alpha1.ValidateIdentifier(credentialRef, "/credentialRef") != nil {
		return ProbeResult{}, ErrInvalidEndpoint
	}
	root, err := os.OpenRoot(directory.path)
	if err != nil {
		return ProbeResult{}, ErrCredentialUnavailable
	}
	defer root.Close()
	caPEM, err := readCredential(root, credentialRef+".ca.crt")
	if err != nil {
		return ProbeResult{}, err
	}
	tokenBytes, err := readCredential(root, credentialRef+".token")
	if err != nil {
		return ProbeResult{}, err
	}
	token := strings.TrimSuffix(strings.TrimSuffix(string(tokenBytes), "\n"), "\r")
	roots := x509.NewCertPool()
	if token == "" || strings.TrimSpace(token) != token || !roots.AppendCertsFromPEM(caPEM) {
		return ProbeResult{}, ErrCredentialInvalid
	}
	transport := &http.Transport{
		DisableCompression: true, ResponseHeaderTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrInvalidResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(endpoint, "/")+"/version", nil)
	if err != nil {
		return ProbeResult{}, ErrInvalidEndpoint
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ProbeResult{}, ctx.Err()
		}
		return ProbeResult{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ProbeResult{}, ErrCredentialInvalid
	}
	if response.StatusCode != http.StatusOK {
		return ProbeResult{}, ErrInvalidResponse
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return ProbeResult{}, ErrInvalidResponse
	}
	var version struct {
		Major      string `json:"major"`
		Minor      string `json:"minor"`
		GitVersion string `json:"gitVersion"`
		Platform   string `json:"platform"`
	}
	if json.Unmarshal(body, &version) != nil {
		return ProbeResult{}, ErrInvalidResponse
	}
	minor := strings.TrimSuffix(version.Minor, "+")
	targetOS, architecture, found := strings.Cut(version.Platform, "/")
	if !versionPartPattern.MatchString(version.Major) || !versionPartPattern.MatchString(minor) || !found || strings.Contains(architecture, "/") ||
		!validFact(version.GitVersion) || !validFact(targetOS) || !validFact(architecture) {
		return ProbeResult{}, ErrInvalidResponse
	}
	return ProbeResult{APIVersion: version.Major + "." + minor, EngineVersion: version.GitVersion, OS: targetOS, Architecture: architecture}, nil
}

func validEndpoint(value string) bool {
	request, err := http.NewRequest(http.MethodGet, value, nil)
	return err == nil && len(value) <= 2048 && request.URL.Scheme == "https" && request.URL.Host != "" && request.URL.User == nil &&
		(request.URL.Path == "" || request.URL.Path == "/") && request.URL.RawQuery == "" && request.URL.Fragment == "" && request.URL.Opaque == ""
}

func readCredential(root *os.Root, path string) ([]byte, error) {
	file, err := root.Open(path)
	if err != nil {
		return nil, ErrCredentialUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxCredentialBytes {
		return nil, ErrCredentialInvalid
	}
	value, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil || len(value) > maxCredentialBytes {
		return nil, ErrCredentialInvalid
	}
	return value, nil
}

func validFact(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
