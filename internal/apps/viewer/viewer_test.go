package viewer

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNaturalLess(t *testing.T) {
	names := []string{"img10.png", "IMG2.png", "img1.png", "a.png", "img02b.png", "B.png", "img2.jpg"}
	slices.SortFunc(names, func(a, b string) int {
		if NaturalLess(a, b) {
			return -1
		}
		if NaturalLess(b, a) {
			return 1
		}
		return 0
	})
	want := []string{"a.png", "B.png", "img1.png", "img2.jpg", "IMG2.png", "img02b.png", "img10.png"}
	if !slices.Equal(names, want) {
		t.Fatalf("got %v, want %v", names, want)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b10.jpg", "b2.png", "notes.txt", ".hidden.png", "a.webp"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o644)
	}
	os.Mkdir(filepath.Join(dir, "sub.png"), 0o755) // a folder, not an image
	base := func(fs []string) (out []string) {
		for _, f := range fs {
			out = append(out, filepath.Base(f))
		}
		return out
	}
	files, i := List([]string{filepath.Join(dir, "b10.jpg")})
	if got := base(files); !slices.Equal(got, []string{"a.webp", "b2.png", "b10.jpg"}) || i != 2 {
		t.Errorf("one file: %v at %d", got, i)
	}
	files, i = List([]string{dir})
	if got := base(files); len(got) != 3 || i != 0 {
		t.Errorf("a folder: %v at %d", got, i)
	}
	files, _ = List([]string{filepath.Join(dir, "b2.png"), filepath.Join(dir, "notes.txt"), filepath.Join(dir, "a.webp")})
	if got := base(files); !slices.Equal(got, []string{"b2.png", "a.webp"}) {
		t.Errorf("several: %v", got)
	}
	files, i = List([]string{filepath.Join(dir, ".hidden.png")})
	if got := base(files); !slices.Equal(got, []string{".hidden.png"}) || i != 0 {
		t.Errorf("hidden: %v at %d", got, i)
	}
}

func TestZoom(t *testing.T) {
	if z := StepZoom(1, true); z != 1.5 {
		t.Errorf("up from 1: %v", z)
	}
	if z := StepZoom(1, false); z != 2.0/3 {
		t.Errorf("down from 1: %v", z)
	}
	if z := StepZoom(0.7, true); z != 1 {
		t.Errorf("up from 0.7: %v", z)
	}
	if z := StepZoom(32, true); z != MaxZoom {
		t.Errorf("up from max: %v", z)
	}
	if z := FitZoom(4000, 3000, 1000, 1000); z != 0.25 {
		t.Errorf("fit: %v", z)
	}
	if z := FitZoom(100, 100, 1000, 1000); z != 1 {
		t.Errorf("fit small: %v", z)
	}
	for z, want := range map[float64]string{1: "100%", 0.125: "12.5%", 2.0 / 3: "66.7%", 4: "400%", 0.05: "5%"} {
		if got := Percent(z); got != want {
			t.Errorf("Percent(%v) = %q, want %q", z, got, want)
		}
	}
}

// tiff builds a little-endian TIFF with an IFD0 and an EXIF IFD.
func tiff() []byte {
	type entry struct {
		tag, typ uint16
		count    uint32
		data     []byte
	}
	le := binary.LittleEndian
	ascii := func(s string) []byte { return append([]byte(s), 0) }
	rational := func(n, d uint32) []byte { return le.AppendUint32(le.AppendUint32(nil, n), d) }
	short := func(v uint16) []byte { return le.AppendUint16(nil, v) }
	ifd0 := []entry{
		{tagMake, 2, 6, ascii("Canon")},
		{tagModel, 2, 13, ascii("Canon EOS R6")},
		{tagOrientation, 3, 1, short(6)},
		{tagExifIFD, 4, 1, nil}, // filled below
	}
	exif := []entry{
		{tagExposure, 5, 1, rational(1, 250)},
		{tagFNumber, 5, 1, rational(28, 10)},
		{tagISO, 3, 1, short(200)},
		{tagTaken, 2, 20, ascii("2026:05:01 18:30:00")},
		{tagFocal, 5, 1, rational(35, 1)},
		{tagLens, 2, 15, ascii("RF24-70mm F2.8")},
	}
	// Layout: header, IFD0, its data, EXIF IFD, its data.
	var out []byte
	out = append(out, 'I', 'I', 42, 0)
	out = le.AppendUint32(out, 8)
	write := func(entries []entry, at int, next func(int) []byte) []byte {
		buf := make([]byte, 0)
		dataAt := at + 2 + len(entries)*12 + 4
		var data []byte
		buf = le.AppendUint16(buf, uint16(len(entries)))
		for _, e := range entries {
			buf = le.AppendUint16(buf, e.tag)
			buf = le.AppendUint16(buf, e.typ)
			buf = le.AppendUint32(buf, e.count)
			d := e.data
			if d == nil {
				d = next(dataAt + len(data))
			}
			if len(d) <= 4 {
				buf = append(buf, append(d, make([]byte, 4-len(d))...)...)
			} else {
				buf = le.AppendUint32(buf, uint32(dataAt+len(data)))
				data = append(data, d...)
				if len(data)%2 == 1 {
					data = append(data, 0)
				}
			}
		}
		buf = le.AppendUint32(buf, 0)
		return append(buf, data...)
	}
	// IFD0's data holds Make and Model; the EXIF IFD follows it.
	var ifd0Bytes []byte
	ifd0Bytes = write(ifd0, 8, func(int) []byte { return nil })
	exifAt := 8 + len(ifd0Bytes)
	ifd0[3].data = le.AppendUint32(nil, uint32(exifAt))
	ifd0Bytes = write(ifd0, 8, nil)
	out = append(out, ifd0Bytes...)
	out = append(out, write(exif, exifAt, nil)...)
	return out
}

func TestParseTIFF(t *testing.T) {
	e, err := ParseTIFF(tiff())
	if err != nil {
		t.Fatal(err)
	}
	if e.Camera() != "Canon EOS R6" {
		t.Errorf("camera %q", e.Camera())
	}
	if e.Lens != "RF24-70mm F2.8" || e.Orientation != 6 {
		t.Errorf("lens %q orientation %d", e.Lens, e.Orientation)
	}
	if s := e.Settings(); s != "f/2.8 · 1/250 s · ISO 200 · 35 mm" {
		t.Errorf("settings %q", s)
	}
	if want := time.Date(2026, 5, 1, 18, 30, 0, 0, time.Local); !e.Taken.Equal(want) {
		t.Errorf("taken %v", e.Taken)
	}
}

func TestReadExif(t *testing.T) {
	tf := tiff()
	be := binary.BigEndian
	le := binary.LittleEndian
	app1 := append([]byte("Exif\x00\x00"), tf...)
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 4, 0, 0, 0xFF, 0xE1}, be.AppendUint16(nil, uint16(len(app1)+2))...)
	jpeg = append(append(jpeg, app1...), 0xFF, 0xDA, 0, 2, 0xFF, 0xD9)

	chunk := func(typ string, data []byte) []byte {
		c := be.AppendUint32(nil, uint32(len(data)))
		c = append(append(c, typ...), data...)
		return be.AppendUint32(c, 0) // CRC, unchecked
	}
	png := []byte("\x89PNG\r\n\x1a\n")
	png = append(png, chunk("IHDR", make([]byte, 13))...)
	png = append(png, chunk("IDAT", bytes.Repeat([]byte{1}, 100))...)
	png = append(png, chunk("eXIf", tf)...)
	png = append(png, chunk("IEND", nil)...)

	ex := append([]byte("EXIF"), le.AppendUint32(nil, uint32(len(tf)))...)
	ex = append(ex, tf...)
	if len(tf)%2 == 1 {
		ex = append(ex, 0)
	}
	vp8 := append([]byte("VP8 "), le.AppendUint32(nil, 4)...)
	vp8 = append(vp8, 0, 0, 0, 0)
	body := append(append([]byte("WEBP"), vp8...), ex...)
	webp := append(append([]byte("RIFF"), le.AppendUint32(nil, uint32(len(body)))...), body...)

	dir := t.TempDir()
	for name, b := range map[string][]byte{"a.jpg": jpeg, "a.png": png, "a.webp": webp, "a.tif": tf} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, b, 0o644)
		e, err := ReadExif(p)
		if err != nil || e.Camera() != "Canon EOS R6" || e.ISO != 200 {
			t.Errorf("%s: %+v %v", name, e, err)
		}
	}
	plain := filepath.Join(dir, "plain.jpg")
	os.WriteFile(plain, []byte{0xFF, 0xD8, 0xFF, 0xDA, 0, 2, 0xFF, 0xD9}, 0o644)
	if _, err := ReadExif(plain); err == nil {
		t.Error("plain JPEG: want an error")
	}
}

func TestCameraAndSettings(t *testing.T) {
	for _, c := range []struct {
		e    Exif
		want string
	}{
		{Exif{Make: "Apple", Model: "iPhone 15 Pro"}, "Apple iPhone 15 Pro"},
		{Exif{Make: "NIKON CORPORATION", Model: "NIKON Z 6"}, "NIKON Z 6"},
		{Exif{Make: "Fujifilm"}, "Fujifilm"},
	} {
		if got := c.e.Camera(); got != c.want {
			t.Errorf("Camera(%+v) = %q, want %q", c.e, got, c.want)
		}
	}
	e := Exif{Exposure: 2.5, FNumber: 8, Focal: 4.2, Focal35: 26}
	if s := e.Settings(); s != "f/8 · 2.5 s · 4.2 mm (26 mm)" {
		t.Errorf("settings %q", s)
	}
}

// The desktop entry offers the viewer for the types it opens.
func TestDesktopMimeTypes(t *testing.T) {
	b, err := os.ReadFile("../../../config/apps/io.github.lyricat.Inua.Viewer.desktop")
	if err != nil {
		t.Skip(err)
	}
	var listed []string
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "MimeType="); ok {
			listed = strings.FieldsFunc(v, func(r rune) bool { return r == ';' })
		}
	}
	var want []string
	for _, m := range Exts {
		if !slices.Contains(want, m) {
			want = append(want, m)
		}
	}
	slices.Sort(want)
	slices.Sort(listed)
	if !slices.Equal(listed, want) {
		t.Errorf("MimeType=%v, want %v", listed, want)
	}
}

func TestRationalAsIntegers(t *testing.T) {
	le := binary.LittleEndian
	b := le.AppendUint32(nil, 4) // the offset of the two longs
	b = le.AppendUint32(le.AppendUint32(b, 35), 1)
	if r := (value{t: b, bo: le, typ: 4, count: 2}).rational(); r != 35 {
		t.Errorf("two longs: %v", r)
	}
	s := le.AppendUint16(nil, 28)
	s = le.AppendUint16(s, 10)
	if r := (value{t: s, bo: le, typ: 3, count: 2}).rational(); r != 2.8 {
		t.Errorf("two shorts: %v", r)
	}
}

// marked is a 3×2 image whose pixels say where they came from: R is x, G
// is y.
func marked() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := range 2 {
		for x := range 3 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 100), uint8(y * 100), 7, 255})
		}
	}
	return img
}

// pixel reads pixels back as R, G, B.
func pixel(px Pixels, x, y int) [3]uint8 {
	bpp := map[Format]int{RGB: 3, RGBA: 4, RGBAPremult: 4, Gray: 1}[px.Format]
	o := y*px.Stride + x*bpp
	if px.Format == Gray {
		return [3]uint8{px.Buf[o], px.Buf[o], px.Buf[o]}
	}
	return [3]uint8{px.Buf[o], px.Buf[o+1], px.Buf[o+2]}
}

func TestConvert(t *testing.T) {
	al := GoAllocator{}
	if px := Convert(marked(), al); px.Format != RGBA || px.W != 3 || pixel(px, 2, 1) != [3]uint8{200, 100, 7} {
		t.Errorf("NRGBA: %+v %v", px.Format, pixel(px, 2, 1))
	}
	// A sub-image: its bounds do not start at the origin.
	sub := marked().SubImage(image.Rect(1, 1, 3, 2)).(*image.NRGBA)
	if px := Convert(sub, al); px.W != 2 || px.H != 1 || pixel(px, 0, 0) != [3]uint8{100, 100, 7} {
		t.Errorf("sub-image: %dx%d %v", px.W, px.H, pixel(px, 0, 0))
	}
	rgba := image.NewRGBA(image.Rect(0, 0, 2, 2))
	rgba.Set(1, 1, color.RGBA{10, 20, 30, 255})
	if px := Convert(rgba, al); px.Format != RGBAPremult || pixel(px, 1, 1) != [3]uint8{10, 20, 30} {
		t.Errorf("RGBA: %v", pixel(px, 1, 1))
	}
	ycc := image.NewYCbCr(image.Rect(0, 0, 4, 4), image.YCbCrSubsampleRatio420)
	for i := range ycc.Y {
		ycc.Y[i] = 120
	}
	for i := range ycc.Cb {
		ycc.Cb[i], ycc.Cr[i] = 90, 160
	}
	r, g, b := color.YCbCrToRGB(120, 90, 160)
	if px := Convert(ycc, al); px.Format != RGB || pixel(px, 3, 3) != [3]uint8{r, g, b} {
		t.Errorf("YCbCr: %v, want %v", pixel(px, 3, 3), [3]uint8{r, g, b})
	}
	pal := image.NewPaletted(image.Rect(0, 0, 2, 1), color.Palette{color.Black, color.RGBA{255, 0, 0, 255}})
	pal.Pix[1] = 1
	if px := Convert(pal, al); pixel(px, 1, 0) != [3]uint8{255, 0, 0} {
		t.Errorf("paletted: %v", pixel(px, 1, 0))
	}
	gray := image.NewGray(image.Rect(0, 0, 1, 1))
	gray.Pix[0] = 77
	if px := Convert(gray, al); px.Format != Gray || pixel(px, 0, 0) != [3]uint8{77, 77, 77} {
		t.Errorf("gray: %v", pixel(px, 0, 0))
	}
	big := image.NewYCbCr(image.Rect(0, 0, 300, 700), image.YCbCrSubsampleRatio444) // more than one band
	big.Y[big.YOffset(299, 699)] = 255
	big.Cb[big.COffset(299, 699)], big.Cr[big.COffset(299, 699)] = 128, 128
	if px := Convert(big, al); pixel(px, 299, 699) != [3]uint8{255, 255, 255} {
		t.Errorf("bands: %v", pixel(px, 299, 699))
	}
}

func TestDecode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	f, _ := os.Create(p)
	png.Encode(f, marked())
	f.Close()
	px, err := Decode(p, GoAllocator{})
	if err != nil || px.W != 3 || pixel(px, 2, 1) != [3]uint8{200, 100, 7} {
		t.Fatalf("png: %+v %v", px, err)
	}
	if w, h, err := ImageSize(p); w != 3 || h != 2 || err != nil {
		t.Errorf("size: %d×%d %v", w, h, err)
	}
	g := filepath.Join(dir, "a.gif")
	f, _ = os.Create(g)
	pal := color.Palette{color.Black, color.White}
	frame := func() *image.Paletted { return image.NewPaletted(image.Rect(0, 0, 2, 2), pal) }
	gif.EncodeAll(f, &gif.GIF{Image: []*image.Paletted{frame(), frame()}, Delay: []int{10, 10}})
	f.Close()
	if _, err := Decode(g, GoAllocator{}); err != ErrAnimated {
		t.Errorf("animated gif: %v", err)
	}
}

func TestOrientation(t *testing.T) {
	for o, want := range map[int][2]any{1: {0, false}, 2: {0, true}, 3: {2, false}, 4: {2, true}, 5: {3, true}, 6: {1, false}, 7: {1, true}, 8: {3, false}, 0: {0, false}} {
		turns, mirror := Orientation(o)
		if turns != want[0] || mirror != want[1] {
			t.Errorf("Orientation(%d) = %d, %v", o, turns, mirror)
		}
	}
	for _, c := range []struct {
		turns  int
		mirror bool
		k      float64
		dx, dy float64
	}{
		{0, false, 1, 0, 0},
		{1, false, 1, 2, 0}, // 3×2 turned: x from -2 to 0
		{2, false, 1, 3, 2},
		{3, false, 1, 0, 3},
		{0, true, 1, 3, 0},
		{1, false, 2, 4, 0},
	} {
		dx, dy := Placement(3, 2, c.k, c.turns, c.mirror)
		if dx != c.dx || dy != c.dy {
			t.Errorf("Placement(3, 2, %v, %d, %v) = %v, %v; want %v, %v", c.k, c.turns, c.mirror, dx, dy, c.dx, c.dy)
		}
	}
}

func TestExifThumb(t *testing.T) {
	le := binary.LittleEndian
	thumb := []byte{0xFF, 0xD8, 1, 2, 3, 0xFF, 0xD9}
	b := []byte{'I', 'I', 42, 0}
	b = le.AppendUint32(b, 8)  // IFD0 at 8
	b = le.AppendUint16(b, 0)  // no entries
	b = le.AppendUint32(b, 14) // IFD1 at 14
	b = le.AppendUint16(b, 2)  // two entries
	thumbAt := uint32(14 + 2 + 2*12 + 4)
	for _, e := range [][3]uint32{{tagThumbAt, 4, thumbAt}, {tagThumbLen, 4, uint32(len(thumb))}} {
		b = le.AppendUint16(b, uint16(e[0]))
		b = le.AppendUint16(b, uint16(e[1]))
		b = le.AppendUint32(b, 1)
		b = le.AppendUint32(b, e[2])
	}
	b = le.AppendUint32(b, 0)
	b = append(b, thumb...)
	e, err := ParseTIFF(b)
	if err != nil || !bytes.Equal(e.Thumb, thumb) {
		t.Fatalf("thumb %v %v", e.Thumb, err)
	}
}

// TestTurbo runs where libturbojpeg is installed (or on LD_LIBRARY_PATH).
func TestTurbo(t *testing.T) {
	if !TurboAvailable() {
		t.Skip("libturbojpeg is not installed")
	}
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95})
	px, w, h, err := DecodeJPEGTurbo(buf.Bytes(), 1, GoAllocator{})
	if err != nil || w != 400 || h != 300 || px.W != 400 || px.Format != RGB {
		t.Fatalf("full: %dx%d %+v %v", w, h, px.W, err)
	}
	if c := pixel(px, 200, 150); c[0] < 195 || c[0] > 205 {
		t.Errorf("colour %v", c)
	}
	px, _, _, err = DecodeJPEGTurbo(buf.Bytes(), 0.3, GoAllocator{})
	if err != nil || px.W != 150 || px.H != 113 { // 3/8, rounded up
		t.Errorf("scaled: %dx%d %v", px.W, px.H, err)
	}
	if n, d := TurboScale(0.1); n != 1 || d != 8 {
		t.Errorf("TurboScale(0.1) = %d/%d", n, d)
	}
}
