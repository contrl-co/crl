package crl

import (
	"strings"
	"testing"
)

// A deeply nested quorum expression must be rejected, not overflow the
// stack. Before the depth cap, ~1.7M parens killed crlc with an
// unrecoverable fatal error that recover() cannot catch.
func TestQuorumExpressionDepthIsBounded(t *testing.T) {
	src := "crl v1\npackage t\nbundle t.d\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal a bool from f.a ttl 30d\n" +
		"\tquorum " + strings.Repeat("(", 5000) + "a" + strings.Repeat(")", 5000) + "\n"
	if _, err := CompileBundle(src); err == nil {
		t.Fatal("a pathologically nested quorum expression compiled; the depth cap is gone")
	}
}

// The cap must not reject expressions a real author would write.
func TestQuorumExpressionNormalDepthCompiles(t *testing.T) {
	src := "crl v1\npackage t\nbundle t.d\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal a bool from f.a ttl 30d\n" +
		"\t\tsignal b bool from f.b ttl 30d\n\tquorum (a or b) and a\n"
	if _, err := CompileBundle(src); err != nil {
		t.Fatalf("a normal quorum expression was rejected: %v", err)
	}
}
