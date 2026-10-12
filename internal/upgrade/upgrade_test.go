package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.0.2", "v0.0.2", 0, true},
		{"v0.0.2", "v0.0.10", -1, true},
		{"v0.1.0", "v0.0.9", 1, true},
		{"v1.0.0", "v0.9.9", 1, true},
		{"0.0.3-SNAPSHOT-abc", "v0.0.3", -1, true},
		{"v0.0.3", "0.0.3-rc1", 1, true},
		{"0.0.3-rc1", "0.0.3-rc2", -1, true},
		{"dev", "v0.0.2", 0, false},
		{"v0.0.2", "v0.2", 0, false},
	} {
		got, ok := Compare(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestVerify(t *testing.T) {
	data := []byte("archive")
	sum := sha256.Sum256(data)
	sums := []byte("00ff  other.tar.gz\n" + hex.EncodeToString(sum[:]) + "  inua_linux_amd64.tar.gz\n")
	if err := Verify(data, sums, "inua_linux_amd64.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if err := Verify([]byte("tampered"), sums, "inua_linux_amd64.tar.gz"); err == nil {
		t.Error("a changed archive passed")
	}
	if err := Verify(data, sums, "inua_linux_arm64.tar.gz"); err == nil {
		t.Error("an archive with no checksum passed")
	}
}

// release builds an archive laid out as GoReleaser's.
func release(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: "inua_linux_amd64/" + name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func TestInstall(t *testing.T) {
	archive := release(t, map[string]string{
		"inua":                            "new inua",
		"inua-shell":                      "new shell",
		"LICENSE":                         "license",
		"README.md":                       "readme",
		"scripts/inua-session":            "session",
		"config/apps/x.Clock.desktop":     "[Desktop Entry]",
		"config/apps/x.Clock.svg":         "<svg/>",
		"config/session/inua.desktop":     "session entry",
		"config/apps/../../../escape.txt": "no",
		"config/apps/nested/deep.desktop": "no",
		"../../inua-outside":              "no",
	})
	root := t.TempDir()
	bin, data := filepath.Join(root, "bin"), filepath.Join(root, "share")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "inua"), []byte("old inua"), 0o755)
	os.WriteFile(filepath.Join(bin, "mine"), []byte("not inua's"), 0o755)

	programs, err := Install(archive, bin, data)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(programs)
	if !slices.Equal(programs, []string{"inua", "inua-shell"}) {
		t.Errorf("programs = %v", programs)
	}
	for path, want := range map[string]string{
		"bin/inua":                           "new inua",
		"bin/inua-shell":                     "new shell",
		"bin/mine":                           "not inua's",
		"share/applications/x.Clock.desktop": "[Desktop Entry]",
		"share/icons/hicolor/scalable/apps/x.Clock.svg": "<svg/>",
	} {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want %q", path, b, err, want)
		}
	}
	if st, _ := os.Stat(filepath.Join(bin, "inua-shell")); st == nil || st.Mode().Perm() != 0o755 {
		t.Errorf("inua-shell mode = %v", st.Mode())
	}
	var left []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			left = append(left, rel)
		}
		return nil
	})
	slices.Sort(left)
	want := []string{"bin/inua", "bin/inua-shell", "bin/mine", "share/applications/x.Clock.desktop", "share/icons/hicolor/scalable/apps/x.Clock.svg"}
	if !slices.Equal(left, want) {
		t.Errorf("files = %v\nwant %v", left, want)
	}
}

func TestInstallNoPrograms(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(release(t, map[string]string{"README.md": "x"}), root, root); err == nil {
		t.Error("an archive without programs installed")
	}
}

func TestSource(t *testing.T) {
	archive := release(t, map[string]string{"inua": "new"})
	sum := sha256.Sum256(archive)
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v0.3.1", http.StatusFound)
	})
	mux.HandleFunc("/releases/download/v0.3.1/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(hex.EncodeToString(sum[:]) + "  inua_linux_amd64.tar.gz\n"))
	})
	mux.HandleFunc("/releases/download/v0.3.1/inua_linux_amd64.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := Source{Base: srv.URL, Client: srv.Client()}
	ctx := context.Background()

	tag, err := s.Latest(ctx)
	if err != nil || tag != "v0.3.1" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}
	got, err := s.Download(ctx, tag, "inua_linux_amd64")
	if err != nil || !bytes.Equal(got, archive) {
		t.Fatalf("Download = %d bytes, %v", len(got), err)
	}
	if _, err := s.Download(ctx, "v9.9.9", "inua_linux_amd64"); err == nil {
		t.Error("a missing release downloaded")
	}

	empty := httptest.NewServer(http.NotFoundHandler())
	defer empty.Close()
	if _, err := (Source{Base: empty.URL, Client: empty.Client()}).Latest(ctx); err == nil {
		t.Error("Latest found a release where there is none")
	}
}

func TestState(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	var st State
	if !st.Due(now) {
		t.Error("never checked, but not due")
	}
	st.Checked = now.Add(-23 * time.Hour)
	if st.Due(now) {
		t.Error("due after 23 hours")
	}
	if !st.Due(now.Add(time.Hour)) {
		t.Error("not due after a day")
	}
	if !st.Due(now.Add(-24 * time.Hour)) {
		t.Error("not due with the clock gone back")
	}

	if !st.Announce("0.0.2", "v0.0.3") {
		t.Error("a new release not announced")
	}
	for _, c := range [][2]string{{"0.0.3", "v0.0.3"}, {"0.0.4", "v0.0.3"}, {"dev", "v0.0.3"}} {
		if st.Announce(c[0], c[1]) {
			t.Errorf("Announce(%q, %q)", c[0], c[1])
		}
	}
	st.Notified = "v0.0.3"
	if st.Announce("0.0.2", "v0.0.3") {
		t.Error("a release announced twice")
	}
	if !st.Announce("0.0.2", "v0.0.4") {
		t.Error("the next release not announced")
	}
}

// A program still running from a binary that was replaced is listed; one
// left out, or not running, is not.
func TestStillOld(t *testing.T) {
	// A copy of sh, waiting for its sleep (sleep itself may be a
	// multi-call binary that would not run under another name).
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	data, err := os.ReadFile(sh)
	if err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "inua-keyring")
	if err := os.WriteFile(prog, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(prog, "-c", "sleep 30; :")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	installed := []string{"inua-keyring", "inua-apps"}
	if got := StillOld(dir, installed); len(got) != 0 {
		t.Errorf("before replacing: %v", got)
	}
	// Replaced as Install does: a new file renamed over it.
	if err := os.WriteFile(prog+".new", data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(prog+".new", prog); err != nil {
		t.Fatal(err)
	}
	if got := StillOld(dir, installed); !slices.Equal(got, []string{"inua-keyring"}) {
		t.Errorf("after replacing: %v", got)
	}
	if got := StillOld(dir, installed, "inua-keyring"); len(got) != 0 {
		t.Errorf("left out: %v", got)
	}
}
