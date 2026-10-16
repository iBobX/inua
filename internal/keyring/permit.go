package keyring

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	sys "golang.org/x/sys/unix"
)

// Answer is how the user answered an app's read.
type Answer int

const (
	Deny        Answer = iota
	AllowOnce          // for this process
	AlwaysAllow        // trusted from now on
)

// Question is what the user is asked when an app the trust list does not
// trust reads a secret.
type Question struct {
	App      string   `json:"app"` // the identity's name: "gh"
	Identity Identity `json:"identity"`
	Package  string   `json:"package,omitempty"` // "github-cli 2.62.0-1"
	Chain    string   `json:"chain,omitempty"`   // what it runs under: "claude ← zsh"
	PID      int      `json:"pid,omitempty"`
	Secret   string   `json:"secret"`          // the item's label
	Entry    string   `json:"entry,omitempty"` // its pass entry
	// Was is how the app was trusted, when its program changed since.
	Was *Trusted `json:"was,omitempty"`
	// Tampered says why the app can only be allowed once.
	Tampered string `json:"tampered,omitempty"`
	// Reseal: the question is whether to trust the list again, after it
	// was changed outside; Apps is how many entries it has.
	Reseal bool `json:"reseal,omitempty"`
	Apps   int  `json:"apps,omitempty"`
	// Path: the question is whether programs at this path (a PathRule's
	// pattern) may read every secret without asking.
	Path string `json:"path,omitempty"`
}

// How a read went, as the record of reads keeps it (Access.Decision).
const (
	DecTrusted = "trusted" // the trust list trusts the app
	DecOnce    = "once"    // allowed once, for the process
	DecAllowed = "allowed" // asked, and trusted from now on
	DecDenied  = "denied"
	DecUnasked = "unasked" // nobody to ask: keyring.untrusted allowed it
)

var errDenied = dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", []any{"The user did not let this app read the secret"})

// pending is a question on its way; reads of the same process and item
// wait for its answer.
type pending struct {
	done chan struct{}
	ans  Answer
	dec  string
}

// identify finds who a client is. With the pidfd the bus gives, the
// process can't have been replaced by another with its PID while it was
// looked at; else its start time is compared.
func (s *Service) identify(unique string, pid int) (Identity, error) {
	var creds map[string]dbus.Variant
	pidfd := -1
	if s.conn.BusObject().Call("org.freedesktop.DBus.GetConnectionCredentials", 0, unique).Store(&creds) == nil {
		if fd, ok := creds["ProcessFD"].Value().(dbus.UnixFD); ok {
			pidfd = int(fd)
			defer sys.Close(pidfd)
		}
	}
	start := startTime(pid)
	id, err := Identify(pid)
	if err != nil {
		return Identity{}, err
	}
	if pidfd >= 0 {
		if sys.PidfdSendSignal(pidfd, 0, nil, 0) != nil {
			return Identity{}, errors.New("the process is gone")
		}
	} else if start == "" || startTime(pid) != start {
		return Identity{}, errors.New("the process is gone")
	}
	return id, nil
}

// startTime is a process's start time, in clock ticks since boot.
func startTime(pid int) string {
	b, err := os.ReadFile(procRoot + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// After the command, in parentheses, which may hold anything.
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 {
		if f := strings.Fields(s[i+1:]); len(f) > 19 {
			return f[19]
		}
	}
	return ""
}

// permit decides whether the client who may read an item, asking the user
// when the trust list does not trust it. unchecked is set when the list
// trusted it but its seal could not be checked yet (locked): the caller
// rechecks once the read unlocked.
func (s *Service) permit(it Item, a app, who, label string) (dec string, unchecked bool, derr *dbus.Error) {
	if s.Trust == nil {
		return "", false, nil
	}
	key := ItemKey(it.Collection, it.ID)
	s.mu.Lock()
	ans, ok := s.decisions[who][key]
	s.mu.Unlock()
	switch {
	case ok && ans == Deny:
		return DecDenied, false, errDenied
	case ok && ans == AllowOnce:
		return DecOnce, false, nil
	case ok:
		return DecTrusted, false, nil // trusted while this process runs, sealed or not yet
	}
	id := a.id
	if a.idErr != nil {
		id = Identity{Exe: a.exe, Tampered: "inua-keyring could not tell which program it is: " + a.idErr.Error()}
	}
	v, was, checked := s.Trust.Decide(key, id)
	if v == Allowed {
		return DecTrusted, !checked, nil
	}
	q := Question{App: id.Name(), Identity: id, Chain: a.chain, PID: a.pid, Secret: label,
		Entry: s.store.Where(it.Collection, it.ID), Was: was, Tampered: id.Tampered}
	return s.ask(who, key, q)
}

// ask asks the user, once for reads of the same process and item waiting
// together, and keeps the answer for the process.
func (s *Service) ask(who, key string, q Question) (string, bool, *dbus.Error) {
	s.mu.Lock()
	p := s.pending[who+"\n"+key]
	if p != nil {
		s.mu.Unlock()
		<-p.done
	} else {
		p = &pending{done: make(chan struct{})}
		s.pending[who+"\n"+key] = p
		s.mu.Unlock()
		p.ans, p.dec = s.question(q)
		if p.ans == AlwaysAllow && !s.trust(key, q) {
			s.mu.Lock()
			s.later = append(s.later, laterTrust{key: key, q: q})
			s.mu.Unlock()
		}
		s.mu.Lock()
		delete(s.pending, who+"\n"+key)
		if s.decisions[who] == nil {
			s.decisions[who] = map[string]Answer{}
		}
		s.decisions[who][key] = p.ans
		s.mu.Unlock()
		close(p.done)
	}
	if p.ans == Deny {
		return p.dec, false, errDenied
	}
	return p.dec, false, nil
}

// question asks the user; without anyone to ask, keyring.untrusted
// answers.
func (s *Service) question(q Question) (Answer, string) {
	if s.Ask == nil {
		return s.untrusted()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	ans, err := s.Ask(ctx, q)
	if err != nil {
		log.Printf("asking about %s: %v", q.App, err)
		return s.untrusted()
	}
	if ans == AlwaysAllow && q.Tampered != "" {
		ans = AllowOnce // not offered; never trusted as it runs
	}
	switch ans {
	case AlwaysAllow:
		return ans, DecAllowed
	case AllowOnce:
		return ans, DecOnce
	}
	return Deny, DecDenied
}

func (s *Service) untrusted() (Answer, string) {
	if s.Untrusted != nil && s.Untrusted() == Deny {
		return Deny, DecDenied
	}
	return AllowOnce, DecUnasked
}

// laterTrust is an app to trust once the trust list's key can be read: it
// was allowed, or created the item, while locked.
type laterTrust struct {
	key string
	q   Question
}

// trust adds the app to the trust list, if its key can be read without
// asking; false when not yet.
func (s *Service) trust(key string, q Question) bool {
	if q.Identity.Exe == "" && q.Identity.Flatpak == "" || q.Tampered != "" {
		return true // nothing to trust
	}
	pkg := q.Package
	if pkg == "" && q.Identity.System {
		pkg = Package(q.Identity.Exe)
	}
	err := s.Trust.Add(key, Trusted{Identity: q.Identity, Package: pkg, Since: time.Now()}, false)
	if err != nil {
		log.Printf("trusting %s with %s: %v", q.App, key, err)
		return errors.Is(err, ErrUntrusted) // not to be tried again: allowed for the process only
	}
	return true
}

// trustLater adds the apps that could not be added before, now that the
// key may be at hand.
func (s *Service) trustLater() {
	s.mu.Lock()
	todo := s.later
	s.later = nil
	s.mu.Unlock()
	var left []laterTrust
	for _, l := range todo {
		if !s.trust(l.key, l.q) {
			left = append(left, l)
		}
	}
	if len(left) > 0 {
		s.mu.Lock()
		s.later = append(left, s.later...)
		s.mu.Unlock()
	}
}

// recheck decides again once the read unlocked and the trust list's seal
// can be checked: a list changed outside turns the read into a question.
func (s *Service) recheck(it Item, a app, who, label string) (string, *dbus.Error) {
	key := ItemKey(it.Collection, it.ID)
	v, was, _ := s.Trust.Decide(key, a.id)
	if v == Allowed {
		return DecTrusted, nil
	}
	q := Question{App: a.id.Name(), Identity: a.id, Chain: a.chain, PID: a.pid, Secret: label,
		Entry: s.store.Where(it.Collection, it.ID), Was: was, Tampered: a.id.Tampered}
	dec, _, derr := s.ask(who, key, q)
	return dec, derr
}

// trustCreator trusts the app that created an item with it.
func (s *Service) trustCreator(it Item, who string) {
	if s.Trust == nil {
		return
	}
	a := s.client(who)
	if a.idErr != nil || a.id.Tampered != "" {
		return
	}
	key := ItemKey(it.Collection, it.ID)
	s.mu.Lock()
	if s.decisions[who] == nil {
		s.decisions[who] = map[string]Answer{}
	}
	s.decisions[who][key] = AlwaysAllow // at least while this process runs
	s.mu.Unlock()
	q := Question{App: a.id.Name(), Identity: a.id}
	if !s.trust(key, q) {
		s.mu.Lock()
		s.later = append(s.later, laterTrust{key: key, q: q})
		s.mu.Unlock()
	}
}

// --- inua's own interface ---

const (
	ownPath  = dbus.ObjectPath("/io/github/lyricat/Inua/Keyring")
	ownIface = "io.github.lyricat.Inua.Keyring"
)

type ownObj struct{ s *Service }

// Lock forgets the secrets kept since the keyring unlocked.
func (o ownObj) Lock() *dbus.Error {
	o.s.store.Lock()
	return nil
}

// State says whether the keyring is unlocked, and why the trust list is
// not trusted ("" when it is).
func (o ownObj) State() (bool, string, *dbus.Error) {
	on, _ := o.s.store.Unlocked()
	problem := ""
	if o.s.Trust != nil {
		problem = o.s.Trust.Problem()
	}
	return on, problem, nil
}

// Untrust stops trusting an app with an item ("collection/item").
func (o ownObj) Untrust(item, exe, script, flatpak string) *dbus.Error {
	if o.s.Trust == nil {
		return nil
	}
	if err := o.s.Trust.Remove(item, Identity{Exe: exe, Script: script, Flatpak: flatpak}); err != nil {
		return failed(err)
	}
	return nil
}

// Reseal trusts the list again after it was changed outside, once the
// user said so in inua-keyring's own question: a program that changed
// the file can't have it sealed by calling this.
func (o ownObj) Reseal() (bool, *dbus.Error) {
	s := o.s
	if s.Trust == nil || s.Trust.Problem() == "" {
		return false, nil
	}
	n := 0
	for _, apps := range ReadTrust(s.Trust.path) {
		n += len(apps)
	}
	if s.Ask == nil {
		return false, failed(errors.New("there is no one to ask"))
	}
	ans, err := s.Ask(context.Background(), Question{Reseal: true, Apps: n})
	if err != nil {
		return false, failed(err)
	}
	if ans != AlwaysAllow {
		return false, nil
	}
	if err := s.Trust.Reseal(); err != nil {
		return false, failed(fmt.Errorf("sealing the list: %w", err))
	}
	return true, nil
}

// AllowPath lets programs at pattern read every secret, once the user
// said so in inua-keyring's own question: a program can't add a rule by
// calling this. It returns the pattern as kept (CheckPattern), and false
// when the user said no.
func (o ownObj) AllowPath(pattern string) (string, bool, *dbus.Error) {
	s := o.s
	if s.Trust == nil {
		return "", false, failed(errors.New("there is no trust list"))
	}
	home, _ := os.UserHomeDir()
	p, err := CheckPattern(pattern, home)
	if err != nil {
		return "", false, failed(err)
	}
	if s.Trust.Problem() != "" {
		return p, false, failed(ErrUntrusted)
	}
	if s.Ask == nil {
		return p, false, failed(errors.New("there is no one to ask"))
	}
	ans, err := s.Ask(context.Background(), Question{Path: p})
	if err != nil {
		return p, false, failed(err)
	}
	if ans != AlwaysAllow {
		return p, false, nil
	}
	if err := s.Trust.AddPath(p, true); err != nil {
		return p, false, failed(fmt.Errorf("adding the path: %w", err))
	}
	return p, true, nil
}

// DisallowPath drops a path rule: its programs are asked about again.
func (o ownObj) DisallowPath(pattern string) *dbus.Error {
	if o.s.Trust == nil {
		return nil
	}
	if err := o.s.Trust.RemovePath(pattern); err != nil {
		return failed(err)
	}
	return nil
}

const ownXML = `<interface name="io.github.lyricat.Inua.Keyring">
  <method name="Lock"/>
  <method name="State"><arg name="unlocked" type="b" direction="out"/><arg name="problem" type="s" direction="out"/></method>
  <method name="Untrust"><arg name="item" type="s" direction="in"/><arg name="exe" type="s" direction="in"/><arg name="script" type="s" direction="in"/><arg name="flatpak" type="s" direction="in"/></method>
  <method name="Reseal"><arg name="sealed" type="b" direction="out"/></method>
  <method name="AllowPath"><arg name="pattern" type="s" direction="in"/><arg name="kept" type="s" direction="out"/><arg name="added" type="b" direction="out"/></method>
  <method name="DisallowPath"><arg name="pattern" type="s" direction="in"/></method>
 </interface>`

// --- for the command line and Settings ---

func own(conn *dbus.Conn) dbus.BusObject { return conn.Object(OwnName, ownPath) }

// LockKeyring asks the running inua-keyring to lock.
func LockKeyring(conn *dbus.Conn) error {
	return own(conn).Call(ownIface+".Lock", 0).Err
}

// KeyringState asks the running inua-keyring whether it is unlocked, and
// why its trust list is not trusted.
func KeyringState(conn *dbus.Conn) (unlocked bool, problem string, err error) {
	err = own(conn).Call(ownIface+".State", 0).Store(&unlocked, &problem)
	return
}

// UntrustApp asks the running inua-keyring to stop trusting an app with
// an item.
func UntrustApp(conn *dbus.Conn, item string, id Identity) error {
	return own(conn).Call(ownIface+".Untrust", 0, item, id.Exe, id.Script, id.Flatpak).Err
}

// ResealTrust asks the running inua-keyring to trust its list again; it
// asks the user first.
func ResealTrust(conn *dbus.Conn) (bool, error) {
	var ok bool
	err := own(conn).Call(ownIface+".Reseal", 0).Store(&ok)
	return ok, err
}

// AllowPath asks the running inua-keyring to let programs at pattern read
// every secret; it asks the user first. kept is the pattern as kept.
func AllowPath(conn *dbus.Conn, pattern string) (kept string, added bool, err error) {
	err = own(conn).Call(ownIface+".AllowPath", 0, pattern).Store(&kept, &added)
	return
}

// DisallowPath asks the running inua-keyring to drop a path rule.
func DisallowPath(conn *dbus.Conn, pattern string) error {
	return own(conn).Call(ownIface+".DisallowPath", 0, pattern).Err
}
