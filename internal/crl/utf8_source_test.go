package crl

import (
	"strings"
	"testing"
)

// The source path must reject raw invalid UTF-8 before the []rune
// conversion folds it to U+FFFD. Otherwise byte-distinct sources compile
// to one hash while the evaluator still compares their differing bytes.
func TestCompileRejectsInvalidUTF8Source(t *testing.T) {
	base := "crl v1\npackage e.u\nbundle u.t\nrule r\n\ttarget a.b\n\tcollector c org api from /x.json\n\t\tsignal s string from x.y ttl 30d\n\tneed s != {LIT}\n\tquorum c\n"
	// The byte-valid twin must compile, so a rejection below can only
	// come from the encoding check, not an unrelated parse failure.
	if _, err := CompileBundle(strings.Replace(base, "{LIT}", "\"AB\"", 1)); err != nil {
		t.Fatalf("byte-valid twin must compile: %v", err)
	}
	for name, lit := range map[string]string{"0xff": "\"A\xffB\"", "0xfe": "\"A\xfeB\""} {
		_, err := CompileBundle(strings.Replace(base, "{LIT}", lit, 1))
		if err == nil {
			t.Fatalf("%s: invalid UTF-8 source must not compile", name)
		}
		if !strings.Contains(err.Error(), "not valid UTF-8") {
			t.Errorf("%s: rejection must come from the UTF-8 check, got: %v", name, err)
		}
	}
	// A literal U+FFFD is valid UTF-8 and must stay compilable — the
	// check rejects invalid bytes, not the replacement rune itself.
	if _, err := CompileBundle(strings.Replace(base, "{LIT}", "\"A�B\"", 1)); err != nil {
		t.Errorf("literal U+FFFD must compile: %v", err)
	}
}

func TestCompileRejectsOversizedSource(t *testing.T) {
	huge := "crl v1\n# " + strings.Repeat("a", maxSourceBytes) + "\n"
	if _, err := Lex(huge); err == nil {
		t.Fatal("source over the size cap must be rejected")
	}
}
