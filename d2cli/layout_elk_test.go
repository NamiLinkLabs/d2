//go:build !noelk

package d2cli

import (
	"context"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
)

func TestELKLayoutOptions(t *testing.T) {
	wantOpts := d2elklayout.ConfigurableOpts{
		Algorithm:       "layered",
		NodeSpacing:     123,
		Padding:         "[top=11,left=22,bottom=33,right=44]",
		EdgeNodeSpacing: 67,
		EdgeEdgeSpacing: go2.Pointer(78),
		SelfLoopSpacing: 89,
	}
	want := layoutGraphJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2elklayout.Layout(ctx, g, &wantOpts)
	})
	defaults := layoutGraphJSON(t, d2elklayout.DefaultLayout)
	if defaults == want {
		t.Fatal("test graph does not exercise configured ELK spacing")
	}
	state := layoutOptionsState(t,
		"--elk-algorithm=layered", "--elk-nodeNodeBetweenLayers=123",
		"--elk-padding=[top=11,left=22,bottom=33,right=44]",
		"--elk-edgeNodeBetweenLayers=67", "--elk-edgeEdgeBetweenLayers=78",
		"--elk-nodeSelfLoop=89")
	resolve := LayoutResolver(t.Context(), state)
	if err := state.Opts.Flags.Set("elk-nodeNodeBetweenLayers", "234"); err != nil {
		t.Fatal(err)
	}
	layout, err := resolve("elk")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, layout); got != want {
		t.Fatal("ELK command-line options differ from native options")
	}
	cached, err := resolve("ELK")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, cached); got != want {
		t.Fatal("changing CLI flags changed an already-resolved ELK layout")
	}
	other, err := LayoutResolver(t.Context(), state)("elk")
	if err != nil {
		t.Fatal(err)
	}
	wantOpts.NodeSpacing = 234
	otherWant := layoutGraphJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2elklayout.Layout(ctx, g, &wantOpts)
	})
	if got := layoutGraphJSON(t, other); got != otherWant {
		t.Fatal("new ELK resolver did not capture changed options")
	}
	if got := layoutGraphJSON(t, layout); got != want {
		t.Fatal("creating another ELK resolver changed the first")
	}
	defaultLayout, err := LayoutResolver(t.Context(), nil)("elk")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, defaultLayout); got != defaults {
		t.Fatal("nil CLI state did not select ELK defaults")
	}
}

func TestELKInLayerSpacingFlags(t *testing.T) {
	const src = "a -> b -> c -> d -> e\na -> e\na -> d\nb -> e\nq -> c\nq -> e"
	native := func(opts d2elklayout.ConfigurableOpts) string {
		return layoutSourceJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
			return d2elklayout.Layout(ctx, g, &opts)
		}, src, nil)
	}
	resolved := func(data map[string]any, args ...string) string {
		layout, err := LayoutResolver(t.Context(), layoutOptionsState(t, args...))("elk")
		if err != nil {
			t.Fatal(err)
		}
		return layoutSourceJSON(t, layout, src, data)
	}

	defaults := native(d2elklayout.DefaultOpts)
	if got := resolved(nil); got != defaults {
		t.Fatal("unset in-layer flags changed the ELK layout")
	}
	// Passing the defaults explicitly matches leaving them out.
	if got := resolved(nil, "--elk-nodeNode=20", "--elk-edgeNode=40", "--elk-edgeEdge=10"); got != defaults {
		t.Fatal("explicit default in-layer flags changed the ELK layout")
	}

	opts := d2elklayout.DefaultOpts
	opts.InLayerNodeSpacing = go2.Pointer(60)
	opts.InLayerEdgeNodeSpacing = go2.Pointer(5)
	opts.InLayerEdgeSpacing = go2.Pointer(30)
	want := native(opts)
	if want == defaults {
		t.Fatal("test graph does not exercise in-layer spacing")
	}
	if got := resolved(nil, "--elk-nodeNode=60", "--elk-edgeNode=5", "--elk-edgeEdge=30"); got != want {
		t.Fatal("ELK in-layer flags differ from native options")
	}

	// Diagram data takes precedence over flags.
	data := map[string]any{"elk-nodeNode": "60", "elk-edgeNode": "5", "elk-edgeEdge": "30"}
	if got := resolved(data, "--elk-nodeNode=1", "--elk-edgeNode=1", "--elk-edgeEdge=1"); got != want {
		t.Fatal("diagram data did not override ELK flags")
	}

	if _, err := LayoutResolver(t.Context(), layoutOptionsState(t, "--elk-nodeNode=-1"))("elk"); err == nil {
		t.Fatal("negative --elk-nodeNode was accepted")
	}
}
