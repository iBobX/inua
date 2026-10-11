// Package bootstrap installs the sample configuration (`inua init`).
package bootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Outcome says what Install did with one file.
type Outcome int

const (
	Written  Outcome = iota // the file did not exist
	Skipped                 // it exists and differs; left alone
	Same                    // it exists with the sample's content
	Replaced                // it existed; the old one was saved as Backup
)

// Result is one file's outcome.
type Result struct {
	Path    string
	Outcome Outcome
	Backup  string // Replaced only
}

// Install copies the named files from fsys into dir, creating directories
// as needed. Existing files are left alone unless force is set; then a
// file that differs is renamed to <path>.bak (an older .bak is replaced)
// and the sample written in its place.
func Install(fsys fs.FS, names []string, dir string, force bool) ([]Result, error) {
	var results []Result
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return results, err
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		r := Result{Path: path}
		old, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			r.Outcome = Written
		case err != nil:
			return results, err
		case bytes.Equal(old, data):
			r.Outcome = Same
		case !force:
			r.Outcome = Skipped
		default:
			r.Outcome, r.Backup = Replaced, path+".bak"
			if err := os.Rename(path, r.Backup); err != nil {
				return results, err
			}
		}
		if r.Outcome == Written || r.Outcome == Replaced {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return results, err
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return results, err
			}
		}
		results = append(results, r)
	}
	return results, nil
}

func (r Result) String() string {
	switch r.Outcome {
	case Written:
		return "wrote    " + r.Path
	case Same:
		return "current  " + r.Path
	case Skipped:
		return "skipped  " + r.Path + " (exists and differs; -force replaces it, keeping a .bak)"
	case Replaced:
		return fmt.Sprintf("replaced %s (old one saved as %s)", r.Path, r.Backup)
	}
	return r.Path
}
