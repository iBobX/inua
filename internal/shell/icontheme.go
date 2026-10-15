package shell

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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
// left there. A broken overlay leaves the built-in icons in place.
func (t IconTheme) Write(dir string) error {
	theme := filepath.Join(dir, t.Name)
	icons := filepath.Join(theme, "symbolic")
	// Icons of a set used before must not linger.
	if err := os.RemoveAll(icons); err != nil {
		return err
	}
	if err := os.MkdirAll(icons, 0o700); err != nil {
		return err
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
	if err := os.WriteFile(filepath.Join(theme, "index.theme"), []byte(index), 0o644); err != nil {
		return err
	}
	for _, name := range t.Builtin {
		if !strings.HasSuffix(name, ".svg") {
			continue
		}
		if err := os.WriteFile(filepath.Join(icons, name), []byte(t.Read(name)), 0o644); err != nil {
			return err
		}
	}
	if t.Overlay == "" {
		return nil
	}
	entries, err := os.ReadDir(t.Overlay)
	if err != nil {
		log.Printf("icons: %v", err)
		return nil
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		svg, err := os.ReadFile(filepath.Join(t.Overlay, e.Name()))
		if err != nil {
			log.Printf("icons: %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(icons, e.Name()), svg, 0o644); err != nil {
			return err
		}
		n++
	}
	log.Printf("icons: %d from %s", n, t.Overlay)
	return nil
}
