package postgres

import (
	"errors"
	"testing"

	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRemoteWorkerCommandDeadlock(t *testing.T) {
	if isRemoteWorkerCommandDeadlock(errors.New("ordinary error")) {
		t.Fatal("ordinary errors must not be retried")
	}
	if !isRemoteWorkerCommandDeadlock(&pgconn.PgError{Code: "40P01"}) {
		t.Fatal("deadlocks must be retried")
	}
	if isRemoteWorkerCommandDeadlock(&pgconn.PgError{Code: "40001"}) {
		t.Fatal("serialization failures are not this retry path")
	}
	if !errors.Is(mapFoundationSandboxExecError(&pgconn.PgError{Code: "40P01"}), internalcoordination.ErrFoundationSandboxConflict) {
		t.Fatal("sandbox exec deadlocks must surface as a retryable conflict")
	}
}
