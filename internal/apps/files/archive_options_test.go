package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An archive locked with a password: none asks for it, a wrong one is
// said to be wrong, the right one opens it.
func TestArchivePassword(t *testing.T) {
	if !Has7z() {
		t.Skip("7z not installed")
	}
	for _, ext := range []string{".zip", ".7z"} {
		t.Run(ext, func(t *testing.T) {
			root := t.TempDir()
			src := album(t, root)
			arch := filepath.Join(root, "album"+ext)
			o := ArchiveOptions{Password: "correct horse", EncryptNames: true}
			if _, err := run(t, Job{Kind: Compress, Sources: []string{src}, Dest: arch, Archive: o}, nil); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(root, "out")
			os.Mkdir(out, 0o755)
			if _, err := run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out}, nil); !errors.Is(err, ErrEncrypted) {
				t.Fatalf("no password: %v", err)
			}
			_, err := run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out, Archive: ArchiveOptions{Password: "wrong"}}, nil)
			if !errors.Is(err, ErrWrongPassword) {
				t.Fatalf("a wrong password: %v", err)
			}
			if es, _ := os.ReadDir(out); len(es) != 0 {
				t.Fatalf("left after the wrong password: %v", es)
			}
			if _, err := run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out, Archive: o}, nil); err != nil {
				t.Fatal(err)
			}
			sameAlbum(t, filepath.Join(out, "album"))
		})
	}
	// tar has no password.
	root := t.TempDir()
	src := album(t, root)
	if _, err := run(t, Job{Kind: Compress, Sources: []string{src}, Dest: filepath.Join(root, "a.tar.gz"),
		Archive: ArchiveOptions{Password: "x"}}, nil); err == nil {
		t.Fatal("a tar.gz with a password")
	}
}

// The password goes to 7z on its input, never on its command line, where
// any program can read it.
func TestPasswordNotOnTheCommandLine(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "log")
	// A 7z that keeps its arguments and input, and makes the archive asked
	// for (the argument after --).
	// Only the shell's own commands: PATH holds only this 7z.
	script := "#!/bin/sh\necho \"$@\" > " + log + ".args\n" +
		"while IFS= read -r l; do echo \"$l\"; done > " + log + ".stdin\n" +
		"while [ \"$1\" != -- ]; do shift; done\n: > \"$2\"\n"
	if err := os.WriteFile(filepath.Join(bin, "7z"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	root := t.TempDir()
	src := album(t, root)
	r := Runner{}
	if _, err := r.Run(context.Background(), Job{Kind: Compress, Sources: []string{src}, Dest: filepath.Join(root, "a.7z"),
		Archive: ArchiveOptions{Password: "s3cret", EncryptNames: true, Level: LevelBest, NoHidden: true}}); err != nil {
		t.Fatal(err)
	}
	args := read(t, log+".args")
	if strings.Contains(args, "s3cret") {
		t.Fatalf("the password on the command line: %s", args)
	}
	for _, want := range []string{"-p", "-mhe=on", "-mx=9", "-xr!.*", "-t7z"} {
		if !strings.Contains(args, want) {
			t.Errorf("no %s in %s", want, args)
		}
	}
	if read(t, log+".stdin") != "s3cret\n" {
		t.Fatalf("its input: %q", read(t, log+".stdin"))
	}
}

// Hidden files inside the folders are left out when asked; a hidden item
// chosen itself is not.
func TestArchiveNoHidden(t *testing.T) {
	for _, ext := range []string{".zip", ".tar.zst"} {
		root := t.TempDir()
		src := album(t, root)
		write(t, filepath.Join(src, ".secret"), "s")
		write(t, filepath.Join(src, ".git", "config"), "c")
		write(t, filepath.Join(root, ".env"), "e")
		arch := filepath.Join(root, "a"+ext)
		if _, err := run(t, Job{Kind: Compress, Sources: []string{src, filepath.Join(root, ".env")}, Dest: arch,
			Archive: ArchiveOptions{NoHidden: true, Level: LevelFast}}, nil); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(root, "out")
		os.Mkdir(out, 0o755)
		if _, err := run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out}, nil); err != nil {
			t.Fatal(err)
		}
		got := filepath.Join(out, "a")
		sameAlbum(t, filepath.Join(got, "album"))
		if exists(filepath.Join(got, "album", ".secret")) || exists(filepath.Join(got, "album", ".git")) {
			t.Errorf("%s: hidden files went in", ext)
		}
		if !exists(filepath.Join(got, ".env")) {
			t.Errorf("%s: the hidden item chosen was left out", ext)
		}
	}
}

// Every level makes an archive that opens.
func TestArchiveLevels(t *testing.T) {
	for _, ext := range []string{".zip", ".tar.gz", ".tar.xz", ".tar.zst"} {
		for _, level := range []int{LevelFast, LevelNormal, LevelBest} {
			root := t.TempDir()
			src := album(t, root)
			arch := filepath.Join(root, "album"+ext)
			if _, err := run(t, Job{Kind: Compress, Sources: []string{src}, Dest: arch, Archive: ArchiveOptions{Level: level}}, nil); err != nil {
				t.Fatal(ext, level, err)
			}
			out := filepath.Join(root, "out")
			os.Mkdir(out, 0o755)
			if _, err := run(t, Job{Kind: Extract, Sources: []string{arch}, Dest: out}, nil); err != nil {
				t.Fatal(ext, level, err)
			}
			sameAlbum(t, filepath.Join(out, "album"))
		}
	}
}
