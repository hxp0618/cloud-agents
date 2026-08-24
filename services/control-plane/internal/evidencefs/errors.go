package evidencefs

import "errors"

var (
	ErrTrustedMountAuthority = errors.New("evidencefs: trusted mount authority is unavailable")
	ErrFilesystem            = errors.New("evidencefs: filesystem admission failed")
	ErrCorrupt               = errors.New("evidencefs: object store is corrupt")
	ErrLimit                 = errors.New("evidencefs: object store limit exceeded")
	ErrUnknown               = errors.New("evidencefs: mutation outcome is unknown")
	ErrLeaseInvalid          = errors.New("evidencefs: lease is invalid")
	ErrInvalidInput          = errors.New("evidencefs: invalid input")
)
