package decisionrecord

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	crl "gitlab.com/contrl-group/crl"
	lang "gitlab.com/contrl-group/crl/internal/crl"
	crlcrypto "gitlab.com/contrl-group/crl/internal/crypto"
)

const verifierSource = `crl v1
package examples.permits
bundle permit.application

rule permit_application
	target permit.application
	collector application_file municipality file_upload from /bundles/application.json
		signal application_complete bool from application.complete ttl 30d
		signal permit_hold_active bool from permit.hold_active ttl 30d
	need application_complete == true
	block permit_hold_active
	quorum application_file
`

func TestPublishedFixtureVerifies(t *testing.T) {
	body, err := os.ReadFile("../spec/testdata/decision-record-v1/valid/authorized.json")
	if err != nil {
		t.Fatal(err)
	}
	record, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := VerifyDecision(record); err != nil {
		t.Fatalf("VerifyDecision: %v", err)
	}
}

func TestParseAndVerifyDecisionAcrossFiveOutcomes(t *testing.T) {
	at := time.Date(2026, 8, 6, 15, 0, 0, 0, time.UTC)
	baseFacts := func() map[string]any {
		observed := at.Format(time.RFC3339Nano)
		return map[string]any{
			"application_complete":             true,
			"permit_hold_active":               false,
			"application_file":                 true,
			"observed_at.application_complete": observed,
			"observed_at.permit_hold_active":   observed,
		}
	}
	tests := []struct {
		name   string
		want   crl.Result
		mutate func(map[string]any)
	}{
		{name: "authorized", want: crl.Authorized, mutate: func(map[string]any) {}},
		{name: "denied", want: crl.Denied, mutate: func(facts map[string]any) {
			facts["application_complete"] = false
		}},
		{name: "blocked", want: crl.Blocked, mutate: func(facts map[string]any) {
			facts["permit_hold_active"] = true
		}},
		{name: "insufficient evidence", want: crl.InsufficientEvidence, mutate: func(facts map[string]any) {
			delete(facts, "application_complete")
			delete(facts, "observed_at.application_complete")
		}},
		{name: "expired", want: crl.Expired, mutate: func(facts map[string]any) {
			facts["observed_at.application_complete"] = at.Add(-31 * 24 * time.Hour).Format(time.RFC3339Nano)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := baseFacts()
			test.mutate(facts)
			body := buildRecord(t, verifierSource, facts, at)
			record, err := Parse(body)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if record.Evaluation.Outcome != test.want {
				t.Fatalf("outcome = %s, want %s", record.Evaluation.Outcome, test.want)
			}
			if err := VerifyDecision(record); err != nil {
				t.Fatalf("VerifyDecision: %v", err)
			}
		})
	}
}

func TestVerifyIntegrityRevalidatesMutatedRecord(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Record)
	}{
		{name: "nil record", mutate: func(*Record) {}},
		{name: "missing required array", mutate: func(record *Record) { record.Signatures = nil }},
		{name: "invalid UTF-8 field", mutate: func(record *Record) { record.Context.Subject = string([]byte{0xff}) }},
		{name: "invalid UTF-8 fact key", mutate: func(record *Record) { record.Evaluation.Facts[string([]byte{0xff})] = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var record *Record
			if test.name != "nil record" {
				record = parseTestRecord(t)
				test.mutate(record)
			}
			if err := VerifyIntegrity(record); !errors.Is(err, ErrStructural) {
				t.Fatalf("VerifyIntegrity error = %v, want ErrStructural", err)
			}
		})
	}
}

func TestVerifyIntegrityDetectsEveryAddressedComponent(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{name: "source hash", mutate: func(record *Record) { record.Rule.SourceHash = strings.Repeat("0", 64) }},
		{name: "noncanonical bundle", mutate: func(record *Record) {
			var bundle any
			if err := json.Unmarshal([]byte(record.Rule.CanonicalBundle), &bundle); err != nil {
				t.Fatal(err)
			}
			body, err := json.MarshalIndent(bundle, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			record.Rule.CanonicalBundle = string(body)
		}},
		{name: "bundle hash", mutate: func(record *Record) { record.Rule.BundleHash = strings.Repeat("0", 64) }},
		{name: "trace hash", mutate: func(record *Record) { record.Evaluation.TraceHash = strings.Repeat("0", 64) }},
		{name: "record hash", mutate: func(record *Record) { record.RecordHash = strings.Repeat("0", 64) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := parseTestRecord(t)
			test.mutate(record)
			if err := VerifyIntegrity(record); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("VerifyIntegrity error = %v, want ErrIntegrity", err)
			}
		})
	}
}

func TestVerifyIntegrityBindsContextAndEmptyExtensions(t *testing.T) {
	record := parseTestRecord(t)
	record.Context.Subject = "example:record:other"
	if err := VerifyIntegrity(record); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("context tamper error = %v, want ErrIntegrity", err)
	}

	record = parseTestRecord(t)
	record.Extensions = map[string]any{}
	rehashRecord(t, record)
	if err := VerifyIntegrity(record); err != nil {
		t.Fatalf("empty present extensions must be hash-bound: %v", err)
	}
}

func TestVerifyDecisionRejectsRehashedIncorrectClaims(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{name: "canonical text", mutate: func(record *Record) {
			record.Rule.CanonicalText += "\n"
		}},
		{name: "trace", mutate: func(record *Record) {
			record.Evaluation.Trace["authorized"] = false
			rehashTrace(t, record)
		}},
		{name: "outcome", mutate: func(record *Record) {
			record.Evaluation.Outcome = crl.Denied
		}},
		{name: "source", mutate: func(record *Record) {
			record.Rule.Source = "not crl"
			record.Rule.SourceHash = crlcrypto.DigestBytes([]byte(record.Rule.Source))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := parseTestRecord(t)
			test.mutate(record)
			rehashRecord(t, record)
			if err := VerifyIntegrity(record); err != nil {
				t.Fatalf("test setup must preserve integrity: %v", err)
			}
			if err := VerifyDecision(record); !errors.Is(err, ErrDecision) {
				t.Fatalf("VerifyDecision error = %v, want ErrDecision", err)
			}
		})
	}
}

func TestVerifyDecisionDoesNotClaimSignatureTrust(t *testing.T) {
	document := decodeTestDocument(t, buildRecord(t, verifierSource, verifierFacts(), verifierTime()))
	document["signatures"] = []any{map[string]any{
		"algorithm": "ed25519",
		"key_id":    "untrusted-key",
		"role":      "issuer",
		"signed_at": verifierTime().Format(time.RFC3339Nano),
		"signature": base64.StdEncoding.EncodeToString(make([]byte, 64)),
	}}
	record, err := Parse(marshalTestDocument(t, document))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := VerifyDecision(record); err != nil {
		t.Fatalf("deterministic decision verification must remain separate from trust: %v", err)
	}
}

func buildRecord(t *testing.T, source string, facts map[string]any, at time.Time) []byte {
	t.Helper()
	compiled, err := crl.Compile(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	compilation, err := lang.CompileLanguage(source)
	if err != nil {
		t.Fatalf("compile language: %v", err)
	}
	canonicalBundle, err := crlcrypto.CanonicalJSON(compilation.Bundle)
	if err != nil {
		t.Fatalf("canonical bundle: %v", err)
	}
	trace, err := normalizedTrace(compiled.EvaluateAt(facts, at))
	if err != nil {
		t.Fatalf("normalize trace: %v", err)
	}
	traceHash, err := domainDigest(traceDomain, trace)
	if err != nil {
		t.Fatalf("trace hash: %v", err)
	}
	provenance := provenanceForFacts(facts, at)
	record := &Record{
		SchemaVersion: "crl-decision-record/v1",
		RecordID:      "01989f7e-7b80-7000-8000-000000000001",
		CreatedAt:     at.Format(time.RFC3339Nano),
		Context: Context{
			Domain:        "contrl.co/example",
			Subject:       "example:record:1",
			CorrelationID: "01989f7e-7b80-7000-8000-000000000002",
		},
		Rule: Rule{
			Edition:         compiled.Edition,
			Source:          source,
			SourceHash:      compiled.SourceHash,
			CanonicalText:   compiled.CanonicalText,
			CanonicalBundle: string(canonicalBundle),
			BundleHash:      compiled.Hash,
		},
		Evaluation: Evaluation{
			At:         at.Format(time.RFC3339Nano),
			Facts:      facts,
			Provenance: provenance,
			Outcome:    compiled.EvaluateAt(facts, at).Result,
			Trace:      trace,
			TraceHash:  traceHash,
		},
		Signatures: []Signature{},
	}
	rehashRecord(t, record)
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	return body
}

func provenanceForFacts(facts map[string]any, at time.Time) []Provenance {
	names := make([]string, 0, len(facts))
	for name := range facts {
		if !strings.HasPrefix(name, "observed_at.") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	items := make([]Provenance, 0, len(names))
	for _, name := range names {
		observedAt := at.Format(time.RFC3339Nano)
		if value, ok := facts["observed_at."+name].(string); ok {
			observedAt = value
		}
		source := "/evidence/" + name + ".json"
		digest := sha256.Sum256([]byte(source))
		items = append(items, Provenance{
			Fact:         name,
			Supplier:     "test-supplier",
			Source:       source,
			SourceDigest: hex.EncodeToString(digest[:]),
			ObservedAt:   observedAt,
		})
	}
	return items
}

func verifierTime() time.Time {
	return time.Date(2026, 8, 6, 15, 0, 0, 0, time.UTC)
}

func verifierFacts() map[string]any {
	at := verifierTime().Format(time.RFC3339Nano)
	return map[string]any{
		"application_complete":             true,
		"permit_hold_active":               false,
		"application_file":                 true,
		"observed_at.application_complete": at,
		"observed_at.permit_hold_active":   at,
	}
}

func parseTestRecord(t *testing.T) *Record {
	t.Helper()
	record, err := Parse(buildRecord(t, verifierSource, verifierFacts(), verifierTime()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := VerifyDecision(record); err != nil {
		t.Fatalf("VerifyDecision fixture: %v", err)
	}
	return record
}

func rehashTrace(t *testing.T, record *Record) {
	t.Helper()
	digest, err := domainDigest(traceDomain, record.Evaluation.Trace)
	if err != nil {
		t.Fatal(err)
	}
	record.Evaluation.TraceHash = digest
}

func rehashRecord(t *testing.T, record *Record) {
	t.Helper()
	digest, err := domainDigest(recordDomain, record.unsigned())
	if err != nil {
		t.Fatal(err)
	}
	record.RecordHash = digest
}

func decodeTestDocument(t *testing.T, body []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	return document
}

func marshalTestDocument(t *testing.T, document any) []byte {
	t.Helper()
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
