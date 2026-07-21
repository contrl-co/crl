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

// json.Marshal folds invalid UTF-8 to U+FFFD inside the hash while the
// evaluator keeps the raw bytes, so a raw 0xff literal and a real U+FFFD
// literal shared a hash and split the verdict. The raw form must not compile.
func TestInvalidUTF8LiteralIsRejected(t *testing.T) {
	build := func(literal string) (CompiledBundle, error) {
		return CompileBundleProgram(Bundle{
			Version: "crl/v1", Package: "t", Name: "t.f",
			Rules: []Rule{{
				Name: "r", Target: "t.x",
				Collectors: []Collector{{
					Name: "c", ProviderType: "municipality", ConnectorKind: "file_upload",
					Source: "/f.json",
					Signals: []Signal{{
						Name: "region", Kind: "string", SourceField: "f.region",
						Expiry: SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 2592000},
					}},
				}},
				Predicates: []Predicate{{Kind: "need", Field: "region", Operator: "==",
					Value: Value{Kind: "string", String: literal}}},
			}},
		})
	}
	if _, err := build("\xff"); err == nil {
		t.Fatal("a raw invalid-UTF-8 literal compiled; the U+FFFD collision is open")
	}
	if _, err := build("�"); err != nil {
		t.Fatalf("a real U+FFFD literal must still compile: %v", err)
	}
}

// A collector source is free text on the hash path, same risk.
func TestInvalidUTF8CollectorSourceIsRejected(t *testing.T) {
	_, err := CompileBundleProgram(Bundle{
		Version: "crl/v1", Package: "t", Name: "t.f",
		Rules: []Rule{{
			Name: "r", Target: "t.x",
			Collectors: []Collector{{
				Name: "c", ProviderType: "municipality", ConnectorKind: "file_upload",
				Source: "/f\xff.json",
				Signals: []Signal{{
					Name: "ok", Kind: "bool", SourceField: "f.ok",
					Expiry: SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 2592000},
				}},
			}},
			Predicates: []Predicate{{Kind: "need", Field: "ok", Operator: "==",
				Value: Value{Kind: "bool", Bool: true}}},
		}},
	})
	if err == nil {
		t.Fatal("a raw invalid-UTF-8 collector source compiled")
	}
}

// A non-string Kind must not carry a Value.String into the hash unchecked;
// the struct API could otherwise smuggle invalid UTF-8 past the guard.
func TestInvalidUTF8NonStringValueIsRejected(t *testing.T) {
	_, err := CompileBundleProgram(Bundle{
		Version: "crl/v1", Package: "t", Name: "t.p",
		Rules: []Rule{{
			Name: "r", Target: "t.x",
			Collectors: []Collector{{
				Name: "c", ProviderType: "m", ConnectorKind: "file_upload", Source: "/f",
				Signals: []Signal{{
					Name: "ok", Kind: "bool", SourceField: "f.ok",
					Expiry: SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 2592000},
				}},
			}},
			Predicates: []Predicate{{Kind: "need", Field: "ok", Operator: "==",
				Value: Value{Kind: "bool", String: "\xff"}}},
		}},
	})
	if err == nil {
		t.Fatal("invalid UTF-8 in a non-string Value.String compiled; hash-smuggling open")
	}
}

// The quorum-count branch never called normalizeValue, so a struct-API
// Value.String on a count predicate reached the hash unchecked and two
// byte-different strings folded to one U+FFFD hash. The count carries only
// its number now, so the String is dropped and cannot smuggle bytes.
func TestQuorumCountValueStringIsDropped(t *testing.T) {
	build := func(s string) (CompiledBundle, error) {
		return CompileBundleProgram(Bundle{
			Version: "crl/v1", Package: "t", Name: "t.q",
			Rules: []Rule{{
				Name: "r", Target: "t.x",
				Collectors: []Collector{{
					Name: "c", ProviderType: "m", ConnectorKind: "file_upload", Source: "/f",
					Signals: []Signal{{
						Name: "a", Kind: "bool", SourceField: "f.a",
						Expiry: SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 2592000},
					}},
				}},
				Predicates: []Predicate{
					{Kind: "need", Field: "a", Operator: "==", Value: Value{Kind: "bool", Bool: true}},
					{Kind: "quorum", Operator: ">=", Value: Value{Kind: "number", Number: 1, String: s}, Providers: []string{"c"}},
				},
			}},
		})
	}
	x, err := build("\xff")
	if err != nil {
		t.Fatalf("compile x: %v", err)
	}
	y, err := build("\xfe")
	if err != nil {
		t.Fatalf("compile y: %v", err)
	}
	if x.Program.Rules[0].Predicates[1].Value.String != "" {
		t.Fatal("quorum count Value.String not dropped; it can still smuggle bytes into the hash")
	}
	if x.Hash != y.Hash {
		t.Fatalf("count programs differing only in a dropped String must share a hash: %s vs %s", x.Hash, y.Hash)
	}
}

// A predicate must carry only the fields its kind uses; a stray carrier
// field (Temporal/Providers/Expression on the wrong kind) reaches the hash
// and folds invalid UTF-8 to one U+FFFD collision via the struct API.
func TestPredicateStrayCarrierFieldsScrubbed(t *testing.T) {
	build := func(mut func(*Predicate)) (CompiledBundle, error) {
		p := Predicate{Kind: "need", Field: "a", Operator: "==", Value: Value{Kind: "bool", Bool: true}}
		mut(&p)
		return CompileBundleProgram(Bundle{Version: "crl/v1", Package: "t", Name: "t.q",
			Rules: []Rule{{Name: "r", Target: "t.x",
				Collectors: []Collector{{Name: "c", ProviderType: "m", ConnectorKind: "file_upload", Source: "/f",
					Signals: []Signal{{Name: "a", Kind: "bool", SourceField: "f.a", Expiry: SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 2592000}}}}},
				Predicates: []Predicate{p}}}})
	}
	cases := map[string][2]func(*Predicate){
		"stray Providers":  {func(p *Predicate) { p.Providers = []string{"\xff"} }, func(p *Predicate) { p.Providers = []string{"\xfe"} }},
		"stray Expression": {func(p *Predicate) { p.Expression = &QuorumExpression{Kind: "subject", Name: "\xff"} }, func(p *Predicate) { p.Expression = &QuorumExpression{Kind: "subject", Name: "\xfe"} }},
		"stray Temporal":   {func(p *Predicate) { p.Temporal = &TemporalExpression{Relation: "before", Reference: "\xff"} }, func(p *Predicate) { p.Temporal = &TemporalExpression{Relation: "before", Reference: "\xfe"} }},
	}
	for name, muts := range cases {
		x, e1 := build(muts[0])
		y, e2 := build(muts[1])
		if e1 != nil || e2 != nil {
			t.Fatalf("%s: compile %v %v", name, e1, e2)
		}
		p := x.Program.Rules[0].Predicates[0]
		if p.Providers != nil || p.Expression != nil || p.Temporal != nil {
			t.Errorf("%s: carrier field not scrubbed from need predicate", name)
		}
		if x.Hash != y.Hash {
			t.Errorf("%s: scrubbed programs must share a hash, got %s vs %s", name, x.Hash, y.Hash)
		}
	}
}
