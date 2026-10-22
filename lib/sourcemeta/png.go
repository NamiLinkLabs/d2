package sourcemeta

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	d2png "github.com/d2lang/d2/lib/png"
)

// pngMeta is the JSON in the d2-source-meta chunk.
type pngMeta struct {
	Format int    `json:"format"`
	Path   string `json:"path,omitempty"`
	// Encoding is "base64" when the d2-source chunk holds base64 instead of
	// the text itself.
	Encoding string    `json:"encoding,omitempty"`
	Version  string    `json:"d2Version,omitempty"`
	Settings *Settings `json:"settings,omitempty"`
	Target   *string   `json:"target,omitempty"`
	Imports  []pngFile `json:"imports,omitempty"`
}

type pngFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"`
}

func isPNG(data []byte) bool {
	return d2png.IsPNG(data)
}

// PNGText returns the text chunks that embed b in a PNG: the entry's text and
// the JSON with everything else.
func PNGText(b *Bundle) ([]d2png.TextEntry, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	meta := pngMeta{Format: Format, Path: b.Entry.Path, Version: b.Version, Target: b.Target}
	if !b.Settings.IsZero() {
		s := b.Settings
		meta.Settings = &s
	}
	source := b.Entry.Content
	if !pngText(source) {
		source = base64.StdEncoding.EncodeToString([]byte(source))
		meta.Encoding = "base64"
	}
	for _, f := range b.Imports {
		pf := pngFile{Path: f.Path, Content: f.Content}
		if !pngText(f.Content) {
			pf.Content = base64.StdEncoding.EncodeToString([]byte(f.Content))
			pf.Encoding = "base64"
		}
		meta.Imports = append(meta.Imports, pf)
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return []d2png.TextEntry{
		{Keyword: PNGSourceKeyword, Text: source},
		{Keyword: PNGMetaKeyword, Text: string(metaJSON)},
	}, nil
}

// pngText reports whether s can be stored as iTXt text and JSON unchanged.
func pngText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// EmbedPNG returns a copy of png with b embedded, replacing an earlier
// embedded source.
func EmbedPNG(png []byte, b *Bundle) ([]byte, error) {
	entries, err := PNGText(b)
	if err != nil {
		return nil, err
	}
	// Drop Boxwire's settings too, so they cannot disagree with ours.
	return d2png.ReplaceText(png, entries, []string{boxwireSettingsKeyword})
}

// ExtractPNG reads the bundle from a PNG's text chunks. It returns nil and no
// error when the PNG has none.
func ExtractPNG(png []byte) (*Bundle, error) {
	text, err := d2png.ReadText(png, MaxBytes*2)
	if err != nil {
		if errors.Is(err, d2png.ErrTextTooLarge) {
			return nil, ErrTooLarge
		}
		return nil, fmt.Errorf("sourcemeta: invalid PNG: %w", err)
	}
	source, ok := text[PNGSourceKeyword]
	if !ok {
		return nil, nil
	}
	b := &Bundle{Entry: File{Content: source}}
	raw, hasMeta := text[PNGMetaKeyword]
	if !hasMeta {
		if settings, ok := text[boxwireSettingsKeyword]; ok {
			b.Settings = settingsFromJSON([]byte(settings))
		}
		return b, b.Validate()
	}
	var meta struct {
		Path     string          `json:"path"`
		Encoding string          `json:"encoding"`
		Version  string          `json:"d2Version"`
		Settings json.RawMessage `json:"settings"`
		Target   *string         `json:"target"`
		Imports  []pngFile       `json:"imports"`
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("sourcemeta: invalid %s JSON: %w", PNGMetaKeyword, err)
	}
	if len(meta.Imports)+1 > MaxFiles {
		return nil, ErrTooLarge
	}
	b.Entry.Path = meta.Path
	if meta.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(stripSpace(source))
		if err != nil {
			return nil, fmt.Errorf("sourcemeta: invalid base64 in %s: %w", PNGSourceKeyword, err)
		}
		b.Entry.Content = string(decoded)
	}
	b.Version = meta.Version
	b.Target = meta.Target
	if len(meta.Settings) > 0 {
		b.Settings = settingsFromJSON(meta.Settings)
	}
	for _, f := range meta.Imports {
		content := f.Content
		if f.Encoding == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(stripSpace(content))
			if err != nil {
				return nil, fmt.Errorf("sourcemeta: invalid base64 in embedded %q: %w", f.Path, err)
			}
			content = string(decoded)
		}
		b.Imports = append(b.Imports, File{Path: f.Path, Content: content})
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

// settingsFromJSON reads settings leniently: a field with the wrong type is
// dropped instead of failing the whole read.
func settingsFromJSON(raw []byte) Settings {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Settings{}
	}
	var s Settings
	// Unmarshaling null leaves the zero value, which must not become a
	// setting (Boxwire writes darkThemeID: null).
	str := func(k string) *string {
		var v string
		if string(fields[k]) == "null" || json.Unmarshal(fields[k], &v) != nil {
			return nil
		}
		return parseLayout(v)
	}
	num := func(k string) *int64 {
		var v int64
		if string(fields[k]) == "null" || json.Unmarshal(fields[k], &v) != nil || v < 0 {
			return nil
		}
		return &v
	}
	flag := func(k string) *bool {
		var v bool
		if string(fields[k]) == "null" || json.Unmarshal(fields[k], &v) != nil {
			return nil
		}
		return &v
	}
	if _, ok := fields["layout"]; ok {
		s.Layout = str("layout")
	}
	if _, ok := fields["themeID"]; ok {
		s.ThemeID = num("themeID")
	}
	if _, ok := fields["darkThemeID"]; ok {
		s.DarkThemeID = num("darkThemeID")
	}
	if _, ok := fields["sketch"]; ok {
		s.Sketch = flag("sketch")
	}
	if _, ok := fields["pad"]; ok {
		s.Pad = num("pad")
	}
	if _, ok := fields["center"]; ok {
		s.Center = flag("center")
	}
	if _, ok := fields["isometric"]; ok {
		s.Isometric = flag("isometric")
	}
	if _, ok := fields["animateInterval"]; ok {
		s.AnimateInterval = num("animateInterval")
	}
	return s
}
