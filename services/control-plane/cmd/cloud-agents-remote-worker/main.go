package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/foundationcontroller"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var (
	version                      = "dev"
	errInvalidRemoteWorkerConfig = errors.New("cloud-agents-remote-worker/invalid_config")
)

type config struct {
	controlPlaneURL                string
	tenantID                       string
	projectID                      string
	enrollmentID                   string
	incarnationID                  string
	certificate                    string
	privateKey                     string
	certificateResourceVersionFile string
	certificateRotationBefore      time.Duration
	rotateCertificateOnce          bool
	serverCA                       string
	stateFile                      string
	dockerEndpoint                 string
	credentialDirectory            string
	credentialRef                  string
	kernelVersion                  string
	capabilities                   []string
	capacity                       platform.RemoteWorkerCapacity
	once                           bool
}

func parseConfig(args []string) (config, error) {
	set := flag.NewFlagSet("cloud-agents-remote-worker", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var value config
	var capabilities string
	set.StringVar(&value.controlPlaneURL, "control-plane-url", "", "Control Plane HTTPS URL")
	set.StringVar(&value.tenantID, "tenant", "", "tenant identifier")
	set.StringVar(&value.projectID, "project", "", "project identifier")
	set.StringVar(&value.enrollmentID, "enrollment", "", "RemoteWorker enrollment identifier")
	set.StringVar(&value.incarnationID, "incarnation", "", "RemoteWorker incarnation identifier")
	set.StringVar(&value.certificate, "certificate", "", "client certificate PEM file")
	set.StringVar(&value.privateKey, "private-key", "", "client private key PEM file")
	set.StringVar(&value.certificateResourceVersionFile, "certificate-resource-version-file", "", "durable certificate resource version file")
	set.DurationVar(&value.certificateRotationBefore, "certificate-rotation-before", defaultCertificateRotationBefore, "rotate this long before certificate expiry")
	set.BoolVar(&value.rotateCertificateOnce, "rotate-certificate-once", false, "force one certificate rotation before the first heartbeat")
	set.StringVar(&value.serverCA, "server-ca", "", "Control Plane CA certificate PEM file")
	set.StringVar(&value.stateFile, "state-file", "", "durable RemoteWorker state file")
	set.StringVar(&value.dockerEndpoint, "docker-endpoint", "", "node-local Docker HTTPS endpoint")
	set.StringVar(&value.credentialDirectory, "credential-directory", "", "node-local runtime credential directory")
	set.StringVar(&value.credentialRef, "credential-ref", "", "node-local runtime credential identifier")
	set.StringVar(&value.kernelVersion, "kernel-version", "", "host kernel version")
	set.StringVar(&capabilities, "capabilities", "", "sorted comma-separated capabilities")
	set.Int64Var(&value.capacity.CPUMillis, "capacity-cpu-millis", 0, "allocatable CPU in millicores")
	set.Int64Var(&value.capacity.MemoryBytes, "capacity-memory-bytes", 0, "allocatable memory in bytes")
	set.Int64Var(&value.capacity.DiskBytes, "capacity-disk-bytes", 0, "allocatable disk in bytes")
	set.BoolVar(&value.once, "once", false, "send one heartbeat and exit")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return config{}, errInvalidRemoteWorkerConfig
	}
	value.capabilities = strings.Split(capabilities, ",")
	request := value.heartbeatRequest(initialNodeState(value.incarnationID))
	runtimeConfigMissing := value.dockerEndpoint == "" || value.credentialDirectory == "" || common.ValidateIdentifier(value.credentialRef, "/credential-ref") != nil
	if common.ValidateIdentifier(value.tenantID, "/tenant") != nil || common.ValidateIdentifier(value.projectID, "/project") != nil || common.ValidateIdentifier(value.enrollmentID, "/enrollment") != nil ||
		value.controlPlaneURL == "" || value.certificate == "" || value.privateKey == "" || value.serverCA == "" || value.stateFile == "" ||
		slices.Contains(value.capabilities, "docker") && runtimeConfigMissing ||
		strings.TrimSpace(value.controlPlaneURL) != value.controlPlaneURL ||
		strings.TrimSpace(value.certificate) != value.certificate || strings.TrimSpace(value.privateKey) != value.privateKey || strings.TrimSpace(value.serverCA) != value.serverCA || strings.TrimSpace(value.stateFile) != value.stateFile ||
		strings.TrimSpace(value.dockerEndpoint) != value.dockerEndpoint || strings.TrimSpace(value.credentialDirectory) != value.credentialDirectory ||
		strings.TrimSpace(value.certificateResourceVersionFile) != value.certificateResourceVersionFile {
		return config{}, errInvalidRemoteWorkerConfig
	}
	if value.certificateResourceVersionFile != "" && (value.certificate != value.privateKey || !filepath.IsAbs(value.certificate) || !filepath.IsAbs(value.certificateResourceVersionFile) ||
		value.certificateRotationBefore <= 0 || value.certificateRotationBefore >= internalremoteworker.CertificateLifetime) ||
		value.rotateCertificateOnce && value.certificateResourceVersionFile == "" {
		return config{}, errInvalidRemoteWorkerConfig
	}
	if _, err := platform.EncodeRemoteWorkerHeartbeatRequestJSON(request); err != nil {
		return config{}, errInvalidRemoteWorkerConfig
	}
	return value, nil
}

func (value config) heartbeatRequest(state nodeState) platform.RemoteWorkerHeartbeatRequest {
	request := platform.RemoteWorkerHeartbeatRequest{
		IncarnationID: value.incarnationID, ObservedGeneration: state.ObservedGeneration, ObservedState: state.ObservedState,
		WorkerVersion: version, OS: runtime.GOOS, Architecture: runtime.GOARCH,
		KernelVersion: value.kernelVersion, Capabilities: value.capabilities, Capacity: value.capacity,
		CommandReceipt: state.CommandReceipt, SandboxCommandReceipt: state.SandboxCommandReceipt,
		SandboxExecCommandReceipt:    state.SandboxExecCommandReceipt,
		SandboxFileCommandReceipt:    state.SandboxFileCommandReceipt,
		SandboxPTYCommandReceipt:     state.SandboxPTYCommandReceipt,
		SandboxPreviewCommandReceipt: state.SandboxPreviewCommandReceipt,
	}
	if state.SandboxCommand != nil && state.SandboxCommandReceipt == nil && state.ExecutingCommandID == state.SandboxCommand.CommandID {
		request.SandboxCommandID = state.SandboxCommand.CommandID
	}
	return request
}

type nodeState struct {
	IncarnationID                string                                             `json:"incarnationId"`
	ObservedGeneration           int64                                              `json:"observedGeneration"`
	ObservedState                string                                             `json:"observedState"`
	LastCommandID                string                                             `json:"lastCommandId,omitempty"`
	ExecutingCommandID           string                                             `json:"executingCommandId,omitempty"`
	CommandReceipt               *platform.RemoteWorkerCommandReceipt               `json:"commandReceipt,omitempty"`
	SandboxCommand               *platform.RemoteWorkerSandboxCommand               `json:"sandboxCommand,omitempty"`
	SandboxCommandReceipt        *platform.RemoteWorkerSandboxCommandReceipt        `json:"sandboxCommandReceipt,omitempty"`
	SandboxExecCommand           *platform.RemoteWorkerSandboxExecCommand           `json:"sandboxExecCommand,omitempty"`
	SandboxExecCommandReceipt    *platform.RemoteWorkerSandboxExecCommandReceipt    `json:"sandboxExecCommandReceipt,omitempty"`
	SandboxFileCommand           *platform.RemoteWorkerSandboxFileCommand           `json:"sandboxFileCommand,omitempty"`
	SandboxFileCommandReceipt    *platform.RemoteWorkerSandboxFileCommandReceipt    `json:"sandboxFileCommandReceipt,omitempty"`
	SandboxPTYCommand            *platform.RemoteWorkerSandboxPTYCommand            `json:"sandboxPtyCommand,omitempty"`
	SandboxPTYCommandReceipt     *platform.RemoteWorkerSandboxPTYCommandReceipt     `json:"sandboxPtyCommandReceipt,omitempty"`
	SandboxPreviewCommand        *platform.RemoteWorkerSandboxPreviewCommand        `json:"sandboxPreviewCommand,omitempty"`
	SandboxPreviewCommandReceipt *platform.RemoteWorkerSandboxPreviewCommandReceipt `json:"sandboxPreviewCommandReceipt,omitempty"`
}

func initialNodeState(incarnationID string) nodeState {
	return nodeState{IncarnationID: incarnationID, ObservedGeneration: 1, ObservedState: "active"}
}

func validateNodeState(value nodeState) error {
	if common.ValidateIdentifier(value.IncarnationID, "/incarnationId") != nil || value.ObservedGeneration < 1 ||
		value.ObservedState != "active" && value.ObservedState != "drained" ||
		value.LastCommandID != "" && common.ValidateIdentifier(value.LastCommandID, "/lastCommandId") != nil ||
		value.ExecutingCommandID != "" && common.ValidateIdentifier(value.ExecutingCommandID, "/executingCommandId") != nil {
		return errInvalidRemoteWorkerConfig
	}
	request := platform.RemoteWorkerHeartbeatRequest{IncarnationID: value.IncarnationID, ObservedGeneration: value.ObservedGeneration,
		ObservedState: value.ObservedState, WorkerVersion: "state", OS: "state", Architecture: "state", KernelVersion: "state",
		Capabilities: []string{"exec"}, Capacity: platform.RemoteWorkerCapacity{CPUMillis: 100, MemoryBytes: 134217728, DiskBytes: 134217728}, CommandReceipt: value.CommandReceipt, SandboxCommandReceipt: value.SandboxCommandReceipt, SandboxExecCommandReceipt: value.SandboxExecCommandReceipt, SandboxFileCommandReceipt: value.SandboxFileCommandReceipt, SandboxPTYCommandReceipt: value.SandboxPTYCommandReceipt, SandboxPreviewCommandReceipt: value.SandboxPreviewCommandReceipt}
	if _, err := platform.EncodeRemoteWorkerHeartbeatRequestJSON(request); err != nil {
		return errInvalidRemoteWorkerConfig
	}
	if value.CommandReceipt != nil && (value.LastCommandID != value.CommandReceipt.CommandID ||
		value.CommandReceipt.Result == "succeeded" && value.CommandReceipt.Generation != value.ObservedGeneration ||
		value.CommandReceipt.Result == "failed" && value.CommandReceipt.Generation != value.ObservedGeneration+1) {
		return errInvalidRemoteWorkerConfig
	}
	if value.SandboxCommand == nil && value.SandboxCommandReceipt != nil || value.SandboxCommand != nil && value.SandboxCommandReceipt != nil && value.SandboxCommand.CommandID != value.SandboxCommandReceipt.CommandID {
		return errInvalidRemoteWorkerConfig
	}
	if value.SandboxExecCommand == nil && value.SandboxExecCommandReceipt != nil || value.SandboxExecCommand != nil && value.SandboxExecCommandReceipt != nil && value.SandboxExecCommand.CommandID != value.SandboxExecCommandReceipt.CommandID ||
		value.SandboxCommand != nil && value.SandboxExecCommand != nil {
		return errInvalidRemoteWorkerConfig
	}
	if value.SandboxFileCommand == nil && value.SandboxFileCommandReceipt != nil || value.SandboxFileCommand != nil && value.SandboxFileCommandReceipt != nil && value.SandboxFileCommand.CommandID != value.SandboxFileCommandReceipt.CommandID ||
		value.SandboxCommand != nil && value.SandboxFileCommand != nil || value.SandboxExecCommand != nil && value.SandboxFileCommand != nil {
		return errInvalidRemoteWorkerConfig
	}
	if value.SandboxPTYCommand == nil && value.SandboxPTYCommandReceipt != nil || value.SandboxPTYCommand != nil && value.SandboxPTYCommandReceipt != nil && value.SandboxPTYCommand.CommandID != value.SandboxPTYCommandReceipt.CommandID ||
		value.SandboxCommand != nil && value.SandboxPTYCommand != nil || value.SandboxExecCommand != nil && value.SandboxPTYCommand != nil || value.SandboxFileCommand != nil && value.SandboxPTYCommand != nil {
		return errInvalidRemoteWorkerConfig
	}
	if value.SandboxPreviewCommand == nil && value.SandboxPreviewCommandReceipt != nil || value.SandboxPreviewCommand != nil && value.SandboxPreviewCommandReceipt != nil && value.SandboxPreviewCommand.CommandID != value.SandboxPreviewCommandReceipt.CommandID ||
		value.SandboxCommand != nil && value.SandboxPreviewCommand != nil || value.SandboxExecCommand != nil && value.SandboxPreviewCommand != nil || value.SandboxFileCommand != nil && value.SandboxPreviewCommand != nil || value.SandboxPTYCommand != nil && value.SandboxPreviewCommand != nil {
		return errInvalidRemoteWorkerConfig
	}
	if value.SandboxCommand != nil {
		raw, err := json.Marshal(value.SandboxCommand)
		if err != nil {
			return errInvalidRemoteWorkerConfig
		}
		if _, err := platform.DecodeRemoteWorkerSandboxCommandJSON(raw); err != nil {
			return errInvalidRemoteWorkerConfig
		}
	}
	if value.SandboxExecCommand != nil {
		raw, err := json.Marshal(value.SandboxExecCommand)
		if err != nil {
			return errInvalidRemoteWorkerConfig
		}
		if _, err := platform.DecodeRemoteWorkerSandboxExecCommandJSON(raw); err != nil {
			return errInvalidRemoteWorkerConfig
		}
	}
	if value.SandboxFileCommand != nil {
		raw, err := json.Marshal(value.SandboxFileCommand)
		if err != nil {
			return errInvalidRemoteWorkerConfig
		}
		if _, err := platform.DecodeRemoteWorkerSandboxFileCommandJSON(raw); err != nil {
			return errInvalidRemoteWorkerConfig
		}
	}
	if value.SandboxPTYCommand != nil {
		raw, err := json.Marshal(value.SandboxPTYCommand)
		if err != nil {
			return errInvalidRemoteWorkerConfig
		}
		if _, err := platform.DecodeRemoteWorkerSandboxPTYCommandJSON(raw); err != nil {
			return errInvalidRemoteWorkerConfig
		}
	}
	if value.SandboxPreviewCommand != nil {
		raw, err := json.Marshal(value.SandboxPreviewCommand)
		if err != nil {
			return errInvalidRemoteWorkerConfig
		}
		if _, err := platform.DecodeRemoteWorkerSandboxPreviewCommandJSON(raw); err != nil {
			return errInvalidRemoteWorkerConfig
		}
	}
	if value.ExecutingCommandID != "" && currentCommandID(value) != value.ExecutingCommandID {
		return errInvalidRemoteWorkerConfig
	}
	return nil
}

func currentCommandID(value nodeState) string {
	if value.SandboxCommand != nil {
		return value.SandboxCommand.CommandID
	}
	if value.SandboxExecCommand != nil {
		return value.SandboxExecCommand.CommandID
	}
	if value.SandboxFileCommand != nil {
		return value.SandboxFileCommand.CommandID
	}
	if value.SandboxPTYCommand != nil {
		return value.SandboxPTYCommand.CommandID
	}
	if value.SandboxPreviewCommand != nil {
		return value.SandboxPreviewCommand.CommandID
	}
	return ""
}

func beginCommandExecution(path string, state *nodeState, commandID string) (bool, error) {
	if state == nil || commandID == "" {
		return false, errInvalidRemoteWorkerConfig
	}
	if state.ExecutingCommandID == commandID {
		return false, nil
	}
	if state.ExecutingCommandID != "" {
		return false, errInvalidRemoteWorkerConfig
	}
	state.ExecutingCommandID = commandID
	if err := saveNodeState(path, *state); err != nil {
		state.ExecutingCommandID = ""
		return false, err
	}
	return true, nil
}

func loadNodeState(path, incarnationID string) (nodeState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return initialNodeState(incarnationID), nil
	}
	if err != nil || len(data) == 0 || len(data) > 32<<20 {
		return nodeState{}, errInvalidRemoteWorkerConfig
	}
	var value nodeState
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF || value.IncarnationID != incarnationID || validateNodeState(value) != nil {
		return nodeState{}, errInvalidRemoteWorkerConfig
	}
	return value, nil
}

func saveNodeState(path string, value nodeState) error {
	if path == "" || validateNodeState(value) != nil {
		return errInvalidRemoteWorkerConfig
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writePrivateFile(path, append(data, '\n'))
}

func reconcileHeartbeat(path string, state *nodeState, heartbeat platform.RemoteWorkerHeartbeat, now time.Time) error {
	if state == nil || heartbeat.IncarnationID != state.IncarnationID {
		return errInvalidRemoteWorkerConfig
	}
	commandCount := 0
	for _, present := range []bool{heartbeat.SandboxCommand != nil, heartbeat.SandboxExecCommand != nil, heartbeat.SandboxFileCommand != nil, heartbeat.SandboxPTYCommand != nil, heartbeat.SandboxPreviewCommand != nil} {
		if present {
			commandCount++
		}
	}
	if commandCount > 1 {
		return errInvalidRemoteWorkerConfig
	}
	changed := state.CommandReceipt != nil || state.SandboxCommandReceipt != nil || state.SandboxExecCommandReceipt != nil || state.SandboxFileCommandReceipt != nil || state.SandboxPTYCommandReceipt != nil || state.SandboxPreviewCommandReceipt != nil
	state.CommandReceipt = nil
	if state.SandboxCommandReceipt != nil {
		state.SandboxCommand, state.SandboxCommandReceipt = nil, nil
		state.ExecutingCommandID = ""
	}
	if state.SandboxExecCommandReceipt != nil {
		state.SandboxExecCommand, state.SandboxExecCommandReceipt = nil, nil
		state.ExecutingCommandID = ""
	}
	if state.SandboxFileCommandReceipt != nil {
		state.SandboxFileCommand, state.SandboxFileCommandReceipt = nil, nil
		state.ExecutingCommandID = ""
	}
	if state.SandboxPTYCommandReceipt != nil {
		state.SandboxPTYCommand, state.SandboxPTYCommandReceipt = nil, nil
		state.ExecutingCommandID = ""
	}
	if state.SandboxPreviewCommandReceipt != nil {
		state.SandboxPreviewCommand, state.SandboxPreviewCommandReceipt = nil, nil
		state.ExecutingCommandID = ""
	}
	if heartbeat.Command != nil {
		command := heartbeat.Command
		deadline, err := time.Parse(time.RFC3339Nano, command.Deadline)
		if err != nil {
			return errInvalidRemoteWorkerConfig
		}
		result, stableErrorCode := "succeeded", ""
		if !deadline.After(now) {
			result, stableErrorCode = "failed", "remote-worker-command-expired"
		} else if command.Generation != state.ObservedGeneration+1 {
			if command.CommandID != state.LastCommandID || command.Generation != state.ObservedGeneration || command.DesiredState != state.ObservedState {
				result, stableErrorCode = "failed", "remote-worker-command-generation-conflict"
			}
		}
		if result == "succeeded" {
			state.ObservedGeneration, state.ObservedState = command.Generation, command.DesiredState
		}
		state.LastCommandID = command.CommandID
		state.CommandReceipt = &platform.RemoteWorkerCommandReceipt{CommandID: command.CommandID, Generation: command.Generation, Result: result, StableErrorCode: stableErrorCode}
		changed = true
	}
	if heartbeat.SandboxCommand != nil {
		if state.SandboxExecCommand != nil || state.SandboxFileCommand != nil || state.SandboxPTYCommand != nil || state.SandboxPreviewCommand != nil || state.SandboxCommand != nil && state.SandboxCommand.CommandID != heartbeat.SandboxCommand.CommandID {
			return errInvalidRemoteWorkerConfig
		}
		state.SandboxCommand = heartbeat.SandboxCommand
		changed = true
	}
	if heartbeat.SandboxExecCommand != nil {
		if state.SandboxCommand != nil || state.SandboxFileCommand != nil || state.SandboxPTYCommand != nil || state.SandboxPreviewCommand != nil || state.SandboxExecCommand != nil && state.SandboxExecCommand.CommandID != heartbeat.SandboxExecCommand.CommandID {
			return errInvalidRemoteWorkerConfig
		}
		state.SandboxExecCommand = heartbeat.SandboxExecCommand
		changed = true
	}
	if heartbeat.SandboxFileCommand != nil {
		if state.SandboxCommand != nil || state.SandboxExecCommand != nil || state.SandboxPTYCommand != nil || state.SandboxPreviewCommand != nil || state.SandboxFileCommand != nil && state.SandboxFileCommand.CommandID != heartbeat.SandboxFileCommand.CommandID {
			return errInvalidRemoteWorkerConfig
		}
		state.SandboxFileCommand = heartbeat.SandboxFileCommand
		changed = true
	}
	if heartbeat.SandboxPTYCommand != nil {
		if state.SandboxCommand != nil || state.SandboxExecCommand != nil || state.SandboxFileCommand != nil || state.SandboxPreviewCommand != nil || state.SandboxPTYCommand != nil && state.SandboxPTYCommand.CommandID != heartbeat.SandboxPTYCommand.CommandID {
			return errInvalidRemoteWorkerConfig
		}
		state.SandboxPTYCommand = heartbeat.SandboxPTYCommand
		changed = true
	}
	if heartbeat.SandboxPreviewCommand != nil {
		if state.SandboxCommand != nil || state.SandboxExecCommand != nil || state.SandboxFileCommand != nil || state.SandboxPTYCommand != nil || state.SandboxPreviewCommand != nil && state.SandboxPreviewCommand.CommandID != heartbeat.SandboxPreviewCommand.CommandID {
			return errInvalidRemoteWorkerConfig
		}
		state.SandboxPreviewCommand = heartbeat.SandboxPreviewCommand
		changed = true
	}
	if changed {
		return saveNodeState(path, *state)
	}
	return nil
}

func executePendingSandbox(ctx context.Context, value config, state *nodeState, renew func(context.Context) error) error {
	if state == nil || state.SandboxCommand == nil || state.SandboxCommandReceipt != nil {
		return nil
	}
	started, err := beginCommandExecution(value.stateFile, state, state.SandboxCommand.CommandID)
	if err != nil {
		return err
	}
	if !started {
		state.SandboxCommand = nil
		state.ExecutingCommandID = ""
		return saveNodeState(value.stateFile, *state)
	}
	effectContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := *state.SandboxCommand
	if deadline, ok := effectContext.Deadline(); ok {
		command.Deadline = deadline.UTC().Format(time.RFC3339Nano)
	}
	result := make(chan platform.RemoteWorkerSandboxCommandReceipt, 1)
	go func() { result <- executeSandboxCommand(effectContext, value, command) }()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case receipt := <-result:
			state.SandboxCommandReceipt = &receipt
			return saveNodeState(value.stateFile, *state)
		case <-ticker.C:
			if renew == nil {
				return errInvalidRemoteWorkerConfig
			}
			if err := renew(ctx); err != nil {
				cancel()
				<-result
				state.SandboxCommand = nil
				state.ExecutingCommandID = ""
				if saveErr := saveNodeState(value.stateFile, *state); saveErr != nil {
					return saveErr
				}
				return err
			}
		case <-ctx.Done():
			cancel()
			<-result
			return ctx.Err()
		}
	}
}

func executeSandboxCommand(ctx context.Context, value config, command platform.RemoteWorkerSandboxCommand) platform.RemoteWorkerSandboxCommandReceipt {
	deadline, err := time.Parse(time.RFC3339Nano, command.Deadline)
	result := foundationcontroller.EffectResult{}
	if err != nil || !deadline.After(time.Now()) {
		result.Err = context.DeadlineExceeded
	} else {
		docker, dockerErr := dockertarget.NewCredentialDirectory(value.credentialDirectory)
		sandbox, sandboxErr := opensandbox.NewCredentialDirectory(value.credentialDirectory)
		if dockerErr != nil {
			result.Err = dockerErr
		} else if sandboxErr != nil {
			result.Err = sandboxErr
		} else {
			effectContext, cancel := context.WithDeadline(ctx, deadline)
			claim := postgres.FoundationSandboxClaim{
				TenantID: value.tenantID, ProjectID: value.projectID, TargetKind: "remote-worker",
				TargetID: command.TargetID, TargetEndpoint: value.dockerEndpoint, CredentialRef: value.credentialRef,
				Action: command.Action, OperationID: command.OperationID, WorkspaceID: command.WorkspaceID,
				WorkspaceName: command.WorkspaceName, SandboxID: command.SandboxID,
				SandboxGeneration: command.SandboxGeneration, ImageURI: command.ImageURI,
				WorkloadTrust: command.WorkloadTrust, IsolationRuntime: command.IsolationRuntime,
				CPUMillis: command.CPUMillis, MemoryBytes: command.MemoryBytes, SpecDigest: command.SpecDigest,
				NetworkPolicyID: command.NetworkPolicyID, NetworkAllowedEgress: command.NetworkAllowedEgress,
			}
			if command.Action != "sandbox.create" {
				claim.PhysicalVolumeName = &command.PhysicalVolumeName
			}
			if command.Action == "sandbox.stop" {
				claim.RuntimeID, claim.RuntimeState = &command.RuntimeID, command.RuntimeState
				claim.RuntimeOperationID, claim.RuntimeGeneration, claim.RuntimeSpecDigest = &command.RuntimeOperationID, &command.RuntimeGeneration, &command.RuntimeSpecDigest
			}
			result = foundationcontroller.ExecuteEffect(effectContext, docker, nil, sandbox, claim)
			cancel()
		}
	}
	receipt := platform.RemoteWorkerSandboxCommandReceipt{CommandID: command.CommandID, Attempt: command.Attempt,
		Action: command.Action, OperationID: command.OperationID, SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration,
		RuntimeID: result.RuntimeID, RuntimeState: result.RuntimeState, VolumeName: result.VolumeName,
		CleanupComplete: result.CleanupComplete}
	if result.Err == nil {
		receipt.Result = "succeeded"
	} else {
		receipt.Result = "failed"
		_, receipt.StableErrorCode = foundationcontroller.ClassifyEffect(result.Err, int32(command.Attempt))
	}
	return receipt
}

func executePendingSandboxExec(ctx context.Context, value config, state *nodeState) error {
	if state == nil || state.SandboxExecCommand == nil || state.SandboxExecCommandReceipt != nil {
		return nil
	}
	command := state.SandboxExecCommand
	receipt := &platform.RemoteWorkerSandboxExecCommandReceipt{CommandID: command.CommandID,
		SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration, Result: "failed"}
	started, err := beginCommandExecution(value.stateFile, state, command.CommandID)
	if err != nil {
		return err
	}
	if !started {
		receipt.StableErrorCode = "sandbox_access_unavailable"
		state.SandboxExecCommandReceipt = receipt
		return saveNodeState(value.stateFile, *state)
	}
	deadline, err := time.Parse(time.RFC3339Nano, command.Deadline)
	if err == nil && deadline.After(time.Now()) {
		directory, directoryErr := opensandbox.NewCredentialDirectory(value.credentialDirectory)
		var client *opensandbox.Client
		if directoryErr == nil {
			client, directoryErr = directory.Client(value.credentialRef)
		}
		if directoryErr == nil {
			execContext, cancel := context.WithDeadline(ctx, deadline)
			result, execErr := client.Exec(execContext, opensandbox.ExecInput{
				Identity: opensandbox.Identity{Tenant: value.tenantID, Project: value.projectID,
					Workspace: command.WorkspaceID, Sandbox: command.SandboxID,
					Operation: command.RuntimeOperationID, Generation: command.SandboxGeneration,
					SpecDigest: command.RuntimeSpecDigest},
				RuntimeID: command.RuntimeID, Command: command.Command,
				Timeout: time.Duration(command.TimeoutSeconds) * time.Second,
			})
			cancel()
			if execErr == nil {
				receipt.Result, receipt.ExitCode, receipt.Stdout, receipt.Stderr, receipt.ExecutionTimeMillis =
					"succeeded", result.ExitCode, result.Stdout, result.Stderr, result.ExecutionTimeMillis
			} else {
				err = execErr
			}
		} else {
			err = directoryErr
		}
	} else {
		err = context.DeadlineExceeded
	}
	if err != nil {
		receipt.StableErrorCode = sandboxExecStableError(err)
	}
	state.SandboxExecCommandReceipt = receipt
	return saveNodeState(value.stateFile, *state)
}

func sandboxExecStableError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "sandbox_exec_timeout"
	case errors.Is(err, opensandbox.ErrOutputLimit):
		return "sandbox_exec_output_limit"
	case errors.Is(err, opensandbox.ErrConflict), errors.Is(err, opensandbox.ErrRuntimeFailed), errors.Is(err, opensandbox.ErrNotFound):
		return "sandbox_runtime_unavailable"
	default:
		return "sandbox_access_unavailable"
	}
}

func executePendingSandboxFile(ctx context.Context, value config, state *nodeState) error {
	if state == nil || state.SandboxFileCommand == nil || state.SandboxFileCommandReceipt != nil {
		return nil
	}
	command := state.SandboxFileCommand
	receipt := &platform.RemoteWorkerSandboxFileCommandReceipt{CommandID: command.CommandID,
		EventID: command.EventID, GrantID: command.GrantID, SandboxID: command.SandboxID,
		SandboxGeneration: command.SandboxGeneration, Action: command.Action, Result: "failed"}
	started, err := beginCommandExecution(value.stateFile, state, command.CommandID)
	if err != nil {
		return err
	}
	if !started {
		receipt.StableErrorCode = "sandbox_access_unavailable"
		state.SandboxFileCommandReceipt = receipt
		return saveNodeState(value.stateFile, *state)
	}
	deadline, err := time.Parse(time.RFC3339Nano, command.Deadline)
	if err == nil && deadline.After(time.Now()) {
		directory, directoryErr := opensandbox.NewCredentialDirectory(value.credentialDirectory)
		var client *opensandbox.Client
		if directoryErr == nil {
			client, directoryErr = directory.Client(value.credentialRef)
		}
		if directoryErr == nil {
			fileContext, cancel := context.WithDeadline(ctx, deadline)
			input := opensandbox.PTYInput{Identity: opensandbox.Identity{Tenant: value.tenantID,
				Project: value.projectID, Workspace: command.WorkspaceID, Sandbox: command.SandboxID,
				Operation: command.RuntimeOperationID, Generation: command.SandboxGeneration,
				SpecDigest: command.RuntimeSpecDigest}, RuntimeID: command.RuntimeID}
			switch command.Action {
			case "list":
				var entries []opensandbox.FileEntry
				entries, err = client.ListFiles(fileContext, input, command.Path)
				if err == nil {
					values := make([]platform.SandboxFileEntry, 0, len(entries))
					for _, entry := range entries {
						values = append(values, platform.SandboxFileEntry{Path: entry.Path, Type: entry.Type,
							SizeBytes: entry.SizeBytes, ModifiedAt: entry.ModifiedAt, FileVersion: entry.FileVersion})
					}
					receipt.List = &platform.RemoteWorkerSandboxFileListResult{Entries: values}
				}
			case "read":
				var read opensandbox.FileRead
				read, err = client.ReadFile(fileContext, input, command.Path, command.Read.Offset, int(command.Read.Limit), command.Read.FileVersion)
				if err == nil {
					receipt.BytesTransferred = int64(len(read.Content))
					receipt.Read = &platform.RemoteWorkerSandboxFileReadResult{FileVersion: read.FileVersion,
						Offset: read.Offset, TotalBytes: read.TotalBytes,
						ContentBase64URL: base64.RawURLEncoding.EncodeToString(read.Content)}
				}
			case "write":
				var content []byte
				content, err = base64.RawURLEncoding.Strict().DecodeString(command.Write.ContentBase64URL)
				var entry opensandbox.FileEntry
				if err == nil {
					entry, err = client.WriteFile(fileContext, input, command.Path, content)
				}
				if err == nil {
					receipt.BytesTransferred = int64(len(content))
					receipt.Write = &platform.RemoteWorkerSandboxFileWriteResult{Entry: platform.SandboxFileEntry{
						Path: entry.Path, Type: entry.Type, SizeBytes: entry.SizeBytes,
						ModifiedAt: entry.ModifiedAt, FileVersion: entry.FileVersion}}
				}
			case "delete":
				err = client.DeleteFile(fileContext, input, command.Path)
			}
			cancel()
		} else {
			err = directoryErr
		}
	} else {
		err = context.DeadlineExceeded
	}
	if err == nil {
		receipt.Result = "succeeded"
	} else {
		receipt.BytesTransferred, receipt.List, receipt.Read, receipt.Write = 0, nil, nil, nil
		receipt.StableErrorCode = sandboxFileStableError(err)
	}
	state.SandboxFileCommandReceipt = receipt
	return saveNodeState(value.stateFile, *state)
}

func sandboxFileStableError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "sandbox_file_timeout"
	case errors.Is(err, opensandbox.ErrNotFound):
		return "sandbox_file_not_found"
	case errors.Is(err, opensandbox.ErrConflict):
		return "sandbox_file_conflict"
	case errors.Is(err, opensandbox.ErrFileLimit):
		return "sandbox_file_limit"
	case errors.Is(err, opensandbox.ErrInvalid):
		return "sandbox_file_invalid"
	case errors.Is(err, opensandbox.ErrRuntimeFailed):
		return "sandbox_runtime_unavailable"
	default:
		return "sandbox_access_unavailable"
	}
}

func executePendingSandboxPTY(ctx context.Context, value config, state *nodeState) error {
	if state == nil || state.SandboxPTYCommand == nil || state.SandboxPTYCommandReceipt != nil {
		return nil
	}
	command := state.SandboxPTYCommand
	receipt := &platform.RemoteWorkerSandboxPTYCommandReceipt{CommandID: command.CommandID,
		GrantID: command.GrantID, SandboxID: command.SandboxID,
		SandboxGeneration: command.SandboxGeneration, Action: command.Action, Result: "failed"}
	started, err := beginCommandExecution(value.stateFile, state, command.CommandID)
	if err != nil {
		return err
	}
	if !started {
		receipt.StableErrorCode = "sandbox_access_unavailable"
		state.SandboxPTYCommandReceipt = receipt
		return saveNodeState(value.stateFile, *state)
	}
	deadline, err := time.Parse(time.RFC3339Nano, command.Deadline)
	if err == nil && deadline.After(time.Now()) {
		directory, directoryErr := opensandbox.NewCredentialDirectory(value.credentialDirectory)
		var client *opensandbox.Client
		if directoryErr == nil {
			client, directoryErr = directory.Client(value.credentialRef)
		}
		if directoryErr == nil {
			ptyContext, cancel := context.WithDeadline(ctx, deadline)
			input := opensandbox.PTYInput{Identity: opensandbox.Identity{Tenant: value.tenantID,
				Project: value.projectID, Workspace: command.WorkspaceID, Sandbox: command.SandboxID,
				Operation: command.RuntimeOperationID, Generation: command.SandboxGeneration,
				SpecDigest: command.RuntimeSpecDigest}, RuntimeID: command.RuntimeID}
			switch command.Action {
			case "create":
				var observation opensandbox.PTYObservation
				observation, err = client.CreatePTY(ptyContext, input)
				if err == nil {
					receipt.SessionID, receipt.Running, receipt.OutputOffset = observation.SessionID, boolPointer(false), int64Pointer(0)
				}
			case "get":
				var observation opensandbox.PTYObservation
				observation, err = client.GetPTY(ptyContext, input, command.SessionID)
				if err == nil {
					receipt.SessionID, receipt.Running, receipt.OutputOffset = observation.SessionID, boolPointer(observation.Running), int64Pointer(observation.OutputOffset)
				}
			case "delete":
				err = client.DeletePTY(ptyContext, input, command.SessionID)
				if errors.Is(err, opensandbox.ErrNotFound) {
					err = nil
				}
				if err == nil {
					receipt.SessionID = command.SessionID
				}
			case "exchange":
				var frames []platform.RemoteWorkerSandboxPTYFrame
				var outputOffset int64
				var running bool
				frames, outputOffset, running, err = exchangeSandboxPTY(ptyContext, client, input, *command)
				if err == nil {
					receipt.SessionID, receipt.Running, receipt.OutputOffset, receipt.Frames =
						command.SessionID, boolPointer(running), int64Pointer(outputOffset), &frames
					for _, frame := range frames {
						payload, _ := base64.RawURLEncoding.DecodeString(frame.PayloadBase64URL)
						receipt.BytesTransferred += int64(len(payload))
					}
				}
			default:
				err = opensandbox.ErrInvalid
			}
			cancel()
		} else {
			err = directoryErr
		}
	} else {
		err = context.DeadlineExceeded
	}
	if err == nil {
		receipt.Result = "succeeded"
	} else {
		receipt.BytesTransferred, receipt.SessionID, receipt.Running, receipt.OutputOffset, receipt.Frames = 0, "", nil, nil, nil
		receipt.StableErrorCode = sandboxPTYStableError(err)
	}
	state.SandboxPTYCommandReceipt = receipt
	return saveNodeState(value.stateFile, *state)
}

func exchangeSandboxPTY(ctx context.Context, client *opensandbox.Client, input opensandbox.PTYInput, command platform.RemoteWorkerSandboxPTYCommand) ([]platform.RemoteWorkerSandboxPTYFrame, int64, bool, error) {
	if client == nil || command.Since == nil || command.Takeover == nil {
		return nil, 0, false, opensandbox.ErrInvalid
	}
	target, headers, err := client.PTYWebSocketTarget(ctx, input, command.SessionID)
	if err != nil {
		return nil, 0, false, err
	}
	switch target.Scheme {
	case "http":
		target.Scheme = "ws"
	case "https":
		target.Scheme = "wss"
	default:
		return nil, 0, false, opensandbox.ErrUnavailable
	}
	query := target.Query()
	query.Set("since", fmt.Sprintf("%d", *command.Since))
	if *command.Takeover {
		query.Set("takeover", "1")
	}
	if command.PTY != nil && !*command.PTY {
		query.Set("pty", "0")
	}
	target.RawQuery = query.Encode()
	connection, response, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).DialContext(ctx, target.String(), headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, 0, false, opensandbox.ErrUnavailable
	}
	defer connection.Close()
	connection.SetReadLimit(1052672)
	if command.Input != nil {
		payload, decodeErr := base64.RawURLEncoding.Strict().DecodeString(command.Input.PayloadBase64URL)
		messageType := websocket.BinaryMessage
		if command.Input.MessageType == "text" {
			messageType = websocket.TextMessage
		}
		if decodeErr != nil || connection.WriteMessage(messageType, payload) != nil {
			return nil, 0, false, opensandbox.ErrUnavailable
		}
	}
	frames := make([]platform.RemoteWorkerSandboxPTYFrame, 0)
	bytesTransferred, outputOffset, running := 0, *command.Since, true
	for len(frames) < 256 {
		readDeadline := time.Now().Add(250 * time.Millisecond)
		if deadline, ok := ctx.Deadline(); ok && deadline.Before(readDeadline) {
			readDeadline = deadline
		}
		_ = connection.SetReadDeadline(readDeadline)
		messageType, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			var networkError net.Error
			if errors.As(readErr, &networkError) && networkError.Timeout() && command.Input != nil && command.PTY != nil && !*command.PTY {
				if ctx.Err() != nil {
					return nil, 0, false, ctx.Err()
				}
				continue
			}
			if errors.As(readErr, &networkError) && networkError.Timeout() || websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			return nil, 0, false, opensandbox.ErrUnavailable
		}
		if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage {
			return nil, 0, false, opensandbox.ErrOutputLimit
		}
		truncated := false
		if messageType == websocket.BinaryMessage {
			if 1052672-bytesTransferred <= 9 {
				break
			}
			payload, outputOffset, truncated, err = boundSandboxPTYBinaryFrame(payload, outputOffset, 1052672-bytesTransferred)
			if err != nil {
				return nil, 0, false, err
			}
		} else {
			if bytesTransferred+len(payload) > 1052672 {
				return nil, 0, false, opensandbox.ErrOutputLimit
			}
			if strings.Contains(string(payload), `"type":"exit"`) {
				running = false
			}
		}
		frameType := "binary"
		if messageType == websocket.TextMessage {
			frameType = "text"
		}
		frames = append(frames, platform.RemoteWorkerSandboxPTYFrame{MessageType: frameType,
			PayloadBase64URL: base64.RawURLEncoding.EncodeToString(payload)})
		bytesTransferred += len(payload)
		if truncated {
			break
		}
	}
	return frames, outputOffset, running, nil
}

func boundSandboxPTYBinaryFrame(payload []byte, outputOffset int64, maximum int) ([]byte, int64, bool, error) {
	if len(payload) == 0 || outputOffset < 0 || outputOffset > 9007199254740991 {
		return nil, 0, false, opensandbox.ErrUnavailable
	}
	header, start := 1, outputOffset
	switch payload[0] {
	case 1, 2:
	case 3:
		header = 9
		if len(payload) < header {
			return nil, 0, false, opensandbox.ErrUnavailable
		}
		start = int64(binary.BigEndian.Uint64(payload[1:9]))
		if start < outputOffset || start > 9007199254740991 {
			return nil, 0, false, opensandbox.ErrUnavailable
		}
	default:
		return nil, 0, false, opensandbox.ErrUnavailable
	}
	if maximum <= header {
		return nil, 0, false, opensandbox.ErrOutputLimit
	}
	truncated := len(payload) > maximum
	if truncated {
		payload = payload[:maximum]
	}
	contentBytes := int64(len(payload) - header)
	if start > 9007199254740991-contentBytes {
		return nil, 0, false, opensandbox.ErrUnavailable
	}
	return payload, start + contentBytes, truncated, nil
}

func boolPointer(value bool) *bool    { return &value }
func int64Pointer(value int64) *int64 { return &value }

func executePendingSandboxPreview(ctx context.Context, value config, state *nodeState) error {
	if state == nil || state.SandboxPreviewCommand == nil || state.SandboxPreviewCommandReceipt != nil {
		return nil
	}
	command := state.SandboxPreviewCommand
	receipt := &platform.RemoteWorkerSandboxPreviewCommandReceipt{CommandID: command.CommandID,
		GrantID: command.GrantID, SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration,
		Port: command.Port, Result: "failed"}
	started, err := beginCommandExecution(value.stateFile, state, command.CommandID)
	if err != nil {
		return err
	}
	if !started {
		receipt.StableErrorCode = "sandbox_access_unavailable"
		state.SandboxPreviewCommandReceipt = receipt
		return saveNodeState(value.stateFile, *state)
	}
	deadline, err := time.Parse(time.RFC3339Nano, command.Deadline)
	if err == nil && deadline.After(time.Now()) {
		directory, directoryErr := opensandbox.NewCredentialDirectory(value.credentialDirectory)
		var client *opensandbox.Client
		if directoryErr == nil {
			client, directoryErr = directory.Client(value.credentialRef)
		}
		body, bodyErr := base64.RawURLEncoding.Strict().DecodeString(command.BodyBase64URL)
		if directoryErr == nil && bodyErr == nil && len(body) <= 1<<20 {
			previewContext, cancel := context.WithDeadline(ctx, deadline)
			headers := make([]opensandbox.PreviewHeader, len(command.Headers))
			for index, header := range command.Headers {
				headers[index] = opensandbox.PreviewHeader{Name: header.Name, Value: header.Value}
			}
			result, requestErr := client.ProxyPreview(previewContext, opensandbox.PTYInput{Identity: opensandbox.Identity{
				Tenant: value.tenantID, Project: value.projectID, Workspace: command.WorkspaceID,
				Sandbox: command.SandboxID, Operation: command.RuntimeOperationID,
				Generation: command.SandboxGeneration, SpecDigest: command.RuntimeSpecDigest,
			}, RuntimeID: command.RuntimeID}, int32(command.Port), command.Method, command.Path, command.RawQuery, headers, body)
			cancel()
			err = requestErr
			if err == nil {
				responseHeaders := make([]platform.RemoteWorkerSandboxPreviewHeader, len(result.Headers))
				for index, header := range result.Headers {
					responseHeaders[index] = platform.RemoteWorkerSandboxPreviewHeader{Name: header.Name, Value: header.Value}
				}
				encoded := base64.RawURLEncoding.EncodeToString(result.Body)
				receipt.Result, receipt.BytesTransferred, receipt.StatusCode, receipt.Headers, receipt.BodyBase64URL =
					"succeeded", int64(len(result.Body)), int64Pointer(int64(result.StatusCode)), &responseHeaders, &encoded
			}
		} else if directoryErr != nil {
			err = directoryErr
		} else if bodyErr != nil {
			err = opensandbox.ErrInvalid
		} else {
			err = opensandbox.ErrFileLimit
		}
	} else {
		err = context.DeadlineExceeded
	}
	if err != nil {
		receipt.StableErrorCode = sandboxPreviewStableError(err)
	}
	state.SandboxPreviewCommandReceipt = receipt
	return saveNodeState(value.stateFile, *state)
}

func sandboxPTYStableError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "sandbox_pty_timeout"
	case errors.Is(err, opensandbox.ErrNotFound):
		return "sandbox_pty_not_found"
	case errors.Is(err, opensandbox.ErrConflict):
		return "sandbox_pty_conflict"
	case errors.Is(err, opensandbox.ErrOutputLimit):
		return "sandbox_pty_output_limit"
	case errors.Is(err, opensandbox.ErrInvalid):
		return "sandbox_pty_invalid"
	case errors.Is(err, opensandbox.ErrRuntimeFailed):
		return "sandbox_runtime_unavailable"
	default:
		return "sandbox_access_unavailable"
	}
}

func sandboxPreviewStableError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "sandbox_preview_timeout"
	case errors.Is(err, opensandbox.ErrNotFound):
		return "sandbox_preview_not_found"
	case errors.Is(err, opensandbox.ErrFileLimit):
		return "sandbox_preview_input_limit"
	case errors.Is(err, opensandbox.ErrOutputLimit):
		return "sandbox_preview_output_limit"
	case errors.Is(err, opensandbox.ErrInvalid):
		return "sandbox_preview_invalid"
	case errors.Is(err, opensandbox.ErrRuntimeFailed):
		return "sandbox_runtime_unavailable"
	default:
		return "sandbox_access_unavailable"
	}
}

func newClient(value config) (*api.Client, error) {
	certificate, err := tls.LoadX509KeyPair(value.certificate, value.privateKey)
	if err != nil {
		return nil, errInvalidRemoteWorkerConfig
	}
	caPEM, err := os.ReadFile(value.serverCA)
	if err != nil || len(caPEM) == 0 || len(caPEM) > 1<<20 {
		return nil, errInvalidRemoteWorkerConfig
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errInvalidRemoteWorkerConfig
	}
	transport := &http.Transport{
		Proxy:             nil,
		ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    roots, Certificates: []tls.Certificate{certificate},
		},
	}
	return api.NewRemoteWorkerMTLSHTTPClientWithClient(value.controlPlaneURL, &http.Client{Transport: transport, Timeout: 15 * time.Second})
}

func runHeartbeatLoop(ctx context.Context, once bool, send func(context.Context) error, wait func(context.Context, time.Duration) error) error {
	if ctx == nil || send == nil || wait == nil {
		return errInvalidRemoteWorkerConfig
	}
	backoff := time.Second
	for {
		err := send(ctx)
		if once {
			return err
		}
		delay := 5 * time.Second
		if err != nil {
			delay = backoff
			backoff = min(backoff*2, 30*time.Second)
		} else {
			backoff = time.Second
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func main() {
	value, err := parseConfig(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	client, err := newClient(value)
	if err != nil {
		log.Fatal(err)
	}
	certificateExpiresAt := time.Time{}
	if value.certificateResourceVersionFile != "" {
		certificateExpiresAt, err = certificateNotAfter(value.certificate, value.privateKey)
		if err != nil {
			log.Fatal(err)
		}
		if _, err = readCertificateResourceVersion(value.certificateResourceVersionFile); err != nil {
			log.Fatal(err)
		}
	}
	state, err := loadNodeState(value.stateFile, value.incarnationID)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rotateIdentity := func(callContext context.Context) error {
		if value.certificateResourceVersionFile != "" && certificateRotationDue(certificateExpiresAt, time.Now(), value.certificateRotationBefore, value.rotateCertificateOnce) {
			var replacementClient *api.Client
			replacementClient, certificateExpiresAt, err = rotateCertificate(callContext, client, value)
			if err != nil {
				return err
			}
			client = replacementClient
			value.rotateCertificateOnce = false
			log.Printf("remote worker certificate rotated; expires at %s", certificateExpiresAt.UTC().Format(time.RFC3339))
		}
		return nil
	}
	heartbeat := func(callContext context.Context) (bool, bool, error) {
		if err := rotateIdentity(callContext); err != nil {
			log.Printf("remote worker certificate rotation failed: %v", err)
			return false, false, err
		}
		requestID := fmt.Sprintf("remote-worker-heartbeat-%d", time.Now().UnixNano())
		response, callErr := client.HeartbeatRemoteWorker(callContext, value.tenantID, value.projectID, value.enrollmentID, requestID, value.heartbeatRequest(state))
		if callErr != nil {
			log.Printf("remote worker heartbeat failed: %v", callErr)
			return false, false, callErr
		}
		if err := reconcileHeartbeat(value.stateFile, &state, response.Value, time.Now()); err != nil {
			return false, false, err
		}
		return response.Value.SandboxPTYCommand != nil, response.Value.SandboxPreviewCommand != nil, nil
	}
	renew := func(callContext context.Context) error {
		_, _, err := heartbeat(callContext)
		return err
	}
	if err := rotateIdentity(ctx); err != nil {
		log.Fatal(err)
	}
	if err := executePendingSandbox(ctx, value, &state, renew); err != nil {
		log.Fatal(err)
	}
	if err := executePendingSandboxExec(ctx, value, &state); err != nil {
		log.Fatal(err)
	}
	if err := executePendingSandboxFile(ctx, value, &state); err != nil {
		log.Fatal(err)
	}
	if err := executePendingSandboxPTY(ctx, value, &state); err != nil {
		log.Fatal(err)
	}
	if err := executePendingSandboxPreview(ctx, value, &state); err != nil {
		log.Fatal(err)
	}
	accessActiveUntil := time.Time{}
	err = runHeartbeatLoop(ctx, value.once, func(callContext context.Context) error {
		ptyActive, previewActive, err := heartbeat(callContext)
		if err != nil {
			return err
		}
		if err := executePendingSandbox(callContext, value, &state, renew); err != nil {
			return err
		}
		if err := executePendingSandboxExec(callContext, value, &state); err != nil {
			return err
		}
		if err := executePendingSandboxFile(callContext, value, &state); err != nil {
			return err
		}
		if ptyActive {
			accessActiveUntil = time.Now().Add(10 * time.Second)
		}
		if previewActive {
			accessActiveUntil = time.Now().Add(10 * time.Second)
		}
		if err := executePendingSandboxPTY(callContext, value, &state); err != nil {
			return err
		}
		return executePendingSandboxPreview(callContext, value, &state)
	}, func(waitContextValue context.Context, delay time.Duration) error {
		if delay == 5*time.Second && time.Now().Before(accessActiveUntil) {
			delay = 100 * time.Millisecond
		}
		return waitContext(waitContextValue, delay)
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
