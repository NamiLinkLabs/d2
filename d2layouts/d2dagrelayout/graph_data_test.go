package d2dagrelayout

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/textmeasure"
)

const imageInContainer = `c: {
  i: {shape: image; icon: https://icons.terrastruct.com/essentials/004-picture.svg}
}
x -> c.i
`

func TestWithGraphData(t *testing.T) {
	opts := DefaultOpts
	opts.RankSep = go2.Pointer(500)
	got, err := withGraphData(opts, map[string]any{
		"dagre-nodesep": "1",
		"DAGRE-EDGESEP": "2",
		"dagre-ranksep": "3",
		"dagre-padding": "4",
		"elk-nodeNode":  "5",
		"cat":           "hat",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := ConfigurableOpts{NodeSep: 1, EdgeSep: 2, RankSep: go2.Pointer(3), Padding: "[top=4,left=4,bottom=4,right=4]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withGraphData = %+v, want %+v", got, want)
	}
	if *opts.RankSep != 500 {
		t.Fatal("withGraphData changed the caller's options")
	}
	if got, err := withGraphData(DefaultOpts, map[string]any{"cat": "hat"}); err != nil || !reflect.DeepEqual(got, DefaultOpts) {
		t.Fatalf("data without dagre keys changed options: %+v, %v", got, err)
	}
}

func TestGraphDataSetsRankSep(t *testing.T) {
	gap := func(data map[string]any) float64 {
		g := dagreFixture(t, "a -> b", data, nil)
		a, b := objectByID(t, g, "a"), objectByID(t, g, "b")
		return b.TopLeft.Y - (a.TopLeft.Y + a.Height)
	}
	if got := gap(nil); got != DefaultRankSep {
		t.Errorf("default rank gap = %g, want %d", got, DefaultRankSep)
	}
	if got := gap(map[string]any{"dagre-ranksep": "60"}); got != 60 {
		t.Errorf("rank gap = %g, want 60", got)
	}
	// dagre places edge labels between ranks, so a label keeps its room.
	if got := gap(map[string]any{"dagre-ranksep": "0"}); got != 40 {
		t.Errorf("rank gap = %g, want the 40px floor", got)
	}
}

func TestGraphDataSetsContainerPadding(t *testing.T) {
	sides := func(data map[string]any) (top, left, bottom, right float64) {
		g := dagreFixture(t, imageInContainer, data, nil)
		c, i := objectByID(t, g, "c"), objectByID(t, g, "c.i")
		top = i.TopLeft.Y - c.TopLeft.Y
		left = i.TopLeft.X - c.TopLeft.X
		bottom = c.TopLeft.Y + c.Height - (i.TopLeft.Y + i.Height)
		right = c.TopLeft.X + c.Width - (i.TopLeft.X + i.Width)
		return top, left, bottom, right
	}
	dTop, dLeft, dBottom, dRight := sides(nil)
	if dLeft != DEFAULT_PADDING || dRight != DEFAULT_PADDING {
		t.Errorf("default side padding = %g/%g, want %g", dLeft, dRight, DEFAULT_PADDING)
	}

	top, left, bottom, right := sides(map[string]any{"dagre-padding": "8"})
	if left != 8 || right != 8 {
		t.Errorf("side padding = %g/%g, want 8", left, right)
	}
	if top >= dTop || bottom > dBottom {
		t.Errorf("padding 8 left top/bottom at %g/%g, want less than the default %g/%g", top, bottom, dTop, dBottom)
	}

	_, left, _, right = sides(map[string]any{"dagre-padding": "[top=30,left=4,bottom=30,right=12]"})
	if left != 4 || right != 12 {
		t.Errorf("per-side padding = %g/%g, want 4/12", left, right)
	}

	// Padding only shrinks containers to fit: beyond the room dagre gave the
	// container, a larger padding leaves it as dagre sized it.
	_, left, _, _ = sides(map[string]any{"dagre-padding": "200"})
	if left >= 200 {
		t.Errorf("padding 200 grew the container to a left padding of %g", left)
	}
	large := objectByID(t, dagreFixture(t, imageInContainer, map[string]any{"dagre-padding": "200"}, nil), "c")
	larger := objectByID(t, dagreFixture(t, imageInContainer, map[string]any{"dagre-padding": "1000"}, nil), "c")
	if large.Width != larger.Width || large.Height != larger.Height {
		t.Errorf("padding 200 and 1000 gave containers of %gx%g and %gx%g, want dagre's own size for both", large.Width, large.Height, larger.Width, larger.Height)
	}
}

func TestGraphDataOverridesOpts(t *testing.T) {
	g := dagreFixture(t, "a -> b", map[string]any{"dagre-ranksep": "60"}, func(opts *ConfigurableOpts) {
		opts.RankSep = go2.Pointer(300)
	})
	a, b := objectByID(t, g, "a"), objectByID(t, g, "b")
	if gap := b.TopLeft.Y - (a.TopLeft.Y + a.Height); gap != 60 {
		t.Fatalf("rank gap = %g, want the diagram's 60", gap)
	}
	g = dagreFixture(t, "a -> b", nil, func(opts *ConfigurableOpts) {
		opts.RankSep = go2.Pointer(300)
	})
	a, b = objectByID(t, g, "a"), objectByID(t, g, "b")
	if gap := b.TopLeft.Y - (a.TopLeft.Y + a.Height); gap != 300 {
		t.Fatalf("rank gap = %g, want the caller's 300", gap)
	}
}

func TestGraphDataUnsetKeepsLayout(t *testing.T) {
	base := dagreFixture(t, imageInContainer, nil, nil)
	same := dagreFixture(t, imageInContainer, map[string]any{"cat": "hat", "elk-nodeNode": "1"}, nil)
	assertSameBoxes(t, base, same)
	explicit := dagreFixture(t, imageInContainer, nil, func(opts *ConfigurableOpts) {
		opts.RankSep = go2.Pointer(DefaultRankSep)
		opts.Padding = "30"
	})
	assertSameBoxes(t, base, explicit)
}

func TestGraphDataRejectsInvalidValues(t *testing.T) {
	for key, value := range map[string]any{
		"dagre-ranksep": "-3",
		"dagre-padding": "[top=x]",
	} {
		g, _, err := d2compiler.Compile("", strings.NewReader("a -> b"), nil)
		if err != nil {
			t.Fatal(err)
		}
		g.Data = map[string]any{key: value}
		if err := DefaultLayout(context.Background(), g); err == nil || !strings.Contains(err.Error(), `for "`+key+`"`) {
			t.Errorf("%s: %v: Layout error = %v", key, value, err)
		}
	}
	g, _, err := d2compiler.Compile("", strings.NewReader("a -> b"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Layout(context.Background(), g, &ConfigurableOpts{Padding: "lots"}); err == nil || !strings.Contains(err.Error(), `invalid dagre padding "lots"`) {
		t.Errorf("Layout error = %v", err)
	}
}

func dagreFixture(t *testing.T, input string, data map[string]any, configure func(*ConfigurableOpts)) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("index.d2", strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.ApplyTheme(d2themescatalog.NeutralDefault.ID); err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	g.Data = data
	opts := DefaultOpts
	if configure != nil {
		configure(&opts)
	}
	if err := Layout(context.Background(), g, &opts); err != nil {
		t.Fatal(err)
	}
	return g
}

func objectByID(t *testing.T, g *d2graph.Graph, id string) *d2graph.Object {
	t.Helper()
	for _, obj := range g.Objects {
		if obj.AbsID() == id {
			return obj
		}
	}
	t.Fatalf("object %q not found", id)
	return nil
}

func assertSameBoxes(t *testing.T, want, got *d2graph.Graph) {
	t.Helper()
	for i, obj := range want.Objects {
		w, g := obj.Box, got.Objects[i].Box
		if *w.TopLeft != *g.TopLeft || w.Width != g.Width || w.Height != g.Height {
			t.Errorf("%s moved from %v %gx%g to %v %gx%g", obj.AbsID(), w.TopLeft, w.Width, w.Height, g.TopLeft, g.Width, g.Height)
		}
	}
	for i, edge := range want.Edges {
		if !reflect.DeepEqual(edge.Route, got.Edges[i].Route) {
			t.Errorf("%s route changed", edge.AbsID())
		}
	}
}
