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

type RemoteWorkerSandboxFileRequest struct {
	Authority          SandboxAccessGrantAuthority
	EventID, RequestID string
	TokenDigest        string
	Action, Path       string
	Offset, Limit      int64
	FileVersion        string
	Content            []byte
}

type remoteWorkerSandboxFileResult struct {
	Receipt  platform.RemoteWorkerSandboxFileCommandReceipt
	State    string
	Deadline time.Time
}

const requestRemoteWorkerSandboxFileSQL = `SELECT command_state, command_deadline_at,
    bytes_transferred, list_entries, result_read_file_version, result_read_offset,
    result_read_total_bytes, result_read_content, write_entry, stable_error_code
FROM cloud_agents.request_remote_worker_sandbox_file_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`

const getRemoteWorkerSandboxFileSQL = `SELECT command_state, command_deadline_at,
    bytes_transferred, list_entries, result_read_file_version, result_read_offset,
    result_read_total_bytes, result_read_content, write_entry, stable_error_code
FROM cloud_agents.get_remote_worker_sandbox_file_v1($1,$2,$3,$4)`

func (store *AccessGatewayStore) ExecuteRemoteWorkerSandboxFile(ctx context.Context, input RemoteWorkerSandboxFileRequest) (platform.RemoteWorkerSandboxFileCommandReceipt, error) {
	access := input.Authority.Access
	if store == nil || store.runner == nil || ctx == nil || access.TargetKind != "remote-worker" ||
		!validMutationIdentifier(input.EventID) || !validMutationIdentifier(input.RequestID) ||
		!validCoordinationDigest(input.TokenDigest) || !validFileAction(input.Action) ||
		platform.ValidateSandboxFilePath(input.Path, input.Action == "list") != nil ||
		input.Action == "read" && (input.Offset < 0 || input.Offset > 16<<20 || input.Limit < 1 || input.Limit > 1<<20 ||
			input.Offset > 0 && input.FileVersion == "" || input.FileVersion != "" && platform.ValidateSandboxFileVersion(input.FileVersion) != nil) ||
		input.Action == "write" && len(input.Content) > 16<<20 {
		return platform.RemoteWorkerSandboxFileCommandReceipt{}, ErrCoordinationInvalidInput
	}
	result := remoteWorkerSandboxFileResult{Receipt: platform.RemoteWorkerSandboxFileCommandReceipt{
		CommandID: "rw" + input.EventID, EventID: input.EventID, GrantID: input.Authority.GrantID,
		SandboxID: access.SandboxID, SandboxGeneration: access.Generation, Action: input.Action,
	}}
	var readOffset, readLimit, readVersion, writeContent any
	switch input.Action {
	case "read":
		readOffset, readLimit = input.Offset, input.Limit
		if input.FileVersion != "" {
			readVersion = input.FileVersion
		}
	case "write":
		writeContent = append([]byte{}, input.Content...)
	}
	err := store.runner.withTenantMutation(ctx, access.Scope.TenantID, func(handle *tenantReadHandle) error {
		return scanRemoteWorkerSandboxFile(handle.transaction.queryRow(ctx, requestRemoteWorkerSandboxFileSQL,
			access.Scope.TenantID, access.Scope.ProjectID, result.Receipt.CommandID, input.EventID,
			input.Authority.GrantID, input.TokenDigest, access.TargetID, access.WorkspaceID,
			access.SandboxID, access.Generation, access.RuntimeID, access.RuntimeOperationID,
			access.RuntimeSpecDigest, input.Action, input.Path, readOffset, readLimit, readVersion,
			writeContent, input.RequestID), &result)
	})
	if err != nil {
		return platform.RemoteWorkerSandboxFileCommandReceipt{}, mapRemoteWorkerSandboxFileError(err)
	}
	waitContext, cancel := context.WithDeadline(ctx, result.Deadline.Add(2*time.Second))
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for result.State == "pending" || result.State == "delivered" {
		select {
		case <-waitContext.Done():
			return platform.RemoteWorkerSandboxFileCommandReceipt{}, context.DeadlineExceeded
		case <-ticker.C:
		}
		err = store.runner.WithTenantRead(waitContext, access.Scope.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return scanRemoteWorkerSandboxFile(handle.transaction.queryRow(readContext, getRemoteWorkerSandboxFileSQL,
				access.Scope.TenantID, access.Scope.ProjectID, result.Receipt.CommandID, input.TokenDigest), &result)
		})
		if err != nil {
			return platform.RemoteWorkerSandboxFileCommandReceipt{}, mapRemoteWorkerSandboxFileError(err)
		}
	}
	return result.Receipt, nil
}

func scanRemoteWorkerSandboxFile(row rowScanner, result *remoteWorkerSandboxFileResult) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var bytesTransferred, readOffset, readTotal *int64
	var listEntries, readContent, writeEntry []byte
	var readVersion, stableError *string
	if err := row.Scan(&result.State, &result.Deadline, &bytesTransferred, &listEntries, &readVersion,
		&readOffset, &readTotal, &readContent, &writeEntry, &stableError); err != nil {
		return err
	}
	receipt := &result.Receipt
	receipt.Result, receipt.BytesTransferred, receipt.List, receipt.Read, receipt.Write, receipt.StableErrorCode = "", 0, nil, nil, nil, ""
	if result.State == "pending" || result.State == "delivered" {
		if bytesTransferred != nil || listEntries != nil || readVersion != nil || readOffset != nil || readTotal != nil || readContent != nil || writeEntry != nil || stableError != nil {
			return ErrCoordinationResultDrift
		}
		return nil
	}
	if bytesTransferred == nil {
		return ErrCoordinationResultDrift
	}
	receipt.Result, receipt.BytesTransferred = result.State, *bytesTransferred
	if result.State == "failed" {
		if stableError == nil || listEntries != nil || readVersion != nil || readOffset != nil || readTotal != nil || readContent != nil || writeEntry != nil {
			return ErrCoordinationResultDrift
		}
		receipt.StableErrorCode = *stableError
	} else if result.State != "succeeded" || stableError != nil {
		return ErrCoordinationResultDrift
	} else {
		switch receipt.Action {
		case "list":
			var entries []platform.SandboxFileEntry
			if json.Unmarshal(listEntries, &entries) != nil || entries == nil {
				return ErrCoordinationResultDrift
			}
			receipt.List = &platform.RemoteWorkerSandboxFileListResult{Entries: entries}
		case "read":
			if readVersion == nil || readOffset == nil || readTotal == nil || readContent == nil {
				return ErrCoordinationResultDrift
			}
			receipt.Read = &platform.RemoteWorkerSandboxFileReadResult{FileVersion: *readVersion,
				Offset: *readOffset, TotalBytes: *readTotal,
				ContentBase64URL: base64.RawURLEncoding.EncodeToString(readContent)}
		case "write":
			var entry platform.SandboxFileEntry
			if json.Unmarshal(writeEntry, &entry) != nil {
				return ErrCoordinationResultDrift
			}
			receipt.Write = &platform.RemoteWorkerSandboxFileWriteResult{Entry: entry}
		case "delete":
		default:
			return ErrCoordinationResultDrift
		}
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return ErrCoordinationResultDrift
	}
	if _, err := platform.DecodeRemoteWorkerSandboxFileCommandReceiptJSON(raw); err != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func mapRemoteWorkerSandboxFileError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "remote worker sandbox file request is invalid", "remote worker sandbox file lookup is invalid":
			return ErrCoordinationInvalidInput
		case "remote worker sandbox file unavailable":
			return internalcoordination.ErrFoundationSandboxConflict
		case "remote worker sandbox file was not found":
			return internalcoordination.ErrFoundationSandboxNotFound
		}
	}
	return mapSandboxAccessGrantError(err)
}
