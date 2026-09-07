package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RuntimeProfilePublish    = "publish"
	RuntimeProfileDisable    = "disable"
	FoundationSandboxStop    = "stop"
	FoundationSandboxRebuild = "rebuild"
)

var (
	ErrInvalidRuntimeProfile     = errors.New("runtime profile input is invalid")
	ErrRuntimeProfileNotFound    = errors.New("runtime profile was not found")
	ErrRuntimeProfileConflict    = errors.New("runtime profile conflicts")
	ErrRuntimeProfileUnavailable = errors.New("runtime profile is unavailable")
	ErrFoundationSandboxConflict = errors.New("foundation sandbox conflicts")
	ErrFoundationSandboxNotFound = errors.New("foundation sandbox was not found")
	runtimeProfileDigestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	runtimeProfileImagePattern   = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$`)
	runtimeProfileIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9._~-]{16,128}$`)
)

type FoundationScope struct{ TenantID, ProjectID string }
type FoundationMutation struct{ RequestID, IdempotencyKey string }

type RuntimeProfileTargetSelector struct {
	RegionID, ResourcePoolID, Runtime, Architecture string
}

type RuntimeProfileCreateInput struct {
	Scope                               FoundationScope
	ProfileID, ProfileName, Description string
	WorkloadTrust, IsolationRuntime     string
	TargetID, NetworkPolicyID           string
	TargetSelector                      *RuntimeProfileTargetSelector
	ImageURI, ReleaseDigest             string
	Version, CPUMillis, MemoryBytes     int64
	Mutation                            FoundationMutation
}

type RuntimeProfileTransitionInput struct {
	Scope                            FoundationScope
	ProfileID, Action                string
	Version, ExpectedResourceVersion int64
	Mutation                         FoundationMutation
}

type RuntimeProfileSnapshot struct {
	Scope                                          FoundationScope
	ProfileVersionID, ProfileID, ProfileName       string
	Description, Status, TargetID, NetworkPolicyID string
	WorkloadTrust, IsolationRuntime                string
	TargetSelector                                 *RuntimeProfileTargetSelector
	ImageURI                                       string
	ReleaseDigest                                  string
	Version, CPUMillis, MemoryBytes                int64
	ResourceVersion                                int64
	CreatedAt, UpdatedAt                           time.Time
	PublishedAt, DisabledAt                        *time.Time
}

type RuntimeProfileSummary struct {
	Scope                                    FoundationScope
	ProfileVersionID, ProfileID, ProfileName string
	Description                              string
	Version, CPUMillis, MemoryBytes          int64
}

type FoundationSandboxCreateInput struct {
	Scope                                 FoundationScope
	WorkspaceID, WorkspaceName, SandboxID string
	RuntimeProfileID                      string
	RuntimeProfileVersion, TTLSeconds     int64
	Mutation                              FoundationMutation
}

type FoundationSandboxSnapshot struct {
	Scope                               FoundationScope
	OperationID, WorkspaceID, SandboxID string
	RuntimeProfileID                    string
	RuntimeProfileVersion, Generation   int64
	TTLSeconds                          int64
	DesiredState, ObservedState         string
	ExpiresAt                           time.Time
}

type FoundationSandboxLifecycleInput struct {
	Scope                                       FoundationScope
	SandboxID, Action, ConfirmedSandboxID       string
	ComputeDisposition, WorkspaceDisposition    string
	ExpectedGeneration, ExpectedResourceVersion int64
	Mutation                                    FoundationMutation
}

type FoundationSandboxLifecycleOperation struct {
	Scope                                                        FoundationScope
	OperationID, IdempotencyKey, Action, SandboxID               string
	RequestedBy, RequestID, State, CleanupPhase, StableErrorCode string
	ComputeDisposition, WorkspaceDisposition                     string
	SandboxGeneration                                            int64
	RequestedAt, UpdatedAt                                       time.Time
}

func (input RuntimeProfileCreateInput) Validate(tenantID string) error {
	if !validFoundationScope(input.Scope, tenantID) || !validIdentifier(input.ProfileID) ||
		!validIdentifier(input.ProfileName) || input.Version < 1 || input.Version > 2147483647 ||
		invalidRuntimeProfileDescription(input.Description) || invalidRuntimeProfileTargetSelection(input.TargetID, input.TargetSelector) ||
		invalidRuntimeIsolation(input.WorkloadTrust, input.IsolationRuntime) ||
		!validIdentifier(input.NetworkPolicyID) ||
		len(input.ImageURI) > 1024 || !runtimeProfileImagePattern.MatchString(input.ImageURI) ||
		!runtimeProfileDigestPattern.MatchString(input.ReleaseDigest) ||
		!strings.HasSuffix(input.ImageURI, "@"+input.ReleaseDigest) ||
		input.CPUMillis < 100 || input.CPUMillis > 64000 ||
		input.MemoryBytes < 134217728 || input.MemoryBytes > 1099511627776 ||
		!validFoundationMutation(input.Mutation) {
		return ErrInvalidRuntimeProfile
	}
	return nil
}

func RuntimeProfileCreateDigest(input RuntimeProfileCreateInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidRuntimeProfile
	}
	return foundationDigest(struct {
		Operation, TenantID, ProjectID, ProfileID, ProfileName, Description string
		WorkloadTrust, IsolationRuntime                                     string
		TargetID, NetworkPolicyID, ImageURI, ReleaseDigest                  string
		TargetSelector                                                      *RuntimeProfileTargetSelector
		Version, CPUMillis, MemoryBytes                                     int64
	}{"runtime-profile.create", input.Scope.TenantID, input.Scope.ProjectID, input.ProfileID,
		input.ProfileName, input.Description, input.WorkloadTrust, input.IsolationRuntime,
		input.TargetID, input.NetworkPolicyID, input.ImageURI, input.ReleaseDigest, input.TargetSelector,
		input.Version, input.CPUMillis, input.MemoryBytes})
}

func (input RuntimeProfileTransitionInput) Validate(tenantID string) error {
	if !validFoundationScope(input.Scope, tenantID) || !validIdentifier(input.ProfileID) ||
		input.Version < 1 || input.Version > 2147483647 || input.ExpectedResourceVersion < 1 ||
		(input.Action != RuntimeProfilePublish && input.Action != RuntimeProfileDisable) ||
		!validFoundationMutation(input.Mutation) {
		return ErrInvalidRuntimeProfile
	}
	return nil
}

func RuntimeProfileTransitionDigest(input RuntimeProfileTransitionInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidRuntimeProfile
	}
	return foundationDigest(struct {
		Operation, TenantID, ProjectID, ProfileID string
		Version, ExpectedResourceVersion          int64
	}{"runtime-profile." + input.Action, input.Scope.TenantID, input.Scope.ProjectID,
		input.ProfileID, input.Version, input.ExpectedResourceVersion})
}

func (snapshot RuntimeProfileSnapshot) Validate() error {
	input := RuntimeProfileCreateInput{Scope: snapshot.Scope, ProfileID: snapshot.ProfileID,
		ProfileName: snapshot.ProfileName, Description: snapshot.Description, TargetID: snapshot.TargetID,
		WorkloadTrust: snapshot.WorkloadTrust, IsolationRuntime: snapshot.IsolationRuntime,
		TargetSelector:  snapshot.TargetSelector,
		NetworkPolicyID: snapshot.NetworkPolicyID,
		ImageURI:        snapshot.ImageURI, ReleaseDigest: snapshot.ReleaseDigest, Version: snapshot.Version,
		CPUMillis: snapshot.CPUMillis, MemoryBytes: snapshot.MemoryBytes,
		Mutation: FoundationMutation{RequestID: "snapshot", IdempotencyKey: "snapshot-runtime-profile"}}
	if snapshot.NetworkPolicyID == "" {
		input.NetworkPolicyID = "legacy-network-policy"
	}
	if input.Validate(snapshot.Scope.TenantID) != nil || !validIdentifier(snapshot.ProfileVersionID) ||
		snapshot.ResourceVersion < 1 || snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.Before(snapshot.CreatedAt) {
		return ErrInvalidRuntimeProfile
	}
	switch snapshot.Status {
	case "draft":
		if snapshot.PublishedAt != nil || snapshot.DisabledAt != nil {
			return ErrInvalidRuntimeProfile
		}
	case "published":
		if snapshot.PublishedAt == nil || snapshot.DisabledAt != nil || snapshot.PublishedAt.Before(snapshot.CreatedAt) {
			return ErrInvalidRuntimeProfile
		}
	case "disabled":
		if snapshot.PublishedAt == nil || snapshot.DisabledAt == nil || snapshot.DisabledAt.Before(*snapshot.PublishedAt) {
			return ErrInvalidRuntimeProfile
		}
	default:
		return ErrInvalidRuntimeProfile
	}
	return nil
}

func invalidRuntimeProfileTargetSelection(targetID string, selector *RuntimeProfileTargetSelector) bool {
	if targetID != "" {
		return selector != nil || !validIdentifier(targetID)
	}
	return selector == nil || !validIdentifier(selector.RegionID) || !validIdentifier(selector.ResourcePoolID) ||
		selector.Runtime != "docker" || selector.Architecture != "amd64" && selector.Architecture != "arm64"
}

func invalidRuntimeIsolation(workloadTrust, isolationRuntime string) bool {
	return (workloadTrust != "trusted-single-tenant" && workloadTrust != "dedicated-node" && workloadTrust != "shared-untrusted") ||
		(isolationRuntime != "runc" && isolationRuntime != "gvisor") ||
		(workloadTrust == "shared-untrusted") != (isolationRuntime == "gvisor")
}

func (summary RuntimeProfileSummary) Validate() error {
	if !validFoundationScope(summary.Scope, summary.Scope.TenantID) || !validIdentifier(summary.ProfileVersionID) ||
		!validIdentifier(summary.ProfileID) || !validIdentifier(summary.ProfileName) ||
		summary.Version < 1 || summary.Version > 2147483647 || invalidRuntimeProfileDescription(summary.Description) ||
		summary.CPUMillis < 100 || summary.CPUMillis > 64000 ||
		summary.MemoryBytes < 134217728 || summary.MemoryBytes > 1099511627776 {
		return ErrInvalidRuntimeProfile
	}
	return nil
}

func (input FoundationSandboxCreateInput) Validate(tenantID string) error {
	if !validFoundationScope(input.Scope, tenantID) || !validIdentifier(input.WorkspaceID) ||
		!validIdentifier(input.WorkspaceName) || !validIdentifier(input.SandboxID) ||
		!validIdentifier(input.RuntimeProfileID) || input.RuntimeProfileVersion < 1 ||
		input.RuntimeProfileVersion > 2147483647 || input.TTLSeconds < 60 || input.TTLSeconds > 86400 ||
		!validFoundationMutation(input.Mutation) {
		return ErrInvalidRuntimeProfile
	}
	return nil
}

func FoundationSandboxCreateDigest(input FoundationSandboxCreateInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidRuntimeProfile
	}
	return foundationDigest(struct {
		Operation, TenantID, ProjectID, WorkspaceID, WorkspaceName, SandboxID, RuntimeProfileID string
		RuntimeProfileVersion, TTLSeconds                                                       int64
	}{"foundation-sandbox.create", input.Scope.TenantID, input.Scope.ProjectID, input.WorkspaceID,
		input.WorkspaceName, input.SandboxID, input.RuntimeProfileID, input.RuntimeProfileVersion, input.TTLSeconds})
}

// FoundationSandboxCreateDigestV1 validates persisted pre-TTL create claims only.
func FoundationSandboxCreateDigestV1(input FoundationSandboxCreateInput) (string, error) {
	input.TTLSeconds = 60
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidRuntimeProfile
	}
	return foundationDigest(struct {
		Operation, TenantID, ProjectID, WorkspaceID, WorkspaceName, SandboxID, RuntimeProfileID string
		RuntimeProfileVersion                                                                   int64
	}{"foundation-sandbox.create", input.Scope.TenantID, input.Scope.ProjectID, input.WorkspaceID,
		input.WorkspaceName, input.SandboxID, input.RuntimeProfileID, input.RuntimeProfileVersion})
}

func (snapshot FoundationSandboxSnapshot) Validate() error {
	if !validFoundationScope(snapshot.Scope, snapshot.Scope.TenantID) || !validIdentifier(snapshot.OperationID) ||
		!validIdentifier(snapshot.WorkspaceID) || !validIdentifier(snapshot.SandboxID) ||
		!validIdentifier(snapshot.RuntimeProfileID) || snapshot.RuntimeProfileVersion < 1 ||
		snapshot.RuntimeProfileVersion > 2147483647 || snapshot.Generation < 1 || snapshot.DesiredState != "running" ||
		snapshot.TTLSeconds < 60 || snapshot.TTLSeconds > 86400 || snapshot.ExpiresAt.IsZero() ||
		(snapshot.ObservedState != "pending" && snapshot.ObservedState != "running" && snapshot.ObservedState != "unknown" &&
			snapshot.ObservedState != "failed" && snapshot.ObservedState != "stopped") {
		return ErrInvalidRuntimeProfile
	}
	return nil
}

func (input FoundationSandboxLifecycleInput) Validate(tenantID string) error {
	expectedCompute := "create"
	if input.Action == FoundationSandboxStop {
		expectedCompute = "delete"
	} else if input.Action != FoundationSandboxRebuild {
		return ErrInvalidRuntimeProfile
	}
	if !validFoundationScope(input.Scope, tenantID) || !validIdentifier(input.SandboxID) ||
		input.ConfirmedSandboxID != input.SandboxID || input.ExpectedGeneration < 1 ||
		input.ExpectedResourceVersion < 1 || input.ComputeDisposition != expectedCompute ||
		input.WorkspaceDisposition != "retain" || !validFoundationMutation(input.Mutation) {
		return ErrInvalidRuntimeProfile
	}
	return nil
}

func FoundationSandboxLifecycleDigest(input FoundationSandboxLifecycleInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidRuntimeProfile
	}
	return foundationDigest(struct {
		Operation, TenantID, ProjectID, SandboxID, ConfirmedSandboxID string
		ComputeDisposition, WorkspaceDisposition                      string
		ExpectedGeneration, ExpectedResourceVersion                   int64
	}{"foundation-sandbox." + input.Action, input.Scope.TenantID, input.Scope.ProjectID,
		input.SandboxID, input.ConfirmedSandboxID, input.ComputeDisposition,
		input.WorkspaceDisposition, input.ExpectedGeneration, input.ExpectedResourceVersion})
}

func (operation FoundationSandboxLifecycleOperation) Validate() error {
	if !validFoundationScope(operation.Scope, operation.Scope.TenantID) ||
		!validIdentifier(operation.OperationID) || !runtimeProfileIdempotencyKey.MatchString(operation.IdempotencyKey) ||
		!validIdentifier(operation.SandboxID) || !runtimeProfileDigestPattern.MatchString(operation.RequestedBy) ||
		!validIdentifier(operation.RequestID) || operation.SandboxGeneration < 2 || operation.RequestedAt.IsZero() ||
		operation.UpdatedAt.Before(operation.RequestedAt) || operation.WorkspaceDisposition != "retain" {
		return ErrInvalidRuntimeProfile
	}
	if operation.Action == "sandbox.stop" {
		if operation.ComputeDisposition != "delete" {
			return ErrInvalidRuntimeProfile
		}
	} else if operation.Action == "sandbox.rebuild" {
		if operation.ComputeDisposition != "create" {
			return ErrInvalidRuntimeProfile
		}
	} else {
		return ErrInvalidRuntimeProfile
	}
	switch operation.State {
	case "pending", "running", "reconciling", "succeeded":
		if operation.StableErrorCode != "" {
			return ErrInvalidRuntimeProfile
		}
	case "failed":
		if !validIdentifier(operation.StableErrorCode) {
			return ErrInvalidRuntimeProfile
		}
	default:
		return ErrInvalidRuntimeProfile
	}
	switch operation.CleanupPhase {
	case "none", "complete", "blocked":
	default:
		return ErrInvalidRuntimeProfile
	}
	return nil
}

func validFoundationScope(scope FoundationScope, tenantID string) bool {
	return scope.TenantID == tenantID && validIdentifier(scope.TenantID) && validIdentifier(scope.ProjectID)
}

func validFoundationMutation(mutation FoundationMutation) bool {
	return validIdentifier(mutation.RequestID) && runtimeProfileIdempotencyKey.MatchString(mutation.IdempotencyKey)
}

func invalidRuntimeProfileDescription(value string) bool {
	return !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 1024 ||
		strings.ContainsFunc(value, func(character rune) bool { return character < 32 || character == 127 })
}

func foundationDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalidRuntimeProfile
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
