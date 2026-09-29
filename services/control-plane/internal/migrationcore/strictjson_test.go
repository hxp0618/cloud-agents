package migrationcore

import (
	"bytes"
	"math"
	"strconv"
	"testing"
)

func TestStrictJSONAndCanonicalization(t *testing.T) {
	t.Parallel()
	left, err := ParseStrictJSON([]byte(`{"z":1,"a":"é","x":{"b":2,"a":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := ParseStrictJSON([]byte(`{"x":{"a":1,"b":2},"a":"é","z":1}`))
	if err != nil {
		t.Fatal(err)
	}
	leftCanonical, err := CanonicalJSON(left)
	if err != nil {
		t.Fatal(err)
	}
	rightCanonical, err := CanonicalJSON(right)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftCanonical, rightCanonical) {
		t.Fatalf("object reorder changed canonical bytes:\n%s\n%s", leftCanonical, rightCanonical)
	}
	if string(leftCanonical) != `{"a":"é","x":{"a":1,"b":2},"z":1}` {
		t.Fatalf("unexpected canonical JSON: %s", leftCanonical)
	}
}

func FuzzStrictJSONNeverPanics(f *testing.F) {
	for _, seed := range [][]byte{[]byte(`{}`), []byte(`{"a":1}`), []byte(`{"a":1,"\u0061":2}`), {0xff}, []byte(`{"s":"\ud800"}`)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		value, err := ParseStrictJSON(input)
		if err != nil {
			return
		}
		if _, err := CanonicalJSON(value); err != nil {
			t.Fatalf("accepted JSON did not canonicalize: %v", err)
		}
	})
}

func TestAdvisoryLockSignedInt64(t *testing.T) {
	t.Parallel()
	lock := AdvisoryLock{Domain: AdvisoryLockDomain, Derivation: AdvisoryLockDerivation, KeyInt64Decimal: "-1047838957622507638"}
	key, err := lock.Key()
	if err != nil {
		t.Fatal(err)
	}
	if key != -1047838957622507638 {
		t.Fatalf("wrong key: %d", key)
	}
	for _, invalid := range []string{"-0", "+1", "01", "-01", " 1", "9223372036854775808", "-9223372036854775809", ""} {
		lock.KeyInt64Decimal = invalid
		if _, err := lock.Key(); err == nil {
			t.Errorf("accepted invalid signed int64 %q", invalid)
		}
	}
	lock.KeyInt64Decimal = strconv.FormatInt(math.MinInt64, 10)
	if _, err := lock.Key(); err == nil {
		t.Error("accepted a valid int64 that does not match the signed domain derivation")
	}
}
