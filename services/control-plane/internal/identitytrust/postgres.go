package identitytrust

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	checkpointOperationTimeout = 5 * time.Second
	maximumCheckpointIssuer    = 2048
	loadTrustCheckpointSQL     = `SELECT authority_digest, state_bytes FROM cloud_agents_identity.load_trust_checkpoint($1)`
	compareTrustCheckpointSQL  = `SELECT cloud_agents_identity.compare_trust_checkpoint($1, $2, $3, $4)`
)

type checkpointDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresCheckpoint struct{ database checkpointDatabase }

func NewPostgresCheckpoint(pool *pgxpool.Pool) (*PostgresCheckpoint, error) {
	if pool == nil {
		return nil, ErrInvalidConfig
	}
	return &PostgresCheckpoint{database: pool}, nil
}

func (checkpoint *PostgresCheckpoint) Load(ctx context.Context, issuer string) (State, bool, error) {
	if checkpoint == nil || checkpoint.database == nil || ctx == nil || !validCheckpointIssuer(issuer) {
		return State{}, false, ErrCheckpoint
	}
	databaseCtx, cancel := context.WithTimeout(ctx, checkpointOperationTimeout)
	defer cancel()
	var authorityDigest string
	var encoded []byte
	err := checkpoint.database.QueryRow(databaseCtx, loadTrustCheckpointSQL, issuer).Scan(&authorityDigest, &encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, false, nil
	}
	if err != nil || len(encoded) == 0 || len(encoded) > maximumAuthorityDocument {
		return State{}, false, ErrCheckpoint
	}
	state, err := decodeCheckpointState(encoded, issuer)
	if err != nil || state.AuthorityDigest != authorityDigest {
		return State{}, false, ErrCheckpoint
	}
	return state, true, nil
}

func (checkpoint *PostgresCheckpoint) CompareAndSwap(ctx context.Context, issuer, expectedDigest string, state State) error {
	if checkpoint == nil || checkpoint.database == nil || ctx == nil || !validCheckpointIssuer(issuer) ||
		expectedDigest != "" && !validAuthorityDigest(expectedDigest) || state.Issuer != issuer {
		return ErrCheckpoint
	}
	normalized, err := normalizeStoredState(state, issuer)
	if err != nil {
		return ErrCheckpoint
	}
	encoded, err := json.Marshal(normalized)
	if err != nil || len(encoded) == 0 || len(encoded) > maximumAuthorityDocument {
		return ErrCheckpoint
	}
	databaseCtx, cancel := context.WithTimeout(ctx, checkpointOperationTimeout)
	defer cancel()
	var swapped bool
	err = checkpoint.database.QueryRow(databaseCtx, compareTrustCheckpointSQL, issuer, expectedDigest, normalized.AuthorityDigest, encoded).Scan(&swapped)
	if err != nil || !swapped {
		return ErrCheckpoint
	}
	return nil
}

func decodeCheckpointState(encoded []byte, issuer string) (State, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return State{}, ErrCheckpoint
	}
	return normalizeStoredState(state, issuer)
}

func validCheckpointIssuer(value string) bool {
	if len(value) == 0 || len(value) > maximumCheckpointIssuer || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validAuthorityDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}
