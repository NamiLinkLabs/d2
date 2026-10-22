package d2elklayout

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
)

func TestInLayerSpacingSerialization(t *testing.T) {
	opts := DefaultOpts
	opts.InLayerNodeSpacing = go2.Pointer(30)
	opts.InLayerEdgeSpacing = go2.Pointer(0)
	opts.InLayerEdgeNodeSpacing = go2.Pointer(9)
	for name, got := range map[string]*elkOpts{
		"root":      newRootLayoutOptions(&opts),
		"container": newContainerLayoutOptions(&opts),
	} {
		fields := serializedOptions(t, got)
		for key, want := range map[string]any{
			"elk.spacing.nodeNode": float64(30),
			"elk.spacing.edgeEdge": float64(0),
			"elk.spacing.edgeNode": float64(9),
		} {
			if fields[key] != want {
				t.Errorf("%s %s = %#v, want %#v", name, key, fields[key], want)
			}
		}
	}

	// Unset in-layer spacings are left to ELK, except edge-node spacing, which
	// D2 has always sent as 40.
	for name, got := range map[string]*elkOpts{
		"root":      newRootLayoutOptions(&DefaultOpts),
		"container": newContainerLayoutOptions(&DefaultOpts),
	} {
		fields := serializedOptions(t, got)
		if _, ok := fields["elk.spacing.nodeNode"]; ok {
			t.Errorf("%s sends elk.spacing.nodeNode by default", name)
		}
		if _, ok := fields["elk.spacing.edgeEdge"]; ok {
			t.Errorf("%s sends elk.spacing.edgeEdge by default", name)
		}
		if fields["elk.spacing.edgeNode"] != float64(40) {
			t.Errorf("%s elk.spacing.edgeNode = %#v, want 40", name, fields["elk.spacing.edgeNode"])
		}
	}
	// Leaf nodes, ports and labels carry no spacing of their own.
	if fields := serializedOptions(t, &elkOpts{SelfLoopDistribution: "EQUALLY"}); fields["elk.spacing.edgeNode"] != nil {
		t.Errorf("leaf options = %v", fields)
	}
	// The options are copied, so changing the caller's value later is safe.
	root := newRootLayoutOptions(&opts)
	*opts.InLayerNodeSpacing = 99
	if *root.InLayerNodeSpacing != 30 {
		t.Error("layout options alias the caller's InLayerNodeSpacing")
	}
}

func TestWithGraphData(t *testing.T) {
	opts := DefaultOpts
	opts.InLayerNodeSpacing = go2.Pointer(5)
	got, err := withGraphData(opts, map[string]any{
		"elk-algorithm":             "mrtree",
		"elk-nodeNodeBetweenLayers": "11",
		"elk-padding":               "15",
		"elk-edgeNodeBetweenLayers": "12",
		"elk-edgeEdgeBetweenLayers": "0",
		"ELK-NodeSelfLoop":          "14",
		"elk-nodeNode":              "80",
		"elk-edgeNode":              "16",
		"elk-edgeEdge":              "17",
		"dagre-ranksep":             "1",
		"cat":                       "hat",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := ConfigurableOpts{
		Algorithm:              "mrtree",
		NodeSpacing:            11,
		Padding:                "[top=15,left=15,bottom=15,right=15]",
		EdgeNodeSpacing:        12,
		EdgeEdgeSpacing:        go2.Pointer(0),
		SelfLoopSpacing:        14,
		InLayerNodeSpacing:     go2.Pointer(80),
		InLayerEdgeNodeSpacing: go2.Pointer(16),
		InLayerEdgeSpacing:     go2.Pointer(17),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withGraphData = %+v\nwant %+v", got, want)
	}
	if *opts.InLayerNodeSpacing != 5 || *DefaultOpts.EdgeEdgeSpacing != default_edge_edge_spacing {
		t.Fatal("withGraphData changed the caller's options")
	}

	got, err = withGraphData(DefaultOpts, map[string]any{"cat": "hat"})
	if err != nil || !reflect.DeepEqual(got, DefaultOpts) {
		t.Fatalf("data without ELK keys changed options: %+v, %v", got, err)
	}
}

func TestGraphDataSetsInLayerNodeSpacing(t *testing.T) {
	// a and b share a layer, so the gap between them is the in-layer spacing.
	const src = "x -> a\nx -> b"
	for _, tt := range []struct {
		data map[string]any
		want float64
	}{
		{data: nil, want: 20},
		{data: map[string]any{"elk-nodeNode": "80"}, want: 80},
		{data: map[string]any{"elk-nodeNode": "0"}, want: 0},
	} {
		// Diagram data wins over the caller's option.
		g := layoutDataFixture(t, src, tt.data, func(opts *ConfigurableOpts) {
			if tt.data != nil {
				opts.InLayerNodeSpacing = go2.Pointer(300)
			}
		})
		a, b := objectByID(t, g, "a"), objectByID(t, g, "b")
		if a.TopLeft.X > b.TopLeft.X {
			a, b = b, a
		}
		if gap := b.TopLeft.X - (a.TopLeft.X + a.Width); gap != tt.want {
			t.Errorf("data %v: gap between a and b = %g, want %g", tt.data, gap, tt.want)
		}
	}
}

func TestGraphDataSetsPadding(t *testing.T) {
	const src = "container: {\n  a\n}"
	for _, tt := range []struct {
		data map[string]any
		want float64
	}{
		{data: nil, want: 50},
		{data: map[string]any{"elk-padding": "15"}, want: 15},
		{data: map[string]any{"elk-padding": "[top=60,left=8,bottom=60,right=60]"}, want: 8},
	} {
		g := layoutDataFixture(t, src, tt.data, nil)
		container, a := objectByID(t, g, "container"), objectByID(t, g, "container.a")
		if left := a.TopLeft.X - container.TopLeft.X; left != tt.want {
			t.Errorf("data %v: left padding = %g, want %g", tt.data, left, tt.want)
		}
	}
}

func TestGraphDataChangesRoutes(t *testing.T) {
	const src = "a -> b -> c -> d -> e\na -> e\na -> d\nb -> e\nq -> c\nq -> e"
	base := routes(layoutDataFixture(t, src, nil, nil))
	for _, data := range []map[string]any{
		{"elk-edgeNode": "5"},
		{"elk-edgeEdge": "40"},
	} {
		if got := routes(layoutDataFixture(t, src, data, nil)); reflect.DeepEqual(got, base) {
			t.Errorf("data %v did not change any route", data)
		}
	}
	if got := routes(layoutDataFixture(t, src, map[string]any{"cat": "hat", "tala-seeds": []any{"1"}}, nil)); !reflect.DeepEqual(got, base) {
		t.Error("data without ELK keys changed the layout")
	}
}

func TestGraphDataRejectsInvalidValues(t *testing.T) {
	for key, value := range map[string]any{
		"elk-nodeNode":  "-3",
		"elk-padding":   "[top=x]",
		"elk-algorithm": "disco",
	} {
		g, _, err := d2compiler.Compile("", strings.NewReader("a -> b"), nil)
		if err != nil {
			t.Fatal(err)
		}
		g.Data = map[string]any{key: value}
		err = Layout(log.WithTB(context.Background(), t), g, nil)
		if err == nil || !strings.Contains(err.Error(), `for "`+key+`"`) {
			t.Errorf("%s: %v: Layout error = %v", key, value, err)
		}
	}
}

func TestParsePadding(t *testing.T) {
	for in, want := range map[string]shapePadding{
		"[top=50,left=50,bottom=50,right=50]": {50, 50, 50, 50},
		"15":                                  {15, 15, 15, 15},
		// Sides left out are 0. A missing right side used to panic.
		"[top=10]":              {top: 10},
		"[top=10,left=5,right]": {top: 10, left: 5},
		// Strings outside the strict syntax keep the lenient reading.
		"top=10.5;left=3": {top: 10, left: 3},
		"":                {},
	} {
		if got := parsePadding(in); got != want {
			t.Errorf("parsePadding(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func layoutDataFixture(t *testing.T, input string, data map[string]any, configure func(*ConfigurableOpts)) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("", strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	for _, obj := range g.Objects {
		obj.Box = geo.NewBox(nil, 60, 40)
	}
	g.Data = data
	opts := DefaultOpts
	if configure != nil {
		configure(&opts)
	}
	if err := Layout(log.WithTB(context.Background(), t), g, &opts); err != nil {
		t.Fatalf("layout fixture: %v", err)
	}
	return g
}

func routes(g *d2graph.Graph) map[string][]geo.Point {
	out := make(map[string][]geo.Point, len(g.Edges))
	for _, edge := range g.Edges {
		points := make([]geo.Point, len(edge.Route))
		for i, p := range edge.Route {
			points[i] = *p
		}
		out[edge.AbsID()] = points
	}
	return out
}

func serializedOptions(t *testing.T, opts *elkOpts) map[string]any {
	t.Helper()
	raw, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
