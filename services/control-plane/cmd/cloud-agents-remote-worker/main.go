package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/foundationcontroller"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

var (
	version                      = "dev"
	errInvalidRemoteWorkerConfig = errors.New("cloud-agents-remote-worker/invalid_config")
)

type config struct {
	controlPlaneURL     string
	tenantID            string
	projectID           string
	enrollmentID        string
	incarnationID       string
	certificate         string
	privateKey          string
	serverCA            string
	stateFile           string
	dockerEndpoint      string
	credentialDirectory string
	credentialRef       string
	kernelVersion       string
	capabilities        []string
	capacity            platform.RemoteWorkerCapacity
	once                bool
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
		strings.TrimSpace(value.dockerEndpoint) != value.dockerEndpoint || strings.TrimSpace(value.credentialDirectory) != value.credentialDirectory {
		return config{}, errInvalidRemoteWorkerConfig
	}
	if _, err := platform.EncodeRemoteWorkerHeartbeatRequestJSON(request); err != nil {
		return config{}, errInvalidRemoteWorkerConfig
	}
	return value, nil
}

func (value config) heartbeatRequest(state nodeState) platform.RemoteWorkerHeartbeatRequest {
	return platform.RemoteWorkerHeartbeatRequest{
		IncarnationID: value.incarnationID, ObservedGeneration: state.ObservedGeneration, ObservedState: state.ObservedState,
		WorkerVersion: version, OS: runtime.GOOS, Architecture: runtime.GOARCH,
		KernelVersion: value.kernelVersion, Capabilities: value.capabilities, Capacity: value.capacity,
		CommandReceipt: state.CommandReceipt, SandboxCommandReceipt: state.SandboxCommandReceipt,
		SandboxExecCommandReceipt: state.SandboxExecCommandReceipt,
		SandboxFileCommandReceipt: state.SandboxFileCommandReceipt,
	}
}

type nodeState struct {
	IncarnationID             string                                          `json:"incarnationId"`
	ObservedGeneration        int64                                           `json:"observedGeneration"`
	ObservedState             string                                          `json:"observedState"`
	LastCommandID             string                                          `json:"lastCommandId,omitempty"`
	CommandReceipt            *platform.RemoteWorkerCommandReceipt            `json:"commandReceipt,omitempty"`
	SandboxCommand            *platform.RemoteWorkerSandboxCommand            `json:"sandboxCommand,omitempty"`
	SandboxCommandReceipt     *platform.RemoteWorkerSandboxCommandReceipt     `json:"sandboxCommandReceipt,omitempty"`
	SandboxExecCommand        *platform.RemoteWorkerSandboxExecCommand        `json:"sandboxExecCommand,omitempty"`
	SandboxExecCommandReceipt *platform.RemoteWorkerSandboxExecCommandReceipt `json:"sandboxExecCommandReceipt,omitempty"`
	SandboxFileCommand        *platform.RemoteWorkerSandboxFileCommand        `json:"sandboxFileCommand,omitempty"`
	SandboxFileCommandReceipt *platform.RemoteWorkerSandboxFileCommandReceipt `json:"sandboxFileCommandReceipt,omitempty"`
}

func initialNodeState(incarnationID string) nodeState {
	return nodeState{IncarnationID: incarnationID, ObservedGeneration: 1, ObservedState: "active"}
}

func validateNodeState(value nodeState) error {
	if common.ValidateIdentifier(value.IncarnationID, "/incarnationId") != nil || value.ObservedGeneration < 1 ||
		value.ObservedState != "active" && value.ObservedState != "drained" ||
		value.LastCommandID != "" && common.ValidateIdentifier(value.LastCommandID, "/lastCommandId") != nil {
		return errInvalidRemoteWorkerConfig
	}
	request := platform.RemoteWorkerHeartbeatRequest{IncarnationID: value.IncarnationID, ObservedGeneration: value.ObservedGeneration,
		ObservedState: value.ObservedState, WorkerVersion: "state", OS: "state", Architecture: "state", KernelVersion: "state",
		Capabilities: []string{"exec"}, Capacity: platform.RemoteWorkerCapacity{CPUMillis: 100, MemoryBytes: 134217728, DiskBytes: 134217728}, CommandReceipt: value.CommandReceipt, SandboxCommandReceipt: value.SandboxCommandReceipt, SandboxExecCommandReceipt: value.SandboxExecCommandReceipt, SandboxFileCommandReceipt: value.SandboxFileCommandReceipt}
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
	return nil
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
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".remote-worker-state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(append(data, '\n'))
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporaryPath, path)
	}
	if err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = dir.Sync()
	if closeErr := dir.Close(); err == nil {
		err = closeErr
	}
	return err
}

func reconcileHeartbeat(path string, state *nodeState, heartbeat platform.RemoteWorkerHeartbeat, now time.Time) error {
	if state == nil || heartbeat.IncarnationID != state.IncarnationID {
		return errInvalidRemoteWorkerConfig
	}
	if heartbeat.SandboxCommand != nil && heartbeat.SandboxExecCommand != nil ||
		heartbeat.SandboxCommand != nil && heartbeat.SandboxFileCommand != nil ||
		heartbeat.SandboxExecCommand != nil && heartbeat.SandboxFileCommand != nil {
		return errInvalidRemoteWorkerConfig
	}
	changed := state.CommandReceipt != nil || state.SandboxCommandReceipt != nil || state.SandboxExecCommandReceipt != nil || state.SandboxFileCommandReceipt != nil
	state.CommandReceipt = nil
	if state.SandboxCommandReceipt != nil {
		state.SandboxCommand, state.SandboxCommandReceipt = nil, nil
	}
	if state.SandboxExecCommandReceipt != nil {
		state.SandboxExecCommand, state.SandboxExecCommandReceipt = nil, nil
	}
	if state.SandboxFileCommandReceipt != nil {
		state.SandboxFileCommand, state.SandboxFileCommandReceipt = nil, nil
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
		if state.SandboxExecCommand != nil || state.SandboxCommand != nil && state.SandboxCommand.CommandID != heartbeat.SandboxCommand.CommandID {
			return errInvalidRemoteWorkerConfig
		}
		state.SandboxCommand = heartbeat.SandboxCommand
		changed = true
	}
	if heartbeat.SandboxExecCommand != nil {
		if state.SandboxCommand != nil || state.SandboxFileCommand != nil || state.SandboxExecCommand != nil && state.SandboxExecCommand.CommandID != heartbeat.SandboxExecCommand.CommandID {
			return errInvalidRemoteWorkerConfig
		}
		state.SandboxExecCommand = heartbeat.SandboxExecCommand
		changed = true
	}
	if heartbeat.SandboxFileCommand != nil {
		if state.SandboxCommand != nil || state.SandboxExecCommand != nil || state.SandboxFileCommand != nil && state.SandboxFileCommand.CommandID != heartbeat.SandboxFileCommand.CommandID {
			return errInvalidRemoteWorkerConfig
		}
		state.SandboxFileCommand = heartbeat.SandboxFileCommand
		changed = true
	}
	if changed {
		return saveNodeState(path, *state)
	}
	return nil
}

func executePendingSandbox(ctx context.Context, value config, state *nodeState) error {
	if state == nil || state.SandboxCommand == nil || state.SandboxCommandReceipt != nil {
		return nil
	}
	command := state.SandboxCommand
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
			result = foundationcontroller.ExecuteEffect(effectContext, docker, sandbox, claim)
			cancel()
		}
	}
	receipt := &platform.RemoteWorkerSandboxCommandReceipt{CommandID: command.CommandID, Attempt: command.Attempt,
		Action: command.Action, OperationID: command.OperationID, SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration,
		RuntimeID: result.RuntimeID, RuntimeState: result.RuntimeState, VolumeName: result.VolumeName,
		CleanupComplete: result.CleanupComplete}
	if result.Err == nil {
		receipt.Result = "succeeded"
	} else {
		receipt.Result = "failed"
		_, receipt.StableErrorCode = foundationcontroller.ClassifyEffect(result.Err, int32(command.Attempt))
	}
	state.SandboxCommandReceipt = receipt
	return saveNodeState(value.stateFile, *state)
}

func executePendingSandboxExec(ctx context.Context, value config, state *nodeState) error {
	if state == nil || state.SandboxExecCommand == nil || state.SandboxExecCommandReceipt != nil {
		return nil
	}
	command := state.SandboxExecCommand
	receipt := &platform.RemoteWorkerSandboxExecCommandReceipt{CommandID: command.CommandID,
		SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration, Result: "failed"}
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
	state, err := loadNodeState(value.stateFile, value.incarnationID)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := executePendingSandbox(ctx, value, &state); err != nil {
		log.Fatal(err)
	}
	if err := executePendingSandboxExec(ctx, value, &state); err != nil {
		log.Fatal(err)
	}
	if err := executePendingSandboxFile(ctx, value, &state); err != nil {
		log.Fatal(err)
	}
	err = runHeartbeatLoop(ctx, value.once, func(callContext context.Context) error {
		requestID := fmt.Sprintf("remote-worker-heartbeat-%d", time.Now().UnixNano())
		response, callErr := client.HeartbeatRemoteWorker(callContext, value.tenantID, value.projectID, value.enrollmentID, requestID, value.heartbeatRequest(state))
		if callErr != nil {
			log.Printf("remote worker heartbeat failed: %v", callErr)
			return callErr
		}
		if err := reconcileHeartbeat(value.stateFile, &state, response.Value, time.Now()); err != nil {
			return err
		}
		if err := executePendingSandbox(callContext, value, &state); err != nil {
			return err
		}
		if err := executePendingSandboxExec(callContext, value, &state); err != nil {
			return err
		}
		return executePendingSandboxFile(callContext, value, &state)
	}, waitContext)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
