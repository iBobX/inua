package files

import (
	"slices"
	"time"

	"github.com/lyricat/inua/internal/apps/files/remote"
	"github.com/lyricat/inua/internal/apps/store"
)

// View is how a pane shows a folder.
type View string

const (
	ListView View = "list"
	GridView View = "grid"
)

// FolderView is what a folder remembers: its view and order.
type FolderView struct {
	View  View  `json:"view"`
	Order Order `json:"order"`
}

// Settings are the file manager's, kept in ~/.local/share/inua/files.json.
type Settings struct {
	Last     FolderView             `json:"last"`              // for folders not seen before
	Folders  map[string]*FolderView `json:"folders,omitempty"` // by path
	Sidebar  bool                   `json:"sidebar"`
	Details  bool                   `json:"details"`
	Dual     bool                   `json:"dual"`
	Zoom     int                    `json:"zoom"`              // -2 … 3: the rows' and icons' size
	Columns  []SortBy               `json:"columns"`           // the list's columns after the name, in order
	Remote   []remote.Disk          `json:"remote,omitempty"`  // the remote disks connected to, by name
	Servers  []string               `json:"servers,omitempty"` // before remote disks: the addresses used
	Jumps    Jumps                  `json:"jumps"`
	Width    int                    `json:"width,omitempty"`
	Height   int                    `json:"height,omitempty"`
	modified bool
}

// Columns are the list's columns after the name, in the order they come.
var Columns = []SortBy{BySize, ByModified, ByCreated, ByOpened, ByKind, ByPerms, ByOwner, ByGroup}

// DefaultColumns are those shown until others are chosen.
var DefaultColumns = []SortBy{BySize, ByModified, ByKind}

// ShowsColumn says whether the list shows column c.
func (s *Settings) ShowsColumn(c SortBy) bool { return slices.Contains(s.Columns, c) }

// ToggleColumn shows column c, or hides it; the columns keep Columns'
// order.
func (s *Settings) ToggleColumn(c SortBy) {
	on := !s.ShowsColumn(c)
	var out []SortBy
	for _, k := range Columns {
		if k == c && on || k != c && s.ShowsColumn(k) {
			out = append(out, k)
		}
	}
	s.Columns = out
	s.modified = true
}

// maxFolders is how many folders keep their own view; the oldest set
// are forgotten past it (they then take the last used).
const maxFolders = 500

// LoadSettings reads the settings, or the defaults.
func LoadSettings() *Settings {
	s := &Settings{Last: FolderView{View: ListView, Order: Order{By: ByName}}, Sidebar: true, Columns: DefaultColumns}
	store.Load("files", s)
	if s.Folders == nil {
		s.Folders = map[string]*FolderView{}
	}
	s.adoptServers()
	return s
}

// adoptServers makes the servers used before remote disks into disks: the
// addresses a remote disk can be at, each named as a new one would be.
func (s *Settings) adoptServers() {
	if len(s.Servers) == 0 {
		return
	}
	for _, uri := range s.Servers {
		a, err := remote.Parse(uri)
		if _, found := remote.Find(s.Remote, uri); err != nil || found {
			continue
		}
		s.Remote = append(s.Remote, remote.Disk{Name: remote.FreeName(a.Name(), s.Remote), Address: a.String()})
	}
	s.Servers = nil
	s.modified = true
}

// Save writes the settings when they changed.
func (s *Settings) Save() error {
	if !s.modified {
		return nil
	}
	s.modified = false
	return store.Save("files", s)
}

// Touch marks the settings as changed.
func (s *Settings) Touch() { s.modified = true }

// FolderView is how dir is shown: its own, else the last used.
func (s *Settings) FolderView(dir string) FolderView {
	if v, ok := s.Folders[dir]; ok {
		return *v
	}
	return s.Last
}

// SetFolderView records dir's view, and makes it the one new folders
// take.
func (s *Settings) SetFolderView(dir string, v FolderView) {
	s.Last = v
	s.Folders[dir] = &v
	if len(s.Folders) > maxFolders {
		for k := range s.Folders { // forget any one: the map has no order
			if k != dir {
				delete(s.Folders, k)
				break
			}
		}
	}
	s.modified = true
}

// Visited records a visit to dir in the jump list.
func (s *Settings) Visited(dir string) {
	s.Jumps.Visit(dir, time.Now())
	s.modified = true
}

// SaveDisk keeps a remote disk, in the place of the one of its name.
func (s *Settings) SaveDisk(d remote.Disk) {
	if i := slices.IndexFunc(s.Remote, func(x remote.Disk) bool { return x.Name == d.Name }); i >= 0 {
		s.Remote[i] = d
	} else {
		s.Remote = append(s.Remote, d)
	}
	s.modified = true
}

// ForgetDisk takes a remote disk off the list.
func (s *Settings) ForgetDisk(name string) {
	s.Remote = slices.DeleteFunc(s.Remote, func(x remote.Disk) bool { return x.Name == name })
	s.modified = true
}
