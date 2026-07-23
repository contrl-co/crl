package crlgraph

import (
	"reflect"
	"testing"

	"gitlab.com/contrl-group/crl/internal/crl"
)

// fundingRuleSource is the single-rule worked example from docs/crl-language.md.
// It is written flat (no indentation); CompileBundle parses it as a one-rule bundle.
const fundingRuleSource = "rule funding_release\n" +
	"target funding\n" +
	"collector inspection inspection webhook from tower.inspection\n" +
	"signal inspection_confidence number from confidence ttl 30d\n" +
	"signal progress_percent number from progress ttl 30d\n" +
	"collector credential_review credential file_upload from tower.credentials\n" +
	"signal credential_approved bool from approved ttl 30d\n" +
	"signal credential_reference string from reference ttl 30d\n" +
	"signal active_blocker bool from blocker ttl 30d\n" +
	"need inspection_confidence >= 0.9\n" +
	"need progress_percent >= 80\n" +
	"need credential_approved == true\n" +
	"need credential_reference == \"CRD-001\"\n" +
	"block active_blocker\n" +
	"quorum inspection & credential_review\n"

// bundleSource is the multi-rule worked example from docs/crl-language.md, with a
// cluster and global predicates. It is indentation-structured.
const bundleSource = "crl v1\n" +
	"\n" +
	"rule application\n" +
	"\ttarget permit.application\n" +
	"\tcollector application_file municipality file_upload from /test-resources/application.json\n" +
	"\t\tsignal application_complete bool from application.complete ttl 30d\n" +
	"\t\tsignal permit_hold_active bool from permit.hold_active ttl 30d\n" +
	"\tneed application_complete == true\n" +
	"\tquorum application_file\n" +
	"\n" +
	"rule capital\n" +
	"\ttarget permit.capital\n" +
	"\tcollector capital_webhook finance webhook from /test-resources/capital.json\n" +
	"\t\tsignal committed_usd number from capital.committed_usd ttl 30d\n" +
	"\tcollector treasury_api municipality api_poll from /test-resources/treasury.json\n" +
	"\t\tsignal fee_escrowed bool from treasury.fee_escrowed ttl 30d\n" +
	"\tneed committed_usd >= 250000\n" +
	"\tneed fee_escrowed == true\n" +
	"\tquorum capital_webhook & treasury_api\n" +
	"\n" +
	"cluster permit_foundation\n" +
	"\trules application + capital\n" +
	"\tquorum application & capital\n" +
	"\n" +
	"need permit_foundation == true\n" +
	"quorum permit_foundation & !permit_hold_active\n"

func mustBuild(t *testing.T, source string) Graph {
	t.Helper()
	compiled, err := crl.CompileBundle(source)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	return Build(compiled.Program)
}

func nodeByID(g Graph, id string) (Node, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

func hasEdge(g Graph, source, target string, kind EdgeKind) bool {
	for _, e := range g.Edges {
		if e.Source == source && e.Target == target && e.Kind == kind {
			return true
		}
	}
	return false
}

// TestBuildStructuralIntegrity checks the graph is internally consistent: unique
// node IDs, every edge endpoint resolves to a node, and every nesting parent
// resolves to a node.
func TestBuildStructuralIntegrity(t *testing.T) {
	for name, source := range map[string]string{"fundingRule": fundingRuleSource, "bundle": bundleSource} {
		g := mustBuild(t, source)
		ids := map[string]struct{}{}
		for _, n := range g.Nodes {
			if _, dup := ids[n.ID]; dup {
				t.Errorf("%s: duplicate node id %q", name, n.ID)
			}
			ids[n.ID] = struct{}{}
		}
		for _, n := range g.Nodes {
			if n.Parent != "" {
				if _, ok := ids[n.Parent]; !ok {
					t.Errorf("%s: node %q has dangling parent %q", name, n.ID, n.Parent)
				}
			}
		}
		for _, e := range g.Edges {
			if _, ok := ids[e.Source]; !ok {
				t.Errorf("%s: edge %q has dangling source %q", name, e.ID, e.Source)
			}
			if _, ok := ids[e.Target]; !ok {
				t.Errorf("%s: edge %q has dangling target %q", name, e.ID, e.Target)
			}
		}
	}
}

// TestBuildSingleRule pins the expected structure of the funding_release example.
func TestBuildSingleRule(t *testing.T) {
	g := mustBuild(t, fundingRuleSource)

	if _, ok := nodeByID(g, "rule:funding_release"); !ok {
		t.Fatal("missing rule node rule:funding_release")
	}
	rule, _ := nodeByID(g, "rule:funding_release")
	if rule.Data["target"] != "funding" {
		t.Errorf("rule target = %q, want funding", rule.Data["target"])
	}

	col, ok := nodeByID(g, "col:funding_release/inspection")
	if !ok {
		t.Fatal("missing collector node col:funding_release/inspection")
	}
	if col.Parent != "rule:funding_release" {
		t.Errorf("collector parent = %q, want rule:funding_release", col.Parent)
	}

	sig, ok := nodeByID(g, "sig:funding_release/inspection/inspection_confidence")
	if !ok {
		t.Fatal("missing signal node sig:funding_release/inspection/inspection_confidence")
	}
	if sig.Parent != "col:funding_release/inspection" {
		t.Errorf("signal parent = %q, want col:funding_release/inspection", sig.Parent)
	}
	if sig.Data["kind"] != "number" {
		t.Errorf("signal kind = %q, want number", sig.Data["kind"])
	}

	// The quorum predicate "quorum inspection & credential_review" must produce
	// quorum edges to both collectors.
	var quorumPred string
	for _, n := range g.Nodes {
		if n.Kind == NodePredicate && n.Data["predicate_kind"] == crl.PredicateQuorum {
			quorumPred = n.ID
		}
	}
	if quorumPred == "" {
		t.Fatal("no quorum predicate node found")
	}
	if !hasEdge(g, quorumPred, "col:funding_release/inspection", EdgeQuorum) {
		t.Errorf("missing quorum edge %s -> col:funding_release/inspection", quorumPred)
	}
	if !hasEdge(g, quorumPred, "col:funding_release/credential_review", EdgeQuorum) {
		t.Errorf("missing quorum edge %s -> col:funding_release/credential_review", quorumPred)
	}

	// A need predicate over a signal must produce a reference edge to that signal.
	if !hasEdgeToSignal(g, "sig:funding_release/inspection/inspection_confidence") {
		t.Error("expected a reference edge to sig:funding_release/inspection/inspection_confidence")
	}
}

func hasEdgeToSignal(g Graph, signalID string) bool {
	for _, e := range g.Edges {
		if e.Target == signalID && e.Kind == EdgeReference {
			return true
		}
	}
	return false
}

// TestBuildBundleClusterAndGlobals pins cluster membership and global references.
func TestBuildBundleClusterAndGlobals(t *testing.T) {
	g := mustBuild(t, bundleSource)

	for _, id := range []string{"rule:application", "rule:capital", "cluster:permit_foundation"} {
		if _, ok := nodeByID(g, id); !ok {
			t.Fatalf("missing node %q", id)
		}
	}

	// Cluster membership edges.
	if !hasEdge(g, "cluster:permit_foundation", "rule:application", EdgeMember) {
		t.Error("missing member edge cluster:permit_foundation -> rule:application")
	}
	if !hasEdge(g, "cluster:permit_foundation", "rule:capital", EdgeMember) {
		t.Error("missing member edge cluster:permit_foundation -> rule:capital")
	}

	// Global predicate "need permit_foundation == true" must reference the cluster.
	var refToCluster bool
	for _, e := range g.Edges {
		if e.Target == "cluster:permit_foundation" && e.Kind == EdgeReference {
			refToCluster = true
		}
	}
	if !refToCluster {
		t.Error("expected a global reference edge to cluster:permit_foundation")
	}

	// Global quorum "permit_foundation & !permit_hold_active" must produce a
	// quorum edge to the cluster and to the permit_hold_active signal.
	if !hasEdge(g, "pred:global/1", "cluster:permit_foundation", EdgeQuorum) {
		t.Error("missing global quorum edge to cluster:permit_foundation")
	}
	if !hasEdge(g, "pred:global/1", "sig:application/application_file/permit_hold_active", EdgeQuorum) {
		t.Error("missing global quorum edge to sig:application/application_file/permit_hold_active")
	}
}

// TestBuildDeterministic asserts the projection is a pure function of the bundle:
// two builds are deeply equal and hash identically.
func TestBuildDeterministic(t *testing.T) {
	for _, source := range []string{fundingRuleSource, bundleSource} {
		compiled, err := crl.CompileBundle(source)
		if err != nil {
			t.Fatalf("CompileBundle: %v", err)
		}
		a := Build(compiled.Program)
		b := Build(compiled.Program)
		if !reflect.DeepEqual(a, b) {
			t.Error("two builds of the same bundle are not equal")
		}
		ha, err := a.Hash()
		if err != nil {
			t.Fatalf("Hash: %v", err)
		}
		hb, err := b.Hash()
		if err != nil {
			t.Fatalf("Hash: %v", err)
		}
		if ha != hb {
			t.Errorf("graph hash not stable: %s != %s", ha, hb)
		}
	}
}
