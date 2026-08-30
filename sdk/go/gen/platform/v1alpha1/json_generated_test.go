package v1alpha1

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
