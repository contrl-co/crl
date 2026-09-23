package crl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const canonicalBundleSource = "crl v1\npackage t\nbundle t.a\n\nrule r\n\ttarget t.x\n" +
	"\tcollector c m file_upload from /f\n\t\tsignal ok bool from f.ok ttl 30d\n\tneed ok == true\n"

// The whole point of exposing these bytes: a consumer verifies a bundle with
// SHA-256 and nothing else. If this ever stops holding, a registry that
// accepted a compiled bundle on the strength of its hash was verifying
// nothing.
func TestCanonicalBundleReproducesTheHash(t *testing.T) {
	compiled, err := Compile(canonicalBundleSource)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	body, err := compiled.CanonicalBundle()
	if err != nil {
		t.Fatalf("canonical bundle: %v", err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != compiled.Hash {
		t.Errorf("sha256(CanonicalBundle()) = %s, Hash = %s", got, compiled.Hash)
	}
}

// Every shipped example, so the property is pinned against the real corpus
// rather than one hand-written rule.
func TestCanonicalBundleReproducesTheHashForEveryExample(t *testing.T) {
	paths, err := filepath.Glob("examples/*.crl")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no examples found: %v", err)
	}
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		compiled, err := Compile(string(source))
		if err != nil {
			continue // lint-failure fixtures are not this test's subject
		}
		body, err := compiled.CanonicalBundle()
		if err != nil {
			t.Errorf("%s: canonical bundle: %v", filepath.Base(path), err)
			continue
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != compiled.Hash {
			t.Errorf("%s: sha256 = %s, Hash = %s", filepath.Base(path), got, compiled.Hash)
		}
	}
}

// A Compiled that did not come from the compiler addresses no program.
// Returning bytes under its Hash would hand a consumer some other bundle's
// content under this one's address, which is worse than returning nothing.
func TestCanonicalBundleFailsClosedWithoutAProgram(t *testing.T) {
	good, err := Compile(canonicalBundleSource)
	if err != nil {
		t.Fatalf("compile: %v", err)
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
		body, err := c.CanonicalBundle()
		if !errors.Is(err, ErrNoProgram) {
			t.Errorf("%s Compiled: want ErrNoProgram, got %v", name, err)
		}
		if body != nil {
			t.Errorf("%s Compiled returned %d bytes; must return none", name, len(body))
		}
	}
}
