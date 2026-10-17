package system

import (
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
)

// logind answers CanHibernate as told; the bus calls it from its own
// goroutine.
type logind struct {
	mu     sync.Mutex
	answer string
}

func (l *logind) CanHibernate() (string, *dbus.Error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.answer, nil
}

func TestCanHibernate(t *testing.T) {
	addr := dbustest.Start(t)
	l := &logind{}
	conn := service(t, addr, "org.freedesktop.login1")
	if err := conn.Export(l, "/org/freedesktop/login1", "org.freedesktop.login1.Manager"); err != nil {
		t.Fatal(err)
	}
	for answer, want := range map[string]bool{"yes": true, "challenge": true, "no": false, "na": false} {
		l.mu.Lock()
		l.answer = answer
		l.mu.Unlock()
		if got, err := CanHibernate(dialer(addr)); err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", answer, got, err, want)
		}
	}
	conn.ReleaseName("org.freedesktop.login1")
	if _, err := CanHibernate(dialer(addr)); err == nil {
		t.Error("no logind: no error")
	}
}
