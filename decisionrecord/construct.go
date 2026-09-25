package decisionrecord

import (
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"

	crlcrypto "github.com/contrl-co/crl/internal/crypto"
)

// Input supplies every assertion needed for an unsigned v1 record. Creation
// neither invents missing identities/times nor approves these caller claims.
type Input struct {
	RecordID    string          `json:"record_id"`
	CreatedAt   string          `json:"created_at"`
	Context     Context         `json:"context"`
	Rule        RuleInput       `json:"rule"`
	Evaluation  EvaluationInput `json:"evaluation"`
	TrustPolicy PolicyReference `json:"trust_policy"`
	Extensions  json.RawMessage `json:"extensions,omitempty"`
}

type Context struct {
	Domain        string `json:"domain"`
	Subject       string `json:"subject"`
	CorrelationID string `json:"correlation_id"`
}

type RuleInput struct {
	Edition         string `json:"edition"`
	Source          string `json:"source"`
	CanonicalText   string `json:"canonical_text"`
	CanonicalBundle string `json:"canonical_bundle"`
}

type EvaluationInput struct {
	At         string            `json:"at"`
	Facts      json.RawMessage   `json:"facts"`
	Provenance []Provenance      `json:"provenance"`
	Outcome    string            `json:"outcome"`
	Trace      json.RawMessage   `json:"trace"`
	Evaluator  EvaluatorIdentity `json:"evaluator"`
}

type Provenance struct {
	Fact         string `json:"fact"`
	Supplier     string `json:"supplier"`
	Source       string `json:"source"`
	SourceDigest string `json:"source_digest"`
	ObservedAt   string `json:"observed_at"`
}

type EvaluatorIdentity struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type PolicyReference struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

// NewUnsigned computes the v1 source, bundle, trace and record hashes from
// complete caller inputs, sorts provenance and validates the finished record.
// Facts and trace retain exact numeric tokens. No compiler is run and no policy,
// provenance assertion or evaluator identity becomes trusted by construction.
func NewUnsigned(input Input) (*Record, error) {
	// encoding/json repairs invalid UTF-8 in Go strings; identity claims and
	// hash inputs must be rejected before that lossy conversion can occur.
	stringsToCheck := []string{
		input.RecordID, input.CreatedAt, input.Context.Domain, input.Context.Subject, input.Context.CorrelationID,
		input.Rule.Edition, input.Rule.Source, input.Rule.CanonicalText, input.Rule.CanonicalBundle,
		input.Evaluation.At, input.Evaluation.Outcome, input.Evaluation.Evaluator.ID, input.Evaluation.Evaluator.Revision,
		input.TrustPolicy.ID, input.TrustPolicy.Hash,
	}
	for _, entry := range input.Evaluation.Provenance {
		stringsToCheck = append(stringsToCheck, entry.Fact, entry.Supplier, entry.Source, entry.SourceDigest, entry.ObservedAt)
	}
	for _, value := range stringsToCheck {
		if !utf8.ValidString(value) {
			return nil, fmt.Errorf("%w: invalid UTF-8 construction input", ErrStructure)
		}
	}
	for _, raw := range []json.RawMessage{input.Evaluation.Facts, input.Evaluation.Trace, input.Extensions} {
		if !utf8.Valid(raw) {
			return nil, fmt.Errorf("%w: invalid UTF-8 construction input", ErrStructure)
		}
	}
	input.Evaluation.Provenance = append([]Provenance{}, input.Evaluation.Provenance...)
	sort.Slice(input.Evaluation.Provenance, func(left, right int) bool {
		return input.Evaluation.Provenance[left].Fact < input.Evaluation.Provenance[right].Fact
	})
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("%w: encode construction input", ErrStructure)
	}
	value, err := strictDocument(body)
	if err != nil {
		return nil, fmt.Errorf("%w: construction input: %v", ErrStructure, err)
	}
	document := value.(map[string]any)
	document["schema_version"] = "crl-decision-record/v1"
	rule := document["rule"].(map[string]any)
	rule["source_hash"] = crlcrypto.DigestBytes([]byte(input.Rule.Source))
	rule["bundle_hash"] = crlcrypto.DigestBytes([]byte(input.Rule.CanonicalBundle))
	evaluation := document["evaluation"].(map[string]any)
	evaluation["trace_hash"], err = digest("crl-decision-trace/v1", evaluation["trace"])
	if err != nil {
		return nil, fmt.Errorf("%w: trace digest: %v", ErrStructure, err)
	}
	document["record_hash"], err = digest("crl-decision-record/v1", document)
	if err != nil {
		return nil, fmt.Errorf("%w: record digest: %v", ErrStructure, err)
	}
	document["signatures"] = []any{}
	wire, err := crlcrypto.CanonicalJSON(document)
	if err != nil {
		return nil, fmt.Errorf("%w: constructed record: %v", ErrStructure, err)
	}
	return Parse(wire)
}
