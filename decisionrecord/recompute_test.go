package decisionrecord_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	crl "github.com/contrl-co/crl"
	"github.com/contrl-co/crl/decisionrecord"
	crlcrypto "github.com/contrl-co/crl/internal/crypto"
)

func portableDigest(test *testing.T, domain string, value any) string {
	test.Helper()
	body, err := crlcrypto.CanonicalJSON(value)
	if err != nil {
		test.Fatal(err)
	}
	return crlcrypto.DigestBytes(append([]byte(domain+"\x00"), body...))
}

func recomputationRecord(test *testing.T, revision string, mutate func(map[string]any)) *decisionrecord.Record {
	test.Helper()
	var document map[string]any
	decode(test, fixture(test, "valid/authorized.json"), &document)
	document["evaluation"].(map[string]any)["evaluator"].(map[string]any)["revision"] = revision
	mutate(document)
	evaluation := document["evaluation"].(map[string]any)
	rule := document["rule"].(map[string]any)
	rule["source_hash"] = crlcrypto.DigestBytes([]byte(rule["source"].(string)))
	rule["bundle_hash"] = crlcrypto.DigestBytes([]byte(rule["canonical_bundle"].(string)))
	evaluation["trace_hash"] = portableDigest(test, "crl-decision-trace/v1", evaluation["trace"])
	delete(document, "record_hash")
	delete(document, "signatures")
	document["record_hash"] = portableDigest(test, "crl-decision-record/v1", document)
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
	return record
}

func TestPinnedArtifactRecomputation(test *testing.T) {
	program := filepath.Join(test.TempDir(), "crlc.exe")
	build := exec.Command("go", "build", "-o", program, "../cmd/crlc")
	if output, err := build.CombinedOutput(); err != nil {
		test.Fatalf("build pinned artifact: %v: %s", err, output)
	}
	executable, err := os.ReadFile(program)
	if err != nil {
		test.Fatal(err)
	}
	revision := "sha256:" + crlcrypto.DigestBytes(executable)
	artifact := decisionrecord.EvaluatorArtifact{ID: "github.com/contrl-co/crl", Path: program}
	privateRoot := test.TempDir()
	test.Setenv("TMPDIR", privateRoot)
	test.Setenv("TMP", privateRoot)
	unchanged := func(map[string]any) {}
	record := recomputationRecord(test, revision, unchanged)
	if err := record.VerifyRecomputation(test.Context(), artifact); err != nil {
		test.Fatal(err)
	}
	if err := record.VerifySignatureMath(nil); !errors.Is(err, decisionrecord.ErrSignature) {
		test.Fatal("successful recomputation must not grant signer authority")
	}
	for _, scenario := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"different canonical text", func(document map[string]any) { document["rule"].(map[string]any)["canonical_text"] = "different" }},
		{"different exact bundle bytes", func(document map[string]any) {
			document["rule"].(map[string]any)["canonical_bundle"] = " " + document["rule"].(map[string]any)["canonical_bundle"].(string)
		}},
		{"different source", func(document map[string]any) {
			document["rule"].(map[string]any)["source"] = strings.ReplaceAll(document["rule"].(map[string]any)["source"].(string), "need approved == true", "need approved == false")
		}},
		{"different facts", func(document map[string]any) {
			document["evaluation"].(map[string]any)["facts"].(map[string]any)["approved"] = false
		}},
		{"different clock", func(document map[string]any) { document["evaluation"].(map[string]any)["at"] = "2027-08-06T15:00:00Z" }},
		{"different trace", func(document map[string]any) {
			document["evaluation"].(map[string]any)["trace"].(map[string]any)["checks"].([]any)[0].(map[string]any)["actual"] = false
		}},
		{"unknown implementation", func(document map[string]any) {
			document["evaluation"].(map[string]any)["evaluator"].(map[string]any)["id"] = "unknown"
		}},
		{"unavailable revision", func(document map[string]any) {
			document["evaluation"].(map[string]any)["evaluator"].(map[string]any)["revision"] = "sha256:" + strings.Repeat("0", 64)
		}},
		{"unmapped source revision", func(document map[string]any) {
			document["evaluation"].(map[string]any)["evaluator"].(map[string]any)["revision"] = "git:" + strings.Repeat("a", 40)
		}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			changed := recomputationRecord(test, revision, scenario.edit)
			if err := changed.VerifyRecomputation(test.Context(), artifact); !errors.Is(err, decisionrecord.ErrRecomputation) {
				test.Fatalf("self-consistent forgery reproduced: %v", err)
			}
		})
	}
	contextCanceled, cancel := context.WithCancel(test.Context())
	cancel()
	if err := record.VerifyRecomputation(contextCanceled, artifact); !errors.Is(err, decisionrecord.ErrRecomputation) {
		test.Fatalf("canceled recomputation passed: %v", err)
	}
	var vectors []struct {
		Name, Source, At, Outcome string
		Facts                     map[string]any
		TraceHash                 string `json:"trace_hash"`
	}
	decode(test, fixture(test, "trace-vectors.json"), &vectors)
	for _, vector := range vectors {
		test.Run(vector.Name, func(test *testing.T) {
			compiled, err := crl.Compile(vector.Source)
			if err != nil {
				test.Fatal(err)
			}
			at, err := time.Parse(time.RFC3339Nano, vector.At)
			if err != nil {
				test.Fatal(err)
			}
			trace := compiled.EvaluateAt(vector.Facts, at)
			if string(trace.Result) != vector.Outcome || portableDigest(test, "crl-decision-trace/v1", trace) != vector.TraceHash {
				test.Fatal("frozen public trace vector changed")
			}
			bundle, err := compiled.CanonicalBundle()
			if err != nil {
				test.Fatal(err)
			}
			var portableTrace any = trace
			if vector.Name == "numeric reference" {
				// The fixture records the public API with json.Number facts. This
				// CLI artifact decodes facts as float64, producing distinct tokens.
				// Attribute only that artifact's actual public trace to its pin.
				body, err := json.Marshal(trace)
				if err != nil {
					test.Fatal(err)
				}
				body = bytes.ReplaceAll(body, []byte(`1.50`), []byte(`1.5`))
				body = bytes.ReplaceAll(body, []byte(`1.0`), []byte(`1`))
				decode(test, body, &portableTrace)
			}
			record := recomputationRecord(test, revision, func(document map[string]any) {
				rule := document["rule"].(map[string]any)
				rule["source"], rule["canonical_text"], rule["canonical_bundle"] = vector.Source, compiled.CanonicalText, string(bundle)
				evaluation := document["evaluation"].(map[string]any)
				evaluation["at"], evaluation["facts"], evaluation["trace"], evaluation["outcome"] = vector.At, vector.Facts, portableTrace, vector.Outcome
				var names []string
				for name := range vector.Facts {
					if !strings.HasPrefix(name, "observed_at.") {
						names = append(names, name)
					}
				}
				sort.Strings(names)
				provenance := []any{}
				for _, name := range names {
					observedAt := vector.Facts["observed_at."+name]
					if observedAt == nil {
						observedAt = vector.At
					}
					provenance = append(provenance, map[string]any{"fact": name, "observed_at": observedAt, "supplier": "conformance-fixture", "source": "conformance-fixture", "source_digest": strings.Repeat("0", 64)})
				}
				evaluation["provenance"] = provenance
			})
			if err := record.VerifyRecomputation(test.Context(), artifact); err != nil {
				test.Fatal(err)
			}
			if vector.Name == "numeric reference" {
				var document map[string]any
				decode(test, record.Bytes(), &document)
				changed := recomputationRecord(test, revision, func(target map[string]any) {
					for key, value := range document {
						target[key] = value
					}
					target["evaluation"].(map[string]any)["trace"] = trace
				})
				if err := changed.VerifyRecomputation(test.Context(), artifact); !errors.Is(err, decisionrecord.ErrRecomputation) {
					test.Fatalf("different numeric trace tokens accepted: %v", err)
				}
			}
		})
	}
	if err := os.WriteFile(program, []byte("replaced artifact"), 0600); err != nil {
		test.Fatal(err)
	}
	if err := record.VerifyRecomputation(test.Context(), artifact); !errors.Is(err, decisionrecord.ErrRecomputation) {
		test.Fatalf("substituted executable accepted: %v", err)
	}
	entries, err := os.ReadDir(privateRoot)
	if err != nil || len(entries) != 0 {
		test.Fatalf("private facts or executable retained after success/refusal: %v %v", entries, err)
	}
}
