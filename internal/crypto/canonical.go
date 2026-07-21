package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"golang.org/x/text/unicode/norm"
)

// ErrDuplicateKey is returned by CanonicalJSON when an input JSON
// document contains an object with two entries for the same key. The
// stdlib encoding/json decoder silently keeps the last value under
// the assumption that duplicate keys are a harmless mistake — for a
// cryptographic canonicalisation layer that assumption is a footgun:
// two JSON inputs that differ in their duplicate-key content can
// canonicalise to the same bytes, giving two distinct payloads the
// same hash. We reject them explicitly.
var ErrDuplicateKey = errors.New("canonical json: duplicate key")

// CanonicalJSON returns a deterministic JSON encoding of value. The
// encoding is the minimum required for cryptographic hashing:
//
//   - Object keys are sorted lexicographically at every level.
//   - There is no whitespace between tokens.
//   - Integers are preserved at full precision (no round-trip through
//     float64).
//   - Duplicate object keys in the input are rejected (ErrDuplicateKey).
//   - All string values are normalised to Unicode Normalization Form C
//     (NFC) before serialisation, so visually-identical strings with
//     different codepoint sequences hash to the same bytes.
//
// The function deliberately goes through json.Marshal first so that
// struct tags, embedded fields, and time.Time formatting all match the
// wire format the rest of the codebase uses. Then it streams the
// intermediate bytes through a duplicate-key-detecting decoder, builds
// an owned JSON value tree, and re-encodes the tree with sorted keys
// and NFC-normalised strings.
//
// The input MUST NOT contain cycles or non-JSON-serialisable values.
func CanonicalJSON(value any) ([]byte, error) {
	initial, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonical json: initial marshal: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(initial))
	decoder.UseNumber() // preserve integer precision for large ids
	intermediate, err := decodeStrict(decoder)
	if err != nil {
		return nil, fmt.Errorf("canonical json: intermediate decode: %w", err)
	}
	// Make sure the decoder consumed exactly one value. Trailing
	// tokens would indicate a mal-encoded input.
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, errors.New("canonical json: unexpected trailing tokens")
		}
		return nil, fmt.Errorf("canonical json: intermediate tail: %w", err)
	}
	var out bytes.Buffer
	if err := encodeCanonical(&out, intermediate); err != nil {
		return nil, fmt.Errorf("canonical json: canonical encode: %w", err)
	}
	return out.Bytes(), nil
}

// Digest returns the hex-encoded SHA-256 of the canonical JSON encoding
// of value. This is the one and only hash function used for CRL
// content addressing.
func Digest(value any) (string, error) {
	body, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// DigestBytes returns the hex-encoded SHA-256 of a raw byte slice. It
// is the low-level primitive Digest is built on; callers that already
// hold canonical bytes use this directly.
func DigestBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// decodeStrict consumes one JSON value from the supplied decoder,
// building an owned JSON value tree in the same shape json.Decode would
// produce, except:
//
//   - Object keys are checked for duplication — ErrDuplicateKey on
//     any repeat at any level of nesting.
//   - String values are NFC-normalised as they land in the tree so
//     the canonical encoder sees a single form.
//
// json.Decoder.Token() yields a stream of JSON tokens; decodeStrict
// consumes them recursively to build the value.
func decodeStrict(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeValue(dec, tok)
}

func decodeValue(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return decodeObject(dec)
		case '[':
			return decodeArray(dec)
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
	case string:
		return norm.NFC.String(t), nil
	case json.Number:
		return t, nil
	case bool:
		return t, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected token %T", tok)
	}
}

// decodeObject consumes tokens until the matching '}' and returns a
// map[string]any. A duplicate key at this level triggers ErrDuplicateKey.
func decodeObject(dec *json.Decoder) (map[string]any, error) {
	out := make(map[string]any)
	for {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := keyTok.(json.Delim); ok && delim == '}' {
			return out, nil
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected object key, got %T", keyTok)
		}
		// Normalise the key itself so visually-equal keys collide
		// correctly in the dup check.
		normalisedKey := norm.NFC.String(key)
		if _, dup := out[normalisedKey]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateKey, normalisedKey)
		}
		valueTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		value, err := decodeValue(dec, valueTok)
		if err != nil {
			return nil, err
		}
		out[normalisedKey] = value
	}
}

// decodeArray consumes tokens until the matching ']' and returns the
// []any in input order.
func decodeArray(dec *json.Decoder) ([]any, error) {
	var out []any
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := tok.(json.Delim); ok && delim == ']' {
			return out, nil
		}
		value, err := decodeValue(dec, tok)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
}

// encodeCanonical walks a JSON value tree (the result of decodeStrict)
// and writes the canonical form to out. Maps are serialised with keys
// sorted lexicographically; arrays preserve their order; scalars are
// delegated to json.Marshal so Go's default string/number/bool
// rendering is used.
func encodeCanonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
		return nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			encoded, err := json.Marshal(k)
			if err != nil {
				return err
			}
			out.Write(encoded)
			out.WriteByte(':')
			if err := encodeCanonical(out, v[k]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
		return nil
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := encodeCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
		return nil
	case json.Number:
		out.WriteString(v.String())
		return nil
	case string:
		encoded, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out.Write(encoded)
		return nil
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
		return nil
	default:
		// This path IS reachable if a map[string]any in the
		// input tree contains a chan, func, or other non-JSON value
		// that survived json.Marshal. Keep the error for robustness.
		return fmt.Errorf("canonical json: unsupported type %T", v)
	}
}
