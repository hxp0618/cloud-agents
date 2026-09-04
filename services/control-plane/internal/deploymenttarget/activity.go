package deploymenttarget

import (
	"regexp"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Operation struct {
	Scope                                         Scope
	OperationID, IdempotencyKey, Action, TargetID string
	RequestedBy, RequestID, State, CurrentStep    string
	StableErrorCode, ImpactSummary                string
	TargetGeneration                              int64
	Retryable                                     bool
	RequestedAt, UpdatedAt                        time.Time
}

type AuditEvent struct {
	Scope                                    Scope
	EventID, Actor, Action, TargetID, Result string
	RequestID, OperationID, StableErrorCode  string
	TargetGeneration                         int64
	OccurredAt                               time.Time
}

func (operation Operation) Validate() error {
	if invalidIdentifier(operation.Scope.TenantID) || invalidIdentifier(operation.Scope.ProjectID) ||
		invalidIdentifier(operation.OperationID) || commonv1alpha1.ValidateIdempotencyKey(operation.IdempotencyKey, "/idempotencyKey") != nil ||
		!validActivityAction(operation.Action) || invalidIdentifier(operation.TargetID) || operation.TargetGeneration < 1 ||
		!digestPattern.MatchString(operation.RequestedBy) || invalidIdentifier(operation.RequestID) ||
		!validOperationState(operation.State) || invalidIdentifier(operation.CurrentStep) ||
		operation.StableErrorCode != "" && invalidIdentifier(operation.StableErrorCode) ||
		len(operation.ImpactSummary) < 1 || len(operation.ImpactSummary) > 256 || strings.ContainsAny(operation.ImpactSummary, "\r\n\x00") ||
		operation.RequestedAt.IsZero() || operation.UpdatedAt.Before(operation.RequestedAt) {
		return ErrInvalidInput
	}
	if operation.State == "failed" {
		if operation.StableErrorCode == "" || !operation.Retryable {
			return ErrInvalidInput
		}
	} else if operation.StableErrorCode != "" || operation.Retryable {
		return ErrInvalidInput
	}
	return nil
}

func (event AuditEvent) Validate() error {
	if invalidIdentifier(event.Scope.TenantID) || invalidIdentifier(event.Scope.ProjectID) ||
		invalidIdentifier(event.EventID) || !digestPattern.MatchString(event.Actor) || !validActivityAction(event.Action) ||
		invalidIdentifier(event.TargetID) || event.TargetGeneration < 1 ||
		(event.Result != "requested" && event.Result != "succeeded" && event.Result != "failed") ||
		event.OccurredAt.IsZero() || invalidIdentifier(event.RequestID) || invalidIdentifier(event.OperationID) ||
		event.StableErrorCode != "" && invalidIdentifier(event.StableErrorCode) {
		return ErrInvalidInput
	}
	if (event.Result == "failed") != (event.StableErrorCode != "") {
		return ErrInvalidInput
	}
	return nil
}

func validActivityAction(value string) bool {
	return value == "target.register" || value == "target.probe" || value == "target.drain" || value == "target.resume" || value == "target.cleanup"
}

func validOperationState(value string) bool {
	return value == "queued" || value == "running" || value == "succeeded" || value == "failed" || value == "cancelled"
}
