package png

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"unicode/utf8"
)

// TextEntry is one keyword and text pair stored in a PNG text chunk.
type TextEntry struct {
	Keyword string
	Text    string
}

const (
	pngChunkIDAT = uint32(0x49444154) // IDAT
	pngChunkITXt = uint32(0x69545874) // iTXt
	pngChunkTEXt = uint32(0x74455874) // tEXt
	pngChunkZTXt = uint32(0x7a545874) // zTXt
)

// ErrTextTooLarge reports that ReadText stopped at its byte limit.
var ErrTextTooLarge = errors.New("png: text chunks exceed the byte limit")

// TextChunk returns one complete, uncompressed iTXt chunk with an empty
// language tag and translated keyword. The keyword must be 1 to 79 printable
// ASCII characters without leading, trailing or consecutive spaces, and the
// text must be valid UTF-8.
func TextChunk(keyword, text string) ([]byte, error) {
	if err := validateTextKeyword(keyword); err != nil {
		return nil, err
	}
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("png: iTXt %q text is not valid UTF-8", keyword)
	}
	// keyword, NUL, compression flag 0, method 0, empty language NUL, empty
	// translated keyword NUL, then the text.
	dataSize := len(keyword) + 5 + len(text)
	if dataSize > math.MaxInt32 {
		return nil, fmt.Errorf("png: iTXt %q exceeds the maximum chunk length", keyword)
	}
	chunk := make([]byte, pngChunkOverhead+dataSize)
	binary.BigEndian.PutUint32(chunk[0:4], uint32(dataSize))
	binary.BigEndian.PutUint32(chunk[4:8], pngChunkITXt)
	n := copy(chunk[8:], keyword)
	n += 5 // the separators and flags are already zero
	copy(chunk[8+n:], text)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	return chunk, nil
}

// TextChunks encodes entries as consecutive iTXt chunks, in order.
func TextChunks(entries []TextEntry) ([]byte, error) {
	var out []byte
	for _, entry := range entries {
		chunk, err := TextChunk(entry.Keyword, entry.Text)
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}

// WriteText writes entries as iTXt chunks. A streaming encoder calls it after
// IHDR and eXIf and before the first IDAT chunk.
func WriteText(w io.Writer, entries []TextEntry) error {
	chunks, err := TextChunks(entries)
	if err != nil {
		return err
	}
	n, err := w.Write(chunks)
	if err == nil && n != len(chunks) {
		return io.ErrShortWrite
	}
	return err
}

// AddText returns a copy of png with entries stored as iTXt chunks. They are
// inserted after IHDR, and after an eXIf chunk that directly follows it, which
// is where D2's own encoder writes them. Existing tEXt, zTXt and iTXt chunks
// with one of the entries' keywords are removed, so adding the same keyword
// again replaces it.
func AddText(png []byte, entries []TextEntry) ([]byte, error) {
	return ReplaceText(png, entries, nil)
}

// ReplaceText is AddText that also removes the text chunks with the keywords
// in remove.
func ReplaceText(png []byte, entries []TextEntry, remove []string) ([]byte, error) {
	chunks, err := TextChunks(entries)
	if err != nil {
		return nil, err
	}
	replace := make(map[string]bool, len(entries)+len(remove))
	for _, entry := range entries {
		replace[entry.Keyword] = true
	}
	for _, keyword := range remove {
		replace[keyword] = true
	}
	var insertAt int
	var drop [][2]int
	previous := uint32(0)
	err = walkChunks(png, func(index, start, end int, typ uint32, data []byte) error {
		switch {
		case index == 0:
			insertAt = end
		case index == 1 && previous == pngChunkIHDR && typ == pngChunkEXIF:
			insertAt = end
		}
		previous = typ
		if isTextChunk(typ) {
			if keyword, ok := textKeyword(data); ok && replace[keyword] {
				if crc32.ChecksumIEEE(png[start+4:end-4]) != binary.BigEndian.Uint32(png[end-4:end]) {
					return fmt.Errorf("png: text chunk at byte %d has an invalid CRC", start)
				}
				drop = append(drop, [2]int{start, end})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(png)+len(chunks))
	offset := 0
	inserted := false
	for _, r := range drop {
		if !inserted && insertAt <= r[0] {
			out = append(out, png[offset:insertAt]...)
			out = append(out, chunks...)
			offset = insertAt
			inserted = true
		}
		out = append(out, png[offset:r[0]]...)
		offset = r[1]
	}
	if !inserted {
		out = append(out, png[offset:insertAt]...)
		out = append(out, chunks...)
		offset = insertAt
	}
	return append(out, png[offset:]...), nil
}

// ReadText returns the text of every tEXt, zTXt and iTXt chunk, keyed by
// keyword. When a keyword repeats, the first chunk wins. Compressed text is
// inflated. Latin-1 text from tEXt and zTXt is converted to UTF-8. Reading
// stops with ErrTextTooLarge once the decoded text would exceed maxBytes.
// Chunks with an invalid CRC or malformed text are skipped.
func ReadText(png []byte, maxBytes int) (map[string]string, error) {
	out := make(map[string]string)
	total := 0
	err := walkChunks(png, func(_, start, end int, typ uint32, data []byte) error {
		if !isTextChunk(typ) {
			return nil
		}
		if crc32.ChecksumIEEE(png[start+4:end-4]) != binary.BigEndian.Uint32(png[end-4:end]) {
			return nil
		}
		keyword, text, ok, err := decodeTextChunk(typ, data, maxBytes-total)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if _, exists := out[keyword]; exists {
			return nil
		}
		total += len(text)
		out[keyword] = text
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// IsPNG reports whether data starts with the PNG signature.
func IsPNG(data []byte) bool {
	return len(data) >= len(pngSignature) && bytes.Equal(data[:len(pngSignature)], pngSignature[:])
}

func isTextChunk(typ uint32) bool {
	return typ == pngChunkTEXt || typ == pngChunkZTXt || typ == pngChunkITXt
}

// walkChunks validates the chunk layout from the signature to IEND and calls
// visit for each chunk. It checks lengths and the IHDR/IEND framing, not CRCs.
func walkChunks(png []byte, visit func(index, start, end int, typ uint32, data []byte) error) error {
	if !IsPNG(png) {
		return fmt.Errorf("not PNG data")
	}
	offset := len(pngSignature)
	for index := 0; ; index++ {
		if len(png)-offset < pngChunkOverhead {
			return fmt.Errorf("truncated PNG chunk at byte %d", offset)
		}
		length := uint64(binary.BigEndian.Uint32(png[offset : offset+4]))
		if length > math.MaxInt32 || length > uint64(len(png)-offset-pngChunkOverhead) {
			return fmt.Errorf("truncated PNG chunk data at byte %d", offset)
		}
		end := offset + pngChunkOverhead + int(length)
		typ := binary.BigEndian.Uint32(png[offset+4 : offset+8])
		if index == 0 && (typ != pngChunkIHDR || length != 13) {
			return fmt.Errorf("PNG does not start with an IHDR chunk")
		}
		if err := visit(index, offset, end, typ, png[offset+8:end-4]); err != nil {
			return err
		}
		if typ == pngChunkIEND {
			return nil
		}
		offset = end
	}
}

func validateTextKeyword(keyword string) error {
	if len(keyword) < 1 || len(keyword) > 79 {
		return fmt.Errorf("png: text keyword %q must be 1 to 79 characters", keyword)
	}
	for i := 0; i < len(keyword); i++ {
		c := keyword[i]
		if c < 0x20 || c > 0x7e {
			return fmt.Errorf("png: text keyword %q must be printable ASCII", keyword)
		}
		if c == ' ' && (i == 0 || i == len(keyword)-1 || keyword[i-1] == ' ') {
			return fmt.Errorf("png: text keyword %q has leading, trailing or consecutive spaces", keyword)
		}
	}
	return nil
}

func textKeyword(data []byte) (string, bool) {
	end := bytes.IndexByte(data, 0)
	if end < 1 || end > 79 {
		return "", false
	}
	return latin1(data[:end]), true
}

func decodeTextChunk(typ uint32, data []byte, limit int) (keyword, text string, ok bool, err error) {
	keyword, ok = textKeyword(data)
	if !ok {
		return "", "", false, nil
	}
	rest := data[len(keyword)+1:]
	switch typ {
	case pngChunkTEXt:
		if len(rest) > limit {
			return "", "", false, ErrTextTooLarge
		}
		return keyword, latin1(rest), true, nil
	case pngChunkZTXt:
		if len(rest) < 1 || rest[0] != 0 {
			return "", "", false, nil
		}
		raw, ok, err := inflateText(rest[1:], limit)
		if !ok || err != nil {
			return "", "", false, err
		}
		return keyword, latin1(raw), true, nil
	default: // iTXt
		if len(rest) < 2 {
			return "", "", false, nil
		}
		compressed, method := rest[0], rest[1]
		rest = rest[2:]
		language := bytes.IndexByte(rest, 0)
		if language < 0 {
			return "", "", false, nil
		}
		rest = rest[language+1:]
		translated := bytes.IndexByte(rest, 0)
		if translated < 0 {
			return "", "", false, nil
		}
		rest = rest[translated+1:]
		switch {
		case compressed == 0:
			if len(rest) > limit {
				return "", "", false, ErrTextTooLarge
			}
		case compressed == 1 && method == 0:
			var ok bool
			rest, ok, err = inflateText(rest, limit)
			if !ok || err != nil {
				return "", "", false, err
			}
		default:
			return "", "", false, nil
		}
		if !utf8.Valid(rest) {
			return "", "", false, nil
		}
		return keyword, string(rest), true, nil
	}
}

// inflateText returns ok=false for corrupt zlib data and ErrTextTooLarge when
// the inflated text would exceed limit.
func inflateText(compressed []byte, limit int) ([]byte, bool, error) {
	if limit < 0 {
		limit = 0
	}
	r, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, false, nil
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, false, nil
	}
	if len(out) > limit {
		return nil, false, ErrTextTooLarge
	}
	return out, true, nil
}

func latin1(b []byte) string {
	ascii := true
	for _, c := range b {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return string(b)
	}
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return string(runes)
}
