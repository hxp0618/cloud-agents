package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
)

type runnerCommitProtocolOutcome string

const (
	runnerCommitProtocolCommitted runnerCommitProtocolOutcome = "committed"
	runnerCommitProtocolRejected  runnerCommitProtocolOutcome = "confirmed_rejected"
	runnerCommitProtocolAmbiguous runnerCommitProtocolOutcome = "ambiguous"
)

const (
	runnerCommitRejectedSerialization = "serialization_failure"
	runnerCommitRejectedDeadlock      = "deadlock_detected"
	runnerCommitRejectedOther         = "other_confirmed_postgres_error"
)

// runnerCommitProtocol is a package-private driver lifecycle seam. The claim
// must succeed exactly once before Commit is called, and status/closed are read
// only after Commit returns. It cannot append evidence or reconnect.
type runnerCommitProtocol interface {
	MigrationTransaction
	claimRunnerCommitProtocol() bool
	runnerCommitProtocolStatus() byte
	runnerCommitProtocolConnectionClosed() bool
	runnerCommitProtocolSealed()
}

type runnerCommitProtocolObservation struct {
	self             *runnerCommitProtocolObservation
	source           runnerCommitProtocol
	outcome          runnerCommitProtocolOutcome
	rejectionReason  string
	commitCalled     bool
	readyForQuery    bool
	connectionClosed bool
	canonical        [32]byte
	consumed         bool
}

type runnerCommitProtocolRegistryRecord struct {
	observation      *runnerCommitProtocolObservation
	source           runnerCommitProtocol
	outcome          runnerCommitProtocolOutcome
	rejectionReason  string
	commitCalled     bool
	readyForQuery    bool
	connectionClosed bool
	canonical        [32]byte
}

type runnerCommitProtocolFacts struct {
	outcome          runnerCommitProtocolOutcome
	rejectionReason  string
	commitCalled     bool
	readyForQuery    bool
	connectionClosed bool
}

var runnerCommitProtocolRegistry sync.Map

func invokeRunnerCommitProtocol(ctx context.Context, transaction MigrationTransaction) (*runnerCommitProtocolObservation, bool, error) {
	protocol, ok := transaction.(runnerCommitProtocol)
	if ctx == nil || !ok || protocol == nil || !runnerOwnedPointer(protocol) {
		return nil, false, fail(CodeTransactionBoundary, "runner-commit-protocol", "transaction commit protocol is unavailable", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, mapRunnerCommitProtocolPreflightError(err)
	}
	if !protocol.claimRunnerCommitProtocol() || protocol.runnerCommitProtocolStatus() != 'T' || protocol.runnerCommitProtocolConnectionClosed() {
		return nil, false, fail(CodeTransactionBoundary, "runner-commit-protocol", "transaction commit protocol is not at the exact open boundary", nil)
	}

	commitErr := protocol.Commit(ctx)
	status := protocol.runnerCommitProtocolStatus()
	connectionClosed := protocol.runnerCommitProtocolConnectionClosed()
	facts := classifyRunnerCommitProtocol(commitErr, status, connectionClosed)
	observation, err := sealRunnerCommitProtocolObservation(protocol, facts)
	return observation, true, err
}

func classifyRunnerCommitProtocol(err error, status byte, connectionClosed bool) runnerCommitProtocolFacts {
	facts := runnerCommitProtocolFacts{
		outcome: runnerCommitProtocolAmbiguous, commitCalled: true,
		readyForQuery: false, connectionClosed: connectionClosed,
	}
	if err == nil {
		if status == 'I' && !connectionClosed {
			facts.outcome = runnerCommitProtocolCommitted
			facts.readyForQuery = true
		}
		return facts
	}
	// Once Commit has been called, cancellation, deadlines, timeouts, EOF and
	// connection failures are always acknowledgement-unknown, even if a driver
	// happens to expose an idle-looking cached status.
	if runnerCommitProtocolAmbiguousError(err) || status != 'I' || connectionClosed {
		return facts
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError == nil || postgresError.Code == "" {
		return facts
	}
	facts.outcome = runnerCommitProtocolRejected
	facts.readyForQuery = true
	switch postgresError.Code {
	case "40001":
		facts.rejectionReason = runnerCommitRejectedSerialization
	case "40P01":
		facts.rejectionReason = runnerCommitRejectedDeadlock
	default:
		facts.rejectionReason = runnerCommitRejectedOther
	}
	return facts
}

func runnerCommitProtocolAmbiguousError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || pgconn.Timeout(err) || pgconn.SafeToRetry(err) {
		return true
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		if postgresError == nil {
			return true
		}
		class := ""
		if len(postgresError.Code) >= 2 {
			class = postgresError.Code[:2]
		}
		return class == "08" || class == "57" || postgresError.Code == "40003" || postgresError.Code == "57014" || strings.EqualFold(postgresError.Severity, "FATAL") || strings.EqualFold(postgresError.Severity, "PANIC")
	}
	var connectError *pgconn.ConnectError
	if errors.As(err, &connectError) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func sealRunnerCommitProtocolObservation(source runnerCommitProtocol, facts runnerCommitProtocolFacts) (*runnerCommitProtocolObservation, error) {
	if source == nil || !runnerOwnedPointer(source) || !validRunnerCommitProtocolFacts(facts) {
		return nil, fail(CodeTransactionBoundary, "runner-commit-protocol-seal", "transaction commit result is contradictory", nil)
	}
	observation := &runnerCommitProtocolObservation{
		source: source, outcome: facts.outcome, rejectionReason: facts.rejectionReason,
		commitCalled: facts.commitCalled, readyForQuery: facts.readyForQuery,
		connectionClosed: facts.connectionClosed,
	}
	observation.self = observation
	observation.canonical = runnerCommitProtocolObservationDigest(observation)
	if observation.canonical == ([32]byte{}) {
		return nil, fail(CodeTransactionBoundary, "runner-commit-protocol-seal", "transaction commit result could not be identified", nil)
	}
	runnerCommitProtocolRegistry.Store(observation, runnerCommitProtocolRegistryRecord{
		observation: observation, source: source, outcome: facts.outcome,
		rejectionReason: facts.rejectionReason, commitCalled: facts.commitCalled,
		readyForQuery: facts.readyForQuery, connectionClosed: facts.connectionClosed,
		canonical: observation.canonical,
	})
	if !validRunnerCommitProtocolObservation(observation) {
		runnerCommitProtocolRegistry.Delete(observation)
		return nil, fail(CodeTransactionBoundary, "runner-commit-protocol-seal", "transaction commit result could not be sealed", nil)
	}
	return observation, nil
}

func consumeRunnerCommitProtocolObservation(observation *runnerCommitProtocolObservation, source runnerCommitProtocol) (runnerCommitProtocolFacts, error) {
	if !validRunnerCommitProtocolObservation(observation) || source == nil || !sameRunnerOwnedPointer(observation.source, source) {
		return runnerCommitProtocolFacts{}, fail(CodeTransactionBoundary, "runner-commit-protocol-claim", "transaction commit result is unavailable or changed", nil)
	}
	registered, ok := runnerCommitProtocolRegistry.LoadAndDelete(observation)
	record, recordOK := registered.(runnerCommitProtocolRegistryRecord)
	if !ok || !recordOK || record.observation != observation || !sameRunnerOwnedPointer(record.source, source) || record.canonical != observation.canonical {
		return runnerCommitProtocolFacts{}, fail(CodeTransactionBoundary, "runner-commit-protocol-claim", "transaction commit result could not be consumed exactly once", nil)
	}
	facts := runnerCommitProtocolFacts{
		outcome: record.outcome, rejectionReason: record.rejectionReason,
		commitCalled: record.commitCalled, readyForQuery: record.readyForQuery,
		connectionClosed: record.connectionClosed,
	}
	if !validRunnerCommitProtocolFacts(facts) {
		return runnerCommitProtocolFacts{}, fail(CodeTransactionBoundary, "runner-commit-protocol-claim", "transaction commit result is contradictory", nil)
	}
	observation.consumed = true
	observation.source = nil
	return facts, nil
}

func validRunnerCommitProtocolObservation(observation *runnerCommitProtocolObservation) bool {
	if observation == nil || observation.self != observation || observation.consumed || observation.source == nil || observation.canonical == ([32]byte{}) || observation.canonical != runnerCommitProtocolObservationDigest(observation) {
		return false
	}
	registered, ok := runnerCommitProtocolRegistry.Load(observation)
	record, recordOK := registered.(runnerCommitProtocolRegistryRecord)
	return ok && recordOK && record.observation == observation && sameRunnerOwnedPointer(record.source, observation.source) && record.outcome == observation.outcome && record.rejectionReason == observation.rejectionReason && record.commitCalled == observation.commitCalled && record.readyForQuery == observation.readyForQuery && record.connectionClosed == observation.connectionClosed && record.canonical == observation.canonical
}

func validRunnerCommitProtocolFacts(facts runnerCommitProtocolFacts) bool {
	if !facts.commitCalled {
		return false
	}
	switch facts.outcome {
	case runnerCommitProtocolCommitted:
		return facts.rejectionReason == "" && facts.readyForQuery && !facts.connectionClosed
	case runnerCommitProtocolRejected:
		return stringIn(facts.rejectionReason, runnerCommitRejectedSerialization, runnerCommitRejectedDeadlock, runnerCommitRejectedOther) && facts.readyForQuery && !facts.connectionClosed
	case runnerCommitProtocolAmbiguous:
		return facts.rejectionReason == "" && !facts.readyForQuery
	default:
		return false
	}
}

func runnerCommitProtocolObservationDigest(observation *runnerCommitProtocolObservation) [32]byte {
	if observation == nil || observation.self != observation || observation.consumed || observation.source == nil {
		return [32]byte{}
	}
	facts := runnerCommitProtocolFacts{
		outcome: observation.outcome, rejectionReason: observation.rejectionReason,
		commitCalled: observation.commitCalled, readyForQuery: observation.readyForQuery,
		connectionClosed: observation.connectionClosed,
	}
	if !validRunnerCommitProtocolFacts(facts) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-runner-commit-protocol/v1\x00"))
	writeAdmissionString(h, string(facts.outcome))
	writeAdmissionString(h, facts.rejectionReason)
	writeAdmissionUint(h, boolUint64(facts.commitCalled))
	writeAdmissionUint(h, boolUint64(facts.readyForQuery))
	writeAdmissionUint(h, boolUint64(facts.connectionClosed))
	writeAdmissionString(h, strconv.FormatBool(runnerOwnedPointer(observation.source)))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func mapRunnerCommitProtocolPreflightError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fail(CodeContextCanceled, "runner-commit-protocol", "transaction commit was interrupted before invocation", nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(CodeDeadlineExceeded, "runner-commit-protocol", "transaction commit deadline expired before invocation", nil)
	}
	return fail(CodeTransactionBoundary, "runner-commit-protocol", "transaction commit protocol preflight failed", nil)
}

func boolUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}
