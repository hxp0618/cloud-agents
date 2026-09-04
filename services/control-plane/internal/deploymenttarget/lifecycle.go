package deploymenttarget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
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

type CleanupInput struct {
	Scope                   Scope
	TargetID                string
	ExpectedGeneration      int64
	ExpectedResourceVersion int64
	ImpactDigest            string
	Mutation                Mutation
}

type CleanupCompletion struct {
	Input           CleanupInput
	Succeeded       bool
	StableErrorCode string
	ImpactSummary   string
}

type SchedulingLease struct {
	LeaseID, LeaseName, ObservedPhase string
	Generation                        int64
}

type SchedulingPreview struct {
	Target       Snapshot
	DesiredState string
	ImpactDigest string
	ActiveLeases []SchedulingLease
}

type SchedulingInput struct {
	Scope                   Scope
	TargetID                string
	ExpectedGeneration      int64
	ExpectedResourceVersion int64
	DesiredState            string
	ImpactDigest            string
	Mutation                Mutation
}

type Snapshot struct {
	Scope                                                Scope
	TargetID, TargetName, Kind, Endpoint, CredentialRef  string
	Generation                                           int64
	SchedulingState                                      string
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

type CleanupStart struct {
	Operation Operation
	Execute   bool
}

func (input RegisterInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.TargetID) || invalidIdentifier(input.TargetName) || !validKind(input.Kind) ||
		!validEndpoint(input.Kind, input.Endpoint) || invalidIdentifier(input.CredentialRef) {
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

func (input CleanupInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) || invalidIdentifier(input.TargetID) ||
		input.ExpectedGeneration < 1 || input.ExpectedResourceVersion < 1 || !digestPattern.MatchString(input.ImpactDigest) {
		return ErrInvalidInput
	}
	return validateMutation(input.Mutation)
}

func (input SchedulingInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) || invalidIdentifier(input.TargetID) ||
		input.ExpectedGeneration < 1 || input.ExpectedResourceVersion < 1 || !validSchedulingState(input.DesiredState) ||
		!digestPattern.MatchString(input.ImpactDigest) {
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

func CleanupMutationDigest(input CleanupInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, TargetID, ImpactDigest string
		ExpectedGeneration, ExpectedResourceVersion            int64
	}{"deployment-target.cleanup", input.Scope.TenantID, input.Scope.ProjectID, input.TargetID, input.ImpactDigest, input.ExpectedGeneration, input.ExpectedResourceVersion}), nil
}

func SchedulingMutationDigest(input SchedulingInput) (string, error) {
	if err := input.Validate(input.Scope.TenantID); err != nil {
		return "", err
	}
	return digest(struct {
		Operation, TenantID, ProjectID, TargetID, DesiredState, ImpactDigest string
		ExpectedGeneration, ExpectedResourceVersion                          int64
	}{"deployment-target.scheduling", input.Scope.TenantID, input.Scope.ProjectID, input.TargetID, input.DesiredState, input.ImpactDigest, input.ExpectedGeneration, input.ExpectedResourceVersion}), nil
}

func NewSchedulingPreview(target Snapshot, leases []SchedulingLease) (SchedulingPreview, error) {
	if target.Validate() != nil || validateSchedulingLeases(leases) != nil {
		return SchedulingPreview{}, ErrInvalidInput
	}
	desiredState := "active"
	if target.SchedulingState == "active" {
		desiredState = "drained"
	}
	impactDigest, err := SchedulingImpactDigest(target, desiredState, leases)
	if err != nil {
		return SchedulingPreview{}, err
	}
	return SchedulingPreview{Target: target, DesiredState: desiredState, ImpactDigest: impactDigest, ActiveLeases: leases}, nil
}

func SchedulingImpactDigest(target Snapshot, desiredState string, leases []SchedulingLease) (string, error) {
	if target.Validate() != nil || !validSchedulingState(desiredState) || validateSchedulingLeases(leases) != nil {
		return "", ErrInvalidInput
	}
	return digest(struct {
		TargetID                    string
		Generation, ResourceVersion int64
		CurrentState, DesiredState  string
		ActiveLeases                []SchedulingLease
	}{target.TargetID, target.Generation, target.ResourceVersion, target.SchedulingState, desiredState, leases}), nil
}

func SchedulingImpactSummary(desiredState string, activeLeaseCount int) (string, error) {
	if !validSchedulingState(desiredState) || activeLeaseCount < 0 {
		return "", ErrInvalidInput
	}
	if desiredState == "active" {
		return "Resumed target; new lease scheduling is enabled", nil
	}
	return "Drained target; " + strconv.Itoa(activeLeaseCount) + " active leases remain running", nil
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

func (completion CleanupCompletion) Validate(tenantID string) error {
	if completion.Input.Validate(tenantID) != nil || len(completion.ImpactSummary) < 1 || len(completion.ImpactSummary) > 256 ||
		strings.ContainsAny(completion.ImpactSummary, "\r\n\x00") ||
		(completion.Succeeded && completion.StableErrorCode != "") ||
		(!completion.Succeeded && invalidIdentifier(completion.StableErrorCode)) {
		return ErrInvalidInput
	}
	return nil
}

func (snapshot Snapshot) Validate() error {
	if invalidIdentifier(snapshot.Scope.TenantID) || invalidIdentifier(snapshot.Scope.ProjectID) ||
		invalidIdentifier(snapshot.TargetID) || invalidIdentifier(snapshot.TargetName) || !validKind(snapshot.Kind) ||
		!validEndpoint(snapshot.Kind, snapshot.Endpoint) || invalidIdentifier(snapshot.CredentialRef) || snapshot.Generation < 1 ||
		!validSchedulingState(snapshot.SchedulingState) || snapshot.ResourceVersion < 1 || snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.IsZero() {
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

func validEndpoint(kind, value string) bool {
	parsed, err := url.Parse(value)
	scheme := "https"
	if kind == "ssh" {
		scheme = "ssh"
	}
	if err != nil || len(value) > 2048 || parsed.Scheme != scheme || parsed.Hostname() == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return false
	}
	if kind != "ssh" || parsed.Port() == "" {
		return true
	}
	port, err := strconv.Atoi(parsed.Port())
	return err == nil && port > 0 && port <= 65535
}

func validKind(value string) bool {
	return value == "docker" || value == "kubernetes" || value == "ssh"
}

func validSchedulingState(value string) bool {
	return value == "active" || value == "drained"
}

func validateSchedulingLeases(leases []SchedulingLease) error {
	previous := ""
	for _, lease := range leases {
		if invalidIdentifier(lease.LeaseID) || lease.LeaseID <= previous || invalidIdentifier(lease.LeaseName) || lease.Generation < 1 ||
			(lease.ObservedPhase != "provisioning" && lease.ObservedPhase != "ready" && lease.ObservedPhase != "terminating" && lease.ObservedPhase != "terminated" && lease.ObservedPhase != "failed") {
			return ErrInvalidInput
		}
		previous = lease.LeaseID
	}
	return nil
}

func invalidIdentifier(value string) bool {
	return commonv1alpha1.ValidateIdentifier(value, "/value") != nil
}

func validateMutation(mutation Mutation) error {
	if invalidIdentifier(mutation.RequestID) || commonv1alpha1.ValidateIdempotencyKey(mutation.IdempotencyKey, "/idempotencyKey") != nil {
		return ErrInvalidInput
	}
	return nil
}

func digest(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}
