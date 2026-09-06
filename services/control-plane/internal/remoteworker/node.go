package remoteworker

import (
	"errors"
	"regexp"
	"slices"
	"time"
)

const (
	HeartbeatInterval = 5 * time.Second
	HeartbeatTTL      = 30 * time.Second
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
		invalidCapabilities(input.Capabilities) || invalidCapacity(input.Capacity) {
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
