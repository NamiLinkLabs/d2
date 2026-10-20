package bench

import (
	"math"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func pts(coords ...float64) []*geo.Point {
	out := make([]*geo.Point, 0, len(coords)/2)
	for i := 0; i+1 < len(coords); i += 2 {
		out = append(out, geo.NewPoint(coords[i], coords[i+1]))
	}
	return out
}

func conn(id string, curve bool, coords ...float64) d2target.Connection {
	return d2target.Connection{ID: id, Route: pts(coords...), IsCurve: curve}
}

func shape(id string, level, x, y, w, h int) d2target.Shape {
	return d2target.Shape{ID: id, Level: level, Pos: d2target.Point{X: x, Y: y}, Width: w, Height: h}
}

func TestCrossings(t *testing.T) {
	cases := []struct {
		name  string
		conns []d2target.Connection
		want  int
	}{
		{"x", []d2target.Connection{conn("a", false, 0, 0, 100, 100), conn("b", false, 0, 100, 100, 0)}, 1},
		{"shared endpoint", []d2target.Connection{conn("a", false, 0, 0, 100, 100), conn("b", false, 0, 0, 100, 0)}, 0},
		{"parallel", []d2target.Connection{conn("a", false, 0, 0, 100, 0), conn("b", false, 0, 10, 100, 10)}, 0},
		{"collinear overlap", []d2target.Connection{conn("a", false, 0, 0, 100, 0), conn("b", false, 50, 0, 150, 0)}, 0},
		// b's corner lies exactly on a: found on two of b's segments, counted once.
		{"through vertex", []d2target.Connection{conn("a", false, 0, 50, 100, 50), conn("b", false, 50, 0, 50, 50, 50, 100)}, 1},
		// One orthogonal route crossing another twice.
		{"double", []d2target.Connection{conn("a", false, 0, 50, 200, 50), conn("b", false, 50, 0, 50, 100, 150, 100, 150, 0)}, 2},
		{"same connection", []d2target.Connection{conn("a", false, 0, 0, 100, 100, 100, 0, 0, 100)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Measure(&d2target.Diagram{Connections: tc.conns})
			if m.Crossings != tc.want {
				t.Fatalf("got %d crossings, want %d", m.Crossings, tc.want)
			}
		})
	}
}

func TestBendsAndLength(t *testing.T) {
	cases := []struct {
		name   string
		c      d2target.Connection
		bends  int
		length float64
	}{
		{"straight", conn("a", false, 0, 0, 30, 40), 0, 50},
		{"straight with redundant vertex", conn("a", false, 0, 0, 50, 0, 100, 0), 0, 100},
		{"L", conn("a", false, 0, 0, 100, 0, 100, 100), 1, 200},
		{"U", conn("a", false, 0, 0, 100, 0, 100, 100, 0, 100), 2, 300},
		{"duplicate points", conn("a", false, 0, 0, 100, 0, 100, 0, 100, 100), 1, 200},
		// A single cubic quarter turn and an S-curve made of two cubics.
		{"curve quarter", conn("a", true, 0, 0, 55, 0, 100, 45, 100, 100), 1, -1},
		{"curve S", conn("a", true, 0, 0, 50, 0, 50, 50, 50, 50, 50, 50, 50, 100, 100, 100), 2, -1},
		{"curve straight", conn("a", true, 0, 0, 10, 0, 20, 0, 30, 0), 0, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Measure(&d2target.Diagram{Connections: []d2target.Connection{tc.c}})
			if m.Bends != tc.bends {
				t.Fatalf("got %d bends, want %d", m.Bends, tc.bends)
			}
			if tc.length >= 0 && math.Abs(m.EdgeLength-tc.length) > 1e-6 {
				t.Fatalf("got length %v, want %v", m.EdgeLength, tc.length)
			}
		})
	}
}

func TestCurveFlatteningEndsOnRoute(t *testing.T) {
	c := conn("a", true, 0, 0, 10, 0, 20, 10, 30, 30, 40, 50, 60, 60, 70, 60)
	poly := RoutePolyline(c)
	if len(poly) != 1+2*curveSamples {
		t.Fatalf("got %d samples", len(poly))
	}
	if poly[0] != (geo.Point{X: 0, Y: 0}) || poly[curveSamples] != (geo.Point{X: 30, Y: 30}) || poly[len(poly)-1] != (geo.Point{X: 70, Y: 60}) {
		t.Fatalf("flattened curve does not pass through its on-curve points: %v", poly)
	}
}

func TestEmptyArea(t *testing.T) {
	d := &d2target.Diagram{Shapes: []d2target.Shape{
		shape("a", 1, 0, 0, 100, 100),
		shape("b", 1, 200, 0, 100, 100),
		// Nested shapes do not count again: their area is inside their parent.
		shape("a.c", 2, 10, 10, 20, 20),
	}}
	m := Measure(d)
	if m.BBoxWidth != 300 || m.BBoxHeight != 100 {
		t.Fatalf("bbox %dx%d", m.BBoxWidth, m.BBoxHeight)
	}
	if m.EmptyArea != 10000 || math.Abs(m.EmptyFraction-1.0/3) > 1e-9 {
		t.Fatalf("empty %v (%v)", m.EmptyArea, m.EmptyFraction)
	}
}

func TestDisplace(t *testing.T) {
	before := &d2target.Diagram{Shapes: []d2target.Shape{
		shape("a", 1, 0, 0, 10, 10),
		shape("b", 1, 100, 0, 10, 10),
		shape("c", 1, 200, 0, 10, 10),
		shape("gone", 1, 300, 0, 10, 10),
	}}
	after := &d2target.Diagram{Shapes: []d2target.Shape{
		shape("a", 1, 3, 4, 10, 10),
		shape("b", 1, 100, 0, 10, 10),
		// Excluded endpoint and a new shape are ignored.
		shape("c", 1, 900, 900, 10, 10),
		shape("new", 1, 0, 0, 10, 10),
	}}
	d := Displace(before, after, map[string]bool{"c": true})
	if d.Compared != 2 || d.Moved != 1 || d.Max != 5 || d.Mean != 2.5 {
		t.Fatalf("got %+v", d)
	}
}

func TestEditHelpers(t *testing.T) {
	src := "a -> b\n" + EditDirective + "a -> c\n"
	edit, ok := EditFromSource(src)
	if !ok || edit != "a -> c" {
		t.Fatalf("got %q %v", edit, ok)
	}
	if got := ApplyEdit("a", edit); got != "a\na -> c\n" {
		t.Fatalf("got %q", got)
	}
}
