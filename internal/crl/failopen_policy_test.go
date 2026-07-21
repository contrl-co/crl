package crl

import (
	"strings"
	"testing"
)

// A global final policy must never authorize a bundle with no evidence.
// The inverting spellings (quorum not/!, need ==false / !=true, block-only)
// all gate on a rule FAILING, so empty facts would authorize — reject them.
func TestInvertingFinalPolicyRejected(t *testing.T) {
	rule := "\nrule r\n\ttarget t.a\n\tcollector c org api from /x.json\n\t\tsignal s bool from x ttl 30d\n\tneed s == true\n"
	for _, policy := range []string{"quorum not r", "quorum !r", "need r == false", "need r != true", "block r"} {
		src := "crl v1\npackage p\nbundle b\n" + policy + rule
		if _, err := CompileBundle(src); err == nil {
			t.Errorf("inverting policy %q must be rejected", policy)
		} else if !strings.Contains(err.Error(), "no evidence") &&
			!strings.Contains(err.Error(), "require something") &&
			!strings.Contains(err.Error(), "failing") {
			t.Errorf("policy %q: want an inverting-policy error, got %v", policy, err)
		}
	}
}

func TestPositiveFinalPolicyCompiles(t *testing.T) {
	for _, policy := range []string{"need r == true", "quorum r"} {
		src := "crl v1\npackage p\nbundle b\n" + policy + "\nrule r\n\ttarget t.a\n\tcollector c org api from /x.json\n\t\tsignal s bool from x ttl 30d\n\tneed s == true\n"
		if _, err := CompileBundle(src); err != nil {
			t.Errorf("positive policy %q must compile: %v", policy, err)
		}
	}
}

// A rule or cluster referenced only under negation while a positive
// conjunct is present still fails open at eval, so it must be rejected at
// compile even though the empty-facts point evaluates to false.
func TestConjoinedNegationRejected(t *testing.T) {
	two := "\nrule r\n\ttarget a.b\n\tcollector c org api from /x.json\n\t\tsignal s bool from x.y ttl 30d\n\tneed s == true\nrule r2\n\ttarget a.c\n\tcollector c2 org api from /y.json\n\t\tsignal s2 bool from y.z ttl 30d\n\tneed s2 == true\n"
	for _, policy := range []string{"quorum r & !r2", "quorum r & (r | !r2)", "need r == true\nblock r2", "need r == true\nquorum !r2"} {
		src := "crl v1\npackage p\nbundle b\n" + policy + two
		if _, err := CompileBundle(src); err == nil {
			t.Errorf("conjoined-negation policy %q must be rejected", policy)
		}
	}
}

// Negating a SIGNAL (not a rule/cluster) in the final policy is fine — an
// absent signal is unknown, not false, so it fails closed.
func TestSignalNegationAllowedInFinalPolicy(t *testing.T) {
	src := "crl v1\npackage p\nbundle b\nquorum r & !safety_hold\nrule r\n\ttarget a.b\n\tcollector c org api from /x.json\n\t\tsignal s bool from x.y ttl 30d\n\t\tsignal safety_hold bool from x.h ttl 30d\n\tneed s == true\n"
	if _, err := CompileBundle(src); err != nil {
		t.Fatalf("negating a signal in the final policy must compile: %v", err)
	}
}
