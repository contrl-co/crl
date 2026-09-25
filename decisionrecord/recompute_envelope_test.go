package decisionrecord

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/contrl-co/crl/internal/pbenvelope"
)

func TestRecomputationRequiresCompleteCompilationEnvelope(test *testing.T) {
	body, err := os.ReadFile("../spec/testdata/decision-record-v1/valid/authorized.json")
	if err != nil {
		test.Fatal(err)
	}
	record, err := Parse(body)
	if err != nil {
		test.Fatal(err)
	}
	rule := record.object("rule")
	envelope := pbenvelope.CompiledBundle{
		Edition: rule["edition"].(string), SourceHash: rule["source_hash"].(string),
		CanonicalText: rule["canonical_text"].(string), CanonicalBundle: []byte(rule["canonical_bundle"].(string)),
		Hash: rule["bundle_hash"].(string),
	}.Marshal()
	if err := record.compareCompilation(envelope); err != nil {
		test.Fatal(err)
	}
	for _, malformed := range [][]byte{
		nil, {0xff}, {0}, {0x0a, 0xff}, {0x0a, 0x7f, 'v'},
		envelope[4:], envelope[:len(envelope)-1],
		append(bytes.Clone(envelope), envelope[:4]...),
		append(bytes.Clone(envelope), 0x32, 0),
	} {
		if err := record.compareCompilation(malformed); !errors.Is(err, ErrRecomputation) {
			test.Fatalf("incomplete, duplicate or malformed compilation accepted: %v", err)
		}
	}
}
