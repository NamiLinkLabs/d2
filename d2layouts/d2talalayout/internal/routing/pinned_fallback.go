package routing

import (
	"errors"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

var errNoPinnedFallback = errors.New("no pinned fallback route")

// pinnedFallbackRoute routes an edge with a pinned side (D2's source-port /
// target-port) when the port search finds no path, instead of the straight
// fallback, which ignores pins. Each end leaves its side straight for
// MinRouteNodeClearance; an unpinned end uses the side facing the other node.
// The two stubs are joined by the orthogonal candidate with at most two bends
// that crosses the fewest other nodes, then is shortest.
func (router *ovgEdgeRouter) pinnedFallbackRoute(edge *layoutgraph.Edge) ([]*OVGNode, float64, error) {
	if !edge.HasPinnedSide() || edge.IsLoop() || edge.From.TopLeft == nil || edge.To.TopLeft == nil {
		return nil, 0, errNoPinnedFallback
	}
	fromCenter, toCenter := router.ovg.Centers[edge.From], router.ovg.Centers[edge.To]
	if fromCenter == nil || toCenter == nil {
		return nil, 0, errNoPinnedFallback
	}
	fromSide := pinnedOrFacingSide(edge.FromSide, edge.From, edge.To)
	toSide := pinnedOrFacingSide(edge.ToSide, edge.To, edge.From)
	fromPort, ok := middlePort(edge.From, fromSide)
	if !ok {
		return nil, 0, errNoPinnedFallback
	}
	toPort, ok := middlePort(edge.To, toSide)
	if !ok {
		return nil, 0, errNoPinnedFallback
	}
	c := layoutgraph.MinRouteNodeClearance
	fromNormal, toNormal := sideNormal(fromSide), sideNormal(toSide)
	a := geo.NewPoint(fromPort.X+fromNormal.X*c, fromPort.Y+fromNormal.Y*c)
	b := geo.NewPoint(toPort.X+toNormal.X*c, toPort.Y+toNormal.Y*c)

	top := math.Min(edge.From.TopLeft.Y, edge.To.TopLeft.Y) - c
	bottom := math.Max(edge.From.TopLeft.Y+edge.From.Height, edge.To.TopLeft.Y+edge.To.Height) + c
	left := math.Min(edge.From.TopLeft.X, edge.To.TopLeft.X) - c
	right := math.Max(edge.From.TopLeft.X+edge.From.Width, edge.To.TopLeft.X+edge.To.Width) + c

	var candidates [][]*geo.Point
	for _, y := range []float64{a.Y, b.Y, (a.Y + b.Y) / 2, top, bottom} {
		candidates = append(candidates, []*geo.Point{a, geo.NewPoint(a.X, y), geo.NewPoint(b.X, y), b})
	}
	for _, x := range []float64{a.X, b.X, (a.X + b.X) / 2, left, right} {
		candidates = append(candidates, []*geo.Point{a, geo.NewPoint(x, a.Y), geo.NewPoint(x, b.Y), b})
	}

	var best []*geo.Point
	bestCost := math.Inf(1)
	for _, candidate := range candidates {
		if err := router.work.step(); err != nil {
			return nil, 0, err
		}
		path := append([]*geo.Point{fromPort}, dedupePoints(candidate)...)
		path = append(path, toPort)
		cost, ok, err := router.pinnedFallbackCost(edge, path, fromNormal, toNormal)
		if err != nil {
			return nil, 0, err
		}
		if ok && cost < bestCost {
			best, bestCost = path, cost
		}
	}
	if best == nil {
		return nil, 0, errNoPinnedFallback
	}
	nodes := []*OVGNode{fromCenter}
	for _, p := range best {
		nodes = append(nodes, NewOVGNode(p.Copy()))
	}
	nodes = append(nodes, toCenter)
	return nodes, pathLength(best), nil
}

// pinnedFallbackCost rejects paths that double back over a stub or cut
// through either endpoint, and otherwise charges for crossing other nodes,
// length and bends.
func (router *ovgEdgeRouter) pinnedFallbackCost(edge *layoutgraph.Edge, path []*geo.Point, fromNormal, toNormal geo.Point) (float64, bool, error) {
	// path[1] is the source stub end; the leg after it may not head back.
	if len(path) > 3 && direction(path[1], path[2]).X*fromNormal.X+direction(path[1], path[2]).Y*fromNormal.Y < 0 {
		return 0, false, nil
	}
	n := len(path)
	if n > 3 {
		// The leg into the target stub may not arrive from inside the target.
		d := direction(path[n-3], path[n-2])
		if d.X*toNormal.X+d.Y*toNormal.Y > 0 {
			return 0, false, nil
		}
	}
	cost := pathLength(path)
	for i := 1; i < n; i++ {
		if path[i-1].X != path[i].X && path[i-1].Y != path[i].Y {
			return 0, false, nil
		}
		if i >= 2 && i < n-1 && segmentEntersNode(edge.From, path[i-1], path[i]) {
			return 0, false, nil
		}
		if i >= 2 && i < n-1 && segmentEntersNode(edge.To, path[i-1], path[i]) {
			return 0, false, nil
		}
		if i >= 2 {
			cost += router.turnCost
		}
		for _, node := range router.graph.Nodes {
			if err := router.work.step(); err != nil {
				return 0, false, err
			}
			if node == edge.From || node == edge.To || node.TopLeft == nil ||
				edge.From.IsDescendantOf(node) || edge.To.IsDescendantOf(node) {
				continue
			}
			if segmentEntersNode(node, path[i-1], path[i]) {
				cost += basicallyInfinity / 1e6
			}
		}
	}
	return cost, true, nil
}

func pinnedOrFacingSide(pinned *geo.Orientation, node, other *layoutgraph.Node) geo.Orientation {
	if pinned != nil {
		return *pinned
	}
	dx := (other.TopLeft.X + other.Width/2) - (node.TopLeft.X + node.Width/2)
	dy := (other.TopLeft.Y + other.Height/2) - (node.TopLeft.Y + node.Height/2)
	if math.Abs(dx) >= math.Abs(dy) {
		if dx >= 0 {
			return geo.Right
		}
		return geo.Left
	}
	if dy >= 0 {
		return geo.Bottom
	}
	return geo.Top
}

// middlePort returns the port of node on side closest to that side's middle.
func middlePort(node *layoutgraph.Node, side geo.Orientation) (*geo.Point, bool) {
	ports := node.PortsByOrientation(side)
	if len(ports) == 0 {
		return nil, false
	}
	cx, cy := node.TopLeft.X+node.Width/2, node.TopLeft.Y+node.Height/2
	best := ports[0]
	for _, p := range ports[1:] {
		if math.Abs(p.X-cx)+math.Abs(p.Y-cy) < math.Abs(best.X-cx)+math.Abs(best.Y-cy) {
			best = p
		}
	}
	return geo.NewPoint(best.X, best.Y), true
}

func sideNormal(side geo.Orientation) geo.Point {
	switch side {
	case geo.Top:
		return geo.Point{X: 0, Y: -1}
	case geo.Bottom:
		return geo.Point{X: 0, Y: 1}
	case geo.Left:
		return geo.Point{X: -1, Y: 0}
	default:
		return geo.Point{X: 1, Y: 0}
	}
}

func direction(from, to *geo.Point) geo.Point {
	return geo.Point{X: sign(to.X - from.X), Y: sign(to.Y - from.Y)}
}

func sign(v float64) float64 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func dedupePoints(points []*geo.Point) []*geo.Point {
	out := make([]*geo.Point, 0, len(points))
	for _, p := range points {
		if len(out) > 0 && out[len(out)-1].X == p.X && out[len(out)-1].Y == p.Y {
			continue
		}
		out = append(out, p)
	}
	return out
}

func pathLength(points []*geo.Point) float64 {
	length := 0.
	for i := 1; i < len(points); i++ {
		length += math.Abs(points[i].X-points[i-1].X) + math.Abs(points[i].Y-points[i-1].Y)
	}
	return length
}

// segmentEntersNode reports whether the axis-aligned segment a-b passes
// through the interior of node's box.
func segmentEntersNode(node *layoutgraph.Node, a, b *geo.Point) bool {
	minX, maxX := math.Min(a.X, b.X), math.Max(a.X, b.X)
	minY, maxY := math.Min(a.Y, b.Y), math.Max(a.Y, b.Y)
	return maxX > node.TopLeft.X && minX < node.TopLeft.X+node.Width &&
		maxY > node.TopLeft.Y && minY < node.TopLeft.Y+node.Height
}
