package decisionrecord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"gitlab.com/contrl-group/crl/spec"
)

const usePolicyDomain = "crl-decision-use-policy/v1"

// ErrUsePolicy marks a malformed, internally inconsistent, or unpinned
// decision use policy. It does not mean the policy is caller-approved.
var ErrUsePolicy = errors.New("decision record: invalid use policy")

var (
	usePolicySchemaOnce sync.Once
	usePolicyV1Schema   *jsonschema.Schema
	usePolicySchemaErr  error
)

// UsePolicy bounds decision freshness and selects the mandatory replay scope
// for one decision-record domain.
type UsePolicy struct {
	SchemaVersion           string `json:"schema_version"`
	PolicyID                string `json:"policy_id"`
	Version                 int64  `json:"version"`
	CreatedAt               string `json:"created_at"`
	Domain                  string `json:"domain"`
	ValidFrom               string `json:"valid_from"`
	ValidUntil              string `json:"valid_until"`
	MaxEvaluationAgeSeconds int64  `json:"max_evaluation_age_seconds"`
	MaxRecordAgeSeconds     int64  `json:"max_record_age_seconds"`
	MaxRecordDelaySeconds   int64  `json:"max_record_delay_seconds"`
	MaxClockSkewSeconds     int64  `json:"max_clock_skew_seconds"`
	ReplayScope             string `json:"replay_scope"`
	PolicyHash              string `json:"policy_hash"`
}

// ParseUsePolicy strictly parses and validates one decision use-policy v1
// document. Approval and policy-hash pinning remain caller responsibilities.
func ParseUsePolicy(body []byte) (*UsePolicy, error) {
	document, err := strictDocument(body)
	if err != nil {
		return nil, usePolicyError("JSON: %v", err)
	}
	if err := validateUsePolicyDocument(document); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, usePolicyError("encode: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var policy UsePolicy
	if err := decoder.Decode(&policy); err != nil {
		return nil, usePolicyError("decode: %v", err)
	}
	if err := validateUsePolicySemantics(&policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

// VerifyUsePolicy revalidates a policy, recomputes its domain-separated hash,
// and requires an exact caller-provided hash pin.
func VerifyUsePolicy(policy *UsePolicy, expectedPolicyHash string) error {
	if err := validateUsePolicy(policy); err != nil {
		return err
	}
	if !canonicalDigest(expectedPolicyHash) {
		return usePolicyError("expected policy hash is not canonical SHA-256")
	}
	actual, err := domainDigest(usePolicyDomain, policy.unsigned())
	if err != nil {
		return usePolicyError("canonical policy: %v", err)
	}
	if policy.PolicyHash != actual {
		return usePolicyError("policy_hash mismatch: got %s, want %s", policy.PolicyHash, actual)
	}
	if expectedPolicyHash != policy.PolicyHash {
		return usePolicyError("policy_hash does not match caller pin")
	}
	return nil
}

func validateUsePolicy(policy *UsePolicy) error {
	if policy == nil {
		return usePolicyError("policy is nil")
	}
	if err := validateUsePolicyUTF8(policy); err != nil {
		return usePolicyError("UTF-8: %v", err)
	}
	body, err := json.Marshal(policy)
	if err != nil {
		return usePolicyError("encode: %v", err)
	}
	document, err := strictDocument(body)
	if err != nil {
		return usePolicyError("JSON: %v", err)
	}
	if err := validateUsePolicyDocument(document); err != nil {
		return err
	}
	return validateUsePolicySemantics(policy)
}

func validateUsePolicyDocument(document any) error {
	schema, err := decisionUsePolicySchema()
	if err != nil {
		return usePolicyError("load embedded schema: %v", err)
	}
	if err := schema.Validate(document); err != nil {
		return usePolicyError("schema: %v", err)
	}
	return nil
}

func decisionUsePolicySchema() (*jsonschema.Schema, error) {
	usePolicySchemaOnce.Do(func() {
		document, err := strictDocument(spec.DecisionUsePolicyV1Schema())
		if err != nil {
			usePolicySchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.AssertFormat()
		if err := compiler.AddResource(spec.DecisionUsePolicyV1SchemaID, document); err != nil {
			usePolicySchemaErr = err
			return
		}
		usePolicyV1Schema, usePolicySchemaErr = compiler.Compile(spec.DecisionUsePolicyV1SchemaID)
	})
	return usePolicyV1Schema, usePolicySchemaErr
}

func validateUsePolicySemantics(policy *UsePolicy) error {
	createdAt, err := usePolicyTime("created_at", policy.CreatedAt)
	if err != nil {
		return err
	}
	validFrom, err := usePolicyTime("valid_from", policy.ValidFrom)
	if err != nil {
		return err
	}
	validUntil, err := usePolicyTime("valid_until", policy.ValidUntil)
	if err != nil {
		return err
	}
	if createdAt.After(validFrom) {
		return usePolicyError("created_at is after valid_from")
	}
	if !validFrom.Before(validUntil) {
		return usePolicyError("policy validity interval is empty")
	}
	if policy.MaxRecordDelaySeconds > policy.MaxEvaluationAgeSeconds {
		return usePolicyError("max_record_delay_seconds exceeds max_evaluation_age_seconds")
	}
	return nil
}

func validateUsePolicyUTF8(policy *UsePolicy) error {
	for _, field := range []namedString{
		{name: "schema_version", value: policy.SchemaVersion},
		{name: "policy_id", value: policy.PolicyID},
		{name: "created_at", value: policy.CreatedAt},
		{name: "domain", value: policy.Domain},
		{name: "valid_from", value: policy.ValidFrom},
		{name: "valid_until", value: policy.ValidUntil},
		{name: "replay_scope", value: policy.ReplayScope},
		{name: "policy_hash", value: policy.PolicyHash},
	} {
		if !utf8.ValidString(field.value) {
			return fmt.Errorf("%s is invalid", field.name)
		}
	}
	return nil
}

func usePolicyTime(name, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, usePolicyError("%s: %v", name, err)
	}
	return parsed, nil
}

func (policy *UsePolicy) unsigned() map[string]any {
	return map[string]any{
		"schema_version":             policy.SchemaVersion,
		"policy_id":                  policy.PolicyID,
		"version":                    policy.Version,
		"created_at":                 policy.CreatedAt,
		"domain":                     policy.Domain,
		"valid_from":                 policy.ValidFrom,
		"valid_until":                policy.ValidUntil,
		"max_evaluation_age_seconds": policy.MaxEvaluationAgeSeconds,
		"max_record_age_seconds":     policy.MaxRecordAgeSeconds,
		"max_record_delay_seconds":   policy.MaxRecordDelaySeconds,
		"max_clock_skew_seconds":     policy.MaxClockSkewSeconds,
		"replay_scope":               policy.ReplayScope,
	}
}

func usePolicyError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUsePolicy, fmt.Sprintf(format, args...))
}
