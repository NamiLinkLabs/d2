package d2talalayout_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout/bench"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

// Fans and trees that TALA bundles by default: a cluster fan-out and fan-in,
// and an org-chart tree that is drawn as a bus from each parent.
var separateEdgesSources = map[string]string{
	"fans": "lb -> web1\nlb -> web2\nlb -> web3\nweb1 -> db\nweb2 -> db\nweb3 -> db\n",
	"tree": "ceo -> cto\nceo -> cfo\nceo -> coo\ncto -> eng1\ncto -> eng2\ncto -> eng3\ncfo -> acct\n",
}

func sharedLength(diagram *d2target.Diagram) float64 {
	return bench.Measure(diagram).SharedLength
}

func TestSeparateEdgesSplitsBundledRoutes(t *testing.T) {
	for name, source := range separateEdgesSources {
		t.Run(name, func(t *testing.T) {
			bundled := compileTALADiagram(t, source, &d2talalayout.Options{MaxConcurrency: 1})
			if sharedLength(bundled) == 0 {
				t.Fatal("the default layout no longer bundles this diagram; the test needs a new diagram")
			}
			separate := compileTALADiagram(t, source, &d2talalayout.Options{SeparateEdges: true, MaxConcurrency: 1})
			if shared := sharedLength(separate); shared != 0 {
				t.Fatalf("separate edges still share %.0fpx of route", shared)
			}
			concurrent := compileTALADiagram(t, source, &d2talalayout.Options{SeparateEdges: true, MaxConcurrency: 3})
			if layoutBytes(t, separate) != layoutBytes(t, concurrent) {
				t.Fatal("separate-edges layout depends on concurrency")
			}
		})
	}
}

func TestSeparateEdgesFromDiagramData(t *testing.T) {
	source := separateEdgesSources["fans"]
	defaults := layoutBytes(t, compileTALADiagram(t, source, nil))
	fromOption := layoutBytes(t, compileTALADiagram(t, source, &d2talalayout.Options{SeparateEdges: true}))
	if fromOption == defaults {
		t.Fatal("SeparateEdges had no effect")
	}
	if got := layoutBytes(t, compileTALADiagram(t, "vars: {d2-config: {data: {tala-separate-edges: true}}}\n"+source, nil)); got != fromOption {
		t.Fatal("tala-separate-edges: true in the diagram differs from Options.SeparateEdges")
	}
	// Diagram data takes precedence over the option.
	if got := layoutBytes(t, compileTALADiagram(t, "vars: {d2-config: {data: {tala-separate-edges: false}}}\n"+source, &d2talalayout.Options{SeparateEdges: true})); got != defaults {
		t.Fatal("tala-separate-edges: false in the diagram did not override Options.SeparateEdges")
	}
}

func TestSeparateEdgesRejectsInvalidData(t *testing.T) {
	// The compiler rejects it where it is written.
	for _, value := range []string{"yes", "[true]"} {
		_, _, err := compileTALAGraph(t.Context(), "vars: {d2-config: {data: {tala-separate-edges: "+value+"}}}\na -> b\n", nil)
		if err == nil || !strings.Contains(err.Error(), `expected true or false for "tala-separate-edges"`) || !strings.Contains(err.Error(), "1:27:") {
			t.Fatalf("tala-separate-edges: %s error = %v", value, err)
		}
	}
	// Layout rejects it too, for Go callers that set graph data themselves.
	for _, value := range []any{"yes", []any{"true"}} {
		_, g, err := compileTALAGraph(t.Context(), "a -> b\n", nil)
		if err != nil {
			t.Fatal(err)
		}
		g.Data = map[string]any{"tala-separate-edges": value}
		if err := d2talalayout.Layout(t.Context(), g, nil); err == nil || !strings.Contains(err.Error(), "tala-separate-edges must be true or false") {
			t.Fatalf("tala-separate-edges: %v layout error = %v", value, err)
		}
	}
}

// rerouteAll compiles source, places its shapes at the given top-left
// corners and routes every edge, as D2.js routeEdges does after shapes move.
func rerouteAll(t *testing.T, source string, at map[string]geo.Point, route func(context.Context, *d2graph.Graph, []*d2graph.Edge) error) *d2target.Diagram {
	t.Helper()
	_, graph, err := compileTALAGraph(t.Context(), source, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, obj := range graph.Objects {
		p, ok := at[obj.AbsID()]
		if !ok {
			t.Fatalf("no position for %s", obj.AbsID())
		}
		obj.TopLeft = geo.NewPoint(p.X, p.Y)
	}
	for _, edge := range graph.Edges {
		edge.Route = nil
		edge.LabelPosition = nil
		edge.LabelPercentage = nil
	}
	if err := route(t.Context(), graph, graph.Edges); err != nil {
		t.Fatal(err)
	}
	diagram := &d2target.Diagram{}
	for _, edge := range graph.Edges {
		diagram.Connections = append(diagram.Connections, d2target.Connection{ID: edge.AbsID(), Route: edge.Route})
	}
	return diagram
}

func TestRouteEdgesWithOptionsSeparatesEdges(t *testing.T) {
	// More connections leave a toward the column on its right than its
	// right side has ports for.
	source := "a -> b1\na -> b2\na -> b3\na -> b4\na -> b5\n"
	at := map[string]geo.Point{"a": {X: 0, Y: 200}, "b1": {X: 400, Y: 0}, "b2": {X: 400, Y: 100}, "b3": {X: 400, Y: 200}, "b4": {X: 400, Y: 300}, "b5": {X: 400, Y: 400}}
	separate := func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		return d2talalayout.RouteEdgesWithOptions(ctx, g, edges, &d2talalayout.Options{SeparateEdges: true})
	}
	bundled := rerouteAll(t, source, at, d2talalayout.RouteEdges)
	if sharedLength(bundled) == 0 {
		t.Fatal("default routing no longer bundles this diagram; the test needs a new diagram")
	}
	separated := rerouteAll(t, source, at, separate)
	if shared := sharedLength(separated); shared != 0 {
		t.Fatalf("separately routed edges share %.0fpx of route", shared)
	}
	// The diagram's data key reaches RouteEdges too, and wins over options.
	fromData := rerouteAll(t, "vars: {d2-config: {data: {tala-separate-edges: true}}}\n"+source, at, d2talalayout.RouteEdges)
	if layoutBytes(t, fromData) != layoutBytes(t, separated) {
		t.Fatal("tala-separate-edges in the diagram did not reach RouteEdges")
	}
	overridden := rerouteAll(t, "vars: {d2-config: {data: {tala-separate-edges: false}}}\n"+source, at, separate)
	if layoutBytes(t, overridden) != layoutBytes(t, bundled) {
		t.Fatal("tala-separate-edges: false in the diagram did not override RouteEdgesWithOptions")
	}
}

// compileTALAGraph compiles source with TALA and opts and returns the diagram and
// the laid-out graph.
func compileTALAGraph(ctx context.Context, source string, opts *d2talalayout.Options) (*d2target.Diagram, *d2graph.Graph, error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, nil, err
	}
	layout := "tala"
	ctx = d2log.With(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return d2lib.Compile(ctx, source, &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: &layout,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return func(ctx context.Context, g *d2graph.Graph) error {
				return d2talalayout.Layout(ctx, g, opts)
			}, nil
		},
	}, &d2svg.RenderOpts{})
}
