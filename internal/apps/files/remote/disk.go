package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Disk is a remote disk the user connected to: its name, which is its
// folder in ~/Remote, and its address. Its secret is in the keyring.
type Disk struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// Dir is the folder the disks are mounted in: ~/Remote.
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	return filepath.Join(home, "Remote")
}

// ErrName is a name that cannot be a folder of its own in ~/Remote.
var ErrName = errors.New("a remote disk's name is one folder's name")

// CheckName refuses a name that is not one folder's: empty, a dot first,
// a slash in it, or too long.
func CheckName(name string) error {
	switch {
	case strings.TrimSpace(name) == "", strings.HasPrefix(name, "."),
		strings.ContainsAny(name, "/\x00\r\n"), len(name) > 64:
		return ErrName
	}
	return nil
}

// FreeName is name, or name with a number after it, that none of disks
// is called.
func FreeName(name string, disks []Disk) string {
	taken := map[string]bool{}
	for _, d := range disks {
		taken[d.Name] = true
	}
	if !taken[name] {
		return name
	}
	for n := 2; ; n++ {
		if c := name + " " + strconv.Itoa(n); !taken[c] {
			return c
		}
	}
}

// Find is the disk at address, if one of disks is.
func Find(disks []Disk, address string) (Disk, bool) {
	a, err := Parse(address)
	if err != nil {
		return Disk{}, false
	}
	for _, d := range disks {
		if b, err := Parse(d.Address); err == nil && b == a {
			return d, true
		}
	}
	return Disk{}, false
}

// unitName is the systemd user unit that runs a disk's rclone, its name
// escaped as systemd-escape does.
func unitName(name string) string {
	var b strings.Builder
	b.WriteString("inua-remote-")
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == ':', c == '_', c == '.' && i > 0:
			b.WriteByte(c)
		default:
			b.WriteString(`\x`)
			b.WriteString(strconv.FormatUint(uint64(c)>>4, 16))
			b.WriteString(strconv.FormatUint(uint64(c)&0xf, 16))
		}
	}
	b.WriteString(".service")
	return b.String()
}
