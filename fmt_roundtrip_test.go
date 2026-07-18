package crl

import "testing"

// A bundle with a global final policy and no cluster used to format to text
// that placed the global at column 0 after a rule body, where the rule-body
// carve-out absorbed it — so crlc fmt output did not re-compile, and crlc
// fmt -w silently corrupted the source. The canonical form must be a fixed
// point: format, re-compile, and get the same program and hash.
func TestFormatGlobalPolicyIsAFixedPoint(t *testing.T) {
	src := "crl v1\npackage p.q\nbundle b.c\nneed r1 == true\n\n" +
		"rule r1\n\ttarget t.x\n\tcollector c m file_upload from /f\n" +
		"\t\tsignal a bool from f.a ttl 30d\n\tneed a == true\n"

	original, err := Compile(src)
	if err != nil {
		t.Fatalf("compile source: %v", err)
	}

	formatted, err := Format(src)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	reCompiled, err := Compile(formatted)
	if err != nil {
		t.Fatalf("formatted text does not re-compile: %v\n---\n%s", err, formatted)
	}
	if reCompiled.Hash != original.Hash {
		t.Fatalf("format changed the program: %s -> %s", original.Hash, reCompiled.Hash)
	}

	// Idempotent: formatting the formatted text yields the same text.
	again, err := Format(formatted)
	if err != nil {
		t.Fatalf("re-format: %v", err)
	}
	if again != formatted {
		t.Fatalf("format is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", formatted, again)
	}
}
