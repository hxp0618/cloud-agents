package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const testCLICredential = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"

func TestCLIProfileCreateReadAndContextUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles", "profile.json")
	profile := cliProfile{
		Version:              cliProfileVersion,
		WebEndpoint:          "https://identity.example.test",
		ControlPlaneEndpoint: "https://control.example.test",
		Application:          "admin",
		CredentialKind:       "cliGrant",
		Credential:           testCLICredential,
		DefaultTenantID:      "tenant-alpha",
	}
	if err := createCLIProfile(path, profile); err != nil {
		t.Fatal(err)
	}
	if err := updateCLIProfileContext(path, "tenant-beta", "project-beta"); err != nil {
		t.Fatal(err)
	}
	updated, err := readCLIProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Credential != profile.Credential || updated.DefaultTenantID != "tenant-beta" || updated.DefaultProjectID != "project-beta" {
		t.Fatalf("updated profile = %#v", updated)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode = %o", info.Mode().Perm())
	}
}

func TestCLIProfileRejectsUnknownAndInvalidFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"webEndpoint":"https://identity.example.test","controlPlaneEndpoint":"https://control.example.test","application":"admin","credentialKind":"cliGrant","credential":"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCLIProfile(path); !errors.Is(err, errCLIProfile) {
		t.Fatalf("unknown field error = %v", err)
	}
	invalid := []cliProfile{
		{Version: 2, WebEndpoint: "https://identity.example.test", ControlPlaneEndpoint: "https://control.example.test", Application: "admin", CredentialKind: "cliGrant", Credential: testCLICredential},
		{Version: 1, WebEndpoint: "http://identity.example.test", ControlPlaneEndpoint: "https://control.example.test", Application: "admin", CredentialKind: "cliGrant", Credential: testCLICredential},
		{Version: 1, WebEndpoint: "https://identity.example.test/path", ControlPlaneEndpoint: "https://control.example.test", Application: "admin", CredentialKind: "cliGrant", Credential: testCLICredential},
		{Version: 1, WebEndpoint: "https://identity.example.test", ControlPlaneEndpoint: "https://control.example.test", Application: "platform", CredentialKind: "cliGrant", Credential: testCLICredential},
		{Version: 1, WebEndpoint: "https://identity.example.test", ControlPlaneEndpoint: "https://control.example.test", Application: "admin", CredentialKind: "bearer", Credential: testCLICredential},
		{Version: 1, WebEndpoint: "https://identity.example.test", ControlPlaneEndpoint: "https://control.example.test", Application: "admin", CredentialKind: "cliGrant", Credential: "short"},
		{Version: 1, WebEndpoint: "https://identity.example.test", ControlPlaneEndpoint: "https://control.example.test", Application: "admin", CredentialKind: "cliGrant", Credential: testCLICredential, DefaultProjectID: "project-without-tenant"},
	}
	for index, profile := range invalid {
		if _, err := encodeCLIProfile(profile); !errors.Is(err, errCLIProfile) {
			t.Fatalf("invalid profile %d error = %v", index, err)
		}
	}
}

func TestCLIProfileContextUpdateRefusesUnsafeReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profiles", "profile.json")
	profile := cliProfile{
		Version:              cliProfileVersion,
		WebEndpoint:          "https://identity.example.test",
		ControlPlaneEndpoint: "https://control.example.test",
		Application:          "user",
		CredentialKind:       "serviceAccount",
		Credential:           testCLICredential,
	}
	if err := createCLIProfile(path, profile); err != nil {
		t.Fatal(err)
	}
	if err := updateCLIProfileContext(path, "bad/tenant", ""); !errors.Is(err, errCLIProfile) {
		t.Fatalf("invalid context error = %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := updateCLIProfileContext(path, "tenant-alpha", ""); !errors.Is(err, errCLIProfile) {
			t.Fatalf("unsafe file error = %v", err)
		}
	}
}
