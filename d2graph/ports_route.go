package d2graph

import (
	"container/heap"
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/lib/geo"
)

// RouteObstacles returns the objects a route of e must stay out of: every
// positioned object except e's endpoints and their ancestors.
func (e *Edge) RouteObstacles() []*Object {
	if e.Src.Graph == nil {
		return nil
	}
	var obstacles []*Object
	for _, obj := range e.Src.Graph.Objects {
		if obj == e.Src || obj == e.Dst || obj.Box == nil || obj.TopLeft == nil ||
			e.Src.IsDescendantOf(obj) || e.Dst.IsDescendantOf(obj) {
			continue
		}
		obstacles = append(obstacles, obj)
	}
	return obstacles
}

// pinnedRouteBlocked reports whether route, a polyline from e.Src to e.Dst,
// crosses an object other than its endpoints and their ancestors, or, past
// its first (last) segment, runs back through Src (Dst). It is the check
// Edge.RouteClear makes, on the polyline before it is turned into a curve.
func (e *Edge) pinnedRouteBlocked(route []*geo.Point, obstacles []*Object) bool {
	n := len(route)
	for i := 1; i < n; i++ {
		a, b := route[i-1], route[i]
		for _, obj := range obstacles {
			if segmentCrossesBox(obj.Box, a, b) {
				return true
			}
		}
		if i != 1 && e.Src.Box != nil && !e.Dst.IsDescendantOf(e.Src) && segmentCrossesObject(e.Src, a, b) {
			return true
		}
		if i != n-1 && e.Dst.Box != nil && !e.Src.IsDescendantOf(e.Dst) && segmentCrossesObject(e.Dst, a, b) {
			return true
		}
	}
	return false
}

// pinnedEnd is one end of a route for routeAroundObstacles: it starts at
// anchor on obj's border and runs straight out along side's normal to stub,
// just outside obj's bounding box.
type pinnedEnd struct {
	obj    *Object
	side   geo.Orientation
	anchor *geo.Point
	stub   *geo.Point
}

func newPinnedEnd(obj *Object, side geo.Orientation, anchor *geo.Point) pinnedEnd {
	n := PortSideNormal(side)
	// How far anchor is inside the side of the bounding box (non-rectangular
	// shapes put it on their outline).
	var inset float64
	switch side {
	case geo.Top:
		inset = anchor.Y - obj.TopLeft.Y
	case geo.Bottom:
		inset = obj.TopLeft.Y + obj.Height - anchor.Y
	case geo.Left:
		inset = anchor.X - obj.TopLeft.X
	default:
		inset = obj.TopLeft.X + obj.Width - anchor.X
	}
	l := math.Max(0, inset) + PortStubLen
	return pinnedEnd{
		obj:    obj,
		side:   side,
		anchor: anchor.Copy(),
		stub:   geo.NewPoint(anchor.X+n[0]*l, anchor.Y+n[1]*l),
	}
}

// facingSide returns the side of obj's bounding box nearest to p, a point on
// its border, for an unpinned end that has to be rerouted.
func facingSide(obj *Object, p *geo.Point) geo.Orientation {
	c := obj.Center()
	dx := (p.X - c.X) / math.Max(obj.Width, 1)
	dy := (p.Y - c.Y) / math.Max(obj.Height, 1)
	if math.Abs(dx) >= math.Abs(dy) {
		if dx < 0 {
			return geo.Left
		}
		return geo.Right
	}
	if dy < 0 {
		return geo.Top
	}
	return geo.Bottom
}

// rerouteBlocked replaces route, the polyline PinRoute built, when it runs
// through a shape: both ends keep their anchor (an unpinned end keeps where
// the layout engine attached it, leaving through the side it is on) and are
// joined by the orthogonal path around every shape in the way with the
// fewest bends, then the shortest. It returns route unchanged when route is
// clear or when no clear path exists.
func (e *Edge) rerouteBlocked(route []*geo.Point, srcSide geo.Orientation, srcOK bool, dstSide geo.Orientation, dstOK bool) []*geo.Point {
	if len(route) < 2 || e.Src == e.Dst || e.Src.TopLeft == nil || e.Dst.TopLeft == nil ||
		e.Src.IsDescendantOf(e.Dst) || e.Dst.IsDescendantOf(e.Src) {
		return route
	}
	obstacles := e.RouteObstacles()
	if !e.pinnedRouteBlocked(route, obstacles) {
		return route
	}
	first, last := route[0], route[len(route)-1]
	if !srcOK {
		srcSide = facingSide(e.Src, first)
	}
	if !dstOK {
		dstSide = facingSide(e.Dst, last)
	}
	src := newPinnedEnd(e.Src, srcSide, first)
	dst := newPinnedEnd(e.Dst, dstSide, last)
	for _, margin := range []float64{4 * PortStubLen, math.Inf(1)} {
		path := routeAroundObstacles(src, dst, obstacles, margin)
		if path == nil {
			continue
		}
		out := append([]*geo.Point{src.anchor}, path...)
		out = append(out, dst.anchor)
		out = slices.CompactFunc(out, func(p, q *geo.Point) bool { return *p == *q })
		if len(out) >= 2 && !e.pinnedRouteBlocked(out, obstacles) {
			return out
		}
	}
	return route
}

// maxRouteObstacles bounds the shapes routeAroundObstacles plans around, so
// that a huge diagram cannot make one connection expensive. Past it the
// blocked route is kept.
const maxRouteObstacles = 250

// routeAroundObstacles finds an orthogonal path from src.stub to dst.stub
// that crosses neither the obstacles nor src's and dst's shapes. It searches
// the grid of lines PortClearance outside every shape near the two ends
// (within margin of them), the lines halfway between neighbouring shapes, and
// the lines through the stubs. The path leaves src.stub without turning back
// onto its stub and arrives at dst.stub without running past it and back.
// Bends cost as much as 2*PortStubLen of length. It returns nil when there
// is no such path.
func routeAroundObstacles(src, dst pinnedEnd, obstacles []*Object, margin float64) []*geo.Point {
	region := geo.NewBox(geo.NewPoint(
		math.Min(math.Min(src.obj.TopLeft.X, dst.obj.TopLeft.X), math.Min(src.stub.X, dst.stub.X))-margin,
		math.Min(math.Min(src.obj.TopLeft.Y, dst.obj.TopLeft.Y), math.Min(src.stub.Y, dst.stub.Y))-margin,
	), 0, 0)
	region.Width = math.Max(math.Max(src.obj.TopLeft.X+src.obj.Width, dst.obj.TopLeft.X+dst.obj.Width), math.Max(src.stub.X, dst.stub.X)) + margin - region.TopLeft.X
	region.Height = math.Max(math.Max(src.obj.TopLeft.Y+src.obj.Height, dst.obj.TopLeft.Y+dst.obj.Height), math.Max(src.stub.Y, dst.stub.Y)) + margin - region.TopLeft.Y
	if math.IsInf(margin, 1) {
		region = nil
	}

	boxes := []*geo.Box{src.obj.Box, dst.obj.Box}
	for _, obj := range obstacles {
		if region == nil || region.Overlaps(*obj.Box) {
			boxes = append(boxes, obj.Box)
		}
	}
	if len(boxes) > maxRouteObstacles {
		return nil
	}

	c := float64(PortClearance)
	var xEdges, yEdges []float64
	xs := []float64{src.stub.X, dst.stub.X}
	ys := []float64{src.stub.Y, dst.stub.Y}
	for _, b := range boxes {
		l, r, t, bt := b.TopLeft.X, b.TopLeft.X+b.Width, b.TopLeft.Y, b.TopLeft.Y+b.Height
		xEdges = append(xEdges, l, r)
		yEdges = append(yEdges, t, bt)
		xs = append(xs, l-c, r+c)
		ys = append(ys, t-c, bt+c)
	}
	midlines := func(edges []float64) []float64 {
		sort.Float64s(edges)
		var mids []float64
		for i := 1; i < len(edges); i++ {
			if edges[i]-edges[i-1] > 2 {
				mids = append(mids, (edges[i]+edges[i-1])/2)
			}
		}
		return mids
	}
	xs = uniqueCoords(append(xs, midlines(xEdges)...))
	ys = uniqueCoords(append(ys, midlines(yEdges)...))

	// Every shape the grid's segments could run into, near the ends or not.
	extent := geo.NewBox(geo.NewPoint(xs[0], ys[0]), xs[len(xs)-1]-xs[0], ys[len(ys)-1]-ys[0])
	blockers := []*geo.Box{src.obj.Box, dst.obj.Box}
	for _, obj := range obstacles {
		if extent.Overlaps(*obj.Box) {
			blockers = append(blockers, obj.Box)
		}
	}

	si, sj := slices.Index(xs, src.stub.X), slices.Index(ys, src.stub.Y)
	ti, tj := slices.Index(xs, dst.stub.X), slices.Index(ys, dst.stub.Y)
	if si < 0 || sj < 0 || ti < 0 || tj < 0 {
		return nil
	}
	nx, ny := len(xs), len(ys)
	node := func(i, j int) int { return j*nx + i }
	point := func(id int) *geo.Point { return geo.NewPoint(xs[id%nx], ys[id/nx]) }

	// Directions: 0 right, 1 down, 2 left, 3 up.
	dirs := [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	dirOf := func(side geo.Orientation) int {
		switch side {
		case geo.Right:
			return 0
		case geo.Bottom:
			return 1
		case geo.Left:
			return 2
		}
		return 3
	}
	startDir := dirOf(src.side)
	dstOut := dirOf(dst.side) // arriving at dst.stub moving this way doubles back
	inDir := (dstOut + 2) % 4 // the way the final stub runs into dst

	// open[id*4+d] caches whether the step from node id in direction d is
	// clear: 1 clear, 2 blocked.
	open := make([]uint8, nx*ny*4)
	stepClear := func(id, d int) (int, bool) {
		i, j := id%nx+dirs[d][0], id/nx+dirs[d][1]
		if i < 0 || i >= nx || j < 0 || j >= ny {
			return 0, false
		}
		next := node(i, j)
		k := id*4 + d
		if open[k] == 0 {
			open[k] = 1
			a, b := point(id), point(next)
			for _, box := range blockers {
				if segmentCrossesBox(box, a, b) {
					open[k] = 2
					break
				}
			}
		}
		return next, open[k] == 1
	}

	bendCost := 2. * PortStubLen
	start, goal := node(si, sj), node(ti, tj)
	dist := make([]float64, nx*ny*4)
	prev := make([]int, nx*ny*4)
	for i := range dist {
		dist[i] = math.Inf(1)
		prev[i] = -1
	}
	pq := &routeQueue{}
	s0 := start*4 + startDir
	dist[s0] = 0
	heap.Push(pq, routeItem{state: s0})
	best, bestCost := -1, math.Inf(1)
	for pq.Len() > 0 {
		it := heap.Pop(pq).(routeItem)
		if it.cost > dist[it.state] || it.cost >= bestCost {
			continue
		}
		id, d := it.state/4, it.state%4
		if id == goal && d != dstOut {
			cost := it.cost
			if d != inDir {
				cost += bendCost
			}
			if cost < bestCost {
				best, bestCost = it.state, cost
			}
			continue
		}
		for nd := 0; nd < 4; nd++ {
			if nd == (d+2)%4 {
				continue
			}
			next, ok := stepClear(id, nd)
			if !ok {
				continue
			}
			a, b := point(id), point(next)
			cost := it.cost + math.Abs(a.X-b.X) + math.Abs(a.Y-b.Y)
			if nd != d {
				cost += bendCost
			}
			ns := next*4 + nd
			if cost < dist[ns] {
				dist[ns] = cost
				prev[ns] = it.state
				heap.Push(pq, routeItem{state: ns, cost: cost})
			}
		}
	}
	if best < 0 {
		return nil
	}
	var states []int
	for s := best; s >= 0; s = prev[s] {
		states = append(states, s)
	}
	slices.Reverse(states)
	// Keep the stubs and the corners.
	path := []*geo.Point{point(states[0] / 4)}
	for k := 1; k < len(states); k++ {
		if k == len(states)-1 || states[k]%4 != states[k+1]%4 {
			path = append(path, point(states[k]/4))
		}
	}
	return path
}

func uniqueCoords(vs []float64) []float64 {
	sort.Float64s(vs)
	return slices.CompactFunc(vs, func(a, b float64) bool { return a == b })
}

type routeItem struct {
	state int
	cost  float64
}

type routeQueue []routeItem

func (q routeQueue) Len() int           { return len(q) }
func (q routeQueue) Less(i, j int) bool { return q[i].cost < q[j].cost }
func (q routeQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *routeQueue) Push(x any)        { *q = append(*q, x.(routeItem)) }
func (q *routeQueue) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}
