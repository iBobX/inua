package pick

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

type pattern struct {
	Kind  uint32
	Value string
}

type filter struct {
	Name     string
	Patterns []pattern
}

func path(s string) dbus.Variant { return dbus.MakeVariant(append([]byte(s), 0)) }

// variants passes the options through D-Bus's encoding, as the portal
// sends them, so they come back typed as godbus decodes them.
func variants(t *testing.T, opts map[string]dbus.Variant) map[string]dbus.Variant {
	t.Helper()
	msg := &dbus.Message{Type: dbus.TypeMethodCall, Headers: map[dbus.HeaderField]dbus.Variant{
		dbus.FieldPath:      dbus.MakeVariant(dbus.ObjectPath("/x")),
		dbus.FieldMember:    dbus.MakeVariant("M"),
		dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf(opts)),
	}, Body: []any{opts}}
	var buf bytes.Buffer
	if err := msg.EncodeTo(&buf, binary.LittleEndian); err != nil {
		t.Fatal(err)
	}
	back, err := dbus.DecodeMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return back.Body[0].(map[string]dbus.Variant)
}

func TestFromOptions(t *testing.T) {
	images := filter{"Images", []pattern{{0, "*.png"}, {1, "image/*"}}}
	opts := variants(t, map[string]dbus.Variant{
		"accept_label":   dbus.MakeVariant("_Upload"),
		"multiple":       dbus.MakeVariant(true),
		"filters":        dbus.MakeVariant([]filter{{"PDF", []pattern{{0, "*.pdf"}}}, images}),
		"current_filter": dbus.MakeVariant(images),
		"current_folder": path("/tmp/x y"),
		"choices": dbus.MakeVariant([]struct {
			ID, Label string
			Options   []struct{ ID, Label string }
			Default   string
		}{
			{"encoding", "Encoding", []struct{ ID, Label string }{{"utf8", "UTF-8"}, {"latin1", "Latin-1"}}, "latin1"},
			{"strip", "Strip metadata", nil, ""},
		}),
		"wrong_type": dbus.MakeVariant(uint32(3)),
	})
	r := FromOptions(Open, "org.example.App", "", opts)
	if r.AcceptLabel != "Upload" || !r.Multiple || r.Directory || r.CurrentFolder != "/tmp/x y" {
		t.Errorf("request %+v", r)
	}
	if len(r.Filters) != 2 || r.Filter != 1 || !r.Filters[1].Patterns[1].MIME || r.Filters[1].Patterns[1].Value != "image/*" {
		t.Errorf("filters %+v, chosen %d", r.Filters, r.Filter)
	}
	if len(r.Choices) != 2 || r.Choices[0].Value != "latin1" || r.Choices[1].Value != "false" || len(r.Choices[0].Options) != 2 {
		t.Errorf("choices %+v", r.Choices)
	}
}

func TestFromOptionsEmpty(t *testing.T) {
	r := FromOptions(Save, "", "", nil)
	if r.Filter != -1 || len(r.Filters) != 0 || r.Heading(func(s string) string { return s }) != "Save" {
		t.Errorf("request %+v", r)
	}
	// A filter of the app's own, not in the list, is offered too.
	r = FromOptions(Open, "", "", variants(t, map[string]dbus.Variant{"current_filter": dbus.MakeVariant(filter{"Text", []pattern{{0, "*.txt"}}})}))
	if len(r.Filters) != 1 || r.Filter != 0 {
		t.Errorf("own filter: %+v", r)
	}
}

func TestFilterMatch(t *testing.T) {
	isA := func(mime, of string) bool {
		if strings.HasSuffix(of, "/*") {
			return strings.HasPrefix(mime, strings.TrimSuffix(of, "*"))
		}
		return mime == of || mime == "application/x-zerosize" && of == "text/plain" // a parent
	}
	f := Filter{Name: "x", Patterns: []Pattern{{Value: "*.JPG"}, {MIME: true, Value: "image/png"}, {MIME: true, Value: "text/*"}}}
	cases := []struct {
		name, mime string
		dir, want  bool
	}{
		{"photo.jpg", "image/jpeg", false, true}, // glob, case ignored
		{"shot.png", "image/png", false, true},
		{"notes.md", "text/markdown", false, true},
		{"song.mp3", "audio/mpeg", false, false},
		{"music", "inode/directory", true, true}, // folders always
	}
	for _, c := range cases {
		if got := f.Match(c.name, c.mime, c.dir, isA); got != c.want {
			t.Errorf("Match(%s, %s) = %v", c.name, c.mime, got)
		}
	}
	text := Filter{Patterns: []Pattern{{MIME: true, Value: "text/plain"}}}
	if !text.Match("empty", "application/x-zerosize", false, isA) {
		t.Error("a type deriving from the filter's not matched")
	}
}

func TestStartFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	downloads := filepath.Join(home, "Downloads")
	app, last, given := filepath.Join(home, "app"), filepath.Join(home, "last"), filepath.Join(home, "given")
	for _, d := range []string{downloads, app, last, given} {
		os.MkdirAll(d, 0o755)
	}
	m := LoadMemory(filepath.Join(home, "picker.json"))
	isDir := func(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }

	if got := (Request{Kind: Open}).StartFolder(m, isDir); got != home {
		t.Errorf("nothing known: %s, want home", got)
	}
	if got := (Request{Kind: Save}).StartFolder(m, isDir); got != downloads {
		t.Errorf("saving: %s, want Downloads", got)
	}
	m.Chose("other", last)
	if got := (Request{Kind: Open, AppID: "org.app"}).StartFolder(m, isDir); got != last {
		t.Errorf("opening: %s, want any app's last", got)
	}
	m.Chose("org.app", app)
	m.Chose("other", last)
	if got := (Request{Kind: Open, AppID: "org.app"}).StartFolder(m, isDir); got != app {
		t.Errorf("the app's: %s", got)
	}
	if got := (Request{Kind: Open, AppID: "org.app", CurrentFolder: given}).StartFolder(m, isDir); got != given {
		t.Errorf("the app's own folder: %s", got)
	}
	if got := (Request{Kind: Save, CurrentFile: filepath.Join(given, "a.txt"), CurrentFolder: app}).StartFolder(m, isDir); got != given {
		t.Errorf("the file's folder: %s", got)
	}
	if got := (Request{Kind: Open, CurrentFolder: filepath.Join(home, "gone")}).StartFolder(m, isDir); got != last {
		t.Errorf("a missing folder: %s, want the next", got)
	}
	// Read again from the file.
	if again := LoadMemory(filepath.Join(home, "picker.json")); again.ForApp("org.app") != app || again.Last() != last {
		t.Errorf("memory read again: %q %q", again.ForApp("org.app"), again.Last())
	}
}

func TestSaveNames(t *testing.T) {
	if n := Stem("report.final.pdf"); n != len("report.final") {
		t.Errorf("Stem = %d", n)
	}
	if n := Stem(".bashrc"); n != len(".bashrc") {
		t.Errorf("Stem(.bashrc) = %d", n)
	}
	if n := Stem("日本.txt"); n != 2 {
		t.Errorf("Stem counts runes: %d", n)
	}
	png := Filter{Patterns: []Pattern{{Value: "*.png"}}}
	jpg := Filter{Patterns: []Pattern{{Value: "*.jpg"}, {Value: "*.jpeg"}}}
	if got := SwapExtension("shot.png", png, jpg); got != "shot.jpg" {
		t.Errorf("swap = %s", got)
	}
	if got := SwapExtension("shot.tiff", png, jpg); got != "shot.tiff" {
		t.Errorf("an extension of no filter changed: %s", got)
	}
	if got := SwapExtension("shot.png", png, Filter{Patterns: []Pattern{{MIME: true, Value: "image/*"}}}); got != "shot.png" {
		t.Errorf("a filter without a glob changed it: %s", got)
	}
}

func TestTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "docs")
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	cases := []struct {
		typed, want string
		err         error
	}{
		{"notes.md", filepath.Join(dir, "notes.md"), nil},
		{"  notes.md ", filepath.Join(dir, "notes.md"), nil},
		{"sub/a.txt", filepath.Join(dir, "sub", "a.txt"), nil},
		{"../b.txt", filepath.Join(home, "b.txt"), nil},
		{"~/c.txt", filepath.Join(home, "c.txt"), nil},
		{"missing/a.txt", "", ErrNoFolder},
		{"sub", "", ErrIsFolder},
		{"", "", ErrNoName},
		{"sub/", "", ErrNoName},
	}
	for _, c := range cases {
		got, err := Target(dir, c.typed)
		if got != c.want || err != c.err {
			t.Errorf("Target(%q) = %q, %v; want %q, %v", c.typed, got, err, c.want, c.err)
		}
	}
}

func TestURIs(t *testing.T) {
	got := URIs([]string{"/home/a b/naïve #1.txt"})
	if got[0] != "file:///home/a%20b/na%C3%AFve%20%231.txt" {
		t.Errorf("URIs = %v", got)
	}
}

func TestSaveFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644)
	r := Request{Kind: SaveFiles, Files: []string{"a.txt", "/elsewhere/b.txt"}}
	if got := r.SaveFilesPaths(dir); !slices.Equal(got, []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}) {
		t.Errorf("paths %v", got)
	}
	if got := r.Taken(dir); !slices.Equal(got, []string{"b.txt"}) {
		t.Errorf("taken %v", got)
	}
}
