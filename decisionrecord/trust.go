package decisionrecord

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	crlcrypto "gitlab.com/contrl-group/crl/internal/crypto"
)

const (
	signatureDomain   = "crl-decision-signature/v1"
	trustPolicyDomain = "crl-decision-trust-policy/v1"
)

var (
	// ErrSignature marks a signature that is mathematically invalid for the
	// record hash and authorized public key.
	ErrSignature = errors.New("decision record: signature verification failed")
	// ErrTrust marks a valid policy that does not authorize the record's
	// domain, extensions, signature timing, keys, roles, or thresholds.
	ErrTrust = errors.New("decision record: trust verification failed")
)

// TrustEvidence identifies the pinned policy and role thresholds that
// successfully authorized a record's signatures.
type TrustEvidence struct {
	PolicyID      string              `json:"policy_id"`
	PolicyVersion int64               `json:"policy_version"`
	PolicyHash    string              `json:"policy_hash"`
	Domain        string              `json:"domain"`
	VerifiedAt    string              `json:"verified_at"`
	Roles         []RoleTrustEvidence `json:"roles"`
}

// RoleTrustEvidence lists the distinct keys counted toward one role.
type RoleTrustEvidence struct {
	Role           string   `json:"role"`
	Threshold      int      `json:"threshold"`
	VerifiedKeyIDs []string `json:"verified_key_ids"`
}

// VerifyTrustPolicy revalidates a policy, recomputes its domain-separated
// hash, and requires an exact caller-provided hash pin.
func VerifyTrustPolicy(policy *TrustPolicy, expectedPolicyHash string) error {
	if err := validateTrustPolicy(policy); err != nil {
		return err
	}
	if !canonicalDigest(expectedPolicyHash) {
		return policyError("expected policy hash is not canonical SHA-256")
	}
	actual, err := domainDigest(trustPolicyDomain, policy.unsigned())
	if err != nil {
		return policyError("canonical policy: %v", err)
	}
	if policy.PolicyHash != actual {
		return policyError("policy_hash mismatch: got %s, want %s", policy.PolicyHash, actual)
	}
	if expectedPolicyHash != policy.PolicyHash {
		return policyError("policy_hash does not match caller pin")
	}
	return nil
}

// VerifyTrust verifies record integrity, a caller-pinned policy that is active
// at the caller's trusted verification time, every supplied Ed25519 signature,
// and every role threshold. It does not verify the recorded CRL decision or
// apply replay/application context policy.
func VerifyTrust(record *Record, policy *TrustPolicy, expectedPolicyHash string, verifiedAt time.Time) (TrustEvidence, error) {
	if err := VerifyIntegrity(record); err != nil {
		return TrustEvidence{}, err
	}
	if err := VerifyTrustPolicy(policy, expectedPolicyHash); err != nil {
		return TrustEvidence{}, err
	}
	if verifiedAt.IsZero() {
		return TrustEvidence{}, trustError("verification time is required")
	}

	recordCreatedAt, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return TrustEvidence{}, trustError("record created_at: %v", err)
	}
	validFrom, err := time.Parse(time.RFC3339Nano, policy.ValidFrom)
	if err != nil {
		return TrustEvidence{}, policyError("valid_from: %v", err)
	}
	validUntil, err := time.Parse(time.RFC3339Nano, policy.ValidUntil)
	if err != nil {
		return TrustEvidence{}, policyError("valid_until: %v", err)
	}
	if policy.Domain != record.Context.Domain {
		return TrustEvidence{}, trustError("record domain %q does not match policy domain %q", record.Context.Domain, policy.Domain)
	}
	if verifiedAt.Before(validFrom) || !verifiedAt.Before(validUntil) {
		return TrustEvidence{}, trustError("policy is not active at verification time")
	}
	if err := verifyTrustedExtensions(record.Extensions, policy); err != nil {
		return TrustEvidence{}, err
	}

	roles := make(map[string]RolePolicy, len(policy.Roles))
	for _, role := range policy.Roles {
		roles[role.Role] = role
	}
	keys := make(map[string]TrustedKey, len(policy.Keys))
	for _, key := range policy.Keys {
		keys[key.KeyID] = key
	}
	verifiedKeys := make(map[string][]string, len(roles))
	for _, signature := range record.Signatures {
		role, exists := roles[signature.Role]
		if !exists {
			return TrustEvidence{}, trustError("signature role %q is not authorized", signature.Role)
		}
		key, exists := keys[signature.KeyID]
		if !exists {
			return TrustEvidence{}, trustError("signature key %q is not authorized", signature.KeyID)
		}
		if key.Role != signature.Role {
			return TrustEvidence{}, trustError("key %q is authorized for role %q, not %q", key.KeyID, key.Role, signature.Role)
		}
		if key.Algorithm != signature.Algorithm {
			return TrustEvidence{}, trustError("key %q algorithm does not match signature", key.KeyID)
		}
		if err := verifySignatureTime(signature, key, role, recordCreatedAt, verifiedAt, policy.MaxClockSkewSeconds); err != nil {
			return TrustEvidence{}, err
		}
		if err := verifyEd25519Signature(record.RecordHash, signature, key); err != nil {
			return TrustEvidence{}, err
		}
		verifiedKeys[role.Role] = append(verifiedKeys[role.Role], key.KeyID)
	}

	evidence := TrustEvidence{
		PolicyID:      policy.PolicyID,
		PolicyVersion: policy.Version,
		PolicyHash:    policy.PolicyHash,
		Domain:        policy.Domain,
		VerifiedAt:    verifiedAt.UTC().Format(time.RFC3339Nano),
		Roles:         make([]RoleTrustEvidence, 0, len(policy.Roles)),
	}
	for _, role := range policy.Roles {
		keyIDs := verifiedKeys[role.Role]
		if len(keyIDs) < role.Threshold {
			return TrustEvidence{}, trustError("role %q has %d valid signatures, requires %d", role.Role, len(keyIDs), role.Threshold)
		}
		evidence.Roles = append(evidence.Roles, RoleTrustEvidence{
			Role:           role.Role,
			Threshold:      role.Threshold,
			VerifiedKeyIDs: append([]string(nil), keyIDs...),
		})
	}
	return evidence, nil
}

func verifyTrustedExtensions(extensions map[string]any, policy *TrustPolicy) error {
	allowed := make(map[string]struct{}, len(policy.AllowedExtensions))
	for _, extension := range policy.AllowedExtensions {
		allowed[extension] = struct{}{}
	}
	names := make([]string, 0, len(extensions))
	for extension := range extensions {
		names = append(names, extension)
	}
	sort.Strings(names)
	for _, extension := range names {
		if _, exists := allowed[extension]; !exists {
			return trustError("extension %q is not allowed", extension)
		}
	}
	for _, extension := range policy.RequiredExtensions {
		if _, exists := extensions[extension]; !exists {
			return trustError("required extension %q is missing", extension)
		}
	}
	return nil
}

func verifySignatureTime(signature Signature, key TrustedKey, role RolePolicy, recordCreatedAt, verifiedAt time.Time, maxClockSkewSeconds int64) error {
	signedAt, err := time.Parse(time.RFC3339Nano, signature.SignedAt)
	if err != nil {
		return trustError("signature (%q, %q) signed_at: %v", signature.Role, signature.KeyID, err)
	}
	if signedAt.Before(recordCreatedAt) {
		return trustError("signature (%q, %q) predates the record", signature.Role, signature.KeyID)
	}
	latestRoleTime := recordCreatedAt.Add(time.Duration(role.MaxSignatureDelaySeconds) * time.Second)
	if signedAt.After(latestRoleTime) {
		return trustError("signature (%q, %q) exceeds the role signing window", signature.Role, signature.KeyID)
	}
	latestClockTime := verifiedAt.Add(time.Duration(maxClockSkewSeconds) * time.Second)
	if signedAt.After(latestClockTime) {
		return trustError("signature (%q, %q) is too far in the future", signature.Role, signature.KeyID)
	}
	notBefore, err := time.Parse(time.RFC3339Nano, key.NotBefore)
	if err != nil {
		return policyError("key %q not_before: %v", key.KeyID, err)
	}
	notAfter, err := time.Parse(time.RFC3339Nano, key.NotAfter)
	if err != nil {
		return policyError("key %q not_after: %v", key.KeyID, err)
	}
	if signedAt.Before(notBefore) || !signedAt.Before(notAfter) {
		return trustError("key %q is not active at signature time", key.KeyID)
	}
	if key.RevokedAt != "" {
		return trustError("key %q is revoked", key.KeyID)
	}
	return nil
}

func verifyEd25519Signature(recordHash string, signature Signature, key TrustedKey) error {
	publicKey, err := base64.StdEncoding.DecodeString(key.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return policyError("key %q public_key is invalid", key.KeyID)
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(signature.Signature)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize {
		return signatureError("signature (%q, %q) encoding is invalid", signature.Role, signature.KeyID)
	}
	payload, err := signaturePayload(recordHash, signature)
	if err != nil {
		return signatureError("signature (%q, %q) payload: %v", signature.Role, signature.KeyID, err)
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signatureBytes) {
		return signatureError("signature (%q, %q) is invalid", signature.Role, signature.KeyID)
	}
	return nil
}

func signaturePayload(recordHash string, signature Signature) ([]byte, error) {
	envelope := map[string]any{
		"algorithm":   signature.Algorithm,
		"key_id":      signature.KeyID,
		"role":        signature.Role,
		"signed_at":   signature.SignedAt,
		"record_hash": recordHash,
	}
	canonical, err := crlcrypto.CanonicalJSON(envelope)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 0, len(signatureDomain)+1+len(canonical))
	payload = append(payload, signatureDomain...)
	payload = append(payload, 0)
	payload = append(payload, canonical...)
	return payload, nil
}

func canonicalDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func signatureError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrSignature, fmt.Sprintf(format, args...))
}

func trustError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTrust, fmt.Sprintf(format, args...))
}
