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
	responses := map[string]Response{
		"GET /v1/tenants/tenant-alpha":                                                                                      responseFixture(t, "platform-tenant", 200),
		"GET /v1/tenants/tenant-alpha/organizations/organization-alpha":                                                     responseFixture(t, "organization", 200),
		"GET /v1/tenants/tenant-alpha/organizations?pageSize=1&pageToken=organization-page-token-1":                         {Status: 200, Body: readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/organization-page.json")},
		"GET /v1/tenants/tenant-alpha/projects/project-alpha":                                                               projectGetResponse,
		"GET /v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageSize=1&pageToken=project-page-token-1": {Status: 200, Body: readOpenAPIFixture(t, "platform/v1alpha1/fixtures/golden/project-page.json")},
		"GET /v1/tenants/tenant-alpha/memberships/membership-alpha":                                                         responseFixture(t, "membership", 200),
		"GET /v1/tenants/tenant-alpha/roles/role-project-viewer-v1":                                                         responseFixture(t, "role", 200),
		"GET /v1/tenants/tenant-alpha/role-bindings/role-binding-alpha":                                                     responseFixture(t, "role-binding", 200),
		"GET /v1/managed-host/tenants/tenant-alpha/projects/project-alpha":                                                  projectGetResponse,
		"GET /v1/managed-host/tenants/tenant-alpha/role-bindings/role-binding-alpha":                                        responseFixture(t, "role-binding", 200),
		"POST /v1/tenants/tenant-alpha/organizations":                                                                       {Status: 201, Headers: map[string]string{HeaderResourceVersion: "2"}, Body: responseFixture(t, "organization", 200).Body},
		"POST /v1/tenants/tenant-alpha/projects":                                                                            {Status: 201, Headers: map[string]string{HeaderResourceVersion: "3"}, Body: projectBody},
		"POST /v1/tenants/tenant-alpha/memberships":                                                                         {Status: 201, Headers: map[string]string{HeaderResourceVersion: "8"}, Body: mutationBody},
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
	if _, err := client.GetRole(ctx, "tenant-alpha", "role-project-viewer-v1", "req-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetRoleBinding(ctx, "tenant-alpha", "role-binding-alpha", "req-alpha"); err != nil {
		t.Fatal(err)
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
	if len(seen) != 13 {
		t.Fatalf("transport calls = %d, want 13", len(seen))
	}
	for _, request := range seen {
		if request.Headers[HeaderRequestID] != "req-alpha" {
			t.Fatalf("request headers = %#v", request.Headers)
		}
	}
	sentBody, sentErr := platform.DecodeProjectCreateRequestJSON(seen[11].Body)
	if seen[11].Headers[HeaderIdempotencyKey] == "" || sentErr != nil || sentBody != body {
		t.Fatalf("create request = %#v", seen[11])
	}
}

func rawTenantRef(id string) *json.RawMessage {
	raw := json.RawMessage(`{"namespace":"cloud-agents","kind":"tenant","id":"` + id + `"}`)
	return &raw
}

func TestGeneratedOpenAPIClientManagedAgentSessionLifecycle(t *testing.T) {
	sessionBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Session","metadata":{"uid":"session-alpha","projectId":"project-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"providerKind":"codex","state":"active"}}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		return Response{Status: map[string]int{"POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions": 201, "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha": 200, "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha:close": 200}[request.Method+" "+request.Path], Headers: map[string]string{HeaderResourceVersion: "2"}, Body: sessionBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := client.CreateManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", ManagedAgentSessionCreateRequest{SessionID: "session-alpha", ProviderKind: "codex"})
	if err != nil || created.Value.Spec.ProviderKind != "codex" {
		t.Fatalf("create = %#v / %v", created, err)
	}
	if _, err := client.GetManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CloseManagedAgentSession(ctx, "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R3"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || string(seen[0].Body) != `{"sessionId":"session-alpha","providerKind":"codex"}` || seen[2].Body != nil {
		t.Fatalf("session requests = %#v", seen)
	}
}

func TestGeneratedOpenAPIClientManagedAgentExecutionLifecycle(t *testing.T) {
	executionBody := []byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Execution","metadata":{"uid":"execution-alpha","projectId":"project-alpha","sessionId":"session-alpha","turnId":"turn-alpha","resourceVersion":"3","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"generation":1,"state":"succeeded","resultDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"messages":[{"requestId":"request-alpha","protocolVersion":{"major":2,"minor":3},"executionId":"execution-alpha","generation":1,"commandId":"command-alpha","occurredAt":"2026-08-29T08:01:00Z","messageType":"Result","payload":{"text":"done"}}]}`)
	var seen []Request
	client, err := NewClient(TransportFunc(func(_ context.Context, request Request) (Response, error) {
		seen = append(seen, request)
		return Response{Status: 200, Headers: map[string]string{HeaderResourceVersion: "3"}, Body: executionBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ExecuteManagedAgent(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "request-alpha", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2", ManagedAgentExecutionCreateRequest{TurnID: "turn-alpha", ExecutionID: "execution-alpha", Model: "codex", InputText: "hello"})
	if err != nil || len(result.Value.Messages) != 1 || result.Value.Messages[0].MessageType != "Result" {
		t.Fatalf("execute = %#v / %v", result, err)
	}
	if _, err := client.GetManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CancelManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-cancel", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX", ManagedAgentExecutionCancelRequest{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.InterruptManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-interrupt", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EY", ManagedAgentExecutionInterruptRequest{Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[0].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions" || seen[1].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha" || seen[2].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:cancel" || seen[3].Path != "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:interrupt" || string(seen[0].Body) != `{"turnId":"turn-alpha","executionId":"execution-alpha","model":"codex","inputText":"hello"}` || string(seen[2].Body) != `{"generation":1}` || string(seen[3].Body) != `{"generation":1}` {
		t.Fatalf("execution requests = %#v", seen)
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
	projects, err := ValidateListProjectsServerRequest("tenant-alpha", "organization-alpha", "req-alpha", 50, "project-page-token-1")
	if err != nil || projects.OrganizationID != "organization-alpha" || projects.PageSize != 50 || projects.PageToken == "" {
		t.Fatalf("project list input = %#v / %v", projects, err)
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
