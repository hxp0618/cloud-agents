package opensandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialDirectoryReadsOnlyOpenSandboxReference(t *testing.T) {
	root := t.TempDir()
	ref := filepath.Join(root, "docker-alpha")
	if err := os.Mkdir(ref, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ref, "opensandbox.json")
	if err := os.WriteFile(path, []byte(`{"endpoint":"https://sandbox.example.test","apiKey":"private-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := NewCredentialDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	client, err := directory.Client("docker-alpha")
	if err != nil || client.endpoint != "https://sandbox.example.test" || client.key != "private-key" {
		t.Fatalf("client = %#v, error = %v", client, err)
	}
	if err := os.WriteFile(path, []byte(`{"endpoint":"https://sandbox.example.test","apiKey":"private-key","secret":"bytes"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := directory.Client("docker-alpha"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestCredentialDirectoryResolvesFlatReferenceAcrossTargetRoots(t *testing.T) {
	dockerRoot, kubernetesRoot := t.TempDir(), t.TempDir()
	path := filepath.Join(kubernetesRoot, "kubernetes-alpha.opensandbox.json")
	if err := os.WriteFile(path, []byte(`{"endpoint":"https://sandbox.example.test","apiKey":"private-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := NewCredentialDirectory(dockerRoot, kubernetesRoot)
	if err != nil {
		t.Fatal(err)
	}
	client, err := directory.Client("kubernetes-alpha")
	if err != nil || client.endpoint != "https://sandbox.example.test" {
		t.Fatalf("client = %#v, error = %v", client, err)
	}
	if err := os.Mkdir(filepath.Join(dockerRoot, "kubernetes-alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dockerRoot, "kubernetes-alpha", "opensandbox.json"), []byte(`{"endpoint":"https://other.example.test","apiKey":"private-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := directory.Client("kubernetes-alpha"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ambiguous credential error = %v", err)
	}
}
