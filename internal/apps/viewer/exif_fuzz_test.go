package viewer

import (
	"os"
	"path/filepath"
	"testing"
)

// Malformed files the parser once panicked on, and others like them.
var malformed = [][]byte{
	{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x00},       // a segment length under 2 (reported)
	{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x01, 0x00}, // length 1
	[]byte("II*\x00"),                          // a TIFF header and nothing more
	[]byte("MM\x00*\x00\x00"),
	append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x0C}, []byte("Exif\x00\x00II*\x00")...), // EXIF holding a bare header
	append([]byte("II*\x00\x08\x00\x00\x00\xFF\xFF"), make([]byte, 12)...),               // more entries than bytes
	[]byte("\x89PNG\r\n\x1a\n\xFF\xFF\xFF\xFFeXIf"),
	[]byte("RIFF\x00\x00\x00\x00WEBPEXIF\xFF\xFF\xFF\xFF"),
}

func TestMalformedExif(t *testing.T) {
	dir := t.TempDir()
	for i, b := range malformed {
		p := filepath.Join(dir, "bad")
		os.WriteFile(p, b, 0o600)
		// Each reads as an error or as nothing, never a panic; the
		// reported file has no EXIF to find.
		if _, err := ReadExif(p); i == 0 && err == nil {
			t.Errorf("case %d: no error", i)
		}
		ParseTIFF(b) // must not panic
		if tiff, err := findTIFF(b); err == nil {
			ParseTIFF(tiff)
		}
	}
}

func FuzzExif(f *testing.F) {
	for _, b := range malformed {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if tiff, err := findTIFF(b); err == nil {
			ParseTIFF(tiff)
		}
		ParseTIFF(b)
	})
}
