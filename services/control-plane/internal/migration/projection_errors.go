package migration

import (
	"fmt"
	"strings"
)

const (
	maxProjectionErrorPhaseBytes = 64
	maxProjectionErrorPathBytes  = 160
	maxProjectionErrorMsgBytes   = 192
)

// ProjectionError is the bounded external projection error shape. It never
// retains a driver error, SQL text, SQLSTATE, DSN, role payload, or raw row.
type ProjectionError struct {
	Code          ErrorCode `json:"code"`
	Phase         string    `json:"phase"`
	Path          string    `json:"path"`
	PostgresMajor uint16    `json:"postgres_major"`
	Retryable     bool      `json:"retryable"`
	message       string
}

func (e *ProjectionError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: phase=%s path=%s postgres_major=%d retryable=%t: %s", e.Code, e.Phase, e.Path, e.PostgresMajor, e.Retryable, e.message)
}

// Unwrap preserves compatibility with IsCode without exposing a raw cause.
func (e *ProjectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return &Error{Code: e.Code, Op: e.Phase, Msg: e.message}
}

func projectionFailure(code ErrorCode, phase, path string, major uint16, retryable bool, message string) error {
	return &ProjectionError{
		Code:          code,
		Phase:         boundedProjectionLabel(phase, maxProjectionErrorPhaseBytes, "projection"),
		Path:          boundedProjectionLabel(path, maxProjectionErrorPathBytes, "unknown"),
		PostgresMajor: major,
		Retryable:     retryable,
		message:       boundedProjectionMessage(message),
	}
}

func boundedProjectionLabel(value string, limit int, fallback string) string {
	if value == "" || len(value) > limit {
		return fallback
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' || r == '[' || r == ']' || r == '/' || r == ':' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return fallback
		}
	}
	return value
}

func boundedProjectionMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "projection operation failed"
	}
	if len(value) > maxProjectionErrorMsgBytes {
		return value[:maxProjectionErrorMsgBytes]
	}
	return value
}
