package d2graph

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// PortSideOrientation maps a source-port / target-port value ("top", "right",
// "bottom" or "left", any case) to the side it names.
func PortSideOrientation(side string) (geo.Orientation, bool) {
	switch strings.ToLower(side) {
	case "top":
		return geo.Top, true
	case "right":
		return geo.Right, true
	case "bottom":
		return geo.Bottom, true
	case "left":
		return geo.Left, true
	}
	return geo.NONE, false
}

// SrcPortSide reports the side of Src that the connection is pinned to with
// source-port.
func (e *Edge) SrcPortSide() (geo.Orientation, bool) {
	if e.SrcPort == nil {
		return geo.NONE, false
	}
	return PortSideOrientation(e.SrcPort.Value)
}

// DstPortSide reports the side of Dst that the connection is pinned to with
// target-port.
func (e *Edge) DstPortSide() (geo.Orientation, bool) {
	if e.DstPort == nil {
		return geo.NONE, false
	}
	return PortSideOrientation(e.DstPort.Value)
}

// HasPort reports whether either end of the connection is pinned to a side.
func (e *Edge) HasPort() bool {
	_, src := e.SrcPortSide()
	_, dst := e.DstPortSide()
	return src || dst
}

// PortSideNormal is the unit vector pointing out of a shape through side.
func PortSideNormal(side geo.Orientation) geo.Vector {
	switch side {
	case geo.Top:
		return geo.Vector{0, -1}
	case geo.Bottom:
		return geo.Vector{0, 1}
	case geo.Left:
		return geo.Vector{-1, 0}
	default:
		return geo.Vector{1, 0}
	}
}

// PortAnchor returns where a connection pinned to side meets the border of
// obj. along is the position on that side, from 0 (the left end of top and
// bottom, the top end of left and right) to 1; 0.5 is the middle.
//
// The side is the one of the shape's bounding box. For shapes that are not
// rectangles (circles, diamonds, clouds, ...) the anchor is moved inward,
// along the side's normal, onto the shape's own outline.
func (obj *Object) PortAnchor(side geo.Orientation, along float64) *geo.Point {
	along = math.Max(0, math.Min(1, along))
	tl := obj.TopLeft
	var p *geo.Point
	switch side {
	case geo.Top:
		p = geo.NewPoint(tl.X+obj.Width*along, tl.Y)
	case geo.Bottom:
		p = geo.NewPoint(tl.X+obj.Width*along, tl.Y+obj.Height)
	case geo.Left:
		p = geo.NewPoint(tl.X, tl.Y+obj.Height*along)
	default:
		p = geo.NewPoint(tl.X+obj.Width, tl.Y+obj.Height*along)
	}
	s := obj.ToShape()
	if s.Is("") || s.IsRectangular() {
		return p
	}
	// TraceToShapeBorder extends the segment from outside through the box and
	// keeps the outline crossing nearest to the box border.
	n := PortSideNormal(side)
	outside := geo.NewPoint(p.X+n[0]*obj.Width, p.Y+n[1]*obj.Height)
	return shape.TraceToShapeBorder(s, p, outside)
}

// PortsHonored reports whether e.Route honours source-port and target-port:
// a pinned end must lie on its side's half of the shape, and the route's first
// (or last) segment must run straight out of that side. It returns nil when
// nothing is pinned.
func (e *Edge) PortsHonored() error {
	if len(e.Route) < 2 {
		if e.HasPort() {
			return fmt.Errorf("%s: route has %d points", e.AbsID(), len(e.Route))
		}
		return nil
	}
	if side, ok := e.SrcPortSide(); ok {
		if err := portEndHonored(e.Src, side, e.Route[0], e.Route[1]); err != nil {
			return fmt.Errorf("%s: source-port %s: %w", e.AbsID(), e.SrcPort.Value, err)
		}
	}
	if side, ok := e.DstPortSide(); ok {
		n := len(e.Route)
		if err := portEndHonored(e.Dst, side, e.Route[n-1], e.Route[n-2]); err != nil {
			return fmt.Errorf("%s: target-port %s: %w", e.AbsID(), e.DstPort.Value, err)
		}
	}
	return nil
}

// RouteClear reports the first place where e's route runs through a shape:
// through the interior of any object other than its endpoints and their
// ancestors, or, after leaving an endpoint, back through it. The first and
// last segments must also head away from their shapes. Layout tests use it
// next to PortsHonored, which checks only the ends.
func (e *Edge) RouteClear() error {
	if len(e.Route) < 2 || e.Src.Graph == nil {
		return nil
	}
	n := len(e.Route)
	ends := []struct {
		obj, other *Object
		end, next  *geo.Point
		skip       int // the segment index that touches this end
	}{
		{e.Src, e.Dst, e.Route[0], e.Route[1], 1},
		{e.Dst, e.Src, e.Route[n-1], e.Route[n-2], n - 1},
	}
	for _, end := range ends {
		if end.obj.TopLeft == nil || (end.obj != end.other && end.other.IsDescendantOf(end.obj)) {
			continue
		}
		c := end.obj.Center()
		if (end.next.X-end.end.X)*(end.end.X-c.X)+(end.next.Y-end.end.Y)*(end.end.Y-c.Y) <= 0 {
			return fmt.Errorf("%s: segment %v -> %v heads into %s", e.AbsID(), end.end, end.next, end.obj.AbsID())
		}
		for i := 1; i < n; i++ {
			if i == end.skip {
				continue
			}
			if segmentCrossesBox(end.obj.Box, e.Route[i-1], e.Route[i]) {
				return fmt.Errorf("%s: segment %v -> %v runs back through %s", e.AbsID(), e.Route[i-1], e.Route[i], end.obj.AbsID())
			}
		}
	}
	for _, obj := range e.Src.Graph.Objects {
		if obj == e.Src || obj == e.Dst || obj.TopLeft == nil ||
			e.Src.IsDescendantOf(obj) || e.Dst.IsDescendantOf(obj) {
			continue
		}
		for i := 1; i < n; i++ {
			if segmentCrossesBox(obj.Box, e.Route[i-1], e.Route[i]) {
				return fmt.Errorf("%s: segment %v -> %v crosses %s", e.AbsID(), e.Route[i-1], e.Route[i], obj.AbsID())
			}
		}
	}
	return nil
}

func portEndHonored(obj *Object, side geo.Orientation, end, next *geo.Point) error {
	const eps = 1.
	if obj.TopLeft == nil {
		return fmt.Errorf("%s has no position", obj.AbsID())
	}
	if end.X < obj.TopLeft.X-eps || end.X > obj.TopLeft.X+obj.Width+eps ||
		end.Y < obj.TopLeft.Y-eps || end.Y > obj.TopLeft.Y+obj.Height+eps {
		return fmt.Errorf("end %v is off %s", end, obj.AbsID())
	}
	n := PortSideNormal(side)
	center := obj.Center()
	if (end.X-center.X)*n[0]+(end.Y-center.Y)*n[1] <= 0 {
		return fmt.Errorf("end %v is not on that side of %s", end, obj.AbsID())
	}
	dx, dy := next.X-end.X, next.Y-end.Y
	along := dx*n[0] + dy*n[1]
	across := math.Abs(dx*n[1] - dy*n[0])
	if along <= 0 || across > eps {
		return fmt.Errorf("segment %v -> %v does not leave %s straight out of that side", end, next, obj.AbsID())
	}
	return nil
}

// PortEnd names one end of a connection: its source end when Src is true.
type PortEnd struct {
	Edge *Edge
	Src  bool
}

func (end PortEnd) obj() *Object {
	if end.Src {
		return end.Edge.Src
	}
	return end.Edge.Dst
}

func (end PortEnd) other() *Object {
	if end.Src {
		return end.Edge.Dst
	}
	return end.Edge.Src
}

func (end PortEnd) side() (geo.Orientation, bool) {
	if end.Src {
		return end.Edge.SrcPortSide()
	}
	return end.Edge.DstPortSide()
}

// PortAnchors places the pinned ends of edges for layout engines that have
// no port support of their own. The ends pinned to one side of an object are
// spread over the middle half of that side, ordered by where their other
// endpoint is to limit crossings; a lone end sits in the middle. Self-loops
// and connections between an object and its own descendant are left out.
func PortAnchors(edges []*Edge) map[PortEnd]*geo.Point {
	type sideKey struct {
		obj  *Object
		side geo.Orientation
	}
	groups := map[sideKey][]PortEnd{}
	var order []sideKey
	for _, e := range edges {
		if e.Src == e.Dst || e.Src.IsDescendantOf(e.Dst) || e.Dst.IsDescendantOf(e.Src) {
			continue
		}
		for _, end := range []PortEnd{{e, true}, {e, false}} {
			side, ok := end.side()
			if !ok {
				continue
			}
			k := sideKey{end.obj(), side}
			if _, seen := groups[k]; !seen {
				order = append(order, k)
			}
			groups[k] = append(groups[k], end)
		}
	}
	anchors := make(map[PortEnd]*geo.Point)
	for _, k := range order {
		ends := groups[k]
		along := func(end PortEnd) float64 {
			c := end.other().Center()
			if k.side == geo.Top || k.side == geo.Bottom {
				return c.X
			}
			return c.Y
		}
		sort.SliceStable(ends, func(i, j int) bool { return along(ends[i]) < along(ends[j]) })
		for i, end := range ends {
			frac := 0.5
			if len(ends) > 1 {
				frac = 0.25 + 0.5*float64(i)/float64(len(ends)-1)
			}
			anchors[end] = k.obj.PortAnchor(k.side, frac)
		}
	}
	return anchors
}

// PortStubLen is how far a route from PinRoute runs straight out of its side
// before it may turn, and PortClearance how far its detours keep from the
// shape.
const (
	PortStubLen   = 3 * MIN_SEGMENT_LEN
	PortClearance = 2 * MIN_SEGMENT_LEN
)

// PinRoute rewrites route, a polyline from Src to Dst, so that its pinned
// ends start at srcAnchor / end at dstAnchor (from PortAnchors; nil leaves
// that end alone) and run straight out of their side for a short stub.
// Interior points behind a stub were planned for a different attachment and
// are dropped; when the next leg would cut through the shape, the route goes
// around its nearer corner instead. It is a best-effort fallback for engines
// without port support.
func (e *Edge) PinRoute(route []*geo.Point, srcAnchor, dstAnchor *geo.Point) []*geo.Point {
	if len(route) < 2 || (srcAnchor == nil && dstAnchor == nil) {
		return route
	}
	srcSide, srcOK := e.SrcPortSide()
	dstSide, dstOK := e.DstPortSide()
	srcOK = srcOK && srcAnchor != nil
	dstOK = dstOK && dstAnchor != nil

	srcStub, dstStub := float64(PortStubLen), float64(PortStubLen)
	if srcOK && dstOK {
		srcStub, dstStub = facingStubLens(srcAnchor, srcSide, dstAnchor, dstSide)
	}

	interior := append([]*geo.Point(nil), route[1:len(route)-1]...)
	head := []*geo.Point{route[0]}
	tail := []*geo.Point{route[len(route)-1]}
	if srcOK {
		stub := portStub(srcAnchor, srcSide, srcStub)
		head = []*geo.Point{srcAnchor.Copy(), stub}
		for len(interior) > 0 && behind(interior[0], stub, srcSide) {
			interior = interior[1:]
		}
	}
	if dstOK {
		stub := portStub(dstAnchor, dstSide, dstStub)
		tail = []*geo.Point{stub, dstAnchor.Copy()}
		for len(interior) > 0 && behind(interior[len(interior)-1], stub, dstSide) {
			interior = interior[:len(interior)-1]
		}
	}

	if srcOK {
		next := tail[0]
		if len(interior) > 0 {
			next = interior[0]
		}
		head = append(head, portDetour(e.Src, srcSide, head[1], next)...)
	}
	if dstOK {
		prev := head[len(head)-1]
		if len(interior) > 0 {
			prev = interior[len(interior)-1]
		}
		detour := portDetour(e.Dst, dstSide, tail[0], prev)
		slices.Reverse(detour)
		tail = append(detour, tail...)
	}

	out := make([]*geo.Point, 0, len(head)+len(interior)+len(tail)+1)
	out = append(out, head...)
	out = append(out, interior...)
	if srcOK && dstOK && len(interior) == 0 {
		// Join the two stubs square, like the rest of a pinned route, rather
		// than with one long diagonal.
		out = append(out, portConnector(e, head[len(head)-2], head[len(head)-1], tail[0], tail[1])...)
	}
	out = append(out, tail...)
	// Stubs that meet (see facingStubLens) leave the same point twice.
	return slices.CompactFunc(out, func(p, q *geo.Point) bool { return *p == *q })
}

// facingStubLens returns the stub lengths for two pinned ends. When the
// sides face each other across a gap too narrow for two full stubs (adjacent
// grid cells, say), each stub takes half the gap so that they meet instead of
// overlapping and the route doubling back on itself.
func facingStubLens(src *geo.Point, srcSide geo.Orientation, dst *geo.Point, dstSide geo.Orientation) (float64, float64) {
	n, m := PortSideNormal(srcSide), PortSideNormal(dstSide)
	l := float64(PortStubLen)
	if n[0] != -m[0] || n[1] != -m[1] {
		return l, l
	}
	gap := (dst.X-src.X)*n[0] + (dst.Y-src.Y)*n[1]
	if gap <= 0 || gap >= 2*l {
		return l, l
	}
	return gap / 2, gap / 2
}

// portConnector returns the corners to insert between h (reached from prev)
// and t (left towards next), the ends of two pinned stubs: an L, a Z through
// the midline, or none (a diagonal). Candidates that cut through either endpoint are
// skipped; of the rest, the one crossing the fewest other shapes wins, then
// the one with the fewest bends, then the L that continues the route's
// direction. A candidate whose first leg turns back over the source stub, or
// whose last leg runs out past the target stub and back, draws a hairpin and
// is only taken when nothing else fits.
func portConnector(e *Edge, prev, h, t, next *geo.Point) []*geo.Point {
	if h.X == t.X || h.Y == t.Y {
		return nil
	}
	mx, my := (h.X+t.X)/2, (h.Y+t.Y)/2
	horizontalFirst := []*geo.Point{geo.NewPoint(t.X, h.Y)}
	verticalFirst := []*geo.Point{geo.NewPoint(h.X, t.Y)}
	if prev.X == h.X {
		horizontalFirst, verticalFirst = verticalFirst, horizontalFirst
	}
	backtracks := func(path []*geo.Point) bool {
		first, last := path[1], path[len(path)-2]
		return (first.X-h.X)*(h.X-prev.X)+(first.Y-h.Y)*(h.Y-prev.Y) < 0 ||
			(t.X-last.X)*(next.X-t.X)+(t.Y-last.Y)*(next.Y-t.Y) < 0
	}
	candidates := [][]*geo.Point{
		horizontalFirst,
		verticalFirst,
		{geo.NewPoint(mx, h.Y), geo.NewPoint(mx, t.Y)},
		{geo.NewPoint(h.X, my), geo.NewPoint(t.X, my)},
		nil,
	}
	var obstacles []*Object
	if e.Src.Graph != nil {
		for _, obj := range e.Src.Graph.Objects {
			if obj == e.Src || obj == e.Dst || obj.Box == nil || obj.TopLeft == nil ||
				e.Src.IsDescendantOf(obj) || e.Dst.IsDescendantOf(obj) {
				continue
			}
			obstacles = append(obstacles, obj)
		}
	}
	var best []*geo.Point
	bestCost := math.Inf(1)
	for _, corners := range candidates {
		path := append(append([]*geo.Point{h}, corners...), t)
		ok := true
		crossings := 0
		for i := 1; i < len(path) && ok; i++ {
			if segmentCrossesBox(e.Src.Box, path[i-1], path[i]) || segmentCrossesBox(e.Dst.Box, path[i-1], path[i]) {
				ok = false
			}
			for _, obj := range obstacles {
				if segmentCrossesBox(obj.Box, path[i-1], path[i]) {
					crossings++
				}
			}
		}
		if !ok {
			continue
		}
		bends := len(corners)
		if corners == nil {
			bends = 3 // a diagonal is the last resort
		}
		cost := float64(crossings*10 + bends)
		if backtracks(path) {
			cost += 1000
		}
		if cost < bestCost {
			best, bestCost = corners, cost
		}
	}
	return best
}

func portStub(anchor *geo.Point, side geo.Orientation, length float64) *geo.Point {
	n := PortSideNormal(side)
	return geo.NewPoint(anchor.X+n[0]*length, anchor.Y+n[1]*length)
}

// behind reports whether p is on the shape's side of the line through stub
// that is parallel to the pinned side.
func behind(p, stub *geo.Point, side geo.Orientation) bool {
	n := PortSideNormal(side)
	return (p.X-stub.X)*n[0]+(p.Y-stub.Y)*n[1] < 0
}

// portDetour returns the points to insert after stub so that the route on to
// next does not cut through obj: first sideways past obj's nearer edge, then,
// if needed, back past obj's far side.
func portDetour(obj *Object, side geo.Orientation, stub, next *geo.Point) []*geo.Point {
	if !segmentCrossesBox(obj.Box, stub, next) {
		return nil
	}
	n := PortSideNormal(side)
	t := geo.Vector{n[1], -n[0]}
	if (next.X-stub.X)*t[0]+(next.Y-stub.Y)*t[1] < 0 {
		t = geo.Vector{-t[0], -t[1]}
	}
	corners := []*geo.Point{
		obj.TopLeft,
		geo.NewPoint(obj.TopLeft.X+obj.Width, obj.TopLeft.Y),
		geo.NewPoint(obj.TopLeft.X, obj.TopLeft.Y+obj.Height),
		geo.NewPoint(obj.TopLeft.X+obj.Width, obj.TopLeft.Y+obj.Height),
	}
	reach := func(from *geo.Point, dir geo.Vector) float64 {
		m := 0.
		for _, c := range corners {
			m = math.Max(m, (c.X-from.X)*dir[0]+(c.Y-from.Y)*dir[1])
		}
		return m + PortClearance
	}
	side1 := reach(stub, t)
	c := geo.NewPoint(stub.X+t[0]*side1, stub.Y+t[1]*side1)
	if !segmentCrossesBox(obj.Box, c, next) {
		return []*geo.Point{c}
	}
	back := geo.Vector{-n[0], -n[1]}
	depth := reach(c, back)
	d := geo.NewPoint(c.X+back[0]*depth, c.Y+back[1]*depth)
	return []*geo.Point{c, d}
}

func segmentCrossesBox(box *geo.Box, a, b *geo.Point) bool {
	if box == nil {
		return false
	}
	inner := geo.NewBox(geo.NewPoint(box.TopLeft.X+1, box.TopLeft.Y+1), box.Width-2, box.Height-2)
	return inner.Contains(a) || inner.Contains(b) || len(inner.Intersections(*geo.NewSegment(a, b))) > 0
}

// RouteStraight routes edges as straight lines between the centers of Src and
// Dst, cut at their borders, for layouts without an edge router (grid cells,
// connections between nested layouts). Ends pinned with source-port /
// target-port start on their side instead, through PinRoute.
func RouteStraight(edges []*Edge) {
	anchors := PortAnchors(edges)
	for _, e := range edges {
		e.Route = []*geo.Point{e.Src.Center(), e.Dst.Center()}
		e.TraceToShape(e.Route, 0, 1)
		e.Route = e.PinRoute(e.Route, anchors[PortEnd{e, true}], anchors[PortEnd{e, false}])
	}
}
