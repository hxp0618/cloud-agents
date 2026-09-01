package localmigration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type readinessQueryer struct {
	row  readinessRow
	args []any
}

func (queryer *readinessQueryer) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	queryer.args = args
	return queryer.row
}

type readinessRow struct {
	count                     int64
	first, last, bundleDigest string
	err                       error
}

func (row readinessRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*int64) = row.count
	*dest[1].(*string) = row.first
	*dest[2].(*string) = row.last
	*dest[3].(*string) = row.bundleDigest
	return nil
}

func TestCheckProductSchemaReadiness(t *testing.T) {
	current := productRunnerBindingSelector("000036")
	tests := []struct {
		name    string
		row     readinessRow
		wantErr bool
	}{
		{name: "current", row: readinessRow{count: 36, first: "000001", last: "000036", bundleDigest: current.schemaBundleDigest}},
		{name: "missing", row: readinessRow{}, wantErr: true},
		{name: "stale", row: readinessRow{count: 31, first: "000001", last: "000031", bundleDigest: productRunnerBindingSelector("000031").schemaBundleDigest}, wantErr: true},
		{name: "ahead", row: readinessRow{count: 37, first: "000001", last: "000037", bundleDigest: current.schemaBundleDigest}, wantErr: true},
		{name: "wrong bundle", row: readinessRow{count: 36, first: "000001", last: "000036", bundleDigest: productRunnerBindingSelector("000035").schemaBundleDigest}, wantErr: true},
		{name: "query failure", row: readinessRow{err: errors.New("query failed")}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryer := &readinessQueryer{row: test.row}
			err := CheckProductSchemaReadiness(context.Background(), queryer)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v", err)
			}
			if len(queryer.args) != 1 || queryer.args[0] != current.schemaHead {
				t.Fatalf("query args = %#v", queryer.args)
			}
		})
	}
}
