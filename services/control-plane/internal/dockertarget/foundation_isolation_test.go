package dockertarget

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyFoundationSandboxIsolation(t *testing.T) {
	runtimeID := "runtime-1"
	runtime := "runsc"
	internal := true
	icc := "false"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/containers/json":
			_ = json.NewEncoder(writer).Encode([]map[string]string{{"Id": "container-1"}})
		case "/containers/container-1/json":
			_ = json.NewEncoder(writer).Encode(map[string]any{"Config": map[string]any{"Labels": map[string]string{"opensandbox.io/id": runtimeID}}, "State": map[string]bool{"Running": true}, "HostConfig": map[string]string{"Runtime": runtime}, "NetworkSettings": map[string]any{"Networks": map[string]any{"isolated": map[string]string{"NetworkID": "network-1"}}}})
		case "/containers/container-1/stats":
			_ = json.NewEncoder(writer).Encode(map[string]any{"networks": map[string]any{
				"eth0": map[string]int64{"rx_bytes": 1234, "tx_bytes": 567},
				"eth1": map[string]int64{"rx_bytes": 6, "tx_bytes": 8},
			}})
		case "/networks/network-1":
			_ = json.NewEncoder(writer).Encode(map[string]any{"Id": "network-1", "Internal": internal, "Options": map[string]string{"com.docker.network.bridge.enable_icc": icc}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	directory := credentialDirectoryForIsolationTest(t, server)
	if err := directory.VerifyFoundationSandboxIsolation(context.Background(), server.URL, "docker", runtimeID); err != nil {
		t.Fatal(err)
	}
	received, transmitted, err := directory.MeasureFoundationSandboxNetwork(context.Background(), server.URL, "docker", runtimeID)
	if err != nil || received != 1240 || transmitted != 575 {
		t.Fatalf("network usage = %d/%d err=%v", received, transmitted, err)
	}
	for name, mutate := range map[string]func(){
		"runtime":  func() { runtime = "runc" },
		"internal": func() { internal = false },
		"icc":      func() { icc = "true" },
	} {
		runtime, internal, icc = "runsc", true, "false"
		mutate()
		if err := directory.VerifyFoundationSandboxIsolation(context.Background(), server.URL, "docker", runtimeID); !errors.Is(err, ErrIsolationUnenforced) {
			t.Fatalf("%s drift error=%v", name, err)
		}
	}
}

func credentialDirectoryForIsolationTest(t *testing.T, server *httptest.Server) *CredentialDirectory {
	t.Helper()
	directory := t.TempDir()
	credential := filepath.Join(directory, "docker")
	if err := os.Mkdir(credential, 0o700); err != nil {
		t.Fatal(err)
	}
	certificate := server.Certificate()
	key := server.TLS.Certificates[0].PrivateKey
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{
		"ca.pem":   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}),
		"cert.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}),
		"key.pem":  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}),
	} {
		if err := os.WriteFile(filepath.Join(credential, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server.TLS.ClientAuth = tls.RequestClientCert
	result, err := NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
