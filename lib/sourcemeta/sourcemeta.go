// Package sourcemeta embeds D2 source files in exported SVG and PNG images and
// reads them back, so an exported image can be re-rendered or edited later.
//
// An SVG carries the files in one <metadata> element, the first child of the
// root <svg>:
//
//	<metadata><d2-source xmlns="https://d2lang.com/ns/source" format="1" path="main.d2"
//	  d2-version="v0.7.0" layout="elk">a -&gt; b: @lib/x</d2-source><d2-source
//	  xmlns="https://d2lang.com/ns/source" path="lib/x.d2">…</d2-source></metadata>
//
// The first d2-source element is the entry file and carries the render
// settings. Text is escaped minimally, so it stays readable; a file with
// characters XML 1.0 cannot hold is stored with encoding="base64".
//
// A PNG carries the entry file's text in an uncompressed iTXt chunk with the
// keyword d2-source, and everything else (paths, settings, imported files) as
// JSON in an iTXt chunk with the keyword d2-source-meta.
//
// Paths are slash-separated and relative to the deepest directory that holds
// every embedded file, so they never contain "..", and nothing absolute is
// stored.
//
// Readers also accept the format Boxwire wrote before D2 supported this: a
// d2-source element in any namespace, and PNG settings in a boxwire-settings
// iTXt chunk.
package sourcemeta

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// Namespace is the XML namespace of the d2-source element.
	Namespace = "https://d2lang.com/ns/source"
	// Format is the version of the format this package writes.
	Format = 1

	// PNGSourceKeyword is the iTXt keyword of the entry file's text.
	PNGSourceKeyword = "d2-source"
	// PNGMetaKeyword is the iTXt keyword of the JSON with everything else.
	PNGMetaKeyword = "d2-source-meta"
	// boxwireSettingsKeyword holds render settings in PNGs Boxwire exported.
	boxwireSettingsKeyword = "boxwire-settings"

	// MaxBytes bounds the embedded text that readers accept.
	MaxBytes = 32 << 20
	// MaxFiles bounds the number of embedded files that readers accept.
	MaxFiles = 1024
)

// ErrTooLarge reports embedded sources beyond MaxBytes or MaxFiles.
var ErrTooLarge = errors.New("sourcemeta: embedded source exceeds the size or file limit")

// File is one embedded D2 file.
type File struct {
	// Path is slash-separated and relative to the bundle's root.
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Settings are the render settings that were given on the command line or as
// D2.js options. Values from vars.d2-config are already in the source, so they
// are not repeated here. A nil field was not set.
type Settings struct {
	Layout          *string `json:"layout,omitempty"`
	ThemeID         *int64  `json:"themeID,omitempty"`
	DarkThemeID     *int64  `json:"darkThemeID,omitempty"`
	Sketch          *bool   `json:"sketch,omitempty"`
	Pad             *int64  `json:"pad,omitempty"`
	Center          *bool   `json:"center,omitempty"`
	Isometric       *bool   `json:"isometric,omitempty"`
	AnimateInterval *int64  `json:"animateInterval,omitempty"`
}

// IsZero reports whether no setting is set.
func (s Settings) IsZero() bool {
	return s == Settings{}
}

// Bundle is the D2 source embedded in one image.
type Bundle struct {
	// Entry is the file that was compiled. Its path is empty only in images
	// written by Boxwire, which stored a single unnamed file.
	Entry File `json:"entry"`
	// Imports are the other files the compiler read, sorted by path.
	Imports  []File   `json:"imports,omitempty"`
	Settings Settings `json:"settings"`
	// Target is the --target that renders exactly what the image shows. It is
	// nil when the image shows the whole diagram, and "" for the root board of
	// a diagram that also has other boards.
	Target *string `json:"target,omitempty"`
	// Version is the D2 version that wrote the image, if it was recorded.
	Version string `json:"version,omitempty"`
}

// NewBundle builds a bundle from the entry file and the files the compiler
// read while importing (see Recorder). Paths are host paths as the compiler
// saw them: absolute in the CLI, or relative keys of an in-memory file system.
// The entry is skipped if it was also opened as an import.
func NewBundle(entryPath, entryContent string, imports map[string]string) *Bundle {
	type file struct {
		abs     string
		content string
	}
	files := []file{{abs: entryPath, content: entryContent}}
	keys := make([]string, 0, len(imports))
	for p := range imports {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		files = append(files, file{abs: p, content: imports[p]})
	}

	// Rebase relative paths that climb above their directory onto enough
	// placeholder directories, so every path shares one coordinate system.
	climb := 0
	for i := range files {
		p := filepath.ToSlash(files[i].abs)
		if !path.IsAbs(p) {
			p = path.Clean(p)
			n := 0
			for _, seg := range strings.Split(p, "/") {
				if seg != ".." {
					break
				}
				n++
			}
			climb = max(climb, n)
		}
		files[i].abs = p
	}
	for i := range files {
		if !path.IsAbs(files[i].abs) {
			files[i].abs = path.Join("/"+strings.Repeat("_/", climb), files[i].abs)
		} else {
			files[i].abs = path.Clean(files[i].abs)
		}
	}

	root := strings.Split(path.Dir(files[0].abs), "/")
	for _, f := range files[1:] {
		dir := strings.Split(path.Dir(f.abs), "/")
		n := 0
		for n < len(root) && n < len(dir) && root[n] == dir[n] {
			n++
		}
		root = root[:n]
	}
	prefix := strings.Join(root, "/")
	rel := func(p string) string {
		p = strings.TrimPrefix(p, prefix)
		return strings.TrimPrefix(p, "/")
	}

	b := &Bundle{Entry: File{Path: rel(files[0].abs), Content: files[0].content}}
	seen := map[string]bool{b.Entry.Path: true}
	for _, f := range files[1:] {
		p := rel(f.abs)
		if seen[p] {
			continue
		}
		seen[p] = true
		b.Imports = append(b.Imports, File{Path: p, Content: f.content})
	}
	sort.Slice(b.Imports, func(i, j int) bool { return b.Imports[i].Path < b.Imports[j].Path })
	return b
}

// Validate checks that every path is a clean relative slash path without ".."
// and that the bundle is within MaxBytes and MaxFiles. An empty entry path is
// allowed for Boxwire's format.
func (b *Bundle) Validate() error {
	if b == nil {
		return errors.New("sourcemeta: nil bundle")
	}
	if len(b.Imports)+1 > MaxFiles {
		return ErrTooLarge
	}
	total := len(b.Entry.Content)
	if b.Entry.Path != "" && !validPath(b.Entry.Path) {
		return fmt.Errorf("sourcemeta: invalid embedded path %q", b.Entry.Path)
	}
	seen := map[string]bool{b.Entry.Path: true}
	for _, f := range b.Imports {
		if !validPath(f.Path) {
			return fmt.Errorf("sourcemeta: invalid embedded path %q", f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("sourcemeta: duplicate embedded path %q", f.Path)
		}
		seen[f.Path] = true
		total += len(f.Content)
	}
	if total > MaxBytes {
		return ErrTooLarge
	}
	return nil
}

func validPath(p string) bool {
	return p != "." && fs.ValidPath(p) && !strings.ContainsRune(p, '\\')
}

// Files places the bundle's files in a host file system: the entry at
// inputPath and every import where the entry's relative imports find it.
// The keys are slash-separated, cleaned paths.
func (b *Bundle) Files(inputPath string) map[string]string {
	inputPath = path.Clean(filepath.ToSlash(inputPath))
	dir := path.Dir(inputPath)
	entryDir := path.Dir(b.Entry.Path)
	out := map[string]string{inputPath: b.Entry.Content}
	for _, f := range b.Imports {
		p := path.Join(dir, relPath(entryDir, f.Path))
		if _, exists := out[p]; !exists {
			out[p] = f.Content
		}
	}
	return out
}

// relPath returns target relative to the directory dir. Both are clean,
// relative slash paths in the same tree.
func relPath(dir, target string) string {
	var from []string
	if dir != "." && dir != "" {
		from = strings.Split(dir, "/")
	}
	to := strings.Split(target, "/")
	n := 0
	for n < len(from) && n < len(to)-1 && from[n] == to[n] {
		n++
	}
	parts := make([]string, 0, len(from)-n+len(to)-n)
	for range from[n:] {
		parts = append(parts, "..")
	}
	parts = append(parts, to[n:]...)
	return strings.Join(parts, "/")
}

// Overlay returns a file system where files, keyed as Files returns them, win
// over fallback. A nil fallback denies everything else.
func Overlay(files map[string]string, fallback fs.FS) fs.FS {
	return &overlayFS{files: files, fallback: fallback}
}

type overlayFS struct {
	files    map[string]string
	fallback fs.FS
}

func (o *overlayFS) Open(name string) (fs.File, error) {
	if content, ok := o.files[path.Clean(filepath.ToSlash(name))]; ok {
		return newMemFile(path.Base(name), []byte(content), time.Time{}), nil
	}
	if o.fallback == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return o.fallback.Open(name)
}

// Recorder is a file system that records every regular file read through it,
// so the files a compilation imported can be embedded.
type Recorder struct {
	fs    fs.FS
	mu    sync.Mutex
	files map[string]string
}

// NewRecorder wraps fsys. A nil fsys records nothing and denies every open.
func NewRecorder(fsys fs.FS) *Recorder {
	return &Recorder{fs: fsys, files: make(map[string]string)}
}

// Open opens name in the wrapped file system and records its content.
func (r *Recorder) Open(name string) (fs.File, error) {
	if r.fs == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	f, err := r.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return f, err
	}
	data, err := io.ReadAll(f)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	r.mu.Lock()
	r.files[name] = string(data)
	r.mu.Unlock()
	return newMemFile(info.Name(), data, info.ModTime()), nil
}

// Files returns the recorded files keyed by the names they were opened with.
func (r *Recorder) Files() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.files))
	for k, v := range r.files {
		out[k] = v
	}
	return out
}

type memFile struct {
	*bytes.Reader
	info memInfo
}

func newMemFile(name string, data []byte, modTime time.Time) *memFile {
	return &memFile{Reader: bytes.NewReader(data), info: memInfo{name: name, size: int64(len(data)), modTime: modTime}}
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Close() error               { return nil }

type memInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() fs.FileMode  { return 0o444 }
func (i memInfo) ModTime() time.Time { return i.modTime }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }

// Extract reads the bundle embedded in a PNG or SVG image. It returns nil and
// no error when the image has none.
func Extract(data []byte) (*Bundle, error) {
	if isPNG(data) {
		return ExtractPNG(data)
	}
	return ExtractSVG(data)
}

// IsImage reports whether data looks like a PNG or an SVG document, for
// callers that must tell an image from D2 source read from stdin.
func IsImage(data []byte) bool {
	if isPNG(data) {
		return true
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	data = bytes.TrimLeft(data, " \t\r\n")
	return bytes.HasPrefix(data, []byte("<svg")) || bytes.HasPrefix(data, []byte("<?xml"))
}
