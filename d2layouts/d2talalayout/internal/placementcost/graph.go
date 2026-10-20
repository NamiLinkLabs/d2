package placementcost

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"runtime"
	"sync"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

const invalidEdgeLengthState uint64 = 0

func placementEdgeLengthState(graph *layoutgraph.Graph, costs layoutgraph.RoutingCostState, options EdgeLengthOptions) uint64 {
	h := fnv.New64a()
	var b [8]byte // Buffer for encoding numbers

	// Process booleans
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

	// Process graph costs
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(graph.CellSize))
	h.Write(b[:])
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(costs.Turn))
	h.Write(b[:])
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(costs.NonCenterPort))
	h.Write(b[:])
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(costs.Crossing))
	h.Write(b[:])

	// Process nodes
	for _, n := range graph.Nodes {
		if n.TopLeft == nil {
			return invalidEdgeLengthState
		}
		binary.LittleEndian.PutUint64(b[:], uint64(n.ID))
		h.Write(b[:])

		binary.LittleEndian.PutUint64(b[:], math.Float64bits(n.TopLeft.X))
		h.Write(b[:])
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(n.TopLeft.Y))
		h.Write(b[:])

		binary.LittleEndian.PutUint64(b[:], math.Float64bits(n.Width))
		h.Write(b[:])
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(n.Height))
		h.Write(b[:])

		for _, e := range n.Edges {
			binary.LittleEndian.PutUint64(b[:], uint64(e.ID))
			h.Write(b[:])
		}
	}

	// Process edge abductions
	for _, ea := range options.EdgeAbductions {
		binary.LittleEndian.PutUint64(b[:], uint64(ea.Edge.ID))
		h.Write(b[:])
		if ea.OriginallyFrom != nil {
			binary.LittleEndian.PutUint64(b[:], uint64(ea.OriginallyFrom.ID))
			h.Write(b[:])
		}
		if ea.OriginallyTo != nil {
			binary.LittleEndian.PutUint64(b[:], uint64(ea.OriginallyTo.ID))
			h.Write(b[:])
		}
		if ea.CurrentTo != nil {
			binary.LittleEndian.PutUint64(b[:], uint64(ea.CurrentTo.ID))
			h.Write(b[:])
		}
		if ea.CurrentFrom != nil {
			binary.LittleEndian.PutUint64(b[:], uint64(ea.CurrentFrom.ID))
			h.Write(b[:])
		}
	}

	return h.Sum64()
}

func edgeLengthWorkerCount(nodeCount, gomaxprocs int) int {
	if nodeCount <= 0 {
		return 0
	}
	// Preserve the inexpensive sequential path for small graphs.
	if nodeCount <= 10 {
		return 1
	}
	if gomaxprocs < 1 {
		gomaxprocs = 1
	}
	return min(nodeCount, gomaxprocs)
}

// EdgeLength evaluates the graph's placement cost and may populate its
// graph-owned edge-length cache.
func EdgeLength(ctx context.Context, graph *layoutgraph.Graph, options EdgeLengthOptions) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}

	edgeLengthState := placementEdgeLengthState(graph, graph.RoutingCosts(), options)
	if edgeLengthState != invalidEdgeLengthState {
		if d, ok := graph.LookupEdgeLengthCost(edgeLengthState); ok {
			return d, nil
		}
	}

	symmetryCost := graph.CellSize
	var totalSum float64

	workerCount := edgeLengthWorkerCount(len(graph.Nodes), runtime.GOMAXPROCS(0))
	var costs []float64
	if workerCount > 1 {
		costs = make([]float64, len(graph.Nodes))
		var wg sync.WaitGroup
		var panicOnce sync.Once
		var workerPanicked bool
		var errOnce sync.Once
		var workerErr error

		for worker := range workerCount {
			wg.Go(func() {
				memo := borrowSymmetryMemo()
				defer returnSymmetryMemo(memo)
				defer func() {
					if recovered := recover(); recovered != nil {
						panicOnce.Do(func() {
							// A recovered value can contain credentials or invoke an
							// attacker-controlled String method. Retain only the fact
							// that this worker violated an invariant.
							workerPanicked = true
						})
					}
				}()

				for i := worker; i < len(graph.Nodes); i += workerCount {
					n := graph.Nodes[i]
					nl, err := NodeEdgeLength(ctx, n, options)
					if err != nil {
						errOnce.Do(func() { workerErr = err })
						return
					}
					if options.IncludeNodeSizes {
						columnCrossingCost, err := ColumnCrossingCost(ctx, n, options.EdgeAbductions)
						if err != nil {
							errOnce.Do(func() { workerErr = err })
							return
						}
						symmetry, err := memoNodeSymmetry(ctx, n, options.EdgeAbductions, true, memo)
						if err != nil {
							errOnce.Do(func() { workerErr = err })
							return
						}
						nl += columnCrossingCost
						nl -= symmetry * symmetryCost * float64(len(n.Edges))
					}
					if err := ctx.Err(); err != nil {
						errOnce.Do(func() { workerErr = fmt.Errorf("EdgeLength: %w", err) })
						return
					}

					costs[i] = nl
				}
			})
		}

		wg.Wait()
		if workerPanicked {
			panic("TALA EdgeLength worker invariant failure")
		}
		if workerErr != nil {
			return 0, workerErr
		}
	} else {
		memo := borrowSymmetryMemo()
		defer returnSymmetryMemo(memo)
		for _, n := range graph.Nodes {
			nl, err := NodeEdgeLength(ctx, n, options)
			if err != nil {
				return 0, err
			}
			if options.IncludeNodeSizes {
				columnCrossingCost, err := ColumnCrossingCost(ctx, n, options.EdgeAbductions)
				if err != nil {
					return 0, err
				}
				symmetry, err := memoNodeSymmetry(ctx, n, options.EdgeAbductions, true, memo)
				if err != nil {
					return 0, err
				}
				nl += columnCrossingCost
				nl -= symmetry * symmetryCost * float64(len(n.Edges))
			}
			if err := ctx.Err(); err != nil {
				return 0, fmt.Errorf("EdgeLength: %w", err)
			}
			totalSum += nl
		}
	}
	if workerCount > 1 {
		for _, c := range costs {
			totalSum += c
		}
	}

	crossings, err := GraphEdgeCrossings(ctx, graph)
	if err != nil {
		return 0, err
	}
	totalSum += graph.CrossingCost() * float64(crossings)
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	if edgeLengthState != invalidEdgeLengthState {
		graph.StoreEdgeLengthCost(edgeLengthState, totalSum)
	}

	return totalSum, nil
}

// GraphEdgeCrossings counts crossings between edges that do not share a node.
//
// Two edges are compared when both endpoints of each are placed at the same
// container level; a pair counts when the edges share no endpoint and the
// segments between their endpoint centers cross (layoutgraph.SegmentsCross).
//
// Placement rescored the whole graph after every speculative move, although
// a move changes only the edges incident to the moved nodes. The graph
// therefore keeps the crossing partners of every edge (see
// edgeCrossingState). Each call recomputes every edge's inputs, keeps the
// stored result for every pair whose inputs are bit-for-bit unchanged, and
// re-evaluates every pair that involves a changed edge, so the count is
// exactly the one a full rescan would produce.
func GraphEdgeCrossings(ctx context.Context, graph *layoutgraph.Graph) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	if len(graph.Edges) == 0 {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("EdgeLength: %w", err)
		}
		return 0, nil
	}

	cache := graph.EdgeCrossingCache()
	cache.Mu.Lock()
	defer cache.Mu.Unlock()
	state, _ := cache.State.(*edgeCrossingState)
	if state == nil {
		state = new(edgeCrossingState)
		cache.State = state
	}
	totalCrossings, err := state.count(ctx, graph.Edges)
	if err != nil {
		return 0, err
	}

	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	return totalCrossings, nil
}

// edgeCrossingInputs is everything GraphEdgeCrossings reads from one edge.
// Center coordinates are compared by their bit patterns, so an edge counts as
// unchanged only when SegmentsCross would receive identical inputs.
type edgeCrossingInputs struct {
	from, to *layoutgraph.Node
	// counted reports whether both endpoints are placed and at the same
	// container level; level is that level.
	counted    bool
	level      int
	start, end geo.Point
}

func edgeCrossingInputsOf(edge *layoutgraph.Edge) edgeCrossingInputs {
	inputs := edgeCrossingInputs{from: edge.From, to: edge.To}
	if edge.From == nil || edge.To == nil || edge.From.TopLeft == nil || edge.To.TopLeft == nil {
		return inputs
	}
	fromLevel := edge.From.ContainerLevel()
	if fromLevel != edge.To.ContainerLevel() {
		return inputs
	}
	inputs.counted = true
	inputs.level = fromLevel
	inputs.start = *edge.From.Center()
	inputs.end = *edge.To.Center()
	return inputs
}

func sameFloatBits(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b)
}

func (inputs *edgeCrossingInputs) equal(other *edgeCrossingInputs) bool {
	return inputs.from == other.from && inputs.to == other.to &&
		inputs.counted == other.counted && inputs.level == other.level &&
		sameFloatBits(inputs.start.X, other.start.X) && sameFloatBits(inputs.start.Y, other.start.Y) &&
		sameFloatBits(inputs.end.X, other.end.X) && sameFloatBits(inputs.end.Y, other.end.Y)
}

// edgeCrossingPair reports whether the edges with inputs first and second
// (first earlier in graph.Edges) count as a crossing.
func edgeCrossingPair(first, second *edgeCrossingInputs) bool {
	if !first.counted || !second.counted || first.level != second.level {
		return false
	}
	// Skip if edges share a common node
	if first.from == second.from || first.from == second.to ||
		first.to == second.from || first.to == second.to {
		return false
	}
	return layoutgraph.SegmentsCross(&first.start, &first.end, &second.start, &second.end)
}

// edgeCrossingState is the graph-owned incremental state of
// GraphEdgeCrossings: the edge list it describes, each edge's inputs, and
// each edge's crossing partners (indices into edges).
type edgeCrossingState struct {
	valid    bool
	edges    []*layoutgraph.Edge
	inputs   []edgeCrossingInputs
	partners [][]int32
	total    int64

	current     []edgeCrossingInputs
	changed     []int32
	changedMark []bool
	levelGroup  map[int]int
	groups      [][]int32
}

func (state *edgeCrossingState) count(ctx context.Context, edges []*layoutgraph.Edge) (int64, error) {
	// A cancellation or panic part way through leaves the state invalid, and
	// the next call rebuilds it from scratch.
	wasValid := state.valid
	state.valid = false

	state.current = state.current[:0]
	for i, edge := range edges {
		if err := scoringCancellationError(ctx, i); err != nil {
			return 0, err
		}
		state.current = append(state.current, edgeCrossingInputsOf(edge))
	}

	sameEdges := wasValid && len(state.edges) == len(edges)
	if sameEdges {
		for i, edge := range edges {
			if state.edges[i] != edge {
				sameEdges = false
				break
			}
		}
	}
	if sameEdges {
		state.changed = state.changed[:0]
		for i := range state.current {
			if !state.current[i].equal(&state.inputs[i]) {
				state.changed = append(state.changed, int32(i))
			}
		}
		// Re-evaluating a changed edge costs one pass over all edges; beyond a
		// few changed edges a full rescan is cheaper.
		if len(state.changed)*8 <= len(edges) {
			if err := state.update(ctx); err != nil {
				return 0, err
			}
			state.valid = true
			return state.total, nil
		}
	}
	if err := state.rebuild(ctx, edges); err != nil {
		return 0, err
	}
	state.valid = true
	return state.total, nil
}

// rebuild counts every pair of edges within each container level, in the
// same order as a plain scan, and records the crossing partners.
func (state *edgeCrossingState) rebuild(ctx context.Context, edges []*layoutgraph.Edge) error {
	state.edges = append(state.edges[:0], edges...)
	state.inputs, state.current = state.current, state.inputs
	state.total = 0
	if cap(state.partners) < len(edges) {
		state.partners = make([][]int32, len(edges))
	} else {
		state.partners = state.partners[:len(edges)]
	}
	for i := range state.partners {
		state.partners[i] = state.partners[i][:0]
	}

	if state.levelGroup == nil {
		state.levelGroup = make(map[int]int)
	} else {
		clear(state.levelGroup)
	}
	for i := range state.groups {
		state.groups[i] = state.groups[i][:0]
	}
	active := 0
	for i := range state.inputs {
		inputs := &state.inputs[i]
		if !inputs.counted {
			continue
		}
		groupIndex, exists := state.levelGroup[inputs.level]
		if !exists {
			groupIndex = active
			if groupIndex == len(state.groups) {
				state.groups = append(state.groups, nil)
			}
			active++
			state.levelGroup[inputs.level] = groupIndex
		}
		state.groups[groupIndex] = append(state.groups[groupIndex], int32(i))
	}

	for _, group := range state.groups[:active] {
		for i := 0; i < len(group)-1; i++ {
			if err := scoringCancellationError(ctx, i); err != nil {
				return err
			}
			first := group[i]
			for j := i + 1; j < len(group); j++ {
				if err := scoringCancellationError(ctx, j-i-1); err != nil {
					return err
				}
				second := group[j]
				if edgeCrossingPair(&state.inputs[first], &state.inputs[second]) {
					state.addPartners(first, second)
				}
			}
		}
	}
	return nil
}

// update re-evaluates every pair involving a changed edge.
func (state *edgeCrossingState) update(ctx context.Context) error {
	if cap(state.changedMark) < len(state.inputs) {
		state.changedMark = make([]bool, len(state.inputs))
	} else {
		state.changedMark = state.changedMark[:len(state.inputs)]
	}
	marks := state.changedMark
	defer clear(marks)

	for _, edge := range state.changed {
		for _, partner := range state.partners[edge] {
			state.removePartner(partner, edge)
			state.total--
		}
		state.partners[edge] = state.partners[edge][:0]
		state.inputs[edge] = state.current[edge]
		marks[edge] = true
	}

	pairs := 0
	for _, edge := range state.changed {
		for other := range state.inputs {
			other := int32(other)
			// Pairs of two changed edges are evaluated once, from the earlier
			// changed edge in this loop.
			if other == edge || marks[other] && other < edge {
				continue
			}
			if err := scoringCancellationError(ctx, pairs); err != nil {
				return err
			}
			pairs++
			first, second := edge, other
			if second < first {
				first, second = second, first
			}
			if edgeCrossingPair(&state.inputs[first], &state.inputs[second]) {
				state.addPartners(first, second)
			}
		}
	}
	return nil
}

func (state *edgeCrossingState) addPartners(first, second int32) {
	state.partners[first] = append(state.partners[first], second)
	state.partners[second] = append(state.partners[second], first)
	state.total++
}

func (state *edgeCrossingState) removePartner(edge, partner int32) {
	partners := state.partners[edge]
	for i, candidate := range partners {
		if candidate == partner {
			last := len(partners) - 1
			partners[i] = partners[last]
			state.partners[edge] = partners[:last]
			return
		}
	}
}

// ContainerAlignmentCost scores misalignment between peer containers.
func ContainerAlignmentCost(ctx context.Context, graph *layoutgraph.Graph) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	l := 0.0
	for i := 0; i < len(graph.Nodes)-1; i++ {
		if err := scoringCancellationError(ctx, i); err != nil {
			return 0, err
		}
		n1 := graph.Nodes[i]
		if !n1.IsContainer() {
			continue
		}
		for j := i + 1; j < len(graph.Nodes); j++ {
			if err := scoringCancellationError(ctx, j-i-1); err != nil {
				return 0, err
			}
			n2 := graph.Nodes[j]
			if !n2.IsContainer() {
				continue
			}
			if n1.EffectiveContainer() != n2.EffectiveContainer() {
				continue
			}
			if n1.Width == n2.Width && n1.Height == n2.Height {
				if n1.TopLeft.X != n2.TopLeft.X && n1.TopLeft.Y != n2.TopLeft.Y {
					l += graph.NonCenterPortCost()
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("EdgeLength: %w", err)
	}
	return l, nil
}
