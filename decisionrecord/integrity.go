package decisionrecord

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"

	crlcrypto "github.com/contrl-co/crl/internal/crypto"
)

var (
	ErrIntegrity = errors.New("decision record: integrity check failed")
	ErrSignature = errors.New("decision record: signature check failed")
)

// VerifyIntegrity recomputes every v1 content digest. It neither trusts the
// signatures nor verifies that the recorded decision was correctly evaluated.
func (record *Record) VerifyIntegrity() error {
	if record == nil || record.document == nil {
		return fmt.Errorf("%w: record was not parsed", ErrStructure)
	}
	rule := record.object("rule")
	for _, field := range []struct{ body, hash string }{{"source", "source_hash"}, {"canonical_bundle", "bundle_hash"}} {
		if crlcrypto.DigestBytes([]byte(rule[field.body].(string))) != rule[field.hash] {
			return fmt.Errorf("%w: %s", ErrIntegrity, field.hash)
		}
	}
	evaluation := record.object("evaluation")
	traceHash, err := digest("crl-decision-trace/v1", evaluation["trace"])
	if err != nil || traceHash != evaluation["trace_hash"] {
		return fmt.Errorf("%w: trace_hash", ErrIntegrity)
	}
	unsigned := make(map[string]any, len(record.document)-2)
	for name, value := range record.document {
		if name != "record_hash" && name != "signatures" {
			unsigned[name] = value
		}
	}
	recordHash, err := digest("crl-decision-record/v1", unsigned)
	if err != nil || recordHash != record.document["record_hash"] {
		return fmt.Errorf("%w: record_hash", ErrIntegrity)
	}
	return nil
}

// VerifySignatureMath checks every supplied signature after content integrity.
// Keys are caller-supplied public material, not an approved trust policy. Success
// says nothing about party independence, mandates, revocation or permission to act.
// In particular, two keys controlled by one party can pass this mathematics check.
func (record *Record) VerifySignatureMath(keys map[string]ed25519.PublicKey) error {
	if err := record.VerifyIntegrity(); err != nil {
		return err
	}
	signatures := record.document["signatures"].([]any)
	if len(signatures) == 0 {
		return fmt.Errorf("%w: unsigned record", ErrSignature)
	}
	for _, value := range signatures {
		envelope := value.(map[string]any)
		key := keys[envelope["key_id"].(string)]
		if len(key) != ed25519.PublicKeySize {
			return fmt.Errorf("%w: missing or invalid public key", ErrSignature)
		}
		payload := map[string]any{"record_hash": record.document["record_hash"]}
		for _, name := range []string{"algorithm", "key_id", "role", "signed_at"} {
			payload[name] = envelope[name]
		}
		canonical, err := crlcrypto.CanonicalJSON(payload)
		if err != nil {
			return fmt.Errorf("%w: signing payload: %v", ErrSignature, err)
		}
		signature, err := base64.StdEncoding.Strict().DecodeString(envelope["signature"].(string))
		if err != nil || !ed25519.Verify(key, append([]byte("crl-decision-signature/v1\x00"), canonical...), signature) {
			return fmt.Errorf("%w: invalid Ed25519 signature", ErrSignature)
		}
	}
	return nil
}

func digest(domain string, value any) (string, error) {
	canonical, err := crlcrypto.CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return crlcrypto.DigestBytes(append([]byte(domain+"\x00"), canonical...)), nil
}
