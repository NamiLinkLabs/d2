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
	fromStub := router.pinnedStubLength(edge, fromPort, fromNormal)
	toStub := router.pinnedStubLength(edge, toPort, toNormal)
	a := geo.NewPoint(fromPort.X+fromNormal.X*fromStub, fromPort.Y+fromNormal.Y*fromStub)
	b := geo.NewPoint(toPort.X+toNormal.X*toStub, toPort.Y+toNormal.Y*toStub)

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
		for _, node := range router.pinnedObstacles(edge) {
			if err := router.work.step(); err != nil {
				return 0, false, err
			}
			if segmentEntersNode(node, path[i-1], path[i]) {
				cost += pinnedCrossingCost
			}
		}
	}
	return cost, true, nil
}

// pinnedCrossingCost is what pinnedFallbackCost charges for each node a
// segment passes through.
const pinnedCrossingCost = basicallyInfinity / 1e6

// pinnedObstacles returns the nodes a route of edge must not pass through:
// every node the OVG was built around, including the nodes of other
// subgraphs near this one, except the edge's endpoints and their ancestors.
func (router *ovgEdgeRouter) pinnedObstacles(edge *layoutgraph.Edge) []*layoutgraph.Node {
	nodes := router.graph.Nodes
	if router.ovg != nil && len(router.ovg.NodesInsideBoundingBox) > 0 {
		nodes = router.ovg.NodesInsideBoundingBox
	}
	out := make([]*layoutgraph.Node, 0, len(nodes))
	for _, node := range nodes {
		if node == edge.From || node == edge.To || node.TopLeft == nil ||
			edge.From.IsDescendantOf(node) || edge.To.IsDescendantOf(node) {
			continue
		}
		out = append(out, node)
	}
	return out
}

// pinnedStubLength is how far a pinned fallback route runs straight out of
// port before it turns: MinRouteNodeClearance, or half the way to the nearest
// node in front of the port when that is closer, so the turn falls in the gap
// rather than inside that node.
func (router *ovgEdgeRouter) pinnedStubLength(edge *layoutgraph.Edge, port *geo.Point, normal geo.Point) float64 {
	c := layoutgraph.MinRouteNodeClearance
	nearest := math.Inf(1)
	for _, node := range router.pinnedObstacles(edge) {
		minX, maxX := node.TopLeft.X, node.TopLeft.X+node.Width
		minY, maxY := node.TopLeft.Y, node.TopLeft.Y+node.Height
		var dist float64
		switch {
		case normal.X > 0 && port.Y > minY && port.Y < maxY:
			dist = minX - port.X
		case normal.X < 0 && port.Y > minY && port.Y < maxY:
			dist = port.X - maxX
		case normal.Y > 0 && port.X > minX && port.X < maxX:
			dist = minY - port.Y
		case normal.Y < 0 && port.X > minX && port.X < maxX:
			dist = port.Y - maxY
		default:
			continue
		}
		if dist > 0 && dist < nearest {
			nearest = dist
		}
	}
	if nearest < 2*c {
		return math.Max(nearest/2, 1)
	}
	return c
}

// pinnedRouteBadness counts how badly the searched route points (centers
// excluded) treat a pinned edge: each node it passes through, plus each
// return into one of its own endpoints after leaving it.
func (router *ovgEdgeRouter) pinnedRouteBadness(edge *layoutgraph.Edge, points []*geo.Point) (int, error) {
	bad := 0
	obstacles := router.pinnedObstacles(edge)
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		for _, end := range []*layoutgraph.Node{edge.From, edge.To} {
			if end.TopLeft == nil || (edge.From != edge.To && (edge.From.IsDescendantOf(edge.To) || edge.To.IsDescendantOf(edge.From))) {
				continue
			}
			if segmentEntersNode(end, a, b) {
				bad++
			}
		}
		for _, node := range obstacles {
			if err := router.work.step(); err != nil {
				return 0, err
			}
			if segmentEntersNode(node, a, b) {
				bad++
			}
		}
	}
	return bad, nil
}

// improvePinnedRoute swaps a searched route of a pinned edge for the pinned
// fallback when the search could only find a route through other nodes or
// back through the edge's own endpoints (it charges those but does not forbid
// them) and the fallback does better.
func (router *ovgEdgeRouter) improvePinnedRoute(edge *layoutgraph.Edge, ovgNodes []*OVGNode, distance float64) ([]*OVGNode, float64, error) {
	if !edge.HasPinnedSide() || edge.IsLoop() || len(ovgNodes) < 4 {
		return ovgNodes, distance, nil
	}
	points := make([]*geo.Point, 0, len(ovgNodes)-2)
	for _, n := range ovgNodes[1 : len(ovgNodes)-1] {
		points = append(points, n.Point)
	}
	bad, err := router.pinnedRouteBadness(edge, points)
	if err != nil || bad == 0 {
		return ovgNodes, distance, err
	}
	fallback, fallbackDistance, err := router.pinnedFallbackRoute(edge)
	if err != nil {
		if isContextTermination(err) {
			return nil, 0, err
		}
		return ovgNodes, distance, nil
	}
	fallbackPoints := make([]*geo.Point, 0, len(fallback)-2)
	for _, n := range fallback[1 : len(fallback)-1] {
		fallbackPoints = append(fallbackPoints, n.Point)
	}
	fallbackBad, err := router.pinnedRouteBadness(edge, fallbackPoints)
	if err != nil {
		return nil, 0, err
	}
	if fallbackBad < bad {
		return fallback, fallbackDistance, nil
	}
	return ovgNodes, distance, nil
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
