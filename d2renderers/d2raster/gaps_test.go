package d2raster_test

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// a -> b runs across, below c -> d running down a container; they cross at
// (100, 100).
func gapsDiagram() *d2target.Diagram {
	diagram := d2target.NewDiagram()
	diagram.Root.Fill = "#ffffff"
	box := func(id string, x, y, w, h int, fill string) d2target.Shape {
		return d2target.Shape{ID: id, Type: d2target.ShapeRectangle, Pos: d2target.Point{X: x, Y: y}, Width: w, Height: h, Fill: fill, Stroke: "#222222", StrokeWidth: 2, Opacity: 1}
	}
	diagram.Shapes = []d2target.Shape{
		box("box", 40, 40, 120, 120, "#ff0000"),
		box("a", -40, 80, 40, 40, "#ffffff"), box("b", 200, 80, 40, 40, "#ffffff"),
		box("c", 80, -40, 40, 40, "#ffffff"), box("d", 80, 200, 40, 40, "#ffffff"),
	}
	connection := func(id string, route ...*geo.Point) d2target.Connection {
		return d2target.Connection{ID: id, Route: route, SrcArrow: d2target.NoArrowhead, DstArrow: d2target.NoArrowhead, Stroke: "#0000ff", StrokeWidth: 2, Opacity: 1, ZIndex: 1}
	}
	diagram.Connections = []d2target.Connection{
		connection("a -> b", geo.NewPoint(0, 100), geo.NewPoint(200, 100)),
		connection("c -> d", geo.NewPoint(100, 0), geo.NewPoint(100, 200)),
	}
	return diagram
}

func renderGaps(t *testing.T, gaps bool) (*image.NRGBA, func(x, y float64) color.NRGBA) {
	t.Helper()
	pad := int64(0)
	document, err := d2scenebuild.Build(context.Background(), gapsDiagram(), d2scenebuild.Options{Pad: &pad, IntersectionGaps: gaps})
	if err != nil {
		t.Fatal(err)
	}
	img, err := d2raster.Render(context.Background(), document, d2raster.FrameOptions{
		Scale: 1, Background: color.White,
		MaxWidth: 1_000, MaxHeight: 1_000, MaxPixels: 1_000_000,
		MaxNodes: 10_000, MaxDepth: 100, MaxPathCommands: 100_000,
		MaxAnimationTracks: 100, MaxAnimationKeyframes: 1_000,
		MaxAssets: 100, MaxAssetBytes: 64 << 20, MaxDecodedAssetBytes: 64 << 20, MaxImportDepth: 32,
		MaxOffscreenBytes: 64 << 20, MaxEvenOddClipWork: 1_000_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y float64) color.NRGBA {
		px := int(math.Floor(x - document.ViewBox.X))
		py := int(math.Floor(y - document.ViewBox.Y))
		return img.NRGBAAt(img.Bounds().Min.X+px, img.Bounds().Min.Y+py)
	}
	return img, at
}

func TestIntersectionGapsPixels(t *testing.T) {
	t.Parallel()
	blue := color.NRGBA{B: 0xff, A: 0xff}
	red := color.NRGBA{R: 0xff, A: 0xff}
	_, at := renderGaps(t, true)
	// Just beside the line on top, the line below is left out: the box's fill
	// shows through.
	for _, x := range []float64{96.5, 103.5} {
		if got := at(x, 100); got != red {
			t.Fatalf("gap at (%v, 100) = %v, want the box's fill %v", x, got, red)
		}
	}
	// The line on top stays whole, and so does the one below away from it.
	for _, p := range [][2]float64{{100.5, 100.5}, {100.5, 60.5}, {60.5, 100.5}, {140.5, 100.5}} {
		if got := at(p[0], p[1]); got != blue {
			t.Fatalf("line at %v = %v, want %v", p, got, blue)
		}
	}

	_, at = renderGaps(t, false)
	if got := at(96.5, 100); got != blue {
		t.Fatalf("without gaps, (96.5, 100) = %v, want the line %v", got, blue)
	}
}
