//go:build !nodagre

package d2cli

import (
	"context"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
)

func TestDagreLayoutOptions(t *testing.T) {
	wantOpts := d2dagrelayout.ConfigurableOpts{NodeSep: 123, EdgeSep: 67}
	want := layoutGraphJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2dagrelayout.Layout(ctx, g, &wantOpts)
	})
	defaults := layoutGraphJSON(t, d2dagrelayout.DefaultLayout)
	if defaults == want {
		t.Fatal("test graph does not exercise configured Dagre spacing")
	}
	state := layoutOptionsState(t, "--dagre-nodesep=123", "--dagre-edgesep=67")
	resolve := LayoutResolver(t.Context(), state)
	if err := state.Opts.Flags.Set("dagre-nodesep", "234"); err != nil {
		t.Fatal(err)
	}
	layout, err := resolve("dagre")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, layout); got != want {
		t.Fatal("Dagre command-line spacing differs from native options")
	}
	cached, err := resolve("DAGRE")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, cached); got != want {
		t.Fatal("changing CLI flags changed an already-resolved Dagre layout")
	}
	other, err := LayoutResolver(t.Context(), state)("dagre")
	if err != nil {
		t.Fatal(err)
	}
	wantOpts.NodeSep = 234
	otherWant := layoutGraphJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2dagrelayout.Layout(ctx, g, &wantOpts)
	})
	if got := layoutGraphJSON(t, other); got != otherWant {
		t.Fatal("new Dagre resolver did not capture changed options")
	}
	if got := layoutGraphJSON(t, layout); got != want {
		t.Fatal("creating another Dagre resolver changed the first")
	}
	defaultLayout, err := LayoutResolver(t.Context(), nil)("dagre")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, defaultLayout); got != defaults {
		t.Fatal("nil CLI state did not select Dagre defaults")
	}
}

func TestDagreRankSepAndPaddingFlags(t *testing.T) {
	const src = "x -> c.i\nc: {\n  i\n}\nc.i -> y"
	native := func(opts d2dagrelayout.ConfigurableOpts) string {
		return layoutSourceJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
			return d2dagrelayout.Layout(ctx, g, &opts)
		}, src, nil)
	}
	resolved := func(data map[string]any, args ...string) string {
		layout, err := LayoutResolver(t.Context(), layoutOptionsState(t, args...))("dagre")
		if err != nil {
			t.Fatal(err)
		}
		return layoutSourceJSON(t, layout, src, data)
	}

	defaults := native(d2dagrelayout.DefaultOpts)
	if got := resolved(nil); got != defaults {
		t.Fatal("unset dagre flags changed the layout")
	}
	if got := resolved(nil, "--dagre-ranksep=100", "--dagre-padding=30"); got != defaults {
		t.Fatal("explicit default dagre flags changed the layout")
	}

	opts := d2dagrelayout.DefaultOpts
	opts.RankSep = go2.Pointer(50)
	opts.Padding = "[top=10,left=5,bottom=10,right=5]"
	want := native(opts)
	if want == defaults {
		t.Fatal("test graph does not exercise rank separation and padding")
	}
	if got := resolved(nil, "--dagre-ranksep=50", "--dagre-padding=[top=10,left=5,bottom=10,right=5]"); got != want {
		t.Fatal("dagre flags differ from native options")
	}

	// Diagram data takes precedence over flags.
	data := map[string]any{"dagre-ranksep": "50", "dagre-padding": "[top=10,left=5,bottom=10,right=5]"}
	if got := resolved(data, "--dagre-ranksep=300", "--dagre-padding=1"); got != want {
		t.Fatal("diagram data did not override dagre flags")
	}

	for _, arg := range []string{"--dagre-ranksep=-1", "--dagre-padding=lots"} {
		if _, err := LayoutResolver(t.Context(), layoutOptionsState(t, arg))("dagre"); err == nil {
			t.Fatalf("%s was accepted", arg)
		}
	}
}
