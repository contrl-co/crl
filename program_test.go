package crl

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProgramViewFreshnessJSON(t *testing.T) {
	for _, test := range []struct {
		clause, expiry, mode, at string
		seconds                  int64
	}{
		{clause: "ttl 90m", expiry: "ttl 90m", mode: "ttl", seconds: 5400},
		{clause: "expires 2h", expiry: "ttl 2h", mode: "ttl", seconds: 7200},
		{clause: `expires "2026-12-31T23:59:59+02:00"`, expiry: "expires 2026-12-31T23:59:59+02:00", mode: "at", at: "2026-12-31T23:59:59+02:00"},
	} {
		t.Run(test.clause, func(t *testing.T) {
			source := "crl v1\nrule r\ntarget delivery\ncollector c source api from /stock.json\nsignal quantity number from stock.quantity unit kg optional " + test.clause + "\nneed quantity >= 0\n"
			compiled, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(compiled.Program().Rules[0].Collectors[0].Signals[0])
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Expiry    string
				Freshness struct {
					Mode    string
					Seconds int64
					At      string
				}
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got.Expiry != test.expiry || got.Freshness.Mode != test.mode || got.Freshness.Seconds != test.seconds || got.Freshness.At != test.at {
				t.Fatalf("freshness must preserve the compiler's normalized value: %s", raw)
			}
		})
	}
}

func TestProgramViewZeroLiteralJSON(t *testing.T) {
	compiled, err := Compile(strings.Replace(comparisonSource, ">= shipped", ">= 0", 1))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(compiled.Program().Rules[0].Predicates[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	var literal map[string]any
	if err := json.Unmarshal(raw, &literal); err != nil {
		t.Fatal(err)
	}
	if literal["kind"] != "number" || literal["number"] != float64(0) {
		t.Fatalf("numeric zero must be present, not omitted: %s", raw)
	}
}

func TestProgramViewMirrorsCompiledBundle(t *testing.T) {
	src := `crl v1
package examples.permits
bundle permit.launch

rule permit_ready
	target permit.application
	collector application_file municipality file_upload from /bundles/application.json schema permit_v1
		signal application_complete bool from application.complete ttl 30d
		signal committed_usd number from capital.committed unit usd ttl 30d
	need application_complete == true
	need committed_usd >= 250000
	block application_complete
	quorum application_file

cluster launch
	rules permit_ready
	quorum permit_ready

need launch == true
`
	compiled, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	p := compiled.Program()
	if p.Package != "examples.permits" || p.Bundle != "permit.launch" {
		t.Fatalf("package/bundle: %q %q", p.Package, p.Bundle)
	}
	if len(p.Rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(p.Rules))
	}
	rule := p.Rules[0]
	if rule.Name != "permit_ready" || rule.Target != "permit.application" {
		t.Fatalf("rule head: %+v", rule)
	}
	if len(rule.Collectors) != 1 || len(rule.Collectors[0].Signals) != 2 {
		t.Fatalf("collectors/signals: %+v", rule.Collectors)
	}
	sig := rule.Collectors[0].Signals[1]
	if sig.Name != "committed_usd" || sig.Kind != "number" || sig.Unit != "usd" || sig.Expiry != "ttl 30d" {
		t.Fatalf("signal view: %+v", sig)
	}
	// need with a number value
	var foundNeed, foundBlock, foundQuorum bool
	for _, pred := range rule.Predicates {
		switch pred.Kind {
		case PredicateNeed:
			if pred.Field == "committed_usd" {
				foundNeed = true
				if pred.Operator != ">=" || pred.Value == nil || pred.Value.Number != 250000 {
					t.Fatalf("need view: %+v", pred)
				}
			}
		case PredicateBlock:
			foundBlock = true
		case PredicateQuorum:
			foundQuorum = true
		}
	}
	if !foundNeed || !foundBlock || !foundQuorum {
		t.Fatalf("missing predicate kinds: need=%v block=%v quorum=%v", foundNeed, foundBlock, foundQuorum)
	}
	if len(p.Clusters) != 1 || p.Clusters[0].Name != "launch" {
		t.Fatalf("clusters: %+v", p.Clusters)
	}
	if len(p.Predicates) != 1 || p.Predicates[0].Field != "launch" {
		t.Fatalf("global predicates: %+v", p.Predicates)
	}
}

func TestProgramViewQuorumExpression(t *testing.T) {
	src := "crl v1\npackage p\nbundle b\nrule r\n\ttarget t.a\n\tcollector ca org api from /x.json\n\t\tsignal s1 bool from x.y ttl 30d\n\tcollector cb org api from /y.json\n\t\tsignal s2 bool from y.z ttl 30d\n\tneed s1 == true\n\tquorum ca & cb\n"
	compiled, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, pred := range compiled.Program().Rules[0].Predicates {
		if pred.Kind == PredicateQuorum {
			if pred.QuorumExpression != "ca & cb" {
				t.Fatalf("quorum expression view: %q", pred.QuorumExpression)
			}
			return
		}
	}
	t.Fatal("no quorum predicate found")
}
