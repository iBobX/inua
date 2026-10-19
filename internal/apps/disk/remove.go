package disk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/lyricat/inua/internal/apps/files"
)

// Trash moves path to the trash, as Files does: its room is freed when
// the trash is emptied.
func Trash(path string) error {
	_, err := files.Trash(path)
	return err
}

// Delete deletes path and everything in it, for good. Folders made
// read-only by their program (Go's module cache) are made writable first,
// as the user may change them.
func Delete(path string) error {
	err := os.RemoveAll(path)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			if fi, err := d.Info(); err == nil && fi.Mode().Perm()&0o200 == 0 {
				os.Chmod(p, fi.Mode().Perm()|0o700)
			}
		}
		return nil
	})
	return os.RemoveAll(path)
}
