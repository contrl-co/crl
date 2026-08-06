package decisionrecord

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestParsePublishedFixture(t *testing.T) {
	if _, err := Parse(readPublishedFixture(t)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestParseRejectsMalformedAndAmbiguousDocuments(t *testing.T) {
	valid := readPublishedFixture(t)
	tests := map[string][]byte{
		"invalid UTF-8":           {'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'},
		"duplicate key":           []byte(`{"schema_version":"crl-decision-record/v1","schema_version":"crl-decision-record/v1"}`),
		"trailing document":       append(append([]byte(nil), valid...), []byte(` {}`)...),
		"unpaired high surrogate": []byte(`{"x":"\ud800"}`),
		"unpaired low surrogate":  []byte(`{"x":"\udc00"}`),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(body); !errors.Is(err, ErrStructural) {
				t.Fatalf("Parse error = %v, want ErrStructural", err)
			}
		})
	}
}

func TestParseRejectsSchemaDowngradeAndUnknownFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "unsupported version", mutate: func(document map[string]any) {
			document["schema_version"] = "crl-decision-record/v0"
		}},
		{name: "unknown top-level field", mutate: func(document map[string]any) {
			document["trusted"] = true
		}},
		{name: "unknown nested field", mutate: func(document map[string]any) {
			document["context"].(map[string]any)["tenant"] = "example"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := decodeParseDocument(t, readPublishedFixture(t))
			test.mutate(document)
			if _, err := Parse(marshalParseDocument(t, document)); !errors.Is(err, ErrStructural) {
				t.Fatalf("Parse error = %v, want ErrStructural", err)
			}
		})
	}
}

func TestParseEnforcesProvenanceInvariants(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "missing fact provenance", mutate: func(document map[string]any) {
			evaluation := document["evaluation"].(map[string]any)
			evaluation["provenance"] = evaluation["provenance"].([]any)[1:]
		}},
		{name: "duplicate fact provenance", mutate: func(document map[string]any) {
			evaluation := document["evaluation"].(map[string]any)
			items := evaluation["provenance"].([]any)
			duplicate := cloneParseDocument(t, items[0]).(map[string]any)
			duplicate["source"] = "/evidence/duplicate.json"
			evaluation["provenance"] = append(items, duplicate)
		}},
		{name: "provenance for absent fact", mutate: func(document map[string]any) {
			evaluation := document["evaluation"].(map[string]any)
			items := evaluation["provenance"].([]any)
			extra := cloneParseDocument(t, items[0]).(map[string]any)
			extra["fact"] = "unused"
			evaluation["provenance"] = append(items, extra)
		}},
		{name: "provenance for observation metadata", mutate: func(document map[string]any) {
			evaluation := document["evaluation"].(map[string]any)
			items := evaluation["provenance"].([]any)
			extra := cloneParseDocument(t, items[0]).(map[string]any)
			extra["fact"] = "observed_at.approved"
			evaluation["provenance"] = []any{items[0], extra, items[1]}
		}},
		{name: "unsorted provenance", mutate: func(document map[string]any) {
			evaluation := document["evaluation"].(map[string]any)
			items := evaluation["provenance"].([]any)
			items[0], items[1] = items[1], items[0]
		}},
		{name: "orphan observation metadata", mutate: func(document map[string]any) {
			facts := document["evaluation"].(map[string]any)["facts"].(map[string]any)
			facts["observed_at.orphan"] = "2026-08-06T15:00:00Z"
		}},
		{name: "mismatched observation metadata", mutate: func(document map[string]any) {
			facts := document["evaluation"].(map[string]any)["facts"].(map[string]any)
			facts["observed_at.approved"] = "2026-08-06T14:00:01Z"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := decodeParseDocument(t, readPublishedFixture(t))
			test.mutate(document)
			if _, err := Parse(marshalParseDocument(t, document)); !errors.Is(err, ErrStructural) {
				t.Fatalf("Parse error = %v, want ErrStructural", err)
			}
		})
	}
}

func TestParseRejectsImpossibleRecordTimeOrder(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "record predates evaluation", mutate: func(document map[string]any) {
			document["created_at"] = "2026-08-06T14:59:59Z"
		}},
		{name: "evidence observed after evaluation", mutate: func(document map[string]any) {
			evaluation := document["evaluation"].(map[string]any)
			provenance := evaluation["provenance"].([]any)[0].(map[string]any)
			provenance["observed_at"] = "2026-08-06T15:00:01Z"
			fact := provenance["fact"].(string)
			evaluation["facts"].(map[string]any)["observed_at."+fact] = provenance["observed_at"]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := decodeParseDocument(t, readPublishedFixture(t))
			test.mutate(document)
			if _, err := Parse(marshalParseDocument(t, document)); !errors.Is(err, ErrStructural) {
				t.Fatalf("Parse error = %v, want ErrStructural", err)
			}
		})
	}
}

func TestParseEnforcesSignatureIdentityAndOrder(t *testing.T) {
	signature := func(role, keyID string) map[string]any {
		return map[string]any{
			"algorithm": "ed25519",
			"key_id":    keyID,
			"role":      role,
			"signed_at": "2026-08-06T15:00:00Z",
			"signature": base64.StdEncoding.EncodeToString(make([]byte, 64)),
		}
	}
	for _, test := range []struct {
		name       string
		signatures []any
	}{
		{name: "duplicate identity", signatures: []any{
			signature("issuer", "key-a"), func() map[string]any {
				second := signature("issuer", "key-a")
				second["signed_at"] = "2026-08-06T15:00:01Z"
				return second
			}(),
		}},
		{name: "unsorted identities", signatures: []any{
			signature("reviewer", "key-b"), signature("issuer", "key-a"),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := decodeParseDocument(t, readPublishedFixture(t))
			document["signatures"] = test.signatures
			if _, err := Parse(marshalParseDocument(t, document)); !errors.Is(err, ErrStructural) {
				t.Fatalf("Parse error = %v, want ErrStructural", err)
			}
		})
	}
}

func TestParseAcceptsValidSurrogatePair(t *testing.T) {
	body := readPublishedFixture(t)
	body = bytes.Replace(body, []byte(`"example:record:1"`), []byte(`"example:record:\ud83d\ude00"`), 1)
	if _, err := Parse(body); err != nil {
		t.Fatalf("Parse valid surrogate pair: %v", err)
	}
}

func FuzzParseNeverPanics(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"schema_version":"crl-decision-record/v1"}`))
	f.Add([]byte{'"', 0xff, '"'})
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = Parse(body)
	})
}

func readPublishedFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("../spec/testdata/decision-record-v1/valid/authorized.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func decodeParseDocument(t *testing.T, body []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	return document
}

func cloneParseDocument(t *testing.T, value any) any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var clone any
	if err := decoder.Decode(&clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func marshalParseDocument(t *testing.T, document any) []byte {
	t.Helper()
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
