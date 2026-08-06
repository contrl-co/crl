package crl

import (
	"fmt"
	"sort"
	"strings"
)

// validateQuorumSourceIndependence rejects a quorum that treats two collector
// subjects backed by the same source as independent evidence. It runs after
// normalization and expansion so inherited and struct-built programs cannot
// bypass the check.
func validateQuorumSourceIndependence(bundle Bundle) error {
	sourceByCollector := map[string]string{}
	for _, rule := range bundle.Rules {
		for _, collector := range rule.Collectors {
			sourceByCollector[collector.Name] = collector.Source
		}
	}

	check := func(scope string, predicates []Predicate) error {
		for _, predicate := range predicates {
			if predicate.Kind != PredicateQuorum {
				continue
			}
			subjects := predicate.Providers
			if predicate.Expression != nil {
				subjects = QuorumExpressionSubjects(predicate.Expression)
			}
			subjectsBySource := map[string]map[string]struct{}{}
			for _, subject := range subjects {
				source, ok := sourceByCollector[subject]
				if !ok {
					continue
				}
				if subjectsBySource[source] == nil {
					subjectsBySource[source] = map[string]struct{}{}
				}
				subjectsBySource[source][subject] = struct{}{}
			}

			sharedSources := make([]string, 0, len(subjectsBySource))
			for source, sourceSubjects := range subjectsBySource {
				if len(sourceSubjects) > 1 {
					sharedSources = append(sharedSources, source)
				}
			}
			if len(sharedSources) == 0 {
				continue
			}
			sort.Strings(sharedSources)
			source := sharedSources[0]
			sourceSubjects := make([]string, 0, len(subjectsBySource[source]))
			for subject := range subjectsBySource[source] {
				sourceSubjects = append(sourceSubjects, subject)
			}
			sort.Strings(sourceSubjects)
			quoted := make([]string, len(sourceSubjects))
			for i, subject := range sourceSubjects {
				quoted[i] = fmt.Sprintf("%q", subject)
			}
			return fmt.Errorf("%w: quorum counts subjects %s that share source %q in %s",
				ErrNonIndependentQuorum, strings.Join(quoted, ", "), source, scope)
		}
		return nil
	}

	for _, rule := range bundle.Rules {
		if err := check(fmt.Sprintf("rule %q", rule.Name), rule.Predicates); err != nil {
			return err
		}
	}
	for _, cluster := range bundle.Clusters {
		if err := check(fmt.Sprintf("cluster %q", cluster.Name), cluster.Predicates); err != nil {
			return err
		}
	}
	return check("global final policy", bundle.Predicates)
}
