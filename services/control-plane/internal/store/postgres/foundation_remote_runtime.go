package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
)

func (service *DurableCoordinationService) OpenFoundationRemoteRuntime(ctx context.Context, principalSource internalmanagedagent.VerifiedPrincipalSource, session internalmanagedagent.RuntimeSessionSnapshot, executionID string) (internalmanagedagent.FoundationRemoteRuntimeHandle, error) {
	if service == nil || service.runner == nil || ctx == nil || principalSource == nil || session.FoundationTargetKind != "remote-worker" || executionID == "" {
		return internalmanagedagent.FoundationRemoteRuntimeHandle{}, internalmanagedagent.ErrRuntimeEnvironmentUnavailable
	}
	seed, err := foundationRemoteSeed()
	if err != nil {
		return internalmanagedagent.FoundationRemoteRuntimeHandle{}, internalmanagedagent.ErrRuntimeEnvironmentUnavailable
	}
	grantID, tokenDigest := "runtime-"+seed, foundationRemoteDigest("token", seed)
	issueID := "issue-" + seed
	principal, err := foundationRemotePrincipal(principalSource)
	if err != nil {
		return internalmanagedagent.FoundationRemoteRuntimeHandle{}, err
	}
	grant, err := service.IssueSandboxAccessGrant(ctx, session.Scope.TenantID, principal, SandboxAccessGrantIssueInput{
		Scope:     internalcoordination.FoundationScope{TenantID: session.Scope.TenantID, ProjectID: session.Scope.ProjectID},
		SandboxID: session.SandboxID, GrantID: grantID, TokenDigest: tokenDigest,
		ExpectedGeneration: int64(session.SandboxGeneration), TTLSeconds: 900,
		RequestDigest: foundationRemoteDigest("issue", grantID, session.SandboxID, fmt.Sprint(session.SandboxGeneration)),
		Mutation:      internalcoordination.FoundationMutation{RequestID: issueID, IdempotencyKey: issueID},
	})
	if err != nil {
		return internalmanagedagent.FoundationRemoteRuntimeHandle{}, err
	}
	handle := internalmanagedagent.FoundationRemoteRuntimeHandle{Scope: session.Scope, GrantID: grantID,
		TokenDigest: tokenDigest, SandboxID: grant.SandboxID, Generation: grant.Generation,
		ResourceVersion: grant.ResourceVersion}
	gateway := &AccessGatewayStore{runner: service.runner}
	authority, err := gateway.ResolveGrant(ctx, session.Scope.TenantID, session.Scope.ProjectID, grantID, tokenDigest)
	if err != nil {
		_ = service.revokeFoundationRemoteGrant(context.WithoutCancel(ctx), principalSource, "projects.act", handle)
		return internalmanagedagent.FoundationRemoteRuntimeHandle{}, err
	}
	commandID := "create-" + seed
	receipt, err := gateway.ExecuteRemoteWorkerSandboxPTY(ctx, RemoteWorkerSandboxPTYRequest{
		Authority: authority, CommandID: commandID, RequestID: commandID,
		TokenDigest: tokenDigest, Action: "create",
	})
	if err != nil || receipt.Result != "succeeded" || receipt.SessionID == "" {
		_ = service.revokeFoundationRemoteGrant(context.WithoutCancel(ctx), principalSource, "projects.act", handle)
		if err != nil {
			return internalmanagedagent.FoundationRemoteRuntimeHandle{}, err
		}
		return internalmanagedagent.FoundationRemoteRuntimeHandle{}, internalmanagedagent.ErrRuntimeEnvironmentUnavailable
	}
	handle.SessionID = receipt.SessionID
	if _, err = gateway.PersistPTYSession(ctx, session.Scope.TenantID, session.Scope.ProjectID, grantID, receipt.SessionID, tokenDigest); err != nil {
		_, _ = gateway.ExecuteRemoteWorkerSandboxPTY(context.WithoutCancel(ctx), RemoteWorkerSandboxPTYRequest{
			Authority: authority, CommandID: "delete-" + seed, RequestID: "delete-" + seed,
			TokenDigest: tokenDigest, Action: "delete", SessionID: receipt.SessionID,
		})
		_ = service.revokeFoundationRemoteGrant(context.WithoutCancel(ctx), principalSource, "projects.act", handle)
		return internalmanagedagent.FoundationRemoteRuntimeHandle{}, err
	}
	return handle, nil
}

func (service *DurableCoordinationService) ExchangeFoundationRemoteRuntime(ctx context.Context, handle internalmanagedagent.FoundationRemoteRuntimeHandle, sequence uint64, since int64, input []byte) (internalmanagedagent.FoundationRemoteRuntimeExchange, error) {
	if service == nil || service.runner == nil || ctx == nil || sequence == 0 || since < 0 || len(input) > 64<<10 {
		return internalmanagedagent.FoundationRemoteRuntimeExchange{}, internalmanagedagent.ErrRuntimeEnvironmentUnavailable
	}
	gateway := &AccessGatewayStore{runner: service.runner}
	session, err := gateway.ResolvePTYSession(ctx, handle.Scope.TenantID, handle.Scope.ProjectID, handle.GrantID, handle.SessionID, handle.TokenDigest)
	if err != nil {
		return internalmanagedagent.FoundationRemoteRuntimeExchange{}, err
	}
	commandID := foundationRemoteID("exchange", handle.GrantID, fmt.Sprint(sequence))
	pty := false
	request := RemoteWorkerSandboxPTYRequest{Authority: session.Grant, CommandID: commandID, RequestID: commandID,
		TokenDigest: handle.TokenDigest, Action: "exchange", SessionID: handle.SessionID,
		Since: since, Takeover: true, PTY: &pty}
	if input != nil {
		request.Input = &platform.RemoteWorkerSandboxPTYFrame{MessageType: "binary", PayloadBase64URL: base64.RawURLEncoding.EncodeToString(append([]byte{0}, input...))}
	}
	receipt, err := gateway.ExecuteRemoteWorkerSandboxPTY(ctx, request)
	if err != nil || receipt.Result != "succeeded" || receipt.Frames == nil || receipt.OutputOffset == nil || receipt.Running == nil {
		if err != nil {
			return internalmanagedagent.FoundationRemoteRuntimeExchange{}, err
		}
		return internalmanagedagent.FoundationRemoteRuntimeExchange{}, internalmanagedagent.ErrRuntimeEnvironmentUnavailable
	}
	result := internalmanagedagent.FoundationRemoteRuntimeExchange{OutputOffset: *receipt.OutputOffset, Running: *receipt.Running,
		Frames: make([]internalmanagedagent.FoundationRemoteRuntimeFrame, 0, len(*receipt.Frames))}
	for _, frame := range *receipt.Frames {
		payload, decodeErr := base64.RawURLEncoding.Strict().DecodeString(frame.PayloadBase64URL)
		if decodeErr != nil {
			return internalmanagedagent.FoundationRemoteRuntimeExchange{}, internalmanagedagent.ErrRuntimeEnvironmentUnavailable
		}
		result.Frames = append(result.Frames, internalmanagedagent.FoundationRemoteRuntimeFrame{MessageType: frame.MessageType, Payload: payload})
	}
	return result, nil
}

func (service *DurableCoordinationService) CloseFoundationRemoteRuntime(ctx context.Context, principalSource internalmanagedagent.VerifiedPrincipalSource, handle internalmanagedagent.FoundationRemoteRuntimeHandle) error {
	if service == nil || service.runner == nil || ctx == nil || principalSource == nil {
		return internalmanagedagent.ErrRuntimeEnvironmentUnavailable
	}
	gateway := &AccessGatewayStore{runner: service.runner}
	var result error
	session, err := gateway.ResolvePTYSession(ctx, handle.Scope.TenantID, handle.Scope.ProjectID, handle.GrantID, handle.SessionID, handle.TokenDigest)
	if err == nil {
		commandID := foundationRemoteID("delete", handle.GrantID, handle.SessionID)
		receipt, deleteErr := gateway.ExecuteRemoteWorkerSandboxPTY(ctx, RemoteWorkerSandboxPTYRequest{
			Authority: session.Grant, CommandID: commandID, RequestID: commandID,
			TokenDigest: handle.TokenDigest, Action: "delete", SessionID: handle.SessionID,
		})
		if deleteErr == nil && receipt.Result != "succeeded" {
			deleteErr = internalmanagedagent.ErrRuntimeEnvironmentUnavailable
		}
		if deleteErr == nil {
			deleteErr = gateway.MarkPTYSessionDeleted(ctx, handle.Scope.TenantID, handle.Scope.ProjectID, handle.GrantID, handle.SessionID, handle.TokenDigest)
		}
		result = deleteErr
	} else {
		result = err
	}
	if revokeErr := service.revokeFoundationRemoteGrant(ctx, principalSource, "projects.act", handle); result == nil {
		result = revokeErr
	}
	return result
}

func (service *DurableCoordinationService) ReadFoundationRemoteArtifact(ctx context.Context, principalSource internalmanagedagent.VerifiedPrincipalSource, session internalmanagedagent.RuntimeSessionSnapshot, artifactPath string) ([]byte, error) {
	if service == nil || service.runner == nil || ctx == nil || principalSource == nil || session.FoundationTargetKind != "remote-worker" {
		return nil, internalmanagedagent.ErrRuntimeArtifactUnavailable
	}
	seed, err := foundationRemoteSeed()
	if err != nil {
		return nil, internalmanagedagent.ErrRuntimeArtifactUnavailable
	}
	grantID, tokenDigest, issueID := "artifact-"+seed, foundationRemoteDigest("token", seed), "issue-"+seed
	principal, err := foundationRemotePrincipal(principalSource)
	if err != nil {
		return nil, err
	}
	grant, err := service.issueSandboxAccessGrant(ctx, session.Scope.TenantID, principal, "projects.get", SandboxAccessGrantIssueInput{
		Scope:     internalcoordination.FoundationScope{TenantID: session.Scope.TenantID, ProjectID: session.Scope.ProjectID},
		SandboxID: session.SandboxID, GrantID: grantID, TokenDigest: tokenDigest,
		ExpectedGeneration: int64(session.SandboxGeneration), TTLSeconds: 900,
		RequestDigest: foundationRemoteDigest("artifact", grantID, session.SandboxID, artifactPath),
		Mutation:      internalcoordination.FoundationMutation{RequestID: issueID, IdempotencyKey: issueID},
	})
	if err != nil {
		return nil, err
	}
	cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cleanupCancel()
	defer service.revokeFoundationRemoteGrant(cleanupContext, principalSource, "projects.get", internalmanagedagent.FoundationRemoteRuntimeHandle{
		Scope: session.Scope, GrantID: grantID, SandboxID: grant.SandboxID, Generation: grant.Generation,
		ResourceVersion: grant.ResourceVersion,
	}) //nolint:errcheck
	gateway := &AccessGatewayStore{runner: service.runner}
	authority, err := gateway.ResolveGrant(ctx, session.Scope.TenantID, session.Scope.ProjectID, grantID, tokenDigest)
	if err != nil {
		return nil, err
	}
	var data []byte
	var version string
	total := int64(-1)
	for offset, page := int64(0), uint64(1); total < 0 || offset < total; page++ {
		eventID := foundationRemoteID("file", grantID, fmt.Sprint(page))
		if err = gateway.StartFileAccess(ctx, session.Scope.TenantID, session.Scope.ProjectID, grantID, eventID, "read", tokenDigest, eventID); err != nil {
			return nil, err
		}
		receipt, readErr := gateway.ExecuteRemoteWorkerSandboxFile(ctx, RemoteWorkerSandboxFileRequest{
			Authority: authority, EventID: eventID, RequestID: eventID, TokenDigest: tokenDigest,
			Action: "read", Path: artifactPath, Offset: offset, Limit: 1 << 20, FileVersion: version,
		})
		stableCode, bytesTransferred := "", int64(0)
		if readErr == nil && receipt.Result != "succeeded" {
			stableCode, readErr = receipt.StableErrorCode, internalmanagedagent.ErrRuntimeArtifactUnavailable
		}
		if readErr == nil && receipt.Read == nil {
			readErr = internalmanagedagent.ErrRuntimeArtifactUnavailable
		}
		if readErr == nil {
			var content []byte
			content, readErr = base64.RawURLEncoding.Strict().DecodeString(receipt.Read.ContentBase64URL)
			if readErr == nil {
				bytesTransferred = int64(len(content))
				if total < 0 {
					version, total = receipt.Read.FileVersion, receipt.Read.TotalBytes
				}
				if receipt.Read.FileVersion != version || receipt.Read.Offset != offset || receipt.Read.TotalBytes != total || len(content) == 0 && offset < total {
					readErr = internalmanagedagent.ErrRuntimeArtifactUnavailable
				} else {
					data = append(data, content...)
					offset += bytesTransferred
				}
			}
		}
		outcome := "succeeded"
		if readErr != nil {
			outcome = "failed"
			if stableCode == "" {
				stableCode = "sandbox_access_unavailable"
			}
		}
		if finishErr := gateway.CompleteFileAccess(ctx, session.Scope.TenantID, session.Scope.ProjectID, grantID, eventID, tokenDigest, outcome, stableCode, bytesTransferred); finishErr != nil {
			return nil, finishErr
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return data, nil
}

func (service *DurableCoordinationService) revokeFoundationRemoteGrant(ctx context.Context, principalSource internalmanagedagent.VerifiedPrincipalSource, permission string, handle internalmanagedagent.FoundationRemoteRuntimeHandle) error {
	principal, err := foundationRemotePrincipal(principalSource)
	if err != nil {
		return err
	}
	requestID := foundationRemoteID("revoke", handle.GrantID)
	_, err = service.revokeSandboxAccessGrant(ctx, handle.Scope.TenantID, principal, permission, SandboxAccessGrantRevokeInput{
		Scope:     internalcoordination.FoundationScope{TenantID: handle.Scope.TenantID, ProjectID: handle.Scope.ProjectID},
		SandboxID: handle.SandboxID, GrantID: handle.GrantID, ConfirmedGrantID: handle.GrantID,
		ExpectedGeneration: handle.Generation, ExpectedResourceVersion: handle.ResourceVersion,
		RequestDigest: foundationRemoteDigest("revoke", handle.GrantID, fmt.Sprint(handle.ResourceVersion)),
		Mutation:      internalcoordination.FoundationMutation{RequestID: requestID, IdempotencyKey: requestID},
	})
	return err
}

func foundationRemotePrincipal(source internalmanagedagent.VerifiedPrincipalSource) (*authn.VerifiedPrincipal, error) {
	if source == nil {
		return nil, internalmanagedagent.ErrRuntimeEnvironmentUnavailable
	}
	principal, err := source()
	if err != nil || principal == nil {
		return nil, internalmanagedagent.ErrRuntimeEnvironmentUnavailable
	}
	return principal, nil
}

func foundationRemoteSeed() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func foundationRemoteDigest(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func foundationRemoteID(prefix string, parts ...string) string {
	digest := foundationRemoteDigest(parts...)
	return prefix + "-" + digest[len("sha256:"):len("sha256:")+32]
}

var _ internalmanagedagent.FoundationRemoteRuntimeStore = (*DurableCoordinationService)(nil)
