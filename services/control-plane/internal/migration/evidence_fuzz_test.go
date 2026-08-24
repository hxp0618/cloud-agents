package migration

import "testing"

func FuzzDecodeCanonicalEvidenceFrame(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 2, '{', '}'})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeCanonicalEvidenceFrame(data)
	})
}

func FuzzDecodeCanonicalLineageFrame(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 2, '{', '}'})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeCanonicalLineageFrame(data)
	})
}
