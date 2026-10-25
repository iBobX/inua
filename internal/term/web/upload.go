package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/lyricat/inua/internal/term/host"
)

// maxUpload is the most a file from the page may be: it is kept in the
// session's runtime folder, which is in memory.
const maxUpload = 100 << 20

// upload keeps a file the page sends (POST, the file as the body, its name
// as name=…) in host.PasteDir, as the window keeps a pasted image, and
// answers with its path, which the page types in the pane shown.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST a file", http.StatusMethodNotAllowed)
		return
	}
	if r.ContentLength > maxUpload {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		return
	}
	dir := host.PasteDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f, err := createFresh(dir, uploadName(r.URL.Query().Get("name")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, maxUpload))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]string{"path": f.Name()})
}

// uploadName is the phone's name for a file, made safe to keep: its last
// part only, without control characters, and not too long for a path.
func uploadName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if name == "" || name == "." || name == ".." || name == "/" {
		return "file"
	}
	if r := []rune(name); len(r) > 100 {
		ext := filepath.Ext(name)
		if len([]rune(ext)) > 20 {
			ext = ""
		}
		name = string(r[:100-len([]rune(ext))]) + ext
	}
	return name
}

// createFresh makes a file of name in dir, after the time, as pasted images
// are named: two files of one name, or sent at once, never meet.
func createFresh(dir, name string) (*os.File, error) {
	stamp := time.Now().Format("2006-01-02-150405.000")
	for i := 1; i < 100; i++ {
		path := filepath.Join(dir, stamp+"-"+name)
		if i > 1 {
			path = filepath.Join(dir, fmt.Sprintf("%s-%d-%s", stamp, i, name))
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
	return nil, fmt.Errorf("no free name for %s in %s", name, dir)
}
