// Package disks is the file manager's view of drives through UDisks2 on
// the system bus: the volumes there are (USB sticks, SD cards, other
// disks' partitions, LUKS containers), mounting and unmounting them,
// unlocking encrypted ones, and ejecting or powering off a drive so it
// can be pulled out. Polkit asks for a password where one is needed (the
// shell is the session's polkit agent).
package disks

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	service   = "org.freedesktop.UDisks2"
	root      = dbus.ObjectPath("/org/freedesktop/UDisks2")
	ifBlock   = service + ".Block"
	ifFS      = service + ".Filesystem"
	ifCrypt   = service + ".Encrypted"
	ifDrive   = service + ".Drive"
	ifPart    = service + ".Partition"
	ifObjects = "org.freedesktop.DBus.ObjectManager"
)

// Volume is a filesystem, or a locked encrypted container, the user may
// open.
type Volume struct {
	Path        dbus.ObjectPath // the block device's object
	Device      string          // /dev/sdb1
	Label       string
	FSType      string // vfat, ext4, crypto_LUKS …
	UUID        string
	Size        uint64
	MountPoints []string
	ReadOnly    bool
	Encrypted   bool            // a container; open it with Unlock
	Cleartext   dbus.ObjectPath // an unlocked container's filesystem
	Backing     dbus.ObjectPath // the container a cleartext filesystem is in
	System      bool            // UDisks2's hint: part of the system
	Icon        string          // UDisks2's icon hint, if any

	Drive Drive
}

// Name is what the sidebar calls a volume: its label, else its drive and
// size.
func (v Volume) Name() string {
	if v.Label != "" {
		return v.Label
	}
	if d := strings.TrimSpace(v.Drive.Vendor + " " + v.Drive.Model); d != "" {
		return fmt.Sprintf("%s %s", d, Size(v.Size))
	}
	return Size(v.Size) + " " + cmp.Or(v.FSType, "volume")
}

// Mounted says whether the volume is mounted somewhere.
func (v Volume) Mounted() bool { return len(v.MountPoints) > 0 }

// Drive is the hardware a volume is on.
type Drive struct {
	Path        dbus.ObjectPath
	Vendor      string
	Model       string
	Bus         string // usb, sdio, ata, nvme …
	Removable   bool   // the drive or its media can be taken out
	Ejectable   bool
	CanPowerOff bool
}

// Client talks to UDisks2.
type Client struct{ conn *dbus.Conn }

// New uses conn, the system bus (or a test's).
func New(conn *dbus.Conn) *Client { return &Client{conn: conn} }

type managed = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// Volumes reads the volumes to show: filesystems and encrypted
// containers that UDisks2 does not hint to ignore, leaving out the
// system's own unless they are mounted somewhere the user browses.
// Removable drives' volumes come first, each drive's together.
func (c *Client) Volumes(ctx context.Context) ([]Volume, error) {
	var objs managed
	if err := c.conn.Object(service, root).CallWithContext(ctx, ifObjects+".GetManagedObjects", 0).Store(&objs); err != nil {
		return nil, err
	}
	drives := map[dbus.ObjectPath]Drive{}
	for path, ifs := range objs {
		if d, ok := ifs[ifDrive]; ok {
			drives[path] = Drive{
				Path:        path,
				Vendor:      str(d["Vendor"]),
				Model:       str(d["Model"]),
				Bus:         str(d["ConnectionBus"]),
				Removable:   boolean(d["Removable"]) || boolean(d["MediaRemovable"]),
				Ejectable:   boolean(d["Ejectable"]),
				CanPowerOff: boolean(d["CanPowerOff"]),
			}
		}
	}
	var out []Volume
	for path, ifs := range objs {
		b, ok := ifs[ifBlock]
		if !ok || boolean(b["HintIgnore"]) {
			continue
		}
		usage := str(b["IdUsage"])
		_, isFS := ifs[ifFS]
		_, isCrypt := ifs[ifCrypt]
		if !(usage == "filesystem" && isFS) && !(usage == "crypto" && isCrypt) {
			continue
		}
		v := Volume{
			Path:     path,
			Device:   bytesStr(b["PreferredDevice"]),
			Label:    cmp.Or(str(b["HintName"]), str(b["IdLabel"])),
			FSType:   str(b["IdType"]),
			UUID:     str(b["IdUUID"]),
			Size:     u64(b["Size"]),
			ReadOnly: boolean(b["ReadOnly"]),
			System:   boolean(b["HintSystem"]),
			Icon:     str(b["HintSymbolicIconName"]),
			Backing:  objPath(b["CryptoBackingDevice"]),
		}
		if v.Device == "" {
			v.Device = bytesStr(b["Device"])
		}
		v.Drive = drives[objPath(b["Drive"])]
		if v.Backing != "/" && v.Backing != "" {
			// A cleartext filesystem is on its container's drive.
			if bb, ok := objs[v.Backing][ifBlock]; ok {
				v.Drive = drives[objPath(bb["Drive"])]
			}
		} else {
			v.Backing = ""
		}
		if fs, ok := ifs[ifFS]; ok {
			v.MountPoints = mountPoints(fs["MountPoints"])
		}
		if e, ok := ifs[ifCrypt]; ok && usage == "crypto" {
			v.Encrypted = true
			if p := objPath(e["CleartextDevice"]); p != "/" {
				v.Cleartext = p
			}
		}
		if v.System && !userMounted(v.MountPoints) {
			continue
		}
		if v.Encrypted && v.Cleartext != "" {
			continue // the unlocked filesystem stands for it
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b Volume) int {
		if a.Drive.Removable != b.Drive.Removable {
			if a.Drive.Removable {
				return -1
			}
			return 1
		}
		// A drive's volumes together, in the order of its partitions.
		return cmp.Or(cmp.Compare(a.Drive.Path, b.Drive.Path), cmp.Compare(a.Device, b.Device))
	})
	return out, nil
}

// userMounted says whether a system volume is mounted somewhere the user
// browses rather than the system's own places.
func userMounted(points []string) bool {
	for _, p := range points {
		if strings.HasPrefix(p, "/media/") || strings.HasPrefix(p, "/run/media/") || strings.HasPrefix(p, "/mnt/") {
			return true
		}
	}
	return false
}

// options are the a{sv} the methods take; interaction is allowed, so
// polkit may ask for a password.
func options() map[string]dbus.Variant { return map[string]dbus.Variant{} }

// Mount mounts a filesystem where UDisks2 chooses (/media/USER/LABEL)
// and returns where.
func (c *Client) Mount(ctx context.Context, v Volume) (string, error) {
	if v.Mounted() {
		return v.MountPoints[0], nil
	}
	var where string
	err := c.conn.Object(service, v.Path).CallWithContext(ctx, ifFS+".Mount", 0, options()).Store(&where)
	return where, explain(err)
}

// Unmount unmounts a filesystem.
func (c *Client) Unmount(ctx context.Context, v Volume) error {
	return explain(c.conn.Object(service, v.Path).CallWithContext(ctx, ifFS+".Unmount", 0, options()).Err)
}

// Unlock opens an encrypted container with its passphrase and returns
// the filesystem inside.
func (c *Client) Unlock(ctx context.Context, v Volume, passphrase string) (dbus.ObjectPath, error) {
	var clear dbus.ObjectPath
	err := c.conn.Object(service, v.Path).CallWithContext(ctx, ifCrypt+".Unlock", 0, passphrase, options()).Store(&clear)
	return clear, explain(err)
}

// Lock closes an unlocked container.
func (c *Client) Lock(ctx context.Context, container dbus.ObjectPath) error {
	return explain(c.conn.Object(service, container).CallWithContext(ctx, ifCrypt+".Lock", 0, options()).Err)
}

// Eject makes a volume's drive safe to pull out: it unmounts every
// filesystem on the drive, locks its unlocked containers, then ejects the
// media or powers the drive off, as the drive allows.
func (c *Client) Eject(ctx context.Context, v Volume) error {
	if v.Drive.Path == "" || v.Drive.Path == "/" {
		if v.Mounted() {
			return c.Unmount(ctx, v)
		}
		return nil
	}
	var objs managed
	if err := c.conn.Object(service, root).CallWithContext(ctx, ifObjects+".GetManagedObjects", 0).Store(&objs); err != nil {
		return err
	}
	onDrive := func(ifs map[string]map[string]dbus.Variant) bool {
		b, ok := ifs[ifBlock]
		if !ok {
			return false
		}
		if objPath(b["Drive"]) == v.Drive.Path {
			return true
		}
		if back := objPath(b["CryptoBackingDevice"]); back != "/" {
			if bb, ok := objs[back][ifBlock]; ok {
				return objPath(bb["Drive"]) == v.Drive.Path
			}
		}
		return false
	}
	// Filesystems first (cleartext ones included), then their containers.
	for path, ifs := range objs {
		if fs, ok := ifs[ifFS]; ok && onDrive(ifs) && len(mountPoints(fs["MountPoints"])) > 0 {
			if err := c.Unmount(ctx, Volume{Path: path}); err != nil {
				return err
			}
		}
	}
	for path, ifs := range objs {
		if e, ok := ifs[ifCrypt]; ok && onDrive(ifs) && objPath(e["CleartextDevice"]) != "/" {
			if err := c.Lock(ctx, path); err != nil {
				return err
			}
		}
	}
	drive := c.conn.Object(service, v.Drive.Path)
	if v.Drive.Ejectable {
		if err := drive.CallWithContext(ctx, ifDrive+".Eject", 0, options()).Err; err != nil {
			return explain(err)
		}
	}
	if v.Drive.CanPowerOff {
		// Some readers cannot power off once ejected; ejecting was enough.
		if err := drive.CallWithContext(ctx, ifDrive+".PowerOff", 0, options()).Err; err != nil && !v.Drive.Ejectable {
			return explain(err)
		}
	}
	return nil
}

// Watch calls changed after UDisks2's objects change (a drive plugged or
// pulled, a volume mounted elsewhere), merged within a moment, until ctx
// ends.
func (c *Client) Watch(ctx context.Context, changed func()) error {
	rules := []dbus.MatchOption{dbus.WithMatchSender(service)}
	if err := c.conn.AddMatchSignal(append(rules, dbus.WithMatchInterface(ifObjects))...); err != nil {
		return err
	}
	if err := c.conn.AddMatchSignal(append(rules, dbus.WithMatchInterface("org.freedesktop.DBus.Properties"), dbus.WithMatchMember("PropertiesChanged"))...); err != nil {
		return err
	}
	ch := make(chan *dbus.Signal, 64)
	c.conn.Signal(ch)
	go func() {
		defer c.conn.RemoveSignal(ch)
		var timer <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case s := <-ch:
				if s == nil {
					return
				}
				if !strings.HasPrefix(string(s.Path), string(root)) {
					continue
				}
				if timer == nil {
					timer = time.After(150 * time.Millisecond)
				}
			case <-timer:
				timer = nil
				changed()
			}
		}
	}()
	return nil
}

// ErrNotAuthorized is returned when polkit refused, or the password
// dialog was cancelled.
var ErrNotAuthorized = errors.New("not authorized")

// ErrBusy is returned when a filesystem cannot be unmounted because
// something has a file open on it.
var ErrBusy = errors.New("in use")

// explain turns UDisks2's errors into the ones the window tells apart.
func explain(err error) error {
	var de dbus.Error
	if !errors.As(err, &de) {
		var dp *dbus.Error
		if !errors.As(err, &dp) {
			return err
		}
		de = *dp
	}
	switch de.Name {
	case "org.freedesktop.UDisks2.Error.NotAuthorized",
		"org.freedesktop.UDisks2.Error.NotAuthorizedCanObtain",
		"org.freedesktop.UDisks2.Error.NotAuthorizedDismissed",
		"org.freedesktop.UDisks2.Error.Cancelled":
		return fmt.Errorf("%w: %v", ErrNotAuthorized, de.Error())
	case "org.freedesktop.UDisks2.Error.DeviceBusy":
		return fmt.Errorf("%w: %v", ErrBusy, de.Error())
	}
	return err
}

// Size writes a byte count as the sidebar shows it: 32 GB, 1.5 TB.
func Size(n uint64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n), 0
	for v >= unit && i < 5 {
		v /= unit
		i++
	}
	s := fmt.Sprintf("%.1f", v)
	if v >= 10 {
		s = fmt.Sprintf("%.0f", v)
	}
	return strings.TrimSuffix(s, ".0") + " " + []string{"B", "kB", "MB", "GB", "TB", "PB"}[i]
}

func str(v dbus.Variant) string {
	s, _ := v.Value().(string)
	return s
}

func boolean(v dbus.Variant) bool {
	b, _ := v.Value().(bool)
	return b
}

func u64(v dbus.Variant) uint64 {
	n, _ := v.Value().(uint64)
	return n
}

func objPath(v dbus.Variant) dbus.ObjectPath {
	p, ok := v.Value().(dbus.ObjectPath)
	if !ok || p == "" {
		return "/"
	}
	return p
}

// bytesStr reads a NUL-terminated byte array (ay), as UDisks2 sends
// device names and mount points.
func bytesStr(v dbus.Variant) string {
	b, _ := v.Value().([]byte)
	return strings.TrimRight(string(b), "\x00")
}

func mountPoints(v dbus.Variant) []string {
	raw, _ := v.Value().([][]byte)
	var out []string
	for _, b := range raw {
		if s := strings.TrimRight(string(b), "\x00"); s != "" {
			out = append(out, s)
		}
	}
	return out
}
