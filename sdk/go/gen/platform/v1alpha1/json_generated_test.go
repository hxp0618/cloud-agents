package v1alpha1

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

func TestGeneratedPlatformJSONFixtures(t *testing.T) {
	checks := []struct {
		name   string
		decode func([]byte) error
	}{
		{"platform-tenant", func(data []byte) error { _, err := DecodePlatformTenantJSON(data); return err }},
		{"organization", func(data []byte) error { _, err := DecodeOrganizationJSON(data); return err }},
		{"organization-page", func(data []byte) error { _, err := DecodeOrganizationPageJSON(data); return err }},
		{"project", func(data []byte) error { _, err := DecodeProjectJSON(data); return err }},
		{"project-page", func(data []byte) error { _, err := DecodeProjectPageJSON(data); return err }},
		{"project-create-request", func(data []byte) error { _, err := DecodeProjectCreateRequestJSON(data); return err }},
		{"membership", func(data []byte) error { _, err := DecodeMembershipJSON(data); return err }},
		{"role", func(data []byte) error { _, err := DecodeRoleJSON(data); return err }},
		{"role-binding", func(data []byte) error { _, err := DecodeRoleBindingJSON(data); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.decode(readPlatformFixture(t, "golden/"+check.name+".json")); err != nil {
				t.Fatalf("golden fixture rejected: %v", err)
			}
		})
	}

	negative := []string{
		"project-create-server-owned-field",
		"organization-tenant-ref-mismatch",
		"role-binding-scope-mismatch",
		"role-binding-unknown-role",
		"role-wildcard-permission",
	}
	for _, name := range negative {
		t.Run(name, func(t *testing.T) {
			var err error
			data := readPlatformFixture(t, "negative/"+name+".json")
			switch name {
			case "project-create-server-owned-field":
				_, err = DecodeProjectCreateRequestJSON(data)
			case "organization-tenant-ref-mismatch":
				_, err = DecodeOrganizationJSON(data)
			case "role-binding-scope-mismatch", "role-binding-unknown-role":
				_, err = DecodeRoleBindingJSON(data)
			case "role-wildcard-permission":
				_, err = DecodeRoleJSON(data)
			}
			if err == nil {
				t.Fatal("negative fixture accepted")
			}
		})
	}
}

func TestGeneratedPlatformJSONRequestAndResponseBoundaries(t *testing.T) {
	membershipBody := bytes.TrimSpace(readPlatformFixture(t, "golden/membership.json"))
	membershipPageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"MembershipPage","memberships":[` + string(membershipBody) + `],"nextPageToken":"membership-page-token-1"}`)
	membershipPage, err := DecodeMembershipPageResponseJSON(membershipPageBody)
	if err != nil || len(membershipPage.Value.Memberships) != 1 || membershipPage.Value.NextPageToken != "membership-page-token-1" {
		t.Fatalf("membership page = %#v / %v", membershipPage, err)
	}

	roleBody := bytes.TrimSpace(readPlatformFixture(t, "golden/role.json"))
	rolePageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RolePage","roles":[` + string(roleBody) + `],"nextPageToken":"role-page-token-1"}`)
	rolePage, err := DecodeRolePageResponseJSON(rolePageBody)
	if err != nil || len(rolePage.Value.Roles) != 1 || rolePage.Value.NextPageToken != "role-page-token-1" {
		t.Fatalf("role page = %#v / %v", rolePage, err)
	}

	roleBindingBody := bytes.TrimSpace(readPlatformFixture(t, "golden/role-binding.json"))
	roleBindingPageBody := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RoleBindingPage","roleBindings":[` + string(roleBindingBody) + `],"nextPageToken":"role-binding-page-token-1"}`)
	roleBindingPage, err := DecodeRoleBindingPageResponseJSON(roleBindingPageBody)
	if err != nil || len(roleBindingPage.Value.RoleBindings) != 1 || roleBindingPage.Value.NextPageToken != "role-binding-page-token-1" {
		t.Fatalf("role binding page = %#v / %v", roleBindingPage, err)
	}

	organization, err := DecodeOrganizationCreateRequestJSON([]byte(`{"expectedTenantRevision":4,"organizationId":"organization-beta","name":"organization-beta","displayName":"Organization Beta","auditFactUid":"audit-organization-beta","reasonCode":"operator-request"}`))
	if err != nil || organization.OrganizationID != "organization-beta" || organization.ExpectedTenantRevision != 4 {
		t.Fatalf("organization create request = %#v / %v", organization, err)
	}
	if _, err := EncodeOrganizationCreateRequestJSON(organization); err != nil {
		t.Fatal(err)
	}

	request := readPlatformFixture(t, "golden/project-create-request.json")
	decoded, err := DecodeProjectCreateRequestJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeProjectCreateRequestJSON(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(request, &want); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(got, want) {
		t.Fatalf("encoded request = %s, want %s", encoded, request)
	}
	if _, err := DecodeProjectCreateRequestJSON(append(request, []byte(`{"future":true}`)...)); err == nil {
		t.Fatal("request unknown field accepted")
	}
	unicode := []byte(`{"name":"project-alpha","organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-café"},"displayName":"Project Alpha"}`)
	if _, err := DecodeProjectCreateRequestJSON(unicode); err == nil {
		t.Fatal("Unicode organization identifier accepted")
	}
	decoded.OrganizationRef.ID = "organization-café"
	if _, err := EncodeProjectCreateRequestJSON(decoded); err == nil {
		t.Fatal("Unicode organization identifier encoded")
	}

	response := bytes.TrimSpace(readPlatformFixture(t, "negative/project-response-n-minus-one.json"))
	envelope, err := DecodeProjectResponseJSON(response)
	if err != nil || string(envelope.Unknown["/futureField"]) != "{\n    \"version\": 2\n  }" || string(envelope.Unknown["/spec/future~1field~0v2"]) != `9007199254740993` {
		t.Fatalf("response sidecar = %#v / %v", envelope.Unknown, err)
	}
	reencoded, err := EncodeProjectResponseJSON(envelope)
	if err != nil || !bytes.Contains(reencoded, []byte(`"future/field~v2":9007199254740993`)) || !bytes.Contains(reencoded, []byte(`"futureField":{"version":2}`)) {
		t.Fatalf("response re-encode = %s / %v", reencoded, err)
	}
	if _, err := DecodeProjectJSON(response); err == nil {
		t.Fatal("response unknown field accepted by strict mutation decoder")
	}
	collision := common.ResponseEnvelope[Project]{Value: envelope.Value, Unknown: common.JSONSidecar{"/spec/state": json.RawMessage(`"future"`)}}
	if _, err := EncodeProjectResponseJSON(collision); err == nil {
		t.Fatal("sidecar known-field collision accepted")
	}
	invalidPointer := common.ResponseEnvelope[Project]{Value: envelope.Value, Unknown: common.JSONSidecar{"spec/future": json.RawMessage(`true`)}}
	if _, err := EncodeProjectResponseJSON(invalidPointer); err == nil {
		t.Fatal("invalid sidecar pointer accepted")
	}

	cross := readPlatformFixture(t, "negative/cross-tenant-project.json")
	var wrapper struct {
		Instance json.RawMessage `json:"instance"`
	}
	if err := json.Unmarshal(cross, &wrapper); err != nil {
		t.Fatal(err)
	}
	project, err := DecodeProjectJSON(wrapper.Instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateProjectResolvedOrganization(project, common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: "tenant-beta"}); err == nil {
		t.Fatal("cross-tenant resolved reference accepted")
	}
}

func TestGeneratedEnvironmentProfileSummaryRejectsInfrastructureFields(t *testing.T) {
	summary := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"EnvironmentProfileSummary","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"profileId":"development","name":"development","version":1,"description":"Daily coding workspace","status":"published","availability":"available","providerKinds":["codex","claudeAgent"],"cpuLimitMillis":2000,"memoryLimitBytes":4294967296}`)
	decoded, err := DecodeEnvironmentProfileSummaryJSON(summary)
	if err != nil || decoded.Status != "published" || decoded.Availability != "available" {
		t.Fatalf("summary=%#v error=%v", decoded, err)
	}
	withTarget := append(append([]byte(nil), summary[:len(summary)-1]...), []byte(`,"targetRefs":["docker-primary"]}`)...)
	if _, err := DecodeEnvironmentProfileSummaryJSON(withTarget); err == nil {
		t.Fatal("User API summary accepted target references")
	}
}

func TestGeneratedWorkerContractKeepsOperationalBoundary(t *testing.T) {
	worker := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Worker","metadata":{"uid":"lease-alpha","name":"worker-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"4","createdAt":"2026-09-04T08:00:00Z","updatedAt":"2026-09-04T08:01:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"leaseId":"lease-alpha","targetId":"docker-alpha","targetKind":"docker","targetGeneration":2,"generation":3,"releaseDigest":"sha256:` + strings.Repeat("a", 64) + `","state":"ready","cleanupPhase":"none","cpuLimitMillis":1000,"memoryLimitBytes":536870912,"workerSpiffeId":"spiffe://cloud-agents.test/worker/lease-alpha","workerServerName":"worker-alpha.test","lastHealthAt":"2026-09-04T08:01:00Z","readyAt":"2026-09-04T08:01:00Z","stableErrorCode":""}}`)
	page, err := DecodeWorkerPageResponseJSON([]byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"WorkerPage","workers":[` + string(worker) + `],"nextPageToken":"worker-page-token-1"}`))
	if err != nil || len(page.Value.Workers) != 1 || page.Value.Workers[0].Spec.State != "ready" {
		t.Fatalf("worker page=%#v error=%v", page, err)
	}
	withEndpoint := bytes.Replace(worker, []byte(`"stableErrorCode":""`), []byte(`"workerEndpoint":"https://worker.test","stableErrorCode":""`), 1)
	if _, err := DecodeWorkerJSON(withEndpoint); err == nil {
		t.Fatal("Worker accepted an infrastructure endpoint")
	}
	missingHealth := bytes.Replace(worker, []byte(`,"lastHealthAt":"2026-09-04T08:01:00Z"`), nil, 1)
	if _, err := DecodeWorkerJSON(missingHealth); err == nil {
		t.Fatal("ready Worker accepted a partial health observation")
	}
}

func TestGeneratedDeploymentTargetCleanupPreviewContract(t *testing.T) {
	body := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"DeploymentTargetCleanupPreview","metadata":{"uid":"docker-alpha","name":"docker-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"7","createdAt":"2026-09-03T08:00:00Z","updatedAt":"2026-09-03T08:01:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"targetKind":"docker","expectedGeneration":2,"expectedResourceVersion":"7","impactDigest":"sha256:` + strings.Repeat("a", 64) + `","canCleanup":false,"workers":[{"workerName":"cloud-agents-worker-alpha","leaseId":"lease-alpha","leaseGeneration":3,"disposition":"blocked","resources":[{"resourceKind":"container","resourceName":"cloud-agents-worker-alpha"},{"resourceKind":"workspace-volume","resourceName":"workspace-alpha"}]}]}}`)
	preview, err := DecodeDeploymentTargetCleanupPreviewResponseJSON(body)
	if err != nil || preview.Value.Spec.CanCleanup || preview.Value.Spec.Workers[0].Resources[1].ResourceName != "workspace-alpha" {
		t.Fatalf("preview=%#v error=%v", preview, err)
	}
	invalid := bytes.Replace(body, []byte(`"canCleanup":false`), []byte(`"canCleanup":true`), 1)
	if _, err := DecodeDeploymentTargetCleanupPreviewResponseJSON(invalid); err == nil {
		t.Fatal("preview allowed cleanup while an active Lease blocker was present")
	}
	request := DeploymentTargetCleanupRequest{ExpectedGeneration: 2, ExpectedResourceVersion: "7", ImpactDigest: "sha256:" + strings.Repeat("a", 64)}
	encoded, err := EncodeDeploymentTargetCleanupRequestJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeDeploymentTargetCleanupRequestJSON(encoded); err != nil || decoded != request {
		t.Fatalf("cleanup request = %#v / %v", decoded, err)
	}
}

func TestGeneratedPlatformJSONCanonicalFraming(t *testing.T) {
	data := readPlatformFixture(t, "golden/project.json")
	for _, suffix := range [][]byte{[]byte("[]")} {
		if _, err := DecodeProjectJSON(append(append([]byte(nil), data...), suffix...)); err == nil {
			t.Fatalf("trailing bytes %q accepted", suffix)
		}
	}
	if _, err := DecodeProjectJSON([]byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Project","metadata":{}}`)); err == nil {
		t.Fatal("missing project fields accepted")
	}
}

func readPlatformFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "contracts", "platform", "v1alpha1", "fixtures", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func jsonEqual(left, right any) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return string(leftBytes) == string(rightBytes)
}
