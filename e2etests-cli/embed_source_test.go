package e2etests_cli

import (
	"bytes"
	"context"
	"image"
	stdpng "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/xos"

	d2png "github.com/d2lang/d2/lib/png"
	"github.com/d2lang/d2/lib/sourcemeta"
)

var embeddedMetadataRE = regexp.MustCompile(`(?s)<metadata><d2-source .*?</metadata>`)

// withoutEmbeddedSource drops the embedded source so renders can be compared.
func withoutEmbeddedSource(svg []byte) string {
	return embeddedMetadataRE.ReplaceAllString(string(svg), "")
}

func entryElement(t *testing.T, svg []byte) string {
	t.Helper()
	m := regexp.MustCompile(`<d2-source [^>]*>`).Find(svg)
	if m == nil {
		t.Fatalf("no embedded source in:\n%.400s", svg)
	}
	return string(m)
}

func extractBundle(t *testing.T, data []byte) *sourcemeta.Bundle {
	t.Helper()
	b, err := sourcemeta.Extract(data)
	assert.Success(t, err)
	if b == nil {
		t.Fatal("no embedded source")
	}
	return b
}

func runWithStdio(t *testing.T, ctx context.Context, dir string, env *xos.Env, stdin []byte, args ...string) ([]byte, error) {
	t.Helper()
	var stdout bytes.Buffer
	tms := testMain(dir, env, args...)
	tms.Stdin = bytes.NewReader(stdin)
	tms.Stdout = nopWriteCloser{&stdout}
	tms.Start(t, ctx)
	defer tms.Cleanup(t)
	err := tms.Wait(ctx)
	return stdout.Bytes(), err
}

func errorContains(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), msg) {
		t.Fatalf("error = %v, want one containing %q", err, msg)
	}
}

type nopWriteCloser struct{ *bytes.Buffer }

func (nopWriteCloser) Close() error { return nil }

func TestCLI_EmbedSource(t *testing.T) {
	t.Parallel()

	const source = "# kept comment\nx -> y: \"a < b & c\"\nz: |md\n  <img width=\"10\"/> **hi**\n|\n"

	tca := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, dir string, env *xos.Env)
	}{
		{
			name: "off-by-default",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", source)
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "in.d2", "out.svg"))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "in.d2", "out.png"))
				assert.False(t, bytes.Contains(readFile(t, dir, "out.svg"), []byte("<metadata>")))
				text, err := d2png.ReadText(readFile(t, dir, "out.png"), 1<<20)
				assert.Success(t, err)
				assert.Equal(t, 0, len(text))
			},
		},
		{
			name: "svg",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", source)
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "--force-appendix", "in.d2", "out.svg"))
				svg := readFile(t, dir, "out.svg")
				assert.True(t, bytes.Contains(svg, []byte(`viewBox="0 0 `)))
				entry := entryElement(t, svg)
				assert.True(t, strings.HasPrefix(entry, `<d2-source xmlns="https://d2lang.com/ns/source" format="1" path="in.d2" d2-version="`))
				// Readable, minimally escaped text that the appendix and image
				// bundling steps did not touch.
				assert.True(t, bytes.Contains(svg, []byte("x -> y: \"a &lt; b &amp; c\"\nz: |md\n  &lt;img width=\"10\"/> **hi**")))
				assert.Equal(t, source, extractBundle(t, svg).Entry.Content)

				// D2_EMBED_SOURCE works too.
				env.Setenv("D2_EMBED_SOURCE", "1")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "in.d2", "env.svg"))
				assert.Equal(t, source, extractBundle(t, readFile(t, dir, "env.svg")).Entry.Content)
			},
		},
		{
			name: "png",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", source)
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "--sketch", "in.d2", "out.png"))
				png := readFile(t, dir, "out.png")
				validatePNG(t, png, 0, 0)
				text, err := d2png.ReadText(png, 1<<20)
				assert.Success(t, err)
				assert.Equal(t, source, text["d2-source"])
				assert.True(t, strings.HasPrefix(text["d2-source-meta"], `{"format":1,"path":"in.d2","d2Version":"`))
				b := extractBundle(t, png)
				assert.True(t, b.Settings.Sketch != nil && *b.Settings.Sketch)

				// The PNG renders again without its source file.
				assert.Success(t, os.Remove(filepath.Join(dir, "in.d2")))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "out.png", "again.png"))
				assert.Equal(t, string(png), string(readFile(t, dir, "again.png")))
			},
		},
		{
			name: "roundtrip-settings",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", source)
				assert.Success(t, runTestMain(t, ctx, dir, env, "--embed-source", "--layout=elk", "--theme=200", "--sketch", "--pad=10", "in.d2", "out.svg"))
				first := readFile(t, dir, "out.svg")
				assert.True(t, strings.Contains(entryElement(t, first), ` layout="elk" theme-id="200" sketch="true" pad="10">`))

				// No flags: the embedded settings apply and embedding is sticky.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "out.svg", "again.svg"))
				again := readFile(t, dir, "again.svg")
				assert.Equal(t, withoutEmbeddedSource(first), withoutEmbeddedSource(again))
				assert.Equal(t, entryElement(t, first), entryElement(t, again))
				// Rendering in place refreshes the image.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "out.svg", "out.svg"))
				assert.Equal(t, string(again), string(readFile(t, dir, "out.svg")))

				// An explicit flag wins and is recorded.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--theme=0", "out.svg", "light.svg"))
				light := readFile(t, dir, "light.svg")
				assert.True(t, strings.Contains(entryElement(t, light), ` theme-id="0" sketch="true"`))
				assert.False(t, withoutEmbeddedSource(light) == withoutEmbeddedSource(first))

				// --embed-source=false drops it.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source=false", "out.svg", "plain.svg"))
				plain := readFile(t, dir, "plain.svg")
				assert.False(t, bytes.Contains(plain, []byte("<metadata>")))
				assert.Equal(t, withoutEmbeddedSource(first), string(plain))
			},
		},
		{
			name: "d2-config-precedence",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", "vars: {d2-config: {theme-id: 4}}\nx -> y\n")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "--theme=200", "in.d2", "flag.svg"))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "in.d2", "config.svg"))
				flag, config := readFile(t, dir, "flag.svg"), readFile(t, dir, "config.svg")
				assert.True(t, strings.Contains(entryElement(t, flag), `theme-id="200"`))
				// d2-config values are in the source already.
				assert.False(t, strings.Contains(entryElement(t, config), `theme-id`))

				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "flag.svg", "flag2.svg"))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "config.svg", "config2.svg"))
				assert.Equal(t, withoutEmbeddedSource(flag), withoutEmbeddedSource(readFile(t, dir, "flag2.svg")))
				assert.Equal(t, withoutEmbeddedSource(config), withoutEmbeddedSource(readFile(t, dir, "config2.svg")))
				assert.False(t, withoutEmbeddedSource(flag) == withoutEmbeddedSource(config))
			},
		},
		{
			name: "imports",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "shared.d2", "classes: {hot: {style.fill: \"#f88\"}}\n")
				writeFile(t, dir, "proj/lib/x.d2", "label: \"X <&> ]]> y\"\n")
				writeFile(t, dir, "proj/main.d2", "...@../shared\nx: @lib/x\nx -> y {class: hot}\n")
				assert.Success(t, runTestMain(t, ctx, dir, env, "--embed-source", "proj/main.d2", "proj/out.svg"))
				first := readFile(t, dir, "proj/out.svg")
				b := extractBundle(t, first)
				assert.Equal(t, "proj/main.d2", b.Entry.Path)
				assert.Equal(t, 2, len(b.Imports))
				assert.Equal(t, "proj/lib/x.d2", b.Imports[0].Path)
				assert.Equal(t, "shared.d2", b.Imports[1].Path)

				// runTestMain removed every .d2 file. Render from elsewhere.
				writeFile(t, dir, "elsewhere/deep/out.svg", string(first))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "elsewhere/deep/out.svg", "elsewhere/again.svg"))
				assert.Equal(t, withoutEmbeddedSource(first), withoutEmbeddedSource(readFile(t, dir, "elsewhere/again.svg")))

				// Extract into a directory recreates the tree.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "extract", "elsewhere/deep/out.svg", "tree"))
				assert.Equal(t, "...@../shared\nx: @lib/x\nx -> y {class: hot}\n", string(readFile(t, dir, "tree/proj/main.d2")))
				assert.Equal(t, "label: \"X <&> ]]> y\"\n", string(readFile(t, dir, "tree/proj/lib/x.d2")))
				assert.Equal(t, "classes: {hot: {style.fill: \"#f88\"}}\n", string(readFile(t, dir, "tree/shared.d2")))
				// Extracting again is a no-op; a changed file is not overwritten.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "extract", "elsewhere/deep/out.svg", "tree"))
				writeFile(t, dir, "tree/shared.d2", "edited\n")
				assert.Error(t, runTestMainPersist(t, ctx, dir, env, "extract", "elsewhere/deep/out.svg", "tree"))
				assert.Equal(t, "edited\n", string(readFile(t, dir, "tree/shared.d2")))
				// A .d2 target refuses imports from above the entry.
				err := runTestMainPersist(t, ctx, dir, env, "extract", "elsewhere/deep/out.svg", "flat/main.d2")
				errorContains(t, err, "imports files from outside the entry's directory")
				_, statErr := os.Stat(filepath.Join(dir, "flat"))
				assert.True(t, os.IsNotExist(statErr))
			},
		},
		{
			name: "extract",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "lib/x.d2", "label: X\n")
				writeFile(t, dir, "in.d2", "x: @lib/x\n"+source)
				assert.Success(t, runTestMain(t, ctx, dir, env, "--embed-source", "in.d2", "out.png"))
				stdout, err := runWithStdio(t, ctx, dir, env, nil, "extract", "out.png")
				assert.Success(t, err)
				assert.Equal(t, "x: @lib/x\n"+source, string(stdout))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "extract", "out.png", "copy/main.d2"))
				assert.Equal(t, "x: @lib/x\n"+source, string(readFile(t, dir, "copy/main.d2")))
				assert.Equal(t, "label: X\n", string(readFile(t, dir, "copy/lib/x.d2")))
				// The image on stdin.
				stdout, err = runWithStdio(t, ctx, dir, env, readFile(t, dir, "out.png"), "extract", "-")
				assert.Success(t, err)
				assert.Equal(t, "x: @lib/x\n"+source, string(stdout))
			},
		},
		{
			name: "extract-rejects-unsafe-paths",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				for i, p := range []string{"../evil.d2", "/etc/evil.d2", `a\..\..\evil.d2`} {
					svg := `<svg xmlns="http://www.w3.org/2000/svg"><metadata><d2-source xmlns="https://d2lang.com/ns/source" path="main.d2">x</d2-source><d2-source xmlns="https://d2lang.com/ns/source" path="` + p + `">y</d2-source></metadata></svg>`
					name := "evil" + string(rune('0'+i)) + ".svg"
					writeFile(t, dir, name, svg)
					assert.Error(t, runTestMainPersist(t, ctx, dir, env, "extract", name, "out"))
					assert.Error(t, runTestMainPersist(t, ctx, dir, env, name, "out.svg"))
				}
				_, err := os.Stat(filepath.Join(dir, "out"))
				assert.True(t, os.IsNotExist(err))
				_, err = os.Stat(filepath.Join(filepath.Dir(dir), "evil.d2"))
				assert.True(t, os.IsNotExist(err))
			},
		},
		{
			name: "image-input-errors",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", "x -> y")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "in.d2", "plain.svg"))
				errorContains(t, runTestMainPersist(t, ctx, dir, env, "plain.svg", "out.png"), "plain.svg has no embedded D2 source (export it with --embed-source)")
				errorContains(t, runTestMainPersist(t, ctx, dir, env, "extract", "plain.svg"), "has no embedded D2 source")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "in.d2", "embedded.svg"))
				errorContains(t, runTestMainPersist(t, ctx, dir, env, "embedded.svg"), "needs an output path")
				errorContains(t, runTestMainPersist(t, ctx, dir, env, "-w", "embedded.svg", "x.svg"), "-w[atch] cannot render an image")
			},
		},
		{
			name: "omit-version",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", "x -> y")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "--omit-version", "in.d2", "out.svg"))
				svg := readFile(t, dir, "out.svg")
				assert.Equal(t, `<d2-source xmlns="https://d2lang.com/ns/source" format="1" path="in.d2">`, entryElement(t, svg))
				assert.False(t, bytes.Contains(svg, []byte("d2-version")))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "--omit-version", "in.d2", "out.png"))
				assert.False(t, bytes.Contains(readFile(t, dir, "out.png"), []byte("d2Version")))
			},
		},
		{
			name: "multiboard",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", "a -> b\nlayers: {x: {c -> d; layers: {\"y z\": {e}}}}\nscenarios: {s: {a.style.fill: red}}\n")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "in.d2", "out.svg"))
				targets := map[string]string{
					"out/index.svg":          ``,
					"out/layers/x/index.svg": `layers.x`,
					"out/layers/x/y z.svg":   `layers.x.layers.y z`,
					"out/scenarios/s.svg":    `scenarios.s`,
				}
				for file, target := range targets {
					b := extractBundle(t, readFile(t, dir, file))
					if b.Target == nil || *b.Target != target {
						t.Fatalf("%s target = %v, want %q", file, b.Target, target)
					}
				}
				// A board's file renders that board again.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--target=layers.x", "in.d2", "direct.svg"))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "out/layers/x/index.svg", "again.svg"))
				assert.Equal(t, string(readFile(t, dir, "direct.svg")), withoutEmbeddedSource(readFile(t, dir, "again.svg")))
				// An explicit --target wins.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--target=*", "out/layers/x/index.svg", "all.svg"))
				_, err := os.Stat(filepath.Join(dir, "all", "scenarios", "s.svg"))
				assert.Success(t, err)
			},
		},
		{
			name: "animate",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", "a -> b\nsteps: {1: {c}; 2: {d}}\n")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "--animate-interval=900", "in.d2", "anim.svg"))
				anim := readFile(t, dir, "anim.svg")
				assert.Equal(t, 1, bytes.Count(anim, []byte("<metadata>")))
				assert.True(t, strings.HasSuffix(entryElement(t, anim), ` animate-interval="900">`))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "anim.svg", "again.svg"))
				assert.Equal(t, string(anim), string(readFile(t, dir, "again.svg")))
				// The interval does not apply to PNG, which renders the root.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--target=", "anim.svg", "root.png"))
				validatePNG(t, readFile(t, dir, "root.png"), 0, 0)
			},
		},
		{
			name: "stdin",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				// Relative imports from stdin resolve against the process's
				// working directory, which tests cannot change.
				writeFile(t, dir, "lib/x.d2", "label: X\n")
				stdinSource := "x: @\"" + filepath.ToSlash(filepath.Join(dir, "lib", "x")) + "\"\n"
				svg, err := runWithStdio(t, ctx, dir, env, []byte(stdinSource), "--embed-source", "-", "-")
				assert.Success(t, err)
				b := extractBundle(t, svg)
				assert.Equal(t, "index.d2", b.Entry.Path)
				assert.Equal(t, 1, len(b.Imports))
				assert.Equal(t, "lib/x.d2", b.Imports[0].Path)

				assert.Success(t, os.Remove(filepath.Join(dir, "lib/x.d2")))
				png, err := runWithStdio(t, ctx, dir, env, svg, "--stdout-format=png", "-", "-")
				assert.Success(t, err)
				validatePNG(t, png, 0, 0)
				assert.Equal(t, stdinSource, extractBundle(t, png).Entry.Content)
			},
		},
		{
			name: "isometric",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", "a -> b\nlayers: {x: {c}}\n")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "--isometric", "in.d2", "iso.svg"))
				for file, target := range map[string]string{"iso/index.svg": "", "iso/x.svg": "layers.x"} {
					b := extractBundle(t, readFile(t, dir, file))
					assert.True(t, b.Settings.Isometric != nil && *b.Settings.Isometric)
					assert.True(t, b.Target != nil && *b.Target == target)
				}
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "--isometric", "--target=", "in.d2", "iso.png"))
				png := readFile(t, dir, "iso.png")
				validatePNG(t, png, 0, 0)
				assert.Equal(t, "a -> b\nlayers: {x: {c}}\n", extractBundle(t, png).Entry.Content)
				// The PNG renders as an isometric SVG of the root board again.
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--isometric", "--target=", "in.d2", "direct.svg"))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "iso.png", "from-png.svg"))
				assert.Equal(t, string(readFile(t, dir, "direct.svg")), withoutEmbeddedSource(readFile(t, dir, "from-png.svg")))
			},
		},
		{
			name: "boxwire-exports",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "want.d2", "a -> b: \"x & y\"\n")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--layout=elk", "--theme=3", "want.d2", "want.svg"))
				writeFile(t, dir, "bw.svg", `<?xml version="1.0" encoding="utf-8"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><metadata><bw:d2-source xmlns:bw="https://github.com/NamiLinkLabs/boxwire" layout="elk" theme-id="3" sketch="false">a -&gt; b: "x &amp; y"
</bw:d2-source></metadata><rect width="1" height="1"/></svg>`)
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "bw.svg", "from-svg.svg"))
				assert.Equal(t, string(readFile(t, dir, "want.svg")), withoutEmbeddedSource(readFile(t, dir, "from-svg.svg")))
				// The re-embedded source is D2's format, named after the image.
				assert.True(t, strings.HasPrefix(entryElement(t, readFile(t, dir, "from-svg.svg")), `<d2-source xmlns="https://d2lang.com/ns/source" format="1" path="bw.d2"`))

				var buf bytes.Buffer
				assert.Success(t, stdpng.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))))
				png, err := d2png.AddText(buf.Bytes(), []d2png.TextEntry{
					{Keyword: "d2-source", Text: "a -> b: \"x & y\"\n"},
					{Keyword: "boxwire-settings", Text: `{"layout":"elk","themeID":3,"sketch":false,"darkThemeID":null}`},
				})
				assert.Success(t, err)
				writeFile(t, dir, "bw.png", string(png))
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "bw.png", "from-png.svg"))
				assert.Equal(t, string(readFile(t, dir, "want.svg")), withoutEmbeddedSource(readFile(t, dir, "from-png.svg")))
			},
		},
		{
			name: "svg-icon-with-embedded-source",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				icon, err := sourcemeta.EmbedSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10"><rect width="10" height="10" fill="#0a0"/></svg>`),
					&sourcemeta.Bundle{Entry: sourcemeta.File{Path: "icon.d2", Content: "a -> b"}})
				assert.Success(t, err)
				writeFile(t, dir, "icon.svg", string(icon))
				writeFile(t, dir, "in.d2", "x: {icon: ./icon.svg}\ny: {shape: image; icon: ./icon.svg}\n")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "in.d2", "out.png"))
				validatePNG(t, readFile(t, dir, "out.png"), 0, 0)
			},
		},
		{
			name: "other-formats-warn",
			run: func(t *testing.T, ctx context.Context, dir string, env *xos.Env) {
				writeFile(t, dir, "in.d2", "x -> y")
				assert.Success(t, runTestMainPersist(t, ctx, dir, env, "--embed-source", "in.d2", "out.pdf"))
				validatePDF(t, readFile(t, dir, "out.pdf"), 1)
			},
		},
	}

	for _, tc := range tca {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			dir, cleanup := assert.TempDir(t)
			defer cleanup()
			tc.run(t, ctx, dir, xos.NewEnv(nil))
		})
	}
}
