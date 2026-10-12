// Package hyprland implements Hyprland's toplevel-export protocol using a
// small standalone Wayland connection. It deliberately stays off GTK's
// Wayland connection and event loop.
package hyprland

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type Frame struct {
	Address               string
	Width, Height, Stride int
	Format                uint32
	YInvert               bool
	Pixels                []byte
}

// Stream is a live preview worker. SetTargets replaces the capture list;
// frame callbacks run on the worker goroutine.
type Stream struct {
	targets chan []string
	cancel  context.CancelFunc
}

func Start(ctx context.Context, targets []string, onFrame func(Frame), onError func(error)) *Stream {
	workerCtx, cancel := context.WithCancel(ctx)
	s := &Stream{targets: make(chan []string, 1), cancel: cancel}
	s.SetTargets(targets)
	go func() {
		if err := run(workerCtx, s.targets, onFrame); err != nil && onError != nil && workerCtx.Err() == nil {
			onError(err)
		}
	}()
	return s
}

func (s *Stream) SetTargets(targets []string) {
	if s == nil {
		return
	}
	copyTargets := append([]string(nil), targets...)
	select {
	case s.targets <- copyTargets:
	default:
		select {
		case <-s.targets:
		default:
		}
		s.targets <- copyTargets
	}
}

func (s *Stream) Stop() {
	if s == nil {
		return
	}
	s.cancel()
}

type buffer struct {
	id           uint32
	data         []byte
	w, h, stride int
	format       uint32
}

type frameState struct {
	id           uint32
	address      string
	w, h, stride int
	format       uint32
	flags        uint32
}

type client struct {
	conn             *net.UnixConn
	readTimeout      time.Duration
	next             uint32
	manager, shm     uint32
	foreign, mapping uint32
	handles          map[string]uint32
	mappings         map[uint32]uint32
	foreignHandles   map[uint32]bool
	frames           map[uint32]*frameState
	buffers          map[string]*buffer
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
		return "", errors.New("XDG_RUNTIME_DIR is unset")
	}
	return filepath.Join(runtime, name), nil
}

func run(ctx context.Context, updates <-chan []string, onFrame func(Frame)) error {
	path, err := socketPath()
	if err != nil {
		return err
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return fmt.Errorf("connect Wayland: %w", err)
	}
	defer conn.Close()
	c := &client{conn: conn, next: 6, frames: map[uint32]*frameState{}, buffers: map[string]*buffer{}, handles: map[string]uint32{}, mappings: map[uint32]uint32{}, foreignHandles: map[uint32]bool{}}
	defer c.releaseBuffers()
	if err := c.init(); err != nil {
		return err
	}
	targets := []string{}
	next := 0
	tick := time.NewTicker(120 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case t := <-updates:
			targets = unique(t)
			keep := map[string]bool{}
			for _, a := range targets {
				keep[a] = true
			}
			for a, b := range c.buffers {
				if !keep[a] {
					c.destroyBuffer(b)
					delete(c.buffers, a)
				}
			}
		case <-tick.C:
			if len(targets) == 0 {
				continue
			}
			if next >= len(targets) {
				next = 0
			}
			a := targets[next]
			next++
			if err := c.capture(a, onFrame); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				// A client can disappear while the overview is open. Drop that
				// frame and let the next state refresh update the target list.
				if !errors.Is(err, errCaptureFailed) {
					return err
				}
			}
		}
	}
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, a := range in {
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

func (c *client) init() error {
	if err := c.send(1, 1, u32(2)); err != nil {
		return err
	} // get_registry
	if err := c.send(1, 0, u32(3)); err != nil {
		return err
	} // sync
	type global struct {
		name, version uint32
		iface         string
	}
	globals := map[string]global{}
	for {
		obj, op, p, err := c.read()
		if err != nil {
			return fmt.Errorf("read Wayland registry: %w", err)
		}
		if obj == 2 && op == 0 {
			g, err := parseGlobal(p)
			if err != nil {
				return err
			}
			globals[g.iface] = g
		} else if obj == 3 && op == 0 {
			break
		} else if obj == 1 && op == 0 {
			return errors.New("Wayland compositor rejected the toplevel preview protocol")
		}
	}
	manager, ok := globals["hyprland_toplevel_export_manager_v1"]
	if !ok {
		return errors.New("Hyprland does not advertise hyprland_toplevel_export_v1")
	}
	shm, ok := globals["wl_shm"]
	if !ok {
		return errors.New("Wayland compositor does not advertise wl_shm")
	}
	c.manager, c.shm = 4, 5
	if err := c.send(2, 0, u32(manager.name), stringArg("hyprland_toplevel_export_manager_v1"), u32(min(manager.version, 2)), u32(c.manager)); err != nil {
		return err
	}
	if err := c.send(2, 0, u32(shm.name), stringArg("wl_shm"), u32(1), u32(c.shm)); err != nil {
		return err
	}
	foreign, ok := globals["zwlr_foreign_toplevel_manager_v1"]
	if !ok {
		return errors.New("foreign toplevel manager unavailable")
	}
	mapping, ok := globals["hyprland_toplevel_mapping_manager_v1"]
	if !ok {
		return errors.New("Hyprland toplevel mapping unavailable")
	}
	c.mapping, c.foreign = c.alloc(), c.alloc()
	if err := c.send(2, 0, u32(mapping.name), stringArg(mapping.iface), u32(1), u32(c.mapping)); err != nil {
		return err
	}
	if err := c.send(2, 0, u32(foreign.name), stringArg(foreign.iface), u32(min(foreign.version, 3)), u32(c.foreign)); err != nil {
		return err
	}
	syncID := c.alloc()
	if err := c.send(1, 0, u32(syncID)); err != nil {
		return err
	}
	for {
		obj, op, _, err := c.read()
		if err != nil {
			return err
		}
		if obj == syncID && op == 0 {
			break
		}
	}
	return c.roundtrip()
}

func parseGlobal(p []byte) (struct {
	name, version uint32
	iface         string
}, error) {
	var g struct {
		name, version uint32
		iface         string
	}
	if len(p) < 12 {
		return g, errors.New("short Wayland global event")
	}
	g.name = get32(p, 0)
	n := int(get32(p, 4))
	if n < 1 || 8+n > len(p) {
		return g, errors.New("invalid Wayland global interface")
	}
	end := 8 + n
	g.iface = string(p[8 : end-1])
	off := 8 + ((n + 3) &^ 3)
	if off+4 > len(p) {
		return g, errors.New("short Wayland global version")
	}
	g.version = get32(p, off)
	return g, nil
}

func (c *client) capture(address string, onFrame func(Frame)) error {
	handle := c.handles[address]
	if handle == 0 {
		if err := c.roundtrip(); err != nil {
			return err
		}
		handle = c.handles[address]
		if handle == 0 {
			return errCaptureFailed
		}
	}
	id := c.alloc()
	c.frames[id] = &frameState{id: id, address: address}
	if err := c.send(c.manager, 2, u32(id), u32(0), u32(handle)); err != nil {
		delete(c.frames, id)
		return err
	}
	previousTimeout := c.readTimeout
	c.readTimeout = 750 * time.Millisecond
	defer func() { c.readTimeout = previousTimeout }()
	for {
		obj, op, p, err := c.read()
		if err != nil {
			if errors.Is(err, errCaptureReadTimeout) {
				// A toplevel can stop producing capture events briefly while
				// Hyprland reparents it during a workspace move. Cancel only this
				// frame request; the stream will retry the client on a later tick.
				c.destroyFrame(id)
				return errCaptureFailed
			}
			delete(c.frames, id)
			return fmt.Errorf("capture %s: %w", address, err)
		}
		f := c.frames[obj]
		if f == nil {
			continue
		}
		switch op {
		case 0: // buffer(format,width,height,stride)
			if len(p) < 16 {
				continue
			}
			f.format = get32(p, 0)
			f.w = int(get32(p, 4))
			f.h = int(get32(p, 8))
			f.stride = int(get32(p, 12))
		case 2: // flags
			if len(p) >= 4 {
				f.flags = get32(p, 0)
			}
		case 6: // buffer_done (after linux_dmabuf in the event table)
			if f.w <= 0 || f.h <= 0 || f.stride < f.w*4 || f.h > 16384 || f.stride*f.h > 256<<20 {
				c.destroyFrame(id)
				return errors.New("invalid Hyprland capture buffer size")
			}
			b := c.buffers[address]
			if b == nil || b.w != f.w || b.h != f.h || b.stride != f.stride || b.format != f.format {
				if b != nil {
					c.destroyBuffer(b)
				}
				b, err = c.newBuffer(*f)
				if err != nil {
					c.destroyFrame(id)
					return err
				}
				c.buffers[address] = b
			}
			if err = c.send(id, 0, u32(b.id), u32(1)); err != nil {
				c.destroyFrame(id)
				return err
			}
		case 3: // ready
			b := c.buffers[address]
			if b != nil && onFrame != nil {
				onFrame(Frame{Address: address, Width: b.w, Height: b.h, Stride: b.stride, Format: b.format, YInvert: f.flags&1 != 0, Pixels: b.data})
			}
			c.destroyFrame(id)
			return nil
		case 4: // failed
			c.destroyFrame(id)
			return errCaptureFailed
		}
	}
}

var errCaptureFailed = errors.New("Hyprland could not capture this toplevel")
var errCaptureReadTimeout = errors.New("timed out waiting for a capture frame")

func (c *client) newBuffer(f frameState) (*buffer, error) {
	size := f.stride * f.h
	fd, err := unix.MemfdCreate("inua-toplevel-preview", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			unix.Close(fd)
		}
	}()
	if err = unix.Ftruncate(fd, int64(size)); err != nil {
		return nil, err
	}
	data, err := unix.Mmap(fd, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	pool, id := c.alloc(), c.alloc()
	if err = c.sendFD(c.shm, 0, fd, u32(pool), u32(uint32(size))); err != nil {
		unix.Munmap(data)
		return nil, err
	}
	if err = c.send(pool, 0, u32(id), u32(0), u32(uint32(f.w)), u32(uint32(f.h)), u32(uint32(f.stride)), u32(f.format)); err != nil {
		unix.Munmap(data)
		return nil, err
	}
	_ = c.send(pool, 1)
	_ = unix.Close(fd)
	cleanup = false
	return &buffer{id: id, data: data, w: f.w, h: f.h, stride: f.stride, format: f.format}, nil
}

func (c *client) destroyFrame(id uint32) { _ = c.send(id, 1); delete(c.frames, id) }
func (c *client) destroyBuffer(b *buffer) {
	if b == nil {
		return
	}
	_ = c.send(b.id, 0)
	if b.data != nil {
		_ = unix.Munmap(b.data)
	}
}
func (c *client) releaseBuffers() {
	for _, b := range c.buffers {
		c.destroyBuffer(b)
	}
}
func (c *client) alloc() uint32 { id := c.next; c.next++; return id }

func (c *client) send(obj uint32, op uint16, args ...[]byte) error {
	return c.sendFD(obj, op, -1, args...)
}
func (c *client) sendFD(obj uint32, op uint16, fd int, args ...[]byte) error {
	var p []byte
	for _, a := range args {
		p = append(p, a...)
	}
	size := 8 + len(p)
	if size%4 != 0 {
		return errors.New("unaligned Wayland request")
	}
	msg := make([]byte, size)
	put32(msg, 0, obj)
	put32(msg, 4, uint32(size)<<16|uint32(op))
	copy(msg[8:], p)
	if fd < 0 {
		return writeAll(c.conn, msg)
	}
	control := unix.UnixRights(fd)
	n, _, err := c.conn.WriteMsgUnix(msg, control, nil)
	if err != nil {
		return err
	}
	if n < len(msg) {
		err = writeAll(c.conn, msg[n:])
	}
	return err
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

func (c *client) readRaw() (uint32, uint16, []byte, error) {
	timeout := c.readTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	var head [8]byte
	if n, err := io.ReadFull(c.conn, head[:]); err != nil {
		if n == 0 {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return 0, 0, nil, errCaptureReadTimeout
			}
		}
		return 0, 0, nil, err
	}
	word := get32(head[:], 4)
	size := int(word >> 16)
	if size < 8 || size > 1<<20 || size%4 != 0 {
		return 0, 0, nil, errors.New("invalid Wayland event size")
	}
	p := make([]byte, size-8)
	if _, err := io.ReadFull(c.conn, p); err != nil {
		return 0, 0, nil, err
	}
	return get32(head[:], 0), uint16(word & 0xffff), p, nil
}

func u32(v uint32) []byte { b := make([]byte, 4); put32(b, 0, v); return b }
func stringArg(s string) []byte {
	n := len(s) + 1
	size := (n + 3) &^ 3
	b := make([]byte, 4+size)
	put32(b, 0, uint32(n))
	copy(b[4:], s)
	return b
}
func get32(b []byte, o int) uint32    { return binary.NativeEndian.Uint32(b[o : o+4]) }
func put32(b []byte, o int, v uint32) { binary.NativeEndian.PutUint32(b[o:o+4], v) }

// read processes foreign handles on the same connection that owns capture
// requests. Full 64-bit IPC addresses must never be used as protocol handles.
func (c *client) read() (uint32, uint16, []byte, error) {
	obj, op, p, err := c.readRaw()
	if err != nil {
		return obj, op, p, err
	}
	if obj == 1 && op == 0 {
		return 0, 0, nil, fmt.Errorf("Wayland protocol error: %x", p)
	}
	if obj == c.foreign && op == 0 && len(p) >= 4 {
		handle := get32(p, 0)
		c.foreignHandles[handle] = true
		mapping := c.alloc()
		c.mappings[mapping] = handle
		if err := c.send(c.mapping, 1, u32(mapping), u32(handle)); err != nil {
			return 0, 0, nil, err
		}
	}
	if handle, ok := c.mappings[obj]; ok {
		if op == 0 && len(p) >= 8 {
			address := uint64(get32(p, 0))<<32 | uint64(get32(p, 4))
			c.handles[fmt.Sprintf("0x%x", address)] = handle
		}
		_ = c.send(obj, 0)
		delete(c.mappings, obj)
	}
	if c.foreignHandles[obj] && op == 6 {
		for address, handle := range c.handles {
			if handle == obj {
				delete(c.handles, address)
			}
		}
		delete(c.foreignHandles, obj)
		_ = c.send(obj, 7)
	}
	return obj, op, p, nil
}

func (c *client) roundtrip() error {
	id := c.alloc()
	if err := c.send(1, 0, u32(id)); err != nil {
		return err
	}
	for {
		obj, op, _, err := c.read()
		if err != nil {
			return err
		}
		if obj == id && op == 0 {
			return nil
		}
	}
}
