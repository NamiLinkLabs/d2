package engine

// Effort trims optional and iterative layout work to trade quality for speed.
// Every field is a deterministic count or switch, never a time budget, so a
// graph, a seed and an Effort still define exactly one layout attempt. The
// zero value runs the full pipeline.
type Effort struct {
	// PlacementIterations replaces node placement's annealing iteration factor:
	// each subgraph runs PlacementIterations × √(nodes) iterations. Zero keeps
	// the default factor.
	PlacementIterations int

	// SingleRouteFlavor routes every subgraph with one edge ordering instead of
	// routing it once per ordering and keeping the shortest result.
	SingleRouteFlavor bool

	// SkipSwap skips the SwapStuff stage, which exchanges node pairs after
	// placement to shorten edges and remove crossings.
	SkipSwap bool

	// SkipGapNormalization skips the GapNormalization stage, which closes
	// uneven gaps between placed nodes.
	SkipGapNormalization bool
}
