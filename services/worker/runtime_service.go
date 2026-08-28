package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	runtimeprocess "github.com/hxp0618/cloud-agents/services/worker/runtime"
)

// OpenSession is the Worker-side bridge from an authenticated Supervisor to
// one local Cloud Agent Runtime process. Runtime JSON remains the source of
// truth; protobuf carries only transport identity and fencing.
func (s *Service) OpenSession(ctx context.Context, stream *connect.BidiStream[workerruntimev1alpha1.RuntimeSessionRequest, workerruntimev1alpha1.RuntimeSessionResponse]) error {
	if !s.ready() || len(s.runtimeCommand) == 0 {
		return runtimeSessionFailure(connect.CodeUnimplemented, "runtime_not_configured", "Runtime command is not configured")
	}
	if stream == nil {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "stream_required", "Runtime stream is required")
	}
	openRequest, err := stream.Receive()
	if err != nil {
		return err
	}
	if openRequest == nil || openRequest.GetOpen() == nil || openRequest.GetCommand() != nil {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "open_required", "the first Runtime frame must open the session")
	}
	open := openRequest.GetOpen()
	clientIdentity, err := s.identity.ClientIdentity(ctx)
	if err != nil || clientIdentity == nil {
		return runtimeSessionFailure(connect.CodeUnauthenticated, "transport_identity_missing", "authenticated client identity is required")
	}
	if err := validateIdentity(clientIdentity); err != nil {
		return runtimeSessionFailure(connect.CodeUnauthenticated, "invalid_transport_identity", "authenticated client identity is invalid")
	}
	if err := validateExpectedIdentity(open.GetExpectedWorkerIdentity(), s.workerIdentity); err != nil {
		return err
	}
	binding, err := s.validateBinding(open.GetNegotiation(), clientIdentity)
	if err != nil {
		return err
	}
	if _, ok := binding.caps[workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH]; !ok {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_not_negotiated", "Runtime sessions require operation dispatch capability")
	}
	if err := s.validateRuntimeFencing(open); err != nil {
		return err
	}
	if err := validateIdentifier(open.GetExecutionId(), "execution_id"); err != nil || open.GetGeneration() == 0 || open.GetGeneration() != open.GetFencing().GetGeneration() {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "runtime_identity_invalid", "execution id and generation are invalid")
	}
	client, err := runtimeprocess.New(ctx, runtimeprocess.Config{
		Command: s.runtimeCommand, Environment: s.runtimeEnvironment, Directory: s.runtimeDirectory,
	})
	if err != nil {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "runtime_start_failed", "Runtime process could not be started")
	}
	defer func() { _ = client.Close(context.Background()) }()

	var sendMu sync.Mutex
	send := func(response *workerruntimev1alpha1.RuntimeSessionResponse) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(response)
	}
	if err := send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Ready{Ready: &workerruntimev1alpha1.RuntimeSessionReady{
		ExecutionId: open.GetExecutionId(), Generation: open.GetGeneration(), ProtocolMajor: runtimeprocess.ProtocolMajor, ProtocolMinor: runtimeprocess.ProtocolMinor,
	}}}); err != nil {
		return err
	}

	sessionContext, cancel := context.WithCancel(ctx)
	defer cancel()
	var commands sync.WaitGroup
	eventsDone := make(chan error, 1)
	var sessionErr error
	go func() {
		for {
			select {
			case <-sessionContext.Done():
				eventsDone <- nil
				return
			case message, ok := <-client.Events():
				if !ok {
					eventsDone <- nil
					return
				}
				if err := sendRuntimeJSON(send, message); err != nil {
					cancel()
					eventsDone <- err
					return
				}
			}
		}
	}()
	for {
		request, receiveErr := stream.Receive()
		if receiveErr != nil {
			if errors.Is(receiveErr, io.EOF) || sessionContext.Err() != nil {
				break
			}
			sessionErr = receiveErr
			cancel()
			break
		}
		if request == nil || request.GetCommand() == nil || request.GetOpen() != nil {
			_ = sendRuntimeError(send, "command_required", "only Runtime command frames are allowed after open")
			sessionErr = runtimeSessionFailure(connect.CodeInvalidArgument, "command_required", "only Runtime command frames are allowed after open")
			cancel()
			break
		}
		frame := request.GetCommand().GetJson()
		if len(frame) == 0 || len(frame) > runtimeprocess.MaxCommandBytes {
			_ = sendRuntimeError(send, "command_invalid", "Runtime command size is invalid")
			sessionErr = runtimeSessionFailure(connect.CodeInvalidArgument, "command_invalid", "Runtime command size is invalid")
			cancel()
			break
		}
		var command runtimeprocess.Command
		if err := json.Unmarshal(frame, &command); err != nil || command.ExecutionID != open.GetExecutionId() || command.Generation != open.GetGeneration() {
			_ = sendRuntimeError(send, "command_invalid", "Runtime command envelope is invalid")
			sessionErr = runtimeSessionFailure(connect.CodeInvalidArgument, "command_invalid", "Runtime command envelope is invalid")
			cancel()
			break
		}
		commands.Add(1)
		go func(command runtimeprocess.Command) {
			defer commands.Done()
			message, executeErr := client.Execute(sessionContext, command)
			if executeErr != nil && message.MessageType == "" {
				_ = sendRuntimeError(send, "runtime_execution_failed", "Runtime command failed")
				cancel()
				return
			}
			if err := sendRuntimeJSON(send, message); err != nil {
				cancel()
			}
		}(command)
	}
	cancel()
	_ = client.Close(context.Background())
	commands.Wait()
	if eventErr := <-eventsDone; eventErr != nil {
		return eventErr
	}
	return sessionErr
}

func (s *Service) validateRuntimeFencing(open *workerruntimev1alpha1.RuntimeSessionOpen) error {
	fencing := open.GetFencing()
	if fencing == nil || fencing.GetLeaseId() == "" || fencing.GetGeneration() == 0 || len(fencing.GetToken()) == 0 || len(fencing.GetToken()) > int(MaxPayloadBytes) {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "fencing_required", "Runtime fencing proof is required")
	}
	if err := validateIdentifier(fencing.GetLeaseId(), "lease_id"); err != nil {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "lease_id_invalid", "Runtime lease id is invalid")
	}
	if s.admissionLeaseID == "" || s.admissionGeneration == 0 {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "generation_authority_missing", "Runtime fencing authority is not configured")
	}
	if fencing.GetLeaseId() != s.admissionLeaseID {
		return runtimeSessionFailure(connect.CodePermissionDenied, "lease_mismatch", "Runtime lease does not match the Worker authority")
	}
	if fencing.GetGeneration() != s.admissionGeneration {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "stale_generation", "Runtime generation does not match the Worker authority")
	}
	return nil
}

func sendRuntimeJSON(send func(*workerruntimev1alpha1.RuntimeSessionResponse) error, message runtimeprocess.Message) error {
	encoded, err := json.Marshal(message)
	if err != nil || len(encoded) > runtimeprocess.MaxMessageBytes {
		return runtimeSessionFailure(connect.CodeInternal, "runtime_message_invalid", "Runtime message is invalid")
	}
	return send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Json{Json: encoded}})
}

func sendRuntimeError(send func(*workerruntimev1alpha1.RuntimeSessionResponse) error, code, message string) error {
	return send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Error{Error: &workerruntimev1alpha1.RuntimeSessionError{Code: code, Message: message}}})
}

func runtimeSessionFailure(code connect.Code, stable, message string) error {
	return connect.NewError(code, fmt.Errorf("worker/runtime_%s: %s", stable, message))
}

var _ workerruntimev1alpha1connect.WorkerRuntimeServiceHandler = (*Service)(nil)

// NewRuntimeHandler returns the separately mounted Runtime stream route. It
// deliberately does not widen the existing Worker v1alpha1 handler.
func NewRuntimeHandler(svc *Service, opts ...connect.HandlerOption) (string, http.Handler) {
	var impl workerruntimev1alpha1connect.WorkerRuntimeServiceHandler = svc
	if svc == nil {
		impl = workerruntimev1alpha1connect.UnimplementedWorkerRuntimeServiceHandler{}
	}
	opts = append(opts, connect.WithReadMaxBytes(int(MaxCommandBytesForRuntimeFrame)), connect.WithSendMaxBytes(int(MaxMessageBytesForRuntimeFrame)))
	return workerruntimev1alpha1connect.NewWorkerRuntimeServiceHandler(impl, opts...)
}

const (
	MaxCommandBytesForRuntimeFrame = runtimeprocess.MaxCommandBytes + 4096
	MaxMessageBytesForRuntimeFrame = runtimeprocess.MaxMessageBytes + 4096
)
