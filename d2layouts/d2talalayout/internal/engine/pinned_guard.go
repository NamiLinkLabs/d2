package engine

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// pinnedRouteGuard keeps the stages after edge routing from breaking the
// routes of edges pinned with source-port / target-port. Several of those
// stages (branch merging, balancing, shortcuts, rerouting) were written for
// free ports and may move a route's first or last leg off its pinned side or
// leave a diagonal. The guard snapshots every sound pinned route before a
// stage and puts it back if the stage broke it while both endpoints stayed
// where they were. Diagrams without pins are untouched.
type pinnedRouteGuard struct {
	routes []pinnedRouteSnapshot
}

type pinnedRouteSnapshot struct {
	edge     *layoutgraph.Edge
	points   []*geo.Point
	from, to nodeRect
}

type nodeRect struct{ x, y, w, h float64 }

func newPinnedRouteGuard(g *layoutgraph.Graph) *pinnedRouteGuard {
	var guard *pinnedRouteGuard
	for _, e := range g.Edges {
		// A route with fewer than two points (before the first edge routing
		// stage) is not a route: snapshotting it would let restore put an
		// empty route back over a real one.
		if e == nil || !e.HasPinnedSide() || len(e.Points) < 2 || !pinnedRouteSound(e) {
			continue
		}
		if guard == nil {
			guard = &pinnedRouteGuard{}
		}
		points := make([]*geo.Point, len(e.Points))
		for i, p := range e.Points {
			points[i] = p.Copy()
		}
		guard.routes = append(guard.routes, pinnedRouteSnapshot{
			edge:   e,
			points: points,
			from:   nodeBox(e.From),
			to:     nodeBox(e.To),
		})
	}
	return guard
}

// restore puts back each snapshotted route that is no longer sound, when its
// endpoints have not moved or resized since the snapshot.
func (guard *pinnedRouteGuard) restore() {
	if guard == nil {
		return
	}
	for _, r := range guard.routes {
		e := r.edge
		if len(r.points) < 2 || pinnedRouteSound(e) || nodeBox(e.From) != r.from || nodeBox(e.To) != r.to {
			continue
		}
		e.Points = r.points
	}
}

func nodeBox(n *layoutgraph.Node) nodeRect {
	if n == nil || n.TopLeft == nil {
		return nodeRect{}
	}
	return nodeRect{n.TopLeft.X, n.TopLeft.Y, n.Width, n.Height}
}

// pinnedRouteSound reports whether a pinned edge's route is made of
// horizontal and vertical segments and leaves each pinned side straight out
// of it. Loops and edges without a full route are not checked.
func pinnedRouteSound(e *layoutgraph.Edge) bool {
	if e.IsLoop() || len(e.Points) < 2 || e.From == nil || e.To == nil {
		return true
	}
	for i := 1; i < len(e.Points); i++ {
		a, b := e.Points[i-1], e.Points[i]
		if a == nil || b == nil {
			return false
		}
		if math.Abs(a.X-b.X) > pinnedAxisTolerance && math.Abs(a.Y-b.Y) > pinnedAxisTolerance {
			return false
		}
	}
	n := len(e.Points)
	if e.FromSide != nil && !leavesPinnedSide(e.From, *e.FromSide, e.Points[0], e.Points[1]) {
		return false
	}
	if e.ToSide != nil && !leavesPinnedSide(e.To, *e.ToSide, e.Points[n-1], e.Points[n-2]) {
		return false
	}
	return true
}

// pinnedAxisTolerance is how far a segment may stray from horizontal or
// vertical, in pixels, and still count as axis-aligned.
const pinnedAxisTolerance = 1.0

func leavesPinnedSide(node *layoutgraph.Node, side geo.Orientation, end, next *geo.Point) bool {
	if node.TopLeft == nil {
		return false
	}
	var nx, ny float64
	switch side {
	case geo.Top:
		ny = -1
	case geo.Bottom:
		ny = 1
	case geo.Left:
		nx = -1
	default:
		nx = 1
	}
	cx, cy := node.TopLeft.X+node.Width/2, node.TopLeft.Y+node.Height/2
	if (end.X-cx)*nx+(end.Y-cy)*ny <= 0 {
		return false
	}
	dx, dy := next.X-end.X, next.Y-end.Y
	return dx*nx+dy*ny > 0 && math.Abs(dx*ny-dy*nx) <= pinnedAxisTolerance
}
