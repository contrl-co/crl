package decisionrecord

import (
	"bytes"
	"encoding/json"

	crl "gitlab.com/contrl-group/crl"
)

// Record represents a CRL decision-record v1 document. Parse returns a
// structurally valid Record; verification revalidates exported fields in case
// a caller changes them.
type Record struct {
	SchemaVersion string         `json:"schema_version"`
	RecordID      string         `json:"record_id"`
	CreatedAt     string         `json:"created_at"`
	Context       Context        `json:"context"`
	Rule          Rule           `json:"rule"`
	Evaluation    Evaluation     `json:"evaluation"`
	RecordHash    string         `json:"record_hash"`
	Signatures    []Signature    `json:"signatures"`
	Extensions    map[string]any `json:"extensions,omitempty"`
}

type Context struct {
	Domain        string `json:"domain"`
	Subject       string `json:"subject"`
	CorrelationID string `json:"correlation_id"`
}

type Rule struct {
	Edition         string `json:"edition"`
	Source          string `json:"source"`
	SourceHash      string `json:"source_hash"`
	CanonicalText   string `json:"canonical_text"`
	CanonicalBundle string `json:"canonical_bundle"`
	BundleHash      string `json:"bundle_hash"`
}

type Evaluation struct {
	At         string         `json:"at"`
	Facts      map[string]any `json:"facts"`
	Provenance []Provenance   `json:"provenance"`
	Outcome    crl.Result     `json:"outcome"`
	Trace      map[string]any `json:"trace"`
	TraceHash  string         `json:"trace_hash"`
}

type Provenance struct {
	Fact         string `json:"fact"`
	Supplier     string `json:"supplier"`
	Source       string `json:"source"`
	SourceDigest string `json:"source_digest"`
	ObservedAt   string `json:"observed_at"`
}

type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Role      string `json:"role"`
	SignedAt  string `json:"signed_at"`
	Signature string `json:"signature"`
}

func decodeRecord(document any) (*Record, error) {
	body, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (record *Record) unsigned() map[string]any {
	unsigned := map[string]any{
		"schema_version": record.SchemaVersion,
		"record_id":      record.RecordID,
		"created_at":     record.CreatedAt,
		"context":        record.Context,
		"rule":           record.Rule,
		"evaluation":     record.Evaluation,
	}
	if record.Extensions != nil {
		unsigned["extensions"] = record.Extensions
	}
	return unsigned
}
