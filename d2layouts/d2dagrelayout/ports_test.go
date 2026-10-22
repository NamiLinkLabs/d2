package d2dagrelayout

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
)

func layoutForPorts(t *testing.T, source string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("", strings.NewReader(source), &d2compiler.CompileOptions{Context: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := DefaultLayout(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	return g
}

// curveSamples samples D2's cubic curve route format.
func curveSamples(route []*geo.Point) []*geo.Point {
	var out []*geo.Point
	for i := 0; i+3 < len(route); i += 3 {
		p0, c1, c2, p3 := route[i], route[i+1], route[i+2], route[i+3]
		for s := 0; s <= 20; s++ {
			t := float64(s) / 20
			u := 1 - t
			out = append(out, geo.NewPoint(
				u*u*u*p0.X+3*u*u*t*c1.X+3*u*t*t*c2.X+t*t*t*p3.X,
				u*u*u*p0.Y+3*u*u*t*c1.Y+3*u*t*t*c2.Y+t*t*t*p3.Y,
			))
		}
	}
	return out
}

func requireOutside(t *testing.T, e *d2graph.Edge, obj *d2graph.Object) {
	t.Helper()
	inner := geo.NewBox(geo.NewPoint(obj.TopLeft.X+2, obj.TopLeft.Y+2), obj.Width-4, obj.Height-4)
	for _, p := range curveSamples(e.Route) {
		if inner.Contains(p) {
			t.Errorf("%s passes through %s at %v\n%s", e.AbsID(), obj.AbsID(), p, dagreRouteString(e))
			return
		}
	}
}

func dagreRouteString(e *d2graph.Edge) string {
	var b strings.Builder
	for _, p := range e.Route {
		fmt.Fprintf(&b, "(%v,%v) ", p.X, p.Y)
	}
	return fmt.Sprintf("src %v %vx%v dst %v %vx%v route %s", e.Src.TopLeft, e.Src.Width, e.Src.Height, e.Dst.TopLeft, e.Dst.Width, e.Dst.Height, b.String())
}

func TestEdgePorts(t *testing.T) {
	sides := []string{"top", "right", "bottom", "left"}
	var sources []string
	for _, s := range sides {
		for _, d := range sides {
			sources = append(sources, fmt.Sprintf("a -> b: {source-port: %s; target-port: %s}", s, d))
		}
	}
	sources = append(sources,
		"a <- b: {source-port: bottom; target-port: right}",
		"direction: right\na -> b: {source-port: bottom; target-port: top}",
		"a -> b -> c -> d\na -> d: {source-port: left; target-port: left}",
		"a: {shape: circle}\nb: {shape: diamond}\na -> b: {source-port: right; target-port: left}",
		"x: {a -> b}\nc\nx.a -> c: {source-port: left; target-port: right}",
		"x: {a}\ny: {b}\nx -> y: {source-port: right; target-port: top}",
		"a -> b: {target-port: left}",
	)
	for _, source := range sources {
		t.Run(strings.ReplaceAll(source, "\n", "; "), func(t *testing.T) {
			g := layoutForPorts(t, source)
			e := g.Edges[len(g.Edges)-1]
			if !e.IsCurve || (len(e.Route)-1)%3 != 0 {
				t.Fatalf("route is not a cubic curve: %d points", len(e.Route))
			}
			if err := e.PortsHonored(); err != nil {
				t.Errorf("%v\n%s", err, dagreRouteString(e))
			}
			requireOutside(t, e, e.Src)
			requireOutside(t, e, e.Dst)
		})
	}
}

func TestEdgePortsSpreadOnOneSide(t *testing.T) {
	g := layoutForPorts(t, `a -> b: {source-port: bottom}
a -> c: {source-port: bottom}
a -> d: {source-port: bottom}
`)
	a := g.Objects[0]
	var xs []float64
	for _, e := range g.Edges {
		if err := e.PortsHonored(); err != nil {
			t.Errorf("%v\n%s", err, dagreRouteString(e))
		}
		xs = append(xs, e.Route[0].X)
	}
	for i := 1; i < len(xs); i++ {
		if xs[i] == xs[i-1] {
			t.Errorf("two pinned ends start at x=%v: %v", xs[i], xs)
		}
	}
	for _, x := range xs {
		if x < a.TopLeft.X+a.Width/4-1 || x > a.TopLeft.X+a.Width*3/4+1 {
			t.Errorf("pinned end at x=%v is outside the middle half of a (%v..%v)", x, a.TopLeft.X, a.TopLeft.X+a.Width)
		}
	}
}

func TestEdgePortsLeaveOtherEdgesAlone(t *testing.T) {
	plain := layoutForPorts(t, "a -> b\nc -> d")
	pinned := layoutForPorts(t, "a -> b\nc -> d: {source-port: right}")
	got, want := pinned.Edges[0].Route, plain.Edges[0].Route
	if len(got) != len(want) {
		t.Fatalf("unpinned edge changed: %v vs %v", got, want)
	}
	for i := range got {
		if *got[i] != *want[i] {
			t.Fatalf("unpinned edge changed at %d: %v vs %v", i, got[i], want[i])
		}
	}
}

// TestEdgePortsAvoidOtherShapes checks that a pinned route the stubs cannot
// join with an L or a Z goes around the shapes in the way instead of through
// them: hub -> m4 leaves hub's bottom, where dagre puts hub2 right below.
func TestEdgePortsAvoidOtherShapes(t *testing.T) {
	g := layoutForPorts(t, `hub -> m4: {source-port: bottom; target-port: bottom}
hub -> m5: {source-port: left; target-port: left}
hub -> hub2: {source-port: top; target-port: bottom}
hub2 -> hub: {source-port: right; target-port: right}
m1 -> m2
x -> hub
hub -> y
`)
	for _, e := range g.Edges {
		if !e.HasPort() {
			continue
		}
		if err := e.PortsHonored(); err != nil {
			t.Errorf("%v\n%s", err, dagreRouteString(e))
		}
		for _, obj := range g.Objects {
			if obj != e.Src && obj != e.Dst {
				requireOutside(t, e, obj)
			}
		}
	}
}
