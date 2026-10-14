package workshop

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed lib template template-js
var files embed.FS

// Languages are the templates New makes apps from.
var Languages = []string{"python", "js"}

// WriteLib writes the helpers inua gives apps (lib/python/inua.py…) to
// LibDir, those that changed: an app finds the ones of the inua running
// it.
func WriteLib() error {
	return fs.WalkDir(files, "lib", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(LibDir(), strings.TrimPrefix(p, "lib/"))
		if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, b) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, b, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, dst)
	})
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// New makes an app from the template in lang (python or js) in dir,
// which must be new or empty; name is its name.
func New(dir, name, lang string) (Manifest, error) {
	tmpl := "template"
	switch lang {
	case "", "python":
	case "js":
		tmpl = "template-js"
	default:
		return Manifest{}, fmt.Errorf("no template for %q: want python or js", lang)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return Manifest{}, fmt.Errorf("%s is not empty", dir)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(dir)
	}
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "app"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, err
	}
	err := fs.WalkDir(files, tmpl, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, ".toml") {
			b = []byte(strings.NewReplacer("{{ID}}", "dev.example."+slug, "{{NAME}}", strings.ReplaceAll(name, `"`, `'`)).Replace(string(b)))
		}
		return os.WriteFile(filepath.Join(dir, strings.TrimPrefix(p, tmpl+"/")), b, 0o644)
	})
	if err != nil {
		return Manifest{}, err
	}
	return Load(dir)
}

// isRemote says whether src is something git clones rather than a folder.
func isRemote(src string) bool {
	if _, err := os.Stat(src); err == nil {
		return false
	}
	return strings.Contains(src, "://") || strings.HasPrefix(src, "git@")
}

// ErrRefused is an install the user said no to.
var ErrRefused = errors.New("not installed")

// trash puts a file or folder in the trash; where there is none (a
// filesystem without one), it moves it to RemovedDir instead, kept. Tests
// replace it.
var trash = func(path string) error {
	out, err := exec.Command("gio", "trash", path).CombinedOutput()
	if err == nil {
		return nil
	}
	dst := filepath.Join(RemovedDir(), filepath.Base(path)+"-"+time.Now().Format("20060102-150405"))
	if mkErr := os.MkdirAll(RemovedDir(), 0o755); mkErr == nil {
		if mvErr := os.Rename(path, dst); mvErr == nil {
			return nil
		}
	}
	return fmt.Errorf("gio trash %s: %v %s", path, err, strings.TrimSpace(string(out)))
}

// RemovedDir keeps what could not go to the trash.
func RemovedDir() string { return filepath.Join(dataHome(), "inua", "workshop", "removed") }

// Install installs the app at src, a git URL (cloned) or a folder
// (copied), under AppsDir as its id, after ok agreed (it gets the
// manifest, and whether an installed app of that id is replaced, which
// then goes to the trash). It writes the app's desktop entry.
func Install(src string, ok func(m Manifest, replaces bool) bool) (Manifest, error) {
	if err := os.MkdirAll(AppsDir(), 0o755); err != nil {
		return Manifest{}, err
	}
	sweep()
	tmp, err := os.MkdirTemp(AppsDir(), ".incoming-")
	if err != nil {
		return Manifest{}, err
	}
	cleanup := func() {
		if _, err := os.Stat(tmp); err == nil {
			os.RemoveAll(tmp) // our own scratch folder, never the user's
		}
	}
	defer cleanup()
	if isRemote(src) {
		out, err := exec.Command("git", "clone", "--depth", "1", "--quiet", src, tmp+"/app").CombinedOutput()
		if err != nil {
			return Manifest{}, fmt.Errorf("git clone %s: %v %s", src, err, strings.TrimSpace(string(out)))
		}
	} else if err := copyDir(src, tmp+"/app"); err != nil {
		return Manifest{}, err
	}
	m, err := Load(tmp + "/app")
	if err != nil {
		return Manifest{}, err
	}
	dst := filepath.Join(AppsDir(), m.ID)
	_, statErr := os.Stat(dst)
	replaces := statErr == nil
	if !ok(m, replaces) {
		return Manifest{}, ErrRefused
	}
	if replaces {
		if err := trash(dst); err != nil {
			return Manifest{}, err
		}
	}
	if err := os.Rename(tmp+"/app", dst); err != nil {
		return Manifest{}, err
	}
	m, err = Load(dst)
	if err != nil {
		return Manifest{}, err
	}
	return m, WriteDesktop(m)
}

// sweep removes what installs cut short left behind a day ago or more:
// their own scratch folders, never an app.
func sweep() {
	entries, _ := os.ReadDir(AppsDir())
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && e.IsDir() && strings.HasPrefix(e.Name(), ".incoming-") && time.Since(info.ModTime()) > 24*time.Hour {
			os.RemoveAll(filepath.Join(AppsDir(), e.Name()))
		}
	}
}

// copyDir copies a folder, leaving out .git.
func copyDir(src, dst string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
}

// Remove puts an installed app's folder and desktop entry in the trash;
// its store stays.
func Remove(id string) (Manifest, error) {
	if !validID.MatchString(id) {
		return Manifest{}, fmt.Errorf("no workshop app %q", id)
	}
	dir := filepath.Join(AppsDir(), id)
	m, err := Load(dir)
	if err != nil {
		return Manifest{}, fmt.Errorf("no workshop app %q: %w", id, err)
	}
	if err := trash(dir); err != nil {
		return m, err
	}
	if p := DesktopPath(m); fileExists(p) {
		if err := trash(p); err != nil {
			return m, err
		}
	}
	return m, nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// DesktopPath is an installed app's desktop entry, named by its
// application ID, so the launcher, the dock and the compositor know it.
func DesktopPath(m Manifest) string {
	return filepath.Join(dataHome(), "applications", m.AppID()+".desktop")
}

// WriteDesktop writes the app's desktop entry.
func WriteDesktop(m Manifest) error {
	icon := m.IconPath()
	if icon == "" {
		icon = "application-x-executable"
	}
	clean := func(s string) string { return strings.NewReplacer("\n", " ", "\r", " ").Replace(s) }
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=%s
Exec=inua-apps workshop %s
Icon=%s
StartupWMClass=%s
Categories=Utility;
X-Inua-Workshop=%s
`, clean(m.Name), clean(m.Description), m.ID, icon, m.AppID(), m.ID)
	p := DesktopPath(m)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(entry), 0o644)
}
