package workerrelease

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const LifecycleProfileID = "cloud-agents/worker-release/v1alpha1"

var (
	ErrInvalidInput        = errors.New("worker release input is invalid")
	ErrConflict            = errors.New("worker release conflicts")
	ErrIdempotencyConflict = errors.New("worker release idempotency key conflicts")
	digestPattern          = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	imageRepositoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]{1,5})?(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$`)
)

type Scope struct{ TenantID, ProjectID string }
type Mutation struct{ RequestID, IdempotencyKey string }

type RegisterInput struct {
	Scope                      Scope
	ReleaseID                  string
	ReleaseName                string
	ImageRepository            string
	ReleaseDigest              string
	PlatformVersion            string
	RuntimeVersion             string
	CodexVersion               string
	ClaudeCodeVersion          string
	Architectures              []string
	VerificationEvidenceDigest string
	Mutation                   Mutation
}

type Snapshot struct {
	Scope                      Scope
	ReleaseID                  string
	ReleaseName                string
	ImageRepository            string
	ReleaseDigest              string
	PlatformVersion            string
	RuntimeVersion             string
	CodexVersion               string
	ClaudeCodeVersion          string
	Architectures              []string
	Status                     string
	VerificationState          string
	VerificationEvidenceDigest string
	ResourceVersion            int64
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	ApprovedAt                 time.Time
}

func (input RegisterInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.ReleaseID) || invalidIdentifier(input.ReleaseName) ||
		!validRelease(input.ImageRepository, input.ReleaseDigest, input.PlatformVersion,
			input.RuntimeVersion, input.CodexVersion, input.ClaudeCodeVersion,
			input.Architectures, input.VerificationEvidenceDigest) ||
		invalidIdentifier(input.Mutation.RequestID) ||
		commonv1alpha1.ValidateIdempotencyKey(input.Mutation.IdempotencyKey, "/idempotencyKey") != nil {
		return ErrInvalidInput
	}
	return nil
}

func RegisterMutationDigest(input RegisterInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	encoded, err := json.Marshal(struct {
		TenantID, ProjectID, ReleaseID, ReleaseName, ImageRepository, ReleaseDigest string
		PlatformVersion, RuntimeVersion, CodexVersion, ClaudeCodeVersion            string
		Architectures                                                               []string
		VerificationEvidenceDigest                                                  string
	}{input.Scope.TenantID, input.Scope.ProjectID, input.ReleaseID, input.ReleaseName,
		input.ImageRepository, input.ReleaseDigest, input.PlatformVersion, input.RuntimeVersion,
		input.CodexVersion, input.ClaudeCodeVersion, input.Architectures,
		input.VerificationEvidenceDigest})
	if err != nil {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (snapshot Snapshot) Validate() error {
	if invalidIdentifier(snapshot.Scope.TenantID) || invalidIdentifier(snapshot.Scope.ProjectID) ||
		invalidIdentifier(snapshot.ReleaseID) || invalidIdentifier(snapshot.ReleaseName) ||
		!validRelease(snapshot.ImageRepository, snapshot.ReleaseDigest, snapshot.PlatformVersion,
			snapshot.RuntimeVersion, snapshot.CodexVersion, snapshot.ClaudeCodeVersion,
			snapshot.Architectures, snapshot.VerificationEvidenceDigest) ||
		snapshot.Status != "approved" || snapshot.VerificationState != "attested" ||
		snapshot.ResourceVersion < 1 || snapshot.CreatedAt.IsZero() ||
		snapshot.UpdatedAt.Before(snapshot.CreatedAt) || snapshot.ApprovedAt.Before(snapshot.CreatedAt) {
		return ErrInvalidInput
	}
	return nil
}

func validRelease(repository, releaseDigest, platformVersion, runtimeVersion, codexVersion, claudeCodeVersion string, architectures []string, evidenceDigest string) bool {
	if len(repository) > 512 || !imageRepositoryPattern.MatchString(repository) ||
		!digestPattern.MatchString(releaseDigest) || !digestPattern.MatchString(evidenceDigest) {
		return false
	}
	for _, version := range []string{platformVersion, runtimeVersion, codexVersion, claudeCodeVersion} {
		if invalidIdentifier(version) {
			return false
		}
	}
	if len(architectures) < 1 || len(architectures) > 2 {
		return false
	}
	seen := make(map[string]struct{}, len(architectures))
	for _, architecture := range architectures {
		if architecture != "linux/amd64" && architecture != "linux/arm64" {
			return false
		}
		if _, duplicate := seen[architecture]; duplicate {
			return false
		}
		seen[architecture] = struct{}{}
	}
	return true
}

func invalidIdentifier(value string) bool {
	return commonv1alpha1.ValidateIdentifier(value, "/value") != nil
}
