package d2talalayout

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

const testIconURL = "https://icons.terrastruct.com/essentials%2F213-alarm.svg"

func TestTranslateGraphCarriesExplicitIconSize(t *testing.T) {
	g := compileD2ForPins(t, `a: {icon: `+testIconURL+`; icon.width: 48; icon.height: 24}
b: {icon: `+testIconURL+`; icon.width: 40}
c: {icon: `+testIconURL+`}
img: {shape: image; icon: `+testIconURL+`; icon.width: 48}
`)
	talaGraph := layoutgraph.NewGraph()
	bindings, err := translateGraph(t.Context(), g, talaGraph, false)
	if err != nil {
		t.Fatal(err)
	}
	nodes := make(map[layoutgraph.EntityID]*layoutgraph.Node, len(talaGraph.Nodes))
	for _, node := range talaGraph.Nodes {
		nodes[node.ID] = node
	}
	want := map[string][2]float64{"a": {48, 24}, "b": {40, 40}, "c": {0, 0}, "img": {0, 0}}
	for obj, id := range bindings.objectIDs {
		size, ok := want[obj.ID]
		if !ok {
			continue
		}
		node := nodes[id]
		if node == nil || node.Icon == nil {
			t.Fatalf("%s: no translated icon", obj.ID)
		}
		if node.Icon.Width != size[0] || node.Icon.Height != size[1] {
			t.Fatalf("%s: icon size %vx%v, want %vx%v", obj.ID, node.Icon.Width, node.Icon.Height, size[0], size[1])
		}
	}
}

func TestLayoutReservesExplicitContainerIconSize(t *testing.T) {
	containerSize := func(source string) (width, height float64) {
		t.Helper()
		g := compileD2ForPins(t, source)
		opts := DefaultOptions()
		opts.Seeds = []int64{1}
		opts.MaxConcurrency = 1
		if err := Layout(t.Context(), g, &opts); err != nil {
			t.Fatal(err)
		}
		obj, ok := g.Root.HasChild([]string{"g"})
		if !ok {
			t.Fatal("container not found")
		}
		return obj.Width, obj.Height
	}
	smallWidth, smallHeight := containerSize(`g: {icon: ` + testIconURL + `; icon.width: 16; a -> b}`)
	autoWidth, autoHeight := containerSize(`g: {icon: ` + testIconURL + `; a -> b}`)
	bigWidth, bigHeight := containerSize(`g: {icon: ` + testIconURL + `; icon.width: 160; a -> b}`)
	if smallWidth*smallHeight >= autoWidth*autoHeight {
		t.Fatalf("a 16 px icon reserved as much as the automatic size: %vx%v vs %vx%v", smallWidth, smallHeight, autoWidth, autoHeight)
	}
	if bigWidth*bigHeight <= autoWidth*autoHeight {
		t.Fatalf("a 160 px icon reserved no more than the automatic size: %vx%v vs %vx%v", bigWidth, bigHeight, autoWidth, autoHeight)
	}
	if bigWidth < 160 || bigHeight < 160 {
		t.Fatalf("container %vx%v cannot hold a 160 px icon", bigWidth, bigHeight)
	}
}
