package worker

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
)

// localDispatchAuthority is intentionally unexported.  A LocalDispatchHandle
// is valid only when it contains the package-owned marker returned by a real
// Service.  Callers can copy a handle, but cannot construct a valid handle for
// an arbitrary service or transport client.
type localDispatchAuthority struct{}

var localDispatchAuthorityMarker = &localDispatchAuthority{}

// LocalDispatchHandle is the only client capability for the process-local
// Supervisor -> Worker dispatch profile.  Its fields are private so a caller
// cannot substitute a generated HTTP client, endpoint, or another Service.
// The zero value is deliberately invalid and all methods fail closed.
type LocalDispatchHandle struct {
	service   *Service
	authority *localDispatchAuthority
}

// LocalDispatchHandle returns an opaque, in-process client capability bound to
// this Service.  It performs no I/O and does not start a listener.  A nil
// Service returns an invalid handle whose methods return Unimplemented.
func (s *Service) LocalDispatchHandle() LocalDispatchHandle {
	if s == nil {
		return LocalDispatchHandle{}
	}
	return LocalDispatchHandle{service: s, authority: localDispatchAuthorityMarker}
}

// Valid reports whether the handle was minted by LocalDispatchHandle on a
// non-nil Service.  It does not imply that the Service has an executor or that
// operation dispatch is configured; those checks remain the Service's
// fail-closed contract.
func (h LocalDispatchHandle) Valid() bool {
	return h.service != nil && h.authority == localDispatchAuthorityMarker
}

func (h LocalDispatchHandle) serviceOrError() (*Service, error) {
	if !h.Valid() {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("worker/local_dispatch_handle_invalid"))
	}
	return h.service, nil
}

// Keep the capability's surface exactly aligned with the generated client.
// These methods directly invoke the in-process Service; no Connect transport,
// URL, listener, or network path is involved.
var _ workerv1alpha1connect.WorkerExecutionServiceClient = LocalDispatchHandle{}

func (h LocalDispatchHandle) Negotiate(ctx context.Context, req *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	s, err := h.serviceOrError()
	if err != nil {
		return nil, err
	}
	return s.Negotiate(ctx, req)
}

func (h LocalDispatchHandle) CheckHealth(ctx context.Context, req *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
	s, err := h.serviceOrError()
	if err != nil {
		return nil, err
	}
	return s.CheckHealth(ctx, req)
}

func (h LocalDispatchHandle) ExecuteOperation(ctx context.Context, req *connect.Request[workerv1alpha1.OperationAttemptEnvelope]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	s, err := h.serviceOrError()
	if err != nil {
		return nil, err
	}
	return s.ExecuteOperation(ctx, req)
}

func (h LocalDispatchHandle) GetOperationReceipt(ctx context.Context, req *connect.Request[workerv1alpha1.ReceiptRequest]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	s, err := h.serviceOrError()
	if err != nil {
		return nil, err
	}
	return s.GetOperationReceipt(ctx, req)
}
