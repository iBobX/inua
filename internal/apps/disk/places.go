package disk

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

// Place is a folder to scan from the index: the home, or a filesystem's
// mount point.
type Place struct {
	Path        string
	Name        string // "" for the home and the root, which the window names
	Total, Free int64  // the filesystem's
}

// filesystems are the kinds that hold files on a disk.
var filesystems = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "btrfs": true, "xfs": true, "f2fs": true, "zfs": true,
	"bcachefs": true, "vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "jfs": true, "reiserfs": true,
	"fuseblk": true,
}

// Places are the home, then each disk's filesystem mounted: the root, then
// the rest by mount point; not the one the home is in, which the home
// stands for. A device mounted twice (a btrfs volume's
// subvolumes) is listed at its first mount point.
func Places(home string) []Place {
	out := []Place{statPlace(Place{Path: home})}
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return out
	}
	defer f.Close()
	seen := map[string]bool{}
	var mounts []Place
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || !filesystems[fields[2]] {
			continue
		}
		dev, at := fields[0], unescapeMount(fields[1])
		// The folder holding the home (/home on a disk of its own) is the
		// home's place already.
		inHome := at != "/" && (home == at || strings.HasPrefix(home, at+"/"))
		if seen[dev] || inHome || strings.HasPrefix(at, "/boot") || strings.HasPrefix(at, "/efi") || strings.HasPrefix(at, "/snap/") {
			continue
		}
		seen[dev] = true
		name := filepath.Base(at)
		if at == "/" {
			name = ""
		}
		mounts = append(mounts, statPlace(Place{Path: at, Name: name}))
	}
	slices.SortStableFunc(mounts, func(a, b Place) int {
		switch {
		case a.Path == "/":
			return -1
		case b.Path == "/":
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return append(out, mounts...)
}

func statPlace(p Place) Place {
	var st unix.Statfs_t
	if unix.Statfs(p.Path, &st) == nil {
		p.Total = int64(st.Blocks) * st.Bsize
		p.Free = int64(st.Bavail) * st.Bsize
	}
	return p
}

// unescapeMount undoes /proc/mounts' octal escapes (\040 for a space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			v := 0
			ok := true
			for _, c := range s[i+1 : i+4] {
				if c < '0' || c > '7' {
					ok = false
					break
				}
				v = v*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
