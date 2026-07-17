package crl

import (
	"strings"
	"testing"
	"time"
)

// Escape sequences, never literal text: an editor that normalises this file
// would collapse the pair and pass vacuously. The guard below catches that.
const (
	forgeryRegionNFC = "caf\u00e9"
	forgeryRegionNFD = "cafe\u0301"
)

func forgerySource(operator, region string) string {
	return "crl v1\npackage t\nbundle t.forge\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c municipality file_upload from /f.json\n" +
		"\t\tsignal region string from f.region ttl 30d\n" +
		"\tneed region " + operator + " \"" + region + "\"\n"
}

func evalRegion(t *testing.T, operator, ruleRegion, factRegion string) (CompiledBundle, string) {
	t.Helper()
	compiled, err := CompileBundle(forgerySource(operator, ruleRegion))
	if err != nil {
		t.Fatalf("compile %s %q: %v", operator, ruleRegion, err)
	}
	now := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	facts := Facts{"region": factRegion, "observed_at.region": now.Format(time.RFC3339)}
	return compiled, EvaluateBundleAt(compiled, facts, now).Result
}

// One hash must mean one program, and both spellings must reach the verdict
// the evidence warrants -- asserting only that the two agree would pass with
// the normal form inverted.
func TestNFCVariantsDecideOnEvidenceNotSpelling(t *testing.T) {
	if forgeryRegionNFC == forgeryRegionNFD {
		t.Fatal("test inputs are byte-equal; this file was normalised and proves nothing")
	}

	for _, rule := range []string{forgeryRegionNFC, forgeryRegionNFD} {
		for _, fact := range []string{forgeryRegionNFC, forgeryRegionNFD} {
			if _, got := evalRegion(t, "==", rule, fact); got != "AUTHORIZED" {
				t.Errorf("== : the region matches, want AUTHORIZED, got %s", got)
			}
			if _, got := evalRegion(t, "!=", rule, fact); got != "DENIED" {
				t.Errorf("!= : the banned region is present, want DENIED, got %s", got)
			}
		}
	}

	precomposed, _ := evalRegion(t, "==", forgeryRegionNFC, forgeryRegionNFC)
	decomposed, _ := evalRegion(t, "==", forgeryRegionNFD, forgeryRegionNFC)
	if precomposed.CanonicalText != decomposed.CanonicalText {
		t.Errorf("NFC variants must canonicalise identically:\n  %q\n  %q",
			precomposed.CanonicalText, decomposed.CanonicalText)
	}
	if precomposed.Hash != decomposed.Hash {
		t.Errorf("NFC variants must share a hash: %s != %s", precomposed.Hash, decomposed.Hash)
	}
}

// A spelling that differs from the rule must not evade a deny-list.
func TestDenyListHoldsAcrossNormalizationForms(t *testing.T) {
	_, got := evalRegion(t, "!=", forgeryRegionNFC, forgeryRegionNFD)
	if got != "DENIED" {
		t.Errorf("deny-list evaded by the NFD spelling of the banned value: got %s", got)
	}
}

// Collector sources are free text on the hash path, same risk as a literal.
func TestNFCVariantsInCollectorSourceAreOneProgram(t *testing.T) {
	build := func(source string) string {
		return "crl v1\npackage t\nbundle t.src\n\nrule r\n\ttarget t.x\n" +
			"\tcollector c municipality file_upload from " + source + "\n" +
			"\t\tsignal ok bool from f.ok ttl 30d\n\tneed ok == true\n"
	}
	precomposed, err := CompileBundle(build("/bundles/caf\u00e9.json"))
	if err != nil {
		t.Fatalf("compile precomposed: %v", err)
	}
	decomposed, err := CompileBundle(build("/bundles/cafe\u0301.json"))
	if err != nil {
		t.Fatalf("compile decomposed: %v", err)
	}
	if precomposed.Hash != decomposed.Hash {
		t.Errorf("NFC variants of a collector source must share a hash: %s != %s",
			precomposed.Hash, decomposed.Hash)
	}
	if precomposed.CanonicalText != decomposed.CanonicalText {
		t.Error("NFC variants of a collector source must canonicalise identically")
	}
}

// NFC specifically, not merely a consistent fold: NFD would satisfy every
// behavioural test here -- both sides would still match -- while publishing a
// hash no other implementation of the spec reproduces.
func TestCanonicalFormFoldsToNFC(t *testing.T) {
	compiled, err := CompileBundle(forgerySource("==", forgeryRegionNFD))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(compiled.CanonicalText, forgeryRegionNFC) {
		t.Errorf("canonical text must carry the precomposed form, got %q", compiled.CanonicalText)
	}
	if strings.Contains(compiled.CanonicalText, forgeryRegionNFD) {
		t.Error("canonical text carries the decomposed form: the fold is NFD, not NFC")
	}
}
