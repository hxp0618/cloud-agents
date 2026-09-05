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

func TestEnsureFoundationWorkspaceVolumeRecoversAndGuardsOwnership(t *testing.T) {
	input := FoundationWorkspaceVolume{"tenant-alpha", "project-alpha", "docker-alpha", "workspace-alpha"}
	name := input.Name()
	var volume *volumeInspect
	creates := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/volumes/"+name:
			if volume == nil {
				http.NotFound(writer, request)
				return
			}
			_ = json.NewEncoder(writer).Encode(volume)
		case request.Method == http.MethodPost && request.URL.Path == "/volumes/create":
			creates++
			var body struct {
				Name   string            `json:"Name"`
				Labels map[string]string `json:"Labels"`
			}
			if json.NewDecoder(request.Body).Decode(&body) != nil || body.Name != name {
				t.Error("invalid create body")
			}
			volume = &volumeInspect{Name: body.Name, Labels: body.Labels}
			http.Error(writer, "lost response", http.StatusInternalServerError)
		default:
			http.NotFound(writer, request)
		}
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()

	root := t.TempDir()
	ref := filepath.Join(root, "docker-alpha")
	if err := os.Mkdir(ref, 0o700); err != nil {
		t.Fatal(err)
	}
	certificate := server.TLS.Certificates[0]
	privateKey, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	for file, value := range map[string][]byte{"ca.pem": certPEM, "cert.pem": certPEM, "key.pem": keyPEM} {
		if err := os.WriteFile(filepath.Join(ref, file), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := NewCredentialDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if actual, err := directory.EnsureFoundationWorkspaceVolume(context.Background(), server.URL, "docker-alpha", input); err != nil || actual != name {
			t.Fatalf("volume = %q, error = %v", actual, err)
		}
	}
	if creates != 1 || volume == nil || volume.Labels["cloud-agents.dev/workspace"] != input.WorkspaceID {
		t.Fatalf("creates = %d, volume = %#v", creates, volume)
	}
	volume.Labels["cloud-agents.dev/workspace"] = "other"
	if _, err := directory.EnsureFoundationWorkspaceVolume(context.Background(), server.URL, "docker-alpha", input); !errors.Is(err, ErrDeploymentConflict) {
		t.Fatal(err)
	}
}
