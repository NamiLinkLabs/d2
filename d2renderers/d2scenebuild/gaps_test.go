package d2scenebuild

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// crossingDiagram has a -> b across and c -> d down, crossing at (100, 100).
func crossingDiagram() *d2target.Diagram {
	diagram := d2target.NewDiagram()
	diagram.Root.Fill = "#ffffff"
	box := func(id string, x, y int) d2target.Shape {
		return d2target.Shape{ID: id, Type: d2target.ShapeRectangle, Pos: d2target.Point{X: x, Y: y}, Width: 40, Height: 40, Fill: "#ffffff", Stroke: "#222222", StrokeWidth: 2, Opacity: 1}
	}
	diagram.Shapes = []d2target.Shape{box("a", -40, 80), box("b", 200, 80), box("c", 80, -40), box("d", 80, 200)}
	connection := func(id, src, dst string, route ...*geo.Point) d2target.Connection {
		return d2target.Connection{ID: id, Src: src, Dst: dst, Route: route, SrcArrow: d2target.NoArrowhead, DstArrow: d2target.TriangleArrowhead, Stroke: "#222222", StrokeWidth: 2, Opacity: 1}
	}
	diagram.Connections = []d2target.Connection{
		connection("(a -> b)[0]", "a", "b", geo.NewPoint(0, 100), geo.NewPoint(200, 100)),
		connection("(c -> d)[0]", "c", "d", geo.NewPoint(100, 0), geo.NewPoint(100, 200)),
	}
	return diagram
}

func findNode(root *d2scene.Node, id string) *d2scene.Node {
	if root == nil {
		return nil
	}
	if root.ID == id {
		return root
	}
	for _, child := range root.Children {
		if match := findNode(child, id); match != nil {
			return match
		}
	}
	return nil
}

func TestBuildIntersectionGaps(t *testing.T) {
	t.Parallel()
	for _, sketch := range []bool{false, true} {
		options := Options{IntersectionGaps: true, Sketch: sketch}
		if sketch {
			options.SketchBudget = SketchBudget{MaxOperationSets: 1_000, MaxOperations: 100_000, MaxPathCommands: 100_000}
		}
		document, err := Build(context.Background(), crossingDiagram(), options)
		if err != nil {
			t.Fatal(err)
		}
		gaps := findNode(document.Root, "(a -> b)[0]:gaps")
		if gaps == nil || gaps.Mask == nil || gaps.Mask.Type != d2scene.MaskLuminance {
			t.Fatalf("sketch=%v: lower connection has no gap mask: %+v", sketch, gaps)
		}
		if findNode(gaps, "(a -> b)[0]:path") == nil || findNode(gaps, "(a -> b)[0]:dst-arrowhead") == nil {
			t.Fatalf("sketch=%v: gap mask doesn't cover the line and its arrowhead", sketch)
		}
		root := gaps.Mask.Root
		if len(root.Children) != 2 {
			t.Fatalf("sketch=%v: gap mask has %d children, want backdrop and one cut", sketch, len(root.Children))
		}
		if base, ok := root.Children[0].Primitive.(d2scene.Rect); !ok || base.Box.X > -10 || base.Box.X+base.Box.Width < 210 || base.Box.Y > 90 || base.Box.Y+base.Box.Height < 110 {
			t.Fatalf("sketch=%v: gap mask backdrop = %+v", sketch, root.Children[0].Primitive)
		}
		cut, ok := root.Children[1].Primitive.(d2scene.Path)
		width := 9.
		if sketch {
			width = 11
		}
		if !ok || cut.Stroke == nil || cut.Stroke.Cap != d2scene.CapButt || cut.Stroke.Width != width ||
			len(cut.Commands) != 2 || cut.Commands[0] != d2scene.MoveTo(100, 93) || cut.Commands[1] != d2scene.LineTo(100, 107) {
			t.Fatalf("sketch=%v: gap cut = %+v", sketch, root.Children[1].Primitive)
		}
		if findNode(document.Root, "(c -> d)[0]:gaps") != nil {
			t.Fatalf("sketch=%v: upper connection got a gap", sketch)
		}
	}

	document, err := Build(context.Background(), crossingDiagram(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if findNode(document.Root, "(a -> b)[0]:gaps") != nil {
		t.Fatal("gaps without the option")
	}
}
