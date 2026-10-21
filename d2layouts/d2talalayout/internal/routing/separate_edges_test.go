package routing

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestEdgeCanOverlapEdgesSeparateEdges(t *testing.T) {
	for _, separate := range []bool{false, true} {
		g := layoutgraph.NewGraph()
		g.SeparateEdges = separate
		a := layoutgraph.NewNode(1, 10, 10)
		b := layoutgraph.NewNode(2, 10, 10)
		c := layoutgraph.NewNode(3, 10, 10)
		for _, n := range []*layoutgraph.Node{a, b, c} {
			g.AddNewNodeToContainer(nil, n)
		}
		ab := g.Connect(a, b)
		ac := g.Connect(a, c)
		duplicate := g.Connect(a, b)
		for _, e := range []*layoutgraph.Edge{ab, ac, duplicate} {
			e.TargetArrowhead = layoutgraph.Arrowhead("triangle")
		}
		if !edgeCanOverlapEdges(ac, nil, nil, nil) {
			t.Fatalf("separate=%v: an edge must be free to use segments no other route uses", separate)
		}
		// Directed edges out of one source are bundled by default.
		for _, other := range []*layoutgraph.Edge{ab, duplicate} {
			want := !separate
			if got := edgeCanOverlapEdges(ac, []*layoutgraph.Edge{other}, nil, nil); got != want {
				t.Fatalf("separate=%v: edgeCanOverlapEdges = %v, want %v", separate, got, want)
			}
		}
	}
}

func TestFixClusterEdgeBranchingKeepsSeparateEdges(t *testing.T) {
	g := layoutgraph.NewGraph()
	g.SeparateEdges = true
	external := layoutgraph.NewNode(1, 10, 10)
	left := layoutgraph.NewNode(2, 10, 10)
	right := layoutgraph.NewNode(3, 10, 10)
	external.TopLeft = geo.NewPoint(10_000, 10_000)
	left.TopLeft = geo.NewPoint(11_000, 11_000)
	right.TopLeft = geo.NewPoint(12_000, 12_000)
	for _, n := range []*layoutgraph.Node{external, left, right} {
		g.AddNewNodeToContainer(nil, n)
	}
	cluster := &layoutgraph.Cluster{
		Vessel:             layoutgraph.NewNode(4, 10, 10),
		Nodes:              []*layoutgraph.Node{left, right},
		Arrangement:        layoutgraph.Row,
		DesiredArrangement: layoutgraph.Row,
		Graph:              g,
	}
	left.Cluster = cluster
	right.Cluster = cluster
	// Without SeparateEdges both branches move onto y=10, as in
	// TestFixClusterEdgeBranchingChoosesLowestCostPoint.
	leftEdge := g.Connect(external, left)
	leftEdge.Points = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(0, -10), geo.NewPoint(-100, -10), geo.NewPoint(-100, 100)}
	rightEdge := g.Connect(external, right)
	rightEdge.Points = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(0, 10), geo.NewPoint(100, 10), geo.NewPoint(100, -5)}

	if err := FixClusterEdgeBranching(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if leftEdge.Points[1].Y != -10 || rightEdge.Points[1].Y != 10 {
		t.Fatalf("branch point y coordinates = (%v, %v), want them unchanged at (-10, 10)", leftEdge.Points[1].Y, rightEdge.Points[1].Y)
	}
}

// Two nested staircase routes whose vertical legs are 0.5px apart. Balancing
// treats segments within 1px as one shared route and moves them together,
// unless edges are separate.
func TestBalanceEdgeSegmentsSpreadsNearlyCoincidentSeparateEdges(t *testing.T) {
	for _, separate := range []bool{false, true} {
		g := layoutgraph.NewGraph()
		g.SeparateEdges = separate
		from := layoutgraph.NewNode(1, 100, 100)
		from.TopLeft = geo.NewPoint(0, 0)
		to := layoutgraph.NewNode(2, 100, 100)
		to.TopLeft = geo.NewPoint(600, 200)
		g.AddNewNodeToContainer(nil, from)
		g.AddNewNodeToContainer(nil, to)
		outer := g.Connect(from, to)
		outer.Points = []*geo.Point{geo.NewPoint(100, 50), geo.NewPoint(300, 50), geo.NewPoint(300, 250), geo.NewPoint(600, 250)}
		inner := g.Connect(from, to)
		inner.Points = []*geo.Point{geo.NewPoint(100, 80), geo.NewPoint(299.5, 80), geo.NewPoint(299.5, 280), geo.NewPoint(600, 280)}

		if err := BalanceEdgeSegments(context.Background(), g); err != nil {
			t.Fatal(err)
		}
		outerX, innerX := outer.Points[1].X, inner.Points[1].X
		if outer.Points[2].X != outerX || inner.Points[2].X != innerX {
			t.Fatalf("separate=%v: vertical legs are no longer vertical: %v, %v", separate, outer.Points, inner.Points)
		}
		if !separate && outerX != innerX {
			t.Fatalf("default balancing split a shared route: x = %v and %v", outerX, innerX)
		}
		if separate && innerX >= outerX {
			t.Fatalf("separate edges were not spread apart in order: inner x = %v, outer x = %v", innerX, outerX)
		}
	}
}
