package mailui

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
)

// HTML mail is shown by WebKitGTK when the system has it (6.0, the GTK 4
// one): the few functions Mail needs, bound on the first HTML message, so
// Mail starts as fast without it and works (with its own renderer) where
// it is not installed. Scripts in the mail do not run, nothing loads from
// the network unless its pictures are allowed (a content policy in the
// page, mail.PrepareWeb), and links open in the browser.
var wk struct {
	once sync.Once
	ok   bool

	viewNew       func() uintptr
	settings      func(view uintptr) uintptr
	setJSMarkup   func(s uintptr, on bool)
	setPageCache  func(s uintptr, on bool)
	setStorage    func(s uintptr, on bool)
	setWebGL      func(s uintptr, on bool)
	setGestures   func(s uintptr, on bool)
	setDefaultSz  func(s uintptr, px uint32)
	loadHTML      func(view uintptr, html, base string)
	setBG         func(view uintptr, rgba *gdk.RGBA)
	setZoom       func(view uintptr, level float64)
	evalJS        func(view uintptr, script string, length int, world, source, cancellable, cb, data uintptr)
	evalJSFinish  func(view, result, errp uintptr) uintptr
	jsToString    func(v uintptr) string
	navAction     func(decision uintptr) uintptr
	actionRequest func(action uintptr) uintptr
	actionType    func(action uintptr) int32
	requestURI    func(req uintptr) string
	ignore        func(decision uintptr)
	use           func(decision uintptr)
}

// webOK loads WebKitGTK the first time it is asked, and says whether it
// is there.
func webOK() bool {
	wk.once.Do(func() {
		lib, err := purego.Dlopen("libwebkitgtk-6.0.so.4", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		jsc, err := purego.Dlopen("libjavascriptcoregtk-6.0.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		for _, f := range []struct {
			fn   any
			lib  uintptr
			name string
		}{
			{&wk.viewNew, lib, "webkit_web_view_new"},
			{&wk.settings, lib, "webkit_web_view_get_settings"},
			{&wk.setJSMarkup, lib, "webkit_settings_set_enable_javascript_markup"},
			{&wk.setPageCache, lib, "webkit_settings_set_enable_page_cache"},
			{&wk.setStorage, lib, "webkit_settings_set_enable_html5_local_storage"},
			{&wk.setWebGL, lib, "webkit_settings_set_enable_webgl"},
			{&wk.setGestures, lib, "webkit_settings_set_enable_back_forward_navigation_gestures"},
			{&wk.setDefaultSz, lib, "webkit_settings_set_default_font_size"},
			{&wk.loadHTML, lib, "webkit_web_view_load_html"},
			{&wk.setBG, lib, "webkit_web_view_set_background_color"},
			{&wk.setZoom, lib, "webkit_web_view_set_zoom_level"},
			{&wk.evalJS, lib, "webkit_web_view_evaluate_javascript"},
			{&wk.evalJSFinish, lib, "webkit_web_view_evaluate_javascript_finish"},
			{&wk.jsToString, jsc, "jsc_value_to_string"},
			{&wk.navAction, lib, "webkit_navigation_policy_decision_get_navigation_action"},
			{&wk.actionRequest, lib, "webkit_navigation_action_get_request"},
			{&wk.actionType, lib, "webkit_navigation_action_get_navigation_type"},
			{&wk.requestURI, lib, "webkit_uri_request_get_uri"},
			{&wk.ignore, lib, "webkit_policy_decision_ignore"},
			{&wk.use, lib, "webkit_policy_decision_use"},
		} {
			if _, err := purego.Dlsym(f.lib, f.name); err != nil {
				return
			}
			purego.RegisterLibFunc(f.fn, f.lib, f.name)
		}
		wk.ok = true
	})
	return wk.ok
}

// webMsg is a message shown by a web view: kept across the reader's
// redraws (a new view each time would load and lay it out again), sized
// to its page, and fitted to the column when the page is wider.
type webMsg struct {
	view   *gtk.Widget
	sig    string // the page loaded: options and message
	height int
	zoom   float64
	asks   int  // measures still to come after a load
	gone   bool // let go of: its view is no more
}

const (
	webLoadFinished   = 3 // WEBKIT_LOAD_FINISHED
	webNavigation     = 0 // WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION
	webNewWindow      = 1 // WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION
	webLinkClicked    = 0 // WEBKIT_NAVIGATION_TYPE_LINK_CLICKED
	webMeasureScript  = `(function(){var d=document.documentElement,b=document.body;return [Math.max(d.scrollHeight,b?b.scrollHeight:0),Math.max(d.scrollWidth,b?b.scrollWidth:0),d.clientWidth].join(",")})()`
	webMinHeight      = 60
	webMeasureLater   = 400
	webMeasureLongest = 2500
)

// webHub sends WebKit's signals to the message they are for, as the hub
// does GTK's: one handler of each kind.
type webHub struct {
	policy   func(view, decision uintptr, kind int32, _ uintptr) bool
	loaded   func(view uintptr, event int32, _ uintptr)
	scrolled func(c gtk.EventControllerScroll, dx, dy float64) bool
	measured uintptr // the C function evaluate_javascript calls back
	byView   map[uintptr]*webMsg
	byScroll map[uintptr]bool
}

// webView shows a message's HTML in its web view, made the first time
// and kept; its page loaded again only when what it shows changed.
func (w *window) webView(box *gtk.Box, k mail.Key, m *mail.Message) (quoted bool) {
	opt := mail.WebOptions{Remote: w.picturesAllowed(k, m), Quotes: w.quotes[k]}
	page := mail.PrepareWeb(m, opt)
	if page.Remote > 0 && !opt.Remote {
		w.drawHeld(box, k, m, page.Remote)
	}
	wm := w.webs[k]
	if wm == nil {
		wm = w.newWebMsg()
		w.webs[k] = wm
	}
	if sig := fmt.Sprintf("%v%v%d", opt.Remote, opt.Quotes, len(m.HTML)); sig != wm.sig {
		wm.sig, wm.zoom, wm.asks = sig, 1, 3
		wk.setZoom(wm.view.Ptr, 1)
		wk.loadHTML(wm.view.Ptr, page.HTML, "about:blank")
	}
	frame := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	frame.AddCssClass("mail-web")
	frame.SetOverflow(gtk.OverflowHiddenValue)
	unparent(wm.view)
	frame.Append(wm.view) // not gtkx.Append: the view's own reference stays the window's
	gtkx.Append(box, &frame.Widget)
	return page.Quoted
}

// newWebMsg makes a web view for a message: no scripts of the mail's, no
// storage, no gestures; white under the page.
func (w *window) newWebMsg() *webMsg {
	h := w.web
	if h == nil {
		h = &webHub{byView: map[uintptr]*webMsg{}, byScroll: map[uintptr]bool{}}
		w.web = h
		h.policy = func(view, decision uintptr, kind int32, _ uintptr) bool {
			if kind != webNavigation && kind != webNewWindow {
				return false
			}
			action := wk.navAction(decision)
			uri := wk.requestURI(wk.actionRequest(action))
			if kind == webNavigation && wk.actionType(action) != webLinkClicked && (uri == "about:blank" || strings.HasPrefix(uri, "data:")) {
				wk.use(decision) // the page itself
				return true
			}
			wk.ignore(decision)
			if strings.HasPrefix(uri, "http:") || strings.HasPrefix(uri, "https:") || strings.HasPrefix(uri, "mailto:") {
				if strings.HasPrefix(uri, "mailto:") {
					w.mailto(uri)
				} else {
					desktop.Spawn([]string{"xdg-open", uri}, "")
				}
			}
			return true
		}
		h.loaded = func(view uintptr, event int32, _ uintptr) {
			if wm := h.byView[view]; wm != nil && event == webLoadFinished {
				w.measure(wm)
			}
		}
		// The reader scrolls, not the page: it is as tall as its content.
		h.scrolled = func(c gtk.EventControllerScroll, dx, dy float64) bool {
			adj := w.readerScroll.GetVadjustment()
			step := dy
			if c.GetUnit() == gdk.ScrollUnitWheelValue {
				step = dy * 48
			}
			adj.SetValue(adj.GetValue() + step)
			return true
		}
		h.measured = purego.NewCallback(func(source, result, _ uintptr) uintptr {
			wm := h.byView[source]
			v := wk.evalJSFinish(source, result, 0)
			if wm == nil || v == 0 {
				return 0
			}
			w.measured(wm, wk.jsToString(v))
			(&gobject.Object{Ptr: v}).Unref()
			return 0
		})
	}
	ptr := wk.viewNew()
	view := &gtk.Widget{}
	view.Ptr = ptr
	(&gobject.Object{Ptr: ptr}).RefSink() // kept across the reader's redraws
	s := wk.settings(ptr)
	wk.setJSMarkup(s, false)
	wk.setPageCache(s, false)
	wk.setStorage(s, false)
	wk.setWebGL(s, false)
	wk.setGestures(s, false)
	wk.setDefaultSz(s, 14)
	wk.setBG(ptr, &gdk.RGBA{Red: 1, Green: 1, Blue: 1, Alpha: 1})
	view.SetHexpand(true)
	view.SetSizeRequest(-1, webMinHeight)
	view.AddCssClass("mail-web-view")
	gobject.SignalConnect(ptr, "decide-policy", glib.NewCallback(&h.policy))
	gobject.SignalConnect(ptr, "load-changed", glib.NewCallback(&h.loaded))
	sc := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue)
	sc.SetPropagationPhase(gtk.PhaseCaptureValue)
	sc.ConnectScroll(&h.scrolled)
	view.AddController(&sc.EventController)
	wm := &webMsg{view: view, zoom: 1}
	h.byView[ptr] = wm
	return wm
}

// measure asks the page how tall and wide it is.
func (w *window) measure(wm *webMsg) {
	if wm.gone {
		return // let go of before its turn came
	}
	script := webMeasureScript
	wk.evalJS(wm.view.Ptr, script, len(script), 0, 0, 0, w.web.measured, 0)
}

// measured sizes the view to its page, and zooms a page wider than the
// column out to fit it; pictures loading later grow the page, so it is
// measured again a few times.
func (w *window) measured(wm *webMsg, answer string) {
	f := strings.Split(answer, ",")
	if len(f) != 3 {
		return
	}
	height, _ := strconv.Atoi(f[0])
	width, _ := strconv.Atoi(f[1])
	client, _ := strconv.Atoi(f[2])
	if client > 0 && width > client+4 && wm.zoom == 1 {
		wm.zoom = max(float64(client)/float64(width), 0.5)
		wk.setZoom(wm.view.Ptr, wm.zoom)
		w.h.later(60, func() { w.measure(wm) })
		return
	}
	px := int(float64(height)*wm.zoom) + 2
	if px > wm.height {
		wm.height = px
		wm.view.SetSizeRequest(-1, int32(max(px, webMinHeight)))
	}
	if wm.asks > 0 {
		wm.asks--
		delay := uint32(webMeasureLater)
		if wm.asks == 0 {
			delay = webMeasureLongest
		}
		w.h.later(delay, func() { w.measure(wm) })
	}
}

// dropWebs lets go of the web views of messages no longer shown.
func (w *window) dropWebs(keep func(mail.Key) bool) {
	for k, wm := range w.webs {
		if keep(k) {
			continue
		}
		unparent(wm.view)
		wm.gone = true
		delete(w.web.byView, wm.view.Ptr)
		(&gobject.Object{Ptr: wm.view.Ptr}).Unref()
		delete(w.webs, k)
	}
}

// unparent takes a kept web view out of the frame it was last shown in.
func unparent(view *gtk.Widget) {
	if p := view.GetParent(); p != nil {
		(&gtk.Box{Widget: *p}).Remove(view)
		p.Unref() // GetParent returned a new reference
	}
}
