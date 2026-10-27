package web

import (
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// entry is a file or a folder in a listing.
type entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir,omitempty"`
	Size int64  `json:"size"`
}

// files lists a folder (dir=…, absolute) for the page to take files from:
// its folders, then its files, each by name; hidden ones are left out.
// A link is listed as what it leads to.
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if !filepath.IsAbs(dir) {
		http.Error(w, "dir= is not absolute", http.StatusBadRequest)
		return
	}
	dir = filepath.Clean(dir)
	all, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, err.Error(), statusOf(err))
		return
	}
	list := []entry{}
	for _, d := range all {
		if strings.HasPrefix(d.Name(), ".") {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, d.Name()))
		if err != nil {
			continue
		}
		e := entry{Name: d.Name(), Dir: info.IsDir()}
		if !e.Dir && !info.Mode().IsRegular() {
			continue
		}
		if !e.Dir {
			e.Size = info.Size()
		}
		list = append(list, e)
	}
	slices.SortFunc(list, func(a, b entry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"dir": dir, "entries": list})
}

// download sends a file (path=…, absolute) for the phone to keep, under
// its own name; a range of it, for a download that goes on after a break.
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if !filepath.IsAbs(path) {
		http.Error(w, "path= is not absolute", http.StatusBadRequest)
		return
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		http.Error(w, err.Error(), statusOf(err))
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !info.Mode().IsRegular() {
		http.Error(w, "not a file", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name()}))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func statusOf(err error) int {
	switch {
	case os.IsNotExist(err):
		return http.StatusNotFound
	case os.IsPermission(err):
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}
