package crl

import (
	"testing"
	"time"
)

func evalExpr(t *testing.T, expr string, facts Facts) string {
	t.Helper()
	src := "crl v1\npackage t\nbundle t.q\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n" +
		"\t\tsignal field_report bool from f.fr ttl 30d\n" +
		"\t\tsignal safety_hold bool from f.sh ttl 30d\n" +
		"\t\tsignal drone bool from f.dr ttl 30d\n" +
		"\tquorum " + expr + "\n"
	c, err := CompileBundle(src)
	if err != nil {
		t.Fatalf("compile %q: %v", expr, err)
	}
	return EvaluateBundleAt(c, facts, time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)).Result
}

// The negated-absent fail-open: missing evidence must not read as "cleared".
func TestQuorumNegatedAbsentDoesNotAuthorize(t *testing.T) {
	obs := "2026-06-01T09:00:00Z"
	base := Facts{"field_report": true, "observed_at.field_report": obs, "observed_at.safety_hold": obs}

	// safety_hold present and false -> hold affirmatively clear -> AUTHORIZED.
	clear := Facts{"field_report": true, "observed_at.field_report": obs, "safety_hold": false, "observed_at.safety_hold": obs}
	if got := evalExpr(t, "field_report and not safety_hold", clear); got != "AUTHORIZED" {
		t.Errorf("hold clear: want AUTHORIZED, got %s", got)
	}
	// safety_hold present and true -> hold active -> not authorized.
	if got := evalExpr(t, "field_report and not safety_hold",
		Facts{"field_report": true, "observed_at.field_report": obs, "safety_hold": true, "observed_at.safety_hold": obs}); got == "AUTHORIZED" {
		t.Error("hold active: must not authorize")
	}
	// safety_hold ABSENT, observed_at fresh -> evidence missing -> FAIL-OPEN closed.
	if got := evalExpr(t, "field_report and not safety_hold", base); got == "AUTHORIZED" {
		t.Errorf("hold absent: must not authorize (was the fail-open), got %s", got)
	}
}

// The false-denial guard: an OR with one present branch still authorizes
// when the other branch's evidence is absent.
func TestQuorumOrWithAbsentBranchStillAuthorizes(t *testing.T) {
	obs := "2026-06-01T09:00:00Z"
	facts := Facts{"field_report": true, "observed_at.field_report": obs, "observed_at.drone": obs}
	if got := evalExpr(t, "drone or field_report", facts); got != "AUTHORIZED" {
		t.Errorf("OR with present field_report and absent drone: want AUTHORIZED, got %s", got)
	}
}
