package crl

import (
	lang "github.com/contrl-co/crl/internal/crl"
	"github.com/contrl-co/crl/internal/crllint"
)

// Evaluation is the outcome of evaluating a compiled bundle against a
// set of facts: one of the five results, plus the full check trace
// that produced it.
type Evaluation struct {
	Result     Result         `json:"result"`
	Authorized bool           `json:"authorized"`
	Aspect     string         `json:"aspect"`
	Rules      []RuleTrace    `json:"rules,omitempty"`
	Clusters   []ClusterTrace `json:"clusters,omitempty"`
	Global     []Check        `json:"global_checks,omitempty"`
	Checks     []Check        `json:"checks,omitempty"`
}

// RuleTrace is the per-rule evaluation record.
type RuleTrace struct {
	Rule       string  `json:"rule_name"`
	Aspect     string  `json:"aspect"`
	Result     Result  `json:"result"`
	Authorized bool    `json:"authorized"`
	Checks     []Check `json:"checks,omitempty"`
}

// ClusterTrace is the per-cluster evaluation record.
type ClusterTrace struct {
	Cluster    string          `json:"cluster_name"`
	Result     Result          `json:"result"`
	Authorized bool            `json:"authorized"`
	Members    []ClusterMember `json:"members,omitempty"`
	Checks     []Check         `json:"checks,omitempty"`
}

// ClusterMember records how one member rule contributed to a cluster.
type ClusterMember struct {
	Rule       string `json:"rule_name"`
	Authorized bool   `json:"authorized"`
	Result     Result `json:"result"`
}

// Check is one evaluated predicate: what was required, what was
// observed, whether it passed, and — when it did not — why.
type Check struct {
	Kind              string             `json:"kind"`
	Rule              string             `json:"rule_name,omitempty"`
	Cluster           string             `json:"cluster_name,omitempty"`
	Scope             string             `json:"scope,omitempty"`
	Field             string             `json:"field,omitempty"`
	Operator          string             `json:"operator"`
	Expected          any                `json:"expected,omitempty"`
	Actual            any                `json:"actual,omitempty"`
	QuorumExpression  string             `json:"quorum_expression,omitempty"`
	TemporalRelation  string             `json:"temporal_relation,omitempty"`
	TemporalReference string             `json:"temporal_reference,omitempty"`
	TemporalWindow    string             `json:"temporal_window,omitempty"`
	Providers         []ProviderPresence `json:"providers,omitempty"`
	Passed            bool               `json:"passed"`
	Reason            string             `json:"reason,omitempty"`
}

// ProviderPresence records whether one quorum subject counted.
type ProviderPresence struct {
	Provider string `json:"provider"`
	Present  bool   `json:"present"`
}

// LintReport is the structured result of linting one source.
type LintReport struct {
	Path          string       `json:"path,omitempty"`
	OK            bool         `json:"ok"`
	Hash          string       `json:"compiled_hash,omitempty"`
	CanonicalText string       `json:"canonical_text,omitempty"`
	Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`
}

// Diagnostic is one lint finding, positioned in the source and labeled
// with a stable CRL### code.
type Diagnostic struct {
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func newEvaluation(in lang.BundleEvaluation) Evaluation {
	out := Evaluation{
		Result:     Result(in.Result),
		Authorized: in.Authorized,
		Aspect:     in.Aspect,
	}
	for _, rule := range in.RuleTraces {
		out.Rules = append(out.Rules, RuleTrace{
			Rule:       rule.RuleName,
			Aspect:     rule.Aspect,
			Result:     Result(rule.Result),
			Authorized: rule.Authorized,
			Checks:     newChecks(rule.Checks),
		})
	}
	for _, cluster := range in.ClusterTraces {
		trace := ClusterTrace{
			Cluster:    cluster.ClusterName,
			Result:     Result(cluster.Result),
			Authorized: cluster.Authorized,
			Checks:     newChecks(cluster.Checks),
		}
		for _, member := range cluster.Members {
			trace.Members = append(trace.Members, ClusterMember{
				Rule:       member.RuleName,
				Authorized: member.Authorized,
				Result:     Result(member.Result),
			})
		}
		out.Clusters = append(out.Clusters, trace)
	}
	out.Global = newChecks(in.GlobalChecks)
	out.Checks = newChecks(in.Checks)
	return out
}

func newChecks(in []lang.Check) []Check {
	out := make([]Check, 0, len(in))
	for _, check := range in {
		out = append(out, Check{
			Kind:              check.Kind,
			Rule:              check.RuleName,
			Cluster:           check.ClusterName,
			Scope:             check.Scope,
			Field:             check.Field,
			Operator:          check.Operator,
			Expected:          expectedValue(check.Expected),
			Actual:            check.Actual,
			QuorumExpression:  check.QuorumExpression,
			TemporalRelation:  check.TemporalRelation,
			TemporalReference: check.TemporalReference,
			TemporalWindow:    check.TemporalWindow,
			Providers:         newProviders(check.Providers),
			Passed:            check.Passed,
			Reason:            check.Reason,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func newProviders(in []lang.ProviderPresence) []ProviderPresence {
	if len(in) == 0 {
		return nil
	}
	out := make([]ProviderPresence, 0, len(in))
	for _, provider := range in {
		out = append(out, ProviderPresence{Provider: provider.Provider, Present: provider.Present})
	}
	return out
}

func expectedValue(value lang.Value) any {
	switch value.Kind {
	case "bool":
		return value.Bool
	case "number":
		return value.Number
	case "string", "time":
		return value.String
	default:
		return nil
	}
}

func newLintReport(in crllint.Report) LintReport {
	out := LintReport{
		Path:          in.Path,
		OK:            in.OK,
		Hash:          in.CompiledHash,
		CanonicalText: in.CanonicalText,
	}
	for _, diagnostic := range in.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{
			Path:     diagnostic.Path,
			Line:     diagnostic.Line,
			Column:   diagnostic.Column,
			Severity: string(diagnostic.Severity),
			Code:     diagnostic.Code,
			Message:  diagnostic.Message,
		})
	}
	return out
}
