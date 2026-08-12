package migration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestOwnedEvidenceRecordIsKindGenerationCursorBoundAndSingleUse(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	witness := buildEvidenceWitness(t, frames, context)
	owner := &evidenceOwnerToken{nonce: [16]byte{8}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	cursor := runtimeCursorAt(generation, frames[0].RecordDigest, 1)
	planWitness := witness.plans[evidenceStatementKey("000001", 1, 0)]
	plan := StatementPlan{MigrationID: planWitness.migrationID, StatementIndex: planWitness.statementIndex, SQLArtifactSHA256: planWitness.sqlArtifactSHA256, SQLArtifactSizeBytes: planWitness.sqlArtifactSizeBytes, StartOffset: planWitness.startOffset, EndOffset: planWitness.endOffset, StatementSHA256: planWitness.statementSHA256, Classification: frames[1].Record.StatementIntent.Classification, ExpectedTransitionDigest: planWitness.expectedTransitionDigest, exact: true, exactCanonical: "owned"}
	plan.sqlBytes = make([]byte, plan.EndOffset-plan.StartOffset)
	// validateExact includes the signed transition and bytes; use the real plan
	// shape from the wire oracle for the runtime binder's mismatch path first.
	badWitness := ownedStatementIntentWitness{ownedAppendContext{generation, cursor, frames[:1], witness}, plan}
	if _, err := bindOwnedEvidenceRecord(frames[1].Record, badWitness); err == nil {
		t.Fatal("accepted incomplete statement plan")
	}

	// Non-plan branches demonstrate the sealed ownership mechanics independent
	// of statement classifier construction.
	terminalFrame := frames[len(frames)-1]
	terminalCursor := runtimeCursorAt(generation, frames[len(frames)-2].RecordDigest, terminalFrame.Sequence)
	terminalWitness := ownedAttemptTerminalWitness{ownedAppendContext{generation, terminalCursor, frames[:len(frames)-1], witness}, terminalFrame.Record.AttemptTerminal.TerminalDigest, nil, 3}
	owned, err := bindOwnedEvidenceRecord(terminalFrame.Record, terminalWitness)
	if err != nil {
		t.Fatal(err)
	}
	other := generation
	other.owner = &evidenceOwnerToken{nonce: [16]byte{9}}
	if _, err := owned.consume(other, terminalCursor); err == nil {
		t.Fatal("accepted generation swap")
	}
	wrongCursor := terminalCursor.clone()
	wrongCursor.nextSequence++
	if _, err := owned.consume(generation, wrongCursor); err == nil {
		t.Fatal("accepted cursor swap")
	}
	if _, err := owned.consume(generation, terminalCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.consume(generation, terminalCursor); err == nil {
		t.Fatal("reused consumed record")
	}

	if _, err := bindOwnedEvidenceRecord(EvidenceRecord{Header: frames[0].Record.Header}, terminalWitness); err == nil {
		t.Fatal("header entered caller witness union")
	}
	var disk EvidenceFrame
	raw := mustJSON(t, terminalFrame)
	if _, err := DecodeStrict(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if _, err := bindOwnedEvidenceRecord(disk.Record, nil); err == nil {
		t.Fatal("disk DTO recovered append authority")
	}
}

func TestAppendUnknownInvalidatesCursorAndNeverMintsDurableAuthority(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	owner := &evidenceOwnerToken{nonce: [16]byte{10}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	terminal := frames[len(frames)-1]
	cursor := runtimeCursorAt(generation, frames[len(frames)-2].RecordDigest, terminal.Sequence)
	witness := buildEvidenceWitness(t, frames, fixtureObjectValue(t, document["validation_context"], "validation context"))
	owned, err := bindOwnedEvidenceRecord(terminal.Record, ownedAttemptTerminalWitness{ownedAppendContext{generation, cursor, frames[:len(frames)-1], witness}, terminal.Record.AttemptTerminal.TerminalDigest, nil, 3})
	if err != nil {
		t.Fatal(err)
	}
	result, err := finishAppend(cursor, owned, generation, appendOutcomeUnknown, nil, terminal.RecordDigest, DigestBytes([]byte("checkpoint")))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome() != "unknown" || result.DurableCursor() != nil || cursor.Valid() {
		t.Fatal("unknown append preserved cursor authority")
	}
}

func TestOwnedRecordRejectsMultipleBranchesAndAppendResultShapeFaults(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	chain := buildEvidenceWitness(t, frames, context)
	owner := &evidenceOwnerToken{nonce: [16]byte{11}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	terminal := frames[len(frames)-1]
	cursor := runtimeCursorAt(generation, frames[len(frames)-2].RecordDigest, terminal.Sequence)
	witness := func(c JournalCursor) ownedAttemptTerminalWitness {
		return ownedAttemptTerminalWitness{ownedAppendContext{generation, c, frames[:len(frames)-1], chain}, terminal.Record.AttemptTerminal.TerminalDigest, nil, 3}
	}
	multiple := cloneEvidenceRecord(terminal.Record)
	intent := cloneProjectionValue(*frames[1].Record.StatementIntent)
	multiple.StatementIntent = &intent
	if _, err := bindOwnedEvidenceRecord(multiple, witness(cursor)); err == nil {
		t.Fatal("accepted multiple union branches")
	}
	newOwned := func(c JournalCursor) *OwnedEvidenceRecord {
		owned, err := bindOwnedEvidenceRecord(terminal.Record, witness(c))
		if err != nil {
			t.Fatal(err)
		}
		return owned
	}
	if _, err := finishAppend(cursor, newOwned(cursor), generation, appendOutcome("bogus"), nil, terminal.RecordDigest, DigestBytes([]byte("checkpoint"))); err == nil || !cursor.Valid() {
		t.Fatal("pre-consume unknown kind changed cursor or was accepted")
	}
	wrongDurable := runtimeCursorAt(generation, DigestBytes([]byte("wrong")), cursor.nextSequence+1)
	if _, err := finishAppend(cursor, newOwned(cursor), generation, appendOutcomeDurable, &wrongDurable, terminal.RecordDigest, DigestBytes([]byte("checkpoint"))); err == nil || !cursor.Valid() {
		t.Fatal("pre-consume durable contradiction changed cursor or was accepted")
	}
	durable := runtimeCursorAt(generation, terminal.RecordDigest, cursor.nextSequence+1)
	result, err := finishAppend(cursor, newOwned(cursor), generation, appendOutcomeDurable, &durable, terminal.RecordDigest, DigestBytes([]byte("checkpoint")))
	if err != nil || result.DurableCursor() == nil || cursor.Valid() {
		t.Fatalf("durable append authority: %v", err)
	}
}

func TestEvidenceRuntimeProductionConstructorRejectsAndHasNoForbiddenImports(t *testing.T) {
	if sink, err := NewEvidenceSink(); sink != nil || !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("production sink accepted: %T %v", sink, err)
	}
	root := filepath.Dir(mustSourceFile(t))
	for _, name := range []string{"evidence_runtime.go", "evidence_recovery.go", "evidence_trust_recovery.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path := spec.Path.Value
			if path == `"os"` || path == `"io/fs"` || path == `"github.com/jackc/pgx/v5"` {
				t.Fatalf("%s imports forbidden %s", name, path)
			}
		}
		ast.Inspect(file, func(ast.Node) bool { return true })
	}
}

func runtimeCursorAt(g generationIdentity, previous Digest, sequence uint64) JournalCursor {
	valid := &atomic.Bool{}
	valid.Store(true)
	return JournalCursor{owner: g.owner, generation: g, nextSequence: sequence, previousRecordDigest: digestPointer(previous), lineageIndexNextSequence: 1, lineageIndexPreviousRecordDigest: DigestBytes([]byte("lineage")), valid: valid}
}
func mustSourceFile(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("evidence_runtime_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}
