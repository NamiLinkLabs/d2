package routing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func onBoxBorder(point *geo.Point, node *layoutgraph.Node) bool {
	const tolerance = 1.0
	left, top := node.TopLeft.X, node.TopLeft.Y
	right, bottom := left+node.Width, top+node.Height
	if point.X < left-tolerance || point.X > right+tolerance || point.Y < top-tolerance || point.Y > bottom+tolerance {
		return false
	}
	return math.Abs(point.X-left) <= tolerance || math.Abs(point.X-right) <= tolerance ||
		math.Abs(point.Y-top) <= tolerance || math.Abs(point.Y-bottom) <= tolerance
}

func requireOrthogonalFallbackRoute(t *testing.T, edge *layoutgraph.Edge) {
	t.Helper()
	if len(edge.Points) < 2 {
		t.Fatalf("edge %s has %d route points", edge.DebugID(), len(edge.Points))
	}
	for i := 1; i < len(edge.Points); i++ {
		previous, current := edge.Points[i-1], edge.Points[i]
		if previous.X != current.X && previous.Y != current.Y {
			t.Fatalf("edge %s segment %d %v -> %v is not orthogonal", edge.DebugID(), i, previous, current)
		}
	}
	if !onBoxBorder(edge.Points[0], edge.From) {
		t.Fatalf("edge %s starts at %v, off the source border", edge.DebugID(), edge.Points[0])
	}
	if !onBoxBorder(edge.Points[len(edge.Points)-1], edge.To) {
		t.Fatalf("edge %s ends at %v, off the target border", edge.DebugID(), edge.Points[len(edge.Points)-1])
	}
}

func pointValues(points []*geo.Point) string {
	values := make([]geo.Point, len(points))
	for i, point := range points {
		values[i] = *point
	}
	return fmt.Sprint(values)
}

func requireSameRoute(t *testing.T, want, got *layoutgraph.Edge) {
	t.Helper()
	if pointValues(want.Points) != pointValues(got.Points) {
		t.Fatalf("route differs between identical runs:\n%v\n%v", pointValues(want.Points), pointValues(got.Points))
	}
}

func box(left, top, right, bottom float64) fallbackRect {
	return fallbackRect{left: left, top: top, right: right, bottom: bottom}
}

func TestOrthogonalFallbackShapes(t *testing.T) {
	source := box(0, 0, 40, 40)
	tests := []struct {
		name       string
		sourceSide geo.Orientation
		sourcePort geo.Point
		targetSide geo.Orientation
		targetPort geo.Point
		target     fallbackRect
		want       string
	}{
		{
			name:       "straight",
			sourceSide: geo.Right, sourcePort: geo.Point{X: 40, Y: 20},
			targetSide: geo.Left, targetPort: geo.Point{X: 200, Y: 20},
			target: box(200, 0, 240, 40),
			want:   "[[{40 20} {200 20}]]",
		},
		{
			name:       "horizontal Z tries three channels",
			sourceSide: geo.Right, sourcePort: geo.Point{X: 40, Y: 20},
			targetSide: geo.Left, targetPort: geo.Point{X: 200, Y: 120},
			target: box(200, 100, 240, 140),
			want: "[[{40 20} {120 20} {120 120} {200 120}] " +
				"[{40 20} {80 20} {80 120} {200 120}] " +
				"[{40 20} {160 20} {160 120} {200 120}]]",
		},
		{
			name:       "vertical Z upwards",
			sourceSide: geo.Top, sourcePort: geo.Point{X: 20, Y: 0},
			targetSide: geo.Bottom, targetPort: geo.Point{X: 120, Y: -100},
			target: box(100, -140, 140, -100),
			want: "[[{20 0} {20 -50} {120 -50} {120 -100}] " +
				"[{20 0} {20 -25} {120 -25} {120 -100}] " +
				"[{20 0} {20 -75} {120 -75} {120 -100}]]",
		},
		{
			name:       "L leaves horizontally and enters from above",
			sourceSide: geo.Right, sourcePort: geo.Point{X: 40, Y: 20},
			targetSide: geo.Top, targetPort: geo.Point{X: 220, Y: 100},
			target: box(200, 100, 240, 140),
			want:   "[[{40 20} {220 20} {220 100}]]",
		},
		{
			name:       "L leaves vertically and enters from the left",
			sourceSide: geo.Bottom, sourcePort: geo.Point{X: 20, Y: 40},
			targetSide: geo.Left, targetPort: geo.Point{X: 200, Y: 120},
			target: box(200, 100, 240, 140),
			want:   "[[{20 40} {20 120} {200 120}]]",
		},
		{
			name:       "U around the outside of both shapes",
			sourceSide: geo.Top, sourcePort: geo.Point{X: 20, Y: 0},
			targetSide: geo.Top, targetPort: geo.Point{X: 320, Y: 10},
			target: box(300, 10, 340, 50),
			want: "[[{20 0} {20 -20} {320 -20} {320 10}] " +
				"[{20 0} {20 -60} {320 -60} {320 10}]]",
		},
		{
			name:       "target behind the source side",
			sourceSide: geo.Left, sourcePort: geo.Point{X: 0, Y: 20},
			targetSide: geo.Right, targetPort: geo.Point{X: 240, Y: 20},
			target: box(200, 0, 240, 40),
			want:   "[]",
		},
		{
			name:       "no gap between facing sides",
			sourceSide: geo.Right, sourcePort: geo.Point{X: 40, Y: 20},
			targetSide: geo.Left, targetPort: geo.Point{X: 41, Y: 30},
			target: box(41, 0, 80, 40),
			want:   "[]",
		},
		{
			name:       "L corner inside the target",
			sourceSide: geo.Right, sourcePort: geo.Point{X: 40, Y: 20},
			targetSide: geo.Bottom, targetPort: geo.Point{X: 220, Y: 40},
			target: box(200, 0, 240, 40),
			want:   "[]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := orthogonalFallbackShapes(test.sourceSide, test.sourcePort, source, test.targetSide, test.targetPort, test.target)
			if fmt.Sprint(got) != test.want {
				t.Fatalf("shapes = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSelectRouteFlavorResponsePrefersCompleteFlavors(t *testing.T) {
	flavors := []RouteGenerationFlavor{ShortestToLongest, LongestToShortest, Default}
	pick := func(responses ...GenerateRouteResponse) RouteGenerationFlavor {
		t.Helper()
		byFlavor := make(map[RouteGenerationFlavor]GenerateRouteResponse, len(responses))
		for _, response := range responses {
			byFlavor[response.Flavor] = response
		}
		selected, ok := selectRouteFlavorResponse(flavors, byFlavor)
		if !ok {
			t.Fatal("no flavor selected")
		}
		return selected.Flavor
	}

	if got := pick(
		GenerateRouteResponse{Flavor: ShortestToLongest, Distance: 100, Fallbacks: 1},
		GenerateRouteResponse{Flavor: Default, Distance: 900},
	); got != Default {
		t.Fatalf("selected %v, want the only complete flavor", got)
	}
	if got := pick(
		GenerateRouteResponse{Flavor: ShortestToLongest, Distance: 300},
		GenerateRouteResponse{Flavor: LongestToShortest, Distance: 200},
		GenerateRouteResponse{Flavor: Default, Distance: 200},
	); got != LongestToShortest {
		t.Fatalf("selected %v, want the established shortest-distance order", got)
	}
	if got := pick(
		GenerateRouteResponse{Flavor: ShortestToLongest, Distance: 100, Fallbacks: 5},
		GenerateRouteResponse{Flavor: LongestToShortest, Distance: 900, Fallbacks: 2},
		GenerateRouteResponse{Flavor: Default, Distance: 800, Fallbacks: 2},
	); got != Default {
		t.Fatalf("selected %v, want fewest fallbacks then distance", got)
	}
	if got := pick(
		GenerateRouteResponse{Flavor: LongestToShortest, Distance: 500, Fallbacks: 2},
		GenerateRouteResponse{Flavor: Default, Distance: 500, Fallbacks: 2},
	); got != LongestToShortest {
		t.Fatalf("selected %v, want declared order on ties", got)
	}
	if _, ok := selectRouteFlavorResponse(flavors, map[RouteGenerationFlavor]GenerateRouteResponse{
		Default: {Flavor: Default, Distance: math.Inf(1)},
	}); ok {
		t.Fatal("an infinite complete distance must keep the established no-selection result")
	}
}

// newFallbackTestGraph is a small row of connected shapes with one obstacle
// between the first pair.
func newFallbackTestGraph() *layoutgraph.Graph {
	g := layoutgraph.NewGraph()
	add := func(id layoutgraph.EntityID, x, y float64) *layoutgraph.Node {
		node := layoutgraph.NewNode(id, 40, 40)
		node.TopLeft = geo.NewPoint(x, y)
		return g.AddNode(node)
	}
	a := add(1, 0, 0)
	b := add(2, 300, 0)
	c := add(3, 300, 300)
	d := add(4, 0, 300)
	add(5, 150, 0) // obstacle on the straight line between a and b
	g.Connect(a, b)
	g.Connect(b, c)
	g.Connect(c, d)
	g.Connect(d, a)
	g.Connect(a, c)
	g.ComputeCellSize()
	return g
}

func TestDegradedFlavorsRouteEveryEdgeIndependentlyOfScheduling(t *testing.T) {
	route := func() []string {
		g := newFallbackTestGraph()
		if _, err := routeEdgesWithSearchWorkLimit(context.Background(), g, nil, 1); err != nil {
			t.Fatalf("route with a one-unit search budget: %v", err)
		}
		routes := make([]string, 0, len(g.Edges))
		for _, edge := range g.Edges {
			requireOrthogonalFallbackRoute(t, edge)
			routes = append(routes, pointValues(edge.Points))
		}
		return routes
	}
	previous := runtime.GOMAXPROCS(1)
	sequential := route()
	runtime.GOMAXPROCS(max(previous, 4))
	parallel := route()
	runtime.GOMAXPROCS(previous)
	if fmt.Sprint(sequential) != fmt.Sprint(parallel) {
		t.Fatalf("degraded routes depend on scheduling:\n%v\n%v", sequential, parallel)
	}
}

func TestFallbackRouteAvoidsObstacles(t *testing.T) {
	g := newFallbackTestGraph()
	if _, err := routeEdgesWithSearchWorkLimit(context.Background(), g, nil, 1); err != nil {
		t.Fatal(err)
	}
	obstacle := g.Nodes[4]
	for _, edge := range g.Edges {
		for i := 1; i < len(edge.Points); i++ {
			if segmentIntersectsBox(edge.Points[i-1], edge.Points[i], &obstacle.Box) {
				t.Fatalf("edge %s crosses the obstacle: %v", edge.DebugID(), edge.Points)
			}
		}
	}
}

func TestFallbackRouteLeavesNestedShapeTowardsAncestor(t *testing.T) {
	g := layoutgraph.NewGraph()
	container := layoutgraph.NewNode(1, 400, 300)
	container.TopLeft = geo.NewPoint(0, 0)
	g.AddNode(container)
	child := layoutgraph.NewNode(2, 40, 40)
	child.TopLeft = geo.NewPoint(300, 130)
	g.AddNewNodeToContainer(container, child)
	edge := g.Connect(child, container)
	g.ComputeCellSize()

	ovg, err := buildPortOVG(context.Background(), g, nil)
	if err != nil {
		t.Fatal(err)
	}
	router, err := newOVGEdgeRouterWithWorkLimit(context.Background(), Default, ovg, g, nil, g.Edges, maxRouteSearchWorkUnits)
	if err != nil {
		t.Fatal(err)
	}
	router.degrade()
	nodes, _, err := router.fallbackRoute(context.Background(), edge)
	if err != nil {
		t.Fatal(err)
	}
	route := (&Route{GEdge: edge, OVGNodes: nodes}).createSegmentEndpoints()
	// The right side of the child is closest to the container's border.
	want := "[{340 150} {400 150}]"
	if got := pointValues(route); got != want {
		t.Fatalf("nested fallback route = %v, want %v", got, want)
	}
}

func TestOptionalStageResultSkipsOnlyItsOwnWorkLimit(t *testing.T) {
	limit := fmt.Errorf("%w: TALA BalanceEdgeSegments work exceeds limit 1", errRouteStageWorkLimit)
	if err := optionalStageResult(limit); err != nil {
		t.Fatalf("work-limit result = %v, want nil", err)
	}
	canceled := fmt.Errorf("BalanceEdgeSegments: %w", context.Canceled)
	if err := optionalStageResult(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled result = %v, want context.Canceled", err)
	}
	other := errors.New("invariant")
	if err := optionalStageResult(other); err != other {
		t.Fatalf("other result = %v, want unchanged", err)
	}
	if err := optionalStageResult(nil); err != nil {
		t.Fatalf("nil result = %v", err)
	}
}

// The public optional stage restores its routes and reports success at its
// limit; the guarded kernel still reports the limit.
func TestOptionalRouteStageAtWorkLimitKeepsRoutes(t *testing.T) {
	g := newFallbackTestGraph()
	if _, err := routeEdges(context.Background(), g, nil); err != nil {
		t.Fatal(err)
	}
	before := make([]string, len(g.Edges))
	for i, edge := range g.Edges {
		before[i] = pointValues(edge.Points)
	}
	err := balanceEdgeSegmentsWithLimit(context.Background(), g, 1)
	if !errors.Is(err, errRouteStageWorkLimit) {
		t.Fatalf("kernel error = %v, want the stage work limit", err)
	}
	if err := optionalStageResult(err); err != nil {
		t.Fatal(err)
	}
	for i, edge := range g.Edges {
		if got := pointValues(edge.Points); got != before[i] {
			t.Fatalf("edge %s changed at the work limit: %v, want %v", edge.DebugID(), got, before[i])
		}
	}
}
