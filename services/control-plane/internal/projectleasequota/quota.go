package projectleasequota

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const (
	MaxConcurrentLeases = int64(8000)
	MaxCPUMillis        = int64(512000000)
	MaxMemoryBytes      = int64(8796093022208000)
	MaxLeaseTTLSeconds  = int64(86400)
)

var ErrInvalidInput = errors.New("project lease quota input is invalid")

type Scope struct{ TenantID, ProjectID string }
type Mutation struct{ RequestID, IdempotencyKey string }

type SetInput struct {
	Scope                   Scope
	ExpectedResourceVersion int64
	MaxConcurrentLeases     int64
	MaxCPUMillis            int64
	MaxMemoryBytes          int64
	MaxLeaseTTLSeconds      int64
	Mutation                Mutation
}

type Snapshot struct {
	Scope               Scope
	QuotaID             string
	QuotaName           string
	MaxConcurrentLeases int64
	MaxCPUMillis        int64
	MaxMemoryBytes      int64
	MaxLeaseTTLSeconds  int64
	ActiveLeases        int64
	UsedCPUMillis       int64
	UsedMemoryBytes     int64
	ResourceVersion     int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type AuditEvent struct {
	Scope                Scope
	EventID              string
	OperationID          string
	Actor                string
	Action               string
	QuotaID              string
	QuotaResourceVersion int64
	Result               string
	RequestID            string
	OccurredAt           time.Time
}

func (input SetInput) Validate(tenantID string) error {
	if input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		input.ExpectedResourceVersion < 0 || !validLimits(input.MaxConcurrentLeases, input.MaxCPUMillis, input.MaxMemoryBytes, input.MaxLeaseTTLSeconds) ||
		invalidIdentifier(input.Mutation.RequestID) || commonv1alpha1.ValidateIdempotencyKey(input.Mutation.IdempotencyKey, "/idempotencyKey") != nil {
		return ErrInvalidInput
	}
	return nil
}

func MutationDigest(input SetInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidInput
	}
	encoded, err := json.Marshal(struct {
		Operation, TenantID, ProjectID                             string
		ExpectedResourceVersion, MaxConcurrentLeases, MaxCPUMillis int64
		MaxMemoryBytes, MaxLeaseTTLSeconds                         int64
	}{
		"project-lease-quota.set", input.Scope.TenantID, input.Scope.ProjectID,
		input.ExpectedResourceVersion, input.MaxConcurrentLeases, input.MaxCPUMillis,
		input.MaxMemoryBytes, input.MaxLeaseTTLSeconds,
	})
	if err != nil {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (snapshot Snapshot) Validate() error {
	if invalidIdentifier(snapshot.Scope.TenantID) || invalidIdentifier(snapshot.Scope.ProjectID) ||
		invalidIdentifier(snapshot.QuotaID) || invalidIdentifier(snapshot.QuotaName) ||
		!validLimits(snapshot.MaxConcurrentLeases, snapshot.MaxCPUMillis, snapshot.MaxMemoryBytes, snapshot.MaxLeaseTTLSeconds) ||
		snapshot.ActiveLeases < 0 || snapshot.ActiveLeases > MaxConcurrentLeases ||
		snapshot.UsedCPUMillis < 0 || snapshot.UsedCPUMillis > MaxCPUMillis ||
		snapshot.UsedMemoryBytes < 0 || snapshot.UsedMemoryBytes > MaxMemoryBytes ||
		snapshot.ResourceVersion < 1 || snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.Before(snapshot.CreatedAt) {
		return ErrInvalidInput
	}
	return nil
}

func (event AuditEvent) Validate() error {
	if invalidIdentifier(event.Scope.TenantID) || invalidIdentifier(event.Scope.ProjectID) ||
		invalidIdentifier(event.EventID) || invalidIdentifier(event.OperationID) ||
		!digest(event.Actor) || event.Action != "quota.set" || invalidIdentifier(event.QuotaID) ||
		event.QuotaResourceVersion < 1 || event.Result != "succeeded" ||
		invalidIdentifier(event.RequestID) || event.OccurredAt.IsZero() {
		return ErrInvalidInput
	}
	return nil
}

func validLimits(maxConcurrentLeases, maxCPUMillis, maxMemoryBytes, maxLeaseTTLSeconds int64) bool {
	return maxConcurrentLeases >= 1 && maxConcurrentLeases <= MaxConcurrentLeases &&
		maxCPUMillis >= 100 && maxCPUMillis <= MaxCPUMillis &&
		maxMemoryBytes >= 134217728 && maxMemoryBytes <= MaxMemoryBytes &&
		maxLeaseTTLSeconds >= 60 && maxLeaseTTLSeconds <= MaxLeaseTTLSeconds
}

func invalidIdentifier(value string) bool {
	return commonv1alpha1.ValidateIdentifier(value, "/value") != nil
}

func digest(value string) bool {
	if len(value) != 71 || value[:7] != "sha256:" {
		return false
	}
	for _, character := range value[7:] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
