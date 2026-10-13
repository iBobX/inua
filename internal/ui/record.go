package ui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/notify"
)

// Recording the screen is the screenshot tool with Record on: the area,
// window or screen chosen is recorded by wf-recorder until the bar's red
// square (or `inua call screenshot stop`) stops it.

// recordingEntry marks a recording's notification; its actions carry the
// file.
const recordingEntry = "inua-recording"

type recorder struct {
	pending []string // what to start once the tool is off screen
	cmd     *exec.Cmd
	file    string
	started time.Time
	ticking bool
}

func (s *shellUI) bindRecordHandlers() {
	s.onRecordTick = func(uintptr) bool {
		r := &s.recording
		if r.cmd == nil {
			r.ticking = false
			return false
		}
		s.showRecording()
		return true
	}
	s.onRecButton = func(gtk.Button) { s.stopScreenRecording() }
	s.onRecordStart = func(uintptr) bool {
		if args := s.recording.pending; args != nil {
			s.recording.pending = nil
			s.startScreenRecording(args)
		}
		return false
	}
}

// recording reports whether the screen is being recorded.
func (s *shellUI) recordingNow() bool { return s.recording.cmd != nil }

// startScreenRecording records the screen with wf-recorder's arguments: "-g",
// "X,Y WxH" for an area, or "-o", NAME for a monitor.
func (s *shellUI) startScreenRecording(args []string) {
	r := &s.recording
	if r.cmd != nil {
		return
	}
	if _, err := exec.LookPath("wf-recorder"); err != nil {
		s.recordNote(i18n.T("Recording needs wf-recorder"), i18n.T("Install it: sudo apt install wf-recorder"), "")
		return
	}
	folder := s.cfg.RecordingFolder()
	if err := os.MkdirAll(folder, 0o755); err != nil {
		log.Printf("record: %v", err)
		return
	}
	file := filepath.Join(folder, "Recording_"+time.Now().Format("2006-01-02_15-04-05")+".mp4")
	cmd := exec.Command("wf-recorder", append(args, "-f", file)...)
	var out strings.Builder
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		log.Printf("record: %v", err)
		return
	}
	r.cmd, r.file, r.started = cmd, file, time.Now()
	log.Printf("record: %s %v", file, args)
	go func() {
		err := cmd.Wait()
		gtkx.Post(func() { s.recordingEnded(err, out.String()) })
	}()
	s.showRecording()
	if !r.ticking {
		r.ticking = true
		glib.TimeoutAdd(1000, &s.onRecordTick, 0)
	}
}

// stopScreenRecording asks wf-recorder to finish the file.
func (s *shellUI) stopScreenRecording() {
	if r := &s.recording; r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Signal(syscall.SIGINT)
	}
}

func (s *shellUI) recordingEnded(err error, stderr string) {
	r := &s.recording
	file, took := r.file, time.Since(r.started)
	r.cmd = nil
	s.showRecording()
	if st, statErr := os.Stat(file); statErr != nil || st.Size() == 0 {
		log.Printf("record: %v %v %s", err, statErr, strings.TrimSpace(stderr))
		s.recordNote(i18n.T("The recording failed"), lastLine(stderr), "")
		return
	}
	s.recordNote(i18n.T("Recording saved"), fmt.Sprintf("%s · %s", filepath.Base(file), clockDuration(took)), file)
	// The drawer, the recording first, to drag into an app; not while
	// another capture is open or over a locked screen.
	sh := &s.shot
	if !sh.open && !sh.busy && !s.lock.held() {
		sh.record = true
		s.openShotDrawer()
	}
}

// showRecording shows the red square and the time so far in every bar
// while recording.
func (s *shellUI) showRecording() {
	r := &s.recording
	for _, b := range s.bars {
		b.rec.SetVisible(r.cmd != nil)
		if r.cmd != nil {
			b.recTime.SetText(clockDuration(time.Since(r.started)))
		}
	}
}

func clockDuration(d time.Duration) string {
	sec := int(d.Seconds())
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// recordNote tells how a recording went; with a file, it opens it or
// shows it in its folder.
func (s *shellUI) recordNote(summary, body, file string) {
	n := notify.Notification{
		App: i18n.T("Screen recording"), Entry: recordingEntry, Icon: "media-record-symbolic",
		Summary: summary, Body: notify.Markup(body), Time: time.Now(), Transient: file == "",
	}
	if file != "" {
		n.Actions = []notify.Action{{Key: "open:" + file, Label: i18n.T("Open")}, {Key: "folder:" + file, Label: i18n.T("Show in Folder")}}
	}
	if s.notes.srv == nil {
		go run(fmt.Sprintf("notify-send -a 'Screen recording' %s %s", shellQuote(summary), shellQuote(body)))
		return
	}
	n.ID = s.notes.srv.NewID()
	s.postNote(n)
}

// recordingNoteAction opens a recording or shows it in its folder.
func (s *shellUI) recordingNoteAction(_ notify.Notification, key string) {
	if file, ok := strings.CutPrefix(key, "folder:"); ok {
		s.shotNoteAction(notify.Notification{ImgPath: file}, "folder")
	} else if file, ok := strings.CutPrefix(key, "open:"); ok {
		s.shotNoteAction(notify.Notification{ImgPath: file}, "default")
	}
}
