package files

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Archives: Compress puts Sources in one archive at Dest, of the format its
// name ends with; Extract takes the archive Sources[0] apart in the folder
// Dest. Both work at a hidden name beside the result and rename it into
// place at the end, so a failed or cancelled one leaves nothing half
// made, and undo trashes what they made.

// Format is a kind of archive, by the ending of its name.
type Format struct {
	Ext   string // ".tar.gz"
	Name  string // as the user picks it
	Write bool   // Files makes it too, not only takes it apart
}

// Formats are the archives Files knows, the ones it makes first.
var Formats = []Format{
	{".zip", "Zip", true},
	{".tar.gz", "tar.gz", true},
	{".tar.xz", "tar.xz", true},
	{".tar.zst", "tar.zst", true},
	{".tar", "tar", false},
	{".tgz", "tgz", false},
	{".tar.bz2", "tar.bz2", false},
	{".tbz2", "tbz2", false},
	{".txz", "txz", false},
	{".tzst", "tzst", false},
	{".7z", "7z", false},
	{".rar", "rar", false},
	{".gz", "gz", false},
	{".bz2", "bz2", false},
	{".xz", "xz", false},
	{".zst", "zst", false},
}

// ArchiveFormat is the format name is of, if it is an archive.
func ArchiveFormat(name string) (Format, bool) {
	lower := strings.ToLower(name)
	for _, f := range Formats { // longer endings (.tar.gz) come before shorter (.gz)
		if strings.HasSuffix(lower, f.Ext) && len(lower) > len(f.Ext) {
			return f, true
		}
	}
	return Format{}, false
}

// IsArchive says whether name is an archive Files can take apart.
func IsArchive(name string) bool {
	_, ok := ArchiveFormat(name)
	return ok
}

// ArchiveStem is name without its archive's ending: "photos.tar.gz" is
// "photos".
func ArchiveStem(name string) string {
	if f, ok := ArchiveFormat(name); ok {
		return name[:len(name)-len(f.Ext)]
	}
	return name
}

// ArchiveName is the name to give an archive of paths in format f: the
// item's own name for one, else "Archive".
func ArchiveName(paths []string, f Format, archive string) string {
	stem := archive
	if len(paths) == 1 {
		stem = filepath.Base(paths[0])
		if fi, err := os.Lstat(paths[0]); err == nil && !fi.IsDir() {
			stem, _ = SplitExt(stem)
		}
	}
	return stem + f.Ext
}

// ErrNeeds7z is a 7z or rar archive without the 7z program to take it
// apart.
var ErrNeeds7z = errors.New("7z or rar archives need the 7z program (7zip)")

// ErrEncrypted is an archive locked with a password.
var ErrEncrypted = errors.New("the archive is locked with a password")

// compress makes the archive dst of srcs.
func (r *Runner) compress(srcs []string, dst string) error {
	f, ok := ArchiveFormat(dst)
	if !ok || !f.Write {
		return fmt.Errorf("%s: Files does not make archives of this kind", filepath.Base(dst))
	}
	r.count(srcs)
	err := r.place(dst, false, func(tmp string) error {
		out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		bw := bufio.NewWriterSize(out, 1<<20)
		var werr error
		if f.Ext == ".zip" {
			werr = r.writeZip(bw, srcs)
		} else {
			werr = r.writeTar(bw, srcs, f.Ext)
		}
		if werr == nil {
			werr = bw.Flush()
		}
		if cerr := out.Close(); werr == nil {
			werr = cerr
		}
		return werr
	})
	if err != nil {
		return err
	}
	r.undo.Created = append(r.undo.Created, dst)
	return nil
}

// walkSources calls f with each item below srcs, named as in the archive:
// each source by its own name, at the archive's top.
func (r *Runner) walkSources(srcs []string, f func(p, name string, fi fs.FileInfo) error) error {
	for _, src := range srcs {
		src = filepath.Clean(src)
		base := filepath.Dir(src)
		err := filepath.Walk(src, func(p string, fi fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if err := r.cancelled(); err != nil {
				return err
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			r.p.Current = p
			return f(p, filepath.ToSlash(rel), fi)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// copyIn copies a file's bytes into w, a chunk at a time, for progress
// and cancelling.
func (r *Runner) copyIn(w io.Writer, p string) error {
	in, err := os.Open(p)
	if err != nil {
		return err
	}
	defer in.Close()
	for {
		if err := r.cancelled(); err != nil {
			return err
		}
		n, err := io.CopyN(w, in, chunk)
		r.p.Bytes += n
		r.report(false)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (r *Runner) writeZip(w io.Writer, srcs []string) error {
	zw := zip.NewWriter(w)
	err := r.walkSources(srcs, func(p, name string, fi fs.FileInfo) error {
		h, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		h.Name = name
		switch {
		case fi.IsDir():
			h.Name += "/"
			_, err = zw.CreateHeader(h)
		case fi.Mode()&fs.ModeSymlink != 0:
			target, lerr := os.Readlink(p)
			if lerr != nil {
				return lerr
			}
			var ww io.Writer
			if ww, err = zw.CreateHeader(h); err == nil {
				_, err = io.WriteString(ww, target)
			}
		case fi.Mode().IsRegular():
			h.Method = zip.Deflate
			var ww io.Writer
			if ww, err = zw.CreateHeader(h); err == nil {
				err = r.copyIn(ww, p)
			}
		default:
			return nil // sockets, devices: not archived
		}
		r.step(0)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}

func (r *Runner) writeTar(w io.Writer, srcs []string, ext string) error {
	var cw io.WriteCloser
	var err error
	switch ext {
	case ".tar.gz":
		cw, err = gzip.NewWriterLevel(w, gzip.DefaultCompression)
	case ".tar.xz":
		cw, err = xz.NewWriter(w)
	case ".tar.zst":
		cw, err = zstd.NewWriter(w)
	default:
		return fmt.Errorf("Files does not make %s archives", ext)
	}
	if err != nil {
		return err
	}
	tw := tar.NewWriter(cw)
	err = r.walkSources(srcs, func(p, name string, fi fs.FileInfo) error {
		link := ""
		if fi.Mode()&fs.ModeSymlink != 0 {
			var err error
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		} else if !fi.IsDir() && !fi.Mode().IsRegular() {
			return nil
		}
		h, err := tar.FileInfoHeader(fi, link)
		if err != nil {
			return err
		}
		h.Name = name
		if fi.IsDir() {
			h.Name += "/"
		}
		h.Uname, h.Gname, h.Uid, h.Gid = "", "", 0, 0 // the archive is not about whose they were
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if fi.Mode().IsRegular() {
			if err := r.copyIn(tw, p); err != nil {
				return err
			}
		}
		r.step(0)
		return nil
	})
	if err == nil {
		err = tw.Close()
	}
	if cerr := cw.Close(); err == nil {
		err = cerr
	}
	return err
}

// --- taking apart ---

// entry is an item in an archive, as extracting needs it.
type entry struct {
	name    string // slash-separated, as the archive has it
	mode    fs.FileMode
	modTime time.Time
	link    string // a symlink's target, or a hard link's (an entry's name)
	hard    bool
	open    func() (io.ReadCloser, error)
}

// extract takes the archive src apart in the folder dir. An archive whose
// items all sit in one folder (or are one file) gives that item; any
// other gives a folder named as the archive.
func (r *Runner) extract(src, dir string) error {
	f, ok := ArchiveFormat(src)
	if !ok {
		return fmt.Errorf("%s is not an archive Files knows", filepath.Base(src))
	}
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	base := r.p.Bytes
	r.p.Current = src
	tmp, err := os.MkdirTemp(dir, ".inua-extract-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	switch f.Ext {
	case ".7z", ".rar":
		err = r.extract7z(src, tmp)
	default:
		err = r.extractGo(src, f.Ext, tmp)
	}
	if err != nil {
		return err
	}
	top, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	from, name := tmp, ArchiveStem(filepath.Base(src))
	isDir := true
	if len(top) == 1 {
		from, name = filepath.Join(tmp, top[0].Name()), top[0].Name()
		isDir = top[0].IsDir()
	} else {
		os.Chmod(tmp, 0o755) // MkdirTemp's 0700, for a folder of the user's
	}
	dst := FreeName(dir, name, isDir)
	if err := renameNoReplace(from, dst); err != nil {
		return err
	}
	r.undo.Created = append(r.undo.Created, dst)
	r.p.Bytes = base + fi.Size()
	r.step(0)
	return nil
}

// counting counts the bytes read through it into the progress.
type counting struct {
	r  io.Reader
	rr *Runner
}

func (c counting) Read(b []byte) (int, error) {
	if err := c.rr.cancelled(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(b)
	c.rr.p.Bytes += int64(n)
	c.rr.report(false)
	return n, err
}

// extractGo takes apart the archives Go reads: zip, tar and the
// compressions over it, and one file compressed.
func (r *Runner) extractGo(src, ext, tmp string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if ext == ".zip" {
		return r.extractZip(in, tmp)
	}
	raw := counting{bufio.NewReaderSize(in, 1<<20), r}
	var dec io.Reader
	var closeDec func()
	switch ext {
	case ".tar":
		dec = raw
	case ".tar.gz", ".tgz", ".gz":
		gz, err := gzip.NewReader(raw)
		if err != nil {
			return err
		}
		dec, closeDec = gz, func() { gz.Close() }
	case ".tar.bz2", ".tbz2", ".bz2":
		dec = bzip2.NewReader(raw)
	case ".tar.xz", ".txz", ".xz":
		x, err := xz.NewReader(raw)
		if err != nil {
			return err
		}
		dec = x
	case ".tar.zst", ".tzst", ".zst":
		z, err := zstd.NewReader(raw)
		if err != nil {
			return err
		}
		dec, closeDec = z, z.Close
	default:
		return fmt.Errorf("%s: Files does not take %s archives apart", filepath.Base(src), ext)
	}
	if closeDec != nil {
		defer closeDec()
	}
	switch ext {
	case ".gz", ".bz2", ".xz", ".zst":
		// One file compressed: its name is the archive's without the ending.
		name := ArchiveStem(filepath.Base(src))
		return r.write(tmp, entry{name: name, mode: 0o644, modTime: time.Now(),
			open: func() (io.ReadCloser, error) { return io.NopCloser(dec), nil }})
	}
	tr := tar.NewReader(dec)
	var links []entry
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		e := entry{name: h.Name, mode: h.FileInfo().Mode(), modTime: h.ModTime, link: h.Linkname}
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeDir:
			e.open = func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }
			if err := r.write(tmp, e); err != nil {
				return err
			}
		case tar.TypeSymlink:
			links = append(links, e)
		case tar.TypeLink:
			e.hard = true
			links = append(links, e)
		}
	}
	return r.writeLinks(tmp, links)
}

func (r *Runner) extractZip(in *os.File, tmp string) error {
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(in, fi.Size())
	if err != nil {
		return err
	}
	var links []entry
	for _, zf := range zr.File {
		if err := r.cancelled(); err != nil {
			return err
		}
		if zf.Flags&0x1 != 0 {
			return ErrEncrypted
		}
		e := entry{name: zf.Name, mode: zf.Mode(), modTime: zf.Modified, open: zf.Open}
		if e.mode&fs.ModeSymlink != 0 {
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			target, err := io.ReadAll(io.LimitReader(rc, 4096))
			rc.Close()
			if err != nil {
				return err
			}
			e.link = string(target)
			links = append(links, e)
		} else if err := r.write(tmp, e); err != nil {
			return err
		}
		r.p.Bytes += int64(zf.CompressedSize64)
		r.report(false)
	}
	return r.writeLinks(tmp, links)
}

// inside is an entry's path in tmp, or "" when its name would go outside
// it (an absolute name, or ".." past the top).
func inside(tmp, name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean("/" + name)
	if clean == "/" || strings.HasPrefix(name, "/") || slices.Contains(strings.Split(name, "/"), "..") {
		return ""
	}
	return filepath.Join(tmp, filepath.FromSlash(clean[1:]))
}

// write makes a folder or a file of an archive's entry. The links come
// last (writeLinks), so nothing is ever written through one.
func (r *Runner) write(tmp string, e entry) error {
	p := inside(tmp, e.name)
	if p == "" {
		return fmt.Errorf("%q: a name outside the archive's folder", e.name)
	}
	r.p.Current = e.name
	perm := e.mode.Perm()
	if e.mode.IsDir() || strings.HasSuffix(e.name, "/") {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
		os.Chmod(p, perm|0o700) // always the user's to open and change
		return nil
	}
	if !e.mode.IsRegular() {
		return nil // devices, pipes: not made
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	os.Remove(p) // a name given twice: the later one wins, as tar has it
	out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm|0o600)
	if err != nil {
		return err
	}
	rc, err := e.open()
	if err == nil {
		_, err = io.Copy(out, cancelReader{rc, r})
		rc.Close()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if !e.modTime.IsZero() {
		os.Chtimes(p, time.Time{}, e.modTime)
	}
	r.p.Items++
	return nil
}

// cancelReader stops when the job is cancelled; the archive's bytes are
// counted where they are read.
type cancelReader struct {
	r  io.Reader
	rr *Runner
}

func (c cancelReader) Read(b []byte) (int, error) {
	if err := c.rr.cancelled(); err != nil {
		return 0, err
	}
	return c.r.Read(b)
}

// writeLinks makes the archive's links, once its files are there: a hard
// link to an entry of the archive, a symbolic link as it is.
func (r *Runner) writeLinks(tmp string, links []entry) error {
	for _, e := range links {
		p := inside(tmp, e.name)
		if p == "" {
			return fmt.Errorf("%q: a name outside the archive's folder", e.name)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		// A link's folder must be a real folder in tmp, not one reached
		// through a link made just before.
		if throughLink(tmp, filepath.Dir(p)) {
			return fmt.Errorf("%q: a link inside a link", e.name)
		}
		os.Remove(p)
		if e.hard {
			target := inside(tmp, e.link)
			if target == "" {
				return fmt.Errorf("%q: a link to outside the archive", e.name)
			}
			if err := os.Link(target, p); err != nil {
				return err
			}
			continue
		}
		if err := os.Symlink(e.link, p); err != nil {
			return err
		}
	}
	return nil
}

// throughLink says whether dir, below tmp, is reached through a symbolic
// link.
func throughLink(tmp, dir string) bool {
	rel, err := filepath.Rel(tmp, dir)
	if err != nil || rel == "." {
		return err != nil
	}
	p := tmp
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		p = filepath.Join(p, part)
		if fi, err := os.Lstat(p); err != nil || fi.Mode()&fs.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// extract7z takes a 7z or rar archive apart with 7z.
func (r *Runner) extract7z(src, tmp string) error {
	bin := ""
	for _, name := range []string{"7z", "7zz", "7za"} {
		if p, err := exec.LookPath(name); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		return ErrNeeds7z
	}
	// -p with nothing: a locked archive fails instead of asking on a
	// terminal there is none of.
	cmd := exec.CommandContext(r.ctx, bin, "x", "-y", "-p", "-bd", "-bso0", "-o"+tmp, "--", src)
	out, err := cmd.CombinedOutput()
	if r.ctx.Err() != nil {
		return ErrCancelled
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "Wrong password") || strings.Contains(msg, "password") {
			return ErrEncrypted
		}
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("7z: %s", msg)
	}
	return nil
}
