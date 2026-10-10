package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	serviceAccountCredentialLifetime       = 365*24*time.Hour - time.Minute
	serviceAccountPageLimit                = 200
	serviceAccountRoleVersion        int64 = 1

	readServiceAccountTenantRevisionSQL = `SELECT cloud_agents_identity.lock_service_account_tenant_revision($1)`
	createServiceAccountRecordSQL       = `SELECT service_account_id, credential_version,
    subject_kind, subject_issuer, subject_value, created_at, updated_at
FROM cloud_agents_identity.create_service_account_record(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17
)`
	lockServiceAccountManagementSQL = `SELECT service_account_id, display_name, application,
    management_scope_level, management_scope_id, membership_uid, role_binding_uid,
    role_name, role_version, state, resource_version,
    subject_kind, subject_issuer, subject_value
FROM cloud_agents_identity.lock_service_account_management($1,$2)`
	rotateServiceAccountCredentialSQL = `SELECT resource_version, credential_version
FROM cloud_agents_identity.rotate_service_account_credential(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10
)`
	disableServiceAccountRecordSQL = `SELECT cloud_agents_identity.disable_service_account_record(
    $1,$2,$3,$4,$5,$6,$7,$8
)`
	recordServiceAccountDenialSQL = `SELECT cloud_agents_identity.record_service_account_management_denial(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10
)`
	listServiceAccountRecordsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(
    pg_catalog.jsonb_build_object(
        'id', account.service_account_id,
        'display_name', account.display_name,
        'application', account.application,
        'scope', pg_catalog.jsonb_build_object('Level', account.management_scope_level, 'ID', account.management_scope_id),
        'membership_uid', account.membership_uid,
        'role_binding_uid', account.role_binding_uid,
        'role_name', account.role_name,
        'role_version', account.role_version,
        'state', account.state,
        'resource_version', account.resource_version,
        'subject', pg_catalog.jsonb_build_object('Kind', account.subject_kind, 'Issuer', account.subject_issuer, 'Subject', account.subject_value),
        'created_at', account.created_at,
        'updated_at', account.updated_at
    ) ORDER BY account.service_account_id
), '[]'::jsonb)
FROM cloud_agents_identity.list_service_account_records($1,$2,$3) AS account`
)

type serviceAccountProofGenerator func() (string, [32]byte, error)
type serviceAccountIDGenerator func() (string, error)

var ErrServiceAccountAuditUnavailable = errors.New("postgres service account audit is unavailable")

// ServiceAccountStore owns the Admin-only service-account management
// transaction. Raw credentials never enter audit inputs and escape only after
// the transaction commit is confirmed.
type ServiceAccountStore struct {
	runner        *TenantTransactionRunner
	newProof      serviceAccountProofGenerator
	newIdentifier serviceAccountIDGenerator
}

type serviceAccountManagement struct {
	ID              string                  `json:"id"`
	DisplayName     string                  `json:"display_name"`
	Application     api.IdentityApplication `json:"application"`
	Scope           authz.ScopeRef          `json:"scope"`
	MembershipUID   string                  `json:"membership_uid"`
	RoleBindingUID  string                  `json:"role_binding_uid"`
	RoleName        string                  `json:"role_name"`
	RoleVersion     int64                   `json:"role_version"`
	State           string                  `json:"state"`
	ResourceVersion int64                   `json:"resource_version"`
	Subject         authz.SubjectRef        `json:"subject"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

const serviceAccountDenialAuditTimeout = 5 * time.Second

func NewServiceAccountStore(pool *pgxpool.Pool) (*ServiceAccountStore, error) {
	runner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		return nil, err
	}
	runner.application = "admin"
	return newServiceAccountStore(runner)
}

func newServiceAccountStore(runner *TenantTransactionRunner) (*ServiceAccountStore, error) {
	if runner == nil || runner.application != "admin" || runner.clock == nil {
		return nil, ErrMutationAuthority
	}
	return &ServiceAccountStore{
		runner: runner, newProof: browserauth.NewProof, newIdentifier: newServiceAccountIdentifier,
	}, nil
}

func (store *ServiceAccountStore) Create(
	ctx context.Context,
	tenantID string,
	membershipPrincipal *authn.VerifiedPrincipal,
	bindingPrincipal *authn.VerifiedPrincipal,
	request api.ServiceAccountCreateRequest,
	correlationID string,
) (api.ServiceAccountCreated, error) {
	if err := store.validateCreate(ctx, tenantID, request, correlationID); err != nil {
		return api.ServiceAccountCreated{}, err
	}
	rawCredential, credentialDigest, err := store.newProof()
	if err != nil {
		return api.ServiceAccountCreated{}, fmt.Errorf("generate service account credential: %w", err)
	}
	identifiers, err := store.newIdentifiers(5)
	if err != nil {
		return api.ServiceAccountCreated{}, err
	}
	credentialExpiresAt := store.runner.clock().UTC().Add(serviceAccountCredentialLifetime)
	scope := authz.ScopeRef{Level: authz.ScopeLevel(request.ScopeLevel), ID: request.ScopeID}

	var created api.ServiceAccountCreated
	var denialActor authz.SubjectRef
	err = authz.WithVerifiedOperation(membershipPrincipal, func(membershipBinder *authz.VerifiedOperationBinder) error {
		membershipOperation, err := membershipBinder.Bind(tenantID, scope, permissionMembershipCreate)
		if err != nil {
			return err
		}
		if actor, ok := membershipOperation.Actor(); ok {
			denialActor = actor
		}
		return authz.WithVerifiedOperation(bindingPrincipal, func(bindingBinder *authz.VerifiedOperationBinder) error {
			bindingOperation, err := bindingBinder.Bind(tenantID, scope, permissionRoleBindingBind)
			if err != nil {
				return err
			}
			return store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
				return executeServiceAccountCreate(ctx, handle, membershipOperation, bindingOperation, scope, func(actor authz.SubjectRef) error {
					if !validServiceAccountManagementActor(actor) {
						return authz.ErrOperationDenied
					}
					result, err := store.createInTransaction(ctx, handle, actor, request, scope, identifiers, credentialDigest, credentialExpiresAt, correlationID)
					if err != nil {
						return err
					}
					created = result
					return nil
				})
			})
		})
	})
	if err != nil {
		if auditErr := store.recordManagementDenial(ctx, tenantID, request.ServiceAccountID, "create", request.Application, denialActor, correlationID, err); auditErr != nil {
			return api.ServiceAccountCreated{}, auditErr
		}
		return api.ServiceAccountCreated{}, mapServiceAccountError(err)
	}
	created.Credential = rawCredential
	return created, nil
}

func (store *ServiceAccountStore) createInTransaction(
	ctx context.Context,
	handle *tenantReadHandle,
	actor authz.SubjectRef,
	request api.ServiceAccountCreateRequest,
	scope authz.ScopeRef,
	identifiers []string,
	credentialDigest [32]byte,
	credentialExpiresAt time.Time,
	correlationID string,
) (api.ServiceAccountCreated, error) {
	var tenantRevision int64
	if err := handle.transaction.queryRow(ctx, readServiceAccountTenantRevisionSQL, handle.tenantID).Scan(&tenantRevision); err != nil {
		return api.ServiceAccountCreated{}, mapServiceAccountDatabaseError("read tenant revision", err)
	}
	if tenantRevision < 1 || tenantRevision >= math.MaxInt64-1 {
		return api.ServiceAccountCreated{}, ErrMutationResultDrift
	}
	subject := authz.SubjectRef{Kind: "serviceAccount", Issuer: actor.Issuer, Subject: "service-" + request.ServiceAccountID}
	if err := subject.Validate(); err != nil {
		return api.ServiceAccountCreated{}, ErrMutationInvalidInput
	}
	membershipInput := CreateMembershipInput{
		CorrelationID: correlationID, ExpectedTenantRevision: tenantRevision,
		MembershipUID: identifiers[0], MembershipName: identifiers[0], Subject: subject,
		Scope: scope, AuditFactUID: identifiers[2], ReasonCode: "service-account-create",
	}
	if _, err := createMembershipInTransaction(ctx, handle, handle.tenantID, actor, membershipInput); err != nil {
		return api.ServiceAccountCreated{}, err
	}
	bindingInput := BindRoleInput{
		CorrelationID: correlationID, ExpectedTenantRevision: tenantRevision + 1,
		RoleBindingUID: identifiers[1], RoleBindingName: identifiers[1], Subject: subject,
		RoleName: request.RoleName, RoleVersion: serviceAccountRoleVersion, Scope: scope,
		AuditFactUID: identifiers[3], ReasonCode: "service-account-create",
	}
	if _, err := bindRoleInTransaction(ctx, handle, handle.tenantID, actor, bindingInput); err != nil {
		return api.ServiceAccountCreated{}, err
	}

	var returnedID, returnedKind, returnedIssuer, returnedSubject string
	var credentialVersion int64
	var createdAt, updatedAt time.Time
	err := handle.transaction.queryRow(ctx, createServiceAccountRecordSQL,
		handle.tenantID, request.ServiceAccountID, request.DisplayName, string(request.Application),
		string(scope.Level), scope.ID, identifiers[0], identifiers[1], request.RoleName,
		serviceAccountRoleVersion, credentialDigest[:], credentialExpiresAt,
		actor.Kind, actor.Issuer, actor.Subject, identifiers[4], correlationID,
	).Scan(&returnedID, &credentialVersion, &returnedKind, &returnedIssuer, &returnedSubject, &createdAt, &updatedAt)
	if err != nil {
		return api.ServiceAccountCreated{}, mapServiceAccountDatabaseError("create service account", err)
	}
	if returnedID != request.ServiceAccountID || credentialVersion != 1 ||
		returnedKind != subject.Kind || returnedIssuer != subject.Issuer || returnedSubject != subject.Subject ||
		createdAt.IsZero() || !createdAt.Equal(updatedAt) {
		return api.ServiceAccountCreated{}, ErrMutationResultDrift
	}
	return api.ServiceAccountCreated{
		ServiceAccount: api.ServiceAccount{
			ID: returnedID, TenantID: handle.tenantID, DisplayName: request.DisplayName,
			Application: request.Application, ScopeLevel: string(scope.Level), ScopeID: scope.ID,
			RoleName: request.RoleName, State: "active", ResourceVersion: "1",
			Subject:   common.SubjectRef{Kind: returnedKind, Issuer: returnedIssuer, Subject: returnedSubject},
			CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), UpdatedAt: updatedAt.UTC().Format(time.RFC3339Nano),
		},
		CredentialExpiresAt: credentialExpiresAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// ManagementScope returns only the persisted scope used to construct the
// exact offline verification request. Mutations lock and re-read it in their
// own transaction before spending authority.
func (store *ServiceAccountStore) ManagementScope(ctx context.Context, tenantID, serviceAccountID string) (authz.ScopeRef, error) {
	if err := store.validateCommon(ctx, tenantID, serviceAccountID, "request-scope"); err != nil {
		return authz.ScopeRef{}, err
	}
	var scope authz.ScopeRef
	err := store.runner.withTenantReadCommittedMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		record, err := readServiceAccountManagement(ctx, handle, serviceAccountID)
		if err != nil {
			return err
		}
		scope = record.Scope
		return nil
	})
	if err != nil {
		return authz.ScopeRef{}, mapServiceAccountError(err)
	}
	return scope, nil
}

func (store *ServiceAccountStore) Rotate(
	ctx context.Context,
	tenantID, serviceAccountID string,
	principal *authn.VerifiedPrincipal,
	expectedResourceVersion int64,
	correlationID string,
) (api.ServiceAccountRotated, error) {
	if err := store.validateVersioned(ctx, tenantID, serviceAccountID, expectedResourceVersion, correlationID); err != nil {
		return api.ServiceAccountRotated{}, err
	}
	rawCredential, credentialDigest, err := store.newProof()
	if err != nil {
		return api.ServiceAccountRotated{}, fmt.Errorf("generate service account credential: %w", err)
	}
	eventID, err := store.newIdentifier()
	if err != nil {
		return api.ServiceAccountRotated{}, fmt.Errorf("generate service account audit identifier: %w", err)
	}
	credentialExpiresAt := store.runner.clock().UTC().Add(serviceAccountCredentialLifetime)
	var result api.ServiceAccountRotated
	var denialActor authz.SubjectRef
	var denialApplication api.IdentityApplication
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		return store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			record, err := readServiceAccountManagement(ctx, handle, serviceAccountID)
			if err != nil {
				return err
			}
			denialApplication = record.Application
			operation, err := binder.Bind(tenantID, record.Scope, permissionMembershipUpdate)
			if err != nil {
				return err
			}
			actor, ok := operation.Actor()
			if !ok {
				return authz.ErrOperationDenied
			}
			denialActor = actor
			if !validServiceAccountManagementActor(actor) {
				return authz.ErrOperationDenied
			}
			return executeVerifiedRBACOperation(ctx, handle, operation, record.Scope, func() error {
				var resourceVersion, credentialVersion int64
				err := handle.transaction.queryRow(ctx, rotateServiceAccountCredentialSQL,
					tenantID, serviceAccountID, expectedResourceVersion, credentialDigest[:], credentialExpiresAt,
					actor.Kind, actor.Issuer, actor.Subject, eventID, correlationID,
				).Scan(&resourceVersion, &credentialVersion)
				if err != nil {
					return mapServiceAccountDatabaseError("rotate service account credential", err)
				}
				if resourceVersion != expectedResourceVersion+1 || credentialVersion < 2 {
					return ErrMutationResultDrift
				}
				result = api.ServiceAccountRotated{
					ResourceVersion:     strconv.FormatInt(resourceVersion, 10),
					CredentialVersion:   strconv.FormatInt(credentialVersion, 10),
					CredentialExpiresAt: credentialExpiresAt.UTC().Format(time.RFC3339Nano),
				}
				return nil
			})
		})
	})
	if err != nil {
		if auditErr := store.recordManagementDenial(ctx, tenantID, serviceAccountID, "rotate", denialApplication, denialActor, correlationID, err); auditErr != nil {
			return api.ServiceAccountRotated{}, auditErr
		}
		return api.ServiceAccountRotated{}, mapServiceAccountError(err)
	}
	result.Credential = rawCredential
	return result, nil
}

func (store *ServiceAccountStore) Disable(
	ctx context.Context,
	tenantID, serviceAccountID string,
	principal *authn.VerifiedPrincipal,
	expectedResourceVersion int64,
	correlationID string,
) (int64, error) {
	if err := store.validateVersioned(ctx, tenantID, serviceAccountID, expectedResourceVersion, correlationID); err != nil {
		return 0, err
	}
	eventID, err := store.newIdentifier()
	if err != nil {
		return 0, fmt.Errorf("generate service account audit identifier: %w", err)
	}
	var resourceVersion int64
	var denialActor authz.SubjectRef
	var denialApplication api.IdentityApplication
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		return store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			record, err := readServiceAccountManagement(ctx, handle, serviceAccountID)
			if err != nil {
				return err
			}
			denialApplication = record.Application
			operation, err := binder.Bind(tenantID, record.Scope, permissionMembershipDelete)
			if err != nil {
				return err
			}
			actor, ok := operation.Actor()
			if !ok {
				return authz.ErrOperationDenied
			}
			denialActor = actor
			if !validServiceAccountManagementActor(actor) {
				return authz.ErrOperationDenied
			}
			return executeVerifiedRBACOperation(ctx, handle, operation, record.Scope, func() error {
				err := handle.transaction.queryRow(ctx, disableServiceAccountRecordSQL,
					tenantID, serviceAccountID, expectedResourceVersion,
					actor.Kind, actor.Issuer, actor.Subject, eventID, correlationID,
				).Scan(&resourceVersion)
				if err != nil {
					return mapServiceAccountDatabaseError("disable service account", err)
				}
				if resourceVersion != expectedResourceVersion+1 {
					return ErrMutationResultDrift
				}
				return nil
			})
		})
	})
	if err != nil {
		if auditErr := store.recordManagementDenial(ctx, tenantID, serviceAccountID, "disable", denialApplication, denialActor, correlationID, err); auditErr != nil {
			return 0, auditErr
		}
		return 0, mapServiceAccountError(err)
	}
	return resourceVersion, nil
}

func (store *ServiceAccountStore) recordManagementDenial(
	ctx context.Context,
	tenantID, serviceAccountID, action string,
	application api.IdentityApplication,
	actor authz.SubjectRef,
	correlationID string,
	operationErr error,
) error {
	reason, record := serviceAccountDenialReason(operationErr)
	if !record || actor.Validate() != nil || actor.Kind != "user" && actor.Kind != "serviceAccount" ||
		action == "create" && !validServiceAccountApplication(application) ||
		action != "create" && application != "" && !validServiceAccountApplication(application) {
		return nil
	}
	eventID, err := store.newIdentifier()
	if err != nil || !validMutationIdentifier(eventID) {
		return ErrServiceAccountAuditUnavailable
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), serviceAccountDenialAuditTimeout)
	defer cancel()
	var recordedID string
	err = store.runner.withTenantReadCommittedMutation(auditCtx, tenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(auditCtx, recordServiceAccountDenialSQL,
			tenantID, serviceAccountID, action, reason, string(application),
			actor.Kind, actor.Issuer, actor.Subject, eventID, correlationID,
		).Scan(&recordedID)
	})
	if err != nil || recordedID != eventID {
		return ErrServiceAccountAuditUnavailable
	}
	return nil
}

// RecordPermissionDenial consumes a separately verified, read-only identity
// proof after the requested management permission was rejected. It can append
// denial evidence only; it never manufactures or spends mutation authority.
func (store *ServiceAccountStore) RecordPermissionDenial(
	ctx context.Context,
	tenantID, serviceAccountID, action string,
	application api.IdentityApplication,
	scope authz.ScopeRef,
	principal *authn.VerifiedPrincipal,
	correlationID string,
) error {
	if err := store.validateCommon(ctx, tenantID, serviceAccountID, correlationID); err != nil {
		return err
	}
	identityPermission, ok := serviceAccountIdentityPermission(scope)
	if !ok || action != "create" && action != "rotate" && action != "disable" ||
		action == "create" && !validServiceAccountApplication(application) ||
		action != "create" && application != "" {
		return ErrMutationInvalidInput
	}
	return authn.ConsumeVerifiedPrincipal(principal, func(view authn.VerifiedPrincipalView) error {
		verifiedTenant, level, resourceID, permission, contextOK := view.AuthorizationContext()
		kind, issuer, subject, actorOK := view.Actor()
		if !contextOK || !actorOK || !view.Check() || verifiedTenant != tenantID ||
			level != string(scope.Level) || resourceID != scope.ID || permission != identityPermission {
			return authz.ErrOperationDenied
		}
		actor := authz.SubjectRef{Kind: kind, Issuer: issuer, Subject: subject}
		if actor.Validate() != nil || actor.Kind != "user" && actor.Kind != "serviceAccount" {
			return authz.ErrOperationDenied
		}
		return store.recordManagementDenial(ctx, tenantID, serviceAccountID, action, application, actor, correlationID, ErrMutationDenied)
	})
}

func serviceAccountIdentityPermission(scope authz.ScopeRef) (string, bool) {
	switch scope.Level {
	case authz.ScopeTenant:
		return "tenants.get", true
	case authz.ScopeOrganization:
		return "organizations.get", true
	case authz.ScopeProject:
		return "projects.get", true
	default:
		return "", false
	}
}

func serviceAccountDenialReason(err error) (string, bool) {
	switch {
	case errors.Is(err, authz.ErrOperationDenied), errors.Is(err, ErrMutationDenied):
		return "authorization_denied", true
	case errors.Is(err, ErrMutationConflict):
		return "conflict", true
	default:
		return "", false
	}
}

func (store *ServiceAccountStore) List(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	after string,
	limit int,
) (api.ServiceAccountPage, error) {
	if ctx == nil {
		return api.ServiceAccountPage{}, ErrNilContext
	}
	if store == nil || store.runner == nil || store.runner.application != "admin" ||
		!validMutationIdentifier(tenantID) || after != "" && !validMutationIdentifier(after) || limit < 1 || limit > serviceAccountPageLimit {
		return api.ServiceAccountPage{}, ErrMutationInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return api.ServiceAccountPage{}, err
	}
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
	page := api.ServiceAccountPage{ServiceAccounts: []api.ServiceAccount{}}
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, err := binder.Bind(tenantID, scope, "memberships.list")
		if err != nil {
			return err
		}
		return store.runner.withTenantReadBinder(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok || handle == nil {
				return ErrMutationAuthority
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listServiceAccountRecordsSQL, tenantID, after, limit+1).Scan(&raw); err != nil {
					return mapServiceAccountDatabaseError("list service accounts", err)
				}
				var records []serviceAccountManagement
				if json.Unmarshal(raw, &records) != nil || records == nil || len(records) > limit+1 {
					return ErrMutationResultDrift
				}
				for index, record := range records {
					if validateServiceAccountManagement(record, tenantID, true) != nil || index > 0 && records[index-1].ID >= record.ID {
						return ErrMutationResultDrift
					}
				}
				if len(records) > limit {
					records = records[:limit]
					page.NextPageToken = records[len(records)-1].ID
				}
				for _, record := range records {
					page.ServiceAccounts = append(page.ServiceAccounts, serviceAccountResource(record, tenantID))
				}
				return nil
			})
		}, bindTenant)
	})
	if err != nil {
		return api.ServiceAccountPage{}, mapServiceAccountError(err)
	}
	return page, nil
}

func readServiceAccountManagement(ctx context.Context, handle *tenantReadHandle, serviceAccountID string) (serviceAccountManagement, error) {
	if handle == nil || handle.transaction == nil {
		return serviceAccountManagement{}, ErrMutationAuthority
	}
	record, err := scanServiceAccountManagement(handle.transaction.queryRow(ctx, lockServiceAccountManagementSQL, handle.tenantID, serviceAccountID), handle.tenantID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return serviceAccountManagement{}, ErrMutationTargetNotFound
	}
	return record, err
}

func scanServiceAccountManagement(row rowScanner, tenantID string, withTimes bool) (serviceAccountManagement, error) {
	var record serviceAccountManagement
	var application, scopeLevel string
	var kind, issuer, subject string
	values := []any{
		&record.ID, &record.DisplayName, &application, &scopeLevel, &record.Scope.ID,
		&record.MembershipUID, &record.RoleBindingUID, &record.RoleName, &record.RoleVersion,
		&record.State, &record.ResourceVersion, &kind, &issuer, &subject,
	}
	if withTimes {
		values = append(values, &record.CreatedAt, &record.UpdatedAt)
	}
	if err := row.Scan(values...); err != nil {
		return serviceAccountManagement{}, err
	}
	record.Application = api.IdentityApplication(application)
	record.Scope.Level = authz.ScopeLevel(scopeLevel)
	record.Subject = authz.SubjectRef{Kind: kind, Issuer: issuer, Subject: subject}
	if err := validateServiceAccountManagement(record, tenantID, withTimes); err != nil {
		return serviceAccountManagement{}, err
	}
	return record, nil
}

func validateServiceAccountManagement(record serviceAccountManagement, tenantID string, withTimes bool) error {
	if !validMutationIdentifier(record.ID) || !validServiceAccountDisplayName(record.DisplayName) ||
		!validServiceAccountApplication(record.Application) || record.Scope.Validate(tenantID) != nil || record.Scope.Level == authz.ScopePlatform ||
		!validMutationIdentifier(record.MembershipUID) || !validMutationIdentifier(record.RoleBindingUID) ||
		!validMutationIdentifier(record.RoleName) || record.RoleName == "platform.admin" || record.RoleVersion != serviceAccountRoleVersion ||
		(record.State != "active" && record.State != "disabled") || record.ResourceVersion < 1 ||
		record.Subject.Validate() != nil || record.Subject.Kind != "serviceAccount" || record.Subject.Subject != "service-"+record.ID ||
		withTimes && (record.CreatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt)) {
		return ErrMutationResultDrift
	}
	return nil
}

func serviceAccountResource(record serviceAccountManagement, tenantID string) api.ServiceAccount {
	return api.ServiceAccount{
		ID: record.ID, TenantID: tenantID, DisplayName: record.DisplayName,
		Application: record.Application, ScopeLevel: string(record.Scope.Level), ScopeID: record.Scope.ID,
		RoleName: record.RoleName, State: record.State, ResourceVersion: strconv.FormatInt(record.ResourceVersion, 10),
		Subject:   common.SubjectRef{Kind: record.Subject.Kind, Issuer: record.Subject.Issuer, Subject: record.Subject.Subject},
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (store *ServiceAccountStore) validateCreate(ctx context.Context, tenantID string, request api.ServiceAccountCreateRequest, correlationID string) error {
	if err := store.validateCommon(ctx, tenantID, request.ServiceAccountID, correlationID); err != nil {
		return err
	}
	scope := authz.ScopeRef{Level: authz.ScopeLevel(request.ScopeLevel), ID: request.ScopeID}
	if !validServiceAccountDisplayName(request.DisplayName) || !validServiceAccountApplication(request.Application) ||
		!validMutationIdentifier(request.RoleName) || request.RoleName == "platform.admin" ||
		scope.Level == authz.ScopePlatform || scope.Validate(tenantID) != nil {
		return ErrMutationInvalidInput
	}
	return nil
}

func (store *ServiceAccountStore) validateVersioned(ctx context.Context, tenantID, serviceAccountID string, version int64, correlationID string) error {
	if err := store.validateCommon(ctx, tenantID, serviceAccountID, correlationID); err != nil {
		return err
	}
	if version < 1 || version == math.MaxInt64 {
		return ErrMutationInvalidInput
	}
	return nil
}

func (store *ServiceAccountStore) validateCommon(ctx context.Context, tenantID, serviceAccountID, correlationID string) error {
	if ctx == nil {
		return ErrNilContext
	}
	if store == nil || store.runner == nil || store.runner.application != "admin" || store.runner.clock == nil || store.newProof == nil || store.newIdentifier == nil {
		return ErrMutationAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validMutationIdentifier(tenantID) || !validMutationIdentifier(serviceAccountID) || !validMutationIdentifier(correlationID) {
		return ErrMutationInvalidInput
	}
	return nil
}

func validServiceAccountDisplayName(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= 1 && utf8.RuneCountInString(value) <= 160
}

func validServiceAccountApplication(value api.IdentityApplication) bool {
	return value == api.IdentityApplicationAdmin || value == api.IdentityApplicationUser
}

func validServiceAccountManagementActor(actor authz.SubjectRef) bool {
	return actor.Validate() == nil && actor.Kind == "user"
}

func (store *ServiceAccountStore) newIdentifiers(count int) ([]string, error) {
	identifiers := make([]string, count)
	seen := make(map[string]struct{}, count)
	for index := range identifiers {
		identifier, err := store.newIdentifier()
		if err != nil {
			return nil, fmt.Errorf("generate service account identifier: %w", err)
		}
		if !validMutationIdentifier(identifier) {
			return nil, ErrMutationAuthority
		}
		if _, duplicate := seen[identifier]; duplicate {
			return nil, ErrMutationAuthority
		}
		seen[identifier] = struct{}{}
		identifiers[index] = identifier
	}
	return identifiers, nil
}

func newServiceAccountIdentifier() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func mapServiceAccountError(err error) error {
	if errors.Is(err, authz.ErrOperationDenied) {
		return ErrMutationDenied
	}
	return err
}

func mapServiceAccountDatabaseError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMutationTargetNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "P0002" {
		return ErrMutationTargetNotFound
	}
	return mapMutationDatabaseError(operation, err)
}
