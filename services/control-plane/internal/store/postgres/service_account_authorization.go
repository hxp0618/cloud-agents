package postgres

import (
	"context"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
)

// executeServiceAccountCreate spends the two independently verified
// permissions against one database snapshot before protected creation work.
func executeServiceAccountCreate(
	ctx context.Context,
	handle *tenantReadHandle,
	membershipOperation *authz.VerifiedOperation,
	bindingOperation *authz.VerifiedOperation,
	scope authz.ScopeRef,
	callback func(authz.SubjectRef) error,
) error {
	if handle == nil {
		return ErrTenantCapabilityClosed
	}
	handle.mutex.Lock()
	defer handle.mutex.Unlock()
	if ctx == nil {
		return ErrNilContext
	}
	if membershipOperation == nil || bindingOperation == nil || callback == nil || !handle.active || handle.transaction == nil {
		return authz.ErrOperationDenied
	}
	membershipActor, membershipOK := membershipOperation.Actor()
	bindingActor, bindingOK := bindingOperation.Actor()
	if !membershipOK || !bindingOK || membershipActor != bindingActor {
		return authz.ErrOperationDenied
	}
	snapshot, now, err := handle.authorizationSnapshot(ctx, membershipActor, scope)
	if err != nil {
		return err
	}
	return membershipOperation.Execute(snapshot, now, func() error {
		return bindingOperation.Execute(snapshot, now, func() error {
			return callback(membershipActor)
		})
	})
}
