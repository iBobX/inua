package viewer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Kind is how the viewer shows a file.
type Kind int

const (
	None     Kind = iota // not a file the viewer knows
	Image                // drawn on the stage, zoomed and turned
	PDF                  // pages drawn by poppler, one under another
	Markdown             // laid out as a document
	Text                 // plain text, in a fixed-width font
)

// textExts are plain text files the viewer opens.
var textExts = map[string]bool{
	".txt": true, ".text": true, ".log": true, ".csv": true, ".tsv": true, ".json": true, ".jsonl": true,
	".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".conf": true, ".cfg": true, ".env": true,
	".xml": true, ".html": true, ".htm": true, ".css": true, ".js": true, ".mjs": true, ".ts": true,
	".go": true, ".py": true, ".rs": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".java": true, ".kt": true, ".swift": true, ".rb": true, ".lua": true, ".sh": true, ".bash": true,
	".zsh": true, ".fish": true, ".sql": true, ".diff": true, ".patch": true, ".rst": true, ".org": true,
	".tex": true, ".srt": true, ".vtt": true, ".nfo": true, ".desktop": true, ".service": true,
	".blp": true, ".po": true, ".pot": true, ".mod": true, ".sum": true,
}

// KindOf is how the viewer shows a file, by its name.
func KindOf(path string) Kind {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case ext == ".pdf":
		return PDF
	case Exts[ext] == "text/markdown":
		return Markdown
	case Exts[ext] != "":
		return Image
	case textExts[ext]:
		return Text
	}
	return None
}

// Suffixes are the extensions of the files the viewer opens, without
// their dots, in order.
func Suffixes() []string {
	var s []string
	for ext := range Exts {
		s = append(s, ext[1:])
	}
	for ext := range textExts {
		s = append(s, ext[1:])
	}
	slices.Sort(s)
	return s
}

// Sniff is KindOf, or Text for a file whose name says nothing but whose
// start reads as text (README, Makefile, LICENSE).
func Sniff(path string) Kind {
	if k := KindOf(path); k != None {
		return k
	}
	f, err := os.Open(path)
	if err != nil {
		return None
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := io.ReadFull(f, buf)
	if n > 0 && looksText(buf[:n], n == len(buf)) {
		return Text
	}
	return None
}

// looksText: no NUL bytes, and valid UTF-8 but maybe for a character
// cut at the end of a part read.
func looksText(b []byte, cut bool) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	if cut {
		for i := 0; i < utf8.UTFMax && len(b) > 0 && !utf8.Valid(b); i++ {
			b = b[:len(b)-1]
		}
	}
	return utf8.Valid(b)
}

// MaxText is the largest text file the viewer reads whole.
const MaxText = 16 << 20

// ErrNotText is the answer for a file that does not read as UTF-8 text.
var ErrNotText = errors.New("not UTF-8 text")

// ReadText reads a text file to show, a byte order mark left out.
func ReadText(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Size() > MaxText {
		return "", fmt.Errorf("larger than %s", Size(MaxText))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if !looksText(b, false) {
		return "", ErrNotText
	}
	return string(b), nil
}

// TextStats are a text's lines, words and characters.
func TextStats(s string) (lines, words, chars int) {
	if s == "" {
		return 0, 0, 0
	}
	lines = strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		lines++
	}
	return lines, len(strings.Fields(s)), utf8.RuneCountInString(s)
}
