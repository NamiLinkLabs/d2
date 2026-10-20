//go:build js && wasm

package d2wasm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

type testBox struct{ x, y, w, h float64 }

func (b testBox) shape(id string) RouteShape {
	return RouteShape{ID: id, X: &b.x, Y: &b.y, Width: &b.w, Height: &b.h}
}

func routeShapes(boxes map[string]testBox) []RouteShape {
	shapes := make([]RouteShape, 0, len(boxes))
	for id, b := range boxes {
		shapes = append(shapes, b.shape(id))
	}
	return shapes
}

// functionalTimeoutMs is the budget for tests that check what gets routed, not how
// fast. The default budget is wall-clock time, and a busy CI machine (make runs
// builds and tests in parallel) can need more than it. Tests about the budget
// itself set TimeoutMs explicitly.
const functionalTimeoutMs = 60000.0

func invokeRouteEdges(t *testing.T, request any) (*RouteEdgesResponse, *WASMError) {
	t.Helper()
	if r, ok := request.(RouteEdgesRequest); ok && r.TimeoutMs == nil {
		v := functionalTimeoutMs
		r.TimeoutMs = &v
		request = r
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	call := wrapWASMCall(RouteEdges)
	defer call.Release()
	got := call.Invoke(string(encoded)).String()
	var response struct {
		Data  *RouteEdgesResponse `json:"data"`
		Error *WASMError          `json:"error"`
	}
	if err := json.Unmarshal([]byte(got), &response); err != nil {
		t.Fatalf("invalid response %q: %v", got, err)
	}
	if response.Error != nil {
		return nil, response.Error
	}
	if response.Data == nil {
		t.Fatalf("response has neither data nor error: %s", got)
	}
	return response.Data, nil
}

func mustRoute(t *testing.T, source string, boxes map[string]testBox, edges ...string) *RouteEdgesResponse {
	t.Helper()
	response, werr := invokeRouteEdges(t, RouteEdgesRequest{
		FS:     map[string]string{"index": source},
		Shapes: routeShapes(boxes),
		Edges:  edges,
	})
	if werr != nil {
		t.Fatalf("routeEdges() error = %d %s", werr.Code, werr.Message)
	}
	for _, id := range edges {
		if msg, failed := response.Errors[id]; failed {
			t.Fatalf("edge %s was not routed: %s", id, msg)
		}
		if len(response.Routes[id]) < 2 {
			t.Fatalf("edge %s route = %v, want at least two points", id, response.Routes[id])
		}
	}
	return response
}

// crossesInterior reports whether segment a-b passes through the inside of
// box shrunk by margin, using Liang-Barsky clipping.
func crossesInterior(a, b RoutePoint, box testBox, margin float64) bool {
	x0, y0 := box.x+margin, box.y+margin
	x1, y1 := box.x+box.w-margin, box.y+box.h-margin
	if x0 >= x1 || y0 >= y1 {
		return false
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	t0, t1 := 0.0, 1.0
	for _, edge := range [][2]float64{{-dx, a.X - x0}, {dx, x1 - a.X}, {-dy, a.Y - y0}, {dy, y1 - a.Y}} {
		p, q := edge[0], edge[1]
		if p == 0 {
			if q <= 0 {
				return false
			}
			continue
		}
		r := q / p
		if p < 0 {
			t0 = math.Max(t0, r)
		} else {
			t1 = math.Min(t1, r)
		}
		if t0 >= t1 {
			return false
		}
	}
	return true
}

func routeCrosses(route []RoutePoint, box testBox) bool {
	for i := 1; i < len(route); i++ {
		if crossesInterior(route[i-1], route[i], box, 1) {
			return true
		}
	}
	return false
}

func onBorder(p RoutePoint, box testBox) bool {
	const tolerance = 1.5
	inOuter := p.X >= box.x-tolerance && p.X <= box.x+box.w+tolerance && p.Y >= box.y-tolerance && p.Y <= box.y+box.h+tolerance
	inInner := p.X > box.x+tolerance && p.X < box.x+box.w-tolerance && p.Y > box.y+tolerance && p.Y < box.y+box.h-tolerance
	return inOuter && !inInner
}

func assertEnds(t *testing.T, id string, route []RoutePoint, src, dst testBox) {
	t.Helper()
	if !onBorder(route[0], src) {
		t.Errorf("%s starts at %v, want a point on the source outline %v", id, route[0], src)
	}
	if last := route[len(route)-1]; !onBorder(last, dst) {
		t.Errorf("%s ends at %v, want a point on the destination outline %v", id, last, dst)
	}
}

func TestRouteEdgesAvoidsBoxInTheWay(t *testing.T) {
	boxes := map[string]testBox{
		"a": {0, 0, 100, 60},
		"b": {500, 0, 100, 60},
		"c": {220, -60, 160, 180},
	}
	const id = "(a -> b)[0]"
	response := mustRoute(t, "a -> b\nc\n", boxes, id)
	route := response.Routes[id]
	if routeCrosses(route, boxes["c"]) {
		t.Fatalf("route %v crosses the box in the way %v", route, boxes["c"])
	}
	assertEnds(t, id, route, boxes["a"], boxes["b"])
	for i := 1; i < len(route); i++ {
		if route[i].X != route[i-1].X && route[i].Y != route[i-1].Y {
			t.Fatalf("route %v has a diagonal segment", route)
		}
	}
	if len(response.Errors) != 0 {
		t.Fatalf("errors = %v", response.Errors)
	}
}

func TestRouteEdgesEndsTouchSourceAndTarget(t *testing.T) {
	boxes := map[string]testBox{
		"a": {0, 0, 120, 60},
		"b": {300, 260, 80, 80},
		"c": {-200, 300, 100, 50},
	}
	ids := []string{"(a -> b)[0]", "(a <- c)[0]", "(b -- c)[0]"}
	response := mustRoute(t, "a -> b\na <- c\nb -- c\nb: {shape: circle}\n", boxes, ids...)
	assertEnds(t, ids[0], response.Routes[ids[0]], boxes["a"], boxes["b"])
	// A reversed arrow still runs from the D2 source (a) to the destination (c).
	assertEnds(t, ids[1], response.Routes[ids[1]], boxes["a"], boxes["c"])
	// Circles are entered on their outline, which stays inside the bounding box.
	route := response.Routes[ids[2]]
	center := RoutePoint{X: 340, Y: 300}
	if d := math.Hypot(route[0].X-center.X, route[0].Y-center.Y); math.Abs(d-40) > 2 {
		t.Fatalf("%s starts %v from the circle's centre, want its radius 40", ids[2], d)
	}
	if last := route[len(route)-1]; !onBorder(last, boxes["c"]) {
		t.Fatalf("%s ends at %v, want c's outline", ids[2], last)
	}
}

func TestRouteEdgesCrossesContainerOnlyToReachInside(t *testing.T) {
	boxes := map[string]testBox{
		"x":   {0, 100, 80, 60},
		"y":   {640, 100, 80, 60},
		"g":   {220, 20, 240, 220},
		"g.q": {300, 100, 80, 60},
	}
	through, into := "(x -> y)[0]", "(x -> g.q)[0]"
	response := mustRoute(t, "x -> y\ng: {q}\nx -> g.q\n", boxes, through, into)
	if route := response.Routes[through]; routeCrosses(route, boxes["g"]) {
		t.Fatalf("%s route %v crosses container g %v though neither end is inside it", through, route, boxes["g"])
	}
	assertEnds(t, through, response.Routes[through], boxes["x"], boxes["y"])
	route := response.Routes[into]
	assertEnds(t, into, route, boxes["x"], boxes["g.q"])
	if !routeCrosses(route, boxes["g"]) {
		t.Fatalf("%s route %v never enters container g", into, route)
	}
}

func TestRouteEdgesMovedBoxInsideContainer(t *testing.T) {
	boxes := map[string]testBox{
		"g":   {0, 0, 760, 340},
		"g.a": {40, 140, 80, 60},
		"g.c": {320, 80, 120, 180},
		// b was dragged to the far side of c.
		"g.b": {620, 140, 80, 60},
	}
	const id = "g.(a -> b)[0]"
	response := mustRoute(t, "g: {\n  a -> b\n  c\n}\n", boxes, id)
	route := response.Routes[id]
	assertEnds(t, id, route, boxes["g.a"], boxes["g.b"])
	if routeCrosses(route, boxes["g.c"]) {
		t.Fatalf("route %v crosses g.c %v", route, boxes["g.c"])
	}
	g := boxes["g"]
	for _, p := range route {
		if p.X < g.x || p.X > g.x+g.w || p.Y < g.y || p.Y > g.y+g.h {
			t.Fatalf("route %v leaves its container %v", route, g)
		}
	}
}

func TestRouteEdgesReturnsLabelPlacement(t *testing.T) {
	boxes := map[string]testBox{"a": {0, 0, 100, 60}, "b": {0, 300, 100, 60}}
	const id = "(a -> b)[0]"
	response := mustRoute(t, "a -> b: a long label\n", boxes, id)
	l, ok := response.Labels[id]
	if !ok || l.Position == "" || l.Percentage < 0 || l.Percentage > 1 {
		t.Fatalf("labels = %#v, want a placement for %s", response.Labels, id)
	}
}

func TestRouteEdgesKeepsOtherConnectionsRoutes(t *testing.T) {
	boxes := map[string]testBox{
		"a": {0, 0, 100, 60},
		"b": {500, 0, 100, 60},
		"c": {0, 200, 100, 60},
		"d": {500, 200, 100, 60},
	}
	x := func(v float64) *float64 { return &v }
	request := RouteEdgesRequest{
		FS:     map[string]string{"index": "a -> b\nc -> d: kept\n"},
		Shapes: routeShapes(boxes),
		Edges:  []string{"(a -> b)[0]"},
		Connections: []RouteConnection{
			{ID: "(c -> d)[0]", Route: []*RouteInputPoint{{x(100), x(230)}, {x(100), x(230)}, {x(500), x(230)}}},
			// Hints that cannot be used are skipped, not reported.
			{ID: "(nope -> nada)[0]", Route: []*RouteInputPoint{{x(0), x(0)}, {x(1), x(0)}}},
			{ID: "(a -> b)[0]", Route: []*RouteInputPoint{{x(0), x(0)}, {nil, x(0)}}},
		},
	}
	response, werr := invokeRouteEdges(t, request)
	if werr != nil {
		t.Fatalf("routeEdges() error = %s", werr.Message)
	}
	if _, ok := response.Routes["(c -> d)[0]"]; ok || len(response.Routes) != 1 {
		t.Fatalf("routes = %v, want only the requested connection", response.Routes)
	}
	assertEnds(t, "(a -> b)[0]", response.Routes["(a -> b)[0]"], boxes["a"], boxes["b"])
}

func TestRouteEdgesRejectsInvalidRequests(t *testing.T) {
	source := "a -> b\ng: {q}\n"
	boxes := func() []RouteShape {
		return routeShapes(map[string]testBox{
			"a": {0, 0, 100, 60}, "b": {300, 0, 100, 60}, "g": {0, 200, 200, 200}, "g.q": {50, 250, 80, 60},
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*RouteEdgesRequest)
		want   string
	}{
		{"unknown edge", func(r *RouteEdgesRequest) { r.Edges = []string{"(a -> z)[0]"} }, `unknown edge id "(a -> z)[0]"`},
		{"repeated edge", func(r *RouteEdgesRequest) { r.Edges = append(r.Edges, r.Edges[0]) }, "listed more than once"},
		{"unknown shape", func(r *RouteEdgesRequest) { r.Shapes = append(r.Shapes, testBox{0, 0, 1, 1}.shape("zz")) }, `unknown shape id "zz"`},
		{"missing shape", func(r *RouteEdgesRequest) { r.Shapes = r.Shapes[1:] }, "missing geometry for shape"},
		{"repeated shape", func(r *RouteEdgesRequest) { r.Shapes = append(r.Shapes, r.Shapes[0]) }, "listed more than once"},
		{"NaN", func(r *RouteEdgesRequest) { r.Shapes[0].X = nil }, "x must be a finite number"},
		{"negative size", func(r *RouteEdgesRequest) { w := -5.0; r.Shapes[0].Width = &w }, "width and height must be positive"},
		{"zero size", func(r *RouteEdgesRequest) { h := 0.0; r.Shapes[0].Height = &h }, "width and height must be positive"},
		{"huge coordinate", func(r *RouteEdgesRequest) { y := 2e9; r.Shapes[0].Y = &y }, "out of range"},
		{"bad label position", func(r *RouteEdgesRequest) { p := "SOMEWHERE"; r.Shapes[0].LabelPosition = &p }, "unknown labelPosition"},
		{"bad timeout", func(r *RouteEdgesRequest) { v := -1.0; r.TimeoutMs = &v }, "timeoutMs"},
		{"missing fs", func(r *RouteEdgesRequest) { r.FS = nil }, "missing 'fs'"},
		{"missing input", func(r *RouteEdgesRequest) { p := "other"; r.InputPath = &p }, "missing 'other' file"},
		{"syntax error", func(r *RouteEdgesRequest) { r.FS["index"] = "a -> {" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := RouteEdgesRequest{
				FS:     map[string]string{"index": source},
				Shapes: boxes(),
				Edges:  []string{"(a -> b)[0]"},
			}
			tc.mutate(&request)
			_, werr := invokeRouteEdges(t, request)
			if werr == nil || werr.Code != 400 || !strings.Contains(werr.Message, tc.want) {
				t.Fatalf("error = %#v, want a 400 containing %q", werr, tc.want)
			}
		})
	}

	// A JavaScript NaN or Infinity arrives as null.
	_, werr := invokeRouteEdges(t, json.RawMessage(`{"fs":{"index":"a"},"shapes":[{"id":"a","x":null,"y":0,"width":10,"height":10}],"edges":["(a -> a)[0]"]}`))
	if werr == nil || werr.Code != 400 {
		t.Fatalf("null coordinate error = %#v, want 400", werr)
	}
	call := wrapWASMCall(RouteEdges)
	defer call.Release()
	assertWASMError(t, call.Invoke().String(), "missing JSON argument", 400)
	assertWASMError(t, call.Invoke("{").String(), "invalid JSON input", 400)
}

func withTALARouter(t *testing.T, router func(context.Context, *d2graph.Graph, []*d2graph.Edge) error) {
	t.Helper()
	original := routeWithTALA
	routeWithTALA = router
	t.Cleanup(func() { routeWithTALA = original })
}

func TestRouteEdgesRetriesEdgeByEdge(t *testing.T) {
	var calls []int
	withTALARouter(t, func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		calls = append(calls, len(edges))
		if len(edges) > 1 {
			return errors.New("TALA EdgeRouting work exceeds limit")
		}
		if edges[0].AbsID() == "(a -> c)[0]" {
			return errors.New("no route for a -> c")
		}
		return realTALARouter(ctx, g, edges)
	})
	boxes := map[string]testBox{"a": {0, 0, 100, 60}, "b": {300, 0, 100, 60}, "c": {0, 300, 100, 60}, "d": {300, 300, 100, 60}}
	ids := []string{"(a -> b)[0]", "(a -> c)[0]", "(c -> d)[0]"}
	response, werr := invokeRouteEdges(t, RouteEdgesRequest{
		FS:     map[string]string{"index": "a -> b\na -> c\nc -> d\n"},
		Shapes: routeShapes(boxes),
		Edges:  ids,
	})
	if werr != nil {
		t.Fatalf("routeEdges() error = %s", werr.Message)
	}
	if len(response.Routes) != 2 || len(response.Routes[ids[0]]) < 2 || len(response.Routes[ids[2]]) < 2 {
		t.Fatalf("routes = %v, want routes for %s and %s", response.Routes, ids[0], ids[2])
	}
	if response.Errors[ids[1]] != "no route for a -> c" || len(response.Errors) != 1 {
		t.Fatalf("errors = %v, want only %s to fail", response.Errors, ids[1])
	}
	// The set once, then each edge on its own.
	if fmt.Sprint(calls) != "[3 1 1 1]" {
		t.Fatalf("TALA calls = %v", calls)
	}
}

func TestRouteEdgesRoutesEdgesAloneWhenTheSetRunsLong(t *testing.T) {
	var withOthers []int
	withTALARouter(t, func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		withOthers = append(withOthers, len(g.Edges)-len(edges))
		if len(edges) > 1 {
			if _, ok := ctx.Deadline(); !ok {
				return errors.New("routing all edges together has no deadline")
			}
			// As if the first pass used up its share of the budget.
			return fmt.Errorf("EdgeRouting: %w", context.DeadlineExceeded)
		}
		return realTALARouter(ctx, g, edges)
	})
	boxes := map[string]testBox{"a": {0, 0, 100, 60}, "b": {300, 0, 100, 60}, "c": {0, 300, 100, 60}}
	x := func(v float64) *float64 { return &v }
	response, werr := invokeRouteEdges(t, RouteEdgesRequest{
		FS:     map[string]string{"index": "a -> b\na -> c\nb -> c\n"},
		Shapes: routeShapes(boxes),
		Edges:  []string{"(a -> b)[0]", "(a -> c)[0]"},
		Connections: []RouteConnection{
			{ID: "(b -> c)[0]", Route: []*RouteInputPoint{{x(350), x(60)}, {x(350), x(330)}, {x(100), x(330)}}},
		},
	})
	if werr != nil {
		t.Fatalf("routeEdges() error = %s", werr.Message)
	}
	if len(response.Routes) != 2 || len(response.Errors) != 0 {
		t.Fatalf("response = %#v, want both edges routed on their own", response)
	}
	// The first pass keeps b -> c; the second routes each edge without it.
	if fmt.Sprint(withOthers) != "[1 0 0]" {
		t.Fatalf("edges besides the routed ones per TALA call = %v", withOthers)
	}
}

func TestSimplifyRoute(t *testing.T) {
	route := []*geo.Point{{X: 0, Y: 0}, {X: 50, Y: 0.5}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 101, Y: 150}, {X: 100, Y: 200}}
	got := simplifyRoute(route, 2)
	if fmt.Sprint(got) != fmt.Sprint([]*geo.Point{route[0], route[2], route[5]}) {
		t.Fatalf("simplifyRoute() = %v", got)
	}
	loop := []*geo.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 0}}
	if got := simplifyRoute(loop, 2); len(got) != 3 {
		t.Fatalf("simplifyRoute() collapsed a loop to %v", got)
	}
}

// realTALARouter is the real router; tests that stub routeWithTALA call through it.
var realTALARouter = routeWithTALA

func TestRouteEdgesReportsTimeoutPerEdge(t *testing.T) {
	withTALARouter(t, func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		return fmt.Errorf("tala edge routing stopped: %w", context.DeadlineExceeded)
	})
	boxes := map[string]testBox{"a": {0, 0, 100, 60}, "b": {300, 0, 100, 60}}
	timeout := 250.0
	response, werr := invokeRouteEdges(t, RouteEdgesRequest{
		FS:        map[string]string{"index": "a -> b\nb -> a\n"},
		Shapes:    routeShapes(boxes),
		Edges:     []string{"(a -> b)[0]", "(b -> a)[0]"},
		TimeoutMs: &timeout,
	})
	if werr != nil {
		t.Fatalf("routeEdges() error = %s", werr.Message)
	}
	if len(response.Routes) != 0 || len(response.Errors) != 2 {
		t.Fatalf("response = %#v, want both edges to time out", response)
	}
	for id, msg := range response.Errors {
		if !strings.Contains(msg, "250 ms budget") {
			t.Fatalf("error for %s = %q", id, msg)
		}
	}
}

func TestRouteEdgesTinyBudgetDoesNotFail(t *testing.T) {
	var source strings.Builder
	boxes := map[string]testBox{}
	var ids []string
	for i := range 40 {
		fmt.Fprintf(&source, "n%d -> n%d\n", i, (i+7)%40)
		boxes[fmt.Sprintf("n%d", i)] = testBox{float64(i%8) * 200, float64(i/8) * 160, 100, 60}
		ids = append(ids, fmt.Sprintf("(n%d -> n%d)[0]", i, (i+7)%40))
	}
	timeout := 1.0
	response, werr := invokeRouteEdges(t, RouteEdgesRequest{
		FS:        map[string]string{"index": source.String()},
		Shapes:    routeShapes(boxes),
		Edges:     ids,
		TimeoutMs: &timeout,
	})
	if werr != nil {
		t.Fatalf("routeEdges() error = %d %s", werr.Code, werr.Message)
	}
	if len(response.Routes)+len(response.Errors) != len(ids) {
		t.Fatalf("got %d routes and %d errors, want one of them for each of %d edges", len(response.Routes), len(response.Errors), len(ids))
	}
}

func TestRouteEdgesRoutesManyEdgesOneAtATime(t *testing.T) {
	var sizes []int
	withTALARouter(t, func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		sizes = append(sizes, len(edges))
		return realTALARouter(ctx, g, edges)
	})
	var source strings.Builder
	boxes := map[string]testBox{}
	var ids []string
	for i := range maxEdgesRoutedTogether + 2 {
		fmt.Fprintf(&source, "s%d -> t%d\n", i, i)
		boxes[fmt.Sprintf("s%d", i)] = testBox{0, float64(i) * 100, 80, 50}
		boxes[fmt.Sprintf("t%d", i)] = testBox{400, float64(i) * 100, 80, 50}
		ids = append(ids, fmt.Sprintf("(s%d -> t%d)[0]", i, i))
	}
	response := mustRoute(t, source.String(), boxes, ids...)
	if len(response.Routes) != len(ids) {
		t.Fatalf("routed %d of %d edges", len(response.Routes), len(ids))
	}
	for _, n := range sizes {
		if n != 1 {
			t.Fatalf("TALA call sizes = %v, want every edge routed on its own", sizes)
		}
	}
}

func TestRouteEdgesReportsSelfLoops(t *testing.T) {
	boxes := map[string]testBox{"a": {0, 0, 100, 60}, "b": {300, 0, 100, 60}}
	response, werr := invokeRouteEdges(t, RouteEdgesRequest{
		FS:     map[string]string{"index": "a -> a\na -> b\n"},
		Shapes: routeShapes(boxes),
		Edges:  []string{"(a -> a)[0]", "(a -> b)[0]"},
	})
	if werr != nil {
		t.Fatalf("routeEdges() error = %s", werr.Message)
	}
	if !strings.Contains(response.Errors["(a -> a)[0]"], "from a shape to itself") || len(response.Errors) != 1 {
		t.Fatalf("errors = %v, want only the self-loop to fail", response.Errors)
	}
	assertEnds(t, "(a -> b)[0]", response.Routes["(a -> b)[0]"], boxes["a"], boxes["b"])
}
