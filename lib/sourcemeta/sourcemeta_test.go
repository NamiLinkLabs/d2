package sourcemeta_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	stdpng "image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/memfs"
	d2png "github.com/d2lang/d2/lib/png"
	"github.com/d2lang/d2/lib/sourcemeta"
)

func ptr[T any](v T) *T { return &v }

const plainSVG = `<?xml version="1.0" encoding="utf-8"?><svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" data-d2-version="v0.7.0" preserveAspectRatio="xMinYMin meet" viewBox="0 0 10 10"><svg class="d2-1 d2-svg" width="10" height="10" viewBox="0 0 10 10"><style type="text/css"><![CDATA[.a{fill:red}]]></style><foreignObject><div xmlns="http://www.w3.org/1999/xhtml"><p>a&nbsp;b<br>c &lt;img width="10"/&gt;</p></div></foreignObject><rect width="10" height="10"/></svg></svg>`

func sample() *sourcemeta.Bundle {
	return &sourcemeta.Bundle{
		Entry: sourcemeta.File{Path: "main.d2", Content: "# comment\n...@lib/x\nx -> y: \"a < b & c > d\" {class: hot}\nz: |md\n  <img width=\"10\"/> **hi** ]]> done\n|\r\nemoji: 🧭 𝄞\ttab\n"},
		Imports: []sourcemeta.File{
			{Path: "lib/x.d2", Content: "label: \"X <&> ]]> y\"\n"},
			{Path: "shared/s.d2", Content: "classes: {hot: {style.fill: \"#f88\"}}\n"},
		},
		Settings: sourcemeta.Settings{Layout: ptr("elk"), ThemeID: ptr[int64](200), DarkThemeID: ptr[int64](201), Sketch: ptr(true), Pad: ptr[int64](0), Center: ptr(false), Isometric: ptr(false), AnimateInterval: ptr[int64](1200)},
		Target:   ptr("layers.x"),
		Version:  "v0.7.0",
	}
}

func TestSVGRoundTrip(t *testing.T) {
	b := sample()
	out, err := sourcemeta.EmbedSVG([]byte(plainSVG), b)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `viewBox="0 0 10 10"><metadata><d2-source xmlns="https://d2lang.com/ns/source" format="1" path="main.d2" d2-version="v0.7.0" layout="elk" theme-id="200" dark-theme-id="201" sketch="true" pad="0" center="false" isometric="false" animate-interval="1200" target="layers.x">`) {
		t.Fatalf("unexpected metadata start:\n%s", s)
	}
	// The source stays readable.
	if !strings.Contains(s, "x -> y: \"a &lt; b &amp; c > d\"") || !strings.Contains(s, "]]&gt; done") || !strings.Contains(s, "|&#13;\n") {
		t.Fatalf("unexpected escaping:\n%s", s)
	}
	if !strings.HasSuffix(s, plainSVG[strings.Index(plainSVG, `<svg class`):]) {
		t.Fatal("the rest of the SVG changed")
	}
	got, err := sourcemeta.ExtractSVG(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, b) {
		t.Fatalf("round trip:\n got %#v\nwant %#v", got, b)
	}
}

func TestSVGBase64Fallback(t *testing.T) {
	for name, content := range map[string]string{
		"control":      "a -> b\x01\n",
		"invalid utf8": "a -> b: \xff\xfe\n",
		"utf16":        "\xff\xfea\x00 \x00-\x00>\x00 \x00b\x00",
		"nonchar":      "a: ￿",
	} {
		t.Run(name, func(t *testing.T) {
			b := &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "main.d2", Content: content}}
			out, err := sourcemeta.EmbedSVG([]byte(plainSVG), b)
			if err != nil {
				t.Fatal(err)
			}
			want := `encoding="base64">` + base64.StdEncoding.EncodeToString([]byte(content)) + `</d2-source>`
			if !bytes.Contains(out, []byte(want)) {
				t.Fatalf("missing %s in\n%s", want, out)
			}
			got, err := sourcemeta.ExtractSVG(out)
			if err != nil {
				t.Fatal(err)
			}
			if got.Entry.Content != content {
				t.Fatalf("content = %q, want %q", got.Entry.Content, content)
			}
		})
	}
}

func TestEmbedSVGPrologVariants(t *testing.T) {
	b := &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "a.d2", Content: "a"}}
	for name, svg := range map[string]string{
		"no prolog":       `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect/></svg>`,
		"prolog":          `<?xml version="1.0"?>` + "\n" + `<!-- c --><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`,
		"quoted >":        `<svg xmlns="http://www.w3.org/2000/svg" data-x="a>b" data-y='c>d'><rect/></svg>`,
		"doctype and bom": "\ufeff" + `<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd"><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`,
	} {
		t.Run(name, func(t *testing.T) {
			out, err := sourcemeta.EmbedSVG([]byte(svg), b)
			if err != nil {
				t.Fatal(err)
			}
			i := strings.Index(svg, "<rect/>")
			if string(out[:i]) != svg[:i] || !bytes.HasPrefix(out[i:], []byte(`<metadata><d2-source`)) || !bytes.HasSuffix(out, []byte("</metadata><rect/></svg>")) {
				t.Fatalf("unexpected output %s", out)
			}
			got, err := sourcemeta.ExtractSVG(out)
			if err != nil || got == nil || got.Entry.Content != "a" {
				t.Fatalf("extract = %#v, %v", got, err)
			}
		})
	}
	for name, svg := range map[string]string{
		"empty root": `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		"not svg":    `<html><body/></html>`,
		"no element": `hello`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := sourcemeta.EmbedSVG([]byte(svg), b); err == nil {
				t.Fatal("embedding succeeded")
			}
		})
	}
}

func TestEmbedSVGReplaces(t *testing.T) {
	first, err := sourcemeta.EmbedSVG([]byte(plainSVG), sample())
	if err != nil {
		t.Fatal(err)
	}
	next := &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "main.d2", Content: "new"}}
	second, err := sourcemeta.EmbedSVG(first, next)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(second, []byte("<metadata>")); n != 1 {
		t.Fatalf("%d metadata elements:\n%s", n, second)
	}
	again, err := sourcemeta.EmbedSVG([]byte(plainSVG), next)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second, again) {
		t.Fatalf("re-embedding differs from embedding once:\n%s\n%s", second, again)
	}
}

func TestExtractSVGIgnoresNestedAndForeignMetadata(t *testing.T) {
	nested := `<svg xmlns="http://www.w3.org/2000/svg"><metadata><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/></metadata>` +
		`<svg><metadata><d2-source xmlns="https://d2lang.com/ns/source" path="board.d2">inner</d2-source></metadata></svg></svg>`
	got, err := sourcemeta.ExtractSVG([]byte(nested))
	if err != nil || got != nil {
		t.Fatalf("extract = %#v, %v; want nil", got, err)
	}
	got, err = sourcemeta.ExtractSVG([]byte(plainSVG))
	if err != nil || got != nil {
		t.Fatalf("plain SVG: extract = %#v, %v; want nil", got, err)
	}
	if _, err := sourcemeta.ExtractSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><metadata><d2-source path="a.d2">a -&gt; `)); err == nil {
		t.Fatal("truncated metadata was accepted")
	}
}

// Boxwire's exports before D2 supported embedding.
func TestExtractBoxwireSVG(t *testing.T) {
	svg := `<?xml version="1.0" encoding="utf-8"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><metadata><bw:d2-source xmlns:bw="https://github.com/NamiLinkLabs/boxwire" layout="tala" theme-id="0" sketch="false" dark-theme-id="200">a -&gt; b: &quot;x &amp; y&quot;&#13;
c</bw:d2-source></metadata><svg class="d2-1"></svg></svg>`
	got, err := sourcemeta.ExtractSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	want := &sourcemeta.Bundle{
		Entry:    sourcemeta.File{Content: "a -> b: \"x & y\"\r\nc"},
		Settings: sourcemeta.Settings{Layout: ptr("tala"), ThemeID: ptr[int64](0), Sketch: ptr(false), DarkThemeID: ptr[int64](200)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}

	encoded := `<svg xmlns="http://www.w3.org/2000/svg"><metadata><bw:d2-source xmlns:bw="https://github.com/NamiLinkLabs/boxwire" layout="bogus layout" theme-id="-1" encoding="base64">` +
		base64.StdEncoding.EncodeToString([]byte("a\x01")) + `</bw:d2-source></metadata></svg>`
	got, err = sourcemeta.ExtractSVG([]byte(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if got.Entry.Content != "a\x01" || !got.Settings.IsZero() {
		t.Fatalf("got %#v", got)
	}

	// Re-embedding a Boxwire export replaces Boxwire's element.
	out, err := sourcemeta.EmbedSVG([]byte(svg), &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "index", Content: "z"}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("bw:d2-source")) || bytes.Count(out, []byte("<metadata>")) != 1 {
		t.Fatalf("Boxwire metadata kept:\n%s", out)
	}
}

func TestExtractSVGLimits(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"><metadata>`)
	for i := 0; i <= sourcemeta.MaxFiles; i++ {
		sb.WriteString(`<d2-source path="f.d2">x</d2-source>`)
	}
	sb.WriteString(`</metadata></svg>`)
	if _, err := sourcemeta.ExtractSVG([]byte(sb.String())); !errors.Is(err, sourcemeta.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	big := &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "a.d2", Content: strings.Repeat("a", sourcemeta.MaxBytes+1)}}
	if _, err := sourcemeta.EmbedSVG([]byte(plainSVG), big); !errors.Is(err, sourcemeta.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	for _, p := range []string{"/abs.d2", "../up.d2", "a/../b.d2", "a\\b.d2", "./a.d2", "."} {
		bad := &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "a.d2"}, Imports: []sourcemeta.File{{Path: p}}}
		if err := bad.Validate(); err == nil {
			t.Errorf("path %q was accepted", p)
		}
		svg := `<svg xmlns="http://www.w3.org/2000/svg"><metadata><d2-source path="a.d2">a</d2-source><d2-source path="` + p + `">b</d2-source></metadata></svg>`
		if _, err := sourcemeta.ExtractSVG([]byte(svg)); err == nil {
			t.Errorf("extracting path %q succeeded", p)
		}
	}
}

func TestPNGRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := stdpng.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	png, err := d2png.AddExif(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	b := sample()
	b.Imports = append(b.Imports, sourcemeta.File{Path: "z/bin.d2", Content: "\xff\x00"})
	out, err := sourcemeta.EmbedPNG(png, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdpng.Decode(bytes.NewReader(out)); err != nil {
		t.Fatal(err)
	}
	text, err := d2png.ReadText(out, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if text[sourcemeta.PNGSourceKeyword] != b.Entry.Content {
		t.Fatalf("d2-source = %q", text[sourcemeta.PNGSourceKeyword])
	}
	if !strings.HasPrefix(text[sourcemeta.PNGMetaKeyword], `{"format":1,"path":"main.d2","d2Version":"v0.7.0","settings":{"layout":"elk","themeID":200,"darkThemeID":201,"sketch":true,"pad":0,"center":false,"isometric":false,"animateInterval":1200},"target":"layers.x","imports":[{"path":"lib/x.d2"`) {
		t.Fatalf("d2-source-meta = %s", text[sourcemeta.PNGMetaKeyword])
	}
	got, err := sourcemeta.Extract(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, b) {
		t.Fatalf("round trip:\n got %#v\nwant %#v", got, b)
	}

	// A binary entry is stored as base64.
	bin := &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "a.d2", Content: "\xff\xfea\x00"}}
	out, err = sourcemeta.EmbedPNG(out, bin)
	if err != nil {
		t.Fatal(err)
	}
	got, err = sourcemeta.ExtractPNG(out)
	if err != nil || !reflect.DeepEqual(got, bin) {
		t.Fatalf("binary round trip = %#v, %v", got, err)
	}

	got, err = sourcemeta.ExtractPNG(png)
	if err != nil || got != nil {
		t.Fatalf("plain PNG: %#v, %v", got, err)
	}
}

func TestExtractBoxwirePNG(t *testing.T) {
	var buf bytes.Buffer
	if err := stdpng.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	png, err := d2png.AddText(buf.Bytes(), []d2png.TextEntry{
		{Keyword: "d2-source", Text: "a -> b"},
		{Keyword: "boxwire-settings", Text: `{"layout":"elk","themeID":3,"sketch":true,"darkThemeID":null}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := sourcemeta.Extract(png)
	if err != nil {
		t.Fatal(err)
	}
	want := &sourcemeta.Bundle{
		Entry:    sourcemeta.File{Content: "a -> b"},
		Settings: sourcemeta.Settings{Layout: ptr("elk"), ThemeID: ptr[int64](3), Sketch: ptr(true)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
	// Embedding again drops Boxwire's settings so they cannot disagree.
	out, err := sourcemeta.EmbedPNG(png, &sourcemeta.Bundle{Entry: sourcemeta.File{Path: "index", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := d2png.ReadText(out, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := text["boxwire-settings"]; ok || text["d2-source"] != "x" {
		t.Fatalf("text = %v", text)
	}
}

func TestIsImage(t *testing.T) {
	for in, want := range map[string]bool{
		"\x89PNG\r\n\x1a\nrest":     true,
		"<svg>":                     true,
		"  \n<?xml version='1.0'?>": true,
		"\ufeff<svg":                true,
		"a -> b":                    false,
		"<x>":                       false,
	} {
		if got := sourcemeta.IsImage([]byte(in)); got != want {
			t.Errorf("IsImage(%q) = %v", in, got)
		}
	}
}

func TestNewBundlePaths(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		imports map[string]string
		want    []string
	}{
		{"no imports", "/home/u/p/main.d2", nil, []string{"main.d2"}},
		{"nested", "/home/u/p/main.d2", map[string]string{"/home/u/p/lib/x.d2": "", "/home/u/p/a.d2": ""}, []string{"main.d2", "a.d2", "lib/x.d2"}},
		{"parent", "/home/u/p/diagrams/main.d2", map[string]string{"/home/u/p/shared/s.d2": "", "/home/u/p/diagrams/lib/x.d2": ""}, []string{"diagrams/main.d2", "diagrams/lib/x.d2", "shared/s.d2"}},
		{"entry imported too", "/p/main.d2", map[string]string{"/p/main.d2": "", "/p/b.d2": ""}, []string{"main.d2", "b.d2"}},
		{"memfs", "index", map[string]string{"lib/x.d2": ""}, []string{"index", "lib/x.d2"}},
		{"memfs parent", "index", map[string]string{"../x.d2": ""}, []string{"_/index", "x.d2"}},
		{"stdin", "index.d2", map[string]string{"x.d2": ""}, []string{"index.d2", "x.d2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := sourcemeta.NewBundle(tc.entry, "e", tc.imports)
			got := []string{b.Entry.Path}
			for _, f := range b.Imports {
				got = append(got, f.Path)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("paths = %v, want %v", got, tc.want)
			}
			if err := b.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBundleFilesAndOverlay(t *testing.T) {
	b := &sourcemeta.Bundle{
		Entry:   sourcemeta.File{Path: "diagrams/main.d2", Content: "...@../shared/s\nx: @lib/x"},
		Imports: []sourcemeta.File{{Path: "diagrams/lib/x.d2", Content: "label: X"}, {Path: "shared/s.d2", Content: "y"}},
	}
	files := b.Files("/img/dir/main.d2")
	want := map[string]string{
		"/img/dir/main.d2":  b.Entry.Content,
		"/img/dir/lib/x.d2": "label: X",
		"/img/shared/s.d2":  "y",
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %v", files)
	}
	fallback, err := memfs.New(map[string]string{"/img/dir/other.d2": "fallback", "/img/dir/lib/x.d2": "shadowed"})
	if err != nil {
		t.Fatal(err)
	}
	overlay := sourcemeta.Overlay(files, fallback)
	for name, content := range map[string]string{"/img/dir/lib/x.d2": "label: X", "/img/dir/./lib/../lib/x.d2": "label: X", "/img/dir/other.d2": "fallback"} {
		data, err := fs.ReadFile(overlay, name)
		if err != nil || string(data) != content {
			t.Errorf("%s = %q, %v; want %q", name, data, err, content)
		}
	}
	if _, err := sourcemeta.Overlay(files, nil).Open("/img/dir/other.d2"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("nil fallback: %v", err)
	}

	// The compiler finds every import through the overlay.
	g, _, err := d2compiler.Compile("/img/dir/main.d2", strings.NewReader(b.Entry.Content), &d2compiler.CompileOptions{FS: overlay})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Objects) != 2 {
		t.Fatalf("objects = %d", len(g.Objects))
	}
}

func TestRecorderRecordsImports(t *testing.T) {
	dir := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		p = filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("shared.d2", "classes: {hot: {style.fill: red}}\n")
	write("proj/lib/x.d2", "label: X\ny: @deep/z\n")
	write("proj/lib/deep/z.d2", "label: Z\n")
	entry := "...@../shared\nx: @lib/x\nx.class: hot\n"
	entryPath := filepath.Join(dir, "proj", "main.d2")

	rec := sourcemeta.NewRecorder(localfile.Unrestricted())
	if _, _, err := d2compiler.Compile(entryPath, strings.NewReader(entry), &d2compiler.CompileOptions{FS: rec}); err != nil {
		t.Fatal(err)
	}
	b := sourcemeta.NewBundle(entryPath, entry, rec.Files())
	var paths []string
	for _, f := range b.Imports {
		paths = append(paths, f.Path)
	}
	if b.Entry.Path != "proj/main.d2" || !reflect.DeepEqual(paths, []string{"proj/lib/deep/z.d2", "proj/lib/x.d2", "shared.d2"}) {
		t.Fatalf("entry %q, imports %v", b.Entry.Path, paths)
	}

	// The recorded bundle compiles elsewhere without the original files.
	files := b.Files("/elsewhere/main.d2")
	g, _, err := d2compiler.Compile("/elsewhere/main.d2", strings.NewReader(entry), &d2compiler.CompileOptions{FS: sourcemeta.Overlay(files, nil)})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Objects) != 2 {
		t.Fatalf("objects = %d", len(g.Objects))
	}

	empty := sourcemeta.NewRecorder(localfile.Unrestricted())
	if _, _, err := d2compiler.Compile(entryPath, strings.NewReader("a -> b"), &d2compiler.CompileOptions{FS: empty}); err != nil {
		t.Fatal(err)
	}
	if len(empty.Files()) != 0 {
		t.Fatalf("recorded %v", empty.Files())
	}
	f, err := rec.Open(filepath.Join(dir, "shared.d2"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	info, _ := f.Stat()
	if string(data) != "classes: {hot: {style.fill: red}}\n" || info.Size() != int64(len(data)) || info.Name() != "shared.d2" {
		t.Fatalf("reopened = %q, %v", data, info)
	}
}
