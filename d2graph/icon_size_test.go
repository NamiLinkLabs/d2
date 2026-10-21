package d2graph_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/textmeasure"
)

func iconSizeGraph(t *testing.T, script string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("", strings.NewReader(script), nil)
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	return g
}

func iconSizeObject(t *testing.T, g *d2graph.Graph, id string) *d2graph.Object {
	t.Helper()
	obj, ok := g.Root.HasChild(strings.Split(id, "."))
	if !ok {
		t.Fatalf("object %q not found", id)
	}
	return obj
}

func TestSetDimensionsGrowsLeafForExplicitIcon(t *testing.T) {
	t.Parallel()

	const icon = "https://icons.terrastruct.com/essentials%2F213-alarm.svg"
	g := iconSizeGraph(t, `
auto: x {icon: `+icon+`}
small: x {icon: `+icon+`; icon.width: 16}
big: {icon: `+icon+`; icon.width: 120}
wide: {icon: `+icon+`; icon.width: 160; icon.height: 40}
nolabel: "" {icon: `+icon+`; icon.width: 150}
circle: {shape: circle; icon: `+icon+`; icon.width: 120}
userwidth: {icon: `+icon+`; icon.width: 120; width: 80}
usersize: {icon: `+icon+`; icon.width: 120; width: 80; height: 60}
outside: x {icon: `+icon+` {near: outside-top-left; width: 120}}
border: x {icon: `+icon+` {near: border-top-left; width: 120}}
`)
	auto := iconSizeObject(t, g, "auto")
	small := iconSizeObject(t, g, "small")
	// a small explicit icon fits the automatic size
	if small.Width != auto.Width || small.Height != auto.Height {
		t.Fatalf("small icon resized the shape: %vx%v, automatic %vx%v", small.Width, small.Height, auto.Width, auto.Height)
	}

	labelHeight := float64(auto.LabelDimensions.Height)
	for _, id := range []string{"big", "wide"} {
		obj := iconSizeObject(t, g, id)
		iconWidth, iconHeight := obj.IconSize()
		if obj.Width < float64(iconWidth+2*label.PADDING) {
			t.Fatalf("%s: width %v does not fit a %d px icon", id, obj.Width, iconWidth)
		}
		// room for the label row above and below the centered icon
		if obj.Height < float64(iconHeight)+2*(labelHeight+2*label.PADDING) {
			t.Fatalf("%s: height %v does not fit a %d px icon and its label", id, obj.Height, iconHeight)
		}
	}

	if obj := iconSizeObject(t, g, "nolabel"); obj.Width != 160 || obj.Height != 160 {
		t.Fatalf("label-less shape: %vx%v, want 160x160", obj.Width, obj.Height)
	}
	if obj := iconSizeObject(t, g, "circle"); obj.Width != obj.Height || obj.Width < 120 {
		t.Fatalf("circle: %vx%v", obj.Width, obj.Height)
	}

	userWidth := iconSizeObject(t, g, "userwidth")
	if userWidth.Width != 80 || userWidth.Height <= auto.Height {
		t.Fatalf("user width: %vx%v, want width 80 and a taller shape", userWidth.Width, userWidth.Height)
	}
	if obj := iconSizeObject(t, g, "usersize"); obj.Width != 80 || obj.Height != 60 {
		t.Fatalf("user size: %vx%v, want 80x60", obj.Width, obj.Height)
	}

	// icons that are not inside the shape don't grow it
	for _, id := range []string{"outside", "border"} {
		obj := iconSizeObject(t, g, id)
		if obj.Width != auto.Width || obj.Height != auto.Height {
			t.Fatalf("%s: %vx%v, want the automatic %vx%v", id, obj.Width, obj.Height, auto.Width, auto.Height)
		}
	}
}

func TestSetDimensionsIgnoresExplicitIconOnContainersAndTables(t *testing.T) {
	t.Parallel()

	const icon = "https://icons.terrastruct.com/essentials%2F213-alarm.svg"
	withSize := iconSizeGraph(t, `
group: {icon: `+icon+`; icon.width: 120; a}
table: {shape: sql_table; id: int; icon: `+icon+`; icon.width: 120}
`)
	without := iconSizeGraph(t, `
group: {icon: `+icon+`; a}
table: {shape: sql_table; id: int; icon: `+icon+`}
`)
	for _, id := range []string{"group", "table"} {
		a, b := iconSizeObject(t, withSize, id), iconSizeObject(t, without, id)
		if a.Width != b.Width || a.Height != b.Height {
			t.Fatalf("%s: %vx%v with icon.width, %vx%v without", id, a.Width, a.Height, b.Width, b.Height)
		}
	}
}

func TestIconReservedSizeAndSpacing(t *testing.T) {
	t.Parallel()

	const icon = "https://icons.terrastruct.com/essentials%2F213-alarm.svg"
	g := iconSizeGraph(t, `
auto: {icon: `+icon+`; icon.near: outside-top-left}
wide: {icon: `+icon+` {near: outside-left-center; width: 100; height: 20}}
tall: {icon: `+icon+` {near: outside-top-center; width: 20; height: 100}}
img: {shape: image; icon: `+icon+`; icon.width: 100}
`)
	auto := iconSizeObject(t, g, "auto")
	if w, h := auto.IconReservedSize(); w != d2target.MAX_ICON_SIZE || h != d2target.MAX_ICON_SIZE {
		t.Fatalf("automatic reserved size %vx%v", w, h)
	}
	if margin := auto.GetMargin(); margin.Top != d2target.MAX_ICON_SIZE+label.PADDING {
		t.Fatalf("automatic outside-top margin %v", margin.Top)
	}

	wide := iconSizeObject(t, g, "wide")
	if margin := wide.GetMargin(); margin.Left != 100+label.PADDING {
		t.Fatalf("wide outside-left margin %v, want %v", margin.Left, 100+label.PADDING)
	}
	tall := iconSizeObject(t, g, "tall")
	if margin := tall.GetMargin(); margin.Top != 100+label.PADDING {
		t.Fatalf("tall outside-top margin %v, want %v", margin.Top, 100+label.PADDING)
	}
	if margin, _ := tall.Spacing(); margin.Top != 100+2*label.PADDING {
		t.Fatalf("tall outside-top spacing %v, want %v", margin.Top, 100+2*label.PADDING)
	}

	img := iconSizeObject(t, g, "img")
	if w, h := img.IconSize(); w != 0 || h != 0 {
		t.Fatalf("image icon size %vx%v, want automatic", w, h)
	}
}

func TestObjectGetIconBoxMatchesTarget(t *testing.T) {
	t.Parallel()

	const icon = "https://icons.terrastruct.com/essentials%2F213-alarm.svg"
	g := iconSizeGraph(t, `
a: {shape: circle; icon: `+icon+` {near: border-top-left; width: 40; height: 30}}
b: {shape: cylinder; icon: `+icon+`; icon.near: top-right}
t: {shape: sql_table; id: int; icon: `+icon+` {near: outside-top-left; width: 24}}
`)
	for _, obj := range g.Objects {
		obj.TopLeft = geo.NewPoint(10, 20)
		got := obj.GetIconBox()
		target := d2target.Shape{
			Type:         obj.Shape.Value,
			Pos:          d2target.Point{X: 10, Y: 20},
			Width:        int(obj.Width),
			Height:       int(obj.Height),
			Icon:         obj.Icon,
			IconPosition: *obj.IconPosition,
		}
		target.IconWidth, target.IconHeight = obj.IconSize()
		want := target.GetIconBox()
		if got.TopLeft.X != want.TopLeft.X || got.TopLeft.Y != want.TopLeft.Y || got.Width != want.Width || got.Height != want.Height {
			t.Fatalf("%s: graph icon box %s, target icon box %s", obj.ID, got.ToString(), want.ToString())
		}
	}
}
