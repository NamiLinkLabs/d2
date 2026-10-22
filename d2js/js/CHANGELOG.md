# Changelog

All notable changes to only the d2.js package will be documented in this file. **Does not
include changes to the main d2 project.**

## Next

- Add `D2.routeEdges()`, which routes connections with TALA's edge router around shapes placed by the caller
- Support `source-port` / `target-port` connection pins: compiled connections carry `srcPort` / `dstPort` (`"top" | "right" | "bottom" | "left"`), `routeEdges` keeps rerouted connections on their pinned sides, and completions suggest the sides
- Route connections that cross into or out of grids, `near` shapes and sequence diagrams, and connections between grid cells, around the other shapes with every layout engine. This is on by default, as in the CLI, so `compile()` gives the same diagram as `d2`. Routing stops at a work budget that grows with the diagram, past which the remaining connections are drawn straight, so it costs at most about 1.7 times the old compile time on large diagrams (`nesting_power`: 0.24 s to 0.40 s) and a few tens of milliseconds on small ones; the budget counts work, not time, so a diagram always compiles the same. Add the `nestedEdgeRouting` compile option: `"straight"` draws them as straight lines through the other shapes again, at the old speed; use it when compile time matters more, or when the caller reroutes the connections itself, for example with `D2.routeEdges()`. A diagram can set the same with `vars.d2-config.nested-edge-routing`; the option takes precedence.
- Type the `explicitThemeFill`, `explicitThemeStroke` and `explicitThemeFontColor` diagram fields, which mark theme color codes (such as `style.fill: B2`) chosen in the source rather than applied by the theme
- Tune ELK, dagre and TALA with the `elk`, `dagre` and `tala` compile options. ELK gains in-layer node, edge and edge-node spacing; dagre gains `rankSep` and container `padding`; padding takes a number or `[top=..,left=..,bottom=..,right=..]`. Layout options a diagram sets under `vars.d2-config.data` take precedence, and invalid ones are compile errors with a source range.
- Add the `intersectionGaps` render option, which leaves empty gaps where connections cross; compile resolves it from `vars.d2-config.intersection-gaps` into `renderOptions` [#1932](https://github.com/d2lang/d2/issues/1932)
- Add `labelPadding` to connections in the diagram, from D2's new `label.padding`
- Add the `embedSource` compile option, which embeds the D2 source in rendered SVGs, and `D2.extractSource()` / `D2.embedSource()` to read it back from SVG and PNG images or add it to other images
- Add `iconWidth` and `iconHeight` to compiled shapes and connections that set `icon.width` or `icon.height`

## [0.1.34]
### September 7, 2026

- Support D2 0.9.0, including the bundled open-source TALA layout engine (`layout: "tala"`) and configurable TALA seeds.
- Add `D2.dispose()` for terminating the backing worker
- Fix concurrent calls sharing a D2 instance
- Fix completions after Unicode characters
- Deprecate the raw `d2.getELKGraph` and `d2.getObjOrder` compatibility exports. They remain callable for one release and emit one migration warning per export.
- Fix TypeScript signatures
- Fix theme-overrides not applying
- Fix grids in ELK layouts
- Significantly reduce bundle size

## [0.1.22]
### March 20, 2025

- Support `d2-config`. Support additional options. [#2343](https://github.com/terrastruct/d2/pull/2343)
  - `themeID`
  - `darkThemeID`
  - `center`
  - `pad`
  - `scale`
  - `forceAppendix`
  - `target`
  - `animateInterval`
  - `salt`
  - `noXMLTag`
- Support relative imports. Improve elk error handling [#2382](https://github.com/terrastruct/d2/pull/2382)
- Support fonts (`fontRegular`, `fontItalic`, `fontBold`, `fontSemiBold`) [#2384](https://github.com/terrastruct/d2/pull/2384)
- Add TypeScript signatures

## [0.1.21]
### January 12, 2025

First public release
