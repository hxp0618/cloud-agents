package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/localmigration"
)

const databaseURLEnvironment = "CLOUD_AGENTS_PLATFORM_DATABASE_URL"

var version = "dev"

type productMigrationConfig struct {
	databaseURL    string
	repositoryRoot string
	manifestPath   string
	selector       string
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = fmt.Printf("cloud-agents-product-migrate %s\n", version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cloud-agents-product-migrate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	config, err := parseProductMigrationConfig(args, getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	result, err := localmigration.Run(ctx, localmigration.Config{
		DatabaseURL: config.databaseURL, RepositoryRoot: config.repositoryRoot,
		ManifestPath: config.manifestPath, ManifestSelector: config.selector,
	}, localmigration.ProductPGXConnector{})
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return errors.New("encode migration result failed")
	}
	_, err = fmt.Println(string(encoded))
	return err
}

func parseProductMigrationConfig(args []string, getenv func(string) string) (productMigrationConfig, error) {
	set := flag.NewFlagSet("cloud-agents-product-migrate", flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	databaseURL := set.String("database-url", "", "PostgreSQL URL")
	repositoryRoot := set.String("repository-root", ".", "repository root containing the product migration bundle")
	manifestPath := set.String("manifest", "services/control-plane/migrations/product/000039/manifest.json", "manifest path relative to repository root")
	selector := set.String("selector", "product-000039", "independent product migration selector")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return productMigrationConfig{}, errors.New("invalid product migration configuration")
	}
	if *databaseURL == "" && getenv != nil {
		*databaseURL = getenv(databaseURLEnvironment)
	}
	if *databaseURL == "" || *repositoryRoot == "" || *manifestPath == "" || *selector != "product-000039" {
		return productMigrationConfig{}, errors.New("database URL, repository root, and product-000039 selector are required")
	}
	return productMigrationConfig{databaseURL: *databaseURL, repositoryRoot: *repositoryRoot, manifestPath: *manifestPath, selector: *selector}, nil
}
