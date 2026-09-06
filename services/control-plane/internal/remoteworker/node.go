package remoteworker

import (
	"errors"
	"regexp"
	"slices"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const (
	HeartbeatInterval = 5 * time.Second
	HeartbeatTTL      = 30 * time.Second
	CommandTTL        = 30 * time.Second
)

var (
	ErrInvalidHeartbeat  = errors.New("remote worker heartbeat is invalid")
	workerVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	capabilitySet        = map[string]struct{}{
		"docker": {}, "exec": {}, "files": {}, "preview": {}, "pty": {}, "ssh": {},
	}
)

type Capacity struct {
	CPUMillis   int64
	MemoryBytes int64
	DiskBytes   int64
}

type HeartbeatInput struct {
	Scope                 Scope
	EnrollmentID          string
	PeerCertificateSHA256 string
	IncarnationID         string
	ObservedGeneration    int64
	ObservedState         string
	WorkerVersion         string
	OS                    string
	Architecture          string
	KernelVersion         string
	Capabilities          []string
	Capacity              Capacity
	CommandReceipt        *CommandReceipt
}

type CommandReceipt struct {
	CommandID       string
	Generation      int64
	Result          string
	StableErrorCode string
}

type Command struct {
	CommandID    string
	Generation   int64
	DesiredState string
	Deadline     time.Time
}

type SchedulingInput struct {
	Scope                   Scope
	EnrollmentID            string
	ExpectedGeneration      int64
	ExpectedResourceVersion int64
	ConfirmedEnrollmentID   string
	DesiredState            string
	ImpactDigest            string
	Mutation                Mutation
}

type SchedulingPreview struct {
	Enrollment    Snapshot
	DesiredState  string
	ImpactDigest  string
	ImpactSummary string
}

type Operation struct {
	Scope                                   Scope
	OperationID, IdempotencyKey, Action     string
	EnrollmentID, CommandID, RequestedBy    string
	RequestID, State, CurrentStep           string
	StableErrorCode, ImpactSummary          string
	Generation                              int64
	Retryable                               bool
	RequestedAt, UpdatedAt, CommandDeadline time.Time
}

type NodeStatus struct {
	Scope              Scope
	EnrollmentID       string
	WorkerID           string
	WorkerName         string
	IncarnationID      string
	ResourceVersion    int64
	Generation         int64
	ObservedGeneration int64
	DesiredState       string
	ObservedState      string
	HealthState        string
	WorkerVersion      string
	OS                 string
	Architecture       string
	KernelVersion      string
	Capabilities       []string
	Capacity           Capacity
	FirstConnectedAt   time.Time
	LastHeartbeatAt    time.Time
	HeartbeatExpiresAt time.Time
}

func (input HeartbeatInput) Validate(tenantID string) error {
	if invalidIdentifier(tenantID) || input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.EnrollmentID) || !digest(input.PeerCertificateSHA256) || invalidIdentifier(input.IncarnationID) ||
		input.ObservedGeneration < 1 || !workerVersionPattern.MatchString(input.WorkerVersion) ||
		(input.ObservedState != "active" && input.ObservedState != "drained") ||
		invalidIdentifier(input.OS) || invalidIdentifier(input.Architecture) || invalidKernelVersion(input.KernelVersion) ||
		invalidCapabilities(input.Capabilities) || invalidCapacity(input.Capacity) ||
		input.CommandReceipt != nil && input.CommandReceipt.Validate() != nil {
		return ErrInvalidHeartbeat
	}
	return nil
}

func (receipt CommandReceipt) Validate() error {
	if invalidIdentifier(receipt.CommandID) || receipt.Generation < 2 ||
		receipt.Result != "succeeded" && receipt.Result != "failed" ||
		receipt.Result == "succeeded" && receipt.StableErrorCode != "" ||
		receipt.Result == "failed" && invalidIdentifier(receipt.StableErrorCode) {
		return ErrInvalidHeartbeat
	}
	return nil
}

func CommandReceiptDigest(receipt CommandReceipt) (string, error) {
	if receipt.Validate() != nil {
		return "", ErrInvalidHeartbeat
	}
	return mutationDigest("remote-worker.command-receipt", receipt.CommandID, receipt.Generation, receipt.Result, receipt.StableErrorCode)
}

func (command Command) Validate() error {
	if invalidIdentifier(command.CommandID) || command.Generation < 2 ||
		command.DesiredState != "active" && command.DesiredState != "drained" || command.Deadline.IsZero() {
		return ErrInvalidHeartbeat
	}
	return nil
}

func (input SchedulingInput) Validate(tenantID string) error {
	if invalidIdentifier(tenantID) || input.Scope.TenantID != tenantID || invalidIdentifier(input.Scope.ProjectID) ||
		invalidIdentifier(input.EnrollmentID) || input.ConfirmedEnrollmentID != input.EnrollmentID ||
		input.ExpectedGeneration < 1 || input.ExpectedResourceVersion < 1 ||
		input.DesiredState != "active" && input.DesiredState != "drained" || !digest(input.ImpactDigest) ||
		invalidMutation(input.Mutation) {
		return ErrInvalidHeartbeat
	}
	return nil
}

func SchedulingMutationDigest(input SchedulingInput) (string, error) {
	if input.Validate(input.Scope.TenantID) != nil {
		return "", ErrInvalidHeartbeat
	}
	return mutationDigest("remote-worker.scheduling", input.Scope, input.EnrollmentID, input.ExpectedGeneration,
		input.ExpectedResourceVersion, input.ConfirmedEnrollmentID, input.DesiredState, input.ImpactDigest)
}

func NewSchedulingPreview(enrollment Snapshot) (SchedulingPreview, error) {
	if enrollment.Validate() != nil || enrollment.Node == nil {
		return SchedulingPreview{}, ErrInvalidHeartbeat
	}
	node := enrollment.Node
	desired := node.DesiredState
	if node.DesiredState == node.ObservedState {
		if desired == "active" {
			desired = "drained"
		} else {
			desired = "active"
		}
	}
	impact, err := mutationDigest("remote-worker.scheduling-impact", enrollment.Scope, enrollment.EnrollmentID,
		enrollment.WorkerID, node.HealthState, node.Generation, node.ResourceVersion, node.DesiredState,
		node.ObservedState, desired)
	if err != nil {
		return SchedulingPreview{}, err
	}
	action := "Drain"
	if desired == "active" {
		action = "Resume"
	}
	return SchedulingPreview{Enrollment: enrollment, DesiredState: desired, ImpactDigest: impact,
		ImpactSummary: action + " RemoteWorker; new scheduling follows the desired state and running Sandboxes or Workspace data are retained"}, nil
}

func (operation Operation) Validate() error {
	if invalidIdentifier(operation.Scope.TenantID) || invalidIdentifier(operation.Scope.ProjectID) ||
		invalidIdentifier(operation.OperationID) || invalidIdentifier(operation.CommandID) ||
		commonv1alpha1.ValidateIdempotencyKey(operation.IdempotencyKey, "/idempotencyKey") != nil ||
		operation.Action != "remote-worker.drain" && operation.Action != "remote-worker.resume" ||
		invalidIdentifier(operation.EnrollmentID) || operation.Generation < 2 || !digest(operation.RequestedBy) ||
		invalidIdentifier(operation.RequestID) ||
		operation.State != "queued" && operation.State != "running" && operation.State != "succeeded" && operation.State != "failed" ||
		invalidIdentifier(operation.CurrentStep) || operation.StableErrorCode != "" && invalidIdentifier(operation.StableErrorCode) ||
		len(operation.ImpactSummary) < 1 || len(operation.ImpactSummary) > 256 ||
		operation.RequestedAt.IsZero() || operation.UpdatedAt.Before(operation.RequestedAt) ||
		operation.CommandDeadline.Sub(operation.RequestedAt) != CommandTTL ||
		operation.State == "failed" && (operation.StableErrorCode == "" || !operation.Retryable) ||
		operation.State != "failed" && (operation.StableErrorCode != "" || operation.Retryable) {
		return ErrInvalidHeartbeat
	}
	return nil
}

func (status NodeStatus) Validate() error {
	if invalidIdentifier(status.Scope.TenantID) || invalidIdentifier(status.Scope.ProjectID) || invalidIdentifier(status.EnrollmentID) ||
		invalidIdentifier(status.WorkerID) || invalidIdentifier(status.WorkerName) || invalidIdentifier(status.IncarnationID) ||
		status.ResourceVersion < 1 || status.Generation < 1 || status.ObservedGeneration < 1 || status.ObservedGeneration > status.Generation ||
		(status.DesiredState != "active" && status.DesiredState != "drained") ||
		(status.ObservedState != "active" && status.ObservedState != "drained") ||
		(status.HealthState != "online" && status.HealthState != "degraded" && status.HealthState != "offline") ||
		!workerVersionPattern.MatchString(status.WorkerVersion) || invalidIdentifier(status.OS) || invalidIdentifier(status.Architecture) ||
		invalidKernelVersion(status.KernelVersion) || invalidCapabilities(status.Capabilities) || invalidCapacity(status.Capacity) ||
		status.FirstConnectedAt.IsZero() || status.LastHeartbeatAt.Before(status.FirstConnectedAt) ||
		!status.HeartbeatExpiresAt.After(status.LastHeartbeatAt) || status.HeartbeatExpiresAt.Sub(status.LastHeartbeatAt) != HeartbeatTTL {
		return ErrInvalidHeartbeat
	}
	return nil
}

func invalidCapabilities(values []string) bool {
	if len(values) < 1 || len(values) > 16 || !slices.IsSorted(values) {
		return true
	}
	for index, value := range values {
		if _, ok := capabilitySet[value]; !ok || index > 0 && values[index-1] == value {
			return true
		}
	}
	return false
}

func invalidCapacity(value Capacity) bool {
	return value.CPUMillis < 100 || value.CPUMillis > 512_000_000 ||
		value.MemoryBytes < 134_217_728 || value.MemoryBytes > 8_796_093_022_208_000 ||
		value.DiskBytes < 134_217_728 || value.DiskBytes > 8_796_093_022_208_000
}

func invalidKernelVersion(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return true
	}
	for _, character := range value {
		if character < 32 || character > 126 {
			return true
		}
	}
	return false
}
