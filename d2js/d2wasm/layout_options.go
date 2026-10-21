//go:build js && wasm

package d2wasm

import (
	"context"
	"fmt"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
)

// ELKOptions tunes ELK. Fields left out keep D2's defaults.
type ELKOptions struct {
	NodeSpacing     *int    `json:"nodeSpacing"`
	EdgeNodeSpacing *int    `json:"edgeNodeSpacing"`
	EdgeEdgeSpacing *int    `json:"edgeEdgeSpacing"`
	SelfLoopSpacing *int    `json:"selfLoopSpacing"`
	Padding         *string `json:"padding"`
}

// DagreOptions tunes dagre. Fields left out keep D2's defaults.
type DagreOptions struct {
	NodeSep *int `json:"nodeSep"`
	EdgeSep *int `json:"edgeSep"`
}

// TALAOptions tunes TALA. Fields left out keep TALA's defaults.
type TALAOptions struct {
	// Seeds selects the deterministic layout attempts; diagram-level tala-seeds still win.
	Seeds []int64 `json:"seeds"`
	// Quality is "full" (default), "balanced" or "fast"; diagram-level
	// tala-quality still wins. Fast runs only the first seed.
	Quality *string `json:"quality"`
	// SeparateEdges gives every connection its own route instead of bundling
	// connections that share an endpoint; diagram-level tala-separate-edges
	// still wins. It applies to compile and to routeEdges.
	SeparateEdges *bool `json:"separateEdges"`
}

func nonNegative(name string, v *int) error {
	if v != nil && *v < 0 {
		return &WASMError{Message: fmt.Sprintf("%s must not be negative", name), Code: 400}
	}
	return nil
}

func (o *ELKOptions) resolve() (*d2elklayout.ConfigurableOpts, error) {
	opts := d2elklayout.DefaultOpts
	if o == nil {
		return &opts, nil
	}
	for name, v := range map[string]*int{"elk.nodeSpacing": o.NodeSpacing, "elk.edgeNodeSpacing": o.EdgeNodeSpacing, "elk.edgeEdgeSpacing": o.EdgeEdgeSpacing, "elk.selfLoopSpacing": o.SelfLoopSpacing} {
		if err := nonNegative(name, v); err != nil {
			return nil, err
		}
	}
	if o.NodeSpacing != nil {
		opts.NodeSpacing = *o.NodeSpacing
	}
	if o.EdgeNodeSpacing != nil {
		opts.EdgeNodeSpacing = *o.EdgeNodeSpacing
	}
	if o.EdgeEdgeSpacing != nil {
		v := *o.EdgeEdgeSpacing
		opts.EdgeEdgeSpacing = &v
	}
	if o.SelfLoopSpacing != nil {
		opts.SelfLoopSpacing = *o.SelfLoopSpacing
	}
	if o.Padding != nil {
		opts.Padding = *o.Padding
	}
	return &opts, nil
}

func (o *DagreOptions) resolve() (*d2dagrelayout.ConfigurableOpts, error) {
	opts := d2dagrelayout.DefaultOpts
	if o == nil {
		return &opts, nil
	}
	if err := nonNegative("dagre.nodeSep", o.NodeSep); err != nil {
		return nil, err
	}
	if err := nonNegative("dagre.edgeSep", o.EdgeSep); err != nil {
		return nil, err
	}
	if o.NodeSep != nil {
		opts.NodeSep = *o.NodeSep
	}
	if o.EdgeSep != nil {
		opts.EdgeSep = *o.EdgeSep
	}
	return &opts, nil
}

func (o *TALAOptions) resolve() (*d2talalayout.Options, error) {
	opts := d2talalayout.DefaultOptions()
	if o == nil {
		return &opts, nil
	}
	if o.Quality != nil {
		switch q := d2talalayout.Quality(*o.Quality); q {
		case d2talalayout.QualityFull, d2talalayout.QualityBalanced, d2talalayout.QualityFast:
			opts.Quality = q
		default:
			return nil, &WASMError{Message: fmt.Sprintf("tala.quality must be one of %q, %q or %q, got %q", d2talalayout.QualityFull, d2talalayout.QualityBalanced, d2talalayout.QualityFast, *o.Quality), Code: 400}
		}
	}
	if len(o.Seeds) > 0 {
		opts.Seeds = append([]int64(nil), o.Seeds...)
	}
	if o.SeparateEdges != nil {
		opts.SeparateEdges = *o.SeparateEdges
	}
	return &opts, nil
}

func compileTALAOptions(opts *CompileOptions) *TALAOptions {
	if opts == nil {
		return nil
	}
	return opts.TALA
}

// routerResolver picks the engine's edge router, which D2 calls for
// connections into nested grid and sequence diagrams, with the caller's TALA
// tuning.
func routerResolver(opts *CompileOptions) (func(string) (d2graph.RouteEdges, error), error) {
	talaOpts, err := compileTALAOptions(opts).resolve()
	if err != nil {
		return nil, err
	}
	return func(engine string) (d2graph.RouteEdges, error) {
		if engine == "tala" {
			return func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
				return d2talalayout.RouteEdgesWithOptions(ctx, g, edges, talaOpts)
			}, nil
		}
		return nil, nil
	}, nil
}

// layoutResolver picks the layout engine and applies the caller's tuning.
func layoutResolver(opts *CompileOptions) (func(string) (d2graph.LayoutGraph, error), error) {
	var elk *ELKOptions
	var dagre *DagreOptions
	var tala *TALAOptions
	if opts != nil {
		elk, dagre, tala = opts.ELK, opts.Dagre, opts.TALA
	}
	elkOpts, err := elk.resolve()
	if err != nil {
		return nil, err
	}
	dagreOpts, err := dagre.resolve()
	if err != nil {
		return nil, err
	}
	talaOpts, err := tala.resolve()
	if err != nil {
		return nil, err
	}
	return func(engine string) (d2graph.LayoutGraph, error) {
		switch engine {
		case "dagre":
			return func(ctx context.Context, g *d2graph.Graph) error { return d2dagrelayout.Layout(ctx, g, dagreOpts) }, nil
		case "elk":
			return func(ctx context.Context, g *d2graph.Graph) error { return d2elklayout.Layout(ctx, g, elkOpts) }, nil
		case "tala":
			return func(ctx context.Context, g *d2graph.Graph) error { return d2talalayout.Layout(ctx, g, talaOpts) }, nil
		default:
			return nil, &WASMError{Message: fmt.Sprintf("layout option '%s' not recognized", engine), Code: 400}
		}
	}, nil
}
