package shell

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// IconTheme is a freedesktop icon theme of symbolic SVGs that inua writes
// for its own process: the built-in Phosphor icons, with a user's set over
// them. It inherits the system's theme, so app icons and any icon the set
// lacks still come from there.
type IconTheme struct {
	Name     string
	Comment  string
	Inherits string              // the system's icon theme
	Builtin  []string            // the built-in icons' file names
	Read     func(string) string // reads a built-in icon by its file name
	Overlay  string              // a folder of SVGs replacing or adding to them, "" for none
}

// Write writes the theme into dir/<Name>, replacing what an earlier Write
// left there, unless that was the same: the icons are written again only
// when the set, the overlay or the theme's details change, so a start
// reads them from where the last one left them. A broken overlay leaves
// the built-in icons in place. The icons are written beside the old ones
// and put in their place, so another process reading them never finds
// them half written.
func (t IconTheme) Write(dir string) error {
	theme := filepath.Join(dir, t.Name)
	icons := filepath.Join(theme, "symbolic")
	files := map[string][]byte{}
	for _, name := range t.Builtin {
		if strings.HasSuffix(name, ".svg") {
			files[name] = []byte(t.Read(name))
		}
	}
	overlaid := 0
	if t.Overlay != "" {
		entries, err := os.ReadDir(t.Overlay)
		if err != nil {
			log.Printf("icons: %v", err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".svg") {
				continue
			}
			svg, err := os.ReadFile(filepath.Join(t.Overlay, e.Name()))
			if err != nil {
				log.Printf("icons: %v", err)
				continue
			}
			files[e.Name()] = svg
			overlaid++
		}
	}
	index := fmt.Sprintf(`[Icon Theme]
Name=%s
Comment=%s
Inherits=%s,hicolor
Directories=symbolic

[symbolic]
Size=16
MinSize=8
MaxSize=512
Type=Scalable
Context=Status
`, t.Name, t.Comment, t.Inherits)
	stamp := themeStamp(index, files)
	stampPath := filepath.Join(theme, ".written")
	if b, err := os.ReadFile(stampPath); err == nil && string(b) == stamp {
		if _, err := os.Stat(icons); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(theme, 0o700); err != nil {
		return err
	}
	tag := strconv.Itoa(os.Getpid())
	fresh := icons + ".new-" + tag
	os.RemoveAll(fresh)
	if err := os.Mkdir(fresh, 0o700); err != nil {
		return err
	}
	for name, svg := range files {
		if err := os.WriteFile(filepath.Join(fresh, name), svg, 0o644); err != nil {
			os.RemoveAll(fresh)
			return err
		}
	}
	// Icons of a set used before must not linger: the folder is replaced.
	old := icons + ".old-" + tag
	if err := os.Rename(icons, old); err != nil && !os.IsNotExist(err) {
		os.RemoveAll(fresh)
		return err
	}
	if err := os.Rename(fresh, icons); err != nil {
		// Another start put the same icons there first.
		os.RemoveAll(fresh)
	}
	os.RemoveAll(old)
	if err := writeReplacing(filepath.Join(theme, "index.theme"), index, tag); err != nil {
		return err
	}
	if t.Overlay != "" {
		log.Printf("icons: %d from %s", overlaid, t.Overlay)
	}
	return writeReplacing(stampPath, stamp, tag)
}

// themeStamp sums what a theme is written from.
func themeStamp(index string, files map[string][]byte) string {
	h := sha256.New()
	io.WriteString(h, index)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(h, "\x00%s\x00%d\x00", name, len(files[name]))
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeReplacing writes path through a file beside it, so it is never
// read half written.
func writeReplacing(path, content, tag string) error {
	tmp := path + ".new-" + tag
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
