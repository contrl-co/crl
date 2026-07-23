package crl

import (
	"testing"
	"time"
)

// An empty or zero-value compiled bundle must fail closed, never authorize.
func TestEmptyBundleFailsClosed(t *testing.T) {
	got := EvaluateBundleAt(CompiledBundle{}, Facts{}, time.Now().UTC())
	if got.Authorized || got.Result == "AUTHORIZED" {
		t.Fatalf("empty bundle must not authorize, got Result=%q Authorized=%v", got.Result, got.Authorized)
	}
}
