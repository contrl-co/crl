// Package decisionrecord checks portable CRL v1 record structure, content
// integrity and signature mathematics. These checks do not establish authority,
// independent parties, current key status, decision replay or permission to act.
package decisionrecord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	crlcrypto "github.com/contrl-co/crl/internal/crypto"
	"github.com/contrl-co/crl/spec"
)

var ErrStructure = errors.New("decision record: invalid structure")

var schema = sync.OnceValues(spec.CompileDecisionRecordSchema)

// Record owns the parsed document. It exposes no mutable view, so validation
// remains true when the caller later checks hashes or signatures.
type Record struct {
	document  map[string]any
	canonical []byte
}

// Parse validates the closed v1 schema, embedded JSON, provenance coverage and
// array ordering. A successfully parsed record can still be forged or untrusted.
func Parse(body []byte) (*Record, error) {
	document, err := strictDocument(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStructure, err)
	}
	validator, err := schema()
	if err != nil {
		return nil, fmt.Errorf("%w: schema unavailable: %v", ErrStructure, err)
	}
	if err := validator.Validate(document); err != nil {
		return nil, fmt.Errorf("%w: schema: %v", ErrStructure, err)
	}
	record := &Record{document: document.(map[string]any)}
	bundle, err := strictDocument([]byte(record.object("rule")["canonical_bundle"].(string)))
	if err != nil {
		return nil, fmt.Errorf("%w: embedded bundle: %v", ErrStructure, err)
	}
	if _, valid := bundle.(map[string]any); !valid {
		return nil, fmt.Errorf("%w: bundle must be an object", ErrStructure)
	}
	if err := record.validateCoverage(); err != nil {
		return nil, err
	}
	record.canonical, err = crlcrypto.CanonicalJSON(document)
	if err != nil {
		return nil, fmt.Errorf("%w: canonical encoding: %v", ErrStructure, err)
	}
	return record, nil
}

// Bytes returns an owned copy of the canonical wire document, preserving number
// tokens. Re-encoding facts through floating point would change their hashes.
func (record *Record) Bytes() []byte {
	if record == nil {
		return nil
	}
	return bytes.Clone(record.canonical)
}

func (record *Record) object(name string) map[string]any {
	return record.document[name].(map[string]any)
}

func (record *Record) validateCoverage() error {
	evaluation := record.object("evaluation")
	facts := evaluation["facts"].(map[string]any)
	provenance := map[string]string{}
	previous := ""
	for _, value := range evaluation["provenance"].([]any) {
		entry := value.(map[string]any)
		name := entry["fact"].(string)
		if name <= previous || strings.HasPrefix(name, "observed_at.") {
			return fmt.Errorf("%w: provenance identity or order", ErrStructure)
		}
		if _, exists := facts[name]; !exists {
			return fmt.Errorf("%w: orphan provenance", ErrStructure)
		}
		provenance[name], previous = entry["observed_at"].(string), name
	}
	for name, value := range facts {
		if base, metadata := strings.CutPrefix(name, "observed_at."); metadata {
			if _, exists := facts[base]; !exists || provenance[base] != value {
				return fmt.Errorf("%w: observation metadata mismatch", ErrStructure)
			}
		} else if _, exists := provenance[name]; !exists {
			return fmt.Errorf("%w: missing provenance", ErrStructure)
		}
	}
	previous = ""
	for _, value := range record.document["signatures"].([]any) {
		signature := value.(map[string]any)
		identity := signature["role"].(string) + "\x00" + signature["key_id"].(string)
		if identity <= previous {
			return fmt.Errorf("%w: signature identity or order", ErrStructure)
		}
		previous = identity
	}
	return nil
}

func strictDocument(body []byte) (any, error) {
	if !utf8.Valid(body) {
		return nil, errors.New("invalid UTF-8")
	}
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
			return nil, err
		}
		offset += 4
		if codepoint >= 0xdc00 && codepoint <= 0xdfff {
			return nil, errors.New("unpaired low surrogate")
		}
		if codepoint >= 0xd800 && codepoint <= 0xdbff {
			if offset+6 >= len(body) || string(body[offset+1:offset+3]) != `\u` {
				return nil, errors.New("unpaired high surrogate")
			}
			low, err := strconv.ParseUint(string(body[offset+3:offset+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return nil, errors.New("unpaired high surrogate")
			}
			offset += 6
		}
	}
	if _, err := crlcrypto.CanonicalJSON(json.RawMessage(body)); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("trailing JSON data")
	}
	return document, nil
}
