package controlplane_test

import (
	"encoding/json"
	"errors"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
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
	path := filepath.Join("..", "..", "..", "contracts", "platform", "v1alpha1", "fixtures", "golden", "managed-agent-create-project-idempotency.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	intent, err := coordination.BindManagedAgentCreateProject(coordination.ManagedAgentCreateProject(), fixture.Request.Path.TenantID, coordination.ManagedAgentCreateProjectRequest{
		Name: fixture.Request.Body.Name,
		OrganizationRef: coordination.OrganizationRef{
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
	valid := coordination.ManagedAgentCreateProjectRequest{
		Name:            "project-alpha",
		OrganizationRef: coordination.OrganizationRef{Namespace: "cloud-agents", Kind: "organization", ID: "organization-alpha"},
		DisplayName:     "Project Alpha",
	}
	tests := []struct {
		name    string
		profile coordination.Profile
		tenant  string
		request coordination.ManagedAgentCreateProjectRequest
	}{
		{name: "zero profile", tenant: "tenant-alpha", request: valid},
		{name: "invalid tenant", profile: coordination.ManagedAgentCreateProject(), tenant: "tenant/alpha", request: valid},
		{name: "invalid name", profile: coordination.ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() coordination.ManagedAgentCreateProjectRequest {
			value := valid
			value.Name = "project/alpha"
			return value
		}()},
		{name: "wrong namespace", profile: coordination.ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() coordination.ManagedAgentCreateProjectRequest {
			value := valid
			value.OrganizationRef.Namespace = "foreign"
			return value
		}()},
		{name: "unicode organization scope", profile: coordination.ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() coordination.ManagedAgentCreateProjectRequest {
			value := valid
			value.OrganizationRef.ID = "organization-café"
			return value
		}()},
		{name: "organization scope too long", profile: coordination.ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() coordination.ManagedAgentCreateProjectRequest {
			value := valid
			value.OrganizationRef.ID = strings.Repeat("a", 129)
			return value
		}()},
		{name: "empty display name", profile: coordination.ManagedAgentCreateProject(), tenant: "tenant-alpha", request: func() coordination.ManagedAgentCreateProjectRequest {
			value := valid
			value.DisplayName = ""
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := coordination.BindManagedAgentCreateProject(test.profile, test.tenant, test.request); !errors.Is(err, coordination.ErrInvalidManagedAgentCreateProjectRequest) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
