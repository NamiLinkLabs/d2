package d2layouts_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/d2lang/d2/d2renderers/d2svg"
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
	router d2graph.RouteEdges
}

func pinnedRouteEngines() []pinnedRouteEngine {
	engines := []pinnedRouteEngine{
		{name: "dagre", layout: d2dagrelayout.DefaultLayout},
		{name: "elk", layout: d2elklayout.DefaultLayout},
	}
	for _, seed := range []int64{1, 2, 3} {
		engines = append(engines, pinnedRouteEngine{
			name: fmt.Sprintf("tala seed %d", seed),
			layout: func(ctx context.Context, g *d2graph.Graph) error {
				return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{seed}, MaxConcurrency: 1})
			},
			router: d2talalayout.RouteEdges,
		})
	}
	return engines
}

// TestPinnedRoutesClear lays out every pinned-route diagram with each engine
// and checks that each pinned connection leaves through its pinned sides,
// heads away from its shapes, never runs back through them and crosses no
// unrelated shape.
func TestPinnedRoutesClear(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	sources := pinnedRouteSources(t)
	for _, engine := range pinnedRouteEngines() {
		for name, source := range sources {
			t.Run(engine.name+"/"+name, func(t *testing.T) {
				layoutName := strings.Fields(engine.name)[0]
				ctx := d2log.With(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
				opts := &d2lib.CompileOptions{
					Ruler:  ruler,
					Layout: &layoutName,
					LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
						return engine.layout, nil
					},
				}
				if engine.router != nil {
					opts.RouterResolver = func(string) (d2graph.RouteEdges, error) { return engine.router, nil }
				}
				_, g, err := d2lib.Compile(ctx, source, opts, &d2svg.RenderOpts{})
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
				}
			})
		}
	}
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
