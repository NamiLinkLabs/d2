# Graceful work limits (B5)

Before this change TALA failed the whole layout when a large graph reached one
of its work limits: `arch_200` failed in edge routing (every route flavor used
up its 120M-unit search budget) and `arch_500` failed in placement (the
aggregate `AutolayoutTransactions` guard, 1e9 units, ran out during
`SwapStuff`). Now both complete. The design is described in `ARCHITECTURE.md`
("Work limits and degradation"); this file records the calibration and the
measurements.

## What degrades

| limit reached | before | now |
|---|---|---|
| optional placement refinement (`SwapStuff`, `Transpose`, `AlignAxes`, `GapNormalization`, `OptimizeClusters`, `BalanceSymmetry`, `Equidistance`) | aggregate transaction guard error | the stage stops at its next candidate boundary; once the total is used up the remaining refinement stages are skipped |
| per-flavor route-search budget | flavor fails; layout fails when every flavor fails | the flavor keeps its searched routes and gives the remaining edges orthogonal fallback routes |
| visibility-graph size or work limit | routing error | port-only routing graph, every edge gets a fallback route |
| work limit of an optional atomic route post-processing stage | error (except `NudgeEdgeChannels`, `ShortcutEdgeRoutes`, which already skipped) | the stage restores its routes and counts as no change |
| route materialization of the selected flavor | error | the selected flavor keeps one fallback allowance of headroom |
| caller's context cancelled or deadline exceeded | error | error (unchanged) |

Fallback routes are straight, L-, Z- or U-shaped between free side ports, and
the candidate that crosses the fewest unrelated shapes wins, then the fewest
bends, then the shortest length. A flavor that searched every edge is always
selected first, exactly as before; among degraded flavors the fewest fallback
routes wins.

## Determinism

Decisions are taken only on deterministic counts:

- The refinement meter (`layoutgraph.RefinementBudget`) charges each
  transaction commit `1 + operations + validated nodes` before any early exit
  and the overlap-candidate references of its post-state sweep. These depend
  only on the deterministic sequence of commits.
- The aggregate transaction guard is not used for decisions. Its count
  differs slightly between runs of the same input (3 runs of `arch_060`:
  8,998,173 / 8,986,622 / 8,992,370 units; the first difference is in
  `SwapStuff`), because some validation scans iterate maps and exit at the
  first rejection. It remains a backstop, now 2e9 for one layout attempt.
- Route-search work is charged in slice order (per-flavor totals were
  identical across runs). The one map-ordered launch loop, over table-column
  ports in the slingshot router, now iterates in coordinate order.

Checks: byte-identical output of `arch_010/060/100` before and after the
change (1 seed); `arch_200` byte-identical over three runs, including one with
`GOMAXPROCS=4` where the three route flavors run concurrently; the engine
tests compare serialized layouts under `GOMAXPROCS` 1 and 4 with every budget
exhausted; `FuzzAutolayoutDegraded` lays every random graph out twice.

## Calibration

Refinement meter per stage, one seed (seed 1 as in `tala-1seed`), with the
aggregate transaction guard alongside (not a decision input):

| case | stage | meter units | guard units | guard / unit |
|---|---|---:|---:|---:|
| arch_100 | all refinement | 1.92M | 27.6M | 14 |
| arch_200 | SwapStuff | 2.04M | 74.5M | 37 |
| arch_200 | AlignAxes (first) | 1.96M | 17.9M | 9 |
| arch_200 | GapNormalization | 1.15M | 29.0M | 25 |
| arch_200 | Equidistance | 0.60M | 14.2M | 24 |
| arch_200 | all refinement | 7.90M | 155.8M | 20 |
| arch_500 | SwapStuff (stopped at the stage budget) | 6.00M | 510.1M | 85 |
| arch_500 | Transpose | 0.18M | 16.6M | 92 |
| arch_500 | AlignAxes (stopped at the total budget) | 5.83M | 88.4M | 15 |

With `MaxRefinementStageWorkUnits` 6M and `MaxRefinementWorkUnits` 12M:
`arch_100` and `arch_200` refine completely (the largest stage uses a third of
its budget, the total two thirds); `arch_500` stops `SwapStuff` and
`AlignAxes` and skips the 7 remaining refinement stages, ending at 645M
transaction units in total, well inside the old 1e9 guard. A layout changes
only if its refinement needs more than 12M units (1.5x `arch_200`) or a single
stage more than 6M (3x `arch_200`'s largest). Because the guard costs 9-92
units per meter unit, no budget in deterministic units can match the old
guard exactly: some graphs that stopped below 1e9 guard units before can now
stop refining earlier, and graphs above it now finish instead of failing.

Routing, seed 1:

- `arch_200`: ShortestToLongest searches 194 of 245 edges before its budget
  runs out (Default 120, LongestToShortest 18); it is selected with 51
  fallback routes.
- `arch_500`: the visibility graph would need 2,956,365 intersection
  candidates (limit 1,000,000), so all 645 edges get fallback routes on the
  port-only graph. `BalanceEdgeSegments` reaches its 50M-unit limit and is
  skipped.

## Measurements

RESULTS_PLACEHOLDER

## Remaining limits

Still errors: input topology limits, the aggregate transaction backstop (2e9),
the port-only graph's own limits, `MaxPlaceTreesWorkUnits`,
`MaxBinPackWorkUnits`, `MaxLabelPlacementWorkUnits`, hierarchy and placement
optimization (`MaxOptimizationWorkUnits`), and the edge-routing stage
envelope, which is shared by all disconnected subgraphs (several large
components can still exhaust it). None of them is reached by the corpus.
