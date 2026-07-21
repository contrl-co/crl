package crl

import (
	"strings"
	"testing"
)

// A deeply nested quorum expression must be rejected, not overflow the
// stack. Before the depth cap, ~1.7M parens killed crlc with an
// unrecoverable fatal error that recover() cannot catch.
func TestQuorumExpressionDepthIsBounded(t *testing.T) {
	src := "crl v1\npackage t\nbundle t.d\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal a bool from f.a ttl 30d\n" +
		"\tquorum " + strings.Repeat("(", 5000) + "a" + strings.Repeat(")", 5000) + "\n"
	if _, err := CompileBundle(src); err == nil {
		t.Fatal("a pathologically nested quorum expression compiled; the depth cap is gone")
	}
}

// The cap must not reject expressions a real author would write.
func TestQuorumExpressionNormalDepthCompiles(t *testing.T) {
	src := "crl v1\npackage t\nbundle t.d\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal a bool from f.a ttl 30d\n" +
		"\t\tsignal b bool from f.b ttl 30d\n\tquorum (a or b) and a\n"
	if _, err := CompileBundle(src); err != nil {
		t.Fatalf("a normal quorum expression was rejected: %v", err)
	}
}

// A wide flat operator chain parses iteratively, so it never trips the
// nesting cap, but its N-deep tree overflowed the recursive normalize and
// eval walks. Total-size bounding must reject it, not crash.
func TestQuorumExpressionFlatChainIsBounded(t *testing.T) {
	src := "crl v1\npackage t\nbundle t.d\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal a bool from f.a ttl 30d\n" +
		"\tquorum a" + strings.Repeat(" & a", 100000) + "\n"
	if _, err := CompileBundle(src); err == nil {
		t.Fatal("a pathologically wide flat quorum chain compiled; the size cap is gone")
	}
}

// A caller-built quorum tree via the struct API bypasses the text parser's
// token cap; normalizeQuorumExpression must bound its own recursion.
func TestStructBuiltQuorumTreeIsBounded(t *testing.T) {
	expr := &QuorumExpression{Kind: "subject", Name: "a"}
	for i := 0; i < 100000; i++ {
		expr = &QuorumExpression{Kind: "and", Left: expr, Right: &QuorumExpression{Kind: "subject", Name: "a"}}
	}
	_, err := CompileBundleProgram(Bundle{Version: "crl/v1", Package: "t", Name: "t.d",
		Rules: []Rule{{Name: "r", Target: "t.x",
			Collectors: []Collector{{Name: "c", ProviderType: "m", ConnectorKind: "file_upload", Source: "/f",
				Signals: []Signal{{Name: "a", Kind: "bool", SourceField: "f.a", Expiry: SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 2592000}}}}},
			Predicates: []Predicate{{Kind: "quorum", Expression: expr}}}}})
	if err == nil {
		t.Fatal("a struct-built deep quorum tree compiled; the recursion is unbounded")
	}
}

// The token cap must be applied DURING tokenization, not after. Otherwise a
// pathological field (millions of nested parens) materializes a multi-million
// entry token slice — gigabytes of transient memory — before the size check
// rejects it. quorumTokens must stop just past the cap.
func TestQuorumTokensStopsAtCap(t *testing.T) {
	field := strings.Repeat("(", maxQuorumTokens*8) + "a" + strings.Repeat(")", maxQuorumTokens*8)
	tokens := quorumTokens([]string{field})
	if len(tokens) > maxQuorumTokens+1 {
		t.Fatalf("quorumTokens built %d tokens for a pathological field; want <= %d", len(tokens), maxQuorumTokens+1)
	}
}

// The lexer emits one token per delimiter, so a source past the token cap must
// be rejected before it materializes a multi-million-entry slice (gigabytes).
// This is what actually bounds the memory of a deeply nested quorum expression;
// the quorum token cap alone runs too late.
func TestLexerRejectsTooManyTokens(t *testing.T) {
	// Well over maxTokens simple space-separated identifiers, so the only
	// failure is the token cap and not some other syntax error.
	src := "crl v1\n" + strings.Repeat("a ", maxTokens+10)
	if _, err := Lex(src); err == nil {
		t.Fatal("a source past the lexer token cap lexed; the cap is gone")
	}
}
