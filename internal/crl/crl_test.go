package crl

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCompileEvaluatesTextualRule(t *testing.T) {
	compiled, err := CompileBundle(`
rule funding_release
target funding

collector inspection inspection webhook from tower.inspection
signal inspection_confidence number from confidence ttl 30d
signal progress_percent number from progress ttl 30d

collector credential_review credential file_upload from tower.credentials
signal credential_approved bool from approved ttl 30d
signal credential_reference string from reference ttl 30d
signal active_blocker bool from blocker ttl 30d

need inspection_confidence >= 0.9
need progress_percent >= 80
need credential_approved == true
need credential_reference == "CRD-001"
block active_blocker
quorum inspection & credential_review
`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.Hash == "" {
		t.Fatal("expected compiled hash")
	}

	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	result := EvaluateBundleAt(compiled, Facts{
		"inspection_confidence":             0.95,
		"progress_percent":                  90,
		"credential_approved":               true,
		"credential_reference":              "CRD-001",
		"active_blocker":                    false,
		"provider.inspection":               true,
		"provider.credential_review":        true,
		"observed_at.inspection_confidence": now.Add(-time.Hour),
		"observed_at.progress_percent":      now.Add(-time.Hour),
		"observed_at.credential_approved":   now.Add(-time.Hour),
		"observed_at.credential_reference":  now.Add(-time.Hour),
		"observed_at.active_blocker":        now.Add(-time.Hour),
	}, now)
	if !result.Authorized {
		t.Fatalf("expected authorization, checks=%+v", result.Checks)
	}
	if len(result.Checks) != 6 {
		t.Fatalf("checks length = %d, want 6", len(result.Checks))
	}
}

func TestEvaluateFailsClosedOnMissingFact(t *testing.T) {
	compiled, err := CompileBundle(`
rule progress
target release
collector field field webhook from field.progress
signal progress_percent number from percent ttl 7d
need progress_percent >= 80
`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	result := EvaluateBundle(compiled, Facts{})
	if result.Authorized {
		t.Fatal("expected missing fact to fail closed")
	}
	if got := result.Checks[0].Reason; got != ErrUnknownFact.Error() {
		t.Fatalf("reason = %q, want %q", got, ErrUnknownFact.Error())
	}
}

func TestCompileBundleProgramDeterministicHash(t *testing.T) {
	bundle := Bundle{Rules: []Rule{
		{
			Name:   "Release",
			Target: "FUNDING",
			Collectors: []Collector{
				{
					Name:          "field",
					ProviderType:  "inspection",
					ConnectorKind: "webhook",
					Source:        "field.progress",
					Signals: []Signal{
						{Name: "Progress_Percent", Kind: "number", SourceField: "percent", Expiry: SignalExpiry{Mode: "ttl", Literal: "7d"}},
					},
				},
			},
			Predicates: []Predicate{
				{Kind: PredicateNeed, Field: "Progress_Percent", Operator: OperatorGTE, Value: Value{Kind: "number", Number: 80}},
			},
		},
	}}
	first, err := CompileBundleProgram(bundle)
	if err != nil {
		t.Fatalf("CompileBundleProgram first: %v", err)
	}
	second, err := CompileBundleProgram(bundle)
	if err != nil {
		t.Fatalf("CompileBundleProgram second: %v", err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("hash mismatch: %q != %q", first.Hash, second.Hash)
	}
	if first.Program.Rules[0].Target != "funding" || first.Program.Rules[0].Predicates[0].Field != "progress_percent" {
		t.Fatalf("bundle was not normalized: %+v", first.Program)
	}
}

func TestCompileBundleProgramRejectsInvalidBundle(t *testing.T) {
	_, err := CompileBundleProgram(Bundle{Rules: []Rule{
		{
			Name:   "release",
			Target: "funding",
			Collectors: []Collector{
				{
					Name:          "field",
					ProviderType:  "inspection",
					ConnectorKind: "webhook",
					Source:        "field.progress",
					Signals: []Signal{
						{Name: "Progress_Percent", Kind: "number", SourceField: "percent", Expiry: SignalExpiry{Mode: "ttl", Literal: "7d"}},
					},
				},
			},
			Predicates: []Predicate{
				{Kind: PredicateNeed, Field: "missing_signal", Operator: OperatorGTE, Value: Value{Kind: "number", Number: 80}},
			},
		},
	}})
	if !errors.Is(err, ErrMissingSignal) {
		t.Fatalf("expected ErrMissingSignal, got %v", err)
	}
}

func TestCompileRejectsUnsupportedOperator(t *testing.T) {
	_, err := CompileBundle(`
rule progress
target release
need progress_percent =~ 80
`)
	if !errors.Is(err, ErrUnsupportedOp) {
		t.Fatalf("expected ErrUnsupportedOp, got %v", err)
	}
}

func TestCompileHashIgnoresWhitespaceAndComments(t *testing.T) {
	first, err := CompileBundle(`
rule release
target funding
collector inspection inspection webhook from tower.inspection
signal progress_percent number from progress ttl 30d
signal credential_expired bool from credential.expired ttl 30d
collector credential_review credential file_upload from tower.credentials
signal credential_ready bool from ready ttl 30d
need progress_percent >= 80
block credential_expired
quorum inspection & credential_review
`)
	if err != nil {
		t.Fatalf("Compile first: %v", err)
	}
	second, err := CompileBundle(`
# comment
rule   release

target   funding
collector inspection inspection webhook from tower.inspection
signal progress_percent number from progress ttl 30d
signal credential_expired bool from credential.expired ttl 30d
collector credential_review credential file_upload from tower.credentials
signal credential_ready bool from ready ttl 30d
need progress_percent >= 80 # trailing comment
block credential_expired
quorum credential_review & inspection
`)
	if err != nil {
		t.Fatalf("Compile second: %v", err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("hash mismatch: %q != %q", first.Hash, second.Hash)
	}
	if first.CanonicalText != second.CanonicalText {
		t.Fatalf("canonical mismatch:\n%s\n---\n%s", first.CanonicalText, second.CanonicalText)
	}
}

func TestCompileRejectsPredicateWithoutCollectorSignal(t *testing.T) {
	_, err := CompileBundle(`
rule release
target funding
collector inspection inspection webhook from tower.inspection
signal inspection_confidence number from confidence ttl 30d
need progress_percent >= 80
`)
	if !errors.Is(err, ErrMissingSignal) {
		t.Fatalf("expected ErrMissingSignal, got %v", err)
	}
}

func TestCompileAcceptsConnectorKindsAndShortDurations(t *testing.T) {
	compiled, err := CompileBundle(`
rule readiness
target release
collector upload inspection file_upload from tot.files
signal permit_current bool from permit.current ttl 45d
collector meter iot stream from sensors.station.7
signal flow_rate number from readings.flow expires 8h
need permit_current == true
need flow_rate > 10
quorum upload & meter
`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	upload := findCollector(t, compiled, "upload")
	meter := findCollector(t, compiled, "meter")
	if got := upload.ConnectorKind; got != "file_upload" {
		t.Fatalf("connector kind = %q", got)
	}
	if got := upload.Signals[0].Expiry.Seconds; got != 45*24*60*60 {
		t.Fatalf("ttl seconds = %d", got)
	}
	if meter.Signals[0].Expiry.Literal != "8h" {
		t.Fatalf("expires duration should canonicalize to ttl literal: %+v", meter.Signals[0].Expiry)
	}
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	result := EvaluateBundleAt(compiled, Facts{
		"permit_current":             true,
		"flow_rate":                  12.5,
		"provider.upload":            true,
		"provider.meter":             true,
		"observed_at.permit_current": now.Add(-time.Hour),
		"observed_at.flow_rate":      now.Add(-time.Hour),
	}, now)
	if !result.Authorized {
		t.Fatalf("expected symbolic quorum to authorize, checks=%+v", result.Checks)
	}
}

func TestCompileAcceptsCountQuorum(t *testing.T) {
	compiled, err := CompileBundle(`
rule readiness
target release
collector inspection inspection file_upload from inspection.upload
signal inspection_score number from inspection.score ttl 7d
collector permit_registry registry webhook from registry.permits
signal permit_current bool from permit.current ttl 30d
need inspection_score >= 90
need permit_current == true
quorum count(inspection, permit_registry) >= 2
`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	result := EvaluateBundleAt(compiled, Facts{
		"inspection_score":             95,
		"permit_current":               true,
		"provider.inspection":          true,
		"provider.permit_registry":     true,
		"observed_at.inspection_score": now.Add(-time.Hour),
		"observed_at.permit_current":   now.Add(-time.Hour),
	}, now)
	if !result.Authorized {
		t.Fatalf("expected count quorum to authorize, checks=%+v", result.Checks)
	}
	if got := compiled.CanonicalText; !strings.Contains(got, "quorum count(inspection, permit_registry) >= 2") {
		t.Fatalf("canonical text missing count quorum:\n%s", got)
	}
}

func TestCompileRejectsSignalTypeMismatch(t *testing.T) {
	_, err := CompileBundle(`
rule credential_gate
target release
collector credential registry webhook from registry.credentials
signal credential_current bool from credential.current ttl 30d
need credential_current >= true
`)
	if !errors.Is(err, ErrUnsupportedOp) {
		t.Fatalf("expected ErrUnsupportedOp, got %v", err)
	}

	_, err = CompileBundle(`
rule credential_gate
target release
collector credential registry webhook from registry.credentials
signal credential_current bool from credential.current ttl 30d
need credential_current == "yes"
`)
	if !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("expected ErrTypeMismatch, got %v", err)
	}
}

func TestEvaluateAtExpiresStaleSignal(t *testing.T) {
	compiled, err := CompileBundle(`
rule permit_gate
target release
collector permit registry webhook from registry.permits
signal permit_current bool from permit.current ttl 30d
need permit_current == true
`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	result := EvaluateBundleAt(compiled, Facts{
		"permit_current":             true,
		"observed_at.permit_current": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC))
	if result.Authorized {
		t.Fatal("expected stale signal to fail")
	}
	if got := result.Checks[0].Reason; got != ErrExpired.Error() {
		t.Fatalf("reason = %q, want %q", got, ErrExpired.Error())
	}
}

func TestCompileRejectsHugeDuration(t *testing.T) {
	_, err := CompileBundle(`
rule huge
target release
collector source registry webhook from source
signal measurement number from value ttl 999999999999999999y
need measurement > 1
`)
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Fatalf("expected ErrInvalidSyntax, got %v", err)
	}
}

func findCollector(t *testing.T, compiled CompiledBundle, name string) Collector {
	t.Helper()
	if len(compiled.Program.Rules) == 0 {
		t.Fatal("compiled bundle has no rules")
	}
	for _, collector := range compiled.Program.Rules[0].Collectors {
		if collector.Name == name {
			return collector
		}
	}
	t.Fatalf("collector %q not found", name)
	return Collector{}
}
