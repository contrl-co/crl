package crl

import (
	"strings"
	"testing"
	"time"
)

const testSource = `crl v1
package examples.permits
bundle permit.application

rule permit_application
	target permit.application
	collector application_file municipality file_upload from /bundles/application.json
		signal application_complete bool from application.complete ttl 30d
		signal permit_hold_active bool from permit.hold_active ttl 30d
	need application_complete == true
	block permit_hold_active
	quorum application_file
`

func testFacts(now time.Time) Facts {
	observed := now.Format(time.RFC3339)
	return Facts{
		"application_complete":             true,
		"permit_hold_active":               false,
		"application_file":                 true,
		"observed_at.application_complete": observed,
		"observed_at.permit_hold_active":   observed,
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	first, err := Compile(testSource)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := Compile("# a comment changes nothing\n" + testSource)
	if err != nil {
		t.Fatalf("compile with comment: %v", err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("hash changed under a comment: %s vs %s", first.Hash, second.Hash)
	}
	if first.CanonicalText != second.CanonicalText {
		t.Fatalf("canonical text changed under a comment")
	}
	recompiled, err := Compile(first.CanonicalText)
	if err != nil {
		t.Fatalf("canonical text does not recompile: %v", err)
	}
	if recompiled.Hash != first.Hash {
		t.Fatalf("canonical round-trip changed hash: %s vs %s", recompiled.Hash, first.Hash)
	}
}

func TestCompileEditionRejectsUnknown(t *testing.T) {
	if _, err := CompileEdition(testSource, "v2"); err == nil {
		t.Fatal("expected unknown-edition error for v2")
	}
	if _, err := CompileEdition(testSource, EditionV1); err != nil {
		t.Fatalf("v1 must compile: %v", err)
	}
}

func TestEvaluateAtAuthorizes(t *testing.T) {
	compiled, err := Compile(testSource)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	evaluation := compiled.EvaluateAt(testFacts(now), now)
	if evaluation.Result != Authorized || !evaluation.Authorized {
		t.Fatalf("want AUTHORIZED, got %s", evaluation.Result)
	}
}

func TestEvaluateWithoutClockFailsClosed(t *testing.T) {
	compiled, err := Compile(testSource)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	evaluation := compiled.Evaluate(testFacts(now))
	if evaluation.Result != Expired {
		t.Fatalf("zero-clock evaluation must be EXPIRED, got %s", evaluation.Result)
	}
}

func TestFiveOutcomes(t *testing.T) {
	compiled, err := Compile(testSource)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	denied := testFacts(now)
	denied["application_complete"] = false
	if got := compiled.EvaluateAt(denied, now).Result; got != Denied {
		t.Fatalf("want DENIED, got %s", got)
	}

	blocked := testFacts(now)
	blocked["permit_hold_active"] = true
	if got := compiled.EvaluateAt(blocked, now).Result; got != Blocked {
		t.Fatalf("want BLOCKED, got %s", got)
	}

	missing := testFacts(now)
	delete(missing, "application_complete")
	delete(missing, "observed_at.application_complete")
	if got := compiled.EvaluateAt(missing, now).Result; got != InsufficientEvidence {
		t.Fatalf("want INSUFFICIENT_EVIDENCE, got %s", got)
	}

	stale := testFacts(now)
	stale["observed_at.application_complete"] = now.Add(-31 * 24 * time.Hour).Format(time.RFC3339)
	if got := compiled.EvaluateAt(stale, now).Result; got != Expired {
		t.Fatalf("want EXPIRED, got %s", got)
	}
}

func TestLintReportsDiagnostics(t *testing.T) {
	report := Lint("good.crl", testSource)
	if !report.OK {
		t.Fatalf("clean source must lint OK: %+v", report.Diagnostics)
	}
	if report.Hash == "" {
		t.Fatal("lint of clean source must include the compiled hash")
	}

	bad := Lint("bad.crl", "rule broken\n\tneed x == true\n")
	if bad.OK {
		t.Fatal("broken source must not lint OK")
	}
	if len(bad.Diagnostics) == 0 {
		t.Fatal("broken source must produce diagnostics")
	}
	if !strings.HasPrefix(bad.Diagnostics[0].Code, "CRL") {
		t.Fatalf("diagnostics must carry CRL codes, got %q", bad.Diagnostics[0].Code)
	}
}

func TestFormatReturnsCanonicalText(t *testing.T) {
	formatted, err := Format(testSource)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if !strings.HasPrefix(formatted, "crl v1\n") {
		t.Fatalf("canonical text must start with the version header, got %q", formatted[:20])
	}
	again, err := Format(formatted)
	if err != nil {
		t.Fatalf("format canonical text: %v", err)
	}
	if again != formatted {
		t.Fatal("Format must be a fixed point on canonical text")
	}
}

func TestGraphIsDeterministic(t *testing.T) {
	first, err := Graph(testSource)
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	second, err := Graph(testSource)
	if err != nil {
		t.Fatalf("graph again: %v", err)
	}
	if string(first.Graph) != string(second.Graph) || string(first.Layout) != string(second.Layout) {
		t.Fatal("graph output must be deterministic")
	}
	if first.Hash == "" {
		t.Fatal("graph result must carry the bundle hash")
	}
}
