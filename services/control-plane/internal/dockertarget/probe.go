package dockertarget

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const (
	maxSocketPathBytes = 4096
	maxResponseBytes   = 64 << 10
	maxCredentialBytes = 1 << 20
)

var (
	ErrInvalidSocket         = errors.New("docker target socket is invalid")
	ErrInvalidEndpoint       = errors.New("docker target endpoint is invalid")
	ErrInvalidDirectory      = errors.New("docker target credential directory is invalid")
	ErrCredentialUnavailable = errors.New("docker target credential is unavailable")
	ErrCredentialInvalid     = errors.New("docker target credential is invalid")
	ErrUnavailable           = errors.New("docker target is unavailable")
	ErrInvalidResponse       = errors.New("docker target returned an invalid response")
	apiVersionPattern        = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
)

type CredentialDirectory struct{ path string }

type ProbeResult struct {
	APIVersion    string `json:"apiVersion"`
	EngineVersion string `json:"engineVersion"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
}

func ProbeUnixSocket(ctx context.Context, socketPath string) (ProbeResult, error) {
	if ctx == nil || !validSocketPath(socketPath) {
		return ProbeResult{}, ErrInvalidSocket
	}
	transport := &http.Transport{
		DisableCompression:    true,
		ResponseHeaderTimeout: 10 * time.Second,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrInvalidResponse
		},
	}
	return probe(ctx, client, "http://docker")
}

func NewCredentialDirectory(path string) (*CredentialDirectory, error) {
	info, err := os.Lstat(path)
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidDirectory
	}
	return &CredentialDirectory{path: path}, nil
}

func (directory *CredentialDirectory) Probe(ctx context.Context, endpoint, credentialRef string) (ProbeResult, error) {
	if ctx == nil || directory == nil || directory.path == "" || !validEndpoint(endpoint) || commonv1alpha1.ValidateIdentifier(credentialRef, "/credentialRef") != nil {
		return ProbeResult{}, ErrInvalidEndpoint
	}
	root, err := os.OpenRoot(directory.path)
	if err != nil {
		return ProbeResult{}, ErrCredentialUnavailable
	}
	defer root.Close()
	caPEM, err := readCredential(root, filepath.Join(credentialRef, "ca.pem"))
	if err != nil {
		return ProbeResult{}, err
	}
	certPEM, err := readCredential(root, filepath.Join(credentialRef, "cert.pem"))
	if err != nil {
		return ProbeResult{}, err
	}
	keyPEM, err := readCredential(root, filepath.Join(credentialRef, "key.pem"))
	if err != nil {
		return ProbeResult{}, err
	}
	roots := x509.NewCertPool()
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || !roots.AppendCertsFromPEM(caPEM) {
		return ProbeResult{}, ErrCredentialInvalid
	}
	transport := &http.Transport{
		DisableCompression: true, ResponseHeaderTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{certificate}},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrInvalidResponse }}
	return probe(ctx, client, strings.TrimSuffix(endpoint, "/"))
}

func probe(ctx context.Context, client *http.Client, endpoint string) (ProbeResult, error) {
	if body, err := get(ctx, client, endpoint+"/_ping", 16); err != nil {
		return ProbeResult{}, err
	} else if string(body) != "OK" {
		return ProbeResult{}, ErrInvalidResponse
	}
	body, err := get(ctx, client, endpoint+"/version", maxResponseBytes)
	if err != nil {
		return ProbeResult{}, err
	}
	var response struct {
		APIVersion    string `json:"ApiVersion"`
		EngineVersion string `json:"Version"`
		OS            string `json:"Os"`
		Architecture  string `json:"Arch"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ProbeResult{}, ErrInvalidResponse
	}
	result := ProbeResult(response)
	if !apiVersionPattern.MatchString(result.APIVersion) || !validFact(result.EngineVersion) || !validFact(result.OS) || !validFact(result.Architecture) {
		return ProbeResult{}, ErrInvalidResponse
	}
	return result, nil
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

func validSocketPath(value string) bool {
	return value != "" && len(value) <= maxSocketPathBytes && !strings.ContainsRune(value, 0) &&
		filepath.IsAbs(value) && filepath.Clean(value) == value && value != string(filepath.Separator)
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

func get(ctx context.Context, client *http.Client, target string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrInvalidResponse
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, ErrInvalidResponse
	}
	if int64(len(body)) > limit {
		return nil, ErrInvalidResponse
	}
	return body, nil
}
