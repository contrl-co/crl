package crl

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

const (
	Version = "crl/v1"

	OperatorEQ  = "=="
	OperatorNE  = "!="
	OperatorGT  = ">"
	OperatorGTE = ">="
	OperatorLT  = "<"
	OperatorLTE = "<="

	PredicateNeed     = "need"
	PredicateBlock    = "block"
	PredicateQuorum   = "quorum"
	PredicateTemporal = "temporal"
)

var (
	ErrEmptyRule      = errors.New("crl: empty rule")
	ErrInvalidSyntax  = errors.New("crl: invalid syntax")
	ErrUnsupportedOp  = errors.New("crl: unsupported operator")
	ErrMissingSignal  = errors.New("crl: missing collector signal")
	ErrUnknownFact    = errors.New("crl: unknown fact")
	ErrTypeMismatch   = errors.New("crl: type mismatch")
	ErrBlocked        = errors.New("crl: blocker active")
	ErrExpired        = errors.New("crl: expired")
	ErrQuorumNotMet   = errors.New("crl: quorum not met")
	identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	sourcePattern     = regexp.MustCompile(`^[A-Za-z0-9_./:@?-]+$`)
	durationPattern   = regexp.MustCompile(`^([1-9][0-9]*)(ms|s|m|h|d|w|y)$`)
)

type Collector struct {
	Name          string   `json:"name"`
	ProviderType  string   `json:"provider_type"`
	ConnectorKind string   `json:"connector_kind"`
	Source        string   `json:"source"`
	Schema        string   `json:"schema,omitempty"`
	Signals       []Signal `json:"signals"`
}

type Signal struct {
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	SourceField string       `json:"source_field"`
	Unit        string       `json:"unit,omitempty"`
	Optional    bool         `json:"optional,omitempty"`
	Expiry      SignalExpiry `json:"expiry"`
}

type SignalExpiry struct {
	Mode    string `json:"mode"`
	Literal string `json:"literal"`
	Seconds int64  `json:"seconds,omitempty"`
}

type Predicate struct {
	Kind       string              `json:"kind"`
	Field      string              `json:"field,omitempty"`
	Operator   string              `json:"operator"`
	Value      Value               `json:"value"`
	Providers  []string            `json:"providers,omitempty"`
	Expression *QuorumExpression   `json:"expression,omitempty"`
	Temporal   *TemporalExpression `json:"temporal,omitempty"`
}

type TemporalExpression struct {
	Relation      string `json:"relation"`
	Reference     string `json:"reference,omitempty"`
	WindowLiteral string `json:"window_literal,omitempty"`
	WindowSeconds int64  `json:"window_seconds,omitempty"`
}

type QuorumExpression struct {
	Kind  string            `json:"kind"`
	Name  string            `json:"name,omitempty"`
	Expr  *QuorumExpression `json:"expr,omitempty"`
	Left  *QuorumExpression `json:"left,omitempty"`
	Right *QuorumExpression `json:"right,omitempty"`
}

type Value struct {
	Kind   string  `json:"kind"`
	String string  `json:"string,omitempty"`
	Bool   bool    `json:"bool,omitempty"`
	Number float64 `json:"number,omitempty"`
}

type Facts map[string]any

type Check struct {
	Kind              string             `json:"kind"`
	RuleName          string             `json:"rule_name,omitempty"`
	ClusterName       string             `json:"cluster_name,omitempty"`
	Scope             string             `json:"scope,omitempty"`
	Field             string             `json:"field,omitempty"`
	Operator          string             `json:"operator"`
	Expected          Value              `json:"expected"`
	Actual            any                `json:"actual,omitempty"`
	QuorumExpression  string             `json:"quorum_expression,omitempty"`
	TemporalRelation  string             `json:"temporal_relation,omitempty"`
	TemporalReference string             `json:"temporal_reference,omitempty"`
	TemporalWindow    string             `json:"temporal_window,omitempty"`
	Providers         []ProviderPresence `json:"providers,omitempty"`
	Passed            bool               `json:"passed"`
	Reason            string             `json:"reason,omitempty"`
}

type ProviderPresence struct {
	Provider string `json:"provider"`
	Present  bool   `json:"present"`
}

func normalizePredicate(predicate Predicate) (Predicate, error) {
	predicate.Kind = normalizeIdentifier(predicate.Kind)
	if predicate.Kind == "" {
		predicate.Kind = PredicateNeed
	}
	switch predicate.Kind {
	case PredicateNeed:
		predicate.Field = normalizeIdentifier(predicate.Field)
		if !identifierPattern.MatchString(predicate.Field) {
			return Predicate{}, fmt.Errorf("%w: invalid field %q", ErrInvalidSyntax, predicate.Field)
		}
		if err := validateOperator(predicate.Operator); err != nil {
			return Predicate{}, err
		}
		predicate.Value = normalizeValue(predicate.Value)
		if err := validateValue(predicate.Value); err != nil {
			return Predicate{}, err
		}
	case PredicateBlock:
		predicate.Field = normalizeIdentifier(predicate.Field)
		if !identifierPattern.MatchString(predicate.Field) {
			return Predicate{}, fmt.Errorf("%w: invalid field %q", ErrInvalidSyntax, predicate.Field)
		}
		predicate.Operator = OperatorEQ
		predicate.Value = Value{Kind: "bool"}
	case PredicateQuorum:
		predicate.Field = PredicateQuorum
		if predicate.Expression != nil {
			expression, err := normalizeQuorumExpression(predicate.Expression)
			if err != nil {
				return Predicate{}, err
			}
			predicate.Operator = OperatorEQ
			predicate.Value = Value{Kind: "bool", Bool: true}
			predicate.Expression = expression
			predicate.Providers = nil
			return predicate, nil
		}
		predicate.Operator = OperatorGTE
		if predicate.Value.Kind != "number" || predicate.Value.Number < 1 || predicate.Value.Number != float64(int(predicate.Value.Number)) {
			return Predicate{}, fmt.Errorf("%w: invalid quorum count", ErrInvalidSyntax)
		}
		if len(predicate.Providers) == 0 {
			return Predicate{}, fmt.Errorf("%w: missing quorum providers", ErrInvalidSyntax)
		}
		seen := map[string]struct{}{}
		providers := make([]string, 0, len(predicate.Providers))
		for _, provider := range predicate.Providers {
			provider = normalizeIdentifier(provider)
			if !identifierPattern.MatchString(provider) {
				return Predicate{}, fmt.Errorf("%w: invalid provider %q", ErrInvalidSyntax, provider)
			}
			if _, ok := seen[provider]; ok {
				return Predicate{}, fmt.Errorf("%w: duplicate provider %q", ErrInvalidSyntax, provider)
			}
			seen[provider] = struct{}{}
			providers = append(providers, provider)
		}
		sort.Strings(providers)
		predicate.Providers = providers
	case PredicateTemporal:
		predicate.Field = normalizeIdentifier(predicate.Field)
		if !identifierPattern.MatchString(predicate.Field) {
			return Predicate{}, fmt.Errorf("%w: invalid temporal field %q", ErrInvalidSyntax, predicate.Field)
		}
		if predicate.Temporal == nil {
			return Predicate{}, fmt.Errorf("%w: missing temporal expression", ErrInvalidSyntax)
		}
		temporal, err := normalizeTemporalExpression(*predicate.Temporal)
		if err != nil {
			return Predicate{}, err
		}
		predicate.Temporal = &temporal
		predicate.Operator = temporal.Relation
		predicate.Value = Value{Kind: "time", String: temporal.Reference}
	default:
		return Predicate{}, fmt.Errorf("%w: invalid predicate %q", ErrInvalidSyntax, predicate.Kind)
	}
	return predicate, nil
}

func normalizeCollector(collector Collector) (Collector, error) {
	collector.Name = normalizeIdentifier(collector.Name)
	if !identifierPattern.MatchString(collector.Name) {
		return Collector{}, fmt.Errorf("%w: invalid collector %q", ErrInvalidSyntax, collector.Name)
	}
	collector.ProviderType = normalizeIdentifier(collector.ProviderType)
	if !identifierPattern.MatchString(collector.ProviderType) {
		return Collector{}, fmt.Errorf("%w: invalid provider type %q", ErrInvalidSyntax, collector.ProviderType)
	}
	collector.ConnectorKind = normalizeIdentifier(collector.ConnectorKind)
	if !identifierPattern.MatchString(collector.ConnectorKind) {
		return Collector{}, fmt.Errorf("%w: invalid connector kind %q", ErrInvalidSyntax, collector.ConnectorKind)
	}
	collector.Source = normalizeText(collector.Source)
	if collector.Source == "" {
		return Collector{}, fmt.Errorf("%w: invalid collector source %q", ErrInvalidSyntax, collector.Source)
	}
	collector.Schema = normalizeIdentifier(collector.Schema)
	if collector.Schema != "" && !identifierPattern.MatchString(collector.Schema) {
		return Collector{}, fmt.Errorf("%w: invalid collector schema %q", ErrInvalidSyntax, collector.Schema)
	}
	seen := map[string]struct{}{}
	for i := range collector.Signals {
		signal, err := normalizeSignal(collector.Signals[i])
		if err != nil {
			return Collector{}, err
		}
		if _, ok := seen[signal.Name]; ok {
			return Collector{}, fmt.Errorf("%w: duplicate signal %q", ErrInvalidSyntax, signal.Name)
		}
		seen[signal.Name] = struct{}{}
		collector.Signals[i] = signal
	}
	return collector, nil
}

func normalizeSignal(signal Signal) (Signal, error) {
	signal.Name = normalizeIdentifier(signal.Name)
	if !identifierPattern.MatchString(signal.Name) {
		return Signal{}, fmt.Errorf("%w: invalid signal %q", ErrInvalidSyntax, signal.Name)
	}
	signal.Kind = normalizeIdentifier(signal.Kind)
	switch signal.Kind {
	case "number", "bool", "string", "time":
	default:
		return Signal{}, fmt.Errorf("%w: invalid signal kind %q", ErrInvalidSyntax, signal.Kind)
	}
	signal.SourceField = normalizeText(signal.SourceField)
	if signal.SourceField == "" {
		return Signal{}, fmt.Errorf("%w: invalid signal source field %q", ErrInvalidSyntax, signal.SourceField)
	}
	signal.Unit = normalizeIdentifier(signal.Unit)
	if signal.Unit != "" {
		if signal.Kind != "number" {
			return Signal{}, fmt.Errorf("%w: unit requires number signal %q", ErrInvalidSyntax, signal.Name)
		}
		if !identifierPattern.MatchString(signal.Unit) {
			return Signal{}, fmt.Errorf("%w: invalid signal unit %q", ErrInvalidSyntax, signal.Unit)
		}
	}
	expiry, err := normalizeSignalExpiry(signal.Expiry)
	if err != nil {
		return Signal{}, err
	}
	signal.Expiry = expiry
	return signal, nil
}

func normalizeTemporalExpression(expression TemporalExpression) (TemporalExpression, error) {
	expression.Relation = normalizeIdentifier(expression.Relation)
	expression.Reference = normalizeTemporalReference(expression.Reference)
	switch expression.Relation {
	case "before", "after":
		if expression.Reference == "" {
			return TemporalExpression{}, fmt.Errorf("%w: temporal %s requires reference", ErrInvalidSyntax, expression.Relation)
		}
		expression.WindowLiteral = ""
		expression.WindowSeconds = 0
	case "within_before", "within_after":
		if expression.Reference == "" {
			return TemporalExpression{}, fmt.Errorf("%w: temporal %s requires reference", ErrInvalidSyntax, expression.Relation)
		}
		window := strings.ToLower(strings.TrimSpace(expression.WindowLiteral))
		seconds, err := parseDurationSeconds(window)
		if err != nil {
			return TemporalExpression{}, err
		}
		expression.WindowLiteral = window
		expression.WindowSeconds = seconds
	case "age_lte", "age_gte":
		window := strings.ToLower(strings.TrimSpace(expression.WindowLiteral))
		seconds, err := parseDurationSeconds(window)
		if err != nil {
			return TemporalExpression{}, err
		}
		expression.Reference = "now"
		expression.WindowLiteral = window
		expression.WindowSeconds = seconds
	default:
		return TemporalExpression{}, fmt.Errorf("%w: invalid temporal relation %q", ErrInvalidSyntax, expression.Relation)
	}
	return expression, nil
}

func parseSignalExpiry(mode, literal string) (SignalExpiry, error) {
	mode = normalizeIdentifier(mode)
	literal = strings.TrimSpace(unquote(literal))
	switch mode {
	case "ttl":
		canonical := strings.ToLower(literal)
		seconds, err := parseDurationSeconds(canonical)
		if err != nil {
			return SignalExpiry{}, err
		}
		return SignalExpiry{Mode: "ttl", Literal: canonical, Seconds: seconds}, nil
	case "expires":
		canonical := strings.ToLower(literal)
		if seconds, err := parseDurationSeconds(canonical); err == nil {
			return SignalExpiry{Mode: "ttl", Literal: canonical, Seconds: seconds}, nil
		}
		if _, err := time.Parse(time.RFC3339, literal); err != nil {
			return SignalExpiry{}, fmt.Errorf("%w: invalid expiry %q", ErrInvalidSyntax, literal)
		}
		return SignalExpiry{Mode: "at", Literal: literal}, nil
	default:
		return SignalExpiry{}, fmt.Errorf("%w: expected ttl or expires", ErrInvalidSyntax)
	}
}

func normalizeSignalExpiry(expiry SignalExpiry) (SignalExpiry, error) {
	mode := normalizeIdentifier(expiry.Mode)
	if mode == "" {
		mode = "ttl"
	}
	// parseSignalExpiry emits Mode "at" for absolute (RFC3339)
	// expiries; map it back to the surface keyword so an already
	// parsed expiry survives re-normalization instead of being
	// rejected as neither ttl nor expires.
	if mode == "at" {
		mode = "expires"
	}
	literal := strings.TrimSpace(expiry.Literal)
	if literal == "" && expiry.Seconds > 0 {
		literal = strconv.FormatInt(expiry.Seconds, 10) + "s"
	}
	return parseSignalExpiry(mode, literal)
}

func parseDurationSeconds(literal string) (int64, error) {
	match := durationPattern.FindStringSubmatch(literal)
	if match == nil {
		return 0, fmt.Errorf("%w: invalid duration %q", ErrInvalidSyntax, literal)
	}
	value, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%w: invalid duration %q", ErrInvalidSyntax, literal)
	}
	switch match[2] {
	case "ms":
		return 1, nil
	case "s":
		return checkedDurationSeconds(value, 1, literal)
	case "m":
		return checkedDurationSeconds(value, 60, literal)
	case "h":
		return checkedDurationSeconds(value, 60*60, literal)
	case "d":
		return checkedDurationSeconds(value, 24*60*60, literal)
	case "w":
		return checkedDurationSeconds(value, 7*24*60*60, literal)
	case "y":
		return checkedDurationSeconds(value, 365*24*60*60, literal)
	default:
		return 0, fmt.Errorf("%w: invalid duration %q", ErrInvalidSyntax, literal)
	}
}

func checkedDurationSeconds(value int64, unitSeconds int64, literal string) (int64, error) {
	maxSeconds := int64(math.MaxInt64 / int64(time.Second))
	if value > maxSeconds/unitSeconds {
		return 0, fmt.Errorf("%w: duration too large %q", ErrInvalidSyntax, literal)
	}
	return value * unitSeconds, nil
}

func validatePredicateCoverage(predicate Predicate, signalKinds map[string]string, collectorNames map[string]struct{}) error {
	if predicate.Kind == PredicateNeed || predicate.Kind == PredicateBlock {
		kind, ok := kernelDerivedKind(predicate.Field)
		if !ok {
			kind, ok = signalKinds[predicate.Field]
		}
		if !ok {
			return fmt.Errorf("%w: %s", ErrMissingSignal, predicate.Field)
		}
		return validatePredicateType(predicate, kind)
	}
	if predicate.Kind == PredicateQuorum {
		if predicate.Expression != nil {
			for _, subject := range QuorumExpressionSubjects(predicate.Expression) {
				if _, ok := collectorNames[subject]; ok {
					continue
				}
				if _, ok := signalKinds[subject]; ok {
					continue
				}
				return fmt.Errorf("%w: quorum subject %s", ErrMissingSignal, subject)
			}
			return nil
		}
		for _, provider := range predicate.Providers {
			if _, ok := collectorNames[provider]; !ok {
				return fmt.Errorf("%w: quorum provider %s", ErrMissingSignal, provider)
			}
		}
	}
	if predicate.Kind == PredicateTemporal {
		kind, ok := signalKinds[predicate.Field]
		if !ok {
			return fmt.Errorf("%w: %s", ErrMissingSignal, predicate.Field)
		}
		return validateTemporalPredicate(predicate, kind, func(field string) (string, bool) {
			kind, ok := signalKinds[field]
			return kind, ok
		})
	}
	return nil
}

func kernelDerivedKind(field string) (string, bool) {
	switch field {
	case "min_provider_trust":
		return "number", true
	default:
		return "", false
	}
}

func validatePredicateType(predicate Predicate, signalKind string) error {
	if predicate.Kind == PredicateBlock {
		switch signalKind {
		case "bool", "number":
			return nil
		default:
			return fmt.Errorf("%w: block requires bool or number signal %q", ErrInvalidSyntax, predicate.Field)
		}
	}
	if predicate.Value.Kind != signalKind {
		return fmt.Errorf("%w: %s expects %s got %s", ErrTypeMismatch, predicate.Field, signalKind, predicate.Value.Kind)
	}
	switch signalKind {
	case "number":
		return nil
	case "bool", "string":
		if predicate.Operator == OperatorEQ || predicate.Operator == OperatorNE {
			return nil
		}
		return fmt.Errorf("%w: %s only supports == or !=", ErrUnsupportedOp, signalKind)
	default:
		return fmt.Errorf("%w: invalid signal kind %q", ErrInvalidSyntax, signalKind)
	}
}

func validateTemporalPredicate(predicate Predicate, fieldKind string, kindForField func(string) (string, bool)) error {
	if fieldKind != "time" {
		return fmt.Errorf("%w: temporal predicate requires time field %q", ErrTypeMismatch, predicate.Field)
	}
	if predicate.Temporal == nil {
		return fmt.Errorf("%w: missing temporal expression", ErrInvalidSyntax)
	}
	switch predicate.Temporal.Relation {
	case "before", "after", "within_before", "within_after":
		reference := predicate.Temporal.Reference
		if reference == "now" || isTimeLiteral(reference) {
			return nil
		}
		kind, ok := kindForField(reference)
		if !ok {
			return fmt.Errorf("%w: temporal reference %s", ErrMissingSignal, reference)
		}
		if kind != "time" {
			return fmt.Errorf("%w: temporal reference %q must be time", ErrTypeMismatch, reference)
		}
		return nil
	case "age_lte", "age_gte":
		return nil
	default:
		return fmt.Errorf("%w: invalid temporal relation %q", ErrInvalidSyntax, predicate.Temporal.Relation)
	}
}

func validateOperator(op string) error {
	switch op {
	case OperatorEQ, OperatorNE, OperatorGT, OperatorGTE, OperatorLT, OperatorLTE:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedOp, op)
	}
}

func validateValue(value Value) error {
	switch value.Kind {
	case "number", "bool", "string":
		if value.Kind == "number" && (math.IsNaN(value.Number) || math.IsInf(value.Number, 0)) {
			return fmt.Errorf("%w: invalid numeric value", ErrInvalidSyntax)
		}
		return nil
	default:
		return fmt.Errorf("%w: invalid value kind %q", ErrInvalidSyntax, value.Kind)
	}
}

func parseValue(raw string) (Value, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Value{}, fmt.Errorf("%w: missing value", ErrInvalidSyntax)
	}
	if strings.HasPrefix(raw, `'`) && strings.HasSuffix(raw, `'`) {
		return Value{Kind: "string", String: strings.TrimSuffix(strings.TrimPrefix(raw, `'`), `'`)}, nil
	}
	if strings.HasPrefix(raw, `"`) {
		s, err := strconv.Unquote(raw)
		if err != nil {
			return Value{}, fmt.Errorf("%w: invalid string literal", ErrInvalidSyntax)
		}
		return Value{Kind: "string", String: s}, nil
	}
	switch strings.ToLower(raw) {
	case "true":
		return Value{Kind: "bool", Bool: true}, nil
	case "false":
		return Value{Kind: "bool"}, nil
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return Value{}, fmt.Errorf("%w: invalid literal %q", ErrInvalidSyntax, raw)
	}
	return Value{Kind: "number", Number: n}, nil
}

func evaluatePredicate(rule Predicate, facts Facts, signals map[string]Signal, now time.Time) Check {
	check := Check{
		Kind:     rule.Kind,
		Field:    rule.Field,
		Operator: rule.Operator,
		Expected: rule.Value,
	}
	if rule.Kind == PredicateQuorum {
		return evaluateQuorum(rule, facts, signals, now, check)
	}
	if rule.Kind == PredicateTemporal {
		return evaluateTemporal(rule, facts, signals, now, check)
	}
	actual, ok := lookupFact(facts, rule.Field)
	if !ok {
		check.Reason = ErrUnknownFact.Error()
		return check
	}
	check.Actual = actual
	if expired, ok := signalExpired(signals[rule.Field], facts, now); ok && expired {
		check.Reason = ErrExpired.Error()
		return check
	}
	if rule.Kind == PredicateBlock {
		passed, reason := evaluateBlock(rule.Field, actual)
		check.Passed = passed
		check.Reason = reason
		return check
	}
	passed, err := compare(actual, rule.Operator, rule.Value)
	if err != nil {
		check.Reason = err.Error()
		return check
	}
	check.Passed = passed
	return check
}

func evaluateTemporal(rule Predicate, facts Facts, signals map[string]Signal, now time.Time, check Check) Check {
	check.Expected = Value{}
	if rule.Temporal == nil {
		check.Reason = ErrInvalidSyntax.Error()
		return check
	}
	check.TemporalRelation = rule.Temporal.Relation
	check.TemporalReference = rule.Temporal.Reference
	check.TemporalWindow = rule.Temporal.WindowLiteral
	actualRaw, ok := lookupFact(facts, rule.Field)
	if !ok {
		check.Reason = ErrUnknownFact.Error()
		return check
	}
	actual, ok := timeFact(actualRaw)
	if !ok {
		check.Actual = actualRaw
		check.Reason = ErrTypeMismatch.Error()
		return check
	}
	check.Actual = actual.Format(time.RFC3339)
	if expired, ok := signalExpired(signals[rule.Field], facts, now); ok && expired {
		check.Reason = ErrExpired.Error()
		return check
	}
	reference, ok, reason := temporalReferenceTime(rule.Temporal.Reference, facts, signals, now)
	if !ok {
		check.Reason = reason
		return check
	}
	switch rule.Temporal.Relation {
	case "before":
		check.Passed = actual.Before(reference)
	case "after":
		check.Passed = actual.After(reference)
	case "within_before":
		start := reference.Add(-time.Duration(rule.Temporal.WindowSeconds) * time.Second)
		check.Passed = (actual.Equal(start) || actual.After(start)) && (actual.Equal(reference) || actual.Before(reference))
	case "within_after":
		end := reference.Add(time.Duration(rule.Temporal.WindowSeconds) * time.Second)
		check.Passed = (actual.Equal(reference) || actual.After(reference)) && (actual.Equal(end) || actual.Before(end))
	case "age_lte":
		if now.IsZero() || actual.After(now) {
			check.Reason = ErrUnknownFact.Error()
			return check
		}
		check.Passed = now.Sub(actual) <= time.Duration(rule.Temporal.WindowSeconds)*time.Second
	case "age_gte":
		if now.IsZero() || actual.After(now) {
			check.Reason = ErrUnknownFact.Error()
			return check
		}
		check.Passed = now.Sub(actual) >= time.Duration(rule.Temporal.WindowSeconds)*time.Second
	default:
		check.Reason = ErrInvalidSyntax.Error()
		return check
	}
	if !check.Passed {
		switch rule.Temporal.Relation {
		case "within_before", "within_after", "age_lte":
			check.Reason = ErrExpired.Error()
		}
	}
	return check
}

func evaluateBlock(field string, actual any) (bool, string) {
	active, ok := actual.(bool)
	if !ok {
		n, numericOK := numeric(actual)
		if !numericOK {
			return false, ErrTypeMismatch.Error()
		}
		active = n != 0
	}
	if !active {
		return true, ""
	}
	// An active blocker always reports BLOCKED. EXPIRED is reserved for
	// declared expiry semantics (signal ttl/expires or temporal
	// predicates) — never inferred from the field's name, which would
	// couple the machine-readable outcome to a naming convention.
	return false, ErrBlocked.Error()
}

func evaluateQuorum(rule Predicate, facts Facts, signals map[string]Signal, now time.Time, check Check) Check {
	if rule.Expression != nil {
		check.QuorumExpression = RenderQuorumExpression(rule.Expression)
		subjects := QuorumExpressionSubjects(rule.Expression)
		check.Providers = make([]ProviderPresence, 0, len(subjects))
		staleSubject := false
		for _, subject := range subjects {
			if signalUnavailable(signals, subject, facts, now) {
				staleSubject = true
			}
			check.Providers = append(check.Providers, ProviderPresence{Provider: subject, Present: subjectPresent(facts, subject, signals, now)})
		}
		// If the expression references a signal whose freshness cannot be
		// proven, fail CLOSED regardless of boolean structure. Assigning a
		// truthy/falsy value per-subject would let a negated stale subject
		// (e.g. `!safety_hold`) read as "cleared" — fail open. An
		// unprovable input taints the whole quorum.
		if staleSubject {
			check.Actual = false
			check.Passed = false
			check.Reason = ErrExpired.Error()
			return check
		}
		passed := evaluateQuorumExpression(rule.Expression, facts, signals, now)
		check.Actual = passed
		check.Passed = passed
		if !check.Passed {
			check.Reason = ErrQuorumNotMet.Error()
		}
		return check
	}
	count := 0
	check.Providers = make([]ProviderPresence, 0, len(rule.Providers))
	for _, provider := range rule.Providers {
		present := subjectPresent(facts, provider, signals, now)
		if present {
			count++
		}
		check.Providers = append(check.Providers, ProviderPresence{Provider: provider, Present: present})
	}
	check.Actual = count
	check.Passed = float64(count) >= rule.Value.Number
	if !check.Passed {
		check.Reason = ErrQuorumNotMet.Error()
	}
	return check
}

func compare(actual any, op string, expected Value) (bool, error) {
	switch expected.Kind {
	case "number":
		n, ok := numeric(actual)
		if !ok {
			return false, ErrTypeMismatch
		}
		switch op {
		case OperatorEQ:
			return n == expected.Number, nil
		case OperatorNE:
			return n != expected.Number, nil
		case OperatorGT:
			return n > expected.Number, nil
		case OperatorGTE:
			return n >= expected.Number, nil
		case OperatorLT:
			return n < expected.Number, nil
		case OperatorLTE:
			return n <= expected.Number, nil
		}
	case "bool":
		b, ok := actual.(bool)
		if !ok {
			return false, ErrTypeMismatch
		}
		switch op {
		case OperatorEQ:
			return b == expected.Bool, nil
		case OperatorNE:
			return b != expected.Bool, nil
		default:
			return false, ErrUnsupportedOp
		}
	case "string":
		s, ok := actual.(string)
		if !ok {
			return false, ErrTypeMismatch
		}
		switch op {
		case OperatorEQ:
			return s == expected.String, nil
		case OperatorNE:
			return s != expected.String, nil
		default:
			return false, ErrUnsupportedOp
		}
	}
	return false, ErrUnsupportedOp
}

func numeric(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		v, err := n.Float64()
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	default:
		return 0, false
	}
}

func signalIndex(collectors []Collector) map[string]Signal {
	out := make(map[string]Signal)
	for _, collector := range collectors {
		for _, signal := range collector.Signals {
			out[signal.Name] = signal
		}
	}
	return out
}

// signalExpired reports whether a signal's declared expiry has lapsed.
// The second return is false only when the signal declares no expiry at
// all — in every other case the expiry IS evaluated, and any gap that
// prevents proving freshness (zero clock, missing or unparseable
// observed_at, unparseable expiry literal) fails CLOSED as expired.
// A missing observation timestamp must never silently disable a
// declared freshness guarantee.
func signalExpired(signal Signal, facts Facts, now time.Time) (bool, bool) {
	if signal.Name == "" {
		return false, false
	}
	hasTTL := signal.Expiry.Mode == "ttl" && signal.Expiry.Seconds > 0
	hasAt := signal.Expiry.Mode == "at"
	if !hasTTL && !hasAt {
		return false, false
	}
	// A declared expiry cannot be evaluated without a clock: fail closed.
	if now.IsZero() {
		return true, true
	}
	if hasAt {
		// Absolute expiry needs only the clock, not an observation time.
		expiresAt, err := time.Parse(time.RFC3339, signal.Expiry.Literal)
		if err != nil {
			return true, true
		}
		return now.UTC().After(expiresAt.UTC()), true
	}
	observedRaw, ok := lookupFact(facts, "observed_at."+signal.Name)
	if !ok {
		// TTL declared but no observation timestamp: age unknowable.
		return true, true
	}
	observedAt, ok := timeFact(observedRaw)
	if !ok {
		return true, true
	}
	expiresAt := observedAt.UTC().Add(time.Duration(signal.Expiry.Seconds) * time.Second)
	return now.UTC().After(expiresAt), true
}

func timeFact(value any) (time.Time, bool) {
	switch v := value.(type) {
	case time.Time:
		return v.UTC(), true
	case string:
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
		return parsed.UTC(), err == nil
	default:
		return time.Time{}, false
	}
}

func temporalReferenceTime(reference string, facts Facts, signals map[string]Signal, now time.Time) (time.Time, bool, string) {
	reference = normalizeTemporalReference(reference)
	if reference == "now" {
		if now.IsZero() {
			return time.Time{}, false, ErrInvalidSyntax.Error()
		}
		return now.UTC(), true, ""
	}
	if parsed, err := time.Parse(time.RFC3339, reference); err == nil {
		return parsed.UTC(), true, ""
	}
	raw, ok := lookupFact(facts, reference)
	if !ok {
		return time.Time{}, false, ErrUnknownFact.Error()
	}
	// When the reference is itself a ttl'd signal, a stale or
	// unknown-age reference must fail closed just like the temporal
	// field would — otherwise a comparison "before/after <deadline>"
	// silently trusts an expired deadline.
	if signalUnavailable(signals, reference, facts, now) {
		return time.Time{}, false, ErrExpired.Error()
	}
	parsed, ok := timeFact(raw)
	if !ok {
		return time.Time{}, false, ErrTypeMismatch.Error()
	}
	return parsed, true, ""
}

func normalizeTemporalReference(reference string) string {
	reference = strings.TrimSpace(unquote(reference))
	if strings.EqualFold(reference, "now") {
		return "now"
	}
	if _, err := time.Parse(time.RFC3339, reference); err == nil {
		return reference
	}
	return normalizeIdentifier(reference)
}

func isTimeLiteral(value string) bool {
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(unquote(value)))
	return err == nil
}

func lookupFact(facts Facts, field string) (any, bool) {
	if facts == nil {
		return nil, false
	}
	value, ok := facts[field]
	return value, ok
}

// signalUnavailable reports whether name refers to a declared
// ttl/expires signal whose freshness cannot be proven at now (stale,
// missing/unparseable observed_at, or zero clock). Such a signal is
// treated as unavailable wherever it is consulted — need/block already
// surface EXPIRED; quorum and temporal-reference paths route through
// here so stale evidence never silently satisfies a gate. Non-signal
// subjects (collectors, rule/cluster names) miss the index and are
// unaffected.
func signalUnavailable(signals map[string]Signal, name string, facts Facts, now time.Time) bool {
	if signals == nil {
		return false
	}
	expired, evaluated := signalExpired(signals[normalizeIdentifier(name)], facts, now)
	return evaluated && expired
}

// subjectPresent is subjectTruthy plus the freshness gate: a truthy but
// stale (or unknown-age) ttl'd signal subject does not count toward a
// quorum.
func subjectPresent(facts Facts, subject string, signals map[string]Signal, now time.Time) bool {
	return subjectTruthy(facts, subject) && !signalUnavailable(signals, subject, facts, now)
}

func subjectTruthy(facts Facts, subject string) bool {
	if facts == nil {
		return false
	}
	subject = normalizeIdentifier(subject)
	for _, key := range []string{
		subject,
		"provider." + subject,
		"provider:" + subject,
		"rule." + subject,
		"cluster." + subject,
	} {
		value, ok := facts[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case bool:
			return v
		case string:
			return strings.TrimSpace(v) != ""
		default:
			number, ok := numeric(v)
			return ok && number != 0
		}
	}
	return false
}

func evaluateQuorumExpression(expression *QuorumExpression, facts Facts, signals map[string]Signal, now time.Time) bool {
	if expression == nil {
		return false
	}
	switch expression.Kind {
	case "subject":
		return subjectPresent(facts, expression.Name, signals, now)
	case "not":
		return !evaluateQuorumExpression(expression.Expr, facts, signals, now)
	case "and":
		return evaluateQuorumExpression(expression.Left, facts, signals, now) && evaluateQuorumExpression(expression.Right, facts, signals, now)
	case "or":
		return evaluateQuorumExpression(expression.Left, facts, signals, now) || evaluateQuorumExpression(expression.Right, facts, signals, now)
	default:
		return false
	}
}

func parseQuorumExpression(fields []string) (*QuorumExpression, error) {
	tokens := quorumTokens(fields)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: missing quorum expression", ErrInvalidSyntax)
	}
	parser := quorumParser{tokens: tokens}
	expr, err := parser.parseOr()
	if err != nil {
		return nil, err
	}
	if parser.pos != len(tokens) {
		return nil, fmt.Errorf("%w: unexpected quorum token %q", ErrInvalidSyntax, tokens[parser.pos])
	}
	return normalizeQuorumExpression(expr)
}

func quorumTokens(fields []string) []string {
	var tokens []string
	for _, field := range fields {
		if alias := logicalFieldAlias(field); alias != field {
			tokens = append(tokens, alias)
			continue
		}
		var current strings.Builder
		for _, char := range field {
			switch char {
			case '&', '|', '!', '(', ')':
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
				tokens = append(tokens, string(char))
			default:
				current.WriteRune(char)
			}
		}
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
		}
	}
	return tokens
}

type quorumParser struct {
	tokens []string
	pos    int
}

func (p *quorumParser) parseOr() (*QuorumExpression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.match("|") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &QuorumExpression{Kind: "or", Left: left, Right: right}
	}
	return left, nil
}

func (p *quorumParser) parseAnd() (*QuorumExpression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.match("&") {
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &QuorumExpression{Kind: "and", Left: left, Right: right}
	}
	return left, nil
}

func (p *quorumParser) parseUnary() (*QuorumExpression, error) {
	if p.match("!") {
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &QuorumExpression{Kind: "not", Expr: expr}, nil
	}
	if p.match("(") {
		expr, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.match(")") {
			return nil, fmt.Errorf("%w: missing ) in quorum expression", ErrInvalidSyntax)
		}
		return expr, nil
	}
	if p.pos >= len(p.tokens) {
		return nil, fmt.Errorf("%w: incomplete quorum expression", ErrInvalidSyntax)
	}
	name := normalizeIdentifier(p.tokens[p.pos])
	p.pos++
	if !identifierPattern.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid quorum subject %q", ErrInvalidSyntax, name)
	}
	return &QuorumExpression{Kind: "subject", Name: name}, nil
}

func (p *quorumParser) match(token string) bool {
	if p.pos >= len(p.tokens) || p.tokens[p.pos] != token {
		return false
	}
	p.pos++
	return true
}

func normalizeQuorumExpression(expression *QuorumExpression) (*QuorumExpression, error) {
	if expression == nil {
		return nil, fmt.Errorf("%w: missing quorum expression", ErrInvalidSyntax)
	}
	switch normalizeIdentifier(expression.Kind) {
	case "subject":
		name := normalizeIdentifier(expression.Name)
		if !identifierPattern.MatchString(name) {
			return nil, fmt.Errorf("%w: invalid quorum subject %q", ErrInvalidSyntax, expression.Name)
		}
		return &QuorumExpression{Kind: "subject", Name: name}, nil
	case "not":
		expr, err := normalizeQuorumExpression(expression.Expr)
		if err != nil {
			return nil, err
		}
		return &QuorumExpression{Kind: "not", Expr: expr}, nil
	case "and", "or":
		left, err := normalizeQuorumExpression(expression.Left)
		if err != nil {
			return nil, err
		}
		right, err := normalizeQuorumExpression(expression.Right)
		if err != nil {
			return nil, err
		}
		if RenderQuorumExpression(right) < RenderQuorumExpression(left) {
			left, right = right, left
		}
		return &QuorumExpression{Kind: normalizeIdentifier(expression.Kind), Left: left, Right: right}, nil
	default:
		return nil, fmt.Errorf("%w: invalid quorum expression kind %q", ErrInvalidSyntax, expression.Kind)
	}
}

func QuorumExpressionSubjects(expression *QuorumExpression) []string {
	seen := map[string]struct{}{}
	var subjects []string
	var walk func(*QuorumExpression)
	walk = func(expr *QuorumExpression) {
		if expr == nil {
			return
		}
		switch expr.Kind {
		case "subject":
			name := normalizeIdentifier(expr.Name)
			if _, ok := seen[name]; !ok && name != "" {
				seen[name] = struct{}{}
				subjects = append(subjects, name)
			}
		case "not":
			walk(expr.Expr)
		default:
			walk(expr.Left)
			walk(expr.Right)
		}
	}
	walk(expression)
	sort.Strings(subjects)
	return subjects
}

func RenderQuorumExpression(expression *QuorumExpression) string {
	if expression == nil {
		return ""
	}
	switch expression.Kind {
	case "subject":
		return expression.Name
	case "not":
		inner := RenderQuorumExpression(expression.Expr)
		if expression.Expr != nil && (expression.Expr.Kind == "and" || expression.Expr.Kind == "or") {
			return "!(" + inner + ")"
		}
		return "!" + inner
	case "and", "or":
		op := " & "
		if expression.Kind == "or" {
			op = " | "
		}
		left := RenderQuorumExpression(expression.Left)
		right := RenderQuorumExpression(expression.Right)
		if expression.Left != nil && expression.Kind == "and" && expression.Left.Kind == "or" {
			left = "(" + left + ")"
		}
		if expression.Right != nil && (expression.Right.Kind == "or" && expression.Kind == "and") {
			right = "(" + right + ")"
		}
		return left + op + right
	default:
		return ""
	}
}

func normalizeIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(norm.NFC.String(value)))
}

// NFC lives here, not in crypto.CanonicalJSON: the hash must cover the
// same bytes the evaluator compares.
func normalizeText(value string) string {
	return norm.NFC.String(strings.TrimSpace(value))
}

func normalizeValue(value Value) Value {
	if value.Kind == "string" {
		value.String = norm.NFC.String(value.String)
	}
	return value
}

func unquote(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return raw
	}
	if (strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`)) || (strings.HasPrefix(raw, `'`) && strings.HasSuffix(raw, `'`)) {
		value, err := strconv.Unquote(raw)
		if err == nil {
			return value
		}
		return raw[1 : len(raw)-1]
	}
	return raw
}

func renderSource(source string) string {
	if sourcePattern.MatchString(source) {
		return source
	}
	return strconv.Quote(source)
}

func renderSignalExpiry(expiry SignalExpiry) string {
	if expiry.Mode == "at" {
		return "expires " + expiry.Literal
	}
	return expiry.Mode + " " + expiry.Literal
}

func renderValue(value Value) string {
	switch value.Kind {
	case "bool":
		if value.Bool {
			return "true"
		}
		return "false"
	case "string":
		return strconv.Quote(value.String)
	case "number":
		return renderNumber(value.Number)
	default:
		return ""
	}
}

func renderTemporalPredicate(predicate Predicate) string {
	if predicate.Temporal == nil {
		return "need " + predicate.Field + " temporal"
	}
	temporal := predicate.Temporal
	switch temporal.Relation {
	case "before":
		return fmt.Sprintf("need %s before %s", predicate.Field, renderTemporalReference(temporal.Reference))
	case "after":
		return fmt.Sprintf("need %s after %s", predicate.Field, renderTemporalReference(temporal.Reference))
	case "within_before":
		return fmt.Sprintf("need %s within %s before %s", predicate.Field, temporal.WindowLiteral, renderTemporalReference(temporal.Reference))
	case "within_after":
		return fmt.Sprintf("need %s within %s after %s", predicate.Field, temporal.WindowLiteral, renderTemporalReference(temporal.Reference))
	case "age_lte":
		return fmt.Sprintf("need %s age <= %s", predicate.Field, temporal.WindowLiteral)
	case "age_gte":
		return fmt.Sprintf("need %s age >= %s", predicate.Field, temporal.WindowLiteral)
	default:
		return "need " + predicate.Field + " temporal"
	}
}

func renderTemporalReference(reference string) string {
	if reference == "now" || identifierPattern.MatchString(reference) {
		return reference
	}
	return strconv.Quote(reference)
}

func renderNumber(number float64) string {
	return strconv.FormatFloat(number, 'f', -1, 64)
}
