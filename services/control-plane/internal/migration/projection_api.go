package migration

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AuthorityProjectionDigestDomain = "cloud-agents-platform-authority-projection/v1"
	CatalogProjectionDigestDomain   = "cloud-agents-platform-catalog-projection/v1"
	ProjectionLimitsProfile         = "cloud-agents-platform-projection-limits/v1"
	ProjectionRedactionProfile      = "cloud-agents-platform-projection-redaction/v1"
	PostgreSQLAuthorityAdapter      = "postgresql-authority-v1"
	PostgreSQLCatalogAdapter        = "postgresql-catalog-v1"
)

type SnapshotMode string

const (
	IdleReadSnapshot  SnapshotMode = "idle_read_repeatable_read_only"
	MigrationSnapshot SnapshotMode = "migration_serializable_read_write"
)

type SnapshotOwnership string

const (
	OwnedIdleSnapshot         SnapshotOwnership = "owned_idle"
	BorrowedMigrationSnapshot SnapshotOwnership = "borrowed_migration"
)

// SnapshotMetadata is the closed, non-digest observation boundary from ADR-0010.
type SnapshotMetadata struct {
	Mode             SnapshotMode      `json:"mode"`
	Ownership        SnapshotOwnership `json:"ownership"`
	PostgresMajor    uint16            `json:"postgres_major"`
	ServerVersionNum uint32            `json:"server_version_num"`
	DatabaseName     string            `json:"database_name"`
	AuthorityPhase   AuthorityPhase    `json:"authority_phase"`
	SessionUser      string            `json:"session_user"`
	CurrentUser      string            `json:"current_user"`
	IsolationLevel   string            `json:"isolation_level"`
	AccessMode       string            `json:"access_mode"`
	Deferrable       bool              `json:"deferrable"`
	TxStatus         string            `json:"tx_status"`
	MigrationID      *string           `json:"migration_id"`
	StatementIndex   *uint32           `json:"statement_index"`
}

type ProjectionKind string

const (
	ProjectionKindAuthority    ProjectionKind = "authority"
	ProjectionKindCatalog      ProjectionKind = "catalog"
	ProjectionKindCatalogState ProjectionKind = "catalog_state"
)

type ProjectionMetadata struct {
	ProjectionKind        ProjectionKind   `json:"projection_kind"`
	DigestDomain          string           `json:"digest_domain"`
	AdapterProfile        string           `json:"adapter_profile"`
	Snapshot              SnapshotMetadata `json:"snapshot"`
	VerifiedSubjectDigest Digest           `json:"verified_subject_digest"`
	Scope                 *ProjectionScope `json:"scope"`
	LimitsProfile         string           `json:"limits_profile"`
	QueryCount            uint32           `json:"query_count"`
	RowCount              uint64           `json:"row_count"`
	TotalBytes            uint64           `json:"total_bytes"`
	RedactionProfile      string           `json:"redaction_profile"`
}

type ProjectionResult[T any] struct {
	Projection T                  `json:"projection"`
	Digest     Digest             `json:"digest"`
	Metadata   ProjectionMetadata `json:"metadata"`
}

func (metadata SnapshotMetadata) validate() error {
	if metadata.PostgresMajor == 0 || metadata.ServerVersionNum/10_000 != uint32(metadata.PostgresMajor) || metadata.DatabaseName == "" || metadata.SessionUser == "" || metadata.CurrentUser == "" || metadata.TxStatus != "T" || metadata.Deferrable {
		return projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-metadata", "identity", metadata.PostgresMajor, false, "snapshot metadata identity or transaction status is invalid")
	}
	switch metadata.Mode {
	case IdleReadSnapshot:
		if metadata.Ownership != OwnedIdleSnapshot || metadata.IsolationLevel != "repeatable_read" || metadata.AccessMode != "read_only" {
			return projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-metadata", "transaction", metadata.PostgresMajor, false, "owned idle transaction metadata is invalid")
		}
		if metadata.AuthorityPhase != AuthorityPhaseConnectedSession && metadata.AuthorityPhase != AuthorityPhaseMigrationRole || metadata.MigrationID != nil || metadata.StatementIndex != nil {
			return projectionFailure(CodeProjectionMetadataMismatch, "snapshot-metadata", "owned_idle", metadata.PostgresMajor, false, "owned idle nullable metadata is invalid")
		}
	case MigrationSnapshot:
		if metadata.Ownership != BorrowedMigrationSnapshot || metadata.IsolationLevel != "serializable" || metadata.AccessMode != "read_write" {
			return projectionFailure(CodeProjectionSnapshotInvalid, "snapshot-metadata", "transaction", metadata.PostgresMajor, false, "borrowed migration transaction metadata is invalid")
		}
		if metadata.AuthorityPhase != AuthorityPhaseMigrationTransaction || metadata.MigrationID == nil || !migrationIDPattern.MatchString(*metadata.MigrationID) {
			return projectionFailure(CodeProjectionMetadataMismatch, "snapshot-metadata", "borrowed_migration", metadata.PostgresMajor, false, "borrowed migration identity metadata is invalid")
		}
	default:
		return projectionFailure(CodeProjectionMetadataMismatch, "snapshot-metadata", "mode", metadata.PostgresMajor, false, "snapshot mode is invalid")
	}
	return nil
}

func (metadata ProjectionMetadata) validate() error {
	if err := metadata.Snapshot.validate(); err != nil {
		return err
	}
	if err := requireDigest("projection-metadata.verified_subject_digest", metadata.VerifiedSubjectDigest); err != nil {
		return err
	}
	if metadata.LimitsProfile != ProjectionLimitsProfile || metadata.RedactionProfile != ProjectionRedactionProfile || metadata.QueryCount > projectionMaxQueriesPerProjection || metadata.TotalBytes > projectionMaxTotalResultBytes {
		return projectionFailure(CodeProjectionMetadataMismatch, "projection-metadata", "bounded_metadata", metadata.Snapshot.PostgresMajor, false, "projection bounded metadata is invalid")
	}
	switch metadata.ProjectionKind {
	case ProjectionKindAuthority:
		if metadata.Scope != nil || metadata.DigestDomain != AuthorityProjectionDigestDomain || metadata.AdapterProfile != PostgreSQLAuthorityAdapter {
			return projectionFailure(CodeProjectionMetadataMismatch, "projection-metadata", "authority", metadata.Snapshot.PostgresMajor, false, "authority projection metadata mapping is invalid")
		}
	case ProjectionKindCatalog:
		if metadata.Scope == nil || metadata.Scope.ScopeKind != "final" || metadata.DigestDomain != CatalogProjectionDigestDomain || metadata.AdapterProfile != PostgreSQLCatalogAdapter {
			return projectionFailure(CodeProjectionMetadataMismatch, "projection-metadata", "catalog", metadata.Snapshot.PostgresMajor, false, "catalog projection metadata mapping is invalid")
		}
		if err := metadata.Scope.Validate(); err != nil {
			return projectionFailure(CodeProjectionMetadataMismatch, "projection-metadata", "catalog.scope", metadata.Snapshot.PostgresMajor, false, "catalog projection scope is invalid")
		}
	case ProjectionKindCatalogState:
		if metadata.Scope == nil || metadata.DigestDomain != CatalogStateDigestDomain || metadata.AdapterProfile != PostgreSQLCatalogAdapter {
			return projectionFailure(CodeProjectionMetadataMismatch, "projection-metadata", "catalog_state", metadata.Snapshot.PostgresMajor, false, "catalog-state projection metadata mapping is invalid")
		}
		if err := metadata.Scope.Validate(); err != nil {
			return projectionFailure(CodeProjectionMetadataMismatch, "projection-metadata", "catalog_state.scope", metadata.Snapshot.PostgresMajor, false, "catalog-state projection scope is invalid")
		}
	default:
		return projectionFailure(CodeProjectionMetadataMismatch, "projection-metadata", "projection_kind", metadata.Snapshot.PostgresMajor, false, "projection kind is invalid")
	}
	return nil
}

// projectionQueryID is deliberately unexported. A ProjectionSnapshot can only
// execute the closed query registry supplied by the PostgreSQL adapter.
type projectionQueryID uint8

const (
	projectionQuerySnapshotMetadata projectionQueryID = iota + 1
	projectionQuerySnapshotConfigure
	projectionQuerySnapshotReset
	projectionQuerySnapshotSanitation
	projectionQuerySnapshotSetMigrationRole
	projectionQuerySnapshotRoleReadback
	projectionQueryCapability
	projectionQueryAuthorityRoles
	projectionQueryAuthorityMemberships
	projectionQueryAuthorityReachability
	projectionQueryDatabaseAuthority
	projectionQueryRoleSettings
	projectionQueryNamespace
	projectionQueryNamespaceCreators
	projectionQueryDefaultACLs
)

type projectionQueryStats struct {
	QueryCount uint32
	RowCount   uint64
	TotalBytes uint64
}

// catalogQueryer has no raw-SQL method. It is also unexported so packages
// outside migration cannot widen or implement the query surface.
type catalogQueryer interface {
	queryProjection(context.Context, projectionQueryID, ...any) (Rows, error)
	queryProjectionRow(context.Context, projectionQueryID, ...any) Row
	projectionStats() projectionQueryStats
}

type ProjectionSnapshot interface {
	catalogQueryer
	Metadata() SnapshotMetadata
	projectionSnapshot()
}

type IdleProjectionSnapshot interface {
	ProjectionSnapshot
	RollbackAndRelease(context.Context) error
	idleProjectionSnapshot()
}

// RunnerSessionProjectionSnapshot borrows the exact dedicated DatabaseSession
// already owned by Runner. It can only rollback its RR/RO projection
// transaction and return that same physical session; it cannot release to a
// pool, set role, unlock, execute migration SQL, commit, or close the session.
type RunnerSessionProjectionSnapshot interface {
	ProjectionSnapshot
	RollbackAndReturnToRunner(context.Context) error
	runnerSessionProjectionSnapshot()
}

type Projector interface {
	ProjectAuthority(context.Context, ProjectionSnapshot, VerifiedAuthorityContract, AuthorityPhase) (ProjectionResult[AuthorityProjection], error)
	ProjectCatalog(context.Context, ProjectionSnapshot, VerifiedCatalogContract, ProjectionScope) (ProjectionResult[CatalogProjection], error)
	ProjectPrecondition(context.Context, ProjectionSnapshot, VerifiedSchemaBundleScope, CatalogPrecondition) (ProjectionResult[CatalogStateProjection], error)
	ProjectTransitionState(context.Context, ProjectionSnapshot, VerifiedCatalogContract, ProjectionScope) (ProjectionResult[CatalogStateProjection], error)
}

// VerifiedAuthorityContract is intentionally opaque. Only a trust verifier in
// this package may set verified after binding the signed authority subject.
type VerifiedAuthorityContract struct {
	verified                      bool
	subjectDigest                 Digest
	expected                      AuthorityExpectedProjections
	expectedCanonical             string
	expectedDigest                Digest
	verifiedDecisionExpiresAt     time.Time
	verifiedDecisionSecurityEpoch uint64
}

func (contract VerifiedAuthorityContract) validate() error {
	return contract.validateAt(time.Now())
}

func (contract VerifiedAuthorityContract) validateAt(now time.Time) error {
	if !contract.verified {
		return projectionFailure(CodeUntrusted, "verified-authority", "subject", 0, false, "authority subject was not produced by a trust verifier")
	}
	if err := validateProjectionTrustDecision("verified-authority", contract.verifiedDecisionExpiresAt, contract.verifiedDecisionSecurityEpoch, now); err != nil {
		return err
	}
	if err := requireDigest("verified-authority.subject", contract.subjectDigest); err != nil {
		return err
	}
	for phase, projection := range map[AuthorityPhase]AuthorityProjection{
		AuthorityPhaseConnectedSession:     contract.expected.ConnectedSession,
		AuthorityPhaseMigrationRole:        contract.expected.MigrationRole,
		AuthorityPhaseMigrationTransaction: contract.expected.MigrationTransaction,
	} {
		if projection.Phase != phase {
			return projectionFailure(CodeProjectionMetadataMismatch, "verified-authority", "expected.phase", 0, false, "authority phase binding is invalid")
		}
		if err := projection.Validate(); err != nil {
			return projectionFailure(CodeUntrusted, "verified-authority", "expected_projection", 0, false, "authority projection is invalid")
		}
	}
	canonical, digest, err := canonicalVerifiedBinding(contract.authorityBinding())
	if err != nil || contract.expectedCanonical == "" || canonical != contract.expectedCanonical || digest != contract.expectedDigest {
		return projectionFailure(CodeUntrusted, "verified-authority", "expected_binding", 0, false, "authority expected projection differs from its verified binding")
	}
	return nil
}

type verifiedAuthorityBinding struct {
	SubjectDigest Digest                       `json:"subject_digest"`
	Expected      AuthorityExpectedProjections `json:"expected"`
	ExpiresAt     string                       `json:"expires_at"`
	SecurityEpoch uint64                       `json:"security_epoch"`
}

func (contract VerifiedAuthorityContract) authorityBinding() verifiedAuthorityBinding {
	return verifiedAuthorityBinding{
		SubjectDigest: contract.subjectDigest, Expected: contract.expected,
		ExpiresAt: canonicalProjectionExpiry(contract.verifiedDecisionExpiresAt), SecurityEpoch: contract.verifiedDecisionSecurityEpoch,
	}
}

// bindVerifiedAuthorityContract is the package-private trust-verifier entry
// point. It owns a deep copy and freezes its canonical identity before the
// wrapper can reach a projector.
func bindVerifiedAuthorityContract(subject Digest, expected AuthorityExpectedProjections, expiresAt time.Time, securityEpoch uint64) (VerifiedAuthorityContract, error) {
	owned := cloneProjectionValue(expected)
	contract := VerifiedAuthorityContract{
		verified: true, subjectDigest: subject, expected: owned,
		verifiedDecisionExpiresAt: expiresAt, verifiedDecisionSecurityEpoch: securityEpoch,
	}
	canonical, digest, err := canonicalVerifiedBinding(contract.authorityBinding())
	if err != nil {
		return VerifiedAuthorityContract{}, projectionFailure(CodeInvalidManifest, "verified-authority", "expected_binding", 0, false, "authority expected projection cannot be bound")
	}
	contract.expectedCanonical = canonical
	contract.expectedDigest = digest
	if err := contract.validateAt(time.Now()); err != nil {
		return VerifiedAuthorityContract{}, err
	}
	return contract, nil
}

func (contract VerifiedAuthorityContract) SubjectDigest() Digest { return contract.subjectDigest }

func (contract VerifiedAuthorityContract) ExpectedProjection(phase AuthorityPhase) (AuthorityProjection, error) {
	if err := contract.validate(); err != nil {
		return AuthorityProjection{}, err
	}
	var projection AuthorityProjection
	switch phase {
	case AuthorityPhaseConnectedSession:
		projection = contract.expected.ConnectedSession
	case AuthorityPhaseMigrationRole:
		projection = contract.expected.MigrationRole
	case AuthorityPhaseMigrationTransaction:
		projection = contract.expected.MigrationTransaction
	default:
		return AuthorityProjection{}, projectionFailure(CodeProjectionMetadataMismatch, "verified-authority", "phase", 0, false, "authority phase is unsupported")
	}
	return cloneProjectionValue(projection), nil
}

// VerifiedCatalogContract binds one exact signed catalog subject to its closed
// scope. No exported constructor accepts loose JSON or caller-owned slices.
type VerifiedCatalogContract struct {
	verified                      bool
	subjectDigest                 Digest
	scope                         ProjectionScope
	expected                      CatalogProjection
	bindingCanonical              string
	bindingDigest                 Digest
	verifiedDecisionExpiresAt     time.Time
	verifiedDecisionSecurityEpoch uint64
}

func (contract VerifiedCatalogContract) validate() error {
	return contract.validateAt(time.Now())
}

func (contract VerifiedCatalogContract) validateAt(now time.Time) error {
	if !contract.verified {
		return projectionFailure(CodeUntrusted, "verified-catalog", "subject", 0, false, "catalog subject was not produced by a trust verifier")
	}
	if err := validateProjectionTrustDecision("verified-catalog", contract.verifiedDecisionExpiresAt, contract.verifiedDecisionSecurityEpoch, now); err != nil {
		return err
	}
	if err := requireDigest("verified-catalog.subject", contract.subjectDigest); err != nil {
		return err
	}
	if err := contract.scope.Validate(); err != nil {
		return projectionFailure(CodeProjectionInvalidScope, "verified-catalog", "scope", 0, false, "catalog scope is invalid")
	}
	if err := contract.expected.Validate(); err != nil {
		return projectionFailure(CodeUntrusted, "verified-catalog", "expected_projection", 0, false, "catalog projection is invalid")
	}
	if contract.scope.SchemaHead == nil || *contract.scope.SchemaHead != contract.expected.SchemaHead {
		return projectionFailure(CodeProjectionMetadataMismatch, "verified-catalog", "schema_head", 0, false, "catalog projection and scope schema heads differ")
	}
	canonical, digest, err := canonicalVerifiedBinding(contract.catalogBinding())
	if err != nil || contract.bindingCanonical == "" || canonical != contract.bindingCanonical || digest != contract.bindingDigest {
		return projectionFailure(CodeUntrusted, "verified-catalog", "catalog_binding", 0, false, "catalog scope or expected projection differs from its verified binding")
	}
	return nil
}

type verifiedCatalogBinding struct {
	SubjectDigest Digest            `json:"subject_digest"`
	Scope         ProjectionScope   `json:"scope"`
	Expected      CatalogProjection `json:"expected"`
	ExpiresAt     string            `json:"expires_at"`
	SecurityEpoch uint64            `json:"security_epoch"`
}

func (contract VerifiedCatalogContract) catalogBinding() verifiedCatalogBinding {
	return verifiedCatalogBinding{
		SubjectDigest: contract.subjectDigest, Scope: contract.scope, Expected: contract.expected,
		ExpiresAt: canonicalProjectionExpiry(contract.verifiedDecisionExpiresAt), SecurityEpoch: contract.verifiedDecisionSecurityEpoch,
	}
}

// bindVerifiedCatalogContract is the only package-private path that promotes
// catalog inputs to an opaque verified wrapper.
func bindVerifiedCatalogContract(subject Digest, scope ProjectionScope, expected CatalogProjection, expiresAt time.Time, securityEpoch uint64) (VerifiedCatalogContract, error) {
	ownedScope := cloneProjectionValue(scope)
	ownedExpected := cloneProjectionValue(expected)
	contract := VerifiedCatalogContract{
		verified: true, subjectDigest: subject, scope: ownedScope, expected: ownedExpected,
		verifiedDecisionExpiresAt: expiresAt, verifiedDecisionSecurityEpoch: securityEpoch,
	}
	canonical, digest, err := canonicalVerifiedBinding(contract.catalogBinding())
	if err != nil {
		return VerifiedCatalogContract{}, projectionFailure(CodeInvalidManifest, "verified-catalog", "catalog_binding", 0, false, "catalog scope and expected projection cannot be bound")
	}
	contract.bindingCanonical = canonical
	contract.bindingDigest = digest
	if err := contract.validateAt(time.Now()); err != nil {
		return VerifiedCatalogContract{}, err
	}
	return contract, nil
}

func (contract VerifiedCatalogContract) SubjectDigest() Digest { return contract.subjectDigest }
func (contract VerifiedCatalogContract) Scope() ProjectionScope {
	return cloneProjectionValue(contract.scope)
}
func (contract VerifiedCatalogContract) ExpectedProjection() CatalogProjection {
	return cloneProjectionValue(contract.expected)
}

// VerifiedSchemaBundleScope is the only authority for projecting an inline
// predecessor before a signed catalog subject exists.
type VerifiedSchemaBundleScope struct {
	verified                      bool
	subjectDigest                 Digest
	scope                         ProjectionScope
	defaultACLOwners              []string
	objectCreatorClosure          []string
	boundPrecondition             CatalogPrecondition
	boundPreconditionCanonical    string
	boundPreconditionDigest       Digest
	boundAcceptedStateDigests     [2]Digest
	bindingCanonical              string
	bindingDigest                 Digest
	verifiedDecisionExpiresAt     time.Time
	verifiedDecisionSecurityEpoch uint64
}

func (scope VerifiedSchemaBundleScope) validate() error {
	return scope.validateAt(time.Now())
}

func (scope VerifiedSchemaBundleScope) validateAt(now time.Time) error {
	if !scope.verified {
		return projectionFailure(CodeUntrusted, "verified-schema-bundle", "subject", 0, false, "schema bundle scope was not produced by a trust verifier")
	}
	if err := validateProjectionTrustDecision("verified-schema-bundle", scope.verifiedDecisionExpiresAt, scope.verifiedDecisionSecurityEpoch, now); err != nil {
		return err
	}
	if err := requireDigest("verified-schema-bundle.subject", scope.subjectDigest); err != nil {
		return err
	}
	if err := scope.scope.Validate(); err != nil {
		return projectionFailure(CodeProjectionInvalidScope, "verified-schema-bundle", "scope", 0, false, "schema bundle scope is invalid")
	}
	if err := validateVerifiedPrincipalClosure("default_acl_owners", scope.defaultACLOwners); err != nil {
		return err
	}
	if err := validateVerifiedPrincipalClosure("object_creator_closure", scope.objectCreatorClosure); err != nil {
		return err
	}
	closure := make(map[string]struct{}, len(scope.objectCreatorClosure))
	for _, principal := range scope.objectCreatorClosure {
		closure[principal] = struct{}{}
	}
	for _, owner := range scope.defaultACLOwners {
		if _, ok := closure[owner]; !ok {
			return projectionFailure(CodeAuthorityDrift, "verified-schema-bundle", "default_acl_owners", 0, false, "default ACL owner is outside the signed object creator closure")
		}
	}
	bound, canonical, digest, stateDigests, err := bindCatalogPrecondition(scope.boundPrecondition, scope.scope)
	if err != nil {
		return err
	}
	if scope.boundPreconditionCanonical == "" || scope.boundPreconditionCanonical != canonical || scope.boundPreconditionDigest != digest || scope.boundAcceptedStateDigests != stateDigests {
		return projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states", 0, false, "bound catalog precondition differs from the verified schema bundle")
	}
	// Ensure the stored value is representable as a defensive clone. This also
	// prevents a verifier from binding an alias with a shape that cannot round-trip.
	if clonedCanonical, cloneErr := canonicalContractKey(bound); cloneErr != nil || clonedCanonical != canonical {
		return projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states", 0, false, "bound catalog precondition is not canonical")
	}
	bindingCanonical, bindingDigest, bindingErr := canonicalVerifiedBinding(scope.schemaBundleBinding())
	if bindingErr != nil || scope.bindingCanonical == "" || bindingCanonical != scope.bindingCanonical || bindingDigest != scope.bindingDigest {
		return projectionFailure(CodeUntrusted, "verified-schema-bundle", "schema_bundle_binding", 0, false, "schema bundle scope differs from its verified binding")
	}
	return nil
}

func (scope VerifiedSchemaBundleScope) SubjectDigest() Digest { return scope.subjectDigest }
func (scope VerifiedSchemaBundleScope) Scope() ProjectionScope {
	return cloneProjectionValue(scope.scope)
}
func (scope VerifiedSchemaBundleScope) DefaultACLOwners() []string {
	return append([]string(nil), scope.defaultACLOwners...)
}
func (scope VerifiedSchemaBundleScope) ObjectCreatorClosure() []string {
	return append([]string(nil), scope.objectCreatorClosure...)
}

// BoundPrecondition returns the exact signed predecessor condition without
// exposing the wrapper's owned slices.
func (scope VerifiedSchemaBundleScope) BoundPrecondition() CatalogPrecondition {
	return cloneProjectionValue(scope.boundPrecondition)
}

// validatePrecondition closes the caller seam used by ProjectPrecondition: the
// caller may repeat the signed condition, but cannot substitute another valid
// accepted-state pair.
func (scope VerifiedSchemaBundleScope) validatePrecondition(condition CatalogPrecondition) error {
	return scope.validatePreconditionAt(condition, time.Now())
}

func (scope VerifiedSchemaBundleScope) validatePreconditionAt(condition CatalogPrecondition, now time.Time) error {
	if err := scope.validateAt(now); err != nil {
		return err
	}
	_, canonical, digest, stateDigests, err := bindCatalogPrecondition(condition, scope.scope)
	if err != nil {
		return err
	}
	if canonical != scope.boundPreconditionCanonical || digest != scope.boundPreconditionDigest || stateDigests != scope.boundAcceptedStateDigests {
		return projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states", 0, false, "caller catalog precondition differs from the verified schema bundle")
	}
	return nil
}

type verifiedSchemaBundleBinding struct {
	SubjectDigest        Digest              `json:"subject_digest"`
	Scope                ProjectionScope     `json:"scope"`
	Precondition         CatalogPrecondition `json:"precondition"`
	DefaultACLOwners     []string            `json:"default_acl_owners"`
	ObjectCreatorClosure []string            `json:"object_creator_closure"`
	ExpiresAt            string              `json:"expires_at"`
	SecurityEpoch        uint64              `json:"security_epoch"`
}

func (scope VerifiedSchemaBundleScope) schemaBundleBinding() verifiedSchemaBundleBinding {
	return verifiedSchemaBundleBinding{
		SubjectDigest: scope.subjectDigest, Scope: scope.scope, Precondition: scope.boundPrecondition,
		DefaultACLOwners: scope.defaultACLOwners, ObjectCreatorClosure: scope.objectCreatorClosure,
		ExpiresAt: canonicalProjectionExpiry(scope.verifiedDecisionExpiresAt), SecurityEpoch: scope.verifiedDecisionSecurityEpoch,
	}
}

// bindVerifiedSchemaBundleScope is the package-private trust-verifier entry
// point. Every caller-owned object and slice is cloned before the canonical
// binding is frozen.
func bindVerifiedSchemaBundleScope(subject Digest, projectionScope ProjectionScope, condition CatalogPrecondition, defaultACLOwners, objectCreatorClosure []string, expiresAt time.Time, securityEpoch uint64) (VerifiedSchemaBundleScope, error) {
	ownedScope := cloneProjectionValue(projectionScope)
	bound, canonical, digest, stateDigests, err := bindCatalogPrecondition(condition, ownedScope)
	if err != nil {
		return VerifiedSchemaBundleScope{}, err
	}
	scope := VerifiedSchemaBundleScope{
		verified: true, subjectDigest: subject, scope: ownedScope,
		defaultACLOwners: append([]string(nil), defaultACLOwners...), objectCreatorClosure: append([]string(nil), objectCreatorClosure...),
		boundPrecondition: bound, boundPreconditionCanonical: canonical, boundPreconditionDigest: digest, boundAcceptedStateDigests: stateDigests,
		verifiedDecisionExpiresAt: expiresAt, verifiedDecisionSecurityEpoch: securityEpoch,
	}
	bindingCanonical, bindingDigest, bindingErr := canonicalVerifiedBinding(scope.schemaBundleBinding())
	if bindingErr != nil {
		return VerifiedSchemaBundleScope{}, projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "schema_bundle_binding", 0, false, "schema bundle scope cannot be bound")
	}
	scope.bindingCanonical = bindingCanonical
	scope.bindingDigest = bindingDigest
	if err := scope.validateAt(time.Now()); err != nil {
		return VerifiedSchemaBundleScope{}, err
	}
	return scope, nil
}

func bindCatalogPrecondition(condition CatalogPrecondition, scope ProjectionScope) (CatalogPrecondition, string, Digest, [2]Digest, error) {
	var stateDigests [2]Digest
	if condition.Artifact != nil || len(condition.AcceptedStates) != len(stateDigests) {
		return CatalogPrecondition{}, "", "", stateDigests, projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states", 0, false, "verified schema bundle requires exactly two inline accepted states")
	}
	bound := cloneProjectionValue(condition)
	if bound.Artifact != nil || len(bound.AcceptedStates) != len(stateDigests) {
		return CatalogPrecondition{}, "", "", stateDigests, projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states", 0, false, "catalog precondition could not be defensively copied")
	}
	absentCount, presentCount := 0, 0
	for index, state := range bound.AcceptedStates {
		if err := state.Validate(); err != nil {
			return CatalogPrecondition{}, "", "", stateDigests, projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states", 0, false, "accepted catalog state is invalid")
		}
		if state.Absent != nil {
			absentCount++
		} else if state.Present != nil {
			presentCount++
		}
		if !equalProjectionScopes(scope, verifiedCatalogStateScope(state)) {
			return CatalogPrecondition{}, "", "", stateDigests, projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states.scope", 0, false, "accepted catalog state scope differs from the verified schema bundle")
		}
		digest, err := state.ComputeDigest()
		if err != nil {
			return CatalogPrecondition{}, "", "", stateDigests, projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states.digest", 0, false, "accepted catalog state digest could not be computed")
		}
		stateDigests[index] = digest
	}
	if absentCount != 1 || presentCount != 1 {
		return CatalogPrecondition{}, "", "", stateDigests, projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states", 0, false, "verified schema bundle requires one absent and one present accepted state")
	}
	canonical, err := canonicalContractKey(bound)
	if err != nil {
		return CatalogPrecondition{}, "", "", stateDigests, projectionFailure(CodeInvalidManifest, "verified-schema-bundle", "accepted_states", 0, false, "catalog precondition could not be canonicalized")
	}
	return bound, canonical, DigestBytes([]byte(canonical)), stateDigests, nil
}

func verifiedCatalogStateScope(state CatalogStateProjection) ProjectionScope {
	if state.Absent != nil {
		return state.Absent.Scope
	}
	if state.Present != nil {
		return state.Present.Scope
	}
	return ProjectionScope{}
}

type principalClosureShapeViolation uint8

const (
	principalClosureValid principalClosureShapeViolation = iota
	principalClosureEmpty
	principalClosureLimit
	principalClosureInvalidIdentity
	principalClosureNonCanonicalOrder
)

func checkPrincipalClosureShape(principals []string) principalClosureShapeViolation {
	if len(principals) == 0 {
		return principalClosureEmpty
	}
	if uint64(len(principals)) > projectionMaxPrincipals {
		return principalClosureLimit
	}
	for index, principal := range principals {
		if principal == "" || !utf8.ValidString(principal) || strings.ContainsRune(principal, '\x00') {
			return principalClosureInvalidIdentity
		}
		if index > 0 && strings.Compare(principals[index-1], principal) >= 0 {
			return principalClosureNonCanonicalOrder
		}
	}
	return principalClosureValid
}

func validateVerifiedPrincipalClosure(path string, principals []string) error {
	switch checkPrincipalClosureShape(principals) {
	case principalClosureValid:
		return nil
	case principalClosureEmpty:
		return projectionFailure(CodeUntrusted, "verified-schema-bundle", path, 0, false, "verified signed principal closure is empty")
	case principalClosureLimit:
		return projectionFailure(CodeProjectionLimitExceeded, "verified-schema-bundle", path, 0, false, "signed principal closure exceeds the fixed limit")
	case principalClosureInvalidIdentity:
		return fail(CodeInvalidManifest, "verified-schema-bundle."+path, "signed principal closure contains an invalid identity", nil)
	case principalClosureNonCanonicalOrder:
		return fail(CodeInvalidManifest, "verified-schema-bundle."+path, "signed principal closure is duplicate or unsorted", nil)
	default:
		return fail(CodeInvalidManifest, "verified-schema-bundle."+path, "signed principal closure shape is unknown", nil)
	}
}

func validateProjectionTrustDecision(phase string, expiresAt time.Time, securityEpoch uint64, now time.Time) error {
	if securityEpoch == 0 || expiresAt.IsZero() || !now.Before(expiresAt) {
		return projectionFailure(CodeUntrusted, phase, "trust_decision", 0, false, "verified trust decision is missing or expired")
	}
	return nil
}

func canonicalProjectionExpiry(expiresAt time.Time) string {
	if expiresAt.IsZero() {
		return ""
	}
	return expiresAt.UTC().Format(time.RFC3339Nano)
}

func canonicalVerifiedBinding(value any) (string, Digest, error) {
	canonical, err := canonicalContractKey(value)
	if err != nil {
		return "", "", err
	}
	return canonical, DigestBytes([]byte(canonical)), nil
}

func cloneProjectionValue[T any](value T) T {
	raw, err := json.Marshal(value)
	if err != nil {
		var zero T
		return zero
	}
	var cloned T
	if err := json.Unmarshal(raw, &cloned); err != nil {
		var zero T
		return zero
	}
	return cloned
}
