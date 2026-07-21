package crl

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gitlab.com/contrl-group/crl/internal/crypto"
)

type Bundle struct {
	Version    string      `json:"version"`
	Name       string      `json:"name,omitempty"`
	Package    string      `json:"package,omitempty"`
	Rules      []Rule      `json:"rules"`
	Clusters   []Cluster   `json:"clusters,omitempty"`
	Predicates []Predicate `json:"predicates,omitempty"`
}

type Rule struct {
	Name       string      `json:"name"`
	Target     string      `json:"target"`
	Collectors []Collector `json:"collectors"`
	Predicates []Predicate `json:"predicates,omitempty"`
}

type Cluster struct {
	Name       string      `json:"name"`
	Rules      []string    `json:"rules"`
	Predicates []Predicate `json:"predicates,omitempty"`
}

type CompiledBundle struct {
	Program       Bundle `json:"program"`
	CanonicalText string `json:"canonical_text"`
	Hash          string `json:"hash"`
}

type BundleEvaluation struct {
	Authorized    bool           `json:"authorized"`
	Result        string         `json:"result"`
	Aspect        string         `json:"aspect"`
	RuleTraces    []RuleTrace    `json:"rules"`
	ClusterTraces []ClusterTrace `json:"clusters"`
	GlobalChecks  []Check        `json:"global_checks"`
	Checks        []Check        `json:"checks"`
}

type RuleTrace struct {
	RuleName   string  `json:"rule_name"`
	Aspect     string  `json:"aspect"`
	Result     string  `json:"result"`
	Authorized bool    `json:"authorized"`
	Checks     []Check `json:"checks"`
}

type ClusterTrace struct {
	ClusterName string          `json:"cluster_name"`
	Result      string          `json:"result"`
	Authorized  bool            `json:"authorized"`
	Members     []ClusterMember `json:"members"`
	Checks      []Check         `json:"checks"`
}

type ClusterMember struct {
	RuleName   string `json:"rule_name"`
	Authorized bool   `json:"authorized"`
	Result     string `json:"result"`
}

func CompileBundle(source string) (CompiledBundle, error) {
	compilation, err := CompileLanguage(source)
	if err != nil {
		return CompiledBundle{}, err
	}
	return CompiledBundle{
		Program:       compilation.Bundle,
		CanonicalText: compilation.CanonicalText,
		Hash:          compilation.Hash,
	}, nil
}

func CompileBundleProgram(bundle Bundle) (CompiledBundle, error) {
	normalized, err := normalizeBundle(bundle)
	if err != nil {
		return CompiledBundle{}, err
	}
	if _, err := analyzeNormalizedBundle(normalized); err != nil {
		return CompiledBundle{}, err
	}
	hash, err := crypto.Digest(normalized)
	if err != nil {
		return CompiledBundle{}, err
	}
	return CompiledBundle{
		Program:       normalized,
		CanonicalText: canonicalBundleText(normalized),
		Hash:          hash,
	}, nil
}

// EvaluateBundle evaluates without a clock and therefore FAILS CLOSED
// on every time-dependent rule: signals that declare a ttl/expires
// evaluate as EXPIRED, and temporal predicates report unknown-fact.
// Any caller that wants freshness or temporal rules to be genuinely
// evaluated must use EvaluateBundleAt with an explicit clock — and
// record that clock, because the decision is a function of it.
func EvaluateBundle(compiled CompiledBundle, facts Facts) BundleEvaluation {
	return EvaluateBundleAt(compiled, facts, time.Time{})
}

func EvaluateBundleAt(compiled CompiledBundle, facts Facts, now time.Time) BundleEvaluation {
	working := copyFacts(facts)
	var ruleTraces []RuleTrace
	var clusterTraces []ClusterTrace
	var globalChecks []Check
	var checks []Check

	// The bundle-wide signal index is used for EVERY scope. The
	// compiler validates rule predicates against the bundle-wide signal
	// set (a rule may reference a signal declared in another rule's
	// collector), so rule evaluation must resolve expiry against the
	// same set — a rule-local index would miss cross-rule references
	// and silently skip the freshness check (fail open).
	allSignals := bundleSignalIndex(compiled.Program)

	ruleByName := make(map[string]RuleTrace, len(compiled.Program.Rules))
	for _, rule := range compiled.Program.Rules {
		trace := evaluateBundleRule(rule, working, allSignals, now)
		ruleTraces = append(ruleTraces, trace)
		ruleByName[trace.RuleName] = trace
		working[trace.RuleName] = trace.Authorized
		working["rule."+trace.RuleName] = trace.Authorized
		checks = append(checks, trace.Checks...)
	}

	for _, cluster := range compiled.Program.Clusters {
		trace := evaluateBundleCluster(cluster, working, allSignals, now, ruleByName)
		clusterTraces = append(clusterTraces, trace)
		working[trace.ClusterName] = trace.Authorized
		working["cluster."+trace.ClusterName] = trace.Authorized
		checks = append(checks, trace.Checks...)
	}

	for _, predicate := range compiled.Program.Predicates {
		check := evaluatePredicate(predicate, working, allSignals, now)
		check.Scope = "global"
		globalChecks = append(globalChecks, check)
		checks = append(checks, check)
	}

	authorized := bundleAuthorized(ruleTraces, clusterTraces, globalChecks, len(compiled.Program.Predicates) > 0)
	result := bundleResult(authorized, checks)
	return BundleEvaluation{
		Authorized:    authorized,
		Result:        result,
		Aspect:        bundleAspect(compiled.Program),
		RuleTraces:    ruleTraces,
		ClusterTraces: clusterTraces,
		GlobalChecks:  globalChecks,
		Checks:        checks,
	}
}

func normalizeBundle(bundle Bundle) (Bundle, error) {
	bundle.Version = strings.TrimSpace(bundle.Version)
	if bundle.Version == "" {
		bundle.Version = Version
	}
	if bundle.Version != Version {
		return Bundle{}, fmt.Errorf("%w: unsupported version %q", ErrInvalidSyntax, bundle.Version)
	}
	bundle.Name = normalizeIdentifier(bundle.Name)
	if bundle.Name != "" && !identifierPattern.MatchString(bundle.Name) {
		return Bundle{}, fmt.Errorf("%w: invalid bundle %q", ErrInvalidSyntax, bundle.Name)
	}
	bundle.Package = normalizeIdentifier(bundle.Package)
	if bundle.Package != "" && !identifierPattern.MatchString(bundle.Package) {
		return Bundle{}, fmt.Errorf("%w: invalid package %q", ErrInvalidSyntax, bundle.Package)
	}
	if len(bundle.Rules) == 0 {
		return Bundle{}, fmt.Errorf("%w: missing rule", ErrInvalidSyntax)
	}

	ruleNames := map[string]string{}
	clusterNames := map[string]string{}
	collectorNames := map[string]struct{}{}
	signalKinds := map[string]string{}

	for i := range bundle.Rules {
		rule, err := normalizeRule(bundle.Rules[i])
		if err != nil {
			return Bundle{}, err
		}
		if _, ok := ruleNames[rule.Name]; ok {
			return Bundle{}, fmt.Errorf("%w: duplicate rule %q", ErrInvalidSyntax, rule.Name)
		}
		ruleNames[rule.Name] = rule.Target
		for _, collector := range rule.Collectors {
			if _, ok := collectorNames[collector.Name]; ok {
				return Bundle{}, fmt.Errorf("%w: duplicate collector %q", ErrInvalidSyntax, collector.Name)
			}
			collectorNames[collector.Name] = struct{}{}
			for _, signal := range collector.Signals {
				if existing, ok := signalKinds[signal.Name]; ok && existing != signal.Kind {
					return Bundle{}, fmt.Errorf("%w: signal %q declared as both %q and %q", ErrInvalidSyntax, signal.Name, existing, signal.Kind)
				}
				signalKinds[signal.Name] = signal.Kind
			}
		}
		bundle.Rules[i] = rule
	}

	for i := range bundle.Clusters {
		cluster, err := normalizeCluster(bundle.Clusters[i], ruleNames)
		if err != nil {
			return Bundle{}, err
		}
		if _, ok := clusterNames[cluster.Name]; ok {
			return Bundle{}, fmt.Errorf("%w: duplicate cluster %q", ErrInvalidSyntax, cluster.Name)
		}
		clusterNames[cluster.Name] = cluster.Name
		bundle.Clusters[i] = cluster
	}

	for i := range bundle.Rules {
		for j := range bundle.Rules[i].Predicates {
			predicate, err := normalizePredicate(bundle.Rules[i].Predicates[j])
			if err != nil {
				return Bundle{}, err
			}
			if err := validateBundlePredicate(predicate, signalKinds, collectorNames, nil, nil); err != nil {
				return Bundle{}, err
			}
			bundle.Rules[i].Predicates[j] = predicate
		}
	}
	for i := range bundle.Clusters {
		for j := range bundle.Clusters[i].Predicates {
			predicate, err := normalizePredicate(bundle.Clusters[i].Predicates[j])
			if err != nil {
				return Bundle{}, err
			}
			if err := validateBundlePredicate(predicate, signalKinds, collectorNames, ruleNames, nil); err != nil {
				return Bundle{}, err
			}
			bundle.Clusters[i].Predicates[j] = predicate
		}
	}
	for i := range bundle.Predicates {
		predicate, err := normalizePredicate(bundle.Predicates[i])
		if err != nil {
			return Bundle{}, err
		}
		if err := validateBundlePredicate(predicate, signalKinds, collectorNames, ruleNames, clusterNames); err != nil {
			return Bundle{}, err
		}
		bundle.Predicates[i] = predicate
	}
	return bundle, nil
}

func normalizeRule(rule Rule) (Rule, error) {
	rule.Name = normalizeIdentifier(rule.Name)
	if !identifierPattern.MatchString(rule.Name) {
		return Rule{}, fmt.Errorf("%w: invalid rule %q", ErrInvalidSyntax, rule.Name)
	}
	rule.Target = normalizeIdentifier(rule.Target)
	if rule.Target == "" {
		return Rule{}, fmt.Errorf("%w: missing target for rule %q", ErrInvalidSyntax, rule.Name)
	}
	if !identifierPattern.MatchString(rule.Target) {
		return Rule{}, fmt.Errorf("%w: invalid target %q", ErrInvalidSyntax, rule.Target)
	}
	if len(rule.Collectors) == 0 {
		return Rule{}, fmt.Errorf("%w: rule %q has no collectors", ErrInvalidSyntax, rule.Name)
	}
	for i := range rule.Collectors {
		collector, err := normalizeCollector(rule.Collectors[i])
		if err != nil {
			return Rule{}, err
		}
		if len(collector.Signals) == 0 {
			return Rule{}, fmt.Errorf("%w: collector %q emits no signals", ErrInvalidSyntax, collector.Name)
		}
		rule.Collectors[i] = collector
	}
	if len(rule.Predicates) == 0 {
		return Rule{}, fmt.Errorf("%w: rule %q has no predicates", ErrInvalidSyntax, rule.Name)
	}
	return rule, nil
}

func normalizeCluster(cluster Cluster, ruleNames map[string]string) (Cluster, error) {
	cluster.Name = normalizeIdentifier(cluster.Name)
	if !identifierPattern.MatchString(cluster.Name) {
		return Cluster{}, fmt.Errorf("%w: invalid cluster %q", ErrInvalidSyntax, cluster.Name)
	}
	if len(cluster.Rules) == 0 {
		return Cluster{}, fmt.Errorf("%w: cluster %q has no rules", ErrInvalidSyntax, cluster.Name)
	}
	for i, rule := range cluster.Rules {
		rule = normalizeIdentifier(rule)
		if _, ok := ruleNames[rule]; !ok {
			return Cluster{}, fmt.Errorf("%w: cluster rule %s", ErrMissingSignal, rule)
		}
		cluster.Rules[i] = rule
	}
	if len(cluster.Predicates) == 0 {
		return Cluster{}, fmt.Errorf("%w: cluster %q has no predicates", ErrInvalidSyntax, cluster.Name)
	}
	return cluster, nil
}

func validateBundlePredicate(predicate Predicate, signalKinds map[string]string, collectorNames map[string]struct{}, ruleNames map[string]string, clusterNames map[string]string) error {
	kindForField := func(field string) (string, bool) {
		if kind, ok := kernelDerivedKind(field); ok {
			return kind, true
		}
		if kind, ok := signalKinds[field]; ok {
			return kind, true
		}
		if _, ok := ruleNames[field]; ok {
			return "bool", true
		}
		if _, ok := clusterNames[field]; ok {
			return "bool", true
		}
		return "", false
	}
	if predicate.Kind == PredicateNeed || predicate.Kind == PredicateBlock {
		kind, ok := kindForField(predicate.Field)
		if !ok {
			return fmt.Errorf("%w: %s", ErrMissingSignal, predicate.Field)
		}
		return validatePredicateType(predicate, kind)
	}
	if predicate.Kind == PredicateTemporal {
		kind, ok := kindForField(predicate.Field)
		if !ok {
			return fmt.Errorf("%w: %s", ErrMissingSignal, predicate.Field)
		}
		return validateTemporalPredicate(predicate, kind, kindForField)
	}
	if predicate.Kind != PredicateQuorum {
		return nil
	}
	if predicate.Expression != nil {
		for _, subject := range QuorumExpressionSubjects(predicate.Expression) {
			if _, ok := collectorNames[subject]; ok {
				continue
			}
			if _, ok := kindForField(subject); ok {
				continue
			}
			return fmt.Errorf("%w: quorum subject %s", ErrMissingSignal, subject)
		}
		return nil
	}
	for _, subject := range predicate.Providers {
		if _, ok := collectorNames[subject]; ok {
			continue
		}
		if _, ok := ruleNames[subject]; ok {
			continue
		}
		if _, ok := clusterNames[subject]; ok {
			continue
		}
		return fmt.Errorf("%w: quorum subject %s", ErrMissingSignal, subject)
	}
	return nil
}

func evaluateBundleRule(rule Rule, facts Facts, signals map[string]Signal, now time.Time) RuleTrace {
	checks := make([]Check, 0, len(rule.Predicates))
	authorized := true
	for _, predicate := range rule.Predicates {
		check := evaluatePredicate(predicate, facts, signals, now)
		check.RuleName = rule.Name
		check.Scope = "rule"
		if !check.Passed {
			authorized = false
		}
		checks = append(checks, check)
	}
	return RuleTrace{
		RuleName:   rule.Name,
		Aspect:     rule.Target,
		Result:     bundleResult(authorized, checks),
		Authorized: authorized,
		Checks:     checks,
	}
}

func evaluateBundleCluster(cluster Cluster, facts Facts, signals map[string]Signal, now time.Time, ruleByName map[string]RuleTrace) ClusterTrace {
	members := make([]ClusterMember, 0, len(cluster.Rules))
	authorized := true
	for _, ruleName := range cluster.Rules {
		rule := ruleByName[ruleName]
		member := ClusterMember{RuleName: ruleName, Authorized: rule.Authorized, Result: rule.Result}
		if member.Result == "" {
			member.Result = "INSUFFICIENT_EVIDENCE"
		}
		if !member.Authorized {
			authorized = false
		}
		members = append(members, member)
	}
	checks := make([]Check, 0, len(cluster.Predicates))
	for _, predicate := range cluster.Predicates {
		check := evaluatePredicate(predicate, facts, signals, now)
		check.ClusterName = cluster.Name
		check.Scope = "cluster"
		if !check.Passed {
			authorized = false
		}
		checks = append(checks, check)
	}
	return ClusterTrace{
		ClusterName: cluster.Name,
		Result:      bundleResult(authorized, checks),
		Authorized:  authorized,
		Members:     members,
		Checks:      checks,
	}
}

func bundleAuthorized(ruleTraces []RuleTrace, clusterTraces []ClusterTrace, globalChecks []Check, hasGlobal bool) bool {
	if hasGlobal {
		return checksPass(globalChecks)
	}
	for _, cluster := range clusterTraces {
		if !cluster.Authorized {
			return false
		}
	}
	for _, rule := range ruleTraces {
		if !rule.Authorized {
			return false
		}
	}
	return true
}

func checksPass(checks []Check) bool {
	for _, check := range checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func bundleResult(authorized bool, checks []Check) string {
	if authorized {
		return "AUTHORIZED"
	}
	for _, check := range checks {
		if check.Reason == ErrExpired.Error() {
			return "EXPIRED"
		}
	}
	for _, check := range checks {
		if check.Reason == ErrBlocked.Error() {
			return "BLOCKED"
		}
	}
	for _, check := range checks {
		switch check.Reason {
		case ErrUnknownFact.Error(), ErrQuorumNotMet.Error():
			return "INSUFFICIENT_EVIDENCE"
		}
	}
	return "DENIED"
}

func bundleAspect(bundle Bundle) string {
	if len(bundle.Rules) == 1 && len(bundle.Clusters) == 0 && len(bundle.Predicates) == 0 {
		return bundle.Rules[0].Target
	}
	return "rule_bundle"
}

func bundleSignalIndex(bundle Bundle) map[string]Signal {
	out := make(map[string]Signal)
	for _, rule := range bundle.Rules {
		for key, signal := range signalIndex(rule.Collectors) {
			out[key] = signal
		}
	}
	return out
}

func copyFacts(facts Facts) Facts {
	out := make(Facts, len(facts))
	for key, value := range facts {
		out[key] = value
	}
	return out
}

func canonicalBundleText(bundle Bundle) string {
	var b strings.Builder
	fmt.Fprintf(&b, "crl v1\n")
	if bundle.Package != "" {
		fmt.Fprintf(&b, "package %s\n", bundle.Package)
	}
	if bundle.Name != "" {
		fmt.Fprintf(&b, "bundle %s\n", bundle.Name)
	}
	for _, rule := range bundle.Rules {
		fmt.Fprintf(&b, "\nrule %s\n", rule.Name)
		fmt.Fprintf(&b, "\ttarget %s\n", rule.Target)
		for _, collector := range rule.Collectors {
			fmt.Fprintf(&b, "\tcollector %s %s %s from %s", collector.Name, collector.ProviderType, collector.ConnectorKind, renderSource(collector.Source))
			if collector.Schema != "" {
				fmt.Fprintf(&b, " schema %s", collector.Schema)
			}
			b.WriteByte('\n')
			for _, signal := range collector.Signals {
				fmt.Fprintf(&b, "\t\tsignal %s %s from %s", signal.Name, signal.Kind, renderSource(signal.SourceField))
				if signal.Unit != "" {
					fmt.Fprintf(&b, " unit %s", signal.Unit)
				}
				if signal.Optional {
					b.WriteString(" optional")
				}
				fmt.Fprintf(&b, " %s\n", renderSignalExpiry(signal.Expiry))
			}
		}
		for _, predicate := range rule.Predicates {
			writePredicate(&b, "\t", predicate)
		}
	}
	for _, cluster := range bundle.Clusters {
		fmt.Fprintf(&b, "\ncluster %s\n", cluster.Name)
		fmt.Fprintf(&b, "\trules %s\n", strings.Join(cluster.Rules, " + "))
		for _, predicate := range cluster.Predicates {
			writePredicate(&b, "\t", predicate)
		}
	}
	if len(bundle.Predicates) > 0 {
		b.WriteByte('\n')
	}
	for _, predicate := range bundle.Predicates {
		writePredicate(&b, "", predicate)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writePredicate(b *strings.Builder, prefix string, predicate Predicate) {
	switch predicate.Kind {
	case PredicateNeed:
		fmt.Fprintf(b, "%sneed %s %s %s\n", prefix, predicate.Field, predicate.Operator, renderValue(predicate.Value))
	case PredicateBlock:
		fmt.Fprintf(b, "%sblock %s\n", prefix, predicate.Field)
	case PredicateQuorum:
		if predicate.Expression != nil {
			fmt.Fprintf(b, "%squorum %s\n", prefix, RenderQuorumExpression(predicate.Expression))
		} else {
			fmt.Fprintf(b, "%squorum count(%s) >= %d\n", prefix, strings.Join(predicate.Providers, ", "), int(predicate.Value.Number))
		}
	case PredicateTemporal:
		fmt.Fprintf(b, "%s%s\n", prefix, renderTemporalPredicate(predicate))
	}
}

func AllBundleCollectors(bundle Bundle) []Collector {
	var collectors []Collector
	for _, rule := range bundle.Rules {
		collectors = append(collectors, rule.Collectors...)
	}
	sort.SliceStable(collectors, func(left, right int) bool {
		return collectors[left].Name < collectors[right].Name
	})
	return collectors
}
