package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// album is a folder to archive: files, a folder in it, a program and a
// link.
func album(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "album")
	tree(t, dir, map[string]string{"a.txt": "alpha", "sub/b.txt": "beta", "sub/deep/c.txt": "gamma"})
	write(t, filepath.Join(dir, "run.sh"), "#!/bin/sh\n")
	os.Chmod(filepath.Join(dir, "run.sh"), 0o755)
	if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// sameAlbum checks that dir holds album's contents.
func sameAlbum(t *testing.T, dir string) {
	t.Helper()
	for p, want := range map[string]string{"a.txt": "alpha", "sub/b.txt": "beta", "sub/deep/c.txt": "gamma", "run.sh": "#!/bin/sh\n"} {
		if got := read(t, filepath.Join(dir, p)); got != want {
			t.Errorf("%s: %q", p, got)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "run.sh")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh is no longer a program: %v", fi.Mode())
	}
	if target, err := os.Readlink(filepath.Join(dir, "link")); err != nil || target != "a.txt" {
		t.Errorf("link: %q, %v", target, err)
	}
}

func TestArchiveRoundTrip(t *testing.T) {
	for _, f := range Formats {
		if !f.Write {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			root := t.TempDir()
			src := album(t, root)
			arch := filepath.Join(root, ArchiveName([]string{src}, f, "Archive"))
			if filepath.Base(arch) != "album"+f.Ext {
				t.Fatalf("named %s", filepath.Base(arch))
			}
			undo, err := run(t, Job{Kind: Compress, Sources: []string{src}, Dest: arch}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(undo.Created, []string{arch}) {
				t.Fatalf("undo: %+v", undo)
			}
			out := filepath.Join(root, "out")
			os.Mkdir(out, 0o755)
			undo, err = run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out}, nil)
			if err != nil {
				t.Fatal(err)
			}
			// One folder in the archive: that folder, not one around it.
			if !slices.Equal(undo.Created, []string{filepath.Join(out, "album")}) {
				t.Fatalf("undo: %+v", undo)
			}
			sameAlbum(t, filepath.Join(out, "album"))
			// Again: the name is taken, so a second one beside it.
			undo, err = run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out}, nil)
			if err != nil || !slices.Equal(undo.Created, []string{filepath.Join(out, "album (2)")}) {
				t.Fatalf("again: %+v, %v", undo, err)
			}
			if es, _ := os.ReadDir(out); len(es) != 2 {
				t.Fatalf("left in out: %v", es)
			}
		})
	}
}

// Many items at the archive's top come out in a folder named as it.
func TestExtractManyIntoAFolder(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]string{"one.txt": "1", "two.txt": "2"})
	arch := filepath.Join(root, "Archive.zip")
	srcs := []string{filepath.Join(root, "one.txt"), filepath.Join(root, "two.txt")}
	if got := ArchiveName(srcs, Formats[0], "Archive"); got != "Archive.zip" {
		t.Fatalf("named %s", got)
	}
	if _, err := run(t, Job{Kind: Compress, Sources: srcs, Dest: arch}, nil); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	os.Mkdir(out, 0o755)
	if _, err := run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out}, nil); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(out, "Archive", "one.txt")) != "1" || read(t, filepath.Join(out, "Archive", "two.txt")) != "2" {
		t.Fatal("not in a folder named as the archive")
	}
	if fi, _ := os.Stat(filepath.Join(out, "Archive")); fi.Mode().Perm() != 0o755 {
		t.Fatalf("the folder's mode: %v", fi.Mode())
	}
}

// One file compressed comes out as that file.
func TestExtractOneCompressedFile(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("log line\n"))
	gz.Close()
	os.WriteFile(filepath.Join(root, "app.log.gz"), buf.Bytes(), 0o644)
	undo, err := run(t, Job{Kind: Extract, Sources: []string{filepath.Join(root, "app.log.gz")}, Dest: root}, nil)
	if err != nil || read(t, filepath.Join(root, "app.log")) != "log line\n" {
		t.Fatalf("%+v, %v", undo, err)
	}
}

// Archives made to write outside the folder they are taken apart in are
// refused, and leave nothing behind.
func TestExtractStaysInside(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out")
	outside := filepath.Join(root, "outside")
	os.Mkdir(out, 0o755)
	os.Mkdir(outside, 0o755)

	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("../escaped.txt")
	w.Write([]byte("x"))
	zw.Close()
	os.WriteFile(filepath.Join(root, "slip.zip"), zbuf.Bytes(), 0o644)

	// A link to outside, then a file through it.
	var tbuf bytes.Buffer
	tw := tar.NewWriter(&tbuf)
	tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: outside})
	tw.WriteHeader(&tar.Header{Name: "link/pwned.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	tw.Write([]byte("x"))
	tw.Close()
	os.WriteFile(filepath.Join(root, "through.tar"), tbuf.Bytes(), 0o644)

	var abuf bytes.Buffer
	tw = tar.NewWriter(&abuf)
	tw.WriteHeader(&tar.Header{Name: "/tmp/absolute.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	tw.Write([]byte("x"))
	tw.Close()
	os.WriteFile(filepath.Join(root, "absolute.tar"), abuf.Bytes(), 0o644)

	for _, name := range []string{"slip.zip", "through.tar", "absolute.tar"} {
		if _, err := run(t, Job{Kind: Extract, Sources: []string{filepath.Join(root, name)}, Dest: out}, nil); err == nil {
			t.Errorf("%s was taken apart", name)
		}
	}
	if exists(filepath.Join(root, "escaped.txt")) || exists(filepath.Join(outside, "pwned.txt")) {
		t.Fatal("a file was written outside")
	}
	if es, _ := os.ReadDir(out); len(es) != 0 {
		t.Fatalf("left in out: %v", es)
	}
}

func TestArchiveFormats(t *testing.T) {
	for name, want := range map[string]string{"a.tar.gz": ".tar.gz", "A.TGZ": ".tgz", "b.gz": ".gz", "c.tar": ".tar", "d.ZIP": ".zip", "e.7z": ".7z"} {
		if f, ok := ArchiveFormat(name); !ok || f.Ext != want {
			t.Errorf("%s: %v %v", name, f, ok)
		}
	}
	for _, name := range []string{"notes.txt", ".zip", "tar"} {
		if IsArchive(name) {
			t.Errorf("%s is an archive", name)
		}
	}
	if ArchiveStem("photos.tar.zst") != "photos" || ArchiveStem("x.txt.gz") != "x.txt" {
		t.Error("stems")
	}
}

// A cancelled extraction leaves nothing, not even its folder at work.
func TestExtractCancelled(t *testing.T) {
	root := t.TempDir()
	src := album(t, root)
	arch := filepath.Join(root, "album.tar.gz")
	if _, err := run(t, Job{Kind: Compress, Sources: []string{src}, Dest: arch}, nil); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	os.Mkdir(out, 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	r := Runner{Report: func(Progress) { cancel() }}
	if _, err := r.Run(ctx, Job{Kind: Extract, Sources: []string{arch}, Dest: out}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err %v", err)
	}
	if es, _ := os.ReadDir(out); len(es) != 0 {
		t.Fatalf("left in out: %v", es)
	}
}

func Test7z(t *testing.T) {
	bin, err := exec.LookPath("7z")
	if err != nil {
		t.Skip("7z not installed")
	}
	root := t.TempDir()
	src := album(t, root)
	arch := filepath.Join(root, "album.7z")
	if out, err := exec.Command(bin, "a", "-snl", arch, src).CombinedOutput(); err != nil {
		t.Fatalf("7z a: %v\n%s", err, out)
	}
	out := filepath.Join(root, "out")
	os.Mkdir(out, 0o755)
	if _, err := run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out}, nil); err != nil {
		t.Fatal(err)
	}
	sameAlbum(t, filepath.Join(out, "album"))
}
