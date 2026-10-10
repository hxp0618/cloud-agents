package identity

import (
	"context"
	"errors"
	"sort"
	"strconv"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maximumEmailPolicyDomains = 64

var (
	ErrEmailPolicyInvalid  = errors.New("email suffix policy input invalid")
	ErrEmailPolicyConflict = errors.New("email suffix policy resource version conflict")
)

// EmailPolicyStore reaches email policy state only through the identity
// schema's tenant-administrator-gated SECURITY DEFINER functions.
type EmailPolicyStore struct{ pool *pgxpool.Pool }

func NewEmailPolicyStore(pool *pgxpool.Pool) (*EmailPolicyStore, error) {
	if pool == nil {
		return nil, errInvalidConfiguration
	}
	return &EmailPolicyStore{pool: pool}, nil
}

func (store *EmailPolicyStore) GetEmailSuffixPolicy(ctx context.Context, sessionDigest [32]byte, tenantID string) (api.EmailSuffixPolicy, error) {
	if ctx == nil {
		return api.EmailSuffixPolicy{}, ErrEmailPolicyInvalid
	}
	if err := ctx.Err(); err != nil {
		return api.EmailSuffixPolicy{}, err
	}
	if common.ValidateIdentifier(tenantID, "/tenantId") != nil {
		return api.EmailSuffixPolicy{}, ErrEmailPolicyInvalid
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var version int64
	var domains []string
	err := store.pool.QueryRow(databaseCtx, `SELECT resource_version, allowed_domains
		FROM cloud_agents_identity.read_email_suffix_policy($1,$2)`, sessionDigest[:], tenantID).Scan(&version, &domains)
	if err != nil {
		return api.EmailSuffixPolicy{}, emailPolicyError(ctx, err)
	}
	if version < 0 || !canonicalEmailPolicyDomains(domains) {
		return api.EmailSuffixPolicy{}, ErrUnavailable
	}
	if domains == nil {
		domains = []string{}
	}
	return api.EmailSuffixPolicy{TenantID: tenantID, ResourceVersion: strconv.FormatInt(version, 10), AllowedDomains: domains}, nil
}

func (store *EmailPolicyStore) UpdateEmailSuffixPolicy(ctx context.Context, sessionDigest [32]byte, tenantID string, update api.EmailSuffixPolicyUpdate) (api.EmailSuffixPolicy, error) {
	if ctx == nil {
		return api.EmailSuffixPolicy{}, ErrEmailPolicyInvalid
	}
	if err := ctx.Err(); err != nil {
		return api.EmailSuffixPolicy{}, err
	}
	if common.ValidateIdentifier(tenantID, "/tenantId") != nil {
		return api.EmailSuffixPolicy{}, ErrEmailPolicyInvalid
	}
	expected, err := parseEmailPolicyVersion(update.ExpectedResourceVersion)
	if err != nil {
		return api.EmailSuffixPolicy{}, ErrEmailPolicyInvalid
	}
	domains, err := normalizeEmailPolicyDomains(update.AllowedDomains)
	if err != nil {
		return api.EmailSuffixPolicy{}, ErrEmailPolicyInvalid
	}
	eventID, err := newAuditEventID()
	if err != nil {
		return api.EmailSuffixPolicy{}, ErrUnavailable
	}
	correlationID, err := requestCorrelationID(ctx)
	if err != nil {
		return api.EmailSuffixPolicy{}, ErrUnavailable
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var version int64
	var stored []string
	err = store.pool.QueryRow(databaseCtx, `SELECT resource_version, allowed_domains
		FROM cloud_agents_identity.update_email_suffix_policy($1,$2,$3,$4,$5,$6)`,
		sessionDigest[:], tenantID, expected, domains, eventID, correlationID).Scan(&version, &stored)
	if err != nil {
		return api.EmailSuffixPolicy{}, emailPolicyError(ctx, err)
	}
	if version != expected+1 || !canonicalEmailPolicyDomains(stored) {
		return api.EmailSuffixPolicy{}, ErrUnavailable
	}
	if stored == nil {
		stored = []string{}
	}
	return api.EmailSuffixPolicy{TenantID: tenantID, ResourceVersion: strconv.FormatInt(version, 10), AllowedDomains: stored}, nil
}

func normalizeEmailPolicyDomains(values []string) ([]string, error) {
	if len(values) > maximumEmailPolicyDomains {
		return nil, ErrEmailPolicyInvalid
	}
	canonical := make([]string, len(values))
	for index, value := range values {
		domain, err := browserauth.NormalizeEmailPolicyDomain(value)
		if err != nil {
			return nil, ErrEmailPolicyInvalid
		}
		canonical[index] = domain
	}
	sort.Strings(canonical)
	if !canonicalEmailPolicyDomains(canonical) {
		return nil, ErrEmailPolicyInvalid
	}
	return canonical, nil
}

func canonicalEmailPolicyDomains(values []string) bool {
	if len(values) > maximumEmailPolicyDomains {
		return false
	}
	for index, value := range values {
		canonical, err := browserauth.NormalizeEmailPolicyDomain(value)
		if err != nil || canonical != value || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func parseEmailPolicyVersion(value string) (int64, error) {
	if value == "" || len(value) > 20 || len(value) > 1 && value[0] == '0' {
		return 0, ErrEmailPolicyInvalid
	}
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version < 0 {
		return 0, ErrEmailPolicyInvalid
	}
	return version, nil
}

func emailPolicyError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "28000":
			return browserauth.ErrUnauthorized
		case "42501":
			return ErrForbidden
		case "40001":
			return ErrEmailPolicyConflict
		}
	}
	return ErrUnavailable
}
