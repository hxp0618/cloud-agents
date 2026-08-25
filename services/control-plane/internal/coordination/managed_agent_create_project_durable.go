package coordination

import (
	"crypto/sha256"
	"encoding/hex"
)

// BindManagedAgentCreateProjectDurable derives the versioned localdev writer
// projection. It deliberately has a separate operation identity so a caller
// cannot substitute the frozen claim-only profile or route.
func BindManagedAgentCreateProjectDurable(
	profile Profile,
	tenantID string,
	request ManagedAgentCreateProjectRequest,
) (ManagedAgentCreateProjectIntent, error) {
	if !profile.Valid() ||
		profile.ProfileID() != "managedAgentCreateProjectDurable/v1alpha1" ||
		profile.OperationID() != "managedAgentCreateProjectDurable" ||
		profile.ProjectionSchemaID() != "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/durable-project-create-idempotency-projection.schema.json" ||
		profile.CanonicalizationProfile() != "cloud-agents-http-idempotency/managedAgentCreateProjectDurable/v1alpha1" ||
		profile.CanonicalizationAlgorithm() != "RFC8785" || profile.DigestAlgorithm() != "SHA-256" ||
		profile.TenantSource() != "path.tenantId" || profile.ScopeSource() != "body.organizationRef" ||
		profile.ScopeIdentitySchemaID() != "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/managed-agent-create-project-organization-ref.schema.json" ||
		profile.ScopeIdentifierProfile() != "cloud-agents-authorization-scope-identifier/ascii-v1" ||
		profile.ScopeIdentityComparison() != "exact_string_no_rewrite" ||
		profile.RequiredPermission() != "projects.create" || profile.RequiredScopeLevel() != "organization" ||
		!profile.CreatesPlatformOperation() || profile.ExternalSideEffectAllowed() ||
		profile.OutboxEventClass() != "operation_effect" || profile.ResultResourceKind() != "project" ||
		profile.ReplayTTLSeconds() != 86400 || !validIdentifier(tenantID) ||
		!validIdentifier(request.Name) || request.OrganizationRef.Namespace != "cloud-agents" ||
		request.OrganizationRef.Kind != "organization" || !validIdentifier(request.OrganizationRef.ID) ||
		!validUnicodeScalarLength(request.DisplayName, 1, 160) {
		return ManagedAgentCreateProjectIntent{}, ErrInvalidManagedAgentCreateProjectRequest
	}

	canonical := make([]byte, 0, len(tenantID)+len(request.Name)+len(request.OrganizationRef.ID)+len(request.DisplayName)+224)
	canonical = append(canonical, `{"body":{"displayName":`...)
	canonical = appendCanonicalJSONString(canonical, request.DisplayName)
	canonical = append(canonical, `,"name":`...)
	canonical = appendCanonicalJSONString(canonical, request.Name)
	canonical = append(canonical, `,"organizationRef":{"id":`...)
	canonical = appendCanonicalJSONString(canonical, request.OrganizationRef.ID)
	canonical = append(canonical, `,"kind":"organization","namespace":"cloud-agents"}},"operationId":"managedAgentCreateProjectDurable","path":{"tenantId":`...)
	canonical = appendCanonicalJSONString(canonical, tenantID)
	canonical = append(canonical, '}', '}')
	digest := sha256.Sum256(canonical)
	return ManagedAgentCreateProjectIntent{
		requestDigest:  "sha256:" + hex.EncodeToString(digest[:]),
		organizationID: request.OrganizationRef.ID,
	}, nil
}
