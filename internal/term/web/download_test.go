package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A folder lists its folders, then its files, by name; hidden ones and
// what is neither are left out.
func TestFiles(t *testing.T) {
	s := server(t)
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "src"), 0o700)
	os.Mkdir(filepath.Join(dir, ".git"), 0o700)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bb"), 0o600)
	os.WriteFile(filepath.Join(dir, "A.png"), []byte("a"), 0o600)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("x"), 0o600)
	os.Symlink("src", filepath.Join(dir, "link"))
	os.Symlink("gone", filepath.Join(dir, "dangling"))
	r := get(t, s.URL+"/files?t=secret&dir="+url.QueryEscape(dir+"/"))
	if r.StatusCode != http.StatusOK {
		t.Fatalf("files: %d", r.StatusCode)
	}
	var got struct {
		Dir     string
		Entries []entry
	}
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := []entry{{Name: "link", Dir: true}, {Name: "src", Dir: true}, {Name: "A.png", Size: 1}, {Name: "b.txt", Size: 2}}
	if got.Dir != dir || !slices.Equal(got.Entries, want) {
		t.Errorf("files = %s %v, want %s %v", got.Dir, got.Entries, dir, want)
	}
	if r := get(t, s.URL+"/files?t=secret&dir="+url.QueryEscape(filepath.Join(dir, "none"))); r.StatusCode != http.StatusNotFound {
		t.Errorf("a folder not there: %d", r.StatusCode)
	}
}

// A file comes whole, or a range of it, named to be kept; a folder does
// not come.
func TestDownload(t *testing.T) {
	s := server(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "report 1.pdf")
	os.WriteFile(path, []byte("0123456789"), 0o600)
	r := get(t, s.URL+"/download?t=secret&path="+url.QueryEscape(path))
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusOK || string(body) != "0123456789" {
		t.Errorf("download: %d %q", r.StatusCode, body)
	}
	if cd := r.Header.Get("Content-Disposition"); cd != `attachment; filename="report 1.pdf"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	req, _ := http.NewRequest("GET", s.URL+"/download?t=secret&path="+url.QueryEscape(path), nil)
	req.Header.Set("Range", "bytes=4-")
	rr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Body.Close()
	if body, _ := io.ReadAll(rr.Body); rr.StatusCode != http.StatusPartialContent || string(body) != "456789" {
		t.Errorf("a range: %d %q", rr.StatusCode, body)
	}
	if r := get(t, s.URL+"/download?t=secret&path="+url.QueryEscape(dir)); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a folder: %d", r.StatusCode)
	}
	if r := get(t, s.URL+"/download?t=secret&path=report.pdf"); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a relative path: %d", r.StatusCode)
	}
}

// Without the token, nothing is listed or sent.
func TestDownloadRefused(t *testing.T) {
	s := server(t)
	for _, path := range []string{"/files?dir=/", "/download?path=/etc/hostname"} {
		if r := get(t, s.URL+path); r.StatusCode != http.StatusForbidden {
			t.Errorf("%s without the token: %d", path, r.StatusCode)
		}
	}
}
