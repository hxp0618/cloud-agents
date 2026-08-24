package mountauthority

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
)

const (
	authorityDirectory = "/run/cloud-agents/evidencefs-mounts"
	authoritySuffix    = ".authority"
	authoritySize      = 224
	ext4SuperMagic     = uint32(0x0000ef53)
	xfsSuperMagic      = uint32(0x58465342)
)

var (
	ErrUnavailable   = errors.New("mount authority is unavailable")
	ErrInvalid       = errors.New("mount authority is invalid")
	ErrFilesystem    = errors.New("mount authority filesystem operation failed")
	ErrConflict      = errors.New("mount authority already exists")
	ErrNotPrivileged = errors.New("mount authority requires root")
	ErrUnsupported   = errors.New("mount authority is unsupported")

	authorityMagic = [32]byte([]byte("CA-EVIDENCEFS-MOUNT-AUTHORITY-V1"))
)

// Observation is a value-only snapshot of the kernel identities bound by an
// authority claim. It is not authority by itself.
type Observation struct {
	RootPathDigest      [32]byte
	BootID              [16]byte
	MountNamespaceDev   uint64
	MountNamespaceInode uint64
	MountID             uint64
	FilesystemType      uint32
	RootDevice          uint64
	RootInode           uint64
	RootUID             uint32
	RootMode            uint32
	DeviceMajor         uint32
	DeviceMinor         uint32
	SourceDigest        [32]byte
	OptionsDigest       [32]byte
}

func (observed Observation) Valid() bool { return validObservation(observed) }

type attestation struct {
	nonce     [16]byte
	rootPath  [32]byte
	runnerUID uint32
	observed  Observation
}

// Claim is deliberately opaque and anti-copy. Reading the public attestation
// bytes elsewhere cannot create a Claim because only Load seals one after the
// fixed root-owned capability path and the current process have been checked.
type Claim struct {
	self *Claim
	seal *struct{}
	body attestation
}

// ProvisionRequest is accepted only by the root-only provisioner path. The
// explicit confirmation prevents an unattended caller from classifying a
// mount as a dedicated direct-local evidence filesystem.
type ProvisionRequest struct {
	RootPath                string
	RunnerUID               uint32
	ConfirmDirectLocalMount bool
}

func RootPathDigest(rootPath string) ([32]byte, error) {
	if !validRootPath(rootPath) {
		return [32]byte{}, ErrInvalid
	}
	return sha256.Sum256([]byte(rootPath)), nil
}

func AuthorityBasename(rootPath string) (string, error) {
	digest, err := RootPathDigest(rootPath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x%s", digest, authoritySuffix), nil
}

func (c *Claim) RunnerUID() (uint32, bool) {
	if !c.valid() {
		return 0, false
	}
	return c.body.runnerUID, true
}

func (c *Claim) Matches(observed Observation) bool {
	return c.valid() && validAttestedObservation(observed) && c.body.rootPath == observed.RootPathDigest && sameObservation(c.body.observed, observed)
}

func (c *Claim) valid() bool {
	return c != nil && c.self == c && c.seal != nil && validAttestation(c.body)
}

func newClaim(body attestation) (*Claim, error) {
	if !validAttestation(body) {
		return nil, ErrInvalid
	}
	claim := &Claim{seal: &struct{}{}, body: body}
	claim.self = claim
	return claim, nil
}

func encodeAttestation(body attestation) ([]byte, error) {
	if !validAttestation(body) {
		return nil, ErrInvalid
	}
	encoded := make([]byte, authoritySize)
	copy(encoded[0:32], authorityMagic[:])
	copy(encoded[32:48], body.nonce[:])
	copy(encoded[48:80], body.rootPath[:])
	binary.BigEndian.PutUint32(encoded[80:84], body.runnerUID)
	copy(encoded[84:100], body.observed.BootID[:])
	binary.BigEndian.PutUint64(encoded[100:108], body.observed.MountNamespaceDev)
	binary.BigEndian.PutUint64(encoded[108:116], body.observed.MountNamespaceInode)
	binary.BigEndian.PutUint64(encoded[116:124], body.observed.MountID)
	binary.BigEndian.PutUint32(encoded[124:128], body.observed.FilesystemType)
	binary.BigEndian.PutUint64(encoded[128:136], body.observed.RootDevice)
	binary.BigEndian.PutUint64(encoded[136:144], body.observed.RootInode)
	binary.BigEndian.PutUint32(encoded[144:148], body.observed.RootUID)
	binary.BigEndian.PutUint32(encoded[148:152], body.observed.RootMode)
	binary.BigEndian.PutUint32(encoded[152:156], body.observed.DeviceMajor)
	binary.BigEndian.PutUint32(encoded[156:160], body.observed.DeviceMinor)
	copy(encoded[160:192], body.observed.SourceDigest[:])
	copy(encoded[192:224], body.observed.OptionsDigest[:])
	return encoded, nil
}

func decodeAttestation(encoded []byte) (attestation, error) {
	if len(encoded) != authoritySize || string(encoded[0:32]) != string(authorityMagic[:]) {
		return attestation{}, ErrInvalid
	}
	var body attestation
	copy(body.nonce[:], encoded[32:48])
	copy(body.rootPath[:], encoded[48:80])
	body.runnerUID = binary.BigEndian.Uint32(encoded[80:84])
	copy(body.observed.BootID[:], encoded[84:100])
	body.observed.MountNamespaceDev = binary.BigEndian.Uint64(encoded[100:108])
	body.observed.MountNamespaceInode = binary.BigEndian.Uint64(encoded[108:116])
	body.observed.MountID = binary.BigEndian.Uint64(encoded[116:124])
	body.observed.FilesystemType = binary.BigEndian.Uint32(encoded[124:128])
	body.observed.RootDevice = binary.BigEndian.Uint64(encoded[128:136])
	body.observed.RootInode = binary.BigEndian.Uint64(encoded[136:144])
	body.observed.RootUID = binary.BigEndian.Uint32(encoded[144:148])
	body.observed.RootMode = binary.BigEndian.Uint32(encoded[148:152])
	body.observed.DeviceMajor = binary.BigEndian.Uint32(encoded[152:156])
	body.observed.DeviceMinor = binary.BigEndian.Uint32(encoded[156:160])
	copy(body.observed.SourceDigest[:], encoded[160:192])
	copy(body.observed.OptionsDigest[:], encoded[192:224])
	body.observed.RootPathDigest = body.rootPath
	if !validAttestation(body) {
		return attestation{}, ErrInvalid
	}
	return body, nil
}

func validAttestation(body attestation) bool {
	return body.runnerUID != 0 && body.runnerUID == body.observed.RootUID && body.rootPath == body.observed.RootPathDigest && !validZeroNonce(body.nonce) && validAttestedObservation(body.observed)
}

func validObservation(observed Observation) bool {
	return observed.RootPathDigest != [32]byte{} &&
		observed.BootID != [16]byte{} &&
		observed.MountNamespaceDev != 0 &&
		observed.MountNamespaceInode != 0 &&
		observed.MountID != 0 &&
		supportedFilesystem(observed.FilesystemType) &&
		observed.RootDevice != 0 &&
		observed.RootInode != 0 &&
		observed.RootMode&^uint32(0o700) == 0 &&
		observed.DeviceMajor == deviceMajor(observed.RootDevice) &&
		observed.DeviceMinor == deviceMinor(observed.RootDevice) &&
		observed.SourceDigest != [32]byte{} &&
		observed.OptionsDigest != [32]byte{}
}

func validAttestedObservation(observed Observation) bool {
	return observed.RootUID != 0 && validObservation(observed)
}

func supportedFilesystem(fsType uint32) bool {
	return fsType == ext4SuperMagic || fsType == xfsSuperMagic
}

// These helpers intentionally use the Linux dev_t encoding because the
// attestation format describes Linux mount identity even when its codec is
// unit-tested on another host OS.
func deviceMajor(device uint64) uint32 {
	major := uint32((device & 0x00000000000fff00) >> 8)
	major |= uint32((device & 0xfffff00000000000) >> 32)
	return major
}

func deviceMinor(device uint64) uint32 {
	minor := uint32(device & 0x00000000000000ff)
	minor |= uint32((device & 0x00000ffffff00000) >> 12)
	return minor
}

func sameObservation(a, b Observation) bool {
	return a == b
}

func validZeroNonce(nonce [16]byte) bool { return nonce == [16]byte{} }

func validRootPath(rootPath string) bool {
	return rootPath != "" && filepath.IsAbs(rootPath) && filepath.Clean(rootPath) == rootPath && rootPath != "/"
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func invalid(op string) error     { return fmt.Errorf("%w: %s", ErrInvalid, op) }
func unavailable(op string) error { return fmt.Errorf("%w: %s", ErrUnavailable, op) }
func filesystem(op string) error  { return fmt.Errorf("%w: %s", ErrFilesystem, op) }
