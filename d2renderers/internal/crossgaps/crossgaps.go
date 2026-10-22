// Package crossgaps finds where connections cross, so that renderers can leave
// a small empty gap in the connection drawn below (d2lang/d2#1932).
//
// Gaps come from the exported routes, not from rendered path data: the same
// result serves the SVG renderer and the native scene builder, and stays right
// for routes changed after layout. Which connection gets the gap follows draw
// order. Connections are drawn by z-index, then in export order; a later one is
// drawn on top and stays whole, while the earlier one gets the gap.
//
// Only real crossings get gaps. Lines that meet where they end (at a shared
// shape), touch without passing through each other, run on top of each other
// (one connection drawn twice, or two merging into one side of a shape) or
// cross at a shallow angle are left alone. So are the synthetic lifelines of
// sequence diagrams and connections that aren't visible.
//
// The work is bounded and deterministic: a uniform grid limits which segments
// are compared, and fixed limits on that work and on the number of gaps keep
// huge diagrams fast. What was found before a limit stops the search is kept,
// in the same order every time, so output stays stable.
package crossgaps

import (
	"context"
	"math"
	"sort"

	"github.com/d2lang/d2/d2layouts/d2sequence"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
)

const (
	// EndClearance is how close to either connection's end a crossing may be
	// before it counts as two lines meeting at a shape.
	EndClearance = 14.
	// Margin is the clear space left on either side of the upper connection's
	// stroke.
	Margin = 3.5
	// SketchMargin is added to Margin in sketch mode, where hand-drawn strokes
	// stray from the route by a pixel or so.
	SketchMargin = 1.

	// MaxGaps bounds the gaps found in one board.
	MaxGaps = 2000
	// MaxWork bounds the candidate segment pairs compared in one board.
	MaxWork = 20_000_000
	// maxSegments, maxEntries and maxHits bound the memory used for one board:
	// the flattened segments, their entries in the grid and the segment pairs
	// that meet.
	maxSegments = 500_000
	maxEntries  = 4_000_000
	maxHits     = 1_000_000

	// minHalf is the least distance a gap reaches along the upper connection
	// either side of the crossing.
	minHalf = 7.
	// probe is how far along both lines together looks, and half the distance
	// within which two hits of the same pair of connections are one crossing.
	probe = 7.
	// Crossings at an angle whose sine is below minSin (about 20°) would get
	// long, ugly gaps and are skipped.
	minSin = .35
	// Curves are flattened until no piece strays further than tolerance from
	// the curve.
	tolerance     = 1.
	maxCurveSteps = 64
	// atVertex treats a hit this close to a segment's end (as a fraction of it)
	// as being at that vertex.
	atVertex = 1e-9
	// Directions at a shared point closer than same (radians) run together.
	same = 1e-3
	// Lines closer than closeDist to each other run together.
	closeDist = 1.
	// The grid has cells at least minCell wide, about maxCells cells in all and
	// at most maxSide cells along either side.
	minCell  = 64.
	maxCells = 250_000.
	maxSide  = 4096.
)

// Gap is one gap to leave in a connection.
type Gap struct {
	// Piece is the upper connection's centerline around the crossing, in
	// diagram coordinates. Stroking it with Width and butt caps covers the part
	// of the lower connection to leave out.
	Piece []geo.Point
	Width float64
	// Upper is the ID of the connection crossing over.
	Upper string
}

type Options struct {
	// Sketch widens gaps for hand-drawn strokes.
	Sketch bool
}

type pt struct{ x, y float64 }

func dist(a, b pt) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }

// line is one connection taking part, flattened into a polyline.
type line struct {
	id    string
	pts   []pt
	lens  []float64
	width float64
	// Distances from the start and the end within which crossings are where
	// lines meet at a shape.
	clear0, clear1 float64
	// Arrowhead lengths at the start and the end; the gap must not reach them.
	arrow0, arrow1 float64
	// Whether it can get gaps: masks are looked up by connection ID, so a
	// connection whose ID isn't unique on its board gets none.
	lower bool
}

// Find returns the gaps to leave in each connection of the board, keyed by
// connection ID, in a fixed order. Connections without gaps are left out. The
// board's layers, scenarios and steps are not searched.
func Find(ctx context.Context, diagram *d2target.Diagram, opts Options) (map[string][]Gap, error) {
	return find(ctx, diagram, opts, MaxWork)
}

func find(ctx context.Context, diagram *d2target.Diagram, opts Options, work int) (map[string][]Gap, error) {
	if diagram == nil || len(diagram.Connections) < 2 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lines, err := collect(ctx, diagram)
	if err != nil || len(lines) < 2 {
		return nil, err
	}
	s := newSegments(lines)
	if s.n == 0 {
		return nil, nil
	}
	start, bucket, err := s.grid(ctx)
	if err != nil {
		return nil, err
	}
	hits, err := s.search(ctx, start, bucket, work)
	if err != nil {
		return nil, err
	}
	return s.gaps(hits, opts), nil
}

// collect flattens the connections that take part, in draw order.
func collect(ctx context.Context, diagram *d2target.Diagram) ([]line, error) {
	conns := diagram.Connections
	order := make([]int, len(conns))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return conns[order[i]].ZIndex < conns[order[j]].ZIndex
	})
	shapes := make(map[string]struct{}, len(diagram.Shapes))
	for _, s := range diagram.Shapes {
		shapes[s.ID] = struct{}{}
	}
	isShape := func(id string) bool {
		_, ok := shapes[id]
		return ok
	}
	ids := make(map[string]int, len(conns))
	for i := range conns {
		ids[conns[i].ID]++
	}

	lines := make([]line, 0, len(conns))
	segments := 0
	for n, i := range order {
		if n&255 == 255 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		c := &conns[i]
		if !takesPart(c, isShape) {
			continue
		}
		pts := flatten(c)
		if len(pts) < 2 {
			continue
		}
		if segments+len(pts)-1 > maxSegments {
			// Out of room: keep the connections drawn first, the same ones
			// every time.
			break
		}
		segments += len(pts) - 1
		lens := make([]float64, len(pts))
		for k := 1; k < len(pts); k++ {
			lens[k] = lens[k-1] + dist(pts[k-1], pts[k])
		}
		width := float64(c.StrokeWidth)
		arrow0, arrow1 := arrowLength(c.SrcArrow, width), arrowLength(c.DstArrow, width)
		lines = append(lines, line{
			id:     c.ID,
			pts:    pts,
			lens:   lens,
			width:  width,
			clear0: math.Max(EndClearance, arrow0),
			clear1: math.Max(EndClearance, arrow1),
			arrow0: arrow0,
			arrow1: arrow1,
			lower:  ids[c.ID] == 1,
		})
	}
	return lines, nil
}

func takesPart(c *d2target.Connection, isShape func(string) bool) bool {
	if len(c.Route) < 2 || c.IsCurve && (len(c.Route)-1)%3 != 0 {
		return false
	}
	for _, p := range c.Route {
		if p == nil || !finite(p.X) || !finite(p.Y) {
			return false
		}
	}
	if c.Opacity == 0 || c.StrokeWidth <= 0 || !finite(c.BorderRadius) {
		return false
	}
	switch c.Stroke {
	case color.None, "transparent":
		return false
	}
	return !d2sequence.IsLifeline(*c, isShape)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// arrowLength is how far an arrowhead reaches back along its connection from
// the route's end.
func arrowLength(a d2target.Arrowhead, strokeWidth float64) float64 {
	if a == d2target.NoArrowhead || a == "" {
		return 0
	}
	w, _ := a.Dimensions(strokeWidth)
	return w
}

// flatten is the geometry the renderers draw for a connection, as a polyline:
// cubic Béziers for curved routes, and polylines with rounded corners
// otherwise. Arrowhead adjustments move the ends by a few pixels and are left
// out; crossings that close to an end are skipped anyway.
func flatten(c *d2target.Connection) []pt {
	r := c.Route
	at := func(i int) pt { return pt{r[i].X, r[i].Y} }
	out := []pt{at(0)}
	if c.IsCurve {
		for i := 1; i+2 < len(r); i += 3 {
			out = cubic(out, at(i), at(i+1), at(i+2))
		}
		return out
	}
	for i := 1; i < len(r)-1; i++ {
		prev, corner, next := r[i-1], r[i], r[i+1]
		pv, cv := prev.VectorTo(corner), corner.VectorTo(next)
		if pv.Length() == 0 || cv.Length() == 0 {
			out = lineTo(out, at(i))
			continue
		}
		units := math.Min(c.BorderRadius, geo.EuclideanDistance(corner.X, corner.Y, next.X, next.Y)/2)
		pt0 := pv.Unit().Multiply(units).ToPoint()
		ct := cv.Unit().Multiply(units).ToPoint()
		out = lineTo(out, pt{corner.X - pt0.X, corner.Y - pt0.Y})
		if units < c.BorderRadius && i < len(r)-2 {
			nv := next.VectorTo(r[i+2])
			if nv.Length() == 0 {
				continue
			}
			i++
			nt := nv.Unit().Multiply(units).ToPoint()
			out = cubic(out, pt{corner.X + pt0.X, corner.Y + pt0.Y}, pt{next.X - nt.X, next.Y - nt.Y}, pt{next.X + nt.X, next.Y + nt.Y})
		} else {
			// The renderers write this corner as an S command after a line, whose
			// first control point is the current point.
			out = cubic(out, out[len(out)-1], at(i), pt{corner.X + ct.X, corner.Y + ct.Y})
		}
	}
	return lineTo(out, at(len(r)-1))
}

func lineTo(out []pt, p pt) []pt {
	if !finite(p.x) || !finite(p.y) || p == out[len(out)-1] {
		return out
	}
	return append(out, p)
}

// cubic appends the cubic Bézier from the last point through controls p1 and
// p2 to p3, flattened.
func cubic(out []pt, p1, p2, p3 pt) []pt {
	p0 := out[len(out)-1]
	if !straight(p0, p1, p2, p3) {
		// Wang's formula: pieces needed to stay within tolerance of the curve.
		m := math.Max(math.Hypot(p0.x-2*p1.x+p2.x, p0.y-2*p1.y+p2.y), math.Hypot(p1.x-2*p2.x+p3.x, p1.y-2*p2.y+p3.y))
		n := int(math.Ceil(math.Sqrt(.75 * m / tolerance)))
		n = max(1, min(maxCurveSteps, n))
		for k := 1; k < n; k++ {
			t := float64(k) / float64(n)
			u := 1 - t
			a, b, cc, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
			out = lineTo(out, pt{a*p0.x + b*p1.x + cc*p2.x + d*p3.x, a*p0.y + b*p1.y + cc*p2.y + d*p3.y})
		}
	}
	return lineTo(out, p3)
}

// straight reports whether the cubic from p0 to p3 is as good as its chord.
func straight(p0, p1, p2, p3 pt) bool {
	dx, dy := p3.x-p0.x, p3.y-p0.y
	l2 := dx*dx + dy*dy
	if l2 < 1e-12 {
		return false
	}
	// The curve strays from the chord by at most 3/4 of the controls' distance.
	l := math.Sqrt(l2)
	d1 := math.Abs((p1.x-p0.x)*dy-(p1.y-p0.y)*dx) / l
	d2 := math.Abs((p2.x-p0.x)*dy-(p2.y-p0.y)*dx) / l
	if .75*math.Max(d1, d2) > tolerance {
		return false
	}
	// Controls beyond the ends would make it overshoot.
	t1 := ((p1.x-p0.x)*dx + (p1.y-p0.y)*dy) / l2
	t2 := ((p2.x-p0.x)*dx + (p2.y-p0.y)*dy) / l2
	return t1 >= 0 && t1 <= 1 && t2 >= 0 && t2 <= 1
}

// segments holds every segment of every line, flat, in draw order and along
// each line.
type segments struct {
	lines          []line
	n              int
	x0, y0, x1, y1 []float64
	// The segment's line, and the index of its end in that line's points.
	line, k                []int32
	minX, minY, maxX, maxY float64
}

func newSegments(lines []line) *segments {
	n := 0
	for _, l := range lines {
		n += len(l.pts) - 1
	}
	s := &segments{
		lines: lines,
		n:     n,
		x0:    make([]float64, 0, n), y0: make([]float64, 0, n),
		x1: make([]float64, 0, n), y1: make([]float64, 0, n),
		line: make([]int32, 0, n), k: make([]int32, 0, n),
		minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1),
	}
	for li, l := range lines {
		for _, p := range l.pts {
			s.minX, s.minY = math.Min(s.minX, p.x), math.Min(s.minY, p.y)
			s.maxX, s.maxY = math.Max(s.maxX, p.x), math.Max(s.maxY, p.y)
		}
		for k := 1; k < len(l.pts); k++ {
			s.x0 = append(s.x0, l.pts[k-1].x)
			s.y0 = append(s.y0, l.pts[k-1].y)
			s.x1 = append(s.x1, l.pts[k].x)
			s.y1 = append(s.y1, l.pts[k].y)
			s.line = append(s.line, int32(li))
			s.k = append(s.k, int32(k))
		}
	}
	return s
}

// grid lists every segment in each cell of a uniform grid it passes through
// (by the boxes of pieces no longer than a cell), so two segments that meet
// share a cell. Cell c holds bucket[start[c]:start[c+1]], in segment order.
func (s *segments) grid(ctx context.Context) (start, bucket []int32, err error) {
	w, h := s.maxX-s.minX, s.maxY-s.minY
	cell := math.Max(minCell, math.Sqrt((w+1)*(h+1)/maxCells))
	cell = math.Max(cell, math.Max(w, h)/maxSide)
	cols := int(math.Floor(w/cell)) + 1
	rows := int(math.Floor(h/cell)) + 1
	cellOf := func(v, lo float64, n int) int {
		return min(n-1, max(0, int(math.Floor((v-lo)/cell))))
	}

	var entryCell, entrySeg []int32
	// first is where the current segment's entries start.
	first := 0
	add := func(seg int, c0, c1, r0, r1 int) bool {
		for r := r0; r <= r1; r++ {
			for c := c0; c <= c1; c++ {
				key := int32(r*cols + c)
				// The pieces of a straight segment pass through cells in order, so
				// a cell listed already is among the last few listed.
				dup := false
				for e := len(entryCell) - 1; e >= max(first, len(entryCell)-8); e-- {
					if entryCell[e] == key {
						dup = true
						break
					}
				}
				if dup {
					continue
				}
				if len(entryCell) >= maxEntries {
					return false
				}
				entryCell = append(entryCell, key)
				entrySeg = append(entrySeg, int32(seg))
			}
		}
		return true
	}
	n := s.n
	for seg := 0; seg < s.n; seg++ {
		if seg&1023 == 1023 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		first = len(entryCell)
		px, py := s.x0[seg], s.y0[seg]
		dx, dy := s.x1[seg]-px, s.y1[seg]-py
		c0, c1 := cellOf(math.Min(px, px+dx), s.minX, cols), cellOf(math.Max(px, px+dx), s.minX, cols)
		r0, r1 := cellOf(math.Min(py, py+dy), s.minY, rows), cellOf(math.Max(py, py+dy), s.minY, rows)
		ok := true
		if c0 == c1 || r0 == r1 {
			// Within one row or column of cells: its box is exact.
			ok = add(seg, c0, c1, r0, r1)
		} else {
			parts := int(math.Ceil(math.Hypot(dx, dy) / cell))
			for k := 0; k < parts && ok; k++ {
				a, b := float64(k)/float64(parts), float64(k+1)/float64(parts)
				ax, ay, bx, by := px+dx*a, py+dy*a, px+dx*b, py+dy*b
				ok = add(seg,
					cellOf(math.Min(ax, bx), s.minX, cols), cellOf(math.Max(ax, bx), s.minX, cols),
					cellOf(math.Min(ay, by), s.minY, rows), cellOf(math.Max(ay, by), s.minY, rows))
			}
		}
		if !ok {
			// Out of room: search the segments listed so far, without this one.
			entryCell, entrySeg = entryCell[:first], entrySeg[:first]
			n = seg
			break
		}
	}
	s.n = n

	cells := cols * rows
	start = make([]int32, cells+1)
	for _, c := range entryCell {
		start[c+1]++
	}
	for c := 0; c < cells; c++ {
		start[c+1] += start[c]
	}
	fill := make([]int32, cells)
	copy(fill, start[:cells])
	bucket = make([]int32, len(entrySeg))
	for e, c := range entryCell {
		bucket[fill[c]] = entrySeg[e]
		fill[c]++
	}
	return start, bucket, nil
}

// hit is where segment a of a lower line meets segment b of an upper one, t
// along a and u along b.
type hit struct {
	a, b int32
	t, u float64
}

// search finds segments of different lines that cross. Candidates share a
// cell; a pair that shares several cells is reported once per cell, and the
// copies are dropped later. It stops after comparing work pairs.
func (s *segments) search(ctx context.Context, start, bucket []int32, work int) ([]hit, error) {
	var hits []hit
	cells := len(start) - 1
	for c := 0; c < cells; c++ {
		if c&255 == 255 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		end := int(start[c+1])
		for x := int(start[c]); x < end; x++ {
			s1 := bucket[x]
			l1 := s.line[s1]
			ax, ay, bx, by := s.x0[s1], s.y0[s1], s.x1[s1], s.y1[s1]
			rx, ry := bx-ax, by-ay
			loX, hiX := math.Min(ax, bx), math.Max(ax, bx)
			loY, hiY := math.Min(ay, by), math.Max(ay, by)
			for y := x + 1; y < end; y++ {
				if work--; work < 0 {
					return hits, nil
				}
				s2 := bucket[y]
				if s.line[s2] == l1 {
					continue
				}
				cx, cy, dx, dy := s.x0[s2], s.y0[s2], s.x1[s2], s.y1[s2]
				if hiX < math.Min(cx, dx) || math.Max(cx, dx) < loX || hiY < math.Min(cy, dy) || math.Max(cy, dy) < loY {
					continue
				}
				sx, sy := dx-cx, dy-cy
				den := rx*sy - ry*sx
				if math.Abs(den) < 1e-9 {
					continue
				}
				t := ((cx-ax)*sy - (cy-ay)*sx) / den
				u := ((cx-ax)*ry - (cy-ay)*rx) / den
				// A little slack, so a line through a vertex isn't lost to rounding
				// on both segments that meet there.
				if t < -atVertex || t > 1+atVertex || u < -atVertex || u > 1+atVertex {
					continue
				}
				// Within a cell segments are in draw order, so s1 is the lower line's.
				if len(hits) >= maxHits {
					return hits, nil
				}
				hits = append(hits, hit{a: s1, b: s2, t: t, u: u})
			}
		}
	}
	return hits, nil
}

// gaps turns hits into gaps, walking each pair of lines (lower first) along
// the lower line.
func (s *segments) gaps(hits []hit, opts Options) map[string][]Gap {
	sort.Slice(hits, func(i, j int) bool {
		h, k := hits[i], hits[j]
		if s.line[h.a] != s.line[k.a] {
			return s.line[h.a] < s.line[k.a]
		}
		if s.line[h.b] != s.line[k.b] {
			return s.line[h.b] < s.line[k.b]
		}
		if h.a != k.a {
			return h.a < k.a
		}
		if h.t != k.t {
			return h.t < k.t
		}
		if h.b != k.b {
			return h.b < k.b
		}
		return h.u < k.u
	})
	margin := Margin
	if opts.Sketch {
		margin += SketchMargin
	}
	out := map[string][]Gap{}
	count := 0
	pair := [2]int32{-1, -1}
	var seen []pt
	for _, h := range hits {
		la, lb := s.line[h.a], s.line[h.b]
		A, B := &s.lines[la], &s.lines[lb]
		if A.id == B.id {
			continue
		}
		if key := [2]int32{la, lb}; key != pair {
			pair = key
			seen = seen[:0]
		}
		kA, kB := int(s.k[h.a]), int(s.k[h.b])
		at := pt{s.x0[h.a] + (s.x1[h.a]-s.x0[h.a])*h.t, s.y0[h.a] + (s.y1[h.a]-s.y0[h.a])*h.t}
		// Lines meeting at a shape aren't crossing.
		if dist(at, A.pts[0]) < A.clear0 || dist(at, A.pts[len(A.pts)-1]) < A.clear1 ||
			dist(at, B.pts[0]) < B.clear0 || dist(at, B.pts[len(B.pts)-1]) < B.clear1 {
			continue
		}
		dup := false
		for _, v := range seen {
			if dist(v, at) < 2*probe {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		// Segments that meet where one of them ends (at a vertex) only touch
		// there unless the lines really cross: lines drawn on top of each other
		// share every vertex.
		ra, rb := rays(A.pts, kA, h.t), rays(B.pts, kB, h.u)
		if !passes(ra, rb) {
			continue
		}
		sin := crossingSin(ra, rb)
		if sin < minSin {
			continue
		}
		lenA := A.lens[kA-1] + (A.lens[kA]-A.lens[kA-1])*h.t
		lenB := B.lens[kB-1] + (B.lens[kB]-B.lens[kB-1])*h.u
		// Nor are lines that run together on one side crossing, however they meet.
		if together(A, lenA, B, lenB) {
			continue
		}
		seen = append(seen, at)
		if !A.lower {
			continue
		}
		width := B.width + 2*margin
		cos := math.Sqrt(math.Max(0, 1-sin*sin))
		// The band of that width along B leaves A out where they are within
		// width/2 of each other. Along B, A's stroke reaches that far from the
		// crossing.
		half := math.Max(minHalf, width/2*cos/sin+A.width/2/sin+1)
		// Along A, the gap reaches this far. Keep it off A's arrowheads, which
		// the gap would cut too.
		reach := width/2/sin + A.width/2*cos/sin
		total := A.lens[len(A.lens)-1]
		if A.arrow0 > 0 && lenA-reach < A.arrow0+1 || A.arrow1 > 0 && total-lenA-reach < A.arrow1+1 {
			continue
		}
		out[A.id] = append(out[A.id], Gap{Piece: piece(B, lenB, half), Width: round(width), Upper: B.id})
		if count++; count >= MaxGaps {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// rays are the directions back along and on along a line from a point on its
// segment k, t along it. At a vertex they come from the segments either side;
// at the line's end, the one segment there is taken as going straight on.
func rays(p []pt, k int, t float64) [2]pt {
	v := -1
	if t <= atVertex {
		v = k - 1
	} else if t >= 1-atVertex {
		v = k
	}
	if v < 0 {
		d := pt{p[k].x - p[k-1].x, p[k].y - p[k-1].y}
		return [2]pt{{-d.x, -d.y}, d}
	}
	var back, on *pt
	if v > 0 {
		back = &pt{p[v-1].x - p[v].x, p[v-1].y - p[v].y}
	}
	if v < len(p)-1 {
		on = &pt{p[v+1].x - p[v].x, p[v+1].y - p[v].y}
	}
	if back == nil {
		back = &pt{-on.x, -on.y}
	}
	if on == nil {
		on = &pt{-back.x, -back.y}
	}
	return [2]pt{*back, *on}
}

// turn is the angle from direction a round to direction b, always the same way
// round, in [0, 2π).
func turn(a, b float64) float64 {
	d := math.Mod(b-a, 2*math.Pi)
	if d < 0 {
		d += 2 * math.Pi
	}
	return d
}

// passes reports whether b passes from one side of a to the other at a point
// they share, given each line's directions away from it. Not where the lines
// run together on either side, nor where one only touches the other.
func passes(a, b [2]pt) bool {
	a0, a1 := math.Atan2(a[0].y, a[0].x), math.Atan2(a[1].y, a[1].x)
	b0, b1 := math.Atan2(b[0].y, b[0].x), math.Atan2(b[1].y, b[1].x)
	for _, x := range [2]float64{a0, a1} {
		for _, y := range [2]float64{b0, b1} {
			if d := turn(x, y); d < same || d > 2*math.Pi-same {
				return false
			}
		}
	}
	// a splits the directions around the point in two; b must leave into both.
	span := turn(a0, a1)
	return turn(a0, b0) < span != (turn(a0, b1) < span)
}

// crossingSin is the sine of the angle between two lines at a shared point,
// from their directions away from it.
func crossingSin(a, b [2]pt) float64 {
	// Each line's direction through the point: the mean of coming in and going on.
	through := func(r [2]pt) pt {
		lp, lq := math.Hypot(r[0].x, r[0].y), math.Hypot(r[1].x, r[1].y)
		if lp == 0 {
			lp = 1
		}
		if lq == 0 {
			lq = 1
		}
		return pt{r[1].x/lq - r[0].x/lp, r[1].y/lq - r[0].y/lp}
	}
	da, db := through(a), through(b)
	d := math.Hypot(da.x, da.y) * math.Hypot(db.x, db.y)
	if d == 0 {
		return 0
	}
	return math.Abs(da.x*db.y-da.y*db.x) / d
}

// after is the index of the first point of l at or past distance d along it
// (at least 1).
func after(l *line, d float64) int {
	return max(1, min(len(l.lens)-1, sort.SearchFloat64s(l.lens, d)))
}

func pointAt(l *line, d float64) pt {
	k := after(l, d)
	a, b := l.pts[k-1], l.pts[k]
	span := l.lens[k] - l.lens[k-1]
	t := 0.
	if span > 0 {
		t = math.Min(1, math.Max(0, (d-l.lens[k-1])/span))
	}
	return pt{a.x + (b.x-a.x)*t, a.y + (b.y-a.y)*t}
}

// piece is the part of l within half of distance d along it.
func piece(l *line, d, half float64) []geo.Point {
	from, to := math.Max(0, d-half), math.Min(l.lens[len(l.lens)-1], d+half)
	ps := []pt{pointAt(l, from)}
	for i := after(l, from); i < len(l.lens) && l.lens[i] < to; i++ {
		if l.lens[i] > from {
			ps = append(ps, l.pts[i])
		}
	}
	ps = append(ps, pointAt(l, to))
	out := make([]geo.Point, len(ps))
	for i, p := range ps {
		out[i] = geo.Point{X: round(p.x), Y: round(p.y)}
	}
	return out
}

// round keeps two decimals, which is plenty for a gap and keeps output stable
// across architectures that fuse multiply-adds.
func round(v float64) float64 {
	return math.Round(v*100) / 100
}

// together reports whether lines a and b, from where they meet (da and db
// along them), stay within closeDist of each other for probe along both,
// either way along either. Routes drawn on top of each other do: one
// connection drawn twice, or both ways, or two merging into one side of a
// shape. Their rounded corners aren't quite the same curve both ways, and
// flattened they cross back and forth.
func together(a *line, da float64, b *line, db float64) bool {
	ta, tb := a.lens[len(a.lens)-1], b.lens[len(b.lens)-1]
	for _, sa := range [2]float64{-1, 1} {
		if da+sa*probe < 0 || da+sa*probe > ta {
			continue
		}
		for _, sb := range [2]float64{-1, 1} {
			if db+sb*probe < 0 || db+sb*probe > tb {
				continue
			}
			close := true
			for i := 1; i <= 4 && close; i++ {
				p := pointAt(a, da+sa*probe*float64(i)/4)
				q := pointAt(b, db+sb*probe*float64(i)/4)
				close = dist(p, q) < closeDist
			}
			if close {
				return true
			}
		}
	}
	return false
}

// Region is a box around everything drawn for a connection's line and
// arrowheads: what a gap mask must leave visible. It may be larger.
func Region(c *d2target.Connection, sketch bool) (x, y, width, height float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	// Curves stay within their controls' hull and rounded corners within the
	// route's, so the route's points bound the line.
	for _, p := range c.Route {
		if p == nil || !finite(p.X) || !finite(p.Y) {
			continue
		}
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	if minX > maxX {
		return 0, 0, 0, 0
	}
	stroke := float64(c.StrokeWidth)
	pad := stroke + 4
	for _, a := range [2]d2target.Arrowhead{c.SrcArrow, c.DstArrow} {
		if a != d2target.NoArrowhead && a != "" {
			w, h := a.Dimensions(stroke)
			pad = math.Max(pad, math.Max(w, h)+stroke+4)
		}
	}
	if sketch {
		pad += 4
	}
	minX, minY = math.Floor(minX-pad), math.Floor(minY-pad)
	maxX, maxY = math.Ceil(maxX+pad), math.Ceil(maxY+pad)
	return minX, minY, maxX - minX, maxY - minY
}
