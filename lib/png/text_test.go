package png

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	stdpng "image/png"
	"strings"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := stdpng.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func rawChunk(typ string, data []byte) []byte {
	chunk := make([]byte, 8, 12+len(data))
	binary.BigEndian.PutUint32(chunk, uint32(len(data)))
	copy(chunk[4:], typ)
	chunk = append(chunk, data...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}

func chunkTypes(t *testing.T, png []byte) []string {
	t.Helper()
	var types []string
	if err := walkChunks(png, func(_, start, end int, typ uint32, data []byte) error {
		if crc32.ChecksumIEEE(png[start+4:end-4]) != binary.BigEndian.Uint32(png[end-4:end]) {
			t.Fatalf("chunk at %d has a bad CRC", start)
		}
		name := string(png[start+4 : start+8])
		if isTextChunk(typ) {
			keyword, _ := textKeyword(data)
			name += ":" + keyword
		}
		types = append(types, name)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return types
}

func insertAfterIHDR(png []byte, chunks ...[]byte) []byte {
	const ihdrEnd = 8 + 12 + 13
	out := append([]byte{}, png[:ihdrEnd]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, png[ihdrEnd:]...)
}

func TestTextChunkLayout(t *testing.T) {
	chunk, err := TextChunk("d2-source", "a -> b: ünï")
	if err != nil {
		t.Fatal(err)
	}
	want := rawChunk("iTXt", append([]byte("d2-source\x00\x00\x00\x00\x00"), "a -> b: ünï"...))
	if !bytes.Equal(chunk, want) {
		t.Fatalf("chunk = %q, want %q", chunk, want)
	}
}

func TestTextChunkRejectsBadInput(t *testing.T) {
	for _, keyword := range []string{"", strings.Repeat("k", 80), " lead", "trail ", "two  spaces", "tab\tkey", "ünï"} {
		if _, err := TextChunk(keyword, "x"); err == nil {
			t.Errorf("TextChunk(%q) succeeded", keyword)
		}
	}
	if _, err := TextChunk("key", "\xff"); err == nil {
		t.Error("TextChunk accepted invalid UTF-8")
	}
}

func TestAddTextKeepsPNGDecodable(t *testing.T) {
	png, err := AddExif(testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := AddText(png, []TextEntry{{"d2-source", "x -> y"}, {"d2-source-meta", `{"format":1}`}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdpng.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	types := chunkTypes(t, out)
	if strings.Join(types[:4], " ") != "IHDR eXIf iTXt:d2-source iTXt:d2-source-meta" {
		t.Fatalf("chunks = %v", types)
	}
	text, err := ReadText(out, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if text["d2-source"] != "x -> y" || text["d2-source-meta"] != `{"format":1}` {
		t.Fatalf("text = %v", text)
	}
}

func TestAddTextReplacesKeywords(t *testing.T) {
	png := insertAfterIHDR(testPNG(t),
		rawChunk("tEXt", []byte("Comment\x00keep")),
		rawChunk("tEXt", []byte("d2-source\x00old")),
	)
	out, err := AddText(png, []TextEntry{{"d2-source", "new"}})
	if err != nil {
		t.Fatal(err)
	}
	out, err = AddText(out, []TextEntry{{"d2-source", "newer"}})
	if err != nil {
		t.Fatal(err)
	}
	types := chunkTypes(t, out)
	if strings.Join(types[:3], " ") != "IHDR iTXt:d2-source tEXt:Comment" {
		t.Fatalf("chunks = %v", types)
	}
	text, err := ReadText(out, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if text["d2-source"] != "newer" || text["Comment"] != "keep" {
		t.Fatalf("text = %v", text)
	}
	removed, err := ReplaceText(out, nil, []string{"Comment"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(chunkTypes(t, removed)[:2], " "); got != "IHDR iTXt:d2-source" {
		t.Fatalf("after remove: %v", got)
	}
}

func TestWriteTextMatchesAddText(t *testing.T) {
	var streamed bytes.Buffer
	entries := []TextEntry{{"d2-source", "a"}}
	if err := WriteText(&streamed, entries); err != nil {
		t.Fatal(err)
	}
	chunks, err := TextChunks(entries)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(streamed.Bytes(), chunks) {
		t.Fatal("WriteText and TextChunks differ")
	}
}

func TestReadTextFormats(t *testing.T) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write([]byte("zipped \xe9"))
	zw.Close()
	var zi bytes.Buffer
	zw = zlib.NewWriter(&zi)
	zw.Write([]byte("zipped ünï"))
	zw.Close()
	bad := rawChunk("tEXt", []byte("bad\x00crc"))
	bad[len(bad)-1] ^= 0xff
	png := insertAfterIHDR(testPNG(t),
		rawChunk("tEXt", []byte("latin\x00caf\xe9")),
		rawChunk("zTXt", append([]byte("z\x00\x00"), z.Bytes()...)),
		rawChunk("iTXt", append([]byte("iz\x00\x01\x00en\x00Zipped\x00"), zi.Bytes()...)),
		rawChunk("iTXt", []byte("plain\x00\x00\x00\x00\x00ünï")),
		rawChunk("iTXt", []byte("plain\x00\x00\x00\x00\x00second")),
		bad,
	)
	text, err := ReadText(png, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"latin": "café", "z": "zipped é", "iz": "zipped ünï", "plain": "ünï"}
	for k, v := range want {
		if text[k] != v {
			t.Errorf("text[%q] = %q, want %q", k, text[k], v)
		}
	}
	if _, ok := text["bad"]; ok {
		t.Error("read a chunk with a bad CRC")
	}
}

func TestReadTextLimits(t *testing.T) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(bytes.Repeat([]byte("a"), 1<<20))
	zw.Close()
	png := insertAfterIHDR(testPNG(t), rawChunk("zTXt", append([]byte("bomb\x00\x00"), z.Bytes()...)))
	if _, err := ReadText(png, 1000); !errors.Is(err, ErrTextTooLarge) {
		t.Fatalf("err = %v, want ErrTextTooLarge", err)
	}
	if _, err := ReadText(png[:len(png)-20], 1<<21); err == nil {
		t.Fatal("truncated PNG was accepted")
	}
	if _, err := ReadText([]byte("<svg/>"), 10); err == nil {
		t.Fatal("non-PNG was accepted")
	}
}
