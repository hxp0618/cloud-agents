package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
)

const (
	workerPort             = 8091
	maxKubernetesBodyBytes = 1 << 20
	workspaceStorage       = "20Gi"
)

var (
	ErrDeploymentConfigUnavailable = errors.New("Kubernetes target deployment configuration is unavailable")
	ErrDeploymentConfigInvalid     = errors.New("Kubernetes target deployment configuration is invalid")
	ErrDeploymentConflict          = errors.New("Kubernetes target deployment conflicts with an existing workload")
	ErrDeploymentFailed            = errors.New("Kubernetes target deployment failed")
	ErrWorkerUnavailable           = errors.New("Kubernetes target worker is unavailable")
	dnsLabelPattern                = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
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
	Request     DeployRequest
	name        string
	namespace   string
	annotations map[string]string
}

type deploymentConfig struct {
	Namespace                 string `json:"namespace"`
	WorkerImageRepository     string `json:"workerImageRepository"`
	WorkerCredentialSecretRef string `json:"workerCredentialSecretRef"`
	WorkerSPIFFEID            string `json:"workerSpiffeId"`
	WorkerServerName          string `json:"workerServerName"`
}

type resourceMetadata struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	UID             string            `json:"uid"`
	ResourceVersion string            `json:"resourceVersion"`
	Generation      int64             `json:"generation"`
	Annotations     map[string]string `json:"annotations"`
}

type resource struct {
	Metadata resourceMetadata `json:"metadata"`
	Status   struct {
		ObservedGeneration int64 `json:"observedGeneration"`
		AvailableReplicas  int64 `json:"availableReplicas"`
		UpdatedReplicas    int64 `json:"updatedReplicas"`
		LoadBalancer       struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

type desiredResource struct {
	path string
	body map[string]any
}

func (directory *CredentialDirectory) DeployWorker(ctx context.Context, endpoint, credentialRef string, request DeployRequest, trust WorkerTrust) (DeployResult, error) {
	return directory.deployWorker(ctx, endpoint, credentialRef, request, trust, false)
}

// DeployWorkerUpgrade applies a rolling update to the existing Deployment and
// waits for one updated replica before the caller advances the Lease.
func (directory *CredentialDirectory) DeployWorkerUpgrade(ctx context.Context, endpoint, credentialRef string, request DeployRequest, trust WorkerTrust) (DeployResult, error) {
	return directory.deployWorker(ctx, endpoint, credentialRef, request, trust, true)
}

func (directory *CredentialDirectory) deployWorker(ctx context.Context, endpoint, credentialRef string, request DeployRequest, trust WorkerTrust, rolling bool) (DeployResult, error) {
	if ctx == nil || request.validate() != nil || trust.RootCAs == nil || len(trust.ClientCertificate.Certificate) == 0 || trust.ClientCertificate.PrivateKey == nil {
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
	for _, secret := range []string{config.WorkerCredentialSecretRef, request.ProviderCredentialRef} {
		if err := requireSecret(ctx, client, base, config.Namespace, secret); err != nil {
			return DeployResult{}, err
		}
	}
	name := workerResourceName(request)
	annotations := deploymentAnnotations(request, config)
	resources := desiredResourcesWithStrategy(name, request, config, annotations, rolling)
	cleanupOnFailure := func() {
		if rolling {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		_ = cleanupResources(cleanupContext, client, base, config.Namespace, name, annotations)
		cancel()
	}
	for _, resource := range resources {
		if err := applyResource(ctx, client, base, resource); err != nil {
			cleanupOnFailure()
			return DeployResult{}, err
		}
	}
	workerEndpoint, err := waitForReadyResourcesMode(ctx, client, base, config.Namespace, name, annotations, rolling)
	if err != nil {
		cleanupOnFailure()
		return DeployResult{}, err
	}
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: config.WorkerSPIFFEID, TrustDomain: spiffeTrustDomain(config.WorkerSPIFFEID)}
	supervisor, err := workerclient.NewMTLS(workerclient.MTLSConfig{Endpoint: workerEndpoint, ExpectedWorkerIdentity: identity, ClientCertificate: trust.ClientCertificate, RootCAs: trust.RootCAs, ServerName: config.WorkerServerName, Clock: time.Now})
	if err != nil || waitForWorker(ctx, supervisor) != nil {
		cleanupOnFailure()
		return DeployResult{}, ErrWorkerUnavailable
	}
	return DeployResult{Endpoint: workerEndpoint, WorkerSPIFFEID: config.WorkerSPIFFEID, WorkerServerName: config.WorkerServerName}, nil
}

func (directory *CredentialDirectory) CleanupWorker(ctx context.Context, endpoint, credentialRef string, request DeployRequest) error {
	if ctx == nil || request.validate() != nil {
		return ErrDeploymentConfigInvalid
	}
	config, err := directory.readDeploymentConfig(credentialRef)
	if err != nil {
		return err
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	return cleanupResources(ctx, client, base, config.Namespace, workerResourceName(request), deploymentAnnotations(request, config))
}

func (directory *CredentialDirectory) ListManagedWorkers(ctx context.Context, endpoint, credentialRef, tenantID, projectID, targetID string, targetGeneration int64) ([]ManagedWorker, error) {
	if ctx == nil || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(targetID, "/targetId") != nil || targetGeneration < 1 {
		return nil, ErrDeploymentConfigInvalid
	}
	config, err := directory.readDeploymentConfig(credentialRef)
	if err != nil {
		return nil, err
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return nil, err
	}
	defer transport.CloseIdleConnections()
	workers := map[string]ManagedWorker{}
	listed := 0
	paths := []string{
		"/apis/apps/v1/namespaces/" + url.PathEscape(config.Namespace) + "/deployments",
		"/api/v1/namespaces/" + url.PathEscape(config.Namespace) + "/services",
		"/api/v1/namespaces/" + url.PathEscape(config.Namespace) + "/persistentvolumeclaims",
	}
	for _, path := range paths {
		continuation := ""
		for {
			query := url.Values{"labelSelector": {"cloud-agents.dev/managed=true"}, "limit": {"200"}}
			if continuation != "" {
				query.Set("continue", continuation)
			}
			var page struct {
				Metadata struct {
					Continue string `json:"continue"`
				} `json:"metadata"`
				Items []resource `json:"items"`
			}
			status, listErr := kubernetesJSON(ctx, client, http.MethodGet, base+path+"?"+query.Encode(), "", nil, &page)
			listed += len(page.Items)
			if listErr != nil || status != http.StatusOK || page.Items == nil || listed > 10_000 || page.Metadata.Continue != "" && page.Metadata.Continue == continuation {
				return nil, ErrDeploymentFailed
			}
			for _, item := range page.Items {
				annotations := item.Metadata.Annotations
				if annotations["cloud-agents.dev/target"] != targetID {
					continue
				}
				if annotations["cloud-agents.dev/tenant"] != tenantID || annotations["cloud-agents.dev/project"] != projectID {
					return nil, ErrDeploymentConflict
				}
				worker, parseErr := managedWorker(item.Metadata, targetGeneration, config)
				if parseErr != nil {
					return nil, parseErr
				}
				if existing, ok := workers[worker.name]; ok && !maps.Equal(existing.annotations, worker.annotations) {
					return nil, ErrDeploymentConflict
				}
				workers[worker.name] = worker
			}
			continuation = page.Metadata.Continue
			if continuation == "" {
				break
			}
		}
	}
	result := make([]ManagedWorker, 0, len(workers))
	for _, worker := range workers {
		result = append(result, worker)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].name < result[right].name })
	return result, nil
}

func (directory *CredentialDirectory) CleanupManagedWorker(ctx context.Context, endpoint, credentialRef string, worker ManagedWorker) error {
	if ctx == nil || worker.Request.validate() != nil || worker.name != workerResourceName(worker.Request) || !dnsLabelPattern.MatchString(worker.namespace) || !ownedResource(resourceMetadata{Name: worker.name, Namespace: worker.namespace, Annotations: worker.annotations}, worker.name, worker.namespace, worker.annotations) {
		return ErrDeploymentConfigInvalid
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	return cleanupResources(ctx, client, base, worker.namespace, worker.name, worker.annotations)
}

func (request DeployRequest) validate() error {
	for path, value := range map[string]string{"/tenantId": request.TenantID, "/projectId": request.ProjectID, "/targetId": request.TargetID, "/leaseId": request.LeaseID, "/providerCredentialRef": request.ProviderCredentialRef} {
		if commonv1alpha1.ValidateIdentifier(value, path) != nil {
			return ErrDeploymentConfigInvalid
		}
	}
	if !validDNSSubdomain(request.ProviderCredentialRef) || request.TargetGeneration < 1 || request.LeaseGeneration < 1 || request.CPULimitMillis < 100 || request.CPULimitMillis > 64_000 || request.MemoryLimitBytes < 128<<20 || request.MemoryLimitBytes > 1<<40 || len(request.ReleaseDigest) != 71 || !strings.HasPrefix(request.ReleaseDigest, "sha256:") {
		return ErrDeploymentConfigInvalid
	}
	for _, character := range request.ReleaseDigest[7:] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return ErrDeploymentConfigInvalid
		}
	}
	return nil
}

func (directory *CredentialDirectory) readDeploymentConfig(credentialRef string) (deploymentConfig, error) {
	if directory == nil || commonv1alpha1.ValidateIdentifier(credentialRef, "/credentialRef") != nil {
		return deploymentConfig{}, ErrDeploymentConfigInvalid
	}
	root, err := os.OpenRoot(directory.path)
	if err != nil {
		return deploymentConfig{}, ErrDeploymentConfigUnavailable
	}
	defer root.Close()
	value, err := readCredential(root, credentialRef+".deployment.json")
	if err != nil {
		return deploymentConfig{}, ErrDeploymentConfigUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var config deploymentConfig
	if decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validDeploymentConfig(config) {
		return deploymentConfig{}, ErrDeploymentConfigInvalid
	}
	return config, nil
}

func validDeploymentConfig(config deploymentConfig) bool {
	identity, err := url.Parse(config.WorkerSPIFFEID)
	return dnsLabelPattern.MatchString(config.Namespace) && imageRepositoryPattern.MatchString(config.WorkerImageRepository) && validDNSSubdomain(config.WorkerCredentialSecretRef) &&
		err == nil && identity.Scheme == "spiffe" && identity.Host != "" && identity.Path != "" && identity.User == nil && identity.RawQuery == "" && identity.Fragment == "" &&
		config.WorkerServerName != "" && len(config.WorkerServerName) <= 253 && strings.TrimSpace(config.WorkerServerName) == config.WorkerServerName && !strings.ContainsAny(config.WorkerServerName, "/@") && strings.IndexFunc(config.WorkerServerName, unicode.IsControl) < 0
}

func workerResourceName(request DeployRequest) string {
	digest := fnv.New64a()
	for _, value := range []string{request.TenantID, request.ProjectID, request.LeaseID} {
		_, _ = digest.Write([]byte(value))
		_, _ = digest.Write([]byte{0})
	}
	return "cloud-agents-" + strconv.FormatUint(digest.Sum64(), 16)
}

func deploymentAnnotations(request DeployRequest, config deploymentConfig) map[string]string {
	return map[string]string{
		"cloud-agents.dev/managed":                      "true",
		"cloud-agents.dev/tenant":                       request.TenantID,
		"cloud-agents.dev/project":                      request.ProjectID,
		"cloud-agents.dev/target":                       request.TargetID,
		"cloud-agents.dev/target-generation":            strconv.FormatInt(request.TargetGeneration, 10),
		"cloud-agents.dev/lease":                        request.LeaseID,
		"cloud-agents.dev/lease-generation":             strconv.FormatInt(request.LeaseGeneration, 10),
		"cloud-agents.dev/release-digest":               request.ReleaseDigest,
		"cloud-agents.dev/provider-credential-ref":      request.ProviderCredentialRef,
		"cloud-agents.dev/cpu-limit-millis":             strconv.FormatInt(request.CPULimitMillis, 10),
		"cloud-agents.dev/memory-limit-bytes":           strconv.FormatInt(request.MemoryLimitBytes, 10),
		"cloud-agents.dev/worker-credential-secret-ref": config.WorkerCredentialSecretRef,
		"cloud-agents.dev/worker-spiffe-id":             config.WorkerSPIFFEID,
		"cloud-agents.dev/worker-server-name":           config.WorkerServerName,
	}
}

func managedWorker(metadata resourceMetadata, expectedTargetGeneration int64, config deploymentConfig) (ManagedWorker, error) {
	annotations := metadata.Annotations
	targetGeneration, targetErr := strconv.ParseInt(annotations["cloud-agents.dev/target-generation"], 10, 64)
	leaseGeneration, leaseErr := strconv.ParseInt(annotations["cloud-agents.dev/lease-generation"], 10, 64)
	cpuLimitMillis, cpuErr := strconv.ParseInt(annotations["cloud-agents.dev/cpu-limit-millis"], 10, 64)
	memoryLimitBytes, memoryErr := strconv.ParseInt(annotations["cloud-agents.dev/memory-limit-bytes"], 10, 64)
	request := DeployRequest{
		TenantID: annotations["cloud-agents.dev/tenant"], ProjectID: annotations["cloud-agents.dev/project"], TargetID: annotations["cloud-agents.dev/target"], LeaseID: annotations["cloud-agents.dev/lease"],
		TargetGeneration: targetGeneration, LeaseGeneration: leaseGeneration, ReleaseDigest: annotations["cloud-agents.dev/release-digest"], ProviderCredentialRef: annotations["cloud-agents.dev/provider-credential-ref"],
		CPULimitMillis: cpuLimitMillis, MemoryLimitBytes: memoryLimitBytes,
	}
	if targetErr != nil || leaseErr != nil || cpuErr != nil || memoryErr != nil || targetGeneration > expectedTargetGeneration || request.validate() != nil || metadata.Namespace != config.Namespace ||
		annotations["cloud-agents.dev/worker-credential-secret-ref"] != config.WorkerCredentialSecretRef || annotations["cloud-agents.dev/worker-spiffe-id"] != config.WorkerSPIFFEID || annotations["cloud-agents.dev/worker-server-name"] != config.WorkerServerName || metadata.Name != workerResourceName(request) {
		return ManagedWorker{}, ErrDeploymentConflict
	}
	return ManagedWorker{Request: request, name: metadata.Name, namespace: metadata.Namespace, annotations: deploymentAnnotations(request, config)}, nil
}

func desiredResources(name string, request DeployRequest, config deploymentConfig, annotations map[string]string) []desiredResource {
	return desiredResourcesWithStrategy(name, request, config, annotations, false)
}

func desiredResourcesWithStrategy(name string, request DeployRequest, config deploymentConfig, annotations map[string]string, rolling bool) []desiredResource {
	metadata := func() map[string]any {
		return map[string]any{"name": name, "namespace": config.Namespace, "labels": map[string]string{"cloud-agents.dev/managed": "true", "cloud-agents.dev/worker": name}, "annotations": annotations}
	}
	selector := map[string]string{"cloud-agents.dev/worker": name}
	base := "/api/v1/namespaces/" + url.PathEscape(config.Namespace)
	strategy := any(map[string]any{"type": "Recreate"})
	if rolling {
		strategy = map[string]any{"type": "RollingUpdate", "rollingUpdate": map[string]string{"maxUnavailable": "0", "maxSurge": "1"}}
	}
	return []desiredResource{
		{path: base + "/persistentvolumeclaims/" + url.PathEscape(name), body: map[string]any{
			"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": metadata(),
			"spec": map[string]any{"accessModes": []string{"ReadWriteOnce"}, "resources": map[string]any{"requests": map[string]string{"storage": workspaceStorage}}},
		}},
		{path: base + "/services/" + url.PathEscape(name), body: map[string]any{
			"apiVersion": "v1", "kind": "Service", "metadata": metadata(),
			"spec": map[string]any{"type": "LoadBalancer", "selector": selector, "ports": []map[string]any{{"name": "https", "port": workerPort, "protocol": "TCP", "targetPort": workerPort}}},
		}},
		{path: "/apis/apps/v1/namespaces/" + url.PathEscape(config.Namespace) + "/deployments/" + url.PathEscape(name), body: map[string]any{
			"apiVersion": "apps/v1", "kind": "Deployment", "metadata": metadata(),
			"spec": map[string]any{"replicas": 1, "strategy": strategy, "selector": map[string]any{"matchLabels": selector}, "template": map[string]any{
				"metadata": map[string]any{"labels": selector},
				"spec": map[string]any{"automountServiceAccountToken": false, "terminationGracePeriodSeconds": 30, "securityContext": map[string]any{"runAsNonRoot": true, "runAsUser": 1000, "runAsGroup": 1000, "fsGroup": 1000, "fsGroupChangePolicy": "OnRootMismatch", "seccompProfile": map[string]string{"type": "RuntimeDefault"}}, "containers": []map[string]any{{
					"name": "worker", "image": config.WorkerImageRepository + "@" + request.ReleaseDigest, "imagePullPolicy": "IfNotPresent",
					"args":            []string{"--listen", ":8091", "--tls-cert", "/run/cloud-agents/worker-credentials/server.crt", "--tls-key", "/run/cloud-agents/worker-credentials/server.key", "--client-ca", "/run/cloud-agents/worker-credentials/client-ca.crt", "--worker-spiffe-id", config.WorkerSPIFFEID, "--runtime-command", "/usr/local/bin/cloud-agent-runtime", "--runtime-directory", "/workspace", "--runtime-max-sessions", "1", "--provider-credential-directory", "/run/cloud-agents/provider-credentials", "--admission-lease-id", request.LeaseID, "--admission-generation", strconv.FormatInt(request.LeaseGeneration, 10), "--admission-token-file", "/run/cloud-agents/worker-credentials/admission-token"},
					"env":             []map[string]string{{"name": "CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS", "value": "codex,claudeAgent"}, {"name": "CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE", "value": "single-tenant-trusted-v1"}},
					"ports":           []map[string]any{{"containerPort": workerPort, "name": "https", "protocol": "TCP"}},
					"resources":       map[string]any{"limits": map[string]string{"cpu": fmt.Sprintf("%dm", request.CPULimitMillis), "memory": strconv.FormatInt(request.MemoryLimitBytes, 10)}, "requests": map[string]string{"cpu": fmt.Sprintf("%dm", request.CPULimitMillis), "memory": strconv.FormatInt(request.MemoryLimitBytes, 10)}},
					"securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}},
					"volumeMounts":    []map[string]any{{"name": "worker-credentials", "mountPath": "/run/cloud-agents/worker-credentials", "readOnly": true}, {"name": "provider-credentials", "mountPath": "/run/cloud-agents/provider-credentials", "readOnly": true}, {"name": "workspace", "mountPath": "/workspace"}, {"name": "tmp", "mountPath": "/tmp"}},
				}}, "volumes": []map[string]any{{"name": "worker-credentials", "secret": map[string]any{"secretName": config.WorkerCredentialSecretRef, "defaultMode": 0o400}}, {"name": "provider-credentials", "secret": map[string]any{"secretName": request.ProviderCredentialRef, "defaultMode": 0o400}}, {"name": "workspace", "persistentVolumeClaim": map[string]string{"claimName": name}}, {"name": "tmp", "emptyDir": map[string]any{"medium": "Memory", "sizeLimit": "64Mi"}}}},
			}},
		}},
	}
}

func requireSecret(ctx context.Context, client *http.Client, base, namespace, name string) error {
	var value resource
	status, err := kubernetesJSON(ctx, client, http.MethodGet, base+"/api/v1/namespaces/"+url.PathEscape(namespace)+"/secrets/"+url.PathEscape(name), "", nil, &value)
	if err != nil || status != http.StatusOK || value.Metadata.Name != name || value.Metadata.Namespace != namespace {
		if status == http.StatusNotFound {
			return ErrDeploymentConfigUnavailable
		}
		return ErrDeploymentFailed
	}
	return nil
}

func applyResource(ctx context.Context, client *http.Client, base string, desired desiredResource) error {
	status, err := kubernetesJSON(ctx, client, http.MethodPatch, base+desired.path+"?fieldManager=cloud-agents-control-plane&fieldValidation=Strict", "application/apply-patch+yaml", desired.body, nil)
	if status == http.StatusConflict {
		return ErrDeploymentConflict
	}
	if err != nil || status != http.StatusOK && status != http.StatusCreated {
		return ErrDeploymentFailed
	}
	return nil
}

func waitForReadyResources(ctx context.Context, client *http.Client, base, namespace, name string, annotations map[string]string) (string, error) {
	return waitForReadyResourcesMode(ctx, client, base, namespace, name, annotations, false)
}

func waitForReadyResourcesMode(ctx context.Context, client *http.Client, base, namespace, name string, annotations map[string]string, rolling bool) (string, error) {
	waitContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deploymentPath := base + "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/deployments/" + url.PathEscape(name)
	servicePath := base + "/api/v1/namespaces/" + url.PathEscape(namespace) + "/services/" + url.PathEscape(name)
	for {
		var deployment, service resource
		deploymentStatus, deploymentErr := kubernetesJSON(waitContext, client, http.MethodGet, deploymentPath, "", nil, &deployment)
		serviceStatus, serviceErr := kubernetesJSON(waitContext, client, http.MethodGet, servicePath, "", nil, &service)
		if deploymentStatus == http.StatusOK && serviceStatus == http.StatusOK {
			if !ownedResource(deployment.Metadata, name, namespace, annotations) || !ownedResource(service.Metadata, name, namespace, annotations) {
				return "", ErrDeploymentConflict
			}
			if deployment.Status.AvailableReplicas >= 1 && deployment.Status.ObservedGeneration >= deployment.Metadata.Generation && (!rolling || deployment.Status.UpdatedReplicas >= 1) {
				if address := loadBalancerAddress(service); address != "" {
					return "https://" + net.JoinHostPort(address, strconv.Itoa(workerPort)), nil
				}
			}
		} else if deploymentErr == nil || serviceErr == nil {
			return "", ErrDeploymentFailed
		}
		select {
		case <-waitContext.Done():
			return "", ErrDeploymentFailed
		case <-ticker.C:
		}
	}
}

func loadBalancerAddress(service resource) string {
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" && net.ParseIP(ingress.IP) != nil {
			return ingress.IP
		}
		if validDNSSubdomain(ingress.Hostname) {
			return ingress.Hostname
		}
	}
	return ""
}

func validDNSSubdomain(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !dnsLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func cleanupResources(ctx context.Context, client *http.Client, base, namespace, name string, annotations map[string]string) error {
	paths := []string{
		"/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/deployments/" + url.PathEscape(name),
		"/api/v1/namespaces/" + url.PathEscape(namespace) + "/services/" + url.PathEscape(name),
		"/api/v1/namespaces/" + url.PathEscape(namespace) + "/persistentvolumeclaims/" + url.PathEscape(name),
	}
	for _, path := range paths {
		if err := deleteOwnedResource(ctx, client, base+path, namespace, name, annotations); err != nil {
			return err
		}
	}
	return nil
}

func deleteOwnedResource(ctx context.Context, client *http.Client, target, namespace, name string, annotations map[string]string) error {
	var current resource
	status, err := kubernetesJSON(ctx, client, http.MethodGet, target, "", nil, &current)
	if status == http.StatusNotFound {
		return nil
	}
	if err != nil || status != http.StatusOK {
		return ErrDeploymentFailed
	}
	if !ownedResource(current.Metadata, name, namespace, annotations) || current.Metadata.UID == "" || current.Metadata.ResourceVersion == "" {
		return ErrDeploymentConflict
	}
	deleteOptions := map[string]any{"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Foreground", "preconditions": map[string]string{"uid": current.Metadata.UID, "resourceVersion": current.Metadata.ResourceVersion}}
	status, err = kubernetesJSON(ctx, client, http.MethodDelete, target, "application/json", deleteOptions, nil)
	if err != nil || status != http.StatusOK && status != http.StatusAccepted && status != http.StatusNotFound {
		return ErrDeploymentFailed
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for status != http.StatusNotFound {
		select {
		case <-ctx.Done():
			return ErrDeploymentFailed
		case <-ticker.C:
			status, err = kubernetesJSON(ctx, client, http.MethodGet, target, "", nil, &resource{})
			if err != nil && status != http.StatusNotFound {
				return ErrDeploymentFailed
			}
		}
	}
	return nil
}

func ownedResource(metadata resourceMetadata, name, namespace string, annotations map[string]string) bool {
	if metadata.Name != name || metadata.Namespace != namespace {
		return false
	}
	for key, value := range annotations {
		if metadata.Annotations[key] != value {
			return false
		}
	}
	return true
}

func kubernetesJSON(ctx context.Context, client *http.Client, method, target, contentType string, body, output any) (int, error) {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, ErrDeploymentFailed
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, ErrDeploymentFailed
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, ErrDeploymentFailed
	}
	defer response.Body.Close()
	if output == nil || response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusConflict {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxKubernetesBodyBytes))
		return response.StatusCode, nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxKubernetesBodyBytes+1))
	if decoder.Decode(output) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return response.StatusCode, ErrDeploymentFailed
	}
	return response.StatusCode, nil
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
