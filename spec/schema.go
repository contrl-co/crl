package spec

import _ "embed"

const DecisionRecordV1SchemaID = "https://contrl.co/schemas/crl/decision-record-v1.schema.json"

const DecisionTrustPolicyV1SchemaID = "https://contrl.co/schemas/crl/decision-trust-policy-v1.schema.json"

const DecisionUsePolicyV1SchemaID = "https://contrl.co/schemas/crl/decision-use-policy-v1.schema.json"

// decisionRecordV1Schema is embedded so verifiers do not depend on a working
// directory or an external schema server.
//
//go:embed decision-record-v1.schema.json
var decisionRecordV1Schema []byte

// decisionTrustPolicyV1Schema is embedded for offline policy verification.
//
//go:embed decision-trust-policy-v1.schema.json
var decisionTrustPolicyV1Schema []byte

// decisionUsePolicyV1Schema is embedded for offline context and replay-policy
// verification.
//
//go:embed decision-use-policy-v1.schema.json
var decisionUsePolicyV1Schema []byte

// DecisionRecordV1Schema returns an owned copy of the v1 JSON Schema.
func DecisionRecordV1Schema() []byte {
	return append([]byte(nil), decisionRecordV1Schema...)
}

// DecisionTrustPolicyV1Schema returns an owned copy of the v1 JSON Schema.
func DecisionTrustPolicyV1Schema() []byte {
	return append([]byte(nil), decisionTrustPolicyV1Schema...)
}

// DecisionUsePolicyV1Schema returns an owned copy of the v1 JSON Schema.
func DecisionUsePolicyV1Schema() []byte {
	return append([]byte(nil), decisionUsePolicyV1Schema...)
}
