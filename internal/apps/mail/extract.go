package mail

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lyricat/inua/internal/apps/viewer"
)

// ErrNoText is an attachment whose text Mail cannot read: a picture, an
// archive, a file of a kind it does not know.
var ErrNoText = errors.New("no text to read in it")

// textMaxRunes caps what is read of an attachment for the assistant.
const textMaxRunes = 20000

// HasText says whether an attachment is of a kind whose text Mail reads.
func (a Attachment) HasText() bool { return a.kind() != "" }

func (a Attachment) kind() string {
	ext := strings.ToLower(filepath.Ext(a.Name))
	switch {
	case a.Type == "application/pdf" || ext == ".pdf":
		return "pdf"
	case ext == ".docx" || a.Type == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx"
	case ext == ".odt" || a.Type == "application/vnd.oasis.opendocument.text":
		return "odt"
	case a.Type == "text/html" || ext == ".html" || ext == ".htm":
		return "html"
	case a.Type == "text/calendar" || ext == ".ics":
		return ""
	case strings.HasPrefix(a.Type, "text/") || ext == ".txt" || ext == ".md" || ext == ".csv" || ext == ".json" || ext == ".log":
		return "text"
	}
	return ""
}

// Text reads an attachment's text, for the assistant: a PDF's pages
// (through the viewer's poppler), a Word or OpenDocument file's
// paragraphs, HTML as text, plain text as it is; at most textMaxRunes.
func (a Attachment) Text() (string, error) {
	var text string
	var err error
	switch a.kind() {
	case "pdf":
		text, err = pdfText(a.Data)
	case "docx":
		text, err = zipXMLText(a.Data, "word/document.xml", "</w:p>")
	case "odt":
		text, err = zipXMLText(a.Data, "content.xml", "</text:p>")
	case "html":
		text, _ = HTMLToText(string(a.Data))
	case "text":
		text = strings.ToValidUTF8(string(a.Data), "�")
	default:
		return "", ErrNoText
	}
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(cleanText(text))
	if text == "" {
		return "", ErrNoText // a scan: pictures of pages
	}
	if utf8.RuneCountInString(text) > textMaxRunes {
		text = string([]rune(text)[:textMaxRunes]) + "\n…"
	}
	return text, nil
}

// pdfText reads a PDF's pages, the first 40, from a copy only the user
// can read, removed after.
func pdfText(data []byte) (string, error) {
	if err := viewer.PDFAvailable(); err != nil {
		return "", err
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	f, err := os.CreateTemp(dir, "inua-mail-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	doc, err := viewer.OpenPDF(f.Name())
	if err != nil {
		return "", err
	}
	defer doc.Close()
	var b strings.Builder
	for i := 0; i < doc.Pages() && i < 40; i++ {
		b.WriteString(doc.PageText(i))
		b.WriteString("\n\n")
		if b.Len() > textMaxRunes*4 {
			break
		}
	}
	return b.String(), nil
}

var xmlTag = regexp.MustCompile(`<[^>]+>`)

// zipXMLText reads the text of an XML file in a zip (a .docx or .odt),
// a line for each paragraph.
func zipXMLText(data []byte, name, paragraph string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		x, err := io.ReadAll(io.LimitReader(rc, 32<<20))
		if err != nil {
			return "", err
		}
		s := strings.ReplaceAll(string(x), paragraph, "\n")
		s = xmlTag.ReplaceAllString(s, "")
		return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'").Replace(s), nil
	}
	return "", ErrNoText
}

// AttachmentPrompt asks for an attachment's gist: what it is, the facts
// that matter, what it asks of the reader.
func AttachmentPrompt(name, text string, about Said, lang string) string {
	return `Sum up a file attached to an e-mail, for the person who received it. The file is "` + name + `", sent by ` + about.From.String() + `.

Write two to four plain sentences: what the file is, the figures, dates and names that matter in it, and anything it asks the reader to do (with its deadline). No headings, no markdown. Answer in the language whose code is "` + langOr(lang) + `".

The e-mail it came with:
` + clip(about.Text, 1500) + `

The file's text:
` + text
}
