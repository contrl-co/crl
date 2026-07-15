package crlgraph

import (
	"reflect"
	"testing"

	"gitlab.com/contrl-group/crl/internal/crl"
)

const eps = 0.001

func mustLayout(t *testing.T, source string) (Graph, LayoutResult) {
	t.Helper()
	compiled, err := crl.CompileBundle(source)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	g := Build(compiled.Program)
	return g, Layout(g)
}

func posByID(l LayoutResult) map[string]PositionedNode {
	m := make(map[string]PositionedNode, len(l.Nodes))
	for _, n := range l.Nodes {
		m[n.ID] = n
	}
	return m
}

// TestLayoutCompletenessAndSizes: every graph node is positioned with a real size.
func TestLayoutCompletenessAndSizes(t *testing.T) {
	for name, source := range map[string]string{"fundingRule": fundingRuleSource, "bundle": bundleSource} {
		g, l := mustLayout(t, source)
		if len(l.Nodes) != len(g.Nodes) {
			t.Errorf("%s: positioned %d nodes, want %d", name, len(l.Nodes), len(g.Nodes))
		}
		if len(l.Edges) != len(g.Edges) {
			t.Errorf("%s: routed %d edges, want %d", name, len(l.Edges), len(g.Edges))
		}
		for _, n := range l.Nodes {
			if n.Width <= 0 || n.Height <= 0 {
				t.Errorf("%s: node %q has non-positive size %vx%v", name, n.ID, n.Width, n.Height)
			}
		}
		if l.Width <= 0 || l.Height <= 0 {
			t.Errorf("%s: layout bounds non-positive: %vx%v", name, l.Width, l.Height)
		}
	}
}

// TestLayoutChildrenWithinParents: nested nodes sit inside their parent's box.
func TestLayoutChildrenWithinParents(t *testing.T) {
	for name, source := range map[string]string{"fundingRule": fundingRuleSource, "bundle": bundleSource} {
		_, l := mustLayout(t, source)
		pos := posByID(l)
		for _, c := range l.Nodes {
			if c.Parent == "" {
				continue
			}
			p, ok := pos[c.Parent]
			if !ok {
				t.Errorf("%s: parent %q of %q not positioned", name, c.Parent, c.ID)
				continue
			}
			if c.X < p.X-eps || c.Y < p.Y-eps ||
				c.X+c.Width > p.X+p.Width+eps || c.Y+c.Height > p.Y+p.Height+eps {
				t.Errorf("%s: child %q (%.1f,%.1f %.1fx%.1f) escapes parent %q (%.1f,%.1f %.1fx%.1f)",
					name, c.ID, c.X, c.Y, c.Width, c.Height, p.ID, p.X, p.Y, p.Width, p.Height)
			}
		}
	}
}

// TestLayoutTopLevelNoOverlap: top-level boxes do not overlap each other.
func TestLayoutTopLevelNoOverlap(t *testing.T) {
	for name, source := range map[string]string{"fundingRule": fundingRuleSource, "bundle": bundleSource} {
		_, l := mustLayout(t, source)
		var top []PositionedNode
		for _, n := range l.Nodes {
			if n.Parent == "" {
				top = append(top, n)
			}
		}
		for i := 0; i < len(top); i++ {
			for j := i + 1; j < len(top); j++ {
				a, b := top[i], top[j]
				ow := min(a.X+a.Width, b.X+b.Width) - max(a.X, b.X)
				oh := min(a.Y+a.Height, b.Y+b.Height) - max(a.Y, b.Y)
				if ow > eps && oh > eps {
					t.Errorf("%s: top-level nodes %q and %q overlap by %.1fx%.1f", name, a.ID, b.ID, ow, oh)
				}
			}
		}
	}
}

// TestLayoutEdgesOrthogonalAndAnchored: routes are right-angled and anchored to
// the source and target node rectangles.
func TestLayoutEdgesOrthogonalAndAnchored(t *testing.T) {
	for name, source := range map[string]string{"fundingRule": fundingRuleSource, "bundle": bundleSource} {
		_, l := mustLayout(t, source)
		pos := posByID(l)
		for _, e := range l.Edges {
			if len(e.Points) < 2 {
				t.Errorf("%s: edge %q has %d points", name, e.ID, len(e.Points))
				continue
			}
			within := func(p Point, id string) bool {
				n := pos[id]
				return p.X >= n.X-eps && p.X <= n.X+n.Width+eps && p.Y >= n.Y-eps && p.Y <= n.Y+n.Height+eps
			}
			if !within(e.Points[0], e.Source) {
				t.Errorf("%s: edge %q start not anchored to source %q", name, e.ID, e.Source)
			}
			if !within(e.Points[len(e.Points)-1], e.Target) {
				t.Errorf("%s: edge %q end not anchored to target %q", name, e.ID, e.Target)
			}
			// Four-point routes must be orthogonal (each segment shares an axis).
			if len(e.Points) == 4 {
				for k := 0; k+1 < len(e.Points); k++ {
					a, b := e.Points[k], e.Points[k+1]
					if abs(a.X-b.X) > eps && abs(a.Y-b.Y) > eps {
						t.Errorf("%s: edge %q segment %d is diagonal", name, e.ID, k)
					}
				}
			}
		}
	}
}

// TestLayoutDeterministic: layout is a pure function of the graph.
func TestLayoutDeterministic(t *testing.T) {
	for _, source := range []string{fundingRuleSource, bundleSource} {
		compiled, err := crl.CompileBundle(source)
		if err != nil {
			t.Fatalf("CompileBundle: %v", err)
		}
		g := Build(compiled.Program)
		a := Layout(g)
		b := Layout(g)
		if !reflect.DeepEqual(a, b) {
			t.Error("two layouts of the same graph differ")
		}
		ha, _ := a.Hash()
		hb, _ := b.Hash()
		if ha != hb {
			t.Errorf("layout hash not stable: %s != %s", ha, hb)
		}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
