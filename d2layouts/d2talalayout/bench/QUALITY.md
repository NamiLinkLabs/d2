# TALA quality levels

`d2talalayout.Options.Quality` (Go), `tala.quality` (D2.js compile options) and
`tala-quality` (diagram data, `vars: {d2-config: {data: {tala-quality: fast}}}`)
select a deterministic speed/quality trade-off. This file records how the
levels were chosen and what they cost. Baseline numbers and metric definitions
are in [BASELINE.md](BASELINE.md).

## Levels

| level | seeds | per-seed pipeline | meant for |
|---|---|---|---|
| `full` (default) | every configured seed (default 1, 2, 3) | unchanged | final layout, export, idle |
| `balanced` | every configured seed | reduced | idle re-layout of large diagrams |
| `fast` | only the first configured seed | reduced | interactive previews, e.g. while dragging |

The reduced pipeline (`reducedEffort` in `quality.go`, `engine.Effort`):

- node-placement annealing runs 30 × √n iterations per subgraph instead of
  90 × √n;
- edge routing searches one edge ordering (`ShortestToLongest`) instead of
  three (ShortestToLongest, LongestToShortest, Default) and keeping the
  shortest; this also applies to the compound-candidate reroute;
- the SwapStuff and GapNormalization stages are skipped.

Everything else, including the compound-container refinement, pins, labels and
validation, runs as at `full`. Every lever is a fixed count or switch, never a
time budget, so each level is as deterministic as `full` (same input and
options give byte-identical output, independent of `MaxConcurrency`).

Precedence, for both seeds and quality: diagram data (`tala-seeds`,
`tala-quality`), then options, then package defaults. Seeds are resolved and
validated first; `fast` then keeps only the first one. With one configured seed
`balanced` and `fast` are the same layout. `full` is byte-identical to the
engine before this change (checked with the d2 CLI on `arch_010/060/100` SVGs
and by the unchanged layout goldens).

## Results (native, GOMAXPROCS=1)

Shipped defaults (seeds 1, 2, 3; `fast` runs seed 1). `base` is `boxwire` @
`5bdad71`, `new` is this branch; both binaries measured back to back,
interleaved, in the same session, median of 3 runs (all three runs listed).
The machine was shared with other jobs (load 3–7 on 4 CPUs), so absolute
times are noisy; ratios within one row group are reliable. Quality metrics
are deterministic and identical across the 3 runs.

| case | build | engine | layout ms (median) | runs | speedup vs base `tala` | crossings | bends | edge length | bbox | empty % |
|---|---|---|---:|---|---:|---:|---:|---:|---|---:|
| arch_010 | base | tala | 237 | 208/239/237 | 1.0x | 0 | 3 | 1753 | 655x867 | 24.5 |
| arch_010 | base | tala-1seed | 87 | 100/87/85 | 2.7x | 0 | 3 | 1799 | 1100x640 | 40.4 |
| arch_010 | new | tala (`full`) | 238 | 237/255/238 | 1.0x | 0 | 3 | 1753 | 655x867 | 24.5 |
| arch_010 | new | tala-balanced | 116 | 98/116/141 | 2.1x | 0 | 4 | 1792 | 916x647 | 22.5 |
| arch_010 | new | tala-fast | 36 | 37/36/36 | 6.6x | 0 | 4 | 1792 | 916x647 | 22.5 |
| arch_060 | base | tala | 6011 | 5836/6011/6105 | 1.0x | 50 | 49 | 93544 | 6659x3490 | 24.6 |
| arch_060 | base | tala-1seed | 2158 | 2213/2055/2158 | 2.8x | 54 | 62 | 107690 | 3404x8441 | 47.4 |
| arch_060 | new | tala (`full`) | 5882 | 5954/5758/5882 | 1.0x | 50 | 49 | 93544 | 6659x3490 | 24.6 |
| arch_060 | new | tala-balanced | 2386 | 2414/2386/2330 | 2.5x | 60 | 57 | 75400 | 4621x6196 | 62.9 |
| arch_060 | new | tala-fast | 979 | 979/949/991 | **6.1x** | 60 | 57 | 75400 | 4621x6196 | 62.9 |
| arch_100 | base | tala | 20250 | 20250/19653/20468 | 1.0x | 107 | 88 | 140568 | 5355x5364 | 12.6 |
| arch_100 | base | tala-1seed | 8629 | 8900/8629/8448 | 2.3x | 133 | 103 | 185019 | 6289x6686 | 31.6 |
| arch_100 | new | tala (`full`) | 19801 | 19801/19617/20643 | 1.0x | 107 | 88 | 140568 | 5355x5364 | 12.6 |
| arch_100 | new | tala-balanced | 7722 | 7827/7227/7722 | 2.6x | 108 | 91 | 152908 | 6660x5184 | 26.6 |
| arch_100 | new | tala-fast | 3003 | 3064/2651/3003 | **6.7x** | 108 | 91 | 152908 | 6660x5184 | 26.6 |

Target check (`arch_060`): `fast` is 6.1x faster than the D2.js default
(`tala`, 3 seeds) with 60 vs 50 crossings (1.2x). Edge length is 19% shorter
and bends 16% more. Empty area is much worse (62.9% vs 24.6%): seed 1 is an
outlier for that metric under the reduced pipeline (mean over 8 seeds 35.3%,
see below).

With the default seeds, `balanced` selects the same seed-1 layout as `fast` on
both `arch_060` and `arch_100` (seed 1 has the best score of the three), so
here it costs 2.5x the time of `fast` for no change. Its benefit shows across
seed sets (next table): best-of-3 selection at the reduced cost.

## Robustness across seeds (native, means)

One seed is a noisy sample: on `arch_060` the full pipeline gives 46–83
crossings depending on the seed. To avoid choosing levers that only suit
seed 1, every candidate was also run on other seeds: single-seed levels on
seeds 1–8, three-seed levels on seed sets {1,2,3}, {4,5,6}, {7,8,9}. Times
here come from runs with two jobs in parallel and are only indicative.

| case | configuration | runs | ms | crossings mean (per run) | bends | edge length | empty % |
|---|---|---:|---:|---|---:|---:|---:|
| arch_060 | `full` (3 seeds) | 3 sets | 5414 | 49.3 (50 52 46) | 53.0 | 84528 | 27.8 |
| arch_060 | 1 seed, full pipeline | 8 seeds | 2823 | 64.4 (54 83 50 76 52 76 46 78) | 58.1 | 88920 | 35.5 |
| arch_060 | `balanced` | 3 sets | 2513 | 55.7 (60 55 52) | 62.0 | 78653 | 39.8 |
| arch_060 | `fast` | 8 seeds | 943 | 67.4 (60 73 92 66 66 55 52 75) | 57.8 | 74052 | 35.3 |
| arch_100 | `full` (3 seeds) | 3 sets | 18888 | 106.7 (107 88 125) | 87.0 | 158327 | 14.9 |
| arch_100 | 1 seed, full pipeline | 8 seeds | 10365 | 124.0 (133 124 107 131 88 139 145 125) | 92.6 | 162820 | 24.9 |
| arch_100 | `balanced` | 3 sets | 7179 | 95.3 (108 86 92) | 90.7 | 157269 | 22.7 |
| arch_100 | `fast` | 8 seeds | 2807 | 123.9 (108 125 116 86 116 186 92 162) | 92.1 | 156566 | 24.9 |
| arch_010 | all levels | | 38–214 | 0 | 3–4 | 1753–2018 | 24–29 |

Across seeds `fast` costs about as many crossings as a single full-pipeline
seed (`arch_060` 67.4 vs 64.4, `arch_100` 123.9 vs 124.0) at a third of its
time; against `full` that is 1.37x (`arch_060`) and 1.16x (`arch_100`).
`balanced` is 1.13x `full` on `arch_060` and better than `full` on `arch_100`
(95.3 vs 106.7), at 2.5–2.6x less time.

## How the levers were chosen

Single levers on top of one seed, seed 1 only (native; times noisy):

| change (1 seed) | arch_060 crossings / length | arch_100 crossings / length |
|---|---|---|
| none | 54 / 107690 | 133 / 185019 |
| placement iterations 90 → 30 | 44 / 58117 | 135 / 143134 |
| placement iterations 90 → 15 | 56 / 81366 | 119 / 178747 |
| one route flavor (Default) | 50 / 107168 | 152 / 192439 |
| one route flavor (ShortestToLongest) | 54 / 107690 | 133 / 185019 |
| one route flavor (LongestToShortest) | 51 / 106870 | 146 / 187525 |
| skip SwapStuff | 62 / 98963 | 160 / 175846 |
| skip compound candidate | 83 / 72105 | 131 / 151548 |
| skip Transpose | 57 / 107312 | 145 / 158932 |
| skip GapNormalization | 52 / 84295 | 139 / 147990 |
| skip Equidistance | 55 / 110271 | 143 / 195472 |

Combinations, means over seeds 1–8 (arch_060 / arch_100 crossings, approximate
ms per layout):

| combination (1 seed) | arch_060 | arch_100 |
|---|---|---|
| iterations 30, 1 flavor (Default) | 67.6 / 1450 ms | 122.0 / 4338 ms |
| + skip SwapStuff, GapNormalization, compound | 74.9 / 816 ms | 134.4 / 2276 ms |
| + keep compound (= reduced pipeline, Default flavor) | 71.1 / 924 ms | 125.6 / 2555 ms |
| reduced, flavor ShortestToLongest (**chosen `fast`**) | 67.4 / 943 ms | 123.9 / 2807 ms |
| reduced, flavor LongestToShortest | 78.8 / 1003 ms | 133.6 / 2630 ms |

Further levers, measured on seeds 1–4 against "iterations 30, 1 flavor
(Default), skip SwapStuff, GapNormalization and compound" (69.8 / 828 ms,
126.2 / 1915 ms on those seeds):

| change | arch_060 | arch_100 |
|---|---|---|
| iterations 20 | 70.0 / 645 ms | 137.8 / 2234 ms |
| also skip Dejitter (and its second routing pass) | 72.5 / 695 ms | 126.2 / 1914 ms |
| AlignAxes at most 1 round per stage | 76.0 / 726 ms | 126.8 / 1644 ms |
| also skip Transpose and Equidistance, AlignAxes ≤ 2 rounds | 75.2 / 594 ms | 127.2 / 1284 ms |
| keep SwapStuff | 69.0 / 997 ms | 126.5 / 3246 ms |

| combination (3 seeds, sets {1,2,3} {4,5,6} {7,8,9}) | arch_060 | arch_100 |
|---|---|---|
| reduced, Default flavor | 60.3 / 2523 ms | 100.0 / 7016 ms |
| reduced, keep SwapStuff | 53.3 / 3208 ms | 115.7 / 9941 ms |
| reduced, keep GapNormalization | 61.0 / 3165 ms | 110.3 / 8977 ms |
| reduced, iterations 45 | 56.3 / 2602 ms | 128.7 / 7418 ms |
| 2 seeds ({1,2} {3,4} {5,6} {7,8}), reduced | 65.2 / 1765 ms | 110.5 / 4695 ms |

Findings:

- Seed selection is the strongest quality lever: best of three reduced
  pipelines beats a single full pipeline in both crossings and time.
- The compound-container refinement is cheap at the reduced level and removes
  crossings on container-heavy graphs, so every level keeps it.
- SwapStuff and GapNormalization cost 20–35% of a reduced seed without a
  consistent gain across seeds; Transpose, Equidistance, Dejitter and capping
  AlignAxes save more time but cost crossings, so they stay.
- ShortestToLongest is the best single route flavor across seeds.

## D2.js (WASM)

Local D2.js builds of `base` and `new` (same build commands as BASELINE.md),
Node 22, `d2.compile` wall time after a warm-up compile, one compile per row
per run, base and new runs interleaved, median of 3.

| case | base `tala` (default) | new default (`full`) | new `balanced` | new `fast` | `fast` speedup vs base |
|---|---:|---:|---:|---:|---:|
| arch_010 | 1452 (1452/1601/1127) | 1125 (1267/1098/1125) | 431 (407/431/440) | 177 (182/177/177) | 8.2x |
| arch_060 | 24850 (24945/24850/24467) | 25064 (25683/25064/24719) | 9938 (9840/10562/9938) | **4535** (4535/4729/3985) | **5.5x** |
| arch_100 | 85380 (one run) | not run | 30447 (31026/30447/30171) | 13832 (13832/14168/10395) | 6.2x |

ms, median (runs). `full` matches base within noise, as expected for the
unchanged code path. `fast` positions in WASM are identical to native `fast`
on `arch_060` (every shape position compared). In the browser `fast` is therefore still
about 4.5 s for 60 objects, far from the B1 target of 200 ms: quality levels
are the configuration part of B1, the rest needs the algorithmic levers in
BASELINE.md (warm start, incremental costs, routing).

## Validation

- `full`: layout goldens unchanged; d2 CLI SVGs for `arch_010/060/100`
  byte-identical to `base`.
- Reduced pipeline: `internal/engine/effort_test.go` lays out all 52 layout
  and regression fixtures and checks completion, layout invariants (two
  fixtures, `large_hierarchy` and `self_edges_large`, already violate them
  identically with the full pipeline) and byte-identical repeat runs.
- A throwaway sweep of the whole D2 E2E corpus (`TestE2E`, TALA only, no
  golden comparison) at `fast` with seeds 1, 2 and 3 produced no layout errors
  beyond the cases that expect one, the same as `full` with those seeds.
- Pins: absolute root and nested pins are exact at `balanced` and `fast`
  (`quality_test.go`).

## Limitations

- The levels are fixed configurations, not a time budget: `fast` on a 500-object
  graph is still slow. Combine with a caller-side context deadline if a hard
  bound is needed (TALA honours cancellation).
- `fast` does not fix the large-graph failures (BASELINE.md), it reaches them
  sooner: `arch_200` fails with the same route-search work limit after 5.9 s
  (`balanced` 17.6 s; 1 seed `full` 18 s); `arch_500` fails after 38 s with an
  OVG intersection-candidate limit (`balanced` 128 s) instead of the 1-seed
  `full` transaction limit after 106 s.
- The levers were chosen on three generated architecture diagrams; other
  diagram families may prefer other trade-offs.
