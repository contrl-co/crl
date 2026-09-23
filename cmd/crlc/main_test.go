package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	crl "github.com/contrl-co/crl"
	"github.com/contrl-co/crl/internal/crlwasm"
)

const goodSource = `crl v1
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

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestNoArgsPrintsUsage(t *testing.T) {
	code, _, stderr := runCLI(t, "")
	if code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "usage: crlc") {
		t.Fatalf("want usage on stderr, got %q", stderr)
	}
}

func TestLintStdinOK(t *testing.T) {
	code, stdout, _ := runCLI(t, goodSource, "lint")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "ok") {
		t.Fatalf("want ok, got %q", stdout)
	}
}

func TestLintBrokenSourceFails(t *testing.T) {
	code, stdout, _ := runCLI(t, "rule broken\n\tneed x == true\n", "lint")
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if !strings.Contains(stdout, "CRL1") {
		t.Fatalf("want CRL error code in output, got %q", stdout)
	}
}

func TestLintFailOnWarningCatchesStyle(t *testing.T) {
	// No `crl v1` header, package, or bundle name: warnings only.
	warnOnly := "rule r\n\ttarget a.b\n\tcollector c org api from /x.json\n\t\tsignal s bool from x ttl 30d\n\tneed s == true\n"
	code, _, _ := runCLI(t, warnOnly, "lint")
	if code != 0 {
		t.Fatalf("warnings must pass at default threshold, got %d", code)
	}
	code, _, _ = runCLI(t, warnOnly, "lint", "-fail-on", "warning")
	if code != 1 {
		t.Fatalf("warnings must fail at warning threshold, got %d", code)
	}
}

func TestLintJSONFormat(t *testing.T) {
	code, stdout, _ := runCLI(t, goodSource, "lint", "-format", "json", "-canonical")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	var parsed struct {
		OK      bool `json:"ok"`
		Reports []struct {
			CompiledHash  string `json:"compiled_hash"`
			CanonicalText string `json:"canonical_text"`
		} `json:"reports"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if !parsed.OK || len(parsed.Reports) != 1 || parsed.Reports[0].CompiledHash == "" || parsed.Reports[0].CanonicalText == "" {
		t.Fatalf("unexpected report: %+v", parsed)
	}
}

func TestLintDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.crl"), []byte(goodSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.crl"), []byte(goodSource), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCLI(t, "", "lint", dir)
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "2 CRL files: ok") {
		t.Fatalf("want 2-file summary, got %q", stdout)
	}
}

func TestCompileTextEndsWithHashComment(t *testing.T) {
	code, stdout, _ := runCLI(t, goodSource, "compile")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "# sha256:") {
		t.Fatalf("want trailing hash comment, got %q", last)
	}
	// The full output must itself be valid CRL.
	relint, _, _ := runCLI(t, stdout, "lint")
	if relint != 0 {
		t.Fatal("compile output must re-lint clean")
	}
}

func TestCompileRejectsUnknownEdition(t *testing.T) {
	code, _, stderr := runCLI(t, goodSource, "compile", "-edition", "v2")
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "unknown edition") {
		t.Fatalf("want unknown-edition error, got %q", stderr)
	}
}

func TestCompileJSONDeterminism(t *testing.T) {
	_, first, _ := runCLI(t, goodSource, "compile", "-format", "json")
	_, second, _ := runCLI(t, "# comment\n"+goodSource, "compile", "-format", "json")
	var a, b struct {
		Hash          string `json:"hash"`
		CanonicalText string `json:"canonical_text"`
	}
	if err := json.Unmarshal([]byte(first), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(second), &b); err != nil {
		t.Fatal(err)
	}
	if a.Hash == "" || a.Hash != b.Hash || a.CanonicalText != b.CanonicalText {
		t.Fatalf("comment changed compile output: %q vs %q", a.Hash, b.Hash)
	}
}

func TestCompileJSONMetadata(t *testing.T) {
	const source = `crl v1
rule receipt
target delivery
collector warehouse source api from /receipts.json
signal received number from warehouse.received unit kg ttl 90m
signal shipped number from warehouse.shipped unit kg expires "2026-12-31T00:00:00Z"
need received >= shipped
need received >= 0
`
	code, stdout, stderr := runCLI(t, source, "compile", "-format", "json")
	if code != 0 {
		t.Fatalf("compile: exit %d, %s", code, stderr)
	}
	var got struct {
		MetadataVersion int              `json:"metadata_version"`
		Program         *crl.ProgramView `json:"program"`
		Hash            string           `json:"hash"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got.MetadataVersion != 1 || got.Program == nil {
		t.Fatalf("missing versioned compiler metadata: %s", stdout)
	}
	compiled, err := crl.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	const originalHash = "b33c3ac4bea6dce8dbf39e902acf1be8fe5b479693ffdc26319edb1bb58382b7"
	if got.Hash != originalHash || got.Hash != compiled.Hash || !reflect.DeepEqual(*got.Program, compiled.Program()) {
		t.Fatal("CLI metadata differs from the compiled program")
	}
	rule := got.Program.Rules[0]
	signals := map[string]crl.SignalView{}
	for _, signal := range rule.Collectors[0].Signals {
		signals[signal.Name] = signal
	}
	for _, name := range []string{"received", "shipped"} {
		if signal := signals[name]; signal.Kind != "number" || signal.Unit != "kg" || signal.SourceField != "warehouse."+name {
			t.Fatalf("missing typed operand %s: %+v", name, signal)
		}
	}
	found := false
	for _, predicate := range rule.Predicates {
		if predicate.Reference == "shipped" {
			found = true
			if predicate.Field != "received" || predicate.Operator != ">=" || predicate.Value != nil {
				t.Fatalf("reference must not become a literal: %+v", predicate)
			}
		}
	}
	if !found {
		t.Fatal("comparison reference is missing")
	}
	request, err := json.Marshal(map[string]string{"source": source})
	if err != nil {
		t.Fatal(err)
	}
	var browser struct {
		Program *crl.ProgramView `json:"program"`
		Hash    string           `json:"hash"`
	}
	if err := json.Unmarshal([]byte((crlwasm.Engine{}).Compile(string(request))), &browser); err != nil {
		t.Fatal(err)
	}
	if browser.Hash != got.Hash || !reflect.DeepEqual(browser.Program, got.Program) {
		t.Fatal("CLI and WASM metadata differ")
	}
}

func TestFmtIsFixedPoint(t *testing.T) {
	_, once, _ := runCLI(t, goodSource, "fmt")
	code, twice, _ := runCLI(t, once, "fmt")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if once != twice {
		t.Fatal("fmt must be a fixed point")
	}
}

func TestFmtWriteInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "messy.crl")
	messy := "# messy spacing\n" + strings.ReplaceAll(goodSource, "\t", "        ")
	if err := os.WriteFile(path, []byte(messy), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCLI(t, "", "fmt", "-w", path)
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(stdout, path) {
		t.Fatalf("want rewritten path printed, got %q", stdout)
	}
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(rewritten), "crl v1\n") {
		t.Fatal("file must be canonicalized in place")
	}
}

func TestEvalFiveOutputs(t *testing.T) {
	dir := t.TempDir()
	writeFacts := func(name string, facts map[string]any) string {
		t.Helper()
		raw, err := json.Marshal(facts)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	at := "2026-01-02T03:04:05Z"
	base := func() map[string]any {
		return map[string]any{
			"application_complete":             true,
			"permit_hold_active":               false,
			"application_file":                 true,
			"observed_at.application_complete": at,
			"observed_at.permit_hold_active":   at,
		}
	}

	cases := []struct {
		name string
		edit func(map[string]any)
		want string
		noAt bool
	}{
		{name: "authorized", edit: func(map[string]any) {}, want: "AUTHORIZED"},
		{name: "denied", edit: func(f map[string]any) { f["application_complete"] = false }, want: "DENIED"},
		{name: "blocked", edit: func(f map[string]any) { f["permit_hold_active"] = true }, want: "BLOCKED"},
		{name: "missing", edit: func(f map[string]any) {
			delete(f, "application_complete")
			delete(f, "observed_at.application_complete")
		}, want: "INSUFFICIENT_EVIDENCE"},
		{name: "expired", edit: func(map[string]any) {}, want: "EXPIRED", noAt: true},
	}
	for _, tc := range cases {
		facts := base()
		tc.edit(facts)
		path := writeFacts(tc.name+".json", facts)
		args := []string{"eval", "-facts", path}
		if !tc.noAt {
			args = append(args, "-at", at)
		}
		code, stdout, stderr := runCLI(t, goodSource, args...)
		if code != 0 {
			t.Fatalf("%s: want exit 0, got %d (%s)", tc.name, code, stderr)
		}
		if !strings.HasPrefix(stdout, tc.want) {
			t.Fatalf("%s: want %s, got %q", tc.name, tc.want, stdout)
		}
	}
}

func TestEvalRequireAuthorized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "facts.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, _ := runCLI(t, goodSource, "eval", "-facts", path, "-require-authorized")
	if code != 1 {
		t.Fatalf("want exit 1 for non-authorized under -require-authorized, got %d", code)
	}
}

func TestGraphEmitsDeterministicJSON(t *testing.T) {
	code, first, _ := runCLI(t, goodSource, "graph")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	_, second, _ := runCLI(t, goodSource, "graph")
	if first != second {
		t.Fatal("graph output must be deterministic")
	}
	var parsed struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(first), &parsed); err != nil {
		t.Fatalf("graph output is not JSON: %v", err)
	}
	if parsed.Hash == "" {
		t.Fatal("graph output must carry the bundle hash")
	}
}

func TestVersionMentionsEditions(t *testing.T) {
	code, stdout, _ := runCLI(t, "", "version")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "editions: v1") {
		t.Fatalf("version must list supported editions, got %q", stdout)
	}
}
