package dockertarget

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeUnixSocketReadsOnlyDockerHealthAndVersion(t *testing.T) {
	temporaryDirectory, err := os.MkdirTemp("/tmp", "cloud-agents-docker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporaryDirectory) })
	socketPath := filepath.Join(temporaryDirectory, "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("method = %s", request.Method)
		}
		switch request.URL.Path {
		case "/_ping":
			_, _ = writer.Write([]byte("OK"))
		case "/version":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"ApiVersion":"1.53","Version":"29.4.0","Os":"linux","Arch":"arm64","Ignored":"safe"}`))
		default:
			http.NotFound(writer, request)
		}
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		<-done
	})

	result, err := ProbeUnixSocket(context.Background(), socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.APIVersion != "1.53" || result.EngineVersion != "29.4.0" || result.OS != "linux" || result.Architecture != "arm64" {
		t.Fatalf("result = %#v", result)
	}
}

func TestProbeUnixSocketFailsClosedWithoutLeakingTheSocket(t *testing.T) {
	if _, err := ProbeUnixSocket(context.Background(), "relative.sock"); !errors.Is(err, ErrInvalidSocket) {
		t.Fatalf("relative socket error = %v", err)
	}
	temporaryDirectory, err := os.MkdirTemp("/tmp", "cloud-agents-missing-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporaryDirectory) })
	missing := filepath.Join(temporaryDirectory, "docker.sock")
	if _, err := ProbeUnixSocket(context.Background(), missing); !errors.Is(err, ErrUnavailable) || err.Error() == missing {
		t.Fatalf("missing socket error = %v", err)
	}
}

func TestCredentialDirectoryProbesHTTPSDockerTarget(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/_ping":
			_, _ = writer.Write([]byte("OK"))
		case "/version":
			_, _ = writer.Write([]byte(`{"ApiVersion":"1.54","Version":"29.4.0","Os":"linux","Arch":"amd64"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)

	credentialRoot := t.TempDir()
	credentialPath := filepath.Join(credentialRoot, "docker-alpha")
	if err := os.Mkdir(credentialPath, 0o700); err != nil {
		t.Fatal(err)
	}
	certificate := server.TLS.Certificates[0]
	privateKey, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	for name, value := range map[string][]byte{"ca.pem": certPEM, "cert.pem": certPEM, "key.pem": keyPEM} {
		if err := os.WriteFile(filepath.Join(credentialPath, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := NewCredentialDirectory(credentialRoot)
	if err != nil {
		t.Fatal(err)
	}
	result, err := directory.Probe(context.Background(), server.URL, "docker-alpha")
	if err != nil || result.APIVersion != "1.54" || result.Architecture != "amd64" {
		t.Fatalf("result = %#v / %v", result, err)
	}
	if _, err := directory.Probe(context.Background(), server.URL, "docker-missing"); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("missing credential error = %v", err)
	}
}
