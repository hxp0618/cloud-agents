package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	permissionMembershipCreate  = "memberships.create"
	permissionMembershipUpdate  = "memberships.update"
	permissionMembershipDelete  = "memberships.delete"
	permissionRoleBindingBind   = "role-bindings.bind"
	permissionRoleBindingDelete = "role-bindings.delete"

	createMembershipSQL = `SELECT resource_uid, resource_version, resource_state
FROM cloud_agents.create_membership($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	suspendMembershipSQL = `SELECT resource_uid, resource_version, resource_state
FROM cloud_agents.suspend_membership($1, $2, $3, $4, $5, $6)`
	revokeMembershipSQL = `SELECT resource_uid, resource_version, resource_state
FROM cloud_agents.revoke_membership($1, $2, $3, $4, $5, $6)`
	bindRoleSQL = `SELECT resource_uid, resource_version, resource_state
FROM cloud_agents.bind_role($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`
	revokeRoleBindingSQL = `SELECT resource_uid, resource_version, resource_state
FROM cloud_agents.revoke_role_binding($1, $2, $3, $4, $5, $6)`

	readMembershipMutationScopeSQL = `SELECT
    membership.scope_level,
    CASE membership.scope_level
        WHEN 'tenant' THEN membership.scope_tenant_uid
        WHEN 'organization' THEN membership.scope_organization_uid
        WHEN 'project' THEN membership.scope_project_uid
    END
FROM cloud_agents.memberships AS membership
WHERE membership.tenant_id = cloud_agents.require_tenant_id()
    AND membership.membership_uid = $1`
	readRoleBindingMutationScopeSQL = `SELECT
    binding.scope_level,
    CASE binding.scope_level
        WHEN 'tenant' THEN binding.scope_tenant_uid
        WHEN 'organization' THEN binding.scope_organization_uid
        WHEN 'project' THEN binding.scope_project_uid
    END
FROM cloud_agents.role_bindings AS binding
WHERE binding.tenant_id = cloud_agents.require_tenant_id()
    AND binding.role_binding_uid = $1`
)

var (
	ErrNilMutationRunner      = errors.New("postgres RBAC mutation runner is nil")
	ErrMutationInvalidInput   = errors.New("postgres RBAC mutation input is invalid")
	ErrMutationDenied         = errors.New("postgres RBAC mutation is denied")
	ErrMutationTargetNotFound = errors.New("postgres RBAC mutation target is not found")
	ErrMutationConflict       = errors.New("postgres RBAC mutation compare-and-swap conflict")
	ErrMutationAuthority      = errors.New("postgres RBAC mutation database authority is invalid")
	ErrMutationResultDrift    = errors.New("postgres RBAC mutation result differs from the requested transition")
	ErrMutationCommitUnknown  = errors.New("postgres RBAC mutation commit outcome is unknown")
)

// MutationResult is the closed result returned by one durable RBAC mutation.
// It contains no database handles and cannot be used to authorize another
// mutation.
type MutationResult struct {
	TenantID        string
	ResourceUID     string
	ResourceVersion int64
	State           string
}

// CreateMembershipInput is the complete ordinary tenant-scoped membership
// creation contract. Platform scope is deliberately outside this service.
type CreateMembershipInput struct {
	ExpectedTenantRevision int64
	MembershipUID          string
	MembershipName         string
	Subject                authz.SubjectRef
	Scope                  authz.ScopeRef
	ExpiresAt              *time.Time
	AuditFactUID           string
	ReasonCode             string
}

// MembershipTransitionInput is shared by the two closed membership state
// transitions. The public methods select the target state; callers cannot.
type MembershipTransitionInput struct {
	ExpectedTenantRevision  int64
	MembershipUID           string
	ExpectedResourceVersion int64
	AuditFactUID            string
	ReasonCode              string
}

// BindRoleInput is the complete ordinary tenant-scoped role binding contract.
// The platform.admin role is reserved for the bootstrap authority boundary.
type BindRoleInput struct {
	ExpectedTenantRevision int64
	RoleBindingUID         string
	RoleBindingName        string
	Subject                authz.SubjectRef
	RoleName               string
	RoleVersion            int64
	Scope                  authz.ScopeRef
	ExpiresAt              *time.Time
	AuditFactUID           string
	ReasonCode             string
}

// RevokeRoleBindingInput is the only ordinary role-binding state transition.
type RevokeRoleBindingInput struct {
	ExpectedTenantRevision  int64
	RoleBindingUID          string
	ExpectedResourceVersion int64
	AuditFactUID            string
	ReasonCode              string
}

// RBACMutationService owns the five closed membership and role-binding
// mutation methods. It intentionally exposes neither callbacks nor raw SQL.
type RBACMutationService struct {
	runner *TenantTransactionRunner
}

// NewRBACMutationService creates the production mutation service over one
// pgxpool. Every method acquires and settles its own physical connection.
func NewRBACMutationService(pool *pgxpool.Pool) (*RBACMutationService, error) {
	runner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		return nil, err
	}
	return newRBACMutationService(runner)
}

func newRBACMutationService(runner *TenantTransactionRunner) (*RBACMutationService, error) {
	if runner == nil {
		return nil, ErrNilMutationRunner
	}
	return &RBACMutationService{runner: runner}, nil
}

// CreateMembership authorizes memberships.create and calls only the typed
// create_membership database function in the same serializable transaction.
func (service *RBACMutationService) CreateMembership(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input CreateMembershipInput,
) (MutationResult, error) {
	var result MutationResult
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		if err := service.validateCommon(ctx, tenantID, input.ExpectedTenantRevision, input.AuditFactUID, input.ReasonCode); err != nil {
			return err
		}
		if err := input.Subject.Validate(); err != nil || input.Scope.Validate(tenantID) != nil || input.Scope.Level == authz.ScopePlatform ||
			!validMutationIdentifier(input.MembershipUID) || !validMutationIdentifier(input.MembershipName) ||
			!validMutationExpiry(input.ExpiresAt, service.runner.clock()) {
			return fmt.Errorf("%w: membership create", ErrMutationInvalidInput)
		}
		var mutationErr error
		result, mutationErr = service.withKnownScopeMutation(ctx, tenantID, binder, permissionMembershipCreate, input.Scope, func(handle *tenantReadHandle) (MutationResult, error) {
			return createMembershipInTransaction(ctx, handle, tenantID, input)
		})
		return mutationErr
	})
	return settledMutationResult(result, mapVerifiedMutationError(err))
}

// SuspendMembership authorizes memberships.update at the stored target scope
// before invoking the exact active-to-suspended transition.
func (service *RBACMutationService) SuspendMembership(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input MembershipTransitionInput,
) (MutationResult, error) {
	return service.transitionMembership(ctx, tenantID, principal, input, permissionMembershipUpdate, suspendMembershipSQL, authz.MembershipSuspended)
}

// RevokeMembership authorizes memberships.delete at the stored target scope
// before invoking the exact active-or-suspended-to-revoked transition.
func (service *RBACMutationService) RevokeMembership(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input MembershipTransitionInput,
) (MutationResult, error) {
	return service.transitionMembership(ctx, tenantID, principal, input, permissionMembershipDelete, revokeMembershipSQL, authz.MembershipRevoked)
}

func (service *RBACMutationService) transitionMembership(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input MembershipTransitionInput,
	permission string,
	statement string,
	targetState string,
) (MutationResult, error) {
	var result MutationResult
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		if err := service.validateCommon(ctx, tenantID, input.ExpectedTenantRevision, input.AuditFactUID, input.ReasonCode); err != nil {
			return err
		}
		if !validMutationIdentifier(input.MembershipUID) || input.ExpectedResourceVersion < 1 {
			return fmt.Errorf("%w: membership transition", ErrMutationInvalidInput)
		}
		var mutationErr error
		result, mutationErr = service.withStoredScopeMutation(ctx, tenantID, binder, permission, input.MembershipUID, readMembershipMutationScopeSQL, func(handle *tenantReadHandle) (MutationResult, error) {
			return transitionMembershipInTransaction(ctx, handle, tenantID, input, statement, targetState)
		})
		return mutationErr
	})
	return settledMutationResult(result, mapVerifiedMutationError(err))
}

// BindRole authorizes role-bindings.bind and calls only the typed bind_role
// database function in the same serializable transaction.
func (service *RBACMutationService) BindRole(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input BindRoleInput,
) (MutationResult, error) {
	var result MutationResult
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		if err := service.validateCommon(ctx, tenantID, input.ExpectedTenantRevision, input.AuditFactUID, input.ReasonCode); err != nil {
			return err
		}
		if err := input.Subject.Validate(); err != nil || input.Scope.Validate(tenantID) != nil || input.Scope.Level == authz.ScopePlatform ||
			!validMutationIdentifier(input.RoleBindingUID) || !validMutationIdentifier(input.RoleBindingName) ||
			!validMutationIdentifier(input.RoleName) || input.RoleName == "platform.admin" || input.RoleVersion < 1 ||
			!validMutationExpiry(input.ExpiresAt, service.runner.clock()) {
			return fmt.Errorf("%w: role binding create", ErrMutationInvalidInput)
		}
		var mutationErr error
		result, mutationErr = service.withKnownScopeMutation(ctx, tenantID, binder, permissionRoleBindingBind, input.Scope, func(handle *tenantReadHandle) (MutationResult, error) {
			return bindRoleInTransaction(ctx, handle, tenantID, input)
		})
		return mutationErr
	})
	return settledMutationResult(result, mapVerifiedMutationError(err))
}

// RevokeRoleBinding authorizes role-bindings.delete at the stored target scope
// before invoking the exact active-to-revoked transition.
func (service *RBACMutationService) RevokeRoleBinding(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input RevokeRoleBindingInput,
) (MutationResult, error) {
	var result MutationResult
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		if err := service.validateCommon(ctx, tenantID, input.ExpectedTenantRevision, input.AuditFactUID, input.ReasonCode); err != nil {
			return err
		}
		if !validMutationIdentifier(input.RoleBindingUID) || input.ExpectedResourceVersion < 1 {
			return fmt.Errorf("%w: role binding revoke", ErrMutationInvalidInput)
		}
		var mutationErr error
		result, mutationErr = service.withStoredScopeMutation(ctx, tenantID, binder, permissionRoleBindingDelete, input.RoleBindingUID, readRoleBindingMutationScopeSQL, func(handle *tenantReadHandle) (MutationResult, error) {
			return revokeRoleBindingInTransaction(ctx, handle, tenantID, input)
		})
		return mutationErr
	})
	return settledMutationResult(result, mapVerifiedMutationError(err))
}

func (service *RBACMutationService) validateCommon(
	ctx context.Context,
	tenantID string,
	expectedTenantRevision int64,
	auditFactUID string,
	reasonCode string,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if service == nil || service.runner == nil || service.runner.clock == nil {
		return ErrNilMutationRunner
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validMutationIdentifier(tenantID) || expectedTenantRevision < 1 ||
		expectedTenantRevision == math.MaxInt64 || !validMutationIdentifier(auditFactUID) ||
		!validMutationIdentifier(reasonCode) {
		return ErrMutationInvalidInput
	}
	return nil
}

type tenantMutationOperation func(*tenantReadHandle) (MutationResult, error)

func createMembershipInTransaction(
	ctx context.Context,
	handle *tenantReadHandle,
	tenantID string,
	input CreateMembershipInput,
) (MutationResult, error) {
	return scanMutationResult(
		handle.transaction.queryRow(
			ctx,
			createMembershipSQL,
			tenantID,
			input.ExpectedTenantRevision,
			input.MembershipUID,
			input.MembershipName,
			input.Subject.Kind,
			input.Subject.Issuer,
			input.Subject.Subject,
			string(input.Scope.Level),
			input.Scope.ID,
			input.ExpiresAt,
			input.AuditFactUID,
			input.ReasonCode,
		),
		tenantID,
		input.MembershipUID,
		input.ExpectedTenantRevision,
		authz.MembershipActive,
		"create membership",
	)
}

func transitionMembershipInTransaction(
	ctx context.Context,
	handle *tenantReadHandle,
	tenantID string,
	input MembershipTransitionInput,
	statement string,
	targetState string,
) (MutationResult, error) {
	return scanMutationResult(
		handle.transaction.queryRow(
			ctx,
			statement,
			tenantID,
			input.ExpectedTenantRevision,
			input.MembershipUID,
			input.ExpectedResourceVersion,
			input.AuditFactUID,
			input.ReasonCode,
		),
		tenantID,
		input.MembershipUID,
		input.ExpectedTenantRevision,
		targetState,
		"transition membership",
	)
}

func bindRoleInTransaction(
	ctx context.Context,
	handle *tenantReadHandle,
	tenantID string,
	input BindRoleInput,
) (MutationResult, error) {
	return scanMutationResult(
		handle.transaction.queryRow(
			ctx,
			bindRoleSQL,
			tenantID,
			input.ExpectedTenantRevision,
			input.RoleBindingUID,
			input.RoleBindingName,
			input.Subject.Kind,
			input.Subject.Issuer,
			input.Subject.Subject,
			input.RoleName,
			input.RoleVersion,
			string(input.Scope.Level),
			input.Scope.ID,
			input.ExpiresAt,
			input.AuditFactUID,
			input.ReasonCode,
		),
		tenantID,
		input.RoleBindingUID,
		input.ExpectedTenantRevision,
		authz.BindingActive,
		"bind role",
	)
}

func revokeRoleBindingInTransaction(
	ctx context.Context,
	handle *tenantReadHandle,
	tenantID string,
	input RevokeRoleBindingInput,
) (MutationResult, error) {
	return scanMutationResult(
		handle.transaction.queryRow(
			ctx,
			revokeRoleBindingSQL,
			tenantID,
			input.ExpectedTenantRevision,
			input.RoleBindingUID,
			input.ExpectedResourceVersion,
			input.AuditFactUID,
			input.ReasonCode,
		),
		tenantID,
		input.RoleBindingUID,
		input.ExpectedTenantRevision,
		authz.BindingRevoked,
		"revoke role binding",
	)
}

func (service *RBACMutationService) withKnownScopeMutation(
	ctx context.Context,
	tenantID string,
	binder *authz.VerifiedOperationBinder,
	permission string,
	scope authz.ScopeRef,
	operation tenantMutationOperation,
) (MutationResult, error) {
	var result MutationResult
	verified, err := binder.Bind(tenantID, scope, permission)
	if err != nil {
		return MutationResult{}, err
	}
	err = service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		var mutationErr error
		return executeVerifiedRBACOperation(ctx, handle, verified, scope, func() error {
			result, mutationErr = operation(handle)
			return mutationErr
		})
	})
	return settledMutationResult(result, err)
}

func (service *RBACMutationService) withStoredScopeMutation(
	ctx context.Context,
	tenantID string,
	binder *authz.VerifiedOperationBinder,
	permission string,
	resourceUID string,
	scopeQuery string,
	operation tenantMutationOperation,
) (MutationResult, error) {
	var result MutationResult
	err := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		scope, err := readMutationScope(ctx, handle, resourceUID, scopeQuery)
		if err != nil {
			return err
		}
		verified, err := binder.Bind(tenantID, scope, permission)
		if err != nil {
			return err
		}
		return executeVerifiedRBACOperation(ctx, handle, verified, scope, func() error {
			result, err = operation(handle)
			return err
		})
	})
	return settledMutationResult(result, err)
}

func settledMutationResult(result MutationResult, err error) (MutationResult, error) {
	if err != nil {
		// A row returned by a SECURITY DEFINER function is not durability
		// evidence. Commit may still fail or its response may be lost, so no
		// result escapes unless the transaction outcome is confirmed.
		return MutationResult{}, err
	}
	return result, nil
}

func mapVerifiedMutationError(err error) error {
	if errors.Is(err, authz.ErrOperationDenied) {
		return ErrMutationDenied
	}
	return err
}

func readMutationScope(
	ctx context.Context,
	handle *tenantReadHandle,
	resourceUID string,
	statement string,
) (authz.ScopeRef, error) {
	var level string
	var scopeUID *string
	err := handle.transaction.queryRow(ctx, statement, resourceUID).Scan(&level, &scopeUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ScopeRef{}, ErrMutationTargetNotFound
	}
	if err != nil {
		return authz.ScopeRef{}, fmt.Errorf("read RBAC mutation target scope: %w", err)
	}
	if scopeUID == nil {
		return authz.ScopeRef{}, ErrMutationResultDrift
	}
	scope := authz.ScopeRef{Level: authz.ScopeLevel(level), ID: *scopeUID}
	if scope.Level == authz.ScopePlatform || scope.Validate(handle.tenantID) != nil {
		return authz.ScopeRef{}, ErrMutationResultDrift
	}
	return scope, nil
}

func scanMutationResult(
	row rowScanner,
	tenantID string,
	expectedUID string,
	expectedTenantRevision int64,
	expectedState string,
	operation string,
) (MutationResult, error) {
	result := MutationResult{TenantID: tenantID}
	if err := row.Scan(&result.ResourceUID, &result.ResourceVersion, &result.State); err != nil {
		return MutationResult{}, mapMutationDatabaseError(operation, err)
	}
	if result.ResourceUID != expectedUID || result.ResourceVersion != expectedTenantRevision+1 || result.State != expectedState {
		return MutationResult{}, ErrMutationResultDrift
	}
	return result, nil
}

func mapMutationDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch postgresError.Code {
	case "22023", "22003", "23502", "23503", "23514":
		return fmt.Errorf("%w: %s", ErrMutationInvalidInput, operation)
	case "23505", "40001":
		return fmt.Errorf("%w: %s", ErrMutationConflict, operation)
	case "42501":
		return fmt.Errorf("%w: %s", ErrMutationAuthority, operation)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func validMutationExpiry(expiresAt *time.Time, now time.Time) bool {
	return expiresAt == nil || !expiresAt.IsZero() && expiresAt.After(now)
}

func validMutationIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		alphaNumeric := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if index == 0 || index == len(value)-1 {
			if !alphaNumeric {
				return false
			}
			continue
		}
		if !alphaNumeric && !strings.ContainsRune("._~-", rune(character)) {
			return false
		}
	}
	return true
}

type tenantMutationCallback func(*tenantReadHandle) error

func (runner *TenantTransactionRunner) withTenantMutation(
	ctx context.Context,
	tenantID string,
	callback tenantMutationCallback,
) error {
	return runner.withTenantMutationBinder(ctx, tenantID, callback, bindTenant)
}

func (runner *TenantTransactionRunner) withTenantMutationBinder(
	ctx context.Context,
	tenantID string,
	callback tenantMutationCallback,
	binder tenantBinder,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if callback == nil || binder == nil {
		return ErrNilCallback
	}

	connection, err := runner.pool.acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire tenant mutation connection: %w", err)
	}
	settled := false
	defer func() {
		if settled {
			return
		}
		cleanupContext, cancel := runner.cleanupContext()
		defer cancel()
		_ = connection.hijackAndClose(cleanupContext)
	}()

	transaction, err := connection.beginTx(ctx, pgx.TxOptions{
		IsoLevel:       pgx.Serializable,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	})
	if err != nil {
		runner.discard(connection, &settled)
		return fmt.Errorf("begin tenant mutation transaction: %w", err)
	}
	if err := binder(ctx, transaction, tenantID); err != nil {
		runner.rollbackAndSettle(connection, transaction, &settled)
		return err
	}

	handle := &tenantReadHandle{
		active: true, transaction: transaction, tenantID: tenantID, clock: runner.clock,
	}
	callbackErr, panicValue, panicked := invokeTenantMutationCallback(callback, handle)
	handle.invalidate()
	if panicked {
		runner.rollbackAndSettle(connection, transaction, &settled)
		panic(panicValue)
	}
	if callbackErr != nil {
		runner.rollbackAndSettle(connection, transaction, &settled)
		return callbackErr
	}
	if err := transaction.commit(ctx); err != nil {
		runner.discard(connection, &settled)
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			return mapMutationDatabaseError("commit tenant mutation transaction", err)
		}
		return errors.Join(ErrMutationCommitUnknown, fmt.Errorf("commit tenant mutation transaction: %w", err))
	}
	if err := runner.releaseIfReusable(connection, &settled); err != nil {
		// Commit has already returned a confirmed success. releaseIfReusable
		// discards every connection whose idle status or cleared tenant GUC is
		// not proven; surfacing its cleanup error as a mutation failure would
		// invite an unsafe retry of the already durable operation.
		return nil
	}
	return nil
}

// withGlobalMutation is the same one-shot SERIALIZABLE settlement boundary as
// withTenantMutation, but deliberately omits tenant GUC binding. It is used
// only by the global leader-lease functions; callers never receive the handle.
func (runner *TenantTransactionRunner) withGlobalMutation(
	ctx context.Context,
	callback tenantMutationCallback,
) error {
	if ctx == nil {
		return ErrNilContext
	}
	if callback == nil {
		return ErrNilCallback
	}
	connection, err := runner.pool.acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire global mutation connection: %w", err)
	}
	settled := false
	defer func() {
		if !settled {
			cleanupContext, cancel := runner.cleanupContext()
			defer cancel()
			_ = connection.hijackAndClose(cleanupContext)
		}
	}()
	transaction, err := connection.beginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite, DeferrableMode: pgx.NotDeferrable,
	})
	if err != nil {
		runner.discard(connection, &settled)
		return fmt.Errorf("begin global mutation transaction: %w", err)
	}
	handle := &tenantReadHandle{active: true, transaction: transaction, clock: runner.clock}
	callbackErr, panicValue, panicked := invokeTenantMutationCallback(callback, handle)
	handle.invalidate()
	if panicked {
		runner.rollbackAndSettle(connection, transaction, &settled)
		panic(panicValue)
	}
	if callbackErr != nil {
		runner.rollbackAndSettle(connection, transaction, &settled)
		return callbackErr
	}
	if err := transaction.commit(ctx); err != nil {
		runner.discard(connection, &settled)
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			return mapMutationDatabaseError("commit global mutation transaction", err)
		}
		return errors.Join(ErrMutationCommitUnknown, fmt.Errorf("commit global mutation transaction: %w", err))
	}
	if err := runner.releaseIfReusable(connection, &settled); err != nil {
		return nil
	}
	return nil
}

func invokeTenantMutationCallback(
	callback tenantMutationCallback,
	handle *tenantReadHandle,
) (callbackErr error, panicValue any, panicked bool) {
	completed := false
	func() {
		defer func() {
			if !completed {
				panicValue = recover()
				panicked = true
			}
		}()
		callbackErr = callback(handle)
		completed = true
	}()
	return callbackErr, panicValue, panicked
}
