//go:build js && wasm

package d2wasm

import (
	"bytes"
	"encoding/json"
	"image"
	stdpng "image/png"
	"regexp"
	"strings"
	"testing"

	"github.com/d2lang/util-go/go2"

	d2png "github.com/d2lang/d2/lib/png"
	"github.com/d2lang/d2/lib/sourcemeta"
)

var embeddedMetadataRE = regexp.MustCompile(`(?s)<metadata><d2-source .*?</metadata>`)

func compileRequestForTest(t *testing.T, request CompileRequest) CompileResponse {
	t.Helper()
	compiled, err := Compile(wasmJSONArgument(t, request))
	if err != nil {
		t.Fatal(err)
	}
	return compiled.(CompileResponse)
}

func renderForTest(t *testing.T, result CompileResponse) []byte {
	t.Helper()
	out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &result.RenderOptions}))
	if err != nil {
		t.Fatal(err)
	}
	return out.([]byte)
}

func TestCompileEmbedSourceRoundTrip(t *testing.T) {
	fs := map[string]string{
		"index":        "# kept\n...@lib/style\nx->y: \"a < b & c\" {class: hot}\n",
		"lib/style.d2": "classes: {hot: {style.fill: red}}\n",
		"unused.d2":    "z",
	}
	result := compileRequestForTest(t, CompileRequest{FS: fs, Opts: &CompileOptions{
		RenderOptions: RenderOptions{ThemeID: go2.Pointer(int64(200)), Sketch: go2.Pointer(true)},
		Layout:        go2.Pointer("elk"),
		EmbedSource:   go2.Pointer(true),
	}})
	source := result.RenderOptions.Source
	if source == nil {
		t.Fatal("no source in renderOptions")
	}
	// The original text, not the formatted one compile returns in fs.
	if source.Entry.Path != "index" || source.Entry.Content != fs["index"] || result.FS["index"] == fs["index"] {
		t.Fatalf("entry = %#v, formatted = %q", source.Entry, result.FS["index"])
	}
	if len(source.Imports) != 1 || source.Imports[0].Path != "lib/style.d2" {
		t.Fatalf("imports = %#v", source.Imports)
	}
	if *source.Settings.Layout != "elk" || *source.Settings.ThemeID != 200 || !*source.Settings.Sketch || source.Settings.Pad != nil {
		t.Fatalf("settings = %#v", source.Settings)
	}

	svg := renderForTest(t, result)
	if !bytes.Contains(svg, []byte(`<metadata><d2-source xmlns="https://d2lang.com/ns/source" format="1" path="index" d2-version="`)) ||
		!bytes.Contains(svg, []byte(`layout="elk" theme-id="200" sketch="true">`)) {
		t.Fatalf("missing metadata:\n%.600s", svg)
	}

	extracted, err := ExtractSource(wasmJSONArgument(t, ExtractSourceRequest{Data: svg}))
	if err != nil {
		t.Fatal(err)
	}
	response := extracted.(*ExtractSourceResponse)
	if response.InputPath != "index" || response.FS["lib/style.d2"] != fs["lib/style.d2"] || *response.Options.Layout != "elk" || !*response.Options.EmbedSource {
		t.Fatalf("extracted = %#v", response)
	}
	// What extractSource returns is a compile request.
	var opts CompileOptions
	encoded, err := json.Marshal(response.Options)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"layout":"elk","themeID":200,"sketch":true,"embedSource":true}` {
		t.Fatalf("options = %s", encoded)
	}
	if err := json.Unmarshal(encoded, &opts); err != nil {
		t.Fatal(err)
	}
	again := compileRequestForTest(t, CompileRequest{FS: response.FS, InputPath: &response.InputPath, Opts: &opts})
	svg2 := renderForTest(t, again)
	if embeddedMetadataRE.ReplaceAllString(string(svg), "") != embeddedMetadataRE.ReplaceAllString(string(svg2), "") {
		t.Fatal("re-rendering the extracted source changed the diagram")
	}
	if !bytes.Equal(svg, svg2) {
		t.Fatalf("re-embedded source differs:\n%s\n%s", embeddedMetadataRE.Find(svg), embeddedMetadataRE.Find(svg2))
	}
}

func TestCompileWithoutEmbedSourceIsUnchanged(t *testing.T) {
	result := compileRequestForTest(t, CompileRequest{FS: map[string]string{"index": "a -> b"}, Opts: &CompileOptions{}})
	encoded, err := json.Marshal(result.RenderOptions)
	if err != nil {
		t.Fatal(err)
	}
	if result.RenderOptions.Source != nil || bytes.Contains(encoded, []byte(`"source"`)) {
		t.Fatalf("render options = %s", encoded)
	}
	if bytes.Contains(renderForTest(t, result), []byte("<metadata>")) {
		t.Fatal("embedded without embedSource")
	}
}

func TestRenderEmbedSourceTargets(t *testing.T) {
	result := compileRequestForTest(t, CompileRequest{
		FS:   map[string]string{"index": "a\nlayers: {x: {b}}\nsteps: {1: {c}}\n"},
		Opts: &CompileOptions{EmbedSource: go2.Pointer(true)},
	})
	for _, test := range []struct {
		target   *string
		animate  *int64
		want     string
		wantNone bool
	}{
		{want: ""},
		{target: go2.Pointer("layers.x"), want: "layers.x"},
		{target: go2.Pointer("*"), animate: go2.Pointer(int64(500)), wantNone: true},
		{target: go2.Pointer("layers.x.*"), animate: go2.Pointer(int64(500)), want: "layers.x.*"},
	} {
		opts := result.RenderOptions
		opts.Target, opts.AnimateInterval = test.target, test.animate
		out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &opts}))
		if err != nil {
			t.Fatal(err)
		}
		b, err := sourcemeta.ExtractSVG(out.([]byte))
		if err != nil || b == nil {
			t.Fatalf("extract = %v, %v", b, err)
		}
		if test.wantNone != (b.Target == nil) || b.Target != nil && *b.Target != test.want {
			t.Fatalf("target %v: embedded %v, want %q", test.target, b.Target, test.want)
		}
	}
	ascii := result.RenderOptions
	ascii.ASCII = go2.Pointer(true)
	out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &ascii}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out.([]byte)), "d2-source") {
		t.Fatal("ASCII output embeds the source")
	}
}

func TestEmbedSourcePNGAndSVG(t *testing.T) {
	var buf bytes.Buffer
	if err := stdpng.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	// A Boxwire PNG export, re-embedded in D2's format.
	png, err := d2png.AddText(buf.Bytes(), []d2png.TextEntry{{Keyword: "d2-source", Text: "old"}, {Keyword: "boxwire-settings", Text: `{"layout":"tala"}`}})
	if err != nil {
		t.Fatal(err)
	}
	source := &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "index", Content: "a -> b"}, Settings: sourcemeta.Settings{Layout: go2.Pointer("elk")}}
	out, err := EmbedSource(wasmJSONArgument(t, EmbedSourceRequest{Data: png, Source: source}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdpng.Decode(bytes.NewReader(out.([]byte))); err != nil {
		t.Fatal(err)
	}
	extracted, err := ExtractSource(wasmJSONArgument(t, ExtractSourceRequest{Data: out.([]byte)}))
	if err != nil {
		t.Fatal(err)
	}
	response := extracted.(*ExtractSourceResponse)
	if response.FS["index"] != "a -> b" || *response.Options.Layout != "elk" {
		t.Fatalf("extracted = %#v", response)
	}

	svg, err := EmbedSource(wasmJSONArgument(t, EmbedSourceRequest{Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`), Source: source}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(svg.([]byte)), `<svg xmlns="http://www.w3.org/2000/svg"><metadata><d2-source`) {
		t.Fatalf("svg = %s", svg)
	}

	none, err := ExtractSource(wasmJSONArgument(t, ExtractSourceRequest{Data: buf.Bytes()}))
	if err != nil || none != nil {
		t.Fatalf("plain PNG = %v, %v", none, err)
	}
	if _, err := EmbedSource(wasmJSONArgument(t, EmbedSourceRequest{Data: []byte("not an image"), Source: source})); err == nil {
		t.Fatal("embedding in a non-image succeeded")
	}
}
