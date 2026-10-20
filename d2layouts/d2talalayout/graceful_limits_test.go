package d2talalayout_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

const gracefulLimitsSource = `
users: {shape: person}
edge: {
  cdn
  lb
  gateway
  cdn -> lb -> gateway
}
orders: {
  api
  db: {shape: cylinder}
  worker
  api -> db
  worker -> db
}
billing: {
  api
  db: {shape: cylinder}
  api -> db: writes
}
search: {
  indexer
  cache
  indexer -> cache
}
users -> edge.cdn
edge.gateway -> orders.api
edge.gateway -> billing.api
orders.api -> billing.api: charge
orders.worker -> search.indexer
billing.db -> search.indexer
search.cache -> orders.api
pinned: {top: 0; left: 1500}
pinned -> orders.api
`

func compileTALAWithLimits(t *testing.T, limits engine.WorkLimits, opts *d2talalayout.Options) *d2target.Diagram {
	t.Helper()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	layout := "tala"
	ctx := d2log.With(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx = engine.ContextWithWorkLimits(ctx, limits)
	diagram, _, err := d2lib.Compile(ctx, gracefulLimitsSource, &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: &layout,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return func(ctx context.Context, g *d2graph.Graph) error {
				return d2talalayout.Layout(ctx, g, opts)
			}, nil
		},
		RouterResolver: func(string) (d2graph.RouteEdges, error) { return d2talalayout.RouteEdges, nil },
	}, &d2svg.RenderOpts{})
	if err != nil {
		t.Fatalf("compile with limits %+v: %v", limits, err)
	}
	return diagram
}

func diagramGeometry(t *testing.T, diagram *d2target.Diagram) string {
	t.Helper()
	data, err := json.Marshal(struct {
		Shapes      []d2target.Shape
		Connections []d2target.Connection
	}{diagram.Shapes, diagram.Connections})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// With every work budget exhausted, the public layout still completes, keeps
// pins exact and produces the same bytes for any seed concurrency.
func TestLayoutDegradesInsteadOfFailingAtWorkLimits(t *testing.T) {
	exhausted := engine.WorkLimits{RefinementTotal: 1, RefinementStage: 1, RouteSearch: 1, OVGCandidates: 1}
	for _, limits := range []engine.WorkLimits{
		{RefinementTotal: 1, RefinementStage: 1},
		{RouteSearch: 1},
		{OVGCandidates: 1},
		exhausted,
	} {
		sequential := compileTALAWithLimits(t, limits, &d2talalayout.Options{Seeds: []int64{1, 2, 3}, MaxConcurrency: 1})
		parallel := compileTALAWithLimits(t, limits, &d2talalayout.Options{Seeds: []int64{1, 2, 3}, MaxConcurrency: 3})
		if diagramGeometry(t, sequential) != diagramGeometry(t, parallel) {
			t.Fatalf("limits %+v: layout depends on seed concurrency", limits)
		}
		shapes := make(map[string]d2target.Shape, len(sequential.Shapes))
		for _, shape := range sequential.Shapes {
			shapes[shape.ID] = shape
		}
		requireAt(t, shapes, "pinned", 1500, 0)
		requireInside(t, shapes, "orders.db", "orders")
		for _, connection := range sequential.Connections {
			if len(connection.Route) < 2 {
				t.Fatalf("limits %+v: connection %s has no route", limits, connection.ID)
			}
		}
	}

	unlimited := compileTALAWithLimits(t, engine.WorkLimits{}, &d2talalayout.Options{Seeds: []int64{1}, MaxConcurrency: 1})
	degraded := compileTALAWithLimits(t, exhausted, &d2talalayout.Options{Seeds: []int64{1}, MaxConcurrency: 1})
	if diagramGeometry(t, unlimited) == diagramGeometry(t, degraded) {
		t.Fatal("exhausted budgets did not change the layout; the limits were not exercised")
	}
}
