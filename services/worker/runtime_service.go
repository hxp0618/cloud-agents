package worker

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	runtimeprocess "github.com/hxp0618/cloud-agents/services/worker/runtime"
)

// OpenSession is the Worker-side bridge from an authenticated Supervisor to
// one local Cloud Agent Runtime process. Runtime JSON remains the source of
// truth; protobuf carries transport identity, fencing, and Provider binding.
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
	_, negotiation := binding.caps[workerv1alpha1.Capability_CAPABILITY_NEGOTIATION]
	_, health := binding.caps[workerv1alpha1.Capability_CAPABILITY_HEALTH]
	if !negotiation || !health {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_not_negotiated", "Runtime sessions require the negotiation and health capabilities")
	}
	if err := s.validateRuntimeFencing(open.GetFencing(), open.GetGeneration()); err != nil {
		return err
	}
	if err := validateIdentifier(open.GetExecutionId(), "execution_id"); err != nil || open.GetGeneration() == 0 || open.GetGeneration() != open.GetFencing().GetGeneration() {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "runtime_identity_invalid", "execution id and generation are invalid")
	}
	credentialFile, err := runtimeProviderCredentialFile(s.runtimeCredentialDirectory, open.GetTenantId(), open.GetProviderKind())
	if err != nil {
		return err
	}
	select {
	case s.runtimeSlots <- struct{}{}:
		defer func() { <-s.runtimeSlots }()
	default:
		return runtimeSessionFailure(connect.CodeResourceExhausted, "capacity_exhausted", "Runtime session capacity is exhausted")
	}
	client, err := runtimeprocess.New(ctx, runtimeprocess.Config{
		Command: s.runtimeCommand, Environment: s.runtimeEnvironment, Directory: s.runtimeDirectory, CredentialFile: credentialFile,
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
		ExecutionId: open.GetExecutionId(), Generation: open.GetGeneration(), ProtocolMajor: runtimeprotocol.ProtocolMajor, ProtocolMinor: runtimeprotocol.ProtocolMinor,
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
		if len(frame) == 0 || len(frame) > runtimeprotocol.MaxCommandBytes {
			_ = sendRuntimeError(send, "command_invalid", "Runtime command size is invalid")
			sessionErr = runtimeSessionFailure(connect.CodeInvalidArgument, "command_invalid", "Runtime command size is invalid")
			cancel()
			break
		}
		var command runtimeprotocol.Command
		if err := json.Unmarshal(frame, &command); err != nil || command.ExecutionID != open.GetExecutionId() || command.Generation != open.GetGeneration() {
			_ = sendRuntimeError(send, "command_invalid", "Runtime command envelope is invalid")
			sessionErr = runtimeSessionFailure(connect.CodeInvalidArgument, "command_invalid", "Runtime command envelope is invalid")
			cancel()
			break
		}
		if err := runtimeprotocol.ValidateCommand(command); err != nil {
			_ = sendRuntimeError(send, "command_invalid", "Runtime command is not supported")
			sessionErr = runtimeSessionFailure(connect.CodeInvalidArgument, "command_invalid", "Runtime command is not supported")
			cancel()
			break
		}
		if providerKind, bindsProvider := runtimeCommandProvider(command); bindsProvider && providerKind != open.GetProviderKind() {
			_ = sendRuntimeError(send, "provider_mismatch", "Runtime command Provider does not match the opened session")
			sessionErr = runtimeSessionFailure(connect.CodePermissionDenied, "provider_mismatch", "Runtime command Provider does not match the opened session")
			cancel()
			break
		}
		commands.Add(1)
		go func(command runtimeprotocol.Command) {
			defer commands.Done()
			message, executeErr := client.Execute(sessionContext, command)
			if executeErr != nil && message.MessageType == "" {
				_ = sendRuntimeError(send, "runtime_execution_failed", "Runtime command failed")
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

// ReadArtifact streams one execution-owned ArtifactCandidate without exposing
// a general Worker filesystem API.
func (s *Service) ReadArtifact(ctx context.Context, request *connect.Request[workerruntimev1alpha1.RuntimeArtifactReadRequest], stream *connect.ServerStream[workerruntimev1alpha1.RuntimeArtifactChunk]) error {
	if !s.ready() {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "worker_unavailable", "Worker service is not initialized")
	}
	if request == nil || request.Msg == nil || stream == nil {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "artifact_request_invalid", "Artifact request is required")
	}
	message := request.Msg
	clientIdentity, err := s.identity.ClientIdentity(ctx)
	if err != nil || clientIdentity == nil || validateIdentity(clientIdentity) != nil {
		return runtimeSessionFailure(connect.CodeUnauthenticated, "transport_identity_missing", "authenticated client identity is required")
	}
	if err := validateExpectedIdentity(message.GetExpectedWorkerIdentity(), s.workerIdentity); err != nil {
		return err
	}
	binding, err := s.validateBinding(message.GetNegotiation(), clientIdentity)
	if err != nil {
		return err
	}
	if _, ok := binding.caps[workerv1alpha1.Capability_CAPABILITY_NEGOTIATION]; !ok {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_not_negotiated", "Artifact reads require the negotiation capability")
	}
	if err := s.validateRuntimeFencing(message.GetFencing(), message.GetGeneration()); err != nil {
		return err
	}
	if err := validateIdentifier(message.GetExecutionId(), "execution_id"); err != nil || message.GetGeneration() == 0 || message.GetGeneration() != message.GetFencing().GetGeneration() || !validRuntimeArtifactRoot(message.GetRootDirectory()) || !validRuntimeArtifactPath(message.GetRelativePath()) || message.GetExpectedSha256() != "" && !runtimeArtifactSHA256Pattern.MatchString(message.GetExpectedSha256()) {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "artifact_request_invalid", "Artifact identity or path is invalid")
	}
	artifactPath, ok := runtimeArtifactPath(s.runtimeDirectory, message.GetRootDirectory(), message.GetRelativePath())
	if !ok {
		return runtimeSessionFailure(connect.CodePermissionDenied, "artifact_root_forbidden", "Artifact root is outside the configured Runtime directory")
	}
	root, err := os.OpenRoot(s.runtimeDirectory)
	if err != nil {
		return runtimeSessionFailure(connect.CodeNotFound, "artifact_root_unavailable", "Artifact root is unavailable")
	}
	defer root.Close()
	file, err := root.Open(artifactPath)
	if err != nil {
		return runtimeSessionFailure(connect.CodeNotFound, "artifact_unavailable", "Artifact is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "artifact_not_regular", "Artifact is not a regular file")
	}
	size := uint64(info.Size())
	if size > runtimeprotocol.MaxArtifactBytes {
		return runtimeSessionFailure(connect.CodeResourceExhausted, "artifact_too_large", "Artifact exceeds the download limit")
	}
	if message.ExpectedSizeBytes != nil && message.GetExpectedSizeBytes() != size {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "artifact_size_mismatch", "Artifact size no longer matches its candidate")
	}
	if expected := message.GetExpectedSha256(); expected != "" {
		digest := sha256.New()
		if _, err := io.Copy(digest, file); err != nil || fmt.Sprintf("%x", digest.Sum(nil)) != expected {
			return runtimeSessionFailure(connect.CodeFailedPrecondition, "artifact_digest_mismatch", "Artifact digest no longer matches its candidate")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return runtimeSessionFailure(connect.CodeInternal, "artifact_seek_failed", "Artifact could not be read")
		}
	}
	buffer := make([]byte, runtimeArtifactChunkBytes)
	if size == 0 {
		return stream.Send(&workerruntimev1alpha1.RuntimeArtifactChunk{SizeBytes: 0})
	}
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			if err := stream.Send(&workerruntimev1alpha1.RuntimeArtifactChunk{Data: append([]byte(nil), buffer[:count]...), SizeBytes: size}); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return runtimeSessionFailure(connect.CodeInternal, "artifact_read_failed", "Artifact could not be read")
		}
	}
}

const (
	runtimeArtifactChunkBytes         = 64 << 10
	maxRuntimeProviderCredentialBytes = 65 << 10
)

var runtimeArtifactSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validRuntimeArtifactRoot(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && filepath.IsAbs(value) && filepath.Clean(value) != string(filepath.Separator) && !runtimePathHasControl(value)
}

func validRuntimeArtifactPath(value string) bool {
	return value != "" && len(value) <= 4096 && value != "." && !strings.Contains(value, `\`) && filepath.IsLocal(value) && filepath.Clean(value) == value && !runtimePathHasControl(value)
}

func runtimeArtifactPath(runtimeDirectory, artifactRoot, relativePath string) (string, bool) {
	if !validRuntimeArtifactRoot(runtimeDirectory) {
		return "", false
	}
	relativeRoot, err := filepath.Rel(filepath.Clean(runtimeDirectory), filepath.Clean(artifactRoot))
	if err != nil || relativeRoot == ".." || strings.HasPrefix(relativeRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeRoot) {
		return "", false
	}
	return filepath.Join(relativeRoot, relativePath), true
}

func runtimePathHasControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

var runtimeProviderKindPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

func validRuntimeProviderKind(value string) bool {
	return runtimeProviderKindPattern.MatchString(value)
}

func runtimeProviderCredentialFile(directory, tenantID, providerKind string) (string, error) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil {
		return "", runtimeSessionFailure(connect.CodeInvalidArgument, "tenant_invalid", "Runtime tenant is invalid")
	}
	if !validRuntimeProviderKind(providerKind) {
		return "", runtimeSessionFailure(connect.CodeInvalidArgument, "provider_invalid", "Runtime provider kind is invalid")
	}
	if directory == "" {
		return "", nil
	}
	path := filepath.Join(directory, tenantID+"."+providerKind+".json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", runtimeSessionFailure(connect.CodeFailedPrecondition, "provider_credential_unavailable", "Runtime Provider credential is unavailable")
	}
	if info.Size() < 0 || info.Size() > maxRuntimeProviderCredentialBytes {
		return "", runtimeSessionFailure(connect.CodeFailedPrecondition, "provider_credential_invalid", "Runtime Provider credential is invalid")
	}
	return path, nil
}

func runtimeCommandProvider(command runtimeprotocol.Command) (string, bool) {
	switch command.CommandType {
	case "Describe":
		providerKind, _ := command.Payload["provider"].(string)
		return providerKind, true
	case "StartSession", "ResumeSession":
		runnerInput, _ := command.Payload["runnerInput"].(map[string]any)
		workload, _ := runnerInput["workload"].(map[string]any)
		providerKind, _ := workload["provider"].(string)
		return providerKind, true
	default:
		return "", false
	}
}

func (s *Service) validateRuntimeFencing(fencing *workerv1alpha1.FencingProof, generation uint64) error {
	if fencing == nil || fencing.GetLeaseId() == "" || fencing.GetGeneration() == 0 || len(fencing.GetToken()) == 0 || len(fencing.GetToken()) > int(MaxPayloadBytes) {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "fencing_required", "Runtime fencing proof is required")
	}
	if err := validateIdentifier(fencing.GetLeaseId(), "lease_id"); err != nil {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "lease_id_invalid", "Runtime lease id is invalid")
	}
	if s.admissionLeaseID == "" || s.admissionGeneration == 0 {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "generation_authority_missing", "Runtime fencing authority is not configured")
	}
	if len(s.admissionToken) == 0 || subtle.ConstantTimeCompare(fencing.GetToken(), s.admissionToken) != 1 {
		return runtimeSessionFailure(connect.CodePermissionDenied, "fencing_token_mismatch", "Runtime fencing token does not match the Worker authority")
	}
	if fencing.GetLeaseId() != s.admissionLeaseID {
		return runtimeSessionFailure(connect.CodePermissionDenied, "lease_mismatch", "Runtime lease does not match the Worker authority")
	}
	if fencing.GetGeneration() != s.admissionGeneration {
		return runtimeSessionFailure(connect.CodeFailedPrecondition, "stale_generation", "Runtime generation does not match the Worker authority")
	}
	if fencing.GetGeneration() != generation {
		return runtimeSessionFailure(connect.CodeInvalidArgument, "runtime_generation_mismatch", "Runtime generation does not match its fencing proof")
	}
	return nil
}

func sendRuntimeJSON(send func(*workerruntimev1alpha1.RuntimeSessionResponse) error, message runtimeprotocol.Message) error {
	encoded, err := json.Marshal(message)
	if err != nil || len(encoded) > runtimeprotocol.MaxMessageBytes {
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
	MaxCommandBytesForRuntimeFrame = runtimeprotocol.MaxCommandBytes + 4096
	MaxMessageBytesForRuntimeFrame = runtimeprotocol.MaxMessageBytes + 4096
)
