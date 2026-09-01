package sshtarget

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"golang.org/x/crypto/ssh"
)

const (
	maxCredentialBytes  = 1 << 20
	maxProbeOutputBytes = 4096
)

var (
	ErrInvalidEndpoint       = errors.New("SSH target endpoint is invalid")
	ErrInvalidDirectory      = errors.New("SSH target credential directory is invalid")
	ErrCredentialUnavailable = errors.New("SSH target credential is unavailable")
	ErrCredentialInvalid     = errors.New("SSH target credential is invalid")
	ErrHostKeyMismatch       = errors.New("SSH target host key does not match")
	ErrUnavailable           = errors.New("SSH target is unavailable")
	ErrInvalidResponse       = errors.New("SSH target returned an invalid response")
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
	client, _, err := directory.connect(ctx, endpoint, credentialRef, 10*time.Second)
	if err != nil {
		return ProbeResult{}, err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return ProbeResult{}, ErrUnavailable
	}
	defer session.Close()
	output := &limitedWriter{remaining: maxProbeOutputBytes}
	session.Stdout, session.Stderr = output, io.Discard
	if err := session.Run("uname -s && uname -m"); err != nil {
		return ProbeResult{}, ErrInvalidResponse
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	serverVersion := strings.TrimPrefix(string(client.ServerVersion()), "SSH-2.0-")
	if len(lines) != 2 || !validFact(serverVersion) || !validFact(lines[0]) || !validFact(lines[1]) {
		return ProbeResult{}, ErrInvalidResponse
	}
	return ProbeResult{APIVersion: "2.0", EngineVersion: serverVersion, OS: strings.ToLower(lines[0]), Architecture: normalizeArchitecture(lines[1])}, nil
}

func (directory *CredentialDirectory) connect(ctx context.Context, endpoint, credentialRef string, operationTimeout time.Duration) (*ssh.Client, string, error) {
	if ctx == nil || directory == nil || operationTimeout <= 0 || commonv1alpha1.ValidateIdentifier(credentialRef, "/credentialRef") != nil {
		return nil, "", ErrInvalidEndpoint
	}
	host, address, err := endpointHostAddress(endpoint)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(directory.path)
	if err != nil {
		return nil, "", ErrCredentialUnavailable
	}
	defer root.Close()
	userBytes, err := readCredential(root, credentialRef+".user", false)
	if err != nil {
		return nil, "", err
	}
	privateKey, err := readCredential(root, credentialRef+".key", true)
	if err != nil {
		return nil, "", err
	}
	hostKeyBytes, err := readCredential(root, credentialRef+".host-key.pub", false)
	if err != nil {
		return nil, "", err
	}
	user := strings.TrimSuffix(strings.TrimSuffix(string(userBytes), "\n"), "\r")
	signer, signerErr := ssh.ParsePrivateKey(privateKey)
	hostKey, _, _, rest, hostKeyErr := ssh.ParseAuthorizedKey(hostKeyBytes)
	if commonv1alpha1.ValidateIdentifier(user, "/sshUser") != nil || user != strings.TrimSpace(user) || signerErr != nil || hostKeyErr != nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, "", ErrCredentialInvalid
	}
	verifiedHostKey := false
	config := &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: 10 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, actual ssh.PublicKey) error {
			if actual.Type() != hostKey.Type() || !bytes.Equal(actual.Marshal(), hostKey.Marshal()) {
				return ErrHostKeyMismatch
			}
			verifiedHostKey = true
			return nil
		},
	}
	connection, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", ErrUnavailable
	}
	deadline := time.Now().Add(operationTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, address, config)
	if err != nil {
		_ = connection.Close()
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		if errors.Is(err, ErrHostKeyMismatch) {
			return nil, "", ErrHostKeyMismatch
		}
		if verifiedHostKey {
			return nil, "", ErrCredentialInvalid
		}
		return nil, "", ErrUnavailable
	}
	return ssh.NewClient(clientConnection, channels, requests), host, nil
}

func endpointAddress(endpoint string) (string, error) {
	_, address, err := endpointHostAddress(endpoint)
	return address, err
}

func endpointHostAddress(endpoint string) (string, string, error) {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || len(endpoint) > 2048 || parsed.Scheme != "ssh" || parsed.Hostname() == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", "", ErrInvalidEndpoint
	}
	port := parsed.Port()
	if port == "" {
		port = "22"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", ErrInvalidEndpoint
	}
	return parsed.Hostname(), net.JoinHostPort(parsed.Hostname(), port), nil
}

func readCredential(root *os.Root, name string, private bool) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrCredentialUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxCredentialBytes || private && info.Mode().Perm()&0o077 != 0 {
		return nil, ErrCredentialInvalid
	}
	value, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil || len(value) > maxCredentialBytes {
		return nil, ErrCredentialInvalid
	}
	return value, nil
}

func validFact(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func normalizeArchitecture(value string) string {
	switch strings.ToLower(value) {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return strings.ToLower(value)
	}
}

type limitedWriter struct {
	bytes.Buffer
	remaining int
}

func (writer *limitedWriter) Write(value []byte) (int, error) {
	if len(value) > writer.remaining {
		return 0, ErrInvalidResponse
	}
	written, err := writer.Buffer.Write(value)
	writer.remaining -= written
	return written, err
}
