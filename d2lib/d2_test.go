package d2lib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestParseAndCompileHonorCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Parse(ctx, "x", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Parse() error = %v, want context.Canceled", err)
	}
	if _, _, err := Compile(ctx, "x", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile() error = %v, want context.Canceled", err)
	}
}

func TestCompilePropagatesVariableExpansionLimit(t *testing.T) {
	_, _, err := Compile(context.Background(), "vars: {x: 12345678}\nout: ${x}${x}${x}${x}${x}", &CompileOptions{
		MaxVariableExpansion: 32,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 32 work units") {
		t.Fatalf("Compile() error = %v, want variable expansion limit", err)
	}
}

func TestCompilePropagatesGlobExpansionLimit(t *testing.T) {
	_, _, err := Compile(context.Background(), "**.a\n**.b\n**.c\nx\n", &CompileOptions{
		MaxGlobExpansion: 64,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "glob expansion exceeds limit of 64 work units") {
		t.Fatalf("Compile() error = %v, want glob expansion limit", err)
	}
}

func TestCompilePropagatesEdgeExpansionLimit(t *testing.T) {
	_, _, err := Compile(context.Background(), "a\nb\nc\n* -> *\n", &CompileOptions{
		MaxEdgeExpansion: 8,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "edge glob expansion exceeds limit of 8 endpoint pairs") {
		t.Fatalf("Compile() error = %v, want edge expansion limit", err)
	}
}

func TestCompilePropagatesEdgeExpansionWorkLimit(t *testing.T) {
	_, _, err := Compile(context.Background(), "(* -> *)[*].style.opacity: 0\na\nb\nc\n", &CompileOptions{
		MaxEdgeExpansion:     9,
		MaxEdgeExpansionWork: 9,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "work limit of 9 endpoint-pair examinations") {
		t.Fatalf("Compile() error = %v, want edge expansion work limit", err)
	}
}

func TestNilFSDeniesImports(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "secret.d2"), []byte("disclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Compile(context.Background(), "...@secret", &CompileOptions{
		InputPath: filepath.Join(directory, "index.d2"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "imports are disabled") {
		t.Fatalf("Compile error = %v, want imports-disabled error", err)
	}
}

func TestGetLayoutDoesNotUseEnvironmentFallback(t *testing.T) {
	t.Setenv("D2_LAYOUT", "dagre")

	_, err := getLayout(&CompileOptions{})
	if err == nil || err.Error() != "no available layout" {
		t.Fatalf("getLayout() error = %v, want no available layout", err)
	}
}

func TestCompileRequiresLayoutResolver(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = Compile(context.Background(), "x", &CompileOptions{Ruler: ruler}, nil)
	const want = `no layout resolver configured for layout engine "dagre"`
	if err == nil || err.Error() != want {
		t.Fatalf("Compile() error = %v, want %q", err, want)
	}
}

func TestCompilePropagatesIndependentConfigDataToEveryBoard(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}

	input := `vars: {
  d2-config: {
    layout-engine: tala
    data: {
      tala-seeds: [7; 11]
    }
  }
}
root
layers: {
  layer: { layer-object }
}
scenarios: {
  scenario: { scenario-object }
}
steps: {
  step: { step-object }
}
`

	var seen [][]interface{}
	opts := &CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(engine string) (d2graph.LayoutGraph, error) {
			if engine != "tala" {
				t.Fatalf("layout engine = %q, want tala", engine)
			}
			return func(_ context.Context, graph *d2graph.Graph) error {
				seeds := graph.Data["tala-seeds"].([]interface{})
				seen = append(seen, append([]interface{}(nil), seeds...))
				// Mutating one board's layout data must not affect later boards.
				seeds[0] = "mutated"
				graph.Data["board-local"] = true
				for i, object := range graph.Objects {
					object.TopLeft = geo.NewPoint(float64(i*100), 0)
				}
				return nil
			}, nil
		},
	}
	ctx := d2log.WithDefault(context.Background())
	if _, _, err := Compile(ctx, input, opts, nil); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 {
		t.Fatalf("boards receiving config data = %d, want 4", len(seen))
	}
	want := []interface{}{"7", "11"}
	for i, seeds := range seen {
		if !reflect.DeepEqual(seeds, want) {
			t.Fatalf("board %d tala-seeds = %#v, want %#v", i, seeds, want)
		}
	}
}

func TestCompileTALAConfigSeedsInNestedLayouts(t *testing.T) {
	const nodes = `a -> b -> c
a -> d -> e
b -> e
d -> c
f -> e
g -> b
`
	const config = `vars: {d2-config: {layout-engine: tala; data: {tala-seeds: [7]}}}
`
	for _, tc := range []struct{ name, source string }{
		{"plain", nodes},
		{"grid-cell", "grid: {grid-columns: 1\ncell: {\n" + nodes + "}\n}\n"},
		{"batched-grids", "grid1: {grid-columns: 1\ncell: {\n" + nodes + "}\n}\ngrid2: {grid-columns: 1\ncell: {\n" + nodes + "}\n}\n"},
		{"constant-near", "main\nlegend: {near: top-left\n" + nodes + "}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ruler, err := textmeasure.NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			ctx := d2log.WithDefault(t.Context())
			opts := &CompileOptions{
				Ruler: ruler,
				RouterResolver: func(string) (d2graph.RouteEdges, error) {
					return d2talalayout.RouteEdges, nil
				},
			}
			setSeeds := func(seed int64) {
				opts.LayoutResolver = func(string) (d2graph.LayoutGraph, error) {
					return func(ctx context.Context, g *d2graph.Graph) error {
						return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{seed}, MaxConcurrency: 1})
					}, nil
				}
			}
			setSeeds(7)
			want, _, err := Compile(ctx, config+tc.source, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			// Source seeds must override the supplied options in every core
			// layout call, including graphs extracted for nested diagrams.
			setSeeds(1)
			got, _, err := Compile(ctx, config+tc.source, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("layout changed when source seeds override the supplied options")
			}
		})
	}
}

func TestCompileGraphMeasuresWithoutLayout(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	const input = "vars: {d2-config: {theme-id: 300}}\ng: {a -> b: hello}\nx -> g.a\n"
	g, err := CompileGraph(context.Background(), input, &CompileOptions{Ruler: ruler}, nil)
	if err != nil {
		t.Fatalf("CompileGraph() error = %v", err)
	}
	if g.Theme == nil || g.Theme.ID != 300 {
		t.Fatalf("theme = %v, want the source configuration's theme", g.Theme)
	}
	_, laidOut, err := Compile(context.Background(), input, &CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			// Place objects without changing anything layout-independent.
			return func(_ context.Context, g *d2graph.Graph) error {
				for _, obj := range g.Objects {
					obj.TopLeft = geo.NewPoint(0, 0)
				}
				return nil
			}, nil
		},
	}, nil)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(g.Objects) != len(laidOut.Objects) || len(g.Edges) != len(laidOut.Edges) {
		t.Fatalf("CompileGraph() has %d objects and %d edges, want %d and %d", len(g.Objects), len(g.Edges), len(laidOut.Objects), len(laidOut.Edges))
	}
	for i, obj := range g.Objects {
		if obj.Box == nil || obj.TopLeft != nil {
			t.Fatalf("object %q box = %#v, want a measured box without a position", obj.AbsID(), obj.Box)
		}
		want := laidOut.Objects[i]
		if obj.AbsID() != want.AbsID() || obj.LabelDimensions != want.LabelDimensions {
			t.Fatalf("object %q label dimensions = %v, want %v", obj.AbsID(), obj.LabelDimensions, want.LabelDimensions)
		}
	}
	for i, edge := range g.Edges {
		if len(edge.Route) != 0 {
			t.Fatalf("edge %q has a route before layout", edge.AbsID())
		}
		if edge.LabelDimensions != laidOut.Edges[i].LabelDimensions {
			t.Fatalf("edge %q label dimensions = %v, want %v", edge.AbsID(), edge.LabelDimensions, laidOut.Edges[i].LabelDimensions)
		}
	}
	if g.Edges[0].LabelDimensions.Width == 0 {
		t.Fatal("edge label was not measured")
	}
}

func TestCompileAppliesLayoutDataOnEveryBoard(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	// a and b share a layer, so the gap between them is ELK's in-layer spacing.
	input := `vars: {
  d2-config: {
    layout-engine: elk
    data: {
      elk-nodeNode: 80
    }
  }
}
x -> a
x -> b
layers: {
  layer: {
    x -> a
    x -> b
  }
}
scenarios: {
  scenario: {
    x.style.stroke-width: 4
  }
}
`
	ctx := d2log.WithDefault(context.Background())
	elk := "elk"
	opts := &CompileOptions{
		Ruler:  ruler,
		Layout: &elk,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2elklayout.DefaultLayout, nil
		},
	}
	diagram, _, err := Compile(ctx, input, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	boards := map[string]*d2target.Diagram{
		"root":     diagram,
		"layer":    diagram.Layers[0],
		"scenario": diagram.Scenarios[0],
	}
	for name, board := range boards {
		var a, b d2target.Shape
		for _, shape := range board.Shapes {
			switch shape.ID {
			case "a":
				a = shape
			case "b":
				b = shape
			}
		}
		if a.Pos.X > b.Pos.X {
			a, b = b, a
		}
		if gap := b.Pos.X - (a.Pos.X + a.Width); gap != 80 {
			t.Errorf("%s: gap between a and b = %d, want 80", name, gap)
		}
	}
}
