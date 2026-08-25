package pbenvelope

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// walk decodes protobuf wire format generically, the way protoc
// --decode_raw does: it knows nothing about this message. Writing it
// independently of Marshal is the point — a decoder that mirrored the
// encoder would agree with it about a wrong tag.
func walk(t *testing.T, data []byte) map[int][]byte {
	t.Helper()
	fields := map[int][]byte{}
	for len(data) > 0 {
		tag, n := readVarint(t, data)
		data = data[n:]
		number, wireType := int(tag>>3), int(tag&0x7)
		if wireType != 2 {
			t.Fatalf("field %d: wire type %d, want 2 (length-delimited)", number, wireType)
		}
		length, n := readVarint(t, data)
		data = data[n:]
		if uint64(len(data)) < length {
			t.Fatalf("field %d: declares %d bytes, %d remain", number, length, len(data))
		}
		if _, seen := fields[number]; seen {
			t.Fatalf("field %d appears twice", number)
		}
		fields[number] = data[:length]
		data = data[length:]
	}
	return fields
}

func readVarint(t *testing.T, data []byte) (uint64, int) {
	t.Helper()
	var value uint64
	for i := 0; i < len(data); i++ {
		value |= uint64(data[i]&0x7f) << (7 * i)
		if data[i] < 0x80 {
			return value, i + 1
		}
	}
	t.Fatal("truncated varint")
	return 0, 0
}

func sample() CompiledBundle {
	return CompiledBundle{
		Edition:         "v1",
		SourceHash:      "aa11",
		CanonicalText:   "crl v1\n",
		CanonicalBundle: []byte(`{"rules":[]}`),
		Hash:            "bb22",
	}
}

// The field numbers on the wire are the contract every consumer generates
// against. Renumbering silently breaks every decoder in the org.
func TestFieldNumbersMatchTheProtoContract(t *testing.T) {
	fields := walk(t, sample().Marshal())
	for number, want := range map[int]string{
		1: "v1", 2: "aa11", 3: "crl v1\n", 4: `{"rules":[]}`, 5: "bb22",
	} {
		if got := string(fields[number]); got != want {
			t.Errorf("field %d = %q, want %q", number, got, want)
		}
	}
	if len(fields) != 5 {
		t.Errorf("decoded %d fields, want 5", len(fields))
	}
}

// A length over 127 needs a multi-byte varint. Getting this wrong is the
// classic hand-rolled-encoder bug, and every real bundle is over 127 bytes.
func TestPayloadLongerThanOneVarintByte(t *testing.T) {
	body := []byte(strings.Repeat("x", 300))
	fields := walk(t, CompiledBundle{CanonicalBundle: body}.Marshal())
	if !bytes.Equal(fields[4], body) {
		t.Errorf("300-byte payload did not survive: got %d bytes", len(fields[4]))
	}
}

// proto3 omits a scalar holding its zero value; a decoder reports the zero
// value for a field that never appears.
func TestEmptyFieldsAreOmitted(t *testing.T) {
	fields := walk(t, CompiledBundle{Edition: "v1"}.Marshal())
	if len(fields) != 1 {
		t.Errorf("empty fields were emitted: decoded %d fields, want 1", len(fields))
	}
}

// Pinned so any change to the encoding is visible in a diff rather than
// discovered by a consumer.
func TestGoldenEncoding(t *testing.T) {
	const want = "0a0276311204616131311a0763726c2076310a22" +
		"0c7b2272756c6573223a5b5d7d2a0462623232"
	if got := hex.EncodeToString(sample().Marshal()); got != want {
		t.Errorf("encoding moved:\n got %s\nwant %s", got, want)
	}
}
