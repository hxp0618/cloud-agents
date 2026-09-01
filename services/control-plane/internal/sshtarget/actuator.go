package sshtarget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
	"golang.org/x/crypto/ssh"
)

const (
	remoteWorkerPort     = "8091/tcp"
	maxCommandOutputSize = 1 << 20
)

var (
	ErrDeploymentConfigUnavailable = errors.New("SSH target deployment configuration is unavailable")
	ErrDeploymentConfigInvalid     = errors.New("SSH target deployment configuration is invalid")
	ErrDeploymentConflict          = errors.New("SSH target deployment conflicts with an existing workload")
	ErrDeploymentFailed            = errors.New("SSH target deployment failed")
	ErrWorkerUnavailable           = errors.New("SSH target worker is unavailable")
)

type DeployResult struct {
	Endpoint         string
	WorkerSPIFFEID   string
	WorkerServerName string
}

type ManagedWorker struct {
	Request dockertarget.DeployRequest
	name    string
	image   string
	labels  map[string]string
}

type remoteContainerInspect struct {
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

func (directory *CredentialDirectory) DeployWorker(ctx context.Context, endpoint, credentialRef string, request dockertarget.DeployRequest, trust dockertarget.WorkerTrust) (DeployResult, error) {
	if ctx == nil || request.Validate() != nil || trust.RootCAs == nil || len(trust.ClientCertificate.Certificate) == 0 || trust.ClientCertificate.PrivateKey == nil {
		return DeployResult{}, ErrDeploymentConfigInvalid
	}
	config, err := directory.readDeploymentConfig(credentialRef)
	if err != nil {
		return DeployResult{}, err
	}
	client, host, err := directory.connect(ctx, endpoint, credentialRef, 30*time.Second)
	if err != nil {
		return DeployResult{}, err
	}
	defer client.Close()
	for _, volume := range []string{config.WorkerCredentialRef, request.ProviderCredentialRef} {
		if err := requireRemoteVolume(client, volume); err != nil {
			return DeployResult{}, err
		}
	}
	name := dockertarget.WorkerContainerName(request)
	labels := dockertarget.DeploymentLabels(request, config)
	image := config.WorkerImageRepository + "@" + request.ReleaseDigest
	if err := ensureRemoteWorker(client, name, image, request, config, labels); err != nil {
		if errors.Is(err, ErrDeploymentFailed) {
			cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			_ = cleanupRemoteWorker(cleanupContext, client, name, image, labels)
			cancel()
		}
		return DeployResult{}, err
	}
	cleanup := func() {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = directory.CleanupWorker(cleanupContext, endpoint, credentialRef, request)
	}
	inspect, err := waitForRemoteWorker(ctx, client, name)
	port, portErr := remotePublishedWorkerPort(inspect)
	_ = client.Close()
	if err != nil || portErr != nil {
		cleanup()
		return DeployResult{}, ErrDeploymentFailed
	}
	workerEndpoint := "https://" + net.JoinHostPort(host, port)
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: config.WorkerSPIFFEID, TrustDomain: spiffeTrustDomain(config.WorkerSPIFFEID)}
	supervisor, err := workerclient.NewMTLS(workerclient.MTLSConfig{Endpoint: workerEndpoint, ExpectedWorkerIdentity: identity, ClientCertificate: trust.ClientCertificate, RootCAs: trust.RootCAs, ServerName: config.WorkerServerName, Clock: time.Now})
	if err != nil || waitForWorker(ctx, supervisor) != nil {
		cleanup()
		return DeployResult{}, ErrWorkerUnavailable
	}
	return DeployResult{Endpoint: workerEndpoint, WorkerSPIFFEID: config.WorkerSPIFFEID, WorkerServerName: config.WorkerServerName}, nil
}

func (directory *CredentialDirectory) CleanupWorker(ctx context.Context, endpoint, credentialRef string, request dockertarget.DeployRequest) error {
	if ctx == nil || request.Validate() != nil {
		return ErrDeploymentConfigInvalid
	}
	config, err := directory.readDeploymentConfig(credentialRef)
	if err != nil {
		return err
	}
	client, _, err := directory.connect(ctx, endpoint, credentialRef, 30*time.Second)
	if err != nil {
		return err
	}
	defer client.Close()
	return cleanupRemoteWorker(ctx, client, dockertarget.WorkerContainerName(request), config.WorkerImageRepository+"@"+request.ReleaseDigest, dockertarget.DeploymentLabels(request, config))
}

func (directory *CredentialDirectory) ListManagedWorkers(ctx context.Context, endpoint, credentialRef, tenantID, projectID, targetID string, targetGeneration int64) ([]ManagedWorker, error) {
	for path, value := range map[string]string{"/tenantId": tenantID, "/projectId": projectID, "/targetId": targetID} {
		if commonv1alpha1.ValidateIdentifier(value, path) != nil {
			return nil, ErrDeploymentConfigInvalid
		}
	}
	if ctx == nil || targetGeneration < 1 {
		return nil, ErrDeploymentConfigInvalid
	}
	client, _, err := directory.connect(ctx, endpoint, credentialRef, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	names, err := listRemoteWorkerNames(client, tenantID, projectID, targetID)
	if err != nil {
		return nil, err
	}
	workers := make([]ManagedWorker, 0, len(names))
	for _, name := range names {
		inspect, inspectErr := inspectRemoteWorker(client, name)
		if inspectErr != nil {
			return nil, inspectErr
		}
		worker, parseErr := managedWorker(name, inspect, targetGeneration)
		if parseErr != nil || worker.Request.TenantID != tenantID || worker.Request.ProjectID != projectID || worker.Request.TargetID != targetID {
			return nil, ErrDeploymentConflict
		}
		workers = append(workers, worker)
	}
	return workers, nil
}

func (directory *CredentialDirectory) CleanupManagedWorker(ctx context.Context, endpoint, credentialRef string, worker ManagedWorker) error {
	if ctx == nil || worker.Request.Validate() != nil || worker.name != dockertarget.WorkerContainerName(worker.Request) || worker.image == "" || len(worker.labels) == 0 {
		return ErrDeploymentConfigInvalid
	}
	client, _, err := directory.connect(ctx, endpoint, credentialRef, 30*time.Second)
	if err != nil {
		return err
	}
	defer client.Close()
	return cleanupRemoteWorker(ctx, client, worker.name, worker.image, worker.labels)
}

func (directory *CredentialDirectory) readDeploymentConfig(credentialRef string) (dockertarget.DeploymentConfig, error) {
	if directory == nil || commonv1alpha1.ValidateIdentifier(credentialRef, "/credentialRef") != nil {
		return dockertarget.DeploymentConfig{}, ErrDeploymentConfigInvalid
	}
	root, err := os.OpenRoot(directory.path)
	if err != nil {
		return dockertarget.DeploymentConfig{}, ErrDeploymentConfigUnavailable
	}
	defer root.Close()
	value, err := readCredential(root, credentialRef+".deployment.json", false)
	if err != nil {
		return dockertarget.DeploymentConfig{}, ErrDeploymentConfigUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var config dockertarget.DeploymentConfig
	if decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF || !config.Valid() {
		return dockertarget.DeploymentConfig{}, ErrDeploymentConfigInvalid
	}
	return config, nil
}

func requireRemoteVolume(client *ssh.Client, name string) error {
	output, err := runSSHCommand(client, dockerCommand("volume", "inspect", "--format", "{{.Name}}", "--", name))
	if err != nil || strings.TrimSpace(string(output)) != name {
		return ErrDeploymentConfigUnavailable
	}
	return nil
}

func ensureRemoteWorker(client *ssh.Client, name, image string, request dockertarget.DeployRequest, config dockertarget.DeploymentConfig, labels map[string]string) error {
	found, err := findRemoteWorker(client, name, request)
	if err != nil {
		return err
	}
	if !found {
		if _, err = runSSHCommand(client, remoteWorkerRunCommand(name, image, request, config, labels)); err != nil {
			if found, err = findRemoteWorker(client, name, request); err != nil || !found {
				return ErrDeploymentFailed
			}
		}
	}
	inspect, err := inspectRemoteWorker(client, name)
	if err != nil {
		return ErrDeploymentFailed
	}
	if inspect.Config.Image != image || !exactLabels(inspect.Config.Labels, labels) {
		return ErrDeploymentConflict
	}
	if !inspect.State.Running {
		if _, err := runSSHCommand(client, dockerCommand("start", "--", name)); err != nil {
			return ErrDeploymentFailed
		}
	}
	return nil
}

func findRemoteWorker(client *ssh.Client, name string, request dockertarget.DeployRequest) (bool, error) {
	arguments := []string{"ps", "-a", "--format", "{{.Names}}"}
	for _, filter := range []string{
		"cloud-agents.dev/managed=true",
		"cloud-agents.dev/tenant=" + request.TenantID,
		"cloud-agents.dev/project=" + request.ProjectID,
		"cloud-agents.dev/lease=" + request.LeaseID,
	} {
		arguments = append(arguments, "--filter", "label="+filter)
	}
	output, err := runSSHCommand(client, dockerCommand(arguments...))
	if err != nil {
		return false, ErrDeploymentFailed
	}
	lines := strings.Fields(string(output))
	if len(lines) > 1 || len(lines) == 1 && lines[0] != name {
		return false, ErrDeploymentConflict
	}
	return len(lines) == 1, nil
}

func listRemoteWorkerNames(client *ssh.Client, tenantID, projectID, targetID string) ([]string, error) {
	arguments := []string{"ps", "-a", "--format", "{{.Names}}"}
	for _, filter := range []string{
		"cloud-agents.dev/managed=true",
		"cloud-agents.dev/tenant=" + tenantID,
		"cloud-agents.dev/project=" + projectID,
		"cloud-agents.dev/target=" + targetID,
	} {
		arguments = append(arguments, "--filter", "label="+filter)
	}
	output, err := runSSHCommand(client, dockerCommand(arguments...))
	if err != nil {
		return nil, ErrDeploymentFailed
	}
	names := strings.Fields(string(output))
	if len(names) > 200 {
		return nil, ErrDeploymentConflict
	}
	slices.Sort(names)
	for index, name := range names {
		if name == "" || len(name) > 128 || index > 0 && names[index-1] == name {
			return nil, ErrDeploymentConflict
		}
	}
	return names, nil
}

func managedWorker(name string, inspect remoteContainerInspect, expectedTargetGeneration int64) (ManagedWorker, error) {
	request, err := dockertarget.ManagedWorkerRequest(name, inspect.Config.Image, inspect.Config.Labels, expectedTargetGeneration)
	if err != nil {
		return ManagedWorker{}, ErrDeploymentConflict
	}
	return ManagedWorker{Request: request, name: name, image: inspect.Config.Image, labels: inspect.Config.Labels}, nil
}

func inspectRemoteWorker(client *ssh.Client, name string) (remoteContainerInspect, error) {
	output, err := runSSHCommand(client, dockerCommand("inspect", "--", name))
	if err != nil {
		return remoteContainerInspect{}, ErrDeploymentFailed
	}
	var values []remoteContainerInspect
	decoder := json.NewDecoder(bytes.NewReader(output))
	if decoder.Decode(&values) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(values) != 1 {
		return remoteContainerInspect{}, ErrDeploymentFailed
	}
	return values[0], nil
}

func waitForRemoteWorker(ctx context.Context, client *ssh.Client, name string) (remoteContainerInspect, error) {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		inspect, err := inspectRemoteWorker(client, name)
		if _, portErr := remotePublishedWorkerPort(inspect); err == nil && inspect.State.Running && portErr == nil {
			return inspect, nil
		}
		select {
		case <-ctx.Done():
			return remoteContainerInspect{}, ctx.Err()
		case <-deadline.C:
			return remoteContainerInspect{}, ErrDeploymentFailed
		case <-ticker.C:
		}
	}
}

func cleanupRemoteWorker(ctx context.Context, client *ssh.Client, name, image string, labels map[string]string) error {
	request := dockertarget.DeployRequest{TenantID: labels["cloud-agents.dev/tenant"], ProjectID: labels["cloud-agents.dev/project"], LeaseID: labels["cloud-agents.dev/lease"]}
	found, err := findRemoteWorker(client, name, request)
	if err != nil || !found {
		return err
	}
	inspect, err := inspectRemoteWorker(client, name)
	if err != nil {
		return err
	}
	if inspect.Config.Image != image || !exactLabels(inspect.Config.Labels, labels) {
		return ErrDeploymentConflict
	}
	if _, err := runSSHCommand(client, dockerCommand("rm", "--force", "--volumes", "--", name)); err == nil {
		return nil
	}
	found, findErr := findRemoteWorker(client, name, request)
	if findErr == nil && !found {
		return nil
	}
	return ErrDeploymentFailed
}

func remoteWorkerRunCommand(name, image string, request dockertarget.DeployRequest, config dockertarget.DeploymentConfig, labels map[string]string) string {
	arguments := []string{
		"run", "--detach", "--pull", "never", "--name", name, "--user", "1000:1000",
		"--env", "CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent",
		"--env", "CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1",
		"--read-only", "--security-opt", "no-new-privileges", "--cap-drop", "ALL",
		"--restart", "unless-stopped", "--memory", strconv.FormatInt(request.MemoryLimitBytes, 10),
		"--cpu-period", "100000", "--cpu-quota", strconv.FormatInt(request.CPULimitMillis*100, 10),
		"--mount", "type=volume,src=" + config.WorkerCredentialRef + ",dst=/run/cloud-agents/worker-credentials,readonly",
		"--mount", "type=volume,src=" + request.ProviderCredentialRef + ",dst=/run/cloud-agents/provider-credentials,readonly",
		"--mount", "type=volume,dst=/workspace",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=67108864,mode=1777", "--publish", "8091",
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		arguments = append(arguments, "--label", key+"="+labels[key])
	}
	arguments = append(arguments,
		image,
		"--listen", ":8091",
		"--tls-cert", "/run/cloud-agents/worker-credentials/server.crt",
		"--tls-key", "/run/cloud-agents/worker-credentials/server.key",
		"--client-ca", "/run/cloud-agents/worker-credentials/client-ca.crt",
		"--worker-spiffe-id", config.WorkerSPIFFEID,
		"--runtime-command", "/usr/local/bin/cloud-agent-runtime",
		"--runtime-directory", "/workspace",
		"--runtime-max-sessions", "1",
		"--provider-credential-directory", "/run/cloud-agents/provider-credentials",
		"--admission-lease-id", request.LeaseID,
		"--admission-generation", strconv.FormatInt(request.LeaseGeneration, 10),
		"--admission-token-file", "/run/cloud-agents/worker-credentials/admission-token",
	)
	return dockerCommand(arguments...)
}

func dockerCommand(arguments ...string) string {
	quoted := make([]string, len(arguments)+1)
	quoted[0] = "docker"
	for index, argument := range arguments {
		quoted[index+1] = shellQuote(argument)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func runSSHCommand(client *ssh.Client, command string) ([]byte, error) {
	if client == nil || command == "" {
		return nil, ErrDeploymentFailed
	}
	session, err := client.NewSession()
	if err != nil {
		return nil, ErrDeploymentFailed
	}
	defer session.Close()
	output := &limitedWriter{remaining: maxCommandOutputSize}
	session.Stdout, session.Stderr = output, io.Discard
	if err := session.Run(command); err != nil {
		return nil, ErrDeploymentFailed
	}
	return output.Bytes(), nil
}

func remotePublishedWorkerPort(inspect remoteContainerInspect) (string, error) {
	bindings := inspect.NetworkSettings.Ports[remoteWorkerPort]
	if len(bindings) == 0 {
		return "", ErrDeploymentFailed
	}
	port := bindings[0].HostPort
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", ErrDeploymentFailed
	}
	for _, binding := range bindings[1:] {
		if binding.HostPort != port {
			return "", ErrDeploymentFailed
		}
	}
	return port, nil
}

func exactLabels(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func spiffeTrustDomain(identity string) string {
	parsed, _ := url.Parse(identity)
	return parsed.Host
}

func waitForWorker(ctx context.Context, supervisor *workerclient.Supervisor) error {
	healthContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if err := supervisor.CheckRuntimeHealth(healthContext); err == nil {
			return nil
		}
		select {
		case <-healthContext.Done():
			return ErrWorkerUnavailable
		case <-time.After(250 * time.Millisecond):
		}
	}
}
