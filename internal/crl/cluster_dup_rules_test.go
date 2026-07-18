package crl

import (
	"strings"
	"testing"
)

// A second `rules` statement in one cluster used to silently overwrite the
// first, dropping members the author wrote and flipping DENIED to
// AUTHORIZED with no diagnostic. It must be rejected.
func TestClusterRejectsDuplicateRulesStatement(t *testing.T) {
	src := `crl v1
package t
bundle t.c

rule foundation
	target c.f
	collector a m file_upload from /a
		signal fp bool from a.fp ttl 60d
	need fp == true
	quorum a

rule utilities
	target c.u
	collector b m file_upload from /b
		signal up bool from b.up ttl 60d
	need up == true
	quorum b

cluster ready
	rules foundation + utilities
	rules foundation
	need foundation == true

need ready == true
quorum ready
`
	_, err := CompileBundle(src)
	if err == nil {
		t.Fatal("a cluster with two rules statements compiled; the second silently dropped members")
	}
	if !strings.Contains(err.Error(), "duplicate rules") {
		t.Fatalf("unexpected error, want duplicate-rules rejection: %v", err)
	}
}

// The "last assignment silently wins" defect class: duplicate target,
// package, and bundle statements used to overwrite silently. Each must be
// rejected so the compiled program matches the source a reviewer reads.
func TestDuplicateSingletonStatementsRejected(t *testing.T) {
	base := "crl v1\npackage t\nbundle t.b\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal a bool from f.a ttl 30d\n\tneed a == true\n"
	cases := map[string]string{
		"duplicate target":  strings.Replace(base, "\ttarget t.x\n", "\ttarget t.x\n\ttarget t.y\n", 1),
		"duplicate package": strings.Replace(base, "package t\n", "package t\npackage u\n", 1),
		"duplicate bundle":  strings.Replace(base, "bundle t.b\n", "bundle t.b\nbundle t.c\n", 1),
	}
	for name, src := range cases {
		if _, err := CompileBundle(src); err == nil {
			t.Errorf("%s compiled; the earlier value was silently dropped", name)
		}
	}
}
