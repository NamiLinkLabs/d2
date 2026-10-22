# Separate edges

By default TALA bundles connections that share an endpoint
([terrastruct/TALA#21](https://github.com/terrastruct/TALA/issues/21)):

- the OVG router lets directed edges with a common source or target, and
  undirected or bidirectional edges of one cluster, share segments for free;
- tree edges are pre-routed as an S-shaped bus out of the parent's center port;
- cluster fans are pulled onto one shared port, and `FixClusterEdgeBranching`
  merges them into one trunk;
- `BalanceEdgeSegments` keeps segments of different edges that lie within 1px
  of each other together, as one shared route.

`Options.SeparateEdges` (`tala-separate-edges: true` in
`vars.d2-config.data`, `--tala-separate-edges`, D2.js `tala.separateEdges`)
turns all of these off. Sharing a segment then costs `sharedRouteCost`
(`basicallyInfinity`), like any other forbidden overlap, in the OVG search,
the slingshot and the standalone router (`RouteEdgesWithOptions`). Tree edges
go through the OVG router; the tree's bus lines and parent-aligned child ports
stay in the OVG, so a child aligned with its parent still gets a straight
route. The penalty is soft: when a side has more connections than free ports,
routes may still overlap.

## Measurements

Native, `GOMAXPROCS=1`, single runs, `talabench -no-edit` at the commit that
added this file. `shared` is `Metrics.SharedLength` (see `BASELINE.md`).
`crossings` counts the points where a bundle branches as crossings, because the
branches properly cross the trunk there; that is why separate edges also show
fewer crossings.

| case | engine | layout ms | crossings | bends | edge length | shared | bbox |
|---|---|---:|---:|---:|---:|---:|---|
| arch_010 | tala-1seed | 143 | 0 | 3 | 1799 | 0 | 1100x640 |
| arch_010 | tala-separate-1seed | 95 | 0 | 5 | 1886 | 0 | 1100x640 |
| arch_010 | tala | 244 | 0 | 3 | 1753 | 0 | 655x867 |
| arch_010 | tala-separate | 289 | 0 | 3 | 1753 | 0 | 655x867 |
| arch_060 | tala-1seed | 2814 | 54 | 62 | 107690 | 11365 | 3404x8441 |
| arch_060 | tala-separate-1seed | 3799 | 51 | 68 | 104064 | 76 | 3404x8441 |
| arch_060 | tala | 6917 | 50 | 49 | 93544 | 10332 | 6659x3490 |
| arch_060 | tala-separate | 7959 | 42 | 49 | 89665 | 0 | 6659x3490 |
| arch_100 | tala-1seed | 9505 | 133 | 103 | 185019 | 17292 | 6289x6686 |
| arch_100 | tala-separate-1seed | 11056 | 116 | 115 | 169650 | 30 | 6238x6005 |
| arch_100 | tala | 23661 | 107 | 88 | 140568 | 34910 | 5355x5364 |
| arch_100 | tala-separate | 25317 | 81 | 106 | 153459 | 0 | 5589x5450 |

Small diagrams (`tala`, three seeds; the one-seed rows are the same except
where noted):

| diagram | default crossings / bends / shared | separate crossings / bends / shared |
|---|---|---|
| fan-out and fan-in, `lb -> web1..3 -> db` | 6 / 8 / 312 | 0 / 4 / 0 |
| fan-out `client -> api, cdn, auth`, fan-in `api, worker, cron -> db` | 2 / 8 / 100 | 0 / 4 / 0 |
| six services fanning into `db` and `cache` from `api` | 7 / 17 / 481 (1 seed: 8 / 17 / 612) | 1 / 17 / 0 |
| TALA#75's container fan-out and fan-in | 1 / 8 / 124 | 2 / 9 / 0 |
| org-chart tree, `ceo -> cto, cfo, coo`, `cto -> eng1..3` | 6 / 8 / 300 | 0 / 5 / 0 |

Summary:

- Shared route length drops to 0 on every three-seed run. The one-seed
  residuals are 76px on `arch_060`, two edges with no common endpoint that
  overlap in the default layout too, and 30px on `arch_100`, two of the many
  edges out of `data.kafka` forced through one port.
- Crossings fall (`arch_100`: 107 to 81) because bundles branch with
  T-junctions the metric counts as crossings. Bends rise slightly on the
  one-seed runs (`arch_060` 62 to 68, `arch_100` 103 to 115) and with three
  seeds on `arch_100` (88 to 106), since separate routes leave through
  non-center ports.
- Runtime grows 7 to 35% on `arch_060` and `arch_100` (single runs; the
  `arch_010` difference is noise): the router explores more of the OVG when
  the cheap shared paths are penalized.
- With the flag unset every code path is unchanged; the engine goldens and the
  default rows above match `BASELINE.md`.

## How the residual sharing was found

Instrumenting each pipeline stage with the shared length of the layout graph
showed two sources after the router itself: `FixClusterEdgeBranching` (now
skipped) and `BalanceEdgeSegments`, which moved two near-coincident segments
(0.5px apart after `SwapEdgePorts` cut a crossing at `data.kafka`) onto one
line. With separate edges, balancing only keeps an edge's own segments
together.
