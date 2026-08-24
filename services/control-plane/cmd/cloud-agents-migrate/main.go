package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
)

const databaseURLEnvironment = "CLOUD_AGENTS_PLATFORM_DATABASE_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cloud-agents-migrate: %v\n", err)
		os.Exit(2)
	}
}

func run(ctx context.Context, args []string) error {
	set := flag.NewFlagSet("cloud-agents-migrate", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	artifactPath := set.String("artifact", "", "path to the deterministic runtime migration tar")
	repository := set.String("repository", "", "expected signed repository identity")
	release := set.String("release", "", "expected signed release identity")
	evidenceRoot := set.String("evidence-root", "", "canonical trusted evidence root locator")
	databaseURL := os.Getenv(databaseURLEnvironment)
	if err := set.Parse(args); err != nil || set.NArg() != 0 || *artifactPath == "" || *repository == "" || *release == "" || *evidenceRoot == "" || databaseURL == "" {
		return errors.New("invalid migration runner configuration")
	}
	evidence, err := migration.NewEvidenceSink(*evidenceRoot)
	if err != nil {
		return err
	}

	// ADR-0009 forbids compiling the test-only accept-any fixture into this CLI.
	// Until detached signature/epoch/revocation trust-root wiring lands, this
	// configured production composition rejects before reading the artifact,
	// opening evidence authority, or connecting to PostgreSQL.
	runner := migration.Runner{
		Trust:        migration.RejectingTrustVerifier{},
		Evidence:     evidence,
		Connector:    migration.PGXConnector{},
		Ledger:       migration.SQLLedgerStore{},
		Authority:    migration.FailClosedAuthorityValidator{},
		Catalog:      migration.FailClosedCatalogValidator{},
		Intermediate: migration.FailClosedIntermediateValidator{},
	}
	_, err = runner.Run(ctx, migration.RunRequest{
		Candidate: migration.CandidateEnvelope{RepositoryIdentity: *repository, ReleaseIdentity: *release},
		Artifact:  migration.FileArtifactSource{Path: *artifactPath},
		TargetDSN: databaseURL,
	})
	return err
}
