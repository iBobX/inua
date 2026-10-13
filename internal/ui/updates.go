package ui

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/notify"
	"github.com/lyricat/inua/internal/upgrade"
)

// updateEntry marks the notifications about new releases.
const updateEntry = "inua-update"

// updatesStore keeps what the daily check remembers
// (~/.local/share/inua/updates.json).
const updatesStore = "updates"

// startUpdates looks for a new release a random few minutes after login,
// then once a day (notifications.updates), and says so in a notification.
// The wait spreads everyone's checks out, and keeps them out of the way
// of logging in. A build from source never checks.
func (s *shellUI) startUpdates() {
	v := s.opts.Version
	if !upgrade.IsRelease(v) {
		return
	}
	s.noteInstalled(v)
	wait := time.Minute + rand.N(9*time.Minute)
	log.Printf("updates: first check in %s", wait.Round(time.Second))
	go func() {
		time.Sleep(wait)
		for {
			s.checkUpdates(v)
			time.Sleep(time.Hour) // checks only once it is due
		}
	}()
}

// checkUpdates asks GitHub for the latest release, if a day has passed
// since it last did, and announces a newer one once.
func (s *shellUI) checkUpdates(v string) {
	on := make(chan bool)
	gtkx.Post(func() { on <- s.cfg.Notify.Updates })
	if !<-on {
		return
	}
	var st upgrade.State
	if err := store.Load(updatesStore, &st); err != nil {
		log.Printf("updates: %v", err)
	}
	now := time.Now()
	if !st.Due(now) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tag, err := upgrade.GitHub().Latest(ctx)
	if err != nil {
		log.Printf("updates: %v", err) // offline: try again in an hour
		return
	}
	st.Checked = now
	announce := st.Announce(v, tag)
	if announce {
		st.Notified = tag
	}
	if err := store.Save(updatesStore, st); err != nil {
		log.Printf("updates: %v", err)
	}
	log.Printf("updates: latest release %s, running %s", tag, v)
	if announce {
		gtkx.Post(func() { s.noteUpdate(v, tag) })
	}
}

// noteUpdate says a new release is out, with buttons to upgrade and to
// read what is new.
func (s *shellUI) noteUpdate(v, tag string) {
	summary := i18n.Tf("inua %s is out", strings.TrimPrefix(tag, "v"))
	body := i18n.Tf("You have %s.", v)
	if s.notes.srv == nil {
		go run(fmt.Sprintf("notify-send -a inua %s %s", shellQuote(summary), shellQuote(i18n.Tf("%s Upgrade with: inua upgrade", body))))
		return
	}
	s.postNote(notify.Notification{
		ID: s.notes.srv.NewID(), App: "inua", Entry: updateEntry, Icon: "software-update-available-symbolic",
		Summary: summary, Body: notify.Markup(body), Urgency: notify.Normal, Time: time.Now(),
		Default: true, Actions: []notify.Action{{Key: "upgrade:" + tag, Label: i18n.T("Upgrade")}, {Key: "notes:" + tag, Label: i18n.T("What's new")}},
	})
}

// noteInstalled says so when the shell starts on a newer version than it
// last ran, as after Upgrade.
func (s *shellUI) noteInstalled(v string) {
	var st upgrade.State
	if err := store.Load(updatesStore, &st); err != nil {
		log.Printf("updates: %v", err)
		return
	}
	if st.Running == v {
		return
	}
	newer := false
	if order, ok := upgrade.Compare(st.Running, v); ok && order < 0 {
		newer = true
	}
	st.Running = v
	if err := store.Save(updatesStore, st); err != nil {
		log.Printf("updates: %v", err)
	}
	if newer && s.notes.srv != nil {
		tag := "v" + strings.TrimPrefix(v, "v")
		s.postNote(notify.Notification{
			ID: s.notes.srv.NewID(), App: "inua", Entry: updateEntry, Icon: "software-update-available-symbolic",
			Summary: i18n.Tf("inua %s is installed", v), Urgency: notify.Low, Time: time.Now(),
			Default: true, Actions: []notify.Action{{Key: "notes:" + tag, Label: i18n.T("What's new")}},
		})
	}
}

// updateNoteAction is a click on a release's notification: Upgrade runs
// inua upgrade, which restarts the shell on the new version; the
// notification itself, or What's new, opens the release's page.
func (s *shellUI) updateNoteAction(n notify.Notification, key string) {
	if key == "default" && len(n.Actions) > 0 {
		key = n.Actions[len(n.Actions)-1].Key // What's new
	}
	what, tag, _ := strings.Cut(key, ":")
	switch what {
	case "notes":
		go func() {
			if err := exec.Command("xdg-open", upgrade.GitHub().ReleaseURL(tag)).Run(); err != nil {
				log.Printf("updates: %v", err)
			}
		}()
	case "upgrade":
		go s.runUpgrade(tag)
	}
}

func (s *shellUI) runUpgrade(tag string) (said string) {
	prog := "inua" // next to inua-shell, else on PATH
	if exe, err := executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "inua"); fileExists(p) {
			prog = p
		}
	}
	log.Printf("updates: %s upgrade -version %s", prog, tag)
	out, err := exec.Command(prog, "upgrade", "-version", tag).CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	msg := strings.TrimPrefix(lines[len(lines)-1], "inua: ")
	if err == nil {
		// Installed, the shell restarts on the new version and says so;
		// still here, there was nothing to do, and msg says why.
		log.Printf("updates: upgrade: %s", msg)
		return msg
	}
	log.Printf("updates: upgrade: %v %s", err, msg)
	gtkx.Post(func() {
		if s.notes.srv == nil {
			return
		}
		s.postNote(notify.Notification{
			ID: s.notes.srv.NewID(), App: "inua", Entry: updateEntry, Icon: "dialog-warning-symbolic",
			Summary: i18n.T("inua could not upgrade"), Body: notify.Markup(cmp.Or(msg, err.Error())), Urgency: notify.Normal, Time: time.Now(),
		})
	})
	return i18n.Tf("Could not upgrade: %s", cmp.Or(msg, err.Error()))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
