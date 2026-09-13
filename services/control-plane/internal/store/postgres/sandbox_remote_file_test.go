package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRemoteWorkerSandboxFileDeadlock(t *testing.T) {
	if isRemoteWorkerSandboxFileDeadlock(errors.New("ordinary error")) {
		t.Fatal("ordinary errors must not be retried")
	}
	if !isRemoteWorkerSandboxFileDeadlock(&pgconn.PgError{Code: "40P01"}) {
		t.Fatal("deadlocks must be retried")
	}
	if isRemoteWorkerSandboxFileDeadlock(&pgconn.PgError{Code: "40001"}) {
		t.Fatal("serialization failures are not this retry path")
	}
}
