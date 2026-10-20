package placementcost

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// referenceGraphEdgeCrossings is the plain all-pairs scan GraphEdgeCrossings
// must always agree with.
func referenceGraphEdgeCrossings(graph *layoutgraph.Graph) int64 {
	groups := map[int][]*layoutgraph.Edge{}
	var levels []int
	for _, edge := range graph.Edges {
		if edge.From == nil || edge.To == nil || edge.From.TopLeft == nil || edge.To.TopLeft == nil {
			continue
		}
		fromLevel := edge.From.ContainerLevel()
		if fromLevel != edge.To.ContainerLevel() {
			continue
		}
		if _, ok := groups[fromLevel]; !ok {
			levels = append(levels, fromLevel)
		}
		groups[fromLevel] = append(groups[fromLevel], edge)
	}
	var crossings int64
	for _, level := range levels {
		edges := groups[level]
		for i := 0; i < len(edges)-1; i++ {
			edge1 := edges[i]
			for j := i + 1; j < len(edges); j++ {
				edge2 := edges[j]
				if edge1.From == edge2.From || edge1.From == edge2.To ||
					edge1.To == edge2.From || edge1.To == edge2.To {
					continue
				}
				seg1 := geo.NewSegment(edge1.From.Center(), edge1.To.Center())
				seg2 := geo.NewSegment(edge2.From.Center(), edge2.To.Center())
				if layoutgraph.SegmentsCross(seg1.Start, seg1.End, seg2.Start, seg2.End) {
					crossings++
				}
			}
		}
	}
	return crossings
}

func TestGraphEdgeCrossingsIncrementalMatchesFullScan(t *testing.T) {
	for seed := uint64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewPCG(seed, 7))
		g := layoutgraph.NewGraph()
		var nodes []*layoutgraph.Node
		id := layoutgraph.EntityID(1)
		newNode := func(container *layoutgraph.Node) *layoutgraph.Node {
			node := layoutgraph.NewNode(id, float64(1+rng.IntN(4)), float64(1+rng.IntN(4)))
			id++
			node.TopLeft = geo.NewPoint(float64(rng.IntN(40)), float64(rng.IntN(40)))
			g.AddNewNodeToContainer(container, node)
			return node
		}
		container := layoutgraph.NewNode(id, 200, 200)
		id++
		container.TopLeft = geo.NewPoint(-100, -100)
		g.AddNewNodeToContainer(nil, container)
		for range 12 {
			nodes = append(nodes, newNode(nil))
		}
		for range 8 {
			nodes = append(nodes, newNode(container))
		}
		for range 40 {
			g.Connect(nodes[rng.IntN(len(nodes))], nodes[rng.IntN(len(nodes))])
		}

		check := func(step int) {
			t.Helper()
			got, err := GraphEdgeCrossings(context.Background(), g)
			if err != nil {
				t.Fatal(err)
			}
			if want := referenceGraphEdgeCrossings(g); got != want {
				t.Fatalf("seed %d step %d: crossings = %d; want %d", seed, step, got, want)
			}
		}
		check(-1)
		for step := range 400 {
			switch op := rng.IntN(10); {
			case op < 5:
				// Move one or two nodes, like placement candidates do.
				for range 1 + rng.IntN(2) {
					node := nodes[rng.IntN(len(nodes))]
					node.TopLeft = geo.NewPoint(float64(rng.IntN(40)), float64(rng.IntN(40)))
				}
			case op < 6:
				// Fractional and coincident positions.
				node := nodes[rng.IntN(len(nodes))]
				other := nodes[rng.IntN(len(nodes))]
				if other.TopLeft != nil {
					node.TopLeft = geo.NewPoint(other.TopLeft.X+0.5*float64(rng.IntN(3)), other.TopLeft.Y)
				}
			case op < 7:
				// Unplace or re-place a node.
				node := nodes[rng.IntN(len(nodes))]
				if node.TopLeft == nil {
					node.TopLeft = geo.NewPoint(float64(rng.IntN(40)), float64(rng.IntN(40)))
				} else {
					node.TopLeft = nil
				}
			case op < 8:
				// Resize a node, which moves its center.
				node := nodes[rng.IntN(len(nodes))]
				node.Width = float64(1 + rng.IntN(6))
			case op < 9:
				// Reattach an edge endpoint.
				edge := g.Edges[rng.IntN(len(g.Edges))]
				edge.To = nodes[rng.IntN(len(nodes))]
			default:
				// Change the edge list itself.
				if rng.IntN(2) == 0 && len(g.Edges) > 1 {
					g.Edges = g.Edges[:len(g.Edges)-1]
				} else {
					g.Connect(nodes[rng.IntN(len(nodes))], nodes[rng.IntN(len(nodes))])
				}
			}
			check(step)
			if rng.IntN(4) == 0 {
				// Repeated calls on an unchanged graph.
				check(step)
			}
		}
		// Restore every node so the final layout has all edges counted.
		for _, node := range nodes {
			if node.TopLeft == nil {
				node.TopLeft = geo.NewPoint(1, 1)
			}
		}
		check(400)
	}
}

func TestGraphEdgeCrossingsRecoversAfterIncrementalCancellation(t *testing.T) {
	g := layoutgraph.NewGraph()
	var nodes []*layoutgraph.Node
	for i := range 200 {
		node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), 1, 1)
		node.TopLeft = geo.NewPoint(float64(i%20), float64(i/20))
		g.AddNewNodeToContainer(nil, node)
		nodes = append(nodes, node)
	}
	for i := range 199 {
		g.Connect(nodes[i], nodes[(i*37+11)%200])
	}
	if _, err := GraphEdgeCrossings(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	nodes[3].TopLeft = geo.NewPoint(7.5, 3.5)
	ctx := &cancelAfterErrChecks{Context: context.Background(), remaining: 6}
	if _, err := GraphEdgeCrossings(ctx, g); err == nil {
		t.Fatal("expected cancellation during the incremental update")
	}
	got, err := GraphEdgeCrossings(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if want := referenceGraphEdgeCrossings(g); got != want {
		t.Fatalf("crossings after canceled update = %d; want %d", got, want)
	}
}
