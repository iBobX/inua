package keyring

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lyricat/inua/internal/dbustest"
)

// asker is a fake user: it answers ans, and counts the questions.
type asker struct {
	mu   sync.Mutex
	ans  Answer
	qs   []Question
	hold chan struct{} // when set, answers once it is closed
}

func (a *asker) ask(ctx context.Context, q Question) (Answer, error) {
	a.mu.Lock()
	a.qs = append(a.qs, q)
	hold, ans := a.hold, a.ans
	a.mu.Unlock()
	if hold != nil {
		<-hold
	}
	return ans, nil
}

func (a *asker) asked() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.qs)
}

// serveTrust runs the service with a trust list and the fake user; the
// item "API key" is in the store, made by no app.
func serveTrust(t *testing.T, a *asker) (addr string, s *Store, trustPath string, reads func() []Access) {
	t.Helper()
	trustPath = filepath.Join(t.TempDir(), "trust.json")
	var mu sync.Mutex
	var got []Access
	addr, s, _ = serveWith(t, func(svc *Service) {
		svc.Trust = NewTrust(trustPath, svc.store.TrustKey)
		if a != nil {
			svc.Ask = a.ask
		}
		svc.OnRead = func(r Access, _ bool) {
			mu.Lock()
			got = append(got, r)
			mu.Unlock()
		}
	})
	if _, _, err := s.CreateItem(Login, "API key", map[string]string{"k": "v"}, []byte("sk-1"), "text/plain", false); err != nil {
		t.Fatal(err)
	}
	reads = func() []Access {
		mu.Lock()
		defer mu.Unlock()
		return append([]Access(nil), got...)
	}
	return addr, s, trustPath, reads
}

// lookup reads the item from a new connection: a client of its own.
func lookup(t *testing.T, addr string) ([]byte, error) {
	t.Helper()
	c, err := Connect(dbustest.Conn(t, addr), AlgPlain)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	v, _, err := c.Lookup(t.Context(), map[string]string{"k": "v"})
	return v, err
}

func TestAlwaysAllow(t *testing.T) {
	a := &asker{ans: AlwaysAllow}
	addr, _, trustPath, reads := serveTrust(t, a)
	if v, err := lookup(t, addr); err != nil || string(v) != "sk-1" {
		t.Fatalf("first read %q %v", v, err)
	}
	if v, err := lookup(t, addr); err != nil || string(v) != "sk-1" {
		t.Fatalf("second read %q %v", v, err)
	}
	if a.asked() != 1 {
		t.Errorf("asked %d times, want once", a.asked())
	}
	exe, _ := os.Executable()
	q := a.qs[0]
	if q.Identity.Exe != exe || q.Identity.Hash == "" || q.Secret != "API key" || q.App != filepath.Base(exe) {
		t.Errorf("question %+v (this is %s)", q, exe)
	}
	apps := ReadTrust(trustPath)[ItemKey(Login, "1")]
	if len(apps) != 1 || apps[0].Exe != exe {
		t.Errorf("trusted %+v", apps)
	}
	if r := reads(); len(r) != 2 || r[0].Decision != DecAllowed || r[1].Decision != DecTrusted {
		t.Errorf("reads %+v", r)
	}
}

func TestAllowOnceAndDeny(t *testing.T) {
	a := &asker{ans: AllowOnce}
	addr, _, trustPath, reads := serveTrust(t, a)
	c, err := Connect(dbustest.Conn(t, addr), AlgPlain)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for range 2 { // one client: asked once
		if v, _, err := c.Lookup(t.Context(), map[string]string{"k": "v"}); err != nil || string(v) != "sk-1" {
			t.Fatalf("read %q %v", v, err)
		}
	}
	if a.asked() != 1 {
		t.Errorf("one client asked %d times", a.asked())
	}
	lookup(t, addr) // another: asked again
	if a.asked() != 2 {
		t.Errorf("a second client: asked %d times in all", a.asked())
	}
	if len(ReadTrust(trustPath)) != 0 {
		t.Error("allowed once, but trusted")
	}

	a.mu.Lock()
	a.ans = Deny
	a.mu.Unlock()
	if v, err := lookup(t, addr); err == nil || !strings.Contains(err.Error(), "did not let") {
		t.Errorf("denied: %q %v", v, err)
	}
	r := reads()
	last := r[len(r)-1]
	if last.Decision != DecDenied || last.Secret != "API key" {
		t.Errorf("the denied read: %+v", last)
	}
}

// Reads waiting together get one question.
func TestOneQuestion(t *testing.T) {
	a := &asker{ans: AllowOnce, hold: make(chan struct{})}
	addr, _, _, _ := serveTrust(t, a)
	c, err := Connect(dbustest.Conn(t, addr), AlgPlain)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Go(func() {
			_, _, err := c.Lookup(t.Context(), map[string]string{"k": "v"})
			errs <- err
		})
	}
	for a.asked() == 0 {
		// wait for the question
	}
	close(a.hold)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if a.asked() != 1 {
		t.Errorf("asked %d times", a.asked())
	}
}

// Nobody to ask: keyring.untrusted decides.
func TestUntrusted(t *testing.T) {
	addr, _, _, reads := serveTrust(t, nil)
	if v, err := lookup(t, addr); err != nil || string(v) != "sk-1" {
		t.Errorf("allowed by default: %q %v", v, err)
	}
	if r := reads(); r[0].Decision != DecUnasked {
		t.Errorf("decision %q", r[0].Decision)
	}

	trustPath := filepath.Join(t.TempDir(), "trust.json")
	addr, s, _ := serveWith(t, func(svc *Service) {
		svc.Trust = NewTrust(trustPath, svc.store.TrustKey)
		svc.Untrusted = func() Answer { return Deny }
	})
	s.CreateItem(Login, "API key", map[string]string{"k": "v"}, []byte("sk-1"), "", false)
	if _, err := lookup(t, addr); err == nil {
		t.Error("read with keyring.untrusted = deny")
	}
}

// The app that stores a secret is trusted with it.
func TestCreatorTrusted(t *testing.T) {
	a := &asker{ans: Deny}
	addr, _, trustPath, _ := serveTrust(t, a)
	c, err := Connect(dbustest.Conn(t, addr), AlgPlain)
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]string{"app": "mine"}
	if err := c.Store(t.Context(), "Mine", attrs, []byte("m")); err != nil {
		t.Fatal(err)
	}
	c.Close()
	c, _ = Connect(dbustest.Conn(t, addr), AlgPlain)
	defer c.Close()
	if v, _, err := c.Lookup(t.Context(), attrs); err != nil || string(v) != "m" {
		t.Errorf("the creator's read: %q %v", v, err)
	}
	if a.asked() != 0 {
		t.Errorf("the creator was asked %d times", a.asked())
	}
	if len(ReadTrust(trustPath)[ItemKey(Login, "2")]) != 1 {
		t.Errorf("trust %+v", ReadTrust(trustPath))
	}
}

// inua's interface: lock, state, untrust, and resealing only with the
// user's yes.
func TestOwnInterface(t *testing.T) {
	a := &asker{ans: AlwaysAllow}
	addr, s, trustPath, _ := serveTrust(t, a)
	s.KeepUnlocked(NewVault())
	lookup(t, addr)
	conn := dbustest.Conn(t, addr)
	if on, problem, err := KeyringState(conn); err != nil || !on || problem != "" {
		t.Errorf("state %v %q %v", on, problem, err)
	}
	if err := LockKeyring(conn); err != nil {
		t.Fatal(err)
	}
	if on, _, _ := KeyringState(conn); on {
		t.Error("unlocked after Lock")
	}

	exe, _ := os.Executable()
	if err := UntrustApp(conn, ItemKey(Login, "1"), Identity{Exe: exe}); err != nil {
		t.Fatal(err)
	}
	if len(ReadTrust(trustPath)) != 0 {
		t.Errorf("still trusted: %+v", ReadTrust(trustPath))
	}

	// Changed outside: the state says so; resealing asks.
	os.WriteFile(trustPath, []byte(`{"items":{"login/1":[{"exe":"/tmp/evil"}]},"mac":"x"}`), 0o600)
	if _, problem, _ := KeyringState(conn); problem == "" {
		t.Error("no problem with a list changed outside")
	}
	UntrustApp(conn, ItemKey(Login, "1"), Identity{Exe: "/nonexistent"})
	if _, problem, _ := KeyringState(conn); problem == "" {
		t.Error("Untrust sealed a list changed outside")
	}
	a.mu.Lock()
	a.ans = Deny
	a.mu.Unlock()
	if ok, err := ResealTrust(conn); ok || err != nil {
		t.Errorf("resealed though the user said no: %v %v", ok, err)
	}
	a.mu.Lock()
	a.ans = AlwaysAllow
	a.mu.Unlock()
	n := a.asked()
	if ok, err := ResealTrust(conn); !ok || err != nil {
		t.Errorf("reseal: %v %v", ok, err)
	}
	a.mu.Lock()
	q := a.qs[n]
	a.mu.Unlock()
	if !q.Reseal || q.Apps != 1 {
		t.Errorf("the question %+v", q)
	}
	if _, problem, _ := KeyringState(conn); problem != "" {
		t.Errorf("after resealing: %q", problem)
	}
}

// A path rule is added only once the user said so, in inua-keyring's own
// question; a pattern that lets too much through is refused unasked.
func TestAllowPath(t *testing.T) {
	a := &asker{ans: Deny}
	addr, s, trustPath, _ := serveTrust(t, a)
	s.KeepUnlocked(NewVault())
	lookup(t, addr)
	conn := dbustest.Conn(t, addr)
	n := a.asked()
	if _, added, err := AllowPath(conn, "/opt/app/bin/*"); added || err != nil || len(ReadPaths(trustPath)) != 0 {
		t.Errorf("added though the user said no: %v %v", added, err)
	}
	a.mu.Lock()
	q := a.qs[n]
	a.ans = AlwaysAllow
	a.mu.Unlock()
	if q.Path != "/opt/app/bin/*" {
		t.Errorf("the question %+v", q)
	}
	if kept, added, err := AllowPath(conn, "/opt/app/bin/*"); !added || err != nil || kept != "/opt/app/bin/*" {
		t.Errorf("allow: %q %v %v", kept, added, err)
	}
	if p := ReadPaths(trustPath); len(p) != 1 {
		t.Errorf("paths %+v", p)
	}
	n = a.asked()
	if _, _, err := AllowPath(conn, "/*"); err == nil || a.asked() != n {
		t.Errorf("too broad: %v, asked %d", err, a.asked()-n)
	}
	if err := DisallowPath(conn, "/opt/app/bin/*"); err != nil || len(ReadPaths(trustPath)) != 0 {
		t.Errorf("disallow: %v %+v", err, ReadPaths(trustPath))
	}
}
