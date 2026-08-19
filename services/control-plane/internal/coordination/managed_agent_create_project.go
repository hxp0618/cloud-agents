package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"unicode/utf8"
)

var ErrInvalidManagedAgentCreateProjectRequest = errors.New("managed agent create project request is invalid")

// OrganizationRef is the strict body.organizationRef input bound by the
// generated managedAgentCreateProject profile.
type OrganizationRef struct {
	Namespace string
	Kind      string
	ID        string
}

// ManagedAgentCreateProjectRequest contains only the client-authored body
// fields admitted by the generated profile. Transport headers deliberately do
// not enter this value or the idempotency projection.
type ManagedAgentCreateProjectRequest struct {
	Name            string
	OrganizationRef OrganizationRef
	DisplayName     string
}

// ManagedAgentCreateProjectIntent is ordinary derived request data, not a
// mutation authority. Its fields stay opaque so consumers cannot substitute a
// caller-authored digest or an organization scope independent of the request.
type ManagedAgentCreateProjectIntent struct {
	requestDigest  string
	organizationID string
}

func (intent ManagedAgentCreateProjectIntent) RequestDigest() string  { return intent.requestDigest }
func (intent ManagedAgentCreateProjectIntent) OrganizationID() string { return intent.organizationID }

// BindManagedAgentCreateProject derives the only current profile-defined
// RFC 8785 projection and SHA-256 digest. Callers supply request data, but not
// projection policy, operation identity, or request digest authority.
func BindManagedAgentCreateProject(
	profile Profile,
	tenantID string,
	request ManagedAgentCreateProjectRequest,
) (ManagedAgentCreateProjectIntent, error) {
	if !profile.Valid() ||
		profile.ProjectionSchemaID() != "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/managed-agent-create-project-idempotency-projection.schema.json" ||
		profile.CanonicalizationProfile() != "cloud-agents-http-idempotency/managedAgentCreateProject/v1alpha1" ||
		profile.CanonicalizationAlgorithm() != "RFC8785" || profile.DigestAlgorithm() != "SHA-256" ||
		profile.TenantSource() != "path.tenantId" || profile.ScopeSource() != "body.organizationRef" ||
		profile.ScopeIdentitySchemaID() != "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/managed-agent-create-project-organization-ref.schema.json" ||
		profile.ScopeIdentifierProfile() != "cloud-agents-authorization-scope-identifier/ascii-v1" ||
		profile.ScopeIdentityComparison() != "exact_string_no_rewrite" ||
		!validIdentifier(tenantID) || !validIdentifier(request.Name) ||
		request.OrganizationRef.Namespace != "cloud-agents" || request.OrganizationRef.Kind != "organization" ||
		!validIdentifier(request.OrganizationRef.ID) ||
		!validUnicodeScalarLength(request.DisplayName, 1, 160) {
		return ManagedAgentCreateProjectIntent{}, ErrInvalidManagedAgentCreateProjectRequest
	}

	canonical := make([]byte, 0, len(tenantID)+len(request.Name)+len(request.OrganizationRef.ID)+len(request.DisplayName)+192)
	canonical = append(canonical, `{"body":{"displayName":`...)
	canonical = appendCanonicalJSONString(canonical, request.DisplayName)
	canonical = append(canonical, `,"name":`...)
	canonical = appendCanonicalJSONString(canonical, request.Name)
	canonical = append(canonical, `,"organizationRef":{"id":`...)
	canonical = appendCanonicalJSONString(canonical, request.OrganizationRef.ID)
	canonical = append(canonical, `,"kind":"organization","namespace":"cloud-agents"}},"operationId":"managedAgentCreateProject","path":{"tenantId":`...)
	canonical = appendCanonicalJSONString(canonical, tenantID)
	canonical = append(canonical, '}', '}')
	digest := sha256.Sum256(canonical)
	return ManagedAgentCreateProjectIntent{
		requestDigest:  "sha256:" + hex.EncodeToString(digest[:]),
		organizationID: request.OrganizationRef.ID,
	}, nil
}

func validIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		alphaNumeric := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		if index == 0 || index == len(value)-1 {
			if !alphaNumeric {
				return false
			}
		} else if !alphaNumeric && character != '.' && character != '_' && character != '~' && character != '-' {
			return false
		}
	}
	return true
}

func validUnicodeScalarLength(value string, minimum, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum
}

func appendCanonicalJSONString(destination []byte, value string) []byte {
	const hexadecimal = "0123456789abcdef"
	destination = append(destination, '"')
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch character {
		case '"', '\\':
			destination = append(destination, '\\', character)
		case '\b':
			destination = append(destination, '\\', 'b')
		case '\t':
			destination = append(destination, '\\', 't')
		case '\n':
			destination = append(destination, '\\', 'n')
		case '\f':
			destination = append(destination, '\\', 'f')
		case '\r':
			destination = append(destination, '\\', 'r')
		default:
			if character < 0x20 {
				destination = append(destination, '\\', 'u', '0', '0', hexadecimal[character>>4], hexadecimal[character&0x0f])
				continue
			}
			destination = append(destination, character)
		}
	}
	return append(destination, '"')
}
