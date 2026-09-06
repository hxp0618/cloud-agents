package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"strings"
	"syscall"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
)

var (
	version                      = "dev"
	errInvalidRemoteWorkerConfig = errors.New("cloud-agents-remote-worker/invalid_config")
)

type config struct {
	controlPlaneURL string
	tenantID        string
	projectID       string
	enrollmentID    string
	incarnationID   string
	certificate     string
	privateKey      string
	serverCA        string
	stateFile       string
	kernelVersion   string
	capabilities    []string
	capacity        platform.RemoteWorkerCapacity
	once            bool
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
	if common.ValidateIdentifier(value.tenantID, "/tenant") != nil || common.ValidateIdentifier(value.projectID, "/project") != nil || common.ValidateIdentifier(value.enrollmentID, "/enrollment") != nil ||
		value.controlPlaneURL == "" || value.certificate == "" || value.privateKey == "" || value.serverCA == "" || value.stateFile == "" ||
		strings.TrimSpace(value.controlPlaneURL) != value.controlPlaneURL ||
		strings.TrimSpace(value.certificate) != value.certificate || strings.TrimSpace(value.privateKey) != value.privateKey || strings.TrimSpace(value.serverCA) != value.serverCA || strings.TrimSpace(value.stateFile) != value.stateFile {
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
		KernelVersion: value.kernelVersion, Capabilities: value.capabilities, Capacity: value.capacity, CommandReceipt: state.CommandReceipt,
	}
}

type nodeState struct {
	IncarnationID      string                               `json:"incarnationId"`
	ObservedGeneration int64                                `json:"observedGeneration"`
	ObservedState      string                               `json:"observedState"`
	LastCommandID      string                               `json:"lastCommandId,omitempty"`
	CommandReceipt     *platform.RemoteWorkerCommandReceipt `json:"commandReceipt,omitempty"`
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
	if value.CommandReceipt == nil {
		return nil
	}
	request := platform.RemoteWorkerHeartbeatRequest{IncarnationID: value.IncarnationID, ObservedGeneration: value.ObservedGeneration,
		ObservedState: value.ObservedState, WorkerVersion: "state", OS: "state", Architecture: "state", KernelVersion: "state",
		Capabilities: []string{"exec"}, Capacity: platform.RemoteWorkerCapacity{CPUMillis: 100, MemoryBytes: 134217728, DiskBytes: 134217728}, CommandReceipt: value.CommandReceipt}
	if _, err := platform.EncodeRemoteWorkerHeartbeatRequestJSON(request); err != nil || value.LastCommandID != value.CommandReceipt.CommandID ||
		value.CommandReceipt.Result == "succeeded" && value.CommandReceipt.Generation != value.ObservedGeneration ||
		value.CommandReceipt.Result == "failed" && value.CommandReceipt.Generation != value.ObservedGeneration+1 {
		return errInvalidRemoteWorkerConfig
	}
	return nil
}

func loadNodeState(path, incarnationID string) (nodeState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return initialNodeState(incarnationID), nil
	}
	if err != nil || len(data) == 0 || len(data) > 64<<10 {
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
	changed := state.CommandReceipt != nil
	state.CommandReceipt = nil
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
	if changed {
		return saveNodeState(path, *state)
	}
	return nil
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
	err = runHeartbeatLoop(ctx, value.once, func(callContext context.Context) error {
		requestID := fmt.Sprintf("remote-worker-heartbeat-%d", time.Now().UnixNano())
		response, callErr := client.HeartbeatRemoteWorker(callContext, value.tenantID, value.projectID, value.enrollmentID, requestID, value.heartbeatRequest(state))
		if callErr != nil {
			log.Printf("remote worker heartbeat failed: %v", callErr)
			return callErr
		}
		return reconcileHeartbeat(value.stateFile, &state, response.Value, time.Now())
	}, waitContext)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
