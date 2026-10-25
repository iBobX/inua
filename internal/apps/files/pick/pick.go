// Package pick is the picker's request without GTK (docs/picker.md): the
// options an app sends through the file chooser portal, its filters and
// what they match, the folder the picker starts in and the folders it
// remembers, the name a file is saved under, and the answer.
package pick

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/files"
)

// Kind is what the app asks for.
type Kind int

const (
	Open      Kind = iota // files, or a folder, to open
	Save                  // a place to save a file
	SaveFiles             // a folder to save several files in
)

// Request is a file chooser request.
type Request struct {
	Kind        Kind
	AppID       string
	Parent      string // the app's window: "wayland:HANDLE", "x11:XID" or ""
	Title       string // the app's, else ""
	AcceptLabel string // with mnemonics taken out
	Multiple    bool   // Open: several files
	Directory   bool   // Open: a folder, not files
	Filters     []Filter
	Filter      int // the filter chosen first, -1 for none
	Choices     []Choice

	CurrentName   string   // Save: the name proposed
	CurrentFolder string   // the folder to start in
	CurrentFile   string   // Save: the file being saved again
	Files         []string // SaveFiles: the names of the files
}

// Filter is a portal filter: a name and its patterns.
type Filter struct {
	Name     string
	Patterns []Pattern
}

// Pattern is a glob ("*.jpg") or a MIME type ("image/*").
type Pattern struct {
	MIME  bool
	Value string
}

// Choice is a check box (no options) or a list the app adds to the dialog.
type Choice struct {
	ID, Label string
	Options   [][2]string // id, label
	Value     string      // the option chosen, "true"/"false" for a check box
}

// Answer is what the user chose.
type Answer struct {
	Paths   []string
	Choices map[string]string // by choice ID
	Filter  int               // the filter chosen, -1 for none
}

// FromOptions reads a request from the portal's options. Paths come as
// byte strings ending in NUL.
func FromOptions(kind Kind, appID, title string, opts map[string]dbus.Variant) Request {
	r := Request{Kind: kind, AppID: appID, Title: title, Filter: -1}
	str := func(k string) string { s, _ := opts[k].Value().(string); return s }
	path := func(k string) string { b, _ := opts[k].Value().([]byte); return strings.TrimRight(string(b), "\x00") }
	r.AcceptLabel = strings.ReplaceAll(str("accept_label"), "_", "")
	r.Multiple, _ = opts["multiple"].Value().(bool)
	r.Directory, _ = opts["directory"].Value().(bool)
	r.CurrentName = str("current_name")
	r.CurrentFolder = path("current_folder")
	r.CurrentFile = path("current_file")
	if fs, ok := opts["files"].Value().([][]byte); ok {
		for _, f := range fs {
			r.Files = append(r.Files, strings.TrimRight(string(f), "\x00"))
		}
	}
	r.Filters = readFilters(opts["filters"].Value())
	if cur := readFilters([]any{opts["current_filter"].Value()}); len(cur) == 1 {
		r.Filter = slices.IndexFunc(r.Filters, func(f Filter) bool { return f.Name == cur[0].Name })
		if r.Filter < 0 { // a filter of its own: offered too
			r.Filters = append(r.Filters, cur[0])
			r.Filter = len(r.Filters) - 1
		}
	}
	if r.Filter < 0 && len(r.Filters) > 0 {
		r.Filter = 0
	}
	if cs, ok := opts["choices"].Value().([][]any); ok {
		for _, c := range cs {
			if ch, ok := readChoice(c); ok {
				r.Choices = append(r.Choices, ch)
			}
		}
	}
	return r
}

// readFilters reads a(sa(us)), from a list or a single (sa(us)).
func readFilters(v any) []Filter {
	var list []any
	switch t := v.(type) {
	case [][]any:
		for _, f := range t {
			list = append(list, f)
		}
	case []any:
		list = t
	}
	var out []Filter
	for _, item := range list {
		f, ok := item.([]any)
		if !ok || len(f) != 2 {
			continue
		}
		name, _ := f[0].(string)
		pats, _ := f[1].([][]any)
		flt := Filter{Name: name}
		for _, p := range pats {
			if len(p) != 2 {
				continue
			}
			kind, _ := p[0].(uint32)
			val, _ := p[1].(string)
			flt.Patterns = append(flt.Patterns, Pattern{MIME: kind == 1, Value: val})
		}
		if name != "" && len(flt.Patterns) > 0 {
			out = append(out, flt)
		}
	}
	return out
}

// readChoice reads (ssa(ss)s).
func readChoice(c []any) (Choice, bool) {
	if len(c) != 4 {
		return Choice{}, false
	}
	ch := Choice{}
	ch.ID, _ = c[0].(string)
	ch.Label, _ = c[1].(string)
	ch.Value, _ = c[3].(string)
	opts, _ := c[2].([][]any)
	for _, o := range opts {
		if len(o) == 2 {
			id, _ := o[0].(string)
			label, _ := o[1].(string)
			ch.Options = append(ch.Options, [2]string{id, label})
		}
	}
	if ch.ID == "" {
		return Choice{}, false
	}
	if len(ch.Options) == 0 && ch.Value != "true" {
		ch.Value = "false"
	}
	if len(ch.Options) > 0 && !slices.ContainsFunc(ch.Options, func(o [2]string) bool { return o[0] == ch.Value }) {
		ch.Value = ch.Options[0][0]
	}
	return ch, true
}

// Variant is the filter as the portal answers with it.
func (f Filter) Variant() dbus.Variant {
	pats := make([]struct {
		Kind  uint32
		Value string
	}, len(f.Patterns))
	for i, p := range f.Patterns {
		if p.MIME {
			pats[i].Kind = 1
		}
		pats[i].Value = p.Value
	}
	return dbus.MakeVariant(struct {
		Name     string
		Patterns []struct {
			Kind  uint32
			Value string
		}
	}{f.Name, pats})
}

// Match is whether a file is one the filter shows: its name matches a
// glob, ignoring case, or its MIME type is or derives from one of the
// filter's (isA: GIO's content type check). Folders always show.
func (f Filter) Match(name, mime string, dir bool, isA func(mime, of string) bool) bool {
	if dir {
		return true
	}
	lower := strings.ToLower(name)
	for _, p := range f.Patterns {
		if p.MIME {
			if mime != "" && (p.Value == "*" || p.Value == "*/*" || isA(mime, p.Value)) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(strings.ToLower(p.Value), lower); ok {
			return true
		}
	}
	return false
}

// Extension is the filter's first glob's extension (".png" from "*.png"),
// or "".
func (f Filter) Extension() string {
	for _, p := range f.Patterns {
		if !p.MIME && strings.HasPrefix(p.Value, "*.") && !strings.ContainsAny(p.Value[2:], "*?[") {
			return p.Value[1:]
		}
	}
	return ""
}

// Heading is the picker's title: the app's, else what it asks for.
func (r Request) Heading(t func(string) string) string {
	if r.Title != "" {
		return r.Title
	}
	switch {
	case r.Kind == Save:
		return t("Save")
	case r.Kind == SaveFiles:
		return t("Choose a folder to save in")
	case r.Directory:
		return t("Choose a folder")
	case r.Multiple:
		return t("Open files")
	}
	return t("Open a file")
}

// Action is the label of the button that chooses: the app's, else Open,
// Choose or Save.
func (r Request) Action(t func(string) string) string {
	if r.AcceptLabel != "" {
		return r.AcceptLabel
	}
	switch {
	case r.Kind != Open:
		return t("Save")
	case r.Directory:
		return t("Choose")
	}
	return t("Open")
}

// ChoosesFolder is whether the answer is a folder: Open with directory,
// and SaveFiles.
func (r Request) ChoosesFolder() bool { return r.Directory || r.Kind == SaveFiles }

// StartFolder is the folder the picker opens on: the file's being saved
// again, else the app's own, else the one last chosen for this app,
// Downloads for saving, the one last chosen by any app for opening, home.
// isDir says whether a folder is there to open.
func (r Request) StartFolder(m *Memory, isDir func(string) bool) string {
	var candidates []string
	if r.CurrentFile != "" {
		candidates = append(candidates, filepath.Dir(r.CurrentFile))
	}
	candidates = append(candidates, r.CurrentFolder, m.ForApp(r.AppID))
	if r.Kind == Open {
		candidates = append(candidates, m.Last())
	} else {
		candidates = append(candidates, cmp.Or(files.UserDirs()["DOWNLOAD"], filepath.Join(files.Home(), "Downloads")))
	}
	for _, c := range candidates {
		if c != "" && filepath.IsAbs(c) && isDir(c) {
			return c
		}
	}
	return files.Home()
}

// SaveName is the name the entry starts with: current_name, else the
// file's being saved again.
func (r Request) SaveName() string {
	if r.CurrentName != "" {
		return r.CurrentName
	}
	return filepath.Base(r.CurrentFile)
}

// Stem is how much of a name is selected for typing over: all of it but
// its extension, as renaming does. It counts runes.
func Stem(name string) int {
	base := name
	if ext := filepath.Ext(name); ext != "" && ext != name {
		base = strings.TrimSuffix(name, ext)
	}
	return len([]rune(base))
}

// SwapExtension changes name's extension to the new filter's when it has
// one of the old filter's, as the filter is changed while saving.
func SwapExtension(name string, old, cur Filter) string {
	to := cur.Extension()
	ext := filepath.Ext(name)
	if to == "" || ext == "" || strings.EqualFold(ext, to) {
		return name
	}
	known := false
	for _, p := range old.Patterns {
		if !p.MIME && strings.EqualFold(p.Value, "*"+ext) {
			known = true
		}
	}
	if !known {
		return name
	}
	return strings.TrimSuffix(name, ext) + to
}

// Errors in the name typed to save under.
var (
	ErrNoName   = errors.New("no name")
	ErrNoFolder = errors.New("no such folder")
	ErrIsFolder = errors.New("a folder has this name")
	ErrBadName  = errors.New("a name cannot hold that")
	ErrReadOnly = errors.New("the folder cannot be written to")
)

// Target is where saving puts the file: the name typed, in dir, or a path
// typed (~/x/notes.md, ../a.txt). exists reports whether something is
// already there, for the question; a missing folder is an error, not made.
func Target(dir, typed string) (path string, err error) {
	typed = strings.TrimSpace(typed)
	if typed == "" || typed == "." || typed == ".." {
		return "", ErrNoName
	}
	if strings.ContainsRune(typed, 0) {
		return "", ErrBadName
	}
	switch {
	case typed == "~" || strings.HasPrefix(typed, "~/"):
		path = filepath.Join(files.Home(), typed[1:])
	case filepath.IsAbs(typed):
		path = filepath.Clean(typed)
	default:
		path = filepath.Join(dir, typed)
	}
	if strings.HasSuffix(typed, "/") {
		return "", ErrNoName
	}
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || !fi.IsDir() {
		return "", ErrNoFolder
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return "", ErrIsFolder
	}
	return path, nil
}

// Writable is whether files can be made in dir.
func Writable(dir string) bool { return syscall.Access(dir, 2) == nil } // W_OK

// Exists is whether a path is taken.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// URIs are the paths as file:// URIs, escaped.
func URIs(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = (&url.URL{Scheme: "file", Path: p}).String()
	}
	return out
}

// SaveFilesPaths are the paths the files will have in dir.
func (r Request) SaveFilesPaths(dir string) []string {
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = filepath.Join(dir, filepath.Base(f))
	}
	return out
}

// Taken are the names of the files already in dir.
func (r Request) Taken(dir string) []string {
	var out []string
	for _, p := range r.SaveFilesPaths(dir) {
		if Exists(p) {
			out = append(out, filepath.Base(p))
		}
	}
	return out
}

// Memory is the folders chosen, by app and the last of any app:
// ~/.local/share/inua/picker.json.
type Memory struct {
	path string
	mu   sync.Mutex
	data memoryData
}

type memoryData struct {
	Apps map[string]string `json:"apps"`
	Last string            `json:"last"`
	// The picker's window, apart from Files': its size and whether the
	// details show.
	Width   int  `json:"width,omitempty"`
	Height  int  `json:"height,omitempty"`
	Details bool `json:"details,omitempty"`
}

// MemoryPath is where the memory is kept.
func MemoryPath() string { return filepath.Join(files.DataHome(), "inua", "picker.json") }

// LoadMemory reads the memory; a missing or broken file is an empty one.
func LoadMemory(path string) *Memory {
	m := &Memory{path: path}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &m.data)
	}
	return m
}

// ForApp is the folder last chosen for an app, or "".
func (m *Memory) ForApp(appID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if appID == "" {
		return ""
	}
	return m.data.Apps[appID]
}

// Last is the folder last chosen by any app, or "".
func (m *Memory) Last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data.Last
}

// View is the picker window's size, 900 × 600 at first, and whether its
// details show.
func (m *Memory) View() (width, height int, details bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	width, height = m.data.Width, m.data.Height
	if width <= 0 || height <= 0 {
		width, height = 900, 600
	}
	return width, height, m.data.Details
}

// SetSize records the picker window's size.
func (m *Memory) SetSize(width, height int) error {
	return m.change(func(d *memoryData) { d.Width, d.Height = width, height })
}

// SetDetails records whether the picker's details show.
func (m *Memory) SetDetails(on bool) error {
	return m.change(func(d *memoryData) { d.Details = on })
}

// Chose records the folder an app's choice was in, and writes the file.
func (m *Memory) Chose(appID, dir string) error {
	return m.change(func(d *memoryData) {
		if d.Apps == nil {
			d.Apps = map[string]string{}
		}
		if appID != "" {
			d.Apps[appID] = dir
		}
		d.Last = dir
	})
}

func (m *Memory) change(f func(*memoryData)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(&m.data)
	b, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("picker memory: %w", err)
	}
	return nil
}
