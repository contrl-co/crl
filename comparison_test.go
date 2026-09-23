package crl

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

const comparisonSource = `crl v1
rule receipt
target delivery
collector warehouse source api from warehouse.receipts
signal shipped number from shipped ttl 1d
signal received number from received ttl 1d
need received >= shipped
`

func TestNumericSignalComparison(t *testing.T) {
	compiled, err := Compile(comparisonSource)
	if err != nil {
		t.Fatal(err)
	}
	predicate := compiled.Program().Rules[0].Predicates[0]
	if predicate.Reference != "shipped" || predicate.Value != nil {
		t.Fatalf("program must expose a reference, not a literal zero: %+v", predicate)
	}
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		right    any
		observed any
		result   Result
	}{
		{"equal", 100, now, "AUTHORIZED"},
		{"short", 101, now, "DENIED"},
		{"missing", nil, now, "INSUFFICIENT_EVIDENCE"},
		{"stale", 100, now.Add(-48 * time.Hour), "EXPIRED"},
		{"unknown age", 100, nil, "EXPIRED"},
		{"wrong type", true, now, "DENIED"},
		{"nan", math.NaN(), now, "DENIED"},
		{"infinite", math.Inf(1), now, "DENIED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := Facts{"received": 100, "observed_at.received": now}
			if tc.right != nil {
				facts["shipped"] = tc.right
			}
			if tc.observed != nil {
				facts["observed_at.shipped"] = tc.observed
			}
			got := compiled.EvaluateAt(facts, now)
			if got.Result != tc.result || got.Authorized != (tc.result == "AUTHORIZED") {
				t.Fatalf("got %+v, want %s", got, tc.result)
			}
		})
	}
	for _, field := range []string{"received", "shipped"} {
		facts := Facts{"received": 100, "shipped": 100, "observed_at.received": now, "observed_at.shipped": now}
		delete(facts, field)
		if got := compiled.EvaluateAt(facts, now); got.Result != "INSUFFICIENT_EVIDENCE" {
			t.Fatalf("missing %s: %+v", field, got)
		}
		facts[field] = 100
		facts["observed_at."+field] = now.Add(-48 * time.Hour)
		if got := compiled.EvaluateAt(facts, now); got.Result != "EXPIRED" {
			t.Fatalf("stale %s: %+v", field, got)
		}
	}
	canonical, err := Compile(compiled.CanonicalText)
	if err != nil || canonical.Hash != compiled.Hash {
		t.Fatalf("canonical round trip: %v", err)
	}
	got := compiled.EvaluateAt(Facts{"received": 100, "shipped": 100, "observed_at.received": now, "observed_at.shipped": now}, now)
	if len(got.Checks) != 1 || got.Checks[0].Reference != "shipped" || got.Checks[0].Expected != float64(100) {
		t.Fatalf("trace must identify the right signal and its observed value: %+v", got.Checks)
	}
	graph, err := Graph(comparisonSource)
	if err != nil || !strings.Contains(string(graph.Graph), "need received ") || !strings.Contains(string(graph.Graph), "shipped") {
		t.Fatalf("comparison graph: %+v %v", graph, err)
	}
}

func TestNumericComparisonOperators(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	for _, op := range []string{"==", "!=", "<", "<=", ">", ">="} {
		for _, right := range []int{99, 100, 101} {
			t.Run(fmt.Sprintf("%s/%d", op, right), func(t *testing.T) {
				source := strings.Replace(comparisonSource, ">= shipped", op+" shipped", 1)
				ref, err := Compile(source)
				if err != nil {
					t.Fatal(err)
				}
				literal, err := Compile(strings.Replace(source, op+" shipped", fmt.Sprintf("%s %d", op, right), 1))
				if err != nil {
					t.Fatal(err)
				}
				facts := Facts{"received": 100, "shipped": right, "observed_at.received": now, "observed_at.shipped": now}
				if a, b := ref.EvaluateAt(facts, now), literal.EvaluateAt(facts, now); a.Result != b.Result {
					t.Fatalf("signal %s differs from literal %s", a.Result, b.Result)
				}
			})
		}
	}
}

func TestNumericComparisonRejectsInvalidReferences(t *testing.T) {
	for _, source := range []string{
		strings.Replace(comparisonSource, ">= shipped", ">= missing", 1),
		strings.Replace(comparisonSource, "shipped number", "shipped bool", 1),
		strings.Replace(comparisonSource, "received number", "received string", 1),
		strings.Replace(comparisonSource, ">= shipped", ">= min_provider_trust", 1),
		strings.Replace(comparisonSource, ">= shipped", ">= shipped + 1", 1),
	} {
		if _, err := Compile(source); err == nil {
			t.Fatalf("invalid comparison accepted:\n%s", source)
		}
	}
}
