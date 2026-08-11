package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
)

func main() {
	artifactPath := flag.String("artifact", "", "path to the deterministic runtime migration tar")
	repository := flag.String("repository", "", "expected signed repository identity")
	release := flag.String("release", "", "expected signed release identity")
	flag.Parse()

	// ADR-0009 forbids compiling the test-only accept-any fixture into this CLI.
	// Until detached signature/epoch/revocation and PG projection adapters land,
	// production wiring rejects before reading the artifact or connecting to DB.
	runner := migration.Runner{
		Trust:        migration.RejectingTrustVerifier{},
		Connector:    migration.PGXConnector{},
		Ledger:       migration.SQLLedgerStore{},
		Authority:    migration.FailClosedAuthorityValidator{},
		Catalog:      migration.FailClosedCatalogValidator{},
		Intermediate: migration.FailClosedIntermediateValidator{},
	}
	_, err := runner.Run(context.Background(), migration.RunRequest{
		Candidate: migration.CandidateEnvelope{RepositoryIdentity: *repository, ReleaseIdentity: *release},
		Artifact:  migration.FileArtifactSource{Path: *artifactPath},
		TargetDSN: os.Getenv("CLOUD_AGENTS_PLATFORM_DATABASE_URL"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
