package localmigration

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestFoundationProductUpgradePostgres(t *testing.T) {
	databaseURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_MIGRATION_UPGRADE_DATABASE_URL")
	root := os.Getenv("CLOUD_AGENTS_FOUNDATION_MIGRATION_REPOSITORY_ROOT")
	if databaseURL == "" || root == "" {
		t.Skip("requires an owned disposable PostgreSQL and read-only repository mount")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := func(head string) Result {
		t.Helper()
		result, err := Run(ctx, Config{
			DatabaseURL: databaseURL, RepositoryRoot: root,
			ManifestPath:     "services/control-plane/migrations/product/" + head + "/manifest.json",
			ManifestSelector: "product-" + head,
		}, ProductPGXConnector{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := run("000053"); result.Applied != 53 || result.NoOp || result.SchemaHead != "000053" {
		t.Fatalf("initial result = %+v", result)
	}
	if result := run("000054"); result.Applied != 1 || result.NoOp || result.SchemaHead != "000054" {
		t.Fatalf("upgrade result = %+v", result)
	}
	if result := run("000054"); result.Applied != 0 || !result.NoOp || result.SchemaHead != "000054" {
		t.Fatalf("replay result = %+v", result)
	}
}
