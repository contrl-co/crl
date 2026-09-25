package decisionrecord

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"

	crlcrypto "github.com/contrl-co/crl/internal/crypto"
)

type SigningIdentity struct {
	KeyID    string
	Role     string
	SignedAt string
}

// Sign returns a new record with one role-bound signature from an explicitly
// supplied Ed25519 signer. The signer may use managed key storage. Sign does not
// approve the claims, validate other signatures or establish party independence.
// Missing/duplicate identities fail before the signer is invoked.
func (record *Record) Sign(identity SigningIdentity, signer crypto.Signer) (*Record, error) {
	if err := record.VerifyIntegrity(); err != nil {
		return nil, err
	}
	value, err := strictDocument(record.Bytes())
	if err != nil {
		return nil, err
	}
	document := value.(map[string]any)
	envelope := map[string]any{
		"algorithm": "ed25519", "key_id": identity.KeyID, "role": identity.Role, "signed_at": identity.SignedAt,
		"signature": base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
	}
	signatures := append(document["signatures"].([]any), envelope)
	sort.Slice(signatures, func(left, right int) bool {
		first, second := signatures[left].(map[string]any), signatures[right].(map[string]any)
		return first["role"].(string)+"\x00"+first["key_id"].(string) < second["role"].(string)+"\x00"+second["key_id"].(string)
	})
	document["signatures"] = signatures
	wire, err := crlcrypto.CanonicalJSON(document)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(wire); err != nil {
		return nil, err
	}
	if signer == nil {
		return nil, fmt.Errorf("%w: missing signer", ErrSignature)
	}
	if key, isPrivateKey := signer.(ed25519.PrivateKey); isPrivateKey && len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: invalid private key", ErrSignature)
	}
	publicKey, valid := signer.Public().(ed25519.PublicKey)
	if !valid || len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: signer is not Ed25519", ErrSignature)
	}
	publicKey = bytes.Clone(publicKey)
	payload := map[string]any{"record_hash": document["record_hash"]}
	for _, name := range []string{"algorithm", "key_id", "role", "signed_at"} {
		payload[name] = envelope[name]
	}
	canonical, err := crlcrypto.CanonicalJSON(payload)
	if err != nil {
		return nil, err
	}
	message := append([]byte("crl-decision-signature/v1\x00"), canonical...)
	signature, err := signer.Sign(rand.Reader, bytes.Clone(message), crypto.Hash(0))
	if err != nil || !ed25519.Verify(publicKey, message, signature) {
		return nil, fmt.Errorf("%w: signer failed or returned an invalid signature", ErrSignature)
	}
	envelope["signature"] = base64.StdEncoding.EncodeToString(signature)
	wire, err = crlcrypto.CanonicalJSON(document)
	if err != nil {
		return nil, err
	}
	return Parse(wire)
}
