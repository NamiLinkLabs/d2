package d2layouts

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
	"github.com/d2lang/util-go/go2"
)

// The edges LayoutNested routes itself, rather than leaving them to the
// layout engine, are the ones that cross into or out of a constant near, a
// grid or a sequence diagram, and the ones between two cells of a grid.

// RouterFor returns the router for the edges LayoutNested routes itself in a
// diagram laid out with engine: DefaultRouter, with its routes curved like
// dagre's own for dagre, and with the budget TALA layouts have always had for
// TALA.
func RouterFor(engine string) d2graph.RouteEdges {
	switch {
	case strings.EqualFold(engine, "dagre"):
		return CurvedRouter(DefaultRouter)
	case strings.EqualFold(engine, "tala"):
		return talaRouter
	default:
		return DefaultRouter
	}
}

// defaultTogetherWork keeps routing edges together cheap: up to a few
// hundred thousand work units in common diagrams, about 0.1 s on one core
// when it runs out.
const defaultTogetherWork = 2_000_000

// DefaultRouter routes edges around the shapes already placed, with TALA's
// edge router, and draws the edges that router cannot route within its work
// budget in a straight line, as StraightRouter does. The calls for one
// LayoutNested share one budget, which grows with the size of the diagram,
// so a diagram with many such edges gets the rest drawn straight once it is
// spent. The budget counts work rather than time: the same diagram always
// gets the same routes.
func DefaultRouter(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
	return routeAroundShapes(ctx, g, edges, &d2talalayout.BestEffortOptions{
		TogetherWork: defaultTogetherWork,
		Budget:       routeBudget(ctx, g),
		Prefer:       func(e *d2graph.Edge) bool { return straightLineCrossesShape(g, e) },
	})
}

// talaRouter is DefaultRouter with the work budget of d2talalayout.RouteEdges,
// which TALA layouts used before, for routing edges together.
func talaRouter(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
	return TALARouter(nil)(ctx, g, edges)
}

// TALARouter returns RouterFor("tala") for a TALA layout with opts: of them
// only SeparateEdges applies, and diagram data under tala-separate-edges takes
// precedence, as it does for the layout. Nil opts uses the defaults.
func TALARouter(opts *d2talalayout.Options) d2graph.RouteEdges {
	talaOpts := d2talalayout.DefaultOptions()
	if opts != nil {
		talaOpts = *opts
	}
	return func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		return routeAroundShapes(ctx, g, edges, &d2talalayout.BestEffortOptions{TALA: &talaOpts})
	}
}

// routeBestEffort is a seam for tests of the straight fallback.
var routeBestEffort = d2talalayout.RouteEdgesBestEffort

func routeAroundShapes(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge, opts *d2talalayout.BestEffortOptions) error {
	var routable, straight []*d2graph.Edge
	for _, e := range edges {
		if isInvisibleRelay(e.Src) || isInvisibleRelay(e.Dst) {
			straight = append(straight, e)
		} else {
			routable = append(routable, e)
		}
	}
	unrouted, err := routeBestEffort(ctx, g, routable, opts)
	if err != nil {
		return err
	}
	straight = append(straight, unrouted...)
	if len(straight) < len(edges) {
		straight = append(straight, badPinnedRoutes(g, edges, straight)...)
	}
	return StraightRouter(ctx, g, straight)
}

// badPinnedRoutes tidies the routes the edge router gave the edges with
// source-port or target-port, those of edges not in straight, and returns
// the ones whose route still leaves a pinned side other than straight out of
// it, runs through a shape, or runs along a shape's border, such as the
// route between two touching sides, which leave the router no room.
// StraightRouter then gives those the pinned route it gives every pinned
// edge when the edges are not routed around shapes.
func badPinnedRoutes(g *d2graph.Graph, edges, straight []*d2graph.Edge) []*d2graph.Edge {
	var bad []*d2graph.Edge
	for _, e := range edges {
		if !e.HasPort() || e.Src == e.Dst || slices.Contains(straight, e) {
			continue
		}
		e.Route = tidyRoute(e.Route)
		if e.PortsHonored() != nil || e.RouteClear() != nil || runsAlongBorder(g, e) {
			bad = append(bad, e)
		}
	}
	return bad
}

// runsAlongBorder reports whether a segment of e's route runs along the
// border of a shape other than e's ends, such as the grid or container they
// are in, where it cannot be told apart from that border.
func runsAlongBorder(g *d2graph.Graph, e *d2graph.Edge) bool {
	const eps, minOverlap = 1., 10.
	// g may be a nested diagram, such as a grid, whose root is a shape too.
	for _, obj := range append([]*d2graph.Object{g.Root}, g.Objects...) {
		if obj == nil || obj == e.Src || obj == e.Dst || obj.Box == nil || obj.TopLeft == nil {
			continue
		}
		l, r := obj.TopLeft.X, obj.TopLeft.X+obj.Width
		t, b := obj.TopLeft.Y, obj.TopLeft.Y+obj.Height
		for i := 1; i < len(e.Route); i++ {
			p, q := e.Route[i-1], e.Route[i]
			if math.Abs(p.Y-q.Y) <= eps && (math.Abs(p.Y-t) <= eps || math.Abs(p.Y-b) <= eps) &&
				math.Min(math.Max(p.X, q.X), r)-math.Max(math.Min(p.X, q.X), l) > minOverlap {
				return true
			}
			if math.Abs(p.X-q.X) <= eps && (math.Abs(p.X-l) <= eps || math.Abs(p.X-r) <= eps) &&
				math.Min(math.Max(p.Y, q.Y), b)-math.Max(math.Min(p.Y, q.Y), t) > minOverlap {
				return true
			}
		}
	}
	return false
}

// tidyRoute drops the points of route that repeat the one before or lie on
// the line through their neighbours, including the tip of a route that runs
// out along a line and back.
func tidyRoute(route []*geo.Point) []*geo.Point {
	const eps = 1e-6
	out := make([]*geo.Point, 0, len(route))
	for _, p := range route {
		if n := len(out); n > 0 && math.Abs(out[n-1].X-p.X) < eps && math.Abs(out[n-1].Y-p.Y) < eps {
			continue
		}
		for n := len(out); n >= 2; n = len(out) {
			a, b := out[n-2], out[n-1]
			if math.Abs((b.X-a.X)*(p.Y-a.Y)-(b.Y-a.Y)*(p.X-a.X)) > eps {
				break
			}
			out = out[:n-1]
		}
		if n := len(out); n > 0 && math.Abs(out[n-1].X-p.X) < eps && math.Abs(out[n-1].Y-p.Y) < eps {
			continue
		}
		out = append(out, p)
	}
	if len(out) < 2 {
		return route
	}
	return out
}

// isInvisibleRelay reports whether obj is a shape that cannot be seen, with
// no label, icon or children: the kind of shape diagrams place to bend a
// chain of straight edges through it, as in a -> relay -> b. Edges to and
// from such a shape stay straight lines, so the chain still meets at it.
// Shapes are invisible as TALA's adapter counts them: at opacity 0, or with
// a transparent fill and no stroke.
func isInvisibleRelay(obj *d2graph.Object) bool {
	if obj == nil || obj.Label.Value != "" || obj.Icon != nil || len(obj.ChildrenArray) > 0 {
		return false
	}
	style := obj.Style
	if style.Opacity != nil {
		if f, err := strconv.ParseFloat(style.Opacity.Value, 64); err == nil && f == 0 {
			return true
		}
	}
	noFill := style.Fill != nil && strings.EqualFold(style.Fill.Value, "transparent")
	noStroke := style.Stroke != nil && strings.EqualFold(style.Stroke.Value, "transparent")
	if style.StrokeWidth != nil {
		if f, err := strconv.ParseFloat(style.StrokeWidth.Value, 64); err == nil && f == 0 {
			noStroke = true
		}
	}
	return noFill && noStroke
}

// curveCornerRadius is how far from each corner of a route CurvedRouter
// starts to round it when dagre's curve would cut through a shape.
const curveCornerRadius = 40

// curveSamples is how many points of each cubic of a curved route
// CurvedRouter checks against the shapes.
const curveSamples = 32

// CurvedRouter returns a router that routes edges with router and then curves
// every route that bends, as dagre curves its own routes. Where that curve
// would cut through a shape the route went around, it rounds only the
// route's corners instead, and where even that would, it keeps the corners.
func CurvedRouter(router d2graph.RouteEdges) d2graph.RouteEdges {
	return func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		if err := router(ctx, g, edges); err != nil {
			return err
		}
		for _, e := range edges {
			if e.IsCurve || len(e.Route) < 3 {
				continue
			}
			if e.HasPort() {
				curvePinned(g, e)
				continue
			}
			curve := geo.Route(e.Route).Curve()
			if curveEntersNewShape(g, e, curve) {
				curve = geo.Route(e.Route).RoundCorners(curveCornerRadius)
				if len(curve) < 4 || curveEntersNewShape(g, e, curve) {
					continue
				}
			}
			e.Route = curve
			e.IsCurve = true
			if e.Label.Value != "" {
				// The curve leaves the route's corners, where the router may have
				// put the label. Put it on the middle of the curve, as dagre does.
				e.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
				e.LabelPercentage = nil
			}
		}
		return nil
	}
}

// curvePinned curves the route of e, which has source-port or target-port,
// as dagre curves its own pinned routes: every segment is kept and only the
// corners are rounded, so the ends still leave their pinned sides straight.
// The route keeps its corners where even that curve would enter a shape.
func curvePinned(g *d2graph.Graph, e *d2graph.Edge) {
	curve := geo.Route(e.PinnedCurve(e.Route))
	if len(curve) < 4 || curveEntersNewShape(g, e, curve) {
		return
	}
	e.Route = curve
	e.IsCurve = true
	if e.Label.Value != "" {
		e.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
		e.LabelPercentage = nil
	}
}

// straightLineCrossesShape reports whether the straight line between the
// centers of e's ends enters a shape other than those ends and the
// containers they are in: whether e needs routing at all.
func straightLineCrossesShape(g *d2graph.Graph, e *d2graph.Edge) bool {
	if e.Src.Box == nil || e.Src.TopLeft == nil || e.Dst.Box == nil || e.Dst.TopLeft == nil {
		return true
	}
	line := []*geo.Point{e.Src.Center(), e.Dst.Center()}
	for _, obj := range g.Objects {
		if obj.Box == nil || obj.TopLeft == nil || e.Src.IsDescendantOf(obj) || e.Dst.IsDescendantOf(obj) {
			continue
		}
		if routeEnters(line, obj.Box) {
			return true
		}
	}
	return false
}

// curveEntersNewShape reports whether curve, the curve of e's route, enters
// a shape that the route itself does not, other than e's ends and the
// containers they are in.
func curveEntersNewShape(g *d2graph.Graph, e *d2graph.Edge, curve geo.Route) bool {
	curveTL, curveBR := curve.GetBoundingBox()
	for _, obj := range g.Objects {
		if obj.Box == nil || obj.TopLeft == nil || e.Src.IsDescendantOf(obj) || e.Dst.IsDescendantOf(obj) {
			continue
		}
		if obj.TopLeft.X > curveBR.X || obj.TopLeft.X+obj.Width < curveTL.X ||
			obj.TopLeft.Y > curveBR.Y || obj.TopLeft.Y+obj.Height < curveTL.Y {
			continue
		}
		if !curveEnters(curve, obj.Box) || routeEnters(e.Route, obj.Box) {
			continue
		}
		return true
	}
	return false
}

// curveEnters reports whether a sampled point of curve is inside box.
func curveEnters(curve geo.Route, box *geo.Box) bool {
	for i := 0; i+3 < len(curve); i += 3 {
		bezier := geo.NewBezierCurve(curve[i : i+4])
		for s := 0; s <= curveSamples; s++ {
			if strictlyInside(bezier.At(float64(s)/curveSamples), box) {
				return true
			}
		}
	}
	return false
}

// routeEnters reports whether a segment of route crosses box's border or has
// an end inside it.
func routeEnters(route []*geo.Point, box *geo.Box) bool {
	for i := 0; i+1 < len(route); i++ {
		if strictlyInside(route[i], box) || len(box.Intersections(geo.Segment{Start: route[i], End: route[i+1]})) > 0 {
			return true
		}
	}
	return len(route) > 0 && strictlyInside(route[len(route)-1], box)
}

// strictlyInside reports whether p is inside box and not on its border.
func strictlyInside(p *geo.Point, box *geo.Box) bool {
	return p.X > box.TopLeft.X && p.X < box.TopLeft.X+box.Width &&
		p.Y > box.TopLeft.Y && p.Y < box.TopLeft.Y+box.Height
}

// maxStraightEdgeSpacing is the most space between two straight edges that
// connect the same two shapes.
const maxStraightEdgeSpacing = 20

// StraightRouter draws each edge in a straight line between its shapes'
// centers, cut at their borders, ignoring the other shapes. Edges between the
// same two shapes are spread apart, side by side, so each one stays visible.
//
// An edge with source-port or target-port instead leaves and enters its
// pinned sides, with the straight line moved onto them by
// d2graph.Edge.PinRoute.
func StraightRouter(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
	type endpoints struct{ a, b *d2graph.Object }
	groups := make(map[endpoints][]*d2graph.Edge, len(edges))
	var order []endpoints
	var pinned []*d2graph.Edge
	for _, e := range edges {
		if e.HasPort() && e.Src != e.Dst {
			pinned = append(pinned, e)
			continue
		}
		key := endpoints{e.Src, e.Dst}
		if _, ok := groups[endpoints{e.Dst, e.Src}]; ok {
			key = endpoints{e.Dst, e.Src}
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], e)
	}
	for _, key := range order {
		if err := ctx.Err(); err != nil {
			return err
		}
		group := groups[key]
		// Every edge in the group is offset the same way from the line from
		// a's center to b's, whichever way the edge points.
		var normal geo.Vector
		spacing := 0.
		if len(group) > 1 && key.a != key.b {
			ac, bc := key.a.Center(), key.b.Center()
			nx, ny := geo.GetUnitNormalVector(ac.X, ac.Y, bc.X, bc.Y)
			normal = geo.NewVector(nx, ny)
			// Keep every line inside both shapes, so each still meets both.
			width := math.Min(extentAlong(key.a, normal), extentAlong(key.b, normal))
			spacing = math.Min(maxStraightEdgeSpacing, 0.8*width/float64(len(group)))
		}
		for i, e := range group {
			offset := geo.NewVector(0, 0)
			if spacing > 0 {
				offset = normal.Multiply((float64(i) - float64(len(group)-1)/2) * spacing)
			}
			routeStraight(e, offset)
		}
	}
	if len(pinned) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		d2graph.RouteStraight(pinned)
		for _, e := range pinned {
			e.IsCurve = false
			if e.Label.Value != "" {
				e.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
			}
		}
	}
	return nil
}

// extentAlong returns how far obj's box reaches along the unit vector v.
func extentAlong(obj *d2graph.Object, v geo.Vector) float64 {
	return math.Abs(v[0])*obj.Width + math.Abs(v[1])*obj.Height
}

// routeStraight routes e in a straight line between its shapes' centers,
// moved by offset, and cut at their borders.
func routeStraight(e *d2graph.Edge, offset geo.Vector) {
	src := e.Src.Center().AddVector(offset)
	dst := e.Dst.Center().AddVector(offset)
	e.Route = []*geo.Point{src, dst}
	e.IsCurve = false
	e.TraceToShape(e.Route, 0, 1)
	if e.Src != e.Dst && e.Route[0].Equals(e.Route[1]) {
		// The shapes touch, so there is no gap between them to draw the edge
		// in. Draw it from the source's center to the destination's border.
		e.Route = []*geo.Point{src, traceToBorder(e.Dst, dst, src)}
	}
	if e.Label.Value != "" {
		e.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
	}
}

// traceToBorder returns where the segment from outside to inside, a point
// in obj, crosses obj's border, or inside if it does not.
func traceToBorder(obj *d2graph.Object, inside, outside *geo.Point) *geo.Point {
	border := inside
	if intersections := obj.Intersections(geo.Segment{Start: outside, End: inside}); len(intersections) > 0 {
		border = intersections[0]
	}
	return shape.TraceToShapeBorder(obj.ToShape(), border, outside)
}
