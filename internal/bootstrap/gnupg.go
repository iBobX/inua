package bootstrap

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// GnuPGHome is gpg's directory: $GNUPGHOME, else ~/.gnupg.
func GnuPGHome() string {
	if d := os.Getenv("GNUPGHOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gnupg")
}

// Pinentry is the pinentry-program gpg-agent.conf under dir names, "" when
// it names none (gpg-agent then runs the system's).
func Pinentry(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "gpg-agent.conf"))
	if err != nil {
		return ""
	}
	prog := ""
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		if key, val, _ := strings.Cut(strings.TrimSpace(sc.Text()), " "); key == "pinentry-program" {
			prog = strings.TrimSpace(val)
		}
	}
	return prog
}

// SetPinentry makes gpg-agent ask for passphrases with program (inua's
// pinentry), in gpg-agent.conf under dir. The rest of the file is kept. A
// pinentry-program already naming another is left alone unless force is
// set; then that line is replaced. It reports whether the file changed.
func SetPinentry(dir, program string, force bool) (changed bool, msg string, err error) {
	path := filepath.Join(dir, "gpg-agent.conf")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, "", err
	}
	want := "pinentry-program " + program
	var lines []string
	found := false
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := sc.Text()
		key, val, _ := strings.Cut(strings.TrimSpace(line), " ")
		if key != "pinentry-program" {
			lines = append(lines, line)
			continue
		}
		found = true
		switch val = strings.TrimSpace(val); {
		case val == program:
			return false, "current  " + path + " (pinentry-program is inua-pinentry)", nil
		case !force:
			return false, fmt.Sprintf("skipped  %s (pinentry-program is %s; -force sets inua-pinentry)", path, val), nil
		}
		lines = append(lines, want)
	}
	if !found {
		lines = append(lines, "# Ask for passphrases in inua's dialog (falls back to the usual pinentry without inua).", want)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, "", err
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return false, "", err
	}
	return true, "wrote    " + path + " (pinentry-program inua-pinentry)", nil
}
