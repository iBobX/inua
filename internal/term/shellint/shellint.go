// Package shellint is inua-term's shell integration: small scripts that
// make bash, zsh and fish send the prompt marks (OSC 133) and their folder
// (OSC 7), injected when the host starts the shell, as Ghostty and kitty
// do: --rcfile for bash, ZDOTDIR for zsh, XDG_DATA_DIRS for fish. The
// user's own startup files still load. The marks tell inua-term what was
// typed at the prompt and which command ran, for remembering it.
package shellint

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:scripts
var scripts embed.FS

// Dir is where the scripts are written: $XDG_RUNTIME_DIR/inua/term-shell.
func Dir() string {
	run := os.Getenv("XDG_RUNTIME_DIR")
	if run == "" {
		run = os.TempDir()
	}
	return filepath.Join(run, "inua", "term-shell")
}

// Install writes the scripts into dir; a file that holds them already is
// left alone.
func Install(dir string) error {
	return fs.WalkDir(scripts, "scripts", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := scripts.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, strings.TrimPrefix(p, "scripts/"))
		if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, b) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o600)
	})
}

// Wrap is argv and env with the integration for the shell argv runs,
// whose scripts are in dir; any other program is left as it is.
func Wrap(argv, env []string, dir string) ([]string, []string) {
	if len(argv) == 0 {
		return argv, env
	}
	env = append([]string(nil), env...)
	switch filepath.Base(argv[0]) {
	case "bash":
		if len(argv) > 1 {
			return argv, env // a script, or options of the user's
		}
		return []string{argv[0], "--rcfile", filepath.Join(dir, "bash", "inua.bash")}, env
	case "zsh":
		if old, ok := lookup(env, "ZDOTDIR"); ok {
			env = set(env, "INUA_TERM_ZDOTDIR", old)
		}
		return argv, set(env, "ZDOTDIR", filepath.Join(dir, "zsh"))
	case "fish":
		data, ok := lookup(env, "XDG_DATA_DIRS")
		if !ok || data == "" {
			data = "/usr/local/share:/usr/share"
		}
		return argv, set(env, "XDG_DATA_DIRS", dir+":"+data)
	}
	return argv, env
}

func lookup(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// set is env with key set to value, once.
func set(env []string, key, value string) []string {
	out := env[:0:0]
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return append(out, key+"="+value)
}
