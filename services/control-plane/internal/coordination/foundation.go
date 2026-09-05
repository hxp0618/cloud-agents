package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
)

var ErrInvalidFoundationIntent = errors.New("invalid resolved foundation intent")
var foundationImage = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$`)

// FoundationResolved is trusted server-resolved data, never a public user request.
// It carries no endpoint, credentials, commands or Workspace content.
type FoundationResolved struct {
	Tenant, Project, Workspace, WorkspaceName, Volume, Target, Sandbox, ImageURI string
	CPUMillis, MemoryBytes                                                       int64
}

// FoundationIntent binds a snapshot to its canonical digest; it is not authorization
// or a durable claim. Only an authorized store transaction may persist it.
type FoundationIntent struct {
	resolved FoundationResolved
	digest   string
}

func (i FoundationIntent) Resolved() FoundationResolved { return i.resolved }
func (i FoundationIntent) RequestDigest() string        { return i.digest }

func BindFoundationIntent(r FoundationResolved) (FoundationIntent, error) {
	for _, id := range []string{r.Tenant, r.Project, r.Workspace, r.WorkspaceName, r.Volume, r.Target, r.Sandbox} {
		if !validIdentifier(id) {
			return FoundationIntent{}, ErrInvalidFoundationIntent
		}
	}
	if len(r.ImageURI) > 1024 || !foundationImage.MatchString(r.ImageURI) ||
		r.CPUMillis < 100 || r.CPUMillis > 64000 || r.MemoryBytes < 134217728 || r.MemoryBytes > 1099511627776 {
		return FoundationIntent{}, ErrInvalidFoundationIntent
	}
	// All strings are ASCII with no JSON/HTML escapes; all integers are < 2^53.
	// encoding/json's sorted map keys therefore match RFC8785 for this closed domain.
	canonical, err := json.Marshal(map[string]any{
		"tenant": r.Tenant, "project": r.Project, "workspace": r.Workspace,
		"workspaceName": r.WorkspaceName, "volume": r.Volume, "target": r.Target,
		"sandbox": r.Sandbox, "imageURI": r.ImageURI, "cpuMillis": r.CPUMillis,
		"memoryBytes": r.MemoryBytes, "profileId": SandboxLifecycleProfileID,
	})
	if err != nil {
		return FoundationIntent{}, ErrInvalidFoundationIntent
	}
	sum := sha256.Sum256(canonical)
	return FoundationIntent{resolved: r, digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}
