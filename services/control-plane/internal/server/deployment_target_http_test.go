package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
)

type deploymentTargetStoreFake struct {
	snapshot   internaldeploymenttarget.Snapshot
	register   int
	get        int
	begin      int
	complete   int
	completion internaldeploymenttarget.ProbeCompletion
}

func (fake *deploymentTargetStoreFake) RegisterDeploymentTarget(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internaldeploymenttarget.RegisterInput) (internaldeploymenttarget.Snapshot, error) {
	fake.register++
	fake.snapshot.Scope = input.Scope
	fake.snapshot.TargetID = input.TargetID
	fake.snapshot.TargetName = input.TargetName
	fake.snapshot.Kind = input.Kind
	fake.snapshot.Endpoint = input.Endpoint
	fake.snapshot.CredentialRef = input.CredentialRef
	return fake.snapshot, nil
}

func (fake *deploymentTargetStoreFake) GetDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, string, string) (internaldeploymenttarget.Snapshot, error) {
	fake.get++
	return fake.snapshot, nil
}

func (fake *deploymentTargetStoreFake) BeginDeploymentTargetProbe(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ internaldeploymenttarget.ProbeInput) (internaldeploymenttarget.ProbeStart, error) {
	fake.begin++
	fake.snapshot.ObservedPhase = "probing"
	fake.snapshot.ResourceVersion++
	return internaldeploymenttarget.ProbeStart{Target: fake.snapshot, Execute: true}, nil
}

func (fake *deploymentTargetStoreFake) CompleteDeploymentTargetProbe(_ context.Context, _ string, _ *authn.VerifiedPrincipal, completion internaldeploymenttarget.ProbeCompletion) (internaldeploymenttarget.Snapshot, error) {
	fake.complete++
	fake.completion = completion
	now := fake.snapshot.UpdatedAt.Add(time.Second)
	fake.snapshot.LastProbeAt = &now
	fake.snapshot.UpdatedAt = now
	fake.snapshot.ResourceVersion++
	if completion.Succeeded {
		fake.snapshot.ObservedPhase = "ready"
		fake.snapshot.APIVersion = completion.APIVersion
		fake.snapshot.EngineVersion = completion.EngineVersion
		fake.snapshot.OS = completion.OS
		fake.snapshot.Arch = completion.Arch
	} else {
		fake.snapshot.ObservedPhase = "unavailable"
		fake.snapshot.StableErrorCode = completion.StableErrorCode
	}
	return fake.snapshot, nil
}

func TestDeploymentTargetHTTPRegisterGetAndSettledProbe(t *testing.T) {
	now := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	store := &deploymentTargetStoreFake{snapshot: internaldeploymenttarget.Snapshot{Generation: 1, ObservedPhase: "unprobed", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now}}
	handler, err := NewDeploymentTargetHTTPServer(verifier, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, requestID, idempotencyKey string) *httptest.ResponseRecorder {
		value := httptest.NewRequest(method, path, strings.NewReader(body))
		value.Header.Set("Authorization", "Bearer access-token")
		value.Header.Set("X-Request-ID", requestID)
		if body != "" {
			value.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			value.Header.Set("Idempotency-Key", idempotencyKey)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, value)
		return response
	}
	created := request(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets", `{"targetId":"docker-alpha","targetName":"docker-alpha","targetKind":"docker","endpoint":"https://docker.example.test:2376","credentialRef":"docker-alpha"}`, "request-register", "register-key-123456")
	if created.Code != http.StatusCreated || store.register != 1 || verifier.seen.RequiredPermission != "projects.act" {
		t.Fatalf("register status=%d calls=%d verification=%#v body=%s", created.Code, store.register, verifier.seen, created.Body.String())
	}
	if _, err := openapiv1alpha1.DecodeDeploymentTargetResponseJSON(created.Body.Bytes()); err != nil {
		t.Fatalf("register response contract: %v", err)
	}
	got := request(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha", "", "request-get", "")
	if got.Code != http.StatusOK || store.get != 1 || verifier.seen.RequiredPermission != "projects.get" {
		t.Fatalf("get status=%d calls=%d verification=%#v body=%s", got.Code, store.get, verifier.seen, got.Body.String())
	}
	probed := request(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:probe", `{"expectedGeneration":1}`, "request-probe", "probe-key-12345678")
	if probed.Code != http.StatusOK || store.begin != 1 || store.complete != 1 || verifier.calls != 4 || store.completion.Succeeded || store.completion.StableErrorCode != "docker-probe-unconfigured" || !strings.Contains(probed.Body.String(), `"observedPhase":"unavailable"`) {
		t.Fatalf("probe status=%d begin=%d complete=%d verifier=%d completion=%#v body=%s", probed.Code, store.begin, store.complete, verifier.calls, store.completion, probed.Body.String())
	}
	if strings.Contains(probed.Body.String(), "PRIVATE KEY") {
		t.Fatal("probe response leaked credential bytes")
	}
}

func TestDeploymentTargetPathDoesNotCaptureProjectRoutes(t *testing.T) {
	if HandlesDeploymentTargetPath("/v1/tenants/tenant-alpha/projects/project-alpha") || !HandlesDeploymentTargetPath("/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:probe") {
		t.Fatal("deployment target route ownership drifted")
	}
}
