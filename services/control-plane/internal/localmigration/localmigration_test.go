//go:build localdev

package localmigration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
)

type fakeConnector struct {
	session *fakeSession
	calls   int
}

func (connector *fakeConnector) Connect(context.Context, string) (Session, error) {
	connector.calls++
	return connector.session, nil
}

type fakeSession struct {
	rows   []migration.LedgerRow
	apply  []string
	closed bool
}

func (session *fakeSession) SetMigrationRole(context.Context) error           { return nil }
func (session *fakeSession) AcquireAdvisoryLock(context.Context, int64) error { return nil }
func (session *fakeSession) ReadLedger(context.Context) ([]migration.LedgerRow, error) {
	return session.rows, nil
}
func (session *fakeSession) Apply(_ context.Context, entry migration.MigrationEntry, _ []byte, bundle migration.Digest) error {
	session.apply = append(session.apply, entry.ID)
	session.rows = append(session.rows, ledgerRow(entry, bundle, "localdev"))
	return nil
}
func (session *fakeSession) ReleaseAdvisoryLock(context.Context, int64) error { return nil }
func (session *fakeSession) Close(context.Context) error                      { session.closed = true; return nil }

func testConfig(t *testing.T) Config {
	t.Helper()
	root, err := filepath.Abs("../../../../")
	if err != nil {
		t.Fatal(err)
	}
	return Config{DatabaseURL: "postgres://localdev", RepositoryRoot: root, ManifestPath: "services/control-plane/migrations/manifest.json"}
}

func TestRunFreshAppliesManifestOrder(t *testing.T) {
	session := &fakeSession{}
	connector := &fakeConnector{session: session}
	result, err := Run(context.Background(), testConfig(t), connector)
	if err != nil {
		t.Fatal(err)
	}
	if result.NoOp || result.Applied != 13 || result.SchemaHead != "000013" {
		t.Fatalf("unexpected result: %+v", result)
	}
	for index, id := range session.apply {
		want := fmt.Sprintf("%06d", index+1)
		if id != want {
			t.Fatalf("apply order[%d] = %s, want %s", index, id, want)
		}
	}
	if !session.closed || connector.calls != 1 {
		t.Fatalf("session lifecycle calls: closed=%v connect=%d", session.closed, connector.calls)
	}
}

func TestRunCompleteLedgerIsDeterministicNoOp(t *testing.T) {
	bundle, err := loadAndVerify(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	session := &fakeSession{}
	for _, entry := range bundle.manifest.SchemaBundle.Migrations {
		session.rows = append(session.rows, ledgerRow(entry, bundle.manifest.SchemaBundleDigest, "localdev"))
	}
	result, err := Run(context.Background(), testConfig(t), &fakeConnector{session: session})
	if err != nil || !result.NoOp || result.Applied != 0 {
		t.Fatalf("unexpected complete ledger result: %+v err=%v", result, err)
	}
	if len(session.apply) != 0 {
		t.Fatalf("no-op applied migrations: %v", session.apply)
	}
}

func TestLoadAndVerifyVersionedSuccessorManifest(t *testing.T) {
	config := testConfig(t)
	config.ManifestPath = "services/control-plane/migrations/successor/000014/manifest.json"
	bundle, err := loadAndVerify(config)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.manifest.SchemaBundle.SchemaHead != "000014" || len(bundle.manifest.SchemaBundle.Migrations) != 14 {
		t.Fatalf("unexpected successor manifest: head=%s migrations=%d", bundle.manifest.SchemaBundle.SchemaHead, len(bundle.manifest.SchemaBundle.Migrations))
	}
}

func TestRunVersionedSuccessorCompleteLedgerIsNoOp(t *testing.T) {
	config := testConfig(t)
	config.ManifestPath = "services/control-plane/migrations/successor/000014/manifest.json"
	bundle, err := loadAndVerify(config)
	if err != nil {
		t.Fatal(err)
	}
	session := &fakeSession{}
	for _, entry := range bundle.manifest.SchemaBundle.Migrations {
		session.rows = append(session.rows, ledgerRow(entry, bundle.manifest.SchemaBundleDigest, "localdev"))
	}
	result, err := Run(context.Background(), config, &fakeConnector{session: session})
	if err != nil || !result.NoOp || result.Applied != 0 || result.SchemaHead != "000014" {
		t.Fatalf("unexpected successor complete ledger result: %+v err=%v", result, err)
	}
	if len(session.apply) != 0 {
		t.Fatalf("successor no-op applied migrations: %v", session.apply)
	}
}

func TestSupportedManifestLengthsAreVersioned(t *testing.T) {
	for _, test := range []struct {
		head   string
		length int
	}{
		{head: "000013", length: 13},
		{head: "000014", length: 14},
	} {
		length, ok := supportedManifestLength(test.head)
		if !ok || length != test.length {
			t.Fatalf("supportedManifestLength(%q) = (%d, %v), want (%d, true)", test.head, length, ok, test.length)
		}
	}
	if length, ok := supportedManifestLength("000015"); ok || length != 0 {
		t.Fatalf("unsupported head accepted: (%d, %v)", length, ok)
	}
}

func TestRunRejectsPartialAndDivergentLedgers(t *testing.T) {
	bundle, err := loadAndVerify(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	partial := &fakeSession{rows: []migration.LedgerRow{ledgerRow(bundle.manifest.SchemaBundle.Migrations[0], bundle.manifest.SchemaBundleDigest, "localdev")}}
	if _, err := Run(context.Background(), testConfig(t), &fakeConnector{session: partial}); err == nil {
		t.Fatal("partial ledger unexpectedly accepted")
	}
	divergent := &fakeSession{}
	for _, entry := range bundle.manifest.SchemaBundle.Migrations {
		row := ledgerRow(entry, bundle.manifest.SchemaBundleDigest, "localdev")
		if entry.ID == "000007" {
			row.SQLSHA256 = migration.DigestBytes([]byte("different"))
		}
		divergent.rows = append(divergent.rows, row)
	}
	if _, err := Run(context.Background(), testConfig(t), &fakeConnector{session: divergent}); err == nil {
		t.Fatal("divergent ledger unexpectedly accepted")
	}
}

func TestRunRejectsArtifactDriftBeforeConnect(t *testing.T) {
	config := testConfig(t)
	temp := t.TempDir()
	bundle, err := loadAndVerify(config)
	if err != nil {
		t.Fatal(err)
	}
	paths := append([]string{config.ManifestPath}, artifactPaths(bundle.manifest)...)
	for _, path := range paths {
		source := filepath.Join(config.RepositoryRoot, filepath.FromSlash(path))
		target := filepath.Join(temp, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	drifted := filepath.Join(temp, filepath.FromSlash(bundle.manifest.SchemaBundle.Migrations[0].SQLArtifact.Path))
	data, err := os.ReadFile(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(drifted, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	config.RepositoryRoot = temp
	connector := &fakeConnector{session: &fakeSession{}}
	if _, err := Run(context.Background(), config, connector); err == nil {
		t.Fatal("drifted manifest unexpectedly accepted")
	}
	if connector.calls != 0 {
		t.Fatalf("connector called before artifact verification: %d", connector.calls)
	}
}

func TestParseLocalPGXConfigRejectsEveryNonLocalTarget(t *testing.T) {
	for _, databaseURL := range []string{
		"postgres://migration@127.0.0.1:5432/cloud_agents?sslmode=disable",
		"postgres://migration@[::1]:5432/cloud_agents?sslmode=disable",
		"postgres://migration@/cloud_agents?host=/tmp",
	} {
		if _, err := parseLocalPGXConfig(databaseURL); err != nil {
			t.Errorf("%s: %v", databaseURL, err)
		}
	}
	for _, databaseURL := range []string{
		"postgres://migration@localhost:5432/cloud_agents?sslmode=disable",
		"postgres://migration@192.168.31.234:5432/cloud_agents?sslmode=disable",
		"postgres://migration@127.0.0.1:5432,192.168.31.234:5432/cloud_agents?sslmode=disable",
		"not-a-database-url",
	} {
		if _, err := parseLocalPGXConfig(databaseURL); err == nil {
			t.Errorf("%s should be rejected", databaseURL)
		}
	}
}

func artifactPaths(manifest *migration.Manifest) []string {
	result := make([]string, 0, len(manifest.SchemaBundle.Migrations))
	for _, entry := range manifest.SchemaBundle.Migrations {
		result = append(result, entry.SQLArtifact.Path)
	}
	return result
}
