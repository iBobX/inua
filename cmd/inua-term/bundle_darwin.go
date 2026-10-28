package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// bundleEnv readies GTK when inua-term runs from its macOS app bundle
// (scripts/macos-app.sh): the GSettings schemas, icons and image loaders
// it carries in Contents/Resources, and a UTF-8 locale, which an app
// opened from the Finder is not given. puregotk finds the libraries in
// Contents/Frameworks by itself. A variable already set is kept.
func bundleEnv() {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil || filepath.Base(filepath.Dir(exe)) != "MacOS" {
		return
	}
	// macOS before 10.9 gave a Finder-opened app its process serial number.
	for i, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-psn_") {
			os.Args = append(os.Args[:i+1], os.Args[i+2:]...)
			break
		}
	}
	contents := filepath.Dir(filepath.Dir(exe))
	res := filepath.Join(contents, "Resources")
	setDefault("GSETTINGS_SCHEMA_DIR", filepath.Join(res, "share", "glib-2.0", "schemas"))
	setDefault("XDG_DATA_DIRS", filepath.Join(res, "share"))
	// GIO's modules are Homebrew's, which the bundle does not carry.
	setDefault("GIO_MODULE_DIR", filepath.Join(res, "lib", "gio", "modules"))
	// The loaders' cache names them by path: the bundle's, wherever it is.
	if in, err := os.ReadFile(filepath.Join(res, "lib", "gdk-pixbuf-2.0", "loaders.cache.in")); err == nil {
		if dir, err := os.UserCacheDir(); err == nil {
			cache := filepath.Join(dir, "inua-term", "loaders.cache")
			out := bytes.ReplaceAll(in, []byte("@CONTENTS@"), []byte(contents))
			if old, err := os.ReadFile(cache); err != nil || !bytes.Equal(old, out) {
				if os.MkdirAll(filepath.Dir(cache), 0o700) == nil {
					os.WriteFile(cache, out, 0o600)
				}
			}
			setDefault("GDK_PIXBUF_MODULE_FILE", cache)
		}
	}
	if os.Getenv("LANG") == "" && os.Getenv("LC_ALL") == "" {
		lang := "en_US"
		if out, err := exec.Command("defaults", "read", "-g", "AppleLocale").Output(); err == nil {
			if l, _, _ := strings.Cut(strings.TrimSpace(string(out)), "@"); l != "" {
				lang = l
			}
		}
		os.Setenv("LANG", lang+".UTF-8")
	}
}

func setDefault(key, value string) {
	if os.Getenv(key) == "" {
		os.Setenv(key, value)
	}
}
