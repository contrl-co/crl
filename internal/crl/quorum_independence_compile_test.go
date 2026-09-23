package crl

import (
	"errors"
	"strings"
	"testing"
)

func TestCompileRejectsCountQuorumOverSharedSource(t *testing.T) {
	source := `crl v1
package test
bundle test.shared_source

rule r
	target test.shared_source
	collector registry_primary registry api from /evidence/registry.json
		signal primary_ok bool from primary.ok ttl 30d
	collector registry_alias registry file_upload from /evidence/registry.json
		signal alias_ok bool from alias.ok ttl 30d
	need primary_ok == true
	quorum count(registry_primary, registry_alias) >= 2
`

	_, err := CompileLanguage(source)
	if !errors.Is(err, ErrNonIndependentQuorum) {
		t.Fatalf("CompileLanguage error = %v, want ErrNonIndependentQuorum", err)
	}
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Fatalf("CompileLanguage error = %v, want ErrInvalidSyntax compatibility", err)
	}
	for _, fragment := range []string{"rule \"r\"", "registry_primary", "registry_alias", "/evidence/registry.json"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("error %q does not identify %q", err, fragment)
		}
	}
}

func TestCompileAllowsBooleanSignalPolicyWithinOneSource(t *testing.T) {
	source := `crl v1
package test
bundle test.shared_signals

rule r
	target test.shared_signals
	collector registry registry api from /evidence/registry.json
		signal registration_ok bool from registration.ok ttl 30d
		signal ownership_ok bool from ownership.ok ttl 30d
	need registration_ok == true
	quorum registration_ok & ownership_ok
`

	if _, err := CompileLanguage(source); err != nil {
		t.Fatalf("CompileLanguage rejected boolean logic over one source: %v", err)
	}
}

func TestCompileRejectsSharedSourceIntroducedByExtends(t *testing.T) {
	source := `crl v1
package test
bundle test.extended_shared_source

abstract rule base
	collector registry_primary registry api from /evidence/registry.json
		signal primary_ok bool from primary.ok ttl 30d
	need primary_ok == true

rule r extends base
	target test.extended_shared_source
	collector registry_alias registry file_upload from /evidence/registry.json
		signal alias_ok bool from alias.ok ttl 30d
	need alias_ok == true
	quorum registry_primary & registry_alias
`

	_, err := CompileLanguage(source)
	if !errors.Is(err, ErrNonIndependentQuorum) {
		t.Fatalf("CompileLanguage error = %v, want ErrNonIndependentQuorum", err)
	}
}

func TestCompileProgramRejectsSharedSourceQuorum(t *testing.T) {
	expiry := SignalExpiry{Mode: "ttl", Literal: "30d", Seconds: 30 * 24 * 60 * 60}
	_, err := CompileBundleProgram(Bundle{Version: Version, Rules: []Rule{{
		Name:   "r",
		Target: "test.shared_source",
		Collectors: []Collector{
			{Name: "a", ProviderType: "registry", ConnectorKind: "api", Source: "/same", Signals: []Signal{{Name: "a_ok", Kind: "bool", SourceField: "a.ok", Expiry: expiry}}},
			{Name: "b", ProviderType: "registry", ConnectorKind: "api", Source: "/same", Signals: []Signal{{Name: "b_ok", Kind: "bool", SourceField: "b.ok", Expiry: expiry}}},
		},
		Predicates: []Predicate{{Kind: PredicateQuorum, Value: Value{Kind: "number", Number: 2}, Providers: []string{"a", "b"}}},
	}}})
	if !errors.Is(err, ErrNonIndependentQuorum) {
		t.Fatalf("CompileBundleProgram error = %v, want ErrNonIndependentQuorum", err)
	}
}

func TestCompileAllowsQuorumOverDistinctSources(t *testing.T) {
	source := `crl v1
package test
bundle test.distinct_sources

rule r
	target test.distinct_sources
	collector registry registry api from /evidence/registry.json
		signal registry_ok bool from registry.ok ttl 30d
	collector municipality municipality file_upload from /evidence/municipality.json
		signal municipality_ok bool from municipality.ok ttl 30d
	need registry_ok == true
	quorum count(registry, municipality) >= 2
`

	if _, err := CompileLanguage(source); err != nil {
		t.Fatalf("CompileLanguage rejected independent sources: %v", err)
	}
}
