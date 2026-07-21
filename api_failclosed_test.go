package crl

import (
	"encoding/json"
	"testing"
	"time"
)

// A Compiled carries its program in an unexported field the compiler alone
// sets. A zero-value, JSON-round-tripped, or hand-built Compiled has no
// program and must not authorize vacuously.
func TestUncompiledBundleFailsClosed(t *testing.T) {
	src := "crl v1\npackage t\nbundle t.a\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal ok bool from f.ok ttl 30d\n\tneed ok == true\n"
	good, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	facts := Facts{"ok": true, "observed_at.ok": now.Format(time.RFC3339)}

	if r := good.EvaluateAt(facts, now).Result; r != Authorized {
		t.Fatalf("genuine compiled bundle: want AUTHORIZED, got %s", r)
	}

	blob, err := json.Marshal(good)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped Compiled
	if err := json.Unmarshal(blob, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for name, c := range map[string]Compiled{
		"zero-value":    {},
		"round-tripped": roundTripped,
		"hand-forged":   {Edition: EditionV1, Hash: good.Hash, CanonicalText: good.CanonicalText},
	} {
		if e := c.EvaluateAt(facts, now); e.Authorized {
			t.Errorf("%s Compiled authorized (%s); must fail closed", name, e.Result)
		}
		if e := c.Evaluate(facts); e.Authorized {
			t.Errorf("%s Compiled authorized via Evaluate (%s); must fail closed", name, e.Result)
		}
	}
}
