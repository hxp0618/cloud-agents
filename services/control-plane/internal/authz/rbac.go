package authz

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
)

const (
	ScopePlatform       ScopeLevel = "platform"
	ScopeTenant         ScopeLevel = "tenant"
	ScopeOrganization   ScopeLevel = "organization"
	ScopeProject        ScopeLevel = "project"
	MembershipActive               = "active"
	MembershipSuspended            = "suspended"
	MembershipRevoked              = "revoked"
	BindingActive                  = "active"
	BindingRevoked                 = "revoked"
)

const expectedBuiltinCatalogDigest = "640dac3144f1f1ce2499d354901a423889a797a4f35b137c45c80a0eda05c6a4"

var (
	ErrInvalidRequest    = errors.New("authorization request is invalid")
	ErrCatalogDrift      = errors.New("builtin role catalog drift")
	ErrSnapshotMalformed = errors.New("authorization snapshot is malformed")
	ErrScopeUnresolved   = errors.New("authorization request scope is unresolved")
	ErrOperationDenied   = errors.New("verified operation denied")
)

type ScopeLevel string

type SubjectRef struct {
	Kind    string
	Issuer  string
	Subject string
}

func (subject SubjectRef) Validate() error {
	if subject.Kind != "user" && subject.Kind != "serviceAccount" && subject.Kind != "workload" {
		return fmt.Errorf("%w: subject kind", ErrInvalidRequest)
	}
	if !validUTF8Length(subject.Issuer, 1, 512) {
		return fmt.Errorf("%w: subject issuer", ErrInvalidRequest)
	}
	if !validSubjectIssuer(subject.Issuer) {
		return fmt.Errorf("%w: subject issuer URI", ErrInvalidRequest)
	}
	if !validUTF8Length(subject.Subject, 1, 256) {
		return fmt.Errorf("%w: subject", ErrInvalidRequest)
	}
	return nil
}

// validSubjectIssuer is the closed absolute-URI lexical profile shared with
// cloud_agents.subject_ref_digest. It deliberately does not normalize the
// issuer: exact source bytes remain part of SubjectRef identity.
func validSubjectIssuer(issuer string) bool {
	colon := strings.IndexByte(issuer, ':')
	if colon < 1 || !asciiAlpha(issuer[0]) {
		return false
	}
	for index := 1; index < colon; index++ {
		character := issuer[index]
		if !asciiAlphaNumeric(character) && character != '+' && character != '-' && character != '.' {
			return false
		}
	}
	for index := 0; index < len(issuer); index++ {
		character := issuer[index]
		if character < 0x20 || character == 0x7f {
			return false
		}
		if character == '%' {
			if index+2 >= len(issuer) || !asciiHex(issuer[index+1]) || !asciiHex(issuer[index+2]) {
				return false
			}
			index += 2
		}
	}
	return true
}

func asciiAlpha(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func asciiAlphaNumeric(character byte) bool {
	return asciiAlpha(character) || character >= '0' && character <= '9'
}

func asciiHex(character byte) bool {
	return character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F'
}

func (subject SubjectRef) CanonicalBytes() ([]byte, error) {
	if err := subject.Validate(); err != nil {
		return nil, err
	}
	// RFC 8785 sorts these three member names as issuer, kind, subject. The
	// values are strings only, so the profile can be emitted without a generic
	// JSON canonicalizer. appendCanonicalJSONString implements the RFC 8785 /
	// ECMAScript string escaping profile without Go-only escapes.
	result := make([]byte, 0, len(subject.Issuer)+len(subject.Subject)+len(subject.Kind)+32)
	result = append(result, '{')
	result = append(result, '"', 'i', 's', 's', 'u', 'e', 'r', '"', ':')
	result = appendCanonicalJSONString(result, subject.Issuer)
	result = append(result, ',', '"', 'k', 'i', 'n', 'd', '"', ':')
	result = appendCanonicalJSONString(result, subject.Kind)
	result = append(result, ',', '"', 's', 'u', 'b', 'j', 'e', 'c', 't', '"', ':')
	result = appendCanonicalJSONString(result, subject.Subject)
	return append(result, '}'), nil
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

func (subject SubjectRef) Digest() (string, error) {
	canonical, err := subject.CanonicalBytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type ScopeRef struct {
	Level ScopeLevel
	ID    string
}

func (scope ScopeRef) Validate(tenantID string) error {
	if scope.Level == ScopePlatform {
		if scope.ID != "" {
			return fmt.Errorf("%w: platform scope has an id", ErrInvalidRequest)
		}
		return nil
	}
	if scope.Level != ScopeTenant && scope.Level != ScopeOrganization && scope.Level != ScopeProject {
		return fmt.Errorf("%w: unknown scope level", ErrInvalidRequest)
	}
	if !validOpaqueIdentifier(scope.ID) {
		return fmt.Errorf("%w: scope id", ErrInvalidRequest)
	}
	if scope.Level == ScopeTenant && scope.ID != tenantID {
		return fmt.Errorf("%w: tenant scope mismatch", ErrInvalidRequest)
	}
	return nil
}

type ScopePath struct {
	Level          ScopeLevel
	TenantID       string
	OrganizationID string
	ProjectID      string
}

func (scope ScopePath) Validate(tenantID string) error {
	switch scope.Level {
	case ScopePlatform:
		if scope.TenantID != "" || scope.OrganizationID != "" || scope.ProjectID != "" {
			return fmt.Errorf("%w: platform ancestry", ErrSnapshotMalformed)
		}
	case ScopeTenant:
		if scope.TenantID != tenantID || !validOpaqueIdentifier(scope.TenantID) || scope.OrganizationID != "" || scope.ProjectID != "" {
			return fmt.Errorf("%w: tenant ancestry", ErrSnapshotMalformed)
		}
	case ScopeOrganization:
		if !validOpaqueIdentifier(scope.TenantID) || scope.TenantID != tenantID || !validOpaqueIdentifier(scope.OrganizationID) || scope.ProjectID != "" {
			return fmt.Errorf("%w: organization ancestry", ErrSnapshotMalformed)
		}
	case ScopeProject:
		if !validOpaqueIdentifier(scope.TenantID) || scope.TenantID != tenantID || !validOpaqueIdentifier(scope.OrganizationID) || !validOpaqueIdentifier(scope.ProjectID) {
			return fmt.Errorf("%w: project ancestry", ErrSnapshotMalformed)
		}
	default:
		return fmt.Errorf("%w: unknown ancestry level", ErrSnapshotMalformed)
	}
	return nil
}

func (scope ScopePath) Contains(child ScopePath, tenantID string) bool {
	if scope.Validate(tenantID) != nil || child.Validate(tenantID) != nil {
		return false
	}
	if scope.Level == ScopePlatform {
		return true
	}
	if child.Level == ScopePlatform || scope.TenantID != child.TenantID {
		return false
	}
	switch scope.Level {
	case ScopeTenant:
		return true
	case ScopeOrganization:
		return child.OrganizationID == scope.OrganizationID
	case ScopeProject:
		return child.Level == ScopeProject && child.OrganizationID == scope.OrganizationID && child.ProjectID == scope.ProjectID
	default:
		return false
	}
}

type Role struct {
	Name            string
	Version         int64
	CatalogRevision int64
	ScopeLevel      ScopeLevel
	State           string
	PublishedAt     string
	Permissions     []string
}

type Catalog struct {
	Roles []Role
}

func (catalog Catalog) Validate() error {
	if len(catalog.Roles) != 7 {
		return fmt.Errorf("%w: role count", ErrCatalogDrift)
	}
	seenRoles := make(map[string]struct{}, len(catalog.Roles))
	for index, role := range catalog.Roles {
		if role.Name == "" || role.Version != 1 || role.CatalogRevision != 1 || role.State != "active" || role.PublishedAt != "2026-08-17T00:00:00Z" {
			return fmt.Errorf("%w: role %d identity", ErrCatalogDrift, index)
		}
		if _, exists := seenRoles[role.Name]; exists {
			return fmt.Errorf("%w: duplicate role", ErrCatalogDrift)
		}
		seenRoles[role.Name] = struct{}{}
		if role.ScopeLevel != ScopePlatform && role.ScopeLevel != ScopeTenant && role.ScopeLevel != ScopeOrganization && role.ScopeLevel != ScopeProject {
			return fmt.Errorf("%w: role scope", ErrCatalogDrift)
		}
		if len(role.Permissions) == 0 {
			return fmt.Errorf("%w: empty role permission set", ErrCatalogDrift)
		}
		for permissionIndex, permission := range role.Permissions {
			if !validPermission(permission) || strings.Contains(permission, "*") {
				return fmt.Errorf("%w: invalid permission", ErrCatalogDrift)
			}
			if permissionIndex > 0 && role.Permissions[permissionIndex-1] >= permission {
				return fmt.Errorf("%w: permission order", ErrCatalogDrift)
			}
		}
		if index > 0 && catalog.Roles[index-1].Name >= role.Name {
			return fmt.Errorf("%w: role order", ErrCatalogDrift)
		}
	}
	canonical, err := catalog.canonicalBytes()
	if err != nil {
		return fmt.Errorf("%w: canonical catalog: %v", ErrCatalogDrift, err)
	}
	digest := sha256.Sum256(canonical)
	if hex.EncodeToString(digest[:]) != expectedBuiltinCatalogDigest {
		return fmt.Errorf("%w: canonical digest", ErrCatalogDrift)
	}
	return nil
}

func (catalog Catalog) Role(name string, version int64) (Role, bool) {
	for _, role := range catalog.Roles {
		if role.Name == name && role.Version == version {
			return role, true
		}
	}
	return Role{}, false
}

func (catalog Catalog) canonicalBytes() ([]byte, error) {
	if len(catalog.Roles) == 0 {
		return nil, ErrCatalogDrift
	}
	canonical := struct {
		APIVersion      string          `json:"apiVersion"`
		CatalogRevision string          `json:"catalogRevision"`
		Kind            string          `json:"kind"`
		PublishedAt     string          `json:"publishedAt"`
		Roles           []canonicalRole `json:"roles"`
	}{
		APIVersion:      "platform.cloud-agents.dev/v1alpha1",
		CatalogRevision: "1",
		Kind:            "BuiltinRoleCatalog",
		PublishedAt:     "2026-08-17T00:00:00Z",
		Roles:           make([]canonicalRole, len(catalog.Roles)),
	}
	for index, role := range catalog.Roles {
		canonical.Roles[index] = canonicalRole{
			Name:        role.Name,
			Permissions: append([]string(nil), role.Permissions...),
			ScopeLevel:  string(role.ScopeLevel),
			State:       role.State,
			Version:     role.Version,
		}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(canonical); err != nil {
		return nil, err
	}
	result := buffer.Bytes()
	return append([]byte(nil), result[:len(result)-1]...), nil
}

type canonicalRole struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	ScopeLevel  string   `json:"scopeLevel"`
	State       string   `json:"state"`
	Version     int64    `json:"version"`
}

type MembershipFact struct {
	UID         string
	Subject     SubjectRef
	SubjectHash string
	Scope       ScopePath
	State       string
	ExpiresAt   *time.Time
}

type RoleBindingFact struct {
	UID         string
	Subject     SubjectRef
	SubjectHash string
	RoleName    string
	RoleVersion int64
	Scope       ScopePath
	State       string
	ExpiresAt   *time.Time
}

type Candidate struct {
	Membership MembershipFact
	Binding    RoleBindingFact
}

type Snapshot struct {
	TenantID      string
	Scope         ScopePath
	ScopeResolved bool
	Catalog       Catalog
	Candidates    []Candidate
}

// VerifiedOperationBinder exists only while WithVerifiedOperation's callback
// is active. Its fields are deliberately private so a caller cannot mint or
// alter an actor, target, or permission binding.
type VerifiedOperationBinder struct {
	self       *VerifiedOperationBinder
	lifetime   *operationLifetime
	consumed   *atomic.Bool
	progress   *atomic.Uint32
	actor      SubjectRef
	tenantID   string
	resource   ScopeRef
	permission string
	binding    [sha256.Size]byte
}

// VerifiedOperation is a one-shot, callback-live authorization operation.
// Actor is only a bounded lookup key; Execute is the sole authority-bearing
// method and never returns a reusable allow decision.
type VerifiedOperation struct {
	self       *VerifiedOperation
	lifetime   *operationLifetime
	consumed   *atomic.Bool
	progress   *atomic.Uint32
	actor      SubjectRef
	tenantID   string
	resource   ScopeRef
	permission string
	binding    [sha256.Size]byte
}

// WithVerifiedOperation atomically consumes a verified principal and keeps
// its exact trust-generation lease through callback completion.
func WithVerifiedOperation(principal *authn.VerifiedPrincipal, callback func(*VerifiedOperationBinder) error) error {
	return authn.ConsumeVerifiedPrincipal(principal, func(view authn.VerifiedPrincipalView) error {
		if callback == nil || !view.Check() {
			return ErrOperationDenied
		}
		kind, issuer, value, actorOK := view.Actor()
		tenantID, resourceLevel, resourceID, permission, contextOK := view.AuthorizationContext()
		if !actorOK || !contextOK || !view.Check() {
			return ErrOperationDenied
		}
		actor := SubjectRef{Kind: kind, Issuer: issuer, Subject: value}
		resource := ScopeRef{Level: ScopeLevel(resourceLevel), ID: resourceID}
		if actor.Validate() != nil || validateOpaqueTenant(tenantID) != nil || resource.Validate(tenantID) != nil || !validPermission(permission) {
			return ErrOperationDenied
		}
		lifetime := newOperationLifetime()
		progress := &atomic.Uint32{}
		binder := &VerifiedOperationBinder{
			lifetime: lifetime, consumed: &atomic.Bool{}, progress: progress, actor: actor, tenantID: tenantID, resource: resource, permission: permission,
		}
		binder.binding = operationBinding(actor, tenantID, resource, permission)
		binder.self = binder
		closed := false
		defer func() {
			if !closed {
				lifetime.close()
			}
		}()
		callbackErr := callback(binder)
		// Drain any Execute call that acquired the callback lifetime before the
		// callback returned. This makes the success check race-free while still
		// denying a goroutine that starts only after authority has expired.
		lifetime.close()
		closed = true
		if callbackErr != nil {
			return callbackErr
		}
		if progress.Load() != operationProgressExecuted {
			return ErrOperationDenied
		}
		return nil
	})
}

// Bind spends the binder before validating the requested operation. Thus a
// mismatch, malformed request, shallow copy, or tamper cannot be retried.
func (binder *VerifiedOperationBinder) Bind(tenantID string, resource ScopeRef, permission string) (*VerifiedOperation, error) {
	if binder == nil || binder.consumed == nil || binder.progress == nil || !binder.consumed.CompareAndSwap(false, true) {
		return nil, ErrOperationDenied
	}
	if binder.self != binder || binder.lifetime == nil ||
		binder.binding != operationBinding(binder.actor, binder.tenantID, binder.resource, binder.permission) || !binder.lifetime.acquire() {
		return nil, ErrOperationDenied
	}
	defer binder.lifetime.release()
	if tenantID != binder.tenantID || resource != binder.resource || permission != binder.permission {
		return nil, ErrOperationDenied
	}
	operation := &VerifiedOperation{
		lifetime: binder.lifetime, consumed: &atomic.Bool{}, progress: binder.progress, actor: binder.actor, tenantID: binder.tenantID,
		resource: binder.resource, permission: binder.permission, binding: binder.binding,
	}
	operation.self = operation
	binder.progress.Store(operationProgressBound)
	return operation, nil
}

// Actor returns the exact decoded lexical identity only while this unspent
// operation and its enclosing verified-principal callback remain live.
func (operation *VerifiedOperation) Actor() (SubjectRef, bool) {
	if !operation.selfBound() || operation.consumed == nil || operation.progress == nil || operation.consumed.Load() || !operation.lifetime.acquire() {
		return SubjectRef{}, false
	}
	defer operation.lifetime.release()
	if operation.consumed.Load() {
		return SubjectRef{}, false
	}
	return operation.actor, true
}

// Execute spends the operation and invokes callback only when the current
// snapshot permits its exact bound actor, target, and permission.
func (operation *VerifiedOperation) Execute(snapshot Snapshot, now time.Time, callback func() error) error {
	if operation == nil || operation.consumed == nil || operation.progress == nil || !operation.consumed.CompareAndSwap(false, true) {
		return ErrOperationDenied
	}
	if !operation.selfBound() || callback == nil || !operation.lifetime.acquire() {
		return ErrOperationDenied
	}
	defer operation.lifetime.release()
	if snapshot.TenantID != operation.tenantID {
		return ErrOperationDenied
	}
	result, err := evaluate(snapshot, authorizationRequest{Subject: operation.actor, Permission: operation.permission, Resource: operation.resource}, now)
	if err != nil {
		return err
	}
	if !result.Allowed {
		return ErrOperationDenied
	}
	// Reaching the protected callback spends the authorized operation even
	// when the protected work reports an operational error. Callers may settle
	// a database rejection or unknown commit outcome into a typed result inside
	// WithVerifiedOperation's callback; leaving progress at Bound would then
	// incorrectly rewrite that successful settlement as ErrOperationDenied.
	operation.progress.Store(operationProgressExecuted)
	return callback()
}

func (operation *VerifiedOperation) selfBound() bool {
	return operation != nil && operation.self == operation && operation.lifetime != nil &&
		operation.binding == operationBinding(operation.actor, operation.tenantID, operation.resource, operation.permission)
}

const (
	operationProgressBound uint32 = iota + 1
	operationProgressExecuted
)

type operationLifetime struct {
	mutex     sync.Mutex
	condition *sync.Cond
	live      bool
	active    int
}

func newOperationLifetime() *operationLifetime {
	lifetime := &operationLifetime{live: true}
	lifetime.condition = sync.NewCond(&lifetime.mutex)
	return lifetime
}

func (lifetime *operationLifetime) acquire() bool {
	if lifetime == nil {
		return false
	}
	lifetime.mutex.Lock()
	defer lifetime.mutex.Unlock()
	if !lifetime.live {
		return false
	}
	lifetime.active++
	return true
}

func (lifetime *operationLifetime) release() {
	if lifetime == nil {
		return
	}
	lifetime.mutex.Lock()
	lifetime.active--
	if lifetime.active == 0 && lifetime.condition != nil {
		lifetime.condition.Broadcast()
	}
	lifetime.mutex.Unlock()
}

func (lifetime *operationLifetime) close() {
	if lifetime == nil {
		return
	}
	lifetime.mutex.Lock()
	lifetime.live = false
	for lifetime.active != 0 {
		lifetime.condition.Wait()
	}
	lifetime.mutex.Unlock()
}

func operationBinding(actor SubjectRef, tenantID string, resource ScopeRef, permission string) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("cloud-agents/authz/verified-operation/v1\x00"))
	for _, value := range []string{actor.Kind, actor.Issuer, actor.Subject, tenantID, string(resource.Level), resource.ID, permission} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(value))
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

type authorizationRequest struct {
	Subject    SubjectRef
	Permission string
	Resource   ScopeRef
}

type denyReason string

const (
	denyInvalidRequest    denyReason = "invalid_request"
	denyUnknownScope      denyReason = "unknown_scope"
	denyNoEligibleBinding denyReason = "no_eligible_binding"
	denyPlatformRuntime   denyReason = "platform_scope_requires_bootstrap"
)

type evidence struct {
	MembershipUID  string
	RoleBindingUID string
	RoleName       string
	RoleVersion    int64
}

type decision struct {
	Allowed  bool
	Reason   denyReason
	evidence *evidence
}

func evaluate(snapshot Snapshot, request authorizationRequest, now time.Time) (decision, error) {
	if err := validateOpaqueTenant(snapshot.TenantID); err != nil || now.IsZero() {
		return decision{}, fmt.Errorf("%w: tenant or clock", ErrInvalidRequest)
	}
	if err := request.Subject.Validate(); err != nil {
		return decision{Reason: denyInvalidRequest}, nil
	}
	if err := request.Resource.Validate(snapshot.TenantID); err != nil {
		return decision{Reason: denyInvalidRequest}, nil
	}
	if request.Resource.Level == ScopePlatform {
		return decision{Reason: denyPlatformRuntime}, nil
	}
	if !snapshot.ScopeResolved {
		return decision{Reason: denyUnknownScope}, nil
	}
	if err := snapshot.Scope.Validate(snapshot.TenantID); err != nil {
		return decision{}, err
	}
	if !snapshot.Scope.matches(request.Resource, snapshot.TenantID) {
		return decision{}, fmt.Errorf("%w: resolved request scope", ErrSnapshotMalformed)
	}
	if err := snapshot.Catalog.Validate(); err != nil {
		return decision{}, err
	}
	requestedDigest, err := request.Subject.Digest()
	if err != nil {
		return decision{Reason: denyInvalidRequest}, nil
	}
	seen := make(map[string]struct{}, len(snapshot.Candidates))
	for _, candidate := range snapshot.Candidates {
		key := candidate.Membership.UID + "\x00" + candidate.Binding.UID
		if _, exists := seen[key]; exists {
			return decision{}, fmt.Errorf("%w: duplicate candidate", ErrSnapshotMalformed)
		}
		seen[key] = struct{}{}
		if err := validateCandidate(snapshot.TenantID, candidate); err != nil {
			return decision{}, err
		}
		if candidate.Membership.Subject != request.Subject || candidate.Binding.Subject != request.Subject || candidate.Membership.SubjectHash != requestedDigest || candidate.Binding.SubjectHash != requestedDigest {
			return decision{}, fmt.Errorf("%w: subject binding", ErrSnapshotMalformed)
		}
	}
	for _, candidate := range snapshot.Candidates {
		if candidate.Membership.State != MembershipActive || expired(candidate.Membership.ExpiresAt, now) || candidate.Binding.State != BindingActive || expired(candidate.Binding.ExpiresAt, now) {
			continue
		}
		role, ok := snapshot.Catalog.Role(candidate.Binding.RoleName, candidate.Binding.RoleVersion)
		if !ok || role.State != "active" || role.ScopeLevel == ScopePlatform || role.ScopeLevel != candidate.Binding.Scope.Level || !containsPermission(role.Permissions, request.Permission) {
			continue
		}
		if !candidate.Membership.Scope.Contains(candidate.Binding.Scope, snapshot.TenantID) || !candidate.Binding.Scope.Contains(snapshot.Scope, snapshot.TenantID) {
			continue
		}
		return decision{
			Allowed: true,
			evidence: &evidence{
				MembershipUID:  candidate.Membership.UID,
				RoleBindingUID: candidate.Binding.UID,
				RoleName:       role.Name,
				RoleVersion:    role.Version,
			},
		}, nil
	}
	return decision{Reason: denyNoEligibleBinding}, nil
}

func (scope ScopePath) matches(resource ScopeRef, tenantID string) bool {
	if scope.Validate(tenantID) != nil || resource.Validate(tenantID) != nil || scope.Level != resource.Level {
		return false
	}
	switch resource.Level {
	case ScopeTenant:
		return scope.TenantID == resource.ID
	case ScopeOrganization:
		return scope.OrganizationID == resource.ID
	case ScopeProject:
		return scope.ProjectID == resource.ID
	default:
		return false
	}
}

func validateCandidate(tenantID string, candidate Candidate) error {
	if !validOpaqueIdentifier(candidate.Membership.UID) || !validOpaqueIdentifier(candidate.Binding.UID) {
		return fmt.Errorf("%w: resource uid", ErrSnapshotMalformed)
	}
	if err := candidate.Membership.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: membership subject row", ErrSnapshotMalformed)
	}
	if err := candidate.Binding.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject row", ErrSnapshotMalformed)
	}
	if err := candidate.Membership.Scope.Validate(tenantID); err != nil {
		return fmt.Errorf("%w: membership scope row", ErrSnapshotMalformed)
	}
	if err := candidate.Binding.Scope.Validate(tenantID); err != nil {
		return fmt.Errorf("%w: scope row", ErrSnapshotMalformed)
	}
	if candidate.Membership.State != MembershipActive && candidate.Membership.State != MembershipSuspended && candidate.Membership.State != MembershipRevoked {
		return fmt.Errorf("%w: membership state", ErrSnapshotMalformed)
	}
	if candidate.Binding.State != BindingActive && candidate.Binding.State != BindingRevoked {
		return fmt.Errorf("%w: binding state", ErrSnapshotMalformed)
	}
	return nil
}

func expired(value *time.Time, now time.Time) bool {
	return value != nil && !now.Before(*value)
}

func containsPermission(permissions []string, requested string) bool {
	for _, permission := range permissions {
		if permission == requested {
			return true
		}
	}
	return false
}

func validPermission(value string) bool {
	if len(value) < 3 || len(value) > 128 || strings.Contains(value, "*") {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for index, character := range part {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
			if index == 0 && (character < 'a' || character > 'z') {
				return false
			}
		}
	}
	return true
}

func validateOpaqueTenant(value string) error {
	if !validOpaqueIdentifier(value) {
		return fmt.Errorf("%w: tenant id", ErrInvalidRequest)
	}
	return nil
}

func validOpaqueIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		isAlphaNumeric := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if index == 0 || index == len(value)-1 {
			if !isAlphaNumeric {
				return false
			}
		} else if !isAlphaNumeric && !strings.ContainsRune("._~-", character) {
			return false
		}
	}
	return true
}

func validUTF8Length(value string, minimum, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum
}
