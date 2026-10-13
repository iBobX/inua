// Package clipboard keeps the text and image history the clipboard panel
// shows: `inua clipboard capture`, which wl-paste --watch runs for each
// selection, adds to it; the panel reads it and deletes from it.
package clipboard

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	// DefaultLimit is how many entries the history keeps unless the
	// settings say otherwise (clipboard.limit).
	DefaultLimit = 300
	// MaxText is the longest text kept: the history is one JSON file,
	// rewritten on every copy.
	MaxText = 1 << 20
	// MaxImage is the largest image kept, in its own file.
	MaxImage = 32 << 20
)

// Entry is a selection in the history.
type Entry struct {
	Type string    `json:"type"` // "text" or "image"
	Text string    `json:"text,omitempty"`
	MIME string    `json:"mime,omitempty"`
	Path string    `json:"path,omitempty"` // an image's file
	Time time.Time `json:"time,omitzero"`  // when it was copied
}

// HistoryPath is the history: $XDG_STATE_HOME/inua/clipboard-history.json
// (~/.local/state/…). Images are in clipboard-images next to it.
func HistoryPath() string {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(root, "inua", "clipboard-history.json")
}

// imageDir holds the images of the history at path.
func imageDir(path string) string { return filepath.Join(filepath.Dir(path), "clipboard-images") }

// Read is the history at path, newest first; none when there is none yet.
func Read(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func same(a, b Entry) bool {
	return a.Type == b.Type && a.Text == b.Text && a.Path == b.Path && a.MIME == b.MIME
}

// Update changes the history at path under a lock, keeps the newest limit
// entries (0: all), and replaces the file in one step, so a reader never
// sees half of it. Images no entry refers to any more are removed.
func Update(path string, limit int, change func([]Entry) []Entry) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	entries, err := Read(path)
	if err != nil {
		return err
	}
	entries = change(entries)
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".clipboard-history-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // a no-op once renamed
	defer f.Close()
	if _, err = f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	pruneImages(path, entries)
	return nil
}

// pruneImages removes the files in the history's image folder that no
// entry refers to: deleted, cleared, or past the limit.
func pruneImages(path string, entries []Entry) {
	dir := imageDir(path)
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, f := range files {
		p := filepath.Join(dir, f.Name())
		if f.Type().IsRegular() && !slices.ContainsFunc(entries, func(e Entry) bool { return e.Path == p }) {
			os.Remove(p)
		}
	}
}

// Size is the bytes the history at path takes: the file and its images.
func Size(path string) int64 {
	var n int64
	if info, err := os.Stat(path); err == nil {
		n = info.Size()
	}
	files, _ := os.ReadDir(imageDir(path))
	for _, f := range files {
		if info, err := f.Info(); err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
	}
	return n
}

// Add puts entry first in the history, dropping an equal older one.
func Add(path string, limit int, entry Entry) error {
	return Update(path, limit, func(entries []Entry) []Entry {
		return append([]Entry{entry}, slices.DeleteFunc(entries, func(old Entry) bool { return same(old, entry) })...)
	})
}

// Delete removes entry from the history.
func Delete(path string, entry Entry) error {
	return Update(path, 0, func(entries []Entry) []Entry {
		return slices.DeleteFunc(entries, func(old Entry) bool { return same(old, entry) })
	})
}

// Clear empties the history.
func Clear(path string) error { return Update(path, 0, func([]Entry) []Entry { return []Entry{} }) }

// SkipFirst has the next Capture of the history at path keep nothing:
// wl-paste --watch hands over what is on the clipboard when it starts, and
// that was copied while nothing was recording.
func SkipFirst(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path+".skip", nil, 0o600)
}

// Capture adds the selection on r, as wl-paste --watch hands it over, to
// the history at path, keeping limit entries. Sensitive selections (a
// password manager's, CLIPBOARD_STATE=sensitive), empty ones, text over
// MaxText and images over MaxImage are not kept. The bytes never pass
// through a shell or a command line.
func Capture(path string, limit int, r io.Reader) error {
	if os.Remove(path+".skip") == nil {
		return nil // see SkipFirst
	}
	switch os.Getenv("CLIPBOARD_STATE") {
	case "sensitive", "nil", "clear":
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxImage+1))
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > MaxImage {
		return nil
	}
	entry := Entry{Type: "text", Time: time.Now()}
	if mime := http.DetectContentType(data); strings.HasPrefix(mime, "image/") {
		dir := imageDir(path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		entry.Type, entry.MIME = "image", mime
		entry.Path = filepath.Join(dir, fmt.Sprintf("%x.%s", sha256.Sum256(data), strings.TrimPrefix(mime, "image/")))
		if err := os.WriteFile(entry.Path, data, 0o600); err != nil {
			return err
		}
	} else {
		if len(data) > MaxText || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return nil
		}
		entry.Text = string(data)
	}
	return Add(path, limit, entry)
}
