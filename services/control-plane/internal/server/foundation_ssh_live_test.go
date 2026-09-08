package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/sshtarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type foundationSSHLiveTarget struct {
	TargetID      string `json:"targetId"`
	TargetName    string `json:"targetName"`
	Endpoint      string `json:"endpoint"`
	CredentialRef string `json:"credentialRef"`
}

type foundationSSHLiveInput struct {
	Targets  []foundationSSHLiveTarget `json:"targets"`
	Mismatch foundationSSHLiveTarget   `json:"mismatch"`
}

func TestFoundationSSHProbeSoakPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_SSH_RUNTIME_DATABASE_URL")
	credentialPath := os.Getenv("CLOUD_AGENTS_FOUNDATION_SSH_CREDENTIAL_DIRECTORY")
	phase := os.Getenv("CLOUD_AGENTS_FOUNDATION_SSH_PHASE")
	var input foundationSSHLiveInput
	inputJSON := os.Getenv("CLOUD_AGENTS_FOUNDATION_SSH_TARGETS")
	if runtimeURL == "" || credentialPath == "" || inputJSON == "" || phase == "" {
		t.Skip("isolated external SSH probe environment not configured")
	}
	if (phase != "before-restart" && phase != "after-restart") || json.Unmarshal([]byte(inputJSON), &input) != nil || len(input.Targets) != 2 {
		t.Fatal("external SSH probe environment is invalid")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := postgres.NewDurableCoordinationService(pool)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := sshtarget.NewCredentialDirectory(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	verifier, tokens := foundationVerifierAndScopedTokens(t,
		"projects.act projects.get targets.act targets.create targets.get targets.list operations.list audit.list",
		"projects.get",
	)
	handler, err := NewAdminDeploymentTargetHTTPServer(verifier, store, nil, nil, credentials)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	admin, _ := api.NewHTTPClientWithClient(server.URL, tokens[0], server.Client())
	user, _ := api.NewHTTPClientWithClient(server.URL, tokens[1], server.Client())

	if phase == "before-restart" {
		for _, target := range append(input.Targets, input.Mismatch) {
			created, createErr := admin.RegisterAdminDeploymentTarget(ctx, "tenant", "project", "request-"+target.TargetID+"-create", target.TargetID+"-create-key", platform.DeploymentTargetRegisterRequest{
				TargetID: target.TargetID, TargetName: target.TargetName, TargetKind: "ssh", Endpoint: target.Endpoint, CredentialRef: target.CredentialRef,
			})
			if createErr != nil || created.Value.Spec.ObservedPhase != "unprobed" || created.Value.Spec.Generation != 1 {
				t.Fatalf("register external SSH target %s: value=%+v err=%v", target.TargetID, created.Value, createErr)
			}
		}
		mismatch, probeErr := admin.ProbeAdminDeploymentTarget(ctx, "tenant", "project", input.Mismatch.TargetID, "request-ssh-host-key-mismatch", "ssh-host-key-mismatch-key", platform.DeploymentTargetProbeRequest{ExpectedGeneration: 1})
		if probeErr != nil || mismatch.Value.Spec.ObservedPhase != "unavailable" || mismatch.Value.Spec.StableErrorCode != "ssh-host-key-mismatch" {
			t.Fatalf("mismatched SSH host key: value=%+v err=%v", mismatch.Value, probeErr)
		}
	}

	const cycles = 16
	probeLatency := make([]time.Duration, 0, cycles*len(input.Targets))
	readLatency := make([]time.Duration, 0, cycles*len(input.Targets))
	deniedLatency := make([]time.Duration, 0, cycles)
	facts := make(map[string]string, len(input.Targets))
	var recoveryToFirstSuccess time.Duration
	for cycle := 0; cycle < cycles; cycle++ {
		for _, target := range input.Targets {
			prefix := fmt.Sprintf("request-ssh-%s-%02d-%s", phase, cycle, target.TargetID)
			probed, probeErr := timedFoundationRequest(&probeLatency, func() (api.DeploymentTargetResult, error) {
				return admin.ProbeAdminDeploymentTarget(ctx, "tenant", "project", target.TargetID, prefix+"-probe", prefix+"-probe-key", platform.DeploymentTargetProbeRequest{ExpectedGeneration: 1})
			})
			if probeErr != nil || probed.Value.Spec.ObservedPhase != "ready" || probed.Value.Spec.APIVersion != "2.0" || probed.Value.Spec.EngineVersion == "" || probed.Value.Spec.OS != "linux" || probed.Value.Spec.Architecture != "amd64" {
				t.Fatalf("probe external SSH target %s cycle %d: value=%+v err=%v", target.TargetID, cycle, probed.Value, probeErr)
			}
			if cycle == 0 && target.TargetID == input.Targets[0].TargetID {
				recoveryToFirstSuccess = time.Since(started)
			}
			currentFacts := probed.Value.Spec.APIVersion + "|" + probed.Value.Spec.EngineVersion + "|" + probed.Value.Spec.OS + "|" + probed.Value.Spec.Architecture
			if facts[target.TargetID] == "" {
				facts[target.TargetID] = currentFacts
			} else if facts[target.TargetID] != currentFacts {
				t.Fatalf("external SSH facts drifted for %s: %q != %q", target.TargetID, currentFacts, facts[target.TargetID])
			}
			read, readErr := timedFoundationRequest(&readLatency, func() (api.DeploymentTargetResult, error) {
				return admin.GetAdminDeploymentTarget(ctx, "tenant", "project", target.TargetID, prefix+"-get")
			})
			if readErr != nil || read.Value.Spec.ObservedPhase != "ready" || read.Value.Spec.EngineVersion != probed.Value.Spec.EngineVersion {
				t.Fatalf("read external SSH target %s cycle %d: value=%+v err=%v", target.TargetID, cycle, read.Value, readErr)
			}
		}
		_, denied := timedFoundationRequest(&deniedLatency, func() (api.DeploymentTargetPageResult, error) {
			return user.ListAdminDeploymentTargets(ctx, "tenant", "project", fmt.Sprintf("request-ssh-%s-%02d-user", phase, cycle), 50, "")
		})
		if clientStatus(denied) != http.StatusForbidden {
			t.Fatalf("ordinary user external SSH Admin cycle %d status=%d err=%v", cycle, clientStatus(denied), denied)
		}
	}

	operationCounts := make(map[string]int, len(input.Targets))
	auditCounts := make(map[string]int, len(input.Targets))
	for _, target := range input.Targets {
		operations, listErr := admin.ListAdminDeploymentTargetOperations(ctx, "tenant", "project", target.TargetID, "request-"+target.TargetID+"-operations-"+phase, 50, "")
		if listErr != nil || len(operations.Value.Operations) < cycles {
			t.Fatalf("external SSH operations %s: count=%d err=%v", target.TargetID, len(operations.Value.Operations), listErr)
		}
		audits, listErr := admin.ListAdminDeploymentTargetAuditEvents(ctx, "tenant", "project", target.TargetID, "request-"+target.TargetID+"-audits-"+phase, 50, "")
		if listErr != nil || len(audits.Value.Events) < cycles {
			t.Fatalf("external SSH audits %s: count=%d err=%v", target.TargetID, len(audits.Value.Events), listErr)
		}
		operationCounts[target.TargetID] = len(operations.Value.Operations)
		auditCounts[target.TargetID] = len(audits.Value.Events)
	}
	mismatch, err := admin.GetAdminDeploymentTarget(ctx, "tenant", "project", input.Mismatch.TargetID, "request-ssh-mismatch-get-"+phase)
	if err != nil || mismatch.Value.Spec.ObservedPhase != "unavailable" || mismatch.Value.Spec.StableErrorCode != "ssh-host-key-mismatch" {
		t.Fatalf("persisted SSH host-key mismatch: value=%+v err=%v", mismatch.Value, err)
	}
	receipt, _ := json.Marshal(map[string]any{
		"phase": phase, "cycles": cycles, "targetCount": len(input.Targets), "probeRequests": len(probeLatency),
		"readRequests": len(readLatency), "deniedRequests": len(deniedLatency),
		"recoveryToFirstSuccessMilliseconds": durationMilliseconds(recoveryToFirstSuccess),
		"probeLatencyMilliseconds":           foundationLatencySummary(probeLatency),
		"readLatencyMilliseconds":            foundationLatencySummary(readLatency),
		"deniedLatencyMilliseconds":          foundationLatencySummary(deniedLatency),
		"facts":                              facts, "operationCounts": operationCounts, "auditCounts": auditCounts,
		"hostKeyMismatchStableError": mismatch.Value.Spec.StableErrorCode,
	})
	t.Logf("FOUNDATION_SSH_SOAK=%s", receipt)
}
