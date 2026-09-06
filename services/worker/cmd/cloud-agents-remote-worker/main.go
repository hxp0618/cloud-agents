package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	request := value.heartbeatRequest()
	if common.ValidateIdentifier(value.tenantID, "/tenant") != nil || common.ValidateIdentifier(value.projectID, "/project") != nil || common.ValidateIdentifier(value.enrollmentID, "/enrollment") != nil ||
		value.controlPlaneURL == "" || value.certificate == "" || value.privateKey == "" || value.serverCA == "" ||
		strings.TrimSpace(value.controlPlaneURL) != value.controlPlaneURL ||
		strings.TrimSpace(value.certificate) != value.certificate || strings.TrimSpace(value.privateKey) != value.privateKey || strings.TrimSpace(value.serverCA) != value.serverCA {
		return config{}, errInvalidRemoteWorkerConfig
	}
	if _, err := platform.EncodeRemoteWorkerHeartbeatRequestJSON(request); err != nil {
		return config{}, errInvalidRemoteWorkerConfig
	}
	return value, nil
}

func (value config) heartbeatRequest() platform.RemoteWorkerHeartbeatRequest {
	// ponytail: heartbeat-only node starts at generation 1; command delivery will persist and advance observations.
	return platform.RemoteWorkerHeartbeatRequest{
		IncarnationID: value.incarnationID, ObservedGeneration: 1, ObservedState: "active",
		WorkerVersion: version, OS: runtime.GOOS, Architecture: runtime.GOARCH,
		KernelVersion: value.kernelVersion, Capabilities: value.capabilities, Capacity: value.capacity,
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
	request := value.heartbeatRequest()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = runHeartbeatLoop(ctx, value.once, func(callContext context.Context) error {
		requestID := fmt.Sprintf("remote-worker-heartbeat-%d", time.Now().UnixNano())
		_, callErr := client.HeartbeatRemoteWorker(callContext, value.tenantID, value.projectID, value.enrollmentID, requestID, request)
		if callErr != nil {
			log.Printf("remote worker heartbeat failed: %v", callErr)
		}
		return callErr
	}, waitContext)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
