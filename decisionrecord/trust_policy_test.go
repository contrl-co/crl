package decisionrecord

import (
	"errors"
	"os"
	"testing"
)

func TestParseTrustPolicy(t *testing.T) {
	policy, err := ParseTrustPolicy(readTrustPolicyFixture(t))
	if err != nil {
		t.Fatalf("ParseTrustPolicy: %v", err)
	}
	if policy.SchemaVersion != "crl-decision-trust-policy/v1" || policy.Domain != "contrl.co/example" {
		t.Fatalf("unexpected policy identity: %+v", policy)
	}
	if len(policy.Roles) != 2 || len(policy.Keys) != 2 {
		t.Fatalf("unexpected policy contents: %+v", policy)
	}
}

func TestTrustPolicySemanticBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*TrustPolicy)
	}{
		{name: "created at activation", mutate: func(policy *TrustPolicy) { policy.CreatedAt = policy.ValidFrom }},
		{name: "revoked at key activation", mutate: func(policy *TrustPolicy) {
			policy.Keys[0].RevokedAt = policy.Keys[0].NotBefore
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := parseTrustPolicyFixture(t)
			test.mutate(policy)
			if err := validateTrustPolicy(policy); err != nil {
				t.Fatalf("validateTrustPolicy: %v", err)
			}
		})
	}
}

func TestParseTrustPolicyRejectsInvalidWireJSON(t *testing.T) {
	tests := map[string][]byte{
		"invalid UTF-8":     {'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'},
		"duplicate key":     []byte(`{"schema_version":"crl-decision-trust-policy/v1","schema_version":"crl-decision-trust-policy/v1"}`),
		"trailing document": append(readTrustPolicyFixture(t), []byte(` {}`)...),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTrustPolicy(body); !errors.Is(err, ErrTrustPolicy) {
				t.Fatalf("ParseTrustPolicy error = %v, want ErrTrustPolicy", err)
			}
		})
	}
}

func TestTrustPolicySemanticValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TrustPolicy)
	}{
		{name: "nil", mutate: func(*TrustPolicy) {}},
		{name: "invalid UTF-8", mutate: func(policy *TrustPolicy) { policy.Domain = string([]byte{0xff}) }},
		{name: "roles out of order", mutate: func(policy *TrustPolicy) {
			policy.Roles[0], policy.Roles[1] = policy.Roles[1], policy.Roles[0]
		}},
		{name: "duplicate role", mutate: func(policy *TrustPolicy) { policy.Roles[1].Role = policy.Roles[0].Role }},
		{name: "keys out of order", mutate: func(policy *TrustPolicy) {
			policy.Keys[0], policy.Keys[1] = policy.Keys[1], policy.Keys[0]
		}},
		{name: "duplicate key id", mutate: func(policy *TrustPolicy) { policy.Keys[1].KeyID = policy.Keys[0].KeyID }},
		{name: "duplicate public key", mutate: func(policy *TrustPolicy) { policy.Keys[1].PublicKey = policy.Keys[0].PublicKey }},
		{name: "unknown key role", mutate: func(policy *TrustPolicy) { policy.Keys[0].Role = "auditor" }},
		{name: "unattainable threshold", mutate: func(policy *TrustPolicy) { policy.Roles[0].Threshold = 2 }},
		{name: "created after activation", mutate: func(policy *TrustPolicy) { policy.CreatedAt = "2026-01-02T00:00:00Z" }},
		{name: "empty policy interval", mutate: func(policy *TrustPolicy) { policy.ValidUntil = policy.ValidFrom }},
		{name: "empty key interval", mutate: func(policy *TrustPolicy) { policy.Keys[0].NotAfter = policy.Keys[0].NotBefore }},
		{name: "revocation before key validity", mutate: func(policy *TrustPolicy) {
			policy.Keys[0].RevokedAt = "2025-12-31T23:59:59Z"
		}},
		{name: "revocation at key expiry", mutate: func(policy *TrustPolicy) {
			policy.Keys[0].RevokedAt = policy.Keys[0].NotAfter
		}},
		{name: "allowed extensions out of order", mutate: func(policy *TrustPolicy) {
			policy.AllowedExtensions[0], policy.AllowedExtensions[1] = policy.AllowedExtensions[1], policy.AllowedExtensions[0]
		}},
		{name: "required extension is not allowed", mutate: func(policy *TrustPolicy) {
			policy.RequiredExtensions[0] = "contrl.co/unrecognized"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var policy *TrustPolicy
			if test.name != "nil" {
				policy = parseTrustPolicyFixture(t)
				test.mutate(policy)
			}
			if err := validateTrustPolicy(policy); !errors.Is(err, ErrTrustPolicy) {
				t.Fatalf("validateTrustPolicy error = %v, want ErrTrustPolicy", err)
			}
		})
	}
}

func readTrustPolicyFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("../spec/testdata/decision-trust-policy-v1/valid/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func parseTrustPolicyFixture(t *testing.T) *TrustPolicy {
	t.Helper()
	policy, err := ParseTrustPolicy(readTrustPolicyFixture(t))
	if err != nil {
		t.Fatalf("ParseTrustPolicy fixture: %v", err)
	}
	return policy
}
