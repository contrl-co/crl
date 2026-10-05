package decisionrecord_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/contrl-co/crl/decisionrecord"
)

func TestEvaluationOutcomeMustMatchTraceResult(test *testing.T) {
	for _, mismatchedField := range []string{"outcome", "trace.result"} {
		test.Run(mismatchedField, func(test *testing.T) {
			var document map[string]any
			decode(test, fixture(test, "valid/authorized.json"), &document)
			evaluation := document["evaluation"].(map[string]any)
			trace := evaluation["trace"].(map[string]any)
			if mismatchedField == "outcome" {
				evaluation["outcome"] = "DENIED"
			} else {
				trace["result"] = "DENIED"
			}
			body, err := json.Marshal(document)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := decisionrecord.Parse(body); !errors.Is(err, decisionrecord.ErrStructure) {
				test.Fatalf("outcome/trace mismatch accepted: %v", err)
			}
		})
	}
}

func TestEveryFactRequiresItsOwnProvenance(test *testing.T) {
	var document map[string]any
	decode(test, fixture(test, "valid/authorized.json"), &document)
	evaluation := document["evaluation"].(map[string]any)
	provenance := evaluation["provenance"].([]any)
	if len(provenance) < 2 {
		test.Fatalf("fixture needs multiple independently sourced facts, got %d", len(provenance))
	}
	for removedIndex, removed := range provenance {
		entry := removed.(map[string]any)
		test.Run(entry["fact"].(string), func(test *testing.T) {
			var mutated map[string]any
			decode(test, fixture(test, "valid/authorized.json"), &mutated)
			facts := mutated["evaluation"].(map[string]any)["facts"].(map[string]any)
			mutatedProvenance := mutated["evaluation"].(map[string]any)["provenance"].([]any)
			if _, exists := facts[entry["fact"].(string)]; !exists || len(mutatedProvenance) <= 1 {
				test.Fatal("mutation must remove provenance for one fact while preserving the other")
			}
			mutatedProvenance = append(mutatedProvenance[:removedIndex], mutatedProvenance[removedIndex+1:]...)
			mutated["evaluation"].(map[string]any)["provenance"] = mutatedProvenance
			body, err := json.Marshal(mutated)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := decisionrecord.Parse(body); !errors.Is(err, decisionrecord.ErrStructure) {
				test.Fatalf("fact without its provenance accepted: %v", err)
			}
		})
	}
}

func TestObservationMetadataRequiresProvenanceForItsBase(test *testing.T) {
	for _, scenario := range []struct {
		name, fact string
		value      any
	}{
		{"metadata of metadata with empty value", "observed_at.observed_at.approved", ""},
		{"metadata of metadata with a timestamp", "observed_at.observed_at.approved", "2026-08-06T14:00:00Z"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			var document map[string]any
			decode(test, fixture(test, "valid/authorized.json"), &document)
			facts := document["evaluation"].(map[string]any)["facts"].(map[string]any)
			facts[scenario.fact] = scenario.value
			body, err := json.Marshal(document)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := decisionrecord.Parse(body); !errors.Is(err, decisionrecord.ErrStructure) {
				test.Fatalf("fact without provenance accepted through observation metadata: %v", err)
			}
		})
	}
}
