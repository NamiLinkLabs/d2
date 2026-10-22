//go:build !nodagre

package d2cli

import (
	"context"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/lib/layoutdata"
	"github.com/d2lang/util-go/xmain"
)

const dagreEnabled = true
const dagreShortHelp = "The directed graph layout library Dagre"
const dagreLongHelp = `dagre is a directed graph layout algorithm implemented natively in Go by Dagro.
See https://d2lang.com/tour/dagre for more.

Dagro implements the Dagre 3.1.1 layout surface used by D2: https://github.com/d2lang/dagro.

Flags correspond to ones found at https://github.com/dagrejs/dagre/wiki.

A diagram can set each flag itself under vars.d2-config.data, using the flag's
name without the dashes, e.g. dagre-ranksep: 60. Values set in the diagram take
precedence over the flags.
`

func registerDagreFlags(opts *xmain.Opts) {
	opts.Int64("", "dagre-nodesep", "", int64(d2dagrelayout.DefaultOpts.NodeSep), "number of pixels that separate nodes horizontally.")
	opts.Int64("", "dagre-edgesep", "", int64(d2dagrelayout.DefaultOpts.EdgeSep), "number of pixels that separate edges horizontally.")
	opts.Int64("", "dagre-ranksep", "", d2dagrelayout.DefaultRankSep, "number of pixels between ranks. Edge labels can make it larger.")
	opts.String("", "dagre-padding", "", strconv.Itoa(d2dagrelayout.DEFAULT_PADDING), "padding containers are fitted to around their contents: one number, or [top=..,left=..,bottom=..,right=..]. Containers never grow beyond dagre's own spacing.")
}

func dagreLayout(ms *xmain.State) (d2graph.LayoutGraph, error) {
	opts := d2dagrelayout.DefaultOpts
	var err error
	opts.NodeSep, err = layoutIntFlag(ms, "dagre-nodesep", opts.NodeSep)
	if err != nil {
		return nil, err
	}
	opts.EdgeSep, err = layoutIntFlag(ms, "dagre-edgesep", opts.EdgeSep)
	if err != nil {
		return nil, err
	}
	opts.RankSep, err = layoutChangedPixelsFlag(ms, "dagre-ranksep")
	if err != nil {
		return nil, err
	}
	opts.Padding, err = layoutStringFlag(ms, "dagre-padding", opts.Padding)
	if err != nil {
		return nil, err
	}
	if opts.Padding != "" {
		if _, err := layoutdata.ParsePadding(opts.Padding); err != nil {
			return nil, xmain.UsageErrorf("--dagre-padding: %v", err)
		}
	}
	return func(ctx context.Context, g *d2graph.Graph) error {
		return d2dagrelayout.Layout(ctx, g, &opts)
	}, nil
}
