package keyring

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/proc"
)

// Access is a secret handed to an app, or a passphrase inua-pinentry
// asked for: the settings window lists them, newest first.
type Access struct {
	Time time.Time `json:"time"`
	// Kind is "" for a secret inua-keyring handed out, Passphrase for
	// gpg-agent asking, through inua-pinentry, for the key that unlocks
	// pass: while it keeps the passphrase, pass reads without asking.
	Kind   string `json:"kind,omitempty"`
	Answer string `json:"answer,omitempty"` // a Passphrase's: Entered, Cancelled or Failed
	App    string `json:"app"`              // the program: "zed", "chrome"
	PID    int    `json:"pid"`              // 0 when the bus did not say
	Secret string `json:"secret"`           // the item's label
	// Where the secret is kept: the pass entry, "inua-keyring/login/…";
	// "" for one kept in memory (the session keyring), and in records
	// written before inua-keyring wrote it.
	Pass       string `json:"pass,omitempty"`
	Collection string `json:"collection,omitempty"` // "login"
	// The process that asked, as it was then: it may have gone since.
	Exe     string `json:"exe,omitempty"`     // its program's full path
	Command string `json:"command,omitempty"` // its command line
	Chain   string `json:"chain,omitempty"`   // what it runs under, proc.Chain
	Bus     string `json:"bus,omitempty"`     // its unique name on the bus, ":1.42"
}

// The kinds of Access, and how a passphrase was answered.
const (
	Passphrase = "passphrase"
	Entered    = "entered"
	Cancelled  = "cancelled"
	Failed     = "failed" // the dialog could not be shown
)

// accessKeep is how many accesses the record keeps; it is trimmed to them
// once it holds twice as many.
const accessKeep = 200

// AccessPath is the record of accesses:
// $XDG_STATE_HOME/inua/keyring-access.jsonl (~/.local/state/…).
func AccessPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(dir, "inua", "keyring-access.jsonl")
}

// RecordAccess adds a to the record at path, one JSON line each. It
// holds a lock while it writes: inua-keyring and inua-pinentry both do.
func RecordAccess(path string, a Access) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close() // and with it the lock
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	line, err := json.Marshal(a)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.Count(data, []byte{'\n'}) <= 2*accessKeep {
		return err
	}
	lines := bytes.SplitAfter(data, []byte{'\n'})
	lines = lines[len(lines)-1-accessKeep:] // the last is empty, after the final newline
	tmp := path + ".new"
	if err := os.WriteFile(tmp, bytes.Join(lines, nil), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadAccess reads the record at path, newest first; none when there is
// no record yet. Lines it cannot read are left out.
func ReadAccess(path string) ([]Access, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Access
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var a Access
		if json.Unmarshal(sc.Bytes(), &a) == nil && a.App != "" {
			out = append(out, a)
		}
	}
	slices.Reverse(out)
	return out, sc.Err()
}

// Reads are accesses one after another by one process to one secret,
// listed as one: the newest, how many, and when the first was.
type Reads struct {
	Access           // the newest
	N      int       // how many
	First  time.Time // the oldest's time
}

// same says whether b is the same read as a, again.
func same(a, b Access) bool {
	return a.Kind == b.Kind && a.App == b.App && a.PID == b.PID && a.Secret == b.Secret && a.Answer == b.Answer
}

// GroupReads groups a record read by ReadAccess, newest first, into at
// most limit Reads.
func GroupReads(record []Access, limit int) []Reads {
	var out []Reads
	for i := 0; i < len(record) && len(out) < limit; {
		a, n := record[i], 1
		for i+n < len(record) && same(record[i+n], a) {
			n++
		}
		out = append(out, Reads{Access: a, N: n, First: record[i+n-1].Time})
		i += n
	}
	return out
}

// PassphraseAccess is gpg-agent asking for the passphrase, through
// inua-pinentry, for owner (the gpg it names): who runs gpg, and what for.
// forKeyring is what inua-keyring put in PINENTRY_USER_DATA after
// pinentry.UserData ("gh (pid 4123)\tlabel"), or "" when it did not ask;
// desc is the agent's description, which names the key; store is pass's
// password store, to name the file gpg decrypts as pass does.
func PassphraseAccess(owner int, forKeyring, desc, store string) Access {
	a := Access{Time: time.Now(), Kind: Passphrase, PID: owner}
	if owner > 0 {
		a.Exe, a.Command = proc.Exe(owner), proc.Command(owner)
		// The program running gpg says more than gpg does.
		app := owner
		if strings.HasPrefix(proc.Name(owner), "gpg") {
			app = proc.Parent(owner)
		}
		a.App, a.Chain = proc.Name(app), proc.Chain(proc.Parent(app), 8)
		for _, arg := range proc.Args(owner) {
			if strings.HasSuffix(arg, ".gpg") {
				a.Pass = passEntry(arg, store)
			}
		}
	}
	if app, secret, ok := strings.Cut(forKeyring, "\t"); ok {
		// inua-keyring's pass runs gpg: what it runs under is
		// inua-keyring, not the app. The app waits for the secret
		// meanwhile, so what it runs under is read from its PID.
		name, pid, _ := strings.Cut(app, " (pid ")
		a.App, a.Secret, a.Chain = name, secret, ""
		if n, err := strconv.Atoi(strings.TrimSuffix(pid, ")")); err == nil && n > 1 {
			a.Chain = proc.Chain(proc.Parent(n), 8)
		}
	}
	for a.App != "" && (a.Chain == a.App || strings.HasPrefix(a.Chain, a.App+" ← ")) {
		a.Chain = strings.TrimPrefix(strings.TrimPrefix(a.Chain, a.App), " ← ") // pass runs itself again
	}
	if a.Secret == "" {
		a.Secret = cmp.Or(a.Pass, keyName(desc))
	}
	return a
}

// passEntry names a file gpg decrypts as pass does, relative to the store
// without ".gpg"; a file outside the store keeps its path.
func passEntry(file, store string) string {
	if rel, err := filepath.Rel(store, file); err == nil && store != "" && filepath.IsAbs(file) && !strings.HasPrefix(rel, "..") {
		return strings.TrimSuffix(rel, ".gpg")
	}
	return file
}

// keyName is the key gpg-agent's description names: the user ID it quotes
// ("Lyric <lyric@example.org>"); "" when it quotes none.
func keyName(desc string) string {
	_, rest, ok := strings.Cut(desc, `"`)
	if name, _, closed := strings.Cut(rest, `"`); ok && closed {
		return name
	}
	return ""
}
