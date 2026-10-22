//go:build js && wasm

package d2elklayout

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/lib/geo"
)

func TestConvertGraphPreservesCustomELKProfile(t *testing.T) {
	g, _, err := d2compiler.Compile("", strings.NewReader("container: {\n  a -> b\n}"), nil)
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	for i, obj := range g.Objects {
		obj.Box = geo.NewBox(geo.NewPoint(float64(i*100), float64(i*50)), 60, 40)
	}
	opts := ConfigurableOpts{
		Algorithm:       "layered",
		NodeSpacing:     123,
		Padding:         "[top=11,left=22,bottom=33,right=44]",
		EdgeNodeSpacing: 67,
		EdgeEdgeSpacing: go2.Pointer(78),
		SelfLoopSpacing: 89,
	}

	elkGraph, err := ConvertGraph(context.Background(), g, &opts)
	if err != nil {
		t.Fatalf("convert graph: %v", err)
	}
	rootOpts := elkGraph.LayoutOptions
	if rootOpts.ConsiderModelOrder != "NODES_AND_EDGES" || rootOpts.CycleBreakingStrategy != "GREEDY_MODEL_ORDER" || rootOpts.ForceNodeModelOrder {
		t.Fatalf("root migration profile = consider %q, cycle %q, force model order %t; want NODES_AND_EDGES/GREEDY_MODEL_ORDER/false",
			rootOpts.ConsiderModelOrder, rootOpts.CycleBreakingStrategy, rootOpts.ForceNodeModelOrder)
	}
	root := rootOpts.ConfigurableOpts
	if root.Algorithm != opts.Algorithm || root.NodeSpacing != opts.NodeSpacing ||
		root.EdgeNodeSpacing != opts.EdgeNodeSpacing ||
		root.EdgeEdgeSpacing == nil || *root.EdgeEdgeSpacing != *opts.EdgeEdgeSpacing ||
		root.SelfLoopSpacing != opts.SelfLoopSpacing {
		t.Fatalf("root profile = %#v, want algorithm/spacing values from %#v", root, opts)
	}
	if len(elkGraph.Children) != 1 || elkGraph.Children[0].ID != "container" {
		t.Fatalf("root children = %#v, want container", elkGraph.Children)
	}
	containerOpts := elkGraph.Children[0].LayoutOptions
	if containerOpts == nil {
		t.Fatal("container layout options are nil")
	}
	if containerOpts.ConsiderModelOrder != "NONE" || containerOpts.CycleBreakingStrategy != "GREEDY" || containerOpts.ForceNodeModelOrder {
		t.Fatalf("container migration profile = consider %q, cycle %q, force model order %t; want NONE/GREEDY/false",
			containerOpts.ConsiderModelOrder, containerOpts.CycleBreakingStrategy, containerOpts.ForceNodeModelOrder)
	}
	// EdgeEdgeSpacing is a pointer, so compare by value rather than identity.
	if !reflect.DeepEqual(containerOpts.ConfigurableOpts, ConfigurableOpts{
		NodeSpacing:     opts.NodeSpacing,
		Padding:         opts.Padding,
		EdgeNodeSpacing: opts.EdgeNodeSpacing,
		EdgeEdgeSpacing: opts.EdgeEdgeSpacing,
		SelfLoopSpacing: opts.SelfLoopSpacing,
	}) {
		t.Fatalf("container profile = %#v, want custom spacing and padding", containerOpts.ConfigurableOpts)
	}
}

func TestConvertGraphPinsPorts(t *testing.T) {
	g, _, err := d2compiler.Compile("", strings.NewReader("a -> b: {source-port: right; target-port: top}\nc -> b"), nil)
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	for i, obj := range g.Objects {
		obj.Box = geo.NewBox(geo.NewPoint(float64(i*100), float64(i*50)), 60, 40)
	}
	elkGraph, err := ConvertGraph(context.Background(), g, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]*ELKNode{}
	for _, n := range elkGraph.Children {
		nodes[n.ID] = n
	}
	for id, side := range map[string]PortSide{"a": East, "b": North} {
		n := nodes[id]
		if n.LayoutOptions.PortConstraints != "FIXED_SIDE" || len(n.Ports) != 1 || n.Ports[0].LayoutOptions.PortSide != side {
			t.Fatalf("%s: constraints %q ports %+v, want one %s port", id, n.LayoutOptions.PortConstraints, n.Ports, side)
		}
	}
	if n := nodes["c"]; n.LayoutOptions.PortConstraints != "" || len(n.Ports) != 0 {
		t.Fatalf("unpinned c got ports: %+v", n)
	}
	if got := elkGraph.Edges[0].Sources[0]; got != nodes["a"].Ports[0].ID {
		t.Fatalf("pinned edge source = %q, want port %q", got, nodes["a"].Ports[0].ID)
	}
	if got := elkGraph.Edges[1].Sources[0]; got != "c" {
		t.Fatalf("unpinned edge source = %q, want c", got)
	}
}
