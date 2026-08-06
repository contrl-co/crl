package decisionrecord

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	crlcrypto "gitlab.com/contrl-group/crl/internal/crypto"
)

func TestVerifyTrust(t *testing.T) {
	record, policy, _ := trustedTestRecord(t)
	evidence, err := VerifyTrust(record, policy, policy.PolicyHash, verifierTime().Add(time.Hour))
	if err != nil {
		t.Fatalf("VerifyTrust: %v", err)
	}
	if evidence.PolicyID != policy.PolicyID || evidence.PolicyHash != policy.PolicyHash || evidence.Domain != record.Context.Domain {
		t.Fatalf("unexpected trust evidence: %+v", evidence)
	}
	if len(evidence.Roles) != 2 || evidence.Roles[0].Role != "issuer" || evidence.Roles[1].Role != "reviewer" {
		t.Fatalf("unexpected role evidence: %+v", evidence.Roles)
	}
}

func TestVerifyTrustPolicyRequiresPinnedIntegrity(t *testing.T) {
	_, policy, _ := trustedTestRecord(t)
	if err := VerifyTrustPolicy(policy, policy.PolicyHash); err != nil {
		t.Fatalf("VerifyTrustPolicy: %v", err)
	}

	tests := []struct {
		name     string
		policy   *TrustPolicy
		expected string
	}{
		{name: "nil policy", policy: nil, expected: policy.PolicyHash},
		{name: "missing pin", policy: policy, expected: ""},
		{name: "noncanonical pin", policy: policy, expected: strings.ToUpper(policy.PolicyHash)},
		{name: "wrong pin", policy: policy, expected: strings.Repeat("0", 64)},
		{name: "mutated policy", policy: func() *TrustPolicy {
			mutated := cloneTrustPolicy(t, policy)
			mutated.Domain = "contrl.co/other"
			return mutated
		}(), expected: policy.PolicyHash},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := VerifyTrustPolicy(test.policy, test.expected); !errors.Is(err, ErrTrustPolicy) {
				t.Fatalf("VerifyTrustPolicy error = %v, want ErrTrustPolicy", err)
			}
		})
	}
}

func TestPublishedTrustPolicyFixtureVerifiesPinnedIntegrity(t *testing.T) {
	policy := parseTrustPolicyFixture(t)
	if err := VerifyTrustPolicy(policy, policy.PolicyHash); err != nil {
		t.Fatalf("VerifyTrustPolicy fixture: %v", err)
	}
}

func TestSignaturePayloadMatchesContractGolden(t *testing.T) {
	payload, err := signaturePayload(strings.Repeat("0", 64), Signature{
		Algorithm: "ed25519",
		KeyID:     "issuer-2026-01",
		Role:      "issuer",
		SignedAt:  "2026-08-06T15:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	if got, want := hex.EncodeToString(digest[:]), "5f9309102a78b759dc5605d839952df8cf410920849a28f81af189d1ef3608a2"; got != want {
		t.Fatalf("signature payload digest = %s, want %s", got, want)
	}
}

func TestVerifyTrustRejectsUntrustedConditions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *Record, *TrustPolicy, map[string]ed25519.PrivateKey, *time.Time)
		want   error
	}{
		{name: "record integrity", want: ErrIntegrity, mutate: func(_ *testing.T, record *Record, _ *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			record.RecordHash = strings.Repeat("0", 64)
		}},
		{name: "wrong domain", want: ErrTrust, mutate: func(t *testing.T, _ *Record, policy *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			policy.Domain = "contrl.co/other"
			rehashTestPolicy(t, policy)
		}},
		{name: "policy not active", want: ErrTrust, mutate: func(t *testing.T, _ *Record, policy *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			policy.ValidFrom = verifierTime().Add(2 * time.Hour).Format(time.RFC3339Nano)
			policy.ValidUntil = verifierTime().Add(3 * time.Hour).Format(time.RFC3339Nano)
			rehashTestPolicy(t, policy)
		}},
		{name: "policy expired", want: ErrTrust, mutate: func(t *testing.T, _ *Record, policy *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			policy.ValidUntil = verifierTime().Format(time.RFC3339Nano)
			rehashTestPolicy(t, policy)
		}},
		{name: "unknown key", want: ErrTrust, mutate: func(t *testing.T, record *Record, _ *TrustPolicy, keys map[string]ed25519.PrivateKey, _ *time.Time) {
			record.Signatures[0].KeyID = "unknown-key"
			resignTestSignature(t, record, 0, keys["issuer-2026-01"])
		}},
		{name: "key used across roles", want: ErrTrust, mutate: func(t *testing.T, record *Record, _ *TrustPolicy, keys map[string]ed25519.PrivateKey, _ *time.Time) {
			record.Signatures[0].Role = "reviewer"
			resignTestSignature(t, record, 0, keys["issuer-2026-01"])
		}},
		{name: "key not active", want: ErrTrust, mutate: func(t *testing.T, _ *Record, policy *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			policy.Keys[0].NotBefore = verifierTime().Add(time.Minute).Format(time.RFC3339Nano)
			rehashTestPolicy(t, policy)
		}},
		{name: "key expired", want: ErrTrust, mutate: func(t *testing.T, _ *Record, policy *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			policy.Keys[0].NotAfter = verifierTime().Format(time.RFC3339Nano)
			rehashTestPolicy(t, policy)
		}},
		{name: "key revoked", want: ErrTrust, mutate: func(t *testing.T, _ *Record, policy *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			policy.Keys[0].RevokedAt = verifierTime().Format(time.RFC3339Nano)
			rehashTestPolicy(t, policy)
		}},
		{name: "signature predates record", want: ErrTrust, mutate: func(t *testing.T, record *Record, _ *TrustPolicy, keys map[string]ed25519.PrivateKey, _ *time.Time) {
			record.Signatures[0].SignedAt = verifierTime().Add(-time.Second).Format(time.RFC3339Nano)
			resignTestSignature(t, record, 0, keys["issuer-2026-01"])
		}},
		{name: "signature exceeds role delay", want: ErrTrust, mutate: func(t *testing.T, record *Record, policy *TrustPolicy, keys map[string]ed25519.PrivateKey, _ *time.Time) {
			policy.Roles[0].MaxSignatureDelaySeconds = 0
			rehashTestPolicy(t, policy)
			record.Signatures[0].SignedAt = verifierTime().Add(time.Second).Format(time.RFC3339Nano)
			resignTestSignature(t, record, 0, keys["issuer-2026-01"])
		}},
		{name: "signature is too far in future", want: ErrTrust, mutate: func(t *testing.T, record *Record, policy *TrustPolicy, keys map[string]ed25519.PrivateKey, verifiedAt *time.Time) {
			policy.Roles[0].MaxSignatureDelaySeconds = 600
			rehashTestPolicy(t, policy)
			record.Signatures[0].SignedAt = verifierTime().Add(301 * time.Second).Format(time.RFC3339Nano)
			resignTestSignature(t, record, 0, keys["issuer-2026-01"])
			*verifiedAt = verifierTime()
		}},
		{name: "missing role threshold", want: ErrTrust, mutate: func(_ *testing.T, record *Record, _ *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			record.Signatures = record.Signatures[:1]
		}},
		{name: "unsigned", want: ErrTrust, mutate: func(_ *testing.T, record *Record, _ *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			record.Signatures = []Signature{}
		}},
		{name: "unknown extension", want: ErrTrust, mutate: func(t *testing.T, record *Record, _ *TrustPolicy, keys map[string]ed25519.PrivateKey, _ *time.Time) {
			record.Extensions = map[string]any{"contrl.co/unknown": true}
			rehashRecord(t, record)
			resignAllTestSignatures(t, record, keys)
		}},
		{name: "missing required extension", want: ErrTrust, mutate: func(t *testing.T, _ *Record, policy *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			policy.AllowedExtensions = []string{"contrl.co/workflow"}
			policy.RequiredExtensions = []string{"contrl.co/workflow"}
			rehashTestPolicy(t, policy)
		}},
		{name: "invalid signature", want: ErrSignature, mutate: func(_ *testing.T, record *Record, _ *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *time.Time) {
			signature, _ := base64.StdEncoding.DecodeString(record.Signatures[0].Signature)
			signature[0] ^= 0xff
			record.Signatures[0].Signature = base64.StdEncoding.EncodeToString(signature)
		}},
		{name: "verification time missing", want: ErrTrust, mutate: func(_ *testing.T, _ *Record, _ *TrustPolicy, _ map[string]ed25519.PrivateKey, verifiedAt *time.Time) {
			*verifiedAt = time.Time{}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, policy, keys := trustedTestRecord(t)
			verifiedAt := verifierTime().Add(time.Hour)
			test.mutate(t, record, policy, keys, &verifiedAt)
			_, err := VerifyTrust(record, policy, policy.PolicyHash, verifiedAt)
			if !errors.Is(err, test.want) {
				t.Fatalf("VerifyTrust error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestVerifyTrustRejectsSignatureBeforeLaterRevocation(t *testing.T) {
	record, policy, _ := trustedTestRecord(t)
	policy.Keys[0].RevokedAt = verifierTime().Add(time.Second).Format(time.RFC3339Nano)
	rehashTestPolicy(t, policy)
	if _, err := VerifyTrust(record, policy, policy.PolicyHash, verifierTime().Add(time.Hour)); !errors.Is(err, ErrTrust) {
		t.Fatalf("VerifyTrust error = %v, want ErrTrust", err)
	}
}

func TestVerifyTrustRejectsKeyRevokedAfterExpiry(t *testing.T) {
	record, policy, _ := trustedTestRecord(t)
	policy.CreatedAt = "2028-01-01T00:00:00Z"
	policy.ValidFrom = "2028-01-02T00:00:00Z"
	policy.ValidUntil = "2029-01-01T00:00:00Z"
	policy.Keys[0].RevokedAt = "2028-01-01T00:00:00Z"
	rehashTestPolicy(t, policy)
	verifiedAt := time.Date(2028, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, err := VerifyTrust(record, policy, policy.PolicyHash, verifiedAt); !errors.Is(err, ErrTrust) {
		t.Fatalf("VerifyTrust error = %v, want ErrTrust", err)
	}
}

func TestVerifyTrustAcceptsHistoricalSignatureFromExpiredUnrevokedKey(t *testing.T) {
	record, policy, _ := trustedTestRecord(t)
	policy.ValidUntil = "2029-01-01T00:00:00Z"
	rehashTestPolicy(t, policy)
	if _, err := VerifyTrust(record, policy, policy.PolicyHash, time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("VerifyTrust historical signature: %v", err)
	}
}

func TestVerifyTrustAcceptsExactDelayAndClockSkewBoundary(t *testing.T) {
	record, policy, keys := trustedTestRecord(t)
	record.Signatures[0].SignedAt = verifierTime().Add(300 * time.Second).Format(time.RFC3339Nano)
	resignTestSignature(t, record, 0, keys["issuer-2026-01"])
	if _, err := VerifyTrust(record, policy, policy.PolicyHash, verifierTime()); err != nil {
		t.Fatalf("VerifyTrust boundary signature: %v", err)
	}
}

func trustedTestRecord(t *testing.T) (*Record, *TrustPolicy, map[string]ed25519.PrivateKey) {
	t.Helper()
	record := parseTestRecord(t)
	issuer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize))
	reviewer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, ed25519.SeedSize))
	keys := map[string]ed25519.PrivateKey{
		"issuer-2026-01":   issuer,
		"reviewer-2026-01": reviewer,
	}
	policy := &TrustPolicy{
		SchemaVersion:       "crl-decision-trust-policy/v1",
		PolicyID:            "01989f7e-7b80-7000-8000-000000000010",
		Version:             1,
		CreatedAt:           "2025-12-15T00:00:00Z",
		Domain:              record.Context.Domain,
		ValidFrom:           "2026-01-01T00:00:00Z",
		ValidUntil:          "2027-01-01T00:00:00Z",
		MaxClockSkewSeconds: 300,
		Roles: []RolePolicy{
			{Role: "issuer", Threshold: 1, MaxSignatureDelaySeconds: 300},
			{Role: "reviewer", Threshold: 1, MaxSignatureDelaySeconds: 86400},
		},
		Keys: []TrustedKey{
			{
				Algorithm: "ed25519", KeyID: "issuer-2026-01", Role: "issuer",
				PublicKey: base64.StdEncoding.EncodeToString(issuer.Public().(ed25519.PublicKey)),
				NotBefore: "2026-01-01T00:00:00Z", NotAfter: "2027-01-01T00:00:00Z",
			},
			{
				Algorithm: "ed25519", KeyID: "reviewer-2026-01", Role: "reviewer",
				PublicKey: base64.StdEncoding.EncodeToString(reviewer.Public().(ed25519.PublicKey)),
				NotBefore: "2026-01-01T00:00:00Z", NotAfter: "2027-01-01T00:00:00Z",
			},
		},
		AllowedExtensions:  []string{},
		RequiredExtensions: []string{},
	}
	rehashTestPolicy(t, policy)
	record.Signatures = []Signature{
		newTestSignature(t, record, "issuer", "issuer-2026-01", verifierTime(), issuer),
		newTestSignature(t, record, "reviewer", "reviewer-2026-01", verifierTime(), reviewer),
	}
	return record, policy, keys
}

func newTestSignature(t *testing.T, record *Record, role, keyID string, signedAt time.Time, privateKey ed25519.PrivateKey) Signature {
	t.Helper()
	signature := Signature{
		Algorithm: "ed25519",
		KeyID:     keyID,
		Role:      role,
		SignedAt:  signedAt.Format(time.RFC3339Nano),
	}
	payload := testSignaturePayload(t, record.RecordHash, signature)
	signature.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return signature
}

func resignTestSignature(t *testing.T, record *Record, index int, privateKey ed25519.PrivateKey) {
	t.Helper()
	payload := testSignaturePayload(t, record.RecordHash, record.Signatures[index])
	record.Signatures[index].Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	sort.Slice(record.Signatures, func(left, right int) bool {
		leftIdentity := record.Signatures[left].Role + "\x00" + record.Signatures[left].KeyID
		rightIdentity := record.Signatures[right].Role + "\x00" + record.Signatures[right].KeyID
		return leftIdentity < rightIdentity
	})
}

func testSignaturePayload(t *testing.T, recordHash string, signature Signature) []byte {
	t.Helper()
	envelope := map[string]any{
		"algorithm":   signature.Algorithm,
		"key_id":      signature.KeyID,
		"role":        signature.Role,
		"signed_at":   signature.SignedAt,
		"record_hash": recordHash,
	}
	canonical, err := crlcrypto.CanonicalJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("crl-decision-signature/v1\x00"), canonical...)
	return payload
}

func resignAllTestSignatures(t *testing.T, record *Record, keys map[string]ed25519.PrivateKey) {
	t.Helper()
	for index := range record.Signatures {
		resignTestSignature(t, record, index, keys[record.Signatures[index].KeyID])
	}
}

func rehashTestPolicy(t *testing.T, policy *TrustPolicy) {
	t.Helper()
	digest, err := domainDigest(trustPolicyDomain, policy.unsigned())
	if err != nil {
		t.Fatal(err)
	}
	policy.PolicyHash = digest
}

func cloneTrustPolicy(t *testing.T, policy *TrustPolicy) *TrustPolicy {
	t.Helper()
	body, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := ParseTrustPolicy(body)
	if err != nil {
		t.Fatal(err)
	}
	return cloned
}
