package d2cli

import (
	"context"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/util-go/xmain"
)

const talaShortHelp = "TALA is D2's native layout and edge-routing engine."
const talaLongHelp = `TALA is D2's native layout and edge-routing engine for software architecture diagrams.

By default TALA bundles connections that share an endpoint: they leave or enter
it through one port and share a trunk. --tala-separate-edges gives every
connection its own route instead.

Diagram data under tala-seeds and tala-separate-edges takes precedence over the
command-line flags.
`

func registerTALAFlags(opts *xmain.Opts) {
	opts.Int64Slice("", "tala-seeds", "", d2talalayout.DefaultOptions().Seeds, "random seeds for deterministic TALA layout attempts; the best complete result is selected.")
	opts.Bool("", "tala-separate-edges", "", false, "give every connection its own route instead of bundling connections that share an endpoint.")
}

// talaOptions reads the TALA flags of ms. A nil ms gives the defaults.
func talaOptions(ms *xmain.State) (d2talalayout.Options, error) {
	opts := d2talalayout.DefaultOptions()
	var err error
	if hasLayoutFlag(ms, "tala-seeds") {
		// GetInt64Slice returns an owned slice, independent of the flag value.
		opts.Seeds, err = ms.Opts.Flags.GetInt64Slice("tala-seeds")
		if err != nil {
			return opts, err
		}
	}
	if hasLayoutFlag(ms, "tala-separate-edges") {
		opts.SeparateEdges, err = ms.Opts.Flags.GetBool("tala-separate-edges")
		if err != nil {
			return opts, err
		}
	}
	return opts, nil
}

func talaLayout(ms *xmain.State) (d2graph.LayoutGraph, error) {
	opts, err := talaOptions(ms)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, g *d2graph.Graph) error {
		return d2talalayout.Layout(ctx, g, &opts)
	}, nil
}

// talaRouter routes the edges D2 hands back to TALA (connections into nested
// grid and sequence diagrams) with the same flags as the layout.
func talaRouter(ms *xmain.State) (d2graph.RouteEdges, error) {
	opts, err := talaOptions(ms)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) error {
		return d2talalayout.RouteEdgesWithOptions(ctx, g, edges, &opts)
	}, nil
}
