package d2target

import (
	"net/url"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

func iconTestShape(shapeType string, iconPosition string, iconWidth, iconHeight int) Shape {
	return Shape{
		ID:           "x",
		Type:         shapeType,
		Pos:          Point{X: 100, Y: 200},
		Width:        200,
		Height:       120,
		Icon:         &url.URL{Scheme: "https", Host: "example.com", Path: "/icon.svg"},
		IconPosition: iconPosition,
		IconWidth:    iconWidth,
		IconHeight:   iconHeight,
		Opacity:      1,
	}
}

func TestIconBoxAllPositions(t *testing.T) {
	t.Parallel()

	for _, shapeType := range []string{ShapeRectangle, ShapeCircle, ShapeCylinder, ShapeHexagon, ShapeCloud} {
		for position := label.OutsideTopLeft; position <= label.BorderBottomRight; position++ {
			positionName := position.String()
			for _, size := range [][2]int{{0, 0}, {48, 32}} {
				s := iconTestShape(shapeType, position.String(), size[0], size[1])
				box := geo.NewBox(geo.NewPoint(100, 200), 200, 120)
				geometry := shape.NewShape(DSL_SHAPE_TO_SHAPE_TYPE[shapeType], box)
				outer, inner := geometry.GetBox(), geometry.GetInnerBox()
				got := s.GetIconBox()
				if got == nil {
					t.Fatalf("%s %s: no icon box", shapeType, positionName)
				}

				if size[0] > 0 {
					if got.Width != float64(size[0]) || got.Height != float64(size[1]) {
						t.Fatalf("%s %s: icon box %vx%v, want explicit %vx%v", shapeType, positionName, got.Width, got.Height, size[0], size[1])
					}
				} else if got.Width != got.Height || got.Width > MAX_ICON_SIZE {
					t.Fatalf("%s %s: automatic icon box %vx%v", shapeType, positionName, got.Width, got.Height)
				}

				center := got.Center()
				switch {
				case position.IsBorder():
					// Border icons are centered on an edge of the outer box, like border labels.
					onEdge := center.Y == outer.TopLeft.Y || center.Y == outer.TopLeft.Y+outer.Height ||
						center.X == outer.TopLeft.X || center.X == outer.TopLeft.X+outer.Width
					if !onEdge {
						t.Fatalf("%s %s: icon center %v is not on an edge of %v", shapeType, positionName, center, outer)
					}
				case position.IsOutside():
					if overlaps(*got, *outer) {
						t.Fatalf("%s %s: outside icon %v overlaps the shape %v", shapeType, positionName, got, outer)
					}
				default:
					if !contains(*inner, *got) {
						t.Fatalf("%s %s: inside icon %v is not in the inner box %v", shapeType, positionName, got, inner)
					}
				}
			}
		}
	}
}

func overlaps(a, b geo.Box) bool {
	return a.TopLeft.X < b.TopLeft.X+b.Width && b.TopLeft.X < a.TopLeft.X+a.Width &&
		a.TopLeft.Y < b.TopLeft.Y+b.Height && b.TopLeft.Y < a.TopLeft.Y+a.Height
}

func contains(outer, inner geo.Box) bool {
	return inner.TopLeft.X >= outer.TopLeft.X && inner.TopLeft.Y >= outer.TopLeft.Y &&
		inner.TopLeft.X+inner.Width <= outer.TopLeft.X+outer.Width &&
		inner.TopLeft.Y+inner.Height <= outer.TopLeft.Y+outer.Height
}

func TestIconBoxBorderUsesOuterBox(t *testing.T) {
	t.Parallel()

	// A circle's inner box is the inscribed square; border icons must sit on
	// the circle's bounding box like border labels do.
	s := iconTestShape(ShapeCircle, label.BorderTopCenter.String(), 40, 40)
	s.Width, s.Height = 200, 200
	got := s.GetIconBox()
	if got.TopLeft.X != 180 || got.TopLeft.Y != 180 {
		t.Fatalf("border-top-center icon at %v, want (180, 180)", got.TopLeft)
	}

	// Cylinders draw their lid inside the inner box; the icon goes on the top.
	s = iconTestShape(ShapeCylinder, label.BorderTopCenter.String(), 0, 0)
	got = s.GetIconBox()
	if got.TopLeft.Y+got.Height/2 != float64(s.Pos.Y) {
		t.Fatalf("cylinder border-top-center icon %v is not centered on y=%d", got, s.Pos.Y)
	}
}

func TestIconBoxTablesUseOwnBox(t *testing.T) {
	t.Parallel()

	for _, shapeType := range []string{ShapeSQLTable, ShapeClass} {
		s := iconTestShape(shapeType, label.OutsideTopLeft.String(), 24, 24)
		got := s.GetIconBox()
		if got.TopLeft.X != 95 || got.TopLeft.Y != 171 || got.Width != 24 || got.Height != 24 {
			t.Fatalf("%s icon box %v, want 24x24 at (95, 171)", shapeType, got.ToString())
		}
	}
}

func TestIconBoxImageShape(t *testing.T) {
	t.Parallel()

	s := iconTestShape(ShapeImage, label.OutsideTopLeft.String(), 48, 48)
	if got := s.GetIconBox(); got != nil {
		t.Fatalf("image shape icon box %v, want nil", got)
	}
}

func TestBoundingBoxIncludesIcons(t *testing.T) {
	t.Parallel()

	newDiagram := func(shapes ...Shape) *Diagram {
		d := NewDiagram()
		d.Shapes = shapes
		return d
	}
	noIcon := iconTestShape(ShapeRectangle, "", 0, 0)
	noIcon.Icon = nil
	noIcon.StrokeWidth = 2
	tl, br := newDiagram(noIcon).BoundingBox()
	if tl != (Point{99, 199}) || br != (Point{301, 321}) {
		t.Fatalf("shape bounds %v %v", tl, br)
	}

	for _, tc := range []struct {
		name     string
		shape    Shape
		tl, br   Point
		position label.Position
	}{
		{
			name:  "border top left",
			shape: iconTestShape(ShapeRectangle, label.BorderTopLeft.String(), 40, 40),
			tl:    Point{99, 180}, br: Point{301, 321},
		},
		{
			name:  "border right middle",
			shape: iconTestShape(ShapeRectangle, label.BorderRightMiddle.String(), 40, 20),
			tl:    Point{99, 199}, br: Point{320, 321},
		},
		{
			name:  "outside top left",
			shape: iconTestShape(ShapeRectangle, label.OutsideTopLeft.String(), 0, 0),
			// automatic size 60, drawn at x - PADDING
			tl: Point{95, 135}, br: Point{301, 321},
		},
		{
			name:  "large inside icon",
			shape: iconTestShape(ShapeRectangle, label.InsideMiddleCenter.String(), 300, 30),
			tl:    Point{50, 199}, br: Point{350, 321},
		},
		{
			name:  "image icon is the shape",
			shape: iconTestShape(ShapeImage, label.OutsideBottomCenter.String(), 0, 0),
			tl:    Point{99, 199}, br: Point{301, 321},
		},
	} {
		tc.shape.StrokeWidth = 2
		tl, br := newDiagram(tc.shape).BoundingBox()
		if tl != tc.tl || br != tc.br {
			t.Errorf("%s: bounds %v %v, want %v %v", tc.name, tl, br, tc.tl, tc.br)
		}
	}
}

func TestConnectionIconSize(t *testing.T) {
	t.Parallel()

	c := Connection{
		Icon:         &url.URL{Scheme: "https", Host: "example.com"},
		IconPosition: label.InsideMiddleCenter.String(),
		Route:        []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(0, 200)},
		StrokeWidth:  2,
	}
	if w, h := c.IconSize(); w != DEFAULT_ICON_SIZE || h != DEFAULT_ICON_SIZE {
		t.Fatalf("default connection icon size %vx%v", w, h)
	}
	def := c.GetIconPosition()

	c.IconWidth, c.IconHeight = 48, 24
	if w, h := c.IconSize(); w != 48 || h != 24 {
		t.Fatalf("explicit connection icon size %vx%v", w, h)
	}
	got := c.GetIconPosition()
	// still centered on the same point of the route
	if got.X+24 != def.X+16 || got.Y+12 != def.Y+16 {
		t.Fatalf("explicit icon at %v, default at %v", got, def)
	}

	// next to a label the icon keeps its gap and is centered vertically
	c.Label = "hello"
	c.LabelWidth, c.LabelHeight = 40, 20
	c.LabelPosition = label.InsideMiddleCenter.String()
	labelTL := c.GetLabelTopLeft()
	got = c.GetIconPosition()
	if got.X != labelTL.X-CONNECTION_ICON_LABEL_GAP-48 || got.Y != labelTL.Y+10-12 {
		t.Fatalf("icon next to label at %v, label at %v", got, labelTL)
	}
}

func TestValidateRenderTargetIconSize(t *testing.T) {
	t.Parallel()

	valid := iconTestShape(ShapeRectangle, label.InsideMiddleCenter.String(), 48, 32)
	d := NewDiagram()
	d.Shapes = []Shape{valid}
	if err := ValidateRenderTarget(d); err != nil {
		t.Fatalf("valid icon size rejected: %v", err)
	}

	invalid := valid
	invalid.IconHeight = -1
	d.Shapes = []Shape{invalid}
	if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), "iconHeight") {
		t.Fatalf("negative shape icon height: %v", err)
	}

	d.Shapes = nil
	d.Connections = []Connection{{
		Route:     []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(10, 10)},
		Opacity:   1,
		IconWidth: -2,
	}}
	if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), "iconWidth") {
		t.Fatalf("negative connection icon width: %v", err)
	}
}

func TestBoundingBoxIncludesConnectionIcons(t *testing.T) {
	t.Parallel()

	d := NewDiagram()
	d.Connections = []Connection{{
		Route:        []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(200, 0)},
		StrokeWidth:  2,
		Opacity:      1,
		Icon:         &url.URL{Scheme: "https", Host: "example.com"},
		IconPosition: label.InsideMiddleCenter.String(),
		IconWidth:    96,
		IconHeight:   96,
	}}
	// Shapes are needed for a non-empty bounding box.
	s := iconTestShape(ShapeRectangle, "", 0, 0)
	s.Icon = nil
	s.Pos, s.Width, s.Height = Point{X: 0, Y: 100}, 10, 10
	d.Shapes = []Shape{s}
	tl, br := d.BoundingBox()
	if tl.Y != -48 || br.X != 200+1 || tl.X != 0-1 {
		t.Fatalf("bounds %v %v, want the 96 px connection icon (y from -48) included", tl, br)
	}
}
