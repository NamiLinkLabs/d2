package routing

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// searchCrossing must return what intersectsWithGuarded returns and charge
// the same units, both on the first scan of an edge in a search and on every
// repeated scan, and must not carry answers across searches when the edge set
// changes in between.
func TestSearchCrossingMatchesGuardedScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for seed := uint64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewPCG(seed, 7))
		coordinate := func() float64 {
			return float64(rng.IntN(30)) - 10 + 0.5*float64(rng.IntN(2))
		}
		ovg := NewOVG(nil)
		for range 150 {
			x, y := coordinate(), coordinate()
			length := float64(rng.IntN(12)) - 4
			other := geo.NewPoint(x+length, y)
			if rng.IntN(2) == 0 {
				other = geo.NewPoint(x, y+length)
			}
			if ovg.Connect(NewOVGNode(geo.NewPoint(x, y)), NewOVGNode(other)) == nil {
				t.Fatal("connect failed")
			}
		}
		memoGuard, err := newRouteSearchWorkGuard(ctx, Default, maxRouteSearchWorkUnits)
		if err != nil {
			t.Fatal(err)
		}
		refGuard, err := newRouteSearchWorkGuard(ctx, Default, maxRouteSearchWorkUnits)
		if err != nil {
			t.Fatal(err)
		}
		if !batchesRouteSteps(memoGuard) {
			t.Fatal("expected a bulk-charging guard")
		}
		router := &ovgEdgeRouter{ovg: ovg, edgeSet: newOvgEdgeSet(), work: memoGuard}
		reference := &ovgEdgeRouter{ovg: ovg, edgeSet: router.edgeSet, work: refGuard}
		for search := range 8 {
			// Routes are added between searches.
			for range 5 + rng.IntN(20) {
				e := ovg.Edges[rng.IntN(len(ovg.Edges))]
				router.edgeSet.add(NewOVGEdge(e.From, e.To))
			}
			router.beginCrossingMemo()
			for range 400 {
				e := ovg.Edges[rng.IntN(len(ovg.Edges))]
				before := memoGuard.used
				got, err := router.searchCrossing(e)
				if err != nil {
					t.Fatal(err)
				}
				refBefore := refGuard.used
				want, err := reference.edgeSet.intersectsWithGuarded(e, reference.work)
				if err != nil {
					t.Fatal(err)
				}
				if got != want || memoGuard.used-before != refGuard.used-refBefore {
					t.Fatalf("seed %d search %d edge %d: memo = (%v, %d units); scan = (%v, %d units)",
						seed, search, e.index, got, memoGuard.used-before, want, refGuard.used-refBefore)
				}
			}
		}
		if memoGuard.used != refGuard.used {
			t.Fatalf("seed %d: total units %d, want %d", seed, memoGuard.used, refGuard.used)
		}
	}
}

func TestClearSearchStateClearsTouchedEntries(t *testing.T) {
	router := &ovgEdgeRouter{
		verticalHops:   make([]*OVGNode, 4, 8),
		horizontalHops: make([]*OVGNode, 4, 8),
		nodeContext:    make([]*searchNodeContext, 4, 8),
	}
	node := NewOVGNode(geo.NewPoint(0, 0))
	for _, i := range []int{0, 3} {
		router.verticalHops[i] = node
		router.horizontalHops[i] = node
		router.nodeContext[i] = &searchNodeContext{}
		router.searchTouched = append(router.searchTouched, i)
	}
	router.clearSearchState()
	for i := range 8 {
		if router.verticalHops[:8][i] != nil || router.horizontalHops[:8][i] != nil || router.nodeContext[:8][i] != nil {
			t.Fatalf("entry %d not cleared", i)
		}
	}
	if len(router.searchTouched) != 0 {
		t.Fatalf("touched list not reset: %v", router.searchTouched)
	}
}
