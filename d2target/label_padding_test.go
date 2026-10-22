package d2target

import (
	"math"
	"net/url"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func paddedTestConnection(position label.Position, padding int) *Connection {
	c := BaseConnection()
	c.ID = "(a -> b)[0]"
	c.Route = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(200, 0)}
	c.Label = "hello"
	c.LabelWidth = 40
	c.LabelHeight = 20
	c.LabelPosition = position.String()
	c.LabelPadding = padding
	return c
}

func assertPoint(t *testing.T, name string, got *geo.Point, x, y float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want (%v,%v)", name, x, y)
	}
	if got.X != x || got.Y != y {
		t.Fatalf("%s = (%v,%v), want (%v,%v)", name, got.X, got.Y, x, y)
	}
}

func assertBox(t *testing.T, name string, got *geo.Box, x, y, width, height float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil", name)
	}
	if got.TopLeft.X != x || got.TopLeft.Y != y || got.Width != width || got.Height != height {
		t.Fatalf("%s = (%v,%v %vx%v), want (%v,%v %vx%v)", name,
			got.TopLeft.X, got.TopLeft.Y, got.Width, got.Height, x, y, width, height)
	}
}

func TestConnectionLabelTopLeftWithoutPaddingIsUnchanged(t *testing.T) {
	t.Parallel()
	for _, position := range []label.Position{label.InsideMiddleCenter, label.OutsideTopCenter, label.OutsideBottomLeft, label.UnlockedTop} {
		c := paddedTestConnection(position, 0)
		c.LabelPercentage = 0.3
		want, _ := position.GetPointOnRoute(c.Route, float64(c.StrokeWidth), c.LabelPercentage, 40, 20)
		assertPoint(t, position.String(), c.GetLabelTopLeft(), want.X, want.Y)
		assertBox(t, position.String()+" box", c.LabelBox(), want.X, want.Y, 40, 20)
	}
}

func TestConnectionLabelPadding(t *testing.T) {
	t.Parallel()

	t.Run("inside labels keep their text in place", func(t *testing.T) {
		t.Parallel()
		unpadded := paddedTestConnection(label.InsideMiddleCenter, 0).GetLabelTopLeft()
		c := paddedTestConnection(label.InsideMiddleCenter, 10)
		assertPoint(t, "text", c.GetLabelTopLeft(), unpadded.X, unpadded.Y)
		assertBox(t, "box", c.LabelBox(), unpadded.X-10, unpadded.Y-10, 60, 40)
	})

	t.Run("outside labels move away from the line", func(t *testing.T) {
		t.Parallel()
		unpadded := paddedTestConnection(label.OutsideTopCenter, 0).GetLabelTopLeft()
		c := paddedTestConnection(label.OutsideTopCenter, 10)
		assertPoint(t, "top text", c.GetLabelTopLeft(), unpadded.X, unpadded.Y-10)
		// The padded box keeps the unpadded label's distance to the line.
		box := c.LabelBox()
		if got, want := box.TopLeft.Y+box.Height, unpadded.Y+20; got != want {
			t.Fatalf("padded box bottom = %v, want %v", got, want)
		}

		unpadded = paddedTestConnection(label.OutsideBottomCenter, 0).GetLabelTopLeft()
		c = paddedTestConnection(label.OutsideBottomCenter, 10)
		assertPoint(t, "bottom text", c.GetLabelTopLeft(), unpadded.X, unpadded.Y+10)
	})

	t.Run("mask", func(t *testing.T) {
		t.Parallel()
		tl := geo.NewPoint(80, -10)
		// Unpadded labels keep the 2px side margins of earlier releases.
		assertBox(t, "p=0", paddedTestConnection(label.InsideMiddleCenter, 0).LabelMaskBox(tl), 78, -10, 44, 20)
		assertBox(t, "p=1", paddedTestConnection(label.InsideMiddleCenter, 1).LabelMaskBox(tl), 78, -11, 44, 22)
		assertBox(t, "p=12", paddedTestConnection(label.InsideMiddleCenter, 12).LabelMaskBox(tl), 68, -22, 64, 44)

		icon, _ := url.Parse("https://icons.terrastruct.com/essentials/004-picture.svg")
		c := paddedTestConnection(label.InsideMiddleCenter, 0)
		c.Icon = icon
		iconSpace := float64(CONNECTION_ICON_LABEL_GAP + DEFAULT_ICON_SIZE)
		assertBox(t, "p=0 icon", c.LabelMaskBox(tl), 78-iconSpace, -10, 44+iconSpace, 20)
		c.LabelPadding = 12
		assertBox(t, "p=12 icon", c.LabelMaskBox(tl), 68-iconSpace-12, -22, 64+iconSpace+12, 44)
	})

	t.Run("fill", func(t *testing.T) {
		t.Parallel()
		tl := geo.NewPoint(80, -10)
		assertBox(t, "p=0", paddedTestConnection(label.InsideMiddleCenter, 0).LabelFillBox(tl), 76, -13, 48, 26)
		assertBox(t, "p=3", paddedTestConnection(label.InsideMiddleCenter, 3).LabelFillBox(tl), 76, -13, 48, 26)
		assertBox(t, "p=5", paddedTestConnection(label.InsideMiddleCenter, 5).LabelFillBox(tl), 75, -15, 50, 30)
		assertBox(t, "p=12", paddedTestConnection(label.InsideMiddleCenter, 12).LabelFillBox(tl), 68, -22, 64, 44)
	})

	t.Run("icon sits left of the padded box", func(t *testing.T) {
		t.Parallel()
		icon, _ := url.Parse("https://icons.terrastruct.com/essentials/004-picture.svg")
		c := paddedTestConnection(label.InsideMiddleCenter, 0)
		c.Icon = icon
		unpadded := c.GetIconPosition()
		c.LabelPadding = 10
		assertPoint(t, "icon", c.GetIconPosition(), unpadded.X-10, unpadded.Y)
	})

	t.Run("explicit icon size with padding", func(t *testing.T) {
		t.Parallel()
		icon, _ := url.Parse("https://icons.terrastruct.com/essentials/004-picture.svg")
		c := paddedTestConnection(label.InsideMiddleCenter, 10)
		c.Icon = icon
		c.IconWidth, c.IconHeight = 48, 24
		text := c.GetLabelTopLeft()
		// The icon keeps its gap to the padded box and is centered on the text.
		assertPoint(t, "icon", c.GetIconPosition(),
			text.X-10-CONNECTION_ICON_LABEL_GAP-48, text.Y+float64(c.LabelHeight)/2-12)
		tl := geo.NewPoint(80, -10)
		iconSpace := float64(CONNECTION_ICON_LABEL_GAP + 48 + 10)
		assertBox(t, "mask", c.LabelMaskBox(tl), 70-iconSpace, -20, 60+iconSpace, 40)

		// On a short route the icon sticks out past the route's start.
		c.Route = []*geo.Point{geo.NewPoint(60, 0), geo.NewPoint(140, 0)}
		d := NewDiagram()
		a := BaseShape()
		a.ID = "a"
		a.Pos = Point{X: 100, Y: 50}
		a.Width, a.Height = 50, 40
		d.Shapes = []Shape{*a}
		d.Connections = []Connection{*c}
		bbTL, _ := d.BoundingBox()
		if want := int(math.Floor(c.GetIconPosition().X)); bbTL.X != want {
			t.Fatalf("bounding box left = %d, want the icon's left %d", bbTL.X, want)
		}
	})

	t.Run("bounding box covers the padded box", func(t *testing.T) {
		t.Parallel()
		d := NewDiagram()
		a := BaseShape()
		a.ID = "a"
		a.Pos = Point{X: -100, Y: -20}
		a.Width, a.Height = 50, 40
		d.Shapes = []Shape{*a}
		c := paddedTestConnection(label.OutsideTopCenter, 0)
		d.Connections = []Connection{*c}
		tl0, br0 := d.BoundingBox()

		d.Connections[0].LabelPadding = 10
		tl, br := d.BoundingBox()
		text := d.Connections[0].GetLabelTopLeft()
		if tl.Y != int(text.Y)-10 || tl.Y >= tl0.Y {
			t.Fatalf("bounding box top = %d, want %d (unpadded %d)", tl.Y, int(text.Y)-10, tl0.Y)
		}
		if br != br0 || tl.X != tl0.X {
			t.Fatalf("bounding box (%v, %v), want only its top to grow from (%v, %v)", tl, br, tl0, br0)
		}
	})
}

func TestValidateRenderTargetLabelPadding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		padding int
		wantErr bool
	}{{0, false}, {12, false}, {MAX_CONNECTION_LABEL_PADDING, false}, {-1, true}, {MAX_CONNECTION_LABEL_PADDING + 1, true}} {
		d := NewDiagram()
		a, b := BaseShape(), BaseShape()
		a.ID, b.ID = "a", "b"
		a.Width, a.Height, b.Width, b.Height = 10, 10, 10, 10
		b.Pos = Point{X: 200}
		d.Shapes = []Shape{*a, *b}
		c := paddedTestConnection(label.InsideMiddleCenter, tc.padding)
		c.Src, c.Dst = "a", "b"
		d.Connections = []Connection{*c}
		err := ValidateRenderTarget(d)
		if tc.wantErr != (err != nil) {
			t.Fatalf("labelPadding %d: ValidateRenderTarget() = %v, want error %v", tc.padding, err, tc.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "labelPadding") {
			t.Fatalf("labelPadding %d: error %q does not name the field", tc.padding, err)
		}
	}
}
