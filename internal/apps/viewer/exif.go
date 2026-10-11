package viewer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

// Exif is what the details panel shows of a photo's EXIF.
type Exif struct {
	Make, Model, Lens string
	Exposure          float64 // seconds
	FNumber           float64
	ISO               int
	Focal             float64 // mm
	Focal35           int     // mm, the 35 mm equivalent
	Taken             time.Time
	Orientation       int    // 1–8, 0 for unknown
	Thumb             []byte // the embedded thumbnail (a small JPEG), if any
}

// Camera is the maker and model, without the maker twice ("Canon EOS R6",
// not "Canon Canon EOS R6").
func (e Exif) Camera() string {
	mk, md := strings.TrimSpace(e.Make), strings.TrimSpace(e.Model)
	if md == "" {
		return mk
	}
	if mk == "" || strings.HasPrefix(strings.ToLower(md), strings.ToLower(strings.Fields(mk)[0])) {
		return md
	}
	return mk + " " + md
}

// Settings are the exposure in one line: "f/2.8 · 1/250 s · ISO 200 · 35 mm".
func (e Exif) Settings() string {
	var parts []string
	if e.FNumber > 0 {
		parts = append(parts, "f/"+trimFloat(e.FNumber, 1))
	}
	if e.Exposure > 0 {
		if e.Exposure < 1 {
			parts = append(parts, fmt.Sprintf("1/%d s", int(math.Round(1/e.Exposure))))
		} else {
			parts = append(parts, trimFloat(e.Exposure, 1)+" s")
		}
	}
	if e.ISO > 0 {
		parts = append(parts, fmt.Sprintf("ISO %d", e.ISO))
	}
	if e.Focal > 0 {
		f := trimFloat(e.Focal, 1) + " mm"
		if e.Focal35 > 0 && float64(e.Focal35) != math.Round(e.Focal) {
			f += fmt.Sprintf(" (%d mm)", e.Focal35)
		}
		parts = append(parts, f)
	}
	return strings.Join(parts, " · ")
}

// Empty reports whether there is nothing to show.
func (e Exif) Empty() bool {
	return e.Camera() == "" && e.Lens == "" && e.Settings() == "" && e.Taken.IsZero()
}

func trimFloat(f float64, prec int) string {
	s := fmt.Sprintf("%.*f", prec, f)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// ReadExif reads the EXIF of a JPEG, PNG, WebP or TIFF file. A file made
// to trip the parser is an error, never a crash.
func ReadExif(path string) (e Exif, err error) {
	defer recoverMalformed("EXIF", &err)
	f, err := os.Open(path)
	if err != nil {
		return Exif{}, err
	}
	defer f.Close()
	head := make([]byte, 64<<10) // EXIF lives near the start
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	tiff, err := findTIFF(head)
	if err != nil && errors.Is(err, errMore) {
		// PNG and WebP may put it after the pixels; read it all.
		f.Seek(0, io.SeekStart)
		all, rerr := io.ReadAll(io.LimitReader(f, 256<<20))
		if rerr != nil {
			return Exif{}, rerr
		}
		tiff, err = findTIFF(all)
	}
	if err != nil {
		return Exif{}, err
	}
	return ParseTIFF(tiff)
}

var (
	errNoExif = errors.New("no EXIF")
	errMore   = errors.New("EXIF may be further in")
)

// findTIFF finds the TIFF structure that holds the EXIF in a file's bytes.
func findTIFF(b []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8}): // JPEG: the APP1 "Exif" segment
		for i := 2; i+4 <= len(b); {
			if b[i] != 0xFF {
				return nil, errNoExif
			}
			marker := b[i+1]
			if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
				i += 2
				continue
			}
			if marker == 0xDA || marker == 0xD9 { // the pixels start: no EXIF before them
				return nil, errNoExif
			}
			l := int(binary.BigEndian.Uint16(b[i+2:])) // its own two bytes and the segment's
			if l < 2 || i+2+l > len(b) {
				return nil, errNoExif
			}
			seg := b[i+4 : i+2+l]
			if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
				return seg[6:], nil
			}
			i += 2 + l
		}
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")): // PNG: the eXIf chunk
		for i := 8; i+8 <= len(b); {
			l := int(binary.BigEndian.Uint32(b[i:]))
			typ := string(b[i+4 : i+8])
			if i+12+l > len(b) {
				return nil, errMore
			}
			if typ == "eXIf" {
				return b[i+8 : i+8+l], nil
			}
			if typ == "IEND" {
				return nil, errNoExif
			}
			i += 12 + l
		}
		return nil, errMore
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP": // WebP: the EXIF chunk
		for i := 12; i+8 <= len(b); {
			l := int(binary.LittleEndian.Uint32(b[i+4:]))
			if i+8+l > len(b) {
				return nil, errMore
			}
			if string(b[i:i+4]) == "EXIF" {
				c := b[i+8 : i+8+l]
				return bytes.TrimPrefix(c, []byte("Exif\x00\x00")), nil
			}
			i += 8 + l + l%2
		}
		return nil, errNoExif
	case bytes.HasPrefix(b, []byte("II*\x00")), bytes.HasPrefix(b, []byte("MM\x00*")): // TIFF itself
		return b, nil
	}
	return nil, errNoExif
}

// EXIF tags the panel shows.
const (
	tagMake        = 0x010F
	tagModel       = 0x0110
	tagOrientation = 0x0112
	tagDateTime    = 0x0132
	tagExifIFD     = 0x8769
	tagThumbAt     = 0x0201
	tagThumbLen    = 0x0202
	tagExposure    = 0x829A
	tagFNumber     = 0x829D
	tagISO         = 0x8827
	tagTaken       = 0x9003
	tagFocal       = 0x920A
	tagFocal35     = 0xA405
	tagLens        = 0xA434
)

// ParseTIFF reads the tags the panel shows from a TIFF structure.
func ParseTIFF(t []byte) (Exif, error) {
	if len(t) < 8 { // the byte order, 42, and where IFD0 is
		return Exif{}, errNoExif
	}
	var bo binary.ByteOrder
	switch {
	case bytes.HasPrefix(t, []byte("II*\x00")):
		bo = binary.LittleEndian
	case bytes.HasPrefix(t, []byte("MM\x00*")):
		bo = binary.BigEndian
	default:
		return Exif{}, errNoExif
	}
	var e Exif
	var dateTime string
	read := func(off uint32, f func(tag uint16, v value)) {
		if int(off)+2 > len(t) {
			return
		}
		n := int(bo.Uint16(t[off:]))
		for k := range n {
			p := int(off) + 2 + k*12
			if p+12 > len(t) {
				return
			}
			f(bo.Uint16(t[p:]), value{t: t, bo: bo, typ: bo.Uint16(t[p+2:]), count: bo.Uint32(t[p+4:]), at: p + 8})
		}
	}
	var exifAt uint32
	ifd0 := bo.Uint32(t[4:])
	read(ifd0, func(tag uint16, v value) {
		switch tag {
		case tagMake:
			e.Make = v.str()
		case tagModel:
			e.Model = v.str()
		case tagOrientation:
			e.Orientation = int(v.uint())
		case tagDateTime:
			dateTime = v.str()
		case tagExifIFD:
			exifAt = uint32(v.uint())
		}
	})
	// IFD1, after IFD0, holds the thumbnail.
	if int(ifd0)+2 <= len(t) {
		if next := int(ifd0) + 2 + int(bo.Uint16(t[ifd0:]))*12; next+4 <= len(t) {
			if ifd1 := bo.Uint32(t[next:]); ifd1 != 0 {
				var at, n uint64
				read(ifd1, func(tag uint16, v value) {
					switch tag {
					case tagThumbAt:
						at = v.uint()
					case tagThumbLen:
						n = v.uint()
					}
				})
				if n > 0 && at+n <= uint64(len(t)) {
					e.Thumb = t[at : at+n]
				}
			}
		}
	}
	var taken string
	if exifAt != 0 {
		read(exifAt, func(tag uint16, v value) {
			switch tag {
			case tagExposure:
				e.Exposure = v.rational()
			case tagFNumber:
				e.FNumber = v.rational()
			case tagISO:
				e.ISO = int(v.uint())
			case tagTaken:
				taken = v.str()
			case tagFocal:
				e.Focal = v.rational()
			case tagFocal35:
				e.Focal35 = int(v.uint())
			case tagLens:
				e.Lens = v.str()
			}
		})
	}
	for _, s := range []string{taken, dateTime} {
		if tm, err := time.ParseInLocation("2006:01:02 15:04:05", strings.TrimSpace(s), time.Local); err == nil {
			e.Taken = tm
			break
		}
	}
	return e, nil
}

// value is one IFD entry's value.
type value struct {
	t     []byte
	bo    binary.ByteOrder
	typ   uint16
	count uint32
	at    int // the entry's value field
}

var typeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}

// data is the value's bytes: in the entry when they fit, else at its offset.
func (v value) data() []byte {
	size := typeSize[v.typ] * int(v.count)
	if size == 0 || v.count > 1<<20 {
		return nil
	}
	if size <= 4 {
		return v.t[v.at : v.at+size]
	}
	off := int(v.bo.Uint32(v.t[v.at:]))
	if off < 0 || off+size > len(v.t) {
		return nil
	}
	return v.t[off : off+size]
}

func (v value) str() string {
	d := v.data()
	if i := bytes.IndexByte(d, 0); i >= 0 {
		d = d[:i]
	}
	return strings.TrimSpace(string(d))
}

func (v value) uint() uint64 {
	d := v.data()
	switch {
	case v.typ == 3 && len(d) >= 2:
		return uint64(v.bo.Uint16(d))
	case (v.typ == 4 || v.typ == 9) && len(d) >= 4:
		return uint64(v.bo.Uint32(d))
	case v.typ == 1 && len(d) >= 1:
		return uint64(d[0])
	}
	return 0
}

// rational reads a fraction. Some writers store one as two integers, or
// as a whole number; those are read too.
func (v value) rational() float64 {
	d := v.data()
	switch {
	case (v.typ == 3 || v.typ == 4) && v.count == 2:
		size := typeSize[v.typ]
		num, den := v.intAt(d, 0), v.intAt(d, size)
		if den == 0 {
			return 0
		}
		return float64(num) / float64(den)
	case (v.typ == 3 || v.typ == 4) && v.count == 1:
		return float64(v.uint())
	}
	if (v.typ != 5 && v.typ != 10) || len(d) < 8 {
		return 0
	}
	num, den := v.bo.Uint32(d), v.bo.Uint32(d[4:])
	if den == 0 {
		return 0
	}
	if v.typ == 10 {
		return float64(int32(num)) / float64(int32(den))
	}
	return float64(num) / float64(den)
}

func (v value) intAt(d []byte, at int) uint64 {
	switch {
	case v.typ == 3 && len(d) >= at+2:
		return uint64(v.bo.Uint16(d[at:]))
	case v.typ == 4 && len(d) >= at+4:
		return uint64(v.bo.Uint32(d[at:]))
	}
	return 0
}
