package notify

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Keep is how many notifications the center holds; older ones fall off.
const Keep = 100

// Center is the notification center's state: the notifications waiting,
// newest first, and Focus. It is not safe for concurrent use; the shell
// keeps it on the main thread.
type Center struct {
	Items []Notification `json:"items"`
	Focus bool           `json:"focus"`
	// Seen is when the user last opened the center; newer items are
	// unread.
	Seen time.Time `json:"seen"`
}

// Policy is how the center treats a new notification.
type Policy struct {
	// CriticalInFocus lets critical notifications show banners in Focus.
	CriticalInFocus bool
}

// Post adds or replaces a notification and reports whether it should show
// as a banner. A replacement keeps its place if it was still in the center.
// Transient notifications only ever show as banners.
func (c *Center) Post(n Notification, p Policy) (banner bool) {
	if i := c.index(n.ID); i >= 0 {
		c.Items = slices.Delete(c.Items, i, i+1)
	}
	if !n.Transient {
		c.Items = slices.Insert(c.Items, 0, n)
		if len(c.Items) > Keep {
			c.Items = c.Items[:Keep]
		}
	}
	return !c.Focus || (n.Urgency == Critical && p.CriticalInFocus)
}

// Get returns the notification with id.
func (c *Center) Get(id uint32) (Notification, bool) {
	if i := c.index(id); i >= 0 {
		return c.Items[i], true
	}
	return Notification{}, false
}

// Remove drops a notification, reporting whether it was there.
func (c *Center) Remove(id uint32) bool {
	i := c.index(id)
	if i < 0 {
		return false
	}
	c.Items = slices.Delete(c.Items, i, i+1)
	return true
}

// ClearApp drops an app's notifications and returns their IDs.
func (c *Center) ClearApp(key string) []uint32 {
	var ids []uint32
	c.Items = slices.DeleteFunc(c.Items, func(n Notification) bool {
		if n.Key() == key {
			ids = append(ids, n.ID)
			return true
		}
		return false
	})
	return ids
}

// ClearAll drops every notification and returns their IDs.
func (c *Center) ClearAll() []uint32 {
	ids := make([]uint32, len(c.Items))
	for i, n := range c.Items {
		ids[i] = n.ID
	}
	c.Items = nil
	return ids
}

// Unread counts notifications that arrived after the center was last
// opened.
func (c *Center) Unread() int {
	n := 0
	for _, it := range c.Items {
		if it.Time.After(c.Seen) {
			n++
		}
	}
	return n
}

// MaxID is the highest ID held, so new IDs can follow it.
func (c *Center) MaxID() uint32 {
	var m uint32
	for _, n := range c.Items {
		m = max(m, n.ID)
	}
	return m
}

// Group is one app's notifications, newest first.
type Group struct {
	Key   string
	Items []Notification
}

// Groups lists the notifications by app, the app with the newest
// notification first.
func (c *Center) Groups() []Group {
	var groups []Group
	at := map[string]int{}
	for _, n := range c.Items {
		k := n.Key()
		i, ok := at[k]
		if !ok {
			i = len(groups)
			at[k] = i
			groups = append(groups, Group{Key: k})
		}
		groups[i].Items = append(groups[i].Items, n)
	}
	return groups
}

func (c *Center) index(id uint32) int {
	return slices.IndexFunc(c.Items, func(n Notification) bool { return n.ID == id })
}

// StatePath is where the center is kept between runs:
// $XDG_STATE_HOME/inua/notifications.json.
func StatePath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "inua", "notifications.json")
}

// Load reads a saved center; a missing file is an empty center. Image data
// is not saved, so loaded notifications have none.
func Load(path string) (Center, error) {
	var c Center
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(data, &c)
	if len(c.Items) > Keep {
		c.Items = c.Items[:Keep]
	}
	return c, err
}

// Save writes the center to path in one step.
func (c *Center) Save(path string) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".notifications-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
