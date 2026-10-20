package placementcost

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// referenceEdgeLengthState is the hash/fnv formulation of
// placementEdgeLengthState; the inline hash must match it exactly because the
// value keys the graph's edge-length cache.
func referenceEdgeLengthState(graph *layoutgraph.Graph, costs layoutgraph.RoutingCostState, options EdgeLengthOptions) uint64 {
	h := fnv.New64a()
	var b [8]byte
	write := func(v uint64) {
		binary.LittleEndian.PutUint64(b[:], v)
		h.Write(b[:])
	}
	var flags byte
	if options.IncludeNodeSizes {
		flags |= 1 << 0
	}
	if options.EnforceMinimumGap {
		flags |= 1 << 1
	}
	if options.PenalizeDirection {
		flags |= 1 << 2
	}
	h.Write([]byte{flags})
	write(math.Float64bits(graph.CellSize))
	write(math.Float64bits(costs.Turn))
	write(math.Float64bits(costs.NonCenterPort))
	write(math.Float64bits(costs.Crossing))
	for _, n := range graph.Nodes {
		if n.TopLeft == nil {
			return invalidEdgeLengthState
		}
		write(uint64(n.ID))
		write(math.Float64bits(n.TopLeft.X))
		write(math.Float64bits(n.TopLeft.Y))
		write(math.Float64bits(n.Width))
		write(math.Float64bits(n.Height))
		for _, e := range n.Edges {
			write(uint64(e.ID))
		}
	}
	for _, ea := range options.EdgeAbductions {
		write(uint64(ea.Edge.ID))
		if ea.OriginallyFrom != nil {
			write(uint64(ea.OriginallyFrom.ID))
		}
		if ea.OriginallyTo != nil {
			write(uint64(ea.OriginallyTo.ID))
		}
		if ea.CurrentTo != nil {
			write(uint64(ea.CurrentTo.ID))
		}
		if ea.CurrentFrom != nil {
			write(uint64(ea.CurrentFrom.ID))
		}
	}
	return h.Sum64()
}

func TestPlacementEdgeLengthStateMatchesFNV(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 8))
	for trial := range 50 {
		g := layoutgraph.NewGraph()
		var nodes []*layoutgraph.Node
		for i := range 1 + rng.IntN(20) {
			node := layoutgraph.NewNode(layoutgraph.EntityID(i+1+rng.IntN(1000)), rng.Float64()*100, rng.Float64()*100)
			node.TopLeft = geo.NewPoint(rng.NormFloat64()*1e3, rng.NormFloat64()*1e3)
			g.AddNewNodeToContainer(nil, node)
			nodes = append(nodes, node)
		}
		var abductions []*layoutgraph.EdgeAbduction
		for range rng.IntN(30) {
			edge := g.Connect(nodes[rng.IntN(len(nodes))], nodes[rng.IntN(len(nodes))])
			if rng.IntN(3) == 0 {
				abduction := &layoutgraph.EdgeAbduction{Edge: edge, CurrentFrom: edge.From}
				if rng.IntN(2) == 0 {
					abduction.OriginallyTo = nodes[rng.IntN(len(nodes))]
				}
				abductions = append(abductions, abduction)
			}
		}
		g.CellSize = rng.Float64() * 50
		costs := layoutgraph.RoutingCostState{Turn: rng.Float64(), NonCenterPort: rng.Float64(), Crossing: rng.Float64()}
		options := EdgeLengthOptions{
			EdgeAbductions:    abductions,
			IncludeNodeSizes:  rng.IntN(2) == 0,
			EnforceMinimumGap: rng.IntN(2) == 0,
			PenalizeDirection: rng.IntN(2) == 0,
		}
		if trial%10 == 9 {
			nodes[rng.IntN(len(nodes))].TopLeft = nil
		}
		if got, want := placementEdgeLengthState(g, costs, options), referenceEdgeLengthState(g, costs, options); got != want {
			t.Fatalf("trial %d: state = %#x; want %#x", trial, got, want)
		}
	}
}
