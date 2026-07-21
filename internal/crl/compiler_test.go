package crl

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCompileLanguageProducesSemanticModelAndIR(t *testing.T) {
	source := `
crl v1

rule power_to_site {
	target utility.power
	collector utility_record utility file_upload from /bundles/power.json {
		signal power_built bool from power.built ttl 10y
		signal capacity_kw number from power.capacity_kw ttl 10y
	}
	need power_built == true
	need capacity_kw >= 2000
	quorum utility_record
}
`
	compilation, err := CompileLanguage(source)
	if err != nil {
		t.Fatalf("CompileLanguage: %v", err)
	}
	if compilation.SourceHash == "" {
		t.Fatal("expected source hash")
	}
	if len(compilation.Syntax.Tokens) == 0 || len(compilation.Syntax.Statements) == 0 {
		t.Fatal("expected lexical and syntax artifacts")
	}
	if got := len(compilation.Document.Rules); got != 1 {
		t.Fatalf("document rules = %d, want 1", got)
	}
	if got := compilation.Semantic.SignalKinds["capacity_kw"]; got != "number" {
		t.Fatalf("semantic signal kind = %q, want number", got)
	}
	if got := len(compilation.IR.Rules); got != 1 {
		t.Fatalf("ir rules = %d, want 1", got)
	}
	obligations := compilation.IR.Rules[0].Obligations
	if got := len(obligations); got != 3 {
		t.Fatalf("obligations = %d, want 3", got)
	}
	if obligations[0].Kind != ProofComparison || obligations[2].Kind != ProofQuorum {
		t.Fatalf("unexpected obligation kinds: %+v", obligations)
	}
	if obligations[2].Logic["calculus"] != "finite_boolean_algebra" {
		t.Fatalf("missing quorum logic metadata: %+v", obligations[2].Logic)
	}
	compiled, err := CompileBundle(source)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	if compilation.CanonicalText != compiled.CanonicalText || compilation.Hash != compiled.Hash {
		t.Fatalf("language compile mismatch:\ncanonical=%q\nbundle=%q\nhash=%q/%q", compilation.CanonicalText, compiled.CanonicalText, compilation.Hash, compiled.Hash)
	}
}

func TestCompileLanguageSupportsBundleStandardsAndTemporalPredicates(t *testing.T) {
	source := `
crl v1
package contrl.utility

bundle buildability.power {
	rule power_to_site {
		target utility.power
		collector utility_record utility file_upload from /bundles/power.json schema utility_power_v1 {
			signal capacity_kw number from power.capacity_kw unit kw ttl 10y
			signal confirmed_at time from power.confirmed_at ttl 10y
			signal construction_start time from project.construction_start optional ttl 30d
		}
		need capacity_kw >= 2000
		need confirmed_at within 10y before construction_start
		need confirmed_at age <= 10y
		quorum utility_record
	}
}
`
	compilation, err := CompileLanguage(source)
	if err != nil {
		t.Fatalf("CompileLanguage: %v", err)
	}
	if got := compilation.IR.Rules[0].Obligations[1].Kind; got != ProofTemporal {
		t.Fatalf("temporal obligation kind = %s, want %s", got, ProofTemporal)
	}
	compiled, err := CompileBundle(source)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	if compiled.Program.Package != "contrl.utility" || compiled.Program.Name != "buildability.power" {
		t.Fatalf("bundle metadata not normalized: %+v", compiled.Program)
	}
	collector := compiled.Program.Rules[0].Collectors[0]
	if collector.Schema != "utility_power_v1" {
		t.Fatalf("collector schema = %q", collector.Schema)
	}
	if got := collector.Signals[0].Unit; got != "kw" {
		t.Fatalf("signal unit = %q", got)
	}
	if !collector.Signals[2].Optional {
		t.Fatal("expected construction_start to be optional")
	}
	result := EvaluateBundleAt(compiled, Facts{
		"capacity_kw":                    2400,
		"confirmed_at":                   "2026-05-01T00:00:00Z",
		"construction_start":             time.Date(2030, 5, 1, 0, 0, 0, 0, time.UTC),
		"provider.utility_record":        true,
		"observed_at.capacity_kw":        "2026-05-01T00:00:00Z",
		"observed_at.confirmed_at":       "2026-05-01T00:00:00Z",
		"observed_at.construction_start": "2030-04-01T00:00:00Z",
	}, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if !result.Authorized {
		t.Fatalf("expected temporal rule to authorize, checks=%+v", result.Checks)
	}
	if got := compiled.CanonicalText; !containsAll(got, []string{
		"package contrl.utility",
		"bundle buildability.power",
		"collector utility_record utility file_upload from /bundles/power.json schema utility_power_v1",
		"signal capacity_kw number from power.capacity_kw unit kw ttl 10y",
		"signal construction_start time from project.construction_start optional ttl 30d",
		"need confirmed_at within 10y before construction_start",
		"need confirmed_at age <= 10y",
	}) {
		t.Fatalf("canonical text missing CRL object-model content:\n%s", got)
	}
}

func TestCompileLanguageRejectsTemporalTypeMismatch(t *testing.T) {
	_, err := CompileBundle(`
crl v1

rule bad_time_reference
	target utility.power
	collector utility_record utility file_upload from /bundles/power.json
		signal confirmed_at time from power.confirmed_at ttl 10y
		signal capacity_kw number from power.capacity_kw unit kw ttl 10y
	need confirmed_at before capacity_kw
	quorum utility_record
`)
	if !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("expected ErrTypeMismatch, got %v", err)
	}
}

func TestCompileLanguageExpandsAbstractConstructors(t *testing.T) {
	compiled, err := CompileBundle(`
crl v1

constructor current_evidence {
	collector registry_record registry webhook from /bundles/registry.json schema registry_evidence_v1 {
		signal record_current bool from record.current ttl 30d
	}
	need record_current == true
	quorum registry_record
}

abstract rule no_active_hold extends current_evidence {
	collector hold_record registry webhook from /bundles/holds.json {
		signal active_hold bool from hold.active ttl 30d
	}
	block active_hold
}

rule permit_ready extends no_active_hold {
	target permit.application
	collector application_file municipality file_upload from /bundles/application.json {
		signal application_complete bool from application.complete ttl 30d
	}
	need application_complete == true
	quorum application_file & registry_record
}
`)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	rule := compiled.Program.Rules[0]
	if got := len(rule.Collectors); got != 3 {
		t.Fatalf("collectors = %d, want inherited + concrete collectors", got)
	}
	if got := len(rule.Predicates); got != 5 {
		t.Fatalf("predicates = %d, want inherited + concrete predicates", got)
	}
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	result := EvaluateBundleAt(compiled, Facts{
		"record_current":                   true,
		"active_hold":                      false,
		"application_complete":             true,
		"provider.registry_record":         true,
		"provider.application_file":        true,
		"observed_at.record_current":       now.Add(-time.Hour),
		"observed_at.active_hold":          now.Add(-time.Hour),
		"observed_at.application_complete": now.Add(-time.Hour),
	}, now)
	if !result.Authorized {
		t.Fatalf("expected inherited rule to authorize, checks=%+v", result.Checks)
	}
	if strings.Contains(compiled.CanonicalText, "constructor") || strings.Contains(compiled.CanonicalText, "abstract") {
		t.Fatalf("canonical text should contain expanded concrete bundle only:\n%s", compiled.CanonicalText)
	}
}

func TestCompileBundleRejectsAmbiguousSymbols(t *testing.T) {
	_, err := CompileBundle(`
rule ambiguous
	target release
	collector inspection inspection webhook from inspection
		signal inspection bool from inspection.ok ttl 30d
	need inspection == true
	quorum inspection
`)
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Fatalf("expected ErrInvalidSyntax, got %v", err)
	}

	_, err = CompileBundleProgram(Bundle{Rules: []Rule{{
		Name:   "ambiguous",
		Target: "release",
		Collectors: []Collector{{
			Name:          "inspection",
			ProviderType:  "inspection",
			ConnectorKind: "webhook",
			Source:        "inspection",
			Signals:       []Signal{{Name: "inspection", Kind: "bool", SourceField: "inspection.ok", Expiry: SignalExpiry{Mode: "ttl", Literal: "30d"}}},
		}},
		Predicates: []Predicate{{Kind: PredicateNeed, Field: "inspection", Operator: OperatorEQ, Value: Value{Kind: "bool", Bool: true}}},
	}}})
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Fatalf("programmatic bundle should enforce semantic ambiguity, got %v", err)
	}
}

func TestCompileBundleRejectsUnreachableGlobalPolicyObjects(t *testing.T) {
	_, err := CompileBundle(`
crl v1

rule application {
	target permit.application
	collector application_file municipality file_upload from application
		signal application_complete bool from application.complete ttl 30d
	need application_complete == true
	quorum application_file
}

rule capital {
	target permit.capital
	collector capital_file finance file_upload from capital
		signal capital_committed bool from capital.committed ttl 30d
	need capital_committed == true
	quorum capital_file
}

need application == true
`)
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Fatalf("expected ErrInvalidSyntax, got %v", err)
	}

	_, err = CompileBundleProgram(Bundle{Rules: []Rule{
		{
			Name:   "application",
			Target: "permit.application",
			Collectors: []Collector{{
				Name:          "application_file",
				ProviderType:  "municipality",
				ConnectorKind: "file_upload",
				Source:        "application",
				Signals:       []Signal{{Name: "application_complete", Kind: "bool", SourceField: "application.complete", Expiry: SignalExpiry{Mode: "ttl", Literal: "30d"}}},
			}},
			Predicates: []Predicate{{Kind: PredicateNeed, Field: "application_complete", Operator: OperatorEQ, Value: Value{Kind: "bool", Bool: true}}},
		},
		{
			Name:   "capital",
			Target: "permit.capital",
			Collectors: []Collector{{
				Name:          "capital_file",
				ProviderType:  "finance",
				ConnectorKind: "file_upload",
				Source:        "capital",
				Signals:       []Signal{{Name: "capital_committed", Kind: "bool", SourceField: "capital.committed", Expiry: SignalExpiry{Mode: "ttl", Literal: "30d"}}},
			}},
			Predicates: []Predicate{{Kind: PredicateNeed, Field: "capital_committed", Operator: OperatorEQ, Value: Value{Kind: "bool", Bool: true}}},
		},
	}, Predicates: []Predicate{{Kind: PredicateNeed, Field: "application", Operator: OperatorEQ, Value: Value{Kind: "bool", Bool: true}}}})
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Fatalf("programmatic bundle should enforce final-policy reachability, got %v", err)
	}
}

func TestCountQuorumCanCountRulesAndClustersAtBundleScope(t *testing.T) {
	compiled, err := CompileBundle(`
crl v1

rule application
	target permit.application
	collector application_file municipality file_upload from application
		signal application_complete bool from application.complete ttl 30d
	need application_complete == true
	quorum application_file

rule capital
	target permit.capital
	collector capital_file finance file_upload from capital
		signal capital_committed bool from capital.committed ttl 30d
	need capital_committed == true
	quorum capital_file

cluster permit_foundation
	rules application + capital
	quorum application & capital

quorum count(application, permit_foundation) >= 2
`)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	result := EvaluateBundleAt(compiled, Facts{
		"application_complete":             true,
		"capital_committed":                true,
		"provider.application_file":        true,
		"provider.capital_file":            true,
		"observed_at.application_complete": now.Add(-time.Hour),
		"observed_at.capital_committed":    now.Add(-time.Hour),
	}, now)
	if !result.Authorized {
		t.Fatalf("expected authorization, checks=%+v", result.Checks)
	}
	if got := result.GlobalChecks[0].Actual; got != 2 {
		t.Fatalf("global count actual = %v, want 2", got)
	}
}
