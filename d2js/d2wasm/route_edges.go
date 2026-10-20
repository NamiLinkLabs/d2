//go:build js && wasm

package d2wasm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"syscall/js"
	"time"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/memfs"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
)

const (
	defaultRouteTimeoutMs = 1500
	maxRouteTimeoutMs     = 60_000
	// TALA rejects geometry outside this range.
	maxRouteCoordinate = 1_000_000_000
	// Routing edges together costs superlinearly more than routing them one at
	// a time: 5 edges on a 60-shape diagram take about as long either way, 30
	// on a 200-shape diagram about 8 times as long together.
	maxEdgesRoutedTogether = 10
)

// routeWithTALA is a seam for tests that exercise the fallback paths.
var routeWithTALA = d2talalayout.RouteEdges

// RouteEdges reroutes the requested connections with TALA's edge router on
// shapes the caller has already placed. See RouteEdgesRequest.
func RouteEdges(args []js.Value) (interface{}, error) {
	if len(args) < 1 {
		return nil, &WASMError{Message: "missing JSON argument", Code: 400}
	}
	var input RouteEdgesRequest
	if err := json.Unmarshal([]byte(args[0].String()), &input); err != nil {
		return nil, &WASMError{Message: "invalid JSON input", Code: 400}
	}
	return routeEdges(input)
}

func badRouteRequest(format string, args ...any) error {
	return &WASMError{Message: fmt.Sprintf(format, args...), Code: 400}
}

func routeEdges(input RouteEdgesRequest) (*RouteEdgesResponse, error) {
	if input.FS == nil {
		return nil, badRouteRequest("missing 'fs' field in input JSON")
	}
	inputPath := DEFAULT_INPUT_PATH
	if input.InputPath != nil {
		inputPath = *input.InputPath
	}
	source, ok := input.FS[inputPath]
	if !ok {
		return nil, badRouteRequest("missing '%s' file in input fs", inputPath)
	}
	timeoutMs := float64(defaultRouteTimeoutMs)
	if input.TimeoutMs != nil {
		timeoutMs = *input.TimeoutMs
		if math.IsNaN(timeoutMs) || timeoutMs <= 0 || timeoutMs > maxRouteTimeoutMs {
			return nil, badRouteRequest("timeoutMs must be between 0 and %d", maxRouteTimeoutMs)
		}
	}
	shapes, err := validateRouteShapes(input.Shapes)
	if err != nil {
		return nil, err
	}
	requestedIDs := make(map[string]struct{}, len(input.Edges))
	for _, id := range input.Edges {
		if _, dup := requestedIDs[id]; dup {
			return nil, badRouteRequest("edge %q is listed more than once", id)
		}
		requestedIDs[id] = struct{}{}
	}
	response := &RouteEdgesResponse{Routes: map[string][]RoutePoint{}}
	if len(input.Edges) == 0 {
		return response, nil
	}

	budget := time.Duration(timeoutMs * float64(time.Millisecond))
	deadline := time.Now().Add(budget)
	ctx, cancel := context.WithDeadline(log.WithDefault(context.Background()), deadline)
	defer cancel()
	timedOut := func() string {
		return fmt.Sprintf("edge routing ran out of its %v ms budget", timeoutMs)
	}

	compileOpts, renderOpts, err := routeCompileOptions(input.Opts, inputPath, input.FS)
	if err != nil {
		return nil, err
	}
	g, err := d2lib.CompileGraph(ctx, source, compileOpts, renderOpts)
	if err != nil {
		if isContextError(err) || ctx.Err() != nil {
			response.Errors = make(map[string]string, len(input.Edges))
			for _, id := range input.Edges {
				response.Errors[id] = timedOut()
			}
			return response, nil
		}
		var pe *d2parser.ParseError
		if errors.As(err, &pe) {
			errs, _ := json.Marshal(pe.Errors)
			return nil, &WASMError{Message: string(errs), Code: 400}
		}
		return nil, &WASMError{Message: err.Error(), Code: 500}
	}

	if err := placeRouteShapes(g, shapes, *compileOpts.Layout == "elk"); err != nil {
		return nil, err
	}
	edgesByID := make(map[string]*d2graph.Edge, len(g.Edges))
	for _, e := range g.Edges {
		edgesByID[e.AbsID()] = e
	}
	requested := make([]*d2graph.Edge, len(input.Edges))
	for i, id := range input.Edges {
		e, ok := edgesByID[id]
		if !ok {
			return nil, badRouteRequest("unknown edge id %q", id)
		}
		e.Route = nil
		e.IsCurve = false
		e.LabelPosition = nil
		e.LabelPercentage = nil
		requested[i] = e
	}
	fixed := applyRouteConnections(edgesByID, requestedIDs, input.Connections)

	r := &edgeRouter{graph: g, all: slices.Clone(g.Edges), keep: fixed}
	routeErrs := r.route(ctx, requested, deadline)
	for i, e := range requested {
		id := input.Edges[i]
		if err, failed := routeErrs[e]; failed {
			if response.Errors == nil {
				response.Errors = map[string]string{}
			}
			if isContextError(err) {
				response.Errors[id] = timedOut()
			} else {
				response.Errors[id] = err.Error()
			}
			continue
		}
		points := make([]RoutePoint, len(e.Route))
		for j, p := range e.Route {
			points[j] = RoutePoint{X: p.X, Y: p.Y}
		}
		response.Routes[id] = points
		if e.Label.Value != "" && e.LabelPosition != nil {
			if response.Labels == nil {
				response.Labels = map[string]RouteLabel{}
			}
			l := RouteLabel{Position: *e.LabelPosition}
			if e.LabelPercentage != nil {
				l.Percentage = *e.LabelPercentage
			}
			response.Labels[id] = l
		}
	}
	return response, nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// edgeRouter routes subsets of a graph's edges. TALA needs a complete route on
// every edge it is not asked to route, so each call only keeps the edges that
// already have one.
type edgeRouter struct {
	graph *d2graph.Graph
	// all is the graph's edge list in its original order.
	all []*d2graph.Edge
	// keep holds the edges whose current route the router works around.
	keep map[*d2graph.Edge]bool
}

func (r *edgeRouter) run(ctx context.Context, edges []*d2graph.Edge, withKept bool) error {
	selected := make(map[*d2graph.Edge]bool, len(edges))
	for _, e := range edges {
		selected[e] = true
	}
	r.graph.Edges = r.graph.Edges[:0:0]
	for _, e := range r.all {
		if selected[e] || withKept && r.keep[e] {
			r.graph.Edges = append(r.graph.Edges, e)
		}
	}
	return routeWithTALA(ctx, r.graph, edges)
}

// route routes edges in up to two passes and returns the edges left without
// a route.
//
// The first pass routes all of them together around the kept routes, so TALA
// spreads them among each other and around their neighbours. When a second
// pass could still help, it gets at most half of the remaining time.
//
// If the first pass fails or runs long, each edge is routed on its own, with
// no other routed edge in the graph. TALA's route search slows down with every
// route already in the graph, so this is an order of magnitude cheaper; it
// also keeps one unroutable edge from costing the others their routes. Edges
// still unrouted when the deadline passes fail with context.DeadlineExceeded.
//
// Past maxEdgesRoutedTogether edges the first pass is skipped: it would rarely
// finish in time, and the second pass makes better use of the budget.
func (r *edgeRouter) route(ctx context.Context, edges []*d2graph.Edge, deadline time.Time) map[*d2graph.Edge]error {
	defer func() { r.graph.Edges = r.all }()
	failed := map[*d2graph.Edge]error{}
	secondPass := len(edges) > 1 || len(r.keep) > 0
	if len(edges) <= maxEdgesRoutedTogether {
		together := ctx
		if secondPass {
			var cancel context.CancelFunc
			together, cancel = context.WithDeadline(ctx, time.Now().Add(time.Until(deadline)/2))
			defer cancel()
		}
		err := r.run(together, edges, true)
		if err == nil {
			return failed
		}
		if !secondPass {
			failed[edges[0]] = err
			return failed
		}
	}
	for _, e := range edges {
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			failed[e] = context.DeadlineExceeded
			continue
		}
		if err := r.run(ctx, []*d2graph.Edge{e}, false); err != nil {
			failed[e] = err
		}
	}
	return failed
}

type validRouteShape struct {
	RouteShape
	x, y, width, height float64
}

func validateRouteShapes(shapes []RouteShape) ([]validRouteShape, error) {
	valid := make([]validRouteShape, len(shapes))
	for i, s := range shapes {
		if s.ID == "" {
			return nil, badRouteRequest("shape %d has no id", i)
		}
		number := func(name string, v *float64) (float64, error) {
			if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
				return 0, badRouteRequest("shape %q: %s must be a finite number", s.ID, name)
			}
			if math.Abs(*v) > maxRouteCoordinate {
				return 0, badRouteRequest("shape %q: %s is out of range", s.ID, name)
			}
			return *v, nil
		}
		v := validRouteShape{RouteShape: s}
		var err error
		if v.x, err = number("x", s.X); err != nil {
			return nil, err
		}
		if v.y, err = number("y", s.Y); err != nil {
			return nil, err
		}
		if v.width, err = number("width", s.Width); err != nil {
			return nil, err
		}
		if v.height, err = number("height", s.Height); err != nil {
			return nil, err
		}
		if v.width <= 0 || v.height <= 0 {
			return nil, badRouteRequest("shape %q: width and height must be positive", s.ID)
		}
		if math.Abs(v.x+v.width) > maxRouteCoordinate || math.Abs(v.y+v.height) > maxRouteCoordinate {
			return nil, badRouteRequest("shape %q is out of range", s.ID)
		}
		if p := s.LabelPosition; p != nil && *p != "" && !label.FromString(*p).IsShapePosition() {
			return nil, badRouteRequest("shape %q: unknown labelPosition %q", s.ID, *p)
		}
		if p := s.IconPosition; p != nil && *p != "" && !label.FromString(*p).IsShapePosition() {
			return nil, badRouteRequest("shape %q: unknown iconPosition %q", s.ID, *p)
		}
		valid[i] = v
	}
	return valid, nil
}

// placeRouteShapes gives every object of g its final geometry. Label and icon
// positions the caller leaves out get the defaults D2's dagre and ELK layouts
// would pick for that geometry.
func placeRouteShapes(g *d2graph.Graph, shapes []validRouteShape, elk bool) error {
	objects := make(map[string]*d2graph.Object, len(g.Objects))
	for _, obj := range g.Objects {
		objects[obj.AbsID()] = obj
	}
	placed := make(map[*d2graph.Object]bool, len(shapes))
	for _, s := range shapes {
		obj, ok := objects[s.ID]
		if !ok {
			return badRouteRequest("unknown shape id %q", s.ID)
		}
		if placed[obj] {
			return badRouteRequest("shape %q is listed more than once", s.ID)
		}
		placed[obj] = true
		obj.TopLeft = geo.NewPoint(s.x, s.y)
		obj.Width = s.width
		obj.Height = s.height
		// The geometry is final; locked positions have nothing left to say.
		obj.Top = nil
		obj.Left = nil
		if s.LabelPosition != nil && *s.LabelPosition != "" && obj.HasLabel() {
			obj.LabelPosition = go2.Pointer(*s.LabelPosition)
		}
		if s.IconPosition != nil && *s.IconPosition != "" && obj.Icon != nil {
			obj.IconPosition = go2.Pointer(*s.IconPosition)
		}
	}
	for _, obj := range g.Objects {
		if !placed[obj] {
			return badRouteRequest("missing geometry for shape %q", obj.AbsID())
		}
		defaultLabelPositions(obj, elk)
	}
	return nil
}

// defaultLabelPositions mirrors positionLabelsIcons in d2dagrelayout and
// d2elklayout, which only differ for containers.
func defaultLabelPositions(obj *d2graph.Object, elk bool) {
	container := len(obj.ChildrenArray) > 0
	if obj.Icon != nil && obj.IconPosition == nil {
		switch {
		case container:
			obj.IconPosition = go2.Pointer(label.OutsideTopLeft.String())
			if elk {
				obj.IconPosition = go2.Pointer(label.InsideTopLeft.String())
			}
			if obj.HasLabel() && obj.LabelPosition == nil {
				obj.LabelPosition = go2.Pointer(label.OutsideTopRight.String())
				if elk {
					obj.LabelPosition = go2.Pointer(label.InsideTopRight.String())
				}
			}
		case obj.SQLTable != nil || obj.Class != nil || obj.Language != "":
			obj.IconPosition = go2.Pointer(label.OutsideTopLeft.String())
		default:
			obj.IconPosition = go2.Pointer(label.InsideMiddleCenter.String())
		}
	}
	if !obj.HasLabel() || obj.LabelPosition != nil {
		return
	}
	position := label.InsideMiddleCenter
	switch {
	case container && elk:
		position = label.InsideTopCenter
	case container:
		position = label.OutsideTopCenter
	case obj.HasOutsideBottomLabel():
		position = label.OutsideBottomCenter
	case obj.Icon != nil:
		position = label.InsideTopCenter
	}
	if float64(obj.LabelDimensions.Width) > obj.Width || float64(obj.LabelDimensions.Height) > obj.Height {
		position = label.OutsideBottomCenter
		if container {
			position = label.OutsideTopCenter
		}
	}
	obj.LabelPosition = go2.Pointer(position.String())
}

// applyRouteConnections loads the current routes of connections that are not
// rerouted. They are hints, so unknown ids and unusable routes are skipped.
func applyRouteConnections(edges map[string]*d2graph.Edge, requested map[string]struct{}, connections []RouteConnection) map[*d2graph.Edge]bool {
	fixed := map[*d2graph.Edge]bool{}
	for _, c := range connections {
		if _, rerouted := requested[c.ID]; rerouted {
			continue
		}
		e, ok := edges[c.ID]
		if !ok || fixed[e] {
			continue
		}
		route := make([]*geo.Point, 0, len(c.Route))
		for _, p := range c.Route {
			if p == nil || p.X == nil || p.Y == nil || !finiteCoordinate(*p.X) || !finiteCoordinate(*p.Y) {
				route = nil
				break
			}
			if n := len(route); n > 0 && route[n-1].X == *p.X && route[n-1].Y == *p.Y {
				continue
			}
			route = append(route, geo.NewPoint(*p.X, *p.Y))
		}
		if len(route) < 2 {
			continue
		}
		e.Route = simplifyRoute(route, routeHintTolerance)
		e.IsCurve = false
		if e.Label.Value != "" {
			position := label.InsideMiddleCenter
			if c.LabelPosition != nil && label.FromString(*c.LabelPosition).IsEdgePosition() {
				position = label.FromString(*c.LabelPosition)
			}
			e.LabelPosition = go2.Pointer(position.String())
			e.LabelPercentage = nil
			if p := c.LabelPercentage; p != nil && *p >= 0 && *p <= 1 {
				e.LabelPercentage = go2.Pointer(*p)
			}
		}
		fixed[e] = true
	}
	return fixed
}

// routeHintTolerance is how far, in pixels, a simplified kept route may stray
// from the original. Every kept route point grows TALA's visibility graph, and
// curved routes (dagre's especially) carry many Bézier control points.
const routeHintTolerance = 2

// simplifyRoute drops the points of route that lie within tolerance of the
// polyline through the others (Ramer-Douglas-Peucker). Ends are kept.
func simplifyRoute(route []*geo.Point, tolerance float64) []*geo.Point {
	if len(route) <= 2 {
		return route
	}
	keep := make([]bool, len(route))
	keep[0], keep[len(route)-1] = true, true
	spans := [][2]int{{0, len(route) - 1}}
	for len(spans) > 0 {
		a, b := spans[len(spans)-1][0], spans[len(spans)-1][1]
		spans = spans[:len(spans)-1]
		farthest, at := tolerance, -1
		for i := a + 1; i < b; i++ {
			if d := distanceToSegment(route[i], route[a], route[b]); d > farthest {
				farthest, at = d, i
			}
		}
		if at >= 0 {
			keep[at] = true
			spans = append(spans, [2]int{a, at}, [2]int{at, b})
		}
	}
	simplified := make([]*geo.Point, 0, len(route))
	for i, p := range route {
		if keep[i] {
			simplified = append(simplified, p)
		}
	}
	if len(simplified) == 2 && simplified[0].Equals(simplified[1]) {
		// A loop must not collapse to a zero-length route.
		return route
	}
	return simplified
}

func distanceToSegment(p, a, b *geo.Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l))
	}
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

func finiteCoordinate(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= maxRouteCoordinate
}

// routeCompileOptions mirrors the parts of Compile's options that change how
// shapes and labels are measured.
func routeCompileOptions(opts *CompileOptions, inputPath string, files map[string]string) (*d2lib.CompileOptions, *d2svg.RenderOpts, error) {
	compileOpts := &d2lib.CompileOptions{UTF16Pos: true, InputPath: inputPath}
	renderOpts := &d2svg.RenderOpts{}
	var err error
	compileOpts.FS, err = memfs.New(files)
	if err != nil {
		return nil, nil, badRouteRequest("invalid fs input: %s", err.Error())
	}
	if opts != nil {
		var regular, italic, bold, semibold []byte
		if opts.FontRegular != nil {
			regular = *opts.FontRegular
		}
		if opts.FontItalic != nil {
			italic = *opts.FontItalic
		}
		if opts.FontBold != nil {
			bold = *opts.FontBold
		}
		if opts.FontSemibold != nil {
			semibold = *opts.FontSemibold
		}
		if regular != nil || italic != nil || bold != nil || semibold != nil {
			compileOpts.FontFamily, err = d2fonts.AddFontFamily("custom", regular, italic, bold, semibold)
			if err != nil {
				return nil, nil, badRouteRequest("custom fonts could not be initialized: %s", err.Error())
			}
		}
		compileOpts.Layout = opts.Layout
		if opts.ASCII != nil && *opts.ASCII && (compileOpts.Layout == nil || *compileOpts.Layout == "dagre") {
			compileOpts.Layout = go2.Pointer("elk")
		}
		renderOpts.Sketch = opts.Sketch
		renderOpts.ThemeID = opts.ThemeID
	}
	compileOpts.Ruler, err = textmeasure.NewRuler()
	if err != nil {
		return nil, nil, &WASMError{Message: fmt.Sprintf("text ruler cannot be initialized: %s", err.Error()), Code: 500}
	}
	return compileOpts, renderOpts, nil
}
