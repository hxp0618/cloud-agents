package controlplane_test

import (
	"errors"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"testing"
)

func TestManagedAgentCreateProjectDurableProfileIsVersionedAndClosed(t *testing.T) {
	profile := coordination.ManagedAgentCreateProjectDurable()
	if !profile.Valid() || profile.ProfileID() != "managedAgentCreateProjectDurable/v1alpha1" ||
		profile.ProfileDigest() != "sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f" ||
		profile.OperationID() != "managedAgentCreateProjectDurable" ||
		profile.OutboxEventClass() != "operation_effect" ||
		profile.RequiredPermission() != "projects.create" || profile.RequiredScopeLevel() != "organization" ||
		profile.ResultResourceKind() != "project" || profile.ReplayTTLSeconds() != 86400 ||
		!profile.CreatesPlatformOperation() || profile.ExternalSideEffectAllowed() {
		t.Fatalf("durable generated profile drift = %#v", profile)
	}
	if coordination.ManagedAgentCreateProject().CreatesPlatformOperation() || coordination.ManagedAgentCreateProject().ExternalSideEffectAllowed() {
		t.Fatal("frozen v1 claim-only profile widened")
	}
	if coordination.RegistryDigest != "sha256:ca5703cbbc68f7501e6fb4da0a0f09bc9fdd6e52bc48f080627bec64fd1b635a" {
		t.Fatal("frozen v1 registry digest drifted")
	}
	if coordination.DurableProjectCreateRegistryDigest != "sha256:8c973a9460b659eb601fb5db547eb9fd85b25bf74d050c56a76f003710118ec8" {
		t.Fatal("durable v2 registry digest drifted")
	}
}

func TestManagedAgentCreateProjectDurableIntentMatchesCanonicalProjection(t *testing.T) {
	intent, err := coordination.BindManagedAgentCreateProjectDurable(
		coordination.ManagedAgentCreateProjectDurable(),
		"tenant-alpha",
		coordination.ManagedAgentCreateProjectRequest{
			Name: "project-alpha",
			OrganizationRef: coordination.OrganizationRef{
				Namespace: "cloud-agents",
				Kind:      "organization",
				ID:        "organization-alpha",
			},
			DisplayName: "Project Alpha",
		},
	)
	if err != nil || intent.RequestDigest() != "sha256:424834e4203084bf6a0a2ab84c995eae8fadb3eb96308be85b5a9520ba2a6738" ||
		intent.OrganizationID() != "organization-alpha" {
		t.Fatalf("intent/error = %#v / %v", intent, err)
	}
}

func TestManagedAgentCreateProjectDurableIntentRejectsClaimOnlyProfileAndDrift(t *testing.T) {
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
		{name: "v1 claim profile", profile: coordination.ManagedAgentCreateProject(), tenant: "tenant-alpha", request: valid},
		{name: "invalid tenant", profile: coordination.ManagedAgentCreateProjectDurable(), tenant: "tenant/alpha", request: valid},
		{name: "wrong organization namespace", profile: coordination.ManagedAgentCreateProjectDurable(), tenant: "tenant-alpha", request: func() coordination.ManagedAgentCreateProjectRequest {
			value := valid
			value.OrganizationRef.Namespace = "foreign"
			return value
		}()},
		{name: "empty display name", profile: coordination.ManagedAgentCreateProjectDurable(), tenant: "tenant-alpha", request: func() coordination.ManagedAgentCreateProjectRequest {
			value := valid
			value.DisplayName = ""
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := coordination.BindManagedAgentCreateProjectDurable(test.profile, test.tenant, test.request); !errors.Is(err, coordination.ErrInvalidManagedAgentCreateProjectRequest) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
