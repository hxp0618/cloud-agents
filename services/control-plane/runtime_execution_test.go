package controlplane_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	controlplane "github.com/hxp0618/cloud-agents/services/control-plane"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
	runtimeprocess "github.com/hxp0618/cloud-agents/services/worker/runtime"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
)

func TestRuntimeExecutionCoordinator(t *testing.T) {
	if os.Getenv("CLOUD_AGENTS_RUNTIME_EXECUTION_HELPER") == "1" {
		runRuntimeExecutionHelper()
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	workerIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/runtime", TrustDomain: "cloud-agents.test"}
	supervisorIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor/runtime", TrustDomain: "cloud-agents.test"}
	worker, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity: workerIdentity,
		Capabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
			workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		},
		IdentityProvider:    workerkernel.StaticIdentityProvider{Identity: supervisorIdentity},
		AdmissionLeaseID:    "runtime-lease",
		AdmissionGeneration: 7,
		AdmissionToken:      []byte("runtime-token"),
		Clock:               func() time.Time { return now },
		RuntimeCommand:      []string{os.Args[0], "-test.run=TestRuntimeExecutionCoordinator", "--"},
		RuntimeEnvironment:  append(os.Environ(), "CLOUD_AGENTS_RUNTIME_EXECUTION_HELPER=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	workerPath, workerHandler := workerkernel.NewHandler(worker)
	runtimePath, runtimeHandler := workerkernel.NewRuntimeHandler(worker)
	mux := http.NewServeMux()
	mux.Handle(workerPath, workerHandler)
	mux.Handle(runtimePath, runtimeHandler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	workerClient := workerv1alpha1connect.NewWorkerExecutionServiceClient(server.Client(), server.URL)
	runtimeClient := workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(server.Client(), server.URL)
	workerSupervisor, err := supervisor.New(supervisor.Config{Client: workerClient, RuntimeClient: runtimeClient, ExpectedWorkerIdentity: workerIdentity, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workerSupervisor.BindRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}

	service, err := controlplane.NewManagedAgentService()
	if err != nil {
		t.Fatal(err)
	}
	scope := controlplane.Scope{TenantID: "tenant-runtime", ProjectID: "project-runtime"}
	if _, err := service.CreateSession(context.Background(), controlplane.CreateSessionInput{
		Scope: scope, SessionID: "session-runtime", ProviderKind: "codex",
		Mutation: controlplane.Mutation{RequestID: "request-session", IdempotencyKey: "session-key"},
	}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := service.NewRuntimeExecutionCoordinator(controlplane.RuntimeExecutionConfig{
		Supervisor: workerSupervisor, Clock: func() time.Time { return now },
		FencingLeaseID: "runtime-lease", FencingGeneration: 7, FencingToken: []byte("runtime-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), controlplane.RuntimeExecutionInput{
		Scope: scope, SessionID: "session-runtime", TurnID: "turn-runtime", ExecutionID: "execution-runtime", Generation: 7,
		FencingLeaseID: "runtime-lease", FencingGeneration: 7, FencingToken: []byte("runtime-token"), Deadline: now.Add(20 * time.Second),
		WorkspaceDirectory: "/tmp/cloud-agents-runtime-test", Model: "test-model", InputText: "hello runtime",
		Mutation: controlplane.Mutation{RequestID: "request-execution", IdempotencyKey: "execution-key"},
	})
	if err != nil || result.Transition.Turn.State != controlplane.TurnCompleted || result.Transition.Execution.State != controlplane.ExecutionSucceeded || len(result.Messages) != 3 {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if result.Messages[1].MessageType != "Event" || result.Messages[2].MessageType != "Result" || result.Messages[2].Payload["inputText"] != "hello runtime" {
		t.Fatalf("runtime messages = %#v", result.Messages)
	}
	if result.Messages[0].RequestID != "request-execution-startsession" || result.Messages[0].CommandID != "request-execution-start" || result.Messages[2].RequestID != "request-execution-sendturn" || result.Messages[2].CommandID != "request-execution-turn" {
		t.Fatalf("runtime correlation IDs = %#v", result.Messages)
	}
	execution := result.Transition.Execution
	responseBody, err := json.Marshal(map[string]any{
		"apiVersion": "managed-agent.cloud-agents.dev/v1alpha1",
		"kind":       "Execution",
		"metadata": map[string]any{
			"uid":             execution.ExecutionID,
			"projectId":       execution.Scope.ProjectID,
			"sessionId":       execution.SessionID,
			"turnId":          execution.TurnID,
			"resourceVersion": fmt.Sprintf("%d", execution.Version),
			"createdAt":       execution.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updatedAt":       execution.UpdatedAt.UTC().Format(time.RFC3339Nano),
		},
		"spec": map[string]any{
			"generation":   execution.Generation,
			"state":        execution.State,
			"resultDigest": execution.ResultDigest,
		},
		"messages": result.Messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openapiv1alpha1.DecodeManagedAgentExecutionResponseJSON(responseBody); err != nil {
		t.Fatalf("public execution response rejected Runtime messages: %v; body=%s", err, responseBody)
	}
	failure, failureErr := coordinator.Execute(context.Background(), controlplane.RuntimeExecutionInput{
		Scope: scope, SessionID: "session-runtime", TurnID: "turn-runtime-failure", ExecutionID: "execution-runtime-failure", Generation: 7,
		FencingLeaseID: "runtime-lease", FencingGeneration: 7, FencingToken: []byte("runtime-token"), Deadline: now.Add(20 * time.Second),
		WorkspaceDirectory: "/tmp/cloud-agents-runtime-test", Model: "test-model", InputText: "fail",
		Mutation: controlplane.Mutation{RequestID: "request-failure", IdempotencyKey: "failure-key"},
	})
	if failureErr == nil || failure.Transition.Turn.State != controlplane.TurnFailed || failure.Transition.Execution.State != controlplane.ExecutionFailed {
		t.Fatalf("failure = %#v, err = %v", failure, failureErr)
	}
}

func runRuntimeExecutionHelper() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command runtimeprocess.Command
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			continue
		}
		if command.CommandType == "SendTurn" {
			if command.Payload["inputText"] == "fail" {
				_, _ = fmt.Fprintf(os.Stdout, `{"requestId":%q,"protocolVersion":{"major":2,"minor":3},"executionId":%q,"generation":%d,"commandId":%q,"occurredAt":"2026-08-29T10:00:00Z","messageType":"Error","error":{"code":"provider_failed","message":"provider failed","retryable":false,"requiresNewExecution":true,"requiresUserAction":false,"canReconstructFromHistory":true,"canMoveWorker":true}}
`, command.RequestID, command.ExecutionID, command.Generation, command.CommandID)
				continue
			}
			_, _ = fmt.Fprintf(os.Stdout, `{"requestId":%q,"protocolVersion":{"major":2,"minor":3},"executionId":%q,"generation":%d,"commandId":%q,"occurredAt":"2026-08-29T10:00:00Z","messageType":"Event","payload":{"text":"partial"}}
`, command.RequestID, command.ExecutionID, command.Generation, command.CommandID)
			time.Sleep(10 * time.Millisecond)
			_, _ = fmt.Fprintf(os.Stdout, `{"requestId":%q,"protocolVersion":{"major":2,"minor":3},"executionId":%q,"generation":%d,"commandId":%q,"occurredAt":"2026-08-29T10:00:00Z","messageType":"Result","payload":{"inputText":%q}}
`, command.RequestID, command.ExecutionID, command.Generation, command.CommandID, command.Payload["inputText"])
			continue
		}
		_, _ = fmt.Fprintf(os.Stdout, `{"requestId":%q,"protocolVersion":{"major":2,"minor":3},"executionId":%q,"generation":%d,"commandId":%q,"occurredAt":"2026-08-29T10:00:00Z","messageType":"Result","payload":{"ok":true}}
`, command.RequestID, command.ExecutionID, command.Generation, command.CommandID)
	}
	os.Exit(0)
}
