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
	summary := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"EnvironmentProfileSummary","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"profileId":"development","name":"development","version":1,"description":"Daily coding workspace","status":"published","availability":"available","providerKinds":["codex","claudeAgent"],"cpuLimitMillis":2000,"memoryLimitBytes":4294967296,"storageSummary":"20 GiB managed workspace","networkSummary":"Public internet access"}`)
	decoded, err := DecodeEnvironmentProfileSummaryJSON(summary)
	if err != nil || decoded.Status != "published" || decoded.Availability != "available" {
		t.Fatalf("summary=%#v error=%v", decoded, err)
	}
	withTarget := append(append([]byte(nil), summary[:len(summary)-1]...), []byte(`,"targetRefs":["docker-primary"]}`)...)
	if _, err := DecodeEnvironmentProfileSummaryJSON(withTarget); err == nil {
		t.Fatal("User API summary accepted target references")
	}
}

func TestRemoteWorkerHeartbeatResponseKeepsSandboxCommand(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	body := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RemoteWorkerHeartbeat","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"enrollmentId":"enrollment-alpha","workerId":"worker-alpha","incarnationId":"incarnation-alpha","generation":1,"observedGeneration":1,"desiredState":"active","observedState":"active","healthState":"online","acceptedAt":"2026-09-06T12:00:00Z","expiresAt":"2026-09-06T12:00:30Z","nextHeartbeatAfterSeconds":5,"reconcileRequired":false,"sandboxCommand":{"commandId":"rwsc-alpha","attempt":1,"action":"sandbox.create","operationId":"operation-alpha","workspaceId":"workspace-alpha","workspaceName":"workspace-alpha","targetId":"target-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":1,"imageUri":"registry.example.test/runtime@` + digest + `","cpuMillis":500,"memoryBytes":536870912,"specDigest":"` + digest + `","networkPolicyId":"network-alpha","networkAllowedEgress":["example.test"],"deadline":"2026-09-06T12:01:00Z"}}`)
	heartbeat, err := DecodeRemoteWorkerHeartbeatResponseJSON(body)
	if err != nil || heartbeat.Value.SandboxCommand == nil || heartbeat.Value.SandboxCommand.OperationID != "operation-alpha" || len(heartbeat.Unknown) != 0 {
		t.Fatalf("sandbox command = %#v, unknown = %#v, error = %v", heartbeat.Value.SandboxCommand, heartbeat.Unknown, err)
	}
}

func TestRemoteWorkerSandboxExecCommandBoundaries(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	command := []byte(`{"commandId":"rwexec-alpha","workspaceId":"workspace-alpha","targetId":"target-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"runtimeId":"runtime-alpha","runtimeOperationId":"operation-alpha","runtimeSpecDigest":"` + digest + `","command":"printf bounded","timeoutSeconds":10,"deadline":"2026-09-07T12:01:00Z"}`)
	if decoded, err := DecodeRemoteWorkerSandboxExecCommandJSON(command); err != nil || decoded.RuntimeOperationID != "operation-alpha" {
		t.Fatalf("exec command=%#v error=%v", decoded, err)
	}
	receipt := []byte(`{"commandId":"rwexec-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"result":"succeeded","exitCode":7,"stdout":"proof\\n","stderr":"failed","executionTimeMillis":1}`)
	if decoded, err := DecodeRemoteWorkerSandboxExecCommandReceiptJSON(receipt); err != nil || decoded.ExitCode != 7 {
		t.Fatalf("exec receipt=%#v error=%v", decoded, err)
	}
	oversized, _ := json.Marshal(RemoteWorkerSandboxExecCommandReceipt{CommandID: "rwexec-alpha", SandboxID: "sandbox-alpha",
		SandboxGeneration: 3, Result: "succeeded", Stdout: strings.Repeat("x", 1<<20), Stderr: "x"})
	if _, err := DecodeRemoteWorkerSandboxExecCommandReceiptJSON(oversized); err == nil {
		t.Fatal("combined RemoteWorker Sandbox Exec output above 1 MiB accepted")
	}
	if _, err := DecodeRemoteWorkerSandboxExecCommandReceiptJSON([]byte(`{"commandId":"rwexec-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"result":"failed","exitCode":0,"stdout":"","stderr":"","executionTimeMillis":0}`)); err == nil {
		t.Fatal("failed RemoteWorker Sandbox Exec receipt accepted without a stable error")
	}
}

func TestRemoteWorkerSandboxFileCommandBoundaries(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	command := []byte(`{"commandId":"rwfile-alpha","eventId":"event-alpha","grantId":"grant-alpha","workspaceId":"workspace-alpha","targetId":"target-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"runtimeId":"runtime-alpha","runtimeOperationId":"operation-alpha","runtimeSpecDigest":"` + digest + `","action":"write","path":"notes.txt","write":{"contentBase64Url":"aGk"},"deadline":"2026-09-07T12:01:00Z"}`)
	if decoded, err := DecodeRemoteWorkerSandboxFileCommandJSON(command); err != nil || decoded.Write == nil || decoded.Write.ContentBase64URL != "aGk" {
		t.Fatalf("file command=%#v error=%v", decoded, err)
	}
	readWithoutVersion := []byte(`{"commandId":"rwfile-alpha","eventId":"event-alpha","grantId":"grant-alpha","workspaceId":"workspace-alpha","targetId":"target-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"runtimeId":"runtime-alpha","runtimeOperationId":"operation-alpha","runtimeSpecDigest":"` + digest + `","action":"read","path":"notes.txt","read":{"offset":1,"limit":1048576},"deadline":"2026-09-07T12:01:00Z"}`)
	if _, err := DecodeRemoteWorkerSandboxFileCommandJSON(readWithoutVersion); err == nil {
		t.Fatal("continued file read accepted without a file version")
	}
	receipt := []byte(`{"commandId":"rwfile-alpha","eventId":"event-alpha","grantId":"grant-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"action":"read","result":"succeeded","bytesTransferred":2,"read":{"fileVersion":"sfv1_` + strings.Repeat("a", 43) + `","offset":0,"totalBytes":2,"contentBase64Url":"aGk"}}`)
	if decoded, err := DecodeRemoteWorkerSandboxFileCommandReceiptJSON(receipt); err != nil || decoded.Read == nil || decoded.BytesTransferred != 2 {
		t.Fatalf("file receipt=%#v error=%v", decoded, err)
	}
	if _, err := DecodeRemoteWorkerSandboxFileCommandReceiptJSON([]byte(`{"commandId":"rwfile-alpha","eventId":"event-alpha","grantId":"grant-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"action":"read","result":"failed","bytesTransferred":0,"stableErrorCode":"secret_error"}`)); err == nil {
		t.Fatal("file receipt accepted an unbounded stable error")
	}
}

func TestRemoteWorkerSandboxPTYCommandBoundaries(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	command := []byte(`{"commandId":"rwpty-alpha","grantId":"grant-alpha","workspaceId":"workspace-alpha","targetId":"target-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"runtimeId":"runtime-alpha","runtimeOperationId":"operation-alpha","runtimeSpecDigest":"` + digest + `","action":"exchange","sessionId":"session-alpha","since":0,"takeover":false,"pty":false,"input":{"messageType":"binary","payloadBase64Url":"AGhp"},"deadline":"2026-09-07T12:01:00Z"}`)
	if decoded, err := DecodeRemoteWorkerSandboxPTYCommandJSON(command); err != nil || decoded.Since == nil || *decoded.Since != 0 || decoded.PTY == nil || *decoded.PTY || decoded.Input == nil {
		t.Fatalf("PTY command=%#v error=%v", decoded, err)
	}
	receipt := []byte(`{"commandId":"rwpty-alpha","grantId":"grant-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"action":"exchange","result":"succeeded","bytesTransferred":3,"sessionId":"session-alpha","running":true,"outputOffset":2,"frames":[{"messageType":"binary","payloadBase64Url":"AWhp"}]}`)
	if decoded, err := DecodeRemoteWorkerSandboxPTYCommandReceiptJSON(receipt); err != nil || decoded.Frames == nil || decoded.BytesTransferred != 3 {
		t.Fatalf("PTY receipt=%#v error=%v", decoded, err)
	}
	if _, err := DecodeRemoteWorkerSandboxPTYCommandReceiptJSON([]byte(strings.Replace(string(receipt), `"bytesTransferred":3`, `"bytesTransferred":2`, 1))); err == nil {
		t.Fatal("PTY receipt accepted a mismatched decoded byte count")
	}
}

func TestRemoteWorkerSandboxPreviewCommandBoundaries(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	command := []byte(`{"commandId":"rwpreview-alpha","grantId":"grant-alpha","workspaceId":"workspace-alpha","targetId":"target-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"runtimeId":"runtime-alpha","runtimeOperationId":"operation-alpha","runtimeSpecDigest":"` + digest + `","port":3000,"method":"POST","path":"/hello","rawQuery":"value=alpha","headers":[{"name":"content-type","value":"text/plain"},{"name":"x-public","value":"visible"}],"bodyBase64Url":"aGk","deadline":"2026-09-07T12:01:00Z"}`)
	if decoded, err := DecodeRemoteWorkerSandboxPreviewCommandJSON(command); err != nil || decoded.Port != 3000 || len(decoded.Headers) != 2 {
		t.Fatalf("Preview command=%#v error=%v", decoded, err)
	}
	if _, err := DecodeRemoteWorkerSandboxPreviewCommandJSON([]byte(strings.Replace(string(command), `"content-type"`, `"authorization"`, 1))); err == nil {
		t.Fatal("Preview command accepted a credential header")
	}
	receipt := []byte(`{"commandId":"rwpreview-alpha","grantId":"grant-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":3,"port":3000,"result":"succeeded","bytesTransferred":2,"statusCode":201,"headers":[],"bodyBase64Url":"aGk"}`)
	if decoded, err := DecodeRemoteWorkerSandboxPreviewCommandReceiptJSON(receipt); err != nil || decoded.StatusCode == nil || *decoded.StatusCode != 201 {
		t.Fatalf("Preview receipt=%#v error=%v", decoded, err)
	}
	if _, err := DecodeRemoteWorkerSandboxPreviewCommandReceiptJSON([]byte(strings.Replace(string(receipt), `"bytesTransferred":2`, `"bytesTransferred":1`, 1))); err == nil {
		t.Fatal("Preview receipt accepted a mismatched decoded byte count")
	}
}

func TestRemoteWorkerSandboxLifecycleCommandsFencePhysicalAuthority(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	legacyCreateReceipt := []byte(`{"commandId":"rwsc-create","attempt":1,"operationId":"operation-create","sandboxId":"sandbox-alpha","sandboxGeneration":1,"result":"succeeded","runtimeId":"runtime-alpha","runtimeState":"Running","volumeName":"volume-alpha","cleanupComplete":false}`)
	if decoded, err := DecodeRemoteWorkerSandboxCommandReceiptJSON(legacyCreateReceipt); err != nil || decoded.Action != "sandbox.create" {
		t.Fatalf("legacy create receipt=%#v error=%v", decoded, err)
	}
	command := []byte(`{"commandId":"rwsc-stop","attempt":1,"action":"sandbox.stop","operationId":"operation-stop","workspaceId":"workspace-alpha","workspaceName":"workspace-alpha","targetId":"target-alpha","sandboxId":"sandbox-alpha","sandboxGeneration":2,"imageUri":"registry.example.test/runtime@` + digest + `","cpuMillis":500,"memoryBytes":536870912,"specDigest":"` + digest + `","networkPolicyId":"network-alpha","networkAllowedEgress":[],"physicalVolumeName":"volume-alpha","runtimeId":"runtime-alpha","runtimeState":"Running","runtimeOperationId":"operation-create","runtimeGeneration":1,"runtimeSpecDigest":"` + digest + `","deadline":"2026-09-06T12:01:00Z"}`)
	if decoded, err := DecodeRemoteWorkerSandboxCommandJSON(command); err != nil || decoded.RuntimeID != "runtime-alpha" {
		t.Fatalf("stop command=%#v error=%v", decoded, err)
	}
	withoutRuntime := []byte(strings.Replace(string(command), `,"runtimeId":"runtime-alpha"`, "", 1))
	if _, err := DecodeRemoteWorkerSandboxCommandJSON(withoutRuntime); err == nil {
		t.Fatal("stop command accepted without the exact prior runtime")
	}
	rebuildCommand := []byte(strings.NewReplacer(
		`"sandbox.stop"`, `"sandbox.rebuild"`,
		`,"runtimeId":"runtime-alpha"`, "",
		`,"runtimeState":"Running"`, "",
		`,"runtimeOperationId":"operation-create"`, "",
		`,"runtimeGeneration":1`, "",
		`,"runtimeSpecDigest":"`+digest+`"`, "",
	).Replace(string(command)))
	if decoded, err := DecodeRemoteWorkerSandboxCommandJSON(rebuildCommand); err != nil || decoded.PhysicalVolumeName != "volume-alpha" {
		t.Fatalf("rebuild command=%#v error=%v", decoded, err)
	}
	withoutVolume := []byte(strings.Replace(string(rebuildCommand), `,"physicalVolumeName":"volume-alpha"`, "", 1))
	if _, err := DecodeRemoteWorkerSandboxCommandJSON(withoutVolume); err == nil {
		t.Fatal("rebuild command accepted without the retained volume")
	}
	receipt := []byte(`{"commandId":"rwsc-stop","attempt":1,"action":"sandbox.stop","operationId":"operation-stop","sandboxId":"sandbox-alpha","sandboxGeneration":2,"result":"succeeded","volumeName":"volume-alpha","cleanupComplete":true}`)
	if _, err := DecodeRemoteWorkerSandboxCommandReceiptJSON(receipt); err != nil {
		t.Fatalf("stop receipt error=%v", err)
	}
	if _, err := DecodeRemoteWorkerSandboxCommandReceiptJSON([]byte(strings.Replace(string(receipt), "true", "false", 1))); err == nil {
		t.Fatal("successful stop receipt accepted incomplete cleanup")
	}
	rebuildReceipt := []byte(`{"commandId":"rwsc-rebuild","attempt":1,"action":"sandbox.rebuild","operationId":"operation-rebuild","sandboxId":"sandbox-alpha","sandboxGeneration":3,"result":"succeeded","runtimeId":"runtime-rebuilt","runtimeState":"Running","volumeName":"volume-alpha","cleanupComplete":false}`)
	if _, err := DecodeRemoteWorkerSandboxCommandReceiptJSON(rebuildReceipt); err != nil {
		t.Fatalf("rebuild receipt error=%v", err)
	}
}

func TestGeneratedRuntimeProfileKeepsAdminAndUserBoundaries(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	profile := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RuntimeProfile","metadata":{"uid":"rp-0123456789abcdef0123456789abcdef","name":"foundation","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-09-05T03:00:00Z","updatedAt":"2026-09-05T03:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"profileId":"foundation","version":1,"description":"Retained no-agent workspace","status":"draft","targetId":"docker-primary","imageUri":"registry.example.test/runtime@` + digest + `","releaseDigest":"` + digest + `","cpuMillis":500,"memoryBytes":536870912}}`)
	decoded, err := DecodeRuntimeProfileResponseJSON(profile)
	if err != nil || decoded.Value.Spec.TargetID != "docker-primary" {
		t.Fatalf("profile=%#v error=%v", decoded.Value, err)
	}
	summary := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"RuntimeProfileSummary","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"profileId":"foundation","name":"foundation","version":1,"description":"Retained no-agent workspace","status":"published","availability":"available","cpuMillis":500,"memoryBytes":536870912,"workspaceRetention":"retained"}`)
	if value, err := DecodeRuntimeProfileSummaryJSON(summary); err != nil || value.WorkspaceRetention != "retained" {
		t.Fatalf("summary=%#v error=%v", value, err)
	}
	withTarget := append(append([]byte(nil), summary[:len(summary)-1]...), []byte(`,"targetId":"docker-primary"}`)...)
	if _, err := DecodeRuntimeProfileSummaryJSON(withTarget); err == nil {
		t.Fatal("public RuntimeProfile summary accepted Admin Target authority")
	}
	request := []byte(`{"workspaceId":"workspace","workspaceName":"workspace","sandboxId":"sandbox","runtimeProfileId":"foundation","runtimeProfileVersion":1,"ttlSeconds":60}`)
	if value, err := DecodeSandboxSessionCreateRequestJSON(request); err != nil || value.RuntimeProfileID != "foundation" {
		t.Fatalf("sandbox request=%#v error=%v", value, err)
	}
	withEndpoint := append(append([]byte(nil), request[:len(request)-1]...), []byte(`,"endpoint":"tcp://host"}`)...)
	if _, err := DecodeSandboxSessionCreateRequestJSON(withEndpoint); err == nil {
		t.Fatal("public Sandbox request accepted an infrastructure endpoint")
	}
}

func TestGeneratedStoragePolicyContractKeepsSupportedLifecycle(t *testing.T) {
	policy := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"StoragePolicy","metadata":{"uid":"storage-standard","name":"storage-standard","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-09-05T03:00:00Z","updatedAt":"2026-09-05T03:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"userSummary":"20 GiB managed workspace","workspaceType":"managed-volume","workspaceCapacityBytes":21474836480,"retentionSeconds":0,"cleanupOnLeaseTermination":true,"allowWorkspaceReuse":true}}`)
	decoded, err := DecodeStoragePolicyResponseJSON(policy)
	if err != nil || decoded.Value.Spec.WorkspaceCapacityBytes != 21474836480 {
		t.Fatalf("policy=%#v error=%v", decoded, err)
	}
	unsupported := bytes.Replace(policy, []byte(`"retentionSeconds":0`), []byte(`"retentionSeconds":3600`), 1)
	if _, err := DecodeStoragePolicyResponseJSON(unsupported); err == nil {
		t.Fatal("unsupported delayed retention accepted")
	}
}

func TestGeneratedNetworkPolicyContractKeepsOpaqueReferences(t *testing.T) {
	if _, err := EncodeNetworkPolicySetRequestJSON(NetworkPolicySetRequest{
		ExpectedResourceVersion: "0", PolicyName: "network-restricted", UserSummary: "Approved outbound access",
		DefaultEgress: "restricted", AllowedEgress: []string{"api.openai.com"}, PreviewEnabled: true,
	}); err != nil {
		t.Fatalf("direct allowlist request: %v", err)
	}
	policy := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"NetworkPolicy","metadata":{"uid":"network-restricted","name":"network-restricted","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-09-05T04:00:00Z","updatedAt":"2026-09-05T04:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"userSummary":"Approved destinations only","defaultEgress":"restricted","allowedEgress":[],"allowlistPolicyRef":"allowlist-standard","ingressEnabled":false,"previewEnabled":false,"dnsPolicyRef":"dns-standard","proxyPolicyRef":"proxy-standard"}}`)
	decoded, err := DecodeNetworkPolicyResponseJSON(policy)
	if err != nil || decoded.Value.Spec.DefaultEgress != "restricted" || decoded.Value.Spec.AllowlistPolicyRef != "allowlist-standard" {
		t.Fatalf("policy=%#v error=%v", decoded, err)
	}
	withEndpoint := bytes.Replace(policy, []byte(`"proxyPolicyRef":"proxy-standard"`), []byte(`"proxyPolicyRef":"proxy-standard","endpoint":"tcp://host"`), 1)
	if _, err := DecodeNetworkPolicyJSON(withEndpoint); err == nil {
		t.Fatal("network policy accepted an endpoint")
	}
}

func TestGeneratedProjectLeaseQuotaSummaryRejectsAdminFields(t *testing.T) {
	summary := []byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"ProjectLeaseQuotaSummary","projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"maxConcurrentLeases":2,"activeLeases":1,"maxCpuMillis":4000,"usedCpuMillis":2000,"maxMemoryBytes":8589934592,"usedMemoryBytes":4294967296,"maxLeaseTtlSeconds":3600}`)
	decoded, err := DecodeProjectLeaseQuotaSummaryJSON(summary)
	if err != nil || decoded.ActiveLeases != 1 || decoded.MaxLeaseTTLSeconds != 3600 {
		t.Fatalf("summary=%#v error=%v", decoded, err)
	}
	withCredential := append(append([]byte(nil), summary[:len(summary)-1]...), []byte(`,"credentialRef":"secret"}`)...)
	if _, err := DecodeProjectLeaseQuotaSummaryJSON(withCredential); err == nil {
		t.Fatal("User quota summary accepted an Admin-only credential reference")
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
