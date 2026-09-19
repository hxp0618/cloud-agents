package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

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
	capabilityManifest, err := validateRuntimeCapabilityBindings(open, s.now().UTC())
	if err != nil {
		return err
	}
	credentialFile, err := runtimeProviderCredentialFile(s.runtimeCredentialDirectory, open.GetTenantId(), open.GetProviderKind())
	if err != nil {
		return err
	}
	capabilityMaterialization, err := runtimeCapabilityMaterializationFile(s.runtimeCapabilityMaterializationDirectory, open.GetTenantId(), open.GetCapabilityBindings())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cloud-agent-worker: capability materialization rejected: %v\n", err)
		return err
	}
	runtimeLeaseKey := open.GetTenantId() + "\x00" + open.GetExecutionId()
	sessionContext, cancel := context.WithCancel(ctx)
	lease := &runtimeLease{cancel: cancel, done: make(chan struct{})}
	if err := s.acquireRuntimeLease(runtimeLeaseKey, lease); err != nil {
		cancel()
		return err
	}
	defer cancel()
	defer s.releaseRuntimeLease(runtimeLeaseKey, lease)
	select {
	case s.runtimeSlots <- struct{}{}:
		defer func() { <-s.runtimeSlots }()
	default:
		return runtimeSessionFailure(connect.CodeResourceExhausted, "capacity_exhausted", "Runtime session capacity is exhausted")
	}
	client, err := runtimeprocess.New(sessionContext, runtimeprocess.Config{
		Command: s.runtimeCommand, Environment: s.runtimeEnvironment, Directory: s.runtimeDirectory, CredentialFile: credentialFile, CapabilityManifest: capabilityManifest, CapabilityMaterialization: capabilityMaterialization,
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
				// Keep the transport response generic, but retain the local process
				// exit detail in Worker logs so a fenced or crashed Runtime can be
				// distinguished from a Provider Error without exposing credentials.
				_, _ = fmt.Fprintf(os.Stderr, "cloud-agent-worker: runtime command %s failed: %v\n", command.CommandType, executeErr)
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
	// Base64url Skill bundles expand inside the descriptor; stay below the Kubernetes Secret limit.
	maxRuntimeCapabilityMaterializationBytes = 768 << 10
	runtimeWriterFenceTimeout                = 5 * time.Second
)

func (s *Service) acquireRuntimeLease(executionID string, lease *runtimeLease) error {
	// ponytail: one bounded global fence lock keeps takeover ordering explicit; shard only after measured concurrent Runtime opens.
	s.runtimeFenceMu.Lock()
	defer s.runtimeFenceMu.Unlock()
	s.runtimeMu.Lock()
	previous := s.runtimeSessions[executionID]
	s.runtimeMu.Unlock()
	if previous != nil {
		previous.cancel()
		timer := time.NewTimer(runtimeWriterFenceTimeout)
		select {
		case <-previous.done:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			return runtimeSessionFailure(connect.CodeFailedPrecondition, "writer_fence_timeout", "the previous Runtime writer did not stop")
		}
	}
	s.runtimeMu.Lock()
	s.runtimeSessions[executionID] = lease
	s.runtimeMu.Unlock()
	return nil
}

func (s *Service) releaseRuntimeLease(executionID string, lease *runtimeLease) {
	s.runtimeMu.Lock()
	if s.runtimeSessions[executionID] == lease {
		delete(s.runtimeSessions, executionID)
	}
	s.runtimeMu.Unlock()
	close(lease.done)
}

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

var (
	runtimeProviderKindPattern         = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	runtimeCapabilityPermissionPattern = regexp.MustCompile(`^[a-z][a-z0-9._:-]{0,127}$`)
)

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

type runtimeCapabilityMaterialization struct {
	Version uint32                        `json:"version"`
	MCP     []runtimeMcpMaterialization   `json:"mcp"`
	Skills  []runtimeSkillMaterialization `json:"skills"`
}

type runtimeMcpMaterialization struct {
	ResourceID   string   `json:"resourceId"`
	Version      string   `json:"version"`
	Digest       string   `json:"digest"`
	Transport    string   `json:"transport"`
	Endpoint     string   `json:"endpoint"`
	Token        string   `json:"token"`
	AllowedHosts []string `json:"allowedHosts"`
}

type runtimeSkillMaterialization struct {
	ResourceID   string `json:"resourceId"`
	Version      string `json:"version"`
	Digest       string `json:"digest"`
	Bundle       string `json:"bundle,omitempty"`
	Signature    string `json:"signature,omitempty"`
	PublicKey    string `json:"publicKey,omitempty"`
	SigningKeyID string `json:"signingKeyId,omitempty"`
}

// runtimeCapabilityMaterializationFile resolves only an operator-owned,
// tenant-scoped descriptor. It is never sent over the Worker protobuf and is
// passed to the child Runtime through a short-lived anonymous FD.
func runtimeCapabilityMaterializationFile(directory, tenantID string, bindings []*workerruntimev1alpha1.RuntimeCapabilityBinding) ([]byte, error) {
	var mcp []*workerruntimev1alpha1.RuntimeCapabilityBinding
	var skills []*workerruntimev1alpha1.RuntimeCapabilityBinding
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		switch binding.GetResourceKind() {
		case "mcp-server":
			mcp = append(mcp, binding)
		case "skill-bundle":
			skills = append(skills, binding)
		}
	}
	if len(mcp) == 0 && len(skills) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(directory) == "" {
		return nil, nil
	}
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil {
		return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "tenant_invalid", "tenant identity is invalid")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_unavailable", "capability materialization is unavailable")
	}
	defer root.Close()
	name := tenantID + ".capabilities.json"
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > maxRuntimeCapabilityMaterializationBytes {
		return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_unavailable", "capability materialization is unavailable")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_unavailable", "capability materialization is unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRuntimeCapabilityMaterializationBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxRuntimeCapabilityMaterializationBytes {
		return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_invalid", "capability materialization is invalid")
	}
	var descriptor runtimeCapabilityMaterialization
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&descriptor) != nil || decoder.Decode(&struct{}{}) != io.EOF || descriptor.Version != 1 || len(descriptor.MCP) != len(mcp) || len(descriptor.Skills) != len(skills) {
		return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_invalid", "capability materialization is invalid")
	}
	byID := make(map[string]*workerruntimev1alpha1.RuntimeCapabilityBinding, len(mcp))
	for _, binding := range mcp {
		if _, exists := byID[binding.GetResourceId()]; exists {
			return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_invalid", "capability materialization is invalid")
		}
		byID[binding.GetResourceId()] = binding
	}
	for _, item := range descriptor.MCP {
		binding := byID[item.ResourceID]
		if binding == nil || item.Version != binding.GetVersion() || item.Digest != binding.GetDigest() || item.Transport != binding.GetTransport() || !validRuntimeMcpMaterialization(item) {
			return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_invalid", "capability materialization is invalid")
		}
		delete(byID, item.ResourceID)
	}
	if len(byID) != 0 {
		return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_invalid", "capability materialization is invalid")
	}
	skillByID := make(map[string]*workerruntimev1alpha1.RuntimeCapabilityBinding, len(skills))
	for _, binding := range skills {
		if _, exists := skillByID[binding.GetResourceId()]; exists {
			return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_invalid", "capability materialization is invalid")
		}
		skillByID[binding.GetResourceId()] = binding
	}
	for _, item := range descriptor.Skills {
		binding := skillByID[item.ResourceID]
		if binding == nil || item.Version != binding.GetVersion() || item.Digest != binding.GetDigest() || !validRuntimeSkillMaterialization(item) || item.Bundle != "" && !runtimeTrustedSkillKey(root, item.SigningKeyID, item.PublicKey) {
			return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_invalid", "capability materialization is invalid")
		}
		delete(skillByID, item.ResourceID)
	}
	if len(skillByID) != 0 {
		return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_materialization_invalid", "capability materialization is invalid")
	}
	return data, nil
}

func validRuntimeMcpMaterialization(item runtimeMcpMaterialization) bool {
	if commonv1alpha1.ValidateIdentifier(item.ResourceID, "/capability/resourceId") != nil || commonv1alpha1.ValidateIdentifier(item.Version, "/capability/version") != nil || !validRuntimeCapabilityDigest(item.Digest) || (item.Transport != "sse" && item.Transport != "streamable-http") || item.Token == "" || len(item.Token) > 4096 {
		return false
	}
	for _, character := range item.Token {
		if character < 33 || character > 126 {
			return false
		}
	}
	parsed, err := url.Parse(item.Endpoint)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && isRuntimeLoopbackHost(parsed.Hostname()))) || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if len(item.AllowedHosts) == 0 || len(item.AllowedHosts) > 32 {
		return false
	}
	found := false
	for _, allowed := range item.AllowedHosts {
		if !validRuntimeMaterializationHost(allowed) {
			return false
		}
		if strings.ToLower(strings.TrimSuffix(allowed, ".")) == host {
			found = true
		}
	}
	return found
}

func validRuntimeSkillMaterialization(item runtimeSkillMaterialization) bool {
	if commonv1alpha1.ValidateIdentifier(item.ResourceID, "/capability/resourceId") != nil || commonv1alpha1.ValidateIdentifier(item.Version, "/capability/version") != nil || !validRuntimeCapabilityDigest(item.Digest) {
		return false
	}
	return item.Bundle != "" && item.Signature != "" && item.PublicKey != "" && validRuntimeBase64URL(item.Bundle, 700000) && validRuntimeBase64URL(item.Signature, 256) && validRuntimeBase64URL(item.PublicKey, 256) && commonv1alpha1.ValidateIdentifier(item.SigningKeyID, "/capability/signingKeyId") == nil
}

func runtimeTrustedSkillKey(root *os.Root, keyID, encoded string) bool {
	if root == nil || commonv1alpha1.ValidateIdentifier(keyID, "/capability/signingKeyId") != nil {
		return false
	}
	file, err := root.Open(keyID + ".pub")
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > 256 {
		return false
	}
	key, err := io.ReadAll(io.LimitReader(file, 257))
	return err == nil && len(key) <= 256 && base64.RawURLEncoding.EncodeToString(key) == encoded
}

func validRuntimeBase64URL(value string, maximum int) bool {
	if len(value) == 0 || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

func validRuntimeMaterializationHost(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 253 && !strings.ContainsAny(value, " /:?#@\t\r\n")
}

func isRuntimeLoopbackHost(value string) bool {
	host := strings.Trim(strings.ToLower(value), "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func validRuntimeCapabilityDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

type runtimeCapabilityManifest struct {
	Version  uint32                           `json:"version"`
	Bindings []runtimeCapabilityManifestEntry `json:"bindings"`
}

type runtimeCapabilityManifestEntry struct {
	ResourceKind        string   `json:"resourceKind"`
	ResourceID          string   `json:"resourceId"`
	Version             string   `json:"version"`
	Digest              string   `json:"digest"`
	Transport           string   `json:"transport,omitempty"`
	ConnectionRef       string   `json:"connectionRef,omitempty"`
	CredentialRef       string   `json:"credentialRef,omitempty"`
	GrantID             string   `json:"grantId"`
	NetworkPolicyRef    string   `json:"networkPolicyRef,omitempty"`
	ExpiresAtUnixSecond uint64   `json:"expiresAtUnixSeconds"`
	Permissions         []string `json:"permissions,omitempty"`
	ReadOnly            bool     `json:"readOnly"`
}

func validateRuntimeCapabilityBindings(open *workerruntimev1alpha1.RuntimeSessionOpen, now time.Time) ([]byte, error) {
	bindings := open.GetCapabilityBindings()
	if len(bindings) == 0 {
		if open.GetCapabilityManifestDigest() != "" {
			return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_manifest_invalid", "empty capability bindings must not carry a manifest digest")
		}
		return nil, nil
	}
	if len(bindings) > 32 {
		return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_binding_invalid", "too many capability bindings")
	}
	manifest := runtimeCapabilityManifest{Version: 1, Bindings: make([]runtimeCapabilityManifestEntry, 0, len(bindings))}
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding == nil || commonv1alpha1.ValidateIdentifier(binding.GetResourceId(), "/capabilityBindings/resourceId") != nil || commonv1alpha1.ValidateIdentifier(binding.GetVersion(), "/capabilityBindings/version") != nil || commonv1alpha1.ValidateIdentifier(binding.GetGrantId(), "/capabilityBindings/grantId") != nil {
			return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_binding_invalid", "capability identity is invalid")
		}
		if _, err := fencingDigestBytes(binding.GetDigest()); err != nil {
			return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_digest_invalid", "capability digest is invalid")
		}
		expiresAt := time.Unix(int64(binding.GetExpiresAtUnixSeconds()), 0).UTC()
		if !expiresAt.After(now) || expiresAt.After(now.Add(15*time.Minute)) {
			return nil, runtimeSessionFailure(connect.CodeFailedPrecondition, "capability_grant_expired", "capability grant is expired or exceeds the short-lived window")
		}
		entry := runtimeCapabilityManifestEntry{ResourceKind: binding.GetResourceKind(), ResourceID: binding.GetResourceId(), Version: binding.GetVersion(), Digest: binding.GetDigest(), Transport: binding.GetTransport(), ConnectionRef: binding.GetConnectionRef(), CredentialRef: binding.GetCredentialRef(), GrantID: binding.GetGrantId(), NetworkPolicyRef: binding.GetNetworkPolicyRef(), ExpiresAtUnixSecond: binding.GetExpiresAtUnixSeconds(), Permissions: append([]string(nil), binding.GetPermissions()...), ReadOnly: binding.GetReadOnly()}
		switch entry.ResourceKind {
		case "mcp-server":
			if entry.ReadOnly || entry.Transport != "stdio" && entry.Transport != "sse" && entry.Transport != "streamable-http" || commonv1alpha1.ValidateIdentifier(entry.ConnectionRef, "/capabilityBindings/connectionRef") != nil || commonv1alpha1.ValidateIdentifier(entry.CredentialRef, "/capabilityBindings/credentialRef") != nil || commonv1alpha1.ValidateIdentifier(entry.NetworkPolicyRef, "/capabilityBindings/networkPolicyRef") != nil || len(entry.Permissions) == 0 || len(entry.Permissions) > int(MaxRepeatedItems) {
				return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_binding_invalid", "MCP capability binding is invalid")
			}
			for _, permission := range entry.Permissions {
				if !runtimeCapabilityPermissionPattern.MatchString(permission) || strings.Contains(permission, "*") {
					return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_permission_invalid", "MCP capability permission is invalid")
				}
			}
		case "skill-bundle":
			if !entry.ReadOnly || entry.Transport != "" || entry.ConnectionRef != "" || entry.CredentialRef != "" || entry.NetworkPolicyRef != "" || len(entry.Permissions) != 0 {
				return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_binding_invalid", "Skill capability binding is invalid")
			}
		default:
			return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_binding_invalid", "capability kind is unsupported")
		}
		key := entry.ResourceKind + "\x00" + entry.ResourceID + "\x00" + entry.Version + "\x00" + entry.Digest
		if _, duplicate := seen[key]; duplicate {
			return nil, runtimeSessionFailure(connect.CodeInvalidArgument, "capability_binding_invalid", "duplicate capability binding")
		}
		seen[key] = struct{}{}
		manifest.Bindings = append(manifest.Bindings, entry)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) > int(MaxPayloadBytes) {
		return nil, runtimeSessionFailure(connect.CodeResourceExhausted, "capability_manifest_too_large", "capability manifest exceeds the Runtime limit")
	}
	sum := sha256.Sum256(encoded)
	if open.GetCapabilityManifestDigest() != fmt.Sprintf("sha256:%x", sum[:]) {
		return nil, runtimeSessionFailure(connect.CodePermissionDenied, "capability_manifest_digest_mismatch", "capability manifest digest does not match the opened session")
	}
	return encoded, nil
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
