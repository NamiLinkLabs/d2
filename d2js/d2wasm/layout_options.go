//go:build js && wasm

package d2wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/layoutdata"
)

// ELKOptions tunes ELK. Fields left out keep D2's defaults. Layout options the
// diagram sets under vars.d2-config.data (elk-nodeNode and so on) take
// precedence.
type ELKOptions struct {
	NodeSpacing     *int `json:"nodeSpacing"`
	EdgeNodeSpacing *int `json:"edgeNodeSpacing"`
	EdgeEdgeSpacing *int `json:"edgeEdgeSpacing"`
	SelfLoopSpacing *int `json:"selfLoopSpacing"`
	// Padding is one number for all four sides, or "[top=..,left=..,bottom=..,right=..]".
	Padding                *PaddingOption `json:"padding"`
	InLayerNodeSpacing     *int           `json:"inLayerNodeSpacing"`
	InLayerEdgeSpacing     *int           `json:"inLayerEdgeSpacing"`
	InLayerEdgeNodeSpacing *int           `json:"inLayerEdgeNodeSpacing"`
}

// DagreOptions tunes dagre. Fields left out keep D2's defaults. Layout options
// the diagram sets under vars.d2-config.data (dagre-ranksep and so on) take
// precedence.
type DagreOptions struct {
	NodeSep *int `json:"nodeSep"`
	EdgeSep *int `json:"edgeSep"`
	RankSep *int `json:"rankSep"`
	// Padding is the padding containers are fitted to: one number, or
	// "[top=..,left=..,bottom=..,right=..]".
	Padding *PaddingOption `json:"padding"`
}

// PaddingOption is padding given as a number or as a string.
type PaddingOption string

func (p *PaddingOption) UnmarshalJSON(b []byte) error {
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*p = PaddingOption(n.String())
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("padding must be a number or a string")
	}
	*p = PaddingOption(s)
	return nil
}

// resolve returns the padding in ELK syntax.
func (p *PaddingOption) resolve(name string) (string, error) {
	sides, err := layoutdata.ParsePadding(string(*p))
	if err != nil {
		return "", &WASMError{Message: fmt.Sprintf("%s must be a number or \"[top=..,left=..,bottom=..,right=..]\", got %s: %v", name, strconv.Quote(string(*p)), err), Code: 400}
	}
	return sides.String(), nil
}

// TALAOptions tunes TALA. Fields left out keep TALA's defaults.
type TALAOptions struct {
	// Seeds selects the deterministic layout attempts; diagram-level tala-seeds still win.
	Seeds []int64 `json:"seeds"`
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
	for _, v := range []struct {
		name  string
		value *int
	}{
		{"elk.nodeSpacing", o.NodeSpacing},
		{"elk.edgeNodeSpacing", o.EdgeNodeSpacing},
		{"elk.edgeEdgeSpacing", o.EdgeEdgeSpacing},
		{"elk.selfLoopSpacing", o.SelfLoopSpacing},
		{"elk.inLayerNodeSpacing", o.InLayerNodeSpacing},
		{"elk.inLayerEdgeSpacing", o.InLayerEdgeSpacing},
		{"elk.inLayerEdgeNodeSpacing", o.InLayerEdgeNodeSpacing},
	} {
		if err := nonNegative(v.name, v.value); err != nil {
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
		padding, err := o.Padding.resolve("elk.padding")
		if err != nil {
			return nil, err
		}
		opts.Padding = padding
	}
	opts.InLayerNodeSpacing = copyInt(o.InLayerNodeSpacing)
	opts.InLayerEdgeSpacing = copyInt(o.InLayerEdgeSpacing)
	opts.InLayerEdgeNodeSpacing = copyInt(o.InLayerEdgeNodeSpacing)
	return &opts, nil
}

func copyInt(v *int) *int {
	if v == nil {
		return nil
	}
	c := *v
	return &c
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
	if err := nonNegative("dagre.rankSep", o.RankSep); err != nil {
		return nil, err
	}
	if o.NodeSep != nil {
		opts.NodeSep = *o.NodeSep
	}
	if o.EdgeSep != nil {
		opts.EdgeSep = *o.EdgeSep
	}
	opts.RankSep = copyInt(o.RankSep)
	if o.Padding != nil {
		padding, err := o.Padding.resolve("dagre.padding")
		if err != nil {
			return nil, err
		}
		opts.Padding = padding
	}
	return &opts, nil
}

func (o *TALAOptions) resolve() *d2talalayout.Options {
	opts := d2talalayout.DefaultOptions()
	if o != nil && len(o.Seeds) > 0 {
		opts.Seeds = append([]int64(nil), o.Seeds...)
	}
	return &opts
}

// nestedEdgeRouting validates the nestedEdgeRouting compile option. Unset,
// it leaves the choice to the diagram's vars.d2-config.nested-edge-routing,
// then the default, as the CLI does.
func nestedEdgeRouting(opts *CompileOptions) (*string, error) {
	if opts == nil || opts.NestedEdgeRouting == nil {
		return nil, nil
	}
	if !d2target.ValidNestedEdgeRouting(*opts.NestedEdgeRouting) {
		return nil, &WASMError{Message: fmt.Sprintf(`nestedEdgeRouting must be %q or %q, not %q`, d2target.NestedEdgeRoutingAroundShapes, d2target.NestedEdgeRoutingStraight, *opts.NestedEdgeRouting), Code: 400}
	}
	return opts.NestedEdgeRouting, nil
}

// routerResolver picks the router for the connections a layout engine does
// not route itself.
func routerResolver(engine string) (d2graph.RouteEdges, error) {
	return d2layouts.RouterFor(engine), nil
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
	talaOpts := tala.resolve()
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
