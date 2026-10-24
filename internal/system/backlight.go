package system

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// BacklightDir is where the kernel lists backlight devices.
const BacklightDir = "/sys/class/backlight"

// exponent matches brightnessctl -e4, which the brightness keys use, so the
// slider and the keys agree on what a step is.
const exponent = 4

// Backlight reads the panel brightness from sysfs and sets it through logind,
// which allows the session user to without root.
type Backlight struct {
	dir  string
	name string
	max  float64
	dial Dialer
}

// FindBacklight returns the first backlight device under root.
func FindBacklight(root string, dial Dialer) (*Backlight, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		m, err := readInt(filepath.Join(dir, "max_brightness"))
		if err == nil && m > 0 {
			return &Backlight{dir: dir, name: e.Name(), max: float64(m), dial: dial}, nil
		}
	}
	return nil, errors.New("system: no backlight device")
}

func readInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// Percent is the perceived brightness, 0-100.
func (b *Backlight) Percent() (float64, error) {
	raw, err := readInt(filepath.Join(b.dir, "brightness"))
	if err != nil {
		return 0, err
	}
	return 100 * math.Pow(float64(raw)/b.max, 1.0/exponent), nil
}

// SetPercent sets the perceived brightness. It never turns the panel fully
// off.
func (b *Backlight) SetPercent(p float64) error {
	p = max(0, min(p, 100)) / 100
	raw := uint32(max(1, math.Round(b.max*math.Pow(p, exponent))))
	conn, err := b.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callLimit)
	defer cancel()
	return conn.Object("org.freedesktop.login1", "/org/freedesktop/login1/session/auto").
		CallWithContext(ctx, "org.freedesktop.login1.Session.SetBrightness", 0, "backlight", b.name, raw).Err
}

// Watch calls onChange with the perceived brightness whenever it changes,
// by whatever means (the brightness keys, the slider, another program): the
// kernel notifies actual_brightness on every write. It returns when ctx
// ends, or with an error when the file cannot be watched.
func (b *Backlight) Watch(ctx context.Context, onChange func(float64)) error {
	f, err := os.Open(filepath.Join(b.dir, "actual_brightness"))
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 32)
	if _, err := f.ReadAt(buf, 0); err != nil && !errors.Is(err, io.EOF) {
		return err // a sysfs attribute is armed by reading it
	}
	last, _ := b.Percent()
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLPRI | unix.POLLERR}}
	for ctx.Err() == nil {
		n, err := unix.Poll(fds, 500) // wakes to see ctx end
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if _, err := f.ReadAt(buf, 0); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if p, err := b.Percent(); err == nil && math.Abs(p-last) >= 0.5 {
			last = p
			onChange(p)
		}
	}
	return nil
}
