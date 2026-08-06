package decisionrecord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"

	crlcrypto "gitlab.com/contrl-group/crl/internal/crypto"
	"gitlab.com/contrl-group/crl/spec"
)

// ErrStructural marks invalid wire bytes or a violation of the closed v1
// schema and structural invariants.
var ErrStructural = errors.New("decision record: structurally invalid")

var (
	schemaOnce  sync.Once
	v1Schema    *jsonschema.Schema
	v1SchemaErr error
)

// Parse strictly parses and structurally validates one decision-record v1
// document. It does not verify hashes, signatures, trust, replay, or the
// recorded decision.
func Parse(body []byte) (*Record, error) {
	document, err := strictDocument(body)
	if err != nil {
		return nil, structural("JSON: %v", err)
	}
	schema, err := decisionRecordSchema()
	if err != nil {
		return nil, structural("load embedded schema: %v", err)
	}
	if err := schema.Validate(document); err != nil {
		return nil, structural("schema: %v", err)
	}
	record, err := decodeRecord(document)
	if err != nil {
		return nil, structural("decode: %v", err)
	}
	if err := validateProvenance(record); err != nil {
		return nil, structural("provenance: %v", err)
	}
	if err := validateSignatures(record.Signatures); err != nil {
		return nil, structural("signatures: %v", err)
	}
	return record, nil
}

func decisionRecordSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		document, err := strictDocument(spec.DecisionRecordV1Schema())
		if err != nil {
			v1SchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.AssertFormat()
		compiler.AssertContent()
		if err := compiler.AddResource(spec.DecisionRecordV1SchemaID, document); err != nil {
			v1SchemaErr = err
			return
		}
		v1Schema, v1SchemaErr = compiler.Compile(spec.DecisionRecordV1SchemaID)
	})
	return v1Schema, v1SchemaErr
}

func strictDocument(body []byte) (any, error) {
	if !utf8.Valid(body) {
		return nil, errors.New("invalid UTF-8")
	}
	if err := validateUnicodeEscapes(body); err != nil {
		return nil, err
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
		if err == nil {
			return nil, errors.New("unexpected trailing JSON document")
		}
		return nil, err
	}
	return document, nil
}

func validateUnicodeEscapes(body []byte) error {
	inString := false
	for index := 0; index < len(body); index++ {
		switch body[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(body) {
				continue
			}
			if body[index+1] != 'u' {
				index++
				continue
			}
			value, ok := unicodeEscape(body, index)
			if !ok {
				continue // The JSON decoder reports truncated or non-hex escapes.
			}
			if value >= 0xdc00 && value <= 0xdfff {
				return errors.New("unpaired low-surrogate escape")
			}
			if value >= 0xd800 && value <= 0xdbff {
				next := index + 6
				low, valid := unicodeEscape(body, next)
				if !valid || low < 0xdc00 || low > 0xdfff {
					return errors.New("unpaired high-surrogate escape")
				}
				index = next + 5
				continue
			}
			index += 5
		}
	}
	return nil
}

func unicodeEscape(body []byte, index int) (uint16, bool) {
	if index < 0 || index+6 > len(body) || body[index] != '\\' || body[index+1] != 'u' {
		return 0, false
	}
	var value uint16
	for _, digit := range body[index+2 : index+6] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func validateProvenance(record *Record) error {
	byFact := make(map[string]Provenance, len(record.Evaluation.Provenance))
	previous := ""
	for index, item := range record.Evaluation.Provenance {
		if _, exists := byFact[item.Fact]; exists {
			return fmt.Errorf("duplicate fact %q", item.Fact)
		}
		if index > 0 && item.Fact <= previous {
			return fmt.Errorf("facts are not in ascending fact-name order")
		}
		byFact[item.Fact] = item
		previous = item.Fact
	}

	allNames := make([]string, 0, len(record.Evaluation.Facts))
	for name := range record.Evaluation.Facts {
		allNames = append(allNames, name)
	}
	sort.Strings(allNames)
	factNames := make([]string, 0, len(allNames))
	for _, name := range allNames {
		if strings.HasPrefix(name, "observed_at.") {
			base := strings.TrimPrefix(name, "observed_at.")
			if _, exists := record.Evaluation.Facts[base]; !exists {
				return fmt.Errorf("metadata %q has no fact %q", name, base)
			}
			provenance, exists := byFact[base]
			if !exists {
				return fmt.Errorf("metadata %q has no provenance for %q", name, base)
			}
			observedAt, ok := record.Evaluation.Facts[name].(string)
			if !ok || observedAt != provenance.ObservedAt {
				return fmt.Errorf("metadata %q does not match provenance observed_at", name)
			}
			continue
		}
		factNames = append(factNames, name)
	}
	for _, name := range factNames {
		if _, exists := byFact[name]; !exists {
			return fmt.Errorf("fact %q has no provenance", name)
		}
	}
	return nil
}

func validateRecord(record *Record) error {
	if record == nil {
		return structural("record is nil")
	}
	body, err := json.Marshal(record)
	if err != nil {
		return structural("encode record: %v", err)
	}
	if err := validateRecordUTF8(record); err != nil {
		return structural("UTF-8: %v", err)
	}
	document, err := strictDocument(body)
	if err != nil {
		return structural("JSON: %v", err)
	}
	schema, err := decisionRecordSchema()
	if err != nil {
		return structural("load embedded schema: %v", err)
	}
	if err := schema.Validate(document); err != nil {
		return structural("schema: %v", err)
	}
	if err := validateProvenance(record); err != nil {
		return structural("provenance: %v", err)
	}
	if err := validateSignatures(record.Signatures); err != nil {
		return structural("signatures: %v", err)
	}
	return nil
}

func validateRecordUTF8(record *Record) error {
	fields := []namedString{
		{name: "schema_version", value: record.SchemaVersion},
		{name: "record_id", value: record.RecordID},
		{name: "created_at", value: record.CreatedAt},
		{name: "context.domain", value: record.Context.Domain},
		{name: "context.subject", value: record.Context.Subject},
		{name: "context.correlation_id", value: record.Context.CorrelationID},
		{name: "rule.edition", value: record.Rule.Edition},
		{name: "rule.source", value: record.Rule.Source},
		{name: "rule.source_hash", value: record.Rule.SourceHash},
		{name: "rule.canonical_text", value: record.Rule.CanonicalText},
		{name: "rule.canonical_bundle", value: record.Rule.CanonicalBundle},
		{name: "rule.bundle_hash", value: record.Rule.BundleHash},
		{name: "evaluation.at", value: record.Evaluation.At},
		{name: "evaluation.outcome", value: string(record.Evaluation.Outcome)},
		{name: "evaluation.trace_hash", value: record.Evaluation.TraceHash},
		{name: "record_hash", value: record.RecordHash},
	}
	for _, field := range fields {
		if !utf8.ValidString(field.value) {
			return fmt.Errorf("%s is invalid", field.name)
		}
	}
	for index, item := range record.Evaluation.Provenance {
		for _, field := range []namedString{
			{name: "fact", value: item.Fact},
			{name: "supplier", value: item.Supplier},
			{name: "source", value: item.Source},
			{name: "source_digest", value: item.SourceDigest},
			{name: "observed_at", value: item.ObservedAt},
		} {
			if !utf8.ValidString(field.value) {
				return fmt.Errorf("evaluation.provenance[%d].%s is invalid", index, field.name)
			}
		}
	}
	for index, item := range record.Signatures {
		for _, field := range []namedString{
			{name: "algorithm", value: item.Algorithm},
			{name: "key_id", value: item.KeyID},
			{name: "role", value: item.Role},
			{name: "signed_at", value: item.SignedAt},
			{name: "signature", value: item.Signature},
		} {
			if !utf8.ValidString(field.value) {
				return fmt.Errorf("signatures[%d].%s is invalid", index, field.name)
			}
		}
	}
	for _, field := range []struct {
		name  string
		value any
	}{
		{name: "evaluation.facts", value: record.Evaluation.Facts},
		{name: "evaluation.trace", value: record.Evaluation.Trace},
		{name: "extensions", value: record.Extensions},
	} {
		if !validJSONUTF8(field.value) {
			return fmt.Errorf("%s contains invalid UTF-8", field.name)
		}
	}
	return nil
}

type namedString struct {
	name  string
	value string
}

func validJSONUTF8(value any) bool {
	return validJSONUTF8Value(reflect.ValueOf(value))
}

func validJSONUTF8Value(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return true
		}
		return validJSONUTF8Value(value.Elem())
	case reflect.String:
		return utf8.ValidString(value.String())
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if !validJSONUTF8Value(value.Index(index)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return false
		}
		iterator := value.MapRange()
		for iterator.Next() {
			if !utf8.ValidString(iterator.Key().String()) || !validJSONUTF8Value(iterator.Value()) {
				return false
			}
		}
		return true
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func validateSignatures(signatures []Signature) error {
	seen := make(map[string]struct{}, len(signatures))
	previous := ""
	for index, signature := range signatures {
		identity := signature.Role + "\x00" + signature.KeyID
		if _, exists := seen[identity]; exists {
			return fmt.Errorf("duplicate identity (%q, %q)", signature.Role, signature.KeyID)
		}
		if index > 0 && identity <= previous {
			return errors.New("entries are not in ascending (role, key_id) order")
		}
		seen[identity] = struct{}{}
		previous = identity
	}
	return nil
}

func structural(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrStructural, fmt.Sprintf(format, args...))
}
