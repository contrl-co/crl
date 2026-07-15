package crl

import (
	"fmt"
	"strconv"
	"strings"
)

func containsCloseBrace(tokens []Token) bool {
	for _, token := range tokens {
		if token.Kind == TokenRBrace {
			return true
		}
	}
	return false
}

func isRuleBodyKeyword(keyword string) bool {
	switch keyword {
	case "target", "collector", "signal", PredicateNeed, PredicateBlock, PredicateQuorum:
		return true
	default:
		return false
	}
}

func parseCollector(fields []string) (Collector, error) {
	if len(fields) != 6 && len(fields) != 8 {
		return Collector{}, fmt.Errorf("%w: collector must look like collector <name> <provider_type> <connector_kind> from <source> [schema <schema>]", ErrInvalidSyntax)
	}
	if fields[4] != "from" {
		return Collector{}, fmt.Errorf("%w: collector must use from <source>", ErrInvalidSyntax)
	}
	collector := Collector{
		Name:          fields[1],
		ProviderType:  fields[2],
		ConnectorKind: fields[3],
		Source:        unquote(fields[5]),
	}
	if len(fields) == 8 {
		if fields[6] != "schema" {
			return Collector{}, fmt.Errorf("%w: expected schema <schema>", ErrInvalidSyntax)
		}
		collector.Schema = fields[7]
	}
	return normalizeCollector(collector)
}

func parseSignal(fields []string) (Signal, error) {
	if len(fields) < 7 || fields[3] != "from" {
		return Signal{}, fmt.Errorf("%w: signal must look like signal <name> <kind> from <field_path> [unit <unit>] [required|optional] ttl <duration|expires>", ErrInvalidSyntax)
	}
	signal := Signal{
		Name:        fields[1],
		Kind:        fields[2],
		SourceField: unquote(fields[4]),
	}
	i := 5
	for i < len(fields) {
		switch fields[i] {
		case "unit":
			if i+1 >= len(fields) {
				return Signal{}, fmt.Errorf("%w: missing signal unit", ErrInvalidSyntax)
			}
			signal.Unit = fields[i+1]
			i += 2
		case "required":
			signal.Optional = false
			i++
		case "optional":
			signal.Optional = true
			i++
		case "ttl", "expires":
			if i+1 >= len(fields) {
				return Signal{}, fmt.Errorf("%w: missing signal expiry", ErrInvalidSyntax)
			}
			expiry, err := parseSignalExpiry(fields[i], fields[i+1])
			if err != nil {
				return Signal{}, err
			}
			signal.Expiry = expiry
			i += 2
			if i != len(fields) {
				return Signal{}, fmt.Errorf("%w: unexpected signal tokens after expiry", ErrInvalidSyntax)
			}
		default:
			return Signal{}, fmt.Errorf("%w: unexpected signal token %q", ErrInvalidSyntax, fields[i])
		}
	}
	if signal.Expiry.Mode == "" {
		return Signal{}, fmt.Errorf("%w: missing signal expiry", ErrInvalidSyntax)
	}
	return normalizeSignal(signal)
}

func parseNeed(field, op, rawValue string) (Predicate, error) {
	if temporal, ok, err := parseTemporalNeed(field, op, rawValue); ok || err != nil {
		return temporal, err
	}
	if err := validateOperator(op); err != nil {
		return Predicate{}, err
	}
	value, err := parseValue(rawValue)
	if err != nil {
		return Predicate{}, err
	}
	return normalizePredicate(Predicate{Kind: PredicateNeed, Field: field, Operator: op, Value: value})
}

func parseTemporalNeed(field, op, rawValue string) (Predicate, bool, error) {
	parts := strings.Fields(rawValue)
	var expression TemporalExpression
	switch op {
	case "before", "after":
		if len(parts) != 1 {
			return Predicate{}, true, fmt.Errorf("%w: need %s %s requires one reference", ErrInvalidSyntax, field, op)
		}
		expression = TemporalExpression{Relation: op, Reference: parts[0]}
	case "within":
		if len(parts) != 3 {
			return Predicate{}, true, fmt.Errorf("%w: need %s within requires <duration> before|after <reference>", ErrInvalidSyntax, field)
		}
		switch parts[1] {
		case "before":
			expression.Relation = "within_before"
		case "after":
			expression.Relation = "within_after"
		default:
			return Predicate{}, true, fmt.Errorf("%w: temporal within requires before or after", ErrInvalidSyntax)
		}
		expression.WindowLiteral = parts[0]
		expression.Reference = parts[2]
	case "age":
		if len(parts) != 2 {
			return Predicate{}, true, fmt.Errorf("%w: need %s age requires <=|>= <duration>", ErrInvalidSyntax, field)
		}
		switch parts[0] {
		case OperatorLTE:
			expression.Relation = "age_lte"
		case OperatorGTE:
			expression.Relation = "age_gte"
		default:
			return Predicate{}, true, fmt.Errorf("%w: temporal age supports <= or >=", ErrInvalidSyntax)
		}
		expression.WindowLiteral = parts[1]
	default:
		return Predicate{}, false, nil
	}
	predicate, err := normalizePredicate(Predicate{Kind: PredicateTemporal, Field: field, Temporal: &expression})
	return predicate, true, err
}

func parseBundlePredicate(fields []string) (Predicate, error) {
	switch {
	case len(fields) >= 4 && fields[0] == PredicateNeed:
		return parseNeed(fields[1], fields[2], strings.Join(fields[3:], " "))
	case len(fields) == 2 && fields[0] == PredicateBlock:
		return parseBlock(fields[1])
	case len(fields) >= 2 && fields[0] == PredicateQuorum:
		return parseQuorum(fields[1:])
	default:
		return Predicate{}, ErrInvalidSyntax
	}
}

func parseBlock(field string) (Predicate, error) {
	return normalizePredicate(Predicate{
		Kind:     PredicateBlock,
		Field:    field,
		Operator: OperatorEQ,
		Value:    Value{Kind: "bool"},
	})
}

func parseQuorum(fields []string) (Predicate, error) {
	if pred, ok, err := parseOfQuorum(fields); ok || err != nil {
		return pred, err
	}
	opIndex := -1
	for i, field := range fields {
		if field == OperatorGTE {
			opIndex = i
			break
		}
	}
	if opIndex >= 1 && opIndex == len(fields)-2 {
		providers, ok, err := parseCountQuorumProviders(strings.Join(fields[:opIndex], " "))
		if err != nil {
			return Predicate{}, err
		}
		if ok {
			value, err := parseValue(fields[len(fields)-1])
			if err != nil {
				return Predicate{}, err
			}
			if value.Kind != "number" || value.Number < 1 || value.Number != float64(int(value.Number)) {
				return Predicate{}, fmt.Errorf("%w: invalid quorum count", ErrInvalidSyntax)
			}
			return normalizePredicate(Predicate{
				Kind:      PredicateQuorum,
				Field:     PredicateQuorum,
				Operator:  OperatorGTE,
				Value:     value,
				Providers: providers,
			})
		}
	}
	if opIndex < 2 || opIndex != len(fields)-2 {
		expression, err := parseQuorumExpression(fields)
		if err != nil {
			return Predicate{}, err
		}
		return normalizePredicate(Predicate{
			Kind:       PredicateQuorum,
			Field:      PredicateQuorum,
			Operator:   OperatorEQ,
			Value:      Value{Kind: "bool", Bool: true},
			Expression: expression,
		})
	}
	expression := fields[:opIndex]
	if len(expression)%2 == 0 {
		return Predicate{}, fmt.Errorf("%w: invalid quorum providers", ErrInvalidSyntax)
	}
	providers := make([]string, 0, (len(expression)+1)/2)
	for i, token := range expression {
		if i%2 == 1 {
			if token != "+" {
				return Predicate{}, fmt.Errorf("%w: expected +", ErrInvalidSyntax)
			}
			continue
		}
		providers = append(providers, token)
	}
	value, err := parseValue(fields[len(fields)-1])
	if err != nil {
		return Predicate{}, err
	}
	if value.Kind != "number" || value.Number < 1 || value.Number != float64(int(value.Number)) {
		return Predicate{}, fmt.Errorf("%w: invalid quorum count", ErrInvalidSyntax)
	}
	return normalizePredicate(Predicate{
		Kind:      PredicateQuorum,
		Field:     PredicateQuorum,
		Operator:  OperatorGTE,
		Value:     value,
		Providers: providers,
	})
}

// parseOfQuorum desugars the `N of M a b c` count-quorum surface form
// into the canonical count(...) >= N predicate. It is pure sugar: the
// compiled result — and therefore the bundle hash — is identical to
// `quorum count(a, b, c) >= N`, and the canonical text renders the
// count() form. M must equal the number of listed subjects, and the
// threshold N must be within 1..M. The form is recognised only when
// the first and third tokens are integers around the `of` keyword, so
// it never shadows a boolean or count() quorum.
func parseOfQuorum(fields []string) (Predicate, bool, error) {
	if len(fields) < 4 || fields[1] != "of" {
		return Predicate{}, false, nil
	}
	n, nErr := strconv.Atoi(fields[0])
	m, mErr := strconv.Atoi(fields[2])
	if nErr != nil || mErr != nil {
		return Predicate{}, false, nil
	}
	subjects := fields[3:]
	if m != len(subjects) {
		return Predicate{}, true, fmt.Errorf("%w: quorum %q lists %d subjects but says %d", ErrInvalidSyntax, fmt.Sprintf("%d of %d", n, m), len(subjects), m)
	}
	if n < 1 || n > m {
		return Predicate{}, true, fmt.Errorf("%w: quorum threshold %d out of range 1..%d", ErrInvalidSyntax, n, m)
	}
	pred, err := normalizePredicate(Predicate{
		Kind:      PredicateQuorum,
		Field:     PredicateQuorum,
		Operator:  OperatorGTE,
		Value:     Value{Kind: "number", Number: float64(n)},
		Providers: subjects,
	})
	return pred, true, err
}

func parseCountQuorumProviders(raw string) ([]string, bool, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "count(") {
		return nil, false, nil
	}
	if !strings.HasSuffix(raw, ")") {
		return nil, true, fmt.Errorf("%w: invalid count quorum", ErrInvalidSyntax)
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "count("), ")"))
	if inner == "" {
		return nil, true, fmt.Errorf("%w: missing quorum providers", ErrInvalidSyntax)
	}
	parts := strings.Split(inner, ",")
	providers := make([]string, 0, len(parts))
	for _, part := range parts {
		provider := strings.TrimSpace(part)
		if provider == "" {
			return nil, true, fmt.Errorf("%w: missing quorum provider", ErrInvalidSyntax)
		}
		providers = append(providers, provider)
	}
	return providers, true, nil
}

func parseClusterRules(fields []string) ([]string, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: cluster rules cannot be empty", ErrInvalidSyntax)
	}
	var rules []string
	expectRule := true
	for _, field := range fields {
		if field == "+" {
			if expectRule {
				return nil, fmt.Errorf("%w: unexpected + in cluster rules", ErrInvalidSyntax)
			}
			expectRule = true
			continue
		}
		if !expectRule {
			return nil, fmt.Errorf("%w: expected + in cluster rules", ErrInvalidSyntax)
		}
		rule := normalizeIdentifier(field)
		if !identifierPattern.MatchString(rule) {
			return nil, fmt.Errorf("%w: invalid cluster rule %q", ErrInvalidSyntax, field)
		}
		rules = append(rules, rule)
		expectRule = false
	}
	if expectRule {
		return nil, fmt.Errorf("%w: trailing + in cluster rules", ErrInvalidSyntax)
	}
	return rules, nil
}
