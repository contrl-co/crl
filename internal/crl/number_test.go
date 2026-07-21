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
