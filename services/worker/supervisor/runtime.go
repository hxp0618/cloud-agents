package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	runtimeprocess "github.com/hxp0618/cloud-agents/services/worker/runtime"
	"google.golang.org/protobuf/proto"
)

// RuntimeSession is a Supervisor-owned stream to one fenced Worker Runtime
// process. Send and Receive may run concurrently, but each direction is
// serialized as required by Connect.
type RuntimeSession struct {
	stream     *connect.BidiStreamForClient[workerruntimev1alpha1.RuntimeSessionRequest, workerruntimev1alpha1.RuntimeSessionResponse]
	execution  string
	generation uint64
	sendMu     sync.Mutex
}

// BindRuntime negotiates the Worker capability required by the Runtime route.
func (s *Supervisor) BindRuntime(ctx context.Context) (BindingSnapshot, error) {
	if s == nil || !runtimeClientAvailable(s.runtimeClient) {
		return BindingSnapshot{}, errInvalidConfig
	}
	return s.BindOperationAdmission(ctx)
}

// CheckRuntimeHealth refreshes an expired Runtime binding before checking the
// Worker health endpoint. It is suitable for readiness probes that should
// stop routing work when the Worker is unavailable.
func (s *Supervisor) CheckRuntimeHealth(ctx context.Context) error {
	if err := s.ensureRuntimeBinding(ctx); err != nil {
		return err
	}
	_, err := s.CheckHealth(ctx)
	return err
}

// OpenRuntimeSession starts a Worker-side Runtime process after binding the
// current negotiation, expected Worker identity, and fencing proof.
func (s *Supervisor) OpenRuntimeSession(ctx context.Context, executionID, providerKind string, generation uint64, fencing *workerv1alpha1.FencingProof) (*RuntimeSession, error) {
	if s == nil || !runtimeClientAvailable(s.runtimeClient) || !validIdentity(s.workerIdentity) || executionID == "" || providerKind == "" || generation == 0 || fencing == nil {
		return nil, errInvalidConfig
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureRuntimeBinding(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	state := cloneBindingState(s.binding)
	s.mu.RUnlock()
	if state == nil || state.profileID != OperationAdmissionProfileID {
		return nil, fail(connect.CodeFailedPrecondition, "runtime_binding_required")
	}
	now, ok := s.nowUTC()
	if !ok || !now.Before(state.expiresAt) {
		return nil, fail(connect.CodeDeadlineExceeded, "binding_expired")
	}
	if fencing.GetGeneration() != generation || fencing.GetLeaseId() == "" || len(fencing.GetToken()) == 0 {
		return nil, fail(connect.CodeInvalidArgument, "runtime_fencing_invalid")
	}
	stream := s.runtimeClient.OpenSession(ctx)
	open := &workerruntimev1alpha1.RuntimeSessionRequest{Frame: &workerruntimev1alpha1.RuntimeSessionRequest_Open{Open: &workerruntimev1alpha1.RuntimeSessionOpen{
		Negotiation: state.negotiationBinding(), Fencing: proto.Clone(fencing).(*workerv1alpha1.FencingProof), ExecutionId: executionID, Generation: generation, ExpectedWorkerIdentity: cloneIdentity(s.workerIdentity), ProviderKind: providerKind,
	}}}
	if err := stream.Send(open); err != nil {
		return nil, rpcFailure("runtime_open", err)
	}
	ready, err := stream.Receive()
	if err != nil {
		return nil, rpcFailure("runtime_ready", err)
	}
	if ready == nil || ready.GetReady() == nil || ready.GetReady().GetExecutionId() != executionID || ready.GetReady().GetGeneration() != generation || ready.GetReady().GetProtocolMajor() != runtimeprocess.ProtocolMajor || ready.GetReady().GetProtocolMinor() != runtimeprocess.ProtocolMinor {
		return nil, fail(connect.CodeInternal, "runtime_ready_invalid")
	}
	return &RuntimeSession{stream: stream, execution: executionID, generation: generation}, nil
}

func (s *Supervisor) ensureRuntimeBinding(ctx context.Context) error {
	if binding, ok := s.CurrentBinding(); ok && binding.ProfileID == OperationAdmissionProfileID {
		return nil
	}
	_, err := s.BindRuntime(ctx)
	return err
}

func (session *RuntimeSession) Send(ctx context.Context, command runtimeprocess.Command) error {
	if session == nil || session.stream == nil {
		return errInvalidConfig
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if command.ExecutionID != session.execution || command.Generation != session.generation {
		return fail(connect.CodeInvalidArgument, "runtime_command_identity_mismatch")
	}
	if err := runtimeprocess.ValidateCommand(command); err != nil {
		return fail(connect.CodeInvalidArgument, "runtime_command_invalid")
	}
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) > runtimeprocess.MaxCommandBytes {
		return fail(connect.CodeInvalidArgument, "runtime_command_too_large")
	}
	session.sendMu.Lock()
	defer session.sendMu.Unlock()
	return session.stream.Send(&workerruntimev1alpha1.RuntimeSessionRequest{Frame: &workerruntimev1alpha1.RuntimeSessionRequest_Command{Command: &workerruntimev1alpha1.RuntimeCommandFrame{Json: encoded}}})
}

func (session *RuntimeSession) Receive() (runtimeprocess.Message, error) {
	if session == nil || session.stream == nil {
		return runtimeprocess.Message{}, errInvalidConfig
	}
	response, err := session.stream.Receive()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return runtimeprocess.Message{}, io.EOF
		}
		return runtimeprocess.Message{}, rpcFailure("runtime_receive", err)
	}
	if response == nil {
		return runtimeprocess.Message{}, fail(connect.CodeInternal, "runtime_response_missing")
	}
	if runtimeError := response.GetError(); runtimeError != nil {
		return runtimeprocess.Message{}, fmt.Errorf("runtime %s: %s", runtimeError.GetCode(), runtimeError.GetMessage())
	}
	if len(response.GetJson()) == 0 || len(response.GetJson()) > runtimeprocess.MaxMessageBytes {
		return runtimeprocess.Message{}, fail(connect.CodeInternal, "runtime_message_invalid")
	}
	var message runtimeprocess.Message
	if err := json.Unmarshal(response.GetJson(), &message); err != nil || runtimeprocess.ValidateMessage(message) != nil {
		return runtimeprocess.Message{}, fail(connect.CodeInternal, "runtime_message_invalid")
	}
	if message.ExecutionID != session.execution || message.Generation != session.generation {
		return runtimeprocess.Message{}, fail(connect.CodePermissionDenied, "runtime_message_identity_mismatch")
	}
	return message, nil
}

func (session *RuntimeSession) CloseRequest() error {
	if session == nil || session.stream == nil {
		return nil
	}
	session.sendMu.Lock()
	defer session.sendMu.Unlock()
	return session.stream.CloseRequest()
}

func (session *RuntimeSession) CloseResponse() error {
	if session == nil || session.stream == nil {
		return nil
	}
	return session.stream.CloseResponse()
}

func runtimeClientAvailable(client workerruntimev1alpha1connect.WorkerRuntimeServiceClient) bool {
	if client == nil {
		return false
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}
