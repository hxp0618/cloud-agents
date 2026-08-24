package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSQLLedgerStoreTreatsEarlyAndDeferredUndefinedTableAsEmpty(t *testing.T) {
	undefined := &pgconn.PgError{Code: "42P01"}
	for _, test := range []struct {
		name      string
		queryer   Queryer
		wantClose int
	}{
		{"query", ledgerFaultQueryer{queryErr: undefined}, 0},
		{"rows", ledgerFaultQueryer{rows: &ledgerFaultRows{err: undefined}}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := (SQLLedgerStore{}).Read(context.Background(), test.queryer)
			if err != nil || rows == nil || len(rows) != 0 {
				t.Fatalf("undefined ledger table did not become an explicit empty prefix: rows=%v err=%v", rows, err)
			}
			if fault, ok := test.queryer.(ledgerFaultQueryer); ok && fault.rows != nil && fault.rows.(*ledgerFaultRows).closeCalls != test.wantClose {
				t.Fatalf("deferred error rows close calls=%d want=%d", fault.rows.(*ledgerFaultRows).closeCalls, test.wantClose)
			}
		})
	}
}

func TestSQLLedgerStoreDoesNotHideOtherDeferredFailures(t *testing.T) {
	rows := &ledgerFaultRows{err: errors.New("secret-ledger-stream")}
	result, err := (SQLLedgerStore{}).Read(context.Background(), ledgerFaultQueryer{rows: rows})
	if result != nil || !IsCode(err, CodeInvalidLedger) || rows.closeCalls != 1 {
		t.Fatalf("non-42P01 ledger stream failure was accepted: rows=%v err=%v close=%d", result, err, rows.closeCalls)
	}
}

type ledgerFaultQueryer struct {
	rows     Rows
	queryErr error
}

func (queryer ledgerFaultQueryer) Query(context.Context, string, ...any) (Rows, error) {
	return queryer.rows, queryer.queryErr
}

func (ledgerFaultQueryer) QueryRow(context.Context, string, ...any) Row {
	return projectionErrorRow{err: errors.New("ledger fault queryer has no row query")}
}

type ledgerFaultRows struct {
	err        error
	closeCalls int
}

func (*ledgerFaultRows) Next() bool        { return false }
func (*ledgerFaultRows) Scan(...any) error { return errors.New("ledger fault rows cannot scan") }
func (rows *ledgerFaultRows) Err() error   { return rows.err }
func (rows *ledgerFaultRows) Close()       { rows.closeCalls++ }
