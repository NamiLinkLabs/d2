package d2cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/localfile"
	d2png "github.com/d2lang/d2/lib/png"
	"github.com/d2lang/d2/lib/sourcemeta"
	"github.com/d2lang/d2/lib/version"
)

// sourceEmbedding is the --embed-source state of one CLI run. It travels in
// the context, so the many render paths need no extra parameters.
type sourceEmbedding struct {
	// write is set when SVG and PNG outputs embed the source.
	write bool
	// requested is set when the user asked for it, rather than an image
	// input carrying it over.
	requested bool
	// settings are the render settings given as flags or environment
	// variables, which the output records.
	settings    sourcemeta.Settings
	omitVersion bool
	// target is the --target value.
	target string

	// image is the bundle read from an image input; nil for D2 input.
	image *sourcemeta.Bundle
	// imageEntryPath is where the image's entry file is placed for the
	// compiler: next to the image, under its embedded name.
	imageEntryPath string
	// imageFS serves the embedded files over the image's directory.
	imageFS fs.FS
}

// embeddedOutput is what the outputs of one compilation embed.
type embeddedOutput struct {
	bundle      *sourcemeta.Bundle
	multiBoard  bool
	target      string
	animated    bool
	omitVersion bool
}

type (
	sourceEmbeddingKey struct{}
	embeddedOutputKey  struct{}
	embeddedBoardKey   struct{}
)

func withSourceEmbedding(ctx context.Context, e *sourceEmbedding) context.Context {
	return context.WithValue(ctx, sourceEmbeddingKey{}, e)
}

func sourceEmbeddingFrom(ctx context.Context) *sourceEmbedding {
	e, _ := ctx.Value(sourceEmbeddingKey{}).(*sourceEmbedding)
	return e
}

func withEmbeddedOutput(ctx context.Context, o *embeddedOutput) context.Context {
	return context.WithValue(ctx, embeddedOutputKey{}, o)
}

// withEmbeddedBoard records the board path of the outputs rendered under ctx.
func withEmbeddedBoard(ctx context.Context, boardPath []string) context.Context {
	if ctx.Value(embeddedOutputKey{}) == nil {
		return ctx
	}
	return context.WithValue(ctx, embeddedBoardKey{}, boardPath)
}

func withEmbeddedChildBoard(ctx context.Context, kind, name string) context.Context {
	if ctx.Value(embeddedOutputKey{}) == nil {
		return ctx
	}
	parent, _ := ctx.Value(embeddedBoardKey{}).([]string)
	return context.WithValue(ctx, embeddedBoardKey{}, append(append([]string(nil), parent...), kind, name))
}

// outputBundle returns the bundle to embed in the output rendered under ctx,
// or nil when the output embeds nothing.
func outputBundle(ctx context.Context) *sourcemeta.Bundle {
	o, _ := ctx.Value(embeddedOutputKey{}).(*embeddedOutput)
	if o == nil {
		return nil
	}
	b := *o.bundle
	switch {
	case o.animated:
		if o.target != "*" {
			target := o.target
			b.Target = &target
		}
	case o.multiBoard:
		boardPath, _ := ctx.Value(embeddedBoardKey{}).([]string)
		target := formatBoardTarget(boardPath)
		b.Target = &target
	}
	return &b
}

// formatBoardTarget returns the --target that selects boardPath alone.
func formatBoardTarget(boardPath []string) string {
	if len(boardPath) == 0 {
		return ""
	}
	return d2format.Format(d2ast.MakeKeyPath(boardPath))
}

func embedSVGOutput(ctx context.Context, svg []byte) ([]byte, error) {
	b := outputBundle(ctx)
	if b == nil {
		return svg, nil
	}
	out, err := sourcemeta.EmbedSVG(svg, b)
	if err != nil {
		return nil, fmt.Errorf("--embed-source: %w", err)
	}
	return out, nil
}

func embedPNGOutput(ctx context.Context, png []byte) ([]byte, error) {
	b := outputBundle(ctx)
	if b == nil {
		return png, nil
	}
	out, err := sourcemeta.EmbedPNG(png, b)
	if err != nil {
		return nil, fmt.Errorf("--embed-source: %w", err)
	}
	return out, nil
}

// pngSourceChunks returns the encoded text chunks the PNG rendered under ctx
// embeds, or nil.
func pngSourceChunks(ctx context.Context) ([]byte, error) {
	b := outputBundle(ctx)
	if b == nil {
		return nil, nil
	}
	entries, err := sourcemeta.PNGText(b)
	if err != nil {
		return nil, fmt.Errorf("--embed-source: %w", err)
	}
	return d2png.TextChunks(entries)
}

// prepareEmbeddedOutput builds the bundle that the outputs of a compilation
// of input embed. Relative paths, as stdin input imports them, are relative
// to workDir.
func (e *sourceEmbedding) prepareEmbeddedOutput(ctx context.Context, workDir, entryPath string, input []byte, recorder *sourcemeta.Recorder, rootDiagram *d2target.Diagram, animated bool) (context.Context, error) {
	if e == nil || !e.write {
		return ctx, nil
	}
	if entryPath == "-" {
		entryPath = "index.d2"
	}
	if !filepath.IsAbs(entryPath) {
		entryPath = filepath.Join(workDir, entryPath)
	}
	imports := make(map[string]string)
	for name, content := range recorder.Files() {
		if !filepath.IsAbs(name) {
			name = filepath.Join(workDir, name)
		}
		imports[name] = content
	}
	b := sourcemeta.NewBundle(entryPath, string(input), imports)
	b.Settings = e.settings
	if !e.omitVersion {
		b.Version = version.Version
	}
	if err := b.Validate(); err != nil {
		return ctx, fmt.Errorf("--embed-source: %w", err)
	}
	o := &embeddedOutput{
		bundle:     b,
		multiBoard: len(rootDiagram.Layers)+len(rootDiagram.Scenarios)+len(rootDiagram.Steps) > 0,
		target:     e.target,
		animated:   animated,
	}
	return withEmbeddedOutput(ctx, o), nil
}

// isImageInputPath reports whether the CLI input is an exported image rather
// than D2 source.
func isImageInputPath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".svg", ".png":
		return true
	}
	return false
}

// readImageInput extracts the embedded source of an image input. For stdin,
// data holds what was read; D2 source on stdin returns nil.
func readImageInput(ms *xmain.State, inputPath string, data []byte) (*sourceEmbedding, error) {
	b, err := sourcemeta.Extract(data)
	if err != nil {
		return nil, fmt.Errorf("failed to read the D2 source embedded in %s: %w", ms.HumanPath(inputPath), err)
	}
	name := ms.HumanPath(inputPath)
	if inputPath == "-" {
		name = "the image on stdin"
	}
	if b == nil {
		return nil, xmain.UsageErrorf("%s has no embedded D2 source (export it with --embed-source)", name)
	}
	dir := ms.PWD
	if inputPath != "-" {
		dir = filepath.Dir(inputPath)
	}
	entryName := path.Base(b.Entry.Path)
	if b.Entry.Path == "" {
		entryName = "index.d2"
		if inputPath != "-" {
			entryName = getFileName(inputPath) + ".d2"
		}
	}
	entryPath := filepath.Join(dir, entryName)
	return &sourceEmbedding{
		image:          b,
		imageEntryPath: entryPath,
	}, nil
}

// openImageFS serves the image's embedded files to the compiler. A file that
// is not embedded is read only from beneath the image's directory, so an
// image cannot import arbitrary host files. The caller closes the policy.
func (e *sourceEmbedding) openImageFS() (localfile.Policy, error) {
	policy, err := localfile.Rooted(filepath.Dir(e.imageEntryPath))
	if err != nil {
		return localfile.Policy{}, err
	}
	e.imageFS = sourcemeta.Overlay(e.image.Files(e.imageEntryPath), policy)
	return policy, nil
}

// extractCmd implements d2 extract: it writes the D2 source embedded in an
// image to stdout, to a .d2 file or into a directory.
func extractCmd(ctx context.Context, ms *xmain.State) error {
	args := ms.Opts.Flags.Args()[1:]
	if len(args) == 0 || len(args) > 2 {
		return xmain.UsageErrorf("usage: d2 extract image.svg|image.png [file.d2 | directory | -]")
	}
	inputPath := args[0]
	if inputPath != "-" {
		inputPath = ms.AbsPath(inputPath)
	}
	output := "-"
	if len(args) == 2 {
		output = args[1]
	}
	data, err := ms.ReadPath(inputPath)
	if err != nil {
		return err
	}
	e, err := readImageInput(ms, inputPath, data)
	if err != nil {
		return err
	}
	b := e.image
	if output == "-" {
		if len(b.Imports) > 0 {
			name := ms.HumanPath(inputPath)
			if inputPath == "-" {
				name = "The image"
			}
			ms.Log.Warn.Printf("%s also embeds %d imported file(s); extract into a directory to get them", name, len(b.Imports))
		}
		_, err := writeStdout(ms.Stdout, []byte(b.Entry.Content))
		return err
	}
	output = ms.AbsPath(output)

	type outFile struct {
		path, content string
		explicit      bool
	}
	var files []outFile
	if strings.EqualFold(filepath.Ext(output), ".d2") {
		// The entry goes where the user asked, its imports next to it.
		dir := filepath.Dir(output)
		placed := b.Files(output)
		paths := make([]string, 0, len(placed))
		for p := range placed {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			hostPath := filepath.FromSlash(p)
			rel, err := filepath.Rel(dir, hostPath)
			if err != nil || !filepath.IsLocal(rel) {
				return fmt.Errorf("%s imports files from outside the entry's directory; extract into a directory instead", ms.HumanPath(inputPath))
			}
			files = append(files, outFile{path: hostPath, content: placed[p], explicit: hostPath == output})
		}
	} else {
		entryPath := b.Entry.Path
		if entryPath == "" {
			entryPath = filepath.Base(e.imageEntryPath)
		}
		for _, f := range append([]sourcemeta.File{{Path: entryPath, Content: b.Entry.Content}}, b.Imports...) {
			rel := filepath.FromSlash(f.Path)
			if !filepath.IsLocal(rel) {
				return fmt.Errorf("%s embeds the unsafe path %q", ms.HumanPath(inputPath), f.Path)
			}
			files = append(files, outFile{path: filepath.Join(output, rel), content: f.Content})
		}
	}
	// Check every file before writing any, so a conflict leaves nothing
	// half-extracted.
	for _, f := range files {
		existing, err := os.ReadFile(f.path)
		if err == nil && !f.explicit && string(existing) != f.content {
			return fmt.Errorf("%s already exists with different content; remove it or extract elsewhere", ms.HumanPath(f.path))
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if existing, err := os.ReadFile(f.path); err == nil && bytes.Equal(existing, []byte(f.content)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return err
		}
		if err := ms.AtomicWritePath(f.path, []byte(f.content)); err != nil {
			return err
		}
		ms.Log.Success.Printf("extracted %s", ms.HumanPath(f.path))
	}
	return nil
}

// flagOrEnvSet reports whether the user set flag on the command line or
// through envKey.
func flagOrEnvSet(ms *xmain.State, flag, envKey string) bool {
	if ms.Opts.Flags.Changed(flag) {
		return true
	}
	return envKey != "" && ms.Env.Getenv(envKey) != ""
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// embeddedTheme returns an embedded theme ID if D2 knows the theme. An
// unknown one, say from a newer D2, is ignored with a warning.
func embeddedTheme(ms *xmain.State, id *int64, what string) *int64 {
	if id == nil {
		return nil
	}
	if d2themescatalog.Find(*id) == (d2themes.Theme{}) {
		ms.Log.Warn.Printf("ignoring the embedded %s %d, which this version of d2 does not have", what, *id)
		return nil
	}
	return clonePtr(id)
}

// embedsSource reports whether outputs of the format can embed their source.
func embedsSource(ext exportExtension) bool {
	switch ext {
	case SVG, PNG, isometricSVG, isometricPNG:
		return true
	}
	return false
}
