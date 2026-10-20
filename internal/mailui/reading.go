package mailui

import (
	"fmt"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
)

// How a message reads: its text at the size Settings → Reading chooses,
// and the reader no wider than a line can be read across, its margins
// growing with the pane past that.

// readWidth is the widest a message reads at, at its own text size.
const readWidth = 720

// applyTextSize sets the size of messages' text: a style of Mail's own
// over style.css for the text the window draws, the zoom of the web views
// for HTML mail.
func (w *window) applyTextSize() {
	scale := w.settings.Reading.Scale()
	if w.textCSS == nil {
		if scale == 1 {
			return // style.css's sizes
		}
		w.textCSS = gtk.NewCssProvider()
		gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), w.textCSS, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)+1)
	}
	// style.css's sizes are scaled by inua's own text size as it loads;
	// these are too.
	whole := 1.0
	if cfg, _, err := config.Load(config.DefaultPath()); err == nil && cfg.Style.TextScale > 0 {
		whole = float64(cfg.Style.TextScale) / 100
	}
	px := func(n float64) string { return fmt.Sprintf("%.1fpx", n*scale*whole) }
	w.textCSS.LoadFromString(fmt.Sprintf(`
window.mail .mail-text, window.mail .mail-quoted, window.mail .mail-html { font-size: %s; }
window.mail .mail-html-h1 { font-size: %s; }
window.mail .mail-html-h2 { font-size: %s; }
window.mail .mail-html-h3, window.mail .mail-html-h4 { font-size: %s; }
`, px(14), px(19), px(17), px(15)))
	for _, wm := range w.webs {
		wm.zoom, wm.fitted, wm.height = scale, false, 0
		wk.setZoom(wm.view.Ptr, scale)
		w.measure(wm)
	}
	w.fitReader()
}

// fitReader keeps the reader's column at most readWidth wide (grown with
// the text), in the middle of the pane.
func (w *window) fitReader() {
	adj := w.readerScroll.GetHadjustment()
	if adj == nil {
		return
	}
	page := adj.GetPageSize()
	adj.Unref()
	most := float64(readWidth)*w.settings.Reading.Scale() + 80 // the reader's own padding, 40 a side
	margin := int32(0)
	if page > most {
		margin = int32((page - most) / 2)
	}
	if w.reader.GetMarginStart() != margin {
		w.reader.SetMarginStart(margin)
		w.reader.SetMarginEnd(margin)
	}
}
