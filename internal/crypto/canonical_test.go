package crypto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCanonicalJSONSortsKeys(t *testing.T) {
	input := map[string]any{
		"zebra": 1,
		"alpha": 2,
		"mango": 3,
	}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	want := `{"alpha":2,"mango":3,"zebra":1}`
	if string(got) != want {
		t.Fatalf("expected %q, got %q", want, string(got))
	}
}

func TestCanonicalJSONIsDeterministicAcrossMapInsertionOrder(t *testing.T) {
	// Go's built-in json.Marshal does sort map[string]any keys, but this
	// test asserts the contract explicitly for hash consumers.
	a := map[string]any{"x": 1, "y": 2, "z": 3}
	b := map[string]any{"z": 3, "y": 2, "x": 1}

	canonA, err := CanonicalJSON(a)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	canonB, err := CanonicalJSON(b)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if string(canonA) != string(canonB) {
		t.Fatalf("canonical encoding not deterministic: %q vs %q", string(canonA), string(canonB))
	}
}

func TestCanonicalJSONNestedObjects(t *testing.T) {
	input := map[string]any{
		"outer": map[string]any{
			"beta":  2,
			"alpha": 1,
		},
		"array": []any{3, 1, 2},
	}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	// Array order is preserved; object keys are sorted at every level.
	want := `{"array":[3,1,2],"outer":{"alpha":1,"beta":2}}`
	if string(got) != want {
		t.Fatalf("expected %q, got %q", want, string(got))
	}
}

func TestCanonicalJSONStructWithTags(t *testing.T) {
	type inner struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	type outer struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		Child     inner     `json:"child"`
	}
	ts := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	in := outer{
		ID:        "prj_1",
		CreatedAt: ts,
		Child:     inner{Name: "b", Value: 7},
	}
	got, err := CanonicalJSON(in)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	// Round-trip to verify every field is present and deterministic.
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["id"] != "prj_1" {
		t.Fatalf("id: got %v", decoded["id"])
	}
	if !strings.HasPrefix(string(got), `{"child":{`) {
		t.Fatalf("expected child first alphabetically, got %s", string(got))
	}
}

func TestCanonicalJSONPreservesLargeIntegers(t *testing.T) {
	// Without UseNumber, large integers round-trip through float64 and
	// lose precision. This test locks down that the canonical encoder
	// preserves them exactly.
	input := map[string]any{
		"count": int64(9223372036854775806), // MaxInt64 - 1
	}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	want := `{"count":9223372036854775806}`
	if string(got) != want {
		t.Fatalf("expected %q, got %q", want, string(got))
	}
}

func TestDigestMatchesSha256(t *testing.T) {
	hash, err := Digest(map[string]any{"a": 1, "b": "two"})
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	// Recompute against the canonical form we know.
	want := DigestBytes([]byte(`{"a":1,"b":"two"}`))
	if hash != want {
		t.Fatalf("digest mismatch: got %q, want %q", hash, want)
	}
}

func TestDigestIsDeterministicUnderReorder(t *testing.T) {
	// Two equivalent payloads differing only in map insertion order
	// must produce the same digest.
	a, err := Digest(map[string]any{"x": 1, "y": 2})
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := Digest(map[string]any{"y": 2, "x": 1})
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if a != b {
		t.Fatalf("digest not stable: %q vs %q", a, b)
	}
}

func TestCanonicalJSONRejectsUnsupportedType(t *testing.T) {
	// json.Marshal errors on channels, so CanonicalJSON surfaces that
	// through its initial marshal step.
	_, err := CanonicalJSON(make(chan int))
	if err == nil {
		t.Fatal("expected error for channel input")
	}
}

func TestDigestBytesStability(t *testing.T) {
	h1 := DigestBytes([]byte("hello"))
	h2 := DigestBytes([]byte("hello"))
	if h1 != h2 {
		t.Fatalf("expected identical digests, got %q and %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex digest, got %d", len(h1))
	}
}

// TestCanonicalJSONRejectsDuplicateKeys verifies duplicate object
// keys must be rejected at decode time with ErrDuplicateKey so that two
// distinct JSON inputs can never canonicalise to the same bytes via the
// "second key silently wins" hole in encoding/json.
//
// The test feeds a pre-built json.RawMessage with a duplicate "a"
// entry. json.Marshal preserves the raw bytes verbatim, so the
// duplicate survives into our strict decoder.
func TestCanonicalJSONRejectsDuplicateKeys(t *testing.T) {
	raw := json.RawMessage(`{"a":1,"a":2}`)
	_, err := CanonicalJSON(raw)
	if !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("expected ErrDuplicateKey, got %v", err)
	}

	// Nested duplicate: the outer object is fine, the inner one is not.
	raw2 := json.RawMessage(`{"outer":{"a":1,"a":2}}`)
	_, err = CanonicalJSON(raw2)
	if !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("expected nested ErrDuplicateKey, got %v", err)
	}

	// Clean input still works.
	raw3 := json.RawMessage(`{"a":1,"b":2}`)
	got, err := CanonicalJSON(raw3)
	if err != nil {
		t.Fatalf("expected clean input to canonicalise, got %v", err)
	}
	if string(got) != `{"a":1,"b":2}` {
		t.Fatalf("unexpected canonical form: %s", string(got))
	}
}

// Distinct bytes are distinct programs: canonicalisation must not merge them.
// Normalisation happens in crl.normalizeBundle, which also executes the value.
func TestCanonicalJSONPreservesUnicodeBytes(t *testing.T) {
	precomposed := map[string]any{"name": "é"}
	decomposed := map[string]any{"name": "é"}

	canonPre, err := CanonicalJSON(precomposed)
	if err != nil {
		t.Fatalf("CanonicalJSON(precomposed): %v", err)
	}
	canonDec, err := CanonicalJSON(decomposed)
	if err != nil {
		t.Fatalf("CanonicalJSON(decomposed): %v", err)
	}
	if string(canonPre) == string(canonDec) {
		t.Fatalf("distinct inputs share a canonical form -- the forgery is back: %s", canonPre)
	}

	// Keys are bytes too: two NFC-equivalent keys are two keys.
	keys := json.RawMessage(`{"café":1,"café":2}`)
	canonKeys, err := CanonicalJSON(keys)
	if err != nil {
		t.Fatalf("CanonicalJSON(NFC-equivalent keys): %v", err)
	}
	// Sorted by UTF-8 bytes: the decomposed key leads, because 0x65 < 0xc3.
	if want := "{\"cafe\u0301\":2,\"caf\u00e9\":1}"; string(canonKeys) != want {
		t.Fatalf("NFC-equivalent keys must stay distinct:\n  got:  %s\n  want: %s", canonKeys, want)
	}
}

func TestCanonicalJSONDuplicateKeyIsRejected(t *testing.T) {
	raw := json.RawMessage(`{"café":1,"café":2}`)
	if _, err := CanonicalJSON(raw); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("expected ErrDuplicateKey for identical keys, got %v", err)
	}
}

// `quorum N of M` lowers to the operator ">=", so HTML escaping would reach
// every bundle's hash and no other implementation could reproduce it.
func TestCanonicalJSONDoesNotHTMLEscape(t *testing.T) {
	got, err := CanonicalJSON(map[string]any{"operator": ">=", "source": "/b?a=1&b=2"})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if want := `{"operator":">=","source":"/b?a=1&b=2"}`; string(got) != want {
		t.Fatalf("HTML escaping reached the hashed bytes:\n  got:  %s\n  want: %s", got, want)
	}
}
