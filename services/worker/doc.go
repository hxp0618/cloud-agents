// Package worker is the public Cloud Agents Platform worker and supervisor service module.
//
// NewService provides a transport-neutral, in-memory P1-A negotiation, health,
// and local operation-admission kernel. AdmitOperation only creates a bounded
// in-memory claim; it does not execute an operation or issue a receipt.
// ExecuteOperation and GetOperationReceipt are available only when an
// explicit, process-local OperationExecutor is injected; a nil executor keeps
// both methods fail-closed and unimplemented. The generated Connect handler is
// a decoded HTTP seam with bounded message options; it does not provide a TLS
// listener or claim mTLS/network pre-decode enforcement. No database,
// provider, workspace, credential, artifact, or production dispatch is
// performed here.
package worker
