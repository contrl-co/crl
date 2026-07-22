package crllint

import "testing"

// CRL210 must fire only on the mixed-indentation shape that hides a
// possible global-policy intent: an indented rule body with a predicate
// dedented to column 0. It must stay silent in object-block form (braces
// scope the predicate) and in a fully flat rule (no ambiguity).
func TestCRL210OnlyMixedIndentation(t *testing.T) {
	objectBlock := "crl v1\npackage t.w\nbundle t.w {\nrule r {\ntarget t.r\ncollector c1 municipality api from /bundles/x.json {\nsignal s1 bool from a.one ttl 30d\n}\nneed s1 == true\n}\n}\n"
	if has(codes(objectBlock), "CRL210") {
		t.Error("CRL210 must not fire in object-block form")
	}
	flat := "crl v1\npackage t.o\nbundle t.o\nrule r\ntarget t.r\ncollector c1 municipality api from /bundles/x.json\nsignal s1 bool from a.one ttl 30d\nneed s1 == true\nquorum c1\n"
	if has(codes(flat), "CRL210") {
		t.Error("CRL210 must not fire on a fully flat rule")
	}
	mixed := "crl v1\npackage t.m\nbundle t.m\nrule ra\n\ttarget t.r\n\tcollector c1 municipality api from /bundles/x.json\n\t\tsignal s1 bool from a.one ttl 30d\n\tneed s1 == true\n\tquorum c1\nrule rb\n\ttarget t.s\n\tcollector c2 municipality api from /bundles/y.json\n\t\tsignal s2 bool from b.one ttl 30d\n\tneed s2 == true\n\tquorum c2\nneed ra == true\n"
	if !has(codes(mixed), "CRL210") {
		t.Error("CRL210 must fire when an indented rule body has a dedented predicate")
	}
	// A bundle-level brace does not scope the predicate to the rule: an
	// indentation-form rule inside `bundle x {` still has the ambiguity.
	bracedBundle := "crl v1\npackage t.bb\nbundle t.bb {\nrule ra\n\ttarget t.r\n\tcollector c1 municipality api from /bundles/x.json\n\t\tsignal s1 bool from a.one ttl 30d\n\tquorum c1\nneed s1 == true\n}\n"
	if !has(codes(bracedBundle), "CRL210") {
		t.Error("CRL210 must fire for an indented rule with a dedented predicate inside a braced bundle")
	}
}

// CRL209 flags a genuinely removable unreferenced signal, but not a
// presence-referenced collector's sole (structurally required) signal.
func TestCRL209StructurallyRequiredSignal(t *testing.T) {
	multi := "crl v1\npackage t.c\nbundle t.c\nrule r\n\ttarget t.r\n\tcollector c1 municipality api from /bundles/x.json\n\t\tsignal s1 bool from a.one ttl 30d\n\t\tsignal s2 bool from a.two ttl 30d\n\tneed s1 == true\n\tquorum c1\n"
	if !has(codes(multi), "CRL209") {
		t.Error("CRL209 must flag a removable unreferenced signal on a multi-signal collector")
	}
	single := "crl v1\npackage t.d\nbundle t.d\nrule r\n\ttarget t.r\n\tcollector c1 municipality api from /bundles/x.json\n\t\tsignal s1 bool from a.one ttl 30d\n\tcollector c2 municipality api from /bundles/y.json\n\t\tsignal s3 bool from b.one ttl 30d\n\tneed s1 == true\n\tquorum c1 + c2 >= 1\n"
	if has(codes(single), "CRL209") {
		t.Error("CRL209 must not flag a presence-referenced collector's sole required signal")
	}
}
