// Package managedhost contains the transport-neutral Managed Host lease
// admission lifecycle. This slice persists admission state only; no external
// workload, volume, provider, or Worker actuator is attached yet.
package managedhost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
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
	Scope                             Scope
	LeaseID, LeaseName, ReleaseDigest string
	TTLSeconds                        int64
	Mutation                          Mutation
}

type TerminateEnvironmentLeaseInput struct {
	Scope              Scope
	LeaseID            string
	ExpectedGeneration int64
	Mutation           Mutation
}

type Snapshot struct {
	Scope                                     Scope
	LeaseID, LeaseName, ReleaseDigest         string
	Generation                                int64
	DesiredPhase, ObservedPhase, CleanupPhase string
	EnvironmentID                             string
	ExpiresAt, CreatedAt, UpdatedAt           time.Time
	ResourceVersion                           int64
}

func (input CreateEnvironmentLeaseInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || !validIdentifier(input.Scope.ProjectID) ||
		!validIdentifier(input.LeaseID) || !validIdentifier(input.LeaseName) ||
		!validDigest(input.ReleaseDigest) || input.TTLSeconds < MinTTLSeconds || input.TTLSeconds > MaxTTLSeconds {
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

func CreateMutationDigest(input CreateEnvironmentLeaseInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, LeaseID, LeaseName, ReleaseDigest string
		TTLSeconds                                                        int64
	}{"environment-lease.create", input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.LeaseName, input.ReleaseDigest, input.TTLSeconds}), nil
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
		snapshot.Generation < 1 || snapshot.ResourceVersion < 1 || snapshot.ExpiresAt.IsZero() ||
		snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.IsZero() ||
		!validPhase(snapshot.DesiredPhase, "active", "terminated") ||
		!validPhase(snapshot.ObservedPhase, "provisioning", "ready", "terminating", "terminated", "failed") ||
		!validPhase(snapshot.CleanupPhase, "none", "pending", "revoking", "reaping", "complete", "blocked") {
		return ErrInvalidInput
	}
	return nil
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
