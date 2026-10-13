package ui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/lock"
	"github.com/lyricat/inua/internal/rundir"
)

// locker is the lock screen. It holds the compositor's session lock
// (ext-session-lock-v1): while it does, only its windows show and take
// input, and if the shell goes away the screen stays locked. A file in the
// runtime directory marks the screen as locked, so a shell started again
// (after a crash or `shell restart`) locks it again at once.
type locker struct {
	inst            uintptr // the session lock instance, made on first use
	locking, locked bool
	surfaces        []*lockSurface
	refs            gtkx.Scope
	ticking         bool

	// ending is set from ::unlocked or ::failed until lockEnded runs; a
	// lock asked for meanwhile sets relock and is taken then.
	ending, relock bool

	checking  bool      // a password is being checked
	failures  int       // wrong passwords in a row
	notBefore time.Time // no password is taken before then
}

// lockSurface is the lock screen on one monitor; the session lock library
// destroys its window when the lock ends.
type lockSurface struct {
	mon                        *gdk.Monitor // our reference, dropped when the lock ends
	win                        gtk.Window
	date, clock, user, message gtk.Label
	password                   gtk.PasswordEntry
}

func (s *shellUI) bindLockHandlers() {
	l := &s.lock
	s.onLockMonitor = func(_, mon, _ uintptr) {
		m := gdk.MonitorNewFromInternalPtr(mon)
		m.Ref() // the signal's monitor is borrowed
		s.buildLockSurface(m)
	}
	s.onLockLocked = func(uintptr, uintptr) {
		l.locking, l.locked = false, true
		log.Printf("lock: locked")
		if !l.ticking {
			l.ticking = true
			glib.TimeoutAdd(1000, &s.onLockTick, 0)
		}
	}
	s.onLockFailed = func(uintptr, uintptr) {
		log.Printf("lock: the compositor did not grant the lock (is another lock screen running?)")
		l.ending = true
		gtkx.Post(s.lockEnded)
	}
	s.onLockUnlocked = func(uintptr, uintptr) {
		log.Printf("lock: unlocked")
		// In 1.1 this signal precedes releasing the Wayland lock. Keep
		// desktop creation blocked until the library finishes the callback.
		l.ending = true
		gtkx.Post(s.lockEnded)
	}
	s.onLockTick = func(uintptr) bool {
		if !l.locked && !l.locking {
			l.ticking = false
			return false
		}
		s.showLockClock()
		return true
	}
	s.onLockActivate = func(e gtk.PasswordEntry) { s.tryUnlock(e.GetText()) }
}

// lockScreen locks the screen. It refuses when it could not unlock again:
// without session lock support, or without the password helper.
func (s *shellUI) lockScreen() error {
	s.closeClipboard()
	l := &s.lock
	if l.ending { // the last lock is still being let go
		l.relock = true
		return nil
	}
	if l.held() {
		return nil
	}
	if !s.layer || !layershell.LockSupported() {
		return errors.New(i18n.T("the compositor does not support session locks"))
	}
	if _, err := os.Stat(lock.Helper); err != nil {
		return fmt.Errorf("cannot check passwords: %w", err)
	}
	if l.inst == 0 {
		l.inst = layershell.NewLock()
		if layershell.AtLeast(1, 2) {
			gobject.SignalConnect(l.inst, "monitor", glib.NewCallback(&s.onLockMonitor))
		}
		gobject.SignalConnect(l.inst, "locked", glib.NewCallback(&s.onLockLocked))
		gobject.SignalConnect(l.inst, "failed", glib.NewCallback(&s.onLockFailed))
		gobject.SignalConnect(l.inst, "unlocked", glib.NewCallback(&s.onLockUnlocked))
	}
	if s.launcher.st.Open {
		s.cancelLauncher()
	}
	s.closeControl()
	s.closeAgenda()
	s.closeCenter()
	s.closeShot("")
	s.markLocked(true)
	l.locking, l.failures, l.notBefore = true, 0, time.Time{}
	if !layershell.Lock(l.inst) {
		// ::failed has already run, or will.
		return errors.New(i18n.T("could not take the session lock"))
	}
	if !layershell.AtLeast(1, 2) { // no ::monitor: every monitor now
		mons := gdk.DisplayGetDefault().GetMonitors()
		for i := range mons.GetNItems() {
			s.buildLockSurface(gdk.MonitorNewFromInternalPtr(mons.GetItem(i))) // a new reference
		}
	}
	return nil
}

// unlockScreen ends the lock without a password: for `inua call lock
// unlock`, which only the user's own processes can reach.
func (s *shellUI) unlockScreen() {
	if s.lock.inst != 0 && (s.lock.locked || s.lock.locking) {
		layershell.Unlock(s.lock.inst)
	}
}

// held reports whether the session lock is held or being taken.
func (l *locker) held() bool { return l.locked || l.locking }

// covers reports whether a monitor has a lock screen.
func (l *locker) covers(mon *gdk.Monitor) bool {
	return slices.ContainsFunc(l.surfaces, func(sf *lockSurface) bool { return sf.mon.GoPointer() == mon.GoPointer() })
}

// forget drops the lock screens of monitors that went away; the library
// destroys their windows when the lock ends.
func (l *locker) forget(gone func(*gdk.Monitor) bool) {
	l.surfaces = slices.DeleteFunc(l.surfaces, func(sf *lockSurface) bool {
		if !gone(sf.mon) {
			return false
		}
		sf.mon.Unref()
		return true
	})
}

func (s *shellUI) lockEnded() {
	l := &s.lock
	relock := l.relock
	l.locking, l.locked, l.checking, l.ending, l.relock = false, false, false, false, false
	l.forget(func(*gdk.Monitor) bool { return true })
	l.refs.Release()
	s.markLocked(false)
	s.syncBars()
	if relock {
		if err := s.lockScreen(); err != nil {
			log.Printf("lock: %v", err)
		}
	}
}

// buildLockSurface puts the lock screen on a monitor, taking over the
// caller's reference to it.
func (s *shellUI) buildLockSurface(mon *gdk.Monitor) {
	l := &s.lock
	sf := &lockSurface{mon: mon}
	ui := s.load("lock.ui", &l.refs)
	ui.Get("lock", &sf.win)
	ui.Get("date", &sf.date)
	ui.Get("time", &sf.clock)
	ui.Get("user", &sf.user)
	ui.Get("message", &sf.message)
	ui.Get("password", &sf.password)
	ui.Close()

	sf.user.SetText(userName())
	sf.password.ConnectActivate(&s.onLockActivate)
	sf.win.SetApplication(s.app)
	l.surfaces = append(l.surfaces, sf)
	s.showLockClock()
	s.showLockMessage("", false)
	layershell.AssignLockWindow(l.inst, sf.win.GoPointer(), mon.GoPointer())
	sf.win.Present()
	sf.password.GrabFocus()
	log.Printf("lock: lock screen on %q", mon.GetConnector())
}

func (s *shellUI) showLockClock() {
	now := time.Now()
	for _, sf := range s.lock.surfaces {
		sf.clock.SetText(now.Format("15:04"))
		sf.date.SetText(strings.ToUpper(i18n.Time(now, "Monday, January 2")))
	}
	l := &s.lock
	if !l.checking && l.failures > 0 && !l.notBefore.IsZero() {
		if wait := time.Until(l.notBefore); wait > 0 {
			s.showLockMessage(i18n.Tf("Wrong password. Try again in %d s", int(wait.Seconds()+0.99)), true)
		} else {
			l.notBefore = time.Time{}
			s.showLockMessage(i18n.T("Wrong password"), true)
		}
	}
}

func (s *shellUI) showLockMessage(text string, bad bool) {
	for _, sf := range s.lock.surfaces {
		sf.message.SetText(text)
		sf.message.SetVisible(text != "")
		toggle(&sf.message.Widget, "error", bad)
	}
}

// tryUnlock checks a password and unlocks if it is the user's.
func (s *shellUI) tryUnlock(password string) {
	l := &s.lock
	for _, sf := range l.surfaces {
		sf.password.SetText("")
	}
	if !l.locked || l.checking || password == "" || time.Now().Before(l.notBefore) {
		return
	}
	l.checking = true
	s.showLockMessage(i18n.T("Checking…"), false)
	for _, sf := range l.surfaces {
		sf.password.SetSensitive(false)
	}
	name := currentUser()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := lock.Check(ctx, lock.Helper, name, password)
		cancel()
		gtkx.Post(func() {
			if !l.locked {
				return
			}
			l.checking = false
			for _, sf := range l.surfaces {
				sf.password.SetSensitive(true)
			}
			switch {
			case err == nil:
				l.failures = 0
				layershell.Unlock(l.inst)
				return
			case errors.Is(err, lock.ErrWrong):
				l.failures++
				if wait := lock.Backoff(l.failures); wait > 0 {
					l.notBefore = time.Now().Add(wait)
				}
				s.showLockMessage(i18n.T("Wrong password"), true)
				s.showLockClock() // the wait, if there is one
			default:
				log.Printf("lock: %v", err)
				s.showLockMessage(i18n.T("The password could not be checked"), true)
			}
			for _, sf := range l.surfaces {
				if sf.win.IsActive() {
					sf.password.GrabFocus()
				}
			}
		})
	}()
}

func currentUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

// userName is the user's full name, else their login name.
func userName() string {
	if u, err := user.Current(); err == nil {
		full, _, _ := strings.Cut(u.Name, ",") // GECOS: the name, then more
		return cmp.Or(strings.TrimSpace(full), u.Username)
	}
	return os.Getenv("USER")
}

// lockMarker is the file that says this instance's screen is locked, in
// the runtime directory.
func (s *shellUI) lockMarker() string {
	dir, err := rundir.Dir()
	if err != nil {
		log.Printf("lock: %v", err)
		return "" // no marker rather than one somewhere else
	}
	return filepath.Join(dir, "locked-"+cmp.Or(s.opts.Instance, "default"))
}

func (s *shellUI) markLocked(on bool) {
	if s.lockMarker() == "" {
		return
	}
	if !on {
		os.Remove(s.lockMarker())
		return
	}
	if err := os.WriteFile(s.lockMarker(), nil, 0o600); err != nil {
		log.Printf("lock: %v", err)
	}
}

func (s *shellUI) lockMarked() bool {
	_, err := os.Stat(s.lockMarker())
	return err == nil
}
