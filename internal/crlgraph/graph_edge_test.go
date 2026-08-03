package crlgraph

import (
	"strings"
	"testing"

	"github.com/contrl-co/crl/internal/crl"
)

func build(t *testing.T, source string) Graph {
	t.Helper()
	compiled, err := crl.CompileBundle(source)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	return Build(compiled.Program)
}

func predicateNodeOfKind(g Graph, kind string) (Node, bool) {
	for _, n := range g.Nodes {
		if n.Kind == NodePredicate && n.Data["predicate_kind"] == kind {
			return n, true
		}
	}
	return Node{}, false
}

// TestBuildBlockEdge: a `block` predicate references its guarded signal.
func TestBuildBlockEdge(t *testing.T) {
	g := build(t, fundingRuleSource)
	block, ok := predicateNodeOfKind(g, crl.PredicateBlock)
	if !ok {
		t.Fatal("no block predicate node")
	}
	target := "sig:funding_release/credential_review/active_blocker"
	if !hasEdge(g, block.ID, target, EdgeReference) {
		t.Errorf("block predicate %s has no reference edge to %s", block.ID, target)
	}
}

// TestBuildCountQuorumEdges: a count-form quorum references each named provider.
func TestBuildCountQuorumEdges(t *testing.T) {
	source := "rule r\n" +
		"target asp\n" +
		"collector c1 p webhook from /c1.json\n" +
		"signal s1 number from f ttl 30d\n" +
		"collector c2 p webhook from /c2.json\n" +
		"signal s2 number from g ttl 30d\n" +
		"need s1 >= 1\n" +
		"quorum count(c1, c2) >= 2\n"
	g := build(t, source)
	q, ok := predicateNodeOfKind(g, crl.PredicateQuorum)
	if !ok {
		t.Fatal("no quorum predicate node")
	}
	for _, target := range []string{"col:r/c1", "col:r/c2"} {
		if !hasEdge(g, q.ID, target, EdgeQuorum) {
			t.Errorf("count quorum %s missing edge to %s", q.ID, target)
		}
	}
}

// TestBuildKernelFieldNoEdge: a need on the kernel-derived min_provider_trust
// field (which has no signal node) must not produce a dangling edge.
func TestBuildKernelFieldNoEdge(t *testing.T) {
	source := "rule r\n" +
		"target asp\n" +
		"collector c p webhook from /c.json\n" +
		"signal s number from f ttl 30d\n" +
		"need s >= 1\n" +
		"need min_provider_trust >= 0.5\n" +
		"quorum c\n"
	g := build(t, source)
	// the min_provider_trust predicate is the need whose label names it
	var kernelPred string
	for _, n := range g.Nodes {
		if n.Kind == NodePredicate && n.Data["predicate_kind"] == crl.PredicateNeed && strings.Contains(n.Label, "min_provider_trust") {
			kernelPred = n.ID
		}
	}
	if kernelPred == "" {
		t.Fatal("no min_provider_trust predicate node")
	}
	for _, e := range g.Edges {
		if e.Source == kernelPred {
			t.Errorf("min_provider_trust predicate %s has an unexpected outgoing edge to %s", kernelPred, e.Target)
		}
	}
}

// TestBuildGlobalNeedReferencesCluster pins edge resolution by scope: a GLOBAL
// `need <name>` edge points at the cluster (whose outcome the evaluator reads),
// while a rule-scope `need <name>` points at the signal. A signal and a cluster
// can no longer share a name — the CRL compiler rejects an ambiguous symbol used
// as both (internal/crl/sema.go) — so the two needs use distinct names here.
func TestBuildGlobalNeedReferencesCluster(t *testing.T) {
	source := "crl v1\n\n" +
		"rule r\n" +
		"\ttarget asp\n" +
		"\tcollector c p webhook from /c.json\n" +
		"\t\tsignal s bool from f ttl 30d\n" +
		"\tneed s == true\n" +
		"\tquorum c\n\n" +
		"cluster x\n" +
		"\trules r\n" +
		"\tquorum r\n\n" +
		"need x == true\n"
	g := build(t, source)
	if _, ok := nodeByID(g, "cluster:x"); !ok {
		t.Fatal("expected a cluster:x node")
	}
	// global predicate index 0 references the cluster (evaluator precedence)
	if !hasEdge(g, "pred:global/0", "cluster:x", EdgeReference) {
		t.Error("global need x should reference cluster:x")
	}
	// the RULE-scope `need s` references the signal (rule fields are signals)
	if !hasEdge(g, "pred:rule/r/0", "sig:r/c/s", EdgeReference) {
		t.Error("rule-scope need s should reference the signal")
	}
}

// TestLayoutTerminatesOnParentCycle: the exported Layout must not hang on an
// adversarial graph whose Parent pointers form a cycle (Build never emits one).
func TestLayoutTerminatesOnParentCycle(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "top", Kind: NodeRule, Label: "top"},
			{ID: "b", Kind: NodeCollector, Label: "b", Parent: "c"},
			{ID: "c", Kind: NodeCollector, Label: "c", Parent: "b"},
		},
		Edges: []Edge{{ID: "e0", Source: "top", Target: "b", Kind: EdgeReference}},
	}
	out := Layout(g) // must return rather than spin forever
	if len(out.Nodes) != 3 {
		t.Errorf("expected 3 positioned nodes, got %d", len(out.Nodes))
	}
}
