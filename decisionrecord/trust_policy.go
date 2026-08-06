package decisionrecord

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"gitlab.com/contrl-group/crl/spec"
)

// ErrTrustPolicy marks a malformed or internally inconsistent decision trust
// policy. It does not mean the policy is approved by a caller.
var ErrTrustPolicy = errors.New("decision record: invalid trust policy")

var (
	trustPolicySchemaOnce sync.Once
	trustPolicyV1Schema   *jsonschema.Schema
	trustPolicySchemaErr  error
)

// TrustPolicy is the public verification material and role policy for one
// decision-record domain.
type TrustPolicy struct {
	SchemaVersion       string       `json:"schema_version"`
	PolicyID            string       `json:"policy_id"`
	Version             int64        `json:"version"`
	CreatedAt           string       `json:"created_at"`
	Domain              string       `json:"domain"`
	ValidFrom           string       `json:"valid_from"`
	ValidUntil          string       `json:"valid_until"`
	MaxClockSkewSeconds int64        `json:"max_clock_skew_seconds"`
	Roles               []RolePolicy `json:"roles"`
	Keys                []TrustedKey `json:"keys"`
	AllowedExtensions   []string     `json:"allowed_extensions"`
	RequiredExtensions  []string     `json:"required_extensions"`
	PolicyHash          string       `json:"policy_hash"`
}

// RolePolicy states how many distinct authorized keys must sign and how long
// after record creation that role may sign.
type RolePolicy struct {
	Role                     string `json:"role"`
	Threshold                int    `json:"threshold"`
	MaxSignatureDelaySeconds int64  `json:"max_signature_delay_seconds"`
}

// TrustedKey binds one Ed25519 public key to one role and validity window.
type TrustedKey struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Role      string `json:"role"`
	PublicKey string `json:"public_key"`
	NotBefore string `json:"not_before"`
	NotAfter  string `json:"not_after"`
	RevokedAt string `json:"revoked_at,omitempty"`
}

// ParseTrustPolicy strictly parses and validates one decision trust-policy v1
// document. Approval and policy-hash pinning remain caller responsibilities.
func ParseTrustPolicy(body []byte) (*TrustPolicy, error) {
	document, err := strictDocument(body)
	if err != nil {
		return nil, policyError("JSON: %v", err)
	}
	if err := validateTrustPolicyDocument(document); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, policyError("encode: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var policy TrustPolicy
	if err := decoder.Decode(&policy); err != nil {
		return nil, policyError("decode: %v", err)
	}
	if err := validateTrustPolicySemantics(&policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func validateTrustPolicy(policy *TrustPolicy) error {
	if policy == nil {
		return policyError("policy is nil")
	}
	if err := validateTrustPolicyUTF8(policy); err != nil {
		return policyError("UTF-8: %v", err)
	}
	body, err := json.Marshal(policy)
	if err != nil {
		return policyError("encode: %v", err)
	}
	document, err := strictDocument(body)
	if err != nil {
		return policyError("JSON: %v", err)
	}
	if err := validateTrustPolicyDocument(document); err != nil {
		return err
	}
	return validateTrustPolicySemantics(policy)
}

func validateTrustPolicyDocument(document any) error {
	schema, err := decisionTrustPolicySchema()
	if err != nil {
		return policyError("load embedded schema: %v", err)
	}
	if err := schema.Validate(document); err != nil {
		return policyError("schema: %v", err)
	}
	return nil
}

func decisionTrustPolicySchema() (*jsonschema.Schema, error) {
	trustPolicySchemaOnce.Do(func() {
		document, err := strictDocument(spec.DecisionTrustPolicyV1Schema())
		if err != nil {
			trustPolicySchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.AssertFormat()
		compiler.AssertContent()
		if err := compiler.AddResource(spec.DecisionTrustPolicyV1SchemaID, document); err != nil {
			trustPolicySchemaErr = err
			return
		}
		trustPolicyV1Schema, trustPolicySchemaErr = compiler.Compile(spec.DecisionTrustPolicyV1SchemaID)
	})
	return trustPolicyV1Schema, trustPolicySchemaErr
}

func validateTrustPolicySemantics(policy *TrustPolicy) error {
	createdAt, err := policyTime("created_at", policy.CreatedAt)
	if err != nil {
		return err
	}
	validFrom, err := policyTime("valid_from", policy.ValidFrom)
	if err != nil {
		return err
	}
	validUntil, err := policyTime("valid_until", policy.ValidUntil)
	if err != nil {
		return err
	}
	if createdAt.After(validFrom) {
		return policyError("created_at is after valid_from")
	}
	if !validFrom.Before(validUntil) {
		return policyError("policy validity interval is empty")
	}

	roles := make(map[string]RolePolicy, len(policy.Roles))
	previousRole := ""
	for index, role := range policy.Roles {
		if index > 0 && role.Role <= previousRole {
			return policyError("roles are not in ascending role order")
		}
		roles[role.Role] = role
		previousRole = role.Role
	}

	keyCounts := make(map[string]int, len(roles))
	publicKeys := make(map[string]struct{}, len(policy.Keys))
	previousKeyID := ""
	for index, key := range policy.Keys {
		if index > 0 && key.KeyID <= previousKeyID {
			return policyError("keys are not in ascending key_id order")
		}
		if _, exists := roles[key.Role]; !exists {
			return policyError("key %q references unknown role %q", key.KeyID, key.Role)
		}
		publicKey, err := base64.StdEncoding.DecodeString(key.PublicKey)
		if err != nil {
			return policyError("key %q public_key: %v", key.KeyID, err)
		}
		identity := string(publicKey)
		if _, exists := publicKeys[identity]; exists {
			return policyError("key %q duplicates public-key material", key.KeyID)
		}
		publicKeys[identity] = struct{}{}
		if err := validateKeyTimes(key); err != nil {
			return err
		}
		keyCounts[key.Role]++
		previousKeyID = key.KeyID
	}
	for _, requirement := range policy.Roles {
		if keyCounts[requirement.Role] < requirement.Threshold {
			return policyError("role %q threshold %d exceeds %d authorized keys", requirement.Role, requirement.Threshold, keyCounts[requirement.Role])
		}
	}

	if err := validateSortedStrings("allowed_extensions", policy.AllowedExtensions); err != nil {
		return err
	}
	if err := validateSortedStrings("required_extensions", policy.RequiredExtensions); err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(policy.AllowedExtensions))
	for _, extension := range policy.AllowedExtensions {
		allowed[extension] = struct{}{}
	}
	for _, extension := range policy.RequiredExtensions {
		if _, exists := allowed[extension]; !exists {
			return policyError("required extension %q is not allowed", extension)
		}
	}
	return nil
}

func validateKeyTimes(key TrustedKey) error {
	notBefore, err := policyTime("key "+key.KeyID+" not_before", key.NotBefore)
	if err != nil {
		return err
	}
	notAfter, err := policyTime("key "+key.KeyID+" not_after", key.NotAfter)
	if err != nil {
		return err
	}
	if !notBefore.Before(notAfter) {
		return policyError("key %q validity interval is empty", key.KeyID)
	}
	if key.RevokedAt == "" {
		return nil
	}
	_, err = policyTime("key "+key.KeyID+" revoked_at", key.RevokedAt)
	return err
}

func validateSortedStrings(name string, values []string) error {
	if !sort.StringsAreSorted(values) {
		return policyError("%s is not in ascending order", name)
	}
	return nil
}

func policyTime(name, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, policyError("%s: %v", name, err)
	}
	return parsed, nil
}

func validateTrustPolicyUTF8(policy *TrustPolicy) error {
	fields := []namedString{
		{name: "schema_version", value: policy.SchemaVersion},
		{name: "policy_id", value: policy.PolicyID},
		{name: "created_at", value: policy.CreatedAt},
		{name: "domain", value: policy.Domain},
		{name: "valid_from", value: policy.ValidFrom},
		{name: "valid_until", value: policy.ValidUntil},
		{name: "policy_hash", value: policy.PolicyHash},
	}
	for index, role := range policy.Roles {
		fields = append(fields, namedString{name: fmt.Sprintf("roles[%d].role", index), value: role.Role})
	}
	for index, key := range policy.Keys {
		fields = append(fields,
			namedString{name: fmt.Sprintf("keys[%d].algorithm", index), value: key.Algorithm},
			namedString{name: fmt.Sprintf("keys[%d].key_id", index), value: key.KeyID},
			namedString{name: fmt.Sprintf("keys[%d].role", index), value: key.Role},
			namedString{name: fmt.Sprintf("keys[%d].public_key", index), value: key.PublicKey},
			namedString{name: fmt.Sprintf("keys[%d].not_before", index), value: key.NotBefore},
			namedString{name: fmt.Sprintf("keys[%d].not_after", index), value: key.NotAfter},
			namedString{name: fmt.Sprintf("keys[%d].revoked_at", index), value: key.RevokedAt},
		)
	}
	for index, extension := range policy.AllowedExtensions {
		fields = append(fields, namedString{name: fmt.Sprintf("allowed_extensions[%d]", index), value: extension})
	}
	for index, extension := range policy.RequiredExtensions {
		fields = append(fields, namedString{name: fmt.Sprintf("required_extensions[%d]", index), value: extension})
	}
	for _, field := range fields {
		if !utf8.ValidString(field.value) {
			return fmt.Errorf("%s is invalid", field.name)
		}
	}
	return nil
}

func (policy *TrustPolicy) unsigned() map[string]any {
	return map[string]any{
		"schema_version":         policy.SchemaVersion,
		"policy_id":              policy.PolicyID,
		"version":                policy.Version,
		"created_at":             policy.CreatedAt,
		"domain":                 policy.Domain,
		"valid_from":             policy.ValidFrom,
		"valid_until":            policy.ValidUntil,
		"max_clock_skew_seconds": policy.MaxClockSkewSeconds,
		"roles":                  policy.Roles,
		"keys":                   policy.Keys,
		"allowed_extensions":     policy.AllowedExtensions,
		"required_extensions":    policy.RequiredExtensions,
	}
}

func policyError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTrustPolicy, fmt.Sprintf(format, args...))
}
