package crl

// Regression tests for the freshness fail-closed contract. These pin
// three properties of the evaluator:
//
//   P1: a signal that declares a ttl must fail CLOSED (EXPIRED) when
//       the observation timestamp is missing or unparseable — a
//       forgotten observed_at must never silently disable the
//       freshness guarantee.
//   P2: evaluating without a clock (EvaluateBundle / zero now) must
//       fail CLOSED for every time-dependent rule, never AUTHORIZE
//       stale-or-unknown-age evidence.
//   P3: EXPIRED comes only from declared expiry semantics; an
//       active blocker reports BLOCKED regardless of what its field
//       name suggests.

import (
	"strings"
	"testing"
	"time"
)

const failClosedSource = `
rule permit_release
target permit
collector permit_registry registry webhook from registry.permits
signal capacity_kw number from capacity.kw ttl 30d
need capacity_kw >= 2000
`

func compileFailClosed(t *testing.T, source string) CompiledBundle {
	t.Helper()
	compiled, err := CompileBundle(source)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	return compiled
}

// P1: value present, observed_at missing, TTL declared => EXPIRED.
func TestTTLSignalWithoutObservedAtFailsClosed(t *testing.T) {
	compiled := compileFailClosed(t, failClosedSource)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	eval := EvaluateBundleAt(compiled, Facts{"capacity_kw": 9000}, now)
	if eval.Authorized {
		t.Fatal("missing observed_at on a ttl signal must not authorize")
	}
	if eval.Result != "EXPIRED" {
		t.Fatalf("Result = %q, want EXPIRED (fail closed on unknown evidence age)", eval.Result)
	}

	// Control: the same evaluation WITH a fresh observation authorizes.
	eval = EvaluateBundleAt(compiled, Facts{
		"capacity_kw":             9000,
		"observed_at.capacity_kw": now.Add(-time.Hour),
	}, now)
	if !eval.Authorized {
		t.Fatalf("fresh observation should authorize, got %q", eval.Result)
	}

	// Control: a stale observation still expires (the freshness math
	// is unchanged — only the missing-timestamp gap is closed).
	eval = EvaluateBundleAt(compiled, Facts{
		"capacity_kw":             9000,
		"observed_at.capacity_kw": now.Add(-40 * 24 * time.Hour),
	}, now)
	if eval.Authorized || eval.Result != "EXPIRED" {
		t.Fatalf("stale observation must expire, got authorized=%v result=%q", eval.Authorized, eval.Result)
	}
}

// P1: an unparseable observed_at is the same gap as a missing one.
func TestTTLSignalWithUnparseableObservedAtFailsClosed(t *testing.T) {
	compiled := compileFailClosed(t, failClosedSource)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	eval := EvaluateBundleAt(compiled, Facts{
		"capacity_kw":             9000,
		"observed_at.capacity_kw": "not-a-timestamp",
	}, now)
	if eval.Authorized {
		t.Fatal("unparseable observed_at must not authorize")
	}
	if eval.Result != "EXPIRED" {
		t.Fatalf("Result = %q, want EXPIRED", eval.Result)
	}
}

// P2: the zero-clock convenience entrypoint must fail closed for
// time-dependent rules instead of silently disabling expiry.
func TestZeroClockFailsClosedOnDeclaredExpiry(t *testing.T) {
	compiled := compileFailClosed(t, failClosedSource)

	facts := Facts{
		"capacity_kw":             9000,
		"observed_at.capacity_kw": time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	eval := EvaluateBundle(compiled, facts) // zero clock
	if eval.Authorized {
		t.Fatal("EvaluateBundle (zero clock) must not authorize a rule with declared ttl")
	}
	if eval.Result != "EXPIRED" {
		t.Fatalf("Result = %q, want EXPIRED (no clock, freshness unprovable)", eval.Result)
	}

	// The same bundle+facts with a real clock evaluates the expiry
	// normally: 2020 observation + 30d ttl at 2027 => EXPIRED.
	at := EvaluateBundleAt(compiled, facts, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if at.Authorized || at.Result != "EXPIRED" {
		t.Fatalf("stale evidence at a real clock must expire, got authorized=%v result=%q", at.Authorized, at.Result)
	}
}

// P2 control: fail-closed applies only to expiry-dependent checks.
// A rule gated purely on collector-presence quorum (no need/block on a
// ttl signal) still evaluates under the zero clock — the grammar makes
// ttl mandatory on every signal, so any need/block over a signal is
// time-dependent by construction.
func TestZeroClockStillEvaluatesTimeFreeRules(t *testing.T) {
	compiled := compileFailClosed(t, `
rule permit_release
target permit
collector permit_registry registry webhook from registry.permits
signal permit_current bool from permit.current ttl 30d
quorum permit_registry
`)
	eval := EvaluateBundle(compiled, Facts{"provider.permit_registry": true})
	if !eval.Authorized {
		t.Fatalf("quorum-only rule should authorize under zero clock, got %q", eval.Result)
	}
}

// P1: absolute (`expires <rfc3339>`) expiry needs only the clock —
// it must fire even when no observation timestamp exists.
func TestAbsoluteExpiryFiresWithoutObservedAt(t *testing.T) {
	compiled := compileFailClosed(t, `
rule permit_release
target permit
collector permit_registry registry webhook from registry.permits
signal license_ok bool from license.ok expires "2026-06-01T00:00:00Z"
need license_ok == true
`)
	past := EvaluateBundleAt(compiled, Facts{"license_ok": true}, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if past.Authorized || past.Result != "EXPIRED" {
		t.Fatalf("past absolute expiry must expire, got authorized=%v result=%q", past.Authorized, past.Result)
	}
	before := EvaluateBundleAt(compiled, Facts{"license_ok": true}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !before.Authorized {
		t.Fatalf("unexpired absolute expiry should authorize, got %q", before.Result)
	}
}

// Regression for the normalizeSignalExpiry round-trip: parseSignalExpiry
// emits Mode "at" for absolute expiries, and bundle normalization used to
// reject that mode as "expected ttl or expires", making `expires <rfc3339>`
// un-compilable end-to-end. The canonical text must also recompile to the
// identical hash (determinism contract).
func TestAtExpiryCompilesAndRoundTrips(t *testing.T) {
	src := "rule r\ntarget permit\ncollector c registry webhook from registry.r\nsignal ok bool from f.ok expires \"2030-06-01T00:00:00Z\"\nneed ok == true\n"
	first, err := CompileBundle(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := CompileBundle(first.CanonicalText)
	if err != nil {
		t.Fatalf("recompile canonical text: %v\n%s", err, first.CanonicalText)
	}
	if first.Hash != second.Hash {
		t.Fatalf("canonical hash drift: %s vs %s", first.Hash, second.Hash)
	}
}

// P3: the block reason is type-driven, not name-driven. An active
// blocker is BLOCKED whatever its field is called; EXPIRED is reserved
// for declared expiry semantics.
func TestBlockReasonIsNotInferredFromFieldName(t *testing.T) {
	for _, field := range []string{"grid_hold", "grid_hold_expired", "license_expires"} {
		passed, reason := evaluateBlock(field, true)
		if passed {
			t.Fatalf("active blocker %q must not pass", field)
		}
		if reason != ErrBlocked.Error() {
			t.Fatalf("evaluateBlock(%q, true) reason = %q, want %q (never name-derived EXPIRED)", field, reason, ErrBlocked.Error())
		}
	}
	if passed, reason := evaluateBlock("grid_hold", false); !passed || reason != "" {
		t.Fatalf("inactive blocker should pass cleanly, got passed=%v reason=%q", passed, reason)
	}
}

// Regressions: freshness must fail closed on EVERY
// path that consults a ttl'd signal, not just the rule-local
// need/block path. Each of these AUTHORIZED on the pre-fix engine.

// Cross-rule reference: rule_b needs a signal declared in rule_a's
// collector. The rule-local index missed it, so expiry was skipped.
func TestCrossRuleSignalReferenceFailsClosed(t *testing.T) {
	src := `
rule rule_a
target permit
collector reg_a registry webhook from registry.a
signal sig_a number from a.value ttl 30d
quorum reg_a

rule rule_b
target permit
collector reg_b registry webhook from registry.b
signal sig_b bool from b.ready ttl 30d
need sig_a >= 5
quorum reg_b
`
	compiled := compileFailClosed(t, src)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	// sig_a value present and passing, but observed 90 days ago.
	stale := EvaluateBundleAt(compiled, Facts{
		"sig_a":             9,
		"observed_at.sig_a": now.Add(-90 * 24 * time.Hour),
		"provider.reg_a":    true,
		"provider.reg_b":    true,
		"sig_b":             true,
		"observed_at.sig_b": now.Add(-time.Hour),
	}, now)
	if stale.Authorized {
		t.Fatalf("stale cross-rule signal must not authorize, got %q", stale.Result)
	}

	// observed_at entirely missing for the cross-rule signal.
	missing := EvaluateBundleAt(compiled, Facts{
		"sig_a":             9,
		"provider.reg_a":    true,
		"provider.reg_b":    true,
		"sig_b":             true,
		"observed_at.sig_b": now.Add(-time.Hour),
	}, now)
	if missing.Authorized {
		t.Fatalf("cross-rule signal with missing observed_at must not authorize, got %q", missing.Result)
	}

	// Control: fresh cross-rule signal authorizes.
	fresh := EvaluateBundleAt(compiled, Facts{
		"sig_a":             9,
		"observed_at.sig_a": now.Add(-time.Hour),
		"provider.reg_a":    true,
		"provider.reg_b":    true,
		"sig_b":             true,
		"observed_at.sig_b": now.Add(-time.Hour),
	}, now)
	if !fresh.Authorized {
		t.Fatalf("fresh cross-rule signal should authorize, got %q", fresh.Result)
	}
}

// Quorum over a ttl'd signal subject: a stale/unknown-age/zero-clock
// subject must not count toward the quorum.
func TestQuorumSubjectFreshnessFailsClosed(t *testing.T) {
	src := `
rule attest
target permit
collector attestor api api_poll from attestor.feed
signal attestor_ok bool from attestor.ok ttl 1h
quorum attestor_ok
`
	compiled := compileFailClosed(t, src)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	stale := EvaluateBundleAt(compiled, Facts{
		"attestor_ok":             true,
		"observed_at.attestor_ok": now.Add(-10 * time.Hour),
	}, now)
	if stale.Authorized {
		t.Fatalf("stale quorum subject must not authorize, got %q", stale.Result)
	}

	missing := EvaluateBundleAt(compiled, Facts{"attestor_ok": true}, now)
	if missing.Authorized {
		t.Fatalf("quorum subject with missing observed_at must not authorize, got %q", missing.Result)
	}

	// Zero clock: quorum over a ttl'd signal is time-dependent.
	zero := EvaluateBundle(compiled, Facts{
		"attestor_ok":             true,
		"observed_at.attestor_ok": now.Add(-time.Minute),
	})
	if zero.Authorized {
		t.Fatalf("zero-clock quorum over a ttl signal must not authorize, got %q", zero.Result)
	}

	fresh := EvaluateBundleAt(compiled, Facts{
		"attestor_ok":             true,
		"observed_at.attestor_ok": now.Add(-time.Minute),
	}, now)
	if !fresh.Authorized {
		t.Fatalf("fresh quorum subject should authorize, got %q", fresh.Result)
	}
}

// Temporal reference signal: `need event_time before deadline` where
// deadline is a stale ttl'd signal must fail closed.
func TestTemporalReferenceSignalFreshnessFailsClosed(t *testing.T) {
	src := `
rule window
target permit
collector clock api api_poll from clock.feed
signal event_time time from clock.event ttl 30d
signal deadline time from clock.deadline ttl 1h
need event_time before deadline
quorum clock
`
	compiled := compileFailClosed(t, src)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	eventAt := "2026-12-01T00:00:00Z"
	deadlineAt := "2026-12-15T00:00:00Z" // event is before deadline: would pass if fresh

	stale := EvaluateBundleAt(compiled, Facts{
		"event_time":             eventAt,
		"observed_at.event_time": now.Add(-time.Hour),
		"deadline":               deadlineAt,
		"observed_at.deadline":   now.Add(-10 * time.Hour), // stale reference
		"provider.clock":         true,
	}, now)
	if stale.Authorized {
		t.Fatalf("stale temporal reference signal must not authorize, got %q", stale.Result)
	}

	fresh := EvaluateBundleAt(compiled, Facts{
		"event_time":             eventAt,
		"observed_at.event_time": now.Add(-time.Hour),
		"deadline":               deadlineAt,
		"observed_at.deadline":   now.Add(-time.Minute),
		"provider.clock":         true,
	}, now)
	if !fresh.Authorized {
		t.Fatalf("fresh temporal reference should authorize, got %q", fresh.Result)
	}
}

// P3 at the RESULT level (not just the evaluateBlock unit): a bundle
// whose only failing check is an active blocker with an expiry-suggestive
// field name must report BLOCKED, not EXPIRED.
func TestExpiryNamedBlockerResultIsBlockedNotExpired(t *testing.T) {
	compiled := compileFailClosed(t, `
rule gate
target permit
collector registry municipality webhook from registry.r
signal grid_hold_expired bool from grid.hold_expired ttl 30d
block grid_hold_expired
quorum registry
`)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	eval := EvaluateBundleAt(compiled, Facts{
		"grid_hold_expired":             true, // blocker ACTIVE
		"observed_at.grid_hold_expired": now.Add(-time.Hour),
		"provider.registry":             true,
	}, now)
	if eval.Authorized {
		t.Fatalf("active blocker must not authorize, got %q", eval.Result)
	}
	if eval.Result != "BLOCKED" {
		t.Fatalf("Result = %q, want BLOCKED (name must not force EXPIRED)", eval.Result)
	}
}

// A NEGATED stale signal in a quorum expression must still fail closed:
// `!safety_hold` where safety_hold is a stale ttl'd signal must NOT read
// as "hold cleared" (that would be fail-open for a safety hold).
func TestNegatedStaleQuorumSubjectFailsClosed(t *testing.T) {
	src := `
rule gate
target permit
collector utility api api_poll from utility.feed
signal utility_record bool from utility.record ttl 30d
signal safety_hold bool from safety.hold ttl 1h
quorum utility_record & !safety_hold
`
	compiled := compileFailClosed(t, src)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	// safety_hold active but STALE (age unknowable) — must not be treated
	// as cleared just because we can't prove it is fresh.
	stale := EvaluateBundleAt(compiled, Facts{
		"utility_record":             true,
		"observed_at.utility_record": now.Add(-time.Hour),
		"safety_hold":                true,
		"observed_at.safety_hold":    now.Add(-10 * time.Hour),
	}, now)
	if stale.Authorized {
		t.Fatalf("negated stale safety_hold must fail closed, got %q", stale.Result)
	}

	// safety_hold missing observed_at — also unprovable, fail closed.
	missing := EvaluateBundleAt(compiled, Facts{
		"utility_record":             true,
		"observed_at.utility_record": now.Add(-time.Hour),
		"safety_hold":                false,
	}, now)
	if missing.Authorized {
		t.Fatalf("negated safety_hold with no observed_at must fail closed, got %q", missing.Result)
	}

	// Control: fresh, cleared safety_hold authorizes.
	fresh := EvaluateBundleAt(compiled, Facts{
		"utility_record":             true,
		"observed_at.utility_record": now.Add(-time.Hour),
		"safety_hold":                false,
		"observed_at.safety_hold":    now.Add(-time.Minute),
	}, now)
	if !fresh.Authorized {
		t.Fatalf("fresh cleared safety_hold should authorize, got %q", fresh.Result)
	}
}

// Regression: the same signal name maps to one fact, so
// declaring it with conflicting expiry across rules is rejected at
// compile time. Silently collapsing to a sibling's looser ttl was a
// fail-open (a stale value satisfied a stricter rule); the mirror was
// an over-fire. Rejecting the ambiguity keeps the eval-time signal
// index unambiguous.
func TestConflictingSignalExpiryRejectedAtCompile(t *testing.T) {
	conflicting := `
rule strict
target permit
collector rega registry webhook from registry.a
signal sig bool from a.ok ttl 1h
need sig == true
quorum rega

rule lax
target permit
collector regb registry webhook from registry.b
signal sig bool from b.ok ttl 30d
quorum regb
`
	if _, err := CompileBundle(conflicting); err == nil {
		t.Fatal("expected compile error for same-named signal with conflicting expiry")
	} else if !strings.Contains(err.Error(), "conflicting expiry") {
		t.Fatalf("error = %v, want a conflicting-expiry rejection", err)
	}

	// Control: the same signal name re-declared with IDENTICAL expiry
	// is a legitimate repeat and still compiles.
	identical := `
rule a
target permit
collector rega registry webhook from registry.a
signal sig bool from a.ok ttl 30d
need sig == true
quorum rega

rule b
target permit
collector regb registry webhook from registry.b
signal sig bool from b.ok ttl 30d
quorum regb
`
	if _, err := CompileBundle(identical); err != nil {
		t.Fatalf("identical re-declaration should compile, got %v", err)
	}
}

// A global final policy of only `block` predicates authorizes when its target
// has no evidence (block ra passes when ra is unproven), so empty facts pass.
// Such a policy must be rejected at compile.
func TestBlockOnlyFinalPolicyRejected(t *testing.T) {
	if _, err := CompileBundle("crl v1\npackage p\nbundle b\nblock ra\n" +
		"\nrule ra\n\ttarget a.a\n\tcollector c1 org api from /x.json\n" +
		"\t\tsignal s1 bool from x.y ttl 30d\n\tneed s1 == true\n"); err == nil {
		t.Fatal("a block-only final policy compiled; empty facts would authorize it")
	}
}

// An observation stamped after the evaluation clock cannot prove freshness —
// nothing is observed in the future. It used to grant a full ttl window from
// that future instant; it must now fail closed.
func TestFutureObservationFailsClosed(t *testing.T) {
	src := "crl v1\npackage p\nbundle b\n\nrule r\n\ttarget a.b\n" +
		"\tcollector c1 org api from /x.json\n\t\tsignal s1 bool from x.y ttl 1h\n\tneed s1 == true\n"
	compiled, err := CompileBundle(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	future := EvaluateBundleAt(compiled, Facts{"s1": true, "observed_at.s1": "2027-01-01T00:00:00Z"}, now)
	if future.Authorized {
		t.Fatalf("a future observation authorized (%s); must fail closed", future.Result)
	}
	fresh := EvaluateBundleAt(compiled, Facts{"s1": true, "observed_at.s1": now.Add(-30 * time.Minute).Format(time.RFC3339)}, now)
	if !fresh.Authorized {
		t.Fatalf("a present fresh observation should authorize, got %s", fresh.Result)
	}
}

// A cluster whose member rule is BLOCKED reported a generic DENIED because the
// member's reason was never in scope for the cluster's result. It must surface
// the member's outcome.
func TestClusterReportsBlockedMemberOutcome(t *testing.T) {
	src := "crl v1\npackage p\nbundle b\nneed cl == true\n\n" +
		"rule ra\n\ttarget a.a\n\tcollector c1 org api from /x.json\n\t\tsignal hold bool from x.y ttl 30d\n\tblock hold\n" +
		"rule rb\n\ttarget a.b\n\tcollector c2 org api from /y.json\n\t\tsignal s2 bool from y.z ttl 30d\n\tneed s2 == true\n" +
		"cluster cl\n\trules ra + rb\n\tneed rb == true\n"
	compiled, err := CompileBundle(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	obs := "2026-06-01T09:00:00Z"
	result := EvaluateBundleAt(compiled, Facts{
		"hold": true, "observed_at.hold": obs, "s2": true, "observed_at.s2": obs,
	}, now)
	if result.Result != "BLOCKED" {
		t.Fatalf("cluster with a blocked member: want BLOCKED, got %s", result.Result)
	}
}
