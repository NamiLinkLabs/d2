package d2graph

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
)

// PinnedCurve turns points, the polyline of e when it has source-port /
// target-port (from PinRoute or a router that honours pins), into D2's cubic
// curve format.
//
// Dagre's usual curve skips a polyline segment near each end, which would cut
// through a shape the route detours around. Here every segment is kept and
// only the corners are rounded, and corners near a pinned shape are rounded
// less than PortClearance, so the curve stays outside the shapes PinRoute
// steered around.
func (edge *Edge) PinnedCurve(points []*geo.Point) []*geo.Point {
	if len(points) < 2 {
		return points
	}
	var pinned []*geo.Box
	if _, ok := edge.SrcPortSide(); ok {
		pinned = append(pinned, edge.Src.Box)
	}
	if _, ok := edge.DstPortSide(); ok {
		pinned = append(pinned, edge.Dst.Box)
	}
	near := float64(PortStubLen + PortClearance + 1)
	obstacles := edge.RouteObstacles()

	type corner struct{ in, at, out *geo.Point }
	corners := make([]corner, 0, len(points))
	for i := 1; i < len(points)-1; i++ {
		prev, at, next := points[i-1], points[i], points[i+1]
		inLen := geo.EuclideanDistance(prev.X, prev.Y, at.X, at.Y)
		outLen := geo.EuclideanDistance(at.X, at.Y, next.X, next.Y)
		if inLen == 0 || outLen == 0 {
			continue
		}
		r := math.Min(inLen, outLen) / 2
		for _, box := range pinned {
			if distanceToBox(box, at) <= near {
				r = math.Min(r, PortClearance*3/4)
			}
		}
		c := corner{at: at}
		for {
			c.in = at.AddVector(at.VectorTo(prev).Unit().Multiply(r))
			c.out = at.AddVector(at.VectorTo(next).Unit().Multiply(r))
			// A wide bend cuts the inside of the corner: tighten it while it
			// would clip a shape the route goes around.
			if r <= PortClearance*3/4 || !bendCrosses(c.in, at, c.out, obstacles) {
				break
			}
			r = math.Max(r/2, PortClearance*3/4)
		}
		corners = append(corners, c)
	}

	path := []*geo.Point{points[0].Copy()}
	line := func(to *geo.Point) {
		from := path[len(path)-1]
		if from.X == to.X && from.Y == to.Y {
			return
		}
		v := from.VectorTo(to)
		path = append(path, from.AddVector(v.Multiply(1./3)), from.AddVector(v.Multiply(2./3)), to.Copy())
	}
	for _, c := range corners {
		line(c.in)
		// A quadratic bend with c.at as its control point, as a cubic.
		path = append(path,
			c.in.AddVector(c.in.VectorTo(c.at).Multiply(2./3)),
			c.out.AddVector(c.out.VectorTo(c.at).Multiply(2./3)),
			c.out.Copy(),
		)
	}
	line(points[len(points)-1])
	if len(path) == 1 {
		// Degenerate route: keep a valid single cubic.
		end := points[len(points)-1]
		path = append(path, path[0].Copy(), end.Copy(), end.Copy())
	}
	return path
}

// bendCrosses reports whether the quadratic bend from in to out with control
// point at runs through one of the obstacles.
func bendCrosses(in, at, out *geo.Point, obstacles []*Object) bool {
	const steps = 8
	prev := in
	for k := 1; k <= steps; k++ {
		t := float64(k) / steps
		p := geo.NewPoint(
			(1-t)*(1-t)*in.X+2*(1-t)*t*at.X+t*t*out.X,
			(1-t)*(1-t)*in.Y+2*(1-t)*t*at.Y+t*t*out.Y,
		)
		for _, obj := range obstacles {
			if SegmentCrossesBox(obj.Box, prev, p) {
				return true
			}
		}
		prev = p
	}
	return false
}

func distanceToBox(box *geo.Box, p *geo.Point) float64 {
	dx := math.Max(0, math.Max(box.TopLeft.X-p.X, p.X-(box.TopLeft.X+box.Width)))
	dy := math.Max(0, math.Max(box.TopLeft.Y-p.Y, p.Y-(box.TopLeft.Y+box.Height)))
	return math.Hypot(dx, dy)
}
