package crl

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLexProducesSourceAwareTokens(t *testing.T) {
	tokens, err := Lex(`rule power_to_site {
	need capacity_kw >= 2000 # trailing comments are skipped
	quorum utility_record and not grid_hold
}`)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	semantic := nonStructuralTokens(tokens)
	want := []Token{
		{Kind: TokenIdentifier, Literal: "rule", Line: 1, Column: 1},
		{Kind: TokenIdentifier, Literal: "power_to_site", Line: 1, Column: 6},
		{Kind: TokenLBrace, Literal: "{", Line: 1, Column: 20},
		{Kind: TokenIdentifier, Literal: "need", Line: 2, Column: 2},
		{Kind: TokenIdentifier, Literal: "capacity_kw", Line: 2, Column: 7},
		{Kind: TokenOperator, Literal: ">=", Line: 2, Column: 19},
		{Kind: TokenNumber, Literal: "2000", Line: 2, Column: 22},
	}
	if len(semantic) < len(want) {
		t.Fatalf("tokens length = %d, want at least %d", len(semantic), len(want))
	}
	for i, expected := range want {
		if semantic[i] != expected {
			t.Fatalf("token %d = %+v, want %+v", i, semantic[i], expected)
		}
	}
	if tokens[len(tokens)-1].Kind != TokenEOF {
		t.Fatalf("last token = %s, want eof", tokens[len(tokens)-1].Kind)
	}
}

func TestParseRendersLogicalSyntaxFields(t *testing.T) {
	tree, err := Parse(`quorum utility_record and not (grid_hold or stale_capacity)`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := tree.Statements[0].Fields()
	want := []string{"quorum", "utility_record", "&", "!", "(", "grid_hold", "|", "stale_capacity", ")"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v, want %#v", got, want)
	}
}

func TestCompileAcceptsObjectRuleBlocks(t *testing.T) {
	compiled, err := CompileBundle(`
crl v1

rule power_to_site {
	target utility.power
	collector utility_record utility file_upload from /bundles/power.json {
		signal power_built bool from power.built ttl 10y
		signal capacity_kw number from power.capacity_kw ttl 10y
		signal grid_hold bool from power.grid_hold ttl 30d
	}
	need power_built == true
	need capacity_kw >= 2000
	quorum utility_record and not grid_hold
}
`)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	result := EvaluateBundleAt(compiled, Facts{
		"power_built":             true,
		"capacity_kw":             2400.0,
		"grid_hold":               false,
		"provider.utility_record": true,
		"observed_at.power_built": now.Add(-time.Hour),
		"observed_at.capacity_kw": now.Add(-time.Hour),
		"observed_at.grid_hold":   now.Add(-time.Hour),
	}, now)
	if !result.Authorized {
		t.Fatalf("expected authorization, checks=%+v", result.Checks)
	}
	if got := compiled.CanonicalText; !containsAll(got, []string{
		"rule power_to_site",
		"target utility.power",
		"quorum !grid_hold & utility_record",
	}) {
		t.Fatalf("canonical text missing object-rule content:\n%s", got)
	}
}

func TestCompileBundleAcceptsObjectBlocksForBuildability(t *testing.T) {
	compiled, err := CompileBundle(`
crl v1

rule power_to_site {
	target utility.power
	collector utility_record utility file_upload from /bundles/power.json {
		signal power_built bool from power.built ttl 10y
		signal capacity_kw number from power.capacity_kw ttl 10y
		signal grid_hold bool from power.grid_hold ttl 30d
	}
	need power_built == true
	need capacity_kw >= 2000
	quorum utility_record and not grid_hold
}

cluster buildable_scope {
	rules power_to_site
	quorum power_to_site
}

need buildable_scope == true
quorum buildable_scope
`)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	result := EvaluateBundleAt(compiled, Facts{
		"power_built":             true,
		"capacity_kw":             2400.0,
		"grid_hold":               false,
		"provider.utility_record": true,
		"observed_at.power_built": now.Add(-time.Hour),
		"observed_at.capacity_kw": now.Add(-time.Hour),
		"observed_at.grid_hold":   now.Add(-time.Hour),
	}, now)
	if !result.Authorized || result.Result != "AUTHORIZED" {
		t.Fatalf("expected authorized bundle, result=%s checks=%+v", result.Result, result.Checks)
	}
	if len(result.RuleTraces) != 1 || len(result.ClusterTraces) != 1 || len(result.GlobalChecks) != 2 {
		t.Fatalf("unexpected trace shape: rules=%d clusters=%d global=%d", len(result.RuleTraces), len(result.ClusterTraces), len(result.GlobalChecks))
	}
}

func containsAll(haystack string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

func nonStructuralTokens(tokens []Token) []Token {
	out := make([]Token, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == TokenNewline || token.Kind == TokenEOF {
			continue
		}
		out = append(out, token)
	}
	return out
}

// A positive UTC offset in an RFC3339 timestamp used to be rejected because
// '+' split the token, while '-' and 'Z' worked. A spaced cluster-rule '+'
// must still tokenize on its own.
func TestPositiveUTCOffsetInExpiresIsAccepted(t *testing.T) {
	for _, offset := range []string{"+05:30", "-05:00", "Z"} {
		src := "crl v1\npackage p\nbundle b\n\nrule r\n\ttarget t.x\n" +
			"\tcollector c m file_upload from /f\n" +
			"\t\tsignal a bool from f.a expires 2026-12-31T23:59:59" + offset + "\n\tneed a == true\n"
		if _, err := CompileBundle(src); err != nil {
			t.Errorf("expires offset %q rejected: %v", offset, err)
		}
	}
	// A spaced cluster-rule '+' must still work.
	cluster := "crl v1\npackage p\nbundle b\n\n" +
		"rule ra\n\ttarget a.a\n\tcollector c1 m file_upload from /a\n\t\tsignal s1 bool from a.a ttl 30d\n\tneed s1 == true\n" +
		"rule rb\n\ttarget a.b\n\tcollector c2 m file_upload from /b\n\t\tsignal s2 bool from b.b ttl 30d\n\tneed s2 == true\n" +
		"cluster cl\n\trules ra + rb\n\tneed rb == true\n"
	if _, err := CompileBundle(cluster); err != nil {
		t.Fatalf("spaced cluster-rule + broke: %v", err)
	}
}
