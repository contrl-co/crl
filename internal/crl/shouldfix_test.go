package crl

import (
	"testing"
	"time"
)

// A millisecond duration must convert to real seconds (rounding up), not
// collapse to a constant 1 second — otherwise `age >= 60000ms` (60s) would
// authorize evidence only 1 second old.
func TestMillisecondDurationConvertsToSeconds(t *testing.T) {
	cases := map[string]int64{"1000ms": 1, "60000ms": 60, "1500ms": 2, "500ms": 1, "1ms": 1}
	for literal, want := range cases {
		got, err := parseDurationSeconds(literal)
		if err != nil {
			t.Fatalf("%s: %v", literal, err)
		}
		if got != want {
			t.Errorf("%s: got %d seconds, want %d", literal, got, want)
		}
	}
}

func TestAgeMillisecondsNotFailOpen(t *testing.T) {
	src := "crl v1\npackage p\nbundle b\nrule r\n\ttarget f.r\n\tcollector src attestor api from /x.json\n\t\tsignal m time from a.m ttl 5y\n\tneed m age >= 60000ms\n\tquorum src\n"
	compiled, err := CompileBundle(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	facts := Facts{"m": "2026-01-01T00:00:25Z", "observed_at.m": "2026-01-01T00:00:29Z", "src": true}
	now := time.Date(2026, 1, 1, 0, 0, 30, 0, time.UTC) // fact is 5s old, need >= 60s
	if got := EvaluateBundleAt(compiled, facts, now).Result; got == "AUTHORIZED" {
		t.Fatalf("age >= 60000ms must not authorize a 5-second-old fact, got %s", got)
	}
}

// An unquoted and/or/not used as a source or field path must stay literal,
// not alias to &/|/! and collide with the operator spelling.
func TestLogicalWordsNotAliasedInSourcePosition(t *testing.T) {
	a, err := CompileBundle("crl v1\nrule r\n\ttarget t.a\n\tcollector c org api from /x.json\n\t\tsignal s string from or ttl 30d\n\tneed s == \"x\"\n\tquorum c\n")
	if err != nil {
		t.Fatalf("compile 'from or': %v", err)
	}
	b, err := CompileBundle("crl v1\nrule r\n\ttarget t.a\n\tcollector c org api from /x.json\n\t\tsignal s string from \"|\" ttl 30d\n\tneed s == \"x\"\n\tquorum c\n")
	if err != nil {
		t.Fatalf("compile 'from \"|\"': %v", err)
	}
	if a.Hash == b.Hash {
		t.Fatal("source field 'or' must not collide with '|'")
	}
}

// Quorum still accepts and/or/not as operator aliases.
func TestQuorumWordAliasesStillWork(t *testing.T) {
	head := "crl v1\npackage p\nbundle b\nrule r\n\ttarget t.a\n\tcollector ca org api from /x.json\n\t\tsignal s1 bool from x.y ttl 30d\n\tcollector cb org api from /y.json\n\t\tsignal s2 bool from y.z ttl 30d\n\tneed s1 == true\n\tquorum "
	word, err := CompileBundle(head + "ca and cb\n")
	if err != nil {
		t.Fatalf("compile 'and': %v", err)
	}
	sym, err := CompileBundle(head + "ca & cb\n")
	if err != nil {
		t.Fatalf("compile '&': %v", err)
	}
	if word.Hash != sym.Hash {
		t.Fatal("quorum 'and' must be identical to '&'")
	}
}

func TestExtendsDepthCapped(t *testing.T) {
	src := "crl v1\nbundle b\nabstract rule a0\n\ttarget a.b\n\tcollector c org api from /x.json\n\t\tsignal g bool from f ttl 1h\n\tneed g == true\n"
	for i := 1; i < 300; i++ {
		src += "abstract rule a" + itoa(i) + " extends a" + itoa(i-1) + "\n"
	}
	src += "rule r extends a299\n\ttarget a.z\n\tcollector cc org api from /cc.json\n\t\tsignal ss bool from ff ttl 1h\n\tneed ss == true\n"
	if _, err := CompileBundle(src); err == nil {
		t.Fatal("an extends chain past the depth cap must be rejected")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
