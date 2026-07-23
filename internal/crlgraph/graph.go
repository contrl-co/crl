// Package crlgraph derives a deterministic node/edge graph from a compiled CRL
// bundle. The graph is a pure projection of crl.Bundle: the same bundle always
// yields the same nodes, edges, and IDs, so the result can be hashed and
// golden-tested the way the compiled rule hash is.
//
// This is the backend half of a "disassembler model" for visual tooling
// (crlc graph): the Go core builds the graph (here), a later layout pass
// positions it, and a renderer only draws the result. Node IDs are structural (derived from rule,
// collector, signal, predicate position) rather than random, so a node keeps
// its identity across edits — the property that lets the diagram be a
// deterministic function of the source instead of a co-equal source of truth.
package crlgraph

import (
	"fmt"
	"strconv"
	"strings"

	"gitlab.com/contrl-group/crl/internal/crl"
	"gitlab.com/contrl-group/crl/internal/crypto"
)

// NodeKind is the CRL entity a node represents.
type NodeKind string

const (
	NodeRule      NodeKind = "rule"
	NodeCollector NodeKind = "collector"
	NodeSignal    NodeKind = "signal"
	NodePredicate NodeKind = "predicate"
	NodeCluster   NodeKind = "cluster"
)

// EdgeKind is the relationship a (visual) edge represents. Containment
// (rule▷collector▷signal, rule/cluster▷predicate) is expressed structurally via
// Node.Parent, not as an edge; edges carry only cross-references, the CRL analog
// of control-flow/data-flow edges in a disassembler's CFG.
type EdgeKind string

const (
	// EdgeReference is a need/block predicate pointing at the signal, rule, or
	// cluster its field reads.
	EdgeReference EdgeKind = "reference"
	// EdgeQuorum is a quorum predicate pointing at one of its subjects
	// (collector, signal, rule, or cluster).
	EdgeQuorum EdgeKind = "quorum"
	// EdgeMember is a cluster pointing at one of its member rules.
	EdgeMember EdgeKind = "member"
)

// Node is one entity in the derived graph.
type Node struct {
	ID     string            `json:"id"`
	Kind   NodeKind          `json:"kind"`
	Label  string            `json:"label"`
	Parent string            `json:"parent,omitempty"`
	Data   map[string]string `json:"data,omitempty"`
}

// Edge is one cross-reference between nodes.
type Edge struct {
	ID     string   `json:"id"`
	Source string   `json:"source"`
	Target string   `json:"target"`
	Kind   EdgeKind `json:"kind"`
}

// Graph is the full derived projection of a CRL bundle.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Hash returns the canonical-JSON digest of the graph. Same bundle → same graph
// → same hash, mirroring the compiled-rule hash contract.
func (g Graph) Hash() (string, error) { return crypto.Digest(g) }

// Build projects a compiled (normalized) crl.Bundle into a Graph. It expects the
// bundle to already be normalized (as returned by crl.CompileBundle); identifiers
// are assumed lowercased and references resolved.
func Build(bundle crl.Bundle) Graph {
	b := &builder{
		signalByName:    map[string]string{},
		collectorByName: map[string]string{},
		ruleByName:      map[string]string{},
		clusterByName:   map[string]string{},
	}

	// Pass 1: structural nodes (rule ▷ collector ▷ signal, cluster) and the
	// name→id indexes used to resolve references in pass 2.
	for _, rule := range bundle.Rules {
		rid := ruleID(rule.Name)
		b.ruleByName[rule.Name] = rid
		b.addNode(Node{ID: rid, Kind: NodeRule, Label: rule.Name, Data: map[string]string{"target": rule.Target}})
		for _, col := range rule.Collectors {
			cid := collectorID(rule.Name, col.Name)
			if _, ok := b.collectorByName[col.Name]; !ok {
				b.collectorByName[col.Name] = cid
			}
			b.addNode(Node{ID: cid, Kind: NodeCollector, Label: col.Name, Parent: rid, Data: map[string]string{
				"provider_type":  col.ProviderType,
				"connector_kind": col.ConnectorKind,
				"source":         col.Source,
			}})
			for _, sig := range col.Signals {
				sid := signalID(rule.Name, col.Name, sig.Name)
				if _, ok := b.signalByName[sig.Name]; !ok {
					b.signalByName[sig.Name] = sid
				}
				b.addNode(Node{ID: sid, Kind: NodeSignal, Label: sig.Name, Parent: cid, Data: map[string]string{
					"kind":         sig.Kind,
					"source_field": sig.SourceField,
					"expiry":       renderExpiry(sig.Expiry),
				}})
			}
		}
	}
	for _, cluster := range bundle.Clusters {
		clid := clusterID(cluster.Name)
		b.clusterByName[cluster.Name] = clid
		b.addNode(Node{ID: clid, Kind: NodeCluster, Label: cluster.Name, Data: map[string]string{"rules": strings.Join(cluster.Rules, ", ")}})
	}

	// Pass 2: predicate nodes + reference/quorum edges, and cluster membership.
	for _, rule := range bundle.Rules {
		rid := ruleID(rule.Name)
		for i, p := range rule.Predicates {
			b.addPredicate(predicateID("rule", rule.Name, i), rid, "rule", p)
		}
	}
	for _, cluster := range bundle.Clusters {
		clid := clusterID(cluster.Name)
		for _, member := range cluster.Rules {
			if rid, ok := b.ruleByName[member]; ok {
				b.addEdge(clid, rid, EdgeMember)
			}
		}
		for i, p := range cluster.Predicates {
			b.addPredicate(predicateID("cluster", cluster.Name, i), clid, "cluster", p)
		}
	}
	for i, p := range bundle.Predicates {
		b.addPredicate(predicateID("global", "", i), "", "global", p)
	}

	return Graph{Nodes: b.nodes, Edges: b.edges}
}

type builder struct {
	nodes   []Node
	edges   []Edge
	edgeSeq int

	signalByName    map[string]string
	collectorByName map[string]string
	ruleByName      map[string]string
	clusterByName   map[string]string
}

func (b *builder) addNode(n Node) { b.nodes = append(b.nodes, n) }

func (b *builder) addEdge(source, target string, kind EdgeKind) {
	b.edges = append(b.edges, Edge{
		ID:     fmt.Sprintf("e%d", b.edgeSeq),
		Source: source,
		Target: target,
		Kind:   kind,
	})
	b.edgeSeq++
}

func (b *builder) addPredicate(id, parent, scope string, p crl.Predicate) {
	b.addNode(Node{ID: id, Kind: NodePredicate, Label: predicateLabel(p), Parent: parent, Data: map[string]string{
		"scope":          scope,
		"predicate_kind": p.Kind,
	}})
	switch p.Kind {
	case crl.PredicateNeed, crl.PredicateBlock:
		if target, ok := b.resolveField(scope, p.Field); ok {
			b.addEdge(id, target, EdgeReference)
		}
	case crl.PredicateQuorum:
		subjects := p.Providers
		if p.Expression != nil {
			subjects = crl.QuorumExpressionSubjects(p.Expression)
		}
		for _, subject := range subjects {
			if target, ok := b.resolveSubject(subject); ok {
				b.addEdge(id, target, EdgeQuorum)
			}
		}
	}
}

// resolveField maps a need/block field to a node, mirroring the evaluator's
// flat-fact precedence so the drawn edge matches the dependency the evaluator
// actually reads. The evaluator writes signal facts first, then rule outcomes,
// then cluster outcomes, so the working key resolves cluster > rule > signal.
// Rule-scope fields can only be signals (the grammar forbids rule/cluster refs
// there); cluster-scope fields prefer a rule outcome over a signal; global-scope
// fields prefer a cluster, then a rule, then a signal. A kernel-derived field such
// as min_provider_trust has no node and resolves to false (no edge).
func (b *builder) resolveField(scope, name string) (string, bool) {
	switch scope {
	case "cluster":
		if id, ok := b.ruleByName[name]; ok {
			return id, true
		}
	case "global":
		if id, ok := b.clusterByName[name]; ok {
			return id, true
		}
		if id, ok := b.ruleByName[name]; ok {
			return id, true
		}
	}
	if id, ok := b.signalByName[name]; ok {
		return id, true
	}
	return "", false
}

// resolveSubject maps a quorum subject to a node. Quorum subjects are most often
// collectors (provider presence), then signals, rules, or clusters.
func (b *builder) resolveSubject(name string) (string, bool) {
	if id, ok := b.collectorByName[name]; ok {
		return id, true
	}
	if id, ok := b.signalByName[name]; ok {
		return id, true
	}
	if id, ok := b.ruleByName[name]; ok {
		return id, true
	}
	if id, ok := b.clusterByName[name]; ok {
		return id, true
	}
	return "", false
}

func ruleID(name string) string    { return "rule:" + name }
func clusterID(name string) string { return "cluster:" + name }

func collectorID(rule, name string) string { return "col:" + rule + "/" + name }

func signalID(rule, collector, name string) string {
	return "sig:" + rule + "/" + collector + "/" + name
}

func predicateID(scope, owner string, index int) string {
	if owner == "" {
		return "pred:" + scope + "/" + strconv.Itoa(index)
	}
	return "pred:" + scope + "/" + owner + "/" + strconv.Itoa(index)
}

func predicateLabel(p crl.Predicate) string {
	switch p.Kind {
	case crl.PredicateNeed:
		return fmt.Sprintf("need %s %s %s", p.Field, p.Operator, renderValue(p.Value))
	case crl.PredicateBlock:
		return "block " + p.Field
	case crl.PredicateQuorum:
		if p.Expression != nil {
			return "quorum " + crl.RenderQuorumExpression(p.Expression)
		}
		return fmt.Sprintf("quorum count(%s) >= %d", strings.Join(p.Providers, ", "), int(p.Value.Number))
	}
	return p.Kind
}

func renderValue(v crl.Value) string {
	switch v.Kind {
	case "bool":
		if v.Bool {
			return "true"
		}
		return "false"
	case "string":
		return strconv.Quote(v.String)
	case "number":
		return strconv.FormatFloat(v.Number, 'f', -1, 64)
	}
	return ""
}

func renderExpiry(e crl.SignalExpiry) string {
	if e.Literal != "" {
		if e.Mode != "" {
			return e.Mode + " " + e.Literal
		}
		return e.Literal
	}
	if e.Seconds > 0 {
		return fmt.Sprintf("%s %ds", e.Mode, e.Seconds)
	}
	return e.Mode
}
