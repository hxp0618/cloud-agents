package managedagent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type durableRuntimeExecutionStoreFake struct {
	principalMu        sync.Mutex
	calls              []string
	principals         []*authn.VerifiedPrincipal
	turn               *TurnSnapshot
	execution          ExecutionSnapshot
	capabilityBindings []*workerruntimev1alpha1.RuntimeCapabilityBinding
	capabilityError    error
	checkpointError    error
	completeError      error
	settlementStarted  chan string
	settlementRelease  <-chan struct{}
	renewed            chan struct{}
	renewStarted       chan context.Context
	renewBlock         <-chan struct{}
	renewError         error
	capabilityEvents   []CapabilityEventInput
	cancel             context.CancelFunc
	blockedCall        string
	blockedCallStarted chan string
	blockedCallDone    chan error
}

func (fake *durableRuntimeExecutionStoreFake) block(ctx context.Context, call string) error {
	if fake.blockedCall != call {
		return nil
	}
	if fake.blockedCallStarted != nil {
		fake.blockedCallStarted <- call
	}
	<-ctx.Done()
	err := ctx.Err()
	if fake.blockedCallDone != nil {
		fake.blockedCallDone <- err
	}
	return err
}

func (fake *durableRuntimeExecutionStoreFake) GetManagedAgentSessionForExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, _ string) (RuntimeSessionSnapshot, error) {
	fake.calls = append(fake.calls, "session")
	fake.recordPrincipal(principal)
	return RuntimeSessionSnapshot{SessionSnapshot: SessionSnapshot{ProviderKind: "codex"}}, nil
}

func (fake *durableRuntimeExecutionStoreFake) ResolveManagedAgentCapabilityBindings(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, _ string, _ string, mcpRefs []McpServerRef, skillRefs []SkillBundleRef) ([]*workerruntimev1alpha1.RuntimeCapabilityBinding, string, error) {
	fake.calls = append(fake.calls, "capabilities")
	fake.recordPrincipal(principal)
	if fake.capabilityError != nil {
		return nil, "", fake.capabilityError
	}
	bindings := make([]*workerruntimev1alpha1.RuntimeCapabilityBinding, 0, len(mcpRefs)+len(skillRefs))
	for _, ref := range mcpRefs {
		bindings = append(bindings, &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "mcp-server", ResourceId: ref.ServerID, Version: ref.Version, Digest: ref.Digest})
	}
	for _, ref := range skillRefs {
		bindings = append(bindings, &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "skill-bundle", ResourceId: ref.BundleID, Version: ref.Version, Digest: ref.Digest, ReadOnly: true})
	}
	fake.capabilityBindings = bindings
	return bindings, "sha256:test", nil
}

func (fake *durableRuntimeExecutionStoreFake) RecordManagedAgentCapabilityEvent(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input CapabilityEventInput) error {
	fake.calls = append(fake.calls, "capability-event")
	fake.recordPrincipal(principal)
	fake.capabilityEvents = append(fake.capabilityEvents, input)
	return nil
}

func (fake *durableRuntimeExecutionStoreFake) GetManagedAgentSessionForArtifact(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, sessionID string) (RuntimeSessionSnapshot, error) {
	return fake.GetManagedAgentSessionForExecution(ctx, tenantID, principal, projectID, sessionID)
}

func (fake *durableRuntimeExecutionStoreFake) FindManagedAgentTurnForExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, _ string, _ string) (TurnSnapshot, bool, error) {
	fake.calls = append(fake.calls, "find-turn")
	fake.recordPrincipal(principal)
	if fake.turn == nil {
		return TurnSnapshot{}, false, nil
	}
	return *fake.turn, true, nil
}

func (fake *durableRuntimeExecutionStoreFake) CreateManagedAgentTurn(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input CreateTurnInput) (TurnSnapshot, error) {
	fake.calls = append(fake.calls, "turn")
	fake.recordPrincipal(principal)
	return TurnSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, State: TurnQueued}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CreateManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input CreateExecutionInput) (ExecutionSnapshot, error) {
	fake.calls = append(fake.calls, "execution")
	fake.recordPrincipal(principal)
	fake.execution.Scope, fake.execution.SessionID, fake.execution.TurnID, fake.execution.ExecutionID, fake.execution.Generation = input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation
	fake.execution.McpServerRefs = append([]McpServerRef(nil), input.McpServerRefs...)
	fake.execution.SkillBundleRefs = append([]SkillBundleRef(nil), input.SkillBundleRefs...)
	if fake.execution.State == "" {
		fake.execution.State = ExecutionSucceeded
	}
	return fake.execution, nil
}

func (fake *durableRuntimeExecutionStoreFake) ClaimManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input ClaimRuntimeExecutionInput) (RuntimeExecutionClaim, error) {
	fake.calls = append(fake.calls, "claim")
	fake.recordPrincipal(principal)
	attempt := fake.execution.AttemptNumber + 1
	claim := RuntimeExecutionClaim{RuntimeExecutionReference: input.RuntimeExecutionReference, Acquired: true, AttemptNumber: attempt,
		HolderID: input.HolderID, Incarnation: input.Incarnation, Token: input.Token, ExpiresAt: time.Now().Add(time.Minute),
		PendingSideEffect: fake.execution.PendingSideEffect}
	if fake.execution.State == ExecutionRunning {
		claim.RecoveryState = "recovering"
		claim.RecoveryReason = "claim_expired"
		claim.RecoveryMode = fake.execution.RecoveryMode
		claim.RecoverySourceTargetID = fake.execution.RecoverySourceTargetID
		claim.RecoveryTargetID = fake.execution.RecoveryTargetID
	}
	if fake.execution.State == ExecutionRunning && (fake.execution.PendingSideEffect || len(fake.execution.Messages) == 0) {
		claim.Acquired = false
		claim.RecoveryState = "awaiting_reconciliation"
		claim.RecoveryReason = "checkpoint_missing"
		if fake.execution.PendingSideEffect {
			claim.RecoveryReason = "side_effect_outcome_unknown"
		}
	}
	if claim.Acquired {
		fake.execution.AttemptNumber = attempt
	}
	return claim, nil
}

func (fake *durableRuntimeExecutionStoreFake) RenewManagedAgentExecutionClaim(ctx context.Context, _ string, principal *authn.VerifiedPrincipal, claim RuntimeExecutionClaim, _ int32) (time.Time, error) {
	fake.recordPrincipal(principal)
	if fake.renewStarted != nil {
		select {
		case fake.renewStarted <- ctx:
		default:
		}
	}
	if fake.renewed != nil {
		select {
		case fake.renewed <- struct{}{}:
		default:
		}
	}
	if fake.renewBlock != nil {
		select {
		case <-fake.renewBlock:
		case <-ctx.Done():
			return time.Time{}, ctx.Err()
		}
	}
	return claim.ExpiresAt.Add(time.Minute), fake.renewError
}

func (fake *durableRuntimeExecutionStoreFake) ReleaseQueuedManagedAgentExecutionClaim(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ RuntimeExecutionClaim) error {
	fake.calls = append(fake.calls, "release-claim")
	fake.recordPrincipal(principal)
	return nil
}

func (fake *durableRuntimeExecutionStoreFake) CheckpointManagedAgentExecution(ctx context.Context, _ string, principal *authn.VerifiedPrincipal, input CheckpointRuntimeExecutionInput) (RuntimeExecutionCheckpoint, error) {
	fake.calls = append(fake.calls, "checkpoint")
	fake.recordPrincipal(principal)
	if err := fake.block(ctx, "checkpoint"); err != nil {
		return RuntimeExecutionCheckpoint{}, err
	}
	if fake.checkpointError != nil {
		return RuntimeExecutionCheckpoint{}, fake.checkpointError
	}
	fake.execution.PendingSideEffect = input.PendingSideEffect
	fake.execution.PendingInteractionCount = input.PendingInteractions
	fake.execution.Messages = append([]runtimeprotocol.Message(nil), input.Messages...)
	digest, err := RuntimeMessagesDigest(input.Messages, input.Claim.ExecutionID, input.Claim.Generation)
	return RuntimeExecutionCheckpoint{Sequence: 1, Digest: digest, ExpiresAt: input.Claim.ExpiresAt.Add(time.Minute), CreatedAt: time.Now()}, err
}

func (fake *durableRuntimeExecutionStoreFake) ResolveManagedAgentExecutionInteraction(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input ResolveRuntimeInteractionInput) (RuntimeInteractionResolution, error) {
	fake.calls = append(fake.calls, "resolve-interaction")
	fake.recordPrincipal(principal)
	digest, _, err := RuntimeInteractionResolutionDigest(input)
	return RuntimeInteractionResolution{InteractionRequestID: input.InteractionRequestID, InteractionType: input.InteractionType, RequestID: input.RequestID, Digest: digest, Payload: input.Payload}, err
}

func (fake *durableRuntimeExecutionStoreFake) ReconcileManagedAgentExecutionSideEffect(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input ReconcileRuntimeSideEffectInput) (RuntimeSideEffectReconciliation, error) {
	fake.recordPrincipal(principal)
	digest, err := RuntimeSideEffectReconciliationDigest(input)
	return RuntimeSideEffectReconciliation{CheckpointDigest: input.CheckpointDigest, Outcome: input.Outcome, Digest: digest, CreatedAt: time.Now()}, err
}

func (fake *durableRuntimeExecutionStoreFake) StartManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ StartExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "start")
	fake.recordPrincipal(principal)
	if fake.cancel != nil {
		fake.cancel()
	}
	fake.execution.State = ExecutionRunning
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnRunning}, Execution: fake.execution}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CompleteManagedAgentExecution(ctx context.Context, _ string, principal *authn.VerifiedPrincipal, input CompleteRuntimeExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "complete")
	fake.recordPrincipal(principal)
	if err := fake.block(ctx, "complete"); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if fake.settlementStarted != nil {
		fake.settlementStarted <- "complete"
		<-fake.settlementRelease
	}
	if fake.completeError != nil {
		return ExecutionTransitionResult{}, fake.completeError
	}
	fake.execution.State = ExecutionSucceeded
	fake.execution.Messages = append([]runtimeprotocol.Message(nil), input.Messages...)
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnCompleted}, Execution: fake.execution}, nil
}

func (fake *durableRuntimeExecutionStoreFake) FailManagedAgentExecution(ctx context.Context, _ string, principal *authn.VerifiedPrincipal, input FailRuntimeExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "fail")
	fake.recordPrincipal(principal)
	if err := fake.block(ctx, "fail"); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if fake.settlementStarted != nil {
		fake.settlementStarted <- "fail"
		<-fake.settlementRelease
	}
	fake.execution.State = ExecutionFailed
	fake.execution.ErrorCode = input.ErrorCode
	return ExecutionTransitionResult{
		Turn:      TurnSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, State: TurnFailed},
		Execution: fake.execution,
	}, nil
}

func (fake *durableRuntimeExecutionStoreFake) InterruptManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input InterruptTurnInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "interrupt")
	fake.recordPrincipal(principal)
	fake.execution.State = ExecutionCancelled
	fake.execution.ErrorCode = "interrupted"
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnInterrupted}, Execution: fake.execution}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CancelManagedAgentExecution(ctx context.Context, _ string, principal *authn.VerifiedPrincipal, input CancelTurnInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "cancel")
	fake.recordPrincipal(principal)
	if err := fake.block(ctx, "cancel"); err != nil {
		return ExecutionTransitionResult{}, err
	}
	fake.execution.State = ExecutionCancelled
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnCancelled}, Execution: fake.execution}, nil
}

func (fake *durableRuntimeExecutionStoreFake) recordPrincipal(principal *authn.VerifiedPrincipal) {
	fake.principalMu.Lock()
	defer fake.principalMu.Unlock()
	if principal != nil {
		fake.principals = append(fake.principals, principal)
	}
}

var _ DurableRuntimeExecutionStore = (*durableRuntimeExecutionStoreFake)(nil)

func TestRuntimeCheckpointStateTracksOnlySideEffectingItems(t *testing.T) {
	event := func(eventType, itemType, itemID string) runtimeprotocol.Message {
		return runtimeprotocol.Message{MessageType: "Event", Payload: map[string]any{
			"eventType": eventType,
			"payload":   map[string]any{"itemType": itemType, "data": map[string]any{"providerItemId": itemID}},
		}}
	}
	messages := []runtimeprotocol.Message{
		event("item.started", "reasoning", "reasoning-1"),
		event("item.started", "command_execution", "command-1"),
		event("item.completed", "command_execution", "command-1"),
		event("item.started", "file_change", "file-1"),
	}
	pendingSideEffect, pendingInteractions := runtimeCheckpointState(messages, nil, 0)
	if !pendingSideEffect || pendingInteractions != 0 {
		t.Fatalf("checkpoint state = (%t, %d), want (true, 0)", pendingSideEffect, pendingInteractions)
	}
	pendingSideEffect, _ = runtimeCheckpointState(messages[:3], nil, 0)
	if pendingSideEffect {
		t.Fatal("completed side-effect item or reasoning item remained pending")
	}
}

type runtimeWorkerFake struct {
	workerv1alpha1connect.UnimplementedWorkerExecutionServiceHandler
	workerruntimev1alpha1connect.UnimplementedWorkerRuntimeServiceHandler
	identity                        *workerv1alpha1.WorkloadIdentity
	now                             time.Time
	full                            bool
	opened                          chan struct{}
	openBindings                    []*workerruntimev1alpha1.RuntimeCapabilityBinding
	openManifestDigest              string
	disconnectAfterCheckpoint       bool
	sendRuntimeErrorAfterCheckpoint bool
	disconnectCode                  connect.Code
	terminalErrorCode               string
	terminalCommandIDMismatch       bool
	capabilityResourceID            string
	skillCapabilityResourceID       string
	capabilityStartedOnly           bool
}

func (fake *runtimeWorkerFake) Negotiate(_ context.Context, request *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	version := &workerv1alpha1.ProtocolVersion{Major: 1, Minor: 0}
	capabilities := append([]workerv1alpha1.Capability(nil), request.Msg.GetRequiredCapabilities()...)
	return connect.NewResponse(&workerv1alpha1.NegotiationResponse{
		SelectedVersion: version, AcceptedCapabilities: capabilities, AuthenticatedServerIdentity: fake.identity,
		NegotiationId: "negotiation-capacity", ExpiresAt: timestamppb.New(fake.now.Add(time.Minute)),
		Server: &workerv1alpha1.ProtocolDescriptor{
			CurrentVersion: version, MinimumCompatibleVersion: version, Capabilities: capabilities,
			MaxPayloadBytes: 64 << 10, MaxDeadlineSeconds: 300, MaxWireMessageBytes: 1 << 20, MaxRepeatedItems: 64, MaxStringBytes: 1024,
		},
	}), nil
}

func (fake *runtimeWorkerFake) OpenSession(ctx context.Context, stream *connect.BidiStream[workerruntimev1alpha1.RuntimeSessionRequest, workerruntimev1alpha1.RuntimeSessionResponse]) error {
	if fake.full {
		return connect.NewError(connect.CodeResourceExhausted, errors.New("capacity_exhausted"))
	}
	request, err := stream.Receive()
	if err != nil || request.GetOpen() == nil {
		return err
	}
	open := request.GetOpen()
	fake.openBindings = append([]*workerruntimev1alpha1.RuntimeCapabilityBinding(nil), open.GetCapabilityBindings()...)
	fake.openManifestDigest = open.GetCapabilityManifestDigest()
	if fake.opened != nil {
		fake.opened <- struct{}{}
	}
	if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Ready{Ready: &workerruntimev1alpha1.RuntimeSessionReady{
		ExecutionId: open.GetExecutionId(), Generation: open.GetGeneration(), ProtocolMajor: runtimeprotocol.ProtocolMajor, ProtocolMinor: runtimeprotocol.ProtocolMinor,
	}}}); err != nil {
		return err
	}
	for {
		request, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		if fake.full || len(request.GetCommand().GetJson()) == 0 {
			continue
		}
		var command runtimeprotocol.Command
		if err := json.Unmarshal(request.GetCommand().GetJson(), &command); err != nil {
			return err
		}
		message := runtimeprotocol.Message{
			RequestID: command.RequestID, Protocol: command.Protocol, ExecutionID: command.ExecutionID,
			Generation: command.Generation, CommandID: command.CommandID, OccurredAt: fake.now.Format(time.RFC3339Nano),
			MessageType: "Result", Payload: map[string]any{"text": "done"},
		}
		if fake.terminalErrorCode != "" && command.CommandType == "SendTurn" {
			message.MessageType, message.Payload = "Error", nil
			message.Error = &runtimeprotocol.Error{Code: fake.terminalErrorCode, Message: "managed capability unavailable", RequiresUserAction: true, CanReconstructHistory: true, CanMoveWorker: true}
		}
		if fake.disconnectAfterCheckpoint && command.CommandType == "SendTurn" {
			message.MessageType = "Progress"
			message.Payload["text"] = "checkpointed"
		}
		if fake.capabilityResourceID != "" && command.CommandType == "SendTurn" {
			eventType, status := "item.completed", "completed"
			if fake.capabilityStartedOnly {
				eventType, status = "item.started", "inProgress"
			}
			activity := runtimeprotocol.Message{
				RequestID: command.RequestID, Protocol: command.Protocol, ExecutionID: command.ExecutionID,
				Generation: command.Generation, CommandID: command.CommandID, OccurredAt: fake.now.Format(time.RFC3339Nano),
				MessageType: "Event", Payload: map[string]any{
					"eventVersion": 2,
					"eventType":    eventType,
					"payload": map[string]any{
						"itemType": "mcp_tool_call", "status": status,
						"data": map[string]any{"capabilityResourceId": fake.capabilityResourceID, "providerItemId": "mcp-call-1"},
					},
				},
			}
			encoded, err := json.Marshal(activity)
			if err != nil {
				return err
			}
			if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Json{Json: encoded}}); err != nil {
				return err
			}
		}
		if fake.skillCapabilityResourceID != "" && command.CommandType == "SendTurn" {
			activity := runtimeprotocol.Message{
				RequestID: command.RequestID, Protocol: command.Protocol, ExecutionID: command.ExecutionID,
				Generation: command.Generation, CommandID: command.CommandID, OccurredAt: fake.now.Format(time.RFC3339Nano),
				MessageType: "Event", Payload: map[string]any{
					"eventVersion": 2,
					"eventType":    "item.completed",
					"payload": map[string]any{
						"itemType": "dynamic_tool_call", "status": "completed",
						"data": map[string]any{
							"capabilityResourceId": fake.skillCapabilityResourceID,
							"sourceItemType":       "skill",
							"providerItemId":       "mcp-call-1",
						},
					},
				},
			}
			encoded, err := json.Marshal(activity)
			if err != nil {
				return err
			}
			if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Json{Json: encoded}}); err != nil {
				return err
			}
		}
		if fake.terminalCommandIDMismatch && command.CommandType == "SendTurn" {
			message.CommandID += "-mismatch"
		}
		encoded, err := json.Marshal(message)
		if err != nil {
			return err
		}
		if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Json{Json: encoded}}); err != nil {
			return err
		}
		if fake.disconnectAfterCheckpoint && command.CommandType == "SendTurn" {
			if fake.sendRuntimeErrorAfterCheckpoint {
				return stream.Send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Error{Error: &workerruntimev1alpha1.RuntimeSessionError{Code: "runtime_execution_failed", Message: "Runtime command failed"}}})
			}
			code := fake.disconnectCode
			if code == connect.CodeUnknown {
				code = connect.CodeUnavailable
			}
			return connect.NewError(code, errors.New("worker exited"))
		}
		if command.CommandType == "SendTurn" {
			return nil
		}
	}
}

func TestDurableRuntimeExecutionPassesExecutionCapabilitiesToRuntimeOpen(t *testing.T) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	skill := SkillBundleRef{BundleID: "skill-bundle", Version: "v1", Digest: "sha256:" + strings.Repeat("b", 64)}
	wire := &runtimeWorkerFake{identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}, now: now}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, SkillBundleRefs: []SkillBundleRef{skill},
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(wire.openBindings) != 2 || wire.openBindings[0].GetResourceId() != mcp.ServerID || wire.openBindings[1].GetResourceId() != skill.BundleID || wire.openManifestDigest != "sha256:test" {
		t.Fatalf("Runtime open capabilities=%v digest=%q", wire.openBindings, wire.openManifestDigest)
	}
	if len(store.capabilityEvents) != 2 || store.capabilityEvents[0].Operation != "mcp.call" || store.capabilityEvents[1].Operation != "skill.load" || store.capabilityEvents[0].Result != "accepted" || store.capabilityEvents[0].MutationDigest == store.capabilityEvents[1].MutationDigest {
		t.Fatalf("capability admission events=%+v", store.capabilityEvents)
	}
}

func TestDurableRuntimeExecutionAuditsProviderCapabilityFailure(t *testing.T) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	wire := &runtimeWorkerFake{identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}, now: now, terminalErrorCode: "capability_unsupported"}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrDurableRuntimeExecutionFailed) || result.Transition.Execution.State != ExecutionFailed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(store.capabilityEvents) != 2 || store.capabilityEvents[0].Result != "accepted" || store.capabilityEvents[1].Operation != "mcp.fail" || store.capabilityEvents[1].Result != "failed" || store.capabilityEvents[1].ErrorCode != "capability_unsupported" {
		t.Fatalf("capability events=%+v", store.capabilityEvents)
	}
}

func TestDurableRuntimeExecutionAuditsUnknownStartedCapabilityFailure(t *testing.T) {
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	wire := &runtimeWorkerFake{
		identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		now:      now, terminalErrorCode: "provider_unavailable", capabilityResourceID: mcp.ServerID, capabilityStartedOnly: true,
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeRecoveryRequiresUserAction) || result.Transition.Execution.State != ExecutionRunning || !store.execution.PendingSideEffect || slices.Contains(store.calls, "fail") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(store.capabilityEvents) != 2 || store.capabilityEvents[0].Result != "accepted" || store.capabilityEvents[1].Operation != "mcp.fail" || store.capabilityEvents[1].Result != "failed" || store.capabilityEvents[1].ErrorCode != "capability_call_unknown" || store.capabilityEvents[1].InputDigest == "" || store.capabilityEvents[1].ResultDigest != "" {
		t.Fatalf("capability events=%+v", store.capabilityEvents)
	}
}

func TestDurableRuntimeExecutionPreservesPendingCapabilityAcrossSettlementPaths(t *testing.T) {
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	for _, test := range []struct {
		name              string
		commandIDMismatch bool
	}{{name: "terminal-result"}, {name: "protocol-error", commandIDMismatch: true}} {
		t.Run(test.name, func(t *testing.T) {
			wire := &runtimeWorkerFake{
				identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
				now:      now, capabilityResourceID: mcp.ServerID, capabilityStartedOnly: true,
				terminalCommandIDMismatch: test.commandIDMismatch,
			}
			store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
			coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
				Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
				FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
				Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
				McpServerRefs: []McpServerRef{mcp}, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
			})
			if !errors.Is(err, ErrRuntimeRecoveryRequiresUserAction) || result.Transition.Execution.State != ExecutionRunning || !store.execution.PendingSideEffect || slices.Contains(store.calls, "complete") || slices.Contains(store.calls, "fail") {
				t.Fatalf("result=%#v calls=%v err=%v", result, store.calls, err)
			}
			if len(store.capabilityEvents) != 2 || store.capabilityEvents[1].Operation != "mcp.fail" || store.capabilityEvents[1].ErrorCode != "capability_call_unknown" {
				t.Fatalf("capability events=%+v", store.capabilityEvents)
			}
		})
	}
}

func TestDurableRuntimeExecutionPreservesPendingCapabilityWhenCheckpointFails(t *testing.T) {
	now := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	wire := &runtimeWorkerFake{
		identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		now:      now, capabilityResourceID: mcp.ServerID, capabilityStartedOnly: true,
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}, checkpointError: errors.New("checkpoint unavailable")}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeRecoveryRequiresUserAction) || result.Transition.Execution.State != ExecutionRunning || slices.Contains(store.calls, "complete") || slices.Contains(store.calls, "fail") {
		t.Fatalf("result=%#v calls=%v err=%v", result, store.calls, err)
	}
	if len(store.capabilityEvents) != 2 || store.capabilityEvents[1].Operation != "mcp.fail" || store.capabilityEvents[1].ErrorCode != "capability_call_unknown" {
		t.Fatalf("capability events=%+v", store.capabilityEvents)
	}
}

func TestDurableRuntimeExecutionAuditsCompletedCapabilityBeforeProviderFailure(t *testing.T) {
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	wire := &runtimeWorkerFake{
		identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		now:      now, terminalErrorCode: "internal_error", capabilityResourceID: mcp.ServerID,
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrDurableRuntimeExecutionFailed) || result.Transition.Execution.State != ExecutionFailed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(store.capabilityEvents) != 2 || store.capabilityEvents[1].Operation != "mcp.call" || store.capabilityEvents[1].Result != "succeeded" || store.capabilityEvents[1].ResultDigest == "" {
		t.Fatalf("capability events=%+v", store.capabilityEvents)
	}
}

func TestDurableRuntimeExecutionAuditsOpaqueMcpOutcome(t *testing.T) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	wire := &runtimeWorkerFake{identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}, now: now, capabilityResourceID: mcp.ServerID}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(store.capabilityEvents) != 2 || store.capabilityEvents[1].Operation != "mcp.call" || store.capabilityEvents[1].Result != "succeeded" || store.capabilityEvents[1].ResourceID != mcp.ServerID || store.capabilityEvents[1].ResultDigest == "" {
		t.Fatalf("capability events=%+v", store.capabilityEvents)
	}
}

func TestDurableRuntimeExecutionAuditsOpaqueSkillOutcome(t *testing.T) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	skill := SkillBundleRef{BundleID: "skill-bundle", Version: "v1", Digest: "sha256:" + strings.Repeat("b", 64)}
	wire := &runtimeWorkerFake{identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}, now: now, skillCapabilityResourceID: skill.BundleID}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		SkillBundleRefs: []SkillBundleRef{skill}, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(store.capabilityEvents) != 2 || store.capabilityEvents[1].Operation != "skill.load" || store.capabilityEvents[1].Result != "succeeded" || store.capabilityEvents[1].ResourceID != skill.BundleID || store.capabilityEvents[1].ResultDigest == "" {
		t.Fatalf("capability events=%+v", store.capabilityEvents)
	}
}

func TestDurableRuntimeExecutionKeepsCapabilityKindsDistinctWhenIDsMatch(t *testing.T) {
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	sharedID := "shared-capability-id"
	mcp := McpServerRef{ServerID: sharedID, Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	skill := SkillBundleRef{BundleID: sharedID, Version: "v1", Digest: "sha256:" + strings.Repeat("b", 64)}
	wire := &runtimeWorkerFake{
		identity:                  &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		now:                       now,
		capabilityResourceID:      sharedID,
		skillCapabilityResourceID: sharedID,
		terminalErrorCode:         "internal_error",
		capabilityStartedOnly:     true,
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, wire), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, SkillBundleRefs: []SkillBundleRef{skill},
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeRecoveryRequiresUserAction) || result.Transition.Execution.State != ExecutionRunning || !store.execution.PendingSideEffect || slices.Contains(store.calls, "fail") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(store.capabilityEvents) != 4 {
		t.Fatalf("capability events=%+v", store.capabilityEvents)
	}
	if store.capabilityEvents[2].Operation != "skill.load" || store.capabilityEvents[2].Resource != ResourceSkillBundle || store.capabilityEvents[2].Digest != skill.Digest || store.capabilityEvents[3].Operation != "mcp.fail" || store.capabilityEvents[3].Resource != ResourceMcpServer || store.capabilityEvents[3].Digest != mcp.Digest || store.capabilityEvents[3].ErrorCode != "capability_call_unknown" {
		t.Fatalf("capability outcome attribution=%+v", store.capabilityEvents)
	}
}

func newRuntimeTestSupervisor(t *testing.T, now time.Time, full bool) *workerclient.Supervisor {
	t.Helper()
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}
	wire := &runtimeWorkerFake{identity: identity, now: now, full: full}
	return newRuntimeTestSupervisorForWire(t, wire)
}

func newRuntimeTestSupervisorForWire(t *testing.T, wire *runtimeWorkerFake) *workerclient.Supervisor {
	t.Helper()
	mux := http.NewServeMux()
	workerPath, workerHandler := workerv1alpha1connect.NewWorkerExecutionServiceHandler(wire)
	runtimePath, runtimeHandler := workerruntimev1alpha1connect.NewWorkerRuntimeServiceHandler(wire)
	mux.Handle(workerPath, workerHandler)
	mux.Handle(runtimePath, runtimeHandler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	supervisor, err := workerclient.New(workerclient.Config{
		Client:                 workerv1alpha1connect.NewWorkerExecutionServiceClient(server.Client(), server.URL),
		RuntimeClient:          workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(server.Client(), server.URL),
		ExpectedWorkerIdentity: wire.identity, Clock: func() time.Time { return wire.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return supervisor
}

func testVerifiedPrincipalSource() VerifiedPrincipalSource {
	return func() (*authn.VerifiedPrincipal, error) { return &authn.VerifiedPrincipal{}, nil }
}

func assertFreshPrincipals(t *testing.T, principals []*authn.VerifiedPrincipal, want int) {
	t.Helper()
	if len(principals) != want {
		t.Fatalf("principal count = %d, want %d", len(principals), want)
	}
	seen := make(map[*authn.VerifiedPrincipal]struct{}, len(principals))
	for _, principal := range principals {
		if _, exists := seen[principal]; exists {
			t.Fatal("coordinator reused a one-shot principal")
		}
		seen[principal] = struct{}{}
	}
}

func TestBoundedRuntimeIdentifierUsesPublicLimit(t *testing.T) {
	base := strings.Repeat("a", maxPublicExecutionMessageIdentifierBytes)
	got := boundedRuntimeIdentifier(base, "start")
	want := strings.Repeat("a", maxPublicExecutionMessageIdentifierBytes-len("-start")) + "-start"
	if got != want || len(got) != maxPublicExecutionMessageIdentifierBytes {
		t.Fatalf("bounded runtime identifier = %q, want %q", got, want)
	}
}

func TestOpenRuntimeSessionWithRetryRecoversUnavailableWorker(t *testing.T) {
	attempts := 0
	var openedContext context.Context
	_, err := openRuntimeSessionWithRetry(context.Background(), func(ctx context.Context) (runtimeSession, error) {
		attempts++
		if attempts < 3 {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("worker unavailable"))
		}
		openedContext = ctx
		return nil, nil
	})
	if err != nil || attempts != 3 || openedContext == nil || openedContext.Err() != nil {
		t.Fatalf("retry result err=%v attempts=%d", err, attempts)
	}
}

func TestStartRuntimeClaimHeartbeatStopsOnWorkerHealthFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	healthCalled := make(chan struct{}, 1)
	stopCalled := make(chan struct{}, 1)
	healthErr := errors.New("stale Worker binding")
	coordinator := &DurableRuntimeExecutionCoordinator{claimRenewInterval: 5 * time.Millisecond}
	claimFailure := coordinator.startRuntimeClaimHeartbeat(ctx, ctx, func() {
		select {
		case stopCalled <- struct{}{}:
		default:
		}
		cancel()
	}, testVerifiedPrincipalSource(), RuntimeExecutionClaim{}, func(healthContext context.Context) error {
		if _, ok := healthContext.Deadline(); !ok {
			t.Error("Worker health check context has no deadline")
		}
		healthCalled <- struct{}{}
		return healthErr
	})

	select {
	case <-healthCalled:
	case <-time.After(time.Second):
		t.Fatal("claim heartbeat did not run the Worker health check")
	}
	select {
	case <-stopCalled:
	case <-time.After(time.Second):
		t.Fatal("claim heartbeat did not stop after Worker health failure")
	}
	select {
	case err := <-claimFailure:
		if !errors.Is(err, ErrRuntimeEnvironmentUnavailable) || !strings.Contains(err.Error(), healthErr.Error()) {
			t.Fatalf("claim heartbeat failure = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("claim heartbeat did not report Worker health failure")
	}
}

func TestRuntimeClaimHeartbeatIgnoresLateHealthFailureAfterRuntimeFinishes(t *testing.T) {
	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	healthCtx, stopHealth := context.WithCancel(heartbeatCtx)
	healthStarted := make(chan struct{})
	releaseHealth := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseHealth) }) }
	t.Cleanup(func() {
		stopHealth()
		stopHeartbeat()
		release()
	})
	renewed := make(chan struct{}, 1)
	store := &durableRuntimeExecutionStoreFake{renewed: renewed}
	coordinator := &DurableRuntimeExecutionCoordinator{store: store, claimRenewInterval: 5 * time.Millisecond}
	failures := coordinator.startRuntimeClaimHeartbeat(heartbeatCtx, healthCtx, func() {
		t.Error("late Worker health failure stopped the runtime")
	}, testVerifiedPrincipalSource(), RuntimeExecutionClaim{}, func(context.Context) error {
		close(healthStarted)
		<-releaseHealth
		return errors.New("Worker stream already closed")
	})

	select {
	case <-healthStarted:
	case <-time.After(time.Second):
		t.Fatal("health check did not start")
	}
	stopHealth()
	release()
	select {
	case <-renewed:
	case err := <-failures:
		t.Fatalf("late health failure stopped claim renewal: %v", err)
	case <-time.After(time.Second):
		t.Fatal("claim did not renew after runtime health checks stopped")
	}
	if err := stopRuntimeClaimHeartbeat(stopHeartbeat, func() {}, failures); err != nil {
		t.Fatalf("heartbeat cleanup error = %v", err)
	}
}

func TestRuntimeClaimHeartbeatBoundsBlockedRenewal(t *testing.T) {
	interval := 40 * time.Millisecond
	renewStarted := make(chan context.Context, 2)
	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	t.Cleanup(stopHeartbeat)
	store := &durableRuntimeExecutionStoreFake{renewStarted: renewStarted, renewBlock: make(chan struct{})}
	coordinator := &DurableRuntimeExecutionCoordinator{store: store, claimRenewInterval: interval}
	runtimeStopped := make(chan struct{})
	var stopOnce sync.Once
	failures := coordinator.startRuntimeClaimHeartbeat(heartbeatCtx, heartbeatCtx, func() {
		stopOnce.Do(func() { close(runtimeStopped) })
	}, testVerifiedPrincipalSource(), RuntimeExecutionClaim{}, nil)

	var renewCtx context.Context
	select {
	case renewCtx = <-renewStarted:
	case <-time.After(time.Second):
		t.Fatal("claim renewal did not start")
	}
	deadline, ok := renewCtx.Deadline()
	if !ok {
		t.Fatal("claim renewal context has no deadline")
	}
	if remaining := time.Until(deadline); remaining > interval/2 {
		t.Fatalf("claim renewal deadline remaining = %v, want no more than %v", remaining, interval/2)
	}
	select {
	case <-runtimeStopped:
	case <-time.After(time.Second):
		t.Fatal("blocked claim renewal did not stop the runtime")
	}
	select {
	case err := <-failures:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("claim renewal failure = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked claim renewal did not report its deadline")
	}
	select {
	case <-renewStarted:
		t.Fatal("claim renewal continued after its deadline")
	case <-time.After(2 * interval):
	}
}

func TestStopRuntimeClaimHeartbeatReturnsFailureBeforeSettlement(t *testing.T) {
	failure := errors.New("claim lost")
	failures := make(chan error, 1)
	failures <- failure
	close(failures)
	stopped := false
	if err := stopRuntimeClaimHeartbeat(func() { stopped = true }, func() {}, failures); !errors.Is(err, failure) || !stopped {
		t.Fatalf("heartbeat stop = err %v stopped %t", err, stopped)
	}
}

func TestDurableRuntimeExecutionDoesNotCompleteAfterKnownClaimFailure(t *testing.T) {
	now := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	renewStarted := make(chan context.Context, 1)
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}, renewStarted: renewStarted, renewBlock: make(chan struct{})}
	worker := &runtimeWorkerFake{identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}, now: now}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.claimRenewInterval = 40 * time.Millisecond
	finalPrincipalStarted := make(chan struct{})
	releaseFinalPrincipal := make(chan struct{})
	var releaseOnce sync.Once
	releasePrincipal := func() { releaseOnce.Do(func() { close(releaseFinalPrincipal) }) }
	t.Cleanup(releasePrincipal)
	var principalMu sync.Mutex
	principalCalls := 0
	principalSource := VerifiedPrincipalSource(func() (*authn.VerifiedPrincipal, error) {
		principalMu.Lock()
		principalCalls++
		call := principalCalls
		principalMu.Unlock()
		if call == 7 {
			close(finalPrincipalStarted)
			<-releaseFinalPrincipal
		}
		return &authn.VerifiedPrincipal{}, nil
	})
	type executionOutcome struct {
		result DurableRuntimeExecutionResult
		err    error
	}
	done := make(chan executionOutcome, 1)
	go func() {
		result, executeErr := coordinator.Execute(context.Background(), principalSource, DurableRuntimeExecutionInput{
			Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
			Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
		})
		done <- executionOutcome{result: result, err: executeErr}
	}()

	select {
	case <-finalPrincipalStarted:
	case <-time.After(time.Second):
		t.Fatal("execution did not reach the final settlement principal")
	}
	var renewCtx context.Context
	select {
	case renewCtx = <-renewStarted:
	case <-time.After(time.Second):
		t.Fatal("claim heartbeat did not begin renewal")
	}
	if _, ok := renewCtx.Deadline(); !ok {
		t.Fatal("claim renewal context has no deadline")
	}
	select {
	case <-renewCtx.Done():
		if !errors.Is(renewCtx.Err(), context.DeadlineExceeded) {
			t.Fatalf("claim renewal context error = %v", renewCtx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("claim renewal did not reach its deadline")
	}
	releasePrincipal()
	select {
	case outcome := <-done:
		if !errors.Is(outcome.err, context.DeadlineExceeded) || outcome.result.Transition.Execution.State != ExecutionRunning {
			t.Fatalf("result=%#v err=%v", outcome.result, outcome.err)
		}
	case <-time.After(time.Second):
		t.Fatal("execution did not return the known claim failure")
	}
	if slices.Contains(store.calls, "complete") || slices.Contains(store.calls, "fail") {
		t.Fatalf("settlement ran after known claim failure: %v", store.calls)
	}
}

func TestDurableRuntimeExecutionRenewsClaimUntilSettlementReturns(t *testing.T) {
	now := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name              string
		terminalErrorCode string
		settlement        string
		wantState         ExecutionState
		renewError        error
		renewInterval     time.Duration
		maxDuration       time.Duration
		pastDeadline      bool
	}{
		{name: "completion", settlement: "complete", wantState: ExecutionSucceeded, renewError: errors.New("claim became stale during settlement"), renewInterval: 50 * time.Millisecond},
		{name: "failure", terminalErrorCode: "provider_failed", settlement: "fail", wantState: ExecutionFailed, renewError: errors.New("claim became stale during settlement"), renewInterval: 50 * time.Millisecond},
		{name: "completion past runtime deadline", settlement: "complete", wantState: ExecutionSucceeded, renewInterval: 20 * time.Millisecond, maxDuration: 80 * time.Millisecond, pastDeadline: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			settlementStarted := make(chan string, 1)
			settlementRelease := make(chan struct{})
			var releaseOnce sync.Once
			releaseSettlement := func() { releaseOnce.Do(func() { close(settlementRelease) }) }
			t.Cleanup(releaseSettlement)
			renewed := make(chan struct{}, 16)
			store := &durableRuntimeExecutionStoreFake{
				execution: ExecutionSnapshot{State: ExecutionQueued}, settlementStarted: settlementStarted,
				settlementRelease: settlementRelease, renewed: renewed, renewError: test.renewError,
			}
			worker := &runtimeWorkerFake{
				identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
				now:      now, terminalErrorCode: test.terminalErrorCode,
			}
			coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
				Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
				FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace", MaxDuration: test.maxDuration,
			})
			if err != nil {
				t.Fatal(err)
			}
			coordinator.claimRenewInterval = test.renewInterval
			type executionOutcome struct {
				result DurableRuntimeExecutionResult
				err    error
			}
			done := make(chan executionOutcome, 1)
			go func() {
				result, executeErr := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
					Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
					Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
				})
				done <- executionOutcome{result: result, err: executeErr}
			}()

			select {
			case got := <-settlementStarted:
				if got != test.settlement {
					t.Fatalf("settlement = %q, want %q", got, test.settlement)
				}
			case <-time.After(time.Second):
				t.Fatal("execution did not reach durable settlement")
			}
			select {
			case <-renewed:
			case <-time.After(time.Second):
				t.Fatal("claim was not renewed while durable settlement was blocked")
			}
			if test.pastDeadline {
				time.Sleep(test.maxDuration + 2*test.renewInterval)
			drainBeforeDeadlineCheck:
				for {
					select {
					case <-renewed:
					default:
						break drainBeforeDeadlineCheck
					}
				}
				select {
				case <-renewed:
				case <-time.After(4 * test.renewInterval):
					t.Fatal("claim stopped renewing when the runtime deadline elapsed during settlement")
				}
			}
			releaseSettlement()
			var outcome executionOutcome
			select {
			case outcome = <-done:
			case <-time.After(time.Second):
				t.Fatal("execution did not finish after settlement returned")
			}
			if outcome.result.Transition.Execution.State != test.wantState {
				t.Fatalf("state = %q, want %q; err=%v", outcome.result.Transition.Execution.State, test.wantState, outcome.err)
			}
			if test.wantState == ExecutionSucceeded && outcome.err != nil {
				t.Fatalf("completion error = %v", outcome.err)
			}
			if test.wantState == ExecutionFailed && !errors.Is(outcome.err, ErrDurableRuntimeExecutionFailed) {
				t.Fatalf("failure error = %v", outcome.err)
			}
		drainRenewals:
			for {
				select {
				case <-renewed:
				default:
					break drainRenewals
				}
			}
			select {
			case <-renewed:
				t.Fatal("claim heartbeat continued after durable settlement")
			case <-time.After(3 * test.renewInterval):
			}
		})
	}
}

func TestDurableRuntimeExecutionBoundsPersistenceCalls(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name, blockedCall, terminalErrorCode string
		wantState                            ExecutionState
	}{
		{name: "checkpoint", blockedCall: "checkpoint", wantState: ExecutionFailed},
		{name: "completion", blockedCall: "complete", wantState: ExecutionRunning},
		{name: "failure", blockedCall: "fail", terminalErrorCode: "provider_failed", wantState: ExecutionRunning},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan string, 1)
			blockedDone := make(chan error, 1)
			store := &durableRuntimeExecutionStoreFake{
				execution: ExecutionSnapshot{State: ExecutionQueued}, blockedCall: test.blockedCall,
				blockedCallStarted: started, blockedCallDone: blockedDone,
			}
			worker := &runtimeWorkerFake{
				identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
				now:      now, terminalErrorCode: test.terminalErrorCode, disconnectAfterCheckpoint: test.blockedCall == "checkpoint",
			}
			coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
				Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
				FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
			})
			if err != nil {
				t.Fatal(err)
			}
			coordinator.persistenceTimeout = 20 * time.Millisecond
			type outcome struct {
				result DurableRuntimeExecutionResult
				err    error
			}
			finished := make(chan outcome, 1)
			go func() {
				result, executeErr := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
					Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
					Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
				})
				finished <- outcome{result: result, err: executeErr}
			}()

			select {
			case got := <-started:
				if got != test.blockedCall {
					t.Fatalf("blocked call = %q, want %q", got, test.blockedCall)
				}
			case <-time.After(time.Second):
				t.Fatal("execution did not reach the blocked persistence call")
			}
			select {
			case callErr := <-blockedDone:
				if !errors.Is(callErr, context.DeadlineExceeded) {
					t.Fatalf("blocked call error = %v", callErr)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked persistence call did not reach its deadline")
			}
			select {
			case got := <-finished:
				if got.result.Transition.Execution.State != test.wantState || got.err == nil {
					t.Fatalf("result = %#v, error = %v", got.result, got.err)
				}
			case <-time.After(time.Second):
				t.Fatal("execution did not finish after the persistence deadline")
			}
		})
	}

	t.Run("cancel", func(t *testing.T) {
		started := make(chan string, 1)
		blockedDone := make(chan error, 1)
		store := &durableRuntimeExecutionStoreFake{blockedCall: "cancel", blockedCallStarted: started, blockedCallDone: blockedDone}
		coordinator := &DurableRuntimeExecutionCoordinator{store: store, persistenceTimeout: 20 * time.Millisecond}
		finished := make(chan error, 1)
		go func() {
			_, err := coordinator.Cancel(context.Background(), &authn.VerifiedPrincipal{}, CancelTurnInput{
				Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", TargetExecutionID: "execution", Generation: 7,
			})
			finished <- err
		}()
		select {
		case got := <-started:
			if got != "cancel" {
				t.Fatalf("blocked call = %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("cancel did not reach the store")
		}
		select {
		case callErr := <-blockedDone:
			if !errors.Is(callErr, context.DeadlineExceeded) {
				t.Fatalf("cancel store error = %v", callErr)
			}
		case <-time.After(time.Second):
			t.Fatal("cancel store call did not reach its deadline")
		}
		select {
		case err := <-finished:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancel error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancel did not finish after the persistence deadline")
		}
	})
}

type blockingRuntimeSession struct {
	closed         chan struct{}
	closeOnce      sync.Once
	receiveStarted chan struct{}
}

func (session *blockingRuntimeSession) Send(context.Context, runtimeprotocol.Command) error {
	return nil
}

func (session *blockingRuntimeSession) Receive() (runtimeprotocol.Message, error) {
	select {
	case <-session.receiveStarted:
	default:
		close(session.receiveStarted)
	}
	<-session.closed
	return runtimeprotocol.Message{}, io.ErrClosedPipe
}

func (session *blockingRuntimeSession) CloseRequest() error {
	session.closeOnce.Do(func() { close(session.closed) })
	return nil
}

func (session *blockingRuntimeSession) CloseResponse() error { return session.CloseRequest() }

func TestExecuteRuntimeTurnClosesBlockingReceiveOnContextCancellation(t *testing.T) {
	coordinator := &DurableRuntimeExecutionCoordinator{active: make(map[durableExecutionKey]*activeDurableExecution)}
	key := durableExecutionKey{tenantID: "tenant", projectID: "project", sessionID: "session", turnID: "turn", executionID: "execution", generation: 7}
	active, unregister, err := coordinator.registerActiveExecution(key, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	ctx, cancel := context.WithCancel(context.Background())
	session := &blockingRuntimeSession{closed: make(chan struct{}), receiveStarted: make(chan struct{})}
	resultErr := make(chan error, 1)
	go func() {
		_, runErr := coordinator.executeRuntimeTurn(ctx, testVerifiedPrincipalSource(), &RuntimeExecutionClaim{}, nil, key, active, session, RuntimeTurnInput{
			Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7,
			WorkspaceDirectory: "/workspace", ProviderKind: "codex", InputText: "hello", OccurredAt: time.Now().UTC(),
		})
		resultErr <- runErr
	}()
	select {
	case <-session.receiveStarted:
	case <-time.After(time.Second):
		t.Fatal("runtime Receive did not block")
	}
	cancel()
	select {
	case runErr := <-resultErr:
		if runErr == nil {
			t.Fatal("runtime turn completed after context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not unblock runtime Receive")
	}
	select {
	case <-session.closed:
	default:
		t.Fatal("context cancellation did not close runtime session")
	}
}

func TestRuntimeWorkerRouteUsesBoundEnvironmentGeneration(t *testing.T) {
	coordinator := &DurableRuntimeExecutionCoordinator{
		now: func() time.Time { return time.Now() }, fencingToken: []byte("token"),
		workerClientCertificate: tls.Certificate{Certificate: [][]byte{{1}}, PrivateKey: struct{}{}}, workerRootCAs: x509.NewCertPool(),
	}
	session := RuntimeSessionSnapshot{SessionSnapshot: SessionSnapshot{EnvironmentLeaseID: "lease-alpha", EnvironmentGeneration: 7},
		WorkerEndpoint: "https://worker.example.test:8091", WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker.example.test", EnvironmentReady: true}
	worker, err := coordinator.workerForSession(session)
	if err != nil || worker.supervisor == nil || worker.leaseID != "lease-alpha" || worker.generation != 7 {
		t.Fatalf("worker = %#v, error = %v", worker, err)
	}
	session.EnvironmentReady = false
	if _, err := coordinator.workerForSession(session); !errors.Is(err, ErrRuntimeEnvironmentUnavailable) {
		t.Fatalf("unready environment error = %v", err)
	}
	session.EnvironmentReady = true
	session.WorkerSPIFFEID += "?unexpected=query"
	if _, err := coordinator.workerForSession(session); !errors.Is(err, ErrRuntimeEnvironmentUnavailable) {
		t.Fatalf("malformed Worker identity error = %v", err)
	}
}

func TestRuntimeWorkspaceDirectoryForWorker(t *testing.T) {
	coordinatorWorkspace := "/tmp/cloud-agents/workspace"
	tests := []struct {
		name   string
		worker runtimeWorker
		want   string
	}{
		{name: "foundation", worker: runtimeWorker{foundation: &FoundationRuntime{}}, want: "/workspace"},
		{name: "local", worker: runtimeWorker{}, want: coordinatorWorkspace},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := runtimeWorkspaceDirectoryForWorker(test.worker, coordinatorWorkspace); got != test.want {
				t.Fatalf("runtime workspace directory = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReceiveRuntimeMessagesPreservesBoundedTranscript(t *testing.T) {
	wire := []runtimeprotocol.Message{
		{RequestID: "resolve-request", CommandID: "interaction-1", MessageType: "Result"},
		{CommandID: "turn", MessageType: "Progress"},
		{CommandID: "turn", MessageType: "Event"},
		{CommandID: "turn", MessageType: "Result", Payload: map[string]any{"text": "done"}},
	}
	index := 0
	accepted := make([]runtimeprotocol.Message, 0, len(wire)-1)
	collected, terminal, err := receiveRuntimeMessages(func() (runtimeprotocol.Message, error) {
		message := wire[index]
		index++
		return message, nil
	}, "turn", func(message runtimeprotocol.Message) (bool, error) {
		return message.CommandID == "interaction-1", nil
	}, func(messages []runtimeprotocol.Message, _ runtimeprotocol.Message) error {
		accepted = append(accepted[:0], messages...)
		return nil
	})
	if err != nil || !reflect.DeepEqual(collected, wire[1:]) || !reflect.DeepEqual(accepted, wire[1:]) || terminal.MessageType != "Result" || terminal.Payload["text"] != "done" || index != len(wire) {
		t.Fatalf("messages=%#v accepted=%#v terminal=%#v reads=%d err=%v", collected, accepted, terminal, index, err)
	}
}

func TestReceiveRuntimeMessagesCompactsAssistantDeltasAndKeepsTerminal(t *testing.T) {
	prior := runtimeprotocol.Message{RequestID: "prior-request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "prior-command", OccurredAt: time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Progress"}
	wire := make([]runtimeprotocol.Message, 0, maxRuntimeExecutionMessages)
	for index := 0; index < maxRuntimeExecutionMessages-1; index++ {
		wire = append(wire, assistantTextDeltaRuntimeMessage(index, "x"))
	}
	wire = append(wire, runtimeprotocol.Message{RequestID: "turn-request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "turn", OccurredAt: time.Date(2026, 8, 31, 10, 1, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "done"}})
	index := 0
	collected, terminal, err := receiveRuntimeMessagesWithLimit(func() (runtimeprotocol.Message, error) {
		message := wire[index]
		index++
		return message, nil
	}, "turn", nil, nil, maxRuntimeExecutionMessages-len([]runtimeprotocol.Message{prior}))
	if err != nil || terminal.MessageType != "Result" || len(collected) != 2 || collected[len(collected)-1].MessageType != "Result" {
		t.Fatalf("collected=%#v terminal=%#v reads=%d err=%v", collected, terminal, index, err)
	}
	if delta, ok := collected[0].Payload["payload"].(map[string]any)["delta"].(string); !ok || delta != strings.Repeat("x", maxRuntimeExecutionMessages-1) {
		t.Fatalf("compacted assistant delta = %#v", collected[0].Payload)
	}
	if total := len(append([]runtimeprotocol.Message{prior}, collected...)); total > maxRuntimeExecutionMessages {
		t.Fatalf("recovery transcript length = %d", total)
	}
}

func TestCompactRuntimeMessagesPreservesCommandOutputDeltas(t *testing.T) {
	first := runtimeprotocol.Message{RequestID: "turn-request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "turn", OccurredAt: time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Event", Payload: map[string]any{"eventType": "content.delta", "payload": map[string]any{"streamKind": "command_output", "delta": "a", "byteOffset": 0, "byteLength": 1}}}
	second := first
	second.Payload = map[string]any{"eventType": "content.delta", "payload": map[string]any{"streamKind": "command_output", "delta": "b", "byteOffset": 1, "byteLength": 1}}
	compacted := compactRuntimeMessages([]runtimeprotocol.Message{first, second})
	if len(compacted) != 2 {
		t.Fatalf("command output deltas were compacted: %#v", compacted)
	}
}

func TestCompactRuntimeMessagesCompactsPriorAssistantTranscript(t *testing.T) {
	prior := make([]runtimeprotocol.Message, 0, maxRuntimeExecutionMessages)
	for index := 0; index < maxRuntimeExecutionMessages; index++ {
		prior = append(prior, assistantTextDeltaRuntimeMessage(index, "x"))
	}
	compacted := compactRuntimeMessages(prior)
	if len(compacted) != 1 || compacted[0].Payload["payload"].(map[string]any)["delta"] != strings.Repeat("x", maxRuntimeExecutionMessages) {
		t.Fatalf("prior assistant transcript = %#v", compacted)
	}
	key := durableExecutionKey{executionID: "execution", generation: 7}
	active := &activeDurableExecution{}
	coordinator := &DurableRuntimeExecutionCoordinator{active: map[durableExecutionKey]*activeDurableExecution{key: active}}
	for _, message := range prior {
		coordinator.recordActiveRuntimeMessage(key, active, message)
	}
	if !reflect.DeepEqual(active.messages, compacted) {
		t.Fatalf("active assistant transcript = %#v", active.messages)
	}
}

func TestCompactedRecoveryTranscriptKeepsNewSideEffectInCheckpointState(t *testing.T) {
	prior := make([]runtimeprotocol.Message, 0, maxRuntimeExecutionMessages)
	for index := 0; index < maxRuntimeExecutionMessages; index++ {
		prior = append(prior, assistantTextDeltaRuntimeMessage(index, "x"))
	}
	compactedPrior := compactRuntimeMessages(prior)
	current := runtimeprotocol.Message{RequestID: "turn-request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "turn", OccurredAt: time.Date(2026, 8, 31, 10, 2, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Event", Payload: map[string]any{"eventType": "item.started", "payload": map[string]any{"itemType": "command_execution", "itemId": "command-1"}}}
	combined := append(append([]runtimeprotocol.Message(nil), compactedPrior...), current)
	pending, _ := runtimeCheckpointState(combined, nil, len(compactedPrior))
	if !pending {
		t.Fatalf("recovery side effect was skipped after transcript compaction: %#v", combined)
	}
}

func assistantTextDeltaRuntimeMessage(index int, delta string) runtimeprotocol.Message {
	return runtimeprotocol.Message{RequestID: "turn-request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "turn", OccurredAt: time.Date(2026, 8, 31, 10, 1, index, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Event", Payload: map[string]any{"eventType": "content.delta", "payload": map[string]any{"streamKind": "assistant_text", "delta": delta}}}
}

func TestRuntimeFailureCodeUsesOnlyPublicStableCodes(t *testing.T) {
	terminal := runtimeprotocol.Message{MessageType: "Error", Error: &runtimeprotocol.Error{Code: "capability_unsupported"}}
	if got := runtimeFailureCode(terminal, "runtime_start_failed"); got != "capability_unsupported" {
		t.Fatalf("Runtime failure code = %q", got)
	}
	terminal.Error.Code = "INVALID_CODE!"
	if got := runtimeFailureCode(terminal, "runtime_start_failed"); got != "runtime_start_failed" {
		t.Fatalf("invalid Runtime failure code = %q", got)
	}
}

func TestIndexCapabilityBindingsByResource(t *testing.T) {
	mcp := &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "mcp-server", ResourceId: "mcp-alpha"}
	skill := &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "skill-bundle", ResourceId: "skill-alpha"}
	indexed := indexCapabilityBindings([]*workerruntimev1alpha1.RuntimeCapabilityBinding{nil, mcp, skill, &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "unknown", ResourceId: "ignored"}})
	if len(indexed) != 2 || indexed[capabilityBindingKey{resource: ResourceMcpServer, id: "mcp-alpha"}] != mcp || indexed[capabilityBindingKey{resource: ResourceSkillBundle, id: "skill-alpha"}] != skill {
		t.Fatalf("indexed capability bindings = %#v", indexed)
	}
}

func TestRuntimeInteractionsResolveOnTheActiveStream(t *testing.T) {
	reference := RuntimeExecutionReference{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7}
	key, err := durableRuntimeExecutionKey(reference)
	if err != nil {
		t.Fatal(err)
	}
	store := &durableRuntimeExecutionStoreFake{}
	coordinator := &DurableRuntimeExecutionCoordinator{store: store, now: func() time.Time { return time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC) }, active: make(map[durableExecutionKey]*activeDurableExecution)}
	active, unregister, err := coordinator.registerActiveExecution(key, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	sent := make(chan runtimeprotocol.Command, 3)
	active.send = func(_ context.Context, command runtimeprotocol.Command) error {
		sent <- command
		return nil
	}
	approval := runtimeprotocol.Message{RequestID: "turn-request", CommandID: "turn-command", MessageType: "InteractionRequest", Payload: map[string]any{"requestId": "codex:generation-7:approval:1", "interactionType": "approval"}}
	if handled, err := coordinator.routeRuntimeMessage(key, active, "turn-command", approval); handled || err != nil {
		t.Fatalf("register approval handled=%v err=%v", handled, err)
	}
	coordinator.recordActiveRuntimeMessage(key, active, approval)
	if got := coordinator.ActiveMessages(reference); len(got) != 1 || got[0].Payload["requestId"] != approval.Payload["requestId"] {
		t.Fatalf("active approval = %#v", got)
	}
	resolved := make(chan error, 1)
	approvalInput := RuntimeApprovalResolutionInput{RuntimeExecutionReference: reference, Principal: &authn.VerifiedPrincipal{}, RequestID: "resolve-approval", InteractionRequestID: "codex:generation-7:approval:1", Decision: "accept"}
	go func() { resolved <- coordinator.ResolveApproval(context.Background(), approvalInput) }()
	command := <-sent
	if command.CommandType != "ResolveApproval" || command.Payload["requestId"] != approvalInput.InteractionRequestID {
		t.Fatalf("approval command = %#v", command)
	}
	if handled, err := coordinator.routeRuntimeMessage(key, active, "turn-command", runtimeprotocol.Message{RequestID: command.RequestID, CommandID: command.CommandID, MessageType: "Result"}); !handled || err != nil {
		t.Fatalf("approval result handled=%v err=%v", handled, err)
	}
	if err := <-resolved; err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ResolveApproval(context.Background(), approvalInput); err != nil {
		t.Fatalf("idempotent approval = %v", err)
	}
	approvalInput.Decision = "decline"
	if err := coordinator.ResolveApproval(context.Background(), approvalInput); !errors.Is(err, ErrRuntimeInteractionConflict) {
		t.Fatalf("conflicting approval = %v", err)
	}

	userInput := runtimeprotocol.Message{RequestID: "turn-request", CommandID: "turn-command", MessageType: "InteractionRequest", Payload: map[string]any{"requestId": "claude:generation-7:user-input:2", "interactionType": "user-input"}}
	if _, err := coordinator.routeRuntimeMessage(key, active, "turn-command", userInput); err != nil {
		t.Fatal(err)
	}
	coordinator.recordActiveRuntimeMessage(key, active, userInput)
	answered := make(chan error, 1)
	questionID := strings.Repeat("问", maxRuntimeInteractionRequestIDCharacters)
	answerInput := RuntimeUserInputResolutionInput{RuntimeExecutionReference: reference, Principal: &authn.VerifiedPrincipal{}, RequestID: "resolve-input", InteractionRequestID: "claude:generation-7:user-input:2", Answers: map[string][]string{questionID: {"one", "two"}}}
	go func() { answered <- coordinator.ResolveUserInput(context.Background(), answerInput) }()
	command = <-sent
	if command.CommandType != "ResolveUserInput" {
		t.Fatalf("user-input command = %#v", command)
	}
	if handled, err := coordinator.routeRuntimeMessage(key, active, "turn-command", runtimeprotocol.Message{RequestID: command.RequestID, CommandID: command.CommandID, MessageType: "Error", Error: &runtimeprotocol.Error{Code: "provider_failed"}}); !handled || err != nil {
		t.Fatalf("user-input error handled=%v err=%v", handled, err)
	}
	if err := <-answered; !errors.Is(err, ErrRuntimeInteractionFailed) {
		t.Fatalf("user-input failure = %v", err)
	}
	go func() { answered <- coordinator.ResolveUserInput(context.Background(), answerInput) }()
	command = <-sent
	if handled, err := coordinator.routeRuntimeMessage(key, active, "turn-command", runtimeprotocol.Message{RequestID: command.RequestID, CommandID: command.CommandID, MessageType: "Result"}); !handled || err != nil {
		t.Fatalf("user-input retry handled=%v err=%v", handled, err)
	}
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	if got := coordinator.ActiveMessages(reference); len(got) != 2 {
		t.Fatalf("active transcript = %#v", got)
	}
}

func TestReceiveRuntimeMessagesRejectsPublicLimits(t *testing.T) {
	reads := 0
	accepted := 0
	messages, _, err := receiveRuntimeMessages(func() (runtimeprotocol.Message, error) {
		reads++
		return runtimeprotocol.Message{CommandID: "turn", MessageType: "Progress"}, nil
	}, "turn", nil, func([]runtimeprotocol.Message, runtimeprotocol.Message) error { accepted++; return nil })
	if err == nil || len(messages) != maxRuntimeExecutionMessages || accepted != maxRuntimeExecutionMessages || reads != maxRuntimeExecutionMessages+1 {
		t.Fatalf("count limit: messages=%d accepted=%d reads=%d err=%v", len(messages), accepted, reads, err)
	}
	accepted = 0
	messages, _, err = receiveRuntimeMessages(func() (runtimeprotocol.Message, error) {
		return runtimeprotocol.Message{CommandID: "turn", MessageType: "Progress", Payload: map[string]any{"text": strings.Repeat("x", runtimeprotocol.MaxMessageBytes)}}, nil
	}, "turn", nil, func([]runtimeprotocol.Message, runtimeprotocol.Message) error { accepted++; return nil })
	if err == nil || len(messages) != 0 || accepted != 0 {
		t.Fatalf("byte limit: messages=%d accepted=%d err=%v", len(messages), accepted, err)
	}
}

func TestRuntimeSideEffectReconciliationDigestBindsIdempotencyKey(t *testing.T) {
	input := ReconcileRuntimeSideEffectInput{
		RuntimeExecutionReference: RuntimeExecutionReference{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7},
		CheckpointDigest:          "sha256:" + strings.Repeat("a", 64), Outcome: "confirmed", RequestID: "request-one", IdempotencyKey: "idempotency-key-1",
	}
	digest, err := RuntimeSideEffectReconciliationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	retry := input
	retry.RequestID = "request-two"
	retryDigest, err := RuntimeSideEffectReconciliationDigest(retry)
	if err != nil || retryDigest != digest {
		t.Fatalf("retry digest = %q / %v, want %q", retryDigest, err, digest)
	}
	retry.IdempotencyKey = "idempotency-key-2"
	otherDigest, err := RuntimeSideEffectReconciliationDigest(retry)
	if err != nil || otherDigest == digest {
		t.Fatalf("other digest = %q / %v, must differ from %q", otherDigest, err, digest)
	}
	retry.IdempotencyKey = ""
	if _, err := RuntimeSideEffectReconciliationDigest(retry); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty idempotency key accepted: %v", err)
	}
}

func TestRuntimeTerminalDigestBindsPublicMessage(t *testing.T) {
	original := runtimeprotocol.Message{RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "command", OccurredAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "done", "providerResumeCursor": "private-cursor"}}
	public := publicRuntimeMessage(original)
	if _, exists := public.Payload["providerResumeCursor"]; exists || original.Payload["providerResumeCursor"] != "private-cursor" {
		t.Fatalf("public/original payload = %#v / %#v", public.Payload, original.Payload)
	}
	digest, err := RuntimeMessageDigest(public)
	if err != nil {
		t.Fatal(err)
	}
	progress := public
	progress.MessageType = "Progress"
	progress.Payload = map[string]any{"text": "working"}
	input := CompleteRuntimeExecutionInput{CompleteExecutionInput: CompleteExecutionInput{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7, ResultDigest: digest, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idempotency-key-1234"}}, ProviderResumeCursor: "private-cursor", Messages: []runtimeprotocol.Message{progress, public}}
	if _, err := RuntimeExecutionCompleteMutationDigest(input); err != nil {
		t.Fatal(err)
	}
	input.ResultDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := RuntimeExecutionCompleteMutationDigest(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mismatched terminal digest error = %v", err)
	}
}

func TestRuntimeRecoveryTranscriptAllowsCommandChangeButKeepsFence(t *testing.T) {
	progress := runtimeprotocol.Message{RequestID: "request-1", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "command-1", OccurredAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Progress", Payload: map[string]any{"text": "working"}}
	result := runtimeprotocol.Message{RequestID: "request-2", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "command-2", OccurredAt: time.Date(2026, 8, 29, 10, 0, 1, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "done"}}
	digest, err := RuntimeMessageDigest(result)
	if err != nil {
		t.Fatal(err)
	}
	input := CompleteRuntimeExecutionInput{CompleteExecutionInput: CompleteExecutionInput{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7, ResultDigest: digest, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idempotency-key-1234"}}, Messages: []runtimeprotocol.Message{progress, result}}
	if _, err := RuntimeExecutionCompleteMutationDigest(input); err != nil {
		t.Fatalf("recovery transcript rejected: %v", err)
	}
	for _, field := range []string{"execution", "generation", "terminal-position"} {
		invalid := result
		messages := []runtimeprotocol.Message{progress, invalid}
		switch field {
		case "execution":
			messages[1].ExecutionID = "other-execution"
		case "generation":
			messages[1].Generation--
		case "terminal-position":
			messages = []runtimeprotocol.Message{invalid, progress, result}
		}
		if err := validateRuntimeMessageTranscript(messages, "execution", 7, true); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid %s accepted: %v", field, err)
		}
	}
	result.MessageType = "Error"
	result.Payload = nil
	result.Error = &runtimeprotocol.Error{Code: "provider_unavailable", Message: "unavailable"}
	if _, err := RuntimeMessagesDigest([]runtimeprotocol.Message{progress, result}, "execution", 7); err != nil {
		t.Fatalf("recovery failure transcript rejected: %v", err)
	}
}

func TestRuntimeRecoveryTranscriptAllowsTwoBoundedAttempts(t *testing.T) {
	messages := make([]runtimeprotocol.Message, maxRuntimeExecutionMessages)
	for index := range messages {
		messages[index] = runtimeprotocol.Message{
			RequestID:   fmt.Sprintf("request-%d", index),
			Protocol:    runtimeprotocol.Protocol{Major: 2, Minor: 3},
			ExecutionID: "execution",
			Generation:  7,
			CommandID:   fmt.Sprintf("command-%d", index),
			OccurredAt:  time.Date(2026, 8, 29, 10, 0, index, 0, time.UTC).Format(time.RFC3339Nano),
			MessageType: "Progress",
			Payload:     map[string]any{"text": "working"},
		}
	}
	input := FailRuntimeExecutionInput{
		FailExecutionInput: FailExecutionInput{
			Scope:       Scope{TenantID: "tenant", ProjectID: "project"},
			SessionID:   "session",
			TurnID:      "turn",
			ExecutionID: "execution",
			Generation:  7,
			ErrorCode:   "runtime_turn_failed",
			Mutation:    Mutation{RequestID: "request", IdempotencyKey: "idempotency-key-1234"},
		},
		Messages: messages,
	}
	if _, err := RuntimeExecutionFailMutationDigest(input); err != nil {
		t.Fatalf("bounded recovery transcript rejected: %v", err)
	}
}

func TestRuntimeResultInvalidFailurePreservesReceivedResult(t *testing.T) {
	result := runtimeprotocol.Message{RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "command", OccurredAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "done"}}
	input := FailRuntimeExecutionInput{FailExecutionInput: FailExecutionInput{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7, ErrorCode: "runtime_result_invalid", Mutation: Mutation{RequestID: "request", IdempotencyKey: "idempotency-key-1234"}}, Messages: []runtimeprotocol.Message{result}}
	if _, err := RuntimeExecutionFailMutationDigest(input); err != nil {
		t.Fatal(err)
	}
	input.ErrorCode = "runtime_failed"
	if _, err := RuntimeExecutionFailMutationDigest(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ordinary failure accepted a Result transcript: %v", err)
	}
}

func TestDeriveRuntimeWorkspacePathsScopesSessionStateAndExecutionOutput(t *testing.T) {
	scope := Scope{TenantID: "tenant", ProjectID: "project"}
	first, err := deriveRuntimeWorkspacePaths("/workspace", scope, "session", "turn-a", "execution-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := deriveRuntimeWorkspacePaths("/workspace", scope, "session", "turn-b", "execution-b")
	if err != nil {
		t.Fatal(err)
	}
	wantSessionRoot := filepath.Join("/workspace", ".cloud-agents", "managed-agent", "tenants", "tenant", "projects", "project", "sessions", "session")
	want := runtimeWorkspacePaths{
		workspaceDirectory:     filepath.Join(wantSessionRoot, "workspace"),
		runtimeOutputDirectory: filepath.Join(wantSessionRoot, "runtime-output", "turn-a", "execution-a"),
		providerStateDirectory: filepath.Join(wantSessionRoot, "provider-state"),
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("first paths = %#v, want %#v", first, want)
	}
	if first.workspaceDirectory != second.workspaceDirectory || first.providerStateDirectory != second.providerStateDirectory || first.runtimeOutputDirectory == second.runtimeOutputDirectory {
		t.Fatalf("session/output scoping = first %#v, second %#v", first, second)
	}
	if _, err := deriveRuntimeWorkspacePaths("relative", scope, "session", "turn", "execution"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("relative base error = %v", err)
	}
	if _, err := deriveRuntimeWorkspacePaths("/workspace", scope, "../session", "turn", "execution"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("traversal session error = %v", err)
	}
}

func TestDurableRuntimeExecutionReturnsTerminalReplayWithoutOpeningWorker(t *testing.T) {
	terminal := runtimeprotocol.Message{RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "command", OccurredAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "persisted"}}
	digest, err := RuntimeMessageDigest(terminal)
	if err != nil {
		t.Fatal(err)
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionSucceeded, ResultDigest: digest, Messages: []runtimeprotocol.Message{terminal}}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded || len(result.Messages) != 1 || result.Messages[0].Payload["text"] != "persisted" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 4)
}

func TestDurableRuntimeExecutionUsesMatchingPrecreatedTurn(t *testing.T) {
	scope := Scope{TenantID: "tenant", ProjectID: "project"}
	digest, err := TurnInputDigest("hello")
	if err != nil {
		t.Fatal(err)
	}
	turn := TurnSnapshot{Scope: scope, SessionID: "session", TurnID: "turn", InputDigest: digest, State: TurnQueued}
	store := &durableRuntimeExecutionStoreFake{turn: &turn, execution: ExecutionSnapshot{State: ExecutionSucceeded}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: scope, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded || !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "execution"}) {
		t.Fatalf("result=%#v calls=%v err=%v", result, store.calls, err)
	}
	assertFreshPrincipals(t, store.principals, 3)
}

func TestDurableRuntimeExecutionResolvesPersistedExecutionCapabilityRefs(t *testing.T) {
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionSucceeded}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	skill := SkillBundleRef{BundleID: "skill-bundle", Version: "v1", Digest: "sha256:" + strings.Repeat("b", 64)}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, SkillBundleRefs: []SkillBundleRef{skill},
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "capabilities", "capability-event", "capability-event"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	if len(store.capabilityBindings) != 2 || store.capabilityBindings[0].GetResourceId() != mcp.ServerID || store.capabilityBindings[1].GetResourceId() != skill.BundleID || !store.capabilityBindings[1].GetReadOnly() {
		t.Fatalf("resolved capability bindings=%v", store.capabilityBindings)
	}
}

func TestDurableRuntimeExecutionRecoveryReopensExecutionCapabilities(t *testing.T) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	skill := SkillBundleRef{BundleID: "skill-bundle", Version: "v1", Digest: "sha256:" + strings.Repeat("b", 64)}
	worker := &runtimeWorkerFake{identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}, now: now}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{
		State:         ExecutionRunning,
		Messages:      []runtimeprotocol.Message{{MessageType: "Progress", Payload: map[string]any{"text": "checkpointed"}}},
		McpServerRefs: []McpServerRef{mcp}, SkillBundleRefs: []SkillBundleRef{skill},
	}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "resume",
		McpServerRefs: []McpServerRef{mcp}, SkillBundleRefs: []SkillBundleRef{skill},
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(worker.openBindings) != 2 || worker.openBindings[0].GetResourceId() != mcp.ServerID || worker.openBindings[1].GetResourceId() != skill.BundleID || worker.openManifestDigest != "sha256:test" {
		t.Fatalf("recovery Runtime open capabilities=%v digest=%q", worker.openBindings, worker.openManifestDigest)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "capabilities", "capability-event", "capability-event", "claim", "complete"}) {
		t.Fatalf("recovery calls=%v", store.calls)
	}
}

func TestDurableRuntimeExecutionSettlesInvalidCompletion(t *testing.T) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	worker := &runtimeWorkerFake{identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}, now: now}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}, completeError: ErrInvalidInput}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrDurableRuntimeExecutionFailed) || result.Transition.Execution.State != ExecutionFailed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "claim", "start", "complete", "fail"}) {
		t.Fatalf("calls=%v", store.calls)
	}
}

func TestDurableRuntimeExecutionAuditsCapabilityResolutionFailureBeforeRuntimeOpen(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name      string
		failure   *CapabilityResolutionFailure
		mcp       []McpServerRef
		skills    []SkillBundleRef
		operation string
		result    string
	}{
		{name: "revoked MCP", failure: &CapabilityResolutionFailure{Resource: ResourceMcpServer, ResourceID: "mcp-server", Version: "v1", Digest: digest, Reason: "revoked"}, mcp: []McpServerRef{{ServerID: "mcp-server", Version: "v1", Digest: digest}}, operation: "mcp.revoke", result: "revoked"},
		{name: "incompatible Skill", failure: &CapabilityResolutionFailure{Resource: ResourceSkillBundle, ResourceID: "skill-bundle", Version: "v1", Digest: digest, Reason: "provider_incompatible"}, skills: []SkillBundleRef{{BundleID: "skill-bundle", Version: "v1", Digest: digest}}, operation: "skill.fail", result: "failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}, capabilityError: test.failure}
			coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
				Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC) },
				FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
				Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
				McpServerRefs: test.mcp, SkillBundleRefs: test.skills, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
			})
			if !errors.Is(err, test.failure) {
				t.Fatalf("error=%v", err)
			}
			if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "capabilities", "capability-event"}) {
				t.Fatalf("calls=%v", store.calls)
			}
			if len(store.capabilityEvents) != 1 || store.capabilityEvents[0].Operation != test.operation || store.capabilityEvents[0].Result != test.result || store.capabilityEvents[0].ErrorCode != "capability_"+test.failure.Reason {
				t.Fatalf("capability events=%+v", store.capabilityEvents)
			}
		})
	}
}

func TestDurableRuntimeExecutionRejectsMismatchedPrecreatedTurn(t *testing.T) {
	scope := Scope{TenantID: "tenant", ProjectID: "project"}
	digest, err := TurnInputDigest("different")
	if err != nil {
		t.Fatal(err)
	}
	turn := TurnSnapshot{Scope: scope, SessionID: "session", TurnID: "turn", InputDigest: digest, State: TurnQueued}
	store := &durableRuntimeExecutionStoreFake{turn: &turn}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: time.Now,
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: scope, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrDurableRuntimeExecutionConflict) || !reflect.DeepEqual(store.calls, []string{"session", "find-turn"}) {
		t.Fatalf("calls=%v err=%v", store.calls, err)
	}
}

func TestDurableRuntimeExecutionLeavesQueuedExecutionRetryableWhenWorkerIsFull(t *testing.T) {
	now := time.Date(2026, 9, 1, 4, 0, 0, 0, time.UTC)
	supervisor := newRuntimeTestSupervisor(t, now, true)
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: supervisor, Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeCapacityExhausted) || result.Transition.Execution.State != ExecutionQueued {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "claim", "release-claim"}) {
		t.Fatalf("capacity exhaustion mutated running state: calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 6)
}

func TestDurableRuntimeExecutionRequiresReconciliationWhenRunningCheckpointIsMissing(t *testing.T) {
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionRunning}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeRecoveryRequiresUserAction) || result.Transition.Execution.State != ExecutionRunning || result.Transition.Execution.RecoveryReason != "checkpoint_missing" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "claim"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 5)
}

func TestDurableRuntimeExecutionFencesWriterBeforeSideEffectReconciliation(t *testing.T) {
	now := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	opened := make(chan struct{}, 1)
	worker := &runtimeWorkerFake{
		identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		now:      now, opened: opened,
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{
		State: ExecutionRunning, PendingSideEffect: true,
		Messages: []runtimeprotocol.Message{{MessageType: "Event", Payload: map[string]any{"eventType": "item.started"}}},
	}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeRecoveryRequiresUserAction) || result.Transition.Execution.RecoveryReason != "side_effect_outcome_unknown" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	select {
	case <-opened:
	default:
		t.Fatal("previous Runtime writer was not fenced before reconciliation")
	}
}

func TestDurableRuntimeExecutionFencesWriterBeforeResolvingRecoveredInteraction(t *testing.T) {
	now := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	opened := make(chan struct{}, 1)
	worker := &runtimeWorkerFake{
		identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		now:      now, opened: opened,
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{
		State: ExecutionRunning, ClaimExpiresAt: now.Add(-time.Second), PendingInteractionCount: 1,
		Messages: []runtimeprotocol.Message{{MessageType: "InteractionRequest", Payload: map[string]any{"requestId": "approval-1", "interactionType": "approval"}}},
	}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeRecoveryRequiresUserAction) || result.Transition.Execution.PendingInteractionCount != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	select {
	case <-opened:
	default:
		t.Fatal("previous Runtime writer was not fenced before recovered interaction resolution")
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution"}) {
		t.Fatalf("interaction fence unexpectedly claimed execution: calls=%v", store.calls)
	}
}

func TestDurableRuntimeExecutionLeavesRecoveredExecutionRunningWhenWorkerOpenFails(t *testing.T) {
	message := runtimeprotocol.Message{CommandID: "turn", MessageType: "Progress", Payload: map[string]any{"text": "checkpointed"}}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionRunning, Messages: []runtimeprotocol.Message{message}, RecoveryMode: "cross-node-takeover", RecoverySourceTargetID: "source", RecoveryTargetID: "target"}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeEnvironmentUnavailable) || result.Transition.Execution.State != ExecutionRunning || result.Transition.Execution.RecoveryMode != "cross-node-takeover" || result.Transition.Execution.RecoverySourceTargetID != "source" || result.Transition.Execution.RecoveryTargetID != "target" || !reflect.DeepEqual(result.Messages, []runtimeprotocol.Message{message}) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "claim"}) {
		t.Fatalf("failed recovery mutated durable execution: calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 5)
}

func TestDurableRuntimeExecutionLeavesCheckpointedExecutionRunningWhenWorkerExits(t *testing.T) {
	now := time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)
	worker := &runtimeWorkerFake{
		identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		now:      now, disconnectAfterCheckpoint: true, disconnectCode: connect.CodeInternal,
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeEnvironmentUnavailable) || result.Transition.Execution.State != ExecutionRunning || len(result.Messages) != 1 || result.Messages[0].Payload["text"] != "checkpointed" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "claim", "start", "checkpoint"}) {
		t.Fatalf("checkpointed execution was settled after Worker exit: calls=%v", store.calls)
	}
}

func TestDurableRuntimeExecutionPreservesPendingCapabilityWhenWorkerExits(t *testing.T) {
	now := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	mcp := McpServerRef{ServerID: "mcp-server", Version: "v1", Digest: "sha256:" + strings.Repeat("a", 64)}
	worker := &runtimeWorkerFake{
		identity: &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		now:      now, disconnectAfterCheckpoint: true, disconnectCode: connect.CodeInternal,
		capabilityResourceID: mcp.ServerID, capabilityStartedOnly: true,
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisorForWire(t, worker), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		McpServerRefs: []McpServerRef{mcp}, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrRuntimeRecoveryRequiresUserAction) || result.Transition.Execution.State != ExecutionRunning || !store.execution.PendingSideEffect || slices.Contains(store.calls, "complete") || slices.Contains(store.calls, "fail") {
		t.Fatalf("result=%#v calls=%v err=%v", result, store.calls, err)
	}
	if len(store.capabilityEvents) != 2 || store.capabilityEvents[1].Operation != "mcp.fail" || store.capabilityEvents[1].ErrorCode != "capability_call_unknown" {
		t.Fatalf("capability events=%+v", store.capabilityEvents)
	}
}

func TestDurableRuntimeExecutionOutlivesCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}, cancel: cancel}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: newRuntimeTestSupervisor(t, now, false), Clock: func() time.Time { return now },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(ctx, testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded || ctx.Err() != context.Canceled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "claim", "start", "complete"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 7)
}

func TestDurableRuntimeExecutionCancelSignalsActiveRuntime(t *testing.T) {
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionRunning}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	activeContext, activeCancel := context.WithCancel(context.Background())
	key := durableExecutionKey{tenantID: "tenant", projectID: "project", sessionID: "session", turnID: "turn", executionID: "execution", generation: 7}
	active, unregister, err := coordinator.registerActiveExecution(key, activeCancel)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	active.stop = func() { stopped = true }
	_, err = coordinator.Cancel(context.Background(), nil, CancelTurnInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", TargetExecutionID: "execution", Generation: 7,
		Mutation: Mutation{RequestID: "cancel-request", IdempotencyKey: "cancel-idem"},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-activeContext.Done():
	case <-time.After(time.Second):
		t.Fatal("active runtime context was not cancelled")
	}
	if !stopped {
		t.Fatal("active runtime stream was not stopped")
	}
	if !unregister() {
		t.Fatal("active cancellation was not recorded")
	}
}

func TestDurableRuntimeExecutionRejectsDuplicateActiveExecution(t *testing.T) {
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: &durableRuntimeExecutionStoreFake{}, Supervisor: &workerclient.Supervisor{}, Clock: time.Now,
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	key := durableExecutionKey{tenantID: "tenant", projectID: "project", sessionID: "session", turnID: "turn", executionID: "execution", generation: 7}
	_, first, err := coordinator.registerActiveExecution(key, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	if _, _, err := coordinator.registerActiveExecution(key, func() {}); !errors.Is(err, ErrDurableRuntimeExecutionConflict) {
		t.Fatalf("duplicate active execution error = %v", err)
	}
}

func TestRuntimeArtifactCandidateUsesPersistedExecutionAuthority(t *testing.T) {
	message := runtimeprotocol.Message{
		RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "turn",
		OccurredAt: "2026-09-01T08:00:00Z", MessageType: "ArtifactCandidate",
		Payload: map[string]any{"artifact": map[string]any{"path": "provider-diffs/result.diff", "kind": "diff", "sourceRoot": "runtime-output", "contentType": "text/x-diff", "reportedSize": float64(10), "sha256": strings.Repeat("a", 64)}},
	}
	input := RuntimeArtifactReadInput{RuntimeExecutionReference: RuntimeExecutionReference{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7}, Message: message}
	candidate, err := runtimeArtifactCandidate(input)
	if err != nil || candidate.sourceRoot != "runtime-output" || candidate.relativePath != "provider-diffs/result.diff" || candidate.expectedSize == nil || *candidate.expectedSize != 10 || candidate.sha256 != strings.Repeat("a", 64) {
		t.Fatalf("candidate=%#v err=%v", candidate, err)
	}
	message.Payload["artifact"].(map[string]any)["path"] = "../secret"
	input.Message = message
	if _, err := runtimeArtifactCandidate(input); !errors.Is(err, ErrRuntimeArtifactUnavailable) {
		t.Fatalf("escaping candidate error=%v", err)
	}
}
