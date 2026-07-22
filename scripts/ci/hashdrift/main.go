// Command hashdrift compiles an older tree's example corpus with the
// CURRENT compiler and compares the hashes against that tree's own
// golden file. Unchanged source producing a different hash means the
// compiler moved hashes, which spec/editions.md requires a disclosed,
// deliberate correction for.
//
// Usage: hashdrift <base-tree-root>
// Exit 0: no drift. Exit 2: drift, one line per affected example.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	crl "gitlab.com/contrl-group/crl"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: hashdrift <base-tree-root>")
		os.Exit(1)
	}
	base := os.Args[1]
	golden, err := os.ReadFile(filepath.Join(base, "examples", "golden.txt"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read base golden: %v\n", err)
		os.Exit(1)
	}
	names := make([]string, 0, 16)
	want := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(golden)), "\n") {
		hash, name, ok := strings.Cut(line, "  ")
		if !ok {
			fmt.Fprintf(os.Stderr, "malformed golden line: %q\n", line)
			os.Exit(1)
		}
		names = append(names, name)
		want[name] = hash
	}
	sort.Strings(names)
	drift := false
	for _, name := range names {
		source, err := os.ReadFile(filepath.Join(base, "examples", name))
		if err != nil {
			fmt.Fprintf(os.Stderr, "read base example %s: %v\n", name, err)
			os.Exit(1)
		}
		compiled, err := crl.Compile(string(source))
		if err != nil {
			fmt.Printf("%s: base source no longer compiles: %v\n", name, err)
			drift = true
			continue
		}
		if compiled.Hash != want[name] {
			fmt.Printf("%s: hash moved %s.. -> %s..\n", name, want[name][:12], compiled.Hash[:12])
			drift = true
		}
	}
	if drift {
		os.Exit(2)
	}
	fmt.Printf("%d base examples compile to their base hashes\n", len(names))
}
