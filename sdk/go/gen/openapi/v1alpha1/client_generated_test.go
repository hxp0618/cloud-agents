package v1alpha1

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
)

func TestGeneratedOpenAPIClientUsesFixtureTransportOnly(t *testing.T) {
	requestBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-create-request.json")
	projectBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project.json")
	projectGetResponse := responseFixture(t, "project", 200)
	projectGetResponse.Body = []byte(strings.Replace(string(projectGetResponse.Body), `"name": "project-alpha"`, `"name": "project-roundtrip"`, 1))
	if string(projectGetResponse.Body) == string(projectBody) {
		t.Fatal("project GET fixture must distinguish metadata.uid from metadata.name")
	}
	mutationBody := []byte(`{"resourceUid":"membership-new","resourceVersion":"8","state":"active"}`)
	resumeBody := []byte(`{"resourceUid":"membership-new","resourceVersion":"9","state":"active"}`)
	membershipBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/membership.json")
	membershipPageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"MembershipPage","memberships":[` + string(membershipBody) + `],"nextPageToken":"membership-page-token-2"}`)
	roleBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/role.json")
	rolePageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RolePage","roles":[` + string(roleBody) + `],"nextPageToken":"role-page-token-2"}`)
	roleBindingBody := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/role-binding.json")
	roleBindingPageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RoleBindingPage","roleBindings":[` + string(roleBindingBody) + `],"nextPageToken":"role-binding-page-token-2"}`)
	responses := map[string]Response{
		"GET /v1/tenants/tenant-alpha":                                                                                      responseFixture(t, "platform-tenant", 200),
		"GET /v1/tenants/tenant-alpha/organizations/organization-alpha":                                                     responseFixture(t, "organization", 200),
		"GET /v1/tenants/tenant-alpha/organizations?pageSize=1&pageToken=organization-page-token-1":                         {Status: 200, Body: readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/organization-page.json")},
		"GET /v1/tenants/tenant-alpha/projects/project-alpha":                                                               projectGetResponse,
		"GET /v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageSize=1&pageToken=project-page-token-1": {Status: 200, Body: readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-page.json")},
		"GET /v1/tenants/tenant-alpha/memberships/membership-alpha":                                                         responseFixture(t, "membership", 200),
		"GET /v1/tenants/tenant-alpha/memberships?pageSize=1&pageToken=membership-page-token-1":                             {Status: 200, Body: membershipPageBody},
		"GET /v1/tenants/tenant-alpha/roles/role-project-viewer-v1":                                                         responseFixture(t, "role", 200),
		"GET /v1/tenants/tenant-alpha/roles?pageSize=1&pageToken=role-page-token-1":                                         {Status: 200, Body: rolePageBody},
		"GET /v1/tenants/tenant-alpha/role-bindings/role-binding-alpha":                                                     responseFixture(t, "role-binding", 200),
		"GET /v1/tenants/tenant-alpha/role-bindings?pageSize=1&pageToken=role-binding-page-token-1":                         {Status: 200, Body: roleBindingPageBody},
		"GET /v1/managed-host/tenants/tenant-alpha/projects/project-alpha":                                                  projectGetResponse,
		"GET /v1/managed-host/tenants/tenant-alpha/role-bindings/role-binding-alpha":                                        responseFixture(t, "role-binding", 200),
		"POST /v1/tenants/tenant-alpha/organizations":                                                                       {Status: 201, Headers: map[string]string{HeaderResourceVersion: "2"}, Body: responseFixture(t, "organization", 200).Body},
		"POST /v1/tenants/tenant-alpha/projects":                                                                            {Status: 201, Headers: map[string]string{HeaderResourceVersion: "3"}, Body: projectBody},
		"POST /v1/tenants/tenant-alpha/memberships":                                                                         {Status: 201, Headers: map[string]string{HeaderResourceVersion: "8"}, Body: mutationBody},
		"POST /v1/tenants/tenant-alpha/memberships/membership-new:resume":                                                   {Status: 200, Headers: map[string]string{HeaderResourceVersion: "9"}, Body: resumeBody},
	}
	var seen []Request
	client, err := NewClient(TransportFunc(func(ctx context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		return responses[request.Method+" "+request.Path], nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.GetPlatformTenant(ctx, "tenant-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetOrganization(ctx, "tenant-alpha", "organization-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListOrganizations(ctx, "tenant-alpha", "req-alpha", 1, "organization-page-token-1"); err != nil || len(page.Value.Organizations) != 1 {
		t.Fatalf("organization page = %#v / %v", page, err)
	}
	if _, err := client.GetProject(ctx, "tenant-alpha", "project-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListProjects(ctx, "tenant-alpha", "organization-alpha", "req-alpha", 1, "project-page-token-1"); err != nil || len(page.Value.Projects) != 1 {
		t.Fatalf("project page = %#v / %v", page, err)
	}
	if _, err := client.GetMembership(ctx, "tenant-alpha", "membership-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListMemberships(ctx, "tenant-alpha", "req-alpha", 1, "membership-page-token-1"); err != nil || len(page.Value.Memberships) != 1 || page.Value.NextPageToken != "membership-page-token-2" {
		t.Fatalf("membership page = %#v / %v", page, err)
	}
	if _, err := client.GetRole(ctx, "tenant-alpha", "role-project-viewer-v1", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListRoles(ctx, "tenant-alpha", "req-alpha", 1, "role-page-token-1"); err != nil || len(page.Value.Roles) != 1 || page.Value.NextPageToken != "role-page-token-2" {
		t.Fatalf("role page = %#v / %v", page, err)
	}
	if _, err := client.GetRoleBinding(ctx, "tenant-alpha", "role-binding-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListRoleBindings(ctx, "tenant-alpha", "req-alpha", 1, "role-binding-page-token-1"); err != nil || len(page.Value.RoleBindings) != 1 || page.Value.NextPageToken != "role-binding-page-token-2" {
		t.Fatalf("role binding page = %#v / %v", page, err)
	}
	if _, err := client.GetProjectContext(ctx, "tenant-alpha", "project-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetManagedHostRoleBinding(ctx, "tenant-alpha", "role-binding-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateOrganization(ctx, "tenant-alpha", "req-alpha", platform.OrganizationCreateRequest{ExpectedTenantRevision: 1, OrganizationID: "organization-alpha", Name: "organization-alpha", DisplayName: "Organization Alpha", AuditFactUID: "audit-organization", ReasonCode: "operator-request"}); err != nil {
		t.Fatal(err)
	}
	body, err := platform.DecodeProjectCreateRequestJSON(requestBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateProject(ctx, "tenant-alpha", "req-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateMembership(ctx, "tenant-alpha", "req-alpha", platform.MembershipCreateRequest{ExpectedTenantRevision: 7, MembershipID: "membership-new", MembershipName: "membership-new", Subject: common.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"}, Scope: common.AuthorizationScope{Level: "tenant", Ref: rawTenantRef("tenant-alpha")}, AuditFactUID: "audit-create", ReasonCode: "operator-request"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResumeMembership(ctx, "tenant-alpha", "membership-new", "req-alpha", platform.MembershipTransitionRequest{ExpectedTenantRevision: 8, ExpectedResourceVersion: 8, AuditFactUID: "audit-resume", ReasonCode: "operator-request"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 17 {
		t.Fatalf("transport calls = %d, want 17", len(seen))
	}
	for _, request := range seen {
		if request.Headers[HeaderRequestID] != "req-alpha" {
			t.Fatalf("request headers = %#v", request.Headers)
		}
	}
	sentBody, sentErr := platform.DecodeProjectCreateRequestJSON(seen[14].Body)
	if seen[14].Headers[HeaderIdempotencyKey] == "" || sentErr != nil || sentBody != body {
		t.Fatalf("create request = %#v", seen[14])
	}
	if string(seen[16].Body) != `{"expectedTenantRevision":8,"expectedResourceVersion":8,"auditFactUid":"audit-resume","reasonCode":"operator-request"}` {
		t.Fatalf("resume request = %#v", seen[16])
	}
}

func rawTenantRef(id string) *json.RawMessage {
	raw := json.RawMessage(`{"namespace":"cloud-agents","kind":"tenant","id":"` + id + `"}`)
	return &raw
}

func TestGeneratedOpenAPIClientUsesAdminDeploymentTargetRoute(t *testing.T) {
	page := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"DeploymentTargetPage","deploymentTargets":[]}`)
	var seen Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = request
		return Response{Status: 200, Body: page}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAdminDeploymentTargets(context.Background(), "tenant-alpha", "project-alpha", "req-alpha", 1, "target-page-token-1"); err != nil {
		t.Fatal(err)
	}
	if seen.Path != "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=1&pageToken=target-page-token-1" {
		t.Fatalf("path = %q", seen.Path)
	}
}

func TestGeneratedOpenAPIClientManagedAgentSessionLifecycle(t *testing.T) {
	sessionBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Session","metadata":{"uid":"session-alpha","projectId":"project-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"providerKind":"codex","state":"active"}}`)
	sessionPageBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"SessionPage","sessions":[{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Session","metadata":{"uid":"session-alpha","projectId":"project-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"providerKind":"codex","state":"active"}}],"nextPageToken":"session-page-token-1"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		body := sessionBody
		if request.Method == "GET" && strings.Contains(request.Path, "/sessions?") {
			body = sessionPageBody
		}
		return Response{Status: map[string]int{"POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions": 201, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha": 200, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions?pageSize=1&pageToken=session-page-token-1": 200, "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha:close": 200}[request.Method+" "+request.Path], Headers: map[string]string{HeaderResourceVersion: "2"}, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := client.CreateManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", ManagedAgentSessionCreateRequest{SessionID: "session-alpha", ProviderKind: "codex", EnvironmentLeaseID: "lease-alpha"})
	if err != nil || created.Value.Spec.ProviderKind != "codex" {
		t.Fatalf("create = %#v / %v", created, err)
	}
	if _, err := client.GetManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListManagedAgentSessions(ctx, "tenant-alpha", "project-alpha", "request-alpha", 1, "session-page-token-1"); err != nil || len(page.Value.Sessions) != 1 || page.Value.NextPageToken == "" {
		t.Fatalf("session page = %#v / %v", page, err)
	}
	if _, err := client.CloseManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R3"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || string(seen[0].Body) != `{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"lease-alpha"}` || seen[2].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions?pageSize=1&pageToken=session-page-token-1" || seen[3].Body != nil {
		t.Fatalf("session requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientManagedAgentTurnLifecycle(t *testing.T) {
	turnBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Turn","metadata":{"uid":"turn-alpha","projectId":"project-alpha","sessionId":"session-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"inputDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"queued"}}`)
	turnPageBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"TurnPage","turns":[{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Turn","metadata":{"uid":"turn-alpha","projectId":"project-alpha","sessionId":"session-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"inputDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"queued"}}],"nextPageToken":"turn-page-token-1"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		body := turnBody
		if request.Method == "GET" && strings.Contains(request.Path, "/turns?") {
			body = turnPageBody
		}
		return Response{Status: map[string]int{"POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns": 201, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha": 200, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns?pageSize=1&pageToken=turn-page-token-1": 200}[request.Method+" "+request.Path], Headers: map[string]string{HeaderResourceVersion: "2"}, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.CreateManagedAgentTurn(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R4", ManagedAgentTurnCreateRequest{TurnID: "turn-alpha", InputText: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetManagedAgentTurn(ctx, "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "request-alpha"); err != nil {
		t.Fatal(err)
	}
	if page, err := client.ListManagedAgentTurns(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", 1, "turn-page-token-1"); err != nil || len(page.Value.Turns) != 1 || page.Value.NextPageToken == "" {
		t.Fatalf("turn page = %#v / %v", page, err)
	}
	if len(seen) != 3 || seen[2].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns?pageSize=1&pageToken=turn-page-token-1" {
		t.Fatalf("turn requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientManagedAgentExecutionLifecycle(t *testing.T) {
	executionBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Execution","metadata":{"uid":"execution-alpha","projectId":"project-alpha","sessionId":"session-alpha","turnId":"turn-alpha","resourceVersion":"3","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"generation":1,"state":"succeeded","resultDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"messages":[{"requestId":"request-alpha","protocolVersion":{"major":2,"minor":3},"executionId":"execution-alpha","generation":1,"commandId":"command-alpha","occurredAt":"2026-08-29T08:01:00Z","messageType":"Result","payload":{"text":"done"}}]}`)
	executionPageBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"ExecutionPage","executions":[{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Execution","metadata":{"uid":"execution-alpha","projectId":"project-alpha","sessionId":"session-alpha","turnId":"turn-alpha","resourceVersion":"3","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"generation":1,"state":"succeeded","resultDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}],"nextPageToken":"execution-page-token-2"}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		if strings.HasSuffix(request.Path, "/messages/0/artifact") {
			return Response{Status: 200, Headers: map[string]string{"Content-Type": "text/plain", "Content-Disposition": `attachment; filename="result.txt"`, "Etag": `"sha256:artifact"`}, Body: []byte("artifact bytes")}, nil
		}
		body := executionBody
		if request.Method == "GET" && strings.Contains(request.Path, "/executions?") {
			body = executionPageBody
		}
		status := 200
		if strings.HasSuffix(request.Path, ":resolveApproval") || strings.HasSuffix(request.Path, ":resolveUserInput") {
			status = 204
		}
		return Response{Status: status, Headers: map[string]string{HeaderResourceVersion: "3"}, Body: body}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ExecuteManagedAgent(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", ManagedAgentExecutionCreateRequest{TurnID: "turn-alpha", ExecutionID: "execution-alpha", Model: "codex", RuntimeMode: "approval-required", InteractionMode: "plan", InputText: "hello"})
	if err != nil || len(result.Value.Messages) != 1 || result.Value.Messages[0].MessageType != "Result" {
		t.Fatalf("execute = %#v / %v", result, err)
	}
	if _, err := encodeManagedAgentExecutionCreateRequest(ManagedAgentExecutionCreateRequest{TurnID: "turn-alpha", ExecutionID: "execution-alpha", RuntimeMode: "always-allow", InputText: "hello"}); err == nil {
		t.Fatal("execution request accepted an invalid runtime mode")
	}
	if _, err := encodeManagedAgentExecutionCreateRequest(ManagedAgentExecutionCreateRequest{TurnID: "turn-alpha", ExecutionID: "execution-alpha", InteractionMode: "chat", InputText: "hello"}); err == nil {
		t.Fatal("execution request accepted an invalid interaction mode")
	}
	if page, err := client.ListManagedAgentExecutions(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "request-list", 1, "execution-page-token-1"); err != nil || len(page.Value.Executions) != 1 || page.Value.NextPageToken == "" || page.Value.Executions[0].Messages != nil {
		t.Fatalf("execution page = %#v / %v", page, err)
	}
	if _, err := client.GetManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-alpha"); err != nil {
		t.Fatal(err)
	}
	artifact, err := client.DownloadManagedAgentArtifact(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-artifact", 0)
	if err != nil || string(artifact.Data) != "artifact bytes" || artifact.FileName != "result.txt" || artifact.ContentType != "text/plain" {
		t.Fatalf("artifact = %#v / %v", artifact, err)
	}
	if _, err := client.DownloadManagedAgentArtifact(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-artifact", 64); err == nil {
		t.Fatal("out-of-range message index was accepted")
	}
	if _, err := client.CancelManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-cancel", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX", ManagedAgentExecutionCancelRequest{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.InterruptManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-interrupt", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EY", ManagedAgentExecutionInterruptRequest{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveManagedAgentApproval(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-approval", ManagedAgentApprovalResolutionRequest{Generation: 1, RequestID: "codex:generation-1:approval:1", Decision: "accept"}); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveManagedAgentUserInput(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-user-input", ManagedAgentUserInputResolutionRequest{Generation: 1, RequestID: "claude:generation-1:user-input:2", Answers: map[string][]string{"question-1": {"one", "two"}}}); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveManagedAgentUserInput(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-user-input-invalid", ManagedAgentUserInputResolutionRequest{Generation: 1, RequestID: "claude:generation-1:user-input:2", Answers: map[string][]string{"question-1": {"bad\x00answer"}}}); err == nil {
		t.Fatal("user-input resolution accepted a NUL answer")
	}
	if len(seen) != 8 || seen[0].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions" || seen[1].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions?pageSize=1&pageToken=execution-page-token-1" || seen[2].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha" || seen[3].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha/messages/0/artifact" || seen[4].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:cancel" || seen[5].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:interrupt" || seen[6].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveApproval" || seen[7].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveUserInput" || string(seen[0].Body) != `{"turnId":"turn-alpha","executionId":"execution-alpha","model":"codex","runtimeMode":"approval-required","interactionMode":"plan","inputText":"hello"}` || string(seen[4].Body) != `{"generation":1}` || string(seen[5].Body) != `{"generation":1}` || string(seen[6].Body) != `{"generation":1,"requestId":"codex:generation-1:approval:1","decision":"accept"}` || string(seen[7].Body) != `{"generation":1,"requestId":"claude:generation-1:user-input:2","answers":{"question-1":["one","two"]}}` {
		t.Fatalf("execution requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientRejectsMissingArtifactDisposition(t *testing.T) {
	client, err := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		return Response{Status: 200, Headers: map[string]string{"Content-Type": "text/plain"}, Body: []byte("artifact")}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.DownloadManagedAgentArtifact(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-artifact", 0); err == nil || !strings.Contains(err.Error(), "Content-Disposition is invalid") {
		t.Fatalf("missing Content-Disposition error=%v", err)
	}
}

func TestGeneratedOpenAPIClientErrorAndCancellationBoundaries(t *testing.T) {
	problem := readOpenAPIFixture(t, "common/v1alpha1/fixtures/golden/problem.json")
	transportCalls := 0
	client, err := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		transportCalls++
		return Response{Status: 404, Body: problem}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetProject(context.Background(), "tenant-alpha", "project-alpha", "req-alpha")
	var clientErr *ClientError
	if !errors.As(err, &clientErr) || clientErr.Problem == nil || clientErr.Problem.Status != 404 {
		t.Fatalf("problem error = %#v", err)
	}
	statusMismatchClient, _ := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		return Response{Status: 500, Body: problem}, nil
	}))
	_, err = statusMismatchClient.GetProject(context.Background(), "tenant-alpha", "project-alpha", "req-alpha")
	if !errors.As(err, &clientErr) || clientErr.Cause == nil || !strings.Contains(clientErr.Cause.Error(), "PROBLEM_STATUS_MISMATCH") {
		t.Fatalf("status mismatch = %#v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.GetProject(cancelled, "tenant-alpha", "project-alpha", "req-alpha")
	if !errors.Is(err, context.Canceled) || transportCalls != 1 {
		t.Fatalf("cancel = %v, transport calls = %d", err, transportCalls)
	}
	expired, expiredCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expiredCancel()
	_, err = client.GetProject(expired, "tenant-alpha", "project-alpha", "req-alpha")
	if !errors.Is(err, context.DeadlineExceeded) || transportCalls != 1 {
		t.Fatalf("deadline = %v, transport calls = %d", err, transportCalls)
	}
	if _, err := client.GetProject(context.Background(), "tenant-alpha", "wrong/id", "req-alpha"); err == nil {
		t.Fatal("invalid path identifier accepted")
	}
	if transportCalls != 1 {
		t.Fatalf("invalid path identifier reached transport: calls = %d", transportCalls)
	}
	authorityBody := []byte(strings.Replace(string(readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project.json")), `"uid": "project-alpha"`, `"uid": "project-other"`, 1))
	authorityClient, _ := NewClient(TransportFunc(func(context.Context, Request) (Response, error) {
		return Response{Status: 200, Headers: map[string]string{HeaderResourceVersion: "3"}, Body: authorityBody}, nil
	}))
	if _, err := authorityClient.GetProject(context.Background(), "tenant-alpha", "project-alpha", "req-alpha"); err == nil || !strings.Contains(err.Error(), "PATH_BODY_AUTHORITY_MISMATCH") {
		t.Fatalf("resource UID authority mismatch = %#v", err)
	}
}

func TestGeneratedOpenAPIServerValidationSeam(t *testing.T) {
	organization, err := ValidateCreateOrganizationServerRequest("tenant-alpha", "req-alpha", []byte(`{"expectedTenantRevision":4,"organizationId":"organization-beta","name":"organization-beta","displayName":"Organization Beta","auditFactUid":"audit-organization-beta","reasonCode":"operator-request"}`))
	if err != nil || organization.Body.OrganizationID != "organization-beta" {
		t.Fatalf("organization server input = %#v / %v", organization, err)
	}
	page, err := ValidateListOrganizationsServerRequest("tenant-alpha", "req-alpha", 50, "organization-page-token-1")
	if err != nil || page.PageSize != 50 || page.PageToken == "" {
		t.Fatalf("organization list input = %#v / %v", page, err)
	}
	roles, err := ValidateListRolesServerRequest("tenant-alpha", "req-alpha", 50, "role-page-token-1")
	if err != nil || roles.PageSize != 50 || roles.PageToken == "" {
		t.Fatalf("role list input = %#v / %v", roles, err)
	}
	memberships, err := ValidateListMembershipsServerRequest("tenant-alpha", "req-alpha", 50, "membership-page-token-1")
	if err != nil || memberships.PageSize != 50 || memberships.PageToken == "" {
		t.Fatalf("membership list input = %#v / %v", memberships, err)
	}
	roleBindings, err := ValidateListRoleBindingsServerRequest("tenant-alpha", "req-alpha", 50, "role-binding-page-token-1")
	if err != nil || roleBindings.PageSize != 50 || roleBindings.PageToken == "" {
		t.Fatalf("role binding list input = %#v / %v", roleBindings, err)
	}
	projects, err := ValidateListProjectsServerRequest("tenant-alpha", "organization-alpha", "req-alpha", 50, "project-page-token-1")
	if err != nil || projects.OrganizationID != "organization-alpha" || projects.PageSize != 50 || projects.PageToken == "" {
		t.Fatalf("project list input = %#v / %v", projects, err)
	}
	sessions, err := ValidateListManagedAgentSessionsServerRequest("tenant-alpha", "project-alpha", "req-alpha", 50, "session-page-token-1")
	if err != nil || sessions.ProjectID != "project-alpha" || sessions.PageSize != 50 || sessions.PageToken == "" {
		t.Fatalf("session list input = %#v / %v", sessions, err)
	}
	turns, err := ValidateListManagedAgentTurnsServerRequest("tenant-alpha", "project-alpha", "session-alpha", "req-alpha", 50, "turn-page-token-1")
	if err != nil || turns.SessionID != "session-alpha" || turns.PageSize != 50 || turns.PageToken == "" {
		t.Fatalf("turn list input = %#v / %v", turns, err)
	}
	executions, err := ValidateListManagedAgentExecutionsServerRequest("tenant-alpha", "project-alpha", "session-alpha", "req-alpha", 50, "execution-page-token-1")
	if err != nil || executions.SessionID != "session-alpha" || executions.PageSize != 50 || executions.PageToken == "" {
		t.Fatalf("execution list input = %#v / %v", executions, err)
	}
	body := readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-create-request.json")
	input, err := ValidateCreateProjectServerRequest("tenant-alpha", "req-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", body)
	if err != nil || input.Body.Name != "project-alpha" {
		t.Fatalf("server input = %#v / %v", input, err)
	}
	if _, err := ValidateCreateProjectServerRequest("tenant-alpha", "req-alpha", "short", body); err == nil {
		t.Fatal("short idempotency key accepted")
	}
	unicode := []byte(`{"name":"project-alpha","organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-café"},"displayName":"Project Alpha"}`)
	if _, err := ValidateCreateProjectServerRequest("tenant-alpha", "req-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", unicode); err == nil {
		t.Fatal("Unicode organization identifier accepted by server seam")
	}
	if _, err := ValidateGetServerRequest("tenant-alpha", "project-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGetServerRequest("tenant-alpha", "project/alpha", "req-alpha"); err == nil {
		t.Fatal("path separator accepted")
	}
}

func responseFixture(t *testing.T, name string, status int) Response {
	t.Helper()
	return Response{
		Status:  status,
		Headers: map[string]string{HeaderResourceVersion: fixtureResourceVersion(t, name)},
		Body:    readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/"+name+".json"),
	}
}

func fixtureResourceVersion(t *testing.T, name string) string {
	t.Helper()
	var value struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/"+name+".json"), &value); err != nil {
		t.Fatal(err)
	}
	return value.Metadata.ResourceVersion
}

func readOpenAPIFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "contracts", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
