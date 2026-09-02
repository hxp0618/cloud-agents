// Package managedhost contains the transport-neutral Managed Host lease
// admission and deployment lifecycle.
package managedhost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	LifecycleProfileID = "cloud-agents/managed-host-environment-lease/v1alpha1"
	MinTTLSeconds      = int64(60)
	MaxTTLSeconds      = int64(86400)
	maxIdentifierBytes = 128
)

var (
	ErrInvalidInput        = errors.New("managed host environment lease input is invalid")
	ErrNotFound            = errors.New("managed host environment lease was not found")
	ErrConflict            = errors.New("managed host environment lease transition is invalid")
	ErrIdempotencyConflict = errors.New("managed host environment lease idempotency key conflicts")
)

type Scope struct{ TenantID, ProjectID string }
type Mutation struct{ RequestID, IdempotencyKey string }

type CreateEnvironmentLeaseInput struct {
	Scope                                Scope
	LeaseID, LeaseName, ReleaseDigest    string
	TargetID                             string
	ProviderCredentialRef                string
	TTLSeconds, ExpectedTargetGeneration int64
	CPULimitMillis, MemoryLimitBytes     int64
	Mutation                             Mutation
}

type TerminateEnvironmentLeaseInput struct {
	Scope              Scope
	LeaseID            string
	ExpectedGeneration int64
	Mutation           Mutation
}

type UpgradeEnvironmentLeaseInput struct {
	Scope              Scope
	LeaseID            string
	ReleaseDigest      string
	ExpectedGeneration int64
	Mutation           Mutation
}

type CompleteEnvironmentLeaseTerminationInput struct {
	Scope              Scope
	LeaseID            string
	ExpectedGeneration int64
}

type CompleteEnvironmentLeaseDeploymentInput struct {
	Scope                                            Scope
	LeaseID, TargetID                                string
	ExpectedGeneration, ExpectedTargetGeneration     int64
	Succeeded                                        bool
	WorkerEndpoint, WorkerSPIFFEID, WorkerServerName string
	StableErrorCode                                  string
}

type Snapshot struct {
	Scope                                            Scope
	LeaseID, LeaseName, ReleaseDigest, TargetID      string
	ProviderCredentialRef                            string
	Generation, TargetGeneration                     int64
	CPULimitMillis, MemoryLimitBytes                 int64
	DesiredPhase, ObservedPhase, CleanupPhase        string
	EnvironmentID                                    string
	WorkerEndpoint, WorkerSPIFFEID, WorkerServerName string
	StableErrorCode                                  string
	ExpiresAt, CreatedAt, UpdatedAt                  time.Time
	ResourceVersion                                  int64
}

type UpgradeStart struct {
	Snapshot Snapshot
	Execute  bool
}

func (input CreateEnvironmentLeaseInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || !validIdentifier(input.Scope.ProjectID) ||
		!validIdentifier(input.LeaseID) || !validIdentifier(input.LeaseName) ||
		!validDigest(input.ReleaseDigest) || !validIdentifier(input.TargetID) ||
		!validIdentifier(input.ProviderCredentialRef) || input.CPULimitMillis < 100 || input.CPULimitMillis > 64_000 ||
		input.MemoryLimitBytes < 128<<20 || input.MemoryLimitBytes > 1<<40 ||
		input.ExpectedTargetGeneration < 1 || input.TTLSeconds < MinTTLSeconds || input.TTLSeconds > MaxTTLSeconds {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func (input TerminateEnvironmentLeaseInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || !validIdentifier(input.Scope.ProjectID) ||
		!validIdentifier(input.LeaseID) || input.ExpectedGeneration < 1 {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func (input UpgradeEnvironmentLeaseInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || !validIdentifier(input.Scope.ProjectID) ||
		!validIdentifier(input.LeaseID) || !validDigest(input.ReleaseDigest) || input.ExpectedGeneration < 1 {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func (input CompleteEnvironmentLeaseTerminationInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || !validIdentifier(input.Scope.ProjectID) ||
		!validIdentifier(input.LeaseID) || input.ExpectedGeneration < 1 {
		return ErrInvalidInput
	}
	return nil
}

func (input CompleteEnvironmentLeaseDeploymentInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || !validIdentifier(input.Scope.ProjectID) || !validIdentifier(input.LeaseID) || !validIdentifier(input.TargetID) || input.ExpectedGeneration < 1 || input.ExpectedTargetGeneration < 1 {
		return ErrInvalidInput
	}
	if input.Succeeded {
		if !validWorkerEndpoint(input.WorkerEndpoint) || !validWorkerSPIFFEID(input.WorkerSPIFFEID) || !validWorkerServerName(input.WorkerServerName) || input.StableErrorCode != "" {
			return ErrInvalidInput
		}
	} else if input.WorkerEndpoint != "" || input.WorkerSPIFFEID != "" || input.WorkerServerName != "" || !validIdentifier(input.StableErrorCode) {
		return ErrInvalidInput
	}
	return nil
}

func CreateMutationDigest(input CreateEnvironmentLeaseInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, LeaseID, LeaseName, ReleaseDigest, TargetID, ProviderCredentialRef string
		TTLSeconds, ExpectedTargetGeneration, CPULimitMillis, MemoryLimitBytes                             int64
	}{"environment-lease.create", input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.LeaseName, input.ReleaseDigest, input.TargetID, input.ProviderCredentialRef, input.TTLSeconds, input.ExpectedTargetGeneration, input.CPULimitMillis, input.MemoryLimitBytes}), nil
}

func TerminateMutationDigest(input TerminateEnvironmentLeaseInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, LeaseID string
		ExpectedGeneration                      int64
	}{"environment-lease.terminate", input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.ExpectedGeneration}), nil
}

func UpgradeMutationDigest(input UpgradeEnvironmentLeaseInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, LeaseID, ReleaseDigest string
		ExpectedGeneration                                     int64
	}{"environment-lease.upgrade", input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.ReleaseDigest, input.ExpectedGeneration}), nil
}

func validIdentifier(value string) bool {
	if !utf8.ValidString(value) || len(value) == 0 || len(value) > maxIdentifierBytes {
		return false
	}
	if !isAlphaNumeric(value[0]) || !isAlphaNumeric(value[len(value)-1]) {
		return false
	}
	for _, char := range value {
		if !(char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("._~-", char)) {
			return false
		}
	}
	return true
}

func isAlphaNumeric(char byte) bool {
	return char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
}

func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[7:] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func validateMutation(mutation Mutation) error {
	if !validIdentifier(mutation.RequestID) || len(mutation.IdempotencyKey) < 16 || len(mutation.IdempotencyKey) > 128 || !validIdentifier(mutation.IdempotencyKey) {
		return ErrInvalidInput
	}
	return nil
}

func (snapshot Snapshot) Validate() error {
	if !validIdentifier(snapshot.Scope.TenantID) || !validIdentifier(snapshot.Scope.ProjectID) ||
		!validIdentifier(snapshot.LeaseID) || !validIdentifier(snapshot.LeaseName) ||
		!validDigest(snapshot.ReleaseDigest) || !validIdentifier(snapshot.EnvironmentID) ||
		(snapshot.TargetID == "") != (snapshot.TargetGeneration == 0) ||
		snapshot.TargetID != "" && (!validIdentifier(snapshot.TargetID) || snapshot.TargetGeneration < 1) ||
		!validDeploymentBinding(snapshot) ||
		snapshot.Generation < 1 || snapshot.ResourceVersion < 1 || snapshot.ExpiresAt.IsZero() ||
		snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.IsZero() ||
		!validPhase(snapshot.DesiredPhase, "active", "terminated") ||
		!validPhase(snapshot.ObservedPhase, "provisioning", "ready", "terminating", "terminated", "failed") ||
		!validPhase(snapshot.CleanupPhase, "none", "pending", "revoking", "reaping", "complete", "blocked") {
		return ErrInvalidInput
	}
	return nil
}

func validDeploymentBinding(snapshot Snapshot) bool {
	legacy := snapshot.ProviderCredentialRef == "" && snapshot.CPULimitMillis == 0 && snapshot.MemoryLimitBytes == 0 && snapshot.WorkerEndpoint == "" && snapshot.WorkerSPIFFEID == "" && snapshot.WorkerServerName == "" && snapshot.StableErrorCode == ""
	if legacy {
		return true
	}
	if !validIdentifier(snapshot.ProviderCredentialRef) || snapshot.CPULimitMillis < 100 || snapshot.CPULimitMillis > 64_000 || snapshot.MemoryLimitBytes < 128<<20 || snapshot.MemoryLimitBytes > 1<<40 {
		return false
	}
	switch snapshot.ObservedPhase {
	case "provisioning":
		return snapshot.WorkerEndpoint == "" && snapshot.WorkerSPIFFEID == "" && snapshot.WorkerServerName == "" && snapshot.StableErrorCode == ""
	case "ready":
		return validWorkerEndpoint(snapshot.WorkerEndpoint) && validWorkerSPIFFEID(snapshot.WorkerSPIFFEID) && (snapshot.WorkerServerName == "" || validWorkerServerName(snapshot.WorkerServerName)) && snapshot.StableErrorCode == ""
	case "failed":
		return snapshot.WorkerEndpoint == "" && snapshot.WorkerSPIFFEID == "" && snapshot.WorkerServerName == "" && validIdentifier(snapshot.StableErrorCode)
	case "terminating", "terminated":
		return snapshot.StableErrorCode == "" && (snapshot.WorkerEndpoint == "" && snapshot.WorkerSPIFFEID == "" && snapshot.WorkerServerName == "" || validWorkerEndpoint(snapshot.WorkerEndpoint) && validWorkerSPIFFEID(snapshot.WorkerSPIFFEID) && (snapshot.WorkerServerName == "" || validWorkerServerName(snapshot.WorkerServerName)))
	default:
		return false
	}
}

func validWorkerEndpoint(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validWorkerSPIFFEID(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && parsed.Scheme == "spiffe" && parsed.Host != "" && parsed.Path != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validWorkerServerName(value string) bool {
	return value != "" && len(value) <= 253 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "/@") && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validPhase(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func digest(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}
