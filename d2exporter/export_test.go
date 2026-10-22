package d2exporter_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	tassert "github.com/stretchr/testify/assert"

	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/diff"
	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

type testCase struct {
	name string
	dsl  string

	assertions func(t *testing.T, d *d2target.Diagram)
}

func TestExport(t *testing.T) {
	t.Parallel()

	t.Run("shape", testShape)
	t.Run("connection", testConnection)
	t.Run("label", testLabel)
	t.Run("theme", testTheme)
	t.Run("legend", testLegend)
}

func testShape(t *testing.T) {
	tcs := []testCase{
		{
			name: "basic",
			dsl: `x
`,
		},
		{
			name: "synonyms",
			dsl: `x: {shape: circle}
y: {shape: square}
`,
		},
		{
			name: "text_color",
			dsl: `x: |md yo | { style.font-color: red }
`,
		},
		{
			name: "border-radius",
			dsl: `Square: "" { style.border-radius: 5 }
`,
		},
		{
			name: "image_dimensions",

			dsl: `hey: "" {
  icon: https://icons.terrastruct.com/essentials/004-picture.svg
  shape: image
	width: 200
	height: 230
}
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				if d.Shapes[0].Width != 200 {
					t.Fatalf("expected width 200, got %v", d.Shapes[0].Width)
				}
				if d.Shapes[0].Height != 230 {
					t.Fatalf("expected height 230, got %v", d.Shapes[0].Height)
				}
			},
		},
		{
			name: "icon_size",

			dsl: `a: {icon: https://icons.terrastruct.com/essentials/004-picture.svg; icon.width: 48}
b: {icon: https://icons.terrastruct.com/essentials/004-picture.svg {near: border-top-left; width: 40; height: 24}}
c: {icon: https://icons.terrastruct.com/essentials/004-picture.svg}
img: {shape: image; icon: https://icons.terrastruct.com/essentials/004-picture.svg; icon.width: 48}
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				byID := map[string]d2target.Shape{}
				for _, s := range d.Shapes {
					byID[s.ID] = s
				}
				tassert.Equal(t, 48, byID["a"].IconWidth)
				tassert.Equal(t, 48, byID["a"].IconHeight)
				tassert.Equal(t, 40, byID["b"].IconWidth)
				tassert.Equal(t, 24, byID["b"].IconHeight)
				tassert.Equal(t, "BORDER_TOP_LEFT", byID["b"].IconPosition)
				tassert.Equal(t, 0, byID["c"].IconWidth)
				// the icon of an image is the shape itself
				tassert.Equal(t, 0, byID["img"].IconWidth)
			},
		},
		{
			name: "sequence_group_position",

			dsl: `hey {
  shape: sequence_diagram
	a
	b
  group: {
    a -> b
  }
}
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				tassert.Equal(t, "hey.group", d.Shapes[3].ID)
				tassert.Equal(t, "INSIDE_TOP_LEFT", d.Shapes[3].LabelPosition)
			},
		},
	}

	runa(t, tcs)
}

func testConnection(t *testing.T) {
	tcs := []testCase{
		{
			name: "basic",
			dsl: `x -> y
`,
		},
		{
			name: "icon_size",
			dsl: `x -> y: {icon: https://icons.terrastruct.com/essentials/004-picture.svg; icon.width: 48; icon.height: 24}
x -> y: {icon: https://icons.terrastruct.com/essentials/004-picture.svg}
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				tassert.Equal(t, 48, d.Connections[0].IconWidth)
				tassert.Equal(t, 24, d.Connections[0].IconHeight)
				tassert.Equal(t, 0, d.Connections[1].IconWidth)
				tassert.Equal(t, 0, d.Connections[1].IconHeight)
			},
		},
		{
			name: "stroke-dash",
			dsl: `x -> y: { style.stroke-dash: 4 }
`,
		},
		{
			name: "arrowhead",
			dsl: `x -> y: {
  source-arrowhead: If you've done six impossible things before breakfast, why not round it
  target-arrowhead: {
    label: A man with one watch knows what time it is.
    shape: diamond
    style.filled: true
  }
}
`,
		},
		{
			// This is a regression test where a connection with stroke-dash of 0 on terrastruct flagship theme would have a diff color
			// than a connection without stroke dash
			name: "theme_stroke-dash",
			dsl: `x -> y: { style.stroke-dash: 0 }
x -> y
`,
		},
	}

	runa(t, tcs)
}

func testLabel(t *testing.T) {
	tcs := []testCase{
		{
			name: "basic_shape",
			dsl: `x: yo
`,
		},
		{
			name: "shape_font_color",
			dsl: `x: yo { style.font-color: red }
`,
		},
		{
			name: "connection_font_color",
			dsl: `x -> y: yo { style.font-color: red }
`,
		},
		{
			name: "theme_color_codes",
			dsl: `x: yo {
  style.fill: N1
  style.stroke: B2
  style.font-color: AA4
}
plain
x -> plain: hi {
  style.stroke: AB5
  style.font-color: AB4
}
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				x := shapeByID(t, d, "x")
				tassert.Equal(t, "N1", x.Fill)
				tassert.Equal(t, "B2", x.Stroke)
				tassert.Equal(t, "AA4", x.Color)
				tassert.Equal(t, "AA4", x.GetFontColor())
				tassert.True(t, x.ExplicitThemeFill)
				tassert.True(t, x.ExplicitThemeStroke)
				tassert.True(t, x.ExplicitThemeFontColor)

				// Theme defaults are theme color codes too, but not explicit.
				plain := shapeByID(t, d, "plain")
				tassert.Equal(t, "B6", plain.Fill)
				tassert.Equal(t, "B1", plain.Stroke)
				tassert.False(t, plain.ExplicitThemeFill)
				tassert.False(t, plain.ExplicitThemeStroke)
				tassert.False(t, plain.ExplicitThemeFontColor)

				tassert.Equal(t, 1, len(d.Connections))
				tassert.Equal(t, "AB5", d.Connections[0].Stroke)
				tassert.Equal(t, "AB4", d.Connections[0].GetFontColor())
				tassert.True(t, d.Connections[0].ExplicitThemeStroke)
				tassert.True(t, d.Connections[0].ExplicitThemeFontColor)
			},
		},
		{
			name: "root_theme_color_codes",
			dsl: `style.fill: AA2
style.stroke: B1
style.font-color: N7
x
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				tassert.Equal(t, "AA2", d.Root.Fill)
				tassert.True(t, d.Root.ExplicitThemeFill)
				tassert.True(t, d.Root.ExplicitThemeStroke)
				tassert.True(t, d.Root.ExplicitThemeFontColor)
			},
		},
		{
			name: "root_default_colors_not_explicit",
			dsl: `x
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				tassert.False(t, d.Root.ExplicitThemeFill)
				tassert.False(t, d.Root.ExplicitThemeStroke)
				tassert.False(t, d.Root.ExplicitThemeFontColor)
			},
		},
		{
			name: "special_shape_theme_font_color",
			dsl: `table: {
  shape: sql_table
  id: int
  style.stroke: B1
  style.font-color: N7
}
person: {
  shape: class
  field: int
  style.font-color: B2
}
hex: {
  shape: sql_table
  id: int
  style.font-color: "#ff0000"
}
default: {
  shape: sql_table
  id: int
  style.stroke: B1
}
`,
			assertions: func(t *testing.T, d *d2target.Diagram) {
				table := shapeByID(t, d, "table")
				tassert.True(t, table.ExplicitThemeFontColor)
				tassert.Equal(t, "N7", table.GetFontColor())

				person := shapeByID(t, d, "person")
				tassert.True(t, person.ExplicitThemeFontColor)
				tassert.Equal(t, "B2", person.GetFontColor())

				hex := shapeByID(t, d, "hex")
				tassert.False(t, hex.ExplicitThemeFontColor)
				tassert.Equal(t, "#ff0000", hex.GetFontColor())

				// Without a font color, the header text uses the stroke, as before.
				def := shapeByID(t, d, "default")
				tassert.False(t, def.ExplicitThemeFontColor)
				tassert.Equal(t, "N1", def.Color)
				tassert.Equal(t, "B1", def.GetFontColor())
			},
		},
	}

	runa(t, tcs)
}

func shapeByID(t *testing.T, d *d2target.Diagram, id string) d2target.Shape {
	t.Helper()
	for _, s := range d.Shapes {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("shape %q not found", id)
	return d2target.Shape{}
}

func testTheme(t *testing.T) {
	tcs := []testCase{
		{
			name: "shape_without_bold",
			dsl: `x: {
	style.bold: false
}
`,
		},
		{
			name: "shape_with_italic",
			dsl: `x: {
	style.italic: true
}
`,
		},
		{
			name: "connection_without_italic",
			dsl: `x -> y: asdf { style.italic: false }
`,
		},
		{
			name: "connection_with_italic",
			dsl: `x -> y: asdf {
  style.italic: true
}
`,
		},
		{
			name: "connection_with_bold",
			dsl: `x -> y: asdf {
  style.bold: true
}
`,
		},
	}

	runa(t, tcs)
}

func testLegend(t *testing.T) {
	tcs := []testCase{
		{
			name: "basic_legend",
			dsl: `vars: {
  d2-legend: {
    legend: {
      l1: Rectangles {shape: rectangle}
      l2: Ovals {shape: oval}
      l1 -> l2: Connection
		}
	}
}
x: {shape: rectangle}
y: {shape: oval}
x -> y: connects

`,
		},
	}

	runa(t, tcs)
}

func runa(t *testing.T, tcs []testCase) {
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run(t, tc)
		})
	}
}

func run(t *testing.T, tc testCase) {
	ctx := context.Background()
	ctx = log.With(ctx, testlog.New(t))
	ctx = log.Leveled(ctx, slog.LevelDebug)

	g, config, err := d2compiler.Compile("", strings.NewReader(tc.dsl), &d2compiler.CompileOptions{
		UTF16Pos: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ruler, err := textmeasure.NewRuler()
	assert.JSON(t, nil, err)

	err = g.SetDimensions(nil, ruler, nil, nil)
	assert.JSON(t, nil, err)

	graphInfo := d2layouts.NestedGraphInfo(g.Root)
	err = d2layouts.LayoutNested(ctx, g, graphInfo, d2dagrelayout.DefaultLayout, d2layouts.DefaultRouter)
	if err != nil {
		t.Fatal(err)
	}

	got, err := d2exporter.Export(ctx, g, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		got.Config = config
	}

	if tc.assertions != nil {
		t.Run("assertions", func(t *testing.T) {
			tc.assertions(t, got)
		})
	}

	// This test is agnostic of layout changes
	for i := range got.Shapes {
		got.Shapes[i].Pos = d2target.Point{}
		got.Shapes[i].Width = 0
		got.Shapes[i].Height = 0
		got.Shapes[i].LabelWidth = 0
		got.Shapes[i].LabelHeight = 0
		got.Shapes[i].LabelPosition = ""
	}
	for i := range got.Connections {
		got.Connections[i].Route = []*geo.Point{}
		got.Connections[i].LabelWidth = 0
		got.Connections[i].LabelHeight = 0
		got.Connections[i].LabelPosition = ""
	}

	err = diff.TestdataJSON(filepath.Join("..", "testdata", "d2exporter", t.Name()), got)
	assert.Success(t, err)
}

// TestHashID tests that 2 diagrams with different theme configs do not equal each other
func TestHashID(t *testing.T) {
	ctx := context.Background()
	ctx = log.With(ctx, testlog.New(t))
	ctx = log.Leveled(ctx, slog.LevelDebug)

	aString := `
vars: {
  d2-config: {
    theme-id: 3
  }
}
a -> b
`

	bString := `
vars: {
  d2-config: {
    theme-id: 4
  }
}
a -> b
`

	da, err := compile(ctx, aString)
	assert.JSON(t, nil, err)

	db, err := compile(ctx, bString)
	assert.JSON(t, nil, err)

	hashA, err := da.HashID(nil)
	assert.JSON(t, nil, err)

	hashB, err := db.HashID(nil)
	assert.JSON(t, nil, err)

	assert.NotEqual(t, hashA, hashB)
}

func layoutResolver(engine string) (d2graph.LayoutGraph, error) {
	return d2dagrelayout.DefaultLayout, nil
}

func compile(ctx context.Context, d2 string) (*d2target.Diagram, error) {
	ruler, _ := textmeasure.NewRuler()
	opts := &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: layoutResolver,
		Layout:         go2.Pointer("dagre"),
	}
	d, _, e := d2lib.Compile(ctx, d2, opts, nil)
	return d, e
}

// TestExportLabelPadding checks that layouts get the padded label box while
// the diagram keeps the text size and the padding apart.
func TestExportLabelPadding(t *testing.T) {
	t.Parallel()
	ctx := log.With(context.Background(), testlog.New(t))

	g, _, err := d2compiler.Compile("", strings.NewReader(`a -> b: hello {label.padding: 9}
c -> d: hello
e -> f: "" {label.padding: 9}
`), nil)
	assert.Success(t, err)
	ruler, err := textmeasure.NewRuler()
	assert.Success(t, err)
	assert.Success(t, g.SetDimensions(nil, ruler, nil, nil))
	text := g.Edges[1].LabelDimensions
	tassert.Equal(t, text.Width+18, g.Edges[0].LabelDimensions.Width)
	tassert.Equal(t, text.Height+18, g.Edges[0].LabelDimensions.Height)
	// SetDimensions measures the text again rather than growing the box twice.
	assert.Success(t, g.SetDimensions(nil, ruler, nil, nil))
	tassert.Equal(t, text.Width+18, g.Edges[0].LabelDimensions.Width)
	tassert.Equal(t, text, g.Edges[1].LabelDimensions)
	tassert.Equal(t, 0, g.Edges[2].LabelDimensions.Width)

	graphInfo := d2layouts.NestedGraphInfo(g.Root)
	assert.Success(t, d2layouts.LayoutNested(ctx, g, graphInfo, d2dagrelayout.DefaultLayout, d2layouts.DefaultRouter))
	d, err := d2exporter.Export(ctx, g, nil, nil)
	assert.Success(t, err)

	padded, plain, empty := d.Connections[0], d.Connections[1], d.Connections[2]
	tassert.Equal(t, 9, padded.LabelPadding)
	tassert.Equal(t, text.Width, padded.LabelWidth)
	tassert.Equal(t, text.Height, padded.LabelHeight)
	tassert.Equal(t, 0, plain.LabelPadding)
	tassert.Equal(t, text.Width, plain.LabelWidth)
	tassert.Equal(t, 0, empty.LabelPadding)
	box := padded.LabelBox()
	tassert.Equal(t, float64(text.Width+18), box.Width)
	tassert.Equal(t, float64(text.Height+18), box.Height)
}
