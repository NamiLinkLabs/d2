# Output-preserving TALA speedups (B1, exact part)

Branch `perf/exact-speedups`, based on `boxwire` @ `5bdad71`. Every change on
this branch keeps TALA's output byte-identical: same geometry, same routes,
same errors, and the same deterministic work-unit accounting, so work limits
trip at exactly the same point (`arch_200` still fails with the identical
route-search limit error, only sooner). See `BASELINE.md` for the corpus,
harness and metric definitions.

## Checking that output is unchanged

`talabench -dump DIR` writes the complete final geometry of every layout
(`bench.GeometryJSON`: all `d2target.Shape` and `d2target.Connection`
records — positions, sizes, label positions, routes) to
`DIR/<case>.<engine>[.edit].json` and prints its SHA-256. Build the old and
the new engine, dump the corpus with both and compare the hashes:

```sh
go build -o /tmp/talabench-old ./d2layouts/d2talalayout/bench/cmd/talabench   # on the base commit
go build -o /tmp/talabench-new ./d2layouts/d2talalayout/bench/cmd/talabench   # on the change
for b in old new; do
  GOMAXPROCS=1 /tmp/talabench-$b -cases 'arch_0[16]0|arch_100' -engines tala,tala-1seed \
    -timeout 10m -format jsonl -dump /tmp/geom-$b >/dev/null 2>/tmp/geom-$b.log
  grep '^geometry' /tmp/geom-$b.log | awk '{print $3, $2}' | sort >/tmp/geom-$b.sums
done
diff /tmp/geom-old.sums /tmp/geom-new.sums && echo identical
```

Each case is laid out unedited and with its `bench-edit` line, with the
D2.js default (3 seeds) and with one seed: 12 layouts. Every commit on the
branch was checked this way (at `GOMAXPROCS=4`, which also exercises the
parallel route-flavor and EdgeLength paths), and the final commit again at
`GOMAXPROCS=1`. All 12 hashes are identical to the base build:

| layout | SHA-256 of the geometry JSON |
|---|---|
| arch_010.tala | `051e1b9159b2b76c25f5f411f28507acb9d8e3138b9335f3c250fdd1361e1dad` |
| arch_010.tala.edit | `a565835db8e9667c38a40095e5770f321f4ea237ff35bcb640101920f53f524c` |
| arch_010.tala-1seed | `8a1cf419f30b152959e3d5cadd459391342ec2cb71b9c09da2798cd658403074` |
| arch_010.tala-1seed.edit | `77d56a21bf259708a1b418091a89d4880e7abf063516b5e0cc821f89c9368bb9` |
| arch_060.tala | `7c646b4b54660b3203535b14ad8ed828796ed93b3bf02497a13ec8b63b925666` |
| arch_060.tala.edit | `3bc794956f4f6b426dadee3343f3902b76b8884c67612dff2a8eb3eab91b289a` |
| arch_060.tala-1seed | `778240d2857aa7d506f9cfbf4974440b0edd6cfc56a8db39223b24540ef3188e` |
| arch_060.tala-1seed.edit | `037652f0bedcbc0fe94a4a15c91d7468b311979ed301f92e076ac32cdd22dd7f` |
| arch_100.tala | `30a9f37c1e79458ae8c070c3f7d8b42e8faeb7d60a7c34abeec6504545383328` |
| arch_100.tala.edit | `4c519b904a6223b6a397fc57bfef23685900d977ca5081553780fffef39f5bec` |
| arch_100.tala-1seed | `70c51aff7c7de595dff673fcfb31193eda843af134f4c85cdef0734bf6f2f91d` |
| arch_100.tala-1seed.edit | `4c161fcdeac78c128d4ddbbcbf151ecdf9d4a2a0b2cd2de4c5a797f11584f890` |

In addition: `go test ./d2layouts/d2talalayout/...` (all TALA layout and
OVG goldens) and D2's `go test ./e2etests/` (which includes ~300 TALA
`board.exp.json` goldens) pass unchanged. Where a change replaces an
algorithm, a randomized test compares it with the original formulation
(answers and charged work units).

## Rules the changes follow

- Same decisions from the same values: no reordering of floating-point
  sums, no change to which cost evaluations run (the whole-graph
  EdgeLength cache sees exactly the same lookups and stores, because its
  key covers only geometry and a skipped evaluation could change what a
  later lookup returns).
- Same work units: a bulk charge is only used where it is provably equal to
  the per-step charges, and through `chargeSkippedRouteWork`, which stops at
  the same limit with the same error and `used` count. Guards that must
  observe every step (synthetic contexts without `Done`, injected per-step
  aggregate budgets) keep the stepped code.
- Map iteration that fed decisions or work accounting is only replaced by
  something order-independent or by a fixed order.

## Changes

| commit | what | why it is exact |
|---|---|---|
| routing: index edge-set lines | `ovgEdgeSet` kept per-line edge lists in a `map[float64]`; now in a slice aligned with the sorted line coordinates | same lists, same scan order and charges |
| routing: dense router state | `pointToRoute` (`map[geo.Point]`), `hasNearbyEdge`, `overlappingRoutes` (`map[*OVGEdge]`) → slices by a per-OVG point ID / edge creation index, map fallback for foreign points/edges | IDs are a bijection on coordinate values (same key equality as the map) |
| placementcost: small node sets | pooled `map[*Node]struct{}` dedup sets in `nodeSymmetry` and the obstruction walk → slice set (map above 32 members) | identical membership; clearing a pooled map cost its peak capacity on every call |
| limits: constant polling strides | `WorkGuard` polled with `used % pollingStride()` (integer division per unit) | same polling points |
| placementcost: incremental crossings | `GraphEdgeCrossings` keeps each edge's inputs and crossing partners in a graph-owned cache; re-evaluates only pairs with a changed edge (inputs compared bit for bit) | every pair whose inputs changed is re-evaluated with the same `SegmentsCross` call; randomized test vs the all-pairs scan |
| placementcost: symmetry memo | per whole-graph `EdgeLength` call, memoize `nodeSymmetry(node, mode)` (the neighbor/child recursion recomputed it once per referring node) | `nodeSymmetry` only reads the graph, which `EdgeLength` does not mutate |
| routing: bulk-charged crossing scans | edge-set scans read contiguous segment extents and charge counted units in bulk | randomized test: same answer and same units as the stepped scan |
| routing: nearby OVG lines | `addRoute` iterated the whole `VerticalEdges`/`HorizontalEdges` map per route segment; now a sorted-key window + the same distance predicate, same total charge | effects never depended on map order; randomized test vs the map scan (router state and work) |
| placementcost: inline FNV | cache key hashed through `hash.Hash` 8 bytes at a time → inline FNV-1a | same bytes, same order; test vs `hash/fnv` |
| routing: port owners slice | per-OVG-node port owners `map` → insertion-ordered slice | all users combine owners order-independently (also makes two early-exit work loops deterministic) |
| layoutgraph: iterative `isDescendantOf` | recursion → loop (falls back to the recursion after 65536 levels, so cyclic ownership fails as before) | same walk |
| routing: hoisted port checks | popped-node port metadata and turn clearances computed once per pop | pure, costs added in the same order |
| bench | `talabench -dump`, `-memprofile` | harness only |

Tried and dropped: a 4-ary heap with inline keys for the route-search queue
(no gain: entries are already chunk-allocated and contiguous), and a
context wrapper with a cached-`Done`/atomic-flag `Err` for the engine (no
measurable gain, and it would stop consulting `Err` of contexts whose `Err`
and `Done` disagree, which `TestLayoutCancellationAfterRouteInvalidationKeepsInput`
relies on).

## Before / after (native, GOMAXPROCS=1)

Layout ms (`talabench -no-edit`, layout time only), base build and this
branch alternated in the same session, median of 5 runs (`arch_010`: Go
benchmark `BenchmarkCorpus`, 30–60 iterations per run, median of 3, because
single cold `talabench` runs of 70–200 ms are dominated by process warm-up).
The machine was shared with other jobs (load 4–8 on 4 vCPUs), so individual
runs vary by ±10%.

| case | engine | base ms | this branch ms | speedup |
|---|---|---:|---:|---:|
| arch_010 | tala (3 seeds) | 220 | 202 | 1.09x |
| arch_010 | tala-1seed | 84 | 82 | 1.03x |
| arch_060 | tala (3 seeds) | 5667 | 4964 | 1.14x |
| arch_060 | tala-1seed | 2229 | 1899 | 1.17x |
| arch_100 | tala (3 seeds) | 20628 | 16304 | 1.27x |
| arch_100 | tala-1seed | 8367 | 6585 | 1.27x |
| arch_200 | tala-1seed (fails, identical error; single run) | 20706 | 15729 | 1.32x |

Larger graphs gain more because the removed costs grow faster than linearly
(per-pair crossing recounts, per-segment map scans, route-search hashing).

## Profile before / after (`arch_100`, one seed, `talabench -cpuprofile`)

Cumulative CPU seconds (the totals include harness and compile work):

| function | base | this branch |
|---|---:|---:|
| total samples | 8.12 | 6.36 |
| `routing.(*ovgEdgeRouter).generateRoutes` | 3.55 | 2.29 |
| `routing.(*ovgEdgeRouter).search` | 2.99 | 1.93 |
| `routing.(*ovgEdgeSet).intersectsWith…` | 0.61 | 0.25 |
| `routing.(*ovgEdgeRouter).addRoute` | 0.26 | 0.07 |
| `placementcost.EdgeLength` (whole graph) | 1.45 | 0.97 |
| `placementcost.nodeSymmetry` | 0.76 | 0.50 |
| `placementcost.GraphEdgeCrossings` | 0.33 | 0.12 |
| `runtime.mapaccess2_fast64` (pointer/float keys) | 0.84 | 0.43 |
| `runtime.mapaccess2` (struct keys, e.g. `geo.Point`) | 0.71 | 0.16 |
| `maps.(*Map).Clear` | 0.13 | 0.05 |
| `limits.(*WorkGuard).Step` | 0.15 | 0.10 |

What remains is spread thinly: the route search's per-relaxation pointer
chasing and priority queue (~30%), per-candidate `evaluateNodeEdgeLength`
(~15%, mostly the bounds-filtered obstruction scan), transaction validation
(~10–15%), and many small map lookups in `layoutgraph`.

## The next exact lever (not done here)

Whole-graph `EdgeLength` is 15–20% of a layout, and almost all of it is
recomputing per-node terms that did not change. Counting on this branch:

| case (1 seed) | whole-graph evaluations (cache misses) | node terms evaluated | node terms whose value changed since that node's previous evaluation |
|---|---:|---:|---:|
| arch_060 | 1332 | 79,920 | 17,444 (22%) |
| arch_100 | 2377 | 237,700 | 32,713 (14%) |

Reusing unchanged node terms exactly needs a sound per-node dependency set:
the node's own geometry, its (abduction-replaced) neighbors, cluster and
sequence vessels, nears, common-uncle siblings, table neighbors, and every
sibling that the bounds-filtered obstruction checks could see — plus
invalidation on any topology change (container membership, cluster
arrangement and abductions, herds, directions), which `layoutgraph` does not
version today (placement writes many of these fields directly). With a
topology version counter in `layoutgraph` and a debug mode that recomputes
and compares every reused term, this would remove most of that 15–20%.

## Observations

- `Transaction.Commit` iterates `g.Containers` (a map) and returns at the
  first bad container, so the transaction work charged on a rejected
  candidate depends on map order. Results are unaffected, but a run that
  ends near `MaxTransactionWorkUnits` (as `arch_500` does) can fail at a
  nondeterministic point. Not changed here (a stable order costs a sort per
  commit); worth fixing together with B5.
