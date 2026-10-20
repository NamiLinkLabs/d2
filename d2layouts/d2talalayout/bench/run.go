package bench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

// Engine is one layout configuration measured by the harness.
type Engine struct {
	// Name identifies the configuration in reports, e.g. "tala" or "elk".
	Name string
	// D2 is the layout engine name passed to d2lib ("tala", "dagre", "elk").
	D2 string
	// TALA options; nil uses D2.js's defaults (d2talalayout.DefaultOptions).
	TALA *d2talalayout.Options
}

// talaOptions builds TALA options that run seeds sequentially, as D2.js does
// in single-threaded WebAssembly.
func talaOptions(seeds ...int64) *d2talalayout.Options {
	return &d2talalayout.Options{Seeds: seeds, MaxConcurrency: 1}
}

// Engines lists the configurations the harness knows about.
var Engines = []Engine{
	// D2.js uses DefaultLayout: seeds {1,2,3}. GOMAXPROCS is 1 in WASM, so
	// they run one after another; MaxConcurrency 1 reproduces that natively.
	{Name: "tala", D2: "tala", TALA: talaOptions(d2talalayout.DefaultOptions().Seeds...)},
	{Name: "tala-1seed", D2: "tala", TALA: talaOptions(1)},
	// Quality levels with their own default seeds, sequential as in D2.js.
	{Name: "tala-balanced", D2: "tala", TALA: &d2talalayout.Options{Quality: d2talalayout.QualityBalanced, MaxConcurrency: 1}},
	{Name: "tala-fast", D2: "tala", TALA: &d2talalayout.Options{Quality: d2talalayout.QualityFast, MaxConcurrency: 1}},
	{Name: "dagre", D2: "dagre"},
	{Name: "elk", D2: "elk"},
}

// EngineByName returns the named configuration.
func EngineByName(name string) (Engine, bool) {
	for _, e := range Engines {
		if e.Name == name {
			return e, true
		}
	}
	return Engine{}, false
}

// Run is the result of compiling and laying out one source once.
type Run struct {
	Diagram *d2target.Diagram
	Graph   *d2graph.Graph
	// Layout is the time spent inside the layout engine's callbacks
	// (layout plus any edge-routing callbacks requested by D2).
	Layout time.Duration
	// Compile is the full d2lib.Compile time, including parsing, text
	// measurement and export.
	Compile time.Duration
}

// ErrTimeout reports that a layout exceeded the harness timeout.
var ErrTimeout = errors.New("layout timed out")

// Compile compiles source with engine, recording layout and compile time.
// A non-positive timeout disables the limit.
func Compile(ctx context.Context, source string, engine Engine, timeout time.Duration) (run Run, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = d2log.With(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return run, err
	}
	layoutName := engine.D2
	timed := func(phase string, f func(context.Context) error) func(context.Context) error {
		return func(ctx context.Context) (err error) {
			pprof.Do(ctx, pprof.Labels("engine", engine.Name, "phase", phase), func(ctx context.Context) {
				start := time.Now()
				err = f(ctx)
				run.Layout += time.Since(start)
			})
			return err
		}
	}
	compileOpts := &d2lib.CompileOptions{
		UTF16Pos: true,
		Ruler:    ruler,
		Layout:   &layoutName,
		LayoutResolver: func(name string) (d2graph.LayoutGraph, error) {
			switch name {
			case "dagre":
				return func(ctx context.Context, g *d2graph.Graph) error {
					return timed("layout", func(ctx context.Context) error { return d2dagrelayout.DefaultLayout(ctx, g) })(ctx)
				}, nil
			case "elk":
				return func(ctx context.Context, g *d2graph.Graph) error {
					return timed("layout", func(ctx context.Context) error { return d2elklayout.DefaultLayout(ctx, g) })(ctx)
				}, nil
			case "tala":
				return func(ctx context.Context, g *d2graph.Graph) error {
					return timed("layout", func(ctx context.Context) error { return d2talalayout.Layout(ctx, g, engine.TALA) })(ctx)
				}, nil
			}
			return nil, fmt.Errorf("unknown layout %q", name)
		},
		RouterResolver: func(name string) (d2graph.RouteEdges, error) {
			if name != "tala" {
				return nil, nil
			}
			return func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
				return timed("routing", func(ctx context.Context) error { return d2talalayout.RouteEdges(ctx, g, edges) })(ctx)
			}, nil
		},
	}
	start := time.Now()
	diagram, graph, err := d2lib.Compile(ctx, source, compileOpts, &d2svg.RenderOpts{})
	run.Compile = time.Since(start)
	if err != nil {
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return run, fmt.Errorf("%w after %s: %v", ErrTimeout, timeout, err)
		}
		return run, err
	}
	run.Diagram, run.Graph = diagram, graph
	return run, nil
}

// Result holds every metric for one corpus case and engine.
type Result struct {
	Case    string
	Engine  string
	Objects int
	Edges   int
	// Layout and Compile describe the unedited source.
	Layout  time.Duration
	Compile time.Duration
	Metrics Metrics
	// EditLayout is the layout time of the edited source.
	EditLayout time.Duration
	// Displacement compares the unedited and edited layouts, excluding the
	// new edge's endpoints.
	Displacement *Displacement
	Err          error
	EditErr      error
}

// MeasureCase lays out source and its one-line edit with engine and computes all
// metrics. edit may be empty to skip the stability measurement.
func MeasureCase(ctx context.Context, name, source, edit string, engine Engine, timeout time.Duration) Result {
	r := Result{Case: name, Engine: engine.Name}
	base, err := Compile(ctx, source, engine, timeout)
	r.Layout, r.Compile = base.Layout, base.Compile
	if err != nil {
		r.Err = err
		return r
	}
	r.Objects, r.Edges = len(base.Graph.Objects), len(base.Graph.Edges)
	r.Metrics = Measure(base.Diagram)
	if edit == "" {
		return r
	}
	edited, err := Compile(ctx, ApplyEdit(source, edit), engine, timeout)
	r.EditLayout = edited.Layout
	if err != nil {
		r.EditErr = err
		return r
	}
	exclude := map[string]bool{}
	src, dst, ok := strings.Cut(edit, "->")
	if ok {
		exclude[strings.TrimSpace(src)] = true
		exclude[strings.TrimSpace(dst)] = true
	}
	d := Displace(base.Diagram, edited.Diagram, exclude)
	r.Displacement = &d
	return r
}
