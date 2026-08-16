package mountauthority

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
)

func TestAuthorityBasenameIsFixedCanonicalPathDigest(t *testing.T) {
	rootPath := "/srv/cloud-agents/evidence"
	digest := sha256.Sum256([]byte(rootPath))
	want := hexDigest(digest) + authoritySuffix
	got, err := AuthorityBasename(rootPath)
	if err != nil || got != want || len(got) != 64+len(authoritySuffix) {
		t.Fatalf("basename=%q want=%q err=%v", got, want, err)
	}
	for _, invalidPath := range []string{"", ".", "relative", "/", "/srv/../evidence", "/srv/evidence/"} {
		if _, err := AuthorityBasename(invalidPath); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid path %q accepted: %v", invalidPath, err)
		}
	}
}

func TestAttestationCodecIsClosedAndClaimIsAntiCopy(t *testing.T) {
	body := testAttestation(t)
	encoded, err := encodeAttestation(body)
	if err != nil || len(encoded) != authoritySize {
		t.Fatalf("encoded=%d err=%v", len(encoded), err)
	}
	decoded, err := decodeAttestation(encoded)
	if err != nil || decoded != body {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	claim, err := newClaim(decoded)
	if err != nil || !claim.Matches(body.observed) {
		t.Fatalf("claim=%v err=%v", claim, err)
	}
	if uid, ok := claim.RunnerUID(); !ok || uid != body.runnerUID {
		t.Fatalf("runner uid=%d ok=%v", uid, ok)
	}
	copyClaim := *claim
	if copyClaim.Matches(body.observed) {
		t.Fatal("copied claim retained authority")
	}
	if (&Claim{}).Matches(body.observed) {
		t.Fatal("literal claim retained authority")
	}
	mutated := body.observed
	mutated.MountID++
	if claim.Matches(mutated) {
		t.Fatal("mount identity drift retained authority")
	}
}

func TestAttestationCodecRejectsEveryClosedIdentityClass(t *testing.T) {
	body := testAttestation(t)
	encoded, err := encodeAttestation(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1, authoritySize - 1, authoritySize + 1} {
		candidate := make([]byte, size)
		copy(candidate, encoded)
		if _, err := decodeAttestation(candidate); !errors.Is(err, ErrInvalid) {
			t.Fatalf("size=%d accepted: %v", size, err)
		}
	}
	mutations := map[string]func([]byte){
		"magic":        func(raw []byte) { raw[0] ^= 1 },
		"nonce":        func(raw []byte) { clear(raw[32:48]) },
		"path":         func(raw []byte) { clear(raw[48:80]) },
		"runner":       func(raw []byte) { clear(raw[80:84]) },
		"boot":         func(raw []byte) { clear(raw[84:100]) },
		"namespace":    func(raw []byte) { clear(raw[100:108]) },
		"namespace-id": func(raw []byte) { clear(raw[108:116]) },
		"mount":        func(raw []byte) { clear(raw[116:124]) },
		"filesystem":   func(raw []byte) { clear(raw[124:128]) },
		"device":       func(raw []byte) { clear(raw[128:136]) },
		"inode":        func(raw []byte) { clear(raw[136:144]) },
		"owner":        func(raw []byte) { clear(raw[144:148]) },
		"mode":         func(raw []byte) { raw[151] = 0o77 },
		"major":        func(raw []byte) { raw[155] ^= 1 },
		"minor":        func(raw []byte) { raw[159] ^= 1 },
		"source":       func(raw []byte) { clear(raw[160:192]) },
		"options":      func(raw []byte) { clear(raw[192:224]) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := append([]byte(nil), encoded...)
			mutate(candidate)
			if _, err := decodeAttestation(candidate); !errors.Is(err, ErrInvalid) {
				t.Fatalf("mutation accepted: %v", err)
			}
		})
	}
}

func TestAuthorityContextAndInputAreFailClosed(t *testing.T) {
	if err := checkContext(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil context=%v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkContext(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context=%v", err)
	}
	if _, err := encodeAttestation(attestation{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero attestation=%v", err)
	}
}

func TestKernelObservationMayDescribeRootButAttestationMayNot(t *testing.T) {
	body := testAttestation(t)
	body.observed.RootUID = 0
	if !validObservation(body.observed) || validAttestedObservation(body.observed) {
		t.Fatal("kernel observation and claim admission were not separated")
	}
	body.runnerUID = 0
	if _, err := encodeAttestation(body); !errors.Is(err, ErrInvalid) {
		t.Fatalf("root-runner attestation accepted: %v", err)
	}
}

func testAttestation(t *testing.T) attestation {
	t.Helper()
	rootPath := "/srv/cloud-agents/evidence"
	pathDigest, err := RootPathDigest(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	device := linuxDevice(259, 7)
	source := sha256.Sum256([]byte("source"))
	body := attestation{
		nonce:     [16]byte{1, 2, 3, 4},
		rootPath:  pathDigest,
		runnerUID: 1001,
		observed: Observation{
			RootPathDigest:      pathDigest,
			BootID:              [16]byte{9, 8, 7, 6},
			MountNamespaceDev:   4,
			MountNamespaceInode: 5,
			MountID:             6,
			FilesystemType:      ext4SuperMagic,
			RootDevice:          device,
			RootInode:           7,
			RootUID:             1001,
			RootMode:            0o700,
			DeviceMajor:         259,
			DeviceMinor:         7,
			SourceDigest:        source,
			OptionsDigest:       sha256.Sum256([]byte("options")),
		},
	}
	return body
}

func linuxDevice(major, minor uint32) uint64 {
	device := (uint64(major) & 0x00000fff) << 8
	device |= (uint64(major) & 0xfffff000) << 32
	device |= uint64(minor) & 0x000000ff
	device |= (uint64(minor) & 0xffffff00) << 12
	return device
}

func hexDigest(digest [32]byte) string {
	const alphabet = "0123456789abcdef"
	encoded := make([]byte, 64)
	for index, value := range digest {
		encoded[index*2] = alphabet[value>>4]
		encoded[index*2+1] = alphabet[value&0x0f]
	}
	return string(encoded)
}
