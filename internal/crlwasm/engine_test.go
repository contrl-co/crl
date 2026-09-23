package crlwasm

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	crl "github.com/contrl-co/crl"
)

const permitSource = `crl v1
package examples.permits
bundle permit.quorum

rule permit_application
	target permit.application
	collector application_file municipality file_upload from /bundles/application.json
	signal application_complete bool from application.complete ttl 30d
	collector registry_check land_registry api from /bundles/registry.json
	signal permit_hold_active bool from permit.hold_active ttl 30d
	collector reviewer_attest reviewer attestation from /bundles/review.json
	signal reviewer_approved bool from review.approved ttl 30d
	need application_complete == true
	block permit_hold_active
	quorum 2 of 3 application_file registry_check reviewer_attest
`

// fixedEngine evaluates at a clock the test controls, so a response is a
// function of the request and not of when the suite runs.
func fixedEngine(t *testing.T) Engine {
	t.Helper()
	at, err := time.Parse(time.RFC3339, "2026-06-02T00:00:00Z")
	if err != nil {
		t.Fatalf("parse fixed clock: %v", err)
	}
	return Engine{Version: "test", Now: func() time.Time { return at }}
}

func decodeResponse(t *testing.T, body string, out any) {
	t.Helper()
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, body)
	}
	if envelope.Error != "" {
		t.Fatalf("unexpected error response: %s", envelope.Error)
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		t.Fatalf("decode response: %v\n%s", err, body)
	}
}

func errorOf(t *testing.T, body string) string {
	t.Helper()
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, body)
	}
	return envelope.Error
}

// TestFunctionsAreTheContract pins the global names and their order.
// A page looks these up by name after the module starts; renaming one
// silently breaks every consumer, so the list is the contract.
func TestFunctionsAreTheContract(t *testing.T) {
	want := []string{
		"contrlCompileCRL",
		"contrlFormatCRL",
		"contrlLintCRL",
		"contrlGraphCRL",
		"contrlEvaluateCRL",
		"contrlEngineInfo",
	}
	functions := Engine{}.Functions()
	if len(functions) != len(want) {
		t.Fatalf("got %d functions, want %d", len(functions), len(want))
	}
	for i, function := range functions {
		if function.Name != want[i] {
			t.Errorf("function %d is %q, want %q", i, function.Name, want[i])
		}
		if function.Call == nil {
			t.Errorf("function %q has no handler", function.Name)
		}
	}
}

// TestCompileReturnsTheBundleHash: pinning the hash is the reason a
// consumer compiles at all, so the compile response must carry it and
// it must equal what the library returns for the same source.
func TestCompileReturnsTheBundleHash(t *testing.T) {
	var got compileResponse
	decodeResponse(t, fixedEngine(t).Compile(`{"source":`+quote(permitSource)+`}`), &got)

	want, err := crl.Compile(permitSource)
	if err != nil {
		t.Fatalf("compile reference: %v", err)
	}
	if got.Hash != want.Hash {
		t.Errorf("hash = %q, want %q", got.Hash, want.Hash)
	}
	if got.SourceHash != want.SourceHash {
		t.Errorf("source_hash = %q, want %q", got.SourceHash, want.SourceHash)
	}
	if got.CanonicalText != want.CanonicalText {
		t.Errorf("canonical_text = %q, want %q", got.CanonicalText, want.CanonicalText)
	}
	if got.Edition != crl.EditionV1 {
		t.Errorf("edition = %q, want %q", got.Edition, crl.EditionV1)
	}
	if len(got.Program.Rules) != 1 || got.Program.Rules[0].Name != "permit_application" {
		t.Fatalf("program view does not describe the rule: %+v", got.Program)
	}
	if len(got.Program.Rules[0].Collectors) != 3 {
		t.Errorf("got %d collectors, want 3", len(got.Program.Rules[0].Collectors))
	}
}

// TestCompileRejectsAnUnknownEdition: an edition is a frozen
// compilation contract, so a request naming one this build does not
// implement must fail rather than silently compile under v1.
func TestCompileRejectsAnUnknownEdition(t *testing.T) {
	message := errorOf(t, fixedEngine(t).Compile(`{"source":`+quote(permitSource)+`,"edition":"v2"}`))
	if !strings.Contains(message, "unknown edition") {
		t.Errorf("error = %q, want it to name the unknown edition", message)
	}
}

// TestEvaluateAuthorizesAtAnExplicitClock: the request's clock decides
// freshness, and the response must say which instant it used.
func TestEvaluateAuthorizesAtAnExplicitClock(t *testing.T) {
	request := `{"source":` + quote(permitSource) + `,"now":"2026-06-02T00:00:00Z","facts":{
		"application_complete": true,
		"permit_hold_active": false,
		"application_file": true,
		"registry_check": true,
		"reviewer_attest": false,
		"observed_at.application_complete": "2026-06-01T09:00:00Z",
		"observed_at.permit_hold_active": "2026-06-01T09:00:00Z"
	}}`
	var got evaluateResponse
	decodeResponse(t, fixedEngine(t).Evaluate(request), &got)

	if got.Result != crl.Authorized {
		t.Errorf("result = %q, want %q", got.Result, crl.Authorized)
	}
	if !got.Authorized {
		t.Error("authorized = false, want true")
	}
	if got.EvaluatedAt != "2026-06-02T00:00:00Z" {
		t.Errorf("evaluated_at = %q, want the request clock", got.EvaluatedAt)
	}
	if got.Hash == "" {
		t.Error("evaluate response carries no bundle hash")
	}
	if len(got.Checks) == 0 {
		t.Error("evaluate response carries no checks; the trace is the explanation")
	}
}

// TestEvaluateExpiresWhenEvidenceIsStale: the same facts a year later
// must not authorize. Fail-closed freshness is the property most worth
// pinning at the browser boundary, where the clock is the visitor's.
func TestEvaluateExpiresWhenEvidenceIsStale(t *testing.T) {
	request := `{"source":` + quote(permitSource) + `,"now":"2027-06-02T00:00:00Z","facts":{
		"application_complete": true,
		"permit_hold_active": false,
		"application_file": true,
		"registry_check": true,
		"reviewer_attest": false,
		"observed_at.application_complete": "2026-06-01T09:00:00Z",
		"observed_at.permit_hold_active": "2026-06-01T09:00:00Z"
	}}`
	var got evaluateResponse
	decodeResponse(t, fixedEngine(t).Evaluate(request), &got)

	if got.Result != crl.Expired {
		t.Errorf("result = %q, want %q", got.Result, crl.Expired)
	}
	if got.Authorized {
		t.Error("authorized = true on stale evidence")
	}
}

// TestEvaluateFallsBackToTheHostClock: a request without a clock still
// evaluates, and the response reports the instant that was used so the
// answer is not a function of a hidden input.
func TestEvaluateFallsBackToTheHostClock(t *testing.T) {
	var got evaluateResponse
	decodeResponse(t, fixedEngine(t).Evaluate(`{"source":`+quote(permitSource)+`}`), &got)
	if got.EvaluatedAt != "2026-06-02T00:00:00Z" {
		t.Errorf("evaluated_at = %q, want the host clock", got.EvaluatedAt)
	}
	if got.Result == crl.Authorized {
		t.Error("result = AUTHORIZED with no facts")
	}
}

// TestEvaluateRejectsAnUnparseableClock: a clock that cannot be read
// must fail loudly, not fall back to the host and answer a different
// question than the caller asked.
func TestEvaluateRejectsAnUnparseableClock(t *testing.T) {
	message := errorOf(t, fixedEngine(t).Evaluate(`{"source":`+quote(permitSource)+`,"now":"yesterday"}`))
	if !strings.Contains(message, "invalid now timestamp") {
		t.Errorf("error = %q, want it to name the bad timestamp", message)
	}
}

// TestGraphIsPositionedAndAddressesTheBundle: the renderer draws what
// the engine positions, so the layout must arrive with geometry and
// must be the graph of the bundle whose hash rides along with it.
func TestGraphIsPositionedAndAddressesTheBundle(t *testing.T) {
	var got graphResponse
	decodeResponse(t, fixedEngine(t).Graph(`{"source":`+quote(permitSource)+`}`), &got)

	compiled, err := crl.Compile(permitSource)
	if err != nil {
		t.Fatalf("compile reference: %v", err)
	}
	if got.Hash != compiled.Hash {
		t.Errorf("hash = %q, want %q", got.Hash, compiled.Hash)
	}
	var layout struct {
		Nodes []struct {
			ID     string  `json:"id"`
			Kind   string  `json:"kind"`
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"nodes"`
		Edges []struct {
			Points []struct{ X, Y float64 } `json:"points"`
		} `json:"edges"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	if err := json.Unmarshal(got.Graph, &layout); err != nil {
		t.Fatalf("decode layout: %v", err)
	}
	if len(layout.Nodes) == 0 {
		t.Fatal("layout has no nodes")
	}
	if layout.Width <= 0 || layout.Height <= 0 {
		t.Errorf("layout is unsized: %vx%v", layout.Width, layout.Height)
	}
	for _, node := range layout.Nodes {
		if node.Width <= 0 || node.Height <= 0 {
			t.Errorf("node %s (%s) has no geometry", node.ID, node.Kind)
		}
	}
	if len(layout.Edges) == 0 {
		t.Fatal("layout has no edges; the quorum references three collectors")
	}
	for _, edge := range layout.Edges {
		if len(edge.Points) < 2 {
			t.Error("edge has no route")
		}
	}
	if len(got.Structure) == 0 {
		t.Error("graph response carries no unpositioned structure")
	}
}

// TestGraphIsDeterministic: the diagram is a function of the source, so
// two calls must return byte-identical geometry.
func TestGraphIsDeterministic(t *testing.T) {
	engine := fixedEngine(t)
	first := engine.Graph(`{"source":` + quote(permitSource) + `}`)
	second := engine.Graph(`{"source":` + quote(permitSource) + `}`)
	if first != second {
		t.Error("two graph calls on one source returned different responses")
	}
}

// TestLintReportsDiagnosticsInsteadOfFailing: a source that does not
// compile is the linter's normal input; the author needs the position
// and the code, not a bare engine error.
func TestLintReportsDiagnosticsInsteadOfFailing(t *testing.T) {
	var got crl.LintReport
	decodeResponse(t, fixedEngine(t).Lint(`{"source":"crl v1\nbundle broken\n"}`), &got)
	if got.OK {
		t.Fatal("ok = true for a source that does not compile")
	}
	if len(got.Diagnostics) == 0 {
		t.Fatal("no diagnostics for a broken source")
	}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code == "" {
			t.Error("diagnostic has no CRL### code")
		}
	}
	if got.Path != "playground.crl" {
		t.Errorf("path = %q, want the default label", got.Path)
	}
}

// TestFormatReturnsCanonicalTextAndItsHash: the formatted bytes and the
// hash must come from one compilation, or a page could show text that
// does not compile to the hash beside it.
func TestFormatReturnsCanonicalTextAndItsHash(t *testing.T) {
	var got formatResponse
	decodeResponse(t, fixedEngine(t).Format(`{"source":`+quote(permitSource)+`}`), &got)

	if !strings.HasSuffix(got.Formatted, "\n") {
		t.Error("formatted text does not end in a newline")
	}
	recompiled, err := crl.Compile(got.Formatted)
	if err != nil {
		t.Fatalf("formatted text does not compile: %v", err)
	}
	if recompiled.Hash != got.Hash {
		t.Errorf("formatted text compiles to %q, but the response reports %q", recompiled.Hash, got.Hash)
	}
}

// TestInfoNamesTheEditionAndFunctions: a page that displays a hash must
// be able to say which toolchain and edition produced it.
func TestInfoNamesTheEditionAndFunctions(t *testing.T) {
	var got infoResponse
	decodeResponse(t, Engine{Version: "1.2.3"}.Info(""), &got)
	if got.Version != "1.2.3" {
		t.Errorf("version = %q, want the stamped version", got.Version)
	}
	if got.Edition != crl.EditionV1 {
		t.Errorf("edition = %q, want %q", got.Edition, crl.EditionV1)
	}
	if len(got.Functions) != len(Engine{}.Functions()) {
		t.Errorf("info lists %d functions, engine installs %d", len(got.Functions), len(Engine{}.Functions()))
	}
	var unstamped infoResponse
	decodeResponse(t, Engine{}.Info(""), &unstamped)
	if unstamped.Version != "dev" {
		t.Errorf("unstamped version = %q, want \"dev\"", unstamped.Version)
	}
}

// TestHandlersAlwaysReturnJSON: the browser boundary has no error
// channel, so no input may produce anything but a JSON envelope.
func TestHandlersAlwaysReturnJSON(t *testing.T) {
	inputs := []string{"", "{}", "not json", `{"source":""}`, `{"source":"   "}`, `{"source":123}`, `[]`, `null`}
	for _, function := range fixedEngine(t).Functions() {
		for _, input := range inputs {
			body := function.Call(input)
			var any map[string]json.RawMessage
			if err := json.Unmarshal([]byte(body), &any); err != nil {
				t.Errorf("%s(%q) returned non-JSON %q: %v", function.Name, input, body, err)
			}
		}
	}
}

// TestEmptySourceIsNamed: an empty editor is the first thing a visitor
// sees, and it must not read as a compiler error about the language.
func TestEmptySourceIsNamed(t *testing.T) {
	for _, function := range []func(string) string{
		fixedEngine(t).Compile,
		fixedEngine(t).Format,
		fixedEngine(t).Graph,
		fixedEngine(t).Evaluate,
	} {
		if message := errorOf(t, function(`{"source":"  "}`)); message != "source is required" {
			t.Errorf("error = %q, want \"source is required\"", message)
		}
	}
}

// TestResponsesDoNotEscapeHTML: canonical text must come back as the
// bytes that were hashed. Go's default JSON encoder rewrites `<`, `>`,
// and `&` as \u escapes, which would leave a page showing text that
// does not compile to the hash printed beside it.
func TestResponsesDoNotEscapeHTML(t *testing.T) {
	source := `crl v1
package examples.strings
bundle t.strings

rule s
	target t.s
	collector c reviewer attestation from /bundles/review.json
	signal grade string from review.grade ttl 30d
	need grade == "a<b&c"
`
	body := fixedEngine(t).Compile(`{"source":` + quote(source) + `}`)
	var got compileResponse
	decodeResponse(t, body, &got)
	if !strings.Contains(got.CanonicalText, `"a<b&c"`) {
		t.Fatalf("canonical text lost the literal: %q", got.CanonicalText)
	}
	if strings.Contains(body, "\\u003c") || strings.Contains(body, "\\u0026") {
		t.Errorf("response escapes HTML characters: %s", body)
	}
	if !strings.Contains(body, `a<b&c`) {
		t.Errorf("response does not carry the literal bytes: %s", body)
	}
}

func quote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
