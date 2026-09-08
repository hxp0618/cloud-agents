package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFoundationAdminReadSoakPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL")
	phase := os.Getenv("CLOUD_AGENTS_FOUNDATION_ADMIN_SOAK_PHASE")
	if runtimeURL == "" || (phase != "before-restart" && phase != "after-restart") {
		t.Skip("isolated foundation Admin soak environment not configured")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	verifier, adminToken, userToken := foundationVerifierAndTokens(t)
	store, err := postgres.NewDurableCoordinationService(pool)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewFoundationHTTPServer(verifier, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	admin, err := api.NewHTTPClientWithClient(server.URL, adminToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	user, err := api.NewHTTPClientWithClient(server.URL, userToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	const cycles = 64
	successLatency := make([]time.Duration, 0, cycles*6)
	deniedLatency := make([]time.Duration, 0, cycles)
	var stateDigest string
	var recoveryToFirstSuccess time.Duration
	var profileCount, sandboxCount, snapshotCount int
	for cycle := 0; cycle < cycles; cycle++ {
		requestPrefix := fmt.Sprintf("request-admin-soak-%s-%02d", phase, cycle)
		profiles, err := timedFoundationRequest(&successLatency, func() (api.RuntimeProfilePageResult, error) {
			return admin.ListAdminRuntimeProfiles(ctx, "tenant", "project", requestPrefix+"-profiles", 50, "")
		})
		if err != nil {
			t.Fatalf("list profiles cycle %d: %v", cycle, err)
		}
		if cycle == 0 {
			recoveryToFirstSuccess = time.Since(started)
		}
		profile, err := timedFoundationRequest(&successLatency, func() (api.RuntimeProfileResult, error) {
			return admin.GetAdminRuntimeProfile(ctx, "tenant", "project", "profile", 1, requestPrefix+"-profile")
		})
		if err != nil {
			t.Fatalf("get profile cycle %d: %v", cycle, err)
		}
		sandboxes, err := timedFoundationRequest(&successLatency, func() (api.AdminSandboxSessionPageResult, error) {
			return admin.ListAdminSandboxSessions(ctx, "tenant", "project", requestPrefix+"-sandboxes", 50, "")
		})
		if err != nil {
			t.Fatalf("list sandboxes cycle %d: %v", cycle, err)
		}
		sandbox, err := timedFoundationRequest(&successLatency, func() (api.AdminSandboxSessionResult, error) {
			return admin.GetAdminSandboxSession(ctx, "tenant", "project", "sandbox", requestPrefix+"-sandbox")
		})
		if err != nil {
			t.Fatalf("get sandbox cycle %d: %v", cycle, err)
		}
		snapshots, err := timedFoundationRequest(&successLatency, func() (api.WorkspaceSnapshotPageResult, error) {
			return admin.ListAdminWorkspaceSnapshots(ctx, "tenant", "project", requestPrefix+"-snapshots", 50, "")
		})
		if err != nil {
			t.Fatalf("list snapshots cycle %d: %v", cycle, err)
		}
		snapshot, err := timedFoundationRequest(&successLatency, func() (api.WorkspaceSnapshotResult, error) {
			return admin.GetAdminWorkspaceSnapshot(ctx, "tenant", "project", "snapshot", requestPrefix+"-snapshot")
		})
		if err != nil {
			t.Fatalf("get snapshot cycle %d: %v", cycle, err)
		}
		_, denied := timedFoundationRequest(&deniedLatency, func() (api.AdminSandboxSessionPageResult, error) {
			return user.ListAdminSandboxSessions(ctx, "tenant", "project", requestPrefix+"-user-denied", 50, "")
		})
		if clientStatus(denied) != http.StatusForbidden {
			t.Fatalf("ordinary user cycle %d status=%d err=%v", cycle, clientStatus(denied), denied)
		}

		profileCount = len(profiles.Value.RuntimeProfiles)
		sandboxCount = len(sandboxes.Value.SandboxSessions)
		snapshotCount = len(snapshots.Value.WorkspaceSnapshots)
		if profileCount < 1 || sandboxCount < 1 || snapshotCount != 1 ||
			profile.Value.Spec.ProfileID != "profile" || sandbox.Value.Metadata.UID != "sandbox" ||
			snapshot.Value.Metadata.UID != "snapshot" {
			t.Fatalf("unexpected persisted resources cycle %d: profiles=%d sandboxes=%d snapshots=%d", cycle, profileCount, sandboxCount, snapshotCount)
		}
		encoded, err := json.Marshal([]any{profiles.Value, profile.Value, sandboxes.Value, sandbox.Value, snapshots.Value, snapshot.Value})
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"credentialRef", "providerCredentialRef", "endpoint", "physicalSnapshot", "fixture-only", "127.0.0.1", "OPEN-SANDBOX", "fileContent", "prompt"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("Admin soak response disclosed %q", forbidden)
			}
		}
		digest := sha256.Sum256(encoded)
		currentDigest := "sha256:" + hex.EncodeToString(digest[:])
		if stateDigest == "" {
			stateDigest = currentDigest
		} else if currentDigest != stateDigest {
			t.Fatalf("persisted Admin state changed during soak: %s != %s", currentDigest, stateDigest)
		}
	}
	receipt, _ := json.Marshal(map[string]any{
		"phase": phase, "cycles": cycles, "successfulRequests": len(successLatency), "deniedRequests": len(deniedLatency),
		"recoveryToFirstSuccessMilliseconds": durationMilliseconds(recoveryToFirstSuccess),
		"successLatencyMilliseconds":         foundationLatencySummary(successLatency),
		"deniedLatencyMilliseconds":          foundationLatencySummary(deniedLatency),
		"stateDigest":                        stateDigest, "resourceCounts": map[string]int{"profiles": profileCount, "sandboxes": sandboxCount, "snapshots": snapshotCount},
		"durationMilliseconds": durationMilliseconds(time.Since(started)),
	})
	t.Logf("FOUNDATION_ADMIN_SOAK=%s", receipt)
}

func TestFoundationAdminLatencySummary(t *testing.T) {
	summary := foundationLatencySummary([]time.Duration{
		20 * time.Millisecond, time.Millisecond, 19 * time.Millisecond, 10 * time.Millisecond,
		2 * time.Millisecond, 18 * time.Millisecond, 9 * time.Millisecond, 11 * time.Millisecond,
		3 * time.Millisecond, 17 * time.Millisecond, 8 * time.Millisecond, 12 * time.Millisecond,
		4 * time.Millisecond, 16 * time.Millisecond, 7 * time.Millisecond, 13 * time.Millisecond,
		5 * time.Millisecond, 15 * time.Millisecond, 6 * time.Millisecond, 14 * time.Millisecond,
	})
	if summary["p50"] != 10 || summary["p95"] != 19 || summary["max"] != 20 {
		t.Fatalf("nearest-rank latency summary = %+v", summary)
	}
}

func timedFoundationRequest[T any](samples *[]time.Duration, call func() (T, error)) (T, error) {
	started := time.Now()
	value, err := call()
	*samples = append(*samples, time.Since(started))
	return value, err
}

func foundationLatencySummary(samples []time.Duration) map[string]float64 {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	return map[string]float64{
		"p50": durationMilliseconds(sorted[(len(sorted)*50+99)/100-1]),
		"p95": durationMilliseconds(sorted[(len(sorted)*95+99)/100-1]),
		"max": durationMilliseconds(sorted[len(sorted)-1]),
	}
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
