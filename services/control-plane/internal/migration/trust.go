package migration

import (
	"context"
	"time"
)

// CandidateEnvelope is opaque signed input. Implementations must verify it
// against deployment trust roots rather than trusting fields inside the bundle.
type CandidateEnvelope struct {
	RepositoryIdentity string
	ReleaseIdentity    string
	Subject            []byte
	DetachedEnvelope   []byte
	Now                time.Time
}

type TrustVerifier interface {
	Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error)
}

// VerifiedTrustDecision has no public constructor. A production verifier in
// this package must set verified after signature, epoch, expiry, revocation,
// repository, release, and launched-runner binding checks succeed.
type VerifiedTrustDecision struct {
	verified                      bool
	expectedSchemaBundleDigest    Digest
	expectedBootstrapBundleDigest Digest
	expectedManifestDigest        Digest
	expectedOuterArtifactDigest   Digest
	expectedRunnerReleaseDigest   Digest
	repositoryIdentity            string
	releaseIdentity               string
	securityEpoch                 uint64
}

func (decision VerifiedTrustDecision) validate() error {
	if !decision.verified {
		return fail(CodeUntrusted, "trust", "decision was not produced by a verified trust provider", nil)
	}
	for field, digest := range map[string]Digest{
		"schema_bundle":    decision.expectedSchemaBundleDigest,
		"bootstrap_bundle": decision.expectedBootstrapBundleDigest,
		"manifest":         decision.expectedManifestDigest,
		"outer_artifact":   decision.expectedOuterArtifactDigest,
		"runner_release":   decision.expectedRunnerReleaseDigest,
	} {
		if err := requireDigest("trust."+field, digest); err != nil {
			return fail(CodeUntrusted, "trust", "verified decision contains an invalid digest", err)
		}
	}
	if decision.repositoryIdentity == "" || decision.releaseIdentity == "" || decision.securityEpoch == 0 {
		return fail(CodeUntrusted, "trust", "verified decision is missing release identity", nil)
	}
	return nil
}

func (decision VerifiedTrustDecision) exactlyMatches(other VerifiedTrustDecision) bool {
	return decision == other
}

func (decision VerifiedTrustDecision) SchemaBundleDigest() Digest {
	return decision.expectedSchemaBundleDigest
}
func (decision VerifiedTrustDecision) BootstrapBundleDigest() Digest {
	return decision.expectedBootstrapBundleDigest
}
func (decision VerifiedTrustDecision) ManifestDigest() Digest { return decision.expectedManifestDigest }
func (decision VerifiedTrustDecision) OuterArtifactDigest() Digest {
	return decision.expectedOuterArtifactDigest
}
func (decision VerifiedTrustDecision) RunnerReleaseDigest() Digest {
	return decision.expectedRunnerReleaseDigest
}
func (decision VerifiedTrustDecision) RepositoryIdentity() string { return decision.repositoryIdentity }
func (decision VerifiedTrustDecision) ReleaseIdentity() string    { return decision.releaseIdentity }
func (decision VerifiedTrustDecision) SecurityEpoch() uint64      { return decision.securityEpoch }

// RejectingTrustVerifier is the production-safe default until detached
// signature and deployment trust-root wiring is supplied.
type RejectingTrustVerifier struct{}

func (RejectingTrustVerifier) Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error) {
	return VerifiedTrustDecision{}, fail(CodeUntrusted, "trust", "no production trust verifier is configured", nil)
}
