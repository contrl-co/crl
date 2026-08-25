// Package pbenvelope encodes the contrl.crl.v1.CompiledBundle transport
// envelope, defined in proto/contrl/crl/v1/envelope.proto.
//
// The encoder is written by hand rather than generated. Generating it
// would add google.golang.org/protobuf, which links 78 further packages
// into a toolchain that otherwise depends on golang.org/x/text alone —
// a dependency set the browser build gate treats as an invariant,
// because everything cmd/crl-wasm links is published to every visitor.
// The message is five length-delimited scalars, the simplest shape the
// wire format has, so the trade is not close.
//
// Only encoding lives here. Consumers decode with a generated stub from
// the .proto file; nothing in this repository reads the envelope back.
//
// The envelope is never hashed, so it does not need canonical encoding.
// Protobuf could not provide one anyway.
package pbenvelope

// CompiledBundle mirrors contrl.crl.v1.CompiledBundle. Field numbers
// live in Marshal and must match the .proto.
type CompiledBundle struct {
	Edition         string
	SourceHash      string
	CanonicalText   string
	CanonicalBundle []byte
	Hash            string
}

// Marshal renders the envelope in protobuf wire format.
//
// Fields are emitted in field-number order. proto3 omits a scalar that
// holds its zero value, so an empty field contributes no bytes and a
// decoder reports the zero value for it.
func (m CompiledBundle) Marshal() []byte {
	var out []byte
	out = appendBytes(out, 1, []byte(m.Edition))
	out = appendBytes(out, 2, []byte(m.SourceHash))
	out = appendBytes(out, 3, []byte(m.CanonicalText))
	out = appendBytes(out, 4, m.CanonicalBundle)
	out = appendBytes(out, 5, []byte(m.Hash))
	return out
}

// appendBytes writes one length-delimited field: a tag carrying the
// field number and wire type 2, the payload length, then the payload.
func appendBytes(dst []byte, number int, payload []byte) []byte {
	if len(payload) == 0 {
		return dst
	}
	const lengthDelimited = 2
	dst = appendVarint(dst, uint64(number)<<3|lengthDelimited)
	dst = appendVarint(dst, uint64(len(payload)))
	return append(dst, payload...)
}

// appendVarint writes a base-128 varint, least significant group first,
// with the high bit set on every group but the last.
func appendVarint(dst []byte, value uint64) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}
