package viewer

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"codeberg.org/puregotk/purego"
)

// PDFs are drawn by poppler-glib (libpoppler-glib8t64) into cairo image
// surfaces, both loaded with purego when the first PDF opens. Without
// them the viewer says what to install.

var poppler struct {
	once sync.Once
	err  error

	open     func(uri string, password *byte, gerr **gError) uintptr
	nPages   func(doc uintptr) int32
	page     func(doc uintptr, i int32) uintptr
	size     func(page uintptr, w, h *float64)
	render   func(page uintptr, cr uintptr)
	text     func(page uintptr) *byte
	title    func(doc uintptr) *byte
	author   func(doc uintptr) *byte
	subject  func(doc uintptr) *byte
	creator  func(doc uintptr) *byte
	producer func(doc uintptr) *byte
	created  func(doc uintptr) int64
	modified func(doc uintptr) int64

	unref     func(obj uintptr)
	free      func(p unsafe.Pointer)
	errorFree func(e *gError)

	stride       func(format, width int32) int32
	surface      func(data unsafe.Pointer, format, w, h, stride int32) uintptr
	surfaceFlush func(s uintptr)
	surfaceFree  func(s uintptr)
	create       func(s uintptr) uintptr
	status       func(cr uintptr) int32
	destroy      func(cr uintptr)
	scale        func(cr uintptr, sx, sy float64)
	rgb          func(cr uintptr, r, g, b float64)
	paint        func(cr uintptr)
}

// gError is GLib's GError.
type gError struct {
	domain  uint32
	code    int32
	message *byte
}

const (
	cairoARGB32       = 0 // CAIRO_FORMAT_ARGB32
	popplerEncrypted  = 1 // POPPLER_ERROR_ENCRYPTED
	maxPagePixels     = 48 << 20
	popplerLibrary    = "libpoppler-glib.so.8"
	cairoLibrary      = "libcairo.so.2"
	gobjectLibrary    = "libgobject-2.0.so.0"
	glibLibrary       = "libglib-2.0.so.0"
	popplerPackage    = "libpoppler-glib8t64"
	errPopplerMissing = "PDF needs poppler-glib (%s): %v"
)

// ErrEncrypted is the answer for a PDF locked with a password.
var ErrEncrypted = errors.New("locked with a password")

// PDFAvailable reports whether poppler-glib could be loaded, and if not why.
func PDFAvailable() error {
	poppler.once.Do(func() {
		libs := map[string]uintptr{}
		for _, name := range []string{popplerLibrary, cairoLibrary, gobjectLibrary, glibLibrary} {
			lib, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL)
			if err != nil {
				poppler.err = fmt.Errorf(errPopplerMissing, popplerPackage, err)
				return
			}
			libs[name] = lib
		}
		defer func() {
			if r := recover(); r != nil { // a symbol is missing: an old library
				poppler.err = fmt.Errorf(errPopplerMissing, popplerPackage, r)
			}
		}()
		p, c := libs[popplerLibrary], libs[cairoLibrary]
		purego.RegisterLibFunc(&poppler.open, p, "poppler_document_new_from_file")
		purego.RegisterLibFunc(&poppler.nPages, p, "poppler_document_get_n_pages")
		purego.RegisterLibFunc(&poppler.page, p, "poppler_document_get_page")
		purego.RegisterLibFunc(&poppler.size, p, "poppler_page_get_size")
		purego.RegisterLibFunc(&poppler.render, p, "poppler_page_render")
		purego.RegisterLibFunc(&poppler.text, p, "poppler_page_get_text")
		purego.RegisterLibFunc(&poppler.title, p, "poppler_document_get_title")
		purego.RegisterLibFunc(&poppler.author, p, "poppler_document_get_author")
		purego.RegisterLibFunc(&poppler.subject, p, "poppler_document_get_subject")
		purego.RegisterLibFunc(&poppler.creator, p, "poppler_document_get_creator")
		purego.RegisterLibFunc(&poppler.producer, p, "poppler_document_get_producer")
		purego.RegisterLibFunc(&poppler.created, p, "poppler_document_get_creation_date")
		purego.RegisterLibFunc(&poppler.modified, p, "poppler_document_get_modification_date")
		purego.RegisterLibFunc(&poppler.unref, libs[gobjectLibrary], "g_object_unref")
		purego.RegisterLibFunc(&poppler.free, libs[glibLibrary], "g_free")
		purego.RegisterLibFunc(&poppler.errorFree, libs[glibLibrary], "g_error_free")
		purego.RegisterLibFunc(&poppler.stride, c, "cairo_format_stride_for_width")
		purego.RegisterLibFunc(&poppler.surface, c, "cairo_image_surface_create_for_data")
		purego.RegisterLibFunc(&poppler.surfaceFlush, c, "cairo_surface_flush")
		purego.RegisterLibFunc(&poppler.surfaceFree, c, "cairo_surface_destroy")
		purego.RegisterLibFunc(&poppler.create, c, "cairo_create")
		purego.RegisterLibFunc(&poppler.status, c, "cairo_status")
		purego.RegisterLibFunc(&poppler.destroy, c, "cairo_destroy")
		purego.RegisterLibFunc(&poppler.scale, c, "cairo_scale")
		purego.RegisterLibFunc(&poppler.rgb, c, "cairo_set_source_rgb")
		purego.RegisterLibFunc(&poppler.paint, c, "cairo_paint")
	})
	return poppler.err
}

// Doc is an open PDF. Its methods may be called from any goroutine, one
// at a time.
type Doc struct {
	mu    sync.Mutex
	doc   uintptr
	sizes [][2]float64 // each page's size in points
}

// PDFInfo is what a PDF says about itself.
type PDFInfo struct {
	Title, Author, Subject, Creator, Producer string
	Created, Modified                         time.Time
}

// OpenPDF opens a PDF and reads its pages' sizes.
func OpenPDF(path string) (*Doc, error) {
	if err := PDFAvailable(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: abs}).String()
	var gerr *gError
	doc := poppler.open(uri, nil, &gerr)
	if doc == 0 {
		if gerr == nil {
			return nil, errors.New("poppler could not read it")
		}
		defer poppler.errorFree(gerr)
		if gerr.code == popplerEncrypted {
			return nil, ErrEncrypted
		}
		return nil, errors.New(cString(gerr.message))
	}
	d := &Doc{doc: doc}
	n := int(poppler.nPages(doc))
	for i := range n {
		var w, h float64
		if p := poppler.page(doc, int32(i)); p != 0 {
			poppler.size(p, &w, &h)
			poppler.unref(p)
		}
		d.sizes = append(d.sizes, [2]float64{w, h})
	}
	return d, nil
}

// Close lets go of the document.
func (d *Doc) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc != 0 {
		poppler.unref(d.doc)
		d.doc = 0
	}
}

// Pages is how many pages the document has.
func (d *Doc) Pages() int { return len(d.sizes) }

// PageSize is a page's size in points (1/72 inch).
func (d *Doc) PageSize(i int) (w, h float64) {
	if i < 0 || i >= len(d.sizes) {
		return 0, 0
	}
	return d.sizes[i][0], d.sizes[i][1]
}

// Info is the document's title, author and dates, as far as it says.
func (d *Doc) Info() PDFInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == 0 {
		return PDFInfo{}
	}
	date := func(t int64) time.Time {
		if t <= 0 {
			return time.Time{}
		}
		return time.Unix(t, 0)
	}
	return PDFInfo{
		Title:    gString(poppler.title(d.doc)),
		Author:   gString(poppler.author(d.doc)),
		Subject:  gString(poppler.subject(d.doc)),
		Creator:  gString(poppler.creator(d.doc)),
		Producer: gString(poppler.producer(d.doc)),
		Created:  date(poppler.created(d.doc)),
		Modified: date(poppler.modified(d.doc)),
	}
}

// PageText is the text on a page, as poppler reads it.
func (d *Doc) PageText(i int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == 0 || i < 0 || i >= len(d.sizes) {
		return ""
	}
	p := poppler.page(d.doc, int32(i))
	if p == 0 {
		return ""
	}
	defer poppler.unref(p)
	return gString(poppler.text(p))
}

// Render draws a page on white at scale pixels per point, made smaller if
// it would be very large, and says the scale it drew at. The pixels are
// cairo's: premultiplied ARGB in native order, B, G, R, A in memory on the
// little-endian machines inua runs on.
func (d *Doc) Render(i int, scale float64, al Allocator) (px Pixels, drawn float64, err error) {
	pw, ph := d.PageSize(i)
	if pw <= 0 || ph <= 0 || scale <= 0 {
		return Pixels{}, 0, fmt.Errorf("page %d has no size", i+1)
	}
	if n := pw * scale * ph * scale; n > maxPagePixels {
		scale *= math.Sqrt(maxPagePixels / n)
	}
	w, h := max(1, int(pw*scale+0.5)), max(1, int(ph*scale+0.5))
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == 0 {
		return Pixels{}, 0, errors.New("closed")
	}
	page := poppler.page(d.doc, int32(i))
	if page == 0 {
		return Pixels{}, 0, fmt.Errorf("no page %d", i+1)
	}
	defer poppler.unref(page)
	stride := int(poppler.stride(cairoARGB32, int32(w)))
	px = Pixels{W: w, H: h, Stride: stride, Format: BGRAPremult, Buf: al.Alloc(stride * h)}
	surface := poppler.surface(unsafe.Pointer(&px.Buf[0]), cairoARGB32, int32(w), int32(h), int32(stride))
	cr := poppler.create(surface)
	if st := poppler.status(cr); st != 0 {
		poppler.destroy(cr)
		poppler.surfaceFree(surface)
		al.Free(px.Buf)
		return Pixels{}, 0, fmt.Errorf("cairo: status %d", st)
	}
	poppler.rgb(cr, 1, 1, 1)
	poppler.paint(cr)
	poppler.scale(cr, float64(w)/pw, float64(h)/ph)
	poppler.render(page, cr)
	poppler.destroy(cr)
	poppler.surfaceFlush(surface)
	poppler.surfaceFree(surface)
	runtime.KeepAlive(px.Buf)
	return px, float64(w) / pw, nil
}

// PaperName names a page size people know, "" for others.
func PaperName(w, h float64) string {
	if w > h {
		w, h = h, w
	}
	mm := func(pt float64) float64 { return pt * 25.4 / 72 }
	papers := []struct {
		name string
		w, h float64
	}{
		{"A3", 297, 420}, {"A4", 210, 297}, {"A5", 148, 210}, {"B5", 176, 250},
		{"Letter", 215.9, 279.4}, {"Legal", 215.9, 355.6},
	}
	for _, p := range papers {
		if math.Abs(mm(w)-p.w) < 1.5 && math.Abs(mm(h)-p.h) < 1.5 {
			return p.name
		}
	}
	return ""
}

// PageSizeText is a page's size as the details show it: "A4 · 210 × 297
// mm".
func PageSizeText(w, h float64) string {
	s := fmt.Sprintf("%.0f × %.0f mm", w*25.4/72, h*25.4/72)
	if name := PaperName(w, h); name != "" {
		s = name + "  ·  " + s
	}
	return s
}

// gString takes a string GLib allocated, and frees it.
func gString(p *byte) string {
	if p == nil {
		return ""
	}
	s := cString(p)
	poppler.free(unsafe.Pointer(p))
	return s
}

// cString copies a C string.
func cString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}
