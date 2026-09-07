package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5/pgconn"
)

type RemoteWorkerSandboxPTYRequest struct {
	Authority            SandboxAccessGrantAuthority
	CommandID, RequestID string
	TokenDigest, Action  string
	SessionID            string
	Since                int64
	Takeover             bool
	Input                *platform.RemoteWorkerSandboxPTYFrame
}

type remoteWorkerSandboxPTYResult struct {
	Receipt  platform.RemoteWorkerSandboxPTYCommandReceipt
	State    string
	Deadline time.Time
}

const requestRemoteWorkerSandboxPTYSQL = `SELECT command_state, command_deadline_at,
    bytes_transferred, result_session_uid, result_running, result_output_offset,
    result_frames, stable_error_code
FROM cloud_agents.request_remote_worker_sandbox_pty_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`

const getRemoteWorkerSandboxPTYSQL = `SELECT command_state, command_deadline_at,
    bytes_transferred, result_session_uid, result_running, result_output_offset,
    result_frames, stable_error_code
FROM cloud_agents.get_remote_worker_sandbox_pty_v1($1,$2,$3,$4)`

func (store *AccessGatewayStore) ExecuteRemoteWorkerSandboxPTY(ctx context.Context, input RemoteWorkerSandboxPTYRequest) (platform.RemoteWorkerSandboxPTYCommandReceipt, error) {
	access := input.Authority.Access
	if store == nil || store.runner == nil || ctx == nil || access.TargetKind != "remote-worker" ||
		!validMutationIdentifier(input.CommandID) || !validMutationIdentifier(input.RequestID) ||
		!validCoordinationDigest(input.TokenDigest) ||
		(input.Action != "create" && input.Action != "get" && input.Action != "delete" && input.Action != "exchange") ||
		input.Action != "create" && !validMutationIdentifier(input.SessionID) ||
		input.Action == "exchange" && (input.Since < 0 || input.Since > 9007199254740991) {
		return platform.RemoteWorkerSandboxPTYCommandReceipt{}, ErrCoordinationInvalidInput
	}
	var session, since, takeover, messageType, payload any
	switch input.Action {
	case "get", "delete":
		session = input.SessionID
	case "exchange":
		session, since, takeover = input.SessionID, input.Since, input.Takeover
		if input.Input != nil {
			raw, err := base64.RawURLEncoding.Strict().DecodeString(input.Input.PayloadBase64URL)
			if err != nil || len(raw) > 64<<10 || base64.RawURLEncoding.EncodeToString(raw) != input.Input.PayloadBase64URL ||
				(input.Input.MessageType != "binary" && input.Input.MessageType != "text") {
				return platform.RemoteWorkerSandboxPTYCommandReceipt{}, ErrCoordinationInvalidInput
			}
			messageType, payload = input.Input.MessageType, raw
		}
	}
	result := remoteWorkerSandboxPTYResult{Receipt: platform.RemoteWorkerSandboxPTYCommandReceipt{
		CommandID: input.CommandID, GrantID: input.Authority.GrantID,
		SandboxID: access.SandboxID, SandboxGeneration: access.Generation, Action: input.Action,
	}}
	err := store.runner.withTenantMutation(ctx, access.Scope.TenantID, func(handle *tenantReadHandle) error {
		return scanRemoteWorkerSandboxPTY(handle.transaction.queryRow(ctx, requestRemoteWorkerSandboxPTYSQL,
			access.Scope.TenantID, access.Scope.ProjectID, input.CommandID, input.Authority.GrantID,
			input.TokenDigest, access.TargetID, access.WorkspaceID, access.SandboxID,
			access.Generation, access.RuntimeID, access.RuntimeOperationID, access.RuntimeSpecDigest,
			input.Action, session, since, takeover, messageType, payload, input.RequestID), &result)
	})
	if err != nil {
		return platform.RemoteWorkerSandboxPTYCommandReceipt{}, mapRemoteWorkerSandboxPTYError(err)
	}
	waitContext, cancel := context.WithDeadline(ctx, result.Deadline.Add(2*time.Second))
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for result.State == "pending" || result.State == "delivered" {
		select {
		case <-waitContext.Done():
			return platform.RemoteWorkerSandboxPTYCommandReceipt{}, waitContext.Err()
		case <-ticker.C:
		}
		err = store.runner.WithTenantRead(waitContext, access.Scope.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return scanRemoteWorkerSandboxPTY(handle.transaction.queryRow(readContext, getRemoteWorkerSandboxPTYSQL,
				access.Scope.TenantID, access.Scope.ProjectID, input.CommandID, input.TokenDigest), &result)
		})
		if err != nil {
			return platform.RemoteWorkerSandboxPTYCommandReceipt{}, mapRemoteWorkerSandboxPTYError(err)
		}
	}
	return result.Receipt, nil
}

func scanRemoteWorkerSandboxPTY(row rowScanner, result *remoteWorkerSandboxPTYResult) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var bytesTransferred, outputOffset *int64
	var session, stableError *string
	var running *bool
	var framesJSON []byte
	if err := row.Scan(&result.State, &result.Deadline, &bytesTransferred, &session,
		&running, &outputOffset, &framesJSON, &stableError); err != nil {
		return err
	}
	receipt := &result.Receipt
	receipt.Result, receipt.BytesTransferred, receipt.SessionID, receipt.Running,
		receipt.OutputOffset, receipt.Frames, receipt.StableErrorCode = "", 0, "", nil, nil, nil, ""
	if result.State == "pending" || result.State == "delivered" {
		if bytesTransferred != nil || session != nil || running != nil || outputOffset != nil || framesJSON != nil || stableError != nil {
			return ErrCoordinationResultDrift
		}
		return nil
	}
	if bytesTransferred == nil {
		return ErrCoordinationResultDrift
	}
	receipt.Result, receipt.BytesTransferred = result.State, *bytesTransferred
	if result.State == "failed" {
		if stableError == nil || session != nil || running != nil || outputOffset != nil || framesJSON != nil {
			return ErrCoordinationResultDrift
		}
		receipt.StableErrorCode = *stableError
	} else if result.State != "succeeded" || stableError != nil || session == nil {
		return ErrCoordinationResultDrift
	} else {
		receipt.SessionID = *session
		switch receipt.Action {
		case "create", "get":
			if running == nil || outputOffset == nil || framesJSON != nil {
				return ErrCoordinationResultDrift
			}
			receipt.Running, receipt.OutputOffset = running, outputOffset
		case "delete":
			if running != nil || outputOffset != nil || framesJSON != nil {
				return ErrCoordinationResultDrift
			}
		case "exchange":
			var frames []platform.RemoteWorkerSandboxPTYFrame
			if running == nil || outputOffset == nil || framesJSON == nil || json.Unmarshal(framesJSON, &frames) != nil || frames == nil {
				return ErrCoordinationResultDrift
			}
			receipt.Running, receipt.OutputOffset, receipt.Frames = running, outputOffset, &frames
		default:
			return ErrCoordinationResultDrift
		}
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return ErrCoordinationResultDrift
	}
	if _, err := platform.DecodeRemoteWorkerSandboxPTYCommandReceiptJSON(raw); err != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func mapRemoteWorkerSandboxPTYError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "remote worker sandbox PTY request is invalid", "remote worker sandbox PTY lookup is invalid":
			return ErrCoordinationInvalidInput
		case "remote worker sandbox PTY unavailable":
			return internalcoordination.ErrFoundationSandboxConflict
		case "remote worker sandbox PTY was not found":
			return internalcoordination.ErrFoundationSandboxNotFound
		}
	}
	return mapSandboxAccessGrantError(err)
}
