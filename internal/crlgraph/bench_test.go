package crlgraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/contrl-co/crl/internal/crl"
)

// largeBundleSource builds a valid n-rule bundle (each rule: 1 collector, 2
// signals, 2 needs, 1 quorum) plus a cluster over the first few rules and a
// global predicate — for performance testing the graph build + layout.
func largeBundleSource(n int) string {
	var b strings.Builder
	b.WriteString("crl v1\n")
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("rule_%d", i)
		names = append(names, name)
		fmt.Fprintf(&b, "\nrule %s\n", name)
		fmt.Fprintf(&b, "\ttarget asp.%d\n", i)
		fmt.Fprintf(&b, "\tcollector col_%d p webhook from /c%d.json\n", i, i)
		fmt.Fprintf(&b, "\t\tsignal sig_%d number from f ttl 30d\n", i)
		fmt.Fprintf(&b, "\t\tsignal flag_%d bool from g ttl 30d\n", i)
		fmt.Fprintf(&b, "\tneed sig_%d >= 1\n", i)
		fmt.Fprintf(&b, "\tneed flag_%d == true\n", i)
		fmt.Fprintf(&b, "\tquorum col_%d\n", i)
	}
	cn := n
	if cn > 8 {
		cn = 8
	}
	fmt.Fprintf(&b, "\ncluster clu\n\trules %s\n\tquorum %s\n", strings.Join(names[:cn], " + "), strings.Join(names[:cn], " & "))
	b.WriteString("\nneed clu == true\n")
	return b.String()
}

func BenchmarkBuildLayout(b *testing.B) {
	for _, n := range []int{40, 120} {
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			compiled, err := crl.CompileBundle(largeBundleSource(n))
			if err != nil {
				b.Fatalf("compile: %v", err)
			}
			bundle := compiled.Program
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				g := Build(bundle)
				if got := Layout(g); len(got.Nodes) == 0 {
					b.Fatal("empty layout")
				}
			}
		})
	}
}
