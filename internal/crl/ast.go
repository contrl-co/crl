package crl

import (
	"fmt"
)

type SourceSpan struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Document struct {
	Version       string            `json:"version"`
	Name          string            `json:"name,omitempty"`
	Package       string            `json:"package,omitempty"`
	AbstractRules []RuleObject      `json:"abstract_rules,omitempty"`
	Rules         []RuleObject      `json:"rules"`
	Clusters      []ClusterObject   `json:"clusters,omitempty"`
	Predicates    []PredicateObject `json:"predicates,omitempty"`
}

type RuleObject struct {
	Span       SourceSpan        `json:"span"`
	Name       string            `json:"name"`
	Abstract   bool              `json:"abstract,omitempty"`
	Extends    string            `json:"extends,omitempty"`
	Target     string            `json:"target"`
	Collectors []CollectorObject `json:"collectors"`
	Predicates []PredicateObject `json:"predicates,omitempty"`
}

type CollectorObject struct {
	Span          SourceSpan     `json:"span"`
	Name          string         `json:"name"`
	ProviderType  string         `json:"provider_type"`
	ConnectorKind string         `json:"connector_kind"`
	Source        string         `json:"source"`
	Schema        string         `json:"schema,omitempty"`
	Signals       []SignalObject `json:"signals"`
}

type SignalObject struct {
	Span   SourceSpan `json:"span"`
	Signal Signal     `json:"signal"`
}

type ClusterObject struct {
	Span       SourceSpan        `json:"span"`
	Name       string            `json:"name"`
	Rules      []string          `json:"rules"`
	Predicates []PredicateObject `json:"predicates,omitempty"`
}

type PredicateObject struct {
	Span      SourceSpan `json:"span"`
	Fields    []string   `json:"fields"`
	Predicate Predicate  `json:"predicate"`
}

func BuildDocument(tree SyntaxTree) (Document, error) {
	document := Document{Version: Version}
	var currentRule *RuleObject
	var currentCluster *ClusterObject
	var currentCollector *CollectorObject
	var blockStack []string

	flushCollector := func() {
		if currentRule != nil && currentCollector != nil {
			currentRule.Collectors = append(currentRule.Collectors, *currentCollector)
		}
		currentCollector = nil
	}
	flushRule := func() {
		flushCollector()
		if currentRule != nil {
			if currentRule.Abstract {
				document.AbstractRules = append(document.AbstractRules, *currentRule)
			} else {
				document.Rules = append(document.Rules, *currentRule)
			}
		}
		currentRule = nil
	}
	flushCluster := func() {
		if currentCluster != nil {
			document.Clusters = append(document.Clusters, *currentCluster)
		}
		currentCluster = nil
	}
	openBlock := func(kind string) {
		blockStack = append(blockStack, kind)
	}
	closeBlock := func(line int) error {
		if len(blockStack) == 0 {
			return fmt.Errorf("%w at line %d: unexpected }", ErrInvalidSyntax, line)
		}
		kind := blockStack[len(blockStack)-1]
		blockStack = blockStack[:len(blockStack)-1]
		switch kind {
		case "collector":
			flushCollector()
		case "rule":
			flushRule()
		case "constructor":
			flushRule()
		case "cluster":
			flushCluster()
		case "bundle":
		default:
			return fmt.Errorf("%w at line %d: invalid block %q", ErrInvalidSyntax, line, kind)
		}
		return nil
	}

	for _, statement := range tree.Statements {
		if statement.ClosesBlockOnly() {
			if err := closeBlock(statement.Line); err != nil {
				return Document{}, err
			}
			continue
		}
		if containsCloseBrace(statement.Tokens) {
			return Document{}, fmt.Errorf("%w at line %d: } must close a block on its own line", ErrInvalidSyntax, statement.Line)
		}
		fields := statement.Fields()
		if len(fields) == 0 {
			return Document{}, fmt.Errorf("%w at line %d", ErrInvalidSyntax, statement.Line)
		}
		keyword := fields[0]
		forceRuleBody := statement.Indent == 0 && currentRule != nil && currentCluster == nil && isRuleBodyKeyword(keyword)
		atTopLevel := (statement.Indent == 0 && len(blockStack) == 0) || (inBundleBlock(blockStack) && currentRule == nil && currentCluster == nil)
		if atTopLevel && !forceRuleBody {
			switch keyword {
			case "crl":
				if len(fields) != 2 || (fields[1] != "v1" && fields[1] != Version) {
					return Document{}, fmt.Errorf("%w at line %d: unsupported version", ErrInvalidSyntax, statement.Line)
				}
				document.Version = Version
				continue
			case "package":
				if len(fields) != 2 {
					return Document{}, fmt.Errorf("%w at line %d: package must look like package <name>", ErrInvalidSyntax, statement.Line)
				}
				document.Package = fields[1]
				continue
			case "bundle":
				if len(fields) != 2 {
					return Document{}, fmt.Errorf("%w at line %d: bundle must look like bundle <name>", ErrInvalidSyntax, statement.Line)
				}
				document.Name = fields[1]
				if statement.OpensBlock() {
					openBlock("bundle")
				}
				continue
			case "abstract":
				if len(fields) != 3 && len(fields) != 5 {
					return Document{}, fmt.Errorf("%w at line %d: abstract rule must look like abstract rule <name> [extends <name>]", ErrInvalidSyntax, statement.Line)
				}
				if fields[1] != "rule" {
					return Document{}, fmt.Errorf("%w at line %d: expected abstract rule", ErrInvalidSyntax, statement.Line)
				}
				if len(fields) == 5 && fields[3] != "extends" {
					return Document{}, fmt.Errorf("%w at line %d: expected extends <name>", ErrInvalidSyntax, statement.Line)
				}
				flushCluster()
				flushRule()
				currentRule = &RuleObject{Span: statement.Span(), Name: fields[2], Abstract: true}
				if len(fields) == 5 {
					currentRule.Extends = fields[4]
				}
				if statement.OpensBlock() {
					openBlock("rule")
				}
				continue
			case "constructor":
				if len(fields) != 2 && len(fields) != 4 {
					return Document{}, fmt.Errorf("%w at line %d: constructor must look like constructor <name> [extends <name>]", ErrInvalidSyntax, statement.Line)
				}
				if len(fields) == 4 && fields[2] != "extends" {
					return Document{}, fmt.Errorf("%w at line %d: expected extends <name>", ErrInvalidSyntax, statement.Line)
				}
				flushCluster()
				flushRule()
				currentRule = &RuleObject{Span: statement.Span(), Name: fields[1], Abstract: true}
				if len(fields) == 4 {
					currentRule.Extends = fields[3]
				}
				if statement.OpensBlock() {
					openBlock("constructor")
				}
				continue
			case "rule":
				if len(fields) != 2 && len(fields) != 4 {
					return Document{}, fmt.Errorf("%w at line %d: rule must look like rule <name> [extends <name>]", ErrInvalidSyntax, statement.Line)
				}
				if len(fields) == 4 && fields[2] != "extends" {
					return Document{}, fmt.Errorf("%w at line %d: expected extends <name>", ErrInvalidSyntax, statement.Line)
				}
				flushCluster()
				flushRule()
				currentRule = &RuleObject{Span: statement.Span(), Name: fields[1]}
				if len(fields) == 4 {
					currentRule.Extends = fields[3]
				}
				if statement.OpensBlock() {
					openBlock("rule")
				}
				continue
			case "cluster":
				if len(fields) != 2 {
					return Document{}, fmt.Errorf("%w at line %d: cluster must look like cluster <name>", ErrInvalidSyntax, statement.Line)
				}
				flushRule()
				flushCluster()
				currentCluster = &ClusterObject{Span: statement.Span(), Name: fields[1]}
				if statement.OpensBlock() {
					openBlock("cluster")
				}
				continue
			case PredicateNeed, PredicateBlock, PredicateQuorum:
				flushRule()
				flushCluster()
				predicate, err := parsePredicateObject(statement, fields)
				if err != nil {
					return Document{}, err
				}
				document.Predicates = append(document.Predicates, predicate)
				continue
			default:
				return Document{}, fmt.Errorf("%w at line %d", ErrInvalidSyntax, statement.Line)
			}
		}

		switch {
		case currentRule != nil:
			switch keyword {
			case "target":
				if len(fields) != 2 {
					return Document{}, fmt.Errorf("%w at line %d: target must look like target <aspect>", ErrInvalidSyntax, statement.Line)
				}
				currentRule.Target = fields[1]
			case "collector":
				flushCollector()
				collector, err := parseCollector(fields)
				if err != nil {
					return Document{}, fmt.Errorf("line %d: %w", statement.Line, err)
				}
				currentCollector = &CollectorObject{
					Span:          statement.Span(),
					Name:          collector.Name,
					ProviderType:  collector.ProviderType,
					ConnectorKind: collector.ConnectorKind,
					Source:        collector.Source,
					Schema:        collector.Schema,
				}
				if statement.OpensBlock() {
					openBlock("collector")
				}
			case "signal":
				if currentCollector == nil {
					return Document{}, fmt.Errorf("line %d: %w: signal must follow collector", statement.Line, ErrInvalidSyntax)
				}
				signal, err := parseSignal(fields)
				if err != nil {
					return Document{}, fmt.Errorf("line %d: %w", statement.Line, err)
				}
				currentCollector.Signals = append(currentCollector.Signals, SignalObject{Span: statement.Span(), Signal: signal})
			case PredicateNeed, PredicateBlock, PredicateQuorum:
				flushCollector()
				predicate, err := parsePredicateObject(statement, fields)
				if err != nil {
					return Document{}, err
				}
				currentRule.Predicates = append(currentRule.Predicates, predicate)
			default:
				return Document{}, fmt.Errorf("%w at line %d", ErrInvalidSyntax, statement.Line)
			}
		case currentCluster != nil:
			switch keyword {
			case "rules":
				if currentCluster.Rules != nil {
					return Document{}, fmt.Errorf("line %d: %w: duplicate rules statement in cluster", statement.Line, ErrInvalidSyntax)
				}
				rules, err := parseClusterRules(fields[1:])
				if err != nil {
					return Document{}, fmt.Errorf("line %d: %w", statement.Line, err)
				}
				currentCluster.Rules = rules
			case PredicateNeed, PredicateBlock, PredicateQuorum:
				predicate, err := parsePredicateObject(statement, fields)
				if err != nil {
					return Document{}, err
				}
				currentCluster.Predicates = append(currentCluster.Predicates, predicate)
			default:
				return Document{}, fmt.Errorf("%w at line %d", ErrInvalidSyntax, statement.Line)
			}
		default:
			return Document{}, fmt.Errorf("%w at line %d: indented line has no active block", ErrInvalidSyntax, statement.Line)
		}
	}
	if len(blockStack) > 0 {
		return Document{}, fmt.Errorf("%w: unclosed %s block", ErrInvalidSyntax, blockStack[len(blockStack)-1])
	}
	flushRule()
	flushCluster()
	return expandAbstractRules(document)
}

func (s SyntaxStatement) Span() SourceSpan {
	return SourceSpan{Line: s.Line, Column: s.Column}
}

func inBundleBlock(stack []string) bool {
	return len(stack) == 1 && stack[0] == "bundle"
}

// expandAbstractRules runs even when the document declares no abstract
// rules: an `extends` clause must always resolve, and a dangling parent
// must fail loudly. Skipping expansion would silently drop the clause
// and ship a rule missing every inherited collector and predicate.
func expandAbstractRules(document Document) (Document, error) {
	abstracts := make(map[string]RuleObject, len(document.AbstractRules))
	for _, abstract := range document.AbstractRules {
		name := normalizeIdentifier(abstract.Name)
		if name == "" {
			return Document{}, fmt.Errorf("%w: invalid abstract rule %q", ErrInvalidSyntax, abstract.Name)
		}
		if _, ok := abstracts[name]; ok {
			return Document{}, fmt.Errorf("%w: duplicate abstract rule %q", ErrInvalidSyntax, abstract.Name)
		}
		abstract.Name = name
		abstract.Extends = normalizeIdentifier(abstract.Extends)
		abstracts[name] = abstract
	}
	for i, rule := range document.Rules {
		expanded, err := expandRuleObject(rule, abstracts, map[string]struct{}{})
		if err != nil {
			return Document{}, err
		}
		document.Rules[i] = expanded
	}
	return document, nil
}

func expandRuleObject(rule RuleObject, abstracts map[string]RuleObject, stack map[string]struct{}) (RuleObject, error) {
	parentName := normalizeIdentifier(rule.Extends)
	rule.Extends = ""
	if parentName == "" {
		return rule, nil
	}
	parent, ok := abstracts[parentName]
	if !ok {
		return RuleObject{}, fmt.Errorf("%w: unknown abstract rule %q", ErrInvalidSyntax, parentName)
	}
	if _, ok := stack[parentName]; ok {
		return RuleObject{}, fmt.Errorf("%w: cyclic abstract rule %q", ErrInvalidSyntax, parentName)
	}
	stack[parentName] = struct{}{}
	parent, err := expandRuleObject(parent, abstracts, stack)
	if err != nil {
		return RuleObject{}, err
	}
	delete(stack, parentName)
	merged := rule
	if merged.Target == "" {
		merged.Target = parent.Target
	}
	merged.Collectors = append(append([]CollectorObject(nil), parent.Collectors...), merged.Collectors...)
	merged.Predicates = append(append([]PredicateObject(nil), parent.Predicates...), merged.Predicates...)
	return merged, nil
}

func parsePredicateObject(statement SyntaxStatement, fields []string) (PredicateObject, error) {
	predicate, err := parseBundlePredicate(fields)
	if err != nil {
		return PredicateObject{}, fmt.Errorf("line %d: %w", statement.Line, err)
	}
	return PredicateObject{Span: statement.Span(), Fields: fields, Predicate: predicate}, nil
}

func (d Document) Bundle() Bundle {
	bundle := Bundle{
		Version:    d.Version,
		Name:       d.Name,
		Package:    d.Package,
		Rules:      make([]Rule, 0, len(d.Rules)),
		Clusters:   make([]Cluster, 0, len(d.Clusters)),
		Predicates: predicatesFromObjects(d.Predicates),
	}
	for _, ruleObject := range d.Rules {
		rule := Rule{
			Name:       ruleObject.Name,
			Target:     ruleObject.Target,
			Collectors: collectorsFromObjects(ruleObject.Collectors),
			Predicates: predicatesFromObjects(ruleObject.Predicates),
		}
		bundle.Rules = append(bundle.Rules, rule)
	}
	for _, clusterObject := range d.Clusters {
		cluster := Cluster{
			Name:       clusterObject.Name,
			Rules:      append([]string(nil), clusterObject.Rules...),
			Predicates: predicatesFromObjects(clusterObject.Predicates),
		}
		bundle.Clusters = append(bundle.Clusters, cluster)
	}
	return bundle
}

func collectorsFromObjects(objects []CollectorObject) []Collector {
	collectors := make([]Collector, 0, len(objects))
	for _, object := range objects {
		collector := Collector{
			Name:          object.Name,
			ProviderType:  object.ProviderType,
			ConnectorKind: object.ConnectorKind,
			Source:        object.Source,
			Schema:        object.Schema,
			Signals:       signalsFromObjects(object.Signals),
		}
		collectors = append(collectors, collector)
	}
	return collectors
}

func signalsFromObjects(objects []SignalObject) []Signal {
	signals := make([]Signal, 0, len(objects))
	for _, object := range objects {
		signals = append(signals, object.Signal)
	}
	return signals
}

func predicatesFromObjects(objects []PredicateObject) []Predicate {
	predicates := make([]Predicate, 0, len(objects))
	for _, object := range objects {
		predicates = append(predicates, object.Predicate)
	}
	return predicates
}
