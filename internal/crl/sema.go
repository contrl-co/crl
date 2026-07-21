package crl

import (
	"fmt"
	"sort"
	"time"
)

type SymbolKind string

const (
	SymbolRule       SymbolKind = "rule"
	SymbolCluster    SymbolKind = "cluster"
	SymbolCollector  SymbolKind = "collector"
	SymbolSignal     SymbolKind = "signal"
	SymbolKernelFact SymbolKind = "kernel_fact"
)

type Symbol struct {
	Kind  SymbolKind `json:"kind"`
	Name  string     `json:"name"`
	Type  string     `json:"type"`
	Scope string     `json:"scope"`
	Owner string     `json:"owner,omitempty"`
}

type SemanticModel struct {
	Symbols        []Symbol          `json:"symbols"`
	RuleTargets    map[string]string `json:"rule_targets"`
	SignalKinds    map[string]string `json:"signal_kinds"`
	CollectorNames []string          `json:"collector_names"`
	ClusterNames   []string          `json:"cluster_names"`
}

func AnalyzeDocument(document Document) (SemanticModel, error) {
	return AnalyzeBundle(document.Bundle())
}

func AnalyzeBundle(bundle Bundle) (SemanticModel, error) {
	normalized, err := normalizeBundle(bundle)
	if err != nil {
		return SemanticModel{}, err
	}
	return analyzeNormalizedBundle(normalized)
}

func analyzeNormalizedBundle(normalized Bundle) (SemanticModel, error) {
	if err := validateFinalPolicyReachability(normalized); err != nil {
		return SemanticModel{}, err
	}
	if err := validateFinalPolicyMonotone(normalized); err != nil {
		return SemanticModel{}, err
	}
	if err := validateSignalExpiryConsistency(normalized); err != nil {
		return SemanticModel{}, err
	}
	builder := semanticBuilder{
		model: SemanticModel{
			RuleTargets: map[string]string{},
			SignalKinds: map[string]string{},
		},
		byName: map[string]Symbol{},
	}
	for _, symbol := range kernelSymbols() {
		if err := builder.add(symbol); err != nil {
			return SemanticModel{}, err
		}
	}
	for _, rule := range normalized.Rules {
		if err := builder.add(Symbol{Kind: SymbolRule, Name: rule.Name, Type: "bool", Scope: "bundle"}); err != nil {
			return SemanticModel{}, err
		}
		builder.model.RuleTargets[rule.Name] = rule.Target
		for _, collector := range rule.Collectors {
			if err := builder.add(Symbol{Kind: SymbolCollector, Name: collector.Name, Type: "bool", Scope: "rule", Owner: rule.Name}); err != nil {
				return SemanticModel{}, err
			}
			builder.model.CollectorNames = append(builder.model.CollectorNames, collector.Name)
			for _, signal := range collector.Signals {
				if err := builder.add(Symbol{Kind: SymbolSignal, Name: signal.Name, Type: signal.Kind, Scope: "collector", Owner: collector.Name}); err != nil {
					return SemanticModel{}, err
				}
				builder.model.SignalKinds[signal.Name] = signal.Kind
			}
		}
	}
	for _, cluster := range normalized.Clusters {
		if err := builder.add(Symbol{Kind: SymbolCluster, Name: cluster.Name, Type: "bool", Scope: "bundle"}); err != nil {
			return SemanticModel{}, err
		}
		builder.model.ClusterNames = append(builder.model.ClusterNames, cluster.Name)
	}
	sort.Slice(builder.model.Symbols, func(i, j int) bool {
		left, right := builder.model.Symbols[i], builder.model.Symbols[j]
		if left.Name == right.Name {
			if left.Kind == right.Kind {
				return left.Owner < right.Owner
			}
			return left.Kind < right.Kind
		}
		return left.Name < right.Name
	})
	sort.Strings(builder.model.CollectorNames)
	sort.Strings(builder.model.ClusterNames)
	return builder.model, nil
}

type semanticBuilder struct {
	model  SemanticModel
	byName map[string]Symbol
}

func (b *semanticBuilder) add(symbol Symbol) error {
	if existing, ok := b.byName[symbol.Name]; ok {
		if compatibleSymbol(existing, symbol) {
			b.model.Symbols = append(b.model.Symbols, symbol)
			return nil
		}
		return fmt.Errorf("%w: ambiguous symbol %q used as %s and %s", ErrInvalidSyntax, symbol.Name, existing.Kind, symbol.Kind)
	}
	b.byName[symbol.Name] = symbol
	b.model.Symbols = append(b.model.Symbols, symbol)
	return nil
}

func compatibleSymbol(existing, next Symbol) bool {
	return existing.Kind == SymbolSignal && next.Kind == SymbolSignal && existing.Type == next.Type
}

// validateSignalExpiryConsistency rejects a bundle that declares the
// same signal name with DIFFERENT expiry contracts. A signal name maps
// to a single fact (facts["<name>"] and observed_at.<name>), so a
// second declaration with a different ttl/expires is incoherent: at
// evaluation the name-keyed signal index can only hold one expiry, and
// silently resolving to a sibling's looser ttl would let stale evidence
// satisfy a stricter rule (or a stricter sibling would wrongly expire a
// legitimately-fresh value). Same-named signals may repeat only with an
// identical expiry.
func validateSignalExpiryConsistency(bundle Bundle) error {
	seen := map[string]SignalExpiry{}
	check := func(signal Signal) error {
		prior, ok := seen[signal.Name]
		if !ok {
			seen[signal.Name] = signal.Expiry
			return nil
		}
		if prior != signal.Expiry {
			return fmt.Errorf("%w: signal %q declared with conflicting expiry (%s vs %s); the same signal name must share one freshness contract",
				ErrInvalidSyntax, signal.Name, renderSignalExpiry(prior), renderSignalExpiry(signal.Expiry))
		}
		return nil
	}
	for _, rule := range bundle.Rules {
		for _, collector := range rule.Collectors {
			for _, signal := range collector.Signals {
				if err := check(signal); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func kernelSymbols() []Symbol {
	return []Symbol{
		{Kind: SymbolKernelFact, Name: "min_provider_trust", Type: "number", Scope: "bundle"},
	}
}

func validateFinalPolicyReachability(bundle Bundle) error {
	if len(bundle.Predicates) == 0 {
		return nil
	}
	// A final policy of only `block` predicates has no positive requirement:
	// `block ra` authorizes exactly when ra fails, so empty evidence (ra
	// unproven) authorizes the bundle. Require at least one need/quorum.
	allBlocks := true
	for _, predicate := range bundle.Predicates {
		if predicate.Kind != PredicateBlock {
			allBlocks = false
			break
		}
	}
	if allBlocks {
		return fmt.Errorf("%w: global final policy must require something (need or quorum); a block-only policy authorizes when its target has no evidence", ErrInvalidSyntax)
	}
	ruleNames := map[string]struct{}{}
	clusterByName := map[string]Cluster{}
	for _, rule := range bundle.Rules {
		ruleNames[rule.Name] = struct{}{}
	}
	for _, cluster := range bundle.Clusters {
		clusterByName[cluster.Name] = cluster
	}
	referencedRules := map[string]struct{}{}
	referencedClusters := map[string]struct{}{}
	for _, predicate := range bundle.Predicates {
		for _, subject := range predicateSubjects(predicate) {
			if _, ok := ruleNames[subject]; ok {
				referencedRules[subject] = struct{}{}
			}
			if cluster, ok := clusterByName[subject]; ok {
				referencedClusters[subject] = struct{}{}
				for _, rule := range cluster.Rules {
					referencedRules[rule] = struct{}{}
				}
			}
		}
	}
	if len(referencedRules) == 0 && len(referencedClusters) == 0 {
		return fmt.Errorf("%w: global predicates must reference at least one rule or cluster", ErrInvalidSyntax)
	}
	for _, rule := range bundle.Rules {
		if _, ok := referencedRules[rule.Name]; !ok {
			return fmt.Errorf("%w: rule %q is not reachable from global final policy", ErrInvalidSyntax, rule.Name)
		}
	}
	for _, cluster := range bundle.Clusters {
		if _, ok := referencedClusters[cluster.Name]; !ok {
			return fmt.Errorf("%w: cluster %q is not reachable from global final policy", ErrInvalidSyntax, cluster.Name)
		}
	}
	return nil
}

// finalPolicyProbeClock is a fixed, non-zero evaluation instant used only
// to test a final policy against empty evidence at compile time. It must
// be a constant (never time.Now) so compilation stays deterministic.
var finalPolicyProbeClock = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// validateFinalPolicyMonotone rejects a global final policy that would
// authorize a bundle carrying no evidence. Under fail-closed semantics no
// rule or cluster can authorize on empty facts, so a sound final policy
// must also withhold there. A policy that authorizes anyway is inverting
// — it gates on a rule or cluster FAILING (`quorum not r`, `block r`,
// `need r == false`) — which authorizes a decision with no evidence at
// all. Probing with empty facts catches every such spelling, and subsumes
// the block-only special case above.
func validateFinalPolicyMonotone(bundle Bundle) error {
	if len(bundle.Predicates) == 0 {
		return nil
	}
	probe := EvaluateBundleAt(CompiledBundle{Program: bundle}, Facts{}, finalPolicyProbeClock)
	if probe.Authorized {
		return fmt.Errorf("%w: global final policy authorizes with no evidence; it must require positive proof (a rule, cluster, or signal holding), not a rule or cluster failing", ErrInvalidSyntax)
	}
	return nil
}

func predicateSubjects(predicate Predicate) []string {
	switch predicate.Kind {
	case PredicateNeed, PredicateBlock:
		if predicate.Field == "" {
			return nil
		}
		return []string{predicate.Field}
	case PredicateQuorum:
		if predicate.Expression != nil {
			return QuorumExpressionSubjects(predicate.Expression)
		}
		return append([]string(nil), predicate.Providers...)
	default:
		return nil
	}
}
