package migration

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

func TestStrictJSONRejectsInvalidProfiles(t *testing.T) {
	t.Parallel()
	cases := [][]byte{
		[]byte(`{"a":1,"\u0061":2}`),
		[]byte(`{"n":-0}`),
		[]byte(`{"n":-1}`),
		[]byte(`{"n":1.0}`),
		[]byte(`{"n":1e2}`),
		[]byte(`{"n":9007199254740992}`),
		append([]byte{0xef, 0xbb, 0xbf}, []byte(`{}`)...),
		{0xff},
		[]byte(`{} {}`),
		[]byte(`{"s":"\ud800"}`),
		[]byte(`{"s":"\udc00"}`),
	}
	for _, input := range cases {
		if _, err := ParseStrictJSON(input); !IsCode(err, CodeInvalidJSON) {
			t.Errorf("expected invalid JSON for %q, got %v", input, err)
		}
	}
}

func TestStrictJSONRequiresFieldsAndDoesNotHTMLEscapeJCS(t *testing.T) {
	t.Parallel()
	var record ArtifactRecord
	if _, err := DecodeStrict([]byte(`{"path":"x","mode":"100644","size_bytes":1}`), &record); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("missing sha256 was not rejected: %v", err)
	}
	value, err := ParseStrictJSON([]byte(`{"s":"<>&"}`))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"s":"<>&"}` {
		t.Fatalf("JCS used HTML escaping: %s", canonical)
	}
}

func TestStrictJSONPreservesProtoAsOrdinaryOwnMember(t *testing.T) {
	t.Parallel()
	value, err := ParseStrictJSON([]byte(`{"__proto__":{"polluted":true},"constructor":1}`))
	if err != nil {
		t.Fatal(err)
	}
	object, ok := value.(map[string]JSONValue)
	if !ok {
		t.Fatal("expected object")
	}
	if _, ok := object["__proto__"]; !ok || len(object) != 2 {
		t.Fatalf("own-member identity was not preserved: %#v", object)
	}
	canonical, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != `{"__proto__":{"polluted":true},"constructor":1}` {
		t.Fatalf("unexpected canonical own members: %s", canonical)
	}
	var record ArtifactRecord
	if _, err := DecodeStrict([]byte(`{"path":"x","mode":"100644","size_bytes":1,"sha256":"sha256:0000000000000000000000000000000000000000000000000000000000000000","__proto__":{}}`), &record); !IsCode(err, CodeInvalidJSON) {
		t.Fatalf("typed decoder accepted unknown __proto__ member: %v", err)
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
