package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"

	"github.com/lyricat/inua/internal/apps/clock"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/notify"
)

// alarmEntry marks the notifications of alarms going off.
const alarmEntry = "inua-alarm"

// alarmSound is the freedesktop sound theme's alarm.
const alarmSound = "/usr/share/sounds/freedesktop/stereo/alarm-clock-elapsed.oga"

// snoozeFor is how long Snooze waits.
const snoozeFor = 9 * time.Minute

// alarms rings the Clock app's alarms, which the shell does so they go
// off with the app closed. It reads the app's file when it changes, and
// turns a once alarm off after it rings.
type alarms struct {
	data    clock.Data
	mod     time.Time
	last    time.Time // alarms due after this have not rung yet
	snoozed []snoozed
	ringing map[uint32]context.CancelFunc // by notification, the sound until it stops
}

type snoozed struct {
	alarm clock.Alarm
	at    time.Time
}

func (s *shellUI) startAlarms() {
	s.alarms.last = time.Now()
	s.alarms.ringing = map[uint32]context.CancelFunc{}
	s.onAlarmTick = func(uintptr) bool {
		s.checkAlarms(time.Now())
		return true
	}
	glib.TimeoutAdd(5000, &s.onAlarmTick, 0)
}

func (s *shellUI) checkAlarms(now time.Time) {
	al := &s.alarms
	if st, err := os.Stat(store.Path(clock.StoreName)); err == nil && !st.ModTime().Equal(al.mod) {
		al.data = clock.Data{}
		if err := store.Load(clock.StoreName, &al.data); err != nil {
			log.Printf("alarms: %v", err)
		}
		al.mod = st.ModTime()
	}
	if now.Sub(al.last) > 10*time.Minute {
		// Asleep or suspended: ring nothing from long ago.
		al.last = now.Add(-10 * time.Second)
	}
	due := clock.Due(al.data.Alarms, al.last, now)
	al.last = now
	for i := 0; i < len(al.snoozed); {
		if !al.snoozed[i].at.After(now) {
			due = append(due, al.snoozed[i].alarm)
			al.snoozed = slices.Delete(al.snoozed, i, i+1)
			continue
		}
		i++
	}
	turnedOff := false
	for _, a := range due {
		s.ringAlarm(a)
		if a.Once() {
			for i := range al.data.Alarms {
				if al.data.Alarms[i].ID == a.ID && al.data.Alarms[i].On {
					al.data.Alarms[i].On = false
					turnedOff = true
				}
			}
		}
	}
	if turnedOff {
		if err := store.Save(clock.StoreName, al.data); err != nil {
			log.Printf("alarms: %v", err)
		}
		if st, err := os.Stat(store.Path(clock.StoreName)); err == nil {
			al.mod = st.ModTime()
		}
	}
}

// ringAlarm shows the alarm's notification and plays the sound until it
// is stopped, snoozed or cleared, or for five minutes.
func (s *shellUI) ringAlarm(a clock.Alarm) {
	summary := fmt.Sprintf("Alarm · %02d:%02d", a.Hour, a.Minute)
	n := notify.Notification{
		App: "Clock", Entry: alarmEntry, Icon: "alarm-symbolic",
		Summary: summary, Body: notify.Markup(a.Label), Urgency: notify.Critical, Time: time.Now(),
		Default: true, Actions: []notify.Action{{Key: "snooze:" + a.ID, Label: "Snooze"}, {Key: "stop", Label: "Stop"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if s.notes.srv != nil {
		n.ID = s.notes.srv.NewID()
		s.alarms.ringing[n.ID] = cancel
		s.postNote(n)
	} else {
		go run(fmt.Sprintf("notify-send -u critical -a Clock %s %s", shellQuote(summary), shellQuote(a.Label)))
	}
	log.Printf("alarms: %s %s", summary, a.Label)
	go func() {
		defer cancel()
		for ctx.Err() == nil {
			if err := exec.CommandContext(ctx, "pw-play", alarmSound).Run(); err != nil && ctx.Err() == nil {
				log.Printf("alarms: sound: %v", err)
				return
			}
			select {
			case <-ctx.Done():
			case <-time.After(1500 * time.Millisecond):
			}
		}
	}()
}

// alarmNoteAction is a click on an alarm's notification: Snooze rings it
// again in nine minutes; anything else stops it.
func (s *shellUI) alarmNoteAction(n notify.Notification, key string) {
	s.stopAlarmSound(n.ID)
	if id, ok := strings.CutPrefix(key, "snooze:"); ok {
		for _, a := range s.alarms.data.Alarms {
			if a.ID == id {
				s.alarms.snoozed = append(s.alarms.snoozed, snoozed{alarm: a, at: time.Now().Add(snoozeFor)})
			}
		}
	}
}

// stopAlarmSound stops a ringing alarm's sound.
func (s *shellUI) stopAlarmSound(id uint32) {
	if cancel, ok := s.alarms.ringing[id]; ok {
		cancel()
		delete(s.alarms.ringing, id)
	}
}
