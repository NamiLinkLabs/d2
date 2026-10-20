export class D2 {
  /** Options in input.options take precedence over the second argument. */
  compile(
    input: string | CompileRequest,
    options?: CompileOptions
  ): Promise<CompileResponse>;

  render(diagram: Diagram, options?: RenderOptions): Promise<string>;

  /**
   * Routes connections with TALA's obstacle-avoiding edge router between shapes
   * whose geometry the caller has already decided, e.g. after moving boxes in a
   * compiled diagram. The D2 source is compiled but not laid out; every shape
   * must be given its final box.
   *
   * Routes are orthogonal polylines from the source shape's outline to the
   * destination's. To draw one, set it as the connection's `route` and set its
   * `isCurve` to false.
   *
   * Invalid requests (unknown or repeated ids, missing shapes, non-finite
   * numbers, sizes that are not positive) reject. Connections TALA cannot route
   * in time or at all are reported in `errors`; the others still get routes.
   * Connections from a shape to itself are always reported there, and
   * sequence diagram lifelines are unknown ids: they only exist after layout.
   */
  routeEdges(request: RouteEdgesRequest): Promise<RouteEdgesResponse>;

  /** Terminates the backing worker and rejects any pending operations. */
  dispose(): Promise<void>;
}

export interface RenderOptions {
  /** Enable sketch mode [default: false] */
  sketch?: boolean;
  /** Enable native isometric SVG rendering, preserving the compiled layout [default: false]. Icons and images must use embedded data URLs. Cannot be combined with sketch, ASCII, darkThemeID, forceAppendix, or animateInterval. */
  isometric?: boolean;
  /** Theme ID to use [default: 0] */
  themeID?: number;
  /** Theme ID to use when client is in dark mode */
  darkThemeID?: number;
  /** Center the SVG in the containing viewbox [default: false] */
  center?: boolean;
  /** Pixels padded around the rendered diagram [default: 100] */
  pad?: number;
  /** Scale the output. E.g., 0.5 to halve the default size. The default will render SVG's that will fit to screen. Setting to 1 turns off SVG fitting to screen. */
  scale?: number;
  /** Adds an appendix for tooltips and links [default: false] */
  forceAppendix?: boolean;
  /** Target board/s to render. If target ends with '', it will be rendered with all of its scenarios, steps, and layers. Otherwise, only the target board will be rendered. E.g. target: 'layers.x.*' to render layer 'x' with all of its children. Pass '' to render all scenarios, steps, and layers. By default, only the root board is rendered. Multi-board outputs are currently only supported for animated SVGs and so animateInterval must be set to a value greater than 0 when targeting multiple boards. */
  target?: string;
  /** If given, multiple boards are packaged as 1 SVG which transitions through each board at the interval (in milliseconds). */
  animateInterval?: number;
  /** Add a salt value to ensure the output uses unique IDs. This is useful when generating multiple identical diagrams to be included in the same HTML doc, so that duplicate IDs do not cause invalid HTML. The salt value is a string that will be appended to IDs in the output. */
  salt?: string;
  /** Omit XML tag (<?xml ...?>) from output SVG files. Useful when generating SVGs for direct HTML embedding. */
  noXMLTag?: boolean;
  /** Render as ASCII instead of SVG [default: false] */
  ascii?: boolean;
  /** ASCII rendering mode for text outputs. Options: 'standard' (basic ASCII chars) or 'extended' (Unicode chars) [default: 'extended'] */
  asciiMode?: "standard" | "extended";
}

export interface CompileOptions extends RenderOptions {
  /** Layout engine to use [default: 'dagre'] */
  layout?: "dagre" | "elk" | "tala";
  /** ELK tuning, used when layout is 'elk'. Unset fields keep D2's defaults. */
  elk?: ELKOptions;
  /** dagre tuning, used when layout is 'dagre'. Unset fields keep D2's defaults. */
  dagre?: DagreOptions;
  /** TALA tuning, used when layout is 'tala'. Unset fields keep TALA's defaults. */
  tala?: TALAOptions;
  /** A byte array containing .ttf file to use for the regular font. If none provided, Source Sans Pro Regular is used. */
  fontRegular?: Uint8Array;
  /** A byte array containing .ttf file to use for the italic font. If none provided, Source Sans Pro Italic is used. */
  fontItalic?: Uint8Array;
  /** A byte array containing .ttf file to use for the bold font. If none provided, Source Sans Pro Bold is used. */
  fontBold?: Uint8Array;
  /** A byte array containing .ttf file to use for the semibold font. If none provided, Source Sans Pro Semibold is used. */
  fontSemibold?: Uint8Array;
}

export interface ELKOptions {
  /** Space between nodes in neighbouring layers [default: 70] */
  nodeSpacing?: number;
  /** Space between edges and nodes [default: 40] */
  edgeNodeSpacing?: number;
  /** Space between parallel edges [default: 50] */
  edgeEdgeSpacing?: number;
  /** Space around self-loops [default: 50] */
  selfLoopSpacing?: number;
  /** Container padding in ELK syntax [default: '[top=50,left=50,bottom=50,right=50]'] */
  padding?: string;
}

export interface DagreOptions {
  /** Horizontal space between nodes of the same rank [default: 60] */
  nodeSep?: number;
  /** Horizontal space between edges [default: 20] */
  edgeSep?: number;
}

export interface TALAOptions {
  /** Deterministic layout attempts. `tala-seeds` in the diagram still takes precedence. */
  seeds?: number[];
}

export interface CompileRequest {
  /** A mapping of D2 file paths to their content*/
  fs: Record<string, string>;
  /** The path of the entry D2 file [default: index]*/
  inputPath?: string;
  /** The CompileOptions to pass to the compiler*/
  options?: CompileOptions;
}

export interface RouteEdgesRequest {
  /** A mapping of D2 file paths to their content */
  fs: Record<string, string>;
  /** The path of the entry D2 file [default: index] */
  inputPath?: string;
  /**
   * The options the diagram was compiled with. Fonts, theme and sketch change
   * how labels are measured; layout picks the default label positions for
   * shapes that don't give one.
   */
  options?: CompileOptions;
  /** The final box of every shape of the root board, as in diagram.shapes. */
  shapes: RouteShape[];
  /** Ids of the connections to route, as in diagram.connections[i].id, e.g. "(a -> b)[0]" or "g.(q -> r)[0]". */
  edges: string[];
  /**
   * Current routes of other connections, which stay as they are. TALA spreads
   * the new routes and labels around them; connections left out are ignored
   * while routing. diagram.connections can be passed as is: entries listed in
   * `edges`, unknown ids and unusable routes are skipped.
   */
  connections?: RouteConnection[];
  /** Time budget for the whole call, in milliseconds [default: 1500, max: 60000] */
  timeoutMs?: number;
}

export interface RouteShape {
  /** Shape id, as in diagram.shapes[i].id */
  id: string;
  /** Absolute top-left corner, as in diagram.shapes[i].pos */
  x: number;
  y: number;
  width: number;
  height: number;
  /** As in diagram.shapes[i].labelPosition. When left out, the layout engine's default is used. */
  labelPosition?: string;
  /** As in diagram.shapes[i].iconPosition. When left out, the layout engine's default is used. */
  iconPosition?: string;
}

export interface RouteConnection {
  id: string;
  route: RoutePoint[];
  labelPosition?: string;
  labelPercentage?: number;
}

export interface RoutePoint {
  x: number;
  y: number;
}

export interface RouteLabel {
  /** Goes in the connection's labelPosition */
  position: string;
  /** Goes in the connection's labelPercentage */
  percentage: number;
}

export interface RouteEdgesResponse {
  /** New route of each routed connection, keyed by connection id */
  routes: Record<string, RoutePoint[]>;
  /** Where TALA placed the label of each routed connection that has one */
  labels?: Record<string, RouteLabel>;
  /** Why each requested connection without a route got none, e.g. the time budget ran out */
  errors?: Record<string, string>;
}

export interface CompileResponse {
  /**  Compiled D2 diagram*/
  diagram: Diagram /* d2target.Diagram */;
  /** RenderOptions: Render options merged with configuration set in diagram*/
  renderOptions: RenderOptions;
  fs: Record<string, string>;
  graph: Graph;
  inputPath: string;
}

export interface Diagram {
  config?: RenderOptions;
  name: string;
  /**
   * See docs on the same field in d2graph to understand what it means.
   */
  isFolderOnly: boolean;
  description?: string;
  fontFamily?: any /* d2fonts.FontFamily */;
  shapes: Shape[];
  connections: Connection[];
  root: Shape;
  legend?: Legend;
  layers?: (Diagram | undefined)[];
  scenarios?: (Diagram | undefined)[];
  steps?: (Diagram | undefined)[];
}

export interface Legend {
  shapes?: Shape[];
  connections?: Connection[];
}

export type Shape = (Class | SQLTable | Text) & ShapeBase;

export interface ShapeBase {
  id: string;
  type: string;
  classes?: string[];
  pos: Point;
  width: number /* int */;
  height: number /* int */;
  opacity: number /* float64 */;
  strokeDash: number /* float64 */;
  strokeWidth: number /* int */;
  borderRadius: number /* int */;
  fill: string;
  fillPattern?: string;
  stroke: string;
  animated: boolean;
  shadow: boolean;
  "3d": boolean;
  multiple: boolean;
  "double-border": boolean;
  tooltip: string;
  link: string;
  prettyLink?: string;
  icon?: string /* url.URL */;
  iconPosition: string;
  /**
   * Whether the shape should allow shapes behind it to bleed through
   * Currently just used for sequence diagram groups
   */
  blend: boolean;
  contentAspectRatio?: number /* float64 */;
  labelPosition?: string;
  zIndex: number /* int */;
  level: number /* int */;
  /**
   * These are used for special shapes, sql_table and class
   */
  primaryAccentColor?: string;
  secondaryAccentColor?: string;
  neutralAccentColor?: string;
}

export interface Point {
  x: number /* int */;
  y: number /* int */;
}

export interface Class {
  fields: ClassField[];
  methods: ClassMethod[];
}

export interface ClassField {
  name: string;
  type: string;
  visibility: string;
}

export interface ClassMethod {
  name: string;
  return: string;
  visibility: string;
}

export interface SQLTable {
  columns: SQLColumn[];
}

export interface SQLColumn {
  name: Text;
  type: Text;
  constraint: string[];
  reference: string;
}

export interface Text {
  label: string;
  fontSize: number /* int */;
  fontFamily: string;
  language: string;
  color: string;
  italic: boolean;
  bold: boolean;
  underline: boolean;
  labelWidth: number /* int */;
  labelHeight: number /* int */;
  labelFill?: string;
}

export interface Connection extends Text {
  id: string;
  classes?: string[];
  src: string;
  srcArrow: Arrowhead;
  srcLabel?: Text;
  dst: string;
  dstArrow: Arrowhead;
  dstLabel?: Text;
  opacity: number /* float64 */;
  strokeDash: number /* float64 */;
  strokeWidth: number /* int */;
  stroke: string;
  fill?: string;
  borderRadius?: number /* float64 */;
  labelPosition: string;
  labelPercentage: number /* float64 */;
  link: string;
  prettyLink?: string;
  route: (any /* geo.Point */ | undefined)[];
  isCurve?: boolean;
  animated: boolean;
  tooltip: string;
  icon?: string /* url.URL */;
  iconPosition?: string;
  zIndex: number /* int */;
}

export type Arrowhead =
  | "none"
  | "arrow"
  | "unfilled-triangle"
  | "triangle"
  | "diamond"
  | "filled-diamond"
  | "circle"
  | "filled-circle"
  | "box"
  | "filled-box"
  | "line"
  | "cf-one"
  | "cf-many"
  | "cf-one-required"
  | "cf-many-required";

export interface Graph {
  name: string;
  /**
   * IsFolderOnly indicates a board or scenario itself makes no modifications from its
   * base. Folder only boards do not have a render and are used purely for organizing
   * the board tree.
   */
  isFolderOnly: boolean;
  ast?: any /* d2ast.Map */;
  root?: Object;
  legend?: Legend;
  edges: (Edge | undefined)[];
  objects: (Object | undefined)[];
  layers?: (Graph | undefined)[];
  scenarios?: (Graph | undefined)[];
  steps?: (Graph | undefined)[];
  theme?: any /* d2themes.Theme */;
  /**
   * Object.Level uses the location of a nested graph
   */
  rootLevel?: number /* int */;
  /**
   * Data embedded from source configuration variables, including layout options.
   */
  data?: { [key: string]: any };
}

export interface Edge {
  index: number /* int */;
  srcTableColumnIndex?: number /* int */;
  dstTableColumnIndex?: number /* int */;
  labelPosition?: string;
  labelPercentage?: number /* float64 */;
  isCurve: boolean;
  route?: (any /* geo.Point */ | undefined)[];
  src_arrow: boolean;
  srcArrowhead?: Attributes;
  /**
   * TODO alixander (Mon Sep 12 2022): deprecate SrcArrow and DstArrow and just use SrcArrowhead and DstArrowhead
   */
  dst_arrow: boolean;
  dstArrowhead?: Attributes;
  references?: EdgeReference[];
  attributes?: Attributes;
  zIndex: number /* int */;
}

export interface Attributes {
  label: Scalar;
  labelDimensions: TextDimensions;
  style: Style;
  icon?: string /* url.URL */;
  tooltip?: Scalar;
  link?: Scalar;
  width?: Scalar;
  height?: Scalar;
  top?: Scalar;
  left?: Scalar;
  /**
   * TODO consider separate Attributes struct for shape-specific and edge-specific
   * Shapes only
   */
  near_key?: any /* d2ast.KeyPath */;
  language?: string;
  /**
   * TODO: default to ShapeRectangle instead of empty string
   */
  shape: Scalar;
  direction: Scalar;
  constraint: string[];
  gridRows?: Scalar;
  gridColumns?: Scalar;
  gridGap?: Scalar;
  verticalGap?: Scalar;
  horizontalGap?: Scalar;
  labelPosition?: Scalar;
  iconPosition?: Scalar;
  /**
   * These names are attached to the rendered elements in SVG
   * so that users can target them however they like outside of D2
   */
  classes?: string[];
}

export interface EdgeReference {
  map_key_edge_index: number /* int */;
}

export interface Scalar {
  value: string;
}

export interface Style {
  opacity?: Scalar;
  stroke?: Scalar;
  fill?: Scalar;
  fillPattern?: Scalar;
  strokeWidth?: Scalar;
  strokeDash?: Scalar;
  borderRadius?: Scalar;
  shadow?: Scalar;
  "3d"?: Scalar;
  multiple?: Scalar;
  font?: Scalar;
  fontSize?: Scalar;
  fontColor?: Scalar;
  animated?: Scalar;
  bold?: Scalar;
  italic?: Scalar;
  underline?: Scalar;
  filled?: Scalar;
  doubleBorder?: Scalar;
  textTransform?: Scalar;
}

export interface TextDimensions {
  width: number /* int */;
  height: number /* int */;
}
