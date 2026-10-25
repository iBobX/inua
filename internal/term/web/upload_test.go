package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/term/host"
)

func post(t *testing.T, url, body string) *http.Response {
	t.Helper()
	r, err := http.Post(url, "application/octet-stream", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Body.Close() })
	return r
}

// A file from the page is kept in the paste folder under its own name, and
// the answer is its path; two of one name are both kept.
func TestUpload(t *testing.T) {
	s := server(t)
	var paths []string
	for _, body := range []string{"one", "two"} {
		r := post(t, s.URL+"/upload?t=secret&name=photo%201.jpg", body)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("upload: %d", r.StatusCode)
		}
		var got struct{ Path string }
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(got.Path) != host.PasteDir() || !strings.HasSuffix(got.Path, "-photo 1.jpg") {
			t.Errorf("path = %q, want photo 1.jpg in %s", got.Path, host.PasteDir())
		}
		if b, err := os.ReadFile(got.Path); err != nil || string(b) != body {
			t.Errorf("%s holds %q (%v), want %q", got.Path, b, err, body)
		}
		paths = append(paths, got.Path)
	}
	if paths[0] == paths[1] {
		t.Errorf("both files went to %s", paths[0])
	}
}

// Without the token, or not a POST, nothing is kept.
func TestUploadRefused(t *testing.T) {
	s := server(t)
	if r := post(t, s.URL+"/upload?name=a", "x"); r.StatusCode != http.StatusForbidden {
		t.Errorf("without the token: %d", r.StatusCode)
	}
	if r := get(t, s.URL+"/upload?t=secret&name=a"); r.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("a GET: %d", r.StatusCode)
	}
	if entries, _ := os.ReadDir(host.PasteDir()); len(entries) != 0 {
		t.Errorf("kept %d files", len(entries))
	}
}

// The phone's name keeps to the paste folder: no folders, no control
// characters, never empty.
func TestUploadName(t *testing.T) {
	for name, want := range map[string]string{
		"photo.jpg":                       "photo.jpg",
		"../../.bashrc":                   ".bashrc",
		`C:\Users\me\a.txt`:               "a.txt",
		"a\nb\x00.txt":                    "ab.txt",
		"":                                "file",
		"..":                              "file",
		"/":                               "file",
		strings.Repeat("x", 300) + ".png": strings.Repeat("x", 96) + ".png",
	} {
		if got := uploadName(name); got != want {
			t.Errorf("uploadName(%q) = %q, want %q", name, got, want)
		}
	}
}
