// Package worker is the public Cloud Agents Platform worker and supervisor service module.
//
// NewService provides a transport-neutral, in-memory P1-A negotiation and health
// kernel. ExecuteOperation and GetOperationReceipt intentionally remain
// unimplemented and have no side effects. The generated Connect handler is a
// decoded HTTP seam with bounded message options; it does not provide a TLS
// listener or claim mTLS/network pre-decode enforcement. No database, provider,
// workspace, credential, artifact, or production dispatch is performed here.
package worker
