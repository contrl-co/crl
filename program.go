package crl

import (
	"bytes"
	"encoding/json"

	lang "github.com/contrl-co/crl/internal/crl"
)

// Predicate kinds, as they appear in a PredicateView.Kind. An embedder
// matches against these rather than hardcoding the strings.
const (
	PredicateNeed     = "need"
	PredicateBlock    = "block"
	PredicateQuorum   = "quorum"
	PredicateTemporal = "temporal"
)

// ProgramView is a read-only projection of a compiled bundle's logical
// structure: the rules, collectors, signals, predicates, clusters, and
// global final policy it declares. It is the stable, curated shape an
// embedder inspects to index or feature-extract a rule; the compiler's
// syntax tree, IR, and proof obligations stay internal and are not
// exposed. The projection is derived from the normalized bundle, so its
// identifiers are already lowercased and its collectors expanded.
type ProgramView struct {
	Package    string          `json:"package,omitempty"`
	Bundle     string          `json:"bundle,omitempty"`
	Rules      []RuleView      `json:"rules"`
	Clusters   []ClusterView   `json:"clusters,omitempty"`
	Predicates []PredicateView `json:"predicates,omitempty"`
}

// RuleView is one concrete rule in a ProgramView (abstract rules and
// constructors are already expanded away).
type RuleView struct {
	Name       string          `json:"name"`
	Target     string          `json:"target"`
	Collectors []CollectorView `json:"collectors"`
	Predicates []PredicateView `json:"predicates"`
}

// CollectorView is one evidence collector declared by a rule.
type CollectorView struct {
	Name          string       `json:"name"`
	ProviderType  string       `json:"provider_type"`
	ConnectorKind string       `json:"connector_kind"`
	Source        string       `json:"source"`
	Schema        string       `json:"schema,omitempty"`
	Signals       []SignalView `json:"signals"`
}

// SignalView is one typed fact a collector yields. Expiry is the
// rendered freshness clause (e.g. "ttl 30d").
type SignalView struct {
	Name        string        `json:"name"`
	Kind        string        `json:"kind"`
	SourceField string        `json:"source_field"`
	Unit        string        `json:"unit,omitempty"`
	Optional    bool          `json:"optional,omitempty"`
	Expiry      string        `json:"expiry"`
	Freshness   FreshnessView `json:"freshness"`
}

// FreshnessView carries normalized expiry without requiring consumers to parse CRL.
// Mode "ttl" uses Seconds; mode "at" uses an RFC3339 timestamp in At.
type FreshnessView struct {
	Mode    string `json:"mode"`
	Seconds int64  `json:"seconds,omitempty"`
	At      string `json:"at,omitempty"`
}

// ClusterView is one cluster: the member rules it composes and its own
// predicates.
type ClusterView struct {
	Name       string          `json:"name"`
	Rules      []string        `json:"rules"`
	Predicates []PredicateView `json:"predicates"`
}

// PredicateView is one proof obligation. Kind is one of the Predicate*
// constants. For a quorum, Providers holds the count-form subjects and
// QuorumExpression holds the rendered boolean form (only one is set).
type PredicateView struct {
	Kind             string     `json:"kind"`
	Reference        string     `json:"reference,omitempty"`
	Field            string     `json:"field,omitempty"`
	Operator         string     `json:"operator,omitempty"`
	Value            *ValueView `json:"value,omitempty"`
	Providers        []string   `json:"providers,omitempty"`
	QuorumExpression string     `json:"quorum_expression,omitempty"`
}

// ValueView is a literal a need compares against.
type ValueView struct {
	Kind   string  `json:"kind"`
	Bool   bool    `json:"bool,omitempty"`
	Number float64 `json:"number,omitempty"`
	String string  `json:"string,omitempty"`
}

// MarshalJSON keeps numeric zero explicit without adding a number to other literal kinds.
func (value ValueView) MarshalJSON() ([]byte, error) {
	type plain ValueView
	var number *float64
	if value.Kind == "number" {
		number = &value.Number
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(struct {
		plain
		Number *float64 `json:"number,omitempty"`
	}{plain: plain(value), Number: number})
	return output.Bytes(), err
}

// Program returns the read-only logical view of the compiled bundle.
func (c Compiled) Program() ProgramView {
	bundle := c.program.Program
	view := ProgramView{
		Package:    bundle.Package,
		Bundle:     bundle.Name,
		Predicates: predicateViews(bundle.Predicates),
	}
	for _, rule := range bundle.Rules {
		view.Rules = append(view.Rules, RuleView{
			Name:       rule.Name,
			Target:     rule.Target,
			Collectors: collectorViews(rule.Collectors),
			Predicates: predicateViews(rule.Predicates),
		})
	}
	for _, cluster := range bundle.Clusters {
		view.Clusters = append(view.Clusters, ClusterView{
			Name:       cluster.Name,
			Rules:      append([]string(nil), cluster.Rules...),
			Predicates: predicateViews(cluster.Predicates),
		})
	}
	return view
}

func collectorViews(collectors []lang.Collector) []CollectorView {
	out := make([]CollectorView, 0, len(collectors))
	for _, collector := range collectors {
		signals := make([]SignalView, 0, len(collector.Signals))
		for _, signal := range collector.Signals {
			freshness := FreshnessView{Mode: signal.Expiry.Mode, Seconds: signal.Expiry.Seconds}
			if signal.Expiry.Mode == "at" {
				freshness.At = signal.Expiry.Literal
			}
			signals = append(signals, SignalView{
				Name:        signal.Name,
				Kind:        signal.Kind,
				SourceField: signal.SourceField,
				Unit:        signal.Unit,
				Optional:    signal.Optional,
				Expiry:      renderExpiry(signal.Expiry),
				Freshness:   freshness,
			})
		}
		out = append(out, CollectorView{
			Name:          collector.Name,
			ProviderType:  collector.ProviderType,
			ConnectorKind: collector.ConnectorKind,
			Source:        collector.Source,
			Schema:        collector.Schema,
			Signals:       signals,
		})
	}
	return out
}

func predicateViews(predicates []lang.Predicate) []PredicateView {
	out := make([]PredicateView, 0, len(predicates))
	for _, predicate := range predicates {
		view := PredicateView{
			Kind:      predicate.Kind,
			Reference: predicate.Reference,
			Field:     predicate.Field,
			Operator:  predicate.Operator,
			Providers: append([]string(nil), predicate.Providers...),
		}
		if (predicate.Kind == PredicateNeed || predicate.Kind == PredicateBlock) && predicate.Reference == "" {
			view.Value = &ValueView{
				Kind:   predicate.Value.Kind,
				Bool:   predicate.Value.Bool,
				Number: predicate.Value.Number,
				String: predicate.Value.String,
			}
		}
		if predicate.Expression != nil {
			view.QuorumExpression = lang.RenderQuorumExpression(predicate.Expression)
		}
		out = append(out, view)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func renderExpiry(expiry lang.SignalExpiry) string {
	if expiry.Mode == "at" {
		return "expires " + expiry.Literal
	}
	if expiry.Mode == "" {
		return ""
	}
	return expiry.Mode + " " + expiry.Literal
}
