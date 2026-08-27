package managedagent

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// lifecycleStateMachineDigest is the checked-in identity of the v1alpha1
// transition table. It is intentionally checked at construction and mutation
// boundaries so a source edit cannot silently widen the kernel.
const lifecycleStateMachineDigest = "sha256:e8e090658e6a0890fcd940cb6d713c7265e5eb38b0edab729c44de6556b9c66b"

func computeLifecycleStateMachineDigest() string {
	var builder strings.Builder
	builder.WriteString(LifecycleProfileID)
	builder.WriteByte(0)
	for _, transition := range lifecycleTransitions {
		builder.WriteString(string(transition.Resource))
		builder.WriteByte(0)
		builder.WriteString(transition.From)
		builder.WriteByte(0)
		builder.WriteString(transition.Event)
		builder.WriteByte(0)
		builder.WriteString(transition.To)
		builder.WriteByte(0)
	}
	digest := sha256.Sum256([]byte(builder.String()))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// profileIdentity is used by tests and review tooling without exposing any
// mutable authority object.
func profileIdentity() string {
	profile := ManagedAgentLifecycleProfile()
	return profile.ID + "@" + profile.StateMachineDigest + "/" + strconv.Itoa(len(lifecycleTransitions))
}
