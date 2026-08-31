package crllint

import "testing"

func TestLintSourceAcceptsNamedPackageBundle(t *testing.T) {
	report := LintSource("valid.crl", `crl v1
package contrl.permits

bundle permit.launch {
    rule application_ready {
        target permit.application
        collector application_file municipality file_upload from /bundles/application.json schema permit_application_v1 {
            signal application_complete bool from application.complete ttl 30d
        }
        need application_complete == true
        quorum application_file
    }
}
`, Options{})
	if !report.OK {
		t.Fatalf("report.OK = false, diagnostics = %#v", report.Diagnostics)
	}
	if report.CompiledHash == "" {
		t.Fatal("compiled hash is empty")
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", report.Diagnostics)
	}
}

func TestLintSourceReportsAuthoringWarnings(t *testing.T) {
	report := LintSource("implicit.crl", `rule permit_application
    target permit.application
    collector application_file municipality file_upload from /bundles/application.json
        signal application_complete bool from application.complete ttl 30d
    need application_complete == true
    quorum application_file
`, Options{})
	if !report.OK {
		t.Fatalf("report.OK = false, diagnostics = %#v", report.Diagnostics)
	}
	wantCodes := []string{"CRL200", "CRL201", "CRL202"}
	for _, code := range wantCodes {
		if !hasDiagnostic(report, code, SeverityWarning) {
			t.Fatalf("missing warning %s in %#v", code, report.Diagnostics)
		}
	}
}

func TestLintSourceReportsCompilerErrors(t *testing.T) {
	report := LintSource("bad.crl", `crl v1
package contrl.bad
bundle bad.bundle {
    rule invalid_rule {
        target permit.application
        signal passed bool from passed ttl 30d
        need passed == true
    }
}
`, Options{})
	if report.OK {
		t.Fatalf("report.OK = true, diagnostics = %#v", report.Diagnostics)
	}
	if !hasDiagnostic(report, "CRL110", SeverityError) {
		t.Fatalf("missing CRL110 error in %#v", report.Diagnostics)
	}
}

func TestLintSourceWarnsWhenMultiObjectBundleHasNoFinalPolicy(t *testing.T) {
	report := LintSource("multi.crl", `crl v1
package contrl.launch
bundle launch.readiness {
    rule application_ready {
        target permit.application
        collector application_file municipality file_upload from /bundles/application.json {
            signal application_complete bool from application.complete ttl 30d
        }
        need application_complete == true
        quorum application_file
    }
    rule capital_ready {
        target finance.capital
        collector capital_file finance file_upload from /bundles/capital.json {
            signal capital_committed bool from capital.committed ttl 30d
        }
        need capital_committed == true
        quorum capital_file
    }
}
`, Options{})
	if !report.OK {
		t.Fatalf("report.OK = false, diagnostics = %#v", report.Diagnostics)
	}
	if !hasDiagnostic(report, "CRL203", SeverityWarning) {
		t.Fatalf("missing CRL203 warning in %#v", report.Diagnostics)
	}
}

func TestLintSourceWarnsOnDuplicateSourceFieldsInCollector(t *testing.T) {
	report := LintSource("dup.crl", `crl v1
package contrl.power
bundle power.readiness {
    rule power_ready {
        target utility.power
        collector utility_file utility file_upload from /bundles/power.json {
            signal power_built bool from power.built ttl 10y
            signal power_confirmed bool from power.built ttl 10y
        }
        need power_built == true
        quorum utility_file
    }
}
`, Options{})
	if !report.OK {
		t.Fatalf("report.OK = false, diagnostics = %#v", report.Diagnostics)
	}
	if !hasDiagnostic(report, "CRL205", SeverityWarning) {
		t.Fatalf("missing CRL205 warning in %#v", report.Diagnostics)
	}
}

func TestLintSourcePlacesMissingSignalErrorOnPredicateLine(t *testing.T) {
	report := LintSource("missing.crl", `crl v1
package contrl.permits
bundle permit.launch {
    rule application_ready {
        target permit.application
        collector application_file municipality file_upload from /bundles/application.json {
            signal application_complete bool from application.complete ttl 30d
        }
        need missing_signal == true
        quorum application_file
    }
}
`, Options{})
	diagnostic, ok := diagnosticByCode(report, "CRL120", SeverityError)
	if !ok {
		t.Fatalf("missing CRL120 error in %#v", report.Diagnostics)
	}
	if diagnostic.Line != 9 || diagnostic.Column != 9 {
		t.Fatalf("diagnostic span = %d:%d, want 9:9", diagnostic.Line, diagnostic.Column)
	}
}

func TestLintSourcePlacesUnsupportedOperatorErrorOnPredicateLine(t *testing.T) {
	report := LintSource("operator.crl", `crl v1
package contrl.permits
bundle permit.launch {
    rule application_ready {
        target permit.application
        collector application_file municipality file_upload from /bundles/application.json {
            signal application_complete bool from application.complete ttl 30d
        }
        need application_complete >= true
        quorum application_file
    }
}
`, Options{})
	diagnostic, ok := diagnosticByCode(report, "CRL120", SeverityError)
	if !ok {
		t.Fatalf("missing CRL120 error in %#v", report.Diagnostics)
	}
	if diagnostic.Line != 9 || diagnostic.Column != 9 {
		t.Fatalf("diagnostic span = %d:%d, want 9:9", diagnostic.Line, diagnostic.Column)
	}
}

func TestLintSourcePlacesMissingQuorumSubjectErrorOnQuorumLine(t *testing.T) {
	report := LintSource("quorum.crl", `crl v1
package contrl.permits
bundle permit.launch {
    rule application_ready {
        target permit.application
        collector application_file municipality file_upload from /bundles/application.json {
            signal application_complete bool from application.complete ttl 30d
        }
        need application_complete == true
        quorum missing_provider
    }
}
`, Options{})
	diagnostic, ok := diagnosticByCode(report, "CRL120", SeverityError)
	if !ok {
		t.Fatalf("missing CRL120 error in %#v", report.Diagnostics)
	}
	if diagnostic.Line != 10 || diagnostic.Column != 9 {
		t.Fatalf("diagnostic span = %d:%d, want 10:9", diagnostic.Line, diagnostic.Column)
	}
}

func hasDiagnostic(report Report, code string, severity Severity) bool {
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code && diagnostic.Severity == severity {
			return true
		}
	}
	return false
}

func diagnosticByCode(report Report, code string, severity Severity) (Diagnostic, bool) {
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code && diagnostic.Severity == severity {
			return diagnostic, true
		}
	}
	return Diagnostic{}, false
}

func diagnosticCodes(report Report) map[string]bool {
	codes := make(map[string]bool, len(report.Diagnostics))
	for _, diagnostic := range report.Diagnostics {
		codes[diagnostic.Code] = true
	}
	return codes
}

// CRL206/CRL207: silent duration canonicalization.
func TestLintSourceWarnsOnSilentTTLRounding(t *testing.T) {
	report := LintSource("rounding.crl", `crl v1
package contrl.permits

bundle permit.launch {
    rule readiness {
        target permit.readiness
        collector meter utility api_poll from /readings.json {
            signal flow_rate number from readings.flow ttl 5ms
            signal license_ok bool from license.ok ttl 2y
        }
        need flow_rate > 10
        need license_ok == true
        quorum meter
    }
}
`, Options{})
	codes := diagnosticCodes(report)
	if !codes["CRL206"] {
		t.Fatalf("expected CRL206 (ms ttl rounds to 1s), got %#v", report.Diagnostics)
	}
	if !codes["CRL207"] {
		t.Fatalf("expected CRL207 (year ttl is 365d, no leap handling), got %#v", report.Diagnostics)
	}
}

// CRL208: a block field named like an expiry flag reports BLOCKED, not
// EXPIRED (reason codes are type-driven).
func TestLintSourceWarnsOnExpirySuggestiveBlockName(t *testing.T) {
	report := LintSource("blockname.crl", `crl v1
package contrl.permits

bundle permit.launch {
    rule gate {
        target permit.gate
        collector registry municipality webhook from /registry.json {
            signal grid_hold_expired bool from grid.hold_expired ttl 30d
        }
        block grid_hold_expired
        quorum registry
    }
}
`, Options{})
	codes := diagnosticCodes(report)
	if !codes["CRL208"] {
		t.Fatalf("expected CRL208 (expiry-suggestive block name), got %#v", report.Diagnostics)
	}
}

// CRL208 must reach block predicates in cluster scope and in abstract
// rules, not only top-level concrete rules.
func TestLintSourceWarnsOnExpiryBlockNameInClusterAndAbstract(t *testing.T) {
	report := LintSource("cluster_abstract.crl", `crl v1
package contrl.permits

bundle permit.launch {
    abstract rule base {
        target permit.base
        collector registry municipality webhook from /registry.json {
            signal permit_expired bool from permit.expired ttl 30d
        }
        block permit_expired
    }
    rule concrete extends base {
        target permit.concrete
    }
    cluster gate {
        rules concrete
        block license_expires
        quorum concrete
    }
}
`, Options{})
	count := 0
	for _, d := range report.Diagnostics {
		if d.Code == "CRL208" {
			count++
		}
	}
	if count < 2 {
		t.Fatalf("expected CRL208 for both the abstract-rule block and the cluster block, got %d: %#v", count, report.Diagnostics)
	}
}

// CRL209: a declared-but-unreferenced signal does not affect the decision.
func TestLintFlagsUnreferencedSignal(t *testing.T) {
	src := "crl v1\npackage p.q\nbundle b.c\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c org api from /x.json\n" +
		"\t\tsignal used bool from x.a ttl 30d\n" +
		"\t\tsignal dead bool from x.b ttl 30d\n\tneed used == true\n"
	report := LintSource("t.crl", src, Options{})
	if !hasDiagnostic(report, "CRL209", SeverityWarning) {
		t.Fatalf("expected CRL209 for the unreferenced signal, got %#v", report.Diagnostics)
	}
}

// CRL210: an unindented predicate after a rule body is silently scoped into
// the rule by the carve-out; the author may have meant a global final policy.
func TestLintFlagsCarvedOutGlobalPredicate(t *testing.T) {
	src := "crl v1\npackage p.q\nbundle b.c\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal a bool from f.a ttl 30d\n" +
		"\tneed a == true\nneed a == true\n"
	report := LintSource("t.crl", src, Options{})
	if !hasDiagnostic(report, "CRL210", SeverityWarning) {
		t.Fatalf("expected CRL210 for the carved-out predicate, got %#v", report.Diagnostics)
	}
}

func TestLintFlagsCountQuorumOverSameSource(t *testing.T) {
	report := LintSource("dup.crl", `crl v1
package t
bundle t.dup

rule r
    target t.dup
    collector a municipality file_upload from /bundles/same.json
        signal sa bool from x.a ttl 30d
    collector b land_registry api from /bundles/same.json
        signal sb bool from x.b ttl 30d
    need sa == true
    quorum 2 of 2 a b
`, Options{})
	if report.OK {
		t.Fatalf("report.OK = true, diagnostics = %#v", report.Diagnostics)
	}
	diagnostic, ok := diagnosticByCode(report, "CRL121", SeverityError)
	if !ok {
		t.Fatalf("expected CRL121 compile error, got %#v", report.Diagnostics)
	}
	if diagnostic.Line != 12 || diagnostic.Column != 5 {
		t.Fatalf("diagnostic span = %d:%d, want 12:5", diagnostic.Line, diagnostic.Column)
	}
}

func TestLintFlagsBooleanQuorumOverSameSource(t *testing.T) {
	report := LintSource("dupbool.crl", `crl v1
package t
bundle t.dup

rule r
    target t.dup
    collector a municipality file_upload from /bundles/same.json
        signal sa bool from x.a ttl 30d
    collector b land_registry api from /bundles/same.json
        signal sb bool from x.b ttl 30d
    need sa == true
    quorum a & b
`, Options{})
	if !hasDiagnostic(report, "CRL121", SeverityError) {
		t.Fatalf("expected CRL121 compile error, got %#v", report.Diagnostics)
	}
}

func TestLintAllowsQuorumOverDistinctSources(t *testing.T) {
	report := LintSource("ok.crl", `crl v1
package t
bundle t.ok

rule r
    target t.ok
    collector a municipality file_upload from /bundles/a.json
        signal sa bool from x.a ttl 30d
    collector b land_registry api from /bundles/b.json
        signal sb bool from x.b ttl 30d
    need sa == true
    quorum 2 of 2 a b
`, Options{})
	if !report.OK {
		t.Fatalf("distinct sources should lint cleanly, got %#v", report.Diagnostics)
	}
}
