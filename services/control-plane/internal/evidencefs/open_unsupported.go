//go:build !linux

package evidencefs

import "context"

func openProductionRoot(context.Context, string) (*Root, error) {
	return nil, ErrTrustedMountAuthority
}
