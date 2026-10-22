package d2elklayout

import (
	"github.com/d2lang/d2/lib/layoutdata"
)

// withGraphData returns opts with the ELK options the diagram sets under
// vars.d2-config.data (elk-nodeNode and so on). Values from the diagram take
// precedence over the caller's, as tala-seeds does for TALA, so a diagram lays
// out the same in the CLI, D2.js and Go.
func withGraphData(opts ConfigurableOpts, data map[string]interface{}) (ConfigurableOpts, error) {
	if algorithm, ok, err := layoutdata.Algorithm(data, layoutdata.ELKAlgorithm); err != nil {
		return opts, err
	} else if ok {
		opts.Algorithm = algorithm
	}
	if padding, ok, err := layoutdata.PaddingValue(data, layoutdata.ELKPadding); err != nil {
		return opts, err
	} else if ok {
		opts.Padding = padding
	}
	for _, option := range []struct {
		key string
		set func(int)
	}{
		{layoutdata.ELKNodeNodeBetweenLayers, func(v int) { opts.NodeSpacing = v }},
		{layoutdata.ELKEdgeNodeBetweenLayers, func(v int) { opts.EdgeNodeSpacing = v }},
		{layoutdata.ELKEdgeEdgeBetweenLayers, func(v int) { opts.EdgeEdgeSpacing = &v }},
		{layoutdata.ELKNodeSelfLoop, func(v int) { opts.SelfLoopSpacing = v }},
		{layoutdata.ELKNodeNode, func(v int) { opts.InLayerNodeSpacing = &v }},
		{layoutdata.ELKEdgeNode, func(v int) { opts.InLayerEdgeNodeSpacing = &v }},
		{layoutdata.ELKEdgeEdge, func(v int) { opts.InLayerEdgeSpacing = &v }},
	} {
		v, ok, err := layoutdata.Int(data, option.key)
		if err != nil {
			return opts, err
		}
		if ok {
			option.set(v)
		}
	}
	return opts, nil
}
