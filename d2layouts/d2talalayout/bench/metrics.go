package bench

import (
	"math"
	"slices"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Metrics summarizes the geometry of one laid-out diagram. See BASELINE.md for
// the exact definitions.
type Metrics struct {
	Shapes      int
	Connections int
	// Crossings counts distinct points where two different connections'
	// rendered paths properly cross, excluding points within 1px of either
	// path's endpoints.
	Crossings int
	// Bends counts direction changes along routes. For polylines, every
	// interior vertex turning by more than 1 degree is one bend. For curved
	// routes, every maximal run of same-direction turning totalling at least
	// 20 degrees is one bend.
	Bends int
	// EdgeLength is the total rendered length of all routes.
	EdgeLength float64
	// BBoxWidth and BBoxHeight are the diagram bounding box over shapes and
	// routes, as computed by d2target.Diagram.BoundingBox.
	BBoxWidth, BBoxHeight int
	// EmptyArea is the bounding-box area minus the summed area of top-level
	// shapes (level 1: root objects, including containers).
	EmptyArea float64
	// EmptyFraction is EmptyArea divided by the bounding-box area.
	EmptyFraction float64
}

// Displacement summarizes how far shape centers moved between two layouts.
type Displacement struct {
	Compared int
	Mean     float64
	Max      float64
	// Moved counts compared shapes whose center moved by more than 1px.
	Moved int
}

const (
	curveSamples        = 16
	polylineBendDegrees = 1.0
	curveBendDegrees    = 20.0
	endpointTolerance   = 1.0
)

// RoutePolyline returns the rendered path of a connection as a polyline.
// Curved routes are cubic Bézier chains (start, then control, control, end
// triples) and are flattened with a fixed number of samples per segment.
func RoutePolyline(c d2target.Connection) []geo.Point {
	route := c.Route
	if len(route) == 0 {
		return nil
	}
	if !c.IsCurve || len(route) < 4 {
		out := make([]geo.Point, 0, len(route))
		for _, p := range route {
			out = append(out, *p)
		}
		return out
	}
	out := []geo.Point{*route[0]}
	for i := 1; i+2 < len(route); i += 3 {
		p0, c1, c2, p3 := *route[i-1], *route[i], *route[i+1], *route[i+2]
		for s := 1; s <= curveSamples; s++ {
			t := float64(s) / curveSamples
			mt := 1 - t
			a, b, cc, d := mt*mt*mt, 3*mt*mt*t, 3*mt*t*t, t*t*t
			out = append(out, geo.Point{
				X: a*p0.X + b*c1.X + cc*c2.X + d*p3.X,
				Y: a*p0.Y + b*c1.Y + cc*c2.Y + d*p3.Y,
			})
		}
	}
	return out
}

func polylineLength(pts []geo.Point) float64 {
	total := 0.0
	for i := 1; i < len(pts); i++ {
		total += math.Hypot(pts[i].X-pts[i-1].X, pts[i].Y-pts[i-1].Y)
	}
	return total
}

// turn returns the signed turning angle in degrees between segment a->b and b->c.
func turn(a, b, c geo.Point) (float64, bool) {
	ux, uy := b.X-a.X, b.Y-a.Y
	vx, vy := c.X-b.X, c.Y-b.Y
	if (ux == 0 && uy == 0) || (vx == 0 && vy == 0) {
		return 0, false
	}
	return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy) * 180 / math.Pi, true
}

// dedupe drops consecutive duplicate points.
func dedupe(pts []geo.Point) []geo.Point {
	out := make([]geo.Point, 0, len(pts))
	for _, p := range pts {
		if len(out) > 0 && out[len(out)-1] == p {
			continue
		}
		out = append(out, p)
	}
	return out
}

func countBends(c d2target.Connection, pts []geo.Point) int {
	pts = dedupe(pts)
	if !c.IsCurve {
		bends := 0
		for i := 1; i+1 < len(pts); i++ {
			if a, ok := turn(pts[i-1], pts[i], pts[i+1]); ok && math.Abs(a) > polylineBendDegrees {
				bends++
			}
		}
		return bends
	}
	bends := 0
	run := 0.0
	for i := 1; i+1 < len(pts); i++ {
		a, ok := turn(pts[i-1], pts[i], pts[i+1])
		if !ok || math.Abs(a) < 0.5 {
			continue
		}
		if run != 0 && math.Signbit(a) != math.Signbit(run) {
			if math.Abs(run) >= curveBendDegrees {
				bends++
			}
			run = 0
		}
		run += a
	}
	if math.Abs(run) >= curveBendDegrees {
		bends++
	}
	return bends
}

type segment struct {
	conn int
	a, b geo.Point
	minX float64
	minY float64
	maxX float64
	maxY float64
}

// segmentIntersection returns the proper intersection point of two segments,
// excluding parallel and collinear segments.
func segmentIntersection(s, t segment) (geo.Point, bool) {
	rx, ry := s.b.X-s.a.X, s.b.Y-s.a.Y
	qx, qy := t.b.X-t.a.X, t.b.Y-t.a.Y
	den := rx*qy - ry*qx
	if math.Abs(den) < 1e-9 {
		return geo.Point{}, false
	}
	wx, wy := t.a.X-s.a.X, t.a.Y-s.a.Y
	u := (wx*qy - wy*qx) / den
	v := (wx*ry - wy*rx) / den
	const eps = 1e-9
	if u < -eps || u > 1+eps || v < -eps || v > 1+eps {
		return geo.Point{}, false
	}
	return geo.Point{X: s.a.X + u*rx, Y: s.a.Y + u*ry}, true
}

func near(p, q geo.Point, tol float64) bool {
	return math.Abs(p.X-q.X) <= tol && math.Abs(p.Y-q.Y) <= tol
}

func countCrossings(polylines [][]geo.Point) int {
	var segs []segment
	for ci, pts := range polylines {
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			if a == b {
				continue
			}
			segs = append(segs, segment{
				conn: ci, a: a, b: b,
				minX: math.Min(a.X, b.X), minY: math.Min(a.Y, b.Y),
				maxX: math.Max(a.X, b.X), maxY: math.Max(a.Y, b.Y),
			})
		}
	}
	if len(segs) < 2 {
		return 0
	}
	// Sweep along X: sort by minX and compare only overlapping X ranges.
	slices.SortFunc(segs, func(a, b segment) int {
		switch {
		case a.minX < b.minX:
			return -1
		case a.minX > b.minX:
			return 1
		}
		return 0
	})
	type pairPoint struct {
		a, b int
		x, y int64
	}
	seen := map[pairPoint]struct{}{}
	for i := range segs {
		s := segs[i]
		for j := i + 1; j < len(segs) && segs[j].minX <= s.maxX; j++ {
			t := segs[j]
			if t.conn == s.conn || t.maxY < s.minY || t.minY > s.maxY {
				continue
			}
			p, ok := segmentIntersection(s, t)
			if !ok {
				continue
			}
			ps, pt := polylines[s.conn], polylines[t.conn]
			if near(p, ps[0], endpointTolerance) || near(p, ps[len(ps)-1], endpointTolerance) ||
				near(p, pt[0], endpointTolerance) || near(p, pt[len(pt)-1], endpointTolerance) {
				continue
			}
			a, b := s.conn, t.conn
			if a > b {
				a, b = b, a
			}
			// Deduplicate crossings found on two adjacent segments at a shared
			// vertex by snapping to a 1px grid.
			seen[pairPoint{a: a, b: b, x: int64(math.Round(p.X)), y: int64(math.Round(p.Y))}] = struct{}{}
		}
	}
	return len(seen)
}

// Measure computes the layout-quality metrics of diagram.
func Measure(diagram *d2target.Diagram) Metrics {
	m := Metrics{Shapes: len(diagram.Shapes), Connections: len(diagram.Connections)}
	polylines := make([][]geo.Point, 0, len(diagram.Connections))
	for _, c := range diagram.Connections {
		pts := RoutePolyline(c)
		m.EdgeLength += polylineLength(pts)
		m.Bends += countBends(c, pts)
		polylines = append(polylines, pts)
	}
	m.Crossings = countCrossings(polylines)

	tl, br := diagram.BoundingBox()
	m.BBoxWidth, m.BBoxHeight = br.X-tl.X, br.Y-tl.Y
	bbox := float64(m.BBoxWidth) * float64(m.BBoxHeight)
	used := 0.0
	for _, s := range diagram.Shapes {
		if s.Level == 1 {
			used += float64(s.Width) * float64(s.Height)
		}
	}
	m.EmptyArea = bbox - used
	if bbox > 0 {
		m.EmptyFraction = m.EmptyArea / bbox
	}
	return m
}

// Displace compares shape centers of two layouts of (nearly) the same diagram.
// Shapes in exclude, and shapes present in only one layout, are skipped.
func Displace(before, after *d2target.Diagram, exclude map[string]bool) Displacement {
	centers := make(map[string]geo.Point, len(before.Shapes))
	for _, s := range before.Shapes {
		centers[s.ID] = geo.Point{X: float64(s.Pos.X) + float64(s.Width)/2, Y: float64(s.Pos.Y) + float64(s.Height)/2}
	}
	var d Displacement
	total := 0.0
	for _, s := range after.Shapes {
		if exclude[s.ID] {
			continue
		}
		prev, ok := centers[s.ID]
		if !ok {
			continue
		}
		cur := geo.Point{X: float64(s.Pos.X) + float64(s.Width)/2, Y: float64(s.Pos.Y) + float64(s.Height)/2}
		dist := math.Hypot(cur.X-prev.X, cur.Y-prev.Y)
		total += dist
		d.Max = math.Max(d.Max, dist)
		if dist > 1 {
			d.Moved++
		}
		d.Compared++
	}
	if d.Compared > 0 {
		d.Mean = total / float64(d.Compared)
	}
	return d
}
