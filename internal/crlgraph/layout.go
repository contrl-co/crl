package crlgraph

import (
	"sort"

	"gitlab.com/contrl-group/crl/internal/crypto"
)

// Layout positions a Graph deterministically: nested entities (rule ▷ collector ▷
// signal, rule/cluster ▷ predicate) are packed into sub-grids, the top-level
// entities (rules, clusters, global predicates) are placed by a layered
// (Sugiyama-style) pass over their cross-references, and every cross-reference
// edge is routed orthogonally. The same Graph always yields the same coordinates,
// so the result is hashable and golden-testable — the backend-owned-layout pattern
// verified in Binary Ninja and Ghidra, brought to CRL.
//
// This mirrors Cutter's GraphGridLayout ("layered graph layout approach … nodes
// placed in a grid") with orthogonal edge routing, adapted for CRL's nesting.

// Geometry constants (CSS pixels).
const (
	leafWidth     = 256.0
	signalHeight  = 58.0
	predHeight    = 62.0
	ruleHeader    = 44.0
	collHeader    = 56.0
	clusterHeader = 44.0
	pad           = 16.0
	childGap      = 12.0
	nodeGap       = 48.0 // horizontal gap between top-level nodes in a layer
	layerGap      = 64.0 // vertical gap between layers
)

// Point is an absolute pixel coordinate.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PositionedNode is a graph node with absolute geometry.
type PositionedNode struct {
	Node
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// RoutedEdge is a graph edge with an orthogonal polyline (absolute points).
type RoutedEdge struct {
	Edge
	Points []Point `json:"points"`
}

// LayoutResult is a fully positioned graph ready for rendering.
type LayoutResult struct {
	Nodes  []PositionedNode `json:"nodes"`
	Edges  []RoutedEdge     `json:"edges"`
	Width  float64          `json:"width"`
	Height float64          `json:"height"`
}

// Hash returns the canonical-JSON digest of the layout (regression/determinism).
func (l LayoutResult) Hash() (string, error) { return crypto.Digest(l) }

// Layout computes positions and orthogonal edge routes for a graph.
func Layout(g Graph) LayoutResult {
	l := &layouter{
		node:     map[string]Node{},
		children: map[string][]string{},
		size:     map[string]size{},
		rel:      map[string]Point{},
		abs:      map[string]Point{},
		visiting: map[string]bool{},
	}
	for _, n := range g.Nodes {
		l.node[n.ID] = n
		l.children[n.Parent] = append(l.children[n.Parent], n.ID)
	}

	// 1. Size every node bottom-up (and place children relative to their parent).
	topLevel := l.children[""]
	for _, id := range topLevel {
		l.sizeNode(id)
	}

	// 2. Place the top-level entities with a layered pass over cross-references.
	l.placeTopLevel(g, topLevel)

	// 3. Resolve absolute positions for all nested nodes.
	for _, id := range topLevel {
		l.resolveAbs(id, Point{})
	}

	// 4. Emit positioned nodes (in graph order) and routed edges.
	return l.emit(g)
}

type size struct{ w, h float64 }

type layouter struct {
	node     map[string]Node
	children map[string][]string
	size     map[string]size
	rel      map[string]Point // position relative to parent
	abs      map[string]Point // absolute position
	visiting map[string]bool  // cycle guard for sizeNode (Build emits a tree; defensive)
}

// sizeNode computes a node's size and lays its children out relative to it.
func (l *layouter) sizeNode(id string) size {
	if s, ok := l.size[id]; ok {
		return s
	}
	if l.visiting[id] {
		return l.leafSize(id) // defensive: a parent/child cycle (never produced by Build)
	}
	l.visiting[id] = true
	defer delete(l.visiting, id)
	kids := l.children[id]
	if len(kids) == 0 {
		s := l.leafSize(id)
		l.size[id] = s
		return s
	}
	header := l.headerHeight(id)
	y := header + pad
	maxW := 0.0
	for _, k := range kids {
		ks := l.sizeNode(k)
		l.rel[k] = Point{X: pad, Y: y}
		y += ks.h + childGap
		if ks.w > maxW {
			maxW = ks.w
		}
	}
	s := size{w: maxW + 2*pad, h: y - childGap + pad}
	l.size[id] = s
	return s
}

func (l *layouter) leafSize(id string) size {
	switch l.node[id].Kind {
	case NodeSignal:
		return size{w: leafWidth, h: signalHeight}
	default: // predicate (and any other leaf)
		return size{w: leafWidth, h: predHeight}
	}
}

func (l *layouter) headerHeight(id string) float64 {
	switch l.node[id].Kind {
	case NodeCollector:
		return collHeader
	case NodeCluster:
		return clusterHeader
	default: // rule
		return ruleHeader
	}
}

// placeTopLevel assigns relative (== absolute, since parent is root) positions to
// the top-level nodes using longest-path layering over the dependency DAG plus a
// barycenter ordering within each layer.
func (l *layouter) placeTopLevel(g Graph, topLevel []string) {
	if len(topLevel) == 0 {
		return
	}
	isTop := map[string]bool{}
	for _, id := range topLevel {
		isTop[id] = true
	}

	// Dependency edges between top-level ancestors (source depends on target),
	// collected in deterministic graph-edge order.
	deps := map[string][]string{}
	seen := map[string]bool{}
	for _, e := range g.Edges {
		from := l.ancestor(e.Source)
		to := l.ancestor(e.Target)
		if from == to || from == "" || to == "" {
			continue
		}
		key := from + "\x00" + to
		if seen[key] {
			continue
		}
		seen[key] = true
		deps[from] = append(deps[from], to)
	}

	// Longest-path layer assignment: layer(n) = 1 + max(layer of its deps).
	layerOf := map[string]int{}
	var assign func(string, map[string]bool) int
	assign = func(n string, path map[string]bool) int {
		if v, ok := layerOf[n]; ok {
			return v
		}
		if path[n] { // defensive cycle guard (CRL grammar forbids these)
			return 0
		}
		path[n] = true
		best := 0
		for _, d := range deps[n] {
			if c := assign(d, path) + 1; c > best {
				best = c
			}
		}
		delete(path, n)
		layerOf[n] = best
		return best
	}
	maxLayer := 0
	for _, id := range topLevel {
		if v := assign(id, map[string]bool{}); v > maxLayer {
			maxLayer = v
		}
	}

	// Group nodes by layer (graph order within a layer to start).
	layers := make([][]string, maxLayer+1)
	for _, id := range topLevel {
		layers[layerOf[id]] = append(layers[layerOf[id]], id)
	}

	// Layer heights and y offsets.
	layerY := make([]float64, len(layers))
	cursorY := 0.0
	for li, layer := range layers {
		layerY[li] = cursorY
		maxH := 0.0
		for _, id := range layer {
			if h := l.size[id].h; h > maxH {
				maxH = h
			}
		}
		cursorY += maxH + layerGap
	}

	// Place layer by layer. Layer 0 keeps graph order; later layers are ordered by
	// the barycenter of their already-placed dependency targets (lower layers).
	centerX := map[string]float64{}
	for li, layer := range layers {
		if li > 0 {
			sort.SliceStable(layer, func(a, b int) bool {
				ba, oka := l.barycenter(deps[layer[a]], centerX)
				bb, okb := l.barycenter(deps[layer[b]], centerX)
				if oka && okb && ba != bb {
					return ba < bb
				}
				if oka != okb {
					return oka
				}
				return l.node[layer[a]].Label < l.node[layer[b]].Label
			})
		}
		cursorX := 0.0
		for _, id := range layer {
			w := l.size[id].w
			l.rel[id] = Point{X: cursorX, Y: layerY[li]}
			centerX[id] = cursorX + w/2
			cursorX += w + nodeGap
		}
	}
}

func (l *layouter) barycenter(targets []string, centerX map[string]float64) (float64, bool) {
	sum, n := 0.0, 0
	for _, t := range targets {
		if cx, ok := centerX[t]; ok {
			sum += cx
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// ancestor walks Parent pointers to the top-level container of a node. The loop
// is capped at the node count so a (Build-impossible) parent cycle terminates.
func (l *layouter) ancestor(id string) string {
	for i := 0; i <= len(l.node); i++ {
		n, ok := l.node[id]
		if !ok {
			return ""
		}
		if n.Parent == "" {
			return id
		}
		id = n.Parent
	}
	return id // defensive: parent cycle
}

// resolveAbs propagates absolute positions down the nesting tree.
func (l *layouter) resolveAbs(id string, parentAbs Point) {
	if _, done := l.abs[id]; done {
		return // defensive: already resolved (breaks a parent/child cycle)
	}
	r := l.rel[id]
	a := Point{X: parentAbs.X + r.X, Y: parentAbs.Y + r.Y}
	l.abs[id] = a
	for _, k := range l.children[id] {
		l.resolveAbs(k, a)
	}
}

func (l *layouter) emit(g Graph) LayoutResult {
	out := LayoutResult{}
	for _, n := range g.Nodes {
		a := l.abs[n.ID]
		s := l.size[n.ID]
		out.Nodes = append(out.Nodes, PositionedNode{Node: n, X: a.X, Y: a.Y, Width: s.w, Height: s.h})
		if right := a.X + s.w; right > out.Width {
			out.Width = right
		}
		if bottom := a.Y + s.h; bottom > out.Height {
			out.Height = bottom
		}
	}
	for _, e := range g.Edges {
		out.Edges = append(out.Edges, RoutedEdge{Edge: e, Points: l.route(e.Source, e.Target)})
	}
	return out
}

// route produces an orthogonal polyline between two nodes' borders. It exits the
// source and enters the target on whichever sides face each other, with a single
// mid-line bend (a right-angle Z when the centers are not aligned).
func (l *layouter) route(srcID, dstID string) []Point {
	sa, ss := l.abs[srcID], l.size[srcID]
	da, ds := l.abs[dstID], l.size[dstID]
	sCx, dCx := sa.X+ss.w/2, da.X+ds.w/2
	sCy, dCy := sa.Y+ss.h/2, da.Y+ds.h/2

	switch {
	case da.Y >= sa.Y+ss.h: // target below source
		midY := (sa.Y + ss.h + da.Y) / 2
		return []Point{{sCx, sa.Y + ss.h}, {sCx, midY}, {dCx, midY}, {dCx, da.Y}}
	case da.Y+ds.h <= sa.Y: // target above source
		midY := (sa.Y + (da.Y + ds.h)) / 2
		return []Point{{sCx, sa.Y}, {sCx, midY}, {dCx, midY}, {dCx, da.Y + ds.h}}
	case da.X >= sa.X+ss.w: // target to the right
		midX := (sa.X + ss.w + da.X) / 2
		return []Point{{sa.X + ss.w, sCy}, {midX, sCy}, {midX, dCy}, {da.X, dCy}}
	case da.X+ds.w <= sa.X: // target to the left
		midX := (sa.X + (da.X + ds.w)) / 2
		return []Point{{sa.X, sCy}, {midX, sCy}, {midX, dCy}, {da.X + ds.w, dCy}}
	default: // overlapping rectangles (degenerate): route orthogonally, vertical then horizontal
		return []Point{{sCx, sCy}, {sCx, dCy}, {dCx, dCy}}
	}
}
