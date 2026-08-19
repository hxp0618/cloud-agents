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
	requiredPermission        string
	requiredScopeLevel        string
	outboxEventClass          string
	resultResourceKind        string
	replayTTLSeconds          int64
	createsPlatformOperation  bool
	externalSideEffectAllowed bool
}

func profileForOperation(operationID string) (operationProfile, error) {
	if operationID != managedAgentCreateProjectProfile.operationID {
		return operationProfile{}, ErrUnknownOperation
	}
	return managedAgentCreateProjectProfile, nil
}

// ManagedAgentCreateProject returns the only current generated profile as an
// opaque value for the PostgreSQL coordination service. No public HTTP route
// or external side effect is attached to this capability.
func ManagedAgentCreateProject() Profile {
	return Profile{profile: managedAgentCreateProjectProfile}
}

// Profile deliberately exposes no fields. Only this package can unwrap it.
type Profile struct{ profile operationProfile }

// Valid reports whether the value is the exact generated profile capability.
// It is used by package-internal consumers before opening a transaction.
func (profile Profile) Valid() bool {
	return profile.profile == managedAgentCreateProjectProfile &&
		profile.profile.profileID != "" && profile.profile.profileDigest != "" &&
		!profile.profile.createsPlatformOperation && !profile.profile.externalSideEffectAllowed
}

func (profile Profile) ProfileID() string     { return profile.profile.profileID }
func (profile Profile) ProfileDigest() string { return profile.profile.profileDigest }
func (profile Profile) OperationID() string   { return profile.profile.operationID }
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
