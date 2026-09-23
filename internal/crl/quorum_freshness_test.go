package crl

import (
	"testing"
	"time"
)

// threeSourceQuorum is "two of three independent sources": three
// collectors, each with one ttl'd signal, counted by a threshold quorum.
const threeSourceQuorum = "crl v1\npackage t.q\nbundle t.q\n" +
	"rule three_sources\n" +
	"\ttarget permit.application\n" +
	"\tcollector field_upload municipality file_upload from /field.json\n" +
	"\t\tsignal field_ok bool from field.ok ttl 30d\n" +
	"\tcollector permit_registry land_registry api from /registry.json\n" +
	"\t\tsignal registry_ok bool from registry.ok ttl 30d\n" +
	"\tcollector utility_record utility api from /utility.json\n" +
	"\t\tsignal utility_ok bool from utility.ok ttl 30d\n" +
	"\tquorum 2 of 3 field_upload permit_registry utility_record\n"

// sourceFacts reports one collector as present and stamps its signal's
// observation time, the way a host supplies collected evidence.
func sourceFacts(facts Facts, collector, signal string, observedAt time.Time) {
	facts["provider."+collector] = true
	facts[signal] = true
	facts["observed_at."+signal] = observedAt
}

func compileThreeSources(t *testing.T) CompiledBundle {
	t.Helper()
	compiled, err := CompileBundle(threeSourceQuorum)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return compiled
}

// The headline defect: a count quorum names collectors, and a collector
// has no observation time of its own, so freshness was never consulted.
// Evidence stamped in 1999 satisfied a thirty-day ttl at a 2027 clock.
func TestCountQuorumRejectsEvidenceOlderThanItsTTL(t *testing.T) {
	compiled := compileThreeSources(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	ancient := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)

	facts := Facts{}
	sourceFacts(facts, "field_upload", "field_ok", ancient)
	sourceFacts(facts, "permit_registry", "registry_ok", ancient)
	sourceFacts(facts, "utility_record", "utility_ok", ancient)

	eval := EvaluateBundleAt(compiled, facts, now)
	if eval.Authorized {
		t.Fatalf("evidence stamped in 1999 satisfied a 30d ttl quorum at a 2027 clock, got %q", eval.Result)
	}
	if eval.Result != "EXPIRED" {
		t.Fatalf("Result = %q, want EXPIRED (the evidence exists; it is only stale)", eval.Result)
	}
}

// All three sources fresh: the quorum is met on the merits.
func TestCountQuorumAuthorizesWhenEverySourceIsFresh(t *testing.T) {
	compiled := compileThreeSources(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)

	facts := Facts{}
	sourceFacts(facts, "field_upload", "field_ok", fresh)
	sourceFacts(facts, "permit_registry", "registry_ok", fresh)
	sourceFacts(facts, "utility_record", "utility_ok", fresh)

	eval := EvaluateBundleAt(compiled, facts, now)
	if !eval.Authorized {
		t.Fatalf("three fresh sources should meet a 2-of-3 quorum, got %q", eval.Result)
	}
}

// A stale source REDUCES the count; it does not poison the quorum. Two
// fresh sources still meet a threshold of two.
func TestCountQuorumAuthorizesOnEnoughFreshDespiteAStaleSource(t *testing.T) {
	compiled := compileThreeSources(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)
	stale := now.Add(-90 * 24 * time.Hour)

	facts := Facts{}
	sourceFacts(facts, "field_upload", "field_ok", fresh)
	sourceFacts(facts, "permit_registry", "registry_ok", fresh)
	sourceFacts(facts, "utility_record", "utility_ok", stale)

	eval := EvaluateBundleAt(compiled, facts, now)
	if !eval.Authorized {
		t.Fatalf("two fresh of three should authorize despite one stale source, got %q", eval.Result)
	}

	// The stale source must be reported as absent from the count, and the
	// count must be the number of FRESH subjects.
	var quorumCheck Check
	for _, check := range eval.Checks {
		if check.Kind == PredicateQuorum {
			quorumCheck = check
		}
	}
	if actual, ok := quorumCheck.Actual.(int); !ok || actual != 2 {
		t.Fatalf("quorum count = %v, want 2 (only fresh subjects count)", quorumCheck.Actual)
	}
	for _, provider := range quorumCheck.Providers {
		if provider.Provider == "utility_record" && provider.Present {
			t.Fatal("a stale source must not be reported present")
		}
	}
}

// Too few fresh sources, but the evidence is all there: the shortfall IS
// the staleness, so the outcome is EXPIRED rather than the generic
// "not enough evidence".
func TestCountQuorumTooFewFreshReportsExpired(t *testing.T) {
	compiled := compileThreeSources(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)
	stale := now.Add(-90 * 24 * time.Hour)

	facts := Facts{}
	sourceFacts(facts, "field_upload", "field_ok", fresh)
	sourceFacts(facts, "permit_registry", "registry_ok", stale)
	sourceFacts(facts, "utility_record", "utility_ok", stale)

	eval := EvaluateBundleAt(compiled, facts, now)
	if eval.Authorized {
		t.Fatalf("one fresh of three must not meet a 2-of-3 quorum, got %q", eval.Result)
	}
	if eval.Result != "EXPIRED" {
		t.Fatalf("Result = %q, want EXPIRED (refreshing the stale sources would meet the quorum)", eval.Result)
	}
}

// Sources that never reported are missing evidence, not stale evidence:
// refreshing what is present would still not reach the threshold, so the
// outcome stays INSUFFICIENT_EVIDENCE.
func TestCountQuorumMissingSourcesReportInsufficientEvidence(t *testing.T) {
	compiled := compileThreeSources(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	facts := Facts{}
	sourceFacts(facts, "field_upload", "field_ok", now.Add(-24*time.Hour))

	eval := EvaluateBundleAt(compiled, facts, now)
	if eval.Authorized {
		t.Fatalf("one of three must not meet a 2-of-3 quorum, got %q", eval.Result)
	}
	if eval.Result != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("Result = %q, want INSUFFICIENT_EVIDENCE (the other two sources never reported)", eval.Result)
	}
}

// A collector whose signal carries a value but no observation time has
// no provable age, so it does not count: supplying half the evidence
// must not buy a subject past the freshness window.
func TestCountQuorumRejectsEvidenceWithNoObservationTime(t *testing.T) {
	compiled := compileThreeSources(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	facts := Facts{}
	sourceFacts(facts, "field_upload", "field_ok", now.Add(-24*time.Hour))
	sourceFacts(facts, "permit_registry", "registry_ok", now.Add(-24*time.Hour))
	facts["provider.utility_record"] = true
	facts["utility_ok"] = true // value with no observed_at: age unknowable

	eval := EvaluateBundleAt(compiled, facts, now)
	if !eval.Authorized {
		t.Fatalf("two fresh sources still meet the threshold, got %q", eval.Result)
	}

	// Drop one of the provable sources: two subjects are truthy, but only
	// one has a provable age, so the quorum is not met.
	delete(facts, "provider.permit_registry")
	delete(facts, "registry_ok")
	delete(facts, "observed_at.registry_ok")
	short := EvaluateBundleAt(compiled, facts, now)
	if short.Authorized {
		t.Fatalf("a value with no observation time must not count toward a quorum, got %q", short.Result)
	}
	if short.Result != "EXPIRED" {
		t.Fatalf("Result = %q, want EXPIRED", short.Result)
	}
}

// The boolean form must behave the same way: a stale subject does not
// taint a branch that is satisfied without it. `a & b | a & c | b & c`
// is two-of-three written as a disjunction.
func TestBooleanQuorumStaleSubjectDoesNotTaintASatisfiedBranch(t *testing.T) {
	src := "crl v1\npackage t.b\nbundle t.b\n" +
		"rule three_sources\n" +
		"\ttarget permit.application\n" +
		"\tcollector sources municipality api from /sources.json\n" +
		"\t\tsignal field_ok bool from field.ok ttl 30d\n" +
		"\t\tsignal registry_ok bool from registry.ok ttl 30d\n" +
		"\t\tsignal utility_ok bool from utility.ok ttl 30d\n" +
		"\tquorum field_ok & registry_ok | field_ok & utility_ok | registry_ok & utility_ok\n"
	compiled, err := CompileBundle(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)
	stale := now.Add(-90 * 24 * time.Hour)

	eval := EvaluateBundleAt(compiled, Facts{
		"field_ok":                true,
		"observed_at.field_ok":    fresh,
		"registry_ok":             true,
		"observed_at.registry_ok": fresh,
		"utility_ok":              true,
		"observed_at.utility_ok":  stale,
	}, now)
	if !eval.Authorized {
		t.Fatalf("two fresh subjects satisfy a disjunct; one stale subject must not taint it, got %q", eval.Result)
	}

	// With two of the three stale, no disjunct is satisfied by fresh
	// evidence and the shortfall is the staleness.
	short := EvaluateBundleAt(compiled, Facts{
		"field_ok":                true,
		"observed_at.field_ok":    fresh,
		"registry_ok":             true,
		"observed_at.registry_ok": stale,
		"utility_ok":              true,
		"observed_at.utility_ok":  stale,
	}, now)
	if short.Authorized {
		t.Fatalf("one fresh subject satisfies no two-of-three disjunct, got %q", short.Result)
	}
	if short.Result != "EXPIRED" {
		t.Fatalf("Result = %q, want EXPIRED", short.Result)
	}
}

// The safety property the taint protected must survive without it: a
// stale subject is unknown, never false, so a negated stale hold still
// denies while a sibling disjunct that is satisfied on fresh evidence
// still passes.
func TestBooleanQuorumStaleSubjectStaysUnknownUnderNegation(t *testing.T) {
	src := "crl v1\npackage t.n\nbundle t.n\n" +
		"rule gate\n" +
		"\ttarget permit.application\n" +
		"\tcollector utility utility api from /utility.json\n" +
		"\t\tsignal utility_ok bool from utility.ok ttl 30d\n" +
		"\t\tsignal registry_ok bool from registry.ok ttl 30d\n" +
		"\t\tsignal safety_hold bool from safety.hold ttl 1h\n" +
		"\tquorum utility_ok & registry_ok | !safety_hold\n"
	compiled, err := CompileBundle(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)
	stale := now.Add(-10 * time.Hour)

	// A stale, cleared safety hold must not read as a clearance, and the
	// other disjunct is unmet: deny.
	held := EvaluateBundleAt(compiled, Facts{
		"utility_ok":              true,
		"observed_at.utility_ok":  fresh,
		"safety_hold":             false,
		"observed_at.safety_hold": stale,
	}, now)
	if held.Authorized {
		t.Fatalf("a stale safety hold must not read as cleared, got %q", held.Result)
	}

	// The same stale hold, but the other disjunct is satisfied on fresh
	// evidence: that branch carries the quorum.
	carried := EvaluateBundleAt(compiled, Facts{
		"utility_ok":              true,
		"observed_at.utility_ok":  fresh,
		"registry_ok":             true,
		"observed_at.registry_ok": fresh,
		"safety_hold":             false,
		"observed_at.safety_hold": stale,
	}, now)
	if !carried.Authorized {
		t.Fatalf("a satisfied fresh disjunct must carry the quorum, got %q", carried.Result)
	}
}
