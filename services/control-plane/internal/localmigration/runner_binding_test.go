//go:build localdev

package localmigration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
)

func TestRunnerBindingAcceptsOnlyTheClosedSelectorSet(t *testing.T) {
	for _, test := range []struct {
		name     string
		selector string
		path     string
		head     string
	}{
		{name: "canonical", path: "services/control-plane/migrations/manifest.json", head: "000013"},
		{name: "successor", selector: "successor-000014", path: "services/control-plane/migrations/successor/000014/manifest.json", head: "000014"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := testConfig(t)
			config.ManifestSelector = test.selector
			config.ManifestPath = test.path
			bundle, err := loadAndVerify(config)
			if err != nil {
				t.Fatal(err)
			}
			if got := bundle.manifest.SchemaBundle.SchemaHead; got != test.head {
				t.Fatalf("schema head = %s, want %s", got, test.head)
			}
		})
	}
}

func TestRunnerBindingRejectsCallerSelectedAndMismatchedPathsBeforeConnect(t *testing.T) {
	for _, test := range []struct {
		name     string
		selector string
		path     string
	}{
		{name: "unknown selector", selector: "successor-000015", path: "services/control-plane/migrations/manifest.json"},
		{name: "foreign path", path: "services/control-plane/migrations/foreign/manifest.json"},
		{name: "successor path without selector", path: "services/control-plane/migrations/successor/000014/manifest.json"},
		{name: "selector path mismatch", selector: "successor-000014", path: "services/control-plane/migrations/manifest.json"},
		{name: "selector whitespace near miss", selector: " successor-000014", path: "services/control-plane/migrations/successor/000014/manifest.json"},
		{name: "path spelling near miss", path: "./services/control-plane/migrations/manifest.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := testConfig(t)
			config.ManifestSelector = test.selector
			config.ManifestPath = test.path
			connector := &fakeConnector{session: &fakeSession{}}
			if _, err := Run(context.Background(), config, connector); err == nil {
				t.Fatal("invalid selector unexpectedly accepted")
			}
			if connector.calls != 0 {
				t.Fatalf("connector called before selector binding: %d", connector.calls)
			}
		})
	}
}

func TestRunnerBindingRejectsBoundArtifactModeAndDigestDriftBeforeConnect(t *testing.T) {
	t.Run("manifest mode", func(t *testing.T) {
		root := copyRunnerBindingFixture(t)
		manifestPath := filepath.Join(root, filepath.FromSlash("services/control-plane/migrations/manifest.json"))
		if err := os.Chmod(manifestPath, 0o600); err != nil {
			t.Fatal(err)
		}
		config := testConfig(t)
		config.RepositoryRoot = root
		connector := &fakeConnector{session: &fakeSession{}}
		if _, err := Run(context.Background(), config, connector); err == nil {
			t.Fatal("mode-drifted manifest unexpectedly accepted")
		}
		if connector.calls != 0 {
			t.Fatalf("connector called before mode verification: %d", connector.calls)
		}
	})

	t.Run("schema bytes and digest", func(t *testing.T) {
		root := copyRunnerBindingFixture(t)
		schemaPath := filepath.Join(root, filepath.FromSlash("services/control-plane/migrations/schema-bundle.json"))
		data, err := os.ReadFile(schemaPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(schemaPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		config := testConfig(t)
		config.RepositoryRoot = root
		connector := &fakeConnector{session: &fakeSession{}}
		if _, err := Run(context.Background(), config, connector); err == nil {
			t.Fatal("digest-drifted schema bundle unexpectedly accepted")
		}
		if connector.calls != 0 {
			t.Fatalf("connector called before digest verification: %d", connector.calls)
		}
	})
}

func TestRunnerBindingRejectsSymlinkedAncestorBeforeConnect(t *testing.T) {
	root := copyRunnerBindingFixture(t)
	migrationsPath := filepath.Join(root, filepath.FromSlash("services/control-plane/migrations"))
	movedPath := filepath.Join(root, "migrations-real")
	if err := os.Rename(migrationsPath, movedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(movedPath, migrationsPath); err != nil {
		t.Fatal(err)
	}
	config := testConfig(t)
	config.RepositoryRoot = root
	connector := &fakeConnector{session: &fakeSession{}}
	if _, err := Run(context.Background(), config, connector); err == nil {
		t.Fatal("symlinked ancestor unexpectedly accepted")
	}
	if connector.calls != 0 {
		t.Fatalf("connector called before ancestor symlink rejection: %d", connector.calls)
	}
}

func TestRunnerBindingRejectsReformattedProfileBeforeConnect(t *testing.T) {
	root := copyRunnerBindingFixture(t)
	profilePath := filepath.Join(root, filepath.FromSlash(runnerBindingProfilePath))
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	reformatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, append(reformatted, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	config := testConfig(t)
	config.RepositoryRoot = root
	connector := &fakeConnector{session: &fakeSession{}}
	if _, err := Run(context.Background(), config, connector); err == nil {
		t.Fatal("reformatted profile unexpectedly accepted")
	}
	if connector.calls != 0 {
		t.Fatalf("connector called before profile binding: %d", connector.calls)
	}
}

func TestRunnerBindingRejectsR2AndR1DescriptorDriftBeforeConnect(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		mode os.FileMode
		edit func([]byte) []byte
	}{
		{name: "r2 source bytes", path: runnerBindingSourcePath, mode: 0o644, edit: func(data []byte) []byte {
			return append(data, '\n')
		}},
		{name: "r2 source schema bytes", path: runnerBindingSourceSchemaPath, mode: 0o644, edit: func(data []byte) []byte {
			return append(data, '\n')
		}},
		{name: "r2 profile schema mode", path: runnerBindingProfileSchemaPath, mode: 0o600, edit: func(data []byte) []byte {
			return data
		}},
		{name: "r1 source bytes", path: "services/control-plane/migrations/successor/000014/authority-source.json", mode: 0o644, edit: func(data []byte) []byte {
			return append(data, '\n')
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := copyRunnerBindingFixture(t)
			path := filepath.Join(root, filepath.FromSlash(test.path))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, test.edit(data), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			config := testConfig(t)
			config.RepositoryRoot = root
			connector := &fakeConnector{session: &fakeSession{}}
			if _, err := Run(context.Background(), config, connector); err == nil {
				t.Fatal("descriptor drift unexpectedly accepted")
			}
			if connector.calls != 0 {
				t.Fatalf("connector called before descriptor verification: %d", connector.calls)
			}
		})
	}
}

func TestRunnerBindingRejectsSelfConsistentLookingManifestAndSymlink(t *testing.T) {
	t.Run("raw formatting drift", func(t *testing.T) {
		root := copyRunnerBindingFixture(t)
		manifestPath := filepath.Join(root, filepath.FromSlash("services/control-plane/migrations/manifest.json"))
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestPath, append([]byte("\n"), data...), 0o644); err != nil {
			t.Fatal(err)
		}
		config := testConfig(t)
		config.RepositoryRoot = root
		connector := &fakeConnector{session: &fakeSession{}}
		if _, err := Run(context.Background(), config, connector); err == nil {
			t.Fatal("reformatted manifest unexpectedly accepted")
		}
		if connector.calls != 0 {
			t.Fatalf("connector called before manifest binding: %d", connector.calls)
		}
	})

	t.Run("manifest symlink", func(t *testing.T) {
		root := copyRunnerBindingFixture(t)
		manifestPath := filepath.Join(root, filepath.FromSlash("services/control-plane/migrations/manifest.json"))
		if err := os.Remove(manifestPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(t.TempDir(), "outside.json"), manifestPath); err != nil {
			t.Fatal(err)
		}
		config := testConfig(t)
		config.RepositoryRoot = root
		connector := &fakeConnector{session: &fakeSession{}}
		if _, err := Run(context.Background(), config, connector); err == nil {
			t.Fatal("manifest symlink unexpectedly accepted")
		}
		if connector.calls != 0 {
			t.Fatalf("connector called before symlink rejection: %d", connector.calls)
		}
	})
}

// copyRunnerBindingFixture copies only the immutable r1/r2 descriptors and
// the two closed selector bundles needed by the localdev pre-connect checks.
// It deliberately does not copy unrelated repository state.
func copyRunnerBindingFixture(t *testing.T) string {
	t.Helper()
	sourceRoot, err := filepath.Abs("../../../../")
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := t.TempDir()
	paths := []string{
		runnerBindingSourcePath,
		runnerBindingProfilePath,
		runnerBindingSourceSchemaPath,
		runnerBindingProfileSchemaPath,
		"services/control-plane/migrations/successor/000014/authority-source.json",
		"services/control-plane/migrations/successor/000014/profile.json",
		"services/control-plane/migrations/successor/000014/authority-source.schema.json",
		"services/control-plane/migrations/successor/000014/profile.schema.json",
		"services/control-plane/migrations/manifest.json",
		"services/control-plane/migrations/schema-bundle.json",
		"services/control-plane/migrations/successor/000014/manifest.json",
		"services/control-plane/migrations/successor/000014/schema-bundle.json",
	}
	for _, manifestPath := range []string{
		"services/control-plane/migrations/manifest.json",
		"services/control-plane/migrations/successor/000014/manifest.json",
	} {
		data, readErr := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(manifestPath)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		manifest, _, decodeErr := migration.DecodeManifest(data)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		for _, entry := range manifest.SchemaBundle.Migrations {
			paths = append(paths, entry.SQLArtifact.Path)
		}
	}
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		source := filepath.Join(sourceRoot, filepath.FromSlash(path))
		target := filepath.Join(targetRoot, filepath.FromSlash(path))
		data, readErr := os.ReadFile(source)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o755); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}
		if writeErr := os.WriteFile(target, data, 0o644); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	return targetRoot
}
