package coordination

import "errors"

var ErrUnknownOperation = errors.New("durable coordination operation profile is not generated")

// operationProfile is a package-private capability copied only from the
// generated registry. Callers select an already resolved operation ID; they
// cannot supply profile identity, state, retry, outbox, or finalizer policy.
type operationProfile struct {
	profileID                 string
	profileDigest             string
	operationID               string
	projectionSchemaID        string
	canonicalizationProfile   string
	canonicalizationAlgorithm string
	digestAlgorithm           string
	tenantSource              string
	scopeSource               string
	scopeIdentitySchemaID     string
	scopeIdentifierProfile    string
	scopeIdentityComparison   string
	requiredPermission        string
	requiredScopeLevel        string
	outboxEventClass          string
	resultResourceKind        string
	replayTTLSeconds          int64
	createsPlatformOperation  bool
	externalSideEffectAllowed bool
}

func profileForOperation(operationID string) (operationProfile, error) {
	switch operationID {
	case managedAgentCreateProjectProfile.operationID:
		return managedAgentCreateProjectProfile, nil
	case managedAgentCreateProjectDurableProfile.operationID:
		return managedAgentCreateProjectDurableProfile, nil
	default:
		return operationProfile{}, ErrUnknownOperation
	}
}

// ManagedAgentCreateProject returns the frozen v1 claim-only generated profile
// as an opaque value for the PostgreSQL coordination service. No public HTTP
// route or external side effect is attached to this capability.
func ManagedAgentCreateProject() Profile {
	return Profile{profile: managedAgentCreateProjectProfile}
}

// ManagedAgentCreateProjectDurable returns the versioned, localdev-only
// project writer capability. It is intentionally a distinct generated profile
// from ManagedAgentCreateProject, whose claim-only semantics remain frozen.
func ManagedAgentCreateProjectDurable() Profile {
	return Profile{profile: managedAgentCreateProjectDurableProfile}
}

// Profile deliberately exposes no fields. Only this package can unwrap it.
type Profile struct{ profile operationProfile }

// Valid reports whether the value is the exact generated profile capability.
// It is used by package-internal consumers before opening a transaction.
func (profile Profile) Valid() bool {
	return (profile.profile == managedAgentCreateProjectProfile ||
		profile.profile == managedAgentCreateProjectDurableProfile) &&
		profile.profile.profileID != "" && profile.profile.profileDigest != "" &&
		!profile.profile.externalSideEffectAllowed
}

func (profile Profile) ProfileID() string     { return profile.profile.profileID }
func (profile Profile) ProfileDigest() string { return profile.profile.profileDigest }
func (profile Profile) OperationID() string   { return profile.profile.operationID }
func (profile Profile) ProjectionSchemaID() string {
	return profile.profile.projectionSchemaID
}
func (profile Profile) CanonicalizationProfile() string {
	return profile.profile.canonicalizationProfile
}
func (profile Profile) CanonicalizationAlgorithm() string {
	return profile.profile.canonicalizationAlgorithm
}
func (profile Profile) DigestAlgorithm() string { return profile.profile.digestAlgorithm }
func (profile Profile) TenantSource() string    { return profile.profile.tenantSource }
func (profile Profile) ScopeSource() string     { return profile.profile.scopeSource }
func (profile Profile) ScopeIdentitySchemaID() string {
	return profile.profile.scopeIdentitySchemaID
}
func (profile Profile) ScopeIdentifierProfile() string {
	return profile.profile.scopeIdentifierProfile
}
func (profile Profile) ScopeIdentityComparison() string {
	return profile.profile.scopeIdentityComparison
}
func (profile Profile) RequiredPermission() string {
	return profile.profile.requiredPermission
}
func (profile Profile) RequiredScopeLevel() string {
	return profile.profile.requiredScopeLevel
}
func (profile Profile) OutboxEventClass() string { return profile.profile.outboxEventClass }
func (profile Profile) ResultResourceKind() string {
	return profile.profile.resultResourceKind
}
func (profile Profile) ReplayTTLSeconds() int64 { return profile.profile.replayTTLSeconds }
func (profile Profile) CreatesPlatformOperation() bool {
	return profile.profile.createsPlatformOperation
}
func (profile Profile) ExternalSideEffectAllowed() bool {
	return profile.profile.externalSideEffectAllowed
}
