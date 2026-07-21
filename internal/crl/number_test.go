package crl

import "testing"

// An integer literal past the exact float64 range must be rejected, not
// silently rounded to a different threshold.
func TestLargeIntegerRejected(t *testing.T) {
	src := func(v string) string {
		return "crl v1\nrule r\n\ttarget t.a\n\tcollector c org api from /x.json\n\t\tsignal s number from x ttl 30d\n\tneed s == " + v + "\n\tquorum c\n"
	}
	if _, err := CompileBundle(src("9007199254740993")); err == nil {
		t.Error("integer above 2^53 must be rejected")
	}
	if _, err := CompileBundle(src("9007199254740992")); err != nil {
		t.Errorf("2^53 must compile: %v", err)
	}
	for _, ok := range []string{"250000", "0", "-5", "3.14", "0.92"} {
		if _, err := CompileBundle(src(ok)); err != nil {
			t.Errorf("%s must compile: %v", ok, err)
		}
	}
}

// -0 and 0 share one canonical form and hash.
func TestNegativeZeroNormalizes(t *testing.T) {
	src := func(v string) string {
		return "crl v1\nrule r\n\ttarget t.a\n\tcollector c org api from /x.json\n\t\tsignal s number from x ttl 30d\n\tneed s == " + v + "\n\tquorum c\n"
	}
	zero, err := CompileBundle(src("0"))
	if err != nil {
		t.Fatal(err)
	}
	negzero, err := CompileBundle(src("-0"))
	if err != nil {
		t.Fatal(err)
	}
	if zero.Hash != negzero.Hash || zero.CanonicalText != negzero.CanonicalText {
		t.Fatal("-0 must canonicalize identically to 0")
	}
}

// Exact-representability, not a magnitude threshold: a large round value
// that float64 represents exactly (10^18 = one ETH in wei) must compile
// and its canonical text must recompile; a value that would round must be
// rejected.
func TestExactLargeIntegersCompileAndRoundTrip(t *testing.T) {
	src := func(v string) string {
		return "crl v1\npackage n\nbundle n\nrule r\n\ttarget t.r\n\tcollector c org api from /x.json\n\t\tsignal s number from x ttl 30d\n\tneed s >= " + v + "\n\tquorum c\n"
	}
	for _, exact := range []string{"1000000000000000000", "10000000000000000000", "1e18", "9007199254740992"} {
		compiled, err := CompileBundle(src(exact))
		if err != nil {
			t.Fatalf("exact value %q must compile: %v", exact, err)
		}
		re, err := CompileBundle(compiled.CanonicalText)
		if err != nil {
			t.Fatalf("exact value %q canonical text must recompile: %v", exact, err)
		}
		if re.Hash != compiled.Hash {
			t.Errorf("exact value %q must round-trip", exact)
		}
	}
	for _, inexact := range []string{"9007199254740993", "100000000000000000000000"} {
		if _, err := CompileBundle(src(inexact)); err == nil {
			t.Errorf("inexact value %q must be rejected", inexact)
		}
	}
}

// Exact whole-number values must render to their exact integer digits so
// the canonical text recompiles; an exponent literal denoting an inexact
// integer must be rejected too.
func TestExactIntegerRenderRoundTrips(t *testing.T) {
	src := func(v string) string {
		return "crl v1\npackage n\nbundle n\nrule r\n\ttarget t.r\n\tcollector c org api from /x.json\n\t\tsignal s number from x ttl 30d\n\tneed s >= " + v + "\n\tquorum c\n"
	}
	for _, exact := range []string{"36028797018963968", "20000000000000008", "18014398509481984", "1e18", "1.5e3"} {
		compiled, err := CompileBundle(src(exact))
		if err != nil {
			t.Fatalf("exact %q must compile: %v", exact, err)
		}
		re, err := CompileBundle(compiled.CanonicalText)
		if err != nil {
			t.Fatalf("exact %q canonical text must recompile: %v", exact, err)
		}
		if re.Hash != compiled.Hash {
			t.Errorf("exact %q must round-trip", exact)
		}
	}
	for _, inexact := range []string{"1e23", "9007199254740993", "36028797018963970"} {
		if _, err := CompileBundle(src(inexact)); err == nil {
			t.Errorf("inexact %q must be rejected", inexact)
		}
	}
}
