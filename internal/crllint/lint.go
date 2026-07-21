package crllint

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gitlab.com/contrl-group/crl/internal/crl"
	"golang.org/x/text/unicode/norm"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Options struct {
	IncludeCanonical bool
}

type Diagnostic struct {
	Path     string   `json:"path,omitempty"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
}

type Report struct {
	Path          string       `json:"path,omitempty"`
	OK            bool         `json:"ok"`
	CompiledHash  string       `json:"compiled_hash,omitempty"`
	CanonicalText string       `json:"canonical_text,omitempty"`
	Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`
}

func LintFile(path string, opts Options) (Report, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	return LintSource(path, string(source), opts), nil
}

func LintSource(path, source string, opts Options) Report {
	report := Report{Path: path, OK: true}
	add := func(diagnostic Diagnostic) {
		diagnostic.Path = path
		if diagnostic.Line < 1 {
			diagnostic.Line = 1
		}
		if diagnostic.Column < 1 {
			diagnostic.Column = 1
		}
		report.Diagnostics = append(report.Diagnostics, diagnostic)
		if diagnostic.Severity == SeverityError {
			report.OK = false
		}
	}

	tree, err := crl.Parse(source)
	if err != nil {
		add(errorDiagnostic("CRL100", err))
		sortDiagnostics(report.Diagnostics)
		return report
	}
	addStyleDiagnostics(tree, &add)

	document, err := crl.BuildDocument(tree)
	if err != nil {
		add(errorDiagnostic("CRL110", err))
		sortDiagnostics(report.Diagnostics)
		return report
	}
	addDocumentDiagnostics(document, &add)

	compilation, err := crl.CompileLanguage(source)
	if err != nil {
		diagnostic := errorDiagnostic("CRL120", err)
		if diagnostic.Line == 1 {
			if span, ok := spanForCompilerError(document, err.Error()); ok {
				diagnostic.Line = span.Line
				diagnostic.Column = span.Column
			}
		}
		add(diagnostic)
		sortDiagnostics(report.Diagnostics)
		return report
	}
	report.CompiledHash = compilation.Hash
	if opts.IncludeCanonical {
		report.CanonicalText = compilation.CanonicalText
	}
	sortDiagnostics(report.Diagnostics)
	return report
}

func addStyleDiagnostics(tree crl.SyntaxTree, add *func(Diagnostic)) {
	if len(tree.Statements) == 0 {
		(*add)(Diagnostic{
			Severity: SeverityError,
			Code:     "CRL101",
			Message:  "CRL source must contain at least one statement",
		})
		return
	}
	first := tree.Statements[0]
	fields := first.Fields()
	if len(fields) == 0 || fields[0] != "crl" {
		(*add)(Diagnostic{
			Line:     first.Line,
			Column:   first.Column,
			Severity: SeverityWarning,
			Code:     "CRL200",
			Message:  "start CRL source with an explicit `crl v1` version statement",
		})
	}
}

func addDocumentDiagnostics(document crl.Document, add *func(Diagnostic)) {
	if strings.TrimSpace(document.Package) == "" {
		(*add)(Diagnostic{
			Severity: SeverityWarning,
			Code:     "CRL201",
			Message:  "add a package declaration to namespace hand-authored bundles",
		})
	}
	if strings.TrimSpace(document.Name) == "" {
		(*add)(Diagnostic{
			Severity: SeverityWarning,
			Code:     "CRL202",
			Message:  "add a bundle name so compiled lineage can be read without external context",
		})
	}
	if len(document.Predicates) == 0 && (len(document.Rules)+len(document.Clusters)) > 1 {
		(*add)(Diagnostic{
			Severity: SeverityWarning,
			Code:     "CRL203",
			Message:  "multiple rules or clusters without a top-level final policy require every object to authorize",
		})
	}
	for _, rule := range document.Rules {
		if !strings.Contains(rule.Target, ".") {
			(*add)(Diagnostic{
				Line:     rule.Span.Line,
				Column:   rule.Span.Column,
				Severity: SeverityWarning,
				Code:     "CRL204",
				Message:  fmt.Sprintf("target %q has no namespace segment", rule.Target),
			})
		}
		for _, collector := range rule.Collectors {
			addDuplicateSourceFieldDiagnostics(rule.Name, collector, add)
		}
	}
	// Expiry-rounding (CRL206/207) and block-naming (CRL208) diagnostics
	// apply to every scope a signal or block predicate can appear in:
	// concrete rules, ABSTRACT rules (whose signals/blocks are inherited
	// into every `extends` rule), and CLUSTER predicates. Walking only
	// document.Rules would silently skip abstract and cluster authors.
	authoredRules := make([]crl.RuleObject, 0, len(document.Rules)+len(document.AbstractRules))
	authoredRules = append(authoredRules, document.Rules...)
	authoredRules = append(authoredRules, document.AbstractRules...)
	for _, rule := range authoredRules {
		for _, collector := range rule.Collectors {
			for _, signal := range collector.Signals {
				addExpiryRoundingDiagnostics(signal, add)
			}
		}
		for _, predicate := range rule.Predicates {
			addBlockNamingDiagnostics(predicate, add)
		}
		addQuorumIndependenceDiagnostics(rule, add)
	}
	for _, cluster := range document.Clusters {
		for _, predicate := range cluster.Predicates {
			addBlockNamingDiagnostics(predicate, add)
		}
	}
	for _, predicate := range document.Predicates {
		addBlockNamingDiagnostics(predicate, add)
	}
	for _, rule := range document.Rules {
		for _, predicate := range rule.Predicates {
			if predicate.CarvedOut {
				(*add)(Diagnostic{
					Line:     predicate.Span.Line,
					Column:   predicate.Span.Column,
					Severity: SeverityWarning,
					Code:     "CRL210",
					Message: fmt.Sprintf(
						"unindented %q after rule %q was scoped INTO that rule by the rule-body carve-out; to declare a global final policy, put it before the rules",
						predicate.Predicate.Kind, rule.Name,
					),
				})
			}
		}
	}
	addUnreferencedSignalDiagnostics(document, add)
}

// addUnreferencedSignalDiagnostics warns about a signal that no predicate
// reads. Such a signal does not affect the decision, so dropping the one
// predicate that used it — a plausible merge accident — silently removes a
// blocker or requirement with nothing to flag it.
func addUnreferencedSignalDiagnostics(document crl.Document, add *func(Diagnostic)) {
	fold := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	referenced := map[string]struct{}{}
	mark := func(name string) {
		if name = fold(name); name != "" {
			referenced[name] = struct{}{}
		}
	}
	markPredicate := func(predicate crl.Predicate) {
		mark(predicate.Field)
		for _, provider := range predicate.Providers {
			mark(provider)
		}
		if predicate.Expression != nil {
			for _, subject := range crl.QuorumExpressionSubjects(predicate.Expression) {
				mark(subject)
			}
		}
		if predicate.Temporal != nil {
			mark(predicate.Temporal.Reference)
		}
	}
	authoredRules := make([]crl.RuleObject, 0, len(document.Rules)+len(document.AbstractRules))
	authoredRules = append(authoredRules, document.Rules...)
	authoredRules = append(authoredRules, document.AbstractRules...)
	for _, rule := range authoredRules {
		for _, predicate := range rule.Predicates {
			markPredicate(predicate.Predicate)
		}
	}
	for _, cluster := range document.Clusters {
		for _, predicate := range cluster.Predicates {
			markPredicate(predicate.Predicate)
		}
	}
	for _, predicate := range document.Predicates {
		markPredicate(predicate.Predicate)
	}
	for _, rule := range authoredRules {
		for _, collector := range rule.Collectors {
			// A quorum over a collector reads that collector's own presence
			// fact, not its signals, so being named in a quorum does not make
			// a collector's signals used. But a collector must declare at
			// least one signal, so when a presence-referenced collector has a
			// single, otherwise-unused signal, that signal is structurally
			// required and dropping it is not an option — flagging it would be
			// noise. A collector with several signals has no such excuse: the
			// unreferenced ones past the first are genuinely removable.
			_, collectorReferenced := referenced[fold(collector.Name)]
			structurallyRequired := collectorReferenced && len(collector.Signals) == 1
			for _, signal := range collector.Signals {
				if _, ok := referenced[fold(signal.Signal.Name)]; ok {
					continue
				}
				if structurallyRequired {
					continue
				}
				(*add)(Diagnostic{
					Line:     signal.Span.Line,
					Column:   signal.Span.Column,
					Severity: SeverityWarning,
					Code:     "CRL209",
					Message: fmt.Sprintf(
						"signal %q is declared but never referenced by a need, block, quorum, or temporal predicate; it does not affect the decision",
						signal.Signal.Name,
					),
				})
			}
		}
	}
}

// addExpiryRoundingDiagnostics surfaces the silent canonicalisation in
// duration parsing: any <n>ms TTL compiles to exactly 1 second (the
// value is discarded), and 1y is exactly 365 days with no leap-year
// handling. Both shift EXPIRED boundaries away from what the author
// plausibly meant, so they warrant a warning even though they compile.
func addExpiryRoundingDiagnostics(signal crl.SignalObject, add *func(Diagnostic)) {
	expiry := signal.Signal.Expiry
	if expiry.Mode != "ttl" {
		return
	}
	literal := strings.ToLower(strings.TrimSpace(expiry.Literal))
	switch {
	case strings.HasSuffix(literal, "ms") && subSecondMillis(literal):
		(*add)(Diagnostic{
			Line:     signal.Span.Line,
			Column:   signal.Span.Column,
			Severity: SeverityWarning,
			Code:     "CRL206",
			Message: fmt.Sprintf(
				"ttl %q rounds up to the next whole second: durations have one-second granularity, so a sub-second value is not represented exactly",
				expiry.Literal,
			),
		})
	case strings.HasSuffix(literal, "y"):
		(*add)(Diagnostic{
			Line:     signal.Span.Line,
			Column:   signal.Span.Column,
			Severity: SeverityWarning,
			Code:     "CRL207",
			Message: fmt.Sprintf(
				"ttl %q counts every year as exactly 365 days (no leap-year handling); spell the intent in days if the boundary matters",
				expiry.Literal,
			),
		})
	}
}

// addBlockNamingDiagnostics flags block fields whose NAME suggests
// expiry semantics. The evaluator reports an active blocker as BLOCKED
// regardless of its name — EXPIRED comes only from a declared signal
// ttl/expires or a temporal predicate — so a field named like an
// expiry flag will not produce the EXPIRED outcome its name implies.
func addBlockNamingDiagnostics(predicate crl.PredicateObject, add *func(Diagnostic)) {
	if predicate.Predicate.Kind != crl.PredicateBlock {
		return
	}
	field := strings.ToLower(predicate.Predicate.Field)
	if strings.Contains(field, "expired") || strings.HasSuffix(field, "_expires") {
		(*add)(Diagnostic{
			Line:     predicate.Span.Line,
			Column:   predicate.Span.Column,
			Severity: SeverityWarning,
			Code:     "CRL208",
			Message: fmt.Sprintf(
				"block field %q reads like an expiry flag but reports BLOCKED, not EXPIRED; declare a signal ttl/expires or use a temporal predicate for expiry semantics",
				predicate.Predicate.Field,
			),
		})
	}
}

func addDuplicateSourceFieldDiagnostics(ruleName string, collector crl.CollectorObject, add *func(Diagnostic)) {
	seen := map[string]crl.SignalObject{}
	for _, signal := range collector.Signals {
		field := strings.TrimSpace(signal.Signal.SourceField)
		if field == "" {
			continue
		}
		if first, ok := seen[field]; ok {
			(*add)(Diagnostic{
				Line:     signal.Span.Line,
				Column:   signal.Span.Column,
				Severity: SeverityWarning,
				Code:     "CRL205",
				Message: fmt.Sprintf(
					"source field %q is mapped by both %q and %q in collector %q for rule %q",
					field,
					first.Signal.Name,
					signal.Signal.Name,
					collector.Name,
					ruleName,
				),
			})
			continue
		}
		seen[field] = signal
	}
}

// addQuorumIndependenceDiagnostics warns when one quorum counts two or more
// distinct collectors that read the SAME source. A count/threshold quorum
// asserts independent corroboration ("N of M independent sources"); collectors
// sharing a source are not independent, so the count overstates how many
// separate sources actually agree. Flagged, not rejected — the bundle compiles.
//
// The check runs per authored rule over that rule's own collectors. A shared
// source introduced across an `extends` boundary (parent and child each
// declaring the same source, merged only at compile time) is not covered here.
func addQuorumIndependenceDiagnostics(rule crl.RuleObject, add *func(Diagnostic)) {
	foldIdent := func(s string) string {
		return strings.ToLower(strings.TrimSpace(norm.NFC.String(s)))
	}
	foldSource := func(s string) string {
		return norm.NFC.String(strings.TrimSpace(s))
	}
	sourceByCollector := make(map[string]string, len(rule.Collectors))
	for _, collector := range rule.Collectors {
		source := foldSource(collector.Source)
		if source == "" {
			continue
		}
		sourceByCollector[foldIdent(collector.Name)] = source
	}
	if len(sourceByCollector) == 0 {
		return
	}
	for _, predicate := range rule.Predicates {
		if predicate.Predicate.Kind != crl.PredicateQuorum {
			continue
		}
		// A count quorum names collectors in Providers; a boolean quorum
		// names them in Expression. Gather subjects from whichever is set.
		subjects := append([]string(nil), predicate.Predicate.Providers...)
		if predicate.Predicate.Expression != nil {
			subjects = append(subjects, crl.QuorumExpressionSubjects(predicate.Predicate.Expression)...)
		}
		collectorsBySource := map[string][]string{}
		seen := map[string]struct{}{}
		for _, subject := range subjects {
			name := foldIdent(subject)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			if source, ok := sourceByCollector[name]; ok {
				collectorsBySource[source] = append(collectorsBySource[source], name)
			}
		}
		sharedSources := make([]string, 0, len(collectorsBySource))
		for source, names := range collectorsBySource {
			if len(names) >= 2 {
				sharedSources = append(sharedSources, source)
			}
		}
		sort.Strings(sharedSources)
		for _, source := range sharedSources {
			names := collectorsBySource[source]
			sort.Strings(names)
			quoted := make([]string, len(names))
			for i, name := range names {
				quoted[i] = fmt.Sprintf("%q", name)
			}
			(*add)(Diagnostic{
				Line:     predicate.Span.Line,
				Column:   predicate.Span.Column,
				Severity: SeverityWarning,
				Code:     "CRL211",
				Message: fmt.Sprintf(
					"quorum in rule %q counts collectors %s that share source %q; a quorum over one source is not independent corroboration",
					rule.Name,
					strings.Join(quoted, ", "),
					source,
				),
			})
		}
	}
}

var (
	linePattern                = regexp.MustCompile(`(?:^|[^A-Za-z])line ([0-9]+)|at line ([0-9]+)`)
	quotedPattern              = regexp.MustCompile(`"([^"]+)"`)
	missingSubjectPattern      = regexp.MustCompile(`missing collector signal: (?:quorum subject |quorum provider |temporal reference |cluster rule )?([A-Za-z_][A-Za-z0-9_.-]*)`)
	typeMismatchFieldPattern   = regexp.MustCompile(`type mismatch: ([A-Za-z_][A-Za-z0-9_.-]*) expects`)
	unsupportedOperatorPattern = regexp.MustCompile(`unsupported operator: (bool|string) only supports == or !=`)
)

func errorDiagnostic(code string, err error) Diagnostic {
	line := extractLine(err.Error())
	return Diagnostic{
		Line:     line,
		Column:   1,
		Severity: SeverityError,
		Code:     code,
		Message:  err.Error(),
	}
}

func extractLine(message string) int {
	matches := linePattern.FindStringSubmatch(message)
	if len(matches) == 0 {
		return 1
	}
	for _, match := range matches[1:] {
		if match == "" {
			continue
		}
		var line int
		if _, err := fmt.Sscanf(match, "%d", &line); err == nil && line > 0 {
			return line
		}
	}
	return 1
}

func spanForCompilerError(document crl.Document, message string) (crl.SourceSpan, bool) {
	for _, subject := range compilerErrorSubjects(message) {
		if span, ok := spanForPredicateSubject(document, subject); ok {
			return span, true
		}
		if span, ok := spanForName(document, subject); ok {
			return span, true
		}
	}
	if span, ok := spanForUnsupportedOperator(document, message); ok {
		return span, true
	}
	matches := quotedPattern.FindAllStringSubmatch(message, -1)
	if len(matches) == 0 {
		return crl.SourceSpan{}, false
	}
	for _, match := range matches {
		name := strings.ToLower(match[1])
		if span, ok := spanForName(document, name); ok {
			return span, true
		}
	}
	return crl.SourceSpan{}, false
}

func compilerErrorSubjects(message string) []string {
	var subjects []string
	for _, pattern := range []*regexp.Regexp{missingSubjectPattern, typeMismatchFieldPattern} {
		matches := pattern.FindAllStringSubmatch(message, -1)
		for _, match := range matches {
			if len(match) > 1 && match[1] != "" {
				subjects = append(subjects, match[1])
			}
		}
	}
	matches := quotedPattern.FindAllStringSubmatch(message, -1)
	for _, match := range matches {
		if len(match) > 1 && match[1] != "" {
			subjects = append(subjects, match[1])
		}
	}
	return subjects
}

func spanForPredicateSubject(document crl.Document, subject string) (crl.SourceSpan, bool) {
	subject = strings.ToLower(subject)
	for _, rule := range document.Rules {
		if span, ok := spanInPredicates(rule.Predicates, subject); ok {
			return span, true
		}
	}
	for _, cluster := range document.Clusters {
		if span, ok := spanInPredicates(cluster.Predicates, subject); ok {
			return span, true
		}
	}
	return spanInPredicates(document.Predicates, subject)
}

func spanInPredicates(predicates []crl.PredicateObject, subject string) (crl.SourceSpan, bool) {
	for _, predicate := range predicates {
		for _, field := range predicate.Fields {
			if strings.EqualFold(field, subject) {
				return predicate.Span, true
			}
		}
		if predicate.Predicate.Expression != nil {
			for _, expressionSubject := range crl.QuorumExpressionSubjects(predicate.Predicate.Expression) {
				if strings.EqualFold(expressionSubject, subject) {
					return predicate.Span, true
				}
			}
		}
		for _, provider := range predicate.Predicate.Providers {
			if strings.EqualFold(provider, subject) {
				return predicate.Span, true
			}
		}
		if predicate.Predicate.Temporal != nil && strings.EqualFold(predicate.Predicate.Temporal.Reference, subject) {
			return predicate.Span, true
		}
	}
	return crl.SourceSpan{}, false
}

func spanForUnsupportedOperator(document crl.Document, message string) (crl.SourceSpan, bool) {
	match := unsupportedOperatorPattern.FindStringSubmatch(message)
	if len(match) < 2 {
		return crl.SourceSpan{}, false
	}
	kind := match[1]
	kinds := symbolKinds(document)
	for _, rule := range document.Rules {
		if span, ok := unsupportedOperatorSpan(rule.Predicates, kinds, kind); ok {
			return span, true
		}
	}
	for _, cluster := range document.Clusters {
		if span, ok := unsupportedOperatorSpan(cluster.Predicates, kinds, kind); ok {
			return span, true
		}
	}
	return unsupportedOperatorSpan(document.Predicates, kinds, kind)
}

func unsupportedOperatorSpan(predicates []crl.PredicateObject, kinds map[string]string, kind string) (crl.SourceSpan, bool) {
	for _, predicate := range predicates {
		if predicate.Predicate.Kind != crl.PredicateNeed {
			continue
		}
		if predicate.Predicate.Operator == crl.OperatorEQ || predicate.Predicate.Operator == crl.OperatorNE {
			continue
		}
		if kinds[strings.ToLower(predicate.Predicate.Field)] == kind {
			return predicate.Span, true
		}
	}
	return crl.SourceSpan{}, false
}

func symbolKinds(document crl.Document) map[string]string {
	kinds := map[string]string{"min_provider_trust": "number"}
	for _, rule := range document.Rules {
		kinds[strings.ToLower(rule.Name)] = "bool"
		for _, collector := range rule.Collectors {
			kinds[strings.ToLower(collector.Name)] = "bool"
			for _, signal := range collector.Signals {
				kinds[strings.ToLower(signal.Signal.Name)] = strings.ToLower(signal.Signal.Kind)
			}
		}
	}
	for _, cluster := range document.Clusters {
		kinds[strings.ToLower(cluster.Name)] = "bool"
	}
	return kinds
}

func spanForName(document crl.Document, name string) (crl.SourceSpan, bool) {
	for _, rule := range document.Rules {
		if strings.EqualFold(rule.Name, name) || strings.EqualFold(rule.Target, name) {
			return rule.Span, true
		}
		for _, collector := range rule.Collectors {
			if strings.EqualFold(collector.Name, name) {
				return collector.Span, true
			}
			for _, signal := range collector.Signals {
				if strings.EqualFold(signal.Signal.Name, name) || strings.EqualFold(signal.Signal.SourceField, name) {
					return signal.Span, true
				}
			}
		}
	}
	for _, rule := range document.AbstractRules {
		if strings.EqualFold(rule.Name, name) {
			return rule.Span, true
		}
	}
	for _, cluster := range document.Clusters {
		if strings.EqualFold(cluster.Name, name) {
			return cluster.Span, true
		}
	}
	return crl.SourceSpan{}, false
}

func sortDiagnostics(diagnostics []Diagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left, right := diagnostics[i], diagnostics[j]
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Column != right.Column {
			return left.Column < right.Column
		}
		if left.Severity != right.Severity {
			return severityRank(left.Severity) < severityRank(right.Severity)
		}
		return left.Code < right.Code
	})
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

func MeetsThreshold(diagnostics []Diagnostic, threshold Severity) bool {
	limit := severityRank(threshold)
	for _, diagnostic := range diagnostics {
		if severityRank(diagnostic.Severity) <= limit {
			return true
		}
	}
	return false
}

// subSecondMillis reports whether an `ms` duration literal is not a whole
// number of seconds (so it is rounded up to the next second at compile).
func subSecondMillis(literal string) bool {
	digits := strings.TrimSuffix(literal, "ms")
	n, err := strconv.Atoi(digits)
	if err != nil {
		return true
	}
	return n%1000 != 0
}
