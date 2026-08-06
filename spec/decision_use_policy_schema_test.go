package spec

import (
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestDecisionUsePolicySchema(t *testing.T) {
	schema := loadDecisionUsePolicySchema(t)
	validBytes := readFixture(t, "testdata/decision-use-policy-v1/valid/policy.json")
	if err := schema.Validate(decodeStrictDocument(t, validBytes)); err != nil {
		t.Fatalf("valid fixture: %v", err)
	}

	var mutations []mutation
	decodeStrictInto(t, readFixture(t, "testdata/decision-use-policy-v1/invalid/cases.json"), &mutations)
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

func loadDecisionUsePolicySchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	document := decodeStrictDocument(t, readFixture(t, "decision-use-policy-v1.schema.json"))
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	if err := compiler.AddResource(DecisionUsePolicyV1SchemaID, document); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile(DecisionUsePolicyV1SchemaID)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return schema
}
