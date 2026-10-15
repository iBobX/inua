// Package files is the file manager's logic without GTK: reading folders,
// sorting and filtering them, places, mounts, the trash, operations on
// files with progress and undo, the clipboard's formats, searching and
// watching folders (docs/files.md).
package files

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Entry is one item of a folder.
type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode // of the item itself; a link's is ModeSymlink
	ModTime time.Time
	Dir     bool   // a folder, or a link to one
	Link    string // where a link points; empty for anything else
	Broken  bool   // a link whose target is missing
	Hidden  bool   // a dot file, or named in the folder's .hidden

	key string // Name folded for sorting and matching
	ext string // the extension, folded, without its dot
}

// Key is the name folded to lower case, as sorting and matching use it.
func (e *Entry) Key() string { return e.key }

// Ext is the extension in lower case without its dot; folders have none.
func (e *Entry) Ext() string { return e.ext }

func newEntry(name string) Entry {
	e := Entry{Name: name, key: fold(name), Hidden: strings.HasPrefix(name, ".")}
	if i := strings.LastIndexByte(e.key, '.'); i > 0 && i < len(e.key)-1 {
		e.ext = e.key[i+1:]
	}
	return e
}

// fold lowers a name, cheaply when it is ASCII.
func fold(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= utf8.RuneSelf {
			return strings.ToLower(s)
		}
		if 'A' <= c && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= utf8.RuneSelf {
					return strings.ToLower(s)
				}
				if 'A' <= b[j] && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// Stat reads one entry, as ReadDir would.
func Stat(path string) (Entry, error) {
	e := newEntry(filepath.Base(path))
	fi, err := os.Lstat(path)
	if err != nil {
		return e, err
	}
	fill(&e, path, fi)
	return e, nil
}

func fill(e *Entry, path string, fi fs.FileInfo) {
	e.Size, e.Mode, e.ModTime = fi.Size(), fi.Mode(), fi.ModTime()
	e.Dir = fi.IsDir()
	if fi.Mode()&fs.ModeSymlink != 0 {
		e.Link, _ = os.Readlink(path)
		if t, err := os.Stat(path); err == nil {
			e.Dir = t.IsDir()
			if !e.Dir {
				e.Size = t.Size()
			}
		} else {
			e.Broken = true
		}
	}
	if e.Dir {
		e.ext = ""
	}
}

// parallelFrom is the folder size from which ReadDir stats its entries
// on several goroutines: below it the goroutines cost more than they save.
const parallelFrom = 512

// ReadDir reads a folder's entries, unsorted. Entries that vanish while it
// reads are left out; one that cannot be stat'ed keeps its name.
func ReadDir(dir string) ([]Entry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil && len(names) == 0 {
		return nil, err
	}
	hidden := hiddenList(dir)
	out := make([]Entry, len(names))
	statRange := func(lo, hi int) {
		for i := lo; i < hi; i++ {
			e := newEntry(names[i])
			if hidden[e.Name] {
				e.Hidden = true
			}
			p := filepath.Join(dir, e.Name)
			if fi, err := os.Lstat(p); err == nil {
				fill(&e, p, fi)
			} else if os.IsNotExist(err) {
				e.Name = "" // gone already
			}
			out[i] = e
		}
	}
	if n := len(names); n < parallelFrom {
		statRange(0, n)
	} else {
		workers := min(runtime.GOMAXPROCS(0), 8)
		chunk := (n + workers - 1) / workers
		var wg sync.WaitGroup
		for lo := 0; lo < n; lo += chunk {
			wg.Go(func() { statRange(lo, min(lo+chunk, n)) })
		}
		wg.Wait()
	}
	kept := out[:0]
	for _, e := range out {
		if e.Name != "" {
			kept = append(kept, e)
		}
	}
	return kept, nil
}

// hiddenList is a folder's .hidden file: names to hide, one a line, as
// Nautilus reads it.
func hiddenList(dir string) map[string]bool {
	b, err := os.ReadFile(filepath.Join(dir, ".hidden"))
	if err != nil || len(b) > 1<<20 {
		return nil
	}
	m := map[string]bool{}
	for line := range strings.SplitSeq(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			m[line] = true
		}
	}
	return m
}

// Owner is the user and group that own path, by name when known.
func Owner(fi fs.FileInfo) (uid, gid uint32, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Uid, st.Gid, true
}

// Times are when a file was last read and its status changed, from its
// stat.
func Times(fi fs.FileInfo) (atime, ctime time.Time) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, time.Time{}
	}
	return time.Unix(st.Atim.Unix()), time.Unix(st.Ctim.Unix())
}
