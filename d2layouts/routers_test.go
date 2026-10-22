package d2layouts

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
)

// From d2lang/d2#1994: connections from a near shape and from inside a grid.
const (
	nearEdgesSource = `
example-no-near-1: {label: No Near 1}
example-no-near-2: {label: No Near 2}
example-near: {
  label: Near
  near: top-right
}
target
* -> target
`
	gridContainerEdgesSource = `
example-table: {
  grid-rows: 1
  label: Table
  child
}
example-no-table-1: {
  label: No Table 1
  child
}
example-no-table-2: {
  label: No Table 2
  child
}
target
*.* -> target
`
	// From d2lang/d2#1741.
	gridParallelEdgesSource = `
grid-columns: 2
a
b
a -> b
b -> a
a -> b: x
`
	gridGap0Source = `
grid-columns: 3
grid-gap: 0
a; b; c; d; e; f
a -> b
b -> a
a -> f: x
d -> c
`
)

func coreLayoutFor(engine string) d2graph.LayoutGraph {
	switch engine {
	case "elk":
		return d2elklayout.DefaultLayout
	case "tala":
		return func(ctx context.Context, g *d2graph.Graph) error {
			return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{1}, MaxConcurrency: 1})
		}
	default:
		return d2dagrelayout.DefaultLayout
	}
}

func layOutForTest(t *testing.T, source, engine string, router d2graph.RouteEdges) *d2graph.Graph {
	t.Helper()
	g := compileLayoutTestGraph(t, source)
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx := log.WithDefault(context.Background())
	if err := LayoutNested(ctx, g, NestedGraphInfo(g.Root), coreLayoutFor(engine), router); err != nil {
		t.Fatal(err)
	}
	return g
}

// measuredGraph compiles source and measures it, ready to be placed by hand.
func measuredGraph(t *testing.T, source string) *d2graph.Graph {
	t.Helper()
	g := compileLayoutTestGraph(t, source)
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, obj := range g.Objects {
		obj.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
	}
	return g
}

func findEdge(t *testing.T, g *d2graph.Graph, id string) *d2graph.Edge {
	t.Helper()
	for _, e := range g.Edges {
		if e.AbsID() == id {
			return e
		}
	}
	t.Fatalf("no edge %s", id)
	return nil
}

// insideBox reports whether p is inside box shrunk by margin on every side.
func insideBox(p *geo.Point, box *geo.Box, margin float64) bool {
	return p.X > box.TopLeft.X+margin && p.X < box.TopLeft.X+box.Width-margin &&
		p.Y > box.TopLeft.Y+margin && p.Y < box.TopLeft.Y+box.Height-margin
}

func distanceToBox(p *geo.Point, box *geo.Box) float64 {
	dx := math.Max(math.Max(box.TopLeft.X-p.X, 0), p.X-(box.TopLeft.X+box.Width))
	dy := math.Max(math.Max(box.TopLeft.Y-p.Y, 0), p.Y-(box.TopLeft.Y+box.Height))
	return math.Hypot(dx, dy)
}

// assertAvoidsShapes fails if e's route passes through a shape other than its
// endpoints and their ancestors, or does not end at its endpoints.
func assertAvoidsShapes(t *testing.T, g *d2graph.Graph, e *d2graph.Edge) {
	t.Helper()
	if len(e.Route) < 3 {
		t.Errorf("%s is a straight line: %v", e.AbsID(), e.Route)
	}
	if d := distanceToBox(e.Route[0], e.Src.Box); d > 1 {
		t.Errorf("%s starts %v px from its source", e.AbsID(), d)
	}
	if d := distanceToBox(e.Route[len(e.Route)-1], e.Dst.Box); d > 1 {
		t.Errorf("%s ends %v px from its destination", e.AbsID(), d)
	}
	for _, obj := range g.Objects {
		if obj == e.Src || obj == e.Dst || e.Src.IsDescendantOf(obj) || e.Dst.IsDescendantOf(obj) {
			continue
		}
		for i := 1; i < len(e.Route); i++ {
			for step := 0; step <= 100; step++ {
				p := e.Route[i-1].Interpolate(e.Route[i], float64(step)/100)
				if insideBox(p, obj.Box, 2) {
					t.Fatalf("%s passes through %s at %v: %v", e.AbsID(), obj.AbsID(), p, e.Route)
				}
			}
		}
	}
}

func TestDefaultRouterAvoidsShapes(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{"dagre", "elk"} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			g := layOutForTest(t, nearEdgesSource, engine, DefaultRouter)
			assertAvoidsShapes(t, g, findEdge(t, g, "(example-near -> target)[0]"))

			g = layOutForTest(t, gridContainerEdgesSource, engine, DefaultRouter)
			assertAvoidsShapes(t, g, findEdge(t, g, "(example-table.child -> target)[0]"))
		})
	}
}

func TestRouterForCurvesDagreRoutes(t *testing.T) {
	t.Parallel()
	g := layOutForTest(t, nearEdgesSource, "dagre", RouterFor("dagre"))
	e := findEdge(t, g, "(example-near -> target)[0]")
	if !e.IsCurve || len(e.Route) < 4 || (len(e.Route)-1)%3 != 0 {
		t.Fatalf("route from the near shape is not a dagre curve: curve=%v %v", e.IsCurve, e.Route)
	}
	g = layOutForTest(t, nearEdgesSource, "elk", RouterFor("elk"))
	if e := findEdge(t, g, "(example-near -> target)[0]"); e.IsCurve {
		t.Fatal("ELK routes should stay orthogonal")
	}
}

// segmentsOverlap reports whether two axis-parallel segments share more than
// a point.
func segmentsOverlap(a1, a2, b1, b2 *geo.Point) bool {
	overlap := func(p1, p2, q1, q2 float64) bool {
		return math.Min(math.Max(p1, p2), math.Max(q1, q2))-math.Max(math.Min(p1, p2), math.Min(q1, q2)) > 1
	}
	if a1.X == a2.X && b1.X == b2.X && math.Abs(a1.X-b1.X) < 1 {
		return overlap(a1.Y, a2.Y, b1.Y, b2.Y)
	}
	if a1.Y == a2.Y && b1.Y == b2.Y && math.Abs(a1.Y-b1.Y) < 1 {
		return overlap(a1.X, a2.X, b1.X, b2.X)
	}
	return false
}

func TestGridCellParallelEdgesDistinct(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{"dagre", "elk", "tala"} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			g := layOutForTest(t, gridParallelEdgesSource, engine, RouterFor(engine))
			if len(g.Edges) != 3 {
				t.Fatalf("got %d edges", len(g.Edges))
			}
			for i, e := range g.Edges {
				if len(e.Route) < 2 {
					t.Fatalf("%s has no route", e.AbsID())
				}
				for _, other := range g.Edges[i+1:] {
					// Edges in opposite directions are compared end to end.
					start, otherStart := e.Route[0], other.Route[0]
					if other.Src != e.Src {
						otherStart = other.Route[len(other.Route)-1]
					}
					if start.Equals(otherStart) {
						t.Errorf("%s and %s leave %s at the same point %v", e.AbsID(), other.AbsID(), e.Src.AbsID(), start)
					}
					if e.IsCurve {
						continue
					}
					for j := 1; j < len(e.Route); j++ {
						for k := 1; k < len(other.Route); k++ {
							if segmentsOverlap(e.Route[j-1], e.Route[j], other.Route[k-1], other.Route[k]) {
								t.Errorf("%s and %s overlap: %v and %v", e.AbsID(), other.AbsID(), e.Route, other.Route)
							}
						}
					}
				}
			}
		})
	}
}

func assertNoZeroSegments(t *testing.T, e *d2graph.Edge) {
	t.Helper()
	if len(e.Route) < 2 {
		t.Fatalf("%s has route %v", e.AbsID(), e.Route)
	}
	for i := 1; i < len(e.Route); i++ {
		if e.Route[i].Equals(e.Route[i-1]) {
			t.Fatalf("%s has a zero-length segment: %v", e.AbsID(), e.Route)
		}
	}
}

func TestGridGap0AdjacentEdges(t *testing.T) {
	t.Parallel()
	routers := map[string]d2graph.RouteEdges{
		"dagre":    RouterFor("dagre"),
		"elk":      RouterFor("elk"),
		"tala":     RouterFor("tala"),
		"straight": StraightRouter,
	}
	for name, router := range routers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			engine := name
			if name == "straight" {
				engine = "dagre"
			}
			g := layOutForTest(t, gridGap0Source, engine, router)
			for _, e := range g.Edges {
				assertNoZeroSegments(t, e)
			}
		})
	}
}

func TestDefaultRouterFallsBackToStraight(t *testing.T) {
	// Not parallel: replaces routeBestEffort.
	defer func(f func(context.Context, *d2graph.Graph, []*d2graph.Edge, *d2talalayout.BestEffortOptions) ([]*d2graph.Edge, error)) {
		routeBestEffort = f
	}(routeBestEffort)
	routeBestEffort = func(_ context.Context, _ *d2graph.Graph, edges []*d2graph.Edge, _ *d2talalayout.BestEffortOptions) ([]*d2graph.Edge, error) {
		return edges, nil
	}
	g := layOutForTest(t, gridParallelEdgesSource, "dagre", RouterFor("dagre"))
	for _, e := range g.Edges {
		if len(e.Route) != 2 || e.IsCurve {
			t.Fatalf("%s did not fall back to a straight line: %v", e.AbsID(), e.Route)
		}
	}
	labeled := g.Edges[2]
	if labeled.LabelPosition == nil || *labeled.LabelPosition != label.InsideMiddleCenter.String() {
		t.Fatalf("label position = %v", labeled.LabelPosition)
	}

	failure := errors.New("injected")
	routeBestEffort = func(context.Context, *d2graph.Graph, []*d2graph.Edge, *d2talalayout.BestEffortOptions) ([]*d2graph.Edge, error) {
		return nil, failure
	}
	if err := DefaultRouter(context.Background(), g, g.Edges); !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the router's error", err)
	}
}

func TestDefaultRouterPropagatesCancel(t *testing.T) {
	t.Parallel()
	g := layOutForTest(t, gridParallelEdgesSource, "elk", RouterFor("elk"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, router := range []d2graph.RouteEdges{DefaultRouter, RouterFor("dagre"), RouterFor("tala"), StraightRouter} {
		if err := router(ctx, g, g.Edges); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	}
}

func TestStraightRouterSpreadsParallel(t *testing.T) {
	t.Parallel()
	g := measuredGraph(t, "a; b; c\na -> b\nb -> a\na -> b\na -> c\n")
	a, b, c := g.Objects[0], g.Objects[1], g.Objects[2]
	a.TopLeft = geo.NewPoint(0, 0)
	b.TopLeft = geo.NewPoint(300, 0)
	c.TopLeft = geo.NewPoint(0, 300)
	if err := StraightRouter(context.Background(), g, g.Edges); err != nil {
		t.Fatal(err)
	}

	center := a.Center().Y
	spacing := math.Min(maxStraightEdgeSpacing, 0.8*math.Min(a.Height, b.Height)/3)
	for i, e := range g.Edges[:3] {
		if len(e.Route) != 2 || e.Route[0].Y != e.Route[1].Y {
			t.Fatalf("%s is not a horizontal line: %v", e.AbsID(), e.Route)
		}
		want := center + (float64(i)-1)*spacing
		if math.Abs(e.Route[0].Y-want) > 1e-9 {
			t.Errorf("%s is at y %v, want %v", e.AbsID(), e.Route[0].Y, want)
		}
		left, right := e.Route[0].X, e.Route[1].X
		if e.Src == b {
			left, right = right, left
		}
		// Borders are traced half a pixel inside, for the stroke.
		if math.Abs(left-(a.TopLeft.X+a.Width)) > 1 || math.Abs(right-b.TopLeft.X) > 1 {
			t.Errorf("%s does not end at the borders: %v %v, a=%v+%v b=%v", e.AbsID(), *e.Route[0], *e.Route[1], a.TopLeft.X, a.Width, b.TopLeft.X)
		}
	}
	// A single edge stays on the line between the centers.
	single := g.Edges[3]
	if single.Route[0].X != a.Center().X || single.Route[1].X != c.Center().X {
		t.Errorf("a -> c moved off the centers: %v", single.Route)
	}
}

func TestStraightRouterTouchingShapes(t *testing.T) {
	t.Parallel()
	g := measuredGraph(t, "a; b\na -> b\n")
	a, b := g.Objects[0], g.Objects[1]
	a.TopLeft = geo.NewPoint(0, 0)
	// One above the other, their shared border is where both ends are cut.
	b.TopLeft = geo.NewPoint(0, a.Height)
	if err := StraightRouter(context.Background(), g, g.Edges); err != nil {
		t.Fatal(err)
	}
	e := g.Edges[0]
	assertNoZeroSegments(t, e)
	if !e.Route[0].Equals(a.Center()) || e.Route[1].Y != b.TopLeft.Y {
		t.Fatalf("route = %v %v, want from a's center %v to b's border %v", *e.Route[0], *e.Route[1], *a.Center(), b.TopLeft.Y)
	}
}

func TestCurvedRouter(t *testing.T) {
	t.Parallel()
	g := compileLayoutTestGraph(t, "a -> b: x\na -> b\n")
	labeled, plain := g.Edges[0], g.Edges[1]
	percentage := 0.3
	base := func(context.Context, *d2graph.Graph, []*d2graph.Edge) error {
		labeled.Route = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(0, 100), geo.NewPoint(100, 100)}
		position := label.OutsideTopLeft.String()
		labeled.LabelPosition = &position
		labeled.LabelPercentage = &percentage
		plain.Route = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(100, 0)}
		return nil
	}
	if err := CurvedRouter(base)(context.Background(), g, g.Edges); err != nil {
		t.Fatal(err)
	}
	if !labeled.IsCurve || len(labeled.Route) < 4 || (len(labeled.Route)-1)%3 != 0 {
		t.Fatalf("route was not curved: %v", labeled.Route)
	}
	if !labeled.Route[0].Equals(geo.NewPoint(0, 0)) || !labeled.Route[len(labeled.Route)-1].Equals(geo.NewPoint(100, 100)) {
		t.Fatalf("curve moved its ends: %v", labeled.Route)
	}
	if *labeled.LabelPosition != label.InsideMiddleCenter.String() || labeled.LabelPercentage != nil {
		t.Fatalf("label is at %v %v, want the middle of the curve", *labeled.LabelPosition, labeled.LabelPercentage)
	}
	if plain.IsCurve || len(plain.Route) != 2 {
		t.Fatalf("a straight route changed: %v", plain.Route)
	}

	failure := errors.New("injected")
	err := CurvedRouter(func(context.Context, *d2graph.Graph, []*d2graph.Edge) error { return failure })(context.Background(), g, g.Edges)
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v", err)
	}
}

func TestRouterFor(t *testing.T) {
	t.Parallel()
	g := measuredGraph(t, "a; b\na -> b\n")
	g.Objects[0].TopLeft = geo.NewPoint(0, 0)
	g.Objects[1].TopLeft = geo.NewPoint(300, 200)
	for _, engine := range []string{"dagre", "DAGRE", "elk", "tala", "custom"} {
		g.Edges[0].Route, g.Edges[0].IsCurve = nil, false
		if err := RouterFor(engine)(context.Background(), g, g.Edges); err != nil {
			t.Fatal(err)
		}
		if curved := g.Edges[0].IsCurve; curved != strings.EqualFold(engine, "dagre") {
			t.Errorf("%s: curve = %v", engine, curved)
		}
	}
}

// From the review of the dagre curves: edges into and out of a grid, a
// sequence diagram and a near shape, whose plain curves cut through bob and e.
const curvesAroundShapesSource = `
g: {
  grid-columns: 3
  t: {shape: sql_table; id: int {constraint: primary_key}; name: string}
  c: {x; y}
  d
  e
  f
  t -> d: label
  d -> t
  e -> f
  c.x -> f
}
seq: {
  shape: sequence_diagram
  alice -> bob: hi
}
legend: {near: bottom-right; k: key}
outside
mid1; mid2
outside -> mid1 -> mid2
outside -> g.e
seq.alice -> outside
legend -> g.t
seq.bob -> g.c.y
`

// shapesEntered returns the shapes, other than e's ends and their
// containers, that a sampled point of e's route is inside, curve or not.
func shapesEntered(g *d2graph.Graph, e *d2graph.Edge) map[string]bool {
	var points []*geo.Point
	if e.IsCurve {
		for i := 0; i+3 < len(e.Route); i += 3 {
			bezier := geo.NewBezierCurve(e.Route[i : i+4])
			for s := 0; s <= 100; s++ {
				points = append(points, bezier.At(float64(s)/100))
			}
		}
	} else {
		for i := 1; i < len(e.Route); i++ {
			for s := 0; s <= 100; s++ {
				points = append(points, e.Route[i-1].Interpolate(e.Route[i], float64(s)/100))
			}
		}
	}
	entered := map[string]bool{}
	for _, obj := range g.Objects {
		if e.Src.IsDescendantOf(obj) || e.Dst.IsDescendantOf(obj) {
			continue
		}
		for _, p := range points {
			if insideBox(p, obj.Box, 1) {
				entered[obj.AbsID()] = true
				break
			}
		}
	}
	return entered
}

func TestRouterForDagreCurvesEnterNoNewShape(t *testing.T) {
	t.Parallel()
	polylines := layOutForTest(t, curvesAroundShapesSource, "dagre", DefaultRouter)
	curves := layOutForTest(t, curvesAroundShapesSource, "dagre", RouterFor("dagre"))
	curved := 0
	for _, id := range []string{"(outside -> g.e)[0]", "(seq.alice -> outside)[0]", "(legend -> g.t)[0]", "(seq.bob -> g.c.y)[0]"} {
		polyline, curve := findEdge(t, polylines, id), findEdge(t, curves, id)
		if curve.IsCurve {
			curved++
		}
		allowed := shapesEntered(polylines, polyline)
		for obj := range shapesEntered(curves, curve) {
			if !allowed[obj] {
				t.Errorf("the curve of %s enters %s, which its route went around: %v", id, obj, curve.Route)
			}
		}
	}
	if curved == 0 {
		t.Fatal("no route was curved")
	}
}

func TestCurvedRouterKeepsCornersNearShapes(t *testing.T) {
	t.Parallel()
	g := measuredGraph(t, "a; b; o\na -> b\n")
	a, b, o := g.Objects[0], g.Objects[1], g.Objects[2]
	a.TopLeft, a.Width, a.Height = geo.NewPoint(-100, 140), 100, 40
	b.TopLeft, b.Width, b.Height = geo.NewPoint(140, -100), 40, 100
	// o sits inside the route's corner at (160, 160), 2 px from both legs:
	// a curve rounding that corner would cut through o.
	o.TopLeft, o.Width, o.Height = geo.NewPoint(100, 100), 58, 58
	e := g.Edges[0]
	route := []*geo.Point{geo.NewPoint(0, 160), geo.NewPoint(160, 160), geo.NewPoint(160, 0)}
	base := func(context.Context, *d2graph.Graph, []*d2graph.Edge) error {
		e.Route = append([]*geo.Point(nil), route...)
		e.IsCurve = false
		return nil
	}
	if err := CurvedRouter(base)(context.Background(), g, g.Edges); err != nil {
		t.Fatal(err)
	}
	if e.IsCurve || len(e.Route) != 3 {
		t.Fatalf("the route was curved through o: %v", e.Route)
	}
	// Away from o, the same corner is rounded.
	o.TopLeft = geo.NewPoint(-300, -300)
	if err := CurvedRouter(base)(context.Background(), g, g.Edges); err != nil {
		t.Fatal(err)
	}
	if !e.IsCurve {
		t.Fatalf("the route was not curved: %v", e.Route)
	}
}

func TestIsInvisibleRelay(t *testing.T) {
	t.Parallel()
	g := compileLayoutTestGraph(t, `
hidden: "" {style.opacity: 0}
relay: "" {width: 1; height: 1; style: {fill: transparent; stroke-width: 0}}
clear: "" {style: {fill: transparent; stroke: transparent}}
labeled: {style.opacity: 0}
nofill: "" {style.stroke-width: 0}
stroked: "" {style.fill: transparent}
parent: "" {style.opacity: 0; child}
plain
`)
	want := map[string]bool{"hidden": true, "relay": true, "clear": true}
	for _, obj := range g.Objects {
		if got := isInvisibleRelay(obj); got != want[obj.AbsID()] {
			t.Errorf("isInvisibleRelay(%s) = %v", obj.AbsID(), got)
		}
	}
}

// From real_world/jupyter_k8s_oidc: a chain of edges bent through an
// invisible relay shape in a grid.
const relaySource = `
grid-rows: 1
a
m
b
relay: "" {width: 1; height: 1; style: {fill: transparent; stroke-width: 0}}
d
a -> relay -> d
a -> b
`

func TestDefaultRouterKeepsRelayEdgesStraight(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{"dagre", "elk", "tala"} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			g := layOutForTest(t, relaySource, engine, RouterFor(engine))
			relay := findEdge(t, g, "(relay -> d)[0]")
			for _, e := range []*d2graph.Edge{findEdge(t, g, "(a -> relay)[0]"), relay} {
				if len(e.Route) != 2 || e.IsCurve {
					t.Errorf("%s is not straight: %v", e.AbsID(), e.Route)
				}
			}
			if e := findEdge(t, g, "(a -> b)[0]"); len(e.Route) < 3 {
				t.Errorf("%s was not routed: %v", e.AbsID(), e.Route)
			}
		})
	}
}
