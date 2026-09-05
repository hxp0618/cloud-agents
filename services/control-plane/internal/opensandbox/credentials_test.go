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
