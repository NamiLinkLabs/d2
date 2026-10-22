package d2talalayout

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

// placedGraph compiles source and places its objects, which must not be
// containers, on a grid of the given width, 200 px apart.
func placedGraph(t *testing.T, source string, columns int) *d2graph.Graph {
	t.Helper()
	g := compileD2ForPins(t, source)
	for i, obj := range g.Objects {
		obj.TopLeft = geo.NewPoint(float64(i%columns)*200, float64(i/columns)*200)
	}
	return g
}

func routeStrings(edges []*d2graph.Edge) []string {
	routes := make([]string, len(edges))
	for i, e := range edges {
		var b strings.Builder
		for _, p := range e.Route {
			fmt.Fprintf(&b, "(%v,%v)", p.X, p.Y)
		}
		if e.LabelPosition != nil {
			fmt.Fprintf(&b, " %s", *e.LabelPosition)
		}
		routes[i] = b.String()
	}
	return routes
}

func straightRoute(e *d2graph.Edge) []*geo.Point {
	return []*geo.Point{e.Src.Center(), e.Dst.Center()}
}

const bestEffortSource = `
a; b; c
d; e; f
a -> b: one
b -> f
d -> c: two
e -> a
c -> e
`

func TestRouteEdgesBestEffortMatchesRouteEdges(t *testing.T) {
	t.Parallel()
	// Up to 10 edges are routed together around the others, as RouteEdges
	// does, so the routes are the same.
	setup := func() (*d2graph.Graph, []*d2graph.Edge) {
		g := placedGraph(t, bestEffortSource, 3)
		if err := RouteEdges(t.Context(), g, g.Edges); err != nil {
			t.Fatal(err)
		}
		selected := g.Edges[2:]
		for _, e := range selected {
			e.Route = nil
		}
		return g, selected
	}
	want, wantEdges := setup()
	if err := RouteEdges(t.Context(), want, wantEdges); err != nil {
		t.Fatal(err)
	}
	got, gotEdges := setup()
	unrouted, err := RouteEdgesBestEffort(t.Context(), got, gotEdges, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrouted) != 0 {
		t.Fatalf("unrouted = %v", unrouted)
	}
	if w, g := routeStrings(want.Edges), routeStrings(got.Edges); !reflect.DeepEqual(w, g) {
		t.Fatalf("routes differ from RouteEdges:\ngot  %q\nwant %q", g, w)
	}
}

func TestRouteEdgesBestEffortRoutesPairsPastTen(t *testing.T) {
	t.Parallel()
	source := `
a; b; c; d
e; f; g; h
a -> b: 1
a -> b: 2
b -> a: 3
a -> f
b -> g
c -> h
d -> e
e -> c
f -> d
g -> a
h -> b
`
	route := func() (*d2graph.Graph, []string) {
		g := placedGraph(t, source, 4)
		unrouted, err := RouteEdgesBestEffort(t.Context(), g, g.Edges, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(unrouted) != 0 {
			t.Fatalf("unrouted = %v", unrouted)
		}
		return g, routeStrings(g.Edges)
	}
	g, first := route()
	if len(g.Edges) <= maxEdgesRoutedTogether {
		t.Fatalf("test needs more than %d edges, has %d", maxEdgesRoutedTogether, len(g.Edges))
	}
	for _, e := range g.Edges {
		if len(e.Route) < 2 {
			t.Fatalf("%s has route %v", e.AbsID(), e.Route)
		}
	}
	// Edges between the same two shapes still get distinct routes.
	parallel := map[string]bool{}
	for _, r := range first[:3] {
		parallel[strings.Split(r, " ")[0]] = true
	}
	if len(parallel) != 3 {
		t.Fatalf("edges between a and b share routes: %q", first[:3])
	}
	_, second := route()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("routes are not deterministic:\n%q\n%q", first, second)
	}
}

func TestRouteEdgesBestEffortReturnsUnroutedEdges(t *testing.T) {
	t.Parallel()
	// The budget runs out after the first pair, and only the second pass
	// runs with more than 10 edges.
	g := placedGraph(t, bestEffortSource+"a -> a\n"+strings.Repeat("b -> f\n", 8), 3)
	previous := make([][]*geo.Point, len(g.Edges))
	for i, e := range g.Edges {
		e.Route = straightRoute(e)
		previous[i] = e.Route
	}
	order := append([]*d2graph.Edge(nil), g.Edges...)
	unrouted, err := RouteEdgesBestEffort(t.Context(), g, g.Edges, &BestEffortOptions{TotalPairWork: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Edges, order) {
		t.Fatal("g.Edges was not restored")
	}
	// a -> b is the first pair.
	routed := g.Edges[0]
	if len(unrouted) != len(g.Edges)-1 || unrouted[0] != g.Edges[1] {
		t.Fatalf("unrouted = %v, want every edge but %s", unrouted, routed.AbsID())
	}
	for i, e := range g.Edges[1:] {
		if !reflect.DeepEqual(e.Route, previous[i+1]) {
			t.Fatalf("unrouted %s has route %v, want its previous route %v", e.AbsID(), e.Route, previous[i+1])
		}
	}
	if reflect.DeepEqual(routed.Route, previous[0]) {
		t.Fatalf("%s was not routed", routed.AbsID())
	}
}

func TestRouteEdgesBestEffortSkipsSelfLoops(t *testing.T) {
	t.Parallel()
	g := placedGraph(t, "a; b\na -> a\na -> b\n", 2)
	loop := g.Edges[0]
	unrouted, err := RouteEdgesBestEffort(t.Context(), g, g.Edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrouted) != 1 || unrouted[0] != loop || loop.Route != nil {
		t.Fatalf("unrouted = %v with route %v, want only the self-loop, unchanged", unrouted, loop.Route)
	}
	if len(g.Edges[1].Route) < 2 {
		t.Fatalf("a -> b was not routed: %v", g.Edges[1].Route)
	}
}

func TestRouteEdgesBestEffortToleratesUnusableRoutes(t *testing.T) {
	t.Parallel()
	g := placedGraph(t, bestEffortSource, 3)
	// RouteEdges rejects a graph with a zero-length route, such as the stub
	// between two touching grid cells.
	stub := g.Edges[0]
	stub.Route = []*geo.Point{stub.Src.Center(), stub.Src.Center()}
	selected := g.Edges[1:3]
	if err := RouteEdges(t.Context(), g, selected); err == nil {
		t.Fatal("RouteEdges accepted a zero-length route")
	}
	unrouted, err := RouteEdgesBestEffort(t.Context(), g, selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrouted) != 0 {
		t.Fatalf("unrouted = %v", unrouted)
	}
	if len(stub.Route) != 2 || !stub.Route[0].Equals(stub.Route[1]) {
		t.Fatalf("the unselected route changed to %v", stub.Route)
	}
	// A placeholder on an edge to route is replaced.
	stub.Route = []*geo.Point{stub.Src.Center(), stub.Src.Center()}
	if unrouted, err := RouteEdgesBestEffort(t.Context(), g, []*d2graph.Edge{stub}, nil); err != nil || len(unrouted) != 0 {
		t.Fatalf("unrouted = %v, err = %v", unrouted, err)
	}
	if stub.Route[0].Equals(stub.Route[len(stub.Route)-1]) {
		t.Fatalf("the placeholder route was kept: %v", stub.Route)
	}
}

func TestRouteEdgesBestEffortErrors(t *testing.T) {
	t.Parallel()
	g := placedGraph(t, bestEffortSource, 3)
	var noContext context.Context
	if _, err := RouteEdgesBestEffort(noContext, g, g.Edges, nil); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := RouteEdgesBestEffort(t.Context(), nil, nil, nil); err == nil {
		t.Fatal("nil graph accepted")
	}
	if _, err := RouteEdgesBestEffort(t.Context(), g, []*d2graph.Edge{{}}, nil); err == nil {
		t.Fatal("an edge outside the graph was accepted")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, e := range g.Edges {
		e.Route = straightRoute(e)
	}
	before := routeStrings(g.Edges)
	if _, err := RouteEdgesBestEffort(ctx, g, g.Edges, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if after := routeStrings(g.Edges); !reflect.DeepEqual(before, after) {
		t.Fatalf("a canceled call changed routes:\n%q\n%q", before, after)
	}
}

func TestRouteEdgesBestEffortLargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("routes 16 edges one at a time on 120 shapes")
	}
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "n%d\n", i)
	}
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, "n%d -> n%d\n", (i*37)%120, (i*53+71)%120)
	}
	g := placedGraph(t, b.String(), 12)
	unrouted, err := RouteEdgesBestEffort(t.Context(), g, g.Edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrouted) != 0 {
		t.Fatalf("%d of %d edges unrouted", len(unrouted), len(g.Edges))
	}

	// The total budget leaves the rest unrouted once it runs out.
	for _, e := range g.Edges {
		e.Route = nil
	}
	unrouted, err = RouteEdgesBestEffort(t.Context(), g, g.Edges, &BestEffortOptions{TotalPairWork: 10_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(unrouted) == 0 || len(unrouted) == len(g.Edges) {
		t.Fatalf("%d of %d edges unrouted, want some", len(unrouted), len(g.Edges))
	}
}

const pairsSource = `
a; b; c; d
e; f; g; h
a -> b: 1
a -> b: 2
b -> a: 3
a -> f
b -> g
c -> h
d -> e
e -> c
f -> d
g -> a
h -> b
`

// routeWithBudget routes every edge of source, placed on a grid of columns
// columns, with opts and a budget of units, and returns the routes, the
// edges left unrouted and what was spent.
func routeWithBudget(t *testing.T, source string, columns int, units uint64, opts BestEffortOptions) ([]string, []string, uint64) {
	t.Helper()
	g := placedGraph(t, source, columns)
	opts.Budget = NewWorkBudget(units)
	unrouted, err := RouteEdgesBestEffort(t.Context(), g, g.Edges, &opts)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range unrouted {
		ids = append(ids, e.AbsID())
	}
	return routeStrings(g.Edges), ids, opts.Budget.Spent()
}

func TestRouteEdgesBestEffortWorkBudget(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{"together": bestEffortSource, "pairs": pairsSource} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := placedGraph(t, source, 4)
			if _, err := RouteEdgesBestEffort(t.Context(), g, g.Edges, nil); err != nil {
				t.Fatal(err)
			}
			unbudgeted := routeStrings(g.Edges)

			// Below the budget, the routes are those without one.
			routes, unrouted, need := routeWithBudget(t, source, 4, 1<<50, BestEffortOptions{})
			if len(unrouted) != 0 || !reflect.DeepEqual(routes, unbudgeted) {
				t.Fatalf("with an ample budget, unrouted %v and routes\n%q\nwant\n%q", unrouted, routes, unbudgeted)
			}
			if need == 0 {
				t.Fatal("routing spent nothing")
			}
			// Routing together may use three quarters of the budget, and its
			// route flavors share what building the routing graph leaves equally,
			// so twice what it spent is enough, every time, and gives the same
			// routes for the same work.
			for range 3 {
				enough := 2 * need
				routes, unrouted, spent := routeWithBudget(t, source, 4, enough, BestEffortOptions{})
				if spent != need || len(unrouted) != 0 || !reflect.DeepEqual(routes, unbudgeted) {
					t.Fatalf("budget %d: spent %d of %d, unrouted %v", enough, spent, need, unrouted)
				}
			}

			// With less, the rest are left as they were, the same ones every time.
			routes, unrouted, spent := routeWithBudget(t, source, 4, need/3, BestEffortOptions{})
			if len(unrouted) == 0 {
				t.Fatalf("a third of the budget routed every edge")
			}
			if spent < need/3 {
				t.Fatalf("spent %d of a budget of %d and left %v unrouted", spent, need/3, unrouted)
			}
			for range 3 {
				again, againUnrouted, againSpent := routeWithBudget(t, source, 4, need/3, BestEffortOptions{})
				if !reflect.DeepEqual(again, routes) || !reflect.DeepEqual(againUnrouted, unrouted) || againSpent != spent {
					t.Fatalf("a third of the budget is not deterministic:\n%q %v %d\n%q %v %d", routes, unrouted, spent, again, againUnrouted, againSpent)
				}
			}
			for i, r := range routes {
				if r != "" && name == "pairs" && r != unbudgeted[i] {
					// Pairs are routed independently, so those that fit keep their routes.
					t.Errorf("edge %d routed as %q within a smaller budget, %q without", i, r, unbudgeted[i])
				}
			}

			// A spent budget routes nothing.
			routes, unrouted, _ = routeWithBudget(t, source, 4, 0, BestEffortOptions{})
			if len(unrouted) != len(routes) {
				t.Fatalf("an empty budget left %d of %d edges unrouted", len(unrouted), len(routes))
			}
		})
	}
}

func TestRouteEdgesBestEffortSharesWorkBudget(t *testing.T) {
	t.Parallel()
	g := placedGraph(t, bestEffortSource, 3)
	budget := NewWorkBudget(1 << 50)
	if _, err := RouteEdgesBestEffort(t.Context(), g, g.Edges[:2], &BestEffortOptions{Budget: budget}); err != nil {
		t.Fatal(err)
	}
	first := budget.Spent()
	if first == 0 || budget.Remaining() != 1<<50-first {
		t.Fatalf("spent %d, %d left", first, budget.Remaining())
	}
	// A second call spends from what the first left, and none of it once it
	// is gone.
	empty := NewWorkBudget(first)
	g = placedGraph(t, bestEffortSource, 3)
	if _, err := RouteEdgesBestEffort(t.Context(), g, g.Edges[:2], &BestEffortOptions{Budget: empty}); err != nil {
		t.Fatal(err)
	}
	if empty.Remaining() != 0 {
		t.Fatalf("%d left after a call that spent the whole budget", empty.Remaining())
	}
	unrouted, err := RouteEdgesBestEffort(t.Context(), g, g.Edges[2:], &BestEffortOptions{Budget: empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(unrouted) != len(g.Edges)-2 {
		t.Fatalf("a spent budget left %d of %d edges unrouted", len(unrouted), len(g.Edges)-2)
	}
}

func TestRouteEdgesBestEffortPrefer(t *testing.T) {
	t.Parallel()
	_, _, need := routeWithBudget(t, pairsSource, 4, 1<<50, BestEffortOptions{})
	preferred := func(e *d2graph.Edge) bool {
		return e.Dst.ID == "b" && e.Src.ID == "h" || e.Dst.ID == "a" && e.Src.ID == "g"
	}
	routes, unrouted, _ := routeWithBudget(t, pairsSource, 4, need/4, BestEffortOptions{Prefer: preferred})
	if len(unrouted) == 0 {
		t.Fatal("a quarter of the budget routed every edge")
	}
	for _, id := range unrouted {
		if id == "(h -> b)[0]" || id == "(g -> a)[0]" {
			t.Fatalf("preferred %s was left unrouted while %d other edges were routed: %v", id, len(routes)-len(unrouted), unrouted)
		}
	}
	_, plain, _ := routeWithBudget(t, pairsSource, 4, need/4, BestEffortOptions{})
	if !slices.Contains(plain, "(h -> b)[0]") {
		t.Fatalf("without a preference, h -> b was routed too: unrouted %v", plain)
	}
}
