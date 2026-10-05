package decisionrecord_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/contrl-co/crl/decisionrecord"
)

func fixture(test *testing.T, name string) []byte {
	test.Helper()
	body, err := os.ReadFile("../spec/testdata/decision-record-v1/" + name)
	if err != nil {
		test.Fatal(err)
	}
	return body
}

func decode(test *testing.T, body []byte, destination any) {
	test.Helper()
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		test.Fatal(err)
	}
}

func fixtureKeys(test *testing.T) map[string]ed25519.PublicKey {
	test.Helper()
	var policy struct {
		Keys map[string]struct {
			PublicKey string `json:"public_key"`
		} `json:"keys"`
	}
	decode(test, fixture(test, "policy.json"), &policy)
	keys := map[string]ed25519.PublicKey{}
	for identifier, entry := range policy.Keys {
		publicKey, err := hex.DecodeString(entry.PublicKey)
		if err != nil {
			test.Fatal(err)
		}
		keys[identifier] = publicKey
	}
	return keys
}

func TestAcceptedContractAndOwnership(test *testing.T) {
	body := fixture(test, "valid/authorized.json")
	record, err := decisionrecord.Parse(body)
	if err != nil {
		test.Fatal(err)
	}
	clear(body)
	wire := record.Bytes()
	clear(record.Bytes())
	if !bytes.Equal(wire, record.Bytes()) {
		test.Fatal("caller changed owned record bytes")
	}
	if err := record.VerifyIntegrity(); err != nil {
		test.Fatal(err)
	}
	keys := fixtureKeys(test)
	if err := record.VerifySignatureMath(keys); err != nil {
		test.Fatal(err)
	}
	delete(keys, "reviewer-2026-01")
	if err := record.VerifySignatureMath(keys); !errors.Is(err, decisionrecord.ErrSignature) {
		test.Fatalf("missing signer: %v", err)
	}
	keys["reviewer-2026-01"] = keys["issuer-2026-01"]
	if err := record.VerifySignatureMath(keys); !errors.Is(err, decisionrecord.ErrSignature) {
		test.Fatalf("substituted signer: %v", err)
	}
}

func TestConformanceMutations(test *testing.T) {
	for _, filename := range []string{"invalid/cases.json", "invalid/contract.json"} {
		var mutations []struct {
			Name  string   `json:"name"`
			Layer string   `json:"layer"`
			Path  []string `json:"path"`
			Value any      `json:"value"`
		}
		decode(test, fixture(test, filename), &mutations)
		for _, mutation := range mutations {
			if mutation.Layer == "decision" || mutation.Layer == "trust" {
				continue // Only structural and integrity contract cases apply to this API.
			}
			test.Run(filename+"/"+mutation.Name, func(test *testing.T) {
				var document any
				decode(test, fixture(test, "valid/authorized.json"), &document)
				parent := document
				for _, segment := range mutation.Path[:len(mutation.Path)-1] {
					if array, ok := parent.([]any); ok {
						index, err := strconv.Atoi(segment)
						if err != nil {
							test.Fatal(err)
						}
						parent = array[index]
					} else {
						parent = parent.(map[string]any)[segment]
					}
				}
				last := mutation.Path[len(mutation.Path)-1]
				if array, ok := parent.([]any); ok {
					index, err := strconv.Atoi(last)
					if err != nil {
						test.Fatal(err)
					}
					array[index] = mutation.Value
				} else {
					parent.(map[string]any)[last] = mutation.Value
				}
				body, err := json.Marshal(document)
				if err != nil {
					test.Fatal(err)
				}
				record, parseError := decisionrecord.Parse(body)
				if mutation.Layer == "integrity" {
					if parseError != nil {
						test.Fatal(parseError)
					}
					if err := record.VerifyIntegrity(); !errors.Is(err, decisionrecord.ErrIntegrity) {
						test.Fatalf("integrity mutation accepted: %v", err)
					}
				} else if parseError == nil {
					test.Fatal("invalid structural mutation accepted")
				}
			})
		}
	}
}

func TestSignatureEnvelopeBindsRoleTimeAndValue(test *testing.T) {
	for field, replacement := range map[string]string{
		"role": "issuer-other", "signed_at": "2026-08-06T16:00:00Z",
		"signature": strings.Repeat("A", 86) + "==",
	} {
		test.Run(field, func(test *testing.T) {
			var document map[string]any
			decode(test, fixture(test, "valid/authorized.json"), &document)
			document["signatures"].([]any)[0].(map[string]any)[field] = replacement
			body, err := json.Marshal(document)
			if err != nil {
				test.Fatal(err)
			}
			record, err := decisionrecord.Parse(body)
			if err != nil {
				test.Fatal(err)
			}
			if err := record.VerifySignatureMath(fixtureKeys(test)); !errors.Is(err, decisionrecord.ErrSignature) {
				test.Fatalf("changed signature envelope accepted: %v", err)
			}
		})
	}
}

func TestWireFailuresAndUnsignedRecord(test *testing.T) {
	for _, body := range []string{
		`{"record_id":"one","record_id":"two"}`, `{} {}`,
		`"\ud800"`, `"\udfff"`, `"\ud800\u0041"`, string([]byte{'"', 0xff, '"'}),
	} {
		if _, err := decisionrecord.Parse([]byte(body)); !errors.Is(err, decisionrecord.ErrStructure) {
			test.Fatalf("invalid wire accepted: %v", err)
		}
	}
	var document map[string]any
	decode(test, fixture(test, "valid/authorized.json"), &document)
	document["signatures"] = []any{}
	body, err := json.Marshal(document)
	if err != nil {
		test.Fatal(err)
	}
	record, err := decisionrecord.Parse(body)
	if err != nil {
		test.Fatal(err)
	}
	if err := record.VerifyIntegrity(); err != nil {
		test.Fatal(err)
	}
	if err := record.VerifySignatureMath(fixtureKeys(test)); !errors.Is(err, decisionrecord.ErrSignature) {
		test.Fatalf("unsigned record passed mathematics check: %v", err)
	}
	// Lexically distinct numbers must not pass through floating-point decoding.
	body = bytes.Replace(body, []byte(`"approved":true`), []byte(`"approved":1.50`), 1)
	record, err = decisionrecord.Parse(body)
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(string(record.Bytes()), `"approved":1.50`) {
		test.Fatal("number token changed")
	}
	for _, empty := range []*decisionrecord.Record{nil, {}} {
		if err := empty.VerifyIntegrity(); !errors.Is(err, decisionrecord.ErrStructure) {
			test.Fatalf("unparsed record accepted: %v", err)
		}
	}
}
