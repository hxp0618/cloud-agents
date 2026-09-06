package accessgrant

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenIsStableAndBoundToGrant(t *testing.T) {
	codec, err := New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	grant := GrantID("tenant-a", "project-a", "sandbox-a", "issue-a")
	token, err := codec.Token(grant)
	if err != nil {
		t.Fatal(err)
	}
	replayed, _ := codec.Token(grant)
	other, _ := codec.Token(GrantID("tenant-a", "project-a", "sandbox-a", "issue-b"))
	if len(token) != 48 || token[:5] != "cag1_" || token != replayed || token == other || Digest(token) == Digest(other) {
		t.Fatalf("grant token binding failed")
	}
}

func TestLoadRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "key")
	link := filepath.Join(directory, "key-link")
	if err := os.WriteFile(target, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err != ErrInvalidKey {
		t.Fatalf("symlink error = %v", err)
	}
	if _, err := Load(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(target); err != ErrInvalidKey {
		t.Fatalf("public key file mode error = %v", err)
	}
}

func TestSSHUsernameRoundTrip(t *testing.T) {
	username, err := SSHUsername("tenant-a", "project-a", "grant-a")
	if err != nil || username != "tenant-a:project-a:grant-a" {
		t.Fatalf("SSH username = %q, %v", username, err)
	}
	tenant, project, grant, ok := ParseSSHUsername(username)
	if !ok || tenant != "tenant-a" || project != "project-a" || grant != "grant-a" {
		t.Fatalf("SSH username did not round trip")
	}
	for _, invalid := range []string{"tenant:project", "tenant:project:grant:extra", "tenant:project:bad grant", ":project:grant"} {
		if _, _, _, ok := ParseSSHUsername(invalid); ok {
			t.Fatalf("accepted invalid SSH username %q", invalid)
		}
	}
}
