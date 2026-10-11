// Package store keeps the light apps' data (world clocks, alarms, weather
// places, the stock list) as JSON files in $XDG_DATA_HOME/inua.
package store

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Dir is $XDG_DATA_HOME/inua (~/.local/share/inua).
func Dir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(cmp.Or(home, os.TempDir()), ".local", "share")
	}
	return filepath.Join(base, "inua")
}

// Path is the file for name: Dir()/name.json.
func Path(name string) string { return filepath.Join(Dir(), name+".json") }

// Load reads name's file into v; a missing file leaves v as it is.
func Load(name string, v any) error {
	data, err := os.ReadFile(Path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Save writes v to name's file, replacing it whole.
func Save(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	path := Path(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
