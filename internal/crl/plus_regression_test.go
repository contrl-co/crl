package crl

import "testing"

// Unspaced `+` between subjects must parse the same as spaced `+` in both
// count quorum and cluster rules, and must compile to an identical hash.
// The RFC3339-offset fix removed `+` from the token delimiters, so these
// forms regressed to compile errors; splitPlusFields restores them.
func TestUnspacedPlusInCountQuorum(t *testing.T) {
	spaced := "crl v1\npackage e.p\nbundle e.p\nrule r\n\ttarget a.b\n\tcollector ca org api from /x.json\n\t\tsignal s1 bool from x.y ttl 30d\n\tcollector cb org api from /y.json\n\t\tsignal s2 bool from y.z ttl 30d\n\tneed s1 == true\n\tquorum ca + cb >= 2\n"
	unspaced := "crl v1\npackage e.p\nbundle e.p\nrule r\n\ttarget a.b\n\tcollector ca org api from /x.json\n\t\tsignal s1 bool from x.y ttl 30d\n\tcollector cb org api from /y.json\n\t\tsignal s2 bool from y.z ttl 30d\n\tneed s1 == true\n\tquorum ca+cb >= 2\n"
	a, err := CompileBundle(spaced)
	if err != nil {
		t.Fatalf("spaced form must compile: %v", err)
	}
	b, err := CompileBundle(unspaced)
	if err != nil {
		t.Fatalf("unspaced form must compile: %v", err)
	}
	if a.Hash != b.Hash {
		t.Fatalf("spaced and unspaced + must hash identically: %s vs %s", a.Hash, b.Hash)
	}
}

func TestUnspacedPlusInClusterRules(t *testing.T) {
	src := func(sep string) string {
		return "crl v1\npackage e.p\nbundle e.p\nrule ra\n\ttarget a.b\n\tcollector ca org api from /x.json\n\t\tsignal s1 bool from x.y ttl 30d\n\tneed s1 == true\n\tquorum ca\nrule rb\n\ttarget a.c\n\tcollector cb org api from /y.json\n\t\tsignal s2 bool from y.z ttl 30d\n\tneed s2 == true\n\tquorum cb\ncluster cl\n\trules ra" + sep + "rb\n\tquorum ra\n"
	}
	a, err := CompileBundle(src(" + "))
	if err != nil {
		t.Fatalf("spaced cluster rules must compile: %v", err)
	}
	b, err := CompileBundle(src("+"))
	if err != nil {
		t.Fatalf("unspaced cluster rules must compile: %v", err)
	}
	if a.Hash != b.Hash {
		t.Fatalf("spaced and unspaced cluster + must hash identically")
	}
}

func TestRFC3339PositiveOffsetStaysWhole(t *testing.T) {
	src := "crl v1\npackage e.t\nbundle e.t\nrule r\n\ttarget a.b\n\tcollector c org api from /x.json\n\t\tsignal t time from x.t ttl 30d\n\tneed t after 2026-01-01T00:00:00+05:30\n\tquorum c\n"
	if _, err := CompileBundle(src); err != nil {
		t.Fatalf("RFC3339 positive-offset timestamp must compile: %v", err)
	}
}
