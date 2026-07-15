package crl

import (
	"testing"
	"time"
)

// The `N of M a b c` count-quorum surface form desugars to the canonical count(...) >= N predicate: it
// compiles, evaluates, and produces a bundle hash identical to the
// explicit count() form. M must equal the subject count and 1 <= N <= M.
func TestQuorumNofMDesugarsToCount(t *testing.T) {
	base := "crl v1\nrule permit_application\ntarget permit.application\n" +
		"collector application_file municipality file_upload from /app.json\n" +
		"signal application_complete bool from application.complete ttl 30d\n" +
		"collector registry_check land_registry api from /registry.json\n" +
		"signal registry_ok bool from registry.ok ttl 30d\n" +
		"collector reviewer_attest reviewer attestation from /review.json\n" +
		"signal reviewer_ok bool from review.ok ttl 30d\n" +
		"need application_complete == true\n"

	ofm, err := CompileBundle(base + "quorum 2 of 3 application_file registry_check reviewer_attest\n")
	if err != nil {
		t.Fatalf("`N of M` quorum failed to compile: %v", err)
	}
	count, err := CompileBundle(base + "quorum count(application_file, registry_check, reviewer_attest) >= 2\n")
	if err != nil {
		t.Fatal(err)
	}
	if ofm.Hash != count.Hash {
		t.Fatalf("`2 of 3` must be identical to count(...) >= 2: %s vs %s", ofm.Hash, count.Hash)
	}

	// Evaluates as a count quorum: 2 of the 3 collectors present authorizes.
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := Facts{
		"application_complete":             true,
		"observed_at.application_complete": now,
		"provider.application_file":        true,
		"provider.registry_check":          true,
	}
	if e := EvaluateBundleAt(ofm, facts, now); !e.Authorized {
		t.Fatalf("2 of 3 present should authorize, got %q", e.Result)
	}
	facts["provider.registry_check"] = false
	if e := EvaluateBundleAt(ofm, facts, now); e.Authorized {
		t.Fatal("1 of 3 present must not authorize a 2-of-3 quorum")
	}

	for _, bad := range []string{
		"quorum 2 of 3 application_file registry_check\n", // M != subject count
		"quorum 5 of 2 application_file registry_check\n", // N > M
		"quorum 0 of 2 application_file registry_check\n", // N < 1
	} {
		if _, err := CompileBundle(base + bad); err == nil {
			t.Fatalf("expected compile error for %q", bad)
		}
	}
}
