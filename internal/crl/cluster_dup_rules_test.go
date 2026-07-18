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
