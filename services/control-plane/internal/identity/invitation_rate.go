package identity

import (
	"context"
	"crypto/sha256"
	"net/netip"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

const invitationIPBucketPrefix = "invitation-ip:"

func (store *InvitationStore) recordInvitationAcceptAttempt(
	ctx context.Context,
	codeDigest [sha256.Size]byte,
	clientIP netip.Addr,
) ([sha256.Size]byte, error) {
	ipBucket, ok := invitationIPBucket(clientIP)
	if store == nil || store.pool == nil || ctx == nil || !ok {
		return ipBucket, ErrInvitationInvalid
	}
	databaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), passwordAttemptTimeout)
	defer cancel()
	var permitted, rateLimited bool
	if err := store.pool.QueryRow(databaseCtx, `SELECT permitted,rate_limited
		FROM cloud_agents_identity.record_invitation_accept_attempt($1,$2)`, codeDigest[:], ipBucket[:]).Scan(&permitted, &rateLimited); err != nil {
		return [sha256.Size]byte{}, invitationError(ctx, err)
	}
	if rateLimited {
		return [sha256.Size]byte{}, ErrRateLimited
	}
	if !permitted {
		return [sha256.Size]byte{}, browserauth.ErrUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return [sha256.Size]byte{}, err
	}
	return ipBucket, nil
}

func invitationIPBucket(clientIP netip.Addr) ([sha256.Size]byte, bool) {
	if !clientIP.IsValid() || clientIP.Zone() != "" {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256([]byte(invitationIPBucketPrefix + clientIP.Unmap().String())), true
}
