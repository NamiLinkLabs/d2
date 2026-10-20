package routing

import (
	"math/rand/v2"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// countingWorkBudget charges every unit one at a time and never refuses.
type countingWorkBudget struct{ used uint64 }

func (budget *countingWorkBudget) step() error            { budget.used++; return nil }
func (budget *countingWorkBudget) add(units uint64) error { budget.used += units; return nil }
func (budget *countingWorkBudget) check() error           { return nil }

// The bulk-charged crossing scan must return the stepped scan's answer and
// charge exactly the units the stepped scan charges.
func TestOVGEdgeSetCountedScanMatchesSteppedScan(t *testing.T) {
	for seed := uint64(1); seed <= 30; seed++ {
		rng := rand.New(rand.NewPCG(seed, 99))
		coordinate := func() float64 {
			return float64(rng.IntN(30)) - 10 + 0.5*float64(rng.IntN(2))
		}
		randomEdge := func() *OVGEdge {
			x, y := coordinate(), coordinate()
			length := float64(rng.IntN(12)) - 4
			other := geo.NewPoint(x+length, y)
			if rng.IntN(2) == 0 {
				other = geo.NewPoint(x, y+length)
			}
			// Both orientations of the same segment occur in routes.
			if rng.IntN(2) == 0 {
				return NewOVGEdge(NewOVGNode(geo.NewPoint(x, y)), NewOVGNode(other))
			}
			return NewOVGEdge(NewOVGNode(other), NewOVGNode(geo.NewPoint(x, y)))
		}
		set := newOvgEdgeSet()
		for range 50 + rng.IntN(250) {
			set.add(randomEdge())
		}
		for range 500 {
			query := randomEdge()
			budget := &countingWorkBudget{}
			want, err := set.intersectsWithSteps(query, budget, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, steps := set.intersectsWithCount(query)
			if got != want || uint64(steps) != budget.used {
				t.Fatalf("seed %d query %v -> %v: counted scan = (%v, %d); stepped scan = (%v, %d)",
					seed, query.From.Point, query.To.Point, got, steps, want, budget.used)
			}
		}
	}
}
