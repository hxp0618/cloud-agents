package migration

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const projectionSnapshotTestDigest Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestRunnerSessionSnapshotReturnsSameConnectionForBothOwnedPhases(t *testing.T) {
	for _, phase := range []AuthorityPhase{AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole} {
		t.Run(string(phase), func(t *testing.T) {
			connection := newFakeRunnerSessionSnapshotConnection()
			snapshot, err := beginRunnerSessionProjectionSnapshot(context.Background(), connection, phase)
			if err != nil {
				t.Fatal(err)
			}
			metadata := snapshot.Metadata()
			wantUser := connection.sessionUser
			if phase == AuthorityPhaseMigrationRole {
				wantUser = MigrationOwnerRole
			}
			if metadata.Mode != IdleReadSnapshot || metadata.Ownership != OwnedIdleSnapshot || metadata.AuthorityPhase != phase || metadata.CurrentUser != wantUser || metadata.TxStatus != "T" {
				t.Fatalf("runner snapshot metadata=%+v", metadata)
			}
			if err := snapshot.RollbackAndReturnToRunner(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := snapshot.RollbackAndReturnToRunner(context.Background()); err != nil {
				t.Fatalf("idempotent runner snapshot close=%v", err)
			}
			if connection.prepareCalls != 1 || connection.beginCalls != 1 || connection.rollbackCalls != 1 || connection.validateCalls != 1 || connection.returnCalls != 1 || connection.invalidateCalls != 0 || connection.releaseCalls != 0 || connection.hijackCalls != 0 || connection.setMigrationRoleCalls != 0 || connection.sanitizeCalls != 0 {
				t.Fatalf("runner snapshot changed connection ownership: %+v", connection)
			}
			if _, err := snapshot.queryProjection(context.Background(), projectionQueryCapability); !IsCode(err, CodeProjectionSnapshotInvalid) {
				t.Fatalf("closed runner snapshot query=%v", err)
			}
		})
	}
}

func TestRunnerSessionSnapshotRejectsUnknownPhaseWithoutMutation(t *testing.T) {
	connection := newFakeRunnerSessionSnapshotConnection()
	if snapshot, err := beginRunnerSessionProjectionSnapshot(context.Background(), connection, AuthorityPhase("unknown")); snapshot != nil || !IsCode(err, CodeProjectionMetadataMismatch) || connection.returnCalls != 1 || connection.prepareCalls != 0 || connection.beginCalls != 0 || connection.invalidateCalls != 0 {
		t.Fatalf("unknown runner phase mutated connection: snapshot=%v err=%v connection=%+v", snapshot, err, connection)
	}
	if snapshot, err := BeginRunnerSessionProjectionSnapshot(context.Background(), &fakeSession{}, AuthorityPhaseConnectedSession); snapshot != nil || !IsCode(err, CodeProjectionSnapshotInvalid) {
		t.Fatalf("foreign DatabaseSession entered runner snapshot factory: snapshot=%v err=%v", snapshot, err)
	}
}

func TestRunnerSessionSnapshotSurfaceCannotOwnRunnerLifecycle(t *testing.T) {
	connection := newFakeRunnerSessionSnapshotConnection()
	snapshot, err := beginRunnerSessionProjectionSnapshot(context.Background(), connection, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatal(err)
	}
	value := any(snapshot)
	for name, forbidden := range map[string]bool{
		"commit":        implements[interface{ Commit(context.Context) error }](value),
		"rollback":      implements[interface{ Rollback(context.Context) error }](value),
		"close-session": implements[interface{ Close(context.Context) error }](value),
		"unlock": implements[interface {
			UnlockAndReset(context.Context, int64) error
		}](value),
		"execute-sql": implements[interface {
			ExecuteStatement(context.Context, []byte) error
		}](value),
		"release-to-pool": implements[interface{ RollbackAndRelease(context.Context) error }](value),
	} {
		if forbidden {
			t.Fatalf("runner projection snapshot exposed %s authority", name)
		}
	}
	if err := snapshot.RollbackAndReturnToRunner(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPGXRunnerSnapshotTrackedLifecycleIsClosed(t *testing.T) {
	session := &pgxSession{projectionActive: true}
	if !session.runnerConnectedProjectionState() {
		t.Fatal("exact connected-session lifecycle was rejected")
	}
	for name, mutate := range map[string]func(*pgxSession){
		"closed":   func(s *pgxSession) { s.closed = true },
		"inactive": func(s *pgxSession) { s.projectionActive = false },
		"role":     func(s *pgxSession) { s.roleConfigured = true },
		"policy":   func(s *pgxSession) { s.settingsPolicy = &ExecutionPolicy{} },
		"lock":     func(s *pgxSession) { key := int64(7); s.advisoryKey = &key },
	} {
		t.Run("connected-"+name, func(t *testing.T) {
			fault := &pgxSession{projectionActive: true}
			mutate(fault)
			if fault.runnerConnectedProjectionState() {
				t.Fatal("invalid connected-session lifecycle was accepted")
			}
		})
	}
	key := int64(17)
	policy := &ExecutionPolicy{StatementTimeoutMS: 5, LockTimeoutMS: 1, IdleInTransactionSessionTimeoutMS: 60}
	migration := &pgxSession{projectionActive: true, roleConfigured: true, settingsPolicy: policy, advisoryKey: &key}
	gotKey, gotPolicy, ok := migration.runnerProjectionBoundary()
	if !ok || gotKey != key || gotPolicy.StatementTimeoutMS != policy.StatementTimeoutMS {
		t.Fatalf("exact migration-role lifecycle rejected: key=%d policy=%+v ok=%v", gotKey, gotPolicy, ok)
	}
	for name, mutate := range map[string]func(*pgxSession){
		"closed":   func(s *pgxSession) { s.closed = true },
		"inactive": func(s *pgxSession) { s.projectionActive = false },
		"role":     func(s *pgxSession) { s.roleConfigured = false },
		"policy":   func(s *pgxSession) { s.settingsPolicy = nil },
		"lock":     func(s *pgxSession) { s.advisoryKey = nil },
	} {
		t.Run("migration-"+name, func(t *testing.T) {
			faultKey := key
			faultPolicy := *policy
			fault := &pgxSession{projectionActive: true, roleConfigured: true, settingsPolicy: &faultPolicy, advisoryKey: &faultKey}
			mutate(fault)
			if _, _, ok := fault.runnerProjectionBoundary(); ok {
				t.Fatal("invalid migration-role lifecycle was accepted")
			}
		})
	}
}

func implements[T any](value any) bool {
	_, ok := value.(T)
	return ok
}

func TestRunnerSessionSnapshotBeginFaultsInvalidateInsteadOfReturningConnection(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*fakeRunnerSessionSnapshotConnection)
	}{
		{"prepare", func(c *fakeRunnerSessionSnapshotConnection) { c.prepareErr = errors.New("secret-prepare") }},
		{"not-idle", func(c *fakeRunnerSessionSnapshotConnection) { c.status = '?' }},
		{"begin", func(c *fakeRunnerSessionSnapshotConnection) { c.beginErr = errors.New("secret-begin") }},
		{"begin-status", func(c *fakeRunnerSessionSnapshotConnection) { c.beginStatus = '?' }},
		{"metadata", func(c *fakeRunnerSessionSnapshotConnection) { c.rowErr = errors.New("secret-metadata") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := newFakeRunnerSessionSnapshotConnection()
			test.mutate(connection)
			snapshot, err := beginRunnerSessionProjectionSnapshot(context.Background(), connection, AuthorityPhaseConnectedSession)
			if snapshot != nil || err == nil || containsErrorText(err, "secret-") || connection.invalidateCalls != 1 || connection.returnCalls != 1 || connection.releaseCalls != 0 {
				t.Fatalf("runner snapshot begin fault escaped fail-closed cleanup: snapshot=%v err=%v connection=%+v", snapshot, err, connection)
			}
		})
	}
}

func TestRunnerSessionSnapshotCloseFaultsInvalidateAndNeverReturnUsableState(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*fakeRunnerSessionSnapshotConnection)
	}{
		{"unknown-status", func(c *fakeRunnerSessionSnapshotConnection) { c.status = '?' }},
		{"rollback", func(c *fakeRunnerSessionSnapshotConnection) { c.rollbackErr = errors.New("secret-rollback") }},
		{"rollback-status", func(c *fakeRunnerSessionSnapshotConnection) { c.rollbackStatus = '?' }},
		{"return-boundary", func(c *fakeRunnerSessionSnapshotConnection) { c.validateErr = errors.New("secret-return") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := newFakeRunnerSessionSnapshotConnection()
			snapshot, err := beginRunnerSessionProjectionSnapshot(context.Background(), connection, AuthorityPhaseMigrationRole)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(connection)
			err = snapshot.RollbackAndReturnToRunner(context.Background())
			if !IsCode(err, CodeProjectionSnapshotInvalid) || containsErrorText(err, "secret-") || connection.invalidateCalls != 1 || connection.returnCalls != 1 || connection.releaseCalls != 0 {
				t.Fatalf("runner snapshot close fault returned unsafe connection: err=%v connection=%+v", err, connection)
			}
		})
	}
}

func TestOwnedIdleSnapshotSuccessAndReuse(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin idle snapshot: %v", err)
	}
	metadata := snapshot.Metadata()
	if metadata.Mode != IdleReadSnapshot || metadata.Ownership != OwnedIdleSnapshot || metadata.IsolationLevel != "repeatable_read" || metadata.AccessMode != "read_only" || metadata.Deferrable || metadata.TxStatus != "T" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if err := snapshot.RollbackAndRelease(context.Background()); err != nil {
		t.Fatalf("close idle snapshot: %v", err)
	}
	if err := snapshot.RollbackAndRelease(context.Background()); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if connection.sanitizeCalls != 2 || connection.setMigrationRoleCalls != 0 || connection.rollbackCalls != 1 || connection.releaseCalls != 1 || connection.hijackCalls != 0 {
		t.Fatalf("unexpected lifecycle: sanitize=%d role=%d rollback=%d release=%d hijack=%d", connection.sanitizeCalls, connection.setMigrationRoleCalls, connection.rollbackCalls, connection.releaseCalls, connection.hijackCalls)
	}
	if _, err := snapshot.queryProjection(context.Background(), projectionQueryCapability); !IsCode(err, CodeProjectionSnapshotInvalid) {
		t.Fatalf("closed snapshot query error = %v", err)
	}
}

func TestOwnedIdleSnapshotSanitizesPreexistingAndPostSnapshotSessionPollution(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	connection.currentRole = "attacker"
	connection.searchPath = "attacker,public"
	connection.statementTimeout = "0"
	connection.lockTimeout = "0"
	connection.idleTimeout = "0"
	connection.preparedCount = 3

	snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin sanitized snapshot: %v", err)
	}
	if connection.sanitizeCalls != 1 || connection.currentRole != connection.sessionUser || connection.searchPath != connection.baselineSearchPath || connection.preparedCount != 0 {
		t.Fatalf("preexisting session pollution reached snapshot: %#v", connection)
	}
	if snapshot.Metadata().CurrentUser != connection.sessionUser {
		t.Fatalf("sanitized current_user = %q", snapshot.Metadata().CurrentUser)
	}

	// Model pollution appearing after transaction completion. The owner must
	// still scrub it before returning the physical connection to the pool.
	connection.afterRollback = func() {
		connection.currentRole = "attacker"
		connection.searchPath = "attacker,public"
		connection.statementTimeout = "91s"
		connection.preparedCount = 1
	}
	if err := snapshot.RollbackAndRelease(context.Background()); err != nil {
		t.Fatalf("close sanitized snapshot: %v", err)
	}
	if connection.sanitizeCalls != 2 || connection.currentRole != connection.sessionUser || connection.searchPath != connection.baselineSearchPath || connection.statementTimeout != connection.baselineStatementTimeout || connection.preparedCount != 0 || connection.releaseCalls != 1 {
		t.Fatalf("session pollution reached next borrower: %#v", connection)
	}
}

func TestOwnedIdleSnapshotSanitationFailureHijacksAndCloses(t *testing.T) {
	for _, failCall := range []int{1, 2} {
		t.Run(string(rune('0'+failCall)), func(t *testing.T) {
			connection := newFakeIdleSnapshotConnection()
			connection.sanitizeErrAtCall = map[int]error{failCall: errors.New("secret-session-state")}
			snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
			if failCall == 1 {
				if !IsCode(err, CodeProjectionSnapshotInvalid) || snapshot != nil || containsErrorText(err, "secret-session-state") {
					t.Fatalf("pre-begin sanitation error = %v snapshot=%v", err, snapshot)
				}
			} else {
				if err != nil {
					t.Fatalf("begin snapshot: %v", err)
				}
				err = snapshot.RollbackAndRelease(context.Background())
				if !IsCode(err, CodeProjectionSnapshotInvalid) || containsErrorText(err, "secret-session-state") {
					t.Fatalf("post-rollback sanitation error = %v", err)
				}
			}
			if connection.releaseCalls != 0 || connection.hijackCalls != 1 || connection.closeCalls != 1 {
				t.Fatalf("unsafe sanitation lifecycle: release=%d hijack=%d close=%d", connection.releaseCalls, connection.hijackCalls, connection.closeCalls)
			}
		})
	}
}

func TestOwnedMigrationRoleSnapshotUsesFixedRoleAfterSanitationAndResetsOnRelease(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	connection.currentRole = "attacker"
	snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseMigrationRole)
	if err != nil {
		t.Fatalf("begin migration role snapshot: %v", err)
	}
	metadata := snapshot.Metadata()
	if metadata.AuthorityPhase != AuthorityPhaseMigrationRole || metadata.SessionUser != connection.sessionUser || metadata.CurrentUser != MigrationOwnerRole {
		t.Fatalf("migration role metadata = %#v", metadata)
	}
	if connection.setMigrationRoleCalls != 1 || !reflect.DeepEqual(connection.lifecycle[:3], []string{"sanitize", "set_migration_role", "begin"}) {
		t.Fatalf("migration role lifecycle = %#v", connection.lifecycle)
	}
	if err := snapshot.RollbackAndRelease(context.Background()); err != nil {
		t.Fatalf("release migration role snapshot: %v", err)
	}
	if connection.currentRole != connection.sessionUser || connection.sanitizeCalls != 2 || connection.releaseCalls != 1 {
		t.Fatalf("migration role leaked to next borrower: %#v", connection)
	}
}

func TestOwnedMigrationRoleFailureHijacksAndCloses(t *testing.T) {
	for name, mutate := range map[string]func(*fakeIdleSnapshotConnection){
		"set-role": func(connection *fakeIdleSnapshotConnection) {
			connection.setMigrationRoleErr = errors.New("secret-role-error")
		},
		"status": func(connection *fakeIdleSnapshotConnection) { connection.setMigrationRoleStatus = '?' },
	} {
		t.Run(name, func(t *testing.T) {
			connection := newFakeIdleSnapshotConnection()
			mutate(connection)
			snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseMigrationRole)
			if snapshot != nil || !IsCode(err, CodeProjectionSnapshotInvalid) || containsErrorText(err, "secret-role-error") {
				t.Fatalf("migration role failure: snapshot=%v err=%v", snapshot, err)
			}
			if connection.beginCalls != 0 || connection.releaseCalls != 0 || connection.hijackCalls != 1 || connection.closeCalls != 1 {
				t.Fatalf("unsafe migration role failure lifecycle: %#v", connection)
			}
		})
	}
}

func TestOwnedIdleSnapshotOnlyAcceptsClosedOwnedPhases(t *testing.T) {
	for _, phase := range []AuthorityPhase{AuthorityPhaseMigrationTransaction, "caller_supplied_role"} {
		connection := newFakeIdleSnapshotConnection()
		snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, phase)
		if snapshot != nil || !IsCode(err, CodeProjectionMetadataMismatch) {
			t.Fatalf("phase %q: snapshot=%v err=%v", phase, snapshot, err)
		}
		if connection.sanitizeCalls != 0 || connection.setMigrationRoleCalls != 0 || connection.beginCalls != 0 {
			t.Fatalf("invalid phase touched connection lifecycle: %#v", connection)
		}
	}
}

func TestOwnedIdleSnapshotRejectsCanceledReadbackAndCleansUp(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	connection.rowErr = context.Canceled
	_, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if !IsCode(err, CodeProjectionCatalogQueryFailed) {
		t.Fatalf("error = %v", err)
	}
	if connection.rollbackCalls != 1 || connection.releaseCalls != 1 || connection.hijackCalls != 0 {
		t.Fatalf("unexpected cleanup: rollback=%d release=%d hijack=%d", connection.rollbackCalls, connection.releaseCalls, connection.hijackCalls)
	}
	if errors.Is(err, context.Canceled) || containsErrorText(err, "secret-driver-message") {
		t.Fatalf("raw cause leaked: %v", err)
	}
}

func TestOwnedIdleSnapshotRollbackFailureHijacksAndCloses(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	connection.rollbackErr = errors.New("secret-driver-message")
	snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin idle snapshot: %v", err)
	}
	err = snapshot.RollbackAndRelease(context.Background())
	if !IsCode(err, CodeProjectionSnapshotInvalid) || containsErrorText(err, "secret-driver-message") {
		t.Fatalf("rollback error = %v", err)
	}
	if connection.releaseCalls != 0 || connection.hijackCalls != 1 || connection.closeCalls != 1 {
		t.Fatalf("unsafe connection lifecycle: release=%d hijack=%d close=%d", connection.releaseCalls, connection.hijackCalls, connection.closeCalls)
	}
}

func TestOwnedIdleSnapshotCleanupUsesBoundedContextAfterCallerCancellation(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin idle snapshot: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := snapshot.RollbackAndRelease(canceled); err != nil {
		t.Fatalf("cleanup with canceled caller context: %v", err)
	}
	if connection.rollbackCalls != 1 || connection.sanitizeCalls != 2 || connection.releaseCalls != 1 || connection.hijackCalls != 0 {
		t.Fatalf("unexpected canceled cleanup lifecycle: %#v", connection)
	}
}

func TestOwnedIdleSnapshotUnknownStatusNeverReturnsToPool(t *testing.T) {
	for _, status := range []byte{'?', 'T'} {
		t.Run(string([]byte{status}), func(t *testing.T) {
			connection := newFakeIdleSnapshotConnection()
			connection.status = status
			_, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
			if !IsCode(err, CodeProjectionSnapshotInvalid) {
				t.Fatalf("error = %v", err)
			}
			if connection.beginCalls != 0 || connection.releaseCalls != 0 || connection.hijackCalls != 1 || connection.closeCalls != 1 {
				t.Fatalf("unsafe lifecycle: begin=%d release=%d hijack=%d close=%d", connection.beginCalls, connection.releaseCalls, connection.hijackCalls, connection.closeCalls)
			}
		})
	}
}

func TestOwnedIdleSnapshotPostRollbackUnknownStatusHijacks(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	connection.rollbackStatus = '?'
	snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin idle snapshot: %v", err)
	}
	err = snapshot.RollbackAndRelease(context.Background())
	if !IsCode(err, CodeProjectionSnapshotInvalid) {
		t.Fatalf("error = %v", err)
	}
	if connection.releaseCalls != 0 || connection.hijackCalls != 1 || connection.closeCalls != 1 {
		t.Fatalf("unsafe lifecycle: release=%d hijack=%d close=%d", connection.releaseCalls, connection.hijackCalls, connection.closeCalls)
	}
}

func TestOwnedIdleSnapshotUnknownStatusBeforeRollbackHijacks(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	snapshot, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin idle snapshot: %v", err)
	}
	connection.status = '?'
	err = snapshot.RollbackAndRelease(context.Background())
	if !IsCode(err, CodeProjectionSnapshotInvalid) {
		t.Fatalf("error = %v", err)
	}
	if connection.rollbackCalls != 0 || connection.releaseCalls != 0 || connection.hijackCalls != 1 || connection.closeCalls != 1 {
		t.Fatalf("unsafe lifecycle: rollback=%d release=%d hijack=%d close=%d", connection.rollbackCalls, connection.releaseCalls, connection.hijackCalls, connection.closeCalls)
	}
}

func TestOwnedIdleSnapshotRejectsStatusChangeAfterReadback(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	connection.afterMetadataScan = func() { connection.status = '?' }
	_, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if !IsCode(err, CodeProjectionSnapshotInvalid) {
		t.Fatalf("error = %v", err)
	}
	if connection.rollbackCalls != 0 || connection.releaseCalls != 0 || connection.hijackCalls != 1 || connection.closeCalls != 1 {
		t.Fatalf("unsafe lifecycle: rollback=%d release=%d hijack=%d close=%d", connection.rollbackCalls, connection.releaseCalls, connection.hijackCalls, connection.closeCalls)
	}
}

func TestOwnedIdleSnapshotReadbackMismatchRollsBackBeforeRelease(t *testing.T) {
	connection := newFakeIdleSnapshotConnection()
	connection.metadata[5] = "off"
	_, err := beginIdleReadSnapshot(context.Background(), fakeIdleSnapshotPool{connection: connection}, AuthorityPhaseConnectedSession)
	if !IsCode(err, CodeProjectionSnapshotInvalid) {
		t.Fatalf("error = %v", err)
	}
	if connection.rollbackCalls != 1 || connection.releaseCalls != 1 || connection.hijackCalls != 0 {
		t.Fatalf("unexpected cleanup: rollback=%d release=%d hijack=%d", connection.rollbackCalls, connection.releaseCalls, connection.hijackCalls)
	}
}

func TestBorrowedMigrationSnapshotHasNoOwnership(t *testing.T) {
	transaction := newFakeMigrationSnapshotTransaction()
	transaction.sessionPollution = "keep-borrowed-session-state"
	statementIndex := uint32(3)
	snapshot, err := BorrowMigrationProjectionSnapshot(context.Background(), transaction, "000001", &statementIndex)
	if err != nil {
		t.Fatalf("borrow migration snapshot: %v", err)
	}
	if _, owns := snapshot.(IdleProjectionSnapshot); owns {
		t.Fatal("borrowed transaction exposed idle snapshot ownership")
	}
	metadata := snapshot.Metadata()
	if metadata.Mode != MigrationSnapshot || metadata.Ownership != BorrowedMigrationSnapshot || metadata.IsolationLevel != "serializable" || metadata.AccessMode != "read_write" || metadata.MigrationID == nil || *metadata.MigrationID != "000001" || metadata.StatementIndex == nil || *metadata.StatementIndex != 3 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	statementIndex = 9
	if got := *snapshot.Metadata().StatementIndex; got != 3 {
		t.Fatalf("statement index alias leaked: %d", got)
	}
	if transaction.beginCalls != 0 || transaction.execCalls != 0 || transaction.commitCalls != 0 || transaction.rollbackCalls != 0 || transaction.boundaryCalls != 0 {
		t.Fatalf("borrowed snapshot changed lifecycle: %#v", transaction)
	}
	if transaction.sessionPollution != "keep-borrowed-session-state" {
		t.Fatalf("borrowed snapshot sanitized caller-owned session: %q", transaction.sessionPollution)
	}
}

func TestBorrowedMigrationSnapshotRejectsInvalidBoundaryWithoutOwnership(t *testing.T) {
	for name, mutate := range map[string]func(*fakeMigrationSnapshotTransaction){
		"unknown-status":  func(transaction *fakeMigrationSnapshotTransaction) { transaction.status = '?' },
		"wrong-isolation": func(transaction *fakeMigrationSnapshotTransaction) { transaction.metadata[4] = "repeatable read" },
		"read-only":       func(transaction *fakeMigrationSnapshotTransaction) { transaction.metadata[5] = "on" },
		"deferrable":      func(transaction *fakeMigrationSnapshotTransaction) { transaction.metadata[6] = "on" },
		"timeout-profile": func(transaction *fakeMigrationSnapshotTransaction) { transaction.metadata[7] = int64(5001) },
	} {
		t.Run(name, func(t *testing.T) {
			transaction := newFakeMigrationSnapshotTransaction()
			mutate(transaction)
			_, err := BorrowMigrationProjectionSnapshot(context.Background(), transaction, "000001", nil)
			if !IsCode(err, CodeProjectionSnapshotInvalid) {
				t.Fatalf("error = %v", err)
			}
			if transaction.execCalls != 0 || transaction.commitCalls != 0 || transaction.rollbackCalls != 0 || transaction.boundaryCalls != 0 {
				t.Fatalf("borrowed snapshot took ownership: %#v", transaction)
			}
		})
	}
}

func TestBorrowedMigrationSnapshotRejectsStatusChangeAfterReadback(t *testing.T) {
	transaction := newFakeMigrationSnapshotTransaction()
	transaction.afterMetadataScan = func() { transaction.status = '?' }
	_, err := BorrowMigrationProjectionSnapshot(context.Background(), transaction, "000001", nil)
	if !IsCode(err, CodeProjectionSnapshotInvalid) {
		t.Fatalf("error = %v", err)
	}
	if transaction.execCalls != 0 || transaction.commitCalls != 0 || transaction.rollbackCalls != 0 || transaction.boundaryCalls != 0 {
		t.Fatalf("borrowed snapshot took ownership: %#v", transaction)
	}
}

func TestProjectionBoundsCannotBeOverridden(t *testing.T) {
	bounds := FixedProjectionBounds()
	if err := validateProjectionBounds(bounds); err != nil {
		t.Fatalf("fixed bounds: %v", err)
	}
	bounds.MaxQueryRows++
	if err := validateProjectionBounds(bounds); !IsCode(err, CodeProjectionLimitOverride) {
		t.Fatalf("override error = %v", err)
	}
}

func TestProjectionSnapshotRejectsLifecycleQueryIDs(t *testing.T) {
	snapshot := &fixedQueryProjectionSnapshot{queryer: &fixedProjectionRowsQueryer{}, metadata: validIdleSnapshotMetadata(), started: time.Now()}
	for _, id := range []projectionQueryID{
		projectionQuerySnapshotMetadata,
		projectionQuerySnapshotConfigure,
		projectionQuerySnapshotReset,
		projectionQuerySnapshotSanitation,
		projectionQuerySnapshotSetMigrationRole,
		projectionQuerySnapshotRoleReadback,
	} {
		if _, err := snapshot.queryProjection(context.Background(), id); !IsCode(err, CodeProjectionCatalogQueryFailed) {
			t.Fatalf("lifecycle query %d escaped sealed snapshot: %v", id, err)
		}
		if err := snapshot.queryProjectionRow(context.Background(), id).Scan(); !IsCode(err, CodeProjectionCatalogQueryFailed) {
			t.Fatalf("lifecycle row query %d escaped sealed snapshot: %v", id, err)
		}
	}
}

func TestProjectionSnapshotInclusiveRowByteAndQueryLimits(t *testing.T) {
	// A one-column canonical row is ["<value>"], four bytes larger than
	// an unescaped ASCII value.
	queryer := &fixedProjectionRowsQueryer{rowsPerQuery: 1, value: strings.Repeat("x", int(projectionMaxRowBytes-4))}
	snapshot := &fixedQueryProjectionSnapshot{queryer: queryer, metadata: validIdleSnapshotMetadata(), started: time.Now()}
	for queryIndex := uint32(0); queryIndex < projectionMaxQueriesPerProjection; queryIndex++ {
		rows, err := snapshot.queryProjection(context.Background(), projectionQueryCapability)
		if err != nil {
			t.Fatalf("query %d: %v", queryIndex, err)
		}
		if !rows.Next() {
			t.Fatalf("query %d returned no row: %v", queryIndex, rows.Err())
		}
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatalf("query %d scan: %v", queryIndex, err)
		}
		if rows.Next() || rows.Err() != nil {
			t.Fatalf("query %d EOF: %v", queryIndex, rows.Err())
		}
		rows.Close()
	}
	stats := snapshot.projectionStats()
	if stats.QueryCount != projectionMaxQueriesPerProjection || stats.RowCount != uint64(projectionMaxQueriesPerProjection) || stats.TotalBytes != projectionMaxTotalResultBytes {
		t.Fatalf("inclusive stats = %#v", stats)
	}
	if _, err := snapshot.queryProjection(context.Background(), projectionQueryCapability); !IsCode(err, CodeProjectionLimitExceeded) {
		t.Fatalf("query max+1 error = %v", err)
	}

	over := &fixedQueryProjectionSnapshot{
		queryer:  &fixedProjectionRowsQueryer{rowsPerQuery: 1, value: strings.Repeat("x", int(projectionMaxRowBytes-3))},
		metadata: validIdleSnapshotMetadata(), started: time.Now(),
	}
	rows, err := over.queryProjection(context.Background(), projectionQueryCapability)
	if err != nil {
		t.Fatalf("oversize query: %v", err)
	}
	if !rows.Next() {
		t.Fatalf("oversize query returned no row: %v", rows.Err())
	}
	var value string
	if err := rows.Scan(&value); !IsCode(err, CodeProjectionLimitExceeded) {
		t.Fatalf("row max+1 error = %v", err)
	}
	rows.Close()
}

func TestProjectionSnapshotInclusiveRowsRequireEOFProbe(t *testing.T) {
	for name, count := range map[string]uint64{"exact": projectionMaxQueryRows, "over": projectionMaxQueryRows + 1} {
		t.Run(name, func(t *testing.T) {
			queryer := &fixedProjectionRowsQueryer{rowsPerQuery: count, value: "x"}
			snapshot := &fixedQueryProjectionSnapshot{queryer: queryer, metadata: validIdleSnapshotMetadata(), started: time.Now()}
			rows, err := snapshot.queryProjection(context.Background(), projectionQueryCapability)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			var observed uint64
			for rows.Next() {
				var value string
				if err := rows.Scan(&value); err != nil {
					t.Fatalf("scan row %d: %v", observed, err)
				}
				observed++
			}
			if observed != projectionMaxQueryRows {
				t.Fatalf("observed rows = %d", observed)
			}
			if name == "exact" && rows.Err() != nil {
				t.Fatalf("exact max error = %v", rows.Err())
			}
			if name == "over" && !IsCode(rows.Err(), CodeProjectionLimitExceeded) {
				t.Fatalf("max+1 error = %v", rows.Err())
			}
			rows.Close()
		})
	}
}

func TestProjectionErrorsAreBoundedAndRedacted(t *testing.T) {
	err := projectionFailure(CodeProjectionCatalogQueryFailed, "bad phase with spaces", "password=secret", 17, false, "query failed")
	var projectionErr *ProjectionError
	if !errors.As(err, &projectionErr) {
		t.Fatalf("error type = %T", err)
	}
	if projectionErr.Phase != "projection" || projectionErr.Path != "unknown" || containsErrorText(err, "password") || !IsCode(err, CodeProjectionCatalogQueryFailed) {
		t.Fatalf("unbounded error = %v", err)
	}
}

func TestProjectionMetadataUsesExactNullableScopeShape(t *testing.T) {
	snapshot := validIdleSnapshotMetadata()
	metadata := ProjectionMetadata{
		ProjectionKind: ProjectionKindAuthority, DigestDomain: AuthorityProjectionDigestDomain,
		AdapterProfile: PostgreSQLAuthorityAdapter, Snapshot: snapshot,
		VerifiedSubjectDigest: projectionSnapshotTestDigest, Scope: nil,
		LimitsProfile: ProjectionLimitsProfile, RedactionProfile: ProjectionRedactionProfile,
	}
	if err := metadata.validate(); err != nil {
		t.Fatalf("authority metadata: %v", err)
	}
	invalidDigest := metadata
	invalidDigest.VerifiedSubjectDigest = "sha256:not-a-digest"
	if err := invalidDigest.validate(); !IsCode(err, CodeInvalidManifest) {
		t.Fatalf("invalid subject digest mapping = %v", err)
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if !stringContains(string(raw), `"scope":null`) || stringContains(string(raw), "database_identity") {
		t.Fatalf("metadata shape = %s", raw)
	}

	head := "000001"
	finalScope := ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: []ObjectIdentityProjection{}}
	metadata.ProjectionKind = ProjectionKindCatalog
	metadata.DigestDomain = CatalogProjectionDigestDomain
	metadata.AdapterProfile = PostgreSQLCatalogAdapter
	metadata.Scope = &finalScope
	if err := metadata.validate(); err != nil {
		t.Fatalf("catalog metadata: %v", err)
	}
	predecessorMigration := "000001"
	predecessor := ProjectionScope{ScopeKind: "predecessor", MigrationID: &predecessorMigration, DeclaredObjects: []ObjectIdentityProjection{}}
	metadata.Scope = &predecessor
	if err := metadata.validate(); !IsCode(err, CodeProjectionMetadataMismatch) {
		t.Fatalf("non-final catalog scope error = %v", err)
	}
}

func TestVerifiedCatalogContractBindsExpectedSchemaHeadToScope(t *testing.T) {
	head := "000001"
	scope := ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: []ObjectIdentityProjection{}}
	expected := CatalogProjection{SchemaHead: head, Body: projectionSnapshotTestPrecondition(ProjectionScope{ScopeKind: "predecessor", MigrationID: &head, DeclaredObjects: []ObjectIdentityProjection{}}).AcceptedStates[1].Present.Body}
	contract, err := bindVerifiedCatalogContract(projectionSnapshotTestDigest, scope, expected, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("bind catalog contract: %v", err)
	}
	if err := contract.validate(); err != nil {
		t.Fatalf("matching schema head: %v", err)
	}
	mismatchedExpected := cloneProjectionValue(expected)
	mismatchedExpected.SchemaHead = "000002"
	if _, err := bindVerifiedCatalogContract(projectionSnapshotTestDigest, scope, mismatchedExpected, time.Now().Add(time.Hour), 1); !IsCode(err, CodeProjectionMetadataMismatch) {
		t.Fatalf("schema head mismatch error = %v", err)
	}
	scope.DeclaredObjects = append(scope.DeclaredObjects, ObjectIdentityProjection{Schema: &SchemaObjectIdentity{Kind: "schema", Name: "attacker"}})
	expected.Body.Schema.Owner = RuntimeRole
	if contract.Scope().DeclaredObjects != nil && len(contract.Scope().DeclaredObjects) != 0 || contract.ExpectedProjection().Body.Schema.Owner != MigrationOwnerRole {
		t.Fatal("catalog constructor retained caller-owned scope or expected projection")
	}
	for name, mutate := range map[string]func(*VerifiedCatalogContract){
		"missing": func(value *VerifiedCatalogContract) { *value = VerifiedCatalogContract{} },
		"expired": func(value *VerifiedCatalogContract) { value.verifiedDecisionExpiresAt = time.Now().Add(-time.Second) },
		"epoch0":  func(value *VerifiedCatalogContract) { value.verifiedDecisionSecurityEpoch = 0 },
		"decision-drift": func(value *VerifiedCatalogContract) {
			value.verifiedDecisionExpiresAt = value.verifiedDecisionExpiresAt.Add(time.Hour)
		},
		"drift": func(value *VerifiedCatalogContract) { value.expected.Body.Schema.Owner = RuntimeRole },
	} {
		t.Run(name, func(t *testing.T) {
			changed := contract
			mutate(&changed)
			if err := changed.validate(); !IsCode(err, CodeUntrusted) {
				t.Fatalf("catalog trust error = %v", err)
			}
		})
	}
}

func TestVerifiedSchemaBundleScopeOwnerClosureIsSignedAndDefensive(t *testing.T) {
	migrationID := "000001"
	projectionScope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: []ObjectIdentityProjection{}}
	condition := projectionSnapshotTestPrecondition(projectionScope)
	owners := []string{MigrationOwnerRole}
	creators := []string{MigrationOwnerRole}
	verified, err := bindVerifiedSchemaBundleScope(projectionSnapshotTestDigest, projectionScope, condition, owners, creators, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("bind signed precondition: %v", err)
	}
	exactCondition := cloneProjectionValue(condition)
	condition.AcceptedStates[0].Absent.Schema = "attacker"
	owners[0] = "attacker"
	creators[0] = "attacker"
	if err := verified.validate(); err != nil {
		t.Fatalf("valid signed owner closure: %v", err)
	}
	ownerCopy := verified.DefaultACLOwners()
	ownerCopy[0] = "attacker"
	if got := verified.DefaultACLOwners()[0]; got != MigrationOwnerRole {
		t.Fatalf("owner accessor leaked mutable storage: %q", got)
	}

	drift := verified
	drift.objectCreatorClosure = []string{RuntimeRole}
	if err := drift.validate(); !IsCode(err, CodeAuthorityDrift) {
		t.Fatalf("owner subset error = %v", err)
	}
	unsorted := verified
	unsorted.objectCreatorClosure = []string{RuntimeRole, MigrationOwnerRole}
	if err := unsorted.validate(); !IsCode(err, CodeInvalidManifest) {
		t.Fatalf("unsorted closure error = %v", err)
	}

	bound := verified.BoundPrecondition()
	bound.AcceptedStates[0].Absent.Schema = "attacker"
	if got := verified.BoundPrecondition().AcceptedStates[0].Absent.Schema; got != projectionTargetSchema {
		t.Fatalf("bound precondition accessor leaked mutable storage: %q", got)
	}
	if err := verified.validatePrecondition(exactCondition); err != nil {
		t.Fatalf("exact caller condition: %v", err)
	}
	different := cloneProjectionValue(exactCondition)
	different.AcceptedStates[1].Present.Body.Schema.Owner = RuntimeRole
	if err := verified.validatePrecondition(different); !IsCode(err, CodeInvalidManifest) {
		t.Fatalf("different caller condition error = %v", err)
	}
	if err := verified.validatePreconditionAt(exactCondition, time.Now().Add(2*time.Hour)); !IsCode(err, CodeUntrusted) {
		t.Fatalf("expired decision error = %v", err)
	}
	zeroEpoch := verified
	zeroEpoch.verifiedDecisionSecurityEpoch = 0
	if err := zeroEpoch.validatePrecondition(exactCondition); !IsCode(err, CodeUntrusted) {
		t.Fatalf("zero epoch error = %v", err)
	}
	internalDrift := verified
	internalDrift.defaultACLOwners = []string{}
	if err := internalDrift.validatePrecondition(exactCondition); !IsCode(err, CodeUntrusted) {
		t.Fatalf("canonical binding drift error = %v", err)
	}
}

func projectionSnapshotTestPrecondition(scope ProjectionScope) CatalogPrecondition {
	body := CatalogProjectionBody{
		Schema: SchemaProjection{
			Name: projectionTargetSchema, Owner: MigrationOwnerRole,
			ExplicitACL: ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}},
			EffectiveACL: []ACLProjection{{
				Grantor: MigrationOwnerRole, Grantee: MigrationOwnerRole,
				Privileges: []string{"CREATE", "USAGE"}, Grantable: []string{"CREATE", "USAGE"}, Origin: "owner_implicit",
			}},
			SecurityLabels: []SecurityLabel{},
		},
		DefaultACL: []DefaultACLProjection{}, Relations: []RelationProjection{}, Functions: []FunctionProjection{},
		Dependencies: []DependencyProjection{}, DeclaredObjects: []ObjectIdentityProjection{}, DeniedObjects: []DeniedObjectProjection{},
	}
	return CatalogPrecondition{AcceptedStates: []CatalogStateProjection{
		{Absent: &SchemaAbsentProjection{State: "schema_absent", Scope: scope, Schema: projectionTargetSchema}},
		{Present: &SchemaPresentProjection{State: "schema_present", Scope: scope, Body: body}},
	}}
}

func validIdleSnapshotMetadata() SnapshotMetadata {
	return SnapshotMetadata{
		Mode: IdleReadSnapshot, Ownership: OwnedIdleSnapshot,
		PostgresMajor: 17, ServerVersionNum: 170006, DatabaseName: "cloud_agents",
		AuthorityPhase: AuthorityPhaseConnectedSession, SessionUser: "projection_login", CurrentUser: "projection_login",
		IsolationLevel: "repeatable_read", AccessMode: "read_only", Deferrable: false, TxStatus: "T",
	}
}

func containsErrorText(err error, text string) bool {
	return err != nil && text != "" && len(err.Error()) >= len(text) && stringContains(err.Error(), text)
}

func stringContains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}

type fakeIdleSnapshotPool struct {
	connection *fakeIdleSnapshotConnection
	err        error
}

type fakeRunnerSessionSnapshotConnection struct {
	*fakeIdleSnapshotConnection
	prepareErr      error
	validateErr     error
	prepareCalls    int
	validateCalls   int
	invalidateCalls int
	returnCalls     int
}

func newFakeRunnerSessionSnapshotConnection() *fakeRunnerSessionSnapshotConnection {
	return &fakeRunnerSessionSnapshotConnection{fakeIdleSnapshotConnection: newFakeIdleSnapshotConnection()}
}

func (connection *fakeRunnerSessionSnapshotConnection) prepare(ctx context.Context, phase AuthorityPhase) error {
	connection.prepareCalls++
	connection.lifecycle = append(connection.lifecycle, "runner_prepare_"+string(phase))
	if err := ctx.Err(); err != nil {
		return err
	}
	if connection.prepareErr != nil {
		return connection.prepareErr
	}
	switch phase {
	case AuthorityPhaseConnectedSession:
		connection.currentRole = connection.sessionUser
	case AuthorityPhaseMigrationRole:
		connection.currentRole = MigrationOwnerRole
	default:
		return errors.New("unknown phase")
	}
	connection.metadata[3] = connection.currentRole
	return nil
}

func (connection *fakeRunnerSessionSnapshotConnection) validateReturn(context.Context, AuthorityPhase) error {
	connection.validateCalls++
	return connection.validateErr
}

func (connection *fakeRunnerSessionSnapshotConnection) invalidate(context.Context) {
	connection.invalidateCalls++
}

func (connection *fakeRunnerSessionSnapshotConnection) returnToRunner() {
	connection.returnCalls++
}

func (pool fakeIdleSnapshotPool) acquire(context.Context) (idleSnapshotConnection, error) {
	if pool.err != nil {
		return nil, pool.err
	}
	return pool.connection, nil
}

type fakeIdleSnapshotConnection struct {
	status                   byte
	beginStatus              byte
	rollbackStatus           byte
	metadata                 []any
	sessionUser              string
	currentRole              string
	searchPath               string
	statementTimeout         string
	lockTimeout              string
	idleTimeout              string
	baselineSearchPath       string
	baselineStatementTimeout string
	baselineLockTimeout      string
	baselineIdleTimeout      string
	preparedCount            int64
	rowErr                   error
	beginErr                 error
	rollbackErr              error
	sanitizeErrAtCall        map[int]error
	sanitizeCalls            int
	setMigrationRoleErr      error
	setMigrationRoleStatus   byte
	setMigrationRoleCalls    int
	beginCalls               int
	rollbackCalls            int
	releaseCalls             int
	hijackCalls              int
	closeCalls               int
	afterMetadataScan        func()
	afterRollback            func()
	lifecycle                []string
}

func newFakeIdleSnapshotConnection() *fakeIdleSnapshotConnection {
	return &fakeIdleSnapshotConnection{
		status: 'I', beginStatus: 'T', rollbackStatus: 'I',
		metadata:    []any{"170006", "cloud_agents", "projection_login", "projection_login", "repeatable read", "on", "off", int64(5000), int64(1000), int64(60000)},
		sessionUser: "projection_login", currentRole: "projection_login",
		searchPath: "\"$user\", public", statementTimeout: "0", lockTimeout: "0", idleTimeout: "0",
		baselineSearchPath: "\"$user\", public", baselineStatementTimeout: "0", baselineLockTimeout: "0", baselineIdleTimeout: "0",
	}
}

func (connection *fakeIdleSnapshotConnection) Query(context.Context, string, ...any) (Rows, error) {
	return &fakeProjectionRows{}, nil
}
func (connection *fakeIdleSnapshotConnection) QueryRow(ctx context.Context, query string, _ ...any) Row {
	if strings.Contains(query, "set_config") {
		connection.metadata[7] = int64(5000)
		connection.metadata[8] = int64(1000)
		connection.metadata[9] = int64(60000)
		return fakeProjectionRow{ctx: ctx, values: []any{"5000ms", "1000ms", "60000ms"}}
	}
	return fakeProjectionRow{ctx: ctx, values: connection.metadata, err: connection.rowErr, afterScan: connection.afterMetadataScan}
}
func (connection *fakeIdleSnapshotConnection) txStatus() byte { return connection.status }
func (connection *fakeIdleSnapshotConnection) sanitize(ctx context.Context) error {
	connection.sanitizeCalls++
	connection.lifecycle = append(connection.lifecycle, "sanitize")
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := connection.sanitizeErrAtCall[connection.sanitizeCalls]; err != nil {
		return err
	}
	connection.currentRole = connection.sessionUser
	connection.searchPath = connection.baselineSearchPath
	connection.statementTimeout = connection.baselineStatementTimeout
	connection.lockTimeout = connection.baselineLockTimeout
	connection.idleTimeout = connection.baselineIdleTimeout
	connection.preparedCount = 0
	connection.metadata[2] = connection.sessionUser
	connection.metadata[3] = connection.currentRole
	connection.metadata[7] = int64(0)
	connection.metadata[8] = int64(0)
	connection.metadata[9] = int64(0)
	return nil
}
func (connection *fakeIdleSnapshotConnection) setMigrationRole(ctx context.Context) error {
	connection.setMigrationRoleCalls++
	connection.lifecycle = append(connection.lifecycle, "set_migration_role")
	if err := ctx.Err(); err != nil {
		return err
	}
	if connection.setMigrationRoleErr != nil {
		return connection.setMigrationRoleErr
	}
	connection.currentRole = MigrationOwnerRole
	connection.metadata[3] = MigrationOwnerRole
	if connection.setMigrationRoleStatus != 0 {
		connection.status = connection.setMigrationRoleStatus
	}
	return nil
}
func (connection *fakeIdleSnapshotConnection) begin(context.Context) error {
	connection.beginCalls++
	connection.lifecycle = append(connection.lifecycle, "begin")
	if connection.beginErr == nil {
		connection.status = connection.beginStatus
	}
	return connection.beginErr
}
func (connection *fakeIdleSnapshotConnection) rollback(ctx context.Context) error {
	connection.rollbackCalls++
	connection.lifecycle = append(connection.lifecycle, "rollback")
	if err := ctx.Err(); err != nil {
		return err
	}
	if connection.rollbackErr == nil {
		connection.status = connection.rollbackStatus
		if connection.afterRollback != nil {
			connection.afterRollback()
		}
	}
	return connection.rollbackErr
}
func (connection *fakeIdleSnapshotConnection) release() { connection.releaseCalls++ }
func (connection *fakeIdleSnapshotConnection) hijackAndClose(context.Context) {
	connection.hijackCalls++
	connection.closeCalls++
}

type fakeMigrationSnapshotTransaction struct {
	status            byte
	metadata          []any
	beginCalls        int
	execCalls         int
	commitCalls       int
	rollbackCalls     int
	boundaryCalls     int
	afterMetadataScan func()
	sessionPollution  string
}

func newFakeMigrationSnapshotTransaction() *fakeMigrationSnapshotTransaction {
	return &fakeMigrationSnapshotTransaction{
		status:   'T',
		metadata: []any{"170006", "cloud_agents", "migration_login", MigrationOwnerRole, "serializable", "off", "off", int64(5000), int64(1000), int64(60000)},
	}
}

func (transaction *fakeMigrationSnapshotTransaction) projectionTxStatus() byte {
	return transaction.status
}
func (transaction *fakeMigrationSnapshotTransaction) Query(context.Context, string, ...any) (Rows, error) {
	return &fakeProjectionRows{}, nil
}
func (transaction *fakeMigrationSnapshotTransaction) QueryRow(ctx context.Context, _ string, _ ...any) Row {
	return fakeProjectionRow{ctx: ctx, values: transaction.metadata, afterScan: transaction.afterMetadataScan}
}
func (transaction *fakeMigrationSnapshotTransaction) Exec(context.Context, string, ...any) (CommandTag, error) {
	transaction.execCalls++
	return fakeProjectionCommandTag(0), nil
}
func (transaction *fakeMigrationSnapshotTransaction) ExecuteStatement(context.Context, []byte) error {
	transaction.execCalls++
	return nil
}
func (transaction *fakeMigrationSnapshotTransaction) Boundary(context.Context, int64) (BoundaryState, error) {
	transaction.boundaryCalls++
	return BoundaryState{TxStatus: transaction.status}, nil
}
func (transaction *fakeMigrationSnapshotTransaction) Commit(context.Context) error {
	transaction.commitCalls++
	return nil
}
func (transaction *fakeMigrationSnapshotTransaction) Rollback(context.Context) error {
	transaction.rollbackCalls++
	return nil
}

type fakeProjectionCommandTag int64

func (tag fakeProjectionCommandTag) RowsAffected() int64 { return int64(tag) }

type fakeProjectionRow struct {
	ctx       context.Context
	values    []any
	err       error
	afterScan func()
}

func (row fakeProjectionRow) Scan(targets ...any) error {
	if row.err != nil {
		return row.err
	}
	if err := row.ctx.Err(); err != nil {
		return err
	}
	if len(targets) != len(row.values) {
		return errors.New("scan target count differs")
	}
	for index := range targets {
		target := reflect.ValueOf(targets[index])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return errors.New("scan target is not a pointer")
		}
		value := reflect.ValueOf(row.values[index])
		if !value.Type().AssignableTo(target.Elem().Type()) {
			return errors.New("scan target type differs")
		}
		target.Elem().Set(value)
	}
	if row.afterScan != nil {
		row.afterScan()
	}
	return nil
}

type fakeProjectionRows struct{ closed bool }

func (*fakeProjectionRows) Next() bool        { return false }
func (*fakeProjectionRows) Scan(...any) error { return nil }
func (*fakeProjectionRows) Err() error        { return nil }
func (rows *fakeProjectionRows) Close()       { rows.closed = true }

type fixedProjectionRowsQueryer struct {
	rowsPerQuery uint64
	value        string
}

func (queryer *fixedProjectionRowsQueryer) Query(context.Context, string, ...any) (Rows, error) {
	return &fixedProjectionRows{remaining: queryer.rowsPerQuery, value: queryer.value}, nil
}

func (*fixedProjectionRowsQueryer) QueryRow(context.Context, string, ...any) Row {
	return fakeProjectionRow{ctx: context.Background(), err: errors.New("query row is unavailable")}
}

type fixedProjectionRows struct {
	remaining uint64
	value     string
	current   bool
}

func (rows *fixedProjectionRows) Next() bool {
	if rows.remaining == 0 {
		rows.current = false
		return false
	}
	rows.remaining--
	rows.current = true
	return true
}

func (rows *fixedProjectionRows) Scan(targets ...any) error {
	if !rows.current || len(targets) != 1 {
		return errors.New("fixed row is unavailable")
	}
	target, ok := targets[0].(*string)
	if !ok {
		return errors.New("fixed row target is invalid")
	}
	*target = rows.value
	rows.current = false
	return nil
}

func (*fixedProjectionRows) Err() error { return nil }
func (*fixedProjectionRows) Close()     {}
