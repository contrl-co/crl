package crl

import (
	"testing"
	"time"
)

// A boolean quorum that is false on complete, fresh evidence reports
// INSUFFICIENT_EVIDENCE. Nothing is missing and nothing is stale, so
// DENIED is the correct outcome and bundleResult can already return it.
// quorumShortfallReason routes everything that is not staleness to
// ErrQuorumNotMet, which bundleResult maps to INSUFFICIENT_EVIDENCE
// alongside ErrUnknownFact.

const exclusiveApproval = "crl v1\npackage t.outcome\nbundle t.outcome\n" +
	"rule exclusive_approval\n" +
	"\ttarget permit.application\n" +
	"\tcollector board_a org api from /a.json\n" +
	"\t\tsignal approved_by_a bool from a.approved ttl 30d\n" +
	"\tcollector board_b org api from /b.json\n" +
	"\t\tsignal approved_by_b bool from b.approved ttl 30d\n" +
	"\tquorum (approved_by_a or approved_by_b) and not (approved_by_a and approved_by_b)\n"

func evaluateExclusiveApproval(t *testing.T, a, b bool) BundleEvaluation {
	t.Helper()
	compiled, err := CompileBundle(exclusiveApproval)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	observed := now.Add(-24 * time.Hour)
	facts := Facts{
		"approved_by_a":             a,
		"observed_at.approved_by_a": observed,
		"approved_by_b":             b,
		"observed_at.approved_by_b": observed,
		"provider.board_a":          true,
		"provider.board_b":          true,
	}
	return EvaluateBundleAt(compiled, facts, now)
}

func TestBooleanQuorumFalseOnCompleteEvidenceReportsInsufficientEvidence(t *testing.T) {
	eval := evaluateExclusiveApproval(t, true, true)
	if eval.Authorized {
		t.Fatal("both approvals present must not satisfy an exclusivity quorum")
	}
	// TODO: want DENIED once quorumShortfallReason distinguishes an unmet
	// expression from missing subjects.
	if eval.Result != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("Result = %q, want INSUFFICIENT_EVIDENCE (today's behavior; DENIED is correct)", eval.Result)
	}
}

func TestBooleanQuorumAuthorizesWhenExactlyOneSubjectIsPresent(t *testing.T) {
	eval := evaluateExclusiveApproval(t, true, false)
	if !eval.Authorized {
		t.Fatalf("exactly one approval must satisfy the exclusivity quorum, got %q", eval.Result)
	}
}

// Absent evidence and complete evidence are indistinguishable from the
// result alone.
func TestAbsentAndCompleteEvidenceShareOneQuorumOutcome(t *testing.T) {
	absent := evaluateExclusiveApproval(t, false, false)
	complete := evaluateExclusiveApproval(t, true, true)
	if absent.Result != complete.Result {
		t.Fatalf("outcomes already differ (%q vs %q); the routing has been fixed", absent.Result, complete.Result)
	}
	if complete.Result != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("Result = %q, want INSUFFICIENT_EVIDENCE", complete.Result)
	}
}

// DENIED is available and correctly routed for a non-quorum predicate.
// Only the quorum path collapses the two failures.
func TestFailedComparisonOnFreshEvidenceReportsDenied(t *testing.T) {
	source := "crl v1\npackage t.outcome\nbundle t.outcome\n" +
		"rule threshold\n" +
		"\ttarget order.release\n" +
		"\tcollector c org api from /x.json\n" +
		"\t\tsignal total number from x.total ttl 30d\n" +
		"\tneed total >= 250000\n" +
		"\tquorum c\n"
	compiled, err := CompileBundle(source)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	facts := Facts{
		"total":             100000.0,
		"observed_at.total": now.Add(-24 * time.Hour),
		"provider.c":        true,
	}
	eval := EvaluateBundleAt(compiled, facts, now)
	if eval.Authorized {
		t.Fatal("100000 must not satisfy a 250000 threshold")
	}
	if eval.Result != "DENIED" {
		t.Fatalf("Result = %q, want DENIED (evidence is present and fresh; the condition is unmet)", eval.Result)
	}
}
