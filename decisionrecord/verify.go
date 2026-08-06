package decisionrecord

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	crl "gitlab.com/contrl-group/crl"
	crlcrypto "gitlab.com/contrl-group/crl/internal/crypto"
)

const (
	traceDomain  = "crl-decision-trace/v1"
	recordDomain = "crl-decision-record/v1"
)

var (
	// ErrIntegrity marks a mismatch in content-addressed record material.
	ErrIntegrity = errors.New("decision record: integrity verification failed")
	// ErrDecision marks a record whose source cannot reproduce its canonical
	// compilation, trace, or outcome.
	ErrDecision = errors.New("decision record: decision verification failed")
)

// VerifyIntegrity recomputes the source, bundle, trace, and record hashes. It
// does not verify signatures or trust, and does not re-run the CRL decision.
func VerifyIntegrity(record *Record) error {
	if err := validateRecord(record); err != nil {
		return err
	}
	if got := crlcrypto.DigestBytes([]byte(record.Rule.Source)); got != record.Rule.SourceHash {
		return integrity("rule.source_hash mismatch: got %s, want %s", record.Rule.SourceHash, got)
	}
	bundle := []byte(record.Rule.CanonicalBundle)
	canonicalBundle, err := crlcrypto.CanonicalJSON(json.RawMessage(bundle))
	if err != nil {
		return integrity("rule.canonical_bundle: %v", err)
	}
	if !bytes.Equal(bundle, canonicalBundle) {
		return integrity("rule.canonical_bundle is not canonical JSON")
	}
	if got := crlcrypto.DigestBytes(bundle); got != record.Rule.BundleHash {
		return integrity("rule.bundle_hash mismatch: got %s, want %s", record.Rule.BundleHash, got)
	}
	traceHash, err := domainDigest(traceDomain, record.Evaluation.Trace)
	if err != nil {
		return integrity("evaluation.trace: %v", err)
	}
	if traceHash != record.Evaluation.TraceHash {
		return integrity("evaluation.trace_hash mismatch: got %s, want %s", record.Evaluation.TraceHash, traceHash)
	}
	recordHash, err := domainDigest(recordDomain, record.unsigned())
	if err != nil {
		return integrity("unsigned record: %v", err)
	}
	if recordHash != record.RecordHash {
		return integrity("record_hash mismatch: got %s, want %s", record.RecordHash, recordHash)
	}
	return nil
}

// VerifyDecision first verifies integrity, then recompiles the source and
// independently re-evaluates the recorded facts at the recorded instant. It
// does not verify signatures, trust, replay, or application context.
func VerifyDecision(record *Record) error {
	if err := VerifyIntegrity(record); err != nil {
		return err
	}
	return verifyDecision(record)
}

func verifyDecision(record *Record) error {
	compiled, err := crl.CompileEdition(record.Rule.Source, record.Rule.Edition)
	if err != nil {
		return decision("compile source: %v", err)
	}
	if compiled.SourceHash != record.Rule.SourceHash {
		return decision("rule.source_hash does not match compiler output")
	}
	if compiled.CanonicalText != record.Rule.CanonicalText {
		return decision("rule.canonical_text does not match compiler output")
	}
	if compiled.Hash != record.Rule.BundleHash {
		return decision("rule.bundle_hash does not match compiler output")
	}
	if crlcrypto.DigestBytes([]byte(record.Rule.CanonicalBundle)) != compiled.Hash {
		return decision("rule.canonical_bundle does not match compiler output")
	}

	at, err := time.Parse(time.RFC3339Nano, record.Evaluation.At)
	if err != nil {
		return decision("evaluation.at: %v", err)
	}
	actualTrace, err := normalizedTrace(compiled.EvaluateAt(record.Evaluation.Facts, at))
	if err != nil {
		return decision("encode recomputed trace: %v", err)
	}
	if equal, err := canonicalEqual(actualTrace, record.Evaluation.Trace); err != nil {
		return decision("compare trace: %v", err)
	} else if !equal {
		return decision("evaluation.trace does not match recomputed trace")
	}
	if result, ok := actualTrace["result"].(string); !ok || crl.Result(result) != record.Evaluation.Outcome {
		return decision("evaluation.outcome does not match recomputed outcome")
	}
	return nil
}

func normalizedTrace(evaluation crl.Evaluation) (map[string]any, error) {
	body, err := json.Marshal(evaluation)
	if err != nil {
		return nil, err
	}
	document, err := strictDocument(body)
	if err != nil {
		return nil, err
	}
	trace, ok := document.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("evaluation encoded as %T, want object", document)
	}
	ensureArray(trace, "rules")
	ensureArray(trace, "clusters")
	ensureArray(trace, "global_checks")
	ensureArray(trace, "checks")
	for _, item := range trace["rules"].([]any) {
		if rule, ok := item.(map[string]any); ok {
			ensureArray(rule, "checks")
		}
	}
	for _, item := range trace["clusters"].([]any) {
		if cluster, ok := item.(map[string]any); ok {
			ensureArray(cluster, "members")
			ensureArray(cluster, "checks")
		}
	}
	return trace, nil
}

func ensureArray(object map[string]any, field string) {
	if _, exists := object[field]; !exists {
		object[field] = []any{}
	}
}

func canonicalEqual(left, right any) (bool, error) {
	leftBytes, err := crlcrypto.CanonicalJSON(left)
	if err != nil {
		return false, err
	}
	rightBytes, err := crlcrypto.CanonicalJSON(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftBytes, rightBytes), nil
}

func domainDigest(domain string, value any) (string, error) {
	body, err := crlcrypto.CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	input := make([]byte, 0, len(domain)+1+len(body))
	input = append(input, domain...)
	input = append(input, 0)
	input = append(input, body...)
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:]), nil
}

func integrity(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrIntegrity, fmt.Sprintf(format, args...))
}

func decision(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrDecision, fmt.Sprintf(format, args...))
}
