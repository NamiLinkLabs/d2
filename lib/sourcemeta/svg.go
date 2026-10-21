package sourcemeta

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// EmbedSVG returns svg with b embedded as the first child of the root <svg>.
// A d2-source <metadata> element that svg already has is replaced. Call it
// last, after every other step that edits the SVG text.
func EmbedSVG(svg []byte, b *Bundle) ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	scan, err := scanSVG(svg, bytes.Contains(svg, []byte("d2-source")))
	if err != nil {
		return nil, err
	}
	meta := b.svgMetadata()
	out := make([]byte, 0, len(svg)+len(meta))
	out = append(out, svg[:scan.rootEnd]...)
	out = append(out, meta...)
	offset := scan.rootEnd
	for _, r := range scan.metadata {
		out = append(out, svg[offset:r[0]]...)
		offset = r[1]
	}
	return append(out, svg[offset:]...), nil
}

// ExtractSVG reads the bundle from the first root-level <metadata> element
// that holds d2-source elements. Metadata of nested <svg> elements, such as
// the boards of an animated export, is ignored. It returns nil and no error
// when the SVG has no embedded source.
func ExtractSVG(svg []byte) (*Bundle, error) {
	scan, err := scanSVG(svg, true)
	if err != nil {
		return nil, err
	}
	return scan.bundle, nil
}

type svgScan struct {
	// rootEnd is the offset just past the root start tag.
	rootEnd int
	// metadata are the byte ranges of root-level d2-source metadata elements.
	metadata [][2]int
	// bundle is read from the first of them.
	bundle *Bundle
}

func newSVGDecoder(svg []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(svg))
	// D2 SVGs can hold HTML from markdown labels. Only the metadata needs to
	// be well-formed.
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	return d
}

// scanSVG finds the root start tag and, when full is set, every root-level
// <metadata> with d2-source children.
func scanSVG(svg []byte, full bool) (*svgScan, error) {
	d := newSVGDecoder(svg)
	scan := &svgScan{}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, errors.New("sourcemeta: not an SVG document")
		}
		if err != nil {
			return nil, fmt.Errorf("sourcemeta: invalid SVG: %w", err)
		}
		if start, ok := tok.(xml.StartElement); ok {
			if start.Name.Local != "svg" {
				return nil, errors.New("sourcemeta: not an SVG document")
			}
			scan.rootEnd = int(d.InputOffset())
			if scan.rootEnd < 2 || scan.rootEnd > len(svg) || svg[scan.rootEnd-1] != '>' || svg[scan.rootEnd-2] == '/' {
				return nil, errors.New("sourcemeta: the root <svg> element is empty")
			}
			break
		}
	}
	if !full {
		return scan, nil
	}
	for {
		start := int(d.InputOffset())
		tok, err := d.Token()
		if err == io.EOF {
			return scan, nil
		}
		if err != nil {
			if scan.bundle != nil {
				return scan, nil
			}
			return nil, fmt.Errorf("sourcemeta: invalid SVG: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			// The root closed.
			return scan, nil
		case xml.StartElement:
			if t.Name.Local != "metadata" {
				if err := d.Skip(); err != nil {
					if scan.bundle != nil {
						return scan, nil
					}
					return nil, fmt.Errorf("sourcemeta: invalid SVG: %w", err)
				}
				continue
			}
			files, err := readMetadata(d)
			if err != nil {
				return nil, err
			}
			if len(files) == 0 {
				continue
			}
			scan.metadata = append(scan.metadata, [2]int{start, int(d.InputOffset())})
			if scan.bundle == nil {
				scan.bundle, err = bundleFromElements(files)
				if err != nil {
					return nil, err
				}
			}
		}
	}
}

type sourceElement struct {
	attrs map[string]string
	text  string
}

// readMetadata reads the d2-source children of a <metadata> element whose
// start tag was just read, through its end tag.
func readMetadata(d *xml.Decoder) ([]sourceElement, error) {
	var files []sourceElement
	total := 0
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("sourcemeta: invalid SVG metadata: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return files, nil
		case xml.StartElement:
			if t.Name.Local != "d2-source" {
				if err := d.Skip(); err != nil {
					return nil, fmt.Errorf("sourcemeta: invalid SVG metadata: %w", err)
				}
				continue
			}
			if len(files) >= MaxFiles {
				return nil, ErrTooLarge
			}
			el := sourceElement{attrs: make(map[string]string, len(t.Attr))}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				el.attrs[a.Name.Local] = a.Value
			}
			var text strings.Builder
			for depth := 1; depth > 0; {
				tok, err := d.Token()
				if err != nil {
					return nil, fmt.Errorf("sourcemeta: invalid d2-source element: %w", err)
				}
				switch t := tok.(type) {
				case xml.StartElement:
					depth++
				case xml.EndElement:
					depth--
				case xml.CharData:
					if depth == 1 {
						total += len(t)
						if total > MaxBytes*2 {
							return nil, ErrTooLarge
						}
						text.Write(t)
					}
				}
			}
			el.text = text.String()
			files = append(files, el)
		}
	}
}

func bundleFromElements(els []sourceElement) (*Bundle, error) {
	b := &Bundle{}
	for i, el := range els {
		content := el.text
		if el.attrs["encoding"] == "base64" {
			raw, err := base64.StdEncoding.DecodeString(stripSpace(content))
			if err != nil {
				return nil, fmt.Errorf("sourcemeta: invalid base64 in d2-source %q: %w", el.attrs["path"], err)
			}
			content = string(raw)
		}
		f := File{Path: el.attrs["path"], Content: content}
		if i == 0 {
			b.Entry = f
			b.Settings = settingsFromAttrs(el.attrs)
			b.Version = el.attrs["d2-version"]
			if target, ok := el.attrs["target"]; ok {
				b.Target = &target
			}
			continue
		}
		b.Imports = append(b.Imports, f)
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		}
		return r
	}, s)
}

func settingsFromAttrs(attrs map[string]string) Settings {
	var s Settings
	if v, ok := attrs["layout"]; ok {
		s.Layout = parseLayout(v)
	}
	s.ThemeID = parseUint(attrs["theme-id"])
	s.DarkThemeID = parseUint(attrs["dark-theme-id"])
	s.Sketch = parseBool(attrs["sketch"])
	s.Pad = parseUint(attrs["pad"])
	s.Center = parseBool(attrs["center"])
	s.Isometric = parseBool(attrs["isometric"])
	s.AnimateInterval = parseUint(attrs["animate-interval"])
	return s
}

// parseLayout keeps plausible engine names only, since the value comes from
// an untrusted file.
func parseLayout(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 64 {
		return nil
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return nil
		}
	}
	return &v
}

func parseUint(v string) *int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 0 {
		return nil
	}
	return &n
}

func parseBool(v string) *bool {
	switch strings.TrimSpace(v) {
	case "true":
		t := true
		return &t
	case "false":
		f := false
		return &f
	}
	return nil
}

func (b *Bundle) svgMetadata() []byte {
	var buf bytes.Buffer
	buf.WriteString("<metadata>")
	attrs := [][2]string{{"format", strconv.Itoa(Format)}}
	if b.Entry.Path != "" {
		attrs = append(attrs, [2]string{"path", b.Entry.Path})
	}
	if b.Version != "" {
		attrs = append(attrs, [2]string{"d2-version", b.Version})
	}
	s := b.Settings
	if s.Layout != nil {
		attrs = append(attrs, [2]string{"layout", *s.Layout})
	}
	if s.ThemeID != nil {
		attrs = append(attrs, [2]string{"theme-id", strconv.FormatInt(*s.ThemeID, 10)})
	}
	if s.DarkThemeID != nil {
		attrs = append(attrs, [2]string{"dark-theme-id", strconv.FormatInt(*s.DarkThemeID, 10)})
	}
	if s.Sketch != nil {
		attrs = append(attrs, [2]string{"sketch", strconv.FormatBool(*s.Sketch)})
	}
	if s.Pad != nil {
		attrs = append(attrs, [2]string{"pad", strconv.FormatInt(*s.Pad, 10)})
	}
	if s.Center != nil {
		attrs = append(attrs, [2]string{"center", strconv.FormatBool(*s.Center)})
	}
	if s.Isometric != nil {
		attrs = append(attrs, [2]string{"isometric", strconv.FormatBool(*s.Isometric)})
	}
	if s.AnimateInterval != nil {
		attrs = append(attrs, [2]string{"animate-interval", strconv.FormatInt(*s.AnimateInterval, 10)})
	}
	if b.Target != nil {
		attrs = append(attrs, [2]string{"target", *b.Target})
	}
	writeSourceElement(&buf, b.Entry, attrs)
	for _, f := range b.Imports {
		writeSourceElement(&buf, f, [][2]string{{"path", f.Path}})
	}
	buf.WriteString("</metadata>")
	return buf.Bytes()
}

func writeSourceElement(buf *bytes.Buffer, f File, attrs [][2]string) {
	buf.WriteString(`<d2-source xmlns="` + Namespace + `"`)
	for _, a := range attrs {
		buf.WriteString(" " + a[0] + `="`)
		_ = xml.EscapeText(buf, []byte(a[1]))
		buf.WriteString(`"`)
	}
	if xmlText(f.Content) {
		buf.WriteString(">")
		buf.WriteString(escapeText(f.Content))
	} else {
		buf.WriteString(` encoding="base64">`)
		buf.WriteString(base64.StdEncoding.EncodeToString([]byte(f.Content)))
	}
	buf.WriteString("</d2-source>")
}

// escapeText escapes only what XML text requires, so D2 source such as
// "a -> b" stays readable. Carriage returns are escaped because XML parsers
// turn CRLF into LF.
var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", "]]>", "]]&gt;", "\r", "&#13;")

func escapeText(s string) string {
	return textEscaper.Replace(s)
}

// xmlText reports whether s is valid UTF-8 made of characters XML 1.0
// allows.
func xmlText(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			return false
		}
		switch {
		case r == 0x9 || r == 0xA || r == 0xD:
		case r >= 0x20 && r <= 0xD7FF:
		case r >= 0xE000 && r <= 0xFFFD:
		case r >= 0x10000 && r <= 0x10FFFF:
		default:
			return false
		}
		i += size
	}
	return true
}
