package files

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Archives: Compress puts Sources in one archive at Dest, of the format its
// name ends with, as Archive says (its compression, a password); Extract
// takes each archive of Sources apart in the folder Dest, opening one
// locked with Archive.Password. Both work at a hidden name beside the
// result and rename it into place at the end, so a failed or cancelled one
// leaves nothing half made, and undo trashes what they made. Go makes and
// reads Zip and tar; the 7z program makes 7z, and Zip with a password
// (AES-256), and reads 7z, rar and locked Zip. A password goes to it on
// its input, never on its command line, where any program could read it.

// Format is a kind of archive, by the ending of its name.
type Format struct {
	Ext      string // ".tar.gz"
	Name     string // as the user picks it
	Write    bool   // Files makes it too, not only takes it apart
	Needs7z  bool   // made by the 7z program
	Password bool   // can be locked with a password (made by 7z then)
}

// Formats are the archives Files knows, the ones it makes first.
var Formats = []Format{
	{Ext: ".zip", Name: "Zip", Write: true, Password: true},
	{Ext: ".7z", Name: "7z", Write: true, Needs7z: true, Password: true},
	{Ext: ".tar.gz", Name: "tar.gz", Write: true},
	{Ext: ".tar.xz", Name: "tar.xz", Write: true},
	{Ext: ".tar.zst", Name: "tar.zst", Write: true},
	{Ext: ".tar", Name: "tar"},
	{Ext: ".tgz", Name: "tgz"},
	{Ext: ".tar.bz2", Name: "tar.bz2"},
	{Ext: ".tbz2", Name: "tbz2"},
	{Ext: ".txz", Name: "txz"},
	{Ext: ".tzst", Name: "tzst"},
	{Ext: ".rar", Name: "rar"},
	{Ext: ".gz", Name: "gz"},
	{Ext: ".bz2", Name: "bz2"},
	{Ext: ".xz", Name: "xz"},
	{Ext: ".zst", Name: "zst"},
}

// Compression levels.
const (
	LevelNormal = iota
	LevelFast
	LevelBest
)

// ArchiveOptions are how an archive is made, or opened.
type ArchiveOptions struct {
	Level        int
	Password     string // locks a Zip or 7z made; opens one locked
	EncryptNames bool   // 7z: the names locked too, not only the contents
	NoHidden     bool   // hidden files (".name") inside folders left out
}

// Has7z says whether the 7z program is there, which 7z and locked
// archives need.
func Has7z() bool { return find7z() != "" }

func find7z() string {
	for _, name := range []string{"7z", "7zz", "7za"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
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

// ErrNeeds7z is an archive that needs the 7z program, which is not there.
var ErrNeeds7z = errors.New("this needs the 7z program (7zip)")

// ErrEncrypted is an archive locked with a password, none given.
var ErrEncrypted = errors.New("the archive is locked with a password")

// ErrWrongPassword is a password that does not open the archive.
var ErrWrongPassword = errors.New("that is not the archive's password")

// compress makes the archive dst of srcs.
func (r *Runner) compress(srcs []string, dst string, o ArchiveOptions) error {
	f, ok := ArchiveFormat(dst)
	if !ok || !f.Write {
		return fmt.Errorf("%s: Files does not make archives of this kind", filepath.Base(dst))
	}
	if o.Password != "" && !f.Password {
		return fmt.Errorf("%s archives cannot have a password", f.Name)
	}
	r.count(srcs)
	err := r.place(dst, false, func(tmp string) error {
		if f.Needs7z || o.Password != "" {
			return r.compress7z(srcs, tmp, f, o)
		}
		out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		bw := bufio.NewWriterSize(out, 1<<20)
		var werr error
		if f.Ext == ".zip" {
			werr = r.writeZip(bw, srcs, o)
		} else {
			werr = r.writeTar(bw, srcs, f.Ext, o)
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
// each source by its own name, at the archive's top. Without hidden, the
// hidden items inside them are left out.
func (r *Runner) walkSources(srcs []string, hidden bool, f func(p, name string, fi fs.FileInfo) error) error {
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
			if !hidden && p != src && strings.HasPrefix(fi.Name(), ".") {
				if fi.IsDir() {
					return filepath.SkipDir
				}
				return nil
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

// levels are each level's deflate (and gzip) level.
var levels = map[int]int{LevelNormal: flate.DefaultCompression, LevelFast: flate.BestSpeed, LevelBest: flate.BestCompression}

func (r *Runner) writeZip(w io.Writer, srcs []string, o ArchiveOptions) error {
	zw := zip.NewWriter(w)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, levels[o.Level])
	})
	err := r.walkSources(srcs, !o.NoHidden, func(p, name string, fi fs.FileInfo) error {
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

func (r *Runner) writeTar(w io.Writer, srcs []string, ext string, o ArchiveOptions) error {
	var cw io.WriteCloser
	var err error
	switch ext {
	case ".tar.gz":
		cw, err = gzip.NewWriterLevel(w, levels[o.Level])
	case ".tar.xz":
		// The dictionary is xz's level: larger finds more, with more memory.
		dict := map[int]int{LevelNormal: 8 << 20, LevelFast: 1 << 20, LevelBest: 64 << 20}[o.Level]
		cw, err = xz.WriterConfig{DictCap: dict}.NewWriter(w)
	case ".tar.zst":
		lvl := map[int]zstd.EncoderLevel{LevelNormal: zstd.SpeedDefault, LevelFast: zstd.SpeedFastest, LevelBest: zstd.SpeedBestCompression}[o.Level]
		cw, err = zstd.NewWriter(w, zstd.WithEncoderLevel(lvl))
	default:
		return fmt.Errorf("Files does not make %s archives", ext)
	}
	if err != nil {
		return err
	}
	tw := tar.NewWriter(cw)
	err = r.walkSources(srcs, !o.NoHidden, func(p, name string, fi fs.FileInfo) error {
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
func (r *Runner) extract(src, dir, password string) error {
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
		err = r.extract7z(src, tmp, password, fi.Size())
	default:
		err = r.extractGo(src, f.Ext, tmp)
		if errors.Is(err, ErrEncrypted) && f.Ext == ".zip" {
			// A locked Zip: 7z opens it, with its password.
			switch {
			case !Has7z():
				err = ErrNeeds7z
			case password != "":
				if err = emptyDir(tmp); err == nil {
					r.p.Bytes = base
					err = r.extract7z(src, tmp, password, fi.Size())
				}
			}
		}
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

// emptyDir removes what is in dir, keeping dir.
func emptyDir(dir string) error {
	es, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range es {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// extract7z takes an archive apart with 7z, its password (if any) given
// on 7z's input. size is the archive's, for the progress.
func (r *Runner) extract7z(src, tmp, password string, size int64) error {
	err := r.run7z("", password, size, "x", "-y", "-o"+tmp, "--", src)
	switch {
	case errors.Is(err, ErrWrongPassword) && password == "":
		return ErrEncrypted
	case err != nil:
		return err
	}
	return nil
}

// compress7z makes the archive tmp of srcs with 7z: a 7z, or a Zip locked
// with a password (AES-256). The items are named from the folder they
// share.
func (r *Runner) compress7z(srcs []string, tmp string, f Format, o ArchiveOptions) error {
	dir := commonDir(srcs)
	args := []string{"a", "-snl", "-mx=" + map[int]string{LevelNormal: "5", LevelFast: "1", LevelBest: "9"}[o.Level]}
	if f.Ext == ".zip" {
		args = append(args, "-tzip")
		if o.Password != "" {
			args = append(args, "-p", "-mem=AES256")
		}
	} else {
		args = append(args, "-t7z")
		if o.Password != "" {
			args = append(args, "-p")
			if o.EncryptNames {
				args = append(args, "-mhe=on")
			}
		}
	}
	if o.NoHidden {
		args = append(args, "-xr!.*")
	}
	args = append(args, "--", tmp)
	for _, s := range srcs {
		rel, err := filepath.Rel(dir, filepath.Clean(s))
		if err != nil {
			return err
		}
		args = append(args, rel)
	}
	return r.run7z(dir, o.Password, r.p.TotalBytes, args...)
}

// commonDir is the folder that holds every path.
func commonDir(paths []string) string {
	dir := filepath.Dir(filepath.Clean(paths[0]))
	for _, p := range paths[1:] {
		for d := filepath.Dir(filepath.Clean(p)); !strings.HasPrefix(d+"/", dir+"/") && dir != "/"; {
			dir = filepath.Dir(dir)
		}
	}
	return dir
}

// percent is 7z's progress, a percentage, as -bsp1 writes it.
var percent = regexp.MustCompile(`(\d+)%`)

// run7z runs 7z with args in dir: its password, if any, on its input (it
// asks there when an archive is locked), its progress read for the job's,
// of total bytes.
func (r *Runner) run7z(dir, password string, total int64, args ...string) error {
	bin := find7z()
	if bin == "" {
		return ErrNeeds7z
	}
	cmd := exec.CommandContext(r.ctx, bin, append([]string{"-bso0", "-bsp1", "-bse2"}, args...)...)
	cmd.Dir = dir
	if password != "" {
		cmd.Stdin = strings.NewReader(password + "\n")
	} // else no input: a locked archive fails at once, asking
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	base := r.p.Bytes
	buf := make([]byte, 4096)
	var seen []byte
	for {
		n, rerr := out.Read(buf)
		seen = append(seen, buf[:n]...)
		if m := percent.FindAllSubmatch(seen, -1); len(m) > 0 {
			if pc, err := strconv.Atoi(string(m[len(m)-1][1])); err == nil && total > 0 {
				r.p.Bytes = base + total*int64(min(pc, 100))/100
				r.report(false)
			}
			seen = seen[len(seen)-min(len(seen), 16):] // enough for a number cut in two
		}
		if rerr != nil {
			break
		}
	}
	err = cmd.Wait()
	if r.ctx.Err() != nil {
		return ErrCancelled
	}
	if err == nil {
		return nil
	}
	msg := stderr.String() + string(seen)
	switch {
	case strings.Contains(msg, "Wrong password"), strings.Contains(msg, "Cannot open encrypted archive"),
		strings.Contains(msg, "Enter password"),
		password == "" && strings.Contains(msg, "Break signaled"): // asked, and its input was empty
		return ErrWrongPassword
	}
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "ERRORS") {
			return fmt.Errorf("7z: %s", l)
		}
	}
	return fmt.Errorf("7z: %w", err)
}
