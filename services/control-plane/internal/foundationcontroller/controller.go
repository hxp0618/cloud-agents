package foundationcontroller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const (
	claimLease    = 60 * time.Second
	renewInterval = 20 * time.Second
	effectTimeout = 2 * time.Minute
)

type Controller struct {
	store       *postgres.DurableCoordinationService
	docker      *dockertarget.CredentialDirectory
	opensandbox *opensandbox.CredentialDirectory
	holder      string
	incarnation string
}

type effectResult struct {
	runtimeID, runtimeState, volumeName string
	cleanupComplete                     bool
	err                                 error
}

func New(store *postgres.DurableCoordinationService, docker *dockertarget.CredentialDirectory, sandbox *opensandbox.CredentialDirectory) (*Controller, error) {
	if store == nil || docker == nil || sandbox == nil {
		return nil, errors.New("foundation controller configuration is invalid")
	}
	return &Controller{store: store, docker: docker, opensandbox: sandbox,
		holder: "foundation-controller", incarnation: randomIdentifier("inc")}, nil
}

func (controller *Controller) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		worked, err := controller.RunOne(ctx)
		if err != nil && ctx.Err() == nil && logger != nil {
			logger.Warn("foundation reconciliation deferred", "error", stableControllerError(err))
		}
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (controller *Controller) RunOne(ctx context.Context) (bool, error) {
	if controller == nil || controller.store == nil {
		return false, errors.New("foundation controller is unavailable")
	}
	subject := digest("foundation-controller")
	reaped, err := controller.store.ReapFoundationSandbox(ctx, subject, randomIdentifier("audit"))
	if err != nil {
		return false, err
	}
	if reaped.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("foundation reaper outcome is unknown")
	}
	if reaped.Found {
		return true, nil
	}
	claimResult, err := controller.store.ClaimFoundationSandbox(ctx, postgres.FoundationSandboxClaimInput{
		HolderID: controller.holder, HolderIncarnation: controller.incarnation,
		ClaimToken: randomIdentifier("claim"), LeaseSeconds: int32(claimLease / time.Second),
		SubjectDigest: subject, AuditFactID: randomIdentifier("audit"),
	})
	if err != nil {
		return false, err
	}
	if claimResult.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("foundation claim outcome is unknown")
	}
	if !claimResult.Found {
		return false, nil
	}
	claim := claimResult.Claim
	result, err := controller.executeWithRenewal(ctx, &claim)
	if err != nil {
		return true, err
	}
	transition, code := classify(result.err, claim.DeliveryAttempts)
	settled, err := controller.store.SettleFoundationSandbox(ctx, postgres.FoundationSandboxSettlement{
		Claim: claim, Transition: transition, RuntimeID: result.runtimeID, RuntimeState: result.runtimeState,
		VolumeName: result.volumeName, StableErrorCode: code, CleanupComplete: result.cleanupComplete,
		SubjectDigest: subject, AuditFactID: randomIdentifier("audit"),
	})
	if err != nil {
		return true, err
	}
	if settled.DatabaseOutcome != postgres.DatabaseCommitted {
		return true, errors.New("foundation settlement outcome is unknown")
	}
	return true, nil
}

func (controller *Controller) executeWithRenewal(ctx context.Context, claim *postgres.FoundationSandboxClaim) (effectResult, error) {
	effectCtx, cancel := context.WithTimeout(ctx, effectTimeout)
	defer cancel()
	results := make(chan effectResult, 1)
	go func() { results <- controller.execute(effectCtx, *claim) }()
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()
	for {
		select {
		case result := <-results:
			if ctx.Err() != nil {
				return effectResult{}, ctx.Err()
			}
			return result, nil
		case <-ticker.C:
			expiry, err := controller.store.RenewFoundationSandbox(ctx, *claim, int32(claimLease/time.Second))
			if err != nil {
				cancel()
				return effectResult{}, err
			}
			claim.ClaimExpiresAt = expiry
		case <-ctx.Done():
			return effectResult{}, ctx.Err()
		}
	}
}

func (controller *Controller) execute(ctx context.Context, claim postgres.FoundationSandboxClaim) effectResult {
	volume := dockertarget.FoundationWorkspaceVolume{TenantID: claim.TenantID, ProjectID: claim.ProjectID,
		TargetID: claim.TargetID, WorkspaceID: claim.WorkspaceID}
	if claim.Action == "sandbox.stop" {
		volumeName, err := controller.docker.VerifyFoundationWorkspaceVolume(ctx, claim.TargetEndpoint, claim.CredentialRef, volume)
		if err != nil || claim.PhysicalVolumeName == nil || volumeName != *claim.PhysicalVolumeName {
			if err == nil {
				err = dockertarget.ErrDeploymentConflict
			}
			return effectResult{err: err}
		}
		client, err := controller.opensandbox.Client(claim.CredentialRef)
		if err != nil {
			return effectResult{volumeName: volumeName, err: err}
		}
		identity := opensandbox.Identity{Tenant: claim.TenantID, Project: claim.ProjectID,
			Workspace: claim.WorkspaceID, Sandbox: claim.SandboxID, Operation: *claim.RuntimeOperationID,
			Generation: *claim.RuntimeGeneration, SpecDigest: *claim.RuntimeSpecDigest}
		if err := client.Delete(ctx, identity, *claim.RuntimeID); err != nil {
			return effectResult{runtimeID: *claim.RuntimeID, runtimeState: claim.RuntimeState,
				volumeName: volumeName, err: err}
		}
		return effectResult{volumeName: volumeName, cleanupComplete: true}
	}

	volumeName, err := controller.docker.EnsureFoundationWorkspaceVolume(ctx, claim.TargetEndpoint, claim.CredentialRef, volume)
	if err != nil {
		return effectResult{err: err}
	}
	if claim.PhysicalVolumeName != nil && volumeName != *claim.PhysicalVolumeName {
		return effectResult{err: dockertarget.ErrDeploymentConflict}
	}
	client, err := controller.opensandbox.Client(claim.CredentialRef)
	if err != nil {
		return effectResult{volumeName: volumeName, err: err}
	}
	identity := opensandbox.Identity{Tenant: claim.TenantID, Project: claim.ProjectID,
		Workspace: claim.WorkspaceID, Sandbox: claim.SandboxID, Operation: claim.OperationID,
		Generation: claim.SandboxGeneration, SpecDigest: claim.SpecDigest}
	observation, err := client.Create(ctx, opensandbox.CreateInput{Identity: identity, ImageURI: claim.ImageURI,
		VolumeName: volumeName, CPUMillis: claim.CPUMillis, MemoryBytes: claim.MemoryBytes})
	if err == nil {
		ready, waitErr := client.WaitReady(ctx, identity, observation.RuntimeID)
		if ready.RuntimeID != "" {
			observation.RuntimeID = ready.RuntimeID
		}
		if ready.RuntimeState != "" {
			observation.RuntimeState = ready.RuntimeState
		}
		err = waitErr
	}
	result := effectResult{runtimeID: observation.RuntimeID, runtimeState: observation.RuntimeState,
		volumeName: volumeName, err: err}
	if errors.Is(err, opensandbox.ErrRuntimeFailed) && observation.RuntimeID != "" {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if cleanupErr := client.Delete(cleanupCtx, identity, observation.RuntimeID); cleanupErr == nil {
			result.cleanupComplete = true
		} else {
			result.err = cleanupErr
		}
	}
	return result
}

func classify(err error, attempt int32) (string, string) {
	if err == nil {
		return "succeeded", ""
	}
	terminal, code := false, "foundation_runtime_unavailable"
	switch {
	case errors.Is(err, opensandbox.ErrRuntimeFailed):
		terminal, code = true, "opensandbox_runtime_failed"
	case errors.Is(err, opensandbox.ErrConflict), errors.Is(err, dockertarget.ErrDeploymentConflict):
		terminal, code = true, "foundation_ownership_conflict"
	case errors.Is(err, opensandbox.ErrInvalid), errors.Is(err, dockertarget.ErrDeploymentConfigInvalid),
		errors.Is(err, dockertarget.ErrCredentialInvalid), errors.Is(err, dockertarget.ErrInvalidEndpoint):
		terminal, code = true, "foundation_configuration_invalid"
	}
	if terminal || attempt >= 8 {
		return "failed", code
	}
	return "retry", code
}

func randomIdentifier(prefix string) string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic("secure randomness unavailable")
	}
	return prefix + "-" + hex.EncodeToString(value)
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stableControllerError(err error) string {
	_, code := classify(err, 1)
	return code
}
