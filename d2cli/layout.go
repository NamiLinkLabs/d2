package d2cli

import (
	"context"
	"strings"

	"github.com/spf13/pflag"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/util-go/xmain"
)

func builtinLayoutNames() []string {
	names := make([]string, 0, 3)
	if dagreEnabled {
		names = append(names, "dagre")
	}
	if elkEnabled {
		names = append(names, "elk")
	}
	return append(names, "tala")
}

func isBuiltinLayout(name string) bool {
	switch strings.ToLower(name) {
	case "dagre":
		return dagreEnabled
	case "elk":
		return elkEnabled
	case "tala":
		return true
	default:
		return false
	}
}

func getBuiltinLayout(name string, ms *xmain.State) (d2graph.LayoutGraph, error) {
	switch strings.ToLower(name) {
	case "dagre":
		return dagreLayout(ms)
	case "elk":
		return elkLayout(ms)
	case "tala":
		return talaLayout(ms)
	default:
		return nil, layoutNotFound(name)
	}
}

// LayoutResolver captures the built-in engines' typed options for one compilation.
// Custom Go layouts can be supplied directly to d2lib.CompileOptions.LayoutResolver.
func LayoutResolver(_ context.Context, ms *xmain.State) func(string) (d2graph.LayoutGraph, error) {
	type result struct {
		layout d2graph.LayoutGraph
		err    error
	}
	layouts := make(map[string]result, 3)
	for _, name := range builtinLayoutNames() {
		layout, err := getBuiltinLayout(name, ms)
		layouts[name] = result{layout, err}
	}
	// The map and bound options are read-only after construction.
	return func(name string) (d2graph.LayoutGraph, error) {
		r, ok := layouts[strings.ToLower(name)]
		if !ok {
			return nil, layoutNotFound(name)
		}
		return r.layout, r.err
	}
}

const (
	nestedEdgeRoutingAroundShapes = d2target.NestedEdgeRoutingAroundShapes
	nestedEdgeRoutingStraight     = d2target.NestedEdgeRoutingStraight
)

// nestedEdgeRoutingOption returns --nested-edge-routing (or
// D2_NESTED_EDGE_ROUTING) when it is set, and nil to leave it to the
// diagram's vars.d2-config.nested-edge-routing and then the default.
func nestedEdgeRoutingOption(ms *xmain.State) (*string, error) {
	const name = "nested-edge-routing"
	if !hasLayoutFlag(ms, name) {
		return nil, nil
	}
	if !ms.Opts.Flags.Lookup(name).Changed && ms.Env.Getenv("D2_NESTED_EDGE_ROUTING") == "" {
		return nil, nil
	}
	routing, err := ms.Opts.Flags.GetString(name)
	if err != nil {
		return nil, err
	}
	if !d2target.ValidNestedEdgeRouting(routing) {
		return nil, xmain.UsageErrorf("--nested-edge-routing must be %q or %q, not %q", nestedEdgeRoutingAroundShapes, nestedEdgeRoutingStraight, routing)
	}
	return &routing, nil
}

// RouterResolver selects the edge router for the edges a built-in engine
// does not route itself.
func RouterResolver(_ context.Context, _ *xmain.State) func(string) (d2graph.RouteEdges, error) {
	return func(name string) (d2graph.RouteEdges, error) {
		if !isBuiltinLayout(name) {
			return nil, layoutNotFound(name)
		}
		return d2layouts.RouterFor(name), nil
	}
}

func populateLayoutOpts(ms *xmain.State) {
	opts := xmain.NewOpts(nil, nil)
	registerDagreFlags(opts)
	registerELKFlags(opts)
	registerTALAFlags(opts)
	opts.Flags.VisitAll(func(flag *pflag.Flag) {
		// Engine options are documented by "d2 layout <name>".
		flag.Hidden = true
		ms.Opts.Flags.AddFlag(flag)
	})
}

func hasLayoutFlag(ms *xmain.State, name string) bool {
	return ms != nil && ms.Opts != nil && ms.Opts.Flags != nil && ms.Opts.Flags.Lookup(name) != nil
}

func layoutIntFlag(ms *xmain.State, name string, fallback int) (int, error) {
	if !hasLayoutFlag(ms, name) {
		return fallback, nil
	}
	value, err := ms.Opts.Flags.GetInt64(name)
	if err != nil {
		return 0, err
	}
	if int64(int(value)) != value {
		return 0, xmain.UsageErrorf("--%s is out of range for this platform", name)
	}
	return int(value), nil
}

// layoutChangedPixelsFlag returns the value of a spacing flag the user passed,
// or nil when they did not, so the layout engine keeps its own default.
func layoutChangedPixelsFlag(ms *xmain.State, name string) (*int, error) {
	if !hasLayoutFlag(ms, name) || !ms.Opts.Flags.Lookup(name).Changed {
		return nil, nil
	}
	value, err := layoutIntFlag(ms, name, 0)
	if err != nil {
		return nil, err
	}
	if value < 0 {
		return nil, xmain.UsageErrorf("--%s must not be negative", name)
	}
	return &value, nil
}

func layoutStringFlag(ms *xmain.State, name, fallback string) (string, error) {
	if !hasLayoutFlag(ms, name) {
		return fallback, nil
	}
	return ms.Opts.Flags.GetString(name)
}
