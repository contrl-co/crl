package crl

import (
	"testing"
	"time"
)

func TestCompileBundleEvaluatesRulesClustersAndGlobalPredicates(t *testing.T) {
	compiled, err := CompileBundle(`
crl v1

rule application
	target permit.application
	collector application_file municipality file_upload from /test-resources/application.json
		signal application_complete bool from application.complete ttl 30d
		signal permit_hold_active bool from permit.hold_active ttl 30d
	need application_complete == true
	quorum application_file

rule capital
	target permit.capital
	collector capital_webhook finance webhook from /test-resources/capital.json
		signal committed_usd number from capital.committed_usd ttl 30d
	collector treasury_api municipality api_poll from /test-resources/treasury.json
		signal fee_escrowed bool from treasury.fee_escrowed ttl 30d
	need committed_usd >= 250000
	need fee_escrowed == true
	quorum capital_webhook & treasury_api

cluster permit_foundation
	rules application + capital
	quorum application & capital

need permit_foundation == true
quorum permit_foundation & !permit_hold_active
`)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	result := EvaluateBundleAt(compiled, Facts{
		"application_complete":             true,
		"committed_usd":                    300000.0,
		"fee_escrowed":                     true,
		"permit_hold_active":               false,
		"provider.application_file":        true,
		"provider.capital_webhook":         true,
		"provider.treasury_api":            true,
		"observed_at.application_complete": now.Add(-time.Hour),
		"observed_at.committed_usd":        now.Add(-time.Hour),
		"observed_at.fee_escrowed":         now.Add(-time.Hour),
		"observed_at.permit_hold_active":   now.Add(-time.Hour),
	}, now)
	if !result.Authorized || result.Result != "AUTHORIZED" {
		t.Fatalf("expected authorized bundle, result=%s checks=%+v", result.Result, result.Checks)
	}
	if len(result.RuleTraces) != 2 || len(result.ClusterTraces) != 1 || len(result.GlobalChecks) != 2 {
		t.Fatalf("unexpected trace shape: rules=%d clusters=%d global=%d", len(result.RuleTraces), len(result.ClusterTraces), len(result.GlobalChecks))
	}
}

func TestCompileBundleRejectsSamples(t *testing.T) {
	_, err := CompileBundle(`
rule application
	target permit.application
	collector application_file municipality file_upload from /test-resources/application.json
		signal application_complete bool from application.complete ttl 30d
		sample application_complete = true confidence 1
	need application_complete == true
`)
	if err == nil {
		t.Fatal("expected sample line to be rejected")
	}
}
