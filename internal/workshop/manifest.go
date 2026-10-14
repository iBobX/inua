// Package workshop runs the light apps users write (docs/feat/workshop.md):
// a folder with a manifest and a program in any language, which describes
// its window in inua's building blocks over JSON-RPC on its stdin and
// stdout. This package has the manifest, the apps' folders, the protocol,
// the session with the program and installing; internal/appui draws the
// window.
package workshop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Protocol is the version of the protocol this inua speaks.
const Protocol = 1

// Uses are the functions an app must name in its manifest to call.
var Uses = []string{"notify", "open", "clipboard", "terminal", "run"}

// Categories group the apps in the dock's workshop drawer, in its order;
// an app without one, or with one this inua does not know, is "other".
var Categories = []string{"everyday", "tools", "network", "development", "media", "games", "system", "other"}

// Manifest is an app's app.toml.
type Manifest struct {
	ID          string   `toml:"id"`
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Version     string   `toml:"version"`
	Icon        string   `toml:"icon"`     // a file in the app's folder
	Category    string   `toml:"category"` // one of Categories
	Run         []string `toml:"run"`
	Uses        []string `toml:"uses"`
	Width       int      `toml:"width"`
	Height      int      `toml:"height"`

	Dir string `toml:"-"` // the app's folder
}

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// ManifestFile is the manifest's name in an app's folder.
const ManifestFile = "app.toml"

// Load reads and checks the manifest in dir.
func Load(dir string) (Manifest, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	md, err := toml.DecodeFile(filepath.Join(dir, ManifestFile), &m)
	if err != nil {
		return Manifest{}, err
	}
	m.Dir = dir
	var bad []string
	for _, k := range md.Undecoded() {
		bad = append(bad, k.String())
	}
	if len(bad) > 0 {
		return m, fmt.Errorf("%s: unknown keys: %s", ManifestFile, strings.Join(bad, ", "))
	}
	return m, m.check()
}

func (m *Manifest) check() error {
	switch {
	case !validID.MatchString(m.ID) || strings.Contains(m.ID, ".."):
		return fmt.Errorf("%s: id %q: want letters, digits, '.', '-' and '_', as dev.example.todo", ManifestFile, m.ID)
	case strings.TrimSpace(m.Name) == "":
		return fmt.Errorf("%s: no name", ManifestFile)
	case len(m.Run) == 0 || m.Run[0] == "":
		return fmt.Errorf("%s: no run: the command, as [\"python3\", \"main.py\"]", ManifestFile)
	}
	for _, u := range m.Uses {
		if !slices.Contains(Uses, u) {
			return fmt.Errorf("%s: uses %q: want some of %s", ManifestFile, u, strings.Join(Uses, ", "))
		}
	}
	if m.Icon != "" && (filepath.IsAbs(m.Icon) || strings.Contains(m.Icon, "..")) {
		return fmt.Errorf("%s: icon %q: want a file in the app's folder", ManifestFile, m.Icon)
	}
	if m.Width <= 0 {
		m.Width = 560
	}
	if m.Height <= 0 {
		m.Height = 640
	}
	return nil
}

// Group is the app's category, "other" for none or one not known (an app
// written for a newer inua).
func (m Manifest) Group() string {
	c := strings.ToLower(strings.TrimSpace(m.Category))
	if slices.Contains(Categories, c) {
		return c
	}
	return "other"
}

// May reports whether the app named use in its manifest.
func (m Manifest) May(use string) bool { return slices.Contains(m.Uses, use) }

// IconPath is the app's icon file; "" for none.
func (m Manifest) IconPath() string {
	if m.Icon == "" {
		return ""
	}
	p := filepath.Join(m.Dir, m.Icon)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// AppID is the window's application ID: the id's dots and dashes made
// underscores, under inua's.
func (m Manifest) AppID() string {
	return "io.github.lyricat.Inua.Workshop." + strings.NewReplacer(".", "_", "-", "_").Replace(m.ID)
}

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func stateHome() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state")
}

// AppsDir holds the installed apps, a folder each, named by id.
func AppsDir() string { return filepath.Join(dataHome(), "inua", "workshop", "apps") }

// LibDir holds the helpers inua gives apps.
func LibDir() string { return filepath.Join(dataHome(), "inua", "workshop", "lib") }

// DataDir is where an app's store is kept.
func DataDir(id string) string { return filepath.Join(stateHome(), "inua", "workshop", id) }

// Installed lists the installed apps by name; broken ones come with why.
func Installed() ([]Manifest, map[string]error) {
	entries, err := os.ReadDir(AppsDir())
	broken := map[string]error{}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		broken[AppsDir()] = err
	}
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") { // .incoming-…: an install under way, or cut short
			continue
		}
		dir := filepath.Join(AppsDir(), e.Name())
		m, err := Load(dir)
		if err != nil {
			broken[dir] = err
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, broken
}

// Find opens an app by its folder (one with an app.toml) or by the id of
// an installed one.
func Find(arg string) (Manifest, error) {
	if _, err := os.Stat(filepath.Join(arg, ManifestFile)); err == nil {
		return Load(arg)
	}
	if validID.MatchString(arg) {
		if _, err := os.Stat(filepath.Join(AppsDir(), arg, ManifestFile)); err == nil {
			return Load(filepath.Join(AppsDir(), arg))
		}
	}
	apps, _ := Installed()
	for _, m := range apps {
		if m.ID == arg {
			return m, nil
		}
	}
	return Manifest{}, fmt.Errorf("no workshop app %q: neither a folder with %s nor an installed app (inua workshop list)", arg, ManifestFile)
}
