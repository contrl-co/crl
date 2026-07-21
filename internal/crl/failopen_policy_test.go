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
		} else if !strings.Contains(err.Error(), "no evidence") && !strings.Contains(err.Error(), "require something") {
			t.Errorf("policy %q: want an evidence-required error, got %v", policy, err)
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
