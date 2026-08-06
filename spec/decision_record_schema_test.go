package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"

	crl "gitlab.com/contrl-group/crl"
	lang "gitlab.com/contrl-group/crl/internal/crl"
	crlcrypto "gitlab.com/contrl-group/crl/internal/crypto"
)

const decisionRecordSchemaID = "https://contrl.co/schemas/crl/decision-record-v1.schema.json"

type mutation struct {
	Name  string   `json:"name"`
	Path  []string `json:"path"`
	Value any      `json:"value"`
}

func TestDecisionRecordSchema(t *testing.T) {
	schema := loadDecisionRecordSchema(t)
	validBytes := readFixture(t, "testdata/decision-record-v1/valid/authorized.json")
	valid := decodeStrictDocument(t, validBytes)
	if err := schema.Validate(valid); err != nil {
		t.Fatalf("valid fixture: %v", err)
	}
	structurallySigned := decodeStrictDocument(t, validBytes).(map[string]any)
	structurallySigned["signatures"] = []any{map[string]any{
		"algorithm": "ed25519",
		"key_id":    "issuer-2026-01",
		"role":      "issuer",
		"signed_at": "2026-08-06T15:00:00Z",
		"signature": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==",
	}}
	if err := schema.Validate(structurallySigned); err != nil {
		t.Fatalf("structurally valid signature envelope: %v", err)
	}
	for _, boundary := range []mutation{
		{Name: "maximum safe number", Path: []string{"evaluation", "facts", "approved"}, Value: json.Number("9007199254740991")},
		{Name: "minimum safe number", Path: []string{"evaluation", "facts", "approved"}, Value: json.Number("-9007199254740991")},
		{Name: "nanosecond timestamp", Path: []string{"created_at"}, Value: "2026-08-06T15:00:00.123456789Z"},
	} {
		t.Run(boundary.Name, func(t *testing.T) {
			document := decodeStrictDocument(t, validBytes)
			if err := replaceAtPath(document, boundary.Path, boundary.Value); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(document); err != nil {
				t.Fatalf("schema rejected valid boundary: %v", err)
			}
		})
	}

	var mutations []mutation
	decodeStrictInto(t, readFixture(t, "testdata/decision-record-v1/invalid/cases.json"), &mutations)
	for _, mutation := range mutations {
		t.Run(mutation.Name, func(t *testing.T) {
			document := decodeStrictDocument(t, validBytes)
			if err := replaceAtPath(document, mutation.Path, mutation.Value); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(document); err == nil {
				t.Fatal("schema accepted invalid fixture mutation")
			}
		})
	}
}

func TestDecisionRecordStrictJSON(t *testing.T) {
	tests := map[string][]byte{
		"invalid UTF-8":     {'"', 0xff, '"'},
		"duplicate key":     []byte(`{"record_id":"one","record_id":"two"}`),
		"trailing document": []byte(`{} {}`),
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := strictDocument(document); err == nil {
				t.Fatal("strict parser accepted invalid JSON")
			}
		})
	}
}

func TestAuthorizedDecisionRecordFixtureIsReproducible(t *testing.T) {
	record := decodeStrictDocument(t, readFixture(t, "testdata/decision-record-v1/valid/authorized.json")).(map[string]any)
	rule := record["rule"].(map[string]any)
	evaluation := record["evaluation"].(map[string]any)
	source := rule["source"].(string)

	compiled, err := crl.Compile(source)
	if err != nil {
		t.Fatalf("compile fixture source: %v", err)
	}
	compilation, err := lang.CompileLanguage(source)
	if err != nil {
		t.Fatalf("compile fixture language: %v", err)
	}
	canonicalBundle, err := crlcrypto.CanonicalJSON(compilation.Bundle)
	if err != nil {
		t.Fatalf("canonical bundle: %v", err)
	}

	wantRule := map[string]string{
		"edition":          compiled.Edition,
		"source_hash":      compiled.SourceHash,
		"canonical_text":   compiled.CanonicalText,
		"canonical_bundle": string(canonicalBundle),
		"bundle_hash":      compiled.Hash,
	}
	for field, want := range wantRule {
		if got := rule[field]; got != want {
			t.Errorf("rule.%s = %v, want %q", field, got, want)
		}
	}

	at, err := time.Parse(time.RFC3339Nano, evaluation["at"].(string))
	if err != nil {
		t.Fatalf("parse evaluation time: %v", err)
	}
	facts := evaluation["facts"].(map[string]any)
	actualTrace := jsonValue(t, compiled.EvaluateAt(facts, at)).(map[string]any)
	for _, field := range []string{"rules", "clusters", "global_checks", "checks"} {
		if _, ok := actualTrace[field]; !ok {
			actualTrace[field] = []any{}
		}
	}
	if !reflect.DeepEqual(actualTrace, evaluation["trace"]) {
		t.Fatalf("fixture trace does not reproduce\ngot:  %v\nwant: %v", actualTrace, evaluation["trace"])
	}
	if got, want := evaluation["outcome"], actualTrace["result"]; got != want {
		t.Errorf("evaluation.outcome = %v, trace.result = %v", got, want)
	}
	if got, want := evaluation["trace_hash"], domainDigest(t, "crl-decision-trace/v1", evaluation["trace"]); got != want {
		t.Errorf("evaluation.trace_hash = %v, want %s", got, want)
	}

	unsigned := make(map[string]any, len(record)-2)
	for field, value := range record {
		if field != "record_hash" && field != "signatures" {
			unsigned[field] = value
		}
	}
	if got, want := record["record_hash"], domainDigest(t, "crl-decision-record/v1", unsigned); got != want {
		t.Errorf("record_hash = %v, want %s", got, want)
	}
}

func loadDecisionRecordSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	document := decodeStrictDocument(t, readFixture(t, "decision-record-v1.schema.json"))
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.AssertContent()
	if err := compiler.AddResource(decisionRecordSchemaID, document); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile(decisionRecordSchemaID)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return schema
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return body
}

func decodeStrictDocument(t *testing.T, body []byte) any {
	t.Helper()
	document, err := strictDocument(body)
	if err != nil {
		t.Fatalf("decode strict JSON: %v", err)
	}
	return document
}

func strictDocument(body []byte) (any, error) {
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	if _, err := crlcrypto.CanonicalJSON(json.RawMessage(body)); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing JSON document")
		}
		return nil, err
	}
	return document, nil
}

func decodeStrictInto(t *testing.T, body []byte, destination any) {
	t.Helper()
	document := decodeStrictDocument(t, body)
	canonical, err := crlcrypto.CanonicalJSON(document)
	if err != nil {
		t.Fatalf("canonicalize fixture: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
}

func replaceAtPath(document any, path []string, value any) error {
	if len(path) == 0 {
		return fmt.Errorf("mutation path is empty")
	}
	if len(path) == 1 {
		switch node := document.(type) {
		case map[string]any:
			node[path[0]] = value
			return nil
		case []any:
			index, err := strconv.Atoi(path[0])
			if err != nil || index < 0 || index >= len(node) {
				return fmt.Errorf("mutation index %q is invalid", path[0])
			}
			node[index] = value
			return nil
		default:
			return fmt.Errorf("mutation parent is %T, want object or array", document)
		}
	}
	var child any
	switch node := document.(type) {
	case map[string]any:
		var ok bool
		child, ok = node[path[0]]
		if !ok {
			return fmt.Errorf("mutation field %q does not exist", path[0])
		}
	case []any:
		index, err := strconv.Atoi(path[0])
		if err != nil || index < 0 || index >= len(node) {
			return fmt.Errorf("mutation index %q is invalid", path[0])
		}
		child = node[index]
	default:
		return fmt.Errorf("mutation path %q crosses %T", path[0], document)
	}
	return replaceAtPath(child, path[1:], value)
}

func jsonValue(t *testing.T, value any) any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON value: %v", err)
	}
	return decodeStrictDocument(t, body)
}

func domainDigest(t *testing.T, domain string, value any) string {
	t.Helper()
	body, err := crlcrypto.CanonicalJSON(value)
	if err != nil {
		t.Fatalf("canonicalize %s: %v", domain, err)
	}
	input := make([]byte, 0, len(domain)+1+len(body))
	input = append(input, domain...)
	input = append(input, 0)
	input = append(input, body...)
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:])
}
