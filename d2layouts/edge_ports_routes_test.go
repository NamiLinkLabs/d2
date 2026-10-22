package d2layouts_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

// pinnedRouteSources returns the diagrams the pinned-route checks run on:
// the files in testdata/edge-ports and the edge-ports e2e cases.
func pinnedRouteSources(t *testing.T) map[string]string {
	t.Helper()
	sources := make(map[string]string)
	files, err := filepath.Glob(filepath.Join("testdata", "edge-ports", "*.d2"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sources[strings.TrimSuffix(filepath.Base(file), ".d2")] = string(b)
	}
	archive, err := txtar.ParseFile(filepath.Join("..", "e2etests", "txtar.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range archive.Files {
		if strings.HasPrefix(f.Name, "edge-ports") {
			sources["e2e "+f.Name] = string(f.Data)
		}
	}
	return sources
}

type pinnedRouteEngine struct {
	name   string
	layout d2graph.LayoutGraph
}

func pinnedRouteEngines() []pinnedRouteEngine {
	engines := []pinnedRouteEngine{
		{name: "dagre", layout: d2dagrelayout.DefaultLayout},
		{name: "elk", layout: d2elklayout.DefaultLayout},
	}
	// Each seed TALA tries by default on its own, and all of them together as
	// the CLI runs it (the best attempt wins).
	for _, seed := range []int64{1, 2, 3} {
		engines = append(engines, pinnedRouteEngine{
			name: fmt.Sprintf("tala seed %d", seed),
			layout: func(ctx context.Context, g *d2graph.Graph) error {
				return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{seed}, MaxConcurrency: 1})
			},
		})
	}
	engines = append(engines, pinnedRouteEngine{
		name:   "tala default",
		layout: d2talalayout.DefaultLayout,
	})
	return engines
}

// TestPinnedRoutesClear lays out every pinned-route diagram with each engine
// and checks that each pinned connection leaves through its pinned sides,
// heads away from its shapes, never runs back through them, crosses no
// unrelated shape and, unless it is drawn as a curve, has only horizontal and
// vertical segments. Every board must also pass the SVG renderer and the
// native (PNG, PDF, ...) scene builder.
func TestPinnedRoutesClear(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	sources := pinnedRouteSources(t)
	modes := []string{d2target.NestedEdgeRoutingAroundShapes, d2target.NestedEdgeRoutingStraight}
	for _, engine := range pinnedRouteEngines() {
		for _, mode := range modes {
			for name, source := range sources {
				t.Run(engine.name+"/"+mode+"/"+name, func(t *testing.T) {
					checkPinnedRoutes(t, ruler, engine, mode, source)
				})
			}
		}
	}
}

// checkPinnedRoutes compiles source with engine, drawing the connections
// that cross nested-layout boundaries and run between grid cells as mode
// says, the way the CLI does, and checks its pinned connections.
func checkPinnedRoutes(t *testing.T, ruler *textmeasure.Ruler, engine pinnedRouteEngine, mode, source string) {
	t.Helper()
	layoutName := strings.Fields(engine.name)[0]
	ctx := d2log.With(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	opts := &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: &layoutName,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return engine.layout, nil
		},
		NestedEdgeRouting: &mode,
	}
	diagram, g, err := d2lib.Compile(ctx, source, opts, &d2svg.RenderOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range allEdges(g) {
		if !e.HasPort() || e.Src == e.Dst {
			continue
		}
		if err := e.PortsHonored(); err != nil {
			t.Error(err)
		}
		if err := e.RouteClear(); err != nil {
			t.Error(err)
		}
		if !e.IsCurve {
			if err := routeOrthogonal(e); err != nil {
				t.Error(err)
			}
		}
	}
	for _, board := range allBoards(diagram) {
		if _, err := d2svg.Render(board, &d2svg.RenderOpts{}); err != nil {
			t.Errorf("svg: %v", err)
		}
		if _, err := d2scenebuild.Build(ctx, sceneBoard(board), d2scenebuild.Options{}); err != nil {
			t.Errorf("scene: %v", err)
		}
	}
}

// routeOrthogonal reports the first segment of e's route that is neither
// horizontal nor vertical.
func routeOrthogonal(e *d2graph.Edge) error {
	const eps = 1.
	for i := 1; i < len(e.Route); i++ {
		a, b := e.Route[i-1], e.Route[i]
		if math.Abs(a.X-b.X) > eps && math.Abs(a.Y-b.Y) > eps {
			return fmt.Errorf("%s: segment %v -> %v is diagonal", e.AbsID(), a, b)
		}
	}
	return nil
}

// sceneBoard returns board without its child boards and icons, which the
// scene builder would have to fetch, for checking route geometry.
func sceneBoard(board *d2target.Diagram) *d2target.Diagram {
	scene := *board
	scene.Layers, scene.Scenarios, scene.Steps = nil, nil, nil
	scene.Shapes = append([]d2target.Shape(nil), board.Shapes...)
	for i := range scene.Shapes {
		scene.Shapes[i].Icon = nil
	}
	scene.Connections = append([]d2target.Connection(nil), board.Connections...)
	for i := range scene.Connections {
		scene.Connections[i].Icon = nil
	}
	return &scene
}

func allBoards(d *d2target.Diagram) []*d2target.Diagram {
	boards := []*d2target.Diagram{d}
	for _, children := range [][]*d2target.Diagram{d.Layers, d.Scenarios, d.Steps} {
		for _, child := range children {
			boards = append(boards, allBoards(child)...)
		}
	}
	return boards
}

func allEdges(g *d2graph.Graph) []*d2graph.Edge {
	edges := append([]*d2graph.Edge(nil), g.Edges...)
	for _, boards := range [][]*d2graph.Graph{g.Layers, g.Scenarios, g.Steps} {
		for _, board := range boards {
			edges = append(edges, allEdges(board)...)
		}
	}
	return edges
}
