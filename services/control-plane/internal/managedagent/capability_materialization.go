package managedagent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"

	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

// Base64url Skill bundles expand inside the descriptor; stay below the Kubernetes Secret limit.
const maxFoundationCapabilityMaterializationBytes = 768 << 10

type foundationCapabilityMaterialization struct {
	Version uint32                           `json:"version"`
	MCP     []foundationMcpMaterialization   `json:"mcp"`
	Skills  []foundationSkillMaterialization `json:"skills"`
}

type foundationMcpMaterialization struct {
	ResourceID   string   `json:"resourceId"`
	Version      string   `json:"version"`
	Digest       string   `json:"digest"`
	Transport    string   `json:"transport"`
	Endpoint     string   `json:"endpoint"`
	Token        string   `json:"token"`
	AllowedHosts []string `json:"allowedHosts"`
}

type foundationSkillMaterialization struct {
	ResourceID   string `json:"resourceId"`
	Version      string `json:"version"`
	Digest       string `json:"digest"`
	Bundle       string `json:"bundle,omitempty"`
	Signature    string `json:"signature,omitempty"`
	PublicKey    string `json:"publicKey,omitempty"`
	SigningKeyID string `json:"signingKeyId,omitempty"`
}

func foundationCapabilityMaterializationFile(directory, tenantID string, bindings []*workerruntimev1alpha1.RuntimeCapabilityBinding) ([]byte, error) {
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
	if len(mcp) == 0 && len(skills) == 0 || strings.TrimSpace(directory) == "" {
		return nil, nil
	}
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	defer root.Close()
	name := tenantID + ".capabilities.json"
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > maxFoundationCapabilityMaterializationBytes {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFoundationCapabilityMaterializationBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxFoundationCapabilityMaterializationBytes {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	var descriptor foundationCapabilityMaterialization
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&descriptor) != nil || decoder.Decode(&struct{}{}) != io.EOF || descriptor.Version != 1 || len(descriptor.MCP) != len(mcp) || len(descriptor.Skills) != len(skills) {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	byID := make(map[string]*workerruntimev1alpha1.RuntimeCapabilityBinding, len(mcp))
	for _, binding := range mcp {
		if _, exists := byID[binding.GetResourceId()]; exists {
			return nil, ErrRuntimeEnvironmentUnavailable
		}
		byID[binding.GetResourceId()] = binding
	}
	for _, item := range descriptor.MCP {
		binding := byID[item.ResourceID]
		if binding == nil || item.Version != binding.GetVersion() || item.Digest != binding.GetDigest() || item.Transport != binding.GetTransport() || !validFoundationMcpMaterialization(item) {
			return nil, ErrRuntimeEnvironmentUnavailable
		}
		delete(byID, item.ResourceID)
	}
	if len(byID) != 0 {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	skillByID := make(map[string]*workerruntimev1alpha1.RuntimeCapabilityBinding, len(skills))
	for _, binding := range skills {
		if _, exists := skillByID[binding.GetResourceId()]; exists {
			return nil, ErrRuntimeEnvironmentUnavailable
		}
		skillByID[binding.GetResourceId()] = binding
	}
	for _, item := range descriptor.Skills {
		binding := skillByID[item.ResourceID]
		if binding == nil || item.Version != binding.GetVersion() || item.Digest != binding.GetDigest() || !validFoundationSkillMaterialization(item) || item.Bundle != "" && !foundationTrustedSkillKey(root, item.SigningKeyID, item.PublicKey) {
			return nil, ErrRuntimeEnvironmentUnavailable
		}
		delete(skillByID, item.ResourceID)
	}
	if len(skillByID) != 0 {
		return nil, ErrRuntimeEnvironmentUnavailable
	}
	return data, nil
}

func validFoundationMcpMaterialization(item foundationMcpMaterialization) bool {
	if commonv1alpha1.ValidateIdentifier(item.ResourceID, "/capability/resourceId") != nil || commonv1alpha1.ValidateIdentifier(item.Version, "/capability/version") != nil || !foundationCapabilityDigest(item.Digest) || item.Transport != "sse" && item.Transport != "streamable-http" || item.Token == "" || len(item.Token) > 4096 {
		return false
	}
	for _, character := range item.Token {
		if character < 33 || character > 126 {
			return false
		}
	}
	parsed, err := url.Parse(item.Endpoint)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "https" && !(parsed.Scheme == "http" && foundationLoopbackHost(parsed.Hostname())) {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if len(item.AllowedHosts) == 0 || len(item.AllowedHosts) > 32 {
		return false
	}
	found := false
	for _, allowed := range item.AllowedHosts {
		if strings.TrimSpace(allowed) == "" || len(allowed) > 253 || strings.ContainsAny(allowed, " /:?#@\t\r\n") {
			return false
		}
		if strings.ToLower(strings.TrimSuffix(allowed, ".")) == host {
			found = true
		}
	}
	return found
}

func foundationLoopbackHost(value string) bool {
	host := strings.Trim(strings.ToLower(value), "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func foundationCapabilityDigest(value string) bool {
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

func validFoundationSkillMaterialization(item foundationSkillMaterialization) bool {
	if commonv1alpha1.ValidateIdentifier(item.ResourceID, "/capability/resourceId") != nil || commonv1alpha1.ValidateIdentifier(item.Version, "/capability/version") != nil || !foundationCapabilityDigest(item.Digest) {
		return false
	}
	return item.Bundle != "" && item.Signature != "" && item.PublicKey != "" && foundationBase64URL(item.Bundle, 700000) && foundationBase64URL(item.Signature, 256) && foundationBase64URL(item.PublicKey, 256) && commonv1alpha1.ValidateIdentifier(item.SigningKeyID, "/capability/signingKeyId") == nil
}

func foundationTrustedSkillKey(root *os.Root, keyID, encoded string) bool {
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

func foundationBase64URL(value string, maximum int) bool {
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
