package kubernetestarget

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFoundationWorkspaceVolumeCreateAdoptAndVerify(t *testing.T) {
	input := FoundationWorkspaceVolume{TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "kubernetes-alpha", WorkspaceID: "workspace-alpha"}
	var stored *resource
	cluster := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer service-account-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.Method {
		case http.MethodGet:
			if stored == nil {
				http.NotFound(writer, request)
				return
			}
			_ = json.NewEncoder(writer).Encode(stored)
		case http.MethodPost:
			var body struct {
				Metadata resourceMetadata `json:"metadata"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			stored = &resource{Metadata: body.Metadata}
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(stored)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(cluster.Close)

	directory := t.TempDir()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cluster.Certificate().Raw})
	for name, value := range map[string][]byte{
		"cluster-alpha.ca.crt":          certificate,
		"cluster-alpha.token":           []byte("service-account-token\n"),
		"cluster-alpha.foundation.json": []byte(`{"namespace":"agents"}`),
	} {
		if err := os.WriteFile(filepath.Join(directory, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.VerifyFoundationWorkspaceVolume(context.Background(), cluster.URL, "cluster-alpha", input); !errors.Is(err, ErrDeploymentConflict) {
		t.Fatalf("missing retained volume error = %v", err)
	}
	name, err := credentials.EnsureFoundationWorkspaceVolume(context.Background(), cluster.URL, "cluster-alpha", input)
	if err != nil || name != input.Name() {
		t.Fatalf("created name = %q, error = %v", name, err)
	}
	if verified, err := credentials.VerifyFoundationWorkspaceVolume(context.Background(), cluster.URL, "cluster-alpha", input); err != nil || verified != name {
		t.Fatalf("verified name = %q, error = %v", verified, err)
	}
	stored.Metadata.Annotations["cloud-agents.dev/workspace"] = "other-workspace"
	if _, err := credentials.EnsureFoundationWorkspaceVolume(context.Background(), cluster.URL, "cluster-alpha", input); !errors.Is(err, ErrDeploymentConflict) {
		t.Fatalf("foreign volume error = %v", err)
	}
}
