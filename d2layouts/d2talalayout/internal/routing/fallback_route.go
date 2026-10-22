package routing

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/invariant"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"

	"github.com/d2lang/d2/lib/geo"
)

// A route flavor that uses up its search budget does not fail the layout.
// The edges it has already routed keep their searched routes; every remaining
// edge gets a fallback route built without a visibility-graph search. The
// switch happens at a deterministic point because route-search work is
// counted only in slice order, so the same input always degrades at the same
// edge regardless of how the flavor workers are scheduled.
const (
	// maxRouteFallbackWorkUnits is the extra budget a degraded flavor may spend
	// on fallback routes, their bookkeeping and the final route
	// materialization. It is also the headroom a single route's bookkeeping may
	// overrun the search budget by before the flavor degrades.
	maxRouteFallbackWorkUnits uint64 = 60_000_000

	// fallbackFinalizeReserve is kept free for materializing the selected
	// routes. Below it, fallback routes skip obstacle scoring.
	fallbackFinalizeReserve = maxRouteFallbackWorkUnits / 2

	// fallbackObstacleCost makes one crossed obstacle outweigh any detour.
	fallbackObstacleCost = basicallyInfinity / 100
)

// isRouteSearchExhaustion reports whether err only says that the flavor used
// up its own search budget. Cancellation and every other failure keep their
// established handling.
func isRouteSearchExhaustion(err error) bool {
	return err != nil && errors.Is(err, errRouteSearchWorkLimit) && !isContextTermination(err)
}

// degrade switches the flavor to fallback routes for its remaining edges.
func (router *ovgEdgeRouter) degrade() {
	if router.degraded {
		return
	}
	router.degraded = true
	used := max(router.work.used, router.work.limit)
	router.work.limit = saturatingAddUint64(used, maxRouteFallbackWorkUnits)
}

func saturatingAddUint64(value, units uint64) uint64 {
	sum, ok := limits.CheckedAddUint64(value, units)
	if !ok {
		return math.MaxUint64
	}
	return sum
}

// newFlavorRouter constructs a flavor's router. A search budget too small for
// the router's own setup starts the flavor degraded instead of failing it.
func newFlavorRouter(
	ctx context.Context,
	flavor RouteGenerationFlavor,
	ovg *OVG,
	g *layoutgraph.Graph,
	existingRoutes []*Route,
	edges []*layoutgraph.Edge,
	searchWorkLimit uint64,
) (*ovgEdgeRouter, error) {
	router, err := newOVGEdgeRouterWithWorkLimit(ctx, flavor, ovg, g, existingRoutes, edges, searchWorkLimit)
	if !isRouteSearchExhaustion(err) {
		return router, err
	}
	router, err = newOVGEdgeRouterWithWorkLimit(ctx, flavor, ovg, g, existingRoutes, edges, saturatingAddUint64(searchWorkLimit, maxRouteFallbackWorkUnits))
	if err != nil {
		return nil, err
	}
	router.work.limit = searchWorkLimit
	router.degrade()
	return router, nil
}

// reserveFinalization guarantees the selected flavor at least one fallback
// allowance for materializing its routes, so a flavor whose search used its
// budget exactly up can still be applied. Finalization never decides between
// alternatives; the extra headroom only replaces a failure.
func reserveFinalization(work *routeSearchWorkGuard) {
	if work == nil {
		return
	}
	work.limit = max(work.limit, saturatingAddUint64(work.used, maxRouteFallbackWorkUnits))
}

// addSearchedRoute indexes a searched route. Its bookkeeping may overrun the
// search budget: a partially indexed route would leave map-ordered router
// state behind, so the route is always indexed completely and the flavor
// degrades afterwards when the budget turns out to be used up.
func (router *ovgEdgeRouter) addSearchedRoute(route *Route) error {
	limit := router.work.limit
	router.work.limit = max(limit, saturatingAddUint64(router.work.used, maxRouteFallbackWorkUnits))
	err := router.addRoute(route)
	router.work.limit = limit
	if err != nil {
		return err
	}
	if router.work.used > limit {
		router.degrade()
	}
	return nil
}

// addFallbackRoute records a fallback route. A degraded flavor never searches
// again, so only the state used by later fallback routes and by route
// finalization is maintained: the route lists and the occupied points.
func (router *ovgEdgeRouter) addFallbackRoute(route *Route) error {
	routeNodes := route.OVGNodes
	if route.GEdge.IsLoop() {
		routeNodes = routeNodes[1:]
	}
	seen := make(map[*OVGNode]struct{}, len(routeNodes))
	for i, node := range routeNodes {
		if err := router.work.step(); err != nil {
			return err
		}
		if _, duplicate := seen[node]; duplicate {
			return invariant.Errorf("fallback route %s repeats OVG node %d", route.GEdge.DebugID(), i)
		}
		seen[node] = struct{}{}
	}
	router.routes = append(router.routes, route)
	router.routedEdges = append(router.routedEdges, route.GEdge)
	for _, node := range route.OVGNodes {
		if err := router.work.step(); err != nil {
			return err
		}
		router.pointToRoute.add(node, route)
	}
	router.fallbacks++
	return router.work.check()
}

// fallbackRoute returns an orthogonal route without a visibility-graph
// search: straight, L- or Z-shaped between free ports on the sides facing each
// other, or U-shaped around both shapes, choosing the candidate that crosses
// the fewest unrelated shapes, then the fewest bends, then the shortest
// length. Nested endpoints leave the inner shape straight towards the nearest
// side of its ancestor. Overlapping endpoints, which have no facing sides, use
// the straight center line that the router already uses for overlapping
// shapes.
func (router *ovgEdgeRouter) fallbackRoute(ctx context.Context, edge *layoutgraph.Edge) ([]*OVGNode, float64, error) {
	if err := router.bindWork(ctx); err != nil {
		return nil, 0, err
	}
	source, target := edge.From, edge.To
	sourceCenter, targetCenter := router.ovg.Centers[source], router.ovg.Centers[target]
	if source == nil || target == nil || sourceCenter == nil || targetCenter == nil {
		return nil, 0, invariant.Errorf("fallback route %s has an endpoint outside the OVG", edge.DebugID())
	}
	if edge.HasPinnedSide() {
		// The facing-side candidates below ignore pins, so a pinned edge first
		// tries the pinned fallback the search uses when it finds no path.
		nodes, distance, err := router.pinnedFallbackRoute(edge)
		if err == nil {
			return nodes, distance, nil
		}
		if isContextTermination(err) {
			return nil, 0, err
		}
	}
	if source != target {
		scored := router.work.limit-router.work.used > fallbackFinalizeReserve
		var candidates []fallbackCandidate
		var err error
		if sourceInside, _ := routeSearchIsDescendantOf(unboundedRouteWork{}, source, target); sourceInside {
			candidates, err = router.nestedFallbackCandidates(edge, source, target, false)
		} else if targetInside, _ := routeSearchIsDescendantOf(unboundedRouteWork{}, target, source); targetInside {
			candidates, err = router.nestedFallbackCandidates(edge, target, source, true)
		} else {
			candidates, err = router.facingFallbackCandidates(edge)
		}
		if err != nil {
			return nil, 0, err
		}
		if len(candidates) > 0 {
			best, err := router.bestFallbackCandidate(edge, candidates, scored)
			if err != nil {
				return nil, 0, err
			}
			nodes := make([]*OVGNode, 0, len(best.points)+2)
			nodes = append(nodes, sourceCenter, best.sourcePort)
			for _, point := range best.points[1 : len(best.points)-1] {
				nodes = append(nodes, NewOVGNode(geo.NewPoint(point.X, point.Y)))
			}
			nodes = append(nodes, best.targetPort, targetCenter)
			return nodes, best.distance, nil
		}
	}
	nodes, distance := straightCenterRoute(router.ovg, edge)
	return nodes, distance, nil
}

// straightCenterRoute is the center-to-center line clipped to both shapes,
// used for shapes that overlap and therefore have no facing ports.
func straightCenterRoute(ovg *OVG, edge *layoutgraph.Edge) ([]*OVGNode, float64) {
	fromCenter := ovg.Centers[edge.From]
	toCenter := ovg.Centers[edge.To]
	var fromPort, toPort *geo.Point
	segment := geo.NewSegment(fromCenter.Point, toCenter.Point)
	if intersections := edge.From.Intersections(*segment); len(intersections) > 0 {
		fromPort = intersections[0]
		segment.Start = fromPort
	}
	if intersections := edge.To.Intersections(*segment); len(intersections) > 0 {
		toPort = intersections[0]
	}
	if fromPort == nil {
		fromPort = fromCenter.Point
	}
	if toPort == nil {
		toPort = toCenter.Point
	}
	ovgNodes := []*OVGNode{fromCenter, NewOVGNode(fromPort), NewOVGNode(toPort), toCenter}
	return ovgNodes, geo.EuclideanDistance(fromCenter.X, fromCenter.Y, toCenter.X, toCenter.Y)
}

type fallbackCandidate struct {
	sourcePort, targetPort *OVGNode
	// points runs from the source port to the target port.
	points   []geo.Point
	distance float64
}

var fallbackSides = [...]geo.Orientation{geo.Right, geo.Bottom, geo.Left, geo.Top}

func sideVector(side geo.Orientation) (float64, float64) {
	switch side {
	case geo.Right:
		return 1, 0
	case geo.Left:
		return -1, 0
	case geo.Bottom:
		return 0, 1
	default:
		return 0, -1
	}
}

func isHorizontalSide(side geo.Orientation) bool {
	return side == geo.Left || side == geo.Right
}

// fallbackPort picks the port on one side of node: a free port before an
// occupied one, the center port before others, then the port closest to the
// side's midpoint. Ports keep their OVG order for ties.
func (router *ovgEdgeRouter) fallbackPort(node *layoutgraph.Node, side geo.Orientation, allowed map[*OVGNode]struct{}) (*OVGNode, error) {
	ports := router.ovg.Ports[node]
	if err := router.work.add(uint64(len(ports))); err != nil {
		return nil, err
	}
	midX := node.TopLeft.X + node.Width/2
	midY := node.TopLeft.Y + node.Height/2
	switch side {
	case geo.Right:
		midX = node.TopLeft.X + node.Width
	case geo.Left:
		midX = node.TopLeft.X
	case geo.Bottom:
		midY = node.TopLeft.Y + node.Height
	case geo.Top:
		midY = node.TopLeft.Y
	}
	var best *OVGNode
	bestRank := [3]float64{}
	for _, port := range ports {
		if !port.hasPortDirection(node, side) {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[port]; !ok {
				continue
			}
		}
		occupied := 0.0
		if len(router.pointToRoute.at(port)) > 0 {
			occupied = 1
		}
		center := 1.0
		if port.isCenterPortOf(node) {
			center = 0
		}
		rank := [3]float64{occupied, center, math.Abs(port.X-midX) + math.Abs(port.Y-midY)}
		if best == nil || rank[0] < bestRank[0] ||
			(rank[0] == bestRank[0] && (rank[1] < bestRank[1] || (rank[1] == bestRank[1] && rank[2] < bestRank[2]))) {
			best, bestRank = port, rank
		}
	}
	return best, nil
}

// allowedFallbackPorts restricts table-column edges to their column rows.
func (router *ovgEdgeRouter) allowedFallbackPorts(edge *layoutgraph.Edge) (map[*OVGNode]struct{}, map[*OVGNode]struct{}, error) {
	if !edge.HasTableColumn() {
		return nil, nil, nil
	}
	sourcePorts, targetPorts, err := router.tablePorts(edge, edge.From, edge.To, false)
	if err != nil {
		return nil, nil, err
	}
	if edge.FromTableColumnIndex == nil {
		sourcePorts = nil
	}
	if edge.ToTableColumnIndex == nil {
		targetPorts = nil
	}
	return sourcePorts, targetPorts, nil
}

// facingFallbackCandidates enumerates straight, L, Z and U routes between
// separated shapes. A Z route tries three channels across the gap, a U route
// two distances outside the shapes.
func (router *ovgEdgeRouter) facingFallbackCandidates(edge *layoutgraph.Edge) ([]fallbackCandidate, error) {
	source, target := edge.From, edge.To
	allowedSource, allowedTarget, err := router.allowedFallbackPorts(edge)
	if err != nil {
		return nil, err
	}
	var sourcePorts, targetPorts [len(fallbackSides)]*OVGNode
	for i, side := range fallbackSides {
		if sourcePorts[i], err = router.fallbackPort(source, side, allowedSource); err != nil {
			return nil, err
		}
		if targetPorts[i], err = router.fallbackPort(target, side, allowedTarget); err != nil {
			return nil, err
		}
	}
	sourceBox := fallbackBox(source)
	targetBox := fallbackBox(target)
	candidates := make([]fallbackCandidate, 0, 8)
	for i, sourceSide := range fallbackSides {
		sourcePort := sourcePorts[i]
		if sourcePort == nil {
			continue
		}
		for j, targetSide := range fallbackSides {
			targetPort := targetPorts[j]
			if targetPort == nil {
				continue
			}
			for _, points := range orthogonalFallbackShapes(sourceSide, *sourcePort.Point, sourceBox, targetSide, *targetPort.Point, targetBox) {
				candidates = append(candidates, fallbackCandidate{sourcePort: sourcePort, targetPort: targetPort, points: points})
			}
		}
	}
	return candidates, nil
}

type fallbackRect struct {
	left, top, right, bottom float64
}

func fallbackBox(node *layoutgraph.Node) fallbackRect {
	return fallbackRect{
		left:   node.TopLeft.X,
		top:    node.TopLeft.Y,
		right:  node.TopLeft.X + node.Width,
		bottom: node.TopLeft.Y + node.Height,
	}
}

// orthogonalFallbackShapes returns the valid routes from sourcePort, leaving
// the source through sourceSide, to targetPort, entering the target through
// targetSide. Every route keeps its interior points outside both shapes.
func orthogonalFallbackShapes(
	sourceSide geo.Orientation, sourcePort geo.Point, sourceBox fallbackRect,
	targetSide geo.Orientation, targetPort geo.Point, targetBox fallbackRect,
) [][]geo.Point {
	sdx, sdy := sideVector(sourceSide)
	tdx, tdy := sideVector(targetSide)
	if sourceSide == targetSide {
		return uFallbackShapes(sourceSide, sourcePort, sourceBox, targetPort, targetBox)
	}
	switch {
	case isHorizontalSide(sourceSide) && isHorizontalSide(targetSide):
		if sdx != -tdx {
			return nil
		}
		// The gap between the source side and the facing target side.
		near, far := sourceBox.right, targetBox.left
		if sdx < 0 {
			near, far = sourceBox.left, targetBox.right
		}
		if (far-near)*sdx < 2 || (targetPort.X-sourcePort.X)*sdx <= 0 {
			return nil
		}
		if sourcePort.Y == targetPort.Y {
			return [][]geo.Point{{sourcePort, targetPort}}
		}
		return zChannels(near, far, func(x float64) []geo.Point {
			return []geo.Point{sourcePort, {X: x, Y: sourcePort.Y}, {X: x, Y: targetPort.Y}, targetPort}
		})
	case !isHorizontalSide(sourceSide) && !isHorizontalSide(targetSide):
		if sdy != -tdy {
			return nil
		}
		near, far := sourceBox.bottom, targetBox.top
		if sdy < 0 {
			near, far = sourceBox.top, targetBox.bottom
		}
		if (far-near)*sdy < 2 || (targetPort.Y-sourcePort.Y)*sdy <= 0 {
			return nil
		}
		if sourcePort.X == targetPort.X {
			return [][]geo.Point{{sourcePort, targetPort}}
		}
		return zChannels(near, far, func(y float64) []geo.Point {
			return []geo.Point{sourcePort, {X: sourcePort.X, Y: y}, {X: targetPort.X, Y: y}, targetPort}
		})
	case isHorizontalSide(sourceSide):
		// Leave horizontally, enter vertically.
		corner := geo.Point{X: targetPort.X, Y: sourcePort.Y}
		if (corner.X-sourcePort.X)*sdx <= 0 || (targetPort.Y-corner.Y)*tdy >= 0 {
			return nil
		}
		if !outsideBox(corner, sourceBox) || !outsideBox(corner, targetBox) {
			return nil
		}
		return [][]geo.Point{{sourcePort, corner, targetPort}}
	default:
		// Leave vertically, enter horizontally.
		corner := geo.Point{X: sourcePort.X, Y: targetPort.Y}
		if (corner.Y-sourcePort.Y)*sdy <= 0 || (targetPort.X-corner.X)*tdx >= 0 {
			return nil
		}
		if !outsideBox(corner, sourceBox) || !outsideBox(corner, targetBox) {
			return nil
		}
		return [][]geo.Point{{sourcePort, corner, targetPort}}
	}
}

// uFallbackShapes leaves and enters through the same side, around the outside
// of both shapes, for example over the top of a shape that blocks the direct
// line between them.
func uFallbackShapes(side geo.Orientation, sourcePort geo.Point, sourceBox fallbackRect, targetPort geo.Point, targetBox fallbackRect) [][]geo.Point {
	shapes := make([][]geo.Point, 0, 2)
	for _, offset := range [...]float64{ovgPadding, 3 * ovgPadding} {
		var first, second geo.Point
		switch side {
		case geo.Top:
			y := math.Min(sourceBox.top, targetBox.top) - offset
			first, second = geo.Point{X: sourcePort.X, Y: y}, geo.Point{X: targetPort.X, Y: y}
		case geo.Bottom:
			y := math.Max(sourceBox.bottom, targetBox.bottom) + offset
			first, second = geo.Point{X: sourcePort.X, Y: y}, geo.Point{X: targetPort.X, Y: y}
		case geo.Left:
			x := math.Min(sourceBox.left, targetBox.left) - offset
			first, second = geo.Point{X: x, Y: sourcePort.Y}, geo.Point{X: x, Y: targetPort.Y}
		default:
			x := math.Max(sourceBox.right, targetBox.right) + offset
			first, second = geo.Point{X: x, Y: sourcePort.Y}, geo.Point{X: x, Y: targetPort.Y}
		}
		if first == second {
			continue
		}
		shapes = append(shapes, []geo.Point{sourcePort, first, second, targetPort})
	}
	return shapes
}

func outsideBox(point geo.Point, box fallbackRect) bool {
	return point.X < box.left || point.X > box.right || point.Y < box.top || point.Y > box.bottom
}

// zChannels places the middle segment of a Z route at the middle, the first
// and the last quarter of the gap between near and far.
func zChannels(near, far float64, build func(float64) []geo.Point) [][]geo.Point {
	shapes := make([][]geo.Point, 0, 3)
	var previous []float64
	for _, fraction := range [...]float64{0.5, 0.25, 0.75} {
		channel := math.Round(near + (far-near)*fraction)
		if (channel-near)*(far-near) <= 0 || (far-channel)*(far-near) <= 0 {
			continue
		}
		duplicate := false
		for _, other := range previous {
			duplicate = duplicate || other == channel
		}
		if duplicate {
			continue
		}
		previous = append(previous, channel)
		shapes = append(shapes, build(channel))
	}
	return shapes
}

// nestedFallbackCandidates routes between a shape and one of its ancestors:
// straight out of each side of the inner shape to the facing side of the
// outer one. innerIsTarget orients the points from source to target.
func (router *ovgEdgeRouter) nestedFallbackCandidates(edge *layoutgraph.Edge, inner, outer *layoutgraph.Node, innerIsTarget bool) ([]fallbackCandidate, error) {
	allowedSource, allowedTarget, err := router.allowedFallbackPorts(edge)
	if err != nil {
		return nil, err
	}
	allowedInner := allowedSource
	if innerIsTarget {
		allowedInner = allowedTarget
	}
	outerBox := fallbackBox(outer)
	candidates := make([]fallbackCandidate, 0, len(fallbackSides))
	for _, side := range fallbackSides {
		port, err := router.fallbackPort(inner, side, allowedInner)
		if err != nil {
			return nil, err
		}
		if port == nil {
			continue
		}
		end := *port.Point
		switch side {
		case geo.Right:
			end.X = outerBox.right
		case geo.Left:
			end.X = outerBox.left
		case geo.Bottom:
			end.Y = outerBox.bottom
		case geo.Top:
			end.Y = outerBox.top
		}
		if end == *port.Point {
			continue
		}
		border := NewOVGNode(geo.NewPoint(end.X, end.Y))
		candidate := fallbackCandidate{sourcePort: port, targetPort: border, points: []geo.Point{*port.Point, end}}
		if innerIsTarget {
			candidate = fallbackCandidate{sourcePort: border, targetPort: port, points: []geo.Point{end, *port.Point}}
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// bestFallbackCandidate scores candidates in enumeration order. Without
// scoring, the first candidate is used: it still leaves and enters through
// facing sides, but obstacles are not checked.
func (router *ovgEdgeRouter) bestFallbackCandidate(edge *layoutgraph.Edge, candidates []fallbackCandidate, scored bool) (fallbackCandidate, error) {
	if !scored {
		best := candidates[0]
		best.distance = fallbackLength(best.points) + float64(len(best.points)-2)*router.turnCost
		return best, nil
	}
	obstacles := router.ovg.NodesInsideBoundingBox
	if len(obstacles) == 0 {
		obstacles = router.graph.Nodes
	}
	related := make(map[*layoutgraph.Node]struct{}, 8)
	for _, endpoint := range [...]*layoutgraph.Node{edge.From, edge.To} {
		for current := endpoint; current != nil; {
			if err := router.work.step(); err != nil {
				return fallbackCandidate{}, err
			}
			related[current] = struct{}{}
			switch {
			case current.Container != nil:
				current = current.Container
			case current.Cluster != nil:
				current = current.Cluster.Vessel
			case current.Sequence != nil:
				current = current.Sequence.Vessel
			default:
				current = nil
			}
		}
	}
	bestIndex := -1
	bestCost := math.Inf(1)
	var bestDistance float64
	for index := range candidates {
		points := candidates[index].points
		if err := router.work.reserveProduct(uint64(len(points)), uint64(len(obstacles))+1); err != nil {
			return fallbackCandidate{}, err
		}
		hits := 0
		for _, obstacle := range obstacles {
			if obstacle == nil || obstacle.TopLeft == nil {
				continue
			}
			if _, skip := related[obstacle]; skip {
				continue
			}
			for i := 1; i < len(points); i++ {
				if segmentIntersectsBox(&points[i-1], &points[i], &obstacle.Box) {
					hits++
					break
				}
			}
		}
		length := fallbackLength(points)
		bends := float64(len(points) - 2)
		cost := float64(hits)*fallbackObstacleCost + bends*router.turnCost + length
		if cost < bestCost {
			bestIndex, bestCost = index, cost
			bestDistance = length + bends*router.turnCost + float64(hits)*router.crossingCost
		}
	}
	if bestIndex < 0 {
		return fallbackCandidate{}, fmt.Errorf("fallback route %s has no finite candidate", edge.DebugID())
	}
	best := candidates[bestIndex]
	best.distance = bestDistance
	return best, nil
}

func fallbackLength(points []geo.Point) float64 {
	length := 0.0
	for i := 1; i < len(points); i++ {
		length += math.Abs(points[i].X-points[i-1].X) + math.Abs(points[i].Y-points[i-1].Y)
	}
	return length
}

// unboundedRouteWork answers structural questions that are bounded by the
// containment depth and must not fail once a flavor has degraded.
type unboundedRouteWork struct{}

func (unboundedRouteWork) step() error      { return nil }
func (unboundedRouteWork) add(uint64) error { return nil }
func (unboundedRouteWork) check() error     { return nil }

// isOVGExhaustion reports whether building the visibility graph stopped at one
// of its own size or work limits rather than on cancellation or an invariant.
func isOVGExhaustion(err error) bool {
	return err != nil && errors.Is(err, errOVGResourceLimit) && !isContextTermination(err)
}

// buildPortOVG is the routing graph used when the full visibility graph is
// too large to build: only the ports and centers of every shape, which is all
// fallback routes need. Its size is linear in the number of shapes.
func buildPortOVG(ctx context.Context, g *layoutgraph.Graph, nearbyNodes []*layoutgraph.Node) (*OVG, error) {
	guard, err := newOVGBuildGuard(ctx, defaultOVGBuildLimits())
	if err != nil {
		return nil, err
	}
	capacity, err := checkedOVGSliceCapacity(len(nearbyNodes), len(g.Nodes))
	if err != nil {
		return nil, err
	}
	nodes := make([]*layoutgraph.Node, 0, capacity)
	nodes = append(nodes, nearbyNodes...)
	nodes = append(nodes, g.Nodes...)
	ovg := newBuildOVG(nodes, guard)
	if err := ovg.addPorts(g, guard); err != nil {
		return nil, err
	}
	if err := ovg.connectPortsToCenter(guard); err != nil {
		return nil, err
	}
	for i, node := range ovg.Nodes {
		if err := guard.step(); err != nil {
			return nil, err
		}
		node.Index = i
	}
	return ovg, nil
}

// buildRoutingOVG builds the visibility graph, or the port-only graph when the
// visibility graph exceeds its limits. portsOnly tells the caller to route
// every edge with fallback routes.
func buildRoutingOVG(ctx context.Context, g *layoutgraph.Graph, nearbyNodes []*layoutgraph.Node, build func() (*OVG, error)) (ovg *OVG, portsOnly bool, err error) {
	ovg, err = build()
	if !isOVGExhaustion(err) {
		return ovg, false, err
	}
	ovg, err = buildPortOVG(ctx, g, nearbyNodes)
	return ovg, true, err
}

// optionalStageResult lets an optional, atomic route post-processing stage
// that used up its own work budget count as a stage that changed nothing: the
// atomic stage has already restored every route it touched, so the drawing it
// received is still complete. NudgeEdgeChannels and ShortcutEdgeRoutes have
// always behaved this way.
func optionalStageResult(err error) error {
	if errors.Is(err, errRouteStageWorkLimit) && !isContextTermination(err) {
		return nil
	}
	return err
}

// RouteDiagnostics reports how much of a graph-routing pass degraded. A pass
// fills it when GraphRouteOptions.Diagnostics is set.
type RouteDiagnostics struct {
	// FallbackRoutes counts selected routes that were built without a
	// visibility-graph search after their flavor used up its search budget.
	FallbackRoutes int
	// PortGraphs counts subgraphs routed on the port-only graph because their
	// visibility graph exceeded its limits.
	PortGraphs int
}

func (diagnostics *RouteDiagnostics) record(fallbacks int, portsOnly bool) {
	if diagnostics == nil {
		return
	}
	diagnostics.FallbackRoutes += fallbacks
	if portsOnly {
		diagnostics.PortGraphs++
	}
}
