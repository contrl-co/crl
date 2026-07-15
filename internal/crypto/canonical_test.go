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

// TestCanonicalJSONNormalizesUnicode verifies the NFC equivalence part
// of . "é" (U+00E9) and "e" + U+0301 (combining acute accent) are
// visually identical but consist of different codepoint sequences; any
// hash-over-canonical-json scheme that does NOT normalise would hash
// them to different digests. After NFC normalisation the two forms
// canonicalise to the exact same bytes.
func TestCanonicalJSONNormalizesUnicode(t *testing.T) {
	// String-valued case.
	a := map[string]any{"name": "\u00e9"}             // "é" precomposed
	b := map[string]any{"name": "e\u0301"}            // "e" + combining acute
	c := map[string]any{"name": "\u006e\u0303\u00e9"} // not a match, sanity check

	canonA, err := CanonicalJSON(a)
	if err != nil {
		t.Fatalf("CanonicalJSON(a): %v", err)
	}
	canonB, err := CanonicalJSON(b)
	if err != nil {
		t.Fatalf("CanonicalJSON(b): %v", err)
	}
	canonC, err := CanonicalJSON(c)
	if err != nil {
		t.Fatalf("CanonicalJSON(c): %v", err)
	}

	if string(canonA) != string(canonB) {
		t.Fatalf("NFC equivalence failed:\n  a: %s\n  b: %s", string(canonA), string(canonB))
	}
	if string(canonA) == string(canonC) {
		t.Fatal("sanity check failed: unrelated string should not equal NFC-normalised one")
	}

	// Key-valued case — keys are also normalised.
	ka := map[string]any{"caf\u00e9": 1}  // "café" precomposed
	kb := map[string]any{"cafe\u0301": 1} // "café" decomposed
	canKA, err := CanonicalJSON(ka)
	if err != nil {
		t.Fatalf("CanonicalJSON(ka): %v", err)
	}
	canKB, err := CanonicalJSON(kb)
	if err != nil {
		t.Fatalf("CanonicalJSON(kb): %v", err)
	}
	if string(canKA) != string(canKB) {
		t.Fatalf("NFC key normalisation failed:\n  ka: %s\n  kb: %s", string(canKA), string(canKB))
	}
}

// TestCanonicalJSONNFCKeyCollisionIsDuplicate verifies that two keys
// that are byte-different but NFC-equal collapse to the same key in
// the canonical form and trigger ErrDuplicateKey rather than silently
// winning-last.
func TestCanonicalJSONNFCKeyCollisionIsDuplicate(t *testing.T) {
	// Precomposed "café" and decomposed "café" as distinct keys in
	// the same object must be rejected as duplicates once the keys
	// are NFC-normalised.
	raw := json.RawMessage(`{"caf\u00e9":1,"cafe\u0301":2}`)
	_, err := CanonicalJSON(raw)
	if !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("expected ErrDuplicateKey for NFC-equivalent keys, got %v", err)
	}
}
