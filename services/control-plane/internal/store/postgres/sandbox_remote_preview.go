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

type RemoteWorkerSandboxPreviewRequest struct {
	Authority            SandboxAccessGrantAuthority
	CommandID, RequestID string
	TokenDigest, Method  string
	Path, RawQuery       string
	Port                 int64
	Headers              []platform.RemoteWorkerSandboxPreviewHeader
	Body                 []byte
}

type remoteWorkerSandboxPreviewResult struct {
	Receipt  platform.RemoteWorkerSandboxPreviewCommandReceipt
	State    string
	Deadline time.Time
}

const requestRemoteWorkerSandboxPreviewSQL = `SELECT command_state, command_deadline_at,
    bytes_transferred, status_code, response_headers, response_body, stable_error_code
FROM cloud_agents.request_remote_worker_sandbox_preview_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`

const getRemoteWorkerSandboxPreviewSQL = `SELECT command_state, command_deadline_at,
    bytes_transferred, status_code, response_headers, response_body, stable_error_code
FROM cloud_agents.get_remote_worker_sandbox_preview_v1($1,$2,$3,$4)`

func (store *AccessGatewayStore) ExecuteRemoteWorkerSandboxPreview(ctx context.Context, input RemoteWorkerSandboxPreviewRequest) (platform.RemoteWorkerSandboxPreviewCommandReceipt, error) {
	access := input.Authority.Access
	command := platform.RemoteWorkerSandboxPreviewCommand{CommandID: input.CommandID, GrantID: input.Authority.GrantID,
		WorkspaceID: access.WorkspaceID, TargetID: access.TargetID, SandboxID: access.SandboxID,
		SandboxGeneration: access.Generation, RuntimeID: access.RuntimeID, RuntimeOperationID: access.RuntimeOperationID,
		RuntimeSpecDigest: access.RuntimeSpecDigest, Port: input.Port, Method: input.Method, Path: input.Path,
		RawQuery: input.RawQuery, Headers: input.Headers, BodyBase64URL: base64.RawURLEncoding.EncodeToString(input.Body),
		Deadline: time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)}
	raw, err := json.Marshal(command)
	if store == nil || store.runner == nil || ctx == nil || access.TargetKind != "remote-worker" ||
		!validMutationIdentifier(input.RequestID) || !validCoordinationDigest(input.TokenDigest) || err != nil {
		return platform.RemoteWorkerSandboxPreviewCommandReceipt{}, ErrCoordinationInvalidInput
	}
	if _, err = platform.DecodeRemoteWorkerSandboxPreviewCommandJSON(raw); err != nil {
		return platform.RemoteWorkerSandboxPreviewCommandReceipt{}, ErrCoordinationInvalidInput
	}
	headers, _ := json.Marshal(input.Headers)
	result := remoteWorkerSandboxPreviewResult{Receipt: platform.RemoteWorkerSandboxPreviewCommandReceipt{
		CommandID: input.CommandID, GrantID: input.Authority.GrantID, SandboxID: access.SandboxID,
		SandboxGeneration: access.Generation, Port: input.Port,
	}}
	err = store.runner.withTenantMutation(ctx, access.Scope.TenantID, func(handle *tenantReadHandle) error {
		return scanRemoteWorkerSandboxPreview(handle.transaction.queryRow(ctx, requestRemoteWorkerSandboxPreviewSQL,
			access.Scope.TenantID, access.Scope.ProjectID, input.CommandID, input.Authority.GrantID,
			input.TokenDigest, access.TargetID, access.WorkspaceID, access.SandboxID, access.Generation,
			access.RuntimeID, access.RuntimeOperationID, access.RuntimeSpecDigest, input.Port, input.Method,
			input.Path, input.RawQuery, headers, input.Body, input.RequestID), &result)
	})
	if err != nil {
		return platform.RemoteWorkerSandboxPreviewCommandReceipt{}, mapRemoteWorkerSandboxPreviewError(err)
	}
	waitContext, cancel := context.WithDeadline(ctx, result.Deadline.Add(2*time.Second))
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for result.State == "pending" || result.State == "delivered" {
		select {
		case <-waitContext.Done():
			return platform.RemoteWorkerSandboxPreviewCommandReceipt{}, waitContext.Err()
		case <-ticker.C:
		}
		err = store.runner.WithTenantRead(waitContext, access.Scope.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return scanRemoteWorkerSandboxPreview(handle.transaction.queryRow(readContext, getRemoteWorkerSandboxPreviewSQL,
				access.Scope.TenantID, access.Scope.ProjectID, input.CommandID, input.TokenDigest), &result)
		})
		if err != nil {
			return platform.RemoteWorkerSandboxPreviewCommandReceipt{}, mapRemoteWorkerSandboxPreviewError(err)
		}
	}
	return result.Receipt, nil
}

func scanRemoteWorkerSandboxPreview(row rowScanner, result *remoteWorkerSandboxPreviewResult) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var bytesTransferred *int64
	var statusCode *int64
	var headersJSON, body []byte
	var stableError *string
	if err := row.Scan(&result.State, &result.Deadline, &bytesTransferred, &statusCode, &headersJSON, &body, &stableError); err != nil {
		return err
	}
	receipt := &result.Receipt
	receipt.Result, receipt.BytesTransferred, receipt.StatusCode, receipt.Headers,
		receipt.BodyBase64URL, receipt.StableErrorCode = "", 0, nil, nil, nil, ""
	if result.State == "pending" || result.State == "delivered" {
		if bytesTransferred != nil || statusCode != nil || headersJSON != nil || body != nil || stableError != nil {
			return ErrCoordinationResultDrift
		}
		return nil
	}
	if bytesTransferred == nil {
		return ErrCoordinationResultDrift
	}
	receipt.Result, receipt.BytesTransferred = result.State, *bytesTransferred
	if result.State == "failed" {
		if stableError == nil || statusCode != nil || headersJSON != nil || body != nil {
			return ErrCoordinationResultDrift
		}
		receipt.StableErrorCode = *stableError
	} else {
		var headers []platform.RemoteWorkerSandboxPreviewHeader
		if result.State != "succeeded" || stableError != nil || statusCode == nil || headersJSON == nil || body == nil ||
			json.Unmarshal(headersJSON, &headers) != nil || headers == nil {
			return ErrCoordinationResultDrift
		}
		encoded := base64.RawURLEncoding.EncodeToString(body)
		receipt.StatusCode, receipt.Headers, receipt.BodyBase64URL = statusCode, &headers, &encoded
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return ErrCoordinationResultDrift
	}
	if _, err := platform.DecodeRemoteWorkerSandboxPreviewCommandReceiptJSON(raw); err != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func mapRemoteWorkerSandboxPreviewError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "remote worker sandbox Preview request is invalid", "remote worker sandbox Preview lookup is invalid":
			return ErrCoordinationInvalidInput
		case "remote worker sandbox Preview unavailable":
			return internalcoordination.ErrFoundationSandboxConflict
		case "remote worker sandbox Preview was not found":
			return internalcoordination.ErrFoundationSandboxNotFound
		}
	}
	return mapSandboxAccessGrantError(err)
}
