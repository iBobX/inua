package pass

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// StoreName is the app's file in the light apps' data folder.
const StoreName = "pass"

// Prefs are what the app keeps: the store in use and the folders added.
type Prefs struct {
	Store string   `json:"store,omitempty"`
	Added []string `json:"added,omitempty"`
}

// IsStore reports whether a folder is a password store: it has a .gpg-id.
func IsStore(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".gpg-id"))
	return err == nil
}

// Stores are the stores to choose from, without repeats: the one in
// $PASSWORD_STORE_DIR, ~/.password-store, then those added, each only if
// it is a store.
func Stores(p Prefs) []string {
	var dirs []string
	add := func(d string) {
		if d == "" {
			return
		}
		if abs, err := filepath.Abs(d); err == nil {
			d = abs
		}
		if IsStore(d) && !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	add(os.Getenv("PASSWORD_STORE_DIR"))
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".password-store"))
	}
	for _, d := range p.Added {
		add(d)
	}
	return dirs
}

// Current is the store in use: the one chosen if it is still there, else
// the first.
func Current(p Prefs, stores []string) string {
	if slices.Contains(stores, p.Store) {
		return p.Store
	}
	if len(stores) > 0 {
		return stores[0]
	}
	return ""
}

// ShortNames names stores for a menu: each folder's name, with as many
// of its parents as tell it from the others ("~" for the home folder).
func ShortNames(dirs []string) []string {
	home, _ := os.UserHomeDir()
	names := make([]string, len(dirs))
	for depth := 1; ; depth++ {
		for i, d := range dirs {
			names[i] = lastParts(d, home, depth)
		}
		unique := true
		for i := range names {
			if slices.Index(names, names[i]) != i {
				unique = false
			}
		}
		if unique || depth > 8 {
			return names
		}
	}
}

// lastParts is a path's last n parts; the home folder is "~".
func lastParts(dir, home string, n int) string {
	if home != "" && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	parts := strings.Split(filepath.ToSlash(dir), "/")
	if n < len(parts) {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "/")
}

// Tilde writes a path under the home folder with "~" for it.
func Tilde(dir string) string {
	home, _ := os.UserHomeDir()
	return lastParts(dir, home, 1<<16)
}
