package d2svg_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"regexp"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
)

// crossing draws b -> h straight down and d -> f straight across a 3x3 grid:
// they cross over e.
const crossing = `grid-columns: 3
a; b; c; d; e; f; g; h; i
`

func compileGaps(t *testing.T, script string) *d2target.Diagram {
	t.Helper()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), script, &d2lib.CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return diagram
}

func renderGaps(t *testing.T, diagram *d2target.Diagram, opts *d2svg.RenderOpts) string {
	t.Helper()
	out, err := d2svg.Render(diagram, opts)
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err := xml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid SVG: %v", err)
	}
	return string(out)
}

// connectionGroup is the markup of a connection's group.
func connectionGroup(t *testing.T, out, id string) string {
	t.Helper()
	open := `<g class="` + base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(id)))
	start := strings.Index(out, open)
	if start < 0 {
		t.Fatalf("connection %s not found", id)
	}
	end := strings.Index(out[start:], "</g>")
	return out[start : start+end]
}

func hashOf(t *testing.T, diagram *d2target.Diagram) string {
	t.Helper()
	hash, err := diagram.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestIntersectionGaps(t *testing.T) {
	t.Parallel()
	diagram := compileGaps(t, crossing+"b -> h\nd -> f\n")
	out := renderGaps(t, diagram, &d2svg.RenderOpts{IntersectionGaps: go2.Pointer(true)})
	hash := hashOf(t, diagram)

	mask := regexp.MustCompile(`<mask id="` + hash + `-gap-0" maskUnits="userSpaceOnUse" x="[-0-9.]+" y="[-0-9.]+" width="[0-9.]+" height="[0-9.]+"><g mask="url\(#` + hash + `\)"><rect [^>]*fill="white" /></g><path d="M [^"]+" fill="none" stroke="black" stroke-width="9" stroke-linecap="butt" /></mask>`)
	if !mask.MatchString(out) {
		t.Fatalf("gap mask missing or malformed:\n%s", out[strings.Index(out, "<mask"):])
	}
	if n := strings.Count(out, "-gap-"); n != 2 {
		t.Fatalf("got %d gap mask mentions, want one mask used once", n)
	}
	// The connection drawn first gets the gap; the one drawn over it stays whole.
	if lower := connectionGroup(t, out, "(b -> h)[0]"); !strings.Contains(lower, `mask="url(#`+hash+`-gap-0)"`) {
		t.Fatalf("lower connection doesn't use the gap mask: %s", lower)
	}
	if upper := connectionGroup(t, out, "(d -> f)[0]"); !strings.Contains(upper, `mask="url(#`+hash+`)"`) {
		t.Fatalf("upper connection doesn't use the shared mask: %s", upper)
	}
	// The shared mask, with its label holes, is still there.
	if !strings.Contains(out, `<mask id="`+hash+`" maskUnits="userSpaceOnUse"`) {
		t.Fatal("shared connection mask is missing")
	}
}

func TestIntersectionGapsOffChangesNothing(t *testing.T) {
	t.Parallel()
	diagram := compileGaps(t, crossing+"b -> h\nd -> f\n")
	want := renderGaps(t, diagram, nil)
	if strings.Contains(want, "-gap-") {
		t.Fatal("gaps without the option")
	}
	if got := renderGaps(t, diagram, &d2svg.RenderOpts{IntersectionGaps: go2.Pointer(false)}); got != want {
		t.Fatal("disabled gaps change the SVG")
	}
	// Nothing crosses: the option changes nothing either.
	plain := compileGaps(t, "a -> b -> c\na -> c")
	if !bytes.Equal([]byte(renderGaps(t, plain, nil)), []byte(renderGaps(t, plain, &d2svg.RenderOpts{IntersectionGaps: go2.Pointer(true)}))) {
		t.Fatal("gaps without crossings change the SVG")
	}
}

func TestIntersectionGapsSketchAndAnimated(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script string
		sketch       bool
		// How many paths draw the lower connection's line.
		paths int
	}{
		// An animated connection with arrowheads at both ends or none is drawn
		// in two halves.
		{name: "animated both ways", script: crossing + "b -- h: {style.animated: true}\nd -> f\n", paths: 2},
		{name: "animated one way", script: crossing + "b -> h: {style.animated: true}\nd -> f\n", paths: 1},
		{name: "sketch", script: crossing + "b -> h\nd -> f\n", sketch: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diagram := compileGaps(t, tc.script)
			out := renderGaps(t, diagram, &d2svg.RenderOpts{IntersectionGaps: go2.Pointer(true), Sketch: go2.Pointer(tc.sketch)})
			hash := hashOf(t, diagram)
			id := "(b -> h)[0]"
			if strings.Contains(tc.script, "b -- h") {
				id = "(b -- h)[0]"
			}
			lower := connectionGroup(t, out, id)
			gapped := strings.Count(lower, `mask="url(#`+hash+`-gap-0)"`)
			if strings.Contains(lower, `mask="url(#`+hash+`)"`) || gapped == 0 || tc.paths > 0 && gapped != tc.paths {
				t.Fatalf("lower connection's paths use the gap mask %d times: %s", gapped, lower)
			}
			width := `stroke-width="9"`
			if tc.sketch {
				width = `stroke-width="11"`
			}
			if !strings.Contains(out, width+` stroke-linecap="butt"`) {
				t.Fatalf("gap is not %s", width)
			}
		})
	}
}

func TestIntersectionGapsMultiboard(t *testing.T) {
	t.Parallel()
	diagram := compileGaps(t, crossing+"b -> h\nd -> f\nlayers: {\n  x: {\n"+crossing+"b -> h\nd -> f\nd -> h\n  }\n}\n")
	masterID := hashOf(t, diagram)
	boards, err := d2svg.RenderMultiboard(diagram, &d2svg.RenderOpts{IntersectionGaps: go2.Pointer(true), MasterID: masterID})
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 2 {
		t.Fatalf("got %d boards", len(boards))
	}
	ids := map[string]bool{}
	for _, board := range boards {
		found := regexp.MustCompile(`<mask id="([^"]+-gap-[0-9]+)"`).FindAllStringSubmatch(string(board), -1)
		if len(found) == 0 {
			t.Fatal("board without gaps")
		}
		for _, m := range found {
			if ids[m[1]] {
				t.Fatalf("gap mask id %s repeats across boards", m[1])
			}
			ids[m[1]] = true
			if !strings.Contains(string(board), `mask="url(#`+m[1]+`)"`) {
				t.Fatalf("gap mask %s isn't used on its board", m[1])
			}
		}
	}
}
