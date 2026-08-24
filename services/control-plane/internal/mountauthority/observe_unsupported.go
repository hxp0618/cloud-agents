//go:build !linux

package mountauthority

import "context"

func ObserveFD(context.Context, int, string) (Observation, error) {
	return Observation{}, ErrUnsupported
}
