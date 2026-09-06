package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestFoundationIntentBinding(t *testing.T) {
	r := FoundationResolved{
		Tenant: "tenant", Project: "project", Workspace: "workspace", WorkspaceName: "workspace",
		Volume: "volume", Target: "target", Sandbox: "sandbox",
		ImageURI: "node@sha256:" + strings.Repeat("a", 64), CPUMillis: 500, MemoryBytes: 536870912,
	}
	i, err := BindFoundationIntent(r)
	if err != nil {
		t.Fatal(err)
	}
	canonical := `{"cpuMillis":500,"imageURI":"node@sha256:` + strings.Repeat("a", 64) + `","memoryBytes":536870912,"networkAllowedEgress":null,"networkDefaultEgress":"","networkPolicyId":"","networkPreviewEnabled":false,"profileId":"foundationSandboxLifecycle/v1alpha1","project":"project","sandbox":"sandbox","target":"target","tenant":"tenant","volume":"volume","workspace":"workspace","workspaceName":"workspace"}`
	sum := sha256.Sum256([]byte(canonical))
	if i.RequestDigest() != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("canonical digest mismatch")
	}
	for _, mutate := range []func(*FoundationResolved){
		func(r *FoundationResolved) { r.Tenant += "1" }, func(r *FoundationResolved) { r.Project += "1" },
		func(r *FoundationResolved) { r.Workspace += "1" }, func(r *FoundationResolved) { r.WorkspaceName += "1" },
		func(r *FoundationResolved) { r.Volume += "1" }, func(r *FoundationResolved) { r.Target += "1" },
		func(r *FoundationResolved) { r.Sandbox += "1" }, func(r *FoundationResolved) { r.ImageURI = "other/" + r.ImageURI },
		func(r *FoundationResolved) { r.CPUMillis++ }, func(r *FoundationResolved) { r.MemoryBytes++ },
	} {
		other := i.Resolved()
		mutate(&other)
		bound, err := BindFoundationIntent(other)
		if err != nil || bound.RequestDigest() == i.RequestDigest() {
			t.Fatal("unbound resolved field", err)
		}
		if !reflect.DeepEqual(i.Resolved(), r) {
			t.Fatal("snapshot mutated")
		}
	}
	for _, mutate := range []func(*FoundationResolved){
		func(r *FoundationResolved) { r.Tenant = "../tenant" }, func(r *FoundationResolved) { r.ImageURI = "node:latest" },
		func(r *FoundationResolved) { r.CPUMillis = 0 }, func(r *FoundationResolved) { r.MemoryBytes = 1 << 54 },
	} {
		other := r
		mutate(&other)
		if _, err := BindFoundationIntent(other); !errors.Is(err, ErrInvalidFoundationIntent) {
			t.Fatal(err)
		}
	}
	// New foundation data never broadens either frozen Project capability.
	if ManagedAgentCreateProject().ExternalSideEffectAllowed() || ManagedAgentCreateProjectDurable().ExternalSideEffectAllowed() {
		t.Fatal("legacy effect boundary changed")
	}
}
