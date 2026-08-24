package coordination

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedAgentCreateProjectIntentMatchesCanonicalContractFixture(t *testing.T) {
	var fixture struct {
		Request struct {
			Path struct {
				TenantID string `json:"tenantId"`
			} `json:"path"`
			Body struct {
				Name            string `json:"name"`
				OrganizationRef struct {
					Namespace string `json:"namespace"`
					Kind      string `json:"kind"`
					ID        string `json:"id"`
				} `json:"organizationRef"`
				DisplayName string `json:"displayName"`
			} `json:"body"`
		} `json:"request"`
		Digest string `json:"digest"`
	}
	path := filepath.Join("..", "..", "..", "..", "contracts", "platform", "v1alpha1", "fixtures", "golden", "managed-agent-create-project-idempotency.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	intent, err := BindManagedAgentCreateProject(ManagedAgentCreateProject(), fixture.Request.Path.TenantID, ManagedAgentCreateProjectRequest{
		Name: fixture.Request.Body.Name,
		OrganizationRef: OrganizationRef{
			Namespace: fixture.Request.Body.OrganizationRef.Namespace,
			Kind:      fixture.Request.Body.OrganizationRef.Kind,
			ID:        fixture.Request.Body.OrganizationRef.ID,
		},
		DisplayName: fixture.Request.Body.DisplayName,
	})
	if err != nil || intent.RequestDigest() != fixture.Digest || intent.OrganizationID() != fixture.Request.Body.OrganizationRef.ID {
		t.Fatalf("intent/error = %#v / %v", intent, err)
	}
}

func TestManagedAgentCreateProjectIntentRejectsProfileAndRequestDrift(t *testing.T) {
	valid := ManagedAgentCreateProjectRequest{
		Name:            "project-alpha",
		OrganizationRef: OrganizationRef{Namespace: "cloud-agents", Kind: "organization", ID: "organization-alpha"},
		DisplayName:     "Project Alpha",
	}
	tests := []struct {
		name    string
		profile Profile
		tenant  string
		request ManagedAgentCreateProjectRequest
	}{
		{name: "zero profile", tenant: "tenant-alpha", request: valid},
		{name: "invalid tenant", profile: ManagedAgentCreateProject(), tenant: "tenant/alpha", request: valid},
		{name: "invalid name", profile: ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() ManagedAgentCreateProjectRequest { value := valid; value.Name = "project/alpha"; return value }()},
		{name: "wrong namespace", profile: ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() ManagedAgentCreateProjectRequest {
			value := valid
			value.OrganizationRef.Namespace = "foreign"
			return value
		}()},
		{name: "unicode organization scope", profile: ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() ManagedAgentCreateProjectRequest {
			value := valid
			value.OrganizationRef.ID = "organization-café"
			return value
		}()},
		{name: "organization scope too long", profile: ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() ManagedAgentCreateProjectRequest {
			value := valid
			value.OrganizationRef.ID = strings.Repeat("a", 129)
			return value
		}()},
		{name: "empty display name", profile: ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() ManagedAgentCreateProjectRequest { value := valid; value.DisplayName = ""; return value }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BindManagedAgentCreateProject(test.profile, test.tenant, test.request); !errors.Is(err, ErrInvalidManagedAgentCreateProjectRequest) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
