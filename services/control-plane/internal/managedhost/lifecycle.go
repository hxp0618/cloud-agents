// Package managedhost contains the transport-neutral Managed Host lease
// admission and deployment lifecycle.
package managedhost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	LifecycleProfileID = "cloud-agents/managed-host-environment-lease/v1alpha1"
	MinTTLSeconds      = int64(60)
	MaxTTLSeconds      = int64(86400)
	DefaultTTLSeconds  = int64(3600)
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

type CreateEnvironmentFromProfileInput struct {
	Scope          Scope
	ProfileID      string
	ProfileVersion int64
	Mutation       Mutation
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

type AdminUpgradeAuthority struct {
	Lease                                                  Snapshot
	TargetKind, TargetSchedulingState, TargetObservedPhase string
	RollbackReleaseDigest                                  string
	RollbackGeneration                                     int64
	TargetReleaseApproved                                  bool
}

type AdminEnvironmentLeaseUpgradePreview struct {
	Lease                                                          Snapshot
	Action, TargetKind, TargetReleaseDigest, RollbackReleaseDigest string
	RollbackGeneration                                             int64
	ImpactDigest                                                   string
}

type AdminEnvironmentLeaseUpgradeInput struct {
	Scope                   Scope
	LeaseID                 string
	Action                  string
	ReleaseDigest           string
	ExpectedGeneration      int64
	ExpectedResourceVersion int64
	ImpactDigest            string
	Mutation                Mutation
}

type CompleteAdminEnvironmentLeaseUpgradeInput struct {
	Upgrade       AdminEnvironmentLeaseUpgradeInput
	Deployment    CompleteEnvironmentLeaseDeploymentInput
	ImpactSummary string
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

type ProfileEnvironmentSnapshot struct {
	Lease          Snapshot
	ProfileID      string
	ProfileVersion int64
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

func (input CreateEnvironmentFromProfileInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || !validIdentifier(input.Scope.ProjectID) ||
		!validIdentifier(input.ProfileID) || input.ProfileVersion < 1 || input.ProfileVersion > 2147483647 {
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

func (input AdminEnvironmentLeaseUpgradeInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || !validIdentifier(input.Scope.ProjectID) ||
		!validIdentifier(input.LeaseID) || !validAdminUpgradeAction(input.Action) || !validDigest(input.ReleaseDigest) ||
		input.ExpectedGeneration < 1 || input.ExpectedResourceVersion < 1 || !validDigest(input.ImpactDigest) {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func (input CompleteAdminEnvironmentLeaseUpgradeInput) Validate(tenantID string) error {
	if input.Upgrade.Validate(tenantID) != nil || input.Deployment.Validate(tenantID) != nil ||
		input.Deployment.Scope != input.Upgrade.Scope || input.Deployment.LeaseID != input.Upgrade.LeaseID ||
		input.Deployment.ExpectedGeneration != input.Upgrade.ExpectedGeneration+1 ||
		len(input.ImpactSummary) < 1 || len(input.ImpactSummary) > 256 || strings.ContainsAny(input.ImpactSummary, "\r\n\x00") {
		return ErrInvalidInput
	}
	return nil
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

func CreateFromProfileMutationDigest(input CreateEnvironmentFromProfileInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, ProfileID string
		ProfileVersion                            int64
	}{"user-environment.create", input.Scope.TenantID, input.Scope.ProjectID, input.ProfileID, input.ProfileVersion}), nil
}

func UserEnvironmentID(input CreateEnvironmentFromProfileInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(input.Scope.TenantID + "\x00" + input.Scope.ProjectID + "\x00" + input.Mutation.IdempotencyKey))
	return "environment-" + hex.EncodeToString(sum[:16]), nil
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

func NewAdminEnvironmentLeaseUpgradePreview(authority AdminUpgradeAuthority, action, releaseDigest string) (AdminEnvironmentLeaseUpgradePreview, error) {
	lease := authority.Lease
	if lease.Validate() != nil || !validAdminUpgradeAction(action) ||
		!validPhase(authority.TargetKind, "docker", "kubernetes", "ssh") || authority.TargetSchedulingState != "drained" || authority.TargetObservedPhase != "ready" ||
		lease.DesiredPhase != "active" || !validPhase(lease.ObservedPhase, "ready", "failed") || lease.CleanupPhase != "none" || !authority.TargetReleaseApproved {
		return AdminEnvironmentLeaseUpgradePreview{}, ErrConflict
	}
	targetDigest, rollbackDigest, rollbackGeneration := releaseDigest, lease.ReleaseDigest, lease.Generation
	if action == "rollback" {
		targetDigest, rollbackDigest, rollbackGeneration = authority.RollbackReleaseDigest, authority.RollbackReleaseDigest, authority.RollbackGeneration
	}
	if !validDigest(targetDigest) || !validDigest(rollbackDigest) || rollbackGeneration < 1 || targetDigest == lease.ReleaseDigest {
		return AdminEnvironmentLeaseUpgradePreview{}, ErrConflict
	}
	impactDigest, err := AdminUpgradeImpactDigest(lease, authority.TargetKind, action, targetDigest, rollbackDigest, rollbackGeneration)
	if err != nil {
		return AdminEnvironmentLeaseUpgradePreview{}, err
	}
	return AdminEnvironmentLeaseUpgradePreview{Lease: lease, Action: action, TargetKind: authority.TargetKind, TargetReleaseDigest: targetDigest, RollbackReleaseDigest: rollbackDigest, RollbackGeneration: rollbackGeneration, ImpactDigest: impactDigest}, nil
}

func AdminUpgradeImpactDigest(lease Snapshot, targetKind, action, targetReleaseDigest, rollbackReleaseDigest string, rollbackGeneration int64) (string, error) {
	if lease.Validate() != nil || !validPhase(targetKind, "docker", "kubernetes", "ssh") || !validAdminUpgradeAction(action) ||
		!validDigest(targetReleaseDigest) || !validDigest(rollbackReleaseDigest) || rollbackGeneration < 1 {
		return "", ErrInvalidInput
	}
	return digest(struct {
		Operation, TenantID, ProjectID, LeaseID, TargetID, TargetKind            string
		Action, CurrentReleaseDigest, TargetReleaseDigest, RollbackReleaseDigest string
		Generation, ResourceVersion, TargetGeneration, RollbackGeneration        int64
	}{"admin.environment-lease.release-transition", lease.Scope.TenantID, lease.Scope.ProjectID, lease.LeaseID, lease.TargetID, targetKind, action, lease.ReleaseDigest, targetReleaseDigest, rollbackReleaseDigest, lease.Generation, lease.ResourceVersion, lease.TargetGeneration, rollbackGeneration}), nil
}

func AdminUpgradeMutationDigest(input AdminEnvironmentLeaseUpgradeInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	return digest(struct {
		Operation, TenantID, ProjectID, LeaseID, Action, ReleaseDigest, ImpactDigest string
		ExpectedGeneration, ExpectedResourceVersion                                  int64
	}{"admin.environment-lease.release-transition", input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.Action, input.ReleaseDigest, input.ImpactDigest, input.ExpectedGeneration, input.ExpectedResourceVersion}), nil
}

func AdminUpgradeImpactSummary(action string, generation int64) (string, error) {
	if !validAdminUpgradeAction(action) || generation < 1 {
		return "", ErrInvalidInput
	}
	verb := "Upgrade"
	if action == "rollback" {
		verb = "Rollback"
	}
	return verb + " 1 Worker and 1 Lease from generation " + strconv.FormatInt(generation, 10), nil
}

func validAdminUpgradeAction(value string) bool {
	return value == "upgrade" || value == "rollback"
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

func (snapshot ProfileEnvironmentSnapshot) Validate() error {
	if snapshot.Lease.Validate() != nil || !validIdentifier(snapshot.ProfileID) ||
		snapshot.ProfileVersion < 1 || snapshot.ProfileVersion > 2147483647 {
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
