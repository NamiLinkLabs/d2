package d2talalayout

import (
	"fmt"

	"github.com/d2lang/d2/d2graph"
)

// separateEdgesDataKey is the diagram data key that gives every connection its
// own route, e.g. vars: {d2-config: {data: {tala-separate-edges: true}}}.
const separateEdgesDataKey = "tala-separate-edges"

// separateEdgesFromData reads tala-separate-edges from diagram data. D2 stores
// scalar config data as strings; Go callers may also store a bool.
func separateEdgesFromData(raw any) (bool, error) {
	switch v := raw.(type) {
	case bool:
		return v, nil
	case string:
		switch v {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, fmt.Errorf("%s must be true or false", separateEdgesDataKey)
}

// resolveSeparateEdges returns whether graph routes every edge on its own.
// Diagram data takes precedence over opts; nil opts means the default, false.
func resolveSeparateEdges(graph *d2graph.Graph, opts *Options) (bool, error) {
	separate := opts != nil && opts.SeparateEdges
	if graph != nil && graph.Data != nil {
		if raw, ok := graph.Data[separateEdgesDataKey]; ok {
			return separateEdgesFromData(raw)
		}
	}
	return separate, nil
}
