package crl

// Regression tests for `extends` resolution. The dangerous failure
// mode: an extends clause that silently resolves to nothing compiles a
// rule WITHOUT its inherited collectors and predicates — a strictly
// weaker rule, with no diagnostic. Resolution must therefore be
// validated even when the document declares no abstract rules at all.

import (
	"strings"
	"testing"
)

func TestExtendsUnknownParentFailsWithoutAbstractRules(t *testing.T) {
	source := "crl v1\n" +
		"rule child extends ghost\n" +
		"\ttarget a.b\n" +
		"\tcollector c1 org api from /x.json\n" +
		"\t\tsignal s1 bool from x.y ttl 30d\n" +
		"\tneed s1 == true\n"
	_, err := CompileLanguage(source)
	if err == nil {
		t.Fatal("extends of an undeclared parent must not compile")
	}
	if !strings.Contains(err.Error(), "unknown abstract rule") {
		t.Fatalf("want unknown abstract rule error, got: %v", err)
	}
}

func TestExtendsConcreteParentFailsWithoutAbstractRules(t *testing.T) {
	source := "crl v1\n" +
		"rule parent\n" +
		"\ttarget a.b\n" +
		"\tcollector c1 org api from /x.json\n" +
		"\t\tsignal s1 bool from x.y ttl 30d\n" +
		"\tneed s1 == true\n" +
		"rule child extends parent\n" +
		"\ttarget a.c\n" +
		"\tcollector c2 org api from /y.json\n" +
		"\t\tsignal s2 bool from y.z ttl 30d\n" +
		"\tneed s2 == true\n"
	_, err := CompileLanguage(source)
	if err == nil {
		t.Fatal("extends of a concrete rule must not compile")
	}
	if !strings.Contains(err.Error(), "unknown abstract rule") {
		t.Fatalf("want unknown abstract rule error, got: %v", err)
	}
}

func TestExtendsStillExpandsWithAbstractParent(t *testing.T) {
	source := "crl v1\n" +
		"abstract rule base\n" +
		"\tcollector c1 org api from /x.json\n" +
		"\t\tsignal s1 bool from x.y ttl 30d\n" +
		"\tneed s1 == true\n" +
		"rule child extends base\n" +
		"\ttarget a.b\n" +
		"\tcollector c2 org api from /y.json\n" +
		"\t\tsignal s2 bool from y.z ttl 30d\n" +
		"\tneed s2 == true\n"
	compilation, err := CompileLanguage(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if got := len(compilation.Bundle.Rules); got != 1 {
		t.Fatalf("want 1 concrete rule, got %d", got)
	}
	rule := compilation.Bundle.Rules[0]
	if len(rule.Collectors) != 2 || len(rule.Predicates) != 2 {
		t.Fatalf("inheritance must prepend parent body: %d collectors, %d predicates", len(rule.Collectors), len(rule.Predicates))
	}
	if rule.Collectors[0].Name != "c1" {
		t.Fatalf("parent collector must come first, got %q", rule.Collectors[0].Name)
	}
}
