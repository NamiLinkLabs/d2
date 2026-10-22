package d2svg_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func paddedLabelDiagram(padding int, language string) *d2target.Diagram {
	diagram := d2target.NewDiagram()
	for _, id := range []string{"a", "b"} {
		s := d2target.BaseShape()
		s.ID = id
		s.Type = d2target.ShapeRectangle
		s.Width, s.Height = 20, 20
		if id == "b" {
			s.Pos.X = 80
		}
		diagram.Shapes = append(diagram.Shapes, *s)
	}
	c := d2target.BaseConnection()
	c.ID = "(a -> b)[0]"
	c.Src, c.Dst = "a", "b"
	c.Route = []*geo.Point{geo.NewPoint(20, 10), geo.NewPoint(80, 10)}
	c.Label = "label"
	c.Language = language
	c.FontSize = 16
	c.LabelWidth, c.LabelHeight = 40, 40
	c.LabelPosition = "INSIDE_MIDDLE_CENTER"
	c.LabelPadding = padding
	c.Fill = "#eeeeee"
	diagram.Connections = []d2target.Connection{*c}
	return diagram
}

func renderPaddedLabel(t *testing.T, padding int, language string) string {
	t.Helper()
	out, err := d2svg.Render(paddedLabelDiagram(padding, language), &d2svg.RenderOpts{})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func assertContains(t *testing.T, svg, name, want string) {
	t.Helper()
	if !strings.Contains(svg, want) {
		t.Fatalf("%s: SVG does not contain %s", name, want)
	}
}

func TestRenderConnectionLabelPadding(t *testing.T) {
	t.Parallel()

	// The text box sits at (30,-10) with or without padding.
	unpadded := renderPaddedLabel(t, 0, "")
	assertContains(t, unpadded, "unpadded mask", `<rect x="28" y="-10" width="44" height="40" fill="black"></rect>`)
	assertContains(t, unpadded, "unpadded fill", `<rect x="26" y="-13" width="48" height="46" rx="10" fill="#eeeeee" />`)
	assertContains(t, unpadded, "unpadded text", `<text x="50" y="6"`)

	padded := renderPaddedLabel(t, 12, "")
	assertContains(t, padded, "padded mask", `<rect x="18" y="-22" width="64" height="64" fill="black"></rect>`)
	assertContains(t, padded, "padded fill", `<rect x="18" y="-22" width="64" height="64" rx="10" fill="#eeeeee" />`)
	assertContains(t, padded, "padded text", `<text x="50" y="6"`)
}

func TestRenderMarkdownConnectionLabelPadding(t *testing.T) {
	t.Parallel()

	unpadded := renderPaddedLabel(t, 0, "markdown")
	assertContains(t, unpadded, "unpadded viewport", `<svg x="30" y="-10" width="40" height="40" viewBox="0 0 40 40" overflow="hidden"><rect width="40" height="40" fill="#eeeeee" />`)

	padded := renderPaddedLabel(t, 6, "markdown")
	assertContains(t, padded, "padded background", `<rect x="24" y="-16" width="52" height="52" fill="#eeeeee" /><svg x="30" y="-10" width="40" height="40" viewBox="0 0 40 40" overflow="hidden"><`)
	if strings.Contains(padded, `overflow="hidden"><rect width="40" height="40"`) {
		t.Fatal("padded Markdown label painted its clipped background too")
	}
	assertContains(t, padded, "padded mask", `<rect x="24" y="-16" width="52" height="52" fill="black"></rect>`)
}
