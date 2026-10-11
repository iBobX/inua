package system

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
