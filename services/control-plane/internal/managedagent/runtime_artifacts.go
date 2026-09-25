package managedagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"path"
	"strings"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

func (coordinator *DurableRuntimeExecutionCoordinator) ReadArtifact(ctx context.Context, principalSource VerifiedPrincipalSource, input RuntimeArtifactReadInput) (RuntimeArtifact, error) {
	if coordinator == nil || coordinator.store == nil || ctx == nil || principalSource == nil {
		return RuntimeArtifact{}, ErrRuntimeArtifactUnavailable
	}
	candidate, err := runtimeArtifactCandidate(input)
	if err != nil {
		return RuntimeArtifact{}, err
	}
	principal, err := nextVerifiedPrincipal(principalSource)
	if err != nil {
		return RuntimeArtifact{}, ErrRuntimeArtifactUnavailable
	}
	session, err := coordinator.store.GetManagedAgentSessionForArtifact(ctx, input.Scope.TenantID, principal, input.Scope.ProjectID, input.SessionID)
	if err != nil {
		return RuntimeArtifact{}, fmt.Errorf("%w: %v", ErrRuntimeArtifactUnavailable, err)
	}
	worker, err := coordinator.workerForSession(session)
	if err != nil || worker.generation != input.Generation {
		return RuntimeArtifact{}, ErrRuntimeArtifactUnavailable
	}
	paths, err := deriveRuntimeWorkspacePaths(coordinator.workspaceDirectory, input.Scope, input.SessionID, input.TurnID, input.ExecutionID)
	if err != nil {
		return RuntimeArtifact{}, err
	}
	root := paths.workspaceDirectory
	if candidate.sourceRoot == "runtime-output" {
		root = paths.runtimeOutputDirectory
	}
	data, err := worker.readArtifact(ctx, principalSource, input.ExecutionID, coordinator.fencingToken, root, candidate.relativePath, candidate.expectedSize, candidate.sha256)
	if err != nil {
		return RuntimeArtifact{}, fmt.Errorf("%w: %v", ErrRuntimeArtifactUnavailable, err)
	}
	return RuntimeArtifact{Data: data, FileName: path.Base(candidate.relativePath), ContentType: candidate.contentType, SHA256: candidate.sha256}, nil
}

type runtimeArtifactReference struct {
	sourceRoot   string
	relativePath string
	contentType  string
	expectedSize *uint64
	sha256       string
}

func runtimeArtifactCandidate(input RuntimeArtifactReadInput) (runtimeArtifactReference, error) {
	message := input.Message
	if runtimeprotocol.ValidateMessage(message) != nil || message.MessageType != "ArtifactCandidate" || message.ExecutionID != input.ExecutionID || message.Generation != input.Generation {
		return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
	}
	artifact, ok := message.Payload["artifact"].(map[string]any)
	if !ok {
		return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
	}
	sourceRoot, _ := artifact["sourceRoot"].(string)
	relativePath, _ := artifact["path"].(string)
	kind, _ := artifact["kind"].(string)
	if sourceRoot != "workspace" && sourceRoot != "runtime-output" || !validRuntimeArtifactRelativePath(relativePath) || !validRuntimeArtifactKind(kind) {
		return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
	}
	contentType := "application/octet-stream"
	if raw, exists := artifact["contentType"]; exists {
		value, ok := raw.(string)
		mediaType, parameters, err := mime.ParseMediaType(value)
		if !ok || err != nil || len(value) > 255 || mediaType == "" {
			return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
		}
		contentType = mime.FormatMediaType(mediaType, parameters)
	}
	var expectedSize *uint64
	if raw, exists := artifact["reportedSize"]; exists {
		size, ok := runtimeArtifactSize(raw)
		if !ok || size > runtimeprotocol.MaxArtifactBytes {
			return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
		}
		expectedSize = &size
	}
	digest := ""
	if raw, exists := artifact["sha256"]; exists {
		value, ok := raw.(string)
		decoded, err := hex.DecodeString(value)
		if !ok || err != nil || len(decoded) != sha256.Size || strings.ToLower(value) != value {
			return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
		}
		digest = value
	}
	return runtimeArtifactReference{sourceRoot: sourceRoot, relativePath: relativePath, contentType: contentType, expectedSize: expectedSize, sha256: digest}, nil
}

func validRuntimeArtifactRelativePath(value string) bool {
	return value != "" && len(value) <= 4096 && value != "." && value != ".." && !strings.HasPrefix(value, "../") && !path.IsAbs(value) && path.Clean(value) == value && !strings.Contains(value, `\`) && !containsRuntimePathControl(value)
}

func validRuntimeArtifactKind(value string) bool {
	switch strings.ReplaceAll(value, "_", "-") {
	case "diff", "generated-file", "terminal-log", "provider-output":
		return true
	default:
		return false
	}
}

func runtimeArtifactSize(value any) (uint64, bool) {
	switch size := value.(type) {
	case float64:
		return uint64(size), size >= 0 && size <= runtimeprotocol.MaxArtifactBytes && float64(uint64(size)) == size
	case int:
		return uint64(size), size >= 0
	case uint64:
		return size, true
	default:
		return 0, false
	}
}
