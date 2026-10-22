package d2graph_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func portGraph(t *testing.T, source string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Place every object in a row, 100x60 boxes 200 apart.
	for i, obj := range g.Objects {
		obj.Box = geo.NewBox(geo.NewPoint(float64(i*200), 0), 100, 60)
	}
	return g
}

func TestPortSideOrientation(t *testing.T) {
	for value, want := range map[string]geo.Orientation{"top": geo.Top, "RIGHT": geo.Right, "Bottom": geo.Bottom, "left": geo.Left} {
		got, ok := d2graph.PortSideOrientation(value)
		if !ok || got != want {
			t.Errorf("PortSideOrientation(%q) = %v, %v", value, got, ok)
		}
	}
	if _, ok := d2graph.PortSideOrientation("middle"); ok {
		t.Error("middle is not a side")
	}
}

func TestPortAnchor(t *testing.T) {
	g := portGraph(t, "a\nb: {shape: circle}")
	a, b := g.Objects[0], g.Objects[1]
	for _, tc := range []struct {
		side  geo.Orientation
		along float64
		want  geo.Point
	}{
		{geo.Top, 0.5, geo.Point{X: 50, Y: 0}},
		{geo.Bottom, 0.25, geo.Point{X: 25, Y: 60}},
		{geo.Left, 0.5, geo.Point{X: 0, Y: 30}},
		{geo.Right, 1, geo.Point{X: 100, Y: 60}},
	} {
		if got := a.PortAnchor(tc.side, tc.along); *got != tc.want {
			t.Errorf("rectangle %v@%v = %v, want %v", tc.side, tc.along, got, tc.want)
		}
	}
	// On a circle (an ellipse in a 100x60 box) an off-center anchor is traced
	// down from the bounding box onto the outline.
	got := b.PortAnchor(geo.Top, 0.25)
	if got.X != 225 || got.Y <= 0 || got.Y >= 30 {
		t.Errorf("circle top@0.25 = %v, want x=225 below the box top", got)
	}
	if got := b.PortAnchor(geo.Top, 0.5); got.X != 250 || got.Y != 0 {
		t.Errorf("circle top@0.5 = %v, want (250, 0)", got)
	}
}

func TestPortAnchorsSpreadsOneSide(t *testing.T) {
	g := portGraph(t, `hub
c -> hub: {target-port: top}
a -> hub: {target-port: top}
b -> hub: {target-port: top}
hub -> c: {source-port: left}
`)
	anchors := d2graph.PortAnchors(g.Edges)
	// Objects sit in declaration order: hub, c, a, b. The three ends on top of
	// hub are ordered by their sources' x: c, a, b.
	var xs []float64
	for _, id := range []string{"c", "a", "b"} {
		for _, e := range g.Edges {
			if e.Src.ID == id && e.Dst.ID == "hub" {
				xs = append(xs, anchors[d2graph.PortEnd{Edge: e, Src: false}].X)
			}
		}
	}
	if len(xs) != 3 || xs[0] != 25 || xs[1] != 50 || xs[2] != 75 {
		t.Errorf("top anchors = %v, want [25 50 75]", xs)
	}
	if got := anchors[d2graph.PortEnd{Edge: g.Edges[3], Src: true}]; got == nil || *got != (geo.Point{X: 0, Y: 30}) {
		t.Errorf("lone left anchor = %v, want (0, 30)", got)
	}
	if len(anchors) != 4 {
		t.Errorf("got %d anchors, want 4", len(anchors))
	}
}

func TestPinRoute(t *testing.T) {
	// a is at x 0..100, b at 200..300, both y 0..60.
	g := portGraph(t, "a -> b: {source-port: bottom; target-port: top}")
	e := g.Edges[0]
	anchors := d2graph.PortAnchors(g.Edges)
	straight := []*geo.Point{geo.NewPoint(100, 30), geo.NewPoint(150, 30), geo.NewPoint(200, 30)}
	e.Route = e.PinRoute(straight, anchors[d2graph.PortEnd{Edge: e, Src: true}], anchors[d2graph.PortEnd{Edge: e, Src: false}])
	if err := e.PortsHonored(); err != nil {
		t.Fatalf("%v: %v", err, e.Route)
	}
	if *e.Route[0] != (geo.Point{X: 50, Y: 60}) || *e.Route[len(e.Route)-1] != (geo.Point{X: 250, Y: 0}) {
		t.Errorf("ends = %v, %v", e.Route[0], e.Route[len(e.Route)-1])
	}
	// The interior point (150, 30) is behind both stubs and is dropped; b's
	// top stub is above a's bottom stub, so the route detours around b.
	inner := geo.NewBox(geo.NewPoint(201, 1), 98, 58)
	for i := 1; i < len(e.Route); i++ {
		if len(inner.Intersections(*geo.NewSegment(e.Route[i-1], e.Route[i]))) > 0 {
			t.Errorf("segment %d %v -> %v cuts through b", i, e.Route[i-1], e.Route[i])
		}
	}
}

func TestPortsHonored(t *testing.T) {
	g := portGraph(t, "a -> b: {source-port: right}")
	e := g.Edges[0]
	e.Route = []*geo.Point{geo.NewPoint(100, 30), geo.NewPoint(200, 30)}
	if err := e.PortsHonored(); err != nil {
		t.Fatalf("straight right exit rejected: %v", err)
	}
	e.Route = []*geo.Point{geo.NewPoint(50, 60), geo.NewPoint(50, 100), geo.NewPoint(200, 30)}
	if err := e.PortsHonored(); err == nil || !strings.Contains(err.Error(), "not on that side") {
		t.Fatalf("bottom exit for a right pin: %v", err)
	}
	e.Route = []*geo.Point{geo.NewPoint(100, 30), geo.NewPoint(150, 0), geo.NewPoint(200, 30)}
	if err := e.PortsHonored(); err == nil || !strings.Contains(err.Error(), "straight out") {
		t.Fatalf("diagonal exit for a right pin: %v", err)
	}
	e.SrcPort = nil
	if err := e.PortsHonored(); err != nil {
		t.Fatalf("unpinned edge: %v", err)
	}
}

func TestPinRouteJoinsStubsSquare(t *testing.T) {
	g := portGraph(t, "a -> b: {source-port: right; target-port: left}\nc")
	a, b, c := g.Objects[0], g.Objects[1], g.Objects[2]
	b.TopLeft = geo.NewPoint(300, 200)
	// c sits where the horizontal-first L would run.
	c.TopLeft = geo.NewPoint(150, 0)
	c.Width, c.Height = 40, 60
	e := g.Edges[0]
	anchors := d2graph.PortAnchors(g.Edges)
	e.Route = e.PinRoute([]*geo.Point{a.Center(), b.Center()}, anchors[d2graph.PortEnd{Edge: e, Src: true}], anchors[d2graph.PortEnd{Edge: e, Src: false}])
	if err := e.PortsHonored(); err != nil {
		t.Fatalf("%v: %v", err, e.Route)
	}
	for i := 1; i < len(e.Route); i++ {
		p, q := e.Route[i-1], e.Route[i]
		if p.X != q.X && p.Y != q.Y {
			t.Errorf("segment %v -> %v is diagonal: %v", p, q, e.Route)
		}
		inner := geo.NewBox(geo.NewPoint(c.TopLeft.X+1, c.TopLeft.Y+1), c.Width-2, c.Height-2)
		if len(inner.Intersections(*geo.NewSegment(p, q))) > 0 {
			t.Errorf("segment %v -> %v crosses c: %v", p, q, e.Route)
		}
	}
}

// assertNoHairpin fails when a segment of route runs back over the one before
// it.
func assertNoHairpin(t *testing.T, route []*geo.Point) {
	t.Helper()
	for i := 2; i < len(route); i++ {
		a, b, c := route[i-2], route[i-1], route[i]
		if (b.X-a.X)*(c.X-b.X)+(b.Y-a.Y)*(c.Y-b.Y) < 0 {
			t.Errorf("route doubles back at %v: %v", b, route)
		}
	}
}

func TestPinRouteFacingNarrowGap(t *testing.T) {
	// Adjacent grid cells 40px apart: a right -> b left (offset diagonally)
	// and c bottom -> d top (straight across).
	g := portGraph(t, "a -> b: {source-port: right; target-port: left}\nc -> d: {source-port: bottom; target-port: top}")
	a, b, c, d := g.Objects[0], g.Objects[1], g.Objects[2], g.Objects[3]
	a.TopLeft = geo.NewPoint(0, 0)
	b.TopLeft = geo.NewPoint(140, 106)
	c.TopLeft = geo.NewPoint(400, 0)
	d.TopLeft = geo.NewPoint(400, 100)
	anchors := d2graph.PortAnchors(g.Edges)
	for _, e := range g.Edges {
		e.Route = e.PinRoute([]*geo.Point{e.Src.Center(), e.Dst.Center()}, anchors[d2graph.PortEnd{Edge: e, Src: true}], anchors[d2graph.PortEnd{Edge: e, Src: false}])
		if err := e.PortsHonored(); err != nil {
			t.Fatalf("%v: %v", err, e.Route)
		}
		assertNoHairpin(t, e.Route)
		for i := 1; i < len(e.Route); i++ {
			p, q := e.Route[i-1], e.Route[i]
			if p.X != q.X && p.Y != q.Y {
				t.Errorf("segment %v -> %v is diagonal: %v", p, q, e.Route)
			}
		}
	}
	// a's right side is at x 100 and b's left at 140: the stubs meet at 120.
	if r := g.Edges[0].Route; r[1].X != 120 || r[len(r)-2].X != 120 {
		t.Errorf("a -> b stubs do not meet in the gap: %v", r)
	}
	if r := g.Edges[1].Route; len(r) != 3 || r[0].X != 450 || r[2].X != 450 {
		t.Errorf("c -> d is not a straight line: %v", r)
	}
}
