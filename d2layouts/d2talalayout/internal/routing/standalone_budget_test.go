package routing

import (
	"context"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// budgetTestGraph is a, b and an obstacle between them, with an unrouted
// edge from a to b.
func budgetTestGraph() (*layoutgraph.Graph, *layoutgraph.Edge) {
	graph := layoutgraph.NewGraph()
	a := graph.AddNode(layoutgraph.NewNode(1, 40, 40))
	obstacle := graph.AddNode(layoutgraph.NewNode(2, 40, 120))
	b := graph.AddNode(layoutgraph.NewNode(3, 40, 40))
	a.TopLeft = geo.NewPoint(0, 40)
	obstacle.TopLeft = geo.NewPoint(100, 0)
	b.TopLeft = geo.NewPoint(200, 40)
	return graph, graph.Connect(a, b)
}

func routePoints(edge *layoutgraph.Edge) []geo.Point {
	points := make([]geo.Point, len(edge.Points))
	for i, p := range edge.Points {
		points[i] = *p
	}
	return points
}

func TestRouteEdgesWithinBudget(t *testing.T) {
	graph, edge := budgetTestGraph()
	if err := RouteEdges(context.Background(), graph, []*layoutgraph.Edge{edge}); err != nil {
		t.Fatal(err)
	}
	want := routePoints(edge)
	if len(want) < 3 {
		t.Fatalf("route %v does not go around the obstacle", want)
	}

	// With enough budget, the routes are RouteEdges'; the telemetry says what
	// it took.
	graph, edge = budgetTestGraph()
	telemetry := &SearchTelemetry{}
	ctx := WithSearchTelemetry(context.Background(), telemetry)
	if err := RouteEdgesWithinBudget(ctx, graph, []*layoutgraph.Edge{edge}, MaxSearchWorkUnits, 1<<40, 3); err != nil {
		t.Fatal(err)
	}
	if got := routePoints(edge); !slices.Equal(got, want) {
		t.Fatalf("route within budget = %v, want %v", got, want)
	}
	ovgWork, search := telemetry.OVGWork(), slices.Max(telemetry.WorkSamples())
	if ovgWork == 0 || search == 0 {
		t.Fatalf("telemetry recorded OVG work %d and search work %d", ovgWork, search)
	}

	// One edge has one order, so one flavor searches and gets what building
	// the graph left: exactly that much budget is enough, and one unit less
	// is not, every time.
	need := 3*ovgWork + search
	for range 3 {
		graph, edge = budgetTestGraph()
		if err := RouteEdgesWithinBudget(context.Background(), graph, []*layoutgraph.Edge{edge}, MaxSearchWorkUnits, need, 3); err != nil {
			t.Fatalf("budget %d: %v", need, err)
		}
		if got := routePoints(edge); !slices.Equal(got, want) {
			t.Fatalf("route within budget %d = %v, want %v", need, got, want)
		}
		graph, edge = budgetTestGraph()
		if err := RouteEdgesWithinBudget(context.Background(), graph, []*layoutgraph.Edge{edge}, MaxSearchWorkUnits, need-1, 3); err == nil {
			t.Fatalf("budget %d routed an edge that needs %d", need-1, need)
		}
		if len(edge.Points) != 0 {
			t.Fatalf("a failed call left route %v", routePoints(edge))
		}
	}

	// Running out while building the graph is recorded too.
	graph, edge = budgetTestGraph()
	telemetry = &SearchTelemetry{}
	ctx = WithSearchTelemetry(context.Background(), telemetry)
	if err := RouteEdgesWithinBudget(ctx, graph, []*layoutgraph.Edge{edge}, MaxSearchWorkUnits, 3*(ovgWork/2), 3); err == nil {
		t.Fatal("half the graph's work routed the edge")
	}
	if got := telemetry.OVGWork(); got == 0 || got > ovgWork/2+1 {
		t.Fatalf("telemetry recorded OVG work %d for a build cut off at %d", got, ovgWork/2)
	}
}

func TestRepeatedFlavorOrdersAreSkipped(t *testing.T) {
	graph, edge := budgetTestGraph()
	telemetry := &SearchTelemetry{}
	ctx := WithSearchTelemetry(context.Background(), telemetry)
	if err := RouteEdges(ctx, graph, []*layoutgraph.Edge{edge}); err != nil {
		t.Fatal(err)
	}
	samples := telemetry.WorkSamples()
	if len(samples) != 3 {
		t.Fatalf("got %d flavor samples, want 3", len(samples))
	}
	searched := 0
	for _, work := range samples {
		// A skipped flavor only sorted its one edge.
		if work > 100 {
			searched++
		}
	}
	if searched != 1 {
		t.Fatalf("flavor work %v: %d flavors searched one edge, want 1", samples, searched)
	}
}
