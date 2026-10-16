package keyring

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Identity is who a process is, for trusting it with a secret
// (docs/feat/keyring-trust.md): its program, the script when the program
// is an interpreter, or its Flatpak app.
type Identity struct {
	Exe     string `json:"exe,omitempty"`     // the program's path
	Script  string `json:"script,omitempty"`  // what an interpreter runs: a path, or "-m module"
	Flatpak string `json:"flatpak,omitempty"` // the app ID, for a Flatpak app
	// Hash is the SHA-256 of the program, and of the script with one.
	Hash string `json:"hash,omitempty"`
	// System is set when only root can change the program (and the
	// script): a new hash is the package manager's update.
	System bool `json:"system,omitempty"`
	// Tampered says why the process is not to be trusted as it runs
	// ("LD_PRELOAD is set"); "" when nothing is wrong.
	Tampered string `json:"-"`
}

// Name is how the identity is shown: the script's or the program's base
// name, or the Flatpak app.
func (id Identity) Name() string {
	switch {
	case id.Flatpak != "":
		return id.Flatpak
	case id.Script != "" && !strings.HasPrefix(id.Script, "-"):
		return filepath.Base(id.Script)
	}
	return filepath.Base(id.Exe)
}

// procRoot is /proc; tests use a directory of their own.
var procRoot = "/proc"

// interpreters run a script that is the app.
var interpreters = regexp.MustCompile(`^(python[0-9.]*|node|nodejs|bun|deno|perl[0-9.]*|ruby[0-9.]*|bash|sh|dash|zsh|fish|lua[0-9.]*|luajit|php[0-9.]*|java|osascript)$`)

// Identify finds who the process pid is. Its program is read through
// /proc/PID/exe: a program replaced on disk since it started is still the
// one that runs.
func Identify(pid int) (Identity, error) {
	dir := filepath.Join(procRoot, strconv.Itoa(pid))
	exe, err := os.Readlink(filepath.Join(dir, "exe"))
	if err != nil {
		return Identity{}, err
	}
	id := Identity{Exe: strings.TrimSuffix(exe, " (deleted)")}
	sum, system, err := fileHash(filepath.Join(dir, "exe"), id.Exe)
	if err != nil {
		return Identity{}, err
	}
	id.Hash, id.System = sum, system

	if interpreters.MatchString(filepath.Base(id.Exe)) {
		args := splitNul(readFile(filepath.Join(dir, "cmdline")))
		if script, file := scriptOf(args); script != "" {
			id.Script = script
			if file {
				if !filepath.IsAbs(script) {
					cwd, _ := os.Readlink(filepath.Join(dir, "cwd"))
					id.Script = filepath.Join(cwd, script)
				}
				ssum, ssystem, err := fileHash(id.Script, id.Script)
				if err != nil {
					return Identity{}, err
				}
				h := sha256.Sum256([]byte(id.Hash + ssum))
				id.Hash, id.System = hex.EncodeToString(h[:]), id.System && ssystem
			}
		}
	}
	if info := readFile(filepath.Join(dir, "root", ".flatpak-info")); info != "" {
		id.Flatpak = flatpakApp(info)
	}
	for _, kv := range splitNul(readFile(filepath.Join(dir, "environ"))) {
		k, v, _ := strings.Cut(kv, "=")
		if v != "" && (k == "LD_PRELOAD" || k == "LD_AUDIT" || k == "LD_LIBRARY_PATH") {
			id.Tampered = k + " is set"
		}
	}
	return id, nil
}

func readFile(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func splitNul(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == 0 })
}

// scriptOf finds the script in an interpreter's command line: the first
// argument that is not an option, or what -m, -c or -e run (file false).
func scriptOf(args []string) (script string, file bool) {
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-m", "-c", "-e", "--eval", "-jar":
			if i+1 < len(args) {
				if a == "-jar" {
					return args[i+1], true
				}
				return a + " " + args[i+1], false
			}
			return "", false
		}
		if a == "--" {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		}
		if !strings.HasPrefix(a, "-") {
			return a, true
		}
	}
	return "", false
}

// flatpakApp is the app ID in a .flatpak-info.
func flatpakApp(info string) string {
	section := ""
	sc := bufio.NewScanner(strings.NewReader(info))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && section == "[Application]" && strings.TrimSpace(k) == "name" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// fileKey tells a file's versions apart without reading it.
type fileKey struct {
	dev, ino     uint64
	size         int64
	mtime, ctime int64
}

var hashes = struct {
	sync.Mutex
	m map[fileKey]string
}{m: map[fileKey]string{}}

// fileHash is the SHA-256 of the file open reads, computed once per
// version of it, and whether only root can change it: the file and every
// directory above path owned by root and writable by no one else.
func fileHash(open, path string) (string, bool, error) {
	f, err := os.Open(open)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false, errors.New("no file status")
	}
	key := fileKey{dev: st.Dev, ino: st.Ino, size: st.Size, mtime: st.Mtim.Nano(), ctime: st.Ctim.Nano()}
	system := st.Uid == 0 && info.Mode().Perm()&0o022 == 0 && rootOwnedDirs(filepath.Dir(path))
	hashes.Lock()
	sum, ok := hashes.m[key]
	hashes.Unlock()
	if ok {
		return sum, system, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", false, err
	}
	sum = hex.EncodeToString(h.Sum(nil))
	hashes.Lock()
	hashes.m[key] = sum
	hashes.Unlock()
	return sum, system, nil
}

// rootOwnedDirs reports whether dir and every directory above it are
// owned by root and writable by no one else.
func rootOwnedDirs(dir string) bool {
	for {
		info, err := os.Stat(dir)
		if err != nil {
			return false
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
			return false
		}
		up := filepath.Dir(dir)
		if up == dir {
			return true
		}
		dir = up
	}
}

// Package names the system package holding path and its version
// ("github-cli 2.62.0-1"), asking pacman, rpm or dpkg; "" for none.
func Package(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	run := func(name string, args ...string) string {
		if _, err := exec.LookPath(name); err != nil {
			return ""
		}
		out, err := exec.CommandContext(ctx, name, args...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if out := run("pacman", "-Qo", path); out != "" {
		if _, pkg, ok := strings.Cut(out, " is owned by "); ok {
			return pkg
		}
	}
	if out := run("rpm", "-qf", "--qf", "%{NAME} %{VERSION}-%{RELEASE}", path); out != "" && !strings.Contains(out, " not owned") {
		return out
	}
	if out := run("dpkg", "-S", path); out != "" {
		if pkg, _, ok := strings.Cut(out, ":"); ok {
			if v := run("dpkg-query", "-W", "-f=${Version}", pkg); v != "" {
				return pkg + " " + v
			}
			return pkg
		}
	}
	return ""
}

// --- the trust list ---

// Trusted is an app trusted with a secret.
type Trusted struct {
	Identity
	Package string    `json:"package,omitempty"`
	Since   time.Time `json:"since"`
}

// trustDoc is the trust file: the apps trusted with each item
// ("collection/item"), the paths whose programs read every secret, and
// their HMAC.
type trustDoc struct {
	Items map[string][]Trusted `json:"items"`
	Paths []PathRule           `json:"paths,omitempty"`
	MAC   string               `json:"mac,omitempty"`
}

// PathRule lets any program at a path read every secret, whatever its
// hash: for an app the user keeps up to date that changes every day. The
// path is a pattern (path/filepath.Match: "*" within a directory).
type PathRule struct {
	Pattern string    `json:"pattern"`
	Since   time.Time `json:"since"`
}

// Verdict is what the trust list says of a read.
type Verdict int

const (
	Unknown  Verdict = iota // not trusted: ask
	Allowed                 // trusted
	Changed                 // trusted once, but the program changed since: ask
	Tampered                // the process is not as its program runs: ask, and only for once
)

// ItemKey is an item's key in the trust list.
func ItemKey(coll, id string) string { return coll + "/" + id }

// Trust is the list of apps trusted with each secret, in a file sealed
// with an HMAC whose key is kept in pass.
type Trust struct {
	path string
	// key gets the HMAC key from pass; ask lets gpg-agent ask for the
	// passphrase. It fails when the key can't be read without asking.
	key func(ask bool) ([]byte, error)

	mu      sync.Mutex
	doc     trustDoc
	stamp   string // the file's time and size when read
	checked bool   // the seal was checked against the key
	bad     string // why the file is not trusted; "" when it is
}

// TrustPath is the trust file: $XDG_STATE_HOME/inua/keyring-trust.json.
func TrustPath() string {
	return filepath.Join(filepath.Dir(AccessPath()), "keyring-trust.json")
}

// NewTrust opens the trust list at path, sealed with the key key gives.
func NewTrust(path string, key func(ask bool) ([]byte, error)) *Trust {
	return &Trust{path: path, key: key, doc: trustDoc{Items: map[string][]Trusted{}}}
}

// load reads the file when it changed; a file unread is not yet checked.
func (t *Trust) load() {
	info, err := os.Stat(t.path)
	if errors.Is(err, os.ErrNotExist) {
		if t.stamp != "-" {
			t.doc, t.stamp, t.checked, t.bad = trustDoc{Items: map[string][]Trusted{}}, "-", true, ""
		}
		return
	}
	if err != nil {
		t.doc, t.stamp, t.bad = trustDoc{Items: map[string][]Trusted{}}, "", err.Error()
		return
	}
	stamp := fmt.Sprint(info.ModTime().UnixNano(), info.Size())
	if stamp == t.stamp {
		return
	}
	var doc trustDoc
	b, err := os.ReadFile(t.path)
	if err == nil {
		err = json.Unmarshal(b, &doc)
	}
	t.stamp, t.checked, t.bad = stamp, false, ""
	if err != nil {
		t.doc, t.checked, t.bad = trustDoc{Items: map[string][]Trusted{}}, true, err.Error()
		return
	}
	if doc.Items == nil {
		doc.Items = map[string][]Trusted{}
	}
	t.doc = doc
}

// mac seals the items and the paths with key; a list without paths is
// sealed as before there were any.
func mac(doc trustDoc, key []byte) string {
	b, _ := json.Marshal(doc.Items) // maps marshal in key order
	m := hmac.New(sha256.New, key)
	m.Write(b)
	if len(doc.Paths) > 0 {
		p, _ := json.Marshal(doc.Paths)
		m.Write([]byte("\x00paths"))
		m.Write(p)
	}
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// check checks the seal, when the key can be had without asking.
func (t *Trust) check() {
	if t.checked {
		return
	}
	key, err := t.key(false)
	if err != nil {
		return // not yet: locked
	}
	want := mac(t.doc, key)
	clear(key)
	t.checked = true
	if !hmac.Equal([]byte(want), []byte(t.doc.MAC)) {
		t.bad = "the list was changed outside inua-keyring"
	}
}

// Decide says whether the app id is trusted with an item, and the entry
// it was trusted by when its program changed. Checked is false when the
// seal could not be checked yet (locked): the caller checks again with
// Recheck once the read unlocked.
func (t *Trust) Decide(item string, id Identity) (v Verdict, was *Trusted, checked bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	t.check()
	if id.Tampered != "" {
		return Tampered, nil, t.checked
	}
	if t.bad != "" {
		return Unknown, nil, true
	}
	for _, e := range t.doc.Items[item] {
		switch {
		case id.Flatpak != "" || e.Flatpak != "":
			if e.Flatpak == id.Flatpak {
				return Allowed, nil, t.checked
			}
		case e.Exe == id.Exe && e.Script == id.Script:
			if e.Hash == id.Hash || (e.System && id.System) {
				return Allowed, nil, t.checked
			}
			if matchesPath(t.doc.Paths, id) {
				return Allowed, nil, t.checked
			}
			return Changed, &e, t.checked
		}
	}
	if matchesPath(t.doc.Paths, id) {
		return Allowed, nil, t.checked
	}
	return Unknown, nil, t.checked
}

// matchesPath says whether a rule lets id read every secret: its program
// is at a rule's path, or, for an interpreter, the script it runs is. A
// Flatpak app, and an interpreter running no script file, match none.
func matchesPath(rules []PathRule, id Identity) bool {
	if id.Flatpak != "" || len(rules) == 0 {
		return false
	}
	target := id.Exe
	if interpreters.MatchString(filepath.Base(id.Exe)) {
		if id.Script == "" || strings.HasPrefix(id.Script, "-") {
			return false
		}
		target = id.Script
	}
	for _, r := range rules {
		if ok, _ := filepath.Match(r.Pattern, target); ok {
			return true
		}
	}
	return false
}

// CheckPattern makes a path rule's pattern: "~/" for the home directory,
// cleaned, and absolute. It refuses one that would let too much through:
// a pattern that is not one, or whose fixed directory, before its first
// wildcard, is less than two deep, or the home directory or above it.
func CheckPattern(pattern, home string) (string, error) {
	p := strings.TrimSpace(pattern)
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%q is not an absolute path", pattern)
	}
	p = filepath.Clean(p)
	if _, err := filepath.Match(p, ""); err != nil {
		return "", fmt.Errorf("%q is not a pattern: %w", pattern, err)
	}
	fixed := filepath.Dir(p) // the directory of a path without wildcards
	if i := strings.IndexAny(p, `*?[\`); i >= 0 {
		fixed = filepath.Dir(p[:i+1])
	}
	h := filepath.Clean(home)
	switch {
	case fixed == "/" || !strings.Contains(strings.Trim(fixed, "/"), "/"):
		return "", fmt.Errorf("%q lets too much through: name a directory at least two deep before any wildcard", pattern)
	case home != "" && (fixed == h || strings.HasPrefix(h+"/", fixed+"/")):
		return "", fmt.Errorf("%q lets too much through: name a directory inside your home, not the home itself", pattern)
	}
	return p, nil
}

// AddPath lets programs at pattern (made by CheckPattern) read every
// secret, and seals the list. ask lets gpg-agent ask for the passphrase.
func (t *Trust) AddPath(pattern string, ask bool) error {
	return t.change(ask, adding, func(doc *trustDoc) {
		doc.Paths = slices.DeleteFunc(doc.Paths, func(r PathRule) bool { return r.Pattern == pattern })
		doc.Paths = append(doc.Paths, PathRule{Pattern: pattern, Since: time.Now()})
	})
}

// RemovePath drops a path rule. As Remove, a list not trusted stays so;
// a rule it does not hold changes nothing, and asks for nothing.
func (t *Trust) RemovePath(pattern string) error {
	t.mu.Lock()
	t.load()
	held := slices.ContainsFunc(t.doc.Paths, func(r PathRule) bool { return r.Pattern == pattern })
	t.mu.Unlock()
	if !held {
		return nil
	}
	return t.change(true, removing, func(doc *trustDoc) {
		doc.Paths = slices.DeleteFunc(doc.Paths, func(r PathRule) bool { return r.Pattern == pattern })
	})
}

// Problem is why the list is not trusted; "" when it is, or is not yet
// checked.
func (t *Trust) Problem() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	t.check()
	return t.bad
}

// ErrUntrusted is a change refused while the list is not trusted: it was
// changed outside, and sealing it would trust that too.
var ErrUntrusted = errors.New("the list of trusted apps is not trusted; trust it again in Settings → Keyring first")

// How change writes the list.
type changeKind int

const (
	adding    changeKind = iota // sealed; refused while the list is not trusted
	removing                    // sealed; while not trusted, written unsealed, still not trusted
	resealing                   // sealed, whatever it holds: the user said so
)

// Add trusts an app with an item, replacing what was trusted for the same
// program or Flatpak app, and seals the list. ask lets gpg-agent ask for
// the passphrase to read the key.
func (t *Trust) Add(item string, e Trusted, ask bool) error {
	return t.change(ask, adding, func(doc *trustDoc) {
		list := slices.DeleteFunc(doc.Items[item], func(x Trusted) bool { return sameApp(x.Identity, e.Identity) })
		doc.Items[item] = append(list, e)
	})
}

// Remove stops trusting an app with an item. A list not trusted stays
// so: removing can only trust less, but sealing would trust the rest.
func (t *Trust) Remove(item string, id Identity) error {
	return t.change(true, removing, func(doc *trustDoc) {
		list := slices.DeleteFunc(doc.Items[item], func(x Trusted) bool { return sameApp(x.Identity, id) })
		if len(list) == 0 {
			delete(doc.Items, item)
		} else {
			doc.Items[item] = list
		}
	})
}

// Forget drops an item deleted from the keyring; it stays in the file
// until the next change, when the key is at hand.
func (t *Trust) Forget(item string) {
	t.mu.Lock()
	_, ok := t.doc.Items[item]
	t.mu.Unlock()
	if ok {
		t.change(false, removing, func(doc *trustDoc) { delete(doc.Items, item) })
	}
}

// Reseal seals the list as it is: the user trusts it again after it was
// changed outside.
func (t *Trust) Reseal() error {
	return t.change(true, resealing, func(*trustDoc) {})
}

func sameApp(a, b Identity) bool {
	if a.Flatpak != "" || b.Flatpak != "" {
		return a.Flatpak == b.Flatpak
	}
	return a.Exe == b.Exe && a.Script == b.Script
}

// change changes the list and writes it, sealed but as how says.
func (t *Trust) change(ask bool, how changeKind, f func(*trustDoc)) error {
	t.mu.Lock()
	t.load()
	t.check()
	unsealed := t.bad != "" && how == removing
	refused := t.bad != "" && how == adding
	t.mu.Unlock()
	if refused {
		return ErrUntrusted
	}
	var key []byte
	if !unsealed {
		var err error
		if key, err = t.key(ask); err != nil {
			return err
		}
		defer clear(key)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	t.check()
	switch {
	case t.bad != "" && how == adding:
		return ErrUntrusted
	case t.bad == "" && unsealed:
		return errors.New("the list changed meanwhile; try again")
	}
	doc := trustDoc{Items: map[string][]Trusted{}, Paths: slices.Clone(t.doc.Paths)}
	for k, v := range t.doc.Items {
		doc.Items[k] = slices.Clone(v)
	}
	f(&doc)
	if !unsealed {
		doc.MAC = mac(doc, key)
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return err
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, t.path); err != nil {
		return err
	}
	t.stamp = ""
	t.load()
	if !unsealed {
		t.checked, t.bad = true, ""
	} else {
		t.checked, t.bad = true, "the list was changed outside inua-keyring"
	}
	return nil
}

// ReadTrust lists the apps trusted with each item as the file at path
// holds them, for Settings; inua-keyring says whether it trusts the file.
func ReadTrust(path string) map[string][]Trusted {
	return readTrustDoc(path).Items
}

// ReadPaths lists the path rules as the file at path holds them.
func ReadPaths(path string) []PathRule {
	return readTrustDoc(path).Paths
}

func readTrustDoc(path string) trustDoc {
	var doc trustDoc
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &doc) != nil {
		return trustDoc{}
	}
	return doc
}

// newTrustKey is a new HMAC key, as kept in pass.
func newTrustKey() []byte {
	k := make([]byte, 32)
	rand.Read(k)
	return []byte(base64.StdEncoding.EncodeToString(k))
}

// trustKeyEntry is the HMAC key's entry, under the keyring's folder.
const trustKeyEntry = ".trust-key"

// TrustKey reads the trust list's HMAC key from the store, making it on
// first use. Without ask, it reads only what needs no passphrase: from
// the vault, or with gpg-agent holding it.
func (s *Store) TrustKey(ask bool) ([]byte, error) {
	s.mu.Lock()
	v := s.vault
	s.mu.Unlock()
	if v != nil {
		if k, ok := v.Get(trustKeyEntry); ok {
			return k, nil
		}
	}
	if !s.back.Exists(trustKeyEntry) {
		k := newTrustKey()
		if err := s.back.Insert(trustKeyEntry, k); err != nil {
			return nil, err
		}
		s.keepKey(k)
		return k, nil
	}
	show := s.back.Show // the other backends ask no one anyway
	if q, ok := s.back.(quietShower); ok && !ask {
		show = q.ShowQuiet
	}
	k, err := show(trustKeyEntry)
	if err != nil {
		return nil, err
	}
	k = bytes.TrimSpace(k)
	s.keepKey(k)
	return k, nil
}

// keepKey keeps the HMAC key in the vault while unlocked.
func (s *Store) keepKey(k []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vault != nil && s.unlocked {
		s.vault.Put(trustKeyEntry, k)
	}
}
