package routing

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestPinnedFallbackRouteHonoursSides(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to *geo.Orientation
	}{
		{"right to top", new(geo.Right), new(geo.Top)},
		{"top to top", new(geo.Top), new(geo.Top)},
		{"bottom to left", new(geo.Bottom), new(geo.Left)},
		{"left only", new(geo.Left), nil},
		{"top target only", nil, new(geo.Top)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := layoutgraph.NewGraph()
			a := g.AddNode(layoutgraph.NewNode(1, 100, 100))
			a.TopLeft = geo.NewPoint(0, 0)
			b := g.AddNode(layoutgraph.NewNode(2, 80, 60))
			b.TopLeft = geo.NewPoint(200, 20)
			// An obstacle right between the two stubs' direct connection.
			c := g.AddNode(layoutgraph.NewNode(3, 40, 40))
			c.TopLeft = geo.NewPoint(130, -80)
			e := g.Connect(a, b)
			e.FromSide, e.ToSide = tc.from, tc.to

			ovg, err := buildOVGFromGraph(context.Background(), g, nil)
			if err != nil {
				t.Fatal(err)
			}
			router, err := newOVGEdgeRouter(context.Background(), Default, ovg, g, nil, g.Edges)
			if err != nil {
				t.Fatal(err)
			}
			if err := router.bindWork(context.Background()); err != nil {
				t.Fatal(err)
			}
			nodes, _, err := router.pinnedFallbackRoute(e)
			if err != nil {
				t.Fatal(err)
			}
			route := &Route{GEdge: e, OVGNodes: nodes}
			points := route.createSegmentEndpoints()
			for i := 1; i < len(points); i++ {
				if points[i-1].X != points[i].X && points[i-1].Y != points[i].Y {
					t.Fatalf("segment %d is not orthogonal: %v", i, points)
				}
				for _, n := range []*layoutgraph.Node{a, b, c} {
					if segmentEntersNode(n, points[i-1], points[i]) {
						t.Errorf("segment %v -> %v passes through node %d: %v", points[i-1], points[i], n.ID, points)
					}
				}
			}
			check := func(node *layoutgraph.Node, side *geo.Orientation, end, next *geo.Point) {
				if side == nil {
					return
				}
				if node.PortOrientation(end) != *side {
					t.Errorf("end %v is on side %v of node %d, want %v", end, node.PortOrientation(end).ToString(), node.ID, side.ToString())
				}
				n := sideNormal(*side)
				if d := direction(end, next); d != n {
					t.Errorf("route leaves node %d towards %v, want %v: %v", node.ID, d, n, points)
				}
			}
			check(a, tc.from, points[0], points[1])
			check(b, tc.to, points[len(points)-1], points[len(points)-2])
		})
	}
}

func TestPinnedFallbackRouteSkipsUnpinnedEdges(t *testing.T) {
	g := layoutgraph.NewGraph()
	a := g.AddNode(layoutgraph.NewNode(1, 100, 100))
	a.TopLeft = geo.NewPoint(0, 0)
	b := g.AddNode(layoutgraph.NewNode(2, 100, 100))
	b.TopLeft = geo.NewPoint(200, 0)
	e := g.Connect(a, b)
	ovg, err := buildOVGFromGraph(context.Background(), g, nil)
	if err != nil {
		t.Fatal(err)
	}
	router, err := newOVGEdgeRouter(context.Background(), Default, ovg, g, nil, g.Edges)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := router.pinnedFallbackRoute(e); err != errNoPinnedFallback {
		t.Fatalf("unpinned edge: err = %v, want errNoPinnedFallback", err)
	}
}
