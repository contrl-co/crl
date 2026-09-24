package spec

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	crl "github.com/contrl-co/crl"
	crlcrypto "github.com/contrl-co/crl/internal/crypto"
)

func rejectSurrogates(body []byte) error {
	for offset := 0; offset < len(body); offset++ {
		if body[offset] != '\\' || offset+1 == len(body) {
			continue
		}
		offset++
		if body[offset] != 'u' || offset+4 >= len(body) {
			continue
		}
		codepoint, err := strconv.ParseUint(string(body[offset+1:offset+5]), 16, 16)
		if err != nil {
			return err
		}
		offset += 4
		if codepoint >= 0xdc00 && codepoint <= 0xdfff {
			return fmt.Errorf("unpaired low surrogate")
		}
		if codepoint >= 0xd800 && codepoint <= 0xdbff {
			if offset+6 >= len(body) || string(body[offset+1:offset+3]) != `\u` {
				return fmt.Errorf("unpaired high surrogate")
			}
			low, err := strconv.ParseUint(string(body[offset+3:offset+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired high surrogate")
			}
			offset += 6
		}
	}
	return nil
}

func TestDecisionRecordCanonicalTokens(test *testing.T) {
	for _, document := range []string{`"\ud800"`, `"\udfff"`, `"\ud800\u0041"`, `{"\ud800":1}`, `"\ud800\\udc00"`} {
		if _, err := strictDocument([]byte(document)); err == nil {
			test.Errorf("accepted invalid scalar: %s", document)
		}
	}
	for _, document := range []string{`"\ud83d\ude00"`, `"�"`, `"\\ud800"`} {
		if _, err := strictDocument([]byte(document)); err != nil {
			test.Errorf("rejected valid string %s: %v", document, err)
		}
	}
	for _, token := range []string{"1.50", "1.5", "1e2", "100", "1.0", "1", "-0", "0"} {
		wire := []byte(`{"number":` + token + `}`)
		parsed := decodeStrictDocument(test, wire)
		canonical, err := crlcrypto.CanonicalJSON(parsed)
		if err != nil || !bytes.Equal(canonical, wire) {
			test.Fatalf("number token changed: %s -> %s (%v)", wire, canonical, err)
		}
		input := append([]byte("crl-decision-record/v1\x00"), wire...)
		expected := sha256.Sum256(input)
		if domainDigest(test, "crl-decision-record/v1", parsed) != hex.EncodeToString(expected[:]) {
			test.Fatalf("wire-token hash mismatch for %s", token)
		}
	}
	value := map[string]any{"text": "<>&/\"\\\b\t\n\f\r\x00\u2028\u2029é"}
	canonical, err := crlcrypto.CanonicalJSON(value)
	if err != nil || string(canonical) != `{"text":"<>&/\"\\\b\t\n\f\r\u0000\u2028\u2029é"}` {
		test.Fatalf("string escaping: %s (%v)", canonical, err)
	}
}

func recordStructure(test *testing.T, record map[string]any) error {
	if err := loadDecisionRecordSchema(test).Validate(record); err != nil {
		return err
	}
	bundle, err := strictDocument([]byte(record["rule"].(map[string]any)["canonical_bundle"].(string)))
	if err != nil {
		return err
	}
	if _, ok := bundle.(map[string]any); !ok {
		return fmt.Errorf("bundle must be an object")
	}
	evaluation := record["evaluation"].(map[string]any)
	facts := evaluation["facts"].(map[string]any)
	provenance := map[string]string{}
	previous := ""
	for _, entry := range evaluation["provenance"].([]any) {
		item := entry.(map[string]any)
		name := item["fact"].(string)
		if name <= previous || strings.HasPrefix(name, "observed_at.") {
			return fmt.Errorf("provenance order or identity")
		}
		if _, exists := facts[name]; !exists {
			return fmt.Errorf("orphan provenance")
		}
		provenance[name] = item["observed_at"].(string)
		previous = name
	}
	for name, value := range facts {
		if base, metadata := strings.CutPrefix(name, "observed_at."); metadata {
			if _, exists := facts[base]; !exists || provenance[base] != value {
				return fmt.Errorf("orphan or mismatched observation time")
			}
		} else if _, exists := provenance[name]; !exists {
			return fmt.Errorf("missing provenance")
		}
	}
	previous = ""
	for _, entry := range record["signatures"].([]any) {
		signature := entry.(map[string]any)
		identity := signature["role"].(string) + "\x00" + signature["key_id"].(string)
		if identity <= previous {
			return fmt.Errorf("signature order or identity")
		}
		previous = identity
	}
	return nil
}

func recordIntegrity(test *testing.T, record map[string]any) error {
	rule := record["rule"].(map[string]any)
	for _, field := range []struct{ content, digest string }{{"source", "source_hash"}, {"canonical_bundle", "bundle_hash"}} {
		digest := sha256.Sum256([]byte(rule[field.content].(string)))
		if rule[field.digest] != hex.EncodeToString(digest[:]) {
			return fmt.Errorf("%s mismatch", field.digest)
		}
	}
	evaluation := record["evaluation"].(map[string]any)
	if evaluation["trace_hash"] != domainDigest(test, "crl-decision-trace/v1", evaluation["trace"]) {
		return fmt.Errorf("trace hash mismatch")
	}
	unsigned := make(map[string]any, len(record)-2)
	for name, value := range record {
		if name != "record_hash" && name != "signatures" {
			unsigned[name] = value
		}
	}
	if record["record_hash"] != domainDigest(test, "crl-decision-record/v1", unsigned) {
		return fmt.Errorf("record hash mismatch")
	}
	return nil
}

func recordDecision(test *testing.T, record map[string]any) error {
	evaluation := record["evaluation"].(map[string]any)
	identity := evaluation["evaluator"].(map[string]any)
	if identity["id"] != "github.com/contrl-co/crl" || identity["revision"] != "git:43f49dcbfc49fe7e3381fad4b37814b95d2d8a34" {
		return fmt.Errorf("unsupported evaluator")
	}
	rule := record["rule"].(map[string]any)
	compiled, err := crl.Compile(rule["source"].(string))
	if err != nil {
		return err
	}
	bundle, err := compiled.CanonicalBundle()
	if err != nil {
		return err
	}
	if rule["edition"] != compiled.Edition || rule["canonical_text"] != compiled.CanonicalText || rule["canonical_bundle"] != string(bundle) {
		return fmt.Errorf("compilation mismatch")
	}
	instant, err := time.Parse(time.RFC3339Nano, evaluation["at"].(string))
	if err != nil {
		return err
	}
	actual := compiled.EvaluateAt(crl.Facts(evaluation["facts"].(map[string]any)), instant)
	actualBytes, err := crlcrypto.CanonicalJSON(actual)
	if err != nil {
		return err
	}
	recordedBytes, err := crlcrypto.CanonicalJSON(evaluation["trace"])
	if err != nil {
		return err
	}
	if evaluation["outcome"] != string(actual.Result) || !bytes.Equal(actualBytes, recordedBytes) {
		return fmt.Errorf("decision mismatch")
	}
	return nil
}

// This policy is a conformance fixture, not the separately versioned trust service.
func fixtureTrust(test *testing.T, record map[string]any) error {
	policyBytes := readFixture(test, "testdata/decision-record-v1/policy.json")
	policy := decodeStrictDocument(test, policyBytes).(map[string]any)
	policyHash := sha256.Sum256(append([]byte("crl-decision-trust-policy/v1\x00"), policyBytes...))
	binding := record["trust_policy"].(map[string]any)
	if binding["id"] != policy["id"] || binding["hash"] != hex.EncodeToString(policyHash[:]) || record["context"].(map[string]any)["domain"] != policy["domain"] {
		return fmt.Errorf("unapproved policy")
	}
	parties := map[string]bool{}
	roles := map[string]bool{}
	for _, entry := range record["signatures"].([]any) {
		signature := entry.(map[string]any)
		key, exists := policy["keys"].(map[string]any)[signature["key_id"].(string)]
		if !exists {
			return fmt.Errorf("unknown key")
		}
		authority := key.(map[string]any)
		if signature["role"] != authority["role"] || signature["signed_at"] != policy["at"] {
			return fmt.Errorf("role or fixture time mismatch")
		}
		publicKey, err := hex.DecodeString(authority["public_key"].(string))
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			test.Fatal("invalid fixture public key")
		}
		payload := map[string]any{"record_hash": record["record_hash"]}
		for _, name := range []string{"algorithm", "key_id", "role", "signed_at"} {
			payload[name] = signature[name]
		}
		canonical, err := crlcrypto.CanonicalJSON(payload)
		if err != nil {
			test.Fatal(err)
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(signature["signature"].(string))
		if err != nil || !ed25519.Verify(publicKey, append([]byte("crl-decision-signature/v1\x00"), canonical...), decoded) {
			return fmt.Errorf("invalid signature")
		}
		parties[authority["party"].(string)] = true
		roles[signature["role"].(string)] = true
	}
	if len(parties) < 2 || !roles["issuer"] || !roles["reviewer"] {
		return fmt.Errorf("independent parties and required roles not satisfied")
	}
	return nil
}

func TestDecisionRecordContract(test *testing.T) {
	fixture := readFixture(test, "testdata/decision-record-v1/valid/authorized.json")
	record := decodeStrictDocument(test, fixture).(map[string]any)
	checks := map[string]func(*testing.T, map[string]any) error{"structure": recordStructure, "integrity": recordIntegrity, "trust": fixtureTrust, "decision": recordDecision}
	for layer, check := range checks {
		if err := check(test, record); err != nil {
			test.Fatalf("valid fixture %s: %v", layer, err)
		}
	}
	var cases []mutation
	decodeStrictInto(test, readFixture(test, "testdata/decision-record-v1/invalid/contract.json"), &cases)
	for _, scenario := range cases {
		test.Run(scenario.Name, func(test *testing.T) {
			changed := decodeStrictDocument(test, fixture).(map[string]any)
			if err := replaceAtPath(changed, scenario.Path, scenario.Value); err != nil {
				test.Fatal(err)
			}
			if err := checks[scenario.Layer](test, changed); err == nil {
				test.Fatalf("%s accepted invalid record", scenario.Layer)
			}
		})
	}
}

func TestDecisionSignatureVectors(test *testing.T) {
	policy := decodeStrictDocument(test, readFixture(test, "testdata/decision-record-v1/policy.json")).(map[string]any)
	record := decodeStrictDocument(test, readFixture(test, "testdata/decision-record-v1/valid/authorized.json")).(map[string]any)
	vectors := decodeStrictDocument(test, readFixture(test, "testdata/decision-record-v1/signature-vectors.json")).([]any)
	for _, entry := range vectors {
		vector := entry.(map[string]any)
		authority := policy["keys"].(map[string]any)[vector["key_id"].(string)].(map[string]any)
		publicKey, err := hex.DecodeString(authority["public_key"].(string))
		if err != nil {
			test.Fatal(err)
		}
		payload := map[string]any{"algorithm": "ed25519", "key_id": vector["key_id"], "role": authority["role"], "signed_at": policy["at"], "record_hash": record["record_hash"]}
		canonical, err := crlcrypto.CanonicalJSON(payload)
		if err != nil {
			test.Fatal(err)
		}
		message := append([]byte("crl-decision-signature/v1\x00"), canonical...)
		if hex.EncodeToString(message) != vector["message_hex"] {
			test.Fatal("signed bytes differ from published vector")
		}
		signature, err := base64.StdEncoding.Strict().DecodeString(vector["signature"].(string))
		if err != nil || !ed25519.Verify(publicKey, message, signature) {
			test.Fatal("published signature does not verify")
		}
		for _, prefix := range []string{"", "crl-decision-record/v1\x00", "crl-decision-trace/v1\x00"} {
			if ed25519.Verify(publicKey, append([]byte(prefix), canonical...), signature) {
				test.Fatalf("signature verified with wrong domain %q", prefix)
			}
		}
	}
}

func TestDecisionRecordTraceShapes(test *testing.T) {
	record := decodeStrictDocument(test, readFixture(test, "testdata/decision-record-v1/valid/authorized.json")).(map[string]any)
	evaluation := record["evaluation"].(map[string]any)
	instant, err := time.Parse(time.RFC3339Nano, evaluation["at"].(string))
	if err != nil {
		test.Fatal(err)
	}
	vectors := decodeStrictDocument(test, readFixture(test, "testdata/decision-record-v1/trace-vectors.json")).([]any)
	for _, entry := range vectors {
		vector := entry.(map[string]any)
		test.Run(vector["name"].(string), func(test *testing.T) {
			compiled, err := crl.Compile(vector["source"].(string))
			if err != nil {
				test.Fatal(err)
			}
			at, err := time.Parse(time.RFC3339Nano, vector["at"].(string))
			if err != nil {
				test.Fatal(err)
			}
			actual := jsonValue(test, compiled.EvaluateAt(crl.Facts(vector["facts"].(map[string]any)), at))
			if actual.(map[string]any)["result"] != vector["outcome"] || domainDigest(test, "crl-decision-trace/v1", actual) != vector["trace_hash"] {
				test.Fatalf("trace vector does not reproduce: %v", actual)
			}
			evaluation["trace"] = actual
			evaluation["outcome"] = actual.(map[string]any)["result"]
			if err := loadDecisionRecordSchema(test).Validate(record); err != nil {
				test.Fatal(err)
			}
		})
	}
	evaluation["trace"] = jsonValue(test, (crl.Compiled{}).EvaluateAt(nil, instant))
	evaluation["outcome"] = string(crl.InsufficientEvidence)
	if err := loadDecisionRecordSchema(test).Validate(record); err != nil {
		test.Fatalf("API refusal is not recordable: %v", err)
	}
	if err := recordDecision(test, record); err == nil {
		test.Fatal("API refusal incorrectly reproduces a valid program")
	}
}
