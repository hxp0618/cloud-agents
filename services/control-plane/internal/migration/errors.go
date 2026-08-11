package migration

import (
	"errors"
	"fmt"
)

// ErrorCode is a stable, non-secret classification suitable for conformance fixtures.
type ErrorCode string

const (
	CodeInvalidDigest       ErrorCode = "MIGRATION_INVALID_DIGEST"
	CodeInvalidJSON         ErrorCode = "MIGRATION_INVALID_JSON"
	CodeInvalidManifest     ErrorCode = "MIGRATION_INVALID_MANIFEST"
	CodeInvalidArtifact     ErrorCode = "MIGRATION_INVALID_ARTIFACT"
	CodeInvalidArchive      ErrorCode = "MIGRATION_INVALID_ARCHIVE"
	CodeInvalidLineage      ErrorCode = "MIGRATION_INVALID_LINEAGE"
	CodeInvalidLedger       ErrorCode = "MIGRATION_INVALID_LEDGER"
	CodeInvalidSQL          ErrorCode = "MIGRATION_INVALID_SQL"
	CodeUntrusted           ErrorCode = "MIGRATION_UNTRUSTED"
	CodeUnsupported         ErrorCode = "MIGRATION_UNSUPPORTED"
	CodeAuthorityDrift      ErrorCode = "MIGRATION_AUTHORITY_DRIFT"
	CodeCatalogDrift        ErrorCode = "MIGRATION_CATALOG_DRIFT"
	CodeLockLost            ErrorCode = "MIGRATION_LOCK_LOST"
	CodeTransactionBoundary ErrorCode = "MIGRATION_TRANSACTION_BOUNDARY"
	CodeAmbiguousCommit     ErrorCode = "MIGRATION_AMBIGUOUS_COMMIT"
)

// Error intentionally carries a stable code and a bounded explanation, not raw SQL or credentials.
type Error struct {
	Code ErrorCode
	Op   string
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Op == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Op, e.Msg)
}

func (e *Error) Unwrap() error { return e.Err }

func fail(code ErrorCode, op, msg string, err error) error {
	return &Error{Code: code, Op: op, Msg: msg, Err: err}
}

// IsCode reports whether err or one of its wrapped errors has code.
func IsCode(err error, code ErrorCode) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}
