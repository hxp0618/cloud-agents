package identitytrust

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/jackc/pgx/v5"
)

type checkpointRow struct {
	values []any
	err    error
}

func (row checkpointRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return errors.New("unexpected checkpoint scan")
	}
	for index, destination := range destinations {
		switch target := destination.(type) {
		case *string:
			*target = row.values[index].(string)
		case *[]byte:
			*target = append([]byte(nil), row.values[index].([]byte)...)
		case *bool:
			*target = row.values[index].(bool)
		default:
			return errors.New("unexpected checkpoint destination")
		}
	}
	return nil
}

type checkpointDatabaseFake struct {
	rows      []pgx.Row
	queries   []string
	arguments [][]any
	deadlines []time.Time
}

func (database *checkpointDatabaseFake) QueryRow(ctx context.Context, query string, arguments ...any) pgx.Row {
	database.queries = append(database.queries, query)
	database.arguments = append(database.arguments, append([]any(nil), arguments...))
	deadline, _ := ctx.Deadline()
	database.deadlines = append(database.deadlines, deadline)
	row := database.rows[0]
	database.rows = database.rows[1:]
	return row
}

func TestPostgresCheckpointLoadsAndComparesBoundedState(t *testing.T) {
	state := checkpointStateForDatabase(t)
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	database := &checkpointDatabaseFake{rows: []pgx.Row{
		checkpointRow{err: pgx.ErrNoRows},
		checkpointRow{values: []any{state.AuthorityDigest, encoded}},
		checkpointRow{values: []any{true}},
	}}
	checkpoint := &PostgresCheckpoint{database: database}
	if _, exists, err := checkpoint.Load(context.Background(), state.Issuer); err != nil || exists {
		t.Fatalf("missing checkpoint exists=%t err=%v", exists, err)
	}
	loaded, exists, err := checkpoint.Load(context.Background(), state.Issuer)
	if err != nil || !exists || loaded.AuthorityDigest != state.AuthorityDigest || loaded.Revision != state.Revision {
		t.Fatalf("loaded checkpoint exists=%t state=%+v err=%v", exists, loaded, err)
	}
	if err := checkpoint.CompareAndSwap(context.Background(), state.Issuer, "", state); err != nil {
		t.Fatal(err)
	}
	if len(database.queries) != 3 || database.queries[0] != loadTrustCheckpointSQL || database.queries[2] != compareTrustCheckpointSQL {
		t.Fatalf("checkpoint queries = %#v", database.queries)
	}
	if len(database.arguments[2]) != 4 || database.arguments[2][0] != state.Issuer || database.arguments[2][1] != "" || database.arguments[2][2] != state.AuthorityDigest {
		t.Fatalf("checkpoint CAS arguments = %#v", database.arguments[2])
	}
	for _, deadline := range database.deadlines {
		if deadline.IsZero() || time.Until(deadline) <= 0 || time.Until(deadline) > checkpointOperationTimeout {
			t.Fatalf("checkpoint deadline = %v", deadline)
		}
	}
}

func TestPostgresCheckpointRejectsInvalidCallerAndStoredState(t *testing.T) {
	state := checkpointStateForDatabase(t)
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	database := &checkpointDatabaseFake{rows: []pgx.Row{
		checkpointRow{values: []any{state.AuthorityDigest, []byte(`{"issuer":"https://issuer.example","unknown":true}`)}},
		checkpointRow{values: []any{"sha256:" + strings.Repeat("f", 64), encoded}},
		checkpointRow{values: []any{false}},
	}}
	checkpoint := &PostgresCheckpoint{database: database}
	if _, _, err := checkpoint.Load(context.Background(), "http://issuer.example"); !errors.Is(err, ErrCheckpoint) || len(database.queries) != 0 {
		t.Fatalf("invalid issuer reached database: err=%v queries=%d", err, len(database.queries))
	}
	if _, _, err := checkpoint.Load(context.Background(), state.Issuer); !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("invalid stored checkpoint error = %v", err)
	}
	if _, _, err := checkpoint.Load(context.Background(), state.Issuer); !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("mismatched stored checkpoint digest error = %v", err)
	}
	if err := checkpoint.CompareAndSwap(context.Background(), state.Issuer, "sha256:"+string(make([]byte, 64)), state); !errors.Is(err, ErrCheckpoint) || len(database.queries) != 2 {
		t.Fatalf("invalid digest reached database: err=%v queries=%d", err, len(database.queries))
	}
	if err := checkpoint.CompareAndSwap(context.Background(), state.Issuer, state.AuthorityDigest, state); !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("lost checkpoint CAS error = %v", err)
	}
}

func checkpointStateForDatabase(t *testing.T) State {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	state := State{
		Issuer: "https://issuer.example", Revision: 7, SecurityEpoch: 3, NotBefore: 1_799_999_900, ExpiresAt: 1_800_001_000,
		Lineage: []LineageKey{{
			JWK:     api.IdentityJWK{Alg: "RS256", E: "AQAB", KeyOps: []string{"verify"}, Kid: "key-1", Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()), Use: "sig"},
			Enabled: true, NotBefore: 1_799_999_000, NotAfter: 1_800_001_000,
		}},
	}
	state.AuthorityDigest, err = authorityDigest(state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
