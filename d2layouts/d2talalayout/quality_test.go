package d2talalayout_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

// qualityTestSource is small enough for the test suite and large enough for
// the levels to produce different layouts: containers, a fan-out and a cycle.
const qualityTestSource = `client -> edge.cdn -> edge.lb
edge.lb -> svc.api
edge.lb -> billing.api
svc.api -> svc.db
svc.api -> svc.cache
svc.worker -> svc.db
billing.api -> billing.db
billing.api -> svc.api: lookup
billing.worker -> billing.db
svc.worker -> billing.api
svc.db: {shape: cylinder}
billing.db: {shape: cylinder}
`

func compileTALADiagram(t *testing.T, source string, opts *d2talalayout.Options) *d2target.Diagram {
	t.Helper()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	layout := "tala"
	ctx := d2log.With(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	diagram, _, err := d2lib.Compile(ctx, source, &d2lib.CompileOptions{
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
		t.Fatal(err)
	}
	return diagram
}

// layoutBytes is the laid-out geometry of a diagram: shapes and routes.
func layoutBytes(t *testing.T, diagram *d2target.Diagram) string {
	t.Helper()
	geometry, err := json.Marshal(struct {
		Shapes      []d2target.Shape
		Connections []d2target.Connection
	}{diagram.Shapes, diagram.Connections})
	if err != nil {
		t.Fatal(err)
	}
	return string(geometry)
}

func TestQualityLevelsAreDeterministic(t *testing.T) {
	layouts := map[d2talalayout.Quality]string{}
	for _, quality := range []d2talalayout.Quality{d2talalayout.QualityFull, d2talalayout.QualityBalanced, d2talalayout.QualityFast} {
		t.Run(string(quality), func(t *testing.T) {
			sequential := layoutBytes(t, compileTALADiagram(t, qualityTestSource, &d2talalayout.Options{Quality: quality, MaxConcurrency: 1}))
			concurrent := layoutBytes(t, compileTALADiagram(t, qualityTestSource, &d2talalayout.Options{Quality: quality, MaxConcurrency: 3}))
			again := layoutBytes(t, compileTALADiagram(t, qualityTestSource, &d2talalayout.Options{Quality: quality, MaxConcurrency: 1}))
			if sequential != concurrent || sequential != again {
				t.Fatalf("%s layout depends on concurrency or run", quality)
			}
			layouts[quality] = sequential
		})
	}
	// The zero value is full quality, byte for byte.
	if zero := layoutBytes(t, compileTALADiagram(t, qualityTestSource, nil)); zero != layouts[d2talalayout.QualityFull] {
		t.Fatal("default options differ from QualityFull")
	}
	if layouts[d2talalayout.QualityFast] == layouts[d2talalayout.QualityFull] {
		t.Fatal("QualityFast had no effect on the test diagram")
	}
}

func TestQualityFromDiagramData(t *testing.T) {
	const config = "vars: {d2-config: {data: {tala-quality: fast}}}\n"
	fromOption := layoutBytes(t, compileTALADiagram(t, qualityTestSource, &d2talalayout.Options{Quality: d2talalayout.QualityFast, MaxConcurrency: 1}))
	defaults := d2talalayout.DefaultOptions()
	defaults.Quality = d2talalayout.QualityFull
	// Diagram data takes precedence over Options.Quality.
	fromData := layoutBytes(t, compileTALADiagram(t, config+qualityTestSource, &defaults))
	if fromData != fromOption {
		t.Fatal("tala-quality in the diagram did not select the fast level")
	}
}

func TestReducedQualityHonoursPins(t *testing.T) {
	for _, quality := range []d2talalayout.Quality{d2talalayout.QualityBalanced, d2talalayout.QualityFast} {
		opts := &d2talalayout.Options{Quality: quality, MaxConcurrency: 1}
		shapesOf := func(source string) map[string]d2target.Shape {
			shapes := map[string]d2target.Shape{}
			for _, shape := range compileTALADiagram(t, source, opts).Shapes {
				shapes[shape.ID] = shape
			}
			return shapes
		}
		t.Run(string(quality)+"/root", func(t *testing.T) {
			requireAt(t, shapesOf("a -> b\nb: {top: 0; left: 600}\nc -> a\n"), "b", 600, 0)
		})
		t.Run(string(quality)+"/nested", func(t *testing.T) {
			shapes := shapesOf("o: {\n  g: {\n    p\n    q: {top: 500; left: 50}\n    p -> q\n  }\n  s\n  s -> g.p\n}\na -> o.s\n")
			requireAt(t, shapes, "o.g.q", 50, 500)
			requireInside(t, shapes, "o.g.q", "o.g")
			requireInside(t, shapes, "o.g", "o")
		})
		t.Run(string(quality)+"/pinned container and child", func(t *testing.T) {
			shapes := shapesOf("g: {\n  top: 200\n  left: 300\n  p\n  q: {top: 400; left: 420}\n  p -> q\n}\na -> g.p\n")
			requireAt(t, shapes, "g", 300, 200)
			requireAt(t, shapes, "g.q", 420, 400)
			requireInside(t, shapes, "g.q", "g")
		})
	}
}
