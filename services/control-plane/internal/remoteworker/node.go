package remoteworker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
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
	Scope                     Scope
	EnrollmentID              string
	PeerCertificateSHA256     string
	IncarnationID             string
	ObservedGeneration        int64
	ObservedState             string
	WorkerVersion             string
	OS                        string
	Architecture              string
	KernelVersion             string
	Capabilities              []string
	Capacity                  Capacity
	CommandReceipt            *CommandReceipt
	SandboxCommandReceipt     *SandboxCommandReceipt
	SandboxExecCommandReceipt *SandboxExecCommandReceipt
	SandboxFileCommandReceipt *SandboxFileCommandReceipt
	SandboxPTYCommandReceipt  *SandboxPTYCommandReceipt
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

type SandboxCommandReceipt struct {
	CommandID, OperationID, SandboxID       string
	Attempt, SandboxGeneration              int64
	Action, Result, RuntimeID, RuntimeState string
	VolumeName                              string
	StableErrorCode                         string
	CleanupComplete                         bool
}

type SandboxCommand struct {
	CommandID, Action, OperationID, WorkspaceID, WorkspaceName string
	TargetID, SandboxID, ImageURI, SpecDigest, NetworkPolicyID string
	PhysicalVolumeName, RuntimeID, RuntimeState                string
	RuntimeOperationID, RuntimeSpecDigest                      string
	Attempt, SandboxGeneration, CPUMillis, MemoryBytes         int64
	RuntimeGeneration                                          int64
	NetworkAllowedEgress                                       []string
	Deadline                                                   time.Time
}

type SandboxExecCommandReceipt struct {
	CommandID, SandboxID, Result, Stdout, Stderr, StableErrorCode string
	SandboxGeneration, ExitCode, ExecutionTimeMillis              int64
}

type SandboxExecCommand struct {
	CommandID, WorkspaceID, TargetID, SandboxID               string
	RuntimeID, RuntimeOperationID, RuntimeSpecDigest, Command string
	SandboxGeneration, TimeoutSeconds                         int64
	Deadline                                                  time.Time
}

type SandboxFileCommand = platformv1alpha1.RemoteWorkerSandboxFileCommand
type SandboxFileCommandReceipt = platformv1alpha1.RemoteWorkerSandboxFileCommandReceipt
type SandboxPTYCommand = platformv1alpha1.RemoteWorkerSandboxPTYCommand
type SandboxPTYCommandReceipt = platformv1alpha1.RemoteWorkerSandboxPTYCommandReceipt

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
	TargetID           string
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
		input.CommandReceipt != nil && input.CommandReceipt.Validate() != nil ||
		input.SandboxCommandReceipt != nil && input.SandboxCommandReceipt.Validate() != nil ||
		input.SandboxExecCommandReceipt != nil && input.SandboxExecCommandReceipt.Validate() != nil ||
		input.SandboxFileCommandReceipt != nil && ValidateSandboxFileCommandReceipt(*input.SandboxFileCommandReceipt) != nil ||
		input.SandboxPTYCommandReceipt != nil && ValidateSandboxPTYCommandReceipt(*input.SandboxPTYCommandReceipt) != nil {
		return ErrInvalidHeartbeat
	}
	return nil
}

func (receipt SandboxCommandReceipt) Validate() error {
	if invalidIdentifier(receipt.CommandID) || receipt.Attempt < 1 || receipt.Attempt > 8 ||
		(receipt.Action != "sandbox.create" && receipt.Action != "sandbox.stop" && receipt.Action != "sandbox.rebuild") ||
		invalidIdentifier(receipt.OperationID) || invalidIdentifier(receipt.SandboxID) || receipt.SandboxGeneration < 1 ||
		receipt.Result != "succeeded" && receipt.Result != "failed" ||
		receipt.RuntimeID != "" && invalidIdentifier(receipt.RuntimeID) ||
		receipt.VolumeName != "" && (invalidIdentifier(receipt.VolumeName) || len(receipt.VolumeName) > 63) ||
		receipt.StableErrorCode != "" && invalidIdentifier(receipt.StableErrorCode) ||
		receipt.Result == "succeeded" && (receipt.VolumeName == "" || receipt.StableErrorCode != "" ||
			(receipt.Action == "sandbox.create" || receipt.Action == "sandbox.rebuild") && (receipt.RuntimeID == "" || receipt.RuntimeState != "Running") ||
			receipt.Action == "sandbox.stop" && (receipt.RuntimeID != "" || receipt.RuntimeState != "" || !receipt.CleanupComplete)) ||
		receipt.Result == "failed" && receipt.StableErrorCode == "" {
		return ErrInvalidHeartbeat
	}
	return nil
}

func (command SandboxCommand) Validate() error {
	if invalidIdentifier(command.CommandID) || command.Attempt < 1 || command.Attempt > 8 ||
		(command.Action != "sandbox.create" && command.Action != "sandbox.stop" && command.Action != "sandbox.rebuild") ||
		invalidIdentifier(command.OperationID) || invalidIdentifier(command.WorkspaceID) || invalidIdentifier(command.WorkspaceName) ||
		invalidIdentifier(command.TargetID) || invalidIdentifier(command.SandboxID) || command.SandboxGeneration < 1 ||
		len(command.ImageURI) < 1 || len(command.ImageURI) > 1024 || !digest(command.SpecDigest) ||
		invalidIdentifier(command.NetworkPolicyID) || command.CPUMillis < 100 || command.CPUMillis > 64000 ||
		command.MemoryBytes < 134_217_728 || command.MemoryBytes > 1_099_511_627_776 || command.Deadline.IsZero() {
		return ErrInvalidHeartbeat
	}
	seen := map[string]bool{}
	for _, target := range command.NetworkAllowedEgress {
		if seen[target] || len(target) < 1 || len(target) > 253 {
			return ErrInvalidHeartbeat
		}
		seen[target] = true
		for _, character := range target {
			if character <= 32 || character == 127 {
				return ErrInvalidHeartbeat
			}
		}
	}
	if len(command.NetworkAllowedEgress) > 64 {
		return ErrInvalidHeartbeat
	}
	validVolume := command.PhysicalVolumeName != "" && len(command.PhysicalVolumeName) <= 63 && !invalidIdentifier(command.PhysicalVolumeName)
	noRuntime := command.RuntimeID == "" && command.RuntimeState == "" && command.RuntimeOperationID == "" &&
		command.RuntimeGeneration == 0 && command.RuntimeSpecDigest == ""
	priorRuntimeValid := validVolume && !invalidIdentifier(command.RuntimeID) &&
		command.RuntimeState == "Running" && !invalidIdentifier(command.RuntimeOperationID) &&
		command.RuntimeGeneration > 0 && command.RuntimeGeneration < command.SandboxGeneration && digest(command.RuntimeSpecDigest)
	if command.Action == "sandbox.stop" && !priorRuntimeValid ||
		command.Action == "sandbox.rebuild" && (!validVolume || !noRuntime) ||
		command.Action == "sandbox.create" && (command.PhysicalVolumeName != "" || !noRuntime) {
		return ErrInvalidHeartbeat
	}
	return nil
}

func SandboxCommandID(operationID string, attempt int64) string {
	sum := sha256.Sum256([]byte(operationID + "|" + strconv.FormatInt(attempt, 10)))
	return "rwsc-" + hex.EncodeToString(sum[:16])
}

func (receipt SandboxExecCommandReceipt) Validate() error {
	validFailure := slices.Contains([]string{"sandbox_access_unavailable", "sandbox_runtime_unavailable", "sandbox_exec_output_limit", "sandbox_exec_timeout"}, receipt.StableErrorCode)
	if invalidIdentifier(receipt.CommandID) || invalidIdentifier(receipt.SandboxID) || receipt.SandboxGeneration < 1 ||
		receipt.Result != "succeeded" && receipt.Result != "failed" || !utf8.ValidString(receipt.Stdout) || !utf8.ValidString(receipt.Stderr) ||
		len(receipt.Stdout)+len(receipt.Stderr) > 1<<20 || receipt.ExecutionTimeMillis < 0 || receipt.ExecutionTimeMillis > 65000 ||
		receipt.Result == "succeeded" && receipt.StableErrorCode != "" ||
		receipt.Result == "failed" && (!validFailure || receipt.ExitCode != 0 || receipt.Stdout != "" || receipt.Stderr != "" || receipt.ExecutionTimeMillis != 0) {
		return ErrInvalidHeartbeat
	}
	return nil
}

func SandboxExecCommandReceiptDigest(receipt SandboxExecCommandReceipt) (string, error) {
	if receipt.Validate() != nil {
		return "", ErrInvalidHeartbeat
	}
	return mutationDigest("remote-worker.sandbox-exec-receipt", receipt.CommandID, receipt.SandboxID,
		receipt.SandboxGeneration, receipt.Result, receipt.ExitCode, receipt.Stdout, receipt.Stderr,
		receipt.ExecutionTimeMillis, receipt.StableErrorCode)
}

func (command SandboxExecCommand) Validate() error {
	if invalidIdentifier(command.CommandID) || invalidIdentifier(command.WorkspaceID) || invalidIdentifier(command.TargetID) ||
		invalidIdentifier(command.SandboxID) || command.SandboxGeneration < 1 || invalidIdentifier(command.RuntimeID) ||
		invalidIdentifier(command.RuntimeOperationID) || !digest(command.RuntimeSpecDigest) || len(command.Command) < 1 ||
		len(command.Command) > 8192 || !utf8.ValidString(command.Command) || command.TimeoutSeconds < 1 ||
		command.TimeoutSeconds > 60 || command.Deadline.IsZero() {
		return ErrInvalidHeartbeat
	}
	for _, character := range command.Command {
		if character == 0 {
			return ErrInvalidHeartbeat
		}
	}
	return nil
}

func ValidateSandboxFileCommand(command SandboxFileCommand) error {
	raw, err := json.Marshal(command)
	if err != nil {
		return ErrInvalidHeartbeat
	}
	if _, err := platformv1alpha1.DecodeRemoteWorkerSandboxFileCommandJSON(raw); err != nil {
		return ErrInvalidHeartbeat
	}
	return nil
}

func ValidateSandboxFileCommandReceipt(receipt SandboxFileCommandReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return ErrInvalidHeartbeat
	}
	if _, err := platformv1alpha1.DecodeRemoteWorkerSandboxFileCommandReceiptJSON(raw); err != nil {
		return ErrInvalidHeartbeat
	}
	return nil
}

func SandboxFileCommandReceiptDigest(receipt SandboxFileCommandReceipt) (string, error) {
	if ValidateSandboxFileCommandReceipt(receipt) != nil {
		return "", ErrInvalidHeartbeat
	}
	return mutationDigest("remote-worker.sandbox-file-receipt", receipt)
}

func ValidateSandboxPTYCommand(command SandboxPTYCommand) error {
	raw, err := json.Marshal(command)
	if err != nil {
		return ErrInvalidHeartbeat
	}
	if _, err := platformv1alpha1.DecodeRemoteWorkerSandboxPTYCommandJSON(raw); err != nil {
		return ErrInvalidHeartbeat
	}
	return nil
}

func ValidateSandboxPTYCommandReceipt(receipt SandboxPTYCommandReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return ErrInvalidHeartbeat
	}
	if _, err := platformv1alpha1.DecodeRemoteWorkerSandboxPTYCommandReceiptJSON(raw); err != nil {
		return ErrInvalidHeartbeat
	}
	return nil
}

func SandboxPTYCommandReceiptDigest(receipt SandboxPTYCommandReceipt) (string, error) {
	if ValidateSandboxPTYCommandReceipt(receipt) != nil {
		return "", ErrInvalidHeartbeat
	}
	return mutationDigest("remote-worker.sandbox-pty-receipt", receipt)
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
		invalidIdentifier(status.WorkerID) || invalidIdentifier(status.WorkerName) || status.TargetID != "" && invalidIdentifier(status.TargetID) || invalidIdentifier(status.IncarnationID) ||
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
