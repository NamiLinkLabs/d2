package layoutgraph

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// randomOwnershipForest links nodes through containers, cluster vessels and
// sequence vessels (including vessels that are nil), always to an earlier
// node so ownership is acyclic.
func randomOwnershipForest(rng *rand.Rand, count int) []*Node {
	nodes := make([]*Node, count)
	for i := range nodes {
		nodes[i] = &Node{}
		if i == 0 {
			continue
		}
		owner := nodes[rng.IntN(i)]
		switch rng.IntN(6) {
		case 0, 1:
			nodes[i].Container = owner
		case 2:
			nodes[i].Cluster = &Cluster{Vessel: owner}
		case 3:
			nodes[i].Sequence = &Sequence{Vessel: owner}
		case 4:
			if rng.IntN(4) == 0 {
				nodes[i].Cluster = &Cluster{}
			}
		}
	}
	return nodes
}

func TestAncestryHelpersMatchIsDescendantOf(t *testing.T) {
	for seed := uint64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewPCG(seed, 3))
		nodes := randomOwnershipForest(rng, 2+rng.IntN(30))
		pick := func() *Node {
			if rng.IntN(8) == 0 {
				return nil
			}
			return nodes[rng.IntN(len(nodes))]
		}
		for range 300 {
			node, a, b, c := pick(), pick(), pick(), pick()
			want2 := node.isDescendantOf(a) || node.isDescendantOf(b)
			if got := node.IsDescendantOfAny(a, b); got != want2 {
				t.Fatalf("seed %d: IsDescendantOfAny(2) = %v, want %v", seed, got, want2)
			}
			want3 := want2 || node.isDescendantOf(c)
			if got := node.IsDescendantOfAny(a, b, c); got != want3 {
				t.Fatalf("seed %d: IsDescendantOfAny(3) = %v, want %v", seed, got, want3)
			}
			chain, ok := node.AppendAncestry(nil)
			if !ok {
				t.Fatalf("seed %d: acyclic ancestry walk did not end", seed)
			}
			chain, ok = b.AppendAncestry(chain)
			if !ok {
				t.Fatalf("seed %d: acyclic ancestry walk did not end", seed)
			}
			wantChain := node.isDescendantOf(a) || b.isDescendantOf(a)
			if got := a == nil || slices.Contains(chain, a); got != wantChain {
				t.Fatalf("seed %d: ancestry chain membership = %v, want %v", seed, got, wantChain)
			}
		}
	}
}

func TestAppendAncestryReportsCyclicOwnership(t *testing.T) {
	a, b := &Node{}, &Node{}
	a.Container, b.Container = b, a
	if _, ok := a.AppendAncestry(nil); ok {
		t.Fatal("cyclic ancestry walk reported complete")
	}
}
