// Package vinput is a pointer and a keyboard of no device: input from
// elsewhere (a phone as a touchpad) given to the compositor through its
// virtual pointer and virtual keyboard protocols, on a Wayland connection
// of its own. The keyboard types any character: it gives the compositor a
// keymap holding the characters it was asked for.
package vinput

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Mouse buttons, as Linux numbers them.
const (
	ButtonLeft   = 0x110
	ButtonRight  = 0x111
	ButtonMiddle = 0x112
)

// Modifiers held while a key is pressed.
type Modifiers struct{ Ctrl, Alt, Shift, Super bool }

// The modifiers' masks in the keymap's types ("complete").
const (
	modShift = 1
	modCtrl  = 4
	modAlt   = 8
	modSuper = 64
)

// Input is the virtual pointer and keyboard.
type Input struct {
	conn  *net.UnixConn
	start time.Time

	mu       sync.Mutex
	next     uint32
	pointer  uint32
	keyboard uint32
	keysyms  []string       // the keymap's keys: evdev code i+1 is keysyms[i]
	index    map[string]int // a keysym's place in keysyms
	err      error
}

// Open connects to the compositor and makes the pointer and keyboard.
func Open() (*Input, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("vinput: connect Wayland: %w", err)
	}
	in := &Input{conn: conn, start: time.Now(), next: 4, index: map[string]int{}}
	for _, k := range common() {
		in.index[k] = len(in.keysyms)
		in.keysyms = append(in.keysyms, k)
	}
	if err := in.init(); err != nil {
		conn.Close()
		return nil, err
	}
	go in.drain()
	return in, nil
}

func socketPath() (string, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		name = "wayland-0"
	}
	if filepath.IsAbs(name) {
		return name, nil
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return "", errors.New("vinput: XDG_RUNTIME_DIR is unset")
	}
	return filepath.Join(runtime, name), nil
}

// Close takes the pointer and keyboard away.
func (in *Input) Close() error { return in.conn.Close() }

func (in *Input) init() error {
	if err := in.send(1, 1, u32(2)); err != nil { // get_registry
		return err
	}
	if err := in.send(1, 0, u32(3)); err != nil { // sync
		return err
	}
	type global struct{ name, version uint32 }
	globals := map[string]global{}
	in.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		obj, op, p, err := in.read()
		if err != nil {
			return fmt.Errorf("vinput: the Wayland registry: %w", err)
		}
		if obj == 2 && op == 0 && len(p) >= 12 {
			n := int(get32(p, 4))
			if n < 1 || 8+((n+3)&^3)+4 > len(p) {
				continue
			}
			globals[string(p[8:8+n-1])] = global{get32(p, 0), get32(p, 8+((n+3)&^3))}
		} else if obj == 3 && op == 0 {
			break
		}
	}
	in.conn.SetReadDeadline(time.Time{})
	seat, ok1 := globals["wl_seat"]
	vp, ok2 := globals["zwlr_virtual_pointer_manager_v1"]
	vk, ok3 := globals["zwp_virtual_keyboard_manager_v1"]
	if !ok1 || !ok2 || !ok3 {
		return errors.New("vinput: the compositor has no virtual pointer or keyboard")
	}
	seatID, vpID, vkID := in.alloc(), in.alloc(), in.alloc()
	in.pointer, in.keyboard = in.alloc(), in.alloc()
	for _, b := range []struct {
		g    global
		name string
		id   uint32
	}{{seat, "wl_seat", seatID}, {vp, "zwlr_virtual_pointer_manager_v1", vpID}, {vk, "zwp_virtual_keyboard_manager_v1", vkID}} {
		if err := in.send(2, 0, u32(b.g.name), stringArg(b.name), u32(1), u32(b.id)); err != nil {
			return err
		}
	}
	if err := in.send(vpID, 0, u32(seatID), u32(in.pointer)); err != nil {
		return err
	}
	if err := in.send(vkID, 0, u32(seatID), u32(in.keyboard)); err != nil {
		return err
	}
	return in.uploadKeymap()
}

// drain reads the compositor's events, which say nothing needed but must
// be read; a protocol error ends the input.
func (in *Input) drain() {
	for {
		obj, op, p, err := in.read()
		if err == nil && obj == 1 && op == 0 {
			err = fmt.Errorf("vinput: Wayland protocol error %x", p)
		}
		if err != nil {
			in.mu.Lock()
			in.err = err
			in.mu.Unlock()
			return
		}
	}
}

// Err is why the input stopped working, or nil.
func (in *Input) Err() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.err
}

func (in *Input) now() uint32 { return uint32(time.Since(in.start).Milliseconds()) }

// Move moves the pointer by dx, dy.
func (in *Input) Move(dx, dy float64) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if err := in.send(in.pointer, 0, u32(in.now()), fixed(dx), fixed(dy)); err != nil {
		return err
	}
	return in.send(in.pointer, 4)
}

// Button presses or releases a mouse button.
func (in *Input) Button(button uint32, down bool) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	state := uint32(0)
	if down {
		state = 1
	}
	if err := in.send(in.pointer, 2, u32(in.now()), u32(button), u32(state)); err != nil {
		return err
	}
	return in.send(in.pointer, 4)
}

// Click presses and releases a button, n times.
func (in *Input) Click(button uint32, n int) error {
	for range n {
		if err := in.Button(button, true); err != nil {
			return err
		}
		if err := in.Button(button, false); err != nil {
			return err
		}
	}
	return nil
}

// Scroll scrolls by dx, dy, as a finger on a touchpad does.
func (in *Input) Scroll(dx, dy float64) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	t := in.now()
	if err := in.send(in.pointer, 5, u32(1)); err != nil { // axis_source finger
		return err
	}
	for axis, v := range []float64{dy, dx} {
		if v != 0 {
			if err := in.send(in.pointer, 3, u32(t), u32(uint32(axis)), fixed(v)); err != nil {
				return err
			}
		}
	}
	return in.send(in.pointer, 4)
}

// Key types the key named by keysym (an X keysym's name: "Return",
// "BackSpace", "F5"), with mods held.
func (in *Input) Key(keysym string, mods Modifiers) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	code, err := in.code(keysym)
	if err != nil {
		return err
	}
	return in.tap(code, mods)
}

// Type types text, a character at a time; mods are held for each.
func (in *Input) Type(text string, mods Modifiers) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, r := range text {
		code, err := in.code(Keysym(r))
		if err != nil {
			return err
		}
		if err := in.tap(code, mods); err != nil {
			return err
		}
	}
	return nil
}

func (in *Input) tap(code uint32, mods Modifiers) error {
	var m uint32
	if mods.Shift {
		m |= modShift
	}
	if mods.Ctrl {
		m |= modCtrl
	}
	if mods.Alt {
		m |= modAlt
	}
	if mods.Super {
		m |= modSuper
	}
	t := in.now()
	if m != 0 {
		if err := in.send(in.keyboard, 2, u32(m), u32(0), u32(0), u32(0)); err != nil {
			return err
		}
	}
	if err := in.send(in.keyboard, 1, u32(t), u32(code), u32(1)); err != nil {
		return err
	}
	if err := in.send(in.keyboard, 1, u32(t), u32(code), u32(0)); err != nil {
		return err
	}
	if m != 0 {
		return in.send(in.keyboard, 2, u32(0), u32(0), u32(0), u32(0))
	}
	return nil
}

// Keysym is the keysym's name for the character r.
func Keysym(r rune) string {
	switch r {
	case '\n', '\r':
		return "Return"
	case '\t':
		return "Tab"
	case '\b':
		return "BackSpace"
	case ' ':
		return "space"
	}
	if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
		return string(r)
	}
	return fmt.Sprintf("U%04X", r)
}

// common are the keys the keymap holds from the start: the special keys
// and every printable ASCII character. A key outside them changes the
// keymap, which the focused window takes a moment to follow, and which
// closes a menu that is open: most typing never does.
func common() []string {
	keys := []string{"Return", "Tab", "BackSpace", "Escape", "Delete", "Insert", "space",
		"Left", "Up", "Right", "Down", "Home", "End", "Page_Up", "Page_Down", "Menu", "Linefeed",
		"Control_L", "Alt_L", "Shift_L", "Super_L", "Sys_Req", "Scroll_Lock", "Print"}
	for i := 1; i <= 12; i++ {
		keys = append(keys, fmt.Sprintf("F%d", i))
	}
	for r := rune('!'); r <= '~'; r++ {
		keys = append(keys, Keysym(r))
	}
	return keys
}

// maxKeys bounds the keymap; past it, it starts again from the common
// keys.
const maxKeys = 240

// code is the key that types keysym, the keymap given anew when the key
// is new.
func (in *Input) code(keysym string) (uint32, error) {
	if i, ok := in.index[keysym]; ok {
		return uint32(i + 1), nil
	}
	if len(in.keysyms) >= maxKeys {
		in.keysyms, in.index = nil, map[string]int{}
		for _, k := range common() {
			in.index[k] = len(in.keysyms)
			in.keysyms = append(in.keysyms, k)
		}
	}
	in.index[keysym] = len(in.keysyms)
	in.keysyms = append(in.keysyms, keysym)
	if err := in.uploadKeymap(); err != nil {
		return 0, err
	}
	return uint32(len(in.keysyms)), nil
}

// Keymap is the XKB keymap with keysyms on keys 9, 10 and so on (evdev
// 1, 2…).
func Keymap(keysyms []string) string {
	var b strings.Builder
	b.WriteString("xkb_keymap {\nxkb_keycodes \"(unnamed)\" {\nminimum = 8;\n")
	fmt.Fprintf(&b, "maximum = %d;\n", max(len(keysyms)+9, 9))
	for i := range keysyms {
		fmt.Fprintf(&b, "<K%d> = %d;\n", i+1, i+9)
	}
	b.WriteString("};\nxkb_types \"(unnamed)\" { include \"complete\" };\n")
	b.WriteString("xkb_compatibility \"(unnamed)\" { include \"complete\" };\n")
	b.WriteString("xkb_symbols \"(unnamed)\" {\n")
	for i, k := range keysyms {
		fmt.Fprintf(&b, "key <K%d> {[%s]};\n", i+1, k)
	}
	b.WriteString("};\n};\n")
	return b.String()
}

func (in *Input) uploadKeymap() error {
	km := Keymap(in.keysyms)
	fd, err := unix.MemfdCreate("inua-keymap", unix.MFD_CLOEXEC)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	data := append([]byte(km), 0)
	if _, err := unix.Write(fd, data); err != nil {
		return err
	}
	return in.sendFD(in.keyboard, 0, fd, u32(1), u32(uint32(len(data)))) // XKB v1
}

func (in *Input) alloc() uint32 { id := in.next; in.next++; return id }

func (in *Input) send(obj uint32, op uint16, args ...[]byte) error {
	return in.sendFD(obj, op, -1, args...)
}

func (in *Input) sendFD(obj uint32, op uint16, fd int, args ...[]byte) error {
	var p []byte
	for _, a := range args {
		p = append(p, a...)
	}
	msg := make([]byte, 8+len(p))
	put32(msg, 0, obj)
	put32(msg, 4, uint32(len(msg))<<16|uint32(op))
	copy(msg[8:], p)
	var control []byte
	if fd >= 0 {
		control = unix.UnixRights(fd)
	}
	n, _, err := in.conn.WriteMsgUnix(msg, control, nil)
	if err == nil && n < len(msg) {
		_, err = in.conn.Write(msg[n:])
	}
	return err
}

func (in *Input) read() (uint32, uint16, []byte, error) {
	var head [8]byte
	if _, err := io.ReadFull(in.conn, head[:]); err != nil {
		return 0, 0, nil, err
	}
	word := get32(head[:], 4)
	size := int(word >> 16)
	if size < 8 || size%4 != 0 {
		return 0, 0, nil, errors.New("vinput: a Wayland event of a wrong size")
	}
	p := make([]byte, size-8)
	if _, err := io.ReadFull(in.conn, p); err != nil {
		return 0, 0, nil, err
	}
	return get32(head[:], 0), uint16(word & 0xffff), p, nil
}

func u32(v uint32) []byte { b := make([]byte, 4); put32(b, 0, v); return b }

// fixed is v in Wayland's 24.8 fixed point.
func fixed(v float64) []byte { return u32(uint32(int32(math.Round(v * 256)))) }

func stringArg(s string) []byte {
	n := len(s) + 1
	b := make([]byte, 4+(n+3)&^3)
	put32(b, 0, uint32(n))
	copy(b[4:], s)
	return b
}

func get32(b []byte, o int) uint32    { return binary.NativeEndian.Uint32(b[o : o+4]) }
func put32(b []byte, o int, v uint32) { binary.NativeEndian.PutUint32(b[o:o+4], v) }
