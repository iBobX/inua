package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/assistant/answer"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The Benchmark dialog of Settings → AI, as Mister Morph's Console has it:
// a row for each benchmark of the profile, running, then passed or failed
// with how long it took; a row clicked shows what the model answered. It is
// a card over the Settings window, not a window of its own: a dialog
// window kept above its parent could cover inua-keyring's and gpg's
// questions, which a benchmark may well bring up.
// inua-assistant --benchmark runs them (assistant.Benchmark): the shell
// does not run Mister Morph.

// benchHead and benchResult are inua-assistant --benchmark's lines.
type benchHead struct {
	Profile, Provider, Model, Endpoint string
	Tests                              []string
	Error                              string
}

type benchResult struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Millis int64  `json:"ms"`
	Detail string `json:"detail"`
	Error  string `json:"error"`
	Raw    string `json:"raw"`
}

// benchDialog is the open dialog.
type benchDialog struct {
	scrim   *gtk.Box // over the window, the card in its middle
	profile string
	target  *gtk.Label
	note    *gtk.Label // why nothing runs
	list    *gtk.Box
	again   *gtk.Button
	rows    map[string]*benchRow
	cancel  context.CancelFunc
	running bool

	onAgain, onClose, onRow func(gtk.Button)
}

// benchRow is one benchmark's row.
type benchRow struct {
	id                    string
	summary               *gtk.Button
	status, time, rawText *gtk.Label
	spinner               *gtk.Spinner
	raw                   *gtk.Revealer
}

// benchNames are the benchmarks as the dialog names them, and what a pass
// and a failure say.
func benchNames(id string) (name, ok, failed string) {
	switch id {
	case "text_reply":
		return i18n.T("Text reply"), i18n.T("The model answered"), i18n.T("The model did not answer")
	case "tool_calling":
		return i18n.T("Tool calling"), i18n.T("The model can call tools"), i18n.T("The model did not call the tool")
	case "quick_choice":
		return i18n.T("Quick choice (decision route)"), i18n.T("The model picks from a list"), i18n.T("The model did not pick the right option")
	}
	return id, "", ""
}

// benchAI opens the Benchmark dialog for a profile and runs it, the
// settings saved first: inua-assistant reads them from config.toml.
func (s *shellUI) benchAI(profile string) {
	st := &s.settings
	if st.dirty {
		s.saveSettings()
	}
	if st.win == nil {
		return
	}
	s.closeBench()
	d := &benchDialog{profile: profile, rows: map[string]*benchRow{}}
	st.ai.bench = d
	// The window's page under an overlay, once, for the card to go over.
	if st.overlay == nil {
		page := st.win.GetChild()
		page.Ref()
		st.win.SetChild(nil)
		st.overlay = gtk.NewOverlay()
		st.overlay.SetChild(page)
		page.Unref()
		st.win.SetChild(&st.overlay.Widget)
		st.overlay.Unref() // the window's
	}
	d.scrim = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	d.scrim.AddCssClass("bench-scrim")
	box := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	box.AddCssClass("bench-dialog")
	box.SetHalign(gtk.AlignCenterValue)
	box.SetValign(gtk.AlignCenterValue)
	box.SetHexpand(true)
	box.SetVexpand(true)
	box.SetSizeRequest(520, -1)
	head := gtk.NewLabel(i18n.T("Benchmark"))
	head.AddCssClass("title")
	head.SetXalign(0)
	gtkx.Append(box, &head.Widget)
	d.target = gtk.NewLabel(profile)
	d.target.AddCssClass("sub")
	d.target.SetXalign(0)
	d.target.SetWrap(true)
	box.Append(&d.target.Widget)
	d.note = gtk.NewLabel("")
	d.note.AddCssClass("sub")
	d.note.AddCssClass("warn-text")
	d.note.SetXalign(0)
	d.note.SetWrap(true)
	d.note.SetSelectable(true)
	d.note.SetVisible(false)
	box.Append(&d.note.Widget)
	d.list = gtk.NewBox(gtk.OrientationVerticalValue, 8)
	box.Append(&d.list.Widget)
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	buttons.SetMarginTop(6)
	d.again = gtk.NewButtonWithLabel(i18n.T("Run again"))
	d.again.AddCssClass("outlined")
	closeButton := gtk.NewButtonWithLabel(i18n.T("Close"))
	closeButton.AddCssClass("outlined")
	d.onAgain = func(gtk.Button) { s.runBench(d) }
	d.onClose = func(gtk.Button) { s.closeBench() }
	d.onRow = func(b gtk.Button) {
		for _, r := range d.rows {
			if r.summary.GoPointer() == b.GoPointer() {
				r.raw.SetRevealChild(!r.raw.GetRevealChild())
			}
		}
	}
	d.again.ConnectClicked(&d.onAgain)
	closeButton.ConnectClicked(&d.onClose)
	buttons.Append(&d.again.Widget)
	gtkx.Append(buttons, &closeButton.Widget)
	gtkx.Append(box, &buttons.Widget)
	gtkx.Append(d.scrim, &box.Widget)
	st.overlay.AddOverlay(&d.scrim.Widget)
	d.scrim.Unref() // the overlay's
	closeButton.GrabFocus()
	s.runBench(d)
}

// closeBench closes the Benchmark dialog, stopping what it runs; false
// when none is open.
func (s *shellUI) closeBench() bool {
	st := &s.settings
	d := st.ai.bench
	if d == nil {
		return false
	}
	st.ai.bench = nil
	if d.cancel != nil {
		d.cancel()
	}
	if st.overlay != nil {
		st.overlay.RemoveOverlay(&d.scrim.Widget)
	}
	return true
}

// runBench runs the benchmarks again, filling the rows as they end.
func (s *shellUI) runBench(d *benchDialog) {
	if d.running {
		return
	}
	d.running = true
	d.again.SetSensitive(false)
	d.again.SetLabel(i18n.T("Running…"))
	d.note.SetVisible(false)
	for _, r := range d.rows {
		s.benchRowRunning(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go func() {
		err := benchRun(ctx, d.profile, func(h benchHead) {
			gtkx.Post(func() { s.benchHead(d, h) })
		}, func(r benchResult) {
			gtkx.Post(func() { s.benchResult(d, r) })
		})
		cancel()
		gtkx.Post(func() {
			d.running = false
			d.again.SetSensitive(true)
			d.again.SetLabel(i18n.T("Run again"))
			if err != nil && ctx.Err() == nil {
				d.note.SetText(err.Error())
				d.note.SetVisible(true)
			}
		})
	}()
}

// benchRun runs inua-assistant --benchmark profile, handing on its lines.
func benchRun(ctx context.Context, profile string, head func(benchHead), result func(benchResult)) error {
	prog, err := answer.Program()
	if err != nil {
		return fmt.Errorf("%s", i18n.T("inua-assistant is not installed"))
	}
	cmd := exec.CommandContext(ctx, prog, "--benchmark", profile)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	first, failed := true, ""
	for sc.Scan() {
		if first {
			first = false
			var h benchHead
			if json.Unmarshal(sc.Bytes(), &h) == nil {
				failed = h.Error
				head(h)
			}
			continue
		}
		var r benchResult
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.ID != "" {
			result(r)
		}
	}
	err = cmd.Wait()
	switch {
	case failed != "":
		return fmt.Errorf("%s", failed)
	case err != nil && ctx.Err() == nil:
		log.Printf("settings: benchmark: %v: %s", err, stderr.String())
		if last := lastLine(stderr.String()); last != "" {
			return fmt.Errorf("%s", last)
		}
		return err
	}
	return nil
}

// benchHead names the target and makes a row for each benchmark to come.
func (s *shellUI) benchHead(d *benchDialog, h benchHead) {
	if s.settings.ai.bench != d {
		return
	}
	target := []string{h.Profile}
	for _, v := range []string{h.Provider, h.Model, h.Endpoint} {
		if v != "" {
			target = append(target, v)
		}
	}
	d.target.SetText(strings.Join(target, " · "))
	if h.Error != "" {
		return
	}
	if len(d.rows) > 0 {
		return // run again: the same rows
	}
	for _, id := range h.Tests {
		r := &benchRow{id: id}
		d.rows[id] = r
		row := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		row.AddCssClass("bench-row")
		line := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		texts := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		texts.SetHexpand(true)
		name, _, _ := benchNames(id)
		title := gtk.NewLabel(name)
		title.AddCssClass("title")
		title.SetXalign(0)
		gtkx.Append(texts, &title.Widget)
		r.summary = gtk.NewButtonWithLabel("")
		r.summary.AddCssClass("flat")
		r.summary.AddCssClass("bench-summary")
		r.summary.SetHalign(gtk.AlignStartValue)
		r.summary.SetTooltipText(i18n.T("Show what the model answered"))
		r.summary.ConnectClicked(&d.onRow)
		texts.Append(&r.summary.Widget)
		gtkx.Append(line, &texts.Widget)
		side := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		side.SetValign(gtk.AlignCenterValue)
		r.spinner = gtk.NewSpinner()
		r.status = gtk.NewLabel("")
		r.status.AddCssClass("bench-status")
		r.status.SetXalign(1)
		r.time = gtk.NewLabel("")
		r.time.AddCssClass("sub")
		r.time.SetXalign(1)
		side.Append(&r.spinner.Widget)
		side.Append(&r.status.Widget)
		side.Append(&r.time.Widget)
		gtkx.Append(line, &side.Widget)
		gtkx.Append(row, &line.Widget)
		r.raw = gtk.NewRevealer()
		r.rawText = gtk.NewLabel("")
		r.rawText.AddCssClass("bench-raw")
		r.rawText.SetXalign(0)
		r.rawText.SetWrap(true)
		r.rawText.SetSelectable(true)
		r.raw.SetChild(&r.rawText.Widget)
		row.Append(&r.raw.Widget)
		gtkx.Append(d.list, &row.Widget)
		s.benchRowRunning(r)
	}
}

func (s *shellUI) benchRowRunning(r *benchRow) {
	r.spinner.SetVisible(true)
	r.spinner.Start()
	r.status.SetVisible(false)
	r.time.SetText("")
	r.summary.SetLabel(i18n.T("Running…"))
	r.summary.SetSensitive(false)
	r.raw.SetRevealChild(false)
}

// benchResult fills a benchmark's row.
func (s *shellUI) benchResult(d *benchDialog, res benchResult) {
	r := d.rows[res.ID]
	if s.settings.ai.bench != d || r == nil {
		return
	}
	_, ok, failed := benchNames(res.ID)
	r.spinner.Stop()
	r.spinner.SetVisible(false)
	r.status.SetVisible(true)
	r.status.SetText(i18n.T("Passed"))
	r.summary.SetLabel(ok)
	if !res.OK {
		r.status.SetText(i18n.T("Failed"))
		r.summary.SetLabel(failed)
	}
	toggle(&r.status.Widget, "ok", res.OK)
	toggle(&r.status.Widget, "failed", !res.OK)
	r.time.SetText(benchSeconds(res.Millis))
	var raw []string
	for _, v := range []string{res.Error, res.Raw} {
		if v != "" {
			raw = append(raw, v)
		}
	}
	if len(raw) == 0 {
		raw = []string{i18n.T("No answer")}
	}
	r.rawText.SetText(strings.Join(raw, "\n\n"))
	r.summary.SetSensitive(true)
}

// benchSeconds is a duration as the dialog shows it: "2.08s", "12.4s".
func benchSeconds(ms int64) string {
	sec := float64(ms) / 1000
	if sec >= 10 {
		return fmt.Sprintf("%.1fs", sec)
	}
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", sec), "0"), ".") + "s"
}
