package crl

import "fmt"

type ProofKind string

const (
	ProofComparison ProofKind = "comparison"
	ProofBlock      ProofKind = "block"
	ProofQuorum     ProofKind = "quorum"
	ProofTemporal   ProofKind = "temporal"
)

type IRProgram struct {
	Version     string            `json:"version"`
	Name        string            `json:"name,omitempty"`
	Package     string            `json:"package,omitempty"`
	Symbols     []Symbol          `json:"symbols"`
	Rules       []IRRule          `json:"rules"`
	Clusters    []IRCluster       `json:"clusters,omitempty"`
	Global      []ProofObligation `json:"global,omitempty"`
	RuleTargets map[string]string `json:"rule_targets"`
}

type IRRule struct {
	Name        string            `json:"name"`
	Target      string            `json:"target"`
	Collectors  []IRCollector     `json:"collectors"`
	Obligations []ProofObligation `json:"obligations"`
}

type IRCollector struct {
	Name          string     `json:"name"`
	ProviderType  string     `json:"provider_type"`
	ConnectorKind string     `json:"connector_kind"`
	Source        string     `json:"source"`
	Schema        string     `json:"schema,omitempty"`
	Signals       []IRSignal `json:"signals"`
}

type IRSignal struct {
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	SourceField string       `json:"source_field"`
	Unit        string       `json:"unit,omitempty"`
	Optional    bool         `json:"optional,omitempty"`
	Expiry      SignalExpiry `json:"expiry"`
}

type IRCluster struct {
	Name        string            `json:"name"`
	Rules       []string          `json:"rules"`
	Obligations []ProofObligation `json:"obligations"`
}

type ProofObligation struct {
	ID         string              `json:"id"`
	Kind       ProofKind           `json:"kind"`
	Scope      string              `json:"scope"`
	Owner      string              `json:"owner,omitempty"`
	Field      string              `json:"field,omitempty"`
	Operator   string              `json:"operator,omitempty"`
	Value      Value               `json:"value,omitempty"`
	Providers  []string            `json:"providers,omitempty"`
	Expression string              `json:"expression,omitempty"`
	Subjects   []string            `json:"subjects,omitempty"`
	Temporal   *TemporalExpression `json:"temporal,omitempty"`
	Logic      map[string]string   `json:"logic,omitempty"`
}

func LowerBundle(bundle Bundle, semantic SemanticModel) IRProgram {
	ir := IRProgram{
		Version:     bundle.Version,
		Name:        bundle.Name,
		Package:     bundle.Package,
		Symbols:     append([]Symbol(nil), semantic.Symbols...),
		Rules:       make([]IRRule, 0, len(bundle.Rules)),
		Clusters:    make([]IRCluster, 0, len(bundle.Clusters)),
		Global:      lowerPredicates("global", "", bundle.Predicates),
		RuleTargets: copyStringMap(semantic.RuleTargets),
	}
	for _, rule := range bundle.Rules {
		ir.Rules = append(ir.Rules, IRRule{
			Name:        rule.Name,
			Target:      rule.Target,
			Collectors:  lowerCollectors(rule.Collectors),
			Obligations: lowerPredicates("rule", rule.Name, rule.Predicates),
		})
	}
	for _, cluster := range bundle.Clusters {
		ir.Clusters = append(ir.Clusters, IRCluster{
			Name:        cluster.Name,
			Rules:       append([]string(nil), cluster.Rules...),
			Obligations: lowerPredicates("cluster", cluster.Name, cluster.Predicates),
		})
	}
	return ir
}

func lowerCollectors(collectors []Collector) []IRCollector {
	out := make([]IRCollector, 0, len(collectors))
	for _, collector := range collectors {
		out = append(out, IRCollector{
			Name:          collector.Name,
			ProviderType:  collector.ProviderType,
			ConnectorKind: collector.ConnectorKind,
			Source:        collector.Source,
			Schema:        collector.Schema,
			Signals:       lowerSignals(collector.Signals),
		})
	}
	return out
}

func lowerSignals(signals []Signal) []IRSignal {
	out := make([]IRSignal, 0, len(signals))
	for _, signal := range signals {
		out = append(out, IRSignal(signal))
	}
	return out
}

func lowerPredicates(scope, owner string, predicates []Predicate) []ProofObligation {
	out := make([]ProofObligation, 0, len(predicates))
	for i, predicate := range predicates {
		obligation := ProofObligation{
			ID:       proofID(scope, owner, i),
			Scope:    scope,
			Owner:    owner,
			Field:    predicate.Field,
			Operator: predicate.Operator,
			Value:    predicate.Value,
		}
		switch predicate.Kind {
		case PredicateNeed:
			obligation.Kind = ProofComparison
		case PredicateBlock:
			obligation.Kind = ProofBlock
		case PredicateQuorum:
			obligation.Kind = ProofQuorum
			if predicate.Expression != nil {
				obligation.Expression = RenderQuorumExpression(predicate.Expression)
				obligation.Subjects = QuorumExpressionSubjects(predicate.Expression)
				obligation.Logic = map[string]string{
					"calculus": "finite_boolean_algebra",
					"truth":    "subject satisfaction in current CRL structure",
				}
			} else {
				obligation.Providers = append([]string(nil), predicate.Providers...)
				obligation.Logic = map[string]string{
					"calculus": "cardinality",
					"truth":    "at least n listed providers present",
				}
			}
		case PredicateTemporal:
			obligation.Kind = ProofTemporal
			if predicate.Temporal != nil {
				temporal := *predicate.Temporal
				obligation.Temporal = &temporal
				obligation.Logic = map[string]string{
					"calculus": "temporal_ordering",
					"truth":    "time comparison over normalized evidence timestamps",
				}
			}
		default:
			obligation.Kind = ProofKind(predicate.Kind)
		}
		out = append(out, obligation)
	}
	return out
}

func proofID(scope, owner string, index int) string {
	if owner == "" {
		owner = "bundle"
	}
	return fmt.Sprintf("%s.%s.%03d", scope, owner, index+1)
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
