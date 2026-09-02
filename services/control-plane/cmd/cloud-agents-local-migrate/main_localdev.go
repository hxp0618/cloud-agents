//go:build localdev

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/localmigration"
)

func main() {
	var databaseURL, repositoryRoot, manifestPath, manifestSelector string
	flag.StringVar(&databaseURL, "database-url", "", "local PostgreSQL database URL")
	flag.StringVar(&repositoryRoot, "repository-root", "", "repository root containing the manifest and SQL artifacts")
	flag.StringVar(&manifestPath, "manifest", "services/control-plane/migrations/manifest.json", "manifest path relative to repository root")
	flag.StringVar(&manifestSelector, "selector", "", "migration selector (canonical-000013, successor-000014, or product-000015 through product-000037)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	result, err := localmigration.Run(ctx, localmigration.Config{
		DatabaseURL: databaseURL, RepositoryRoot: repositoryRoot, ManifestPath: manifestPath,
		ManifestSelector: manifestSelector,
	}, localmigration.PGXConnector{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode migration result:", err)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
}
