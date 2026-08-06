package decisionrecord

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestPublishedUsePolicyVerifiesPinnedIntegrity(t *testing.T) {
	policy := parseUsePolicyFixture(t)
	if err := VerifyUsePolicy(policy, policy.PolicyHash); err != nil {
		t.Fatalf("VerifyUsePolicy: %v", err)
	}
}

func TestParseUsePolicyRejectsUnknownFieldsAndImpossibleWindows(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "unknown field", mutate: func(document map[string]any) {
			document["allow_replay"] = true
		}},
		{name: "created after activation", mutate: func(document map[string]any) {
			document["created_at"] = "2026-08-06T00:00:01Z"
		}},
		{name: "empty validity", mutate: func(document map[string]any) {
			document["valid_until"] = document["valid_from"]
		}},
		{name: "record delay exceeds evaluation age", mutate: func(document map[string]any) {
			document["max_record_delay_seconds"] = json.Number("301")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := decodeParseDocument(t, readUsePolicyFixture(t))
			test.mutate(document)
			if _, err := ParseUsePolicy(marshalParseDocument(t, document)); !errors.Is(err, ErrUsePolicy) {
				t.Fatalf("ParseUsePolicy error = %v, want ErrUsePolicy", err)
			}
		})
	}
}

func TestVerifyUsePolicyRequiresExactPin(t *testing.T) {
	policy := parseUsePolicyFixture(t)
	for _, test := range []struct {
		name     string
		policy   *UsePolicy
		expected string
	}{
		{name: "wrong caller pin", policy: policy, expected: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		{name: "malformed caller pin", policy: policy, expected: "not-a-hash"},
		{name: "mutated policy", policy: func() *UsePolicy {
			clone := cloneUsePolicy(t, policy)
			clone.MaxRecordAgeSeconds++
			return clone
		}(), expected: policy.PolicyHash},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := VerifyUsePolicy(test.policy, test.expected); !errors.Is(err, ErrUsePolicy) {
				t.Fatalf("VerifyUsePolicy error = %v, want ErrUsePolicy", err)
			}
		})
	}
}

func FuzzParseUsePolicyNeverPanics(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"schema_version":"crl-decision-use-policy/v1"}`))
	f.Add([]byte{'"', 0xff, '"'})
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = ParseUsePolicy(body)
	})
}

func parseUsePolicyFixture(t *testing.T) *UsePolicy {
	t.Helper()
	policy, err := ParseUsePolicy(readUsePolicyFixture(t))
	if err != nil {
		t.Fatalf("ParseUsePolicy: %v", err)
	}
	return policy
}

func readUsePolicyFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("../spec/testdata/decision-use-policy-v1/valid/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func cloneUsePolicy(t *testing.T, policy *UsePolicy) *UsePolicy {
	t.Helper()
	body, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	var clone UsePolicy
	if err := json.Unmarshal(body, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
}
