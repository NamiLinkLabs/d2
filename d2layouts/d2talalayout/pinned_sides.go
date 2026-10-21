package d2talalayout

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// pinnedSideViolations counts the pinned edge ends (source-port /
// target-port) whose route does not leave straight out of the pinned side.
func pinnedSideViolations(graph *layoutgraph.Graph) int {
	count := 0
	for _, e := range graph.Edges {
		if e == nil || len(e.Points) < 2 || e.IsLoop() {
			continue
		}
		n := len(e.Points)
		if e.FromSide != nil && !leavesSide(e.From, *e.FromSide, e.Points[0], e.Points[1]) {
			count++
		}
		if e.ToSide != nil && !leavesSide(e.To, *e.ToSide, e.Points[n-1], e.Points[n-2]) {
			count++
		}
	}
	return count
}

func leavesSide(node *layoutgraph.Node, side geo.Orientation, end, next *geo.Point) bool {
	if node == nil || node.TopLeft == nil || end == nil || next == nil {
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
	return dx*nx+dy*ny > 0 && math.Abs(dx*ny-dy*nx) <= 1
}
