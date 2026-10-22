package engine

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// TestPinnedRouteGuardSkipsUnroutedEdges checks that the guard never
// snapshots a pinned edge before it has a route, so that it cannot put an
// empty route back over the one a later stage gives it.
func TestPinnedRouteGuardSkipsUnroutedEdges(t *testing.T) {
	g := layoutgraph.NewGraph()
	a := g.AddNode(layoutgraph.NewNode(1, 100, 100))
	a.TopLeft = geo.NewPoint(0, 0)
	b := g.AddNode(layoutgraph.NewNode(2, 100, 100))
	b.TopLeft = geo.NewPoint(300, 0)
	e := g.Connect(a, b)
	e.FromSide, e.ToSide = new(geo.Right), new(geo.Left)

	for _, points := range [][]*geo.Point{nil, {geo.NewPoint(100, 50)}} {
		e.Points = points
		if guard := newPinnedRouteGuard(g); guard != nil {
			t.Fatalf("%d points: guard snapshotted an unrouted edge", len(points))
		}
	}

	// A sound route is kept, and put back when a later stage breaks it.
	e.Points = []*geo.Point{geo.NewPoint(100, 50), geo.NewPoint(300, 50)}
	guard := newPinnedRouteGuard(g)
	if guard == nil || len(guard.routes) != 1 {
		t.Fatal("guard did not snapshot the routed edge")
	}
	e.Points = []*geo.Point{geo.NewPoint(50, 100), geo.NewPoint(300, 50)}
	guard.restore()
	if len(e.Points) != 2 || e.Points[0].X != 100 || e.Points[0].Y != 50 {
		t.Fatalf("broken route not restored: %v", e.Points)
	}
}
