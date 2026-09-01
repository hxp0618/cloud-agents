package kubernetestarget

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialDirectoryProbesKubernetesVersion(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/version" || request.Header.Get("Authorization") != "Bearer service-account-token" {
			t.Fatalf("request path=%q authorization=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{"major":"1","minor":"34+","gitVersion":"v1.34.2","platform":"linux/arm64"}`))
	}))
	defer server.Close()
	directory := t.TempDir()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(directory, "cluster-alpha.ca.crt"), certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "cluster-alpha.token"), []byte("service-account-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials, err := NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	result, err := credentials.Probe(context.Background(), server.URL, "cluster-alpha")
	if err != nil || result.APIVersion != "1.34" || result.EngineVersion != "v1.34.2" || result.OS != "linux" || result.Architecture != "arm64" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := credentials.Probe(context.Background(), server.URL, "missing"); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("missing credential error=%v", err)
	}
}
