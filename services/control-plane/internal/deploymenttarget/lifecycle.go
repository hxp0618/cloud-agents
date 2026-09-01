package deploymenttarget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const LifecycleProfileID = "cloud-agents/deployment-target/v1alpha1"

var (
	ErrInvalidInput        = errors.New("deployment target input is invalid")
	ErrNotFound            = errors.New("deployment target was not found")
	ErrConflict            = errors.New("deployment target transition is invalid")
	ErrIdempotencyConflict = errors.New("deployment target idempotency key conflicts")
)

type Scope struct{ TenantID, ProjectID string }
type Mutation struct{ RequestID, IdempotencyKey string }

type RegisterInput struct {
	Scope                      Scope
	TargetID, TargetName, Kind string
	Endpoint, CredentialRef    string
	Mutation                   Mutation
}

type ProbeInput struct {
	Scope              Scope
	TargetID           string
	ExpectedGeneration int64
	Mutation           Mutation
}

type ProbeCompletion struct {
	Input                               ProbeInput
	Succeeded                           bool
	APIVersion, EngineVersion, OS, Arch string
	StableErrorCode                     string
}

type Snapshot struct {
	Scope                                                Scope
	TargetID, TargetName, Kind, Endpoint, CredentialRef  string
	Generation                                           int64
	ObservedPhase                                        string
	APIVersion, EngineVersion, OS, Arch, StableErrorCode string
	LastProbeAt                                          *time.Time
	ResourceVersion                                      int64
	CreatedAt, UpdatedAt                                 time.Time
}

type ProbeStart struct {
	Target  Snapshot
	Execute bool
}

func (input RegisterInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.TargetID) || invalidIdentifier(input.TargetName) || !validKind(input.Kind) ||
		!validEndpoint(input.Endpoint) || invalidIdentifier(input.CredentialRef) {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func (input ProbeInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.TargetID) || input.ExpectedGeneration < 1 {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func RegisterMutationDigest(input RegisterInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, TargetID, TargetName, Kind, Endpoint, CredentialRef string
	}{"deployment-target.register", input.Scope.TenantID, input.Scope.ProjectID, input.TargetID, input.TargetName, input.Kind, input.Endpoint, input.CredentialRef}), nil
}

func ProbeMutationDigest(input ProbeInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, TargetID string
		ExpectedGeneration                       int64
	}{"deployment-target.probe", input.Scope.TenantID, input.Scope.ProjectID, input.TargetID, input.ExpectedGeneration}), nil
}

func (completion ProbeCompletion) Validate(tenantID string) error {
	if completion.Input.Validate(tenantID) != nil {
		return ErrInvalidInput
	}
	if completion.Succeeded {
		if completion.APIVersion == "" || completion.EngineVersion == "" || completion.OS == "" || completion.Arch == "" || completion.StableErrorCode != "" {
			return ErrInvalidInput
		}
	} else if completion.APIVersion != "" || completion.EngineVersion != "" || completion.OS != "" || completion.Arch != "" || invalidIdentifier(completion.StableErrorCode) {
		return ErrInvalidInput
	}
	for _, value := range []string{completion.APIVersion, completion.EngineVersion, completion.OS, completion.Arch} {
		if len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
			return ErrInvalidInput
		}
	}
	return nil
}

func (snapshot Snapshot) Validate() error {
	if invalidIdentifier(snapshot.Scope.TenantID) || invalidIdentifier(snapshot.Scope.ProjectID) ||
		invalidIdentifier(snapshot.TargetID) || invalidIdentifier(snapshot.TargetName) || !validKind(snapshot.Kind) ||
		!validEndpoint(snapshot.Endpoint) || invalidIdentifier(snapshot.CredentialRef) || snapshot.Generation < 1 ||
		snapshot.ResourceVersion < 1 || snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.IsZero() {
		return ErrInvalidInput
	}
	switch snapshot.ObservedPhase {
	case "unprobed", "probing":
		if snapshot.APIVersion != "" || snapshot.EngineVersion != "" || snapshot.OS != "" || snapshot.Arch != "" || snapshot.StableErrorCode != "" {
			return ErrInvalidInput
		}
	case "ready":
		if snapshot.LastProbeAt == nil || snapshot.APIVersion == "" || snapshot.EngineVersion == "" || snapshot.OS == "" || snapshot.Arch == "" || snapshot.StableErrorCode != "" {
			return ErrInvalidInput
		}
	case "unavailable":
		if snapshot.LastProbeAt == nil || invalidIdentifier(snapshot.StableErrorCode) || snapshot.APIVersion != "" || snapshot.EngineVersion != "" || snapshot.OS != "" || snapshot.Arch != "" {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func validEndpoint(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		(parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Opaque == ""
}

func validKind(value string) bool { return value == "docker" || value == "kubernetes" }

func invalidIdentifier(value string) bool {
	return commonv1alpha1.ValidateIdentifier(value, "/value") != nil
}

func validateMutation(mutation Mutation) error {
	if invalidIdentifier(mutation.RequestID) || len(mutation.IdempotencyKey) < 16 || len(mutation.IdempotencyKey) > 128 || invalidIdentifier(mutation.IdempotencyKey) {
		return ErrInvalidInput
	}
	return nil
}

func digest(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}
