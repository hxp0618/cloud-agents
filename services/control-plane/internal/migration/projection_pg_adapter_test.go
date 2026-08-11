package migration

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

type pgTestQuery struct {
	rows    [][]any
	err     error
	scanErr error
	iterErr error
}

type pgTestSnapshot struct {
	metadata  SnapshotMetadata
	queries   map[projectionQueryID][]pgTestQuery
	stats     projectionQueryStats
	queryHook func(projectionQueryID)
}

func (snapshot *pgTestSnapshot) projectionSnapshot()                   {}
func (snapshot *pgTestSnapshot) Metadata() SnapshotMetadata            { return snapshot.metadata }
func (snapshot *pgTestSnapshot) projectionStats() projectionQueryStats { return snapshot.stats }

func (snapshot *pgTestSnapshot) queryProjection(_ context.Context, id projectionQueryID, _ ...any) (Rows, error) {
	if snapshot.queryHook != nil {
		snapshot.queryHook(id)
	}
	queue := snapshot.queries[id]
	if len(queue) == 0 {
		return nil, errors.New("missing scripted projection query")
	}
	query := queue[0]
	snapshot.queries[id] = queue[1:]
	snapshot.stats.QueryCount++
	if query.err != nil {
		return nil, query.err
	}
	return &pgTestRows{snapshot: snapshot, rows: query.rows, scanErr: query.scanErr, iterErr: query.iterErr, index: -1}, nil
}

func (snapshot *pgTestSnapshot) queryProjectionRow(context.Context, projectionQueryID, ...any) Row {
	return pgTestRow{err: errors.New("row query is not scripted")}
}

type pgTestRows struct {
	snapshot *pgTestSnapshot
	rows     [][]any
	index    int
	scanErr  error
	iterErr  error
	closed   bool
}

func (rows *pgTestRows) Next() bool {
	if rows.closed || rows.index+1 >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}

func (rows *pgTestRows) Scan(targets ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	if rows.index < 0 || rows.index >= len(rows.rows) || len(targets) != len(rows.rows[rows.index]) {
		return errors.New("test scan shape mismatch")
	}
	var size uint64
	for index, target := range targets {
		if err := pgTestAssign(target, rows.rows[rows.index][index]); err != nil {
			return err
		}
		size += pgTestValueBytes(rows.rows[rows.index][index])
	}
	rows.snapshot.stats.RowCount++
	rows.snapshot.stats.TotalBytes += size
	return nil
}

func (rows *pgTestRows) Err() error { return rows.iterErr }
func (rows *pgTestRows) Close()     { rows.closed = true }

type pgTestRow struct{ err error }

func (row pgTestRow) Scan(...any) error { return row.err }

func pgTestAssign(target, value any) error {
	destination := reflect.ValueOf(target)
	if destination.Kind() != reflect.Pointer || destination.IsNil() {
		return errors.New("test scan destination is not a pointer")
	}
	destination = destination.Elem()
	if value == nil {
		destination.Set(reflect.Zero(destination.Type()))
		return nil
	}
	source := reflect.ValueOf(value)
	if source.Type().AssignableTo(destination.Type()) {
		destination.Set(source)
		return nil
	}
	return errors.New("test scan type mismatch")
}

func pgTestValueBytes(value any) uint64 {
	switch typed := value.(type) {
	case string:
		return uint64(len(typed))
	case *string:
		if typed != nil {
			return uint64(len(*typed))
		}
	case []string:
		var size uint64
		for _, item := range typed {
			size += uint64(len(item))
		}
		return size
	}
	return 1
}

func pgString(value string) *string { return &value }
func pgBool(value bool) *bool       { return &value }

func pgTestMetadata(major uint16) SnapshotMetadata {
	return SnapshotMetadata{
		Mode: IdleReadSnapshot, Ownership: OwnedIdleSnapshot,
		PostgresMajor: major, ServerVersionNum: uint32(major)*10_000 + 9,
		DatabaseName: "cloud_agents_test", AuthorityPhase: AuthorityPhaseConnectedSession,
		SessionUser: "workload", CurrentUser: "workload",
		IsolationLevel: "repeatable_read", AccessMode: "read_only", TxStatus: "T",
	}
}

func pgCapabilityRow(major uint16) []any {
	options := major >= 16
	return []any{strings.TrimSpace(strings.Join([]string{string(rune('0' + major/10)), string(rune('0' + major%10)), "0009"}, "")), options, options, major >= 16, major >= 17}
}

func TestProjectionFixedQueryRegistryIsClosedAndPG15ParseSafe(t *testing.T) {
	ordinalCollation := regexp.MustCompile(`(?i)ORDER\s+BY\s+[0-9]+\s+COLLATE`)
	for id := projectionQuerySnapshotMetadata; id <= projectionQueryDefaultACLs; id++ {
		query, ok := projectionFixedQuery(id)
		if !ok || strings.TrimSpace(query) == "" {
			t.Fatalf("query id %d is absent", id)
		}
		if strings.Contains(query, ";") {
			t.Fatalf("query id %d contains a statement separator", id)
		}
		for _, alias := range []string{"pg_catalog.bigint", "pg_catalog.integer", "pg_catalog.boolean", "pg_catalog.smallint"} {
			if strings.Contains(query, alias) {
				t.Fatalf("query id %d uses non-existent schema-qualified type alias %s", id, alias)
			}
		}
		if ordinalCollation.MatchString(query) {
			t.Fatalf("query id %d applies collation to an ordinal ORDER BY expression", id)
		}
	}
	if _, ok := projectionFixedQuery(255); ok {
		t.Fatal("unknown query id was accepted")
	}
	membership, _ := projectionFixedQuery(projectionQueryAuthorityMemberships)
	if strings.Contains(membership, "m.inherit_option") || strings.Contains(membership, "m.set_option") {
		t.Fatal("membership SQL directly parses PG16-only columns")
	}
	if !strings.Contains(membership, "to_jsonb(m)->>'inherit_option'") {
		t.Fatal("membership SQL lacks parse-safe capability mapping")
	}
	metadata, _ := projectionFixedQuery(projectionQuerySnapshotMetadata)
	sanitation, _ := projectionFixedQuery(projectionQuerySnapshotSanitation)
	for id, query := range map[projectionQueryID]string{
		projectionQuerySnapshotMetadata:   metadata,
		projectionQuerySnapshotSanitation: sanitation,
	} {
		if !strings.Contains(query, "::pg_catalog.int8") {
			t.Fatalf("query id %d omits the PG15-safe int8 cast", id)
		}
	}
	reachability, _ := projectionFixedQuery(projectionQueryAuthorityReachability)
	if !strings.Contains(reachability, "::pg_catalog.int4") {
		t.Fatal("reachability SQL omits the PG15-safe int4 server-version cast")
	}
	if strings.Contains(reachability, "unnest($1::pg_catalog.text[], $2::pg_catalog.text[])") {
		t.Fatal("reachability SQL uses the unsupported multi-array unnest overload")
	}
	if strings.Count(reachability, "WITH ORDINALITY") != 2 || !strings.Contains(reachability, "cardinality($1::pg_catalog.text[])") || !strings.Contains(reachability, "cardinality($2::pg_catalog.text[])") || !strings.Contains(reachability, "JOIN roles USING (ordinality)") {
		t.Fatal("reachability SQL does not enforce equal-length ordinal pairing")
	}
	for _, privilege := range []string{"'MEMBER'", "'USAGE'", "'SET'"} {
		if !strings.Contains(reachability, privilege) {
			t.Fatalf("pg_has_role fixed query omits %s", privilege)
		}
	}
	namespace, _ := projectionFixedQuery(projectionQueryNamespace)
	for _, fragment := range []string{"AS row_kind", "AS value1", "FROM output", `ORDER BY row_kind COLLATE "C"`, `value1 COLLATE "C" NULLS FIRST`} {
		if !strings.Contains(namespace, fragment) {
			t.Fatalf("namespace SQL omits stable named text ordering fragment %q", fragment)
		}
	}
	for _, catalog := range []string{"pg_collation", "pg_operator", "pg_opclass", "pg_opfamily", "pg_conversion", "pg_ts_config", "pg_ts_dict", "pg_ts_parser", "pg_ts_template", "pg_statistic_ext"} {
		if !strings.Contains(namespace, catalog) {
			t.Fatalf("namespace detection SQL omits %s", catalog)
		}
	}
	defaultACL, _ := projectionFixedQuery(projectionQueryDefaultACLs)
	if strings.Contains(defaultACL, `a.defaclobjtype COLLATE "C"`) {
		t.Fatal("default ACL SQL applies collation to the internal char catalog column")
	}
	for _, fragment := range []string{"a.defaclobjtype::pg_catalog.text AS object_kind", "FROM projected", `ORDER BY owner COLLATE "C"`, `object_kind COLLATE "C"`} {
		if !strings.Contains(defaultACL, fragment) {
			t.Fatalf("default ACL SQL omits safe projected ordering fragment %q", fragment)
		}
	}
}

func TestPGProjectionCapabilityMatrix(t *testing.T) {
	for _, major := range []uint16{15, 16, 17} {
		snapshot := &pgTestSnapshot{metadata: pgTestMetadata(major), queries: map[projectionQueryID][]pgTestQuery{
			projectionQueryCapability: {{rows: [][]any{pgCapabilityRow(major)}}},
		}}
		projector, err := NewPGProjector(context.Background(), snapshot)
		if err != nil || projector.major != major {
			t.Fatalf("PG%d capability failed: projector=%+v err=%v", major, projector, err)
		}
	}
}

func TestPGProjectionCapabilityMismatchAndRedaction(t *testing.T) {
	snapshot := &pgTestSnapshot{metadata: pgTestMetadata(15), queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryCapability: {{rows: [][]any{{"150009", true, false, false, false}}}},
	}}
	_, err := NewPGProjector(context.Background(), snapshot)
	if !IsCode(err, CodeProjectionCapabilityMismatch) {
		t.Fatalf("wanted capability mismatch, got %v", err)
	}
	secret := "postgres://admin:password@db/private"
	snapshot = &pgTestSnapshot{metadata: pgTestMetadata(15), queries: map[projectionQueryID][]pgTestQuery{
		projectionQueryCapability: {{scanErr: errors.New(secret), rows: [][]any{{"150009", false, false, false, false}}}},
	}}
	_, err = NewPGProjector(context.Background(), snapshot)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("driver secret leaked through projection error: %v", err)
	}
}

func TestPGProjectionLocaleCapabilityMatrixIsExact(t *testing.T) {
	tests := []struct {
		major uint16
		row   []any
	}{
		{15, []any{"150009", false, false, true, false}},
		{15, []any{"150009", false, false, false, true}},
		{16, []any{"160009", true, true, false, false}},
		{16, []any{"160009", true, true, true, true}},
		{17, []any{"170009", true, true, false, true}},
		{17, []any{"170009", true, true, true, false}},
	}
	for _, test := range tests {
		snapshot := &pgTestSnapshot{metadata: pgTestMetadata(test.major), queries: map[projectionQueryID][]pgTestQuery{
			projectionQueryCapability: {{rows: [][]any{test.row}}},
		}}
		if _, err := NewPGProjector(context.Background(), snapshot); !IsCode(err, CodeProjectionCapabilityMismatch) {
			t.Fatalf("PG%d accepted invalid locale capability combination %v: %v", test.major, test.row[3:], err)
		}
	}
}

func TestPGProjectionInclusiveBoundsUseMaxPlusOneSentinel(t *testing.T) {
	budget := projectionReadBudget{major: 17}
	for index := uint64(0); index < projectionMaxQueryRows; index++ {
		if err := budget.add("bounds.rows", ""); err != nil {
			t.Fatalf("inclusive row max rejected at %d: %v", index, err)
		}
	}
	if err := budget.add("bounds.rows", ""); !IsCode(err, CodeProjectionLimitExceeded) {
		t.Fatalf("max+1 row sentinel was accepted: %v", err)
	}
	budget = projectionReadBudget{major: 17}
	if err := budget.add("bounds.bytes", strings.Repeat("x", int(projectionMaxRowBytes))); err != nil {
		t.Fatalf("inclusive row-byte max rejected: %v", err)
	}
	if err := (&projectionReadBudget{major: 17}).add("bounds.bytes", strings.Repeat("x", int(projectionMaxRowBytes+1))); !IsCode(err, CodeProjectionLimitExceeded) {
		t.Fatalf("max+1 row bytes were accepted: %v", err)
	}
	if err := (&projectionReadBudget{major: 17}).add("bounds.utf8", string([]byte{0xff})); !IsCode(err, CodeProjectionCatalogQueryFailed) {
		t.Fatalf("invalid UTF-8 was accepted: %v", err)
	}
	budget = projectionReadBudget{major: 17}
	chunk := strings.Repeat("y", int(projectionMaxRowBytes))
	for index := uint64(0); index < projectionMaxTotalResultBytes/projectionMaxRowBytes; index++ {
		if err := budget.add("bounds.total", chunk); err != nil {
			t.Fatalf("inclusive total-byte max rejected at chunk %d: %v", index, err)
		}
	}
	if err := budget.add("bounds.total", "z"); !IsCode(err, CodeProjectionLimitExceeded) {
		t.Fatalf("max+1 total byte was accepted: %v", err)
	}
}
