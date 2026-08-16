//go:build !linux

package mountauthority

import "context"

func Load(context.Context, string, uint32) (*Claim, error) { return nil, ErrUnsupported }
func Provision(context.Context, ProvisionRequest) error    { return ErrUnsupported }
func Revoke(context.Context, string) error                 { return ErrUnsupported }
