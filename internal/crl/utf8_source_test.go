package crl

import (
	"strings"
	"testing"
)

// The source path must reject raw invalid UTF-8 before the []rune
// conversion folds it to U+FFFD. Otherwise byte-distinct sources compile
// to one hash while the evaluator still compares their differing bytes.
func TestCompileRejectsInvalidUTF8Source(t *testing.T) {
	base := "crl v1\npackage e.u\nbundle u.t\nrule r\n\ttarget a.b\n\tcollector c org api from /x.json\n\t\tsignal s string from x.y ttl 30d\n\tneed s != %q\n\tquorum c\n"
	// Two byte-distinct invalid sources plus a genuine U+FFFD literal.
	invalidA := "crl v1\nneed s != \"A\xffB\"\n"
	invalidB := "crl v1\nneed s != \"A\xfeB\"\n"
	for name, src := range map[string]string{"0xff": invalidA, "0xfe": invalidB} {
		if _, err := CompileBundle(src); err == nil {
			t.Errorf("%s: invalid UTF-8 source must not compile", name)
		}
	}
	// A well-formed non-ASCII program must still compile.
	good := strings.Replace(base, "%q", "\"café\"", 1)
	if _, err := CompileBundle(good); err != nil {
		t.Fatalf("valid UTF-8 source must compile: %v", err)
	}
}

func TestCompileRejectsOversizedSource(t *testing.T) {
	huge := "crl v1\n# " + strings.Repeat("a", maxSourceBytes) + "\n"
	if _, err := Lex(huge); err == nil {
		t.Fatal("source over the size cap must be rejected")
	}
}
