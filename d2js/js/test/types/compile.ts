import {
  D2,
  type CompileOptions,
  type CompileRequest,
  type CompileResponse,
  type RenderOptions,
} from "../../index.js";

declare const d2: D2;
declare const input: string | CompileRequest;

const source = "x -> y";
const request: CompileRequest = { fs: { index: source } };
const options: CompileOptions = {
  layout: "tala",
  sketch: false,
  isometric: true,
};

const defaults: Promise<CompileResponse> = d2.compile(source);
const configured: Promise<CompileResponse> = d2.compile(source, options);
const imported: Promise<CompileResponse> = d2.compile(request);
const overridden: Promise<CompileResponse> = d2.compile(request, options);
const dynamic: Promise<CompileResponse> = d2.compile(input, options);

for (const layout of ["dagre", "elk", "tala"] as const) {
  d2.compile(source, { layout });
  d2.compile({ ...request, inputPath: "index", options: { layout } });
}

configured.then((response) => {
  const renderOptions: RenderOptions = response.renderOptions;
  d2.render(response.diagram, renderOptions);
  d2.render(response.diagram, { ...renderOptions, isometric: false });

  // @ts-expect-error The response exposes renderOptions, not options.
  response.options;
});

// @ts-expect-error Compile options belong directly in the second argument.
d2.compile(source, { options: { layout: "tala" } });
// @ts-expect-error Object input uses the same flat second-argument options.
d2.compile(request, { options: { layout: "tala" } });
// @ts-expect-error Unsupported layout names are rejected.
d2.compile(source, { layout: "unknown" });
// @ts-expect-error An object input must supply its file contents.
d2.compile({ inputPath: "index" });
// @ts-expect-error Layout options belong in input.options, not the request root.
d2.compile({ fs: { index: source }, layout: "tala" });

d2.compile(source, {
  layout: "elk",
  elk: { inLayerNodeSpacing: 30, inLayerEdgeSpacing: 5, inLayerEdgeNodeSpacing: 20, padding: 15 },
});
d2.compile(source, { layout: "elk", elk: { padding: "[top=30,left=10,bottom=10,right=10]" } });
d2.compile(source, { layout: "dagre", dagre: { nodeSep: 40, rankSep: 60, padding: 10 } });
d2.compile(source, { layout: "dagre", dagre: { padding: "[top=20,left=5,bottom=5,right=5]" } });
// @ts-expect-error Padding is a number or a string.
d2.compile(source, { layout: "dagre", dagre: { padding: true } });
// @ts-expect-error ELK options use D2.js names, not the diagram's data keys.
d2.compile(source, { layout: "elk", elk: { "elk-nodeNode": 30 } });
