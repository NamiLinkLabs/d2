package crossgaps_test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/internal/crossgaps"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

type pt struct{ x, y float64 }

// conn is a straight-cornered polyline connection drawn with the default
// stroke.
func conn(id string, pts ...float64) d2target.Connection {
	c := d2target.BaseConnection()
	c.ID = id
	c.Src, c.Dst = "", ""
	c.SrcArrow, c.DstArrow = d2target.NoArrowhead, d2target.NoArrowhead
	c.Stroke = "#0D32B2"
	c.StrokeWidth = 2
	c.Opacity = 1
	c.BorderRadius = 0
	for i := 0; i+1 < len(pts); i += 2 {
		c.Route = append(c.Route, geo.NewPoint(pts[i], pts[i+1]))
	}
	return *c
}

func rounded(radius float64, c d2target.Connection) d2target.Connection {
	c.BorderRadius = radius
	return c
}

func curve(c d2target.Connection) d2target.Connection {
	c.IsCurve = true
	return c
}

// board names its connections e0, e1, ... in order.
func board(conns ...d2target.Connection) *d2target.Diagram {
	d := d2target.NewDiagram()
	for i := range conns {
		if conns[i].ID == "" {
			conns[i].ID = fmt.Sprintf("e%d", i)
		}
	}
	d.Connections = conns
	return d
}

type found struct {
	lower, upper string
	gap          crossgaps.Gap
}

// find returns the gaps in the order of the connections that get them.
func find(t *testing.T, d *d2target.Diagram, opts crossgaps.Options) []found {
	t.Helper()
	gaps, err := crossgaps.Find(context.Background(), d, opts)
	if err != nil {
		t.Fatal(err)
	}
	return flat(d, gaps)
}

func flat(d *d2target.Diagram, gaps map[string][]crossgaps.Gap) []found {
	var out []found
	for _, c := range d.Connections {
		for _, g := range gaps[c.ID] {
			out = append(out, found{lower: c.ID, upper: g.Upper, gap: g})
		}
	}
	return out
}

func near(a, b pt, tol float64) bool { return math.Hypot(a.x-b.x, a.y-b.y) <= tol }

// mid is halfway along a gap's piece.
func mid(g crossgaps.Gap) pt {
	p := g.Piece
	lens := []float64{0}
	for i := 1; i < len(p); i++ {
		lens = append(lens, lens[i-1]+math.Hypot(p[i].X-p[i-1].X, p[i].Y-p[i-1].Y))
	}
	half := lens[len(lens)-1] / 2
	i := 1
	for i < len(p)-1 && lens[i] < half {
		i++
	}
	span := lens[i] - lens[i-1]
	if span == 0 {
		span = 1
	}
	t := (half - lens[i-1]) / span
	return pt{p[i-1].X + (p[i].X-p[i-1].X)*t, p[i-1].Y + (p[i].Y-p[i-1].Y)*t}
}

func mids(fs []found) []pt {
	var out []pt
	for _, f := range fs {
		out = append(out, mid(f.gap))
	}
	return out
}

func TestPlainCrossing(t *testing.T) {
	t.Parallel()
	r := find(t, board(conn("", 0, 100, 200, 100), conn("", 100, 0, 100, 200)), crossgaps.Options{})
	if len(r) != 1 {
		t.Fatalf("got %d gaps, want 1: %v", len(r), r)
	}
	if r[0].lower != "e0" || r[0].upper != "e1" {
		t.Fatalf("gap is in %s under %s, want e0 under e1", r[0].lower, r[0].upper)
	}
	want := []geo.Point{{X: 100, Y: 93}, {X: 100, Y: 107}}
	if !reflect.DeepEqual(r[0].gap.Piece, want) {
		t.Fatalf("piece = %v, want %v", r[0].gap.Piece, want)
	}
	if r[0].gap.Width != 2+2*crossgaps.Margin {
		t.Fatalf("width = %v, want %v", r[0].gap.Width, 2+2*crossgaps.Margin)
	}

	sketch := find(t, board(conn("", 0, 100, 200, 100), conn("", 100, 0, 100, 200)), crossgaps.Options{Sketch: true})
	if len(sketch) != 1 || sketch[0].gap.Width != 2+2*(crossgaps.Margin+crossgaps.SketchMargin) {
		t.Fatalf("sketch gaps = %v", sketch)
	}
}

func TestZIndexWins(t *testing.T) {
	t.Parallel()
	a := conn("", 0, 100, 200, 100)
	a.ZIndex = 1
	r := find(t, board(a, conn("", 100, 0, 100, 200)), crossgaps.Options{})
	if len(r) != 1 || r[0].lower != "e1" || r[0].upper != "e0" {
		t.Fatalf("gaps = %v, want e1 under e0", r)
	}
}

func TestThickLinesGetWiderGaps(t *testing.T) {
	t.Parallel()
	lower := conn("", 0, 100, 200, 100)
	lower.StrokeWidth = 12
	upper := conn("", 100, 0, 100, 200)
	upper.StrokeWidth = 6
	r := find(t, board(lower, upper), crossgaps.Options{})
	if len(r) != 1 {
		t.Fatalf("got %d gaps, want 1", len(r))
	}
	g := r[0].gap
	if g.Width != 6+2*crossgaps.Margin {
		t.Fatalf("width = %v, want %v", g.Width, 6+2*crossgaps.Margin)
	}
	// The piece must reach past the lower line's stroke on both sides.
	if first, last := g.Piece[0], g.Piece[len(g.Piece)-1]; first.Y > 100-6 || last.Y < 100+6 {
		t.Fatalf("piece %v doesn't cover a 12px stroke", g.Piece)
	}
}

func TestMeetingAtShapesIsNotCrossing(t *testing.T) {
	t.Parallel()
	r := find(t, board(
		conn("", 0, 0, 100, 100),
		conn("", 100, 100, 200, 0),
		conn("", 95, -20, 95, 20),
		conn("", 0, 0, 100, 0),
	), crossgaps.Options{})
	if len(r) != 0 {
		t.Fatalf("got gaps %v", mids(r))
	}
}

func TestShallowCrossingsAreSkipped(t *testing.T) {
	t.Parallel()
	r := find(t, board(conn("", 0, 100, 400, 100), conn("", 0, 80, 400, 120)), crossgaps.Options{})
	if len(r) != 0 {
		t.Fatalf("got gaps %v", mids(r))
	}
}

func TestCurvesCrossWhereTheyReallyDo(t *testing.T) {
	t.Parallel()
	r := find(t, board(
		curve(conn("", 0, 100, 100, 100, 100, 100, 200, 100)),
		curve(conn("", 100, 0, 100, 60, 100, 140, 100, 200)),
		curve(conn("", 0, 0, 50, 100, 150, 100, 200, 200)),
	), crossgaps.Options{})
	if len(r) != 3 {
		t.Fatalf("got %d gaps, want 3: %v", len(r), mids(r))
	}
	for _, f := range r {
		if !near(mid(f.gap), pt{100, 100}, 8) {
			t.Fatalf("gap of %s under %s at %v, want near (100, 100)", f.lower, f.upper, mid(f.gap))
		}
	}
}

func TestDagreCurves(t *testing.T) {
	t.Parallel()
	// A two-cubic dagre route crossing a straight one three times.
	r := find(t, board(
		conn("", -100, 50, 500, 50),
		curve(conn("", 0, 0, 50, 200, 150, 200, 200, 0, 250, -200, 350, 100, 400, 100)),
	), crossgaps.Options{})
	if len(r) != 3 {
		t.Fatalf("got %d gaps, want 3: %v", len(r), mids(r))
	}
	for _, f := range r {
		if m := mid(f.gap); math.Abs(m.y-50) > 1.5 {
			t.Fatalf("gap at %v, want on y = 50", m)
		}
	}
}

func TestLinesOnTopOfEachOther(t *testing.T) {
	t.Parallel()
	// One connection drawn twice between pinned boxes: both get the same route,
	// with a rounded corner.
	dup := func() d2target.Connection { return rounded(10, conn("", 55, 33, 326.5, 33, 326.5, 196)) }
	if r := find(t, board(dup(), dup()), crossgaps.Options{}); len(r) != 0 {
		t.Fatalf("routes on top of each other cross at %v", mids(r))
	}
	// The same route both ways (a -> b and b -> a).
	rev := rounded(10, conn("", 326.5, 196, 326.5, 33, 55, 33))
	if r := find(t, board(dup(), rev), crossgaps.Options{}); len(r) != 0 {
		t.Fatalf("a route and its reverse cross at %v", mids(r))
	}
	// Two connections from either side merging into one side of a shape.
	r := find(t, board(
		rounded(10, conn("", 100, 50, 170, 50, 170, 200)),
		rounded(10, conn("", 240, 50, 170, 50, 170, 200)),
	), crossgaps.Options{})
	if len(r) != 0 {
		t.Fatalf("routes merging into one side of a shape cross at %v", mids(r))
	}
	// A line crossing both copies cuts both, once each.
	r = find(t, board(dup(), dup(), conn("", 200, 0, 200, 100)), crossgaps.Options{})
	if len(r) != 2 || r[0].lower == r[1].lower {
		t.Fatalf("gaps = %v, want one in each copy", r)
	}
	for _, f := range r {
		if f.upper != "e2" || !near(mid(f.gap), pt{200, 33}, 1) {
			t.Fatalf("gap of %s under %s at %v", f.lower, f.upper, mid(f.gap))
		}
	}
}

func TestThroughAndTouchingVertices(t *testing.T) {
	t.Parallel()
	// One route crossing another exactly where its rounded corner starts.
	r := find(t, board(rounded(10, conn("", 0, 100, 310, 100, 310, 300)), conn("", 300, 0, 300, 200)), crossgaps.Options{})
	if len(r) != 1 || !near(mid(r[0].gap), pt{300, 100}, .5) {
		t.Fatalf("crossing through a vertex: %v", mids(r))
	}
	// Touching at a vertex, from one side, isn't crossing.
	if r := find(t, board(conn("", 0, 0, 100, 100, 200, 0), conn("", 0, 100, 200, 100)), crossgaps.Options{}); len(r) != 0 {
		t.Fatalf("a touch is a crossing at %v", mids(r))
	}
	// Nor when it comes in square and leaves at an angle.
	if r := find(t, board(conn("", 100, 0, 100, 100, 200, 0), conn("", 0, 100, 300, 100)), crossgaps.Options{}); len(r) != 0 {
		t.Fatalf("a lopsided touch is a crossing at %v", mids(r))
	}
	// A T-junction isn't either.
	if r := find(t, board(conn("", 0, 100, 300, 100), conn("", 150, 0, 150, 100)), crossgaps.Options{}); len(r) != 0 {
		t.Fatalf("a T-junction is a crossing at %v", mids(r))
	}
}

func TestPieceFollowsRoundedCorner(t *testing.T) {
	t.Parallel()
	r := find(t, board(
		conn("", 240, 30, 360, 150),
		rounded(20, conn("", 300, 0, 300, 100, 0, 100)),
	), crossgaps.Options{})
	if len(r) != 1 {
		t.Fatalf("got %d gaps, want 1", len(r))
	}
	p := r[0].gap.Piece
	if len(p) < 4 {
		t.Fatalf("piece %v doesn't follow the corner", p)
	}
	d0 := math.Atan2(p[1].Y-p[0].Y, p[1].X-p[0].X)
	d1 := math.Atan2(p[len(p)-1].Y-p[len(p)-2].Y, p[len(p)-1].X-p[len(p)-2].X)
	if math.Abs(d1-d0) < 10*math.Pi/180 {
		t.Fatalf("piece %v doesn't bend", p)
	}
	// Every point lies on the corner's curve: within its radius of (280, 80).
	for _, q := range p {
		if d := math.Hypot(q.X-280, q.Y-80); d < 17 || d > 23 {
			t.Fatalf("piece point %v is %v from the corner's center", q, d)
		}
	}
}

func TestArrowheadsStayWhole(t *testing.T) {
	t.Parallel()
	// The lower line ends 16px past the crossing, under its arrowhead.
	lower := conn("", 0, 100, 116, 100)
	lower.DstArrow = d2target.ArrowArrowhead
	if r := find(t, board(lower, conn("", 100, 0, 100, 200)), crossgaps.Options{}); len(r) != 0 {
		t.Fatalf("gap cuts the arrowhead: %v", mids(r))
	}
	lower.Route[1].X = 140
	if r := find(t, board(lower, conn("", 100, 0, 100, 200)), crossgaps.Options{}); len(r) != 1 {
		t.Fatalf("got %d gaps clear of the arrowhead, want 1", len(r))
	}
}

func TestLeftOut(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*d2target.Connection){
		"transparent upper": func(c *d2target.Connection) { c.Stroke = "transparent" },
		"invisible upper":   func(c *d2target.Connection) { c.Opacity = 0 },
		"no stroke":         func(c *d2target.Connection) { c.StrokeWidth = 0 },
		"one point":         func(c *d2target.Connection) { c.Route = c.Route[:1] },
		"bad curve":         func(c *d2target.Connection) { c.IsCurve = true },
		"non-finite":        func(c *d2target.Connection) { c.Route[0].X = math.NaN() },
	}
	for name, change := range cases {
		change := change
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			upper := conn("", 100, 0, 100, 200)
			change(&upper)
			if r := find(t, board(conn("", 0, 100, 200, 100), upper), crossgaps.Options{}); len(r) != 0 {
				t.Fatalf("got gaps %v", r)
			}
		})
	}
	t.Run("duplicate IDs", func(t *testing.T) {
		t.Parallel()
		r := find(t, board(conn("a", 0, 100, 200, 100), conn("a", 0, 150, 200, 150), conn("b", 100, 0, 100, 200)), crossgaps.Options{})
		if len(r) != 0 {
			t.Fatalf("got gaps %v", r)
		}
	})
}

func TestSequenceLifelines(t *testing.T) {
	t.Parallel()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	ctx := log.WithDefault(context.Background())
	layout := "dagre"
	diagram, _, err := d2lib.Compile(ctx, `
shape: sequence_diagram
a; b; c
a -> c: over b
c -> a
`, &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: &layout,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	lifelines := 0
	for _, c := range diagram.Connections {
		if len(c.Route) == 2 && c.Route[0].X == c.Route[1].X {
			lifelines++
		}
	}
	if lifelines != 3 {
		t.Fatalf("got %d lifelines, want 3", lifelines)
	}
	if r := find(t, diagram, crossgaps.Options{}); len(r) != 0 {
		t.Fatalf("lifelines got gaps: %v", r)
	}
}

// The grid search finds the same crossings as checking every pair of lines.
func TestGridAgreesWithAllPairs(t *testing.T) {
	t.Parallel()
	seed := int64(12345)
	rnd := func() float64 {
		seed = (seed*1103515245 + 12345) % 2147483648
		return float64(seed) / 2147483648
	}
	var conns []d2target.Connection
	var lines [][]pt
	for i := 0; i < 60; i++ {
		x, y := rnd()*600, rnd()*600
		ps := []float64{x, y}
		line := []pt{{x, y}}
		n := 1 + int(rnd()*5)
		for k := 0; k < n; k++ {
			x += (rnd() - .5) * 300
			y += (rnd() - .5) * 300
			ps = append(ps, x, y)
			line = append(line, pt{x, y})
		}
		conns = append(conns, conn("", ps...))
		lines = append(lines, line)
	}
	d := board(conns...)
	got := find(t, d, crossgaps.Options{})

	type want struct {
		lower, upper string
		at           pt
	}
	var wants []want
	for i := range lines {
		for j := i + 1; j < len(lines); j++ {
			A, B := lines[i], lines[j]
			type h struct {
				sa, sb int
				t, sin float64
				at     pt
			}
			var hits []h
			for a := 1; a < len(A); a++ {
				for b := 1; b < len(B); b++ {
					p, q, r, s := A[a-1], A[a], B[b-1], B[b]
					rx, ry, sx, sy := q.x-p.x, q.y-p.y, s.x-r.x, s.y-r.y
					den := rx*sy - ry*sx
					if math.Abs(den) < 1e-9 {
						continue
					}
					tt := ((r.x-p.x)*sy - (r.y-p.y)*sx) / den
					u := ((r.x-p.x)*ry - (r.y-p.y)*rx) / den
					if tt < 0 || tt > 1 || u < 0 || u > 1 {
						continue
					}
					hits = append(hits, h{sa: a, sb: b, t: tt, sin: math.Abs(den) / (math.Hypot(rx, ry) * math.Hypot(sx, sy)), at: pt{p.x + rx*tt, p.y + ry*tt}})
				}
			}
			sort.Slice(hits, func(x, y int) bool {
				if hits[x].sa != hits[y].sa {
					return hits[x].sa < hits[y].sa
				}
				if hits[x].t != hits[y].t {
					return hits[x].t < hits[y].t
				}
				return hits[x].sb < hits[y].sb
			})
			var seen []pt
			for _, hh := range hits {
				ends := []pt{A[0], A[len(A)-1], B[0], B[len(B)-1]}
				skip := false
				for _, e := range append(ends, seen...) {
					if near(e, hh.at, 13.999999) {
						skip = true
					}
				}
				if skip || hh.sin < .35 {
					continue
				}
				seen = append(seen, hh.at)
				wants = append(wants, want{lower: fmt.Sprintf("e%d", i), upper: fmt.Sprintf("e%d", j), at: hh.at})
			}
		}
	}
	if len(wants) < 50 {
		t.Fatalf("only %d crossings to compare", len(wants))
	}
	if len(got) != len(wants) {
		for k := 0; k < len(wants) || k < len(got); k++ {
			var g, w string
			if k < len(got) {
				g = fmt.Sprintf("%s under %s at %v", got[k].lower, got[k].upper, mid(got[k].gap))
			}
			if k < len(wants) {
				w = fmt.Sprintf("%s under %s at %v", wants[k].lower, wants[k].upper, wants[k].at)
			}
			t.Logf("%d: got %s, want %s", k, g, w)
		}
		t.Fatalf("grid found %d crossings, all pairs %d", len(got), len(wants))
	}
	for k, w := range wants {
		if got[k].lower != w.lower || got[k].upper != w.upper || !near(mid(got[k].gap), w.at, 1.5) {
			t.Fatalf("crossing %d: got %s under %s at %v, want %s under %s at %v", k, got[k].lower, got[k].upper, mid(got[k].gap), w.lower, w.upper, w.at)
		}
	}
}

// mesh is n horizontal and n vertical lines crossing everywhere.
func mesh(n int) *d2target.Diagram {
	var conns []d2target.Connection
	size := float64(n*40 + 40)
	for i := 0; i < n; i++ {
		v := float64(i*40 + 20)
		conns = append(conns, conn("", 0, v, size, v), conn("", v, 0, v, size))
	}
	return board(conns...)
}

func TestBigDiagrams(t *testing.T) {
	t.Parallel()
	// 250 connections the way dagre draws a busy diagram: long curves whose
	// boxes all overlap. 200 run side by side; 50 short lines cross a few each.
	var conns []d2target.Connection
	for i := 0; i < 200; i++ {
		y := float64(10 * i)
		conns = append(conns, curve(conn("", 0, y, 300, y+150, 600, y+150, 900, y+300, 1200, y+450, 1500, y+450, 1800, y+600)))
	}
	for k := 0; k < 50; k++ {
		x, y := float64(100+32*k), float64(600+10*k)
		conns = append(conns, conn("", x, y, x, y+60))
	}
	d := board(conns...)
	start := time.Now()
	r := find(t, d, crossgaps.Options{})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %s", elapsed)
	}
	pairs := map[string]bool{}
	for _, f := range r {
		pairs[f.lower+"|"+f.upper] = true
		var n int
		fmt.Sscanf(f.upper, "e%d", &n)
		if n < 200 {
			t.Fatalf("side by side curves cross: %s under %s at %v", f.lower, f.upper, mid(f.gap))
		}
	}
	if len(r) < 100 || len(pairs) != len(r) {
		t.Fatalf("got %d gaps over %d pairs", len(r), len(pairs))
	}

	// 2000 connections crossing a million times stop at MaxGaps.
	d = mesh(1000)
	start = time.Now()
	gaps, err := crossgaps.Find(context.Background(), d, crossgaps.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("mesh took %s", elapsed)
	}
	if n := len(flat(d, gaps)); n != crossgaps.MaxGaps {
		t.Fatalf("mesh got %d gaps, want %d", n, crossgaps.MaxGaps)
	}
}

func TestDeterministic(t *testing.T) {
	t.Parallel()
	d := mesh(40)
	a, err := crossgaps.Find(context.Background(), d, crossgaps.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(flat(d, a)); n != 40*40 {
		t.Fatalf("got %d gaps, want %d", n, 40*40)
	}
	for i := 0; i < 3; i++ {
		b, err := crossgaps.Find(context.Background(), d, crossgaps.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatal("gaps differ between runs")
		}
	}

	// Running out of work keeps what was found by then, the same every time.
	some, err := crossgaps.FindWithWork(context.Background(), d, crossgaps.Options{}, 200)
	if err != nil {
		t.Fatal(err)
	}
	n := len(flat(d, some))
	if n == 0 || n >= 40*40/4 {
		t.Fatalf("limited search got %d gaps", n)
	}
	again, err := crossgaps.FindWithWork(context.Background(), d, crossgaps.Options{}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(some, again) {
		t.Fatal("limited gaps differ between runs")
	}
}

func TestCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := crossgaps.Find(ctx, mesh(10), crossgaps.Options{}); err == nil {
		t.Fatal("canceled search succeeded")
	}
}

func TestRegion(t *testing.T) {
	t.Parallel()
	c := conn("", 10, 20, 110, 220)
	c.DstArrow = d2target.ArrowArrowhead
	x, y, w, h := crossgaps.Region(&c, false)
	aw, ah := d2target.ArrowArrowhead.Dimensions(2)
	pad := math.Max(aw, ah) + 2 + 4
	if x > 10-pad || y > 20-pad || x+w < 110+pad || y+h < 220+pad {
		t.Fatalf("region %v %v %v %v doesn't cover the line and its arrowhead", x, y, w, h)
	}
	if x != math.Floor(x) || w != math.Floor(w) {
		t.Fatalf("region %v %v %v %v isn't whole pixels", x, y, w, h)
	}
}
