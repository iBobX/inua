package workshop

import (
	"context"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Watch calls changed whenever a file in dir is added, removed or
// written, until ctx ends; it looks every interval. Hidden folders and
// caches (.git, __pycache__, node_modules) are left out.
func Watch(ctx context.Context, dir string, interval time.Duration, changed func()) {
	last := snapshot(dir)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := snapshot(dir)
		if now != last {
			last = now
			changed()
		}
	}
}

// snapshot sums up dir's files: their paths, sizes and times.
func snapshot(dir string) string {
	var b strings.Builder
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() && p != dir && (strings.HasPrefix(name, ".") || name == "__pycache__" || name == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			b.WriteString(p)
			b.WriteString(info.ModTime().String())
			b.WriteString(strconv.FormatInt(info.Size(), 10))
			b.WriteByte(0)
		}
		return nil
	})
	return b.String()
}
