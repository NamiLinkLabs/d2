//go:build js && wasm

package d2wasm

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2target"
	d2png "github.com/d2lang/d2/lib/png"
	"github.com/d2lang/d2/lib/sourcemeta"
)

// embeddedSettings are the options a compile was given, which its rendered
// image records. Values from vars.d2-config are in the source already.
func embeddedSettings(opts *CompileOptions) sourcemeta.Settings {
	s := sourcemeta.Settings{
		Layout:      opts.Layout,
		ThemeID:     opts.ThemeID,
		DarkThemeID: opts.DarkThemeID,
		Sketch:      opts.Sketch,
		Pad:         opts.Pad,
		Center:      opts.Center,
		Isometric:   opts.Isometric,
	}
	if opts.AnimateInterval != nil && *opts.AnimateInterval > 0 {
		s.AnimateInterval = opts.AnimateInterval
	}
	return s
}

// renderedSource returns a copy of source whose target renders exactly what
// the image shows.
func renderedSource(source *sourcemeta.Bundle, diagram *d2target.Diagram, boardPath []string, target *string, animated bool) *sourcemeta.Bundle {
	b := *source
	b.Target = nil
	multiBoard := len(diagram.Layers)+len(diagram.Scenarios)+len(diagram.Steps) > 0
	switch {
	case target != nil && *target == "*":
	case animated && target != nil:
		t := *target
		b.Target = &t
	case multiBoard:
		t := ""
		if len(boardPath) > 0 {
			t = d2format.Format(d2ast.MakeKeyPath(boardPath))
		}
		b.Target = &t
	}
	return &b
}

// ExtractSource reads the D2 source embedded in an SVG or PNG image. It
// returns null for an image without one.
func ExtractSource(args []js.Value) (interface{}, error) {
	if len(args) < 1 {
		return nil, &WASMError{Message: "missing JSON argument", Code: 400}
	}
	var input ExtractSourceRequest
	if err := json.Unmarshal([]byte(args[0].String()), &input); err != nil {
		return nil, &WASMError{Message: "invalid JSON input", Code: 400}
	}
	b, err := sourcemeta.Extract(input.Data)
	if err != nil {
		return nil, &WASMError{Message: err.Error(), Code: 400}
	}
	if b == nil {
		return nil, nil
	}
	return extractSourceResponse(b), nil
}

func extractSourceResponse(b *sourcemeta.Bundle) *ExtractSourceResponse {
	inputPath := b.Entry.Path
	if inputPath == "" {
		inputPath = DEFAULT_INPUT_PATH
	}
	// The files keep their embedded layout, so the entry's relative
	// imports find them.
	fs := map[string]string{inputPath: b.Entry.Content}
	for _, f := range b.Imports {
		fs[f.Path] = f.Content
	}
	target := b.Target
	if target == nil && b.Settings.AnimateInterval != nil {
		// An animation of the whole diagram; D2.js renders one board unless
		// told otherwise.
		all := "*"
		target = &all
	}
	embed := true
	opts := ExtractedOptions{
		Layout:          b.Settings.Layout,
		ThemeID:         b.Settings.ThemeID,
		DarkThemeID:     b.Settings.DarkThemeID,
		Sketch:          b.Settings.Sketch,
		Pad:             b.Settings.Pad,
		Center:          b.Settings.Center,
		Isometric:       b.Settings.Isometric,
		AnimateInterval: b.Settings.AnimateInterval,
		Target:          target,
		EmbedSource:     &embed,
	}
	return &ExtractSourceResponse{FS: fs, InputPath: inputPath, Options: opts, Source: b}
}

// EmbedSource embeds a source, as compile returns it in renderOptions.source
// or extractSource returns it, in an SVG or PNG image, replacing an earlier
// embedded source.
func EmbedSource(args []js.Value) (interface{}, error) {
	if len(args) < 1 {
		return nil, &WASMError{Message: "missing JSON argument", Code: 400}
	}
	var input EmbedSourceRequest
	if err := json.Unmarshal([]byte(args[0].String()), &input); err != nil {
		return nil, &WASMError{Message: "invalid JSON input", Code: 400}
	}
	if input.Source == nil {
		return nil, &WASMError{Message: "missing 'source' field in input JSON", Code: 400}
	}
	var out []byte
	var err error
	if d2png.IsPNG(input.Data) {
		out, err = sourcemeta.EmbedPNG(input.Data, input.Source)
	} else {
		out, err = sourcemeta.EmbedSVG(input.Data, input.Source)
	}
	if err != nil {
		return nil, &WASMError{Message: fmt.Sprintf("embedding the source failed: %s", err.Error()), Code: 400}
	}
	return out, nil
}
