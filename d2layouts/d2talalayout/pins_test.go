package d2talalayout_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func compileTALA(t *testing.T, source string) map[string]d2target.Shape {
	t.Helper()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	layout := "tala"
	ctx := d2log.With(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	diagram, _, err := d2lib.Compile(ctx, source, &d2lib.CompileOptions{
		Ruler:  ruler,
		Layout: &layout,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return func(ctx context.Context, g *d2graph.Graph) error {
				return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{1}, MaxConcurrency: 1})
			}, nil
		},
		RouterResolver: func(string) (d2graph.RouteEdges, error) { return d2talalayout.RouteEdges, nil },
	}, &d2svg.RenderOpts{})
	if err != nil {
		t.Fatal(err)
	}
	shapes := make(map[string]d2target.Shape, len(diagram.Shapes))
	for _, shape := range diagram.Shapes {
		shapes[shape.ID] = shape
	}
	return shapes
}

func requireAt(t *testing.T, shapes map[string]d2target.Shape, id string, x, y int) {
	t.Helper()
	shape, ok := shapes[id]
	if !ok {
		t.Fatalf("missing shape %q", id)
	}
	if shape.Pos.X != x || shape.Pos.Y != y {
		t.Fatalf("%s at (%d,%d), want pinned (%d,%d)", id, shape.Pos.X, shape.Pos.Y, x, y)
	}
}

func requireInside(t *testing.T, shapes map[string]d2target.Shape, child, container string) {
	t.Helper()
	c, p := shapes[child], shapes[container]
	if c.Pos.X < p.Pos.X || c.Pos.Y < p.Pos.Y ||
		c.Pos.X+c.Width > p.Pos.X+p.Width || c.Pos.Y+c.Height > p.Pos.Y+p.Height {
		t.Fatalf("%s %v %dx%d is not inside %s %v %dx%d", child, c.Pos, c.Width, c.Height, container, p.Pos, p.Width, p.Height)
	}
}

func overlaps(a, b d2target.Shape) bool {
	return a.Pos.X < b.Pos.X+b.Width && b.Pos.X < a.Pos.X+a.Width &&
		a.Pos.Y < b.Pos.Y+b.Height && b.Pos.Y < a.Pos.Y+a.Height
}

// D2's top and left are absolute diagram coordinates at every nesting level.
func TestPinsAreAbsoluteInsideContainers(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		shapes := compileTALA(t, "a -> b\nb: {top: 0; left: 600}\nc -> a\n")
		requireAt(t, shapes, "b", 600, 0)
	})
	t.Run("child of unpinned container", func(t *testing.T) {
		shapes := compileTALA(t, "a -> b\ng: {\n  p\n  q: {top: 500; left: 50}\n  p -> q\n}\na -> g.p\n")
		requireAt(t, shapes, "g.q", 50, 500)
		requireInside(t, shapes, "g.q", "g")
		requireInside(t, shapes, "g.p", "g")
		// The container hugs its pinned child rather than stretching from the
		// pin's container-relative origin.
		if g := shapes["g"]; g.Pos.Y < 500-100 || g.Pos.X < 50-100 {
			t.Fatalf("container g at %v does not hug its pinned child", g.Pos)
		}
	})
	t.Run("two pinned children", func(t *testing.T) {
		shapes := compileTALA(t, "g: {\n  p: {top: 100; left: 100}\n  q: {top: 300; left: 400}\n  r\n  p -> q -> r\n}\na -> g.r\n")
		requireAt(t, shapes, "g.p", 100, 100)
		requireAt(t, shapes, "g.q", 400, 300)
		requireInside(t, shapes, "g.r", "g")
	})
	t.Run("nested two levels", func(t *testing.T) {
		shapes := compileTALA(t, "o: {\n  g: {\n    p\n    q: {top: 500; left: 50}\n    p -> q\n  }\n  s\n  s -> g.p\n}\na -> o.s\n")
		requireAt(t, shapes, "o.g.q", 50, 500)
		requireInside(t, shapes, "o.g.q", "o.g")
		requireInside(t, shapes, "o.g", "o")
	})
	t.Run("pinned container and child", func(t *testing.T) {
		shapes := compileTALA(t, "g: {\n  top: 200\n  left: 300\n  p\n  q: {top: 400; left: 420}\n  p -> q\n}\na -> g.p\n")
		requireAt(t, shapes, "g", 300, 200)
		requireAt(t, shapes, "g.q", 420, 400)
		requireInside(t, shapes, "g.q", "g")
	})
	t.Run("tall container label", func(t *testing.T) {
		// A three-line label needs more than the default container padding,
		// which the verification pass measures and corrects.
		shapes := compileTALA(t, "g: \"one\\ntwo\\nthree\\nfour\" {\n  p\n  q: {top: 400; left: 80}\n  p -> q\n}\na -> g.p\n")
		requireAt(t, shapes, "g.q", 80, 400)
		requireInside(t, shapes, "g.q", "g")
	})
	t.Run("container with icon", func(t *testing.T) {
		shapes := compileTALA(t, "g: {\n  icon: https://icons.terrastruct.com/essentials/004-picture.svg\n  p\n  q: {top: 300; left: 200}\n  p -> q\n}\n")
		requireAt(t, shapes, "g.q", 200, 300)
		requireInside(t, shapes, "g.q", "g")
	})
	t.Run("pinned siblings and unpinned root nodes", func(t *testing.T) {
		shapes := compileTALA(t, "x -> y\ny -> g.p\ng: {\n  p\n  q: {top: 50; left: 700}\n  p -> q\n}\nh: {\n  r: {top: 600; left: 0}\n}\ng.q -> h.r\n")
		requireAt(t, shapes, "g.q", 700, 50)
		requireAt(t, shapes, "h.r", 0, 600)
		requireInside(t, shapes, "g.q", "g")
		requireInside(t, shapes, "h.r", "h")
		if overlaps(shapes["g"], shapes["h"]) {
			t.Fatalf("containers overlap: g %v, h %v", shapes["g"].Pos, shapes["h"].Pos)
		}
	})
}

// A child pinned above and left of its explicitly pinned container cannot
// honor both pins. The container pin wins and the child stays inside.
func TestConflictingContainerPinKeepsChildInside(t *testing.T) {
	shapes := compileTALA(t, "g: {\n  top: 300\n  left: 300\n  p\n  q: {top: 0; left: 0}\n  p -> q\n}\n")
	requireAt(t, shapes, "g", 300, 300)
	requireInside(t, shapes, "g.q", "g")
}

func TestNestedPinsAreDeterministic(t *testing.T) {
	source := "a -> b\ng: {\n  p\n  q: {top: 500; left: 50}\n  p -> q\n}\na -> g.p\n"
	first, err := json.Marshal(compileTALA(t, source))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(compileTALA(t, source))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("nested pin layout is not deterministic")
	}
}
