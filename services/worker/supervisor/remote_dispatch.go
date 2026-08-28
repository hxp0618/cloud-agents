package supervisor

import (
	"context"
	"time"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
)

// dispatchRemoteOperation is the launcher process-boundary equivalent of the
// local opaque-handle path. It reuses the same admission and receipt checks;
// the only difference is the generated Connect client call.
func (s *Supervisor) dispatchRemoteOperation(ctx context.Context, req *workerv1alpha1.OperationAttemptEnvelope) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	live, state, err := s.remoteDispatchBinding()
	if err != nil {
		return nil, err
	}
	attempt, err := prepareLocalDispatchAttempt(req, state)
	if err != nil {
		return nil, err
	}
	deadline, err := operationDeadline(attempt)
	if err != nil {
		return nil, err
	}
	now, ok := s.nowUTC()
	if !ok || !now.Before(deadline) {
		return nil, fail(connect.CodeDeadlineExceeded, "operation_deadline_exceeded")
	}
	callCtx, cancel := localBoundContext(ctx, now, state.expiresAt, deadline)
	defer cancel()
	response, callErr := s.client.ExecuteOperation(callCtx, connect.NewRequest(attempt))
	if callErr != nil {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		if err := s.localDispatchPostCall(ctx, live, state, deadline); err != nil {
			return nil, err
		}
		return nil, rpcFailure("execute_operation", callErr)
	}
	if err := s.localDispatchPostCall(ctx, live, state, deadline); err != nil {
		return nil, err
	}
	if err := validateLocalDispatchReceipt(response, attempt); err != nil {
		return nil, err
	}
	return detachedReceiptResponse(response), nil
}

func (s *Supervisor) getRemoteOperationReceipt(ctx context.Context, req *workerv1alpha1.ReceiptRequest) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	live, state, err := s.remoteDispatchBinding()
	if err != nil {
		return nil, err
	}
	receiptRequest, err := prepareLocalReceiptRequest(req, state)
	if err != nil {
		return nil, err
	}
	now, ok := s.nowUTC()
	if !ok {
		return nil, errInvalidConfig
	}
	callCtx, cancel := localBoundContext(ctx, now, state.expiresAt, time.Time{})
	defer cancel()
	response, callErr := s.client.GetOperationReceipt(callCtx, connect.NewRequest(receiptRequest))
	if callErr != nil {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		if err := s.localDispatchPostCall(ctx, live, state, time.Time{}); err != nil {
			return nil, err
		}
		return nil, rpcFailure("get_operation_receipt", callErr)
	}
	if err := s.localDispatchPostCall(ctx, live, state, time.Time{}); err != nil {
		return nil, err
	}
	if err := validateLocalReceiptResponse(response, receiptRequest.GetOperationId(), receiptRequest.GetReceiptId(), receiptRequest.GetFencing()); err != nil {
		return nil, err
	}
	return detachedReceiptResponse(response), nil
}
