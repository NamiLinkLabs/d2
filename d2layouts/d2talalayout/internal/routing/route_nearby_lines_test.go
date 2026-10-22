package routing

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// addRoute's bulk-charged nearby-line scan must leave exactly the router
// state and work count of the per-line map scan.
func TestAddRouteNearbyLinesMatchMapScan(t *testing.T) {
	nearbyEdges, overlappingRoutes := 0, 0
	for seed := uint64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewPCG(seed, 3))
		coordinate := func() float64 {
			return float64(rng.IntN(60)) + 0.5*float64(rng.IntN(2))
		}
		ovg := NewOVG(nil)
		for range 300 {
			a := ovg.AddNode(NewOVGNode(geo.NewPoint(coordinate(), coordinate())))
			var b *OVGNode
			if rng.IntN(2) == 0 {
				b = ovg.AddNode(NewOVGNode(geo.NewPoint(a.X, coordinate())))
			} else {
				b = ovg.AddNode(NewOVGNode(geo.NewPoint(coordinate(), a.Y)))
			}
			if a != b {
				ovg.Connect(a, b)
			}
		}
		for i, node := range ovg.Nodes {
			node.Index = i
		}

		var routes []*Route
		for range 25 {
			x, y := coordinate(), coordinate()
			nodes := []*OVGNode{NewOVGNode(geo.NewPoint(x, y))}
			seen := map[geo.Point]bool{{X: x, Y: y}: true}
			for len(nodes) < 6 {
				if len(nodes)%2 == 0 {
					x = coordinate()
				} else {
					y = coordinate()
				}
				if seen[geo.Point{X: x, Y: y}] {
					continue
				}
				seen[geo.Point{X: x, Y: y}] = true
				if rng.IntN(3) == 0 {
					// Reuse an OVG node at this point when there is one.
					if existing, ok := ovg.OccupiedPoints[geo.Point{X: x, Y: y}]; ok {
						nodes = append(nodes, existing)
						continue
					}
				}
				nodes = append(nodes, NewOVGNode(geo.NewPoint(x, y)))
			}
			edge := layoutgraph.NewEdge(layoutgraph.NewNode(1, 10, 10), layoutgraph.NewNode(2, 10, 10))
			routes = append(routes, &Route{GEdge: edge, OVGNodes: nodes})
		}

		run := func(ctx context.Context) *ovgEdgeRouter {
			t.Helper()
			router, err := newOVGEdgeRouter(ctx, ShortestToLongest, ovg, layoutgraph.NewGraph(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range routes {
				if err := router.addRoute(route); err != nil {
					t.Fatal(err)
				}
			}
			return router
		}
		ctx, cancel := context.WithCancel(context.Background())
		bulk := run(ctx)
		cancel()
		stepped := run(context.Background())
		if !bulk.work.batchesSteps() || stepped.work.batchesSteps() {
			t.Fatal("test routers do not exercise both scan forms")
		}
		if bulk.work.used != stepped.work.used {
			t.Fatalf("seed %d: work = %d; map scan charged %d", seed, bulk.work.used, stepped.work.used)
		}
		if !slices.Equal(bulk.routeEdges.nearby, stepped.routeEdges.nearby) {
			t.Fatalf("seed %d: nearby edges differ", seed)
		}
		for i := range bulk.routeEdges.overlaps {
			if !slices.Equal(bulk.routeEdges.overlaps[i], stepped.routeEdges.overlaps[i]) {
				t.Fatalf("seed %d: overlapping routes of edge %d differ", seed, i)
			}
			overlappingRoutes += len(bulk.routeEdges.overlaps[i])
			if bulk.routeEdges.nearby[i] {
				nearbyEdges++
			}
		}
	}
	if nearbyEdges == 0 || overlappingRoutes == 0 {
		t.Fatalf("fixture exercised %d nearby edges and %d overlaps; want both", nearbyEdges, overlappingRoutes)
	}
}
