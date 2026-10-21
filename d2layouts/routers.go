package d2layouts

import (
	"context"
	"math"
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

// defaultRouteWork keeps routing edges together cheap: up to a few hundred
// thousand work units in common diagrams, about 0.1 s on one core when it
// runs out.
var defaultRouteWork = &d2talalayout.BestEffortOptions{TogetherWork: 2_000_000}

// DefaultRouter routes edges around the shapes already placed, with TALA's
// edge router, and draws the edges that router cannot route within its work
// budget in a straight line, as StraightRouter does.
func DefaultRouter(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
	return routeAroundShapes(ctx, g, edges, defaultRouteWork)
}

// talaRouter is DefaultRouter with the work budget of d2talalayout.RouteEdges,
// which TALA layouts used before, for routing edges together.
func talaRouter(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
	return routeAroundShapes(ctx, g, edges, nil)
}

func routeAroundShapes(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge, opts *d2talalayout.BestEffortOptions) error {
	unrouted, err := d2talalayout.RouteEdgesBestEffort(ctx, g, edges, opts)
	if err != nil {
		return err
	}
	return StraightRouter(ctx, g, unrouted)
}

// CurvedRouter returns a router that routes edges with router and then curves
// every route that bends, as dagre curves its own routes.
func CurvedRouter(router d2graph.RouteEdges) d2graph.RouteEdges {
	return func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		if err := router(ctx, g, edges); err != nil {
			return err
		}
		for _, e := range edges {
			if e.IsCurve || len(e.Route) < 3 {
				continue
			}
			e.Route = geo.Route(e.Route).Curve()
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

// maxStraightEdgeSpacing is the most space between two straight edges that
// connect the same two shapes.
const maxStraightEdgeSpacing = 20

// StraightRouter draws each edge in a straight line between its shapes'
// centers, cut at their borders, ignoring the other shapes. Edges between the
// same two shapes are spread apart, side by side, so each one stays visible.
func StraightRouter(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
	type endpoints struct{ a, b *d2graph.Object }
	groups := make(map[endpoints][]*d2graph.Edge, len(edges))
	var order []endpoints
	for _, e := range edges {
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
