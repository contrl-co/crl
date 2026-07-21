package crl

import "testing"

// Associative quorum groupings must all normalize to one canonical tree
// so the emitted canonical text recompiles to the same hash.
func TestQuorumAssociativityRoundTrips(t *testing.T) {
	head := "crl v1\npackage t.a\nbundle t.a\nrule r\n\ttarget t.b\n\tcollector c p k from /x.json\n\t\tsignal a bool from fa ttl 1h\n\t\tsignal b bool from fb ttl 1h\n\t\tsignal d bool from fd ttl 1h\n\tneed a == true\n\tquorum "
	for _, expr := range []string{"a & (b & d)", "(a & b) & d", "a & b & d", "a | (b | d)", "d & b & a", "a & (b | d)", "!(a & b)"} {
		compiled, err := CompileBundle(head + expr + "\n")
		if err != nil {
			t.Fatalf("%q: compile: %v", expr, err)
		}
		re, err := CompileBundle(compiled.CanonicalText)
		if err != nil {
			t.Fatalf("%q: recompile canonical text: %v", expr, err)
		}
		if re.Hash != compiled.Hash {
			t.Errorf("%q: canonical text does not round-trip to the same hash", expr)
		}
	}
}

// Equivalent groupings must hash identically.
func TestQuorumAssociativityCanonical(t *testing.T) {
	head := "crl v1\npackage t.a\nbundle t.a\nrule r\n\ttarget t.b\n\tcollector c p k from /x.json\n\t\tsignal a bool from fa ttl 1h\n\t\tsignal b bool from fb ttl 1h\n\t\tsignal d bool from fd ttl 1h\n\tneed a == true\n\tquorum "
	want, _ := CompileBundle(head + "a & b & d\n")
	for _, expr := range []string{"a & (b & d)", "(a & b) & d", "d & a & b", "b & d & a"} {
		got, err := CompileBundle(head + expr + "\n")
		if err != nil {
			t.Fatalf("%q: %v", expr, err)
		}
		if got.Hash != want.Hash {
			t.Errorf("%q must hash identically to the flat form", expr)
		}
	}
}

func TestBundleQuorumBudget(t *testing.T) {
	var b []string
	b = append(b, "crl v1\nrule r\n\ttarget a.b\n\tcollector c org api from /x.json\n\t\tsignal s bool from f ttl 1h")
	chain := "\tquorum " + repeatJoin("s", " & ", 256)
	for i := 0; i < 8000; i++ {
		b = append(b, chain)
	}
	b = append(b, "\tneed s == true\n")
	if _, err := CompileBundle(joinLines(b)); err == nil {
		t.Fatal("a bundle packed with quorum expressions past the budget must be rejected")
	}
}

func repeatJoin(s, sep string, n int) string {
	out := s
	for i := 1; i < n; i++ {
		out += sep + s
	}
	return out
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
