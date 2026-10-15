// Package files is the file manager's logic without GTK: reading folders,
// sorting and filtering them, places, mounts, the trash, operations on
// files with progress and undo, the clipboard's formats, searching and
// watching folders (docs/files.md).
package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// Entry is one item of a folder.
type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode // of the item itself; a link's is ModeSymlink
	ModTime time.Time
	Dir     bool      // a folder, or a link to one
	Link    string    // where a link points; empty for anything else
	Broken  bool      // a link whose target is missing
	Hidden  bool      // a dot file, or named in the folder's .hidden
	Created time.Time // the birth time, where the filesystem keeps one
	Opened  time.Time // the access time
	UID     uint32
	GID     uint32

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
	return e, statEntry(&e, path)
}

// statEntry fills e from path itself (not what a link points to) with
// statx, which gives the birth time too, in the one call lstat would be;
// where statx is missing, from lstat.
func statEntry(e *Entry, path string) error {
	var st unix.Statx_t
	err := unix.Statx(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW|unix.AT_NO_AUTOMOUNT,
		unix.STATX_BASIC_STATS|unix.STATX_BTIME, &st)
	if errors.Is(err, unix.ENOSYS) {
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		fill(e, path, fi)
		return nil
	}
	if err != nil {
		return &fs.PathError{Op: "statx", Path: path, Err: err}
	}
	e.Size, e.Mode = int64(st.Size), statxMode(uint32(st.Mode))
	e.ModTime = time.Unix(st.Mtime.Sec, int64(st.Mtime.Nsec))
	e.Opened = time.Unix(st.Atime.Sec, int64(st.Atime.Nsec))
	if st.Mask&unix.STATX_BTIME != 0 && st.Btime.Sec != 0 {
		e.Created = time.Unix(st.Btime.Sec, int64(st.Btime.Nsec))
	}
	e.UID, e.GID = st.Uid, st.Gid
	e.Dir = e.Mode.IsDir()
	followLink(e, path)
	return nil
}

// statxMode turns a stat mode into Go's, as os does.
func statxMode(m uint32) fs.FileMode {
	mode := fs.FileMode(m & 0o777)
	switch m & unix.S_IFMT {
	case unix.S_IFBLK:
		mode |= fs.ModeDevice
	case unix.S_IFCHR:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case unix.S_IFDIR:
		mode |= fs.ModeDir
	case unix.S_IFIFO:
		mode |= fs.ModeNamedPipe
	case unix.S_IFLNK:
		mode |= fs.ModeSymlink
	case unix.S_IFSOCK:
		mode |= fs.ModeSocket
	}
	if m&unix.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if m&unix.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if m&unix.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}

// fill fills e from a FileInfo (lstat's, or a directory entry's).
func fill(e *Entry, path string, fi fs.FileInfo) {
	e.Size, e.Mode, e.ModTime = fi.Size(), fi.Mode(), fi.ModTime()
	e.Dir = fi.IsDir()
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		e.UID, e.GID = st.Uid, st.Gid
		e.Opened = time.Unix(st.Atim.Unix())
	}
	followLink(e, path)
}

// followLink reads where a link points, and takes the target's kind and
// size.
func followLink(e *Entry, path string) {
	if e.Mode&fs.ModeSymlink != 0 {
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
			if err := statEntry(&e, filepath.Join(dir, e.Name)); errors.Is(err, fs.ErrNotExist) {
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
