package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const initialTenantPageSize = 50

func (store *PasswordStore) Tenants(ctx context.Context, application api.IdentityApplication, sessionDigest [32]byte, pageSize int, pageToken string) (api.BrowserTenantPage, error) {
	if !validApplication(application) || pageSize < 1 || pageSize > 200 {
		return api.BrowserTenantPage{}, ErrForbidden
	}
	after, err := store.tenantCursorAfter(sessionDigest, application, pageToken)
	if err != nil {
		return api.BrowserTenantPage{}, err
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	tx, err := store.pool.Begin(databaseCtx)
	if err != nil {
		return api.BrowserTenantPage{}, ErrUnavailable
	}
	defer rollbackTransaction(tx)
	page, err := store.readTenantPage(databaseCtx, tx, application, sessionDigest, pageSize, after)
	if err != nil {
		return api.BrowserTenantPage{}, err
	}
	if err := tx.Commit(databaseCtx); err != nil {
		return api.BrowserTenantPage{}, ErrUnavailable
	}
	return page, nil
}

func (store *PasswordStore) readTenantPage(ctx context.Context, tx pgx.Tx, application api.IdentityApplication, sessionDigest [32]byte, pageSize int, after string) (api.BrowserTenantPage, error) {
	rows, err := tx.Query(ctx, `SELECT tenant_id, tenant_name, tenant_admin
		FROM cloud_agents_identity.list_session_tenants($1, $2, $3, $4)`, sessionDigest[:], string(application), after, pageSize+1)
	if err != nil {
		return api.BrowserTenantPage{}, tenantPageError(err)
	}
	defer rows.Close()
	page := api.BrowserTenantPage{Tenants: make([]api.BrowserTenant, 0, pageSize)}
	for rows.Next() {
		var tenant api.BrowserTenant
		var admin bool
		if err := rows.Scan(&tenant.ID, &tenant.Name, &admin); err != nil {
			return api.BrowserTenantPage{}, ErrUnavailable
		}
		if len(page.Tenants) == pageSize {
			page.NextPageToken = store.tenantCursor(sessionDigest, application, page.Tenants[len(page.Tenants)-1].ID)
			break
		}
		tenant.DisplayRoles = []string{}
		if admin {
			tenant.DisplayRoles = append(tenant.DisplayRoles, "tenant.admin")
		}
		page.Tenants = append(page.Tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		return api.BrowserTenantPage{}, tenantPageError(err)
	}
	return page, nil
}

func tenantPageError(err error) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "28000" {
		return browserauth.ErrUnauthorized
	}
	return ErrUnavailable
}

func (store *PasswordStore) tenantCursor(sessionDigest [32]byte, application api.IdentityApplication, after string) string {
	return base64.RawURLEncoding.EncodeToString(append([]byte(after), store.tenantCursorMAC(sessionDigest, application, after)...))
}

func (store *PasswordStore) tenantCursorAfter(sessionDigest [32]byte, application api.IdentityApplication, cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) > 256 {
		return "", ErrForbidden
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != cursor {
		return "", ErrForbidden
	}
	after := string(raw[:len(raw)-sha256.Size])
	if common.ValidateIdentifier(after, "/pageToken") != nil || !hmac.Equal(raw[len(raw)-sha256.Size:], store.tenantCursorMAC(sessionDigest, application, after)) {
		return "", ErrForbidden
	}
	return after, nil
}

func (store *PasswordStore) tenantCursorMAC(sessionDigest [32]byte, application api.IdentityApplication, after string) []byte {
	mac := hmac.New(sha256.New, store.csrfKey)
	_, _ = mac.Write([]byte("cloud-agents.identity.tenant-cursor.v1\x00"))
	_, _ = mac.Write(sessionDigest[:])
	_, _ = mac.Write([]byte(application))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(after))
	return mac.Sum(nil)
}
