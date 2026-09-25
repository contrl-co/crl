package decisionrecord_test

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/contrl-co/crl/decisionrecord"
)

func constructionInput(test *testing.T) decisionrecord.Input {
	test.Helper()
	var input decisionrecord.Input
	decode(test, fixture(test, "valid/authorized.json"), &input)
	return input
}

func TestConstructionReproducesPublishedUnsignedRecord(test *testing.T) {
	input := constructionInput(test)
	input.Evaluation.Provenance[0], input.Evaluation.Provenance[1] = input.Evaluation.Provenance[1], input.Evaluation.Provenance[0]
	record, err := decisionrecord.NewUnsigned(input)
	if err != nil {
		test.Fatal(err)
	}
	if input.Evaluation.Provenance[0].Fact != "provider.registry" {
		test.Fatal("constructor reordered caller-owned provenance")
	}
	var expected map[string]any
	decode(test, fixture(test, "valid/authorized.json"), &expected)
	expected["signatures"] = []any{}
	body, err := json.Marshal(expected)
	if err != nil {
		test.Fatal(err)
	}
	vector, err := decisionrecord.Parse(body)
	if err != nil || !bytes.Equal(record.Bytes(), vector.Bytes()) {
		test.Fatalf("construction differs from the accepted vector: %v", err)
	}
	clear(input.Evaluation.Facts)
	if err := record.VerifyIntegrity(); err != nil {
		test.Fatal("caller changed the constructed record")
	}
	if err := record.VerifySignatureMath(nil); !errors.Is(err, decisionrecord.ErrSignature) {
		test.Fatal("construction manufactured signatures")
	}
}

func TestConstructionRefusesMissingOrInconsistentClaims(test *testing.T) {
	for name, mutate := range map[string]func(*decisionrecord.Input){
		"record identity": func(input *decisionrecord.Input) { input.RecordID = "" },
		"creation time":   func(input *decisionrecord.Input) { input.CreatedAt = "" },
		"context":         func(input *decisionrecord.Input) { input.Context = decisionrecord.Context{} },
		"source":          func(input *decisionrecord.Input) { input.Rule.Source = "" },
		"bundle":          func(input *decisionrecord.Input) { input.Rule.CanonicalBundle = "{} {}" },
		"facts":           func(input *decisionrecord.Input) { input.Evaluation.Facts = nil },
		"duplicate facts": func(input *decisionrecord.Input) {
			input.Evaluation.Facts = json.RawMessage(`{"approved":true,"approved":false}`)
		},
		"provenance":               func(input *decisionrecord.Input) { input.Evaluation.Provenance = nil },
		"supplier":                 func(input *decisionrecord.Input) { input.Evaluation.Provenance[0].Supplier = "" },
		"source digest":            func(input *decisionrecord.Input) { input.Evaluation.Provenance[0].SourceDigest = "" },
		"observed time":            func(input *decisionrecord.Input) { input.Evaluation.Provenance[0].ObservedAt = "2026-08-06T13:00:00Z" },
		"trace":                    func(input *decisionrecord.Input) { input.Evaluation.Trace = nil },
		"evaluator":                func(input *decisionrecord.Input) { input.Evaluation.Evaluator = decisionrecord.EvaluatorIdentity{} },
		"invalid unicode source":   func(input *decisionrecord.Input) { input.Rule.Source = string([]byte{0xff}) },
		"invalid unicode identity": func(input *decisionrecord.Input) { input.Context.Subject = string([]byte{0xff}) },
		"invalid unicode facts":    func(input *decisionrecord.Input) { input.Evaluation.Facts = json.RawMessage{'"', 0xff, '"'} },
		"policy":                   func(input *decisionrecord.Input) { input.TrustPolicy = decisionrecord.PolicyReference{} },
	} {
		test.Run(name, func(test *testing.T) {
			input := constructionInput(test)
			mutate(&input)
			if _, err := decisionrecord.NewUnsigned(input); !errors.Is(err, decisionrecord.ErrStructure) {
				test.Fatalf("missing/inconsistent input accepted: %v", err)
			}
		})
	}
	input := constructionInput(test)
	input.Evaluation.Facts = bytes.Replace(input.Evaluation.Facts, []byte("true"), []byte("1.50"), 1)
	record, err := decisionrecord.NewUnsigned(input)
	if err != nil || !bytes.Contains(record.Bytes(), []byte(`"approved":1.50`)) {
		test.Fatalf("fact number token changed: %v", err)
	}
}

type fixtureSigner struct {
	key     ed25519.PrivateKey
	calls   int
	fail    bool
	invalid bool
	mutate  bool
}

func (signer *fixtureSigner) Public() crypto.PublicKey { return signer.key.Public() }
func (signer *fixtureSigner) Sign(random io.Reader, message []byte, options crypto.SignerOpts) ([]byte, error) {
	signer.calls++
	if signer.fail {
		return nil, errors.New("private-signer-diagnostic")
	}
	if signer.invalid {
		return make([]byte, ed25519.SignatureSize), nil
	}
	if signer.mutate {
		message[0] ^= 1
	}
	return signer.key.Sign(random, message, options)
}

func TestExplicitSigningPreservesRecordAndBindsEverySigner(test *testing.T) {
	record, err := decisionrecord.NewUnsigned(constructionInput(test))
	if err != nil {
		test.Fatal(err)
	}
	original := record.Bytes()
	// Deterministic fixture keys exercise mathematics, not independent parties.
	issuer := &fixtureSigner{key: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize))}
	reviewer := &fixtureSigner{key: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, ed25519.SeedSize))}
	reviewerIdentity := decisionrecord.SigningIdentity{KeyID: "fixture-reviewer", Role: "reviewer", SignedAt: "2026-08-06T15:00:00Z"}
	reviewed, err := record.Sign(reviewerIdentity, reviewer)
	if err != nil {
		test.Fatal(err)
	}
	signed, err := reviewed.Sign(decisionrecord.SigningIdentity{KeyID: "fixture-issuer", Role: "issuer", SignedAt: reviewerIdentity.SignedAt}, issuer)
	if err != nil {
		test.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{"fixture-issuer": issuer.Public().(ed25519.PublicKey), "fixture-reviewer": reviewer.Public().(ed25519.PublicKey)}
	if err := signed.VerifySignatureMath(keys); err != nil {
		test.Fatal(err)
	}
	if !bytes.Equal(record.Bytes(), original) || issuer.calls != 1 || reviewer.calls != 1 {
		test.Fatal("signing changed the input record or called a signer more than once")
	}
	if _, err := signed.Sign(reviewerIdentity, reviewer); err == nil || reviewer.calls != 1 {
		test.Fatal("duplicate identity reached the signer")
	}
	for _, identity := range []decisionrecord.SigningIdentity{{}, {KeyID: "fixture", Role: "issuer", SignedAt: "yesterday"}} {
		if _, err := record.Sign(identity, issuer); err == nil || issuer.calls != 1 {
			test.Fatal("invalid signing identity reached the signer")
		}
	}
	if _, err := record.Sign(reviewerIdentity, nil); !errors.Is(err, decisionrecord.ErrSignature) {
		test.Fatal("missing signer accepted")
	}
	if _, err := record.Sign(reviewerIdentity, ed25519.PrivateKey{}); !errors.Is(err, decisionrecord.ErrSignature) {
		test.Fatal("invalid private key accepted")
	}
	mutatingSigner := &fixtureSigner{key: issuer.key, mutate: true}
	if _, err := record.Sign(reviewerIdentity, mutatingSigner); !errors.Is(err, decisionrecord.ErrSignature) || !bytes.Equal(record.Bytes(), original) {
		test.Fatal("signer changed the checked payload or source record")
	}
	issuer.invalid = true
	if _, err := record.Sign(reviewerIdentity, issuer); !errors.Is(err, decisionrecord.ErrSignature) {
		test.Fatal("invalid signer output accepted")
	}
	issuer.fail = true
	if _, err := record.Sign(reviewerIdentity, issuer); !errors.Is(err, decisionrecord.ErrSignature) || strings.Contains(err.Error(), "private-signer-diagnostic") {
		test.Fatal("signer failure accepted or exposed a private diagnostic")
	}
}
