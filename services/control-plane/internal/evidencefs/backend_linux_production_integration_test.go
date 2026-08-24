//go:build linux && evidencefsintegration

package evidencefs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const linuxProductionOpenRequiredEnv = "CLOUD_AGENTS_REQUIRE_EVIDENCEFS_PRODUCTION_OPEN"

// TestLinuxProductionOpenWithProvisionedAuthority must run as the non-root
// runner in the same boot and mount namespace in which the root-only helper
// provisioned the fixed local attestation.
func TestLinuxProductionOpenWithProvisionedAuthority(t *testing.T) {
	if os.Getenv(linuxProductionOpenRequiredEnv) != "1" {
		t.Skip("production evidencefs Open was not explicitly required")
	}
	if os.Geteuid() == 0 {
		t.Fatal("production evidencefs Open integration must run as non-root")
	}
	rootPath := os.Getenv(linuxIntegrationRootEnv)
	filesystem := os.Getenv(linuxIntegrationFSEnv)
	if rootPath == "" || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		t.Fatal("integration root must be an absolute canonical path")
	}
	mountID := requireLinuxIntegrationMount(t, rootPath, filesystem)
	root, err := Open(context.Background(), rootPath)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := root.AcquireRoot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("EVIDENCEFS_PRODUCTION_OPEN filesystem=%s mount_id=%d runner_uid=%d result=PASS", filesystem, mountID, os.Geteuid())
}
