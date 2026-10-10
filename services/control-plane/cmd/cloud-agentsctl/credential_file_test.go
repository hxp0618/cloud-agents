package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrivateCredentialFileCreateAndRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profiles", "admin.json")
	contents := []byte(`{"credential":"secret-value"}`)
	if err := createPrivateCredentialFile(path, contents); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o", info.Mode().Perm())
	}
	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o", directoryInfo.Mode().Perm())
	}
	read, err := readPrivateCredentialFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(read, contents) {
		t.Fatalf("credential changed: %q", read)
	}
}

func TestPrivateCredentialFileRefusesReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles", "profile.json")
	if err := createPrivateCredentialFile(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := createPrivateCredentialFile(path, []byte("second")); !errors.Is(err, errCLICredentialFile) {
		t.Fatalf("replacement error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "first" {
		t.Fatalf("credential was replaced: %q", contents)
	}
}

func TestPrivateCredentialFileRejectsUnsafeInputs(t *testing.T) {
	root := t.TempDir()
	if err := createPrivateCredentialFile("relative.json", []byte("secret")); !errors.Is(err, errCLICredentialFile) {
		t.Fatalf("relative path error = %v", err)
	}
	if err := createPrivateCredentialFile(filepath.Join(root, "empty"), nil); !errors.Is(err, errCLICredentialFile) {
		t.Fatalf("empty credential error = %v", err)
	}
	if err := createPrivateCredentialFile(filepath.Join(root, "large"), bytes.Repeat([]byte("x"), maxCLICredentialFileBytes+1)); !errors.Is(err, errCLICredentialFile) {
		t.Fatalf("large credential error = %v", err)
	}
	if runtime.GOOS != "windows" {
		permissive := filepath.Join(root, "permissive")
		if err := os.WriteFile(permissive, []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readPrivateCredentialFile(permissive); !errors.Is(err, errCLICredentialFile) {
			t.Fatalf("permissive file error = %v", err)
		}
		target := filepath.Join(root, "target")
		if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := readPrivateCredentialFile(link); !errors.Is(err, errCLICredentialFile) {
			t.Fatalf("symlink error = %v", err)
		}
	}
}
