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
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
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
	kubernetes  *kubernetestarget.CredentialDirectory
	opensandbox *opensandbox.CredentialDirectory
	targetKinds []string
	nextTarget  int
	holder      string
	incarnation string
}

type EffectResult struct {
	RuntimeID, RuntimeState, VolumeName, ContentDigest string
	SizeBytes                                          int64
	CleanupComplete                                    bool
	Err                                                error
}

func New(store *postgres.DurableCoordinationService, docker *dockertarget.CredentialDirectory, kubernetes *kubernetestarget.CredentialDirectory, sandbox *opensandbox.CredentialDirectory) (*Controller, error) {
	if store == nil || sandbox == nil || docker == nil && kubernetes == nil {
		return nil, errors.New("foundation controller configuration is invalid")
	}
	targetKinds := make([]string, 0, 2)
	if docker != nil {
		targetKinds = append(targetKinds, "docker")
	}
	if kubernetes != nil {
		targetKinds = append(targetKinds, "kubernetes")
	}
	return &Controller{store: store, docker: docker, kubernetes: kubernetes, opensandbox: sandbox, targetKinds: targetKinds,
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
	checkpoint, err := controller.store.CheckpointFoundationSandboxUsage(ctx, 200)
	if err != nil {
		return false, err
	}
	if checkpoint.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("foundation usage checkpoint outcome is unknown")
	}
	subject := digest("foundation-controller")
	if controller.docker != nil {
		worked, err := controller.runWorkspaceSnapshotOne(ctx, subject)
		if worked || err != nil {
			return worked, err
		}
	}
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
	expired, err := controller.store.ExpireFoundationSandbox(ctx, subject, randomIdentifier("audit"))
	if err != nil {
		return false, err
	}
	if expired.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("foundation expiry outcome is unknown")
	}
	if expired.Found {
		return true, nil
	}
	var claimResult postgres.FoundationSandboxClaimResult
	for offset := range controller.targetKinds {
		index := (controller.nextTarget + offset) % len(controller.targetKinds)
		claimResult, err = controller.store.ClaimFoundationSandbox(ctx, postgres.FoundationSandboxClaimInput{
			TargetKind: controller.targetKinds[index],
			HolderID:   controller.holder, HolderIncarnation: controller.incarnation,
			ClaimToken: randomIdentifier("claim"), LeaseSeconds: int32(claimLease / time.Second),
			SubjectDigest: subject, AuditFactID: randomIdentifier("audit"),
		})
		if err != nil {
			return false, err
		}
		if claimResult.DatabaseOutcome != postgres.DatabaseCommitted {
			return false, errors.New("foundation claim outcome is unknown")
		}
		if claimResult.Found {
			controller.nextTarget = (index + 1) % len(controller.targetKinds)
			break
		}
	}
	if !claimResult.Found {
		return false, nil
	}
	claim := claimResult.Claim
	result, err := controller.executeWithRenewal(ctx, &claim)
	if err != nil {
		return true, err
	}
	transition, code := classify(result.Err, claim.DeliveryAttempts)
	settled, err := controller.store.SettleFoundationSandbox(ctx, postgres.FoundationSandboxSettlement{
		Claim: claim, Transition: transition, RuntimeID: result.RuntimeID, RuntimeState: result.RuntimeState,
		VolumeName: result.VolumeName, StableErrorCode: code, CleanupComplete: result.CleanupComplete,
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

func (controller *Controller) runWorkspaceSnapshotOne(ctx context.Context, subject string) (bool, error) {
	cleanupReaped, err := controller.store.ReapWorkspaceSnapshotCleanup(ctx, subject, randomIdentifier("audit"))
	if err != nil {
		return false, err
	}
	if cleanupReaped.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("workspace snapshot cleanup reaper outcome is unknown")
	}
	if cleanupReaped.Found {
		return true, nil
	}
	expired, err := controller.store.ExpireWorkspaceSnapshot(ctx, subject)
	if err != nil {
		return false, err
	}
	if expired.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("workspace snapshot expiry outcome is unknown")
	}
	if expired.Found {
		return true, nil
	}
	cleanupClaim, err := controller.store.ClaimWorkspaceSnapshotCleanup(ctx, controller.holder, controller.incarnation,
		randomIdentifier("claim"), int32(claimLease/time.Second), subject, randomIdentifier("audit"))
	if err != nil {
		return false, err
	}
	if cleanupClaim.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("workspace snapshot cleanup claim outcome is unknown")
	}
	if cleanupClaim.Found {
		claim := cleanupClaim.Claim
		result, effectErr := controller.executeSnapshotCleanupWithRenewal(ctx, &claim)
		if effectErr != nil {
			return true, effectErr
		}
		transition, code := classify(result.Err, claim.DeliveryAttempts)
		settled, settleErr := controller.store.SettleWorkspaceSnapshotCleanup(ctx, postgres.WorkspaceSnapshotCleanupSettlement{
			Claim: claim, Transition: transition, StableErrorCode: code,
			SubjectDigest: subject, AuditFactID: randomIdentifier("audit"),
		})
		if settleErr != nil {
			return true, settleErr
		}
		if settled.DatabaseOutcome != postgres.DatabaseCommitted {
			return true, errors.New("workspace snapshot cleanup settlement outcome is unknown")
		}
		return true, nil
	}
	reaped, err := controller.store.ReapWorkspaceSnapshot(ctx, subject, randomIdentifier("audit"))
	if err != nil {
		return false, err
	}
	if reaped.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("workspace snapshot reaper outcome is unknown")
	}
	if reaped.Found {
		return true, nil
	}
	claimResult, err := controller.store.ClaimWorkspaceSnapshot(ctx, controller.holder, controller.incarnation,
		randomIdentifier("claim"), int32(claimLease/time.Second), subject, randomIdentifier("audit"))
	if err != nil {
		return false, err
	}
	if claimResult.DatabaseOutcome != postgres.DatabaseCommitted {
		return false, errors.New("workspace snapshot claim outcome is unknown")
	}
	if !claimResult.Found {
		return false, nil
	}
	claim := claimResult.Claim
	result, err := controller.executeSnapshotWithRenewal(ctx, &claim)
	if err != nil {
		return true, err
	}
	transition, code := classify(result.Err, claim.DeliveryAttempts)
	settled, err := controller.store.SettleWorkspaceSnapshot(ctx, postgres.WorkspaceSnapshotSettlement{
		Claim: claim, Transition: transition, SnapshotVolume: result.VolumeName,
		ContentDigest: result.ContentDigest, SizeBytes: result.SizeBytes, StableErrorCode: code,
		CleanupComplete: result.CleanupComplete, SubjectDigest: subject, AuditFactID: randomIdentifier("audit"),
	})
	if err != nil {
		return true, err
	}
	if settled.DatabaseOutcome != postgres.DatabaseCommitted {
		return true, errors.New("workspace snapshot settlement outcome is unknown")
	}
	return true, nil
}

func (controller *Controller) executeSnapshotCleanupWithRenewal(ctx context.Context, claim *postgres.WorkspaceSnapshotCleanupClaim) (EffectResult, error) {
	effectCtx, cancel := context.WithTimeout(ctx, effectTimeout)
	defer cancel()
	results := make(chan EffectResult, 1)
	go func() {
		err := controller.docker.CleanupFoundationWorkspaceSnapshot(effectCtx, claim.TargetEndpoint, claim.CredentialRef,
			dockertarget.FoundationWorkspaceSnapshotCleanup{TenantID: claim.TenantID, ProjectID: claim.ProjectID,
				TargetID: claim.TargetID, SourceWorkspaceID: claim.SourceWorkspaceID,
				SnapshotID: claim.SnapshotID, PhysicalSnapshotID: claim.PhysicalSnapshotID})
		results <- EffectResult{CleanupComplete: err == nil, Err: err}
	}()
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()
	for {
		select {
		case result := <-results:
			if ctx.Err() != nil {
				return EffectResult{}, ctx.Err()
			}
			return result, nil
		case <-ticker.C:
			expiry, err := controller.store.RenewWorkspaceSnapshotCleanup(ctx, *claim, int32(claimLease/time.Second))
			if err != nil {
				cancel()
				return EffectResult{}, err
			}
			claim.ClaimExpiresAt = expiry
		case <-ctx.Done():
			return EffectResult{}, ctx.Err()
		}
	}
}

func (controller *Controller) executeSnapshotWithRenewal(ctx context.Context, claim *postgres.WorkspaceSnapshotClaim) (EffectResult, error) {
	effectCtx, cancel := context.WithTimeout(ctx, effectTimeout)
	defer cancel()
	results := make(chan EffectResult, 1)
	go func() {
		result, err := controller.docker.SnapshotFoundationWorkspace(effectCtx, claim.TargetEndpoint, claim.CredentialRef,
			dockertarget.FoundationWorkspaceSnapshot{TenantID: claim.TenantID, ProjectID: claim.ProjectID,
				TargetID: claim.TargetID, WorkspaceID: claim.WorkspaceID, SnapshotID: claim.SnapshotID,
				SourceVolumeName: claim.SourceVolumeName, ImageURI: claim.ImageURI})
		results <- EffectResult{VolumeName: result.VolumeName, ContentDigest: result.ContentDigest,
			SizeBytes: result.SizeBytes, CleanupComplete: result.CleanupComplete, Err: err}
	}()
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()
	for {
		select {
		case result := <-results:
			if ctx.Err() != nil {
				return EffectResult{}, ctx.Err()
			}
			return result, nil
		case <-ticker.C:
			expiry, err := controller.store.RenewWorkspaceSnapshot(ctx, *claim, int32(claimLease/time.Second))
			if err != nil {
				cancel()
				return EffectResult{}, err
			}
			claim.ClaimExpiresAt = expiry
		case <-ctx.Done():
			return EffectResult{}, ctx.Err()
		}
	}
}

func (controller *Controller) executeWithRenewal(ctx context.Context, claim *postgres.FoundationSandboxClaim) (EffectResult, error) {
	effectCtx, cancel := context.WithTimeout(ctx, effectTimeout)
	defer cancel()
	results := make(chan EffectResult, 1)
	go func() {
		results <- ExecuteEffect(effectCtx, controller.docker, controller.kubernetes, controller.opensandbox, *claim)
	}()
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()
	for {
		select {
		case result := <-results:
			if ctx.Err() != nil {
				return EffectResult{}, ctx.Err()
			}
			return result, nil
		case <-ticker.C:
			expiry, err := controller.store.RenewFoundationSandbox(ctx, *claim, int32(claimLease/time.Second))
			if err != nil {
				cancel()
				return EffectResult{}, err
			}
			claim.ClaimExpiresAt = expiry
		case <-ctx.Done():
			return EffectResult{}, ctx.Err()
		}
	}
}

func ExecuteEffect(ctx context.Context, docker *dockertarget.CredentialDirectory, kubernetes *kubernetestarget.CredentialDirectory, sandbox *opensandbox.CredentialDirectory, claim postgres.FoundationSandboxClaim) EffectResult {
	if ctx == nil || sandbox == nil || (claim.TargetKind != "kubernetes" && docker == nil) || (claim.TargetKind == "kubernetes" && kubernetes == nil) {
		return EffectResult{Err: errors.New("foundation executor configuration is invalid")}
	}
	if claim.Action == "sandbox.stop" {
		volumeName, err := foundationWorkspaceVolume(ctx, docker, kubernetes, claim, false)
		if err != nil || claim.PhysicalVolumeName == nil || volumeName != *claim.PhysicalVolumeName {
			if err == nil {
				err = dockertarget.ErrDeploymentConflict
			}
			return EffectResult{Err: err}
		}
		client, err := sandbox.Client(claim.CredentialRef)
		if err != nil {
			return EffectResult{VolumeName: volumeName, Err: err}
		}
		identity := opensandbox.Identity{Tenant: claim.TenantID, Project: claim.ProjectID,
			Workspace: claim.WorkspaceID, Sandbox: claim.SandboxID, Operation: *claim.RuntimeOperationID,
			Generation: *claim.RuntimeGeneration, SpecDigest: *claim.RuntimeSpecDigest}
		if err := client.Delete(ctx, identity, *claim.RuntimeID); err != nil {
			return EffectResult{RuntimeID: *claim.RuntimeID, RuntimeState: claim.RuntimeState,
				VolumeName: volumeName, Err: err}
		}
		return EffectResult{VolumeName: volumeName, CleanupComplete: true}
	}

	client, err := sandbox.Client(claim.CredentialRef)
	if err != nil {
		return EffectResult{Err: err}
	}
	identity := opensandbox.Identity{Tenant: claim.TenantID, Project: claim.ProjectID,
		Workspace: claim.WorkspaceID, Sandbox: claim.SandboxID, Operation: claim.OperationID,
		Generation: claim.SandboxGeneration, SpecDigest: claim.SpecDigest}
	createVolume := true
	if claim.RestoreSnapshotID != nil {
		if _, findErr := client.Find(ctx, identity); findErr == nil {
			createVolume = false
		} else if !errors.Is(findErr, opensandbox.ErrNotFound) {
			return EffectResult{Err: findErr}
		} else {
			restored, restoreErr := docker.RestoreFoundationWorkspace(ctx, claim.TargetEndpoint, claim.CredentialRef,
				dockertarget.FoundationWorkspaceRestore{TenantID: claim.TenantID, ProjectID: claim.ProjectID,
					TargetID: claim.TargetID, SourceWorkspaceID: *claim.RestoreSourceWorkspaceID,
					SnapshotID: *claim.RestoreSnapshotID, SnapshotVolumeName: *claim.RestoreSnapshotVolume,
					ContentDigest: *claim.RestoreContentDigest, WorkspaceID: claim.WorkspaceID, ImageURI: claim.ImageURI})
			if restoreErr != nil {
				return EffectResult{VolumeName: restored.VolumeName, CleanupComplete: restored.CleanupComplete, Err: restoreErr}
			}
		}
	}
	volumeName, err := foundationWorkspaceVolume(ctx, docker, kubernetes, claim, createVolume)
	if err != nil {
		return EffectResult{Err: err}
	}
	if claim.PhysicalVolumeName != nil && volumeName != *claim.PhysicalVolumeName {
		return EffectResult{Err: dockertarget.ErrDeploymentConflict}
	}
	observation, err := client.Create(ctx, opensandbox.CreateInput{Identity: identity, ImageURI: claim.ImageURI,
		VolumeName: volumeName, CPUMillis: claim.CPUMillis, MemoryBytes: claim.MemoryBytes,
		NetworkPolicy: foundationNetworkPolicy(claim)})
	if err == nil {
		ready, waitErr := client.WaitReady(ctx, identity, observation.RuntimeID)
		if ready.RuntimeID != "" {
			observation.RuntimeID = ready.RuntimeID
		}
		if ready.RuntimeState != "" {
			observation.RuntimeState = ready.RuntimeState
		}
		err = waitErr
		if err == nil && claim.IsolationRuntime == "gvisor" {
			err = docker.VerifyFoundationSandboxIsolation(ctx, claim.TargetEndpoint, claim.CredentialRef, observation.RuntimeID)
		} else if err == nil && claim.NetworkPolicyID != "" {
			err = client.VerifyNetworkPolicy(ctx, identity, observation.RuntimeID, foundationNetworkPolicy(claim))
		}
	}
	result := EffectResult{RuntimeID: observation.RuntimeID, RuntimeState: observation.RuntimeState,
		VolumeName: volumeName, Err: err}
	if (errors.Is(err, opensandbox.ErrRuntimeFailed) || errors.Is(err, opensandbox.ErrPolicyUnenforced) || errors.Is(err, dockertarget.ErrIsolationUnenforced)) && observation.RuntimeID != "" {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if cleanupErr := client.Delete(cleanupCtx, identity, observation.RuntimeID); cleanupErr == nil {
			result.CleanupComplete = true
		} else {
			result.Err = cleanupErr
		}
	}
	return result
}

func foundationWorkspaceVolume(ctx context.Context, docker *dockertarget.CredentialDirectory, kubernetes *kubernetestarget.CredentialDirectory, claim postgres.FoundationSandboxClaim, create bool) (string, error) {
	if claim.TargetKind == "kubernetes" {
		volume := kubernetestarget.FoundationWorkspaceVolume{TenantID: claim.TenantID, ProjectID: claim.ProjectID,
			TargetID: claim.TargetID, WorkspaceID: claim.WorkspaceID}
		if create {
			return kubernetes.EnsureFoundationWorkspaceVolume(ctx, claim.TargetEndpoint, claim.CredentialRef, volume)
		}
		return kubernetes.VerifyFoundationWorkspaceVolume(ctx, claim.TargetEndpoint, claim.CredentialRef, volume)
	}
	volume := dockertarget.FoundationWorkspaceVolume{TenantID: claim.TenantID, ProjectID: claim.ProjectID,
		TargetID: claim.TargetID, WorkspaceID: claim.WorkspaceID}
	if create {
		return docker.EnsureFoundationWorkspaceVolume(ctx, claim.TargetEndpoint, claim.CredentialRef, volume)
	}
	return docker.VerifyFoundationWorkspaceVolume(ctx, claim.TargetEndpoint, claim.CredentialRef, volume)
}

func foundationNetworkPolicy(claim postgres.FoundationSandboxClaim) *opensandbox.NetworkPolicy {
	if claim.NetworkPolicyID == "" || claim.IsolationRuntime == "gvisor" {
		return nil
	}
	policy := &opensandbox.NetworkPolicy{DefaultAction: "deny", Egress: make([]opensandbox.NetworkRule, len(claim.NetworkAllowedEgress))}
	for index, target := range claim.NetworkAllowedEgress {
		policy.Egress[index] = opensandbox.NetworkRule{Action: "allow", Target: target}
	}
	return policy
}

func classify(err error, attempt int32) (string, string) {
	if err == nil {
		return "succeeded", ""
	}
	terminal, code := false, "foundation_runtime_unavailable"
	switch {
	case errors.Is(err, opensandbox.ErrRuntimeFailed):
		terminal, code = true, "opensandbox_runtime_failed"
	case errors.Is(err, opensandbox.ErrPolicyUnenforced):
		terminal, code = true, "foundation_network_policy_unenforced"
	case errors.Is(err, dockertarget.ErrIsolationUnenforced):
		terminal, code = true, "foundation_isolation_unenforced"
	case errors.Is(err, dockertarget.ErrSnapshotTooLarge):
		terminal, code = true, "workspace_snapshot_too_large"
	case errors.Is(err, opensandbox.ErrConflict), errors.Is(err, dockertarget.ErrDeploymentConflict), errors.Is(err, kubernetestarget.ErrDeploymentConflict):
		terminal, code = true, "foundation_ownership_conflict"
	case errors.Is(err, opensandbox.ErrInvalid), errors.Is(err, dockertarget.ErrDeploymentConfigInvalid),
		errors.Is(err, dockertarget.ErrCredentialInvalid), errors.Is(err, dockertarget.ErrInvalidEndpoint),
		errors.Is(err, kubernetestarget.ErrDeploymentConfigInvalid), errors.Is(err, kubernetestarget.ErrCredentialInvalid),
		errors.Is(err, kubernetestarget.ErrInvalidEndpoint):
		terminal, code = true, "foundation_configuration_invalid"
	}
	if terminal || attempt >= 8 {
		return "failed", code
	}
	return "retry", code
}

func ClassifyEffect(err error, attempt int32) (string, string) {
	return classify(err, attempt)
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
