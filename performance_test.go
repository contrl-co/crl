package crl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	benchmarkCompiled   Compiled
	benchmarkEvaluation Evaluation
	benchmarkGraph      GraphResult
	benchmarkLint       LintReport
)

func TestAllocationBudgets(t *testing.T) {
	representativeSource, representativeFacts, at := representativeInputs(t)
	largeSource, largeFacts := largeBenchmarkInputs(40, at)
	representativeCompiled, err := Compile(representativeSource)
	if err != nil {
		t.Fatal(err)
	}
	largeCompiled, err := Compile(largeSource)
	if err != nil {
		t.Fatal(err)
	}

	assertAllocationBudget(t, "compile/representative", 1_900, func() {
		benchmarkCompiled, err = Compile(representativeSource)
		if err != nil {
			t.Fatal(err)
		}
	})
	assertAllocationBudget(t, "compile/rules=40", 56_000, func() {
		benchmarkCompiled, err = Compile(largeSource)
		if err != nil {
			t.Fatal(err)
		}
	})
	assertAllocationBudget(t, "evaluate/representative", 30, func() {
		benchmarkEvaluation = representativeCompiled.EvaluateAt(representativeFacts, at)
	})
	assertAllocationBudget(t, "evaluate/rules=40", 700, func() {
		benchmarkEvaluation = largeCompiled.EvaluateAt(largeFacts, at)
	})
	assertAllocationBudget(t, "lint/representative", 2_100, func() {
		benchmarkLint = Lint("benchmark.crl", representativeSource)
	})
	assertAllocationBudget(t, "lint/rules=40", 64_000, func() {
		benchmarkLint = Lint("benchmark.crl", largeSource)
	})
	assertAllocationBudget(t, "graph/rules=40", 63_000, func() {
		benchmarkGraph, err = Graph(largeSource)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func assertAllocationBudget(t *testing.T, name string, limit float64, operation func()) {
	t.Helper()
	got := testing.AllocsPerRun(10, operation)
	if got > limit {
		t.Errorf("%s allocations = %.0f, budget %.0f", name, got, limit)
	}
}

func BenchmarkCompile(b *testing.B) {
	benchmarkSources(b, func(b *testing.B, source string) {
		b.ReportAllocs()
		for b.Loop() {
			compiled, err := Compile(source)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkCompiled = compiled
		}
	})
}

func BenchmarkEvaluate(b *testing.B) {
	representativeSource, representativeFacts, at := representativeInputs(b)
	largeSource, largeFacts := largeBenchmarkInputs(40, at)
	for _, workload := range []struct {
		name   string
		source string
		facts  Facts
	}{
		{name: "representative", source: representativeSource, facts: representativeFacts},
		{name: "rules=40", source: largeSource, facts: largeFacts},
	} {
		b.Run(workload.name, func(b *testing.B) {
			compiled, err := Compile(workload.source)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkEvaluation = compiled.EvaluateAt(workload.facts, at)
			}
			if benchmarkEvaluation.Result != Authorized {
				b.Fatalf("result = %s, want %s", benchmarkEvaluation.Result, Authorized)
			}
		})
	}
}

func BenchmarkEvaluateParallel(b *testing.B) {
	source, facts, at := representativeInputs(b)
	compiled, err := Compile(source)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var failed atomic.Bool
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			evaluation := compiled.EvaluateAt(facts, at)
			if evaluation.Result != Authorized {
				failed.Store(true)
			}
		}
	})
	if failed.Load() {
		b.Fatalf("evaluation result was not %s", Authorized)
	}
}

func BenchmarkLint(b *testing.B) {
	benchmarkSources(b, func(b *testing.B, source string) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkLint = Lint("benchmark.crl", source)
		}
		if !benchmarkLint.OK {
			b.Fatalf("lint failed: %+v", benchmarkLint.Diagnostics)
		}
	})
}

func BenchmarkGraph(b *testing.B) {
	source := largeBenchmarkSource(40)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		graph, err := Graph(source)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkGraph = graph
	}
	if len(benchmarkGraph.Graph) == 0 || len(benchmarkGraph.Layout) == 0 {
		b.Fatal("empty graph result")
	}
}

func benchmarkSources(b *testing.B, benchmark func(*testing.B, string)) {
	b.Helper()
	representative, _, _ := representativeInputs(b)
	for _, workload := range []struct {
		name   string
		source string
	}{
		{name: "representative", source: representative},
		{name: "rules=40", source: largeBenchmarkSource(40)},
	} {
		b.Run(workload.name, func(b *testing.B) {
			b.ReportMetric(float64(len(workload.source)), "source-B")
			benchmark(b, workload.source)
		})
	}
}

func representativeInputs(t testing.TB) (string, Facts, time.Time) {
	t.Helper()
	source, err := os.ReadFile("examples/permit_quorum_2of3.crl")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("examples/facts/permit_quorum_2of3.authorized.json")
	if err != nil {
		t.Fatal(err)
	}
	var facts Facts
	if err := json.Unmarshal(body, &facts); err != nil {
		t.Fatal(err)
	}
	return string(source), facts, time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
}

func largeBenchmarkSource(ruleCount int) string {
	var source strings.Builder
	source.WriteString("crl v1\npackage benchmarks\nbundle large\n")
	names := make([]string, 0, ruleCount)
	for index := range ruleCount {
		name := fmt.Sprintf("rule_%d", index)
		names = append(names, name)
		fmt.Fprintf(&source, "\nrule %s\n", name)
		fmt.Fprintf(&source, "\ttarget benchmark.%d\n", index)
		fmt.Fprintf(&source, "\tcollector collector_%d benchmark api from /evidence/%d.json\n", index, index)
		fmt.Fprintf(&source, "\t\tsignal value_%d number from value ttl 30d\n", index)
		fmt.Fprintf(&source, "\t\tsignal enabled_%d bool from enabled ttl 30d\n", index)
		fmt.Fprintf(&source, "\tneed value_%d >= 1\n", index)
		fmt.Fprintf(&source, "\tneed enabled_%d == true\n", index)
		fmt.Fprintf(&source, "\tquorum collector_%d\n", index)
	}
	fmt.Fprintf(&source, "\ncluster all_rules\n\trules %s\n\tquorum %s\n", strings.Join(names, " + "), strings.Join(names, " & "))
	source.WriteString("\nneed all_rules == true\n")
	return source.String()
}

func largeBenchmarkInputs(ruleCount int, at time.Time) (string, Facts) {
	facts := make(Facts, ruleCount*5)
	observedAt := at.Add(-time.Hour).Format(time.RFC3339)
	for index := range ruleCount {
		facts[fmt.Sprintf("collector_%d", index)] = true
		facts[fmt.Sprintf("value_%d", index)] = float64(1)
		facts[fmt.Sprintf("enabled_%d", index)] = true
		facts[fmt.Sprintf("observed_at.value_%d", index)] = observedAt
		facts[fmt.Sprintf("observed_at.enabled_%d", index)] = observedAt
	}
	return largeBenchmarkSource(ruleCount), facts
}
