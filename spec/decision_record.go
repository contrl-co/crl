package spec

import (
	_ "embed"
	"encoding/json"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed decision-record-v1.schema.json
var recordSchema []byte

//go:embed decision-record-v1-dialect.json
var recordDialect []byte

// CompileDecisionRecordSchema loads the versioned record contract and its
// required format-assertion dialect without fetching resources from the network.
func CompileDecisionRecordSchema() (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertContent()
	for _, resource := range []struct {
		identifier string
		body       []byte
	}{
		{"https://contrl.co/schemas/crl/decision-record-v1-dialect.json", recordDialect},
		{"https://contrl.co/schemas/crl/decision-record-v1.schema.json", recordSchema},
	} {
		var document any
		if err := json.Unmarshal(resource.body, &document); err != nil {
			return nil, err
		}
		if err := compiler.AddResource(resource.identifier, document); err != nil {
			return nil, err
		}
	}
	return compiler.Compile("https://contrl.co/schemas/crl/decision-record-v1.schema.json")
}
