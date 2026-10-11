package viewer

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"codeberg.org/puregotk/purego"
)

// libjpeg-turbo's TurboJPEG library (libturbojpeg0), when it is there:
// it decodes JPEG several times faster than Go, and at 1/2, 1/4 or 1/8 of
// the size when the picture will be shown smaller. Without it, Go decodes.

type tjScale struct{ Num, Denom int32 }

var turbo struct {
	once    sync.Once
	ok      bool
	init    func() uintptr
	header  func(h uintptr, buf unsafe.Pointer, size uint64, w, ht, subsamp, colorspace *int32) int32
	decode  func(h uintptr, buf unsafe.Pointer, size uint64, dst unsafe.Pointer, w, pitch, ht, format, flags int32) int32
	destroy func(h uintptr) int32
	errStr  func(h uintptr) string
	scales  []tjScale
}

const tjRGB = 0 // TJPF_RGB

// TurboAvailable reports whether libturbojpeg could be loaded.
func TurboAvailable() bool {
	turbo.once.Do(func() {
		lib, err := purego.Dlopen("libturbojpeg.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			return
		}
		defer func() {
			if recover() != nil { // a symbol is missing: an old library
				turbo.ok = false
			}
		}()
		purego.RegisterLibFunc(&turbo.init, lib, "tjInitDecompress")
		purego.RegisterLibFunc(&turbo.header, lib, "tjDecompressHeader3")
		purego.RegisterLibFunc(&turbo.decode, lib, "tjDecompress2")
		purego.RegisterLibFunc(&turbo.destroy, lib, "tjDestroy")
		purego.RegisterLibFunc(&turbo.errStr, lib, "tjGetErrorStr2")
		var getScales func(n *int32) *tjScale
		purego.RegisterLibFunc(&getScales, lib, "tjGetScalingFactors")
		var n int32
		if p := getScales(&n); p != nil && n > 0 {
			turbo.scales = append([]tjScale(nil), unsafe.Slice(p, n)...)
		}
		turbo.ok = true
	})
	return turbo.ok
}

// TurboScale is the smallest scale libturbojpeg decodes at that is at
// least min (1 for full size).
func TurboScale(min float64) (num, denom int) {
	num, denom = 1, 1
	for _, s := range turbo.scales {
		f := float64(s.Num) / float64(s.Denom)
		if f >= min && f <= 1 && f < float64(num)/float64(denom) {
			num, denom = int(s.Num), int(s.Denom)
		}
	}
	return num, denom
}

// DecodeJPEGTurbo decodes a JPEG with libturbojpeg into RGB, at the
// smallest scale it can that is at least scale, and says the full size.
func DecodeJPEGTurbo(data []byte, scale float64, al Allocator) (px Pixels, fullW, fullH int, err error) {
	if !TurboAvailable() {
		return Pixels{}, 0, 0, errors.New("libturbojpeg is not installed")
	}
	if len(data) == 0 {
		return Pixels{}, 0, 0, errors.New("empty file")
	}
	h := turbo.init()
	if h == 0 {
		return Pixels{}, 0, 0, errors.New("libturbojpeg: no handle")
	}
	defer turbo.destroy(h)
	var w, ht, subsamp, cs int32
	src := unsafe.Pointer(&data[0])
	if turbo.header(h, src, uint64(len(data)), &w, &ht, &subsamp, &cs) != 0 {
		return Pixels{}, 0, 0, fmt.Errorf("libturbojpeg: %s", turbo.errStr(h))
	}
	num, denom := TurboScale(scale)
	sw := (int(w)*num + denom - 1) / denom
	sh := (int(ht)*num + denom - 1) / denom
	px = alloc(al, sw, sh, 3, RGB)
	if turbo.decode(h, src, uint64(len(data)), unsafe.Pointer(&px.Buf[0]), int32(sw), int32(px.Stride), int32(sh), tjRGB, 0) != 0 {
		al.Free(px.Buf)
		return Pixels{}, 0, 0, fmt.Errorf("libturbojpeg: %s", turbo.errStr(h))
	}
	return px, int(w), int(ht), nil
}
