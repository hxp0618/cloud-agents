package server

import (
	"context"
	"errors"

	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/sshtarget"
)

// environmentActuator owns the target-specific worker calls used by lease
// deployment. Authorization, persistence and lease state transitions remain in
// ManagedHostEnvironmentLeaseHTTPServer.
type environmentActuator struct {
	dockerCredentials     *dockertarget.CredentialDirectory
	kubernetesCredentials *kubernetestarget.CredentialDirectory
	sshCredentials        *sshtarget.CredentialDirectory
	workerTrust           dockertarget.WorkerTrust
	kubernetesWorkerTrust kubernetestarget.WorkerTrust
}

func newEnvironmentActuator(dockerCredentials *dockertarget.CredentialDirectory, kubernetesCredentials *kubernetestarget.CredentialDirectory, sshCredentials *sshtarget.CredentialDirectory, workerTrust dockertarget.WorkerTrust) environmentActuator {
	return environmentActuator{
		dockerCredentials:     dockerCredentials,
		kubernetesCredentials: kubernetesCredentials,
		sshCredentials:        sshCredentials,
		workerTrust:           workerTrust,
		kubernetesWorkerTrust: kubernetestarget.WorkerTrust{ClientCertificate: workerTrust.ClientCertificate, RootCAs: workerTrust.RootCAs},
	}
}

// completeEnvironmentDeployment runs the target adapter and maps its result
// into the store's stable completion shape. upgrade selects the rolling
// deployment operation used by lease upgrades.
func (actuator environmentActuator) completeEnvironmentDeployment(ctx context.Context, tenantID, projectID string, snapshot internalmanagedhost.Snapshot, target internaldeploymenttarget.Snapshot, upgrade bool) internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput {
	completion := internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput{
		Scope: snapshot.Scope, LeaseID: snapshot.LeaseID, TargetID: snapshot.TargetID,
		ExpectedGeneration: snapshot.Generation, ExpectedTargetGeneration: snapshot.TargetGeneration,
	}
	if target.Generation != snapshot.TargetGeneration || target.ObservedPhase != "ready" {
		completion.StableErrorCode = target.Kind + "-target-not-ready"
		if !supportedEnvironmentTargetKind(target.Kind) {
			completion.StableErrorCode = "target-kind-unsupported"
		}
		return completion
	}
	switch target.Kind {
	case "docker":
		if actuator.dockerCredentials == nil {
			completion.StableErrorCode = "docker-actuator-unconfigured"
			return completion
		}
		request := dockerDeployRequest(tenantID, projectID, snapshot, snapshot.Generation)
		var deployed dockertarget.DeployResult
		var err error
		if upgrade {
			deployed, err = actuator.dockerCredentials.DeployWorkerUpgrade(ctx, target.Endpoint, target.CredentialRef, request, actuator.workerTrust)
		} else {
			deployed, err = actuator.dockerCredentials.DeployWorker(ctx, target.Endpoint, target.CredentialRef, request, actuator.workerTrust)
		}
		if err != nil {
			completion.StableErrorCode = dockerDeploymentErrorCode(err)
			return completion
		}
		completion.Succeeded, completion.WorkerEndpoint, completion.WorkerSPIFFEID, completion.WorkerServerName = true, deployed.Endpoint, deployed.WorkerSPIFFEID, deployed.WorkerServerName
	case "kubernetes":
		if actuator.kubernetesCredentials == nil {
			completion.StableErrorCode = "kubernetes-actuator-unconfigured"
			return completion
		}
		request := kubernetesDeployRequest(tenantID, projectID, snapshot, snapshot.Generation)
		var deployed kubernetestarget.DeployResult
		var err error
		if upgrade {
			deployed, err = actuator.kubernetesCredentials.DeployWorkerUpgrade(ctx, target.Endpoint, target.CredentialRef, request, actuator.kubernetesWorkerTrust)
		} else {
			deployed, err = actuator.kubernetesCredentials.DeployWorker(ctx, target.Endpoint, target.CredentialRef, request, actuator.kubernetesWorkerTrust)
		}
		if err != nil {
			completion.StableErrorCode = kubernetesDeploymentErrorCode(err)
			return completion
		}
		completion.Succeeded, completion.WorkerEndpoint, completion.WorkerSPIFFEID, completion.WorkerServerName = true, deployed.Endpoint, deployed.WorkerSPIFFEID, deployed.WorkerServerName
	case "ssh":
		if actuator.sshCredentials == nil {
			completion.StableErrorCode = "ssh-actuator-unconfigured"
			return completion
		}
		request := dockerDeployRequest(tenantID, projectID, snapshot, snapshot.Generation)
		var deployed sshtarget.DeployResult
		var err error
		if upgrade {
			deployed, err = actuator.sshCredentials.DeployWorkerUpgrade(ctx, target.Endpoint, target.CredentialRef, request, actuator.workerTrust)
		} else {
			deployed, err = actuator.sshCredentials.DeployWorker(ctx, target.Endpoint, target.CredentialRef, request, actuator.workerTrust)
		}
		if err != nil {
			completion.StableErrorCode = sshDeploymentErrorCode(err)
			return completion
		}
		completion.Succeeded, completion.WorkerEndpoint, completion.WorkerSPIFFEID, completion.WorkerServerName = true, deployed.Endpoint, deployed.WorkerSPIFFEID, deployed.WorkerServerName
	default:
		completion.StableErrorCode = "target-kind-unsupported"
	}
	return completion
}

func (actuator environmentActuator) cleanupOlderEnvironmentWorkers(ctx context.Context, tenantID, projectID string, target internaldeploymenttarget.Snapshot, snapshot internalmanagedhost.Snapshot) error {
	switch target.Kind {
	case "docker":
		return actuator.dockerCredentials.CleanupOlderWorkers(ctx, target.Endpoint, target.CredentialRef, dockerDeployRequest(tenantID, projectID, snapshot, snapshot.Generation))
	case "ssh":
		return actuator.sshCredentials.CleanupOlderWorkers(ctx, target.Endpoint, target.CredentialRef, dockerDeployRequest(tenantID, projectID, snapshot, snapshot.Generation))
	default:
		return nil
	}
}

func supportedEnvironmentTargetKind(kind string) bool {
	return kind == "docker" || kind == "kubernetes" || kind == "ssh"
}

func dockerDeployRequest(tenantID, projectID string, snapshot internalmanagedhost.Snapshot, leaseGeneration int64) dockertarget.DeployRequest {
	return dockertarget.DeployRequest{
		TenantID: tenantID, ProjectID: projectID, TargetID: snapshot.TargetID, LeaseID: snapshot.LeaseID,
		TargetGeneration: snapshot.TargetGeneration, LeaseGeneration: leaseGeneration,
		ReleaseDigest: snapshot.ReleaseDigest, ProviderCredentialRef: snapshot.ProviderCredentialRef,
		CPULimitMillis: snapshot.CPULimitMillis, MemoryLimitBytes: snapshot.MemoryLimitBytes,
	}
}

func kubernetesDeployRequest(tenantID, projectID string, snapshot internalmanagedhost.Snapshot, leaseGeneration int64) kubernetestarget.DeployRequest {
	return kubernetestarget.DeployRequest{
		TenantID: tenantID, ProjectID: projectID, TargetID: snapshot.TargetID, LeaseID: snapshot.LeaseID,
		TargetGeneration: snapshot.TargetGeneration, LeaseGeneration: leaseGeneration,
		ReleaseDigest: snapshot.ReleaseDigest, ProviderCredentialRef: snapshot.ProviderCredentialRef,
		CPULimitMillis: snapshot.CPULimitMillis, MemoryLimitBytes: snapshot.MemoryLimitBytes,
	}
}

func dockerDeploymentErrorCode(err error) string {
	switch {
	case errors.Is(err, dockertarget.ErrDeploymentConfigUnavailable):
		return "docker-deployment-config-unavailable"
	case errors.Is(err, dockertarget.ErrDeploymentConfigInvalid):
		return "docker-deployment-config-invalid"
	case errors.Is(err, dockertarget.ErrDeploymentConflict):
		return "docker-deployment-conflict"
	case errors.Is(err, dockertarget.ErrWorkerUnavailable):
		return "docker-worker-unavailable"
	default:
		return "docker-deployment-failed"
	}
}

func kubernetesDeploymentErrorCode(err error) string {
	switch {
	case errors.Is(err, kubernetestarget.ErrDeploymentConfigUnavailable):
		return "kubernetes-deployment-config-unavailable"
	case errors.Is(err, kubernetestarget.ErrDeploymentConfigInvalid):
		return "kubernetes-deployment-config-invalid"
	case errors.Is(err, kubernetestarget.ErrDeploymentConflict):
		return "kubernetes-deployment-conflict"
	case errors.Is(err, kubernetestarget.ErrWorkerUnavailable):
		return "kubernetes-worker-unavailable"
	default:
		return "kubernetes-deployment-failed"
	}
}

func sshDeploymentErrorCode(err error) string {
	switch {
	case errors.Is(err, sshtarget.ErrHostKeyMismatch):
		return "ssh-host-key-mismatch"
	case errors.Is(err, sshtarget.ErrCredentialUnavailable), errors.Is(err, sshtarget.ErrCredentialInvalid):
		return "ssh-credential-unavailable"
	case errors.Is(err, sshtarget.ErrUnavailable):
		return "ssh-target-unavailable"
	case errors.Is(err, sshtarget.ErrDeploymentConfigUnavailable):
		return "ssh-deployment-config-unavailable"
	case errors.Is(err, sshtarget.ErrDeploymentConfigInvalid):
		return "ssh-deployment-config-invalid"
	case errors.Is(err, sshtarget.ErrDeploymentConflict):
		return "ssh-deployment-conflict"
	case errors.Is(err, sshtarget.ErrWorkerUnavailable):
		return "ssh-worker-unavailable"
	default:
		return "ssh-deployment-failed"
	}
}
