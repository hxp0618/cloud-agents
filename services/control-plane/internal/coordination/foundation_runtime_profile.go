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
	RuntimeProfilePublish = "publish"
	RuntimeProfileDisable = "disable"
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

type RuntimeProfileCreateInput struct {
	Scope                               FoundationScope
	ProfileID, ProfileName, Description string
	TargetID, ImageURI, ReleaseDigest   string
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
	Scope                                    FoundationScope
	ProfileVersionID, ProfileID, ProfileName string
	Description, Status, TargetID, ImageURI  string
	ReleaseDigest                            string
	Version, CPUMillis, MemoryBytes          int64
	ResourceVersion                          int64
	CreatedAt, UpdatedAt                     time.Time
	PublishedAt, DisabledAt                  *time.Time
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
	RuntimeProfileVersion                 int64
	Mutation                              FoundationMutation
}

type FoundationSandboxSnapshot struct {
	Scope                               FoundationScope
	OperationID, WorkspaceID, SandboxID string
	RuntimeProfileID                    string
	RuntimeProfileVersion, Generation   int64
	DesiredState, ObservedState         string
}

func (input RuntimeProfileCreateInput) Validate(tenantID string) error {
	if !validFoundationScope(input.Scope, tenantID) || !validIdentifier(input.ProfileID) ||
		!validIdentifier(input.ProfileName) || input.Version < 1 || input.Version > 2147483647 ||
		invalidRuntimeProfileDescription(input.Description) || !validIdentifier(input.TargetID) ||
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
		TargetID, ImageURI, ReleaseDigest                                   string
		Version, CPUMillis, MemoryBytes                                     int64
	}{"runtime-profile.create", input.Scope.TenantID, input.Scope.ProjectID, input.ProfileID,
		input.ProfileName, input.Description, input.TargetID, input.ImageURI, input.ReleaseDigest,
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
		ImageURI: snapshot.ImageURI, ReleaseDigest: snapshot.ReleaseDigest, Version: snapshot.Version,
		CPUMillis: snapshot.CPUMillis, MemoryBytes: snapshot.MemoryBytes,
		Mutation: FoundationMutation{RequestID: "snapshot", IdempotencyKey: "snapshot-runtime-profile"}}
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
		input.RuntimeProfileVersion > 2147483647 || !validFoundationMutation(input.Mutation) {
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
		RuntimeProfileVersion                                                                   int64
	}{"foundation-sandbox.create", input.Scope.TenantID, input.Scope.ProjectID, input.WorkspaceID,
		input.WorkspaceName, input.SandboxID, input.RuntimeProfileID, input.RuntimeProfileVersion})
}

func (snapshot FoundationSandboxSnapshot) Validate() error {
	if !validFoundationScope(snapshot.Scope, snapshot.Scope.TenantID) || !validIdentifier(snapshot.OperationID) ||
		!validIdentifier(snapshot.WorkspaceID) || !validIdentifier(snapshot.SandboxID) ||
		!validIdentifier(snapshot.RuntimeProfileID) || snapshot.RuntimeProfileVersion < 1 ||
		snapshot.RuntimeProfileVersion > 2147483647 || snapshot.Generation < 1 || snapshot.DesiredState != "running" ||
		(snapshot.ObservedState != "pending" && snapshot.ObservedState != "running" && snapshot.ObservedState != "unknown" &&
			snapshot.ObservedState != "failed" && snapshot.ObservedState != "stopped") {
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
