package spec

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const decisionTrustPolicySchemaID = "https://contrl.co/schemas/crl/decision-trust-policy-v1.schema.json"

func TestDecisionTrustPolicySchema(t *testing.T) {
	schema := loadDecisionTrustPolicySchema(t)
	validBytes := readFixture(t, "testdata/decision-trust-policy-v1/valid/policy.json")
	valid := decodeStrictDocument(t, validBytes)
	if err := schema.Validate(valid); err != nil {
		t.Fatalf("valid fixture: %v", err)
	}

	for _, boundary := range []mutation{
		{Name: "maximum clock skew", Path: []string{"max_clock_skew_seconds"}, Value: json.Number("3600")},
		{Name: "maximum policy version", Path: []string{"version"}, Value: json.Number("2147483647")},
		{Name: "maximum signature delay", Path: []string{"roles", "0", "max_signature_delay_seconds"}, Value: json.Number("31536000")},
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
	decodeStrictInto(t, readFixture(t, "testdata/decision-trust-policy-v1/invalid/cases.json"), &mutations)
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

func TestDecisionTrustPolicyFixtureHash(t *testing.T) {
	policy := decodeStrictDocument(t, readFixture(t, "testdata/decision-trust-policy-v1/valid/policy.json")).(map[string]any)
	want := policy["policy_hash"]
	delete(policy, "policy_hash")
	if got := domainDigest(t, "crl-decision-trust-policy/v1", policy); got != want {
		t.Fatalf("policy_hash = %v, want %s", want, got)
	}
}

func loadDecisionTrustPolicySchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	document := decodeStrictDocument(t, readFixture(t, "decision-trust-policy-v1.schema.json"))
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.AssertContent()
	if err := compiler.AddResource(decisionTrustPolicySchemaID, document); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile(decisionTrustPolicySchemaID)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return schema
}
