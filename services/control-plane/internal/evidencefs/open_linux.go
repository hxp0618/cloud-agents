//go:build linux

package evidencefs

import (
	"context"
	"errors"
	"os"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/mountauthority"
)

func openProductionRoot(ctx context.Context, rootPath string) (*Root, error) {
	runnerUID := os.Geteuid()
	if runnerUID <= 0 {
		return nil, ErrTrustedMountAuthority
	}
	claim, err := mountauthority.Load(ctx, rootPath, uint32(runnerUID))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, ErrTrustedMountAuthority
	}
	return newRootWithRequiredProbe(ctx, rootPath, uint32(runnerUID), linuxBackend{}, mountAuthority{seal: &struct{}{}, claim: claim})
}
