package filesui

import (
	"path/filepath"
	"strconv"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// renamer renames many items on a card: the way (replace, add, number,
// case), its fields, built again when the way changes, and a preview of
// every new name with what stops it.
type renamer struct {
	w     *window
	c     *card
	paths []string // in the pane's order
	rule  files.RenameRule
	plan  []files.Renamed
	bad   string // what is wrong with the rule itself
	ptrs  []uintptr

	preview *gtk.Box
	note    *gtk.Label
}

// previewMost is how many new names the preview lists.
const previewMost = 200

func (w *window) askRenameMany(p *pane, paths []string) {
	r := &renamer{w: w, paths: paths, rule: w.app.renameRule}
	r.c = &card{def: 1, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Rename"), class: "suggested-action", action: r.submit},
	}}
	r.c.closed = func() { w.app.h.forget(r.ptrs) }
	w.openCard(r.c, cardParts{title: i18n.Nf("Rename %d item", "Rename %d items", len(paths), len(paths)), form: true})
	r.build()
}

// build makes the card's form for the way chosen.
func (r *renamer) build() {
	w := r.w
	w.app.h.forget(r.ptrs)
	r.ptrs = nil
	gtkx.Clear(&w.cardForm)

	modes := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	modes.AddCssClass("files-kinds")
	modes.SetHalign(gtk.AlignStartValue)
	var first *gtk.ToggleButton
	for _, m := range []struct {
		mode  files.RenameMode
		label string
	}{
		{files.ReplaceText, i18n.T("Replace text")},
		{files.AddText, i18n.T("Add text")},
		{files.Number, i18n.T("Number")},
		{files.ChangeCase, i18n.T("Change case")},
	} {
		b := gtk.NewToggleButtonWithLabel(m.label)
		if first == nil {
			first = b
		} else {
			b.SetGroup(first)
		}
		b.SetActive(m.mode == r.rule.Mode)
		w.app.h.toggle(b, func(on bool) {
			if on && m.mode != r.rule.Mode {
				r.rule.Mode = m.mode
				gtkx.Post(r.build) // not from within the button's own signal
			}
		})
		r.ptrs = append(r.ptrs, b.GoPointer())
		gtkx.Append(modes, &b.Widget)
	}
	gtkx.Append(&w.cardForm, &modes.Widget)

	grid := gtk.NewGrid()
	grid.SetRowSpacing(8)
	grid.SetColumnSpacing(14)
	row := int32(0)
	label := func(text string) {
		l := gtk.NewLabel(text)
		l.AddCssClass("files-form-label")
		l.SetXalign(1)
		grid.Attach(&l.Widget, 0, row, 1, 1)
	}
	var focus *gtk.Entry
	entry := func(text, hint string, set func(string)) *gtk.Entry {
		e := gtk.NewEntry()
		e.SetHexpand(true)
		e.SetPlaceholderText(hint)
		e.SetText(text)
		w.app.h.enter(e, r.submit)
		w.app.h.edit(e, func() {
			set(e.GetText())
			r.update()
		})
		r.ptrs = append(r.ptrs, e.GoPointer())
		grid.Attach(&e.Widget, 1, row, 1, 1)
		if focus == nil {
			focus = e
		}
		return e
	}
	check := func(text string, on bool, set func(bool)) {
		c := gtk.NewCheckButtonWithLabel(text)
		c.SetActive(on)
		w.app.h.check(c, func(v bool) {
			set(v)
			r.update()
		})
		r.ptrs = append(r.ptrs, c.GoPointer())
		grid.Attach(&c.Widget, 1, row, 1, 1)
	}
	choice := func(labels []string, chosen int, set func(int)) {
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		box.AddCssClass("files-kinds")
		box.SetHalign(gtk.AlignStartValue)
		var first *gtk.ToggleButton
		for i, l := range labels {
			b := gtk.NewToggleButtonWithLabel(l)
			if first == nil {
				first = b
			} else {
				b.SetGroup(first)
			}
			b.SetActive(i == chosen)
			w.app.h.toggle(b, func(on bool) {
				if on {
					set(i)
					r.update()
				}
			})
			r.ptrs = append(r.ptrs, b.GoPointer())
			gtkx.Append(box, &b.Widget)
		}
		grid.Attach(&box.Widget, 1, row, 1, 1)
	}

	switch r.rule.Mode {
	case files.ReplaceText:
		label(i18n.T("Find"))
		entry(r.rule.Find, "", func(s string) { r.rule.Find = s })
		row++
		label(i18n.T("Replace with"))
		entry(r.rule.Replace, i18n.T("empty: removed"), func(s string) { r.rule.Replace = s })
		row++
		check(i18n.T("Match case"), r.rule.MatchCase, func(v bool) { r.rule.MatchCase = v })
		row++
		check(i18n.T("A regular expression ($1 in the replacement)"), r.rule.Regexp, func(v bool) { r.rule.Regexp = v })
	case files.AddText:
		label(i18n.T("Text"))
		entry(r.rule.Text, "", func(s string) { r.rule.Text = s })
		row++
		label(i18n.T("Where"))
		after := 0
		if r.rule.After {
			after = 1
		}
		choice([]string{i18n.T("Before the name"), i18n.T("After the name")}, after, func(i int) { r.rule.After = i == 1 })
	case files.Number:
		label(i18n.T("Name"))
		entry(r.rule.Text, i18n.T("empty: the number alone"), func(s string) { r.rule.Text = s })
		row++
		label(i18n.T("Start at"))
		start := r.rule.Start
		if start == 0 {
			start = 1
		}
		e := entry(strconv.Itoa(start), "1", func(s string) {
			n, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil || n < 0 {
				n = 1
			}
			r.rule.Start = n
		})
		e.SetInputPurpose(gtk.InputPurposeDigitsValue)
		e.SetHexpand(false)
		e.SetWidthChars(6)
	case files.ChangeCase:
		label(i18n.T("Case"))
		choice([]string{i18n.T("lowercase"), i18n.T("UPPERCASE"), i18n.T("Title Case")}, r.rule.Case, func(i int) { r.rule.Case = i })
	}
	gtkx.Append(&w.cardForm, &grid.Widget)

	r.note = gtk.NewLabel("")
	r.note.AddCssClass("sub")
	r.note.SetXalign(0)
	gtkx.Append(&w.cardForm, &r.note.Widget)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetMaxContentHeight(260)
	scroll.AddCssClass("files-rename-preview")
	r.preview = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	scroll.SetChild(&r.preview.Widget)
	gtkx.Append(&w.cardForm, &scroll.Widget)

	r.update()
	if focus != nil {
		focus.GrabFocus()
	}
}

// update plans the new names and shows them.
func (r *renamer) update() {
	w := r.w
	if w.card != r.c {
		return
	}
	plan, err := files.Plan(r.paths, r.rule)
	r.plan, r.bad = plan, ""
	if err != nil {
		r.bad = i18n.T("The regular expression is not one.")
	}
	gtkx.Clear(r.preview)
	changed, problems := 0, 0
	for i, rn := range plan {
		if rn.To != rn.From {
			changed++
		}
		if rn.Problem != "" {
			problems++
		}
		if i >= previewMost {
			continue
		}
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.AddCssClass("files-rename-row")
		from := gtk.NewLabel(filepath.Base(rn.From))
		from.AddCssClass("sub")
		from.SetXalign(0)
		from.SetEllipsize(2)         // middle
		from.SetSizeRequest(180, -1) // the arrows in one column
		gtkx.Append(row, &from.Widget)
		arrow := gtk.NewLabel("→")
		arrow.AddCssClass("sub")
		gtkx.Append(row, &arrow.Widget)
		to := gtk.NewLabel(filepath.Base(rn.To))
		to.SetXalign(0)
		to.SetEllipsize(3) // end: the problem stays readable
		to.SetHexpand(true)
		to.SetSizeRequest(1, -1)
		if rn.To == rn.From && rn.Problem == "" {
			to.AddCssClass("sub")
		}
		if rn.Problem != "" {
			to.AddCssClass("files-rename-problem")
			to.SetTooltipText(problemText(rn.Problem))
			to.SetText(filepath.Base(rn.To) + " — " + problemText(rn.Problem))
		}
		gtkx.Append(row, &to.Widget)
		gtkx.Append(r.preview, &row.Widget)
	}
	note := i18n.Nf("%d name changes.", "%d names change.", changed, changed)
	if problems > 0 {
		note = i18n.Nf("%d name cannot be given.", "%d names cannot be given.", problems, problems)
	}
	if len(plan) > previewMost {
		note += " " + i18n.Tf("The first %d are shown.", previewMost)
	}
	r.note.SetText(note)
	setClass(&r.note.Widget, "files-rename-problem", problems > 0)
	w.cardFail(r.bad)
}

// problemText is a plan's problem as the preview says it.
func problemText(p string) string {
	switch p {
	case files.ErrNameTaken.Error():
		return i18n.T("taken by another file")
	case files.ErrNameTwice.Error():
		return i18n.T("given to two items")
	case files.ErrNameEmpty.Error():
		return i18n.T("empty")
	case files.ErrNameSlash.Error():
		return i18n.T("holds “/”")
	case files.ErrNameDots.Error():
		return i18n.T("not a name")
	case files.ErrNameTooLong.Error():
		return i18n.T("too long")
	}
	return p
}

// submit renames the items whose names change, when none is stopped.
func (r *renamer) submit() {
	w := r.w
	if w.card != r.c {
		return
	}
	if r.bad != "" {
		w.cardFail(r.bad)
		return
	}
	var job files.Job
	job.Kind = files.RenameMany
	for _, rn := range r.plan {
		if rn.Problem != "" {
			w.cardFail(i18n.T("Some names cannot be given: they are marked."))
			return
		}
		if rn.To != rn.From {
			job.Sources = append(job.Sources, rn.From)
			job.Dests = append(job.Dests, rn.To)
		}
	}
	w.closeCard()
	w.app.renameRule = r.rule
	if len(job.Sources) > 0 {
		w.app.run(w, job)
	}
}
