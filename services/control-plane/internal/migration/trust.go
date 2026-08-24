package migration

import (
	"context"
	"time"
)

const maxCandidateEnvelopeComponentBytes = 1 << 20

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

// currentEvidenceTrustVerifier is the production Runner extension of
// TrustVerifier. The same verifier invocation returns both the current release
// decision and the deterministic, bounded recovery artifact bytes. Keeping the
// method package-private prevents callers from overlaying recovery bytes from a
// second verifier or from loose runtime input.
type currentEvidenceTrustVerifier interface {
	TrustVerifier
	verifyCurrentEvidence(context.Context, CandidateEnvelope) (VerifiedTrustDecision, []byte, error)
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
	expiresAt                     time.Time
	securityEpoch                 uint64
	projectionBindings            *RunnerProjectionBindings
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
	if decision.projectionBindings != nil {
		bindings := decision.projectionBindings
		if err := bindings.validateAt(time.Now()); err != nil {
			return err
		}
		if bindings.schemaBundleDigest != decision.expectedSchemaBundleDigest ||
			bindings.releaseSubject.RepositoryIdentity != decision.repositoryIdentity ||
			bindings.releaseSubject.ReleaseIdentity != decision.releaseIdentity ||
			bindings.releaseSubject.BootstrapBundleDigest != decision.expectedBootstrapBundleDigest ||
			bindings.releaseSubject.ManifestDigest != decision.expectedManifestDigest ||
			bindings.releaseSubject.OuterArtifactDigest != decision.expectedOuterArtifactDigest ||
			bindings.releaseSubject.RunnerReleaseDigest != decision.expectedRunnerReleaseDigest ||
			!bindings.releaseExpiresAt.Equal(decision.expiresAt) || bindings.releaseSecurityEpoch != decision.securityEpoch {
			return fail(CodeUntrusted, "trust", "projection bindings differ from their release decision", nil)
		}
	}
	return nil
}

func (decision VerifiedTrustDecision) exactlyMatches(other VerifiedTrustDecision) bool {
	if decision.verified != other.verified ||
		decision.expectedSchemaBundleDigest != other.expectedSchemaBundleDigest ||
		decision.expectedBootstrapBundleDigest != other.expectedBootstrapBundleDigest ||
		decision.expectedManifestDigest != other.expectedManifestDigest ||
		decision.expectedOuterArtifactDigest != other.expectedOuterArtifactDigest ||
		decision.expectedRunnerReleaseDigest != other.expectedRunnerReleaseDigest ||
		decision.repositoryIdentity != other.repositoryIdentity ||
		decision.releaseIdentity != other.releaseIdentity ||
		!decision.expiresAt.Equal(other.expiresAt) ||
		decision.securityEpoch != other.securityEpoch {
		return false
	}
	if decision.projectionBindings == nil || other.projectionBindings == nil {
		return decision.projectionBindings == nil && other.projectionBindings == nil
	}
	return decision.projectionBindings.exactlyMatches(*other.projectionBindings)
}

func (decision VerifiedTrustDecision) validateHistorical(bindings RunnerProjectionBindings) error {
	if !decision.verified {
		return fail(CodeUntrusted, "historical-trust", "historical decision was not produced by the recovery verifier", nil)
	}
	for field, digest := range map[string]Digest{
		"schema_bundle": decision.expectedSchemaBundleDigest, "bootstrap_bundle": decision.expectedBootstrapBundleDigest,
		"manifest": decision.expectedManifestDigest, "outer_artifact": decision.expectedOuterArtifactDigest,
		"runner_release": decision.expectedRunnerReleaseDigest,
	} {
		if err := requireDigest("historical-trust."+field, digest); err != nil {
			return fail(CodeUntrusted, "historical-trust", "historical decision contains an invalid digest", err)
		}
	}
	if decision.repositoryIdentity == "" || decision.releaseIdentity == "" || decision.securityEpoch == 0 || decision.projectionBindings == nil || !decision.projectionBindings.historicallyExactlyMatches(bindings) {
		return fail(CodeUntrusted, "historical-trust", "historical decision is missing or differs from its recovered bindings", nil)
	}
	if bindings.schemaBundleDigest != decision.expectedSchemaBundleDigest || bindings.releaseSubject.RepositoryIdentity != decision.repositoryIdentity || bindings.releaseSubject.ReleaseIdentity != decision.releaseIdentity || bindings.releaseSubject.BootstrapBundleDigest != decision.expectedBootstrapBundleDigest || bindings.releaseSubject.ManifestDigest != decision.expectedManifestDigest || bindings.releaseSubject.OuterArtifactDigest != decision.expectedOuterArtifactDigest || bindings.releaseSubject.RunnerReleaseDigest != decision.expectedRunnerReleaseDigest || !bindings.releaseExpiresAt.Equal(decision.expiresAt) || bindings.releaseSecurityEpoch != decision.securityEpoch {
		return fail(CodeUntrusted, "historical-trust", "historical projection bindings differ from their release decision", nil)
	}
	return nil
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

// runnerProjectionBindings returns an owned copy of the opaque projection
// decision. It is deliberately package-private: callers outside the verified
// runner path cannot supply or overlay deployment projection subjects.
func (decision VerifiedTrustDecision) runnerProjectionBindings() (RunnerProjectionBindings, error) {
	if decision.projectionBindings == nil {
		return RunnerProjectionBindings{}, fail(CodeUntrusted, "runner-projection-bindings", "verified release decision has no projection bindings", nil)
	}
	if err := decision.projectionBindings.validateAt(time.Now()); err != nil {
		return RunnerProjectionBindings{}, err
	}
	return decision.projectionBindings.ownedCopy(), nil
}

// RejectingTrustVerifier is the production-safe default until detached
// signature and deployment trust-root wiring is supplied.
type RejectingTrustVerifier struct{}

func (RejectingTrustVerifier) Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error) {
	return VerifiedTrustDecision{}, fail(CodeUntrusted, "trust", "no production trust verifier is configured", nil)
}

func (RejectingTrustVerifier) verifyCurrentEvidence(context.Context, CandidateEnvelope) (VerifiedTrustDecision, []byte, error) {
	return VerifiedTrustDecision{}, nil, fail(CodeUntrusted, "trust", "no production trust verifier is configured", nil)
}
