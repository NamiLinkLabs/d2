import {
  D2,
  type CompileOptions,
  type CompileRequest,
  type CompileResponse,
  type EmbeddedSource,
  type ExtractedSource,
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
  const shapeIconWidth: number | undefined = response.diagram.shapes[0].iconWidth;
  const shapeIconHeight: number | undefined = response.diagram.shapes[0].iconHeight;
  const connectionIconWidth: number | undefined = response.diagram.connections[0].iconWidth;
  const connectionIconHeight: number | undefined = response.diagram.connections[0].iconHeight;
  void [shapeIconWidth, shapeIconHeight, connectionIconWidth, connectionIconHeight];
  const renderOptions: RenderOptions = response.renderOptions;
  d2.render(response.diagram, renderOptions);
  d2.render(response.diagram, { ...renderOptions, isometric: false });
  d2.render(response.diagram, { ...renderOptions, intersectionGaps: true });
  const gaps: boolean | undefined = renderOptions.intersectionGaps;

  // @ts-expect-error The response exposes renderOptions, not options.
  response.options;

  const shape = response.diagram.shapes[0];
  const themedShape: boolean | undefined =
    shape.explicitThemeFill ?? shape.explicitThemeStroke ?? shape.explicitThemeFontColor;
  const edge = response.diagram.connections[0];
  const themedEdge: boolean | undefined = edge.explicitThemeStroke ?? edge.explicitThemeFontColor;
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

const embedded: Promise<CompileResponse> = d2.compile(source, { embedSource: true });
embedded.then(async (result) => {
  const svg: string = await d2.render(result.diagram, result.renderOptions);
  const extracted: ExtractedSource | null = await d2.extractSource(svg);
  if (extracted) {
    const again: CompileResponse = await d2.compile(extracted);
    const sourceOfImage: EmbeddedSource = extracted.source;
    const pngOut: Uint8Array = await d2.embedSource(new Uint8Array(), sourceOfImage);
    const svgOut: string = await d2.embedSource(svg, sourceOfImage);
    void again;
    void pngOut;
    void svgOut;
  }
});
