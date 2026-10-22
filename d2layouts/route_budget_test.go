package d2layouts

import (
	"context"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/lib/geo"
)

// budgetSource has edges routed in four calls: from the near shape, out of
// the grid, between grid cells and out of the sequence diagram.
const budgetSource = `
legend: {near: top-right; l1; l2; l1 -> l2}
g: {
  grid-columns: 2
  a; b; c; d
  a -> d
  c -> b
}
seq: {
  shape: sequence_diagram
  alice -> bob
}
x; y; z
x -> y -> z
legend -> z
g.a -> z
seq.bob -> x
g.c -> y
`

func TestRouteWorkBudget(t *testing.T) {
	t.Parallel()
	if got := routeWorkBudget(0); got != 2_500_000 {
		t.Fatalf("budget of an empty diagram = %d", got)
	}
	if got := routeWorkBudget(150); got != 4_300_000 {
		t.Fatalf("budget of 150 shapes = %d", got)
	}
}

func TestLayoutNestedSharesRouteBudget(t *testing.T) {
	// Not parallel: replaces routeBestEffort.
	defer func(f func(context.Context, *d2graph.Graph, []*d2graph.Edge, *d2talalayout.BestEffortOptions) ([]*d2graph.Edge, error)) {
		routeBestEffort = f
	}(routeBestEffort)
	route := routeBestEffort
	var budgets []*d2talalayout.WorkBudget
	var initial uint64
	routeBestEffort = func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge, opts *d2talalayout.BestEffortOptions) ([]*d2graph.Edge, error) {
		if len(budgets) == 0 {
			initial = opts.Budget.Remaining()
		}
		budgets = append(budgets, opts.Budget)
		return route(ctx, g, edges, opts)
	}

	g := compileLayoutTestGraph(t, budgetSource)
	shapes := len(g.Objects)
	layOutForTest(t, budgetSource, "elk", DefaultRouter)
	if len(budgets) < 2 {
		t.Fatalf("routed in %d calls, want several", len(budgets))
	}
	for _, b := range budgets {
		if b != budgets[0] {
			t.Fatal("the calls of one layout got different budgets")
		}
	}
	if initial != routeWorkBudget(shapes) {
		t.Fatalf("budget of %d shapes = %d, want %d", shapes, initial, routeWorkBudget(shapes))
	}
	first := budgets[0]
	budgets = nil
	layOutForTest(t, budgetSource, "elk", DefaultRouter)
	if budgets[0] == first {
		t.Fatal("a second layout reused the first one's budget")
	}

	// Called on its own, the router gets a budget for its graph alone.
	budgets = nil
	g = layOutForTest(t, "a; b; c\na -> b -> c\na -> c\n", "elk", StraightRouter)
	if err := DefaultRouter(context.Background(), g, g.Edges[2:]); err != nil {
		t.Fatal(err)
	}
	if len(budgets) != 1 || initial != routeWorkBudget(len(g.Objects)) {
		t.Fatalf("a call on its own got %d budgets of %d", len(budgets), initial)
	}
}

// routesOf returns the routes of g's edges.
func routesOf(g *d2graph.Graph) [][]geo.Point {
	routes := make([][]geo.Point, len(g.Edges))
	for i, e := range g.Edges {
		for _, p := range e.Route {
			routes[i] = append(routes[i], *p)
		}
	}
	return routes
}

func TestDefaultRouterBudgetFallsBackDeterministically(t *testing.T) {
	// Not parallel: changes the budget.
	base, perShape := routeWorkBase, routeWorkPerShape
	defer func() { routeWorkBase, routeWorkPerShape = base, perShape }()
	for _, engine := range []string{"dagre", "elk"} {
		// The edges routed are those not drawn as StraightRouter draws them.
		straight := routesOf(layOutForTest(t, budgetSource, engine, StraightRouter))
		routed := func(g *d2graph.Graph) (n int) {
			for i, route := range routesOf(g) {
				if !reflect.DeepEqual(route, straight[i]) {
					n++
				}
			}
			return n
		}

		// Small diagrams fit in the default budget: a bigger one changes
		// nothing.
		for _, source := range []string{nearEdgesSource, gridContainerEdgesSource, gridParallelEdgesSource, gridGap0Source} {
			want := routesOf(layOutForTest(t, source, engine, RouterFor(engine)))
			routeWorkBase = 1 << 40
			if got := routesOf(layOutForTest(t, source, engine, RouterFor(engine))); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: a bigger budget changed the routes of\n%s", engine, source)
			}
			routeWorkBase = base
		}

		routeWorkBase = 1 << 40
		full := layOutForTest(t, budgetSource, engine, RouterFor(engine))
		all := routed(full)
		if all < 4 {
			t.Fatalf("%s: an unlimited budget routed only %d edges", engine, all)
		}

		// A small one routes some and draws the rest straight, the same way
		// every time.
		routeWorkBase, routeWorkPerShape = 60_000, 0
		small := layOutForTest(t, budgetSource, engine, RouterFor(engine))
		if n := routed(small); n == 0 || n >= all {
			t.Fatalf("%s: a small budget routed %d of the %d edges the default routes", engine, n, all)
		}
		for range 3 {
			if again := layOutForTest(t, budgetSource, engine, RouterFor(engine)); !reflect.DeepEqual(routesOf(again), routesOf(small)) {
				t.Fatalf("%s: a small budget gave different routes on another run", engine)
			}
		}

		// None routes nothing, and every edge it leaves is straight.
		routeWorkBase = 0
		none := layOutForTest(t, budgetSource, engine, RouterFor(engine))
		if n := routed(none); n != 0 {
			t.Fatalf("%s: an empty budget routed %d edges", engine, n)
		}
		routeWorkBase, routeWorkPerShape = base, perShape
	}
}

func TestStraightLineCrossesShape(t *testing.T) {
	t.Parallel()
	g := measuredGraph(t, "a; b; c\nbox: {inner}\na -> c\na -> b\nbox.inner -> a\n")
	place := map[string][2]float64{"a": {0, 0}, "b": {200, 0}, "c": {400, 0}, "box": {0, 200}, "box.inner": {20, 220}}
	for _, obj := range g.Objects {
		p := place[obj.AbsID()]
		obj.TopLeft = geo.NewPoint(p[0], p[1])
		if obj.AbsID() == "box" {
			obj.Width, obj.Height = 200, 200
		}
	}
	for id, want := range map[string]bool{
		"(a -> c)[0]":         true,
		"(a -> b)[0]":         false,
		"(box.inner -> a)[0]": false,
	} {
		if got := straightLineCrossesShape(g, findEdge(t, g, id)); got != want {
			t.Errorf("%s crosses a shape = %v, want %v", id, got, want)
		}
	}
}
