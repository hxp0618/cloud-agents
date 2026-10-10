package controlplane_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/targetcredential"
)

func TestTargetCredentialKeyringBindsTargetAndKey(t *testing.T) {
	keyring, err := targetcredential.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(`{"token":"cluster-secret"}`)
	keyID, sealed, err := keyring.Seal("tenant-alpha", "project-alpha", "target-alpha", plaintext)
	if err != nil || keyID != keyring.KeyID() || bytes.Contains(sealed, []byte("cluster-secret")) {
		t.Fatalf("seal keyID=%q err=%v", keyID, err)
	}
	opened, err := keyring.Open("tenant-alpha", "project-alpha", "target-alpha", keyID, sealed)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("open=%q err=%v", opened, err)
	}
	for name, scope := range map[string][3]string{"tenant": {"tenant-beta", "project-alpha", "target-alpha"}, "project": {"tenant-alpha", "project-beta", "target-alpha"}, "target": {"tenant-alpha", "project-alpha", "target-beta"}} {
		if _, err := keyring.Open(scope[0], scope[1], scope[2], keyID, sealed); !errors.Is(err, targetcredential.ErrInvalidSealed) {
			t.Fatalf("%s rebinding error=%v", name, err)
		}
	}
	other, err := targetcredential.New(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open("tenant-alpha", "project-alpha", "target-alpha", keyID, sealed); !errors.Is(err, targetcredential.ErrKeyMismatch) {
		t.Fatalf("other key error=%v", err)
	}
	first, _ := keyring.Fingerprint(plaintext)
	second, _ := other.Fingerprint(plaintext)
	if first == second || first == "" {
		t.Fatalf("fingerprint is not keyed: %q %q", first, second)
	}

	directory := t.TempDir()
	for name, file := range map[string]struct {
		value []byte
		mode  os.FileMode
		valid bool
	}{
		"owner-only": {bytes.Repeat([]byte{1}, 32), 0o600, true},
		"readable":   {bytes.Repeat([]byte{1}, 32), 0o644, false},
		"short":      {bytes.Repeat([]byte{1}, 31), 0o600, false},
		"long":       {bytes.Repeat([]byte{1}, 33), 0o600, false},
	} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, file.value, file.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, file.mode); err != nil {
			t.Fatal(err)
		}
		if _, err := targetcredential.Load(path); (err == nil) != file.valid {
			t.Fatalf("%s key load error=%v", name, err)
		}
	}
}
