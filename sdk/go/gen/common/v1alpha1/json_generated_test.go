package v1alpha1

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestGeneratedCommonJSONFixtures(t *testing.T) {
	problem, err := DecodeProblemJSON(readFixtureBytes(t, "golden/problem.json"))
	if err != nil || problem.Status != 404 || problem.Error.Code != "RESOURCE_NOT_FOUND" {
		t.Fatalf("problem = %#v / %v", problem, err)
	}
	idempotency, err := DecodeIdempotencyJSON(readFixtureBytes(t, "golden/idempotency.json"))
	if err != nil || idempotency.Key != "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2" {
		t.Fatalf("idempotency = %#v / %v", idempotency, err)
	}
	cursor, err := DecodeWatchCursorJSON(readFixtureBytes(t, "golden/watch-cursor.json"))
	if err != nil || cursor.ResourceVersion != "42" {
		t.Fatalf("cursor = %#v / %v", cursor, err)
	}
	watchNMinusOne := bytes.TrimSpace(readFixtureBytes(t, "negative/watch-cursor-extra-field.json"))
	watchEnvelope, err := DecodeWatchCursorResponseJSON(watchNMinusOne)
	if err != nil || watchEnvelope.Value.ResourceVersion != "42" || string(watchEnvelope.Unknown["/tenantId"]) != `"cross-tenant-leak"` {
		t.Fatalf("watch sidecar = %#v / %v", watchEnvelope, err)
	}
	watchReencoded, err := EncodeWatchCursorResponseJSON(watchEnvelope)
	if err != nil || !bytes.Contains(watchReencoded, []byte(`"tenantId":"cross-tenant-leak"`)) {
		t.Fatalf("watch re-encode = %s / %v", watchReencoded, err)
	}
	if _, err := DecodeWatchCursorJSON(watchNMinusOne); err == nil {
		t.Fatal("watch mutation decoder accepted a future field")
	}

	negative := []struct {
		name   string
		decode func([]byte) error
	}{
		{"problem-secret-field", func(data []byte) error { _, err := DecodeProblemJSON(data); return err }},
		{"idempotency-short-key", func(data []byte) error { _, err := DecodeIdempotencyJSON(data); return err }},
		{"watch-cursor-extra-field", func(data []byte) error { _, err := DecodeWatchCursorJSON(data); return err }},
	}
	for _, test := range negative {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode(readFixtureBytes(t, "negative/"+test.name+".json")); err == nil {
				t.Fatal("negative fixture accepted")
			}
		})
	}
}

func TestGeneratedCommonJSONStrictFramingAndSidecar(t *testing.T) {
	for name, input := range map[string]string{
		"duplicate":   `{"pageSize":1,"pageSize":2}`,
		"trailing":    `{"pageSize":1}[]`,
		"zero":        `{"pageSize":0}`,
		"too-large":   `{"pageSize":201}`,
		"short-token": `{"pageSize":1,"pageToken":"short"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePaginationJSON([]byte(input)); err == nil {
				t.Fatal("invalid pagination accepted")
			}
		})
	}
	page, err := DecodePaginationJSON([]byte(`{"pageSize":200,"pageToken":"ZXZlbnQ6MDE5MDAwMDAwMDAwMDAwMA"}`))
	if err != nil || page.PageSize != 200 {
		t.Fatalf("pagination = %#v / %v", page, err)
	}

	known, sidecar, err := DecodeJSONObjectWithSidecar(
		[]byte(`{"known":1,"future":{"secret-free":true}}`),
		[]string{"known"},
	)
	if err != nil || string(known["known"]) != "1" || string(sidecar["future"]) != `{"secret-free":true}` {
		t.Fatalf("known/sidecar = %#v / %#v / %v", known, sidecar, err)
	}
	mutated := append(json.RawMessage(nil), sidecar["future"]...)
	mutated[0] = '['
	if string(sidecar["future"]) != `{"secret-free":true}` {
		t.Fatal("sidecar did not own its raw JSON bytes")
	}
	if _, _, err := DecodeJSONObjectWithSidecar([]byte(`{"known":1,"known":2}`), []string{"known"}); err == nil {
		t.Fatal("duplicate sidecar input accepted")
	}
	knownRaw, nested, err := DecodeResponseJSONWithSidecar(
		[]byte(`{"items":[{"known":1,"future/raw":9007199254740993}]}`),
		ObjectResponseShape(map[string]ResponseShape{
			"items": ArrayResponseShape(ObjectResponseShape(map[string]ResponseShape{
				"known": ScalarResponseShape(),
			})),
		}),
	)
	if err != nil || string(nested["/items/0/future~1raw"]) != `9007199254740993` {
		t.Fatalf("nested sidecar = %s / %#v / %v", knownRaw, nested, err)
	}
	if _, err := EncodeJSONObjectWithSidecar(json.RawMessage(knownRaw), nested); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeJSONObjectWithSidecar(json.RawMessage(knownRaw), JSONSidecar{"/items/0/known": json.RawMessage(`2`)}); err == nil {
		t.Fatal("nested known-field collision accepted")
	}
	if _, err := EncodeJSONObjectWithSidecar(json.RawMessage(knownRaw), JSONSidecar{"items/future": json.RawMessage(`true`)}); err == nil {
		t.Fatal("invalid nested pointer accepted")
	}
	if _, err := EncodeJSONObjectWithSidecar(json.RawMessage(knownRaw), JSONSidecar{
		"/future":        json.RawMessage(`{}`),
		"/future/nested": json.RawMessage(`true`),
	}); err == nil {
		t.Fatal("overlapping sidecar pointers accepted")
	}
}

func TestGeneratedCommonJSONErrorsAreStableAndSecretFree(t *testing.T) {
	var firstErr error
	for _, typ := range []string{"relative", "/relative"} {
		_, err := DecodeProblemJSON([]byte(`{"type":"` + typ + `","title":"x","status":500,"error":{"code":"INTERNAL_ERROR","retryable":false},"requestId":"req-alpha"}`))
		if firstErr == nil {
			firstErr = err
		}
		var contract *JSONContractError
		if !errors.As(err, &contract) || contract.Code != "INVALID_URI" || contract.Path != "/type" {
			t.Fatalf("type %q error = %#v", typ, err)
		}
	}
	_, err := DecodeProblemJSON([]byte(`{"type":"https://errors.example.test/internal","title":"x","status":500,"error":{"code":"INTERNAL_ERROR","retryable":false},"requestId":"req-alpha"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := firstErr.Error(); got != "INVALID_URI at /type" {
		t.Fatalf("stable error changed = %q", got)
	}
}
