// Package pass reads and changes a password store (pass) for inua's Pass
// app: its entries, grouped by their first path segment into categories;
// an entry's password, fields, one-time password and notes; and the
// stores to choose from. Changes go through pass itself, so git, .gpg-id
// and re-encryption work as on the command line (docs/feat/pass.md).
package pass

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Store is a password store: a folder of .gpg files with a .gpg-id.
type Store struct{ Dir string }

// passTime is how long a pass command may take; decrypting waits for the
// passphrase, which the user may take a while to type.
const passTime = 5 * time.Minute

// Entries lists the store's entries by name ("work/mail/ann"), sorted;
// hidden folders (.git) are left out.
func (s Store) Entries() ([]string, error) {
	var names []string
	err := filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != s.Dir && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".gpg") {
			rel, err := filepath.Rel(s.Dir, path)
			if err != nil {
				return err
			}
			names = append(names, filepath.ToSlash(strings.TrimSuffix(rel, ".gpg")))
		}
		return nil
	})
	slices.Sort(names)
	return names, err
}

// Has reports whether an entry or a folder of that name is in the store.
func (s Store) Has(name string) bool {
	p := filepath.Join(s.Dir, filepath.FromSlash(name))
	if _, err := os.Stat(p + ".gpg"); err == nil {
		return true
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Show decrypts an entry.
func (s Store) Show(name string) (string, error) {
	out, err := s.run(nil, "show", "--", name)
	return string(out), err
}

// Insert writes an entry, replacing one of that name.
func (s Store) Insert(name, content string) error {
	_, err := s.run([]byte(content), "insert", "--multiline", "--force", "--", name)
	return err
}

// Remove deletes an entry.
func (s Store) Remove(name string) error {
	_, err := s.run(nil, "rm", "--force", "--", name)
	return err
}

// Move renames or moves an entry or a folder of entries; to must not
// exist.
func (s Store) Move(from, to string) error {
	if err := ValidName(to); err != nil {
		return err
	}
	if s.Has(to) {
		return fmt.Errorf("%s is already in the store", to)
	}
	_, err := s.run(nil, "mv", "--", from, to)
	return err
}

// ValidName reports whether a name can be an entry's or a folder's: not
// empty, relative, without empty, "." or ".." segments.
func ValidName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("the name is empty")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("%q: a name neither starts nor ends with /", name)
	}
	for seg := range strings.SplitSeq(name, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return fmt.Errorf("%q: each part needs a name, not starting with a dot", name)
		}
	}
	return nil
}

// run runs pass on the store, stdin given; the error carries pass's own
// words.
func (s Store) run(stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), passTime)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pass", args...)
	cmd.Env = append(os.Environ(), "PASSWORD_STORE_DIR="+s.Dir)
	var out, errb bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		return nil, fmt.Errorf("pass %s: %s", args[0], cmp.Or(lastLine(msg), err.Error()))
	}
	return out.Bytes(), nil
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

// Split splits an entry's name into its category, the first segment, and
// the rest; an entry at the top level has no category.
func Split(name string) (category, sub string) {
	if c, rest, ok := strings.Cut(name, "/"); ok {
		return c, rest
	}
	return "", name
}

// Join is Split's inverse.
func Join(category, sub string) string {
	if category == "" {
		return sub
	}
	return category + "/" + sub
}

// Category is a first segment and how many entries it holds.
type Category struct {
	Name  string // "" for the top level
	Count int
}

// Categories groups entries by their first segment: the top level first,
// then by name.
func Categories(entries []string) []Category {
	count := map[string]int{}
	for _, e := range entries {
		c, _ := Split(e)
		count[c]++
	}
	out := make([]Category, 0, len(count))
	for name, n := range count {
		out = append(out, Category{name, n})
	}
	slices.SortFunc(out, func(a, b Category) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Subpaths are the entries of a category, without it.
func Subpaths(entries []string, category string) []string {
	var out []string
	for _, e := range entries {
		if c, sub := Split(e); c == category {
			out = append(out, sub)
		}
	}
	return out
}
