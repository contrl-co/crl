package crl

import "testing"

// A struct-built quorum expression must never compile to canonical text
// the compiler then refuses to recompile: that would break hash
// re-verification. The struct and text paths must accept the same set.
func TestStructQuorumCanonicalTextRoundTrips(t *testing.T) {
	for _, n := range []int{2, 100, 256, 257, 400} {
		expr := &QuorumExpression{Kind: "subject", Name: "c"}
		for i := 1; i < n; i++ {
			expr = &QuorumExpression{Kind: "and", Left: expr, Right: &QuorumExpression{Kind: "subject", Name: "c"}}
		}
		b := Bundle{Version: Version, Rules: []Rule{{Name: "r", Target: "a.b",
			Collectors: []Collector{{Name: "c", ProviderType: "org", ConnectorKind: "api", Source: "/x.json",
				Signals: []Signal{{Name: "s", Kind: "bool", SourceField: "x.y", Expiry: SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 2592000}}}}},
			Predicates: []Predicate{{Kind: PredicateQuorum, Expression: expr}}}}}
		compiled, err := CompileBundleProgram(b)
		if err != nil {
			continue // rejected is fine; emitting bad canonical text is not
		}
		if _, err := CompileBundle(compiled.CanonicalText); err != nil {
			t.Errorf("n=%d: struct compiled but its canonical text does not recompile: %v", n, err)
		}
	}
}

// A subject named after an operator word can never survive the text
// round-trip: rendered `!and` re-lexes as `! &`. The struct path must
// reject it up front instead of emitting canonical text it refuses to
// recompile.
func TestStructQuorumRejectsOperatorWordSubjects(t *testing.T) {
	for _, name := range []string{"and", "or", "not", "AND"} {
		expr := &QuorumExpression{Kind: "or",
			Left:  &QuorumExpression{Kind: "not", Expr: &QuorumExpression{Kind: "subject", Name: name}},
			Right: &QuorumExpression{Kind: "subject", Name: "s1"},
		}
		b := Bundle{Version: Version, Rules: []Rule{{Name: "r", Target: "a.b",
			Collectors: []Collector{{Name: name, ProviderType: "org", ConnectorKind: "api", Source: "/x.json",
				Signals: []Signal{{Name: "s1", Kind: "bool", SourceField: "x.y", Expiry: SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 2592000}}}}},
			Predicates: []Predicate{{Kind: PredicateQuorum, Expression: expr}}}}}
		compiled, err := CompileBundleProgram(b)
		if err != nil {
			continue // rejected up front is the required outcome
		}
		if _, err := CompileBundle(compiled.CanonicalText); err != nil {
			t.Errorf("subject %q: struct compiled but its canonical text does not recompile: %v", name, err)
		}
	}
}
