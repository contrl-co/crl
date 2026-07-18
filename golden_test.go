package crl

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite examples/golden.txt from current compiler output")

// TestExamplesMatchGoldenHashes is the determinism gate: every example
// compiles, and its canonical text and bundle hash match the
// checked-in golden file byte for byte. Any drift — a compiler change,
// a platform difference, an edited example — fails here. Regenerate
// with -update-golden only for an intentional, reviewed change.
func TestExamplesMatchGoldenHashes(t *testing.T) {
	entries, err := os.ReadDir("examples")
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	var lines []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".crl") {
			continue
		}
		path := filepath.Join("examples", entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		compiled, err := Compile(string(source))
		if err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		lines = append(lines, fmt.Sprintf("%s  %s", compiled.Hash, entry.Name()))
	}
	if len(lines) == 0 {
		t.Fatal("no examples found")
	}
	sort.Strings(lines)
	got := strings.Join(lines, "\n") + "\n"

	goldenPath := filepath.Join("examples", "golden.txt")
	if *updateGolden {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("rewrote %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update-golden to create): %v", err)
	}
	if string(want) != got {
		t.Fatalf("bundle hashes drifted from examples/golden.txt\n\nwant:\n%s\ngot:\n%s\nIf this change is intentional, it needs a new edition (see spec/editions.md); regenerate with -update-golden.", want, got)
	}
}

// TestExamplesCanonicalRoundTrip asserts the formatter fixed point on
// the whole corpus: canonical text recompiles to itself and to the
// same hash.
func TestExamplesCanonicalRoundTrip(t *testing.T) {
	entries, err := os.ReadDir("examples")
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".crl") {
			continue
		}
		path := filepath.Join("examples", entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		first, err := Compile(string(source))
		if err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		second, err := Compile(first.CanonicalText)
		if err != nil {
			t.Fatalf("recompile canonical %s: %v", path, err)
		}
		if second.Hash != first.Hash {
			t.Fatalf("%s: canonical round-trip changed hash", path)
		}
		if second.CanonicalText != first.CanonicalText {
			t.Fatalf("%s: canonical text is not a fixed point", path)
		}
	}
}
