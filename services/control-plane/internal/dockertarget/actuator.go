package dockertarget

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
)

const (
	MinCPULimitMillis   = int64(100)
	MaxCPULimitMillis   = int64(64_000)
	MinMemoryLimitBytes = int64(128 << 20)
	MaxMemoryLimitBytes = int64(1 << 40)
	workerPort          = "8091/tcp"
	maxDockerBodyBytes  = 1 << 20
)

var (
	ErrDeploymentConfigUnavailable = errors.New("docker target deployment configuration is unavailable")
	ErrDeploymentConfigInvalid     = errors.New("docker target deployment configuration is invalid")
	ErrDeploymentConflict          = errors.New("docker target deployment conflicts with an existing workload")
	ErrDeploymentFailed            = errors.New("docker target deployment failed")
	ErrWorkerUnavailable           = errors.New("docker target worker is unavailable")
	volumeNamePattern              = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	imageRepositoryPattern         = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]{1,5})?(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$`)
)

type WorkerTrust struct {
	ClientCertificate tls.Certificate
	RootCAs           *x509.CertPool
}

type DeployRequest struct {
	TenantID, ProjectID, TargetID, LeaseID string
	TargetGeneration, LeaseGeneration      int64
	ReleaseDigest, ProviderCredentialRef   string
	CPULimitMillis, MemoryLimitBytes       int64
}

type DeployResult struct {
	Endpoint         string
	WorkerSPIFFEID   string
	WorkerServerName string
}

type ManagedWorker struct {
	Request DeployRequest
	id      string
	image   string
	labels  map[string]string
}

type DeploymentConfig struct {
	WorkerImageRepository string `json:"workerImageRepository"`
	WorkerCredentialRef   string `json:"workerCredentialRef"`
	WorkerSPIFFEID        string `json:"workerSpiffeId"`
	WorkerServerName      string `json:"workerServerName"`
}

type containerInspect struct {
	Name   string `json:"Name"`
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
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

type containerSummary struct {
	ID string `json:"Id"`
}

func (directory *CredentialDirectory) DeployWorker(ctx context.Context, endpoint, credentialRef string, request DeployRequest, trust WorkerTrust) (DeployResult, error) {
	if ctx == nil || request.Validate() != nil || trust.RootCAs == nil || len(trust.ClientCertificate.Certificate) == 0 || trust.ClientCertificate.PrivateKey == nil {
		return DeployResult{}, ErrDeploymentConfigInvalid
	}
	config, err := directory.readDeploymentConfig(credentialRef)
	if err != nil {
		return DeployResult{}, err
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return DeployResult{}, err
	}
	defer transport.CloseIdleConnections()
	if err := requireVolume(ctx, client, base, config.WorkerCredentialRef); err != nil {
		return DeployResult{}, err
	}
	if err := requireVolume(ctx, client, base, request.ProviderCredentialRef); err != nil {
		return DeployResult{}, err
	}
	labels := DeploymentLabels(request, config)
	image := config.WorkerImageRepository + "@" + request.ReleaseDigest
	containerID, err := ensureWorkerContainer(ctx, client, base, request, config, image, labels)
	if err != nil {
		return DeployResult{}, err
	}
	owned := false
	inspect, err := inspectWorkerContainer(ctx, client, base, containerID)
	if err == nil && inspect.Config.Image == image && exactLabels(inspect.Config.Labels, labels) {
		owned = true
	} else if err == nil {
		err = ErrDeploymentConflict
	}
	if err != nil {
		return DeployResult{}, err
	}
	cleanup := func() {
		if owned {
			cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = removeWorkerContainer(cleanupContext, client, base, containerID)
		}
	}
	if err := startWorkerContainer(ctx, client, base, containerID); err != nil {
		cleanup()
		return DeployResult{}, err
	}
	inspect, err = waitForRunningContainer(ctx, client, base, containerID)
	if err != nil {
		cleanup()
		return DeployResult{}, err
	}
	workerEndpoint, err := deployedWorkerEndpoint(endpoint, inspect)
	if err != nil {
		cleanup()
		return DeployResult{}, err
	}
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: config.WorkerSPIFFEID, TrustDomain: spiffeTrustDomain(config.WorkerSPIFFEID)}
	supervisor, err := workerclient.NewMTLS(workerclient.MTLSConfig{Endpoint: workerEndpoint, ExpectedWorkerIdentity: identity, ClientCertificate: trust.ClientCertificate, RootCAs: trust.RootCAs, ServerName: config.WorkerServerName, Clock: time.Now})
	if err != nil || waitForWorker(ctx, supervisor) != nil {
		cleanup()
		return DeployResult{}, ErrWorkerUnavailable
	}
	return DeployResult{Endpoint: workerEndpoint, WorkerSPIFFEID: config.WorkerSPIFFEID, WorkerServerName: config.WorkerServerName}, nil
}

func (directory *CredentialDirectory) CleanupWorker(ctx context.Context, endpoint, credentialRef string, request DeployRequest) error {
	if ctx == nil || request.Validate() != nil {
		return ErrDeploymentConfigInvalid
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	workers, err := listManagedWorkers(ctx, client, base, request.TenantID, request.ProjectID, request.TargetID, request.TargetGeneration)
	if err != nil {
		return err
	}
	for _, worker := range workers {
		if worker.Request.LeaseID != request.LeaseID {
			continue
		}
		if err := cleanupManagedWorker(ctx, client, base, worker); err != nil {
			return err
		}
	}
	return nil
}

// DeployWorkerUpgrade starts the requested generation beside any existing
// generation and reuses its workspace volume. Older generations are removed
// only by CleanupOlderWorkers after the Lease records the new ready endpoint.
func (directory *CredentialDirectory) DeployWorkerUpgrade(ctx context.Context, endpoint, credentialRef string, request DeployRequest, trust WorkerTrust) (DeployResult, error) {
	if ctx == nil || request.Validate() != nil || trust.RootCAs == nil || len(trust.ClientCertificate.Certificate) == 0 || trust.ClientCertificate.PrivateKey == nil {
		return DeployResult{}, ErrDeploymentConfigInvalid
	}
	config, err := directory.readDeploymentConfig(credentialRef)
	if err != nil {
		return DeployResult{}, err
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return DeployResult{}, err
	}
	defer transport.CloseIdleConnections()
	if err := requireVolume(ctx, client, base, config.WorkerCredentialRef); err != nil {
		return DeployResult{}, err
	}
	if err := requireVolume(ctx, client, base, request.ProviderCredentialRef); err != nil {
		return DeployResult{}, err
	}
	workers, err := listManagedWorkers(ctx, client, base, request.TenantID, request.ProjectID, request.TargetID, request.TargetGeneration)
	if err != nil {
		return DeployResult{}, err
	}
	labels := DeploymentLabels(request, config)
	image := config.WorkerImageRepository + "@" + request.ReleaseDigest
	var current *ManagedWorker
	workspaceSource := ""
	for index := range workers {
		worker := &workers[index]
		if worker.Request.LeaseID != request.LeaseID {
			continue
		}
		inspect, inspectErr := inspectWorkerContainer(ctx, client, base, worker.id)
		if inspectErr != nil {
			return DeployResult{}, inspectErr
		}
		if worker.Request.LeaseGeneration == request.LeaseGeneration {
			if current != nil || inspect.Config.Image != image || !exactLabels(inspect.Config.Labels, labels) {
				return DeployResult{}, ErrDeploymentConflict
			}
			current = worker
			continue
		}
		if worker.Request.LeaseGeneration > request.LeaseGeneration {
			return DeployResult{}, ErrDeploymentConflict
		}
		volume, volumeErr := workspaceVolume(inspect)
		if volumeErr != nil {
			return DeployResult{}, volumeErr
		}
		if workspaceSource == "" {
			workspaceSource = volume
		} else if workspaceSource != volume {
			return DeployResult{}, ErrDeploymentConflict
		}
	}
	containerID := ""
	created := false
	if current != nil {
		containerID = current.id
	} else {
		containerID, err = createWorkerContainerNamed(ctx, client, base, request, config, image, labels, WorkerContainerName(request), workspaceSource)
		if err != nil {
			if found, findErr := findWorkerContainerForGeneration(ctx, client, base, request); findErr == nil && found != "" {
				candidate, inspectErr := inspectWorkerContainer(ctx, client, base, found)
				if inspectErr != nil {
					return DeployResult{}, inspectErr
				}
				if candidate.Name != "/"+WorkerContainerName(request) || candidate.Config.Image != image || !exactLabels(candidate.Config.Labels, labels) {
					return DeployResult{}, ErrDeploymentConflict
				}
				containerID = found
			} else {
				return DeployResult{}, err
			}
		} else {
			created = true
		}
	}
	cleanup := func() {
		if !created || containerID == "" {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = removeWorkerContainer(cleanupContext, client, base, containerID)
	}
	if err := startWorkerContainer(ctx, client, base, containerID); err != nil {
		cleanup()
		return DeployResult{}, err
	}
	inspect, err := waitForRunningContainer(ctx, client, base, containerID)
	if err != nil {
		cleanup()
		return DeployResult{}, err
	}
	workerEndpoint, err := deployedWorkerEndpoint(endpoint, inspect)
	if err != nil {
		cleanup()
		return DeployResult{}, err
	}
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: config.WorkerSPIFFEID, TrustDomain: spiffeTrustDomain(config.WorkerSPIFFEID)}
	supervisor, err := workerclient.NewMTLS(workerclient.MTLSConfig{Endpoint: workerEndpoint, ExpectedWorkerIdentity: identity, ClientCertificate: trust.ClientCertificate, RootCAs: trust.RootCAs, ServerName: config.WorkerServerName, Clock: time.Now})
	if err != nil || waitForWorker(ctx, supervisor) != nil {
		cleanup()
		return DeployResult{}, ErrWorkerUnavailable
	}
	return DeployResult{Endpoint: workerEndpoint, WorkerSPIFFEID: config.WorkerSPIFFEID, WorkerServerName: config.WorkerServerName}, nil
}

// CleanupOlderWorkers removes lower lease generations after a newer one has
// become ready. Repeating it is safe because missing workers are ignored.
func (directory *CredentialDirectory) CleanupOlderWorkers(ctx context.Context, endpoint, credentialRef string, request DeployRequest) error {
	if ctx == nil || request.Validate() != nil {
		return ErrDeploymentConfigInvalid
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	workers, err := listManagedWorkers(ctx, client, base, request.TenantID, request.ProjectID, request.TargetID, request.TargetGeneration)
	if err != nil {
		return err
	}
	for _, worker := range workers {
		if worker.Request.LeaseID == request.LeaseID && worker.Request.LeaseGeneration < request.LeaseGeneration {
			if err := cleanupManagedWorker(ctx, client, base, worker); err != nil {
				return err
			}
		}
	}
	return nil
}

func (directory *CredentialDirectory) ListManagedWorkers(ctx context.Context, endpoint, credentialRef, tenantID, projectID, targetID string, targetGeneration int64) ([]ManagedWorker, error) {
	if ctx == nil || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(targetID, "/targetId") != nil || targetGeneration < 1 {
		return nil, ErrDeploymentConfigInvalid
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return nil, err
	}
	defer transport.CloseIdleConnections()
	return listManagedWorkers(ctx, client, base, tenantID, projectID, targetID, targetGeneration)
}

func (directory *CredentialDirectory) CleanupManagedWorker(ctx context.Context, endpoint, credentialRef string, worker ManagedWorker) error {
	if ctx == nil || worker.Request.Validate() != nil || !volumeNamePattern.MatchString(worker.id) || worker.image == "" || len(worker.labels) == 0 {
		return ErrDeploymentConfigInvalid
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	return cleanupManagedWorker(ctx, client, base, worker)
}

func cleanupWorkerContainer(ctx context.Context, client *http.Client, base string, request DeployRequest, config DeploymentConfig) error {
	containerID, err := findWorkerContainer(ctx, client, base, request)
	if err != nil || containerID == "" {
		return err
	}
	inspect, err := inspectWorkerContainer(ctx, client, base, containerID)
	if err != nil {
		remaining, findErr := findWorkerContainer(ctx, client, base, request)
		if findErr != nil || remaining != "" {
			return ErrDeploymentFailed
		}
		return nil
	}
	image := config.WorkerImageRepository + "@" + request.ReleaseDigest
	if inspect.Config.Image != image || !exactLabels(inspect.Config.Labels, DeploymentLabels(request, config)) {
		return ErrDeploymentConflict
	}
	return removeWorkerContainer(ctx, client, base, containerID)
}

func (request DeployRequest) Validate() error {
	for path, value := range map[string]string{"/tenantId": request.TenantID, "/projectId": request.ProjectID, "/targetId": request.TargetID, "/leaseId": request.LeaseID, "/providerCredentialRef": request.ProviderCredentialRef} {
		if commonv1alpha1.ValidateIdentifier(value, path) != nil {
			return ErrDeploymentConfigInvalid
		}
	}
	if !volumeNamePattern.MatchString(request.ProviderCredentialRef) || request.TargetGeneration < 1 || request.LeaseGeneration < 1 || request.CPULimitMillis < MinCPULimitMillis || request.CPULimitMillis > MaxCPULimitMillis || request.MemoryLimitBytes < MinMemoryLimitBytes || request.MemoryLimitBytes > MaxMemoryLimitBytes || len(request.ReleaseDigest) != 71 || !strings.HasPrefix(request.ReleaseDigest, "sha256:") {
		return ErrDeploymentConfigInvalid
	}
	for _, character := range request.ReleaseDigest[7:] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return ErrDeploymentConfigInvalid
		}
	}
	return nil
}

func (directory *CredentialDirectory) readDeploymentConfig(credentialRef string) (DeploymentConfig, error) {
	if directory == nil || commonv1alpha1.ValidateIdentifier(credentialRef, "/credentialRef") != nil {
		return DeploymentConfig{}, ErrDeploymentConfigInvalid
	}
	root, err := os.OpenRoot(directory.path)
	if err != nil {
		return DeploymentConfig{}, ErrDeploymentConfigUnavailable
	}
	defer root.Close()
	value, err := readCredential(root, filepath.Join(credentialRef, "deployment.json"))
	if err != nil {
		return DeploymentConfig{}, ErrDeploymentConfigUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var config DeploymentConfig
	if decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF || !config.Valid() {
		return DeploymentConfig{}, ErrDeploymentConfigInvalid
	}
	return config, nil
}

func (config DeploymentConfig) Valid() bool {
	parsed, err := url.Parse(config.WorkerSPIFFEID)
	return imageRepositoryPattern.MatchString(config.WorkerImageRepository) && volumeNamePattern.MatchString(config.WorkerCredentialRef) &&
		err == nil && parsed.Scheme == "spiffe" && parsed.Host != "" && parsed.Path != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" &&
		config.WorkerServerName != "" && len(config.WorkerServerName) <= 253 && strings.TrimSpace(config.WorkerServerName) == config.WorkerServerName && !strings.ContainsAny(config.WorkerServerName, "/@") && strings.IndexFunc(config.WorkerServerName, unicode.IsControl) < 0
}

func spiffeTrustDomain(identity string) string {
	parsed, _ := url.Parse(identity)
	return parsed.Host
}

func DeploymentLabels(request DeployRequest, config DeploymentConfig) map[string]string {
	return map[string]string{
		"cloud-agents.dev/managed":                 "true",
		"cloud-agents.dev/tenant":                  request.TenantID,
		"cloud-agents.dev/project":                 request.ProjectID,
		"cloud-agents.dev/target":                  request.TargetID,
		"cloud-agents.dev/target-generation":       strconv.FormatInt(request.TargetGeneration, 10),
		"cloud-agents.dev/lease":                   request.LeaseID,
		"cloud-agents.dev/lease-generation":        strconv.FormatInt(request.LeaseGeneration, 10),
		"cloud-agents.dev/release-digest":          request.ReleaseDigest,
		"cloud-agents.dev/provider-credential-ref": request.ProviderCredentialRef,
		"cloud-agents.dev/worker-credential-ref":   config.WorkerCredentialRef,
		"cloud-agents.dev/cpu-limit-millis":        strconv.FormatInt(request.CPULimitMillis, 10),
		"cloud-agents.dev/memory-limit-bytes":      strconv.FormatInt(request.MemoryLimitBytes, 10),
		"cloud-agents.dev/worker-spiffe-id":        config.WorkerSPIFFEID,
		"cloud-agents.dev/worker-server-name":      config.WorkerServerName,
	}
}

func ManagedWorkerRequest(name, image string, labels map[string]string, expectedTargetGeneration int64) (DeployRequest, error) {
	targetGeneration, targetErr := strconv.ParseInt(labels["cloud-agents.dev/target-generation"], 10, 64)
	leaseGeneration, leaseErr := strconv.ParseInt(labels["cloud-agents.dev/lease-generation"], 10, 64)
	cpuLimit, cpuErr := strconv.ParseInt(labels["cloud-agents.dev/cpu-limit-millis"], 10, 64)
	memoryLimit, memoryErr := strconv.ParseInt(labels["cloud-agents.dev/memory-limit-bytes"], 10, 64)
	request := DeployRequest{
		TenantID: labels["cloud-agents.dev/tenant"], ProjectID: labels["cloud-agents.dev/project"], TargetID: labels["cloud-agents.dev/target"], LeaseID: labels["cloud-agents.dev/lease"],
		TargetGeneration: targetGeneration, LeaseGeneration: leaseGeneration, ReleaseDigest: labels["cloud-agents.dev/release-digest"],
		ProviderCredentialRef: labels["cloud-agents.dev/provider-credential-ref"], CPULimitMillis: cpuLimit, MemoryLimitBytes: memoryLimit,
	}
	config := DeploymentConfig{
		WorkerImageRepository: strings.TrimSuffix(image, "@"+request.ReleaseDigest),
		WorkerCredentialRef:   labels["cloud-agents.dev/worker-credential-ref"],
		WorkerSPIFFEID:        labels["cloud-agents.dev/worker-spiffe-id"],
		WorkerServerName:      labels["cloud-agents.dev/worker-server-name"],
	}
	if targetErr != nil || leaseErr != nil || cpuErr != nil || memoryErr != nil || expectedTargetGeneration < 1 || targetGeneration > expectedTargetGeneration || request.Validate() != nil || !config.Valid() || image != config.WorkerImageRepository+"@"+request.ReleaseDigest || name != WorkerContainerName(request) || !exactLabels(labels, DeploymentLabels(request, config)) {
		return DeployRequest{}, ErrDeploymentConflict
	}
	return request, nil
}

func exactLabels(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func requireVolume(ctx context.Context, client *http.Client, base, name string) error {
	var response struct {
		Name string `json:"Name"`
	}
	if err := dockerJSON(ctx, client, http.MethodGet, base+"/volumes/"+url.PathEscape(name), nil, http.StatusOK, &response); err != nil || response.Name != name {
		return ErrDeploymentConfigUnavailable
	}
	return nil
}

func findWorkerContainer(ctx context.Context, client *http.Client, base string, request DeployRequest) (string, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {
		"cloud-agents.dev/managed=true",
		"cloud-agents.dev/tenant=" + request.TenantID,
		"cloud-agents.dev/project=" + request.ProjectID,
		"cloud-agents.dev/lease=" + request.LeaseID,
	}})
	var containers []containerSummary
	if err := dockerJSON(ctx, client, http.MethodGet, base+"/containers/json?all=1&filters="+url.QueryEscape(string(filters)), nil, http.StatusOK, &containers); err != nil {
		return "", err
	}
	if len(containers) > 1 || len(containers) == 1 && containers[0].ID == "" {
		return "", ErrDeploymentConflict
	}
	if len(containers) == 1 {
		return containers[0].ID, nil
	}
	return "", nil
}

func findWorkerContainerForGeneration(ctx context.Context, client *http.Client, base string, request DeployRequest) (string, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {
		"cloud-agents.dev/managed=true",
		"cloud-agents.dev/tenant=" + request.TenantID,
		"cloud-agents.dev/project=" + request.ProjectID,
		"cloud-agents.dev/target=" + request.TargetID,
		"cloud-agents.dev/lease=" + request.LeaseID,
		"cloud-agents.dev/lease-generation=" + strconv.FormatInt(request.LeaseGeneration, 10),
	}})
	var containers []containerSummary
	if err := dockerJSON(ctx, client, http.MethodGet, base+"/containers/json?all=1&filters="+url.QueryEscape(string(filters)), nil, http.StatusOK, &containers); err != nil {
		return "", err
	}
	if len(containers) > 1 || len(containers) == 1 && containers[0].ID == "" {
		return "", ErrDeploymentConflict
	}
	if len(containers) == 1 {
		return containers[0].ID, nil
	}
	return "", nil
}

func listManagedWorkers(ctx context.Context, client *http.Client, base, tenantID, projectID, targetID string, targetGeneration int64) ([]ManagedWorker, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {
		"cloud-agents.dev/managed=true",
		"cloud-agents.dev/tenant=" + tenantID,
		"cloud-agents.dev/project=" + projectID,
		"cloud-agents.dev/target=" + targetID,
	}})
	var containers []containerSummary
	if err := dockerJSON(ctx, client, http.MethodGet, base+"/containers/json?all=1&filters="+url.QueryEscape(string(filters)), nil, http.StatusOK, &containers); err != nil || len(containers) > 200 {
		return nil, ErrDeploymentFailed
	}
	slices.SortFunc(containers, func(left, right containerSummary) int { return strings.Compare(left.ID, right.ID) })
	workers := make([]ManagedWorker, 0, len(containers))
	for index, container := range containers {
		if !volumeNamePattern.MatchString(container.ID) || index > 0 && containers[index-1].ID == container.ID {
			return nil, ErrDeploymentConflict
		}
		inspect, err := inspectWorkerContainer(ctx, client, base, container.ID)
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(inspect.Name, "/")
		request, parseErr := ManagedWorkerRequest(name, inspect.Config.Image, inspect.Config.Labels, targetGeneration)
		if inspect.Name != "/"+name || parseErr != nil || request.TenantID != tenantID || request.ProjectID != projectID || request.TargetID != targetID {
			return nil, ErrDeploymentConflict
		}
		workers = append(workers, ManagedWorker{Request: request, id: container.ID, image: inspect.Config.Image, labels: maps.Clone(inspect.Config.Labels)})
	}
	return workers, nil
}

func cleanupManagedWorker(ctx context.Context, client *http.Client, base string, worker ManagedWorker) error {
	containerID, err := findWorkerContainer(ctx, client, base, worker.Request)
	if err != nil || containerID == "" {
		return err
	}
	if containerID != worker.id {
		return ErrDeploymentConflict
	}
	inspect, err := inspectWorkerContainer(ctx, client, base, worker.id)
	if err != nil {
		remaining, findErr := findWorkerContainer(ctx, client, base, worker.Request)
		if findErr != nil || remaining != "" {
			return ErrDeploymentFailed
		}
		return nil
	}
	if inspect.Name != "/"+WorkerContainerName(worker.Request) || inspect.Config.Image != worker.image || !exactLabels(inspect.Config.Labels, worker.labels) {
		return ErrDeploymentConflict
	}
	return removeWorkerContainer(ctx, client, base, worker.id)
}

func ensureWorkerContainer(ctx context.Context, client *http.Client, base string, request DeployRequest, config DeploymentConfig, image string, labels map[string]string) (string, error) {
	containerID, err := findWorkerContainer(ctx, client, base, request)
	if err != nil || containerID != "" {
		return containerID, err
	}
	containerID, err = createWorkerContainer(ctx, client, base, request, config, image, labels)
	if err == nil {
		return containerID, nil
	}
	// A concurrent retry may have won the deterministic-name create.
	containerID, err = findWorkerContainer(ctx, client, base, request)
	if err != nil || containerID == "" {
		return "", ErrDeploymentFailed
	}
	return containerID, nil
}

func createWorkerContainer(ctx context.Context, client *http.Client, base string, request DeployRequest, config DeploymentConfig, image string, labels map[string]string) (string, error) {
	return createWorkerContainerNamed(ctx, client, base, request, config, image, labels, WorkerContainerName(request), "")
}

func createWorkerContainerNamed(ctx context.Context, client *http.Client, base string, request DeployRequest, config DeploymentConfig, image string, labels map[string]string, name, workspaceSource string) (string, error) {
	body := map[string]any{
		"Image": image,
		"User":  "1000:1000",
		"Env": []string{
			"CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent",
			"CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1",
		},
		"Cmd": []string{
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
		},
		"Labels":       labels,
		"ExposedPorts": map[string]any{workerPort: map[string]any{}},
		"HostConfig": map[string]any{
			"Mounts": []map[string]any{
				{"Type": "volume", "Source": config.WorkerCredentialRef, "Target": "/run/cloud-agents/worker-credentials", "ReadOnly": true},
				{"Type": "volume", "Source": request.ProviderCredentialRef, "Target": "/run/cloud-agents/provider-credentials", "ReadOnly": true},
			},
			"Tmpfs":          map[string]string{"/tmp": "rw,noexec,nosuid,size=67108864,mode=1777"},
			"PortBindings":   map[string]any{workerPort: []map[string]string{{"HostPort": ""}}},
			"Memory":         request.MemoryLimitBytes,
			"NanoCpus":       request.CPULimitMillis * 1_000_000,
			"ReadonlyRootfs": true,
			"SecurityOpt":    []string{"no-new-privileges"},
			"CapDrop":        []string{"ALL"},
			"RestartPolicy":  map[string]string{"Name": "unless-stopped"},
		},
	}
	workspaceMount := map[string]any{"Type": "volume", "Target": "/workspace"}
	if workspaceSource != "" {
		workspaceMount["Source"] = workspaceSource
	}
	hostConfig := body["HostConfig"].(map[string]any)
	hostConfig["Mounts"] = append(hostConfig["Mounts"].([]map[string]any), workspaceMount)
	var response struct {
		ID string `json:"Id"`
	}
	if !volumeNamePattern.MatchString(name) {
		return "", ErrDeploymentConfigInvalid
	}
	createURL := base + "/containers/create?name=" + url.QueryEscape(name)
	if err := dockerJSON(ctx, client, http.MethodPost, createURL, body, http.StatusCreated, &response); err != nil || response.ID == "" {
		return "", ErrDeploymentFailed
	}
	return response.ID, nil
}

func WorkerContainerName(request DeployRequest) string {
	base := workerContainerBaseName(request)
	if request.LeaseGeneration <= 1 {
		return base
	}
	return base + "-g" + strconv.FormatInt(request.LeaseGeneration, 10)
}

func workerContainerBaseName(request DeployRequest) string {
	digest := fnv.New64a()
	for _, value := range []string{request.TenantID, request.ProjectID, request.LeaseID} {
		_, _ = digest.Write([]byte(value))
		_, _ = digest.Write([]byte{0})
	}
	return "cloud-agents-" + strconv.FormatUint(digest.Sum64(), 16)
}

func workspaceVolume(inspect containerInspect) (string, error) {
	for _, mount := range inspect.Mounts {
		if mount.Destination != "/workspace" || mount.Type != "volume" {
			continue
		}
		name := mount.Name
		if name == "" {
			name = mount.Source
		}
		if volumeNamePattern.MatchString(name) {
			return name, nil
		}
	}
	return "", ErrDeploymentConflict
}

func inspectWorkerContainer(ctx context.Context, client *http.Client, base, containerID string) (containerInspect, error) {
	var response containerInspect
	if containerID == "" || dockerJSON(ctx, client, http.MethodGet, base+"/containers/"+url.PathEscape(containerID)+"/json", nil, http.StatusOK, &response) != nil {
		return containerInspect{}, ErrDeploymentFailed
	}
	return response, nil
}

func startWorkerContainer(ctx context.Context, client *http.Client, base, containerID string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/containers/"+url.PathEscape(containerID)+"/start", http.NoBody)
	if err != nil {
		return ErrDeploymentFailed
	}
	response, err := client.Do(request)
	if err != nil {
		return ErrDeploymentFailed
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDockerBodyBytes))
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotModified {
		return ErrDeploymentFailed
	}
	return nil
}

func waitForRunningContainer(ctx context.Context, client *http.Client, base, containerID string) (containerInspect, error) {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		inspect, err := inspectWorkerContainer(ctx, client, base, containerID)
		if _, portErr := publishedWorkerPort(inspect); err == nil && inspect.State.Running && portErr == nil {
			return inspect, nil
		}
		select {
		case <-ctx.Done():
			return containerInspect{}, ctx.Err()
		case <-deadline.C:
			return containerInspect{}, ErrDeploymentFailed
		case <-ticker.C:
		}
	}
}

func deployedWorkerEndpoint(targetEndpoint string, inspect containerInspect) (string, error) {
	target, err := url.Parse(targetEndpoint)
	port, portErr := publishedWorkerPort(inspect)
	if err != nil || target.Hostname() == "" || portErr != nil {
		return "", ErrDeploymentFailed
	}
	return "https://" + net.JoinHostPort(target.Hostname(), port), nil
}

func publishedWorkerPort(inspect containerInspect) (string, error) {
	bindings := inspect.NetworkSettings.Ports[workerPort]
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

func removeWorkerContainer(ctx context.Context, client *http.Client, base, containerID string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/containers/"+url.PathEscape(containerID)+"?force=1&v=1", http.NoBody)
	if err != nil {
		return ErrDeploymentFailed
	}
	response, err := client.Do(request)
	if err != nil {
		return ErrDeploymentFailed
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDockerBodyBytes))
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return ErrDeploymentFailed
	}
	return nil
}

func dockerJSON(ctx context.Context, client *http.Client, method, target string, body any, expectedStatus int, output any) error {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return ErrDeploymentFailed
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return ErrDeploymentFailed
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return ErrDeploymentFailed
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDockerBodyBytes))
		return ErrDeploymentFailed
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDockerBodyBytes))
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxDockerBodyBytes+1))
	if decoder.Decode(output) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ErrDeploymentFailed
	}
	return nil
}
