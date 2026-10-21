package d2dagrelayout

import (
	"fmt"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/layoutdata"
)

// withGraphData returns opts with the dagre options the diagram sets under
// vars.d2-config.data (dagre-nodesep and so on). Values from the diagram take
// precedence over the caller's, as tala-seeds does for TALA, so a diagram lays
// out the same in the CLI, D2.js and Go.
func withGraphData(opts ConfigurableOpts, data map[string]interface{}) (ConfigurableOpts, error) {
	for _, option := range []struct {
		key string
		set func(int)
	}{
		{layoutdata.DagreNodeSep, func(v int) { opts.NodeSep = v }},
		{layoutdata.DagreEdgeSep, func(v int) { opts.EdgeSep = v }},
		{layoutdata.DagreRankSep, func(v int) { opts.RankSep = &v }},
	} {
		v, ok, err := layoutdata.Int(data, option.key)
		if err != nil {
			return opts, err
		}
		if ok {
			option.set(v)
		}
	}
	if padding, ok, err := layoutdata.PaddingValue(data, layoutdata.DagrePadding); err != nil {
		return opts, err
	} else if ok {
		opts.Padding = padding
	}
	return opts, nil
}

// parseContainerPadding reads ConfigurableOpts.Padding.
func parseContainerPadding(s string) (geo.Spacing, error) {
	if s == "" {
		return geo.Spacing{Top: DEFAULT_PADDING, Bottom: DEFAULT_PADDING, Left: DEFAULT_PADDING, Right: DEFAULT_PADDING}, nil
	}
	p, err := layoutdata.ParsePadding(s)
	if err != nil {
		return geo.Spacing{}, fmt.Errorf(`invalid dagre padding %q: %v`, s, err)
	}
	return geo.Spacing{Top: float64(p.Top), Bottom: float64(p.Bottom), Left: float64(p.Left), Right: float64(p.Right)}, nil
}
