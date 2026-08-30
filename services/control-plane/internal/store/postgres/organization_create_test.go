package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestCreateOrganizationKernelReturnsInsertedResource(t *testing.T) {
	now := time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC)
	transaction := &fakeTransaction{rows: []rowScanner{
		rowValues("organization-beta", int64(5), "active"),
		rowValues("organization-beta", "organization-beta", "tenant-alpha", "Organization Beta", "active", int64(5), now, now),
	}}
	handle := &tenantReadHandle{active: true, transaction: transaction, tenantID: "tenant-alpha", clock: time.Now}
	result, err := createOrganizationInTransaction(context.Background(), handle, "tenant-alpha", CreateOrganizationInput{
		ExpectedTenantRevision: 4,
		OrganizationUID:        "organization-beta",
		OrganizationName:       "organization-beta",
		DisplayName:            "Organization Beta",
		AuditFactUID:           "audit-organization-beta",
		ReasonCode:             "operator-request",
	})
	if err != nil || result.UID != "organization-beta" || result.ResourceVersion != 5 || result.State != "active" {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
	if len(transaction.queries) != 2 || transaction.queries[0].sql != createOrganizationSQL || transaction.queries[1].sql != getOrganizationSQL {
		t.Fatalf("organization create SQL trace = %#v", transaction.queries)
	}
}

func TestCreateOrganizationKernelDistinguishesMissingResultFromReadFailure(t *testing.T) {
	readFailure := errors.New("read failed")
	for _, test := range []struct {
		name    string
		rowErr  error
		wantErr error
	}{
		{name: "missing result", rowErr: pgx.ErrNoRows, wantErr: ErrMutationResultDrift},
		{name: "read failure", rowErr: readFailure, wantErr: readFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := &fakeTransaction{rows: []rowScanner{
				rowValues("organization-beta", int64(5), "active"),
				rowError(test.rowErr),
			}}
			handle := &tenantReadHandle{active: true, transaction: transaction, tenantID: "tenant-alpha", clock: time.Now}
			_, err := createOrganizationInTransaction(context.Background(), handle, "tenant-alpha", CreateOrganizationInput{
				ExpectedTenantRevision: 4,
				OrganizationUID:        "organization-beta",
				OrganizationName:       "organization-beta",
				DisplayName:            "Organization Beta",
				AuditFactUID:           "audit-organization-beta",
				ReasonCode:             "operator-request",
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
